package bif

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
	"github.com/wsilabs/opentile-go/internal/tiffstrip"
)

// associatedImage is the BIF AssociatedImage implementation. BIF
// associated pages span three layouts across the two fixture
// generations:
//
//	Spec-compliant (Ventana-1):
//	  IFD 0 — Label_Image,         multi-strip, Compression=NONE   (RGB raw rows)
//	  IFD 1 — Probability_Image,   multi-strip, Compression=LZW    (grayscale)
//
//	Legacy iScan (OS-1):
//	  IFD 0 — "Label Image",       single-tile JPEG (Compression=JPEG)
//	  IFD 1 — "Thumbnail",         single-tile JPEG
//
// Bytes() handles both layouts. Single-tile JPEG returns the JPEG
// bytes (with JPEGTables splice if the IFD carries shared tables —
// rare on associated pages but supported). Multi-strip pages return
// the concatenated raw stored bytes; Compression() reports the
// source compression so consumers can decode appropriately.
//
// Caveat: multi-strip LZW pages (Ventana-1's probability map) yield
// a concatenation of independent per-strip LZW streams — not
// directly decodable as one stream. Consumers needing pixel data
// should decode each strip separately (boundaries available via the
// IFD's StripByteCounts tag). This matches BIF's "metadata reader"
// scope; if a real consumer surfaces, we can add a richer accessor.
type associatedImage struct {
	imageType   opentile.AssociatedType
	size        opentile.Size
	compression opentile.Compression

	// Exactly one set is populated, depending on layout:
	stripOffsets []uint64
	stripCounts  []uint64
	tileOffsets  []uint64
	tileCounts   []uint64

	jpegTables   []byte // tag 347 if present (typically nil on associated pages)
	samples      int    // strip-codec decode (None/LZW): SamplesPerPixel
	photometric  int
	predictor    int
	rowsPerStrip int
	reader       io.ReaderAt
	tiffTags     opentile.TIFFTags
}

func (a *associatedImage) Type() opentile.AssociatedType     { return a.imageType }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

// Encoding returns the strip source + tags for faithful standalone
// re-emission (GH #22). Strip-based only; ok=false for tiled associated pages.
func (a *associatedImage) Encoding() (opentile.AssociatedEncoding, bool) {
	if len(a.stripOffsets) == 0 {
		return opentile.AssociatedEncoding{}, false // tiled / no strips
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
		Predictor:    a.predictor,
		JPEGTables:   a.jpegTables,
		RowsPerStrip: a.rowsPerStrip,
		Samples:      a.samples,
		Photometric:  a.photometric,
	}, true
}

// TIFFTags returns the parsed TIFF tags of this associated image's backing IFD.
func (a *associatedImage) TIFFTags() (opentile.TIFFTags, bool) {
	if a.tiffTags == nil {
		return nil, false
	}
	return a.tiffTags, true
}

// IFDOffset returns the byte offset of this associated image's backing IFD.
// BIF format doesn't record per-associated IFD offsets; always returns ok=false.
func (a *associatedImage) IFDOffset() (int64, bool) { return 0, false }

// Decode returns the faithfully-decoded associated-image pixels (GH #20).
// JPEG decodes via the registry; strip-based None/LZW/Deflate (BIF overview
// is uncompressed, probability is LZW) decode via the strip path with
// predictor + sample interpretation.
func (a *associatedImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	switch a.compression {
	case opentile.CompressionNone, opentile.CompressionLZW, opentile.CompressionDeflate:
		if len(a.stripOffsets) == 0 {
			break // tiled None/LZW unsupported; fall through to a clean error
		}
		strips := make([][]byte, len(a.stripOffsets))
		for i := range a.stripOffsets {
			buf := make([]byte, a.stripCounts[i])
			if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
				return nil, fmt.Errorf("bif: read associated %s strip %d: %w", a.imageType, i, err)
			}
			strips[i] = buf
		}
		return tiffstrip.Decode(tiffstrip.Params{
			Width:        a.size.W,
			Height:       a.size.H,
			Samples:      a.samples,
			Photometric:  a.photometric,
			Predictor:    a.predictor,
			Compression:  int(opentile.CompressionToTIFFTag(a.compression)),
			RowsPerStrip: a.rowsPerStrip,
			Strips:       strips,
		}, opts)
	}
	data, err := a.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(a.Compression(), data, opts)
}

