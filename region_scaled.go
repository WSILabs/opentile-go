package opentile

import (
	"errors"
	"fmt"
	imagelib "image"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// ReadRegionScaled reads an L0-coord rectangle and resamples it to out.
// The library picks the best source pyramid level via
// BestLevelForDownsample, reads at that level, then resamples to the
// target.
//
// src is at L0 (full-resolution) coordinates. out dimensions are the
// desired final pixel size — any value, regardless of level resolutions.
//
// Use WithResampleKernel to choose the resample quality. Default:
// resample.Lanczos. Use WithFormat / WithScale as with DecodedTile.
//
// Out-of-bounds: the L0 rectangle is auto-clipped against the slide
// dimensions before level selection. Out-of-bounds output pixels are
// white-filled.
//
// Returns ErrRegionEmpty if the L0 rectangle has no in-bounds pixels.
//
// Shortcut for ImageReadRegionScaled(0, src, out, opts...).
//
// Added in v0.25.
func (s *Slide) ReadRegionScaled(src Region, out Size, opts ...DecodeOption) (*decoder.Image, error) {
	return s.ImageReadRegionScaled(0, src, out, opts...)
}

// ReadRegionScaledInto fills caller-provided dst. dst.Width / dst.Height
// define the output dimensions.
func (s *Slide) ReadRegionScaledInto(src Region, dst *decoder.Image, opts ...DecodeOption) error {
	return s.ImageReadRegionScaledInto(0, src, dst, opts...)
}

// ImageReadRegionScaled is the multi-image variant of ReadRegionScaled.
func (s *Slide) ImageReadRegionScaled(image int, src Region, out Size, opts ...DecodeOption) (*decoder.Image, error) {
	l0x, l0y, l0w, l0h := src.Origin.X, src.Origin.Y, src.Size.W, src.Size.H
	outW, outH := out.W, out.H
	if l0w <= 0 || l0h <= 0 {
		return nil, ErrRegionEmpty
	}
	if outW <= 0 || outH <= 0 {
		return nil, fmt.Errorf("opentile: ReadRegionScaled: out dims must be positive")
	}
	cfg := newDecodeConfig(opts)

	downsampleX := float64(l0w) / float64(outW)
	downsampleY := float64(l0h) / float64(outH)
	downsample := downsampleX
	if downsampleY > downsample {
		downsample = downsampleY
	}
	if downsample < 1.0 {
		downsample = 1.0 // never upsample below L0
	}

	level := s.ImageBestLevelForDownsample(image, downsample)
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}

	// Fast path: route through the codec-scaling strip machinery when it
	// applies — RGB output, no explicit scale override, and the source
	// level's codec can downscale in the codec domain with a residual > 1.
	// This decodes part of the residual in the codec (faster + anti-aliased)
	// instead of full-decode + spatial resample. Other cases fall through.
	l0Rect := imagelib.Rect(l0x, l0y, l0x+l0w, l0y+l0h)
	outSize := imagelib.Pt(outW, outH)
	if cfg.format == decoder.PixelFormatRGB && cfg.scale <= 1 &&
		scaleCapable(lvl.Compression) && autoIDCTScale(lvl, l0Rect, outSize) > 1 {
		sc := newStripConfig([]StripOption{WithStripKernel(cfg.kernel)})
		it := newStripIterator(s, image, l0Rect, outSize, outH, sc)
		defer it.Close()
		strip, err := it.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, ErrRegionEmpty
			}
			return nil, err
		}
		return strip, nil
	}

	// Translate L0 rect to level rect via the chosen level's Downsample.
	lds := lvl.Downsample
	if lds <= 0 {
		lds = 1
	}
	levelX := int(float64(l0x) / lds)
	levelY := int(float64(l0y) / lds)
	levelW := int(float64(l0w) / lds)
	levelH := int(float64(l0h) / lds)
	if levelW <= 0 {
		levelW = 1
	}
	if levelH <= 0 {
		levelH = 1
	}

	intermediate, err := s.ImageReadRegion(image, level, Region{Origin: Point{X: levelX, Y: levelY}, Size: Size{W: levelW, H: levelH}}, opts...)
	if err != nil {
		return nil, err
	}

	if intermediate.Width == outW && intermediate.Height == outH {
		return intermediate, nil // no resample needed; exact-match
	}

	dstImg := decoder.NewImageFormat(outW, outH, cfg.format)
	if err := resample.ImageInto(intermediate, dstImg, cfg.kernel); err != nil {
		return nil, fmt.Errorf("opentile: ReadRegionScaled resample: %w", err)
	}
	return dstImg, nil
}

// ImageReadRegionScaledInto is the multi-image variant.
func (s *Slide) ImageReadRegionScaledInto(image int, src Region, dst *decoder.Image, opts ...DecodeOption) error {
	if dst == nil {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst is nil")
	}
	if dst.Width <= 0 || dst.Height <= 0 {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst dims must be positive")
	}
	out, err := s.ImageReadRegionScaled(image, src, Size{W: dst.Width, H: dst.Height}, opts...)
	if err != nil {
		return err
	}
	if dst.Width != out.Width || dst.Height != out.Height {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst dims (%dx%d) != computed output dims (%dx%d)",
			dst.Width, dst.Height, out.Width, out.Height)
	}
	if dst.Format != out.Format {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst format %v != opts format %v", dst.Format, out.Format)
	}
	copy(dst.Pix, out.Pix)
	return nil
}
