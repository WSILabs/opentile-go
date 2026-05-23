package ndpi

import (
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/oneframe"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// newOneFrameImage constructs an NDPI Level backed by a single JPEG
// per page (typical for lower pyramid levels that fit in one JPEG).
// Delegates to internal/oneframe — the v0.6 OneFrame factor moved the
// generic single-strip-JPEG-with-virtual-tile-coords machinery into a
// shared package so OME TIFF can reuse it.
//
// NDPI passes a zero opentile.Size in Options.Size so oneframe reads
// ImageWidth / ImageLength from the page itself; OME passes a
// caller-controlled Size for SubIFD-corrected dims. Otherwise the
// shape is identical to v0.5's NDPI-only implementation.
func newOneFrameImage(
	index int,
	p *tiff.Page,
	tileSize opentile.Size,
	r io.ReaderAt,
) (*oneframe.Image, error) {
	return oneframe.New(p, r, oneframe.Options{
		Index:    index,
		TileSize: tileSize,
	})
}