func (a *associatedImage) Bytes() ([]byte, error) {
	var buf []byte
	switch {
	case len(a.tileOffsets) == 1:
		// Single-tile path (legacy iScan IFD 0/1).
		b := make([]byte, a.tileCounts[0])
		if err := tiff.ReadAtFull(a.reader, b, int64(a.tileOffsets[0])); err != nil {
			return nil, fmt.Errorf("bif: read associated %s tile: %w", a.imageType, err)
		}
		buf = b

	case len(a.tileOffsets) > 1:
		// Multi-tile associated page — not seen in our fixtures.
		// Defensive: refuse rather than silently returning tile 0.
		return nil, fmt.Errorf("bif: associated %s has %d tiles; multi-tile not supported on associated pages", a.imageType, len(a.tileOffsets))

	case len(a.stripOffsets) == 0:
		return nil, fmt.Errorf("bif: associated %s has no strips or tiles", a.imageType)

	default:
		// Multi-strip path (spec-compliant IFD 0/1). Concatenate
		// every strip's raw bytes in order.
		total := uint64(0)
		for _, c := range a.stripCounts {
			total += c
		}
		b := make([]byte, total)
		cursor := uint64(0)
		for i, off := range a.stripOffsets {
			n := a.stripCounts[i]
			if err := tiff.ReadAtFull(a.reader, b[cursor:cursor+n], int64(off)); err != nil {
				return nil, fmt.Errorf("bif: read associated %s strip %d: %w", a.imageType, i, err)
			}
			cursor += n
		}
		buf = b
	}

	// JPEGTables splice: only meaningful on JPEG-compressed bytes.
	// Real BIF associated pages we've seen don't carry tag 347, but
	// we apply the splice symmetrically with the level path.
	if a.compression == opentile.CompressionJPEG && len(a.jpegTables) > 0 {
		out, err := jpeg.InsertTables(buf, a.jpegTables)
		if err != nil {
			return nil, fmt.Errorf("bif: splice tables for associated %s: %w", a.imageType, err)
		}
		return out, nil
	}
	return buf, nil
}

// newAssociatedImage builds the AssociatedImage from a classified
// IFD. The type label ("overview" / "probability" / "thumbnail") is
// supplied by the caller per the IFD-classification table in spec
// §5.3.
func newAssociatedImage(imageType opentile.AssociatedType, p *tiff.Page, r io.ReaderAt) (*associatedImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("bif: associated %s missing ImageWidth", imageType)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("bif: associated %s missing ImageLength", imageType)
	}
	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)
	spp, _ := p.SamplesPerPixel()
	photo, _ := p.Photometric()
	pred, _ := p.Predictor()
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)

	out := &associatedImage{
		imageType:    imageType,
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  ocomp,
		samples:      int(spp),
		photometric:  int(photo),
		predictor:    int(pred),
		rowsPerStrip: int(rps),
		reader:       r,
	}

	// Tile-based vs strip-based discrimination by tag presence.
	if _, hasTW := p.TileWidth(); hasTW {
		toffs, err := p.TileOffsets64()
		if err != nil {
			return nil, fmt.Errorf("bif: associated %s TileOffsets: %w", imageType, err)
		}
		tcnts, err := p.TileByteCounts64()
		if err != nil {
			return nil, fmt.Errorf("bif: associated %s TileByteCounts: %w", imageType, err)
		}
		if len(toffs) != len(tcnts) {
			return nil, fmt.Errorf("bif: associated %s tile table mismatch: offsets=%d counts=%d", imageType, len(toffs), len(tcnts))
		}
		out.tileOffsets = toffs
		out.tileCounts = tcnts
	} else {
		soffs, err := p.ScalarArrayU64(tiff.TagStripOffsets)
		if err != nil {
			return nil, fmt.Errorf("bif: associated %s StripOffsets: %w", imageType, err)
		}
		scnts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
		if err != nil {
			return nil, fmt.Errorf("bif: associated %s StripByteCounts: %w", imageType, err)
		}
		if len(soffs) != len(scnts) {
			return nil, fmt.Errorf("bif: associated %s strip table mismatch: offsets=%d counts=%d", imageType, len(soffs), len(scnts))
		}
		out.stripOffsets = soffs
		out.stripCounts = scnts
	}

	// JPEGTables (tag 347) — defensive read; rarely populated on
	// associated pages but follow the same shape as level.go.
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			out.jpegTables = tb
		}
	}
	out.tiffTags = opentile.TIFFTagsFromPage(p)
	return out, nil
}

