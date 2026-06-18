package opentile

import (
	"fmt"
	"math"

	"github.com/wsilabs/opentile-go/decoder"
)

// RenderThumbnail renders the whole slide (this pyramid) downscaled to fit
// bounds, preserving aspect ratio, as a freshly-resampled image. A zero (or
// negative) value on either axis of bounds is unconstrained — the output size
// is derived from the other axis and the slide's aspect ratio:
//
//	bounds {W:256, H:256} → largest image fitting inside 256×256 (fit-box)
//	bounds {W:256, H:0}   → width 256, height from aspect (fit-width)
//	bounds {W:0,   H:256} → height 256, width from aspect (fit-height)
//
// It never upscales beyond level 0 (if bounds exceeds L0 the output is L0's
// extent). The thumbnail is RENDERED from the image pyramid (best-level-sourced
// and Lanczos-resampled via ReadRegionScaled, so memory stays bounded for large
// slides) — it is NOT the embedded AssociatedThumbnail / AssociatedOverview;
// use AssociatedImages() for those. For BIF the render is correctly stitched.
//
// Returns an error if bounds constrains neither axis, if level 0 is unavailable,
// or (like ReadRegionScaled) if no decoder is registered for level 0's
// compression. opts pass through to the underlying decode (e.g. WithFormat).
func (p *Pyramid) RenderThumbnail(bounds Size, opts ...DecodeOption) (*decoder.Image, error) {
	l0, err := p.Level(0)
	if err != nil {
		return nil, err
	}
	out, err := thumbnailTargetSize(l0.Size, bounds)
	if err != nil {
		return nil, err
	}
	src := Region{Origin: Point{X: 0, Y: 0}, Size: l0.Size}
	return p.slide.imageReadRegionScaled(p.Index, src, out, opts...)
}

// RenderThumbnail renders the whole slide downscaled to fit bounds, using the
// first pyramid. See (*Pyramid).RenderThumbnail for the sizing convention and
// semantics. For multi-pyramid files (e.g. some OME-TIFF), use
// s.Pyramid(i).RenderThumbnail to choose a specific pyramid.
func (s *Slide) RenderThumbnail(bounds Size, opts ...DecodeOption) (*decoder.Image, error) {
	p := s.Pyramid(0)
	if p == nil {
		return nil, ErrImageIndexOutOfRange
	}
	return p.RenderThumbnail(bounds, opts...)
}

// thumbnailTargetSize computes the aspect-preserving output size for fitting an
// l0 (W×H) image into bounds. A zero or negative axis in bounds is
// unconstrained (its scale is ignored). The scale is the most constraining of
// the constrained axes, capped at 1.0 so a thumbnail never upscales past L0.
// Each output axis floors at 1px. Errors if bounds constrains neither axis or
// if l0 is degenerate.
func thumbnailTargetSize(l0, bounds Size) (Size, error) {
	if l0.W <= 0 || l0.H <= 0 {
		return Size{}, fmt.Errorf("opentile: RenderThumbnail: level 0 has degenerate size %dx%d", l0.W, l0.H)
	}
	if bounds.W <= 0 && bounds.H <= 0 {
		return Size{}, fmt.Errorf("opentile: RenderThumbnail: bounds must constrain at least one axis (got %dx%d)", bounds.W, bounds.H)
	}
	scale := math.Inf(1)
	if bounds.W > 0 {
		scale = math.Min(scale, float64(bounds.W)/float64(l0.W))
	}
	if bounds.H > 0 {
		scale = math.Min(scale, float64(bounds.H)/float64(l0.H))
	}
	if scale > 1.0 {
		scale = 1.0 // never upscale beyond L0
	}
	w := int(math.Round(float64(l0.W) * scale))
	h := int(math.Round(float64(l0.H) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return Size{W: w, H: h}, nil
}
