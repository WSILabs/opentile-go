package opentile

import (
	imagelib "image"
)

// imageScaledStrips is the logic-bearing scaled-strip iterator, backing
// (*Pyramid).ScaledStrips. It scopes the iteration to the given pyramid
// index, returning horizontal strips of stripHeight rows scaled to
// outSize.
//
// Out-of-bounds l0Rect: auto-clipped to slide bounds; entirely
// out-of-bounds yields all-white strips.
//
// Internally manages parallel decode workers + a per-iterator tile
// cache + lookahead pre-fetch. Caller must call Close on the
// returned iterator.
func (s *Slide) imageScaledStrips(
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
