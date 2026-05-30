package opentile

import (
	"context"
	"fmt"
	"image"
	"io"
	"sync"
	"sync/atomic"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// StripIterator yields horizontal strips of a slide scaled to a
// target output resolution. Not safe for concurrent use; one
// goroutine drives Next() / Close() per iterator.
type StripIterator struct {
	// Construction-time constants.
	slide       *Slide
	imageIdx    int
	l0Rect      image.Rectangle
	outSize     image.Point
	stripHeight int
	cfg         stripConfig

	// Resolved at init.
	sourceLevel Level // chosen pyramid level
	idctScale   int  // 1, 2, 4, or 8 (JPEG); 1 otherwise
	stripsTotal int  // ceil(outSize.Y / stripHeight)

	// Runtime state.
	mu        sync.Mutex
	nextStrip int  // 0-based index of the next strip to yield
	closed    bool

	// Internal pipeline.
	cache     *tileCache
	cancelCtx context.Context    // derived from cfg.ctx + iterator's own cancel
	cancelFn  context.CancelFunc

	// Tile dispatch channel — lookahead goroutine pushes (tx, ty)
	// requests; workers pop them, decode, and put into cache.
	tileReqs      chan tileKey
	tileReqsOpen  atomic.Bool // true while tileReqs is open for sends

	// Worker pool wait group.
	workersDone sync.WaitGroup

	// Lookahead goroutine wait group + signal channel.
	lookaheadDone sync.WaitGroup
	advance       chan struct{} // signaled when nextStrip advances
}

func newStripIterator(s *Slide, imageIdx int, l0Rect image.Rectangle, outSize image.Point, stripHeight int, cfg stripConfig) *StripIterator {
	it := &StripIterator{
		slide:       s,
		imageIdx:    imageIdx,
		l0Rect:      l0Rect,
		outSize:     outSize,
		stripHeight: stripHeight,
		cfg:         cfg,
	}

	if outSize.X <= 0 || outSize.Y <= 0 || stripHeight <= 0 {
		it.stripsTotal = 0
		return it
	}

	it.stripsTotal = (outSize.Y + stripHeight - 1) / stripHeight

	// Select source level.
	level := s.bestLevelForRegion(imageIdx, l0Rect, outSize)
	it.sourceLevel = level

	// Select IDCT scale.
	if cfg.idctScale != 0 {
		it.idctScale = cfg.idctScale
	} else {
		it.idctScale = autoIDCTScale(level, l0Rect, outSize)
	}
	if it.idctScale == 0 {
		it.idctScale = 1
	}

	// Cache size: byte-budget-derived, floored at max(workers,8) and
	// capped at the original count formula. The count formula
	// (workers × (lookahead+1) × tilesPerStripWidth) is the upper
	// bound; the byte budget shrinks it on wide levels so the C1
	// decoded-tile cache cannot balloon with slide width (v0.30).
	tilesPerStripWidth := 1
	if level.TileSize.W > 0 {
		tilesPerStripWidth = (level.Size.W + level.TileSize.W - 1) / level.TileSize.W
	}
	countFormulaCap := cfg.workers * (cfg.lookahead + 1) * tilesPerStripWidth
	if countFormulaCap < 8 {
		countFormulaCap = 8
	}
	// Per-tile bytes at the decoded (post-IDCT) resolution the workers
	// actually cache; 3 bytes/px RGB. idctScale shrinks both dims.
	scale := it.idctScale
	if scale < 1 {
		scale = 1
	}
	bytesPerTile := int64((level.TileSize.W/scale)*(level.TileSize.H/scale)) * 3
	if bytesPerTile < 1 {
		bytesPerTile = 1
	}
	budget := s.readBudget
	if budget < 1 {
		budget = defaultReadMemoryBudget
	}
	cacheCapacity := stripCacheCapacity(budget, bytesPerTile, cfg.workers, countFormulaCap)
	it.cache = newTileCache(cacheCapacity)

	// Wire up cancellation.
	parentCtx := cfg.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	it.cancelCtx, it.cancelFn = context.WithCancel(parentCtx)

	// Start worker pool + lookahead.
	it.tileReqs = make(chan tileKey, cfg.workers*2)
	it.tileReqsOpen.Store(true)
	it.advance = make(chan struct{}, 1)

	// Start workers.
	for i := 0; i < cfg.workers; i++ {
		it.workersDone.Add(1)
		go it.decodeWorker()
	}

	// Start lookahead.
	it.lookaheadDone.Add(1)
	go it.lookahead()

	return it
}

// Strips returns the total number of strips this iterator will yield.
func (it *StripIterator) Strips() int {
	return it.stripsTotal
}

// Next returns the next decoded strip image. Returns io.EOF when all
// strips have been consumed, and io.ErrClosedPipe after Close.
func (it *StripIterator) Next() (*decoder.Image, error) {
	it.mu.Lock()
	if it.closed {
		it.mu.Unlock()
		return nil, io.ErrClosedPipe
	}
	if it.nextStrip >= it.stripsTotal {
		it.mu.Unlock()
		return nil, io.EOF
	}
	stripIdx := it.nextStrip
	it.nextStrip++
	it.mu.Unlock()

	// Signal lookahead that consumer advanced.
	select {
	case it.advance <- struct{}{}:
	default:
	}

	// Compute output strip dimensions.
	outY0 := stripIdx * it.stripHeight
	outY1 := outY0 + it.stripHeight
	if outY1 > it.outSize.Y {
		outY1 = it.outSize.Y
	}
	stripH := outY1 - outY0
	stripImg := decoder.NewImageFormat(it.outSize.X, stripH, decoder.PixelFormatRGB)
	fillWhite(stripImg)

	// Compute source-level region covering this strip.
	scaleY := float64(it.l0Rect.Dy()) / (it.sourceLevel.Downsample * float64(it.outSize.Y))
	levelY0 := int(float64(outY0)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.sourceLevel.Downsample)
	levelY1 := int(float64(outY1)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.sourceLevel.Downsample) + 1
	scaleX := 1.0 / it.sourceLevel.Downsample
	levelX0 := int(float64(it.l0Rect.Min.X) * scaleX)
	levelX1 := int(float64(it.l0Rect.Max.X)*scaleX) + 1

	// Clip to level bounds.
	cy0 := levelY0
	cy1 := levelY1
	cx0 := levelX0
	cx1 := levelX1
	if cy0 < 0 {
		cy0 = 0
	}
	if cy1 > it.sourceLevel.Size.H {
		cy1 = it.sourceLevel.Size.H
	}
	if cx0 < 0 {
		cx0 = 0
	}
	if cx1 > it.sourceLevel.Size.W {
		cx1 = it.sourceLevel.Size.W
	}
	if cy0 >= cy1 || cx0 >= cx1 {
		// Entirely out of bounds; strip stays all white.
		return stripImg, nil
	}

	// Allocate an intermediate image at level resolution for this strip's
	// source-level region. We'll blit tiles into it then resample to output.
	intermediateW := cx1 - cx0
	intermediateH := cy1 - cy0
	intermediate := decoder.NewImageFormat(intermediateW, intermediateH, decoder.PixelFormatRGB)
	fillWhite(intermediate)

	tileW := it.sourceLevel.TileSize.W
	tileH := it.sourceLevel.TileSize.H
	txMin := cx0 / tileW
	tyMin := cy0 / tileH
	txMax := (cx1 - 1) / tileW
	tyMax := (cy1 - 1) / tileH

	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			k := tileKey{tx: tx, ty: ty}
			// Reserve in case lookahead hasn't gotten to it yet.
			// If the lookahead has already closed tileReqs (all strips
			// dispatched, cache cap smaller than the full slide) fall back
			// to a synchronous in-line decode rather than sending on a
			// closed channel.
			if it.cache.reserve(k) {
				if it.tileReqsOpen.Load() {
					select {
					case it.tileReqs <- k:
					case <-it.cancelCtx.Done():
						return nil, it.cancelCtx.Err()
					}
				} else {
					it.decodeAndStore(k)
				}
			}
			tileImg, err, _ := it.cache.waitGet(k, it.cancelCtx)
			if err != nil {
				return nil, fmt.Errorf("opentile: ScaledStrips: decode tile (%d,%d) at level %d: %w", tx, ty, it.sourceLevel.Index, err)
			}
			if tileImg == nil {
				if err := it.cancelCtx.Err(); err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("opentile: ScaledStrips: tile (%d,%d) missing from cache", tx, ty)
			}
			// Blit the intersection of tileImg with the clipped strip region
			// into the intermediate image.
			tileLevelX := tx * tileW
			tileLevelY := ty * tileH
			ax0 := maxInt(tileLevelX, cx0)
			ay0 := maxInt(tileLevelY, cy0)
			ax1 := minInt(tileLevelX+tileImg.Width, cx1)
			ay1 := minInt(tileLevelY+tileImg.Height, cy1)
			if ax0 >= ax1 || ay0 >= ay1 {
				continue
			}
			blitInto(tileImg, ax0-tileLevelX, ay0-tileLevelY, ax1-ax0, ay1-ay0,
				intermediate, ax0-cx0, ay0-cy0)
		}
	}

	// Resample intermediate → stripImg's dimensions.
	scratch := decoder.NewImageFormat(it.outSize.X, stripH, decoder.PixelFormatRGB)
	fillWhite(scratch)
	if intermediate.Width > 0 && intermediate.Height > 0 {
		if err := resampleImageIntoUsing(intermediate, scratch, it.cfg.kernel); err != nil {
			return nil, fmt.Errorf("opentile: ScaledStrips: resample: %w", err)
		}
	}
	// Copy scratch into stripImg verbatim.
	copy(stripImg.Pix, scratch.Pix)

	return stripImg, nil
}

