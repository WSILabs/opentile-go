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

	// Cache size heuristic: workers * (lookahead + 1) * tilesPerStripWidth
	// (workers concurrent in-flight tiles per strip × strips in window).
	tilesPerStripWidth := 1
	if level.TileSize.W > 0 {
		tilesPerStripWidth = (level.Size.W + level.TileSize.W - 1) / level.TileSize.W
	}
	cacheCapacity := cfg.workers * (cfg.lookahead + 1) * tilesPerStripWidth
	if cacheCapacity < 8 {
		cacheCapacity = 8
	}
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

// Next returns the next strip. STUB in this task — returns an error
// for "not yet implemented" when strips remain; io.EOF when exhausted;
// io.ErrClosedPipe after Close. Phase 4 implements the real strip assembly.
func (it *StripIterator) Next() (*decoder.Image, error) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.closed {
		return nil, io.ErrClosedPipe
	}
	if it.nextStrip >= it.stripsTotal {
		return nil, io.EOF
	}
	it.nextStrip++
	return nil, fmt.Errorf("strip iterator not yet implemented (Phase 4)")
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
	defer close(it.tileReqs)

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
