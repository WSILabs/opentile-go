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

	// Phase 4 will start workers + lookahead goroutines here.
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
	it.mu.Unlock()

	if it.cache != nil {
		it.cache.close()
	}
	return nil
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
