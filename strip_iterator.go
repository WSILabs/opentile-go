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
	idctScale   int   // 1, 2, 4, or 8 codec-domain downscale; 1 if none
	stripsTotal int   // ceil(outSize.Y / stripHeight)

	// Effective (codec-scaled) source geometry. When idctScale = s, decoded
	// tiles are ceil(TileSize/s) and the assembled strip intermediate lives
	// at the level resolution / s. All strip geometry (region clip, tile
	// selection, blit) runs on this virtual s-times-coarser level so the
	// unscaled blit math stays correct for scaled tiles.
	effDownsample float64
	effLevelW     int
	effLevelH     int
	effTileW      int
	effTileH      int

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

	// layout is non-nil for readers whose tile grid is not a regular
	// spatial partition of the level (BIF stitching, #60). When present,
	// tile selection and blit placement use TilesIntersecting + TileOrigin
	// instead of the naive tx*tileW formula.
	layout regionLayout
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

	// Effective (s-times-coarser) source geometry for the scaled tiles.
	es := it.idctScale
	it.effDownsample = level.Downsample * float64(es)
	it.effLevelW = (level.Size.W + es - 1) / es
	it.effLevelH = (level.Size.H + es - 1) / es
	it.effTileW = (level.TileSize.W + es - 1) / es
	it.effTileH = (level.TileSize.H + es - 1) / es

	// Layout-aware compositing (#60): if the reader implements regionLayout
	// (e.g. BIF, whose tile grid is not a regular spatial partition),
	// cache it for use in tilesForStrip and Next. The non-BIF formats don't
	// implement regionLayout, so they take the unchanged naive grid path.
	if rl, ok := regionLayoutOf(s.r); ok {
		it.layout = rl
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

	// Compute source-level region covering this strip, in EFFECTIVE
	// (codec-scaled) coordinates so scaled tiles blit correctly.
	scaleY := float64(it.l0Rect.Dy()) / (it.effDownsample * float64(it.outSize.Y))
	levelY0 := int(float64(outY0)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.effDownsample)
	levelY1 := int(float64(outY1)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.effDownsample) + 1
	scaleX := 1.0 / it.effDownsample
	levelX0 := int(float64(it.l0Rect.Min.X) * scaleX)
	levelX1 := int(float64(it.l0Rect.Max.X)*scaleX) + 1

	// Clip to effective level bounds.
	cy0 := levelY0
	cy1 := levelY1
	cx0 := levelX0
	cx1 := levelX1
	if cy0 < 0 {
		cy0 = 0
	}
	if cy1 > it.effLevelH {
		cy1 = it.effLevelH
	}
	if cx0 < 0 {
		cx0 = 0
	}
	if cx1 > it.effLevelW {
		cx1 = it.effLevelW
	}
	if cy0 >= cy1 || cx0 >= cx1 {
		// Entirely out of bounds; strip stays all white.
		return stripImg, nil
	}

	// Allocate an intermediate image at the EFFECTIVE (codec-scaled) level
	// resolution for this strip's region. We'll blit tiles into it then
	// resample to output.
	intermediateW := cx1 - cx0
	intermediateH := cy1 - cy0
	intermediate := decoder.NewImageFormat(intermediateW, intermediateH, decoder.PixelFormatRGB)
	fillWhite(intermediate)

	tileW := it.effTileW
	tileH := it.effTileH

	// blitOneTile is the shared blit helper used by both the naive and
	// layout-aware tile loop below.
	blitOneTile := func(k tileKey, tileLevelX, tileLevelY int) error {
		// Reserve in case lookahead hasn't gotten to it yet, then hand
		// the request to a worker. tileReqs is never closed (workers
		// exit via cancelCtx), so this send never races a close.
		if it.cache.reserve(k) {
			select {
			case it.tileReqs <- k:
			case <-it.cancelCtx.Done():
				return it.cancelCtx.Err()
			}
		}
		tileImg, err, _ := it.cache.waitGet(k, it.cancelCtx)
		if err != nil {
			return fmt.Errorf("opentile: ScaledStrips: decode tile (%d,%d) at level %d: %w", k.tx, k.ty, it.sourceLevel.Index, err)
		}
		if tileImg == nil {
			if err := it.cancelCtx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("opentile: ScaledStrips: tile (%d,%d) missing from cache", k.tx, k.ty)
		}
		// Blit the intersection of tileImg with the clipped strip region
		// into the intermediate image.
		ax0 := maxInt(tileLevelX, cx0)
		ay0 := maxInt(tileLevelY, cy0)
		ax1 := minInt(tileLevelX+tileImg.Width, cx1)
		ay1 := minInt(tileLevelY+tileImg.Height, cy1)
		if ax0 >= ax1 || ay0 >= ay1 {
			return nil
		}
		blitInto(tileImg, ax0-tileLevelX, ay0-tileLevelY, ax1-ax0, ay1-ay0,
			intermediate, ax0-cx0, ay0-cy0)
		return nil
	}

	// Layout-aware tile iteration (#60): for readers with a non-regular tile
	// grid (BIF), use TilesIntersecting + TileOrigin rather than the naive
	// txMin..txMax grid. The effective-domain strip region must be converted
	// back to level coordinates (× idctScale) for the regionLayout query, and
	// each tile's level-resolution origin is divided by idctScale to land in
	// the effective domain for blitting.
	if rl := it.layout; rl != nil {
		es := it.idctScale
		if es < 1 {
			es = 1
		}
		lvlX0, lvlY0 := cx0*es, cy0*es
		lvlW, lvlH := (cx1-cx0)*es, (cy1-cy0)*es
		for _, tp := range rl.TilesIntersecting(it.sourceLevel.Index, lvlX0, lvlY0, lvlW, lvlH) {
			lx, ly, ok := rl.TileOrigin(it.sourceLevel.Index, tp.Col, tp.Row)
			if !ok {
				continue
			}
			// Scale down the level-resolution origin to the effective domain.
			effX := lx / es
			effY := ly / es
			if err := blitOneTile(tileKey{tx: tp.Col, ty: tp.Row}, effX, effY); err != nil {
				return nil, err
			}
		}
	} else {
		txMin := cx0 / tileW
		tyMin := cy0 / tileH
		txMax := (cx1 - 1) / tileW
		tyMax := (cy1 - 1) / tileH
		for ty := tyMin; ty <= tyMax; ty++ {
			for tx := txMin; tx <= txMax; tx++ {
				if err := blitOneTile(tileKey{tx: tx, ty: ty}, tx*tileW, ty*tileH); err != nil {
					return nil, err
				}
			}
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
