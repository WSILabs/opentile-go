package ndpi

import (
	"math"

	opentile "github.com/wsilabs/opentile-go"
)

// AdjustTileSize returns the output tile size to use for an NDPI tiler given
// the user's requested size and the smallest native strip width in the file.
//
// Upstream opentile's algorithm: the adjusted size is a power-of-2 multiple
// of the smallest strip width, where the exponent is
// round(log2(ratio(requested, strip))). If there are no stripped pages
// (stripWidth == 0), the request passes through unchanged. The result is
// always square.
//
// Concretely this guarantees every output tile is an integer number of
// native strips wide, so the strip-concat code never needs to crop
// horizontally within a strip — it just concatenates whole strips.
func AdjustTileSize(requested, stripWidth int) opentile.Size {
	if stripWidth == 0 || requested == stripWidth {
		return opentile.Size{W: requested, H: requested}
	}
	var factor float64
	if requested > stripWidth {
		factor = float64(requested) / float64(stripWidth)
	} else {
		factor = float64(stripWidth) / float64(requested)
	}
	factor2 := math.Pow(2, math.Round(math.Log2(factor)))
	adjusted := int(factor2) * stripWidth
	return opentile.Size{W: adjusted, H: adjusted}
}
