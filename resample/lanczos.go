package resample

import (
	"math"

	"github.com/wsilabs/opentile-go/decoder"
)

const lanczosA = 3.0 // lobe count

// lanczos2D returns the 2-D Lanczos kernel weight for position (dx, dy)
// from the sample centre.
func lanczosWeight(x float64) float64 {
	if x == 0 {
		return 1
	}
	if x < -lanczosA || x > lanczosA {
		return 0
	}
	px := math.Pi * x
	return (lanczosA * math.Sin(px) * math.Sin(px/lanczosA)) / (px * px)
}

// lanczosInto resamples src into dst using Lanczos resampling with a=3.
// Best quality for arbitrary downsampling ratios. It delegates to the
// separable two-pass implementation (lanczosSeparableInto): O(out·scale)
// with cached, pre-normalized weights and no transcendental in the hot
// loops. Output matches the original non-separable convolution to within
// 1 LSB per channel (TestLanczosSeparableEquivalence). Handles both
// PixelFormatRGB (3 bpp) and PixelFormatRGBA (4 bpp) via src/dst.Stride.
func lanczosInto(src, dst *decoder.Image) error {
	return lanczosSeparableInto(src, dst)
}
