package opentile

import (
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// DecodeOption configures a *Slide.DecodedTile / DecodedTileInto /
// ReadRegion / ReadRegionScaled call. See WithFormat, WithScale,
// WithResampleKernel.
type DecodeOption func(*decodeConfig)

// decodeConfig is the resolved option set. Defaults: PixelFormatRGB,
// scale=1, kernel=Lanczos.
type decodeConfig struct {
	format decoder.PixelFormat
	scale  int
	kernel resample.Kernel
}

func newDecodeConfig(opts []DecodeOption) decodeConfig {
	c := decodeConfig{
		format: decoder.PixelFormatRGB,
		scale:  1,
		kernel: resample.Lanczos,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// WithFormat sets the requested output pixel format. Defaults to
// PixelFormatRGB (3 bytes per pixel; alpha-free). PixelFormatRGBA
// is also universally supported.
func WithFormat(f decoder.PixelFormat) DecodeOption {
	return func(c *decodeConfig) { c.format = f }
}

// WithScale sets the in-codec downscale factor, decoding to
// ceil(dim/scale). Valid values: 1, 2, 4, 8. Honored by jpeg (IDCT
// fast-scale), jpeg2000 (cp_reduce) and htj2k (restrict_input_resolution);
// other codecs return ErrUnsupportedScale for scale != 1.
func WithScale(s int) DecodeOption {
	return func(c *decodeConfig) { c.scale = s }
}

// WithResampleKernel chooses the resample kernel for ReadRegionScaled.
// Has no effect on ReadRegion (no resampling step).
//
//	resample.Lanczos  — best perceptual quality (default)
//	resample.Box      — fast integer-ratio downsampling, sharp
//	resample.Bilinear — fast, decent quality
//	resample.Nearest  — fastest, ugly under downsampling
func WithResampleKernel(k resample.Kernel) DecodeOption {
	return func(c *decodeConfig) { c.kernel = k }
}