// resampleImageIntoUsing is a thin wrapper over resample.ImageInto that
// preserves the kernel choice. Allows future no-op fast-path when src
// dims match dst dims.
func resampleImageIntoUsing(src, dst *decoder.Image, kernel resample.Kernel) error {
	if src.Width == dst.Width && src.Height == dst.Height {
		copy(dst.Pix, src.Pix)
		return nil
	}
	return resample.ImageInto(src, dst, kernel)
}

// Close releases the iterator's workers + cache. Safe to call
// multiple times.
func (it *StripIterator) Close() error {
	it.mu.Lock()
	if it.closed {
		it.mu.Unlock()
		return nil
	}
	it.closed = true
	if it.cancelFn != nil {
		it.cancelFn()
	}
	// Signal lookahead to wake so it can observe closed.
	select {
	case it.advance <- struct{}{}:
	default:
	}
	it.mu.Unlock()

	it.lookaheadDone.Wait()
	it.workersDone.Wait()

	if it.cache != nil {
		it.cache.close()
	}
	return nil
}

// decodeWorker pulls tileKey requests from tileReqs, decodes via
// ImageDecodedTile(WithScale), and stores into the cache.
//
// Exits when tileReqs closes or cancelCtx fires.
func (it *StripIterator) decodeWorker() {
	defer it.workersDone.Done()
	for {
		select {
		case k, ok := <-it.tileReqs:
			if !ok {
				return
			}
			it.decodeAndStore(k)
		case <-it.cancelCtx.Done():
			return
		}
	}
}

