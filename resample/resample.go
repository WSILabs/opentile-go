package resample

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

type Kernel int

const (
	Nearest Kernel = iota
	Bilinear
	Lanczos
	Box
)

// Image returns a freshly-allocated Image at the requested output
// dimensions, resampled from src using kernel k. The output format
// matches src.Format.
func Image(src *decoder.Image, outW, outH int, k Kernel) *decoder.Image {
	dst := decoder.NewImageFormat(outW, outH, src.Format)
	_ = ImageInto(src, dst, k) // can't fail for matched formats
	return dst
}

// ImageInto writes the resampled output into dst (dimensions
// determined by dst). dst.Format must match src.Format.
func ImageInto(src, dst *decoder.Image, k Kernel) error {
	if src.Format != dst.Format {
		return fmt.Errorf("resample: format mismatch: src=%d dst=%d", src.Format, dst.Format)
	}
	switch k {
	case Nearest:
		return nearestInto(src, dst)
	case Bilinear:
		return bilinearInto(src, dst)
	case Lanczos:
		return lanczosInto(src, dst)
	case Box:
		return boxInto(src, dst)
	default:
		return fmt.Errorf("resample: unknown kernel %d", k)
	}
}
