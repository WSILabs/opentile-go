package ometiff

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
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
	imageType    opentile.AssociatedType
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	jpegTables   []byte
	samples      int // SamplesPerPixel (planar JPEG reassembly)
	rowsPerStrip int
	planar       int // PlanarConfiguration (2 = separate R/G/B planes)
	photometric  int
	reader       io.ReaderAt
}

func (a *associatedImage) Type() opentile.AssociatedType     { return a.imageType }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

// AssociatedEncoding returns the strip source + tags for faithful standalone
// re-emission (GH #22). ok=false for PlanarConfiguration=2 pages (Leica
// macro) — not representable as a simple single-IFD strip copy.
func (a *associatedImage) AssociatedEncoding() (opentile.AssociatedEncoding, bool) {
	if a.planar == 2 || len(a.stripOffsets) == 0 {
		return opentile.AssociatedEncoding{}, false
	}
	strips := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return opentile.AssociatedEncoding{}, false
		}
		strips[i] = buf
	}
	return opentile.AssociatedEncoding{
		Strips:       strips,
		Compression:  a.compression,
		JPEGTables:   a.jpegTables,
		RowsPerStrip: a.rowsPerStrip,
		Samples:      a.samples,
		Photometric:  a.photometric,
	}, true
}

// Decode returns the faithfully-decoded associated-image pixels (GH #20).
// Planar (PlanarConfiguration=2) multi-strip JPEG pages — Leica's macro is
// stored as one grayscale JPEG per (plane,row) — are reassembled per channel;
// Bytes() deliberately returns only strip 0 for Python byte-parity, so it
// can't be used for decode. Other (single-strip) JPEGs decode via Bytes().
func (a *associatedImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if a.compression == opentile.CompressionJPEG && a.planar == 2 && len(a.stripOffsets) > 1 {
		return a.decodePlanarJPEG(opts)
	}
	data, err := a.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(a.Compression(), data, opts)
}

// decodePlanarJPEG reassembles a PlanarConfiguration=2 multi-strip JPEG page.
// Strips are ordered plane-major (all of plane 0's rows, then plane 1, then
// plane 2), each a grayscale JPEG of RowsPerStrip rows. Each plane fills one
// output channel.
func (a *associatedImage) decodePlanarJPEG(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale > 1 {
		return nil, decoder.ErrUnsupportedScale
	}
	fac, ok := decoder.GetByCompressionTag(7) // JPEG
	if !ok {
		return nil, fmt.Errorf("ome: no JPEG decoder: %w", decoder.ErrCodecUnavailable)
	}
	dec := fac.New()
	defer dec.Close()
	w, h := a.size.W, a.size.H
	spp := a.samples
	if spp <= 0 {
		spp = 3
	}
	rps := a.rowsPerStrip
	if rps <= 0 {
		rps = 1
	}
	stripsPerPlane := (h + rps - 1) / rps
	if stripsPerPlane*spp != len(a.stripOffsets) {
		return nil, fmt.Errorf("ome: planar strip count %d != %d planes x %d strips/plane",
			len(a.stripOffsets), spp, stripsPerPlane)
	}
	full := decoder.NewImageFormat(w, h, opts.Format)
	bpp := 3
	if opts.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	planes := spp
	if planes > 3 {
		planes = 3
	}
	for pl := 0; pl < planes; pl++ {
		for s := 0; s < stripsPerPlane; s++ {
			idx := pl*stripsPerPlane + s
			buf := make([]byte, a.stripCounts[idx])
			if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[idx])); err != nil {
				return nil, fmt.Errorf("ome: read planar strip %d: %w", idx, err)
			}
			data := buf
			if len(a.jpegTables) > 0 {
				if d, err := jpeg.InsertTables(buf, a.jpegTables); err == nil {
					data = d
				}
			}
			sub, err := dec.Decode(data, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
			if err != nil {
				return nil, fmt.Errorf("ome: decode planar strip %d: %w", idx, err)
			}
			y0 := s * rps
			for y := 0; y < sub.Height && y0+y < h; y++ {
				srow := y * sub.Stride
				drow := (y0 + y) * full.Stride
				for x := 0; x < sub.Width && x < w; x++ {
					full.Pix[drow+x*bpp+pl] = sub.Pix[srow+x*3] // grayscale → channel pl
				}
			}
		}
	}
	if opts.Format == decoder.PixelFormatRGBA {
		for i := 3; i < len(full.Pix); i += 4 {
			full.Pix[i] = 0xFF
		}
	}
	return full, nil
}

func (a *associatedImage) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 {
		return nil, fmt.Errorf("ome: associated %s has no strips", a.imageType)
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
		return nil, fmt.Errorf("ome: read associated %s strip: %w", a.imageType, err)
	}
	if a.compression != opentile.CompressionJPEG || len(a.jpegTables) == 0 {
		return buf, nil
	}
	out, err := jpeg.InsertTables(buf, a.jpegTables)
	if err != nil {
		return nil, fmt.Errorf("ome: splice tables for associated %s: %w", a.imageType, err)
	}
	return out, nil
}

// newAssociatedImage builds an AssociatedImage from a macro / label /
// thumbnail page. Reads StripOffsets / StripByteCounts and JPEGTables.
// The type label ("macro" / "label" / "thumbnail") is supplied by the
// caller from the OME-XML classifier output.
//
// One Open quirk: Python opentile names its overview accessor
// `get_overview()` while OME XML uses Name="macro". We map "macro"
// → Type() == "overview" to keep our public AssociatedImage.Type()
// semantics consistent across all formats (SVS / NDPI / Philips
// already use "overview").
func newAssociatedImage(imageType opentile.AssociatedType, p *tiff.Page, r io.ReaderAt) (*associatedImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ome: associated %s missing ImageWidth", imageType)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ome: associated %s missing ImageLength", imageType)
	}
	soffs, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("ome: associated %s StripOffsets: %w", imageType, err)
	}
	scnts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("ome: associated %s StripByteCounts: %w", imageType, err)
	}
	if len(soffs) != len(scnts) {
		return nil, fmt.Errorf("ome: associated %s strip table mismatch: offsets=%d counts=%d",
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

	spp, _ := p.SamplesPerPixel()
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	planar, _ := p.ScalarU32(284) // PlanarConfiguration
	photo, _ := p.Photometric()

	return &associatedImage{
		imageType:    imageType,
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  ocomp,
		stripOffsets: soffs,
		stripCounts:  scnts,
		jpegTables:   jpegTables,
		samples:      int(spp),
		rowsPerStrip: int(rps),
		planar:       int(planar),
		photometric:  int(photo),
		reader:       r,
	}, nil
}