// decodeAndStore decodes one tile and stores it in the cache (with
// any error). The caller (lookahead) has already reserve()'d the
// key, so this is unconditional put.
func (it *StripIterator) decodeAndStore(k tileKey) {
	opts := []DecodeOption{WithFormat(decoder.PixelFormatRGB)}
	if it.idctScale > 1 {
		opts = append(opts, WithScale(it.idctScale))
	}
	img, err := it.slide.ImageDecodedTile(it.imageIdx, it.sourceLevel.Index, k.tx, k.ty, opts...)
	it.cache.put(k, img, err)
}

// lookahead walks output strips in order, enqueueing tile decode
// requests for strips within [currentStrip, currentStrip + lookahead].
// Exits when cancelCtx fires or all strips' tiles have been enqueued.
func (it *StripIterator) lookahead() {
	defer it.lookaheadDone.Done()
	defer func() {
		it.tileReqsOpen.Store(false)
		close(it.tileReqs)
	}()

	dispatchedStrip := 0 // last strip whose tiles we've enqueued

	for {
		// Compute the cap of strips we should have enqueued for, given
		// the current consumer position.
		it.mu.Lock()
		consumerStrip := it.nextStrip
		closed := it.closed
		it.mu.Unlock()
		if closed {
			return
		}
		targetCap := consumerStrip + it.cfg.lookahead

		// Enqueue tiles for strips up through targetCap (inclusive).
		for dispatchedStrip <= targetCap && dispatchedStrip < it.stripsTotal {
			tiles := it.tilesForStrip(dispatchedStrip)
			for _, k := range tiles {
				if !it.cache.reserve(k) {
					continue // already requested or cached
				}
				select {
				case it.tileReqs <- k:
				case <-it.cancelCtx.Done():
					return
				}
			}
			dispatchedStrip++
		}

		if dispatchedStrip >= it.stripsTotal {
			return // all done
		}

		// Wait for consumer to advance.
		select {
		case <-it.advance:
		case <-it.cancelCtx.Done():
			return
		}
	}
}

