package opentile

import (
	"context"
	"fmt"
	"image"
	"io"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
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
	idctScale   int   // 1, 2, 4, or 8 (JPEG); 1 otherwise
	stripsTotal int   // ceil(outSize.Y / stripHeight)

	// Runtime state.
	mu        sync.Mutex
	nextStrip int // 0-based index of the next strip to yield
	closed    bool

	// Internal pipeline.
	cache     *tileCache
	cancelCtx context.Context // derived from cfg.ctx + iterator's own cancel
	cancelFn  context.CancelFunc

	// Tile dispatch channel — the lookahead goroutine and Next() push
	// (tx, ty) requests; workers pop them, decode, and put into cache.
	// Never closed: workers shut down via cancelCtx (see decodeWorker),
	// so a sender can never panic on a closed channel.
	tileReqs chan tileKey

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
			// Reserve in case lookahead hasn't gotten to it yet, then hand
			// the request to a worker. tileReqs is never closed (workers
			// exit via cancelCtx), so this send never races a close.
			if it.cache.reserve(k) {
				select {
				case it.tileReqs <- k:
				case <-it.cancelCtx.Done():
					return nil, it.cancelCtx.Err()
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
