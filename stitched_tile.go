package opentile

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

// compositeStitchedLoop blits every regionLayout tile intersecting the clipped
// rectangle [x0,y0,x1,y1) into dst, which represents the stitched-space
// rectangle whose top-left is (regionX, regionY) and whose extent is dst's
// dimensions. fetch returns the decoded raw tile for (col,row); callers supply
// either a fresh-decode-into-scratch fetch (ReadRegion) or a cache-backed fetch
// (StitchedTile). dst must already be white-initialized by the caller.
//
// Shared by imageReadRegionImpl and imageStitchedTile so the two compositing
// paths cannot drift.
func compositeStitchedLoop(rl regionLayout, level, regionX, regionY, x0, y0, x1, y1, tileW, tileH int, dst *decoder.Image, fetch func(col, row int) (*decoder.Image, error)) error {
	for _, tp := range rl.TilesIntersecting(level, x0, y0, x1-x0, y1-y0) {
		tileX, tileY, ok := rl.TileOrigin(level, tp.Col, tp.Row)
		if !ok {
			continue
		}
		src, err := fetch(tp.Col, tp.Row)
		if err != nil {
			return err
		}
		ix0 := maxInt(tileX, x0)
		iy0 := maxInt(tileY, y0)
		ix1 := minInt(tileX+tileW, x1)
		iy1 := minInt(tileY+tileH, y1)
		if ix0 >= ix1 || iy0 >= iy1 {
			continue
		}
		blitInto(src, ix0-tileX, iy0-tileY, ix1-ix0, iy1-iy0, dst, ix0-regionX, iy0-regionY)
	}
	return nil
}

// minFrameCacheBytes floors the display-tile cache so a small or unset memory
// budget still retains a few source frames for the decode-once-blit-many win.
const minFrameCacheBytes = 64 << 20 // 64 MiB

// frameCacheFor lazily creates the per-Slide decoded-frame cache, sized from
// readBudget (floored at minFrameCacheBytes). Guarded by handlesMu.
func (s *Slide) frameCacheFor() *decodedFrameCache {
	s.handlesMu.Lock()
	defer s.handlesMu.Unlock()
	if s.frameCache == nil {
		budget := s.readBudget
		if budget < minFrameCacheBytes {
			budget = minFrameCacheBytes
		}
		s.frameCache = newDecodedFrameCache(budget)
	}
	return s.frameCache
}

// imageStitchedTile returns a clean, non-overlapping display tile from the
// canonical grid ceil(Size/TileSize). For readers that expose the regionLayout
// capability (overlapping formats such as stitched BIF) it composites the
// stitched-space rectangle [tx*TileW, ty*TileH, TileW, TileH] from the
// per-Slide decoded-frame cache. For every other reader it is exactly
// imageDecodedTile, so callers can use StitchedTile uniformly across formats.
//
// Scale > 1 is unsupported on overlapping levels (use ScaledStrips /
// ReadRegionScaled for scaled traversal); it returns decoder.ErrUnsupportedScale.
func (s *Slide) imageStitchedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	if _, ok := regionLayoutOf(s.r); !ok {
		return s.imageDecodedTile(image, level, tx, ty, opts...)
	}
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}
	dst := decoder.NewImageFormat(lvl.TileSize.W, lvl.TileSize.H, newDecodeConfig(opts).format)
	if err := s.imageStitchedTileInto(image, level, tx, ty, dst, opts...); err != nil {
		return nil, err
	}
	return dst, nil
}

// imageStitchedTileInto is the allocation-free form of imageStitchedTile: it
// composites the display tile (tx,ty) into the caller-provided dst, which must
// be exactly the level's TileSize. dst is white-filled before compositing
// (overlaps/gaps and out-of-hull remainders stay white). The cache, scale, and
// no-layout-delegation semantics are identical to imageStitchedTile; the
// composite is done in dst's own pixel format.
func (s *Slide) imageStitchedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	if dst == nil {
		return fmt.Errorf("opentile: StitchedTileInto: dst is nil")
	}
	rl, ok := regionLayoutOf(s.r)
	if !ok {
		return s.imageDecodedTileInto(image, level, tx, ty, dst, opts...)
	}
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}
	if newDecodeConfig(opts).scale > 1 {
		return decoder.ErrUnsupportedScale
	}
	tileW, tileH := lvl.TileSize.W, lvl.TileSize.H
	if dst.Width != tileW || dst.Height != tileH {
		return fmt.Errorf("opentile: StitchedTileInto: dst is %dx%d, want TileSize %dx%d",
			dst.Width, dst.Height, tileW, tileH)
	}
	x0, y0 := tx*tileW, ty*tileH
	x1, y1 := x0+tileW, y0+tileH
	if sw, sh, ok := rl.StitchedSize(level); ok {
		if x1 > sw {
			x1 = sw
		}
		if y1 > sh {
			y1 = sh
		}
	}
	fillWhite(dst)
	if x0 >= x1 || y0 >= y1 {
		return nil // fully outside the stitched hull → white tile
	}
	fc := s.frameCacheFor()
	// Composite in dst's format: blitInto requires src and dst share a format,
	// and the cache frames must be decoded accordingly. Use a copy of opts with
	// the format pinned to dst.Format (full-slice cap so the caller's backing
	// array is never mutated).
	loadOpts := append(opts[:len(opts):len(opts)], WithFormat(dst.Format))
	return compositeStitchedLoop(rl, level, x0, y0, x0, y0, x1, y1, tileW, tileH, dst,
		func(col, row int) (*decoder.Image, error) {
			key := frameCacheKey{image: image, level: level, col: col, row: row, format: dst.Format}
			return fc.getOrLoad(key, func() (*decoder.Image, error) {
				return s.imageDecodedTile(image, level, col, row, loadOpts...)
			})
		})
}