// tilesForStrip computes the set of source-level tile keys that
// overlap the given output strip's source-level coverage.
func (it *StripIterator) tilesForStrip(stripIdx int) []tileKey {
	// Output strip rows: [stripIdx*stripHeight, min((stripIdx+1)*stripHeight, outSize.Y))
	outY0 := stripIdx * it.stripHeight
	outY1 := outY0 + it.stripHeight
	if outY1 > it.outSize.Y {
		outY1 = it.outSize.Y
	}

	// Simpler: source-level row range = floor(stripOutY0 * outSize.Y → l0 → level), ceil(stripOutY1 → l0 → level).
	scaleY := float64(it.l0Rect.Dy()) / (it.sourceLevel.Downsample * float64(it.outSize.Y))
	levelY0 := int(float64(outY0)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.sourceLevel.Downsample)
	levelY1 := int(float64(outY1)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.sourceLevel.Downsample) + 1

	// Source-level column range covers the full L0 x-range.
	scaleX := 1.0 / it.sourceLevel.Downsample
	levelX0 := int(float64(it.l0Rect.Min.X) * scaleX)
	levelX1 := int(float64(it.l0Rect.Max.X)*scaleX) + 1

	// Clip to level bounds.
	if levelY0 < 0 {
		levelY0 = 0
	}
	if levelY1 > it.sourceLevel.Size.H {
		levelY1 = it.sourceLevel.Size.H
	}
	if levelX0 < 0 {
		levelX0 = 0
	}
	if levelX1 > it.sourceLevel.Size.W {
		levelX1 = it.sourceLevel.Size.W
	}

	if levelY0 >= levelY1 || levelX0 >= levelX1 {
		return nil
	}

	tileW := it.sourceLevel.TileSize.W
	tileH := it.sourceLevel.TileSize.H
	if tileW <= 0 || tileH <= 0 {
		return nil
	}

	txMin := levelX0 / tileW
	tyMin := levelY0 / tileH
	txMax := (levelX1 - 1) / tileW
	tyMax := (levelY1 - 1) / tileH

	keys := make([]tileKey, 0, (txMax-txMin+1)*(tyMax-tyMin+1))
	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			keys = append(keys, tileKey{tx: tx, ty: ty})
		}
	}
	return keys
}

// stripCacheCapacity converts a byte budget into a tile-count cap for
// the per-iterator decoded-tile cache (C1). The result is floored at
// max(workers, 8) so each worker always has an in-flight slot and tiny
// budgets don't livelock, and capped at the original count-formula
// value so a generous budget never over-provisions a narrow level.
func stripCacheCapacity(budgetBytes, bytesPerTile int64, workers, countFormulaCap int) int {
	if bytesPerTile < 1 {
		bytesPerTile = 1
	}
	byteCap := int(budgetBytes / bytesPerTile)
	floor := workers
	if floor < 8 {
		floor = 8
	}
	capacity := byteCap
	if capacity < floor {
		capacity = floor
	}
	if capacity > countFormulaCap {
		capacity = countFormulaCap
	}
	return capacity
}

// autoIDCTScale picks the IDCT scale factor for a JPEG source level
// based on the effective downsample from level dims to output dims.
//
// Returns 1, 2, 4, or 8. Non-JPEG levels still get a return value
// (the caller's WithScale option call may be a no-op for non-JPEG,
// but the iterator passes it through).
func autoIDCTScale(level Level, l0Rect image.Rectangle, outSize image.Point) int {
	if level.Compression != CompressionJPEG {
		return 1
	}
	// Effective downsample from level to output.
	dx := float64(l0Rect.Dx()) / (level.Downsample * float64(outSize.X))
	dy := float64(l0Rect.Dy()) / (level.Downsample * float64(outSize.Y))
	d := dx
	if dy > d {
		d = dy
	}
	switch {
	case d >= 8:
		return 8
	case d >= 4:
		return 4
	case d >= 2:
		return 2
	default:
		return 1
	}
}

// bestLevelForRegion is a thin wrapper around ImageBestLevelForDownsample
// that computes the downsample from l0Rect + outSize.
func (s *Slide) bestLevelForRegion(imageIdx int, l0Rect image.Rectangle, outSize image.Point) Level {
	dx := float64(l0Rect.Dx()) / float64(outSize.X)
	dy := float64(l0Rect.Dy()) / float64(outSize.Y)
	d := dx
	if dy > d {
		d = dy
	}
	if d < 1.0 {
		d = 1.0
	}
	levelIdx := s.ImageBestLevelForDownsample(imageIdx, d)
	level, err := s.r.Level(imageIdx, levelIdx)
	if err != nil {
		// Should never happen if ImageBestLevelForDownsample is correct.
		return Level{}
	}
	return level
}