// typeFromIFDRole maps the layout-classified role to the public
// AssociatedImage.Type() string. Per spec §5.3 + opentile-go's
// existing type taxonomy:
//
//	ifdRoleLabel       → "overview" (matches SVS / NDPI / Philips
//	                     convention; BIF whitepaper calls it "label")
//	ifdRoleProbability → "probability" (new in v0.7)
//	ifdRoleThumbnail   → "thumbnail"
//
// Returns an empty string for any other role; the caller skips it.
func typeFromIFDRole(role ifdRole) opentile.AssociatedType {
	switch role {
	case ifdRoleLabel:
		return opentile.AssociatedOverview
	case ifdRoleProbability:
		return opentile.AssociatedProbability
	case ifdRoleThumbnail:
		return opentile.AssociatedThumbnail
	default:
		return "" // empty = unknown; caller skips
	}
}

// labelHeightDenom is the denominator of the top-fraction of the overview
// (Label_Image) that the synthesized "label" covers. The Roche BIF whitepaper
// v1.0 reserves the top 25 mm of every 75 mm slide for the printed label, so
// the label is the top 25/75 = 1/3. A fixed geometric fraction is robust across
// both generations where the XMP boundary metadata is not (LabelBoundary is
// 1000 on OS-1 but 0 on Ventana-1 / DP 200). See GH #19.
const labelHeightDenom = 3

// synthesizedLabel is a top-fraction crop of another associated image (BIF's
// Label_Image, surfaced as "overview"), exposed with Type() == "label". It
// mirrors NDPI's macro-crop label so consumers can ask both formats "where is
// the label" (GH #19) — but BIF's overview can be uncompressed multi-strip RGB
// (DP 200) or tiled abbreviated JPEG (legacy OS-1), so the crop is pixel-domain
// (decode the source, keep the top rows) rather than NDPI's JPEG-domain crop.
// It is synthesized: no backing IFD, so Encoding / TIFFTags / IFDOffset report
// false, and Compression() is None (the cropped raster is uncompressed).
type synthesizedLabel struct {
	source opentile.AssociatedImage // the overview (Label_Image)
	size   opentile.Size            // {source.W, source.H / labelHeightDenom}
}

// newSynthesizedLabel builds the top-1/labelHeightDenom crop of source. Returns
// nil if the crop would be empty (degenerate source height).
func newSynthesizedLabel(source opentile.AssociatedImage) *synthesizedLabel {
	h := source.Size().H / labelHeightDenom
	if h < 1 {
		return nil
	}
	return &synthesizedLabel{source: source, size: opentile.Size{W: source.Size().W, H: h}}
}

func (l *synthesizedLabel) Type() opentile.AssociatedType     { return opentile.AssociatedLabel }
func (l *synthesizedLabel) Size() opentile.Size               { return l.size }
func (l *synthesizedLabel) Compression() opentile.Compression { return opentile.CompressionNone }

// Decode returns the decoded label pixels — the top 1/labelHeightDenom of the
// decoded overview. The crop height is derived from the decoded image, so it
// stays a true top-third even when the overview is decoded at WithScale > 1.
func (l *synthesizedLabel) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	full, err := l.source.Decode(opts)
	if err != nil {
		return nil, fmt.Errorf("bif: decode source for synthesized label: %w", err)
	}
	h := full.Height / labelHeightDenom
	if h < 1 {
		h = 1
	}
	return cropTopRows(full, h), nil
}

// Bytes returns the cropped label as tightly-packed RGB (Compression() == None).
func (l *synthesizedLabel) Bytes() ([]byte, error) {
	img, err := l.Decode(decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
	if err != nil {
		return nil, err
	}
	return img.Pix, nil
}

// Encoding is unsupported — the label is synthesized, with no source strip form.
func (l *synthesizedLabel) Encoding() (opentile.AssociatedEncoding, bool) {
	return opentile.AssociatedEncoding{}, false
}

// TIFFTags / IFDOffset report false — synthesized, no backing IFD.
func (l *synthesizedLabel) TIFFTags() (opentile.TIFFTags, bool) { return nil, false }
func (l *synthesizedLabel) IFDOffset() (int64, bool)            { return 0, false }

// cropTopRows returns a tightly-packed copy of the top h rows of src.
func cropTopRows(src *decoder.Image, h int) *decoder.Image {
	if h > src.Height {
		h = src.Height
	}
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	dst := decoder.NewImageFormat(src.Width, h, src.Format)
	for y := 0; y < h; y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+src.Width*bpp], src.Pix[y*src.Stride:y*src.Stride+src.Width*bpp])
	}
	return dst
}
