package ometiff

import (
	"fmt"
	"io"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// associatedImage is the OME opentile.AssociatedImage implementation
// for macro / label / thumbnail Image entries. Bytes() returns the
// page's single-strip JPEG payload (with optional JPEGTables splice
// for OME files that carry them — Leica fixtures don't).
//
// Mirrors Philips's associated.go shape. OME associated images are
// typically single-strip stripped JPEGs; their pyramid (lower
// resolutions via the page's own SubIFDs) is NOT exposed by upstream
// or by us.
type associatedImage struct {
	kind         string
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	jpegTables   []byte
	reader       io.ReaderAt
}

func (a *associatedImage) Type() string                      { return a.kind }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

func (a *associatedImage) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 {
		return nil, fmt.Errorf("ome: associated %s has no strips", a.kind)
	}
	// OME associated images on planar=2 pages carry rowsperstrip *
	// samplesperpixel strips (e.g. Leica-1 macro: 14004 strips for a
	// 4668-row planar=2 RGB page). Python opentile silently consumes
	// only strip 0 (which is plane 0 row 0) via NdpiOneFrameImage's
	// _read_frame(0); we mirror that for byte parity. The other strips
	// are dropped — listed as a deviation alongside multi-image OME
	// exposure.
	buf := make([]byte, a.stripCounts[0])
	if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[0])); err != nil {
		return nil, fmt.Errorf("ome: read associated %s strip: %w", a.kind, err)
	}
	if a.compression != opentile.CompressionJPEG || len(a.jpegTables) == 0 {
		return buf, nil
	}
	out, err := jpeg.InsertTables(buf, a.jpegTables)
	if err != nil {
		return nil, fmt.Errorf("ome: splice tables for associated %s: %w", a.kind, err)
	}
	return out, nil
}

// newAssociatedImage builds an AssociatedImage from a macro / label /
// thumbnail page. Reads StripOffsets / StripByteCounts and JPEGTables.
// The kind label ("macro" / "label" / "thumbnail") is supplied by the
// caller from the OME-XML classifier output.
//
// One Open quirk: Python opentile names its overview accessor
// `get_overview()` while OME XML uses Name="macro". We map "macro"
// → Type() == "overview" to keep our public AssociatedImage.Type()
// semantics consistent across all formats (SVS / NDPI / Philips
// already use "overview").
func newAssociatedImage(kind string, p *tiff.Page, r io.ReaderAt) (*associatedImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ome: associated %s missing ImageWidth", kind)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ome: associated %s missing ImageLength", kind)
	}
	soffs, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("ome: associated %s StripOffsets: %w", kind, err)
	}
	scnts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("ome: associated %s StripByteCounts: %w", kind, err)
	}
	if len(soffs) != len(scnts) {
		return nil, fmt.Errorf("ome: associated %s strip table mismatch: offsets=%d counts=%d",
			kind, len(soffs), len(scnts))
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
		kind:         kind,
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  ocomp,
		stripOffsets: soffs,
		stripCounts:  scnts,
		jpegTables:   jpegTables,
		reader:       r,
	}, nil
}
