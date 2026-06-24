package opentile

import "github.com/wsilabs/opentile-go/decoder"

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
