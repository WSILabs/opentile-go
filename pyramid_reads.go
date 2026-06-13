package opentile

import (
	"github.com/wsilabs/opentile-go/decoder"
)

// This file holds the v1.0 cross-level receiver-method read API on
// *Pyramid. Each method delegates to the owning Slide using the pyramid's
// Index, which ensurePyramids guarantees is populated. A *Pyramid obtained
// via navigation (Slide.Pyramid, Slide.Pyramids) carries its
// back-reference; calling these on a zero-value Pyramid (slide == nil)
// panics, by design.

// LevelPtrs returns stable *Level pointers for this pyramid's levels,
// finest-to-coarsest. Named LevelPtrs (not Levels) because the exported
// struct field Pyramid.Levels []Level — the reader-population contract —
// occupies the Levels identifier; a field and a method cannot share a
// name in Go. Use the Levels field for value-type iteration, or LevelPtrs
// / Level(i) for the receiver-method read API.
func (p *Pyramid) LevelPtrs() []*Level {
	p.slide.ensurePyramids()
	out := make([]*Level, len(p.Levels))
	for i := range p.Levels {
		out[i] = &p.Levels[i]
	}
	return out
}

// Level returns the i-th level of this pyramid, or ErrLevelOutOfRange if
// i is out of range.
func (p *Pyramid) Level(i int) (*Level, error) {
	p.slide.ensurePyramids()
	if i < 0 || i >= len(p.Levels) {
		return nil, ErrLevelOutOfRange
	}
	return &p.Levels[i], nil
}

// BestLevelForDownsample returns the *Level whose Downsample factor is
// the largest value ≤ the requested downsample (openslide semantics).
// Returns level 0 when every level is finer than requested.
func (p *Pyramid) BestLevelForDownsample(downsample float64) *Level {
	idx := p.slide.imageBestLevelForDownsample(p.Index, downsample)
	if idx < 0 || idx >= len(p.Levels) {
		return nil
	}
	return &p.Levels[idx]
}

// ReadRegionScaled reads an L0-coord rectangle within this pyramid and
// resamples it to out. See (*Slide).ReadRegionScaled for semantics.
func (p *Pyramid) ReadRegionScaled(src Region, out Size, opts ...DecodeOption) (*decoder.Image, error) {
	return p.slide.imageReadRegionScaled(p.Index, src, out, opts...)
}

// ReadRegionScaledInto fills the caller-provided dst with the resampled
// L0-coord rectangle within this pyramid.
func (p *Pyramid) ReadRegionScaledInto(src Region, dst *decoder.Image, opts ...DecodeOption) error {
	return p.slide.imageReadRegionScaledInto(p.Index, src, dst, opts...)
}

// ScaledStrips returns a scaled-strip iterator over this pyramid's L0
// rectangle. See (*Slide).ScaledStrips for the iteration API; the caller
// must Close the returned iterator.
func (p *Pyramid) ScaledStrips(src Region, out Size, stripHeight int, opts ...StripOption) *StripIterator {
	return p.slide.imageScaledStrips(p.Index, src, out, stripHeight, opts...)
}
