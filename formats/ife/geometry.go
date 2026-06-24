package ife

import (
	"math"

	opentile "github.com/wsilabs/opentile-go"
)

// ifeGeometry computes per-API-level content Size and Downsample from the layer
// scales and the L0 content extent. api is native-first (api[0] = finest =
// max scale). The spec's downsample is max_scale/scale; the content extent at
// each level is L0_content / downsample, rounded. L0 content comes from
// TileTable.x_extent/y_extent when those are valid pixel dimensions, else the
// padded XTiles*256 grid (the cervix fixture stores tile counts there).
func ifeGeometry(api []LayerExtent, tt TileTable) (sizes []opentile.Size, downs []float64) {
	n := len(api)
	sizes = make([]opentile.Size, n)
	downs = make([]float64, n)
	if n == 0 {
		return sizes, downs
	}
	maxScale := float64(api[0].Scale)

	size0 := opentile.Size{
		W: int(api[0].XTiles) * TileSidePixels,
		H: int(api[0].YTiles) * TileSidePixels,
	}
	if validPixelExtent(int(tt.XExtent), int(api[0].XTiles)) &&
		validPixelExtent(int(tt.YExtent), int(api[0].YTiles)) {
		size0 = opentile.Size{W: int(tt.XExtent), H: int(tt.YExtent)}
	}

	for i := range api {
		ds := maxScale / float64(api[i].Scale)
		downs[i] = ds
		sizes[i] = opentile.Size{
			W: int(math.Round(float64(size0.W) / ds)),
			H: int(math.Round(float64(size0.H) / ds)),
		}
	}
	return sizes, downs
}

// validPixelExtent reports whether ext is a plausible pixel dimension for a
// tile grid of `tiles` columns/rows: it must lie in ((tiles-1)*256, tiles*256].
// Tile COUNTS (e.g. cervix's x_extent) fail this test, so the caller falls back
// to the padded grid base.
func validPixelExtent(ext, tiles int) bool {
	if tiles <= 0 {
		return false
	}
	return ext > (tiles-1)*TileSidePixels && ext <= tiles*TileSidePixels
}
