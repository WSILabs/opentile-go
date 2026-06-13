package opentile

import (
	imagelib "image"
)

// ScaledStrips returns an iterator over a slide's L0 rectangle,
// scaled to outSize, in horizontal strips of stripHeight rows.
// See the *StripIterator type for the iteration API.
//
// Out-of-bounds l0Rect: auto-clipped to slide bounds; entirely
// out-of-bounds yields all-white strips.
//
// Internally manages parallel decode workers + a per-iterator tile
// cache + lookahead pre-fetch. Caller must call Close on the
// returned iterator.
//
// Shortcut for ImageScaledStrips(0, ...) — multi-image variant is
// deferred to a future release.
//
// Added in v0.26.
func (s *Slide) ScaledStrips(
	l0Rect Region,
	outSize Size,
	stripHeight int,
	opts ...StripOption,
) *StripIterator {
	return s.ImageScaledStrips(0, l0Rect, outSize, stripHeight, opts...)
}

// ImageScaledStrips is the multi-image variant of ScaledStrips: it scopes
// the scaled-strip iteration to the given pyramid index. Backs
// (*Pyramid).ScaledStrips.
func (s *Slide) ImageScaledStrips(
	image int,
	l0Rect Region,
	outSize Size,
	stripHeight int,
	opts ...StripOption,
) *StripIterator {
	cfg := newStripConfig(opts)
	r := imagelib.Rect(l0Rect.Origin.X, l0Rect.Origin.Y,
		l0Rect.Origin.X+l0Rect.Size.W, l0Rect.Origin.Y+l0Rect.Size.H)
	p := imagelib.Pt(outSize.W, outSize.H)
	return newStripIterator(s, image, r, p, stripHeight, cfg)
}
