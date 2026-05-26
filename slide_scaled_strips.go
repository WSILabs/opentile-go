package opentile

import (
	"image"
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
	l0Rect image.Rectangle,
	outSize image.Point,
	stripHeight int,
	opts ...StripOption,
) *StripIterator {
	cfg := newStripConfig(opts)
	return newStripIterator(s, 0, l0Rect, outSize, stripHeight, cfg)
}
