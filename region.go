package opentile

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

// imageReadRegion is the logic-bearing region read, backing
// (*Level).ReadRegion. All coords are at the level's own resolution.
//
// The rectangle may extend beyond the level's bounds; out-of-bounds
// pixels are white-filled (0xFF, 0xFF, 0xFF). Returns ErrRegionEmpty
// if the entire rectangle is outside the level.
//
// Requires a registered decoder for the level's Compression — same
// constraint as DecodedTile.
//
// Internally spans the relevant tiles and decodes each once.
// Adjacent ReadRegion calls do NOT share a decoded-tile cache; for
// high-throughput patterns use the ScaledStrips iterator (v0.26).
func (s *Slide) imageReadRegion(image, level int, r Region, opts ...DecodeOption) (*decoder.Image, error) {
	if r.Size.W <= 0 || r.Size.H <= 0 {
		return nil, ErrRegionEmpty
	}
	cfg := newDecodeConfig(opts)
	dst := decoder.NewImageFormat(r.Size.W, r.Size.H, cfg.format)
	if err := s.imageReadRegionImpl(image, level, r.Origin.X, r.Origin.Y, dst, opts); err != nil {
		return nil, err
	}
	return dst, nil
}

// imageReadRegionInto is the logic-bearing region-into-dst read,
// backing (*Level).ReadRegionInto. origin is at the level's resolution;
// the region size is taken from dst.Width × dst.Height.
func (s *Slide) imageReadRegionInto(image, level int, origin Point, dst *decoder.Image, opts ...DecodeOption) error {
	if dst == nil {
		return fmt.Errorf("opentile: ReadRegionInto: dst is nil")
	}
	if dst.Width <= 0 || dst.Height <= 0 {
		return ErrRegionEmpty
	}
	return s.imageReadRegionImpl(image, level, origin.X, origin.Y, dst, opts)
}

// imageReadRegionImpl is the shared core. dst is pre-allocated; this
// function fills it (white-fills, then blits decoded tile bytes over
// the in-bounds intersection).
func (s *Slide) imageReadRegionImpl(image, level, x, y int, dst *decoder.Image, opts []DecodeOption) error {
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}

	w, h := dst.Width, dst.Height

	// Clip the requested rectangle to the level's bounds.
	x0 := x
	y0 := y
	x1 := x + w
	y1 := y + h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > lvl.Size.W {
		x1 = lvl.Size.W
	}
	if y1 > lvl.Size.H {
		y1 = lvl.Size.H
	}
	if x0 >= x1 || y0 >= y1 {
		return ErrRegionEmpty
	}

	// v0.29 Layer 1: skip fillWhite when the requested region is fully
	// in-bounds AND no edge tile contributes. Edge tiles return less
	// than nominal TileSize, and the blit only writes the actual
	// decoded extent — pre-existing dst contents would leak in
	// without a fillWhite prelude.
	fullyInBounds := x0 == x && y0 == y && x1 == x+w && y1 == y+h
	edgeTileX := x1 == lvl.Size.W && lvl.Size.W%lvl.TileSize.W != 0
	edgeTileY := y1 == lvl.Size.H && lvl.Size.H%lvl.TileSize.H != 0
	if !fullyInBounds || edgeTileX || edgeTileY {
		fillWhite(dst)
	}

	// Tile grid covering the clipped rectangle.
	txMin := x0 / lvl.TileSize.W
	tyMin := y0 / lvl.TileSize.H
	txMax := (x1 - 1) / lvl.TileSize.W
	tyMax := (y1 - 1) / lvl.TileSize.H

	// v0.29 Layer 2: borrow a scratch *decoder.Image once per call,
	// reuse across every tile in the loop. Returned on defer.
	// Format follows dst's so the decoder writes into the right
	// pixel layout.
	scratch := borrowTileScratch(lvl.TileSize.W, lvl.TileSize.H, dst.Format)
	defer returnTileScratch(scratch)

	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			if err := s.imageDecodedTileInto(image, level, tx, ty, scratch, opts...); err != nil {
				return fmt.Errorf("opentile: decode tile (%d,%d) at level %d: %w", tx, ty, level, err)
			}
			tileX := tx * lvl.TileSize.W
			tileY := ty * lvl.TileSize.H
			tileW := lvl.TileSize.W
			tileH := lvl.TileSize.H
			// Edge tiles may decode to less than nominal TileSize. The
			// decoder writes only the actual extent into scratch; the
			// scratch's Width/Height are the nominal pool size, which
			// stays constant across reuse. Derive the actual decoded
			// extent from the level geometry instead.
			actualW := lvl.Size.W - tileX
			if actualW > lvl.TileSize.W {
				actualW = lvl.TileSize.W
			}
			actualH := lvl.Size.H - tileY
			if actualH > lvl.TileSize.H {
				actualH = lvl.TileSize.H
			}
			if actualW < tileW {
				tileW = actualW
			}
			if actualH < tileH {
				tileH = actualH
			}
			// Intersect tile bounds with the clipped output region.
			ix0 := maxInt(tileX, x0)
			iy0 := maxInt(tileY, y0)
			ix1 := minInt(tileX+tileW, x1)
			iy1 := minInt(tileY+tileH, y1)
			if ix0 >= ix1 || iy0 >= iy1 {
				continue
			}
			srcX := ix0 - tileX
			srcY := iy0 - tileY
			srcW := ix1 - ix0
			srcH := iy1 - iy0
			dstX := ix0 - x
			dstY := iy0 - y
			blitInto(scratch, srcX, srcY, srcW, srcH, dst, dstX, dstY)
		}
	}
	return nil
}

func fillWhite(img *decoder.Image) {
	bpp := 3
	if img.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	for y := 0; y < img.Height; y++ {
		off := y * img.Stride
		for x := 0; x < img.Width; x++ {
			img.Pix[off+0] = 0xFF
			img.Pix[off+1] = 0xFF
			img.Pix[off+2] = 0xFF
			if bpp == 4 {
				img.Pix[off+3] = 0xFF
			}
			off += bpp
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
