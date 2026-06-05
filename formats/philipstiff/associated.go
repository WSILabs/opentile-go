package philipstiff

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// associatedImage is the Philips opentile.AssociatedImage implementation
// for label / macro / thumbnail pages.
//
// Philips stores associated images as single-strip JPEG-compressed pages
// (Compression=7, no TileWidth/Length tags). Bytes() reads the strip data,
// splices the page's JPEGTables before SOS — no APP14, since Philips
// encodes standard YCbCr — and returns the result. Direct port of
// PhilipsAssociatedTiffImage / PhilipsThumbnailTiffImage's NativeTiledTiffImage
// inheritance: tiled_size = (1, 1), get_tile(0,0) reads the lone strip
// + splices tables (philips_tiff_image.py:32-75 + tiff_image.py:490-498).
type associatedImage struct {
	imageType    string
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	jpegTables   []byte
	reader       io.ReaderAt
}

func (a *associatedImage) Type() string                      { return a.imageType }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

func (a *associatedImage) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 {
		return nil, fmt.Errorf("philips: associated %s has no strips", a.imageType)
	}
	if len(a.stripOffsets) > 1 {
		// Our 4 fixtures all have single-strip associated images. Multi-
		// strip would require ConcatenateScans-style assembly (see
		// formats/svs/associated.go); leave it as a clear error rather
		// than silently returning the first strip.
		return nil, fmt.Errorf("philips: associated %s has %d strips; multi-strip not yet supported",
			a.imageType, len(a.stripOffsets))
	}
	buf := make([]byte, a.stripCounts[0])
	if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[0])); err != nil {
		return nil, fmt.Errorf("philips: read associated %s strip: %w", a.imageType, err)
	}
	if a.compression != opentile.CompressionJPEG || len(a.jpegTables) == 0 {
		return buf, nil
	}
	out, err := jpeg.InsertTables(buf, a.jpegTables)
	if err != nil {
		return nil, fmt.Errorf("philips: splice tables for associated %s: %w", a.imageType, err)
	}
	return out, nil
}

// newAssociatedImage builds an AssociatedImage from a Philips
// label/macro/thumbnail page. Reads StripOffsets/StripByteCounts and
// JPEGTables; the type label is supplied by the caller (mapped from
// the description-substring classifier).
func newAssociatedImage(imageType string, p *tiff.Page, r io.ReaderAt) (*associatedImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("philips: associated %s missing ImageWidth", imageType)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("philips: associated %s missing ImageLength", imageType)
	}
	soffs, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("philips: associated %s StripOffsets: %w", imageType, err)
	}
	scnts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("philips: associated %s StripByteCounts: %w", imageType, err)
	}
	if len(soffs) != len(scnts) {
		return nil, fmt.Errorf("philips: associated %s strip table mismatch: offsets=%d counts=%d",
			imageType, len(soffs), len(scnts))
	}
	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	return &associatedImage{
		imageType:    imageType,
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  ocomp,
		stripOffsets: soffs,
		stripCounts:  scnts,
		jpegTables:   jpegTables,
		reader:       r,
	}, nil
}
