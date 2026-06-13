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
	cfg := newStripConfig(opts)
	r := imagelib.Rect(l0Rect.Origin.X, l0Rect.Origin.Y,
		l0Rect.Origin.X+l0Rect.Size.W, l0Rect.Origin.Y+l0Rect.Size.H)
	p := imagelib.Pt(outSize.W, outSize.H)
	return newStripIterator(s, 0, r, p, stripHeight, cfg)
}
