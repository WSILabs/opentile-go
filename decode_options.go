package opentile

import "github.com/wsilabs/opentile-go/decoder"

// DecodeOption configures a *Slide.DecodedTile / DecodedTileInto call.
// See WithFormat, WithScale.
type DecodeOption func(*decodeConfig)

// decodeConfig is the resolved option set. Defaults: PixelFormatRGB,
// scale=1.
type decodeConfig struct {
	format decoder.PixelFormat
	scale  int
}

func newDecodeConfig(opts []DecodeOption) decodeConfig {
	c := decodeConfig{
		format: decoder.PixelFormatRGB,
		scale:  1,
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

// WithScale sets the IDCT-time scale factor (JPEG decoders only).
// Valid values: 1, 2, 4, 8. Non-JPEG sources return ErrUnsupportedScale
// from the underlying decoder if Scale != 1.
func WithScale(s int) DecodeOption {
	return func(c *decodeConfig) { c.scale = s }
}
