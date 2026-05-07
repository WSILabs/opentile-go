package ometiff

import (
	"io"
	"math"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/oneframe"
	"github.com/cornish/opentile-go/internal/tiff"
)

// newOneFrameImage constructs an OME Level backed by a single-strip
// JPEG (typical for OME's reduced-resolution levels — the v0.6 T2
// audit found 3 of 5 levels in Leica-1 and 4 of 6 in each Leica-2
// main pyramid are non-tiled).
//
// Delegates to internal/oneframe (factored from formats/ndpi/ in
// v0.6 T10). Upstream Python opentile's OmeTiffOneFrameImage extends
// NdpiOneFrameImage directly; our shared package mirrors that.
func newOneFrameImage(
	index int,
	p *tiff.Page,
	tileSize opentile.Size,
	baseSize opentile.Size,
	baseMPP opentile.SizeMm,
	r io.ReaderAt,
) (*oneframe.Image, error) {
	iw, _ := p.ImageWidth()
	il, _ := p.ImageLength()
	size := opentile.Size{W: int(iw), H: int(il)}

	pyr := 0
	if baseSize.W > 0 && size.W > 0 {
		pyr = int(math.Round(math.Log2(float64(baseSize.W) / float64(size.W))))
		if pyr < 0 {
			pyr = 0
		}
	}
	mpp := scaleMPP(baseMPP, baseSize, size)

	return oneframe.New(p, r, oneframe.Options{
		Index:          index,
		PyramidIdx:     pyr,
		MPP:            mpp,
		Size:           size,
		TileSize:       tileSize,
		FirstStripOnly: true, // OME planar pages have rowsperstrip*samplesperpixel strips; Python reads strip 0
	})
}
