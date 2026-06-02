package opentile

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// ReadRegionScaled reads an L0-coord rectangle and resamples it to
// (outW, outH). The library picks the best source pyramid level via
// BestLevelForDownsample, reads at that level, then resamples to the
// target.
//
// All input coords (l0x, l0y, l0w, l0h) are at L0 (full-resolution)
// coordinates. Output dimensions (outW, outH) are the desired final
// pixel size — any value, regardless of level resolutions.
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
// Shortcut for ImageReadRegionScaled(0, l0x, l0y, l0w, l0h, outW, outH, opts...).
//
// Added in v0.25.
func (s *Slide) ReadRegionScaled(l0x, l0y, l0w, l0h, outW, outH int, opts ...DecodeOption) (*decoder.Image, error) {
	return s.ImageReadRegionScaled(0, l0x, l0y, l0w, l0h, outW, outH, opts...)
}

// ReadRegionScaledInto fills caller-provided dst. dst.Width / dst.Height
// define the output dimensions.
func (s *Slide) ReadRegionScaledInto(l0x, l0y, l0w, l0h int, dst *decoder.Image, opts ...DecodeOption) error {
	return s.ImageReadRegionScaledInto(0, l0x, l0y, l0w, l0h, dst, opts...)
}

// ImageReadRegionScaled is the multi-image variant of ReadRegionScaled.
func (s *Slide) ImageReadRegionScaled(image int, l0x, l0y, l0w, l0h, outW, outH int, opts ...DecodeOption) (*decoder.Image, error) {
	if l0w <= 0 || l0h <= 0 {
		return nil, ErrRegionEmpty
	}
	if outW <= 0 || outH <= 0 {
		return nil, fmt.Errorf("opentile: ReadRegionScaled: outW and outH must be positive")
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

	intermediate, err := s.ImageReadRegion(image, level, levelX, levelY, levelW, levelH, opts...)
	if err != nil {
		return nil, err
	}

	if intermediate.Width == outW && intermediate.Height == outH {
		return intermediate, nil // no resample needed; exact-match
	}

	dst := decoder.NewImageFormat(outW, outH, cfg.format)
	if err := resample.ImageInto(intermediate, dst, cfg.kernel); err != nil {
		return nil, fmt.Errorf("opentile: ReadRegionScaled resample: %w", err)
	}
	return dst, nil
}

// ImageReadRegionScaledInto is the multi-image variant.
func (s *Slide) ImageReadRegionScaledInto(image int, l0x, l0y, l0w, l0h int, dst *decoder.Image, opts ...DecodeOption) error {
	if dst == nil {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst is nil")
	}
	if dst.Width <= 0 || dst.Height <= 0 {
		return fmt.Errorf("opentile: ReadRegionScaledInto: dst dims must be positive")
	}
	out, err := s.ImageReadRegionScaled(image, l0x, l0y, l0w, l0h, dst.Width, dst.Height, opts...)
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
