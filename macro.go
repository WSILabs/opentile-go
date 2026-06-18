package opentile

import (
	"fmt"
	"math"

	"github.com/wsilabs/opentile-go/decoder"
)

// macroScanWidthMM / macroScanHeightMM are the physical dimensions of the
// non-label scan area of a standard 1"×3" microscope slide (the coverslip
// region), in millimetres. The label end (~25 mm) is intentionally excluded —
// RenderMacro composites only this region. (Future option: include the label.)
const (
	macroScanWidthMM  = 50.0
	macroScanHeightMM = 25.0
)

// RenderMacro renders a synthesized "pseudo-macro": a slide-shaped canvas
// (the non-label scan area of a standard microscope slide, ~50×25 mm) with the
// whole-slide tissue thumbnail composited at its TRUE physical size — derived
// from the slide's microns-per-pixel — and centred. It gives a viewer a
// macro-style orientation image for slides that don't embed one.
//
// `bounds` sizes the slide canvas (same zero-axis fit convention as
// RenderThumbnail: a zero axis is unconstrained). The scan area is ~2:1, so
// bounds {W:600} → a 600×300 canvas.
//
// Physical scale comes from Metadata.MPP; if that's absent, it falls back to
// the objective magnification (mpp ≈ 10 / Magnification: 40× → 0.25, 20× → 0.5).
// If the slide reports neither MPP nor magnification, RenderMacro returns an
// error (it cannot place the tissue to scale).
//
// The tissue is centred (true on-slide POSITION is not yet modelled — a future
// enhancement). If the tissue is physically larger than the scan area it is
// scaled down to fit, preserving aspect. For BIF the tissue render is correctly
// stitched. opts pass through to the tissue decode (e.g. WithFormat).
func (s *Slide) RenderMacro(bounds Size, opts ...DecodeOption) (*decoder.Image, error) {
	p := s.Pyramid(0)
	if p == nil {
		return nil, ErrImageIndexOutOfRange
	}
	l0, err := p.Level(0)
	if err != nil {
		return nil, err
	}
	mppX, mppY, err := effectiveMPP(s.Metadata())
	if err != nil {
		return nil, err
	}

	// Canvas = the scan area fitted into bounds.
	canvas, err := fitAspect(macroScanWidthMM, macroScanHeightMM, bounds)
	if err != nil {
		return nil, err
	}
	pxPerMM := float64(canvas.W) / macroScanWidthMM

	// Tissue physical extent (mm) → canvas pixels.
	tissueWmm := float64(l0.Size.W) * mppX / 1000.0
	tissueHmm := float64(l0.Size.H) * mppY / 1000.0
	tw := int(tissueWmm*pxPerMM + 0.5)
	th := int(tissueHmm*pxPerMM + 0.5)
	// Clamp to the canvas if the tissue is physically larger than the scan area.
	if tw > canvas.W || th > canvas.H {
		fit := math.Min(float64(canvas.W)/float64(tw), float64(canvas.H)/float64(th))
		tw = int(float64(tw)*fit + 0.5)
		th = int(float64(th)*fit + 0.5)
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}

	// Render the tissue to exactly tw×th (ReadRegionScaled stretches L0 to the
	// requested out size, so anisotropic MPP is honoured as physical proportion).
	tissue, err := p.ReadRegionScaled(Region{Origin: Point{X: 0, Y: 0}, Size: l0.Size}, Size{W: tw, H: th}, opts...)
	if err != nil {
		return nil, err
	}

	cfg := newDecodeConfig(opts)
	out := decoder.NewImageFormat(canvas.W, canvas.H, cfg.format)
	fillWhite(out)
	dstX := (canvas.W - tw) / 2
	dstY := (canvas.H - th) / 2
	blitInto(tissue, 0, 0, tw, th, out, dstX, dstY)
	return out, nil
}

// effectiveMPP resolves the microns-per-pixel to use for physical scaling:
// Metadata.MPP if present (a single populated axis fills the other), else
// 10/Magnification (the standard objective rule of thumb: 40× → 0.25,
// 20× → 0.5), else an error.
func effectiveMPP(md Metadata) (x, y float64, err error) {
	x, y = md.MPP.X, md.MPP.Y
	if x == 0 {
		x = y
	}
	if y == 0 {
		y = x
	}
	if x > 0 && y > 0 {
		return x, y, nil
	}
	if md.Magnification > 0 {
		m := 10.0 / md.Magnification
		return m, m, nil
	}
	return 0, 0, fmt.Errorf("opentile: RenderMacro: slide reports neither MPP nor objective magnification; cannot scale the macro accurately")
}

// fitAspect returns the largest Size with aspect aw:ah that fits within bounds,
// where a zero (or negative) bounds axis is unconstrained. Unlike
// thumbnailTargetSize it does NOT clamp to avoid upscaling — the macro canvas is
// synthetic and may be rendered at any resolution. Errors if bounds constrains
// neither axis or the aspect is degenerate.
func fitAspect(aw, ah float64, bounds Size) (Size, error) {
	if aw <= 0 || ah <= 0 {
		return Size{}, fmt.Errorf("opentile: fitAspect: degenerate aspect %gx%g", aw, ah)
	}
	if bounds.W <= 0 && bounds.H <= 0 {
		return Size{}, fmt.Errorf("opentile: RenderMacro: bounds must constrain at least one axis (got %dx%d)", bounds.W, bounds.H)
	}
	aspect := aw / ah
	var w, h float64
	switch {
	case bounds.W > 0 && bounds.H > 0:
		if float64(bounds.W)/float64(bounds.H) > aspect {
			h = float64(bounds.H)
			w = h * aspect
		} else {
			w = float64(bounds.W)
			h = w / aspect
		}
	case bounds.W > 0:
		w = float64(bounds.W)
		h = w / aspect
	default: // bounds.H > 0
		h = float64(bounds.H)
		w = h * aspect
	}
	return Size{W: max(1, int(w+0.5)), H: max(1, int(h+0.5))}, nil
}
