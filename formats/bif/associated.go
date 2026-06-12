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
	imageType   string
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
}

func (a *associatedImage) Type() string                      { return a.imageType }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

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
func newAssociatedImage(imageType string, p *tiff.Page, r io.ReaderAt) (*associatedImage, error) {
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
func typeFromIFDRole(role ifdRole) string {
	switch role {
	case ifdRoleLabel:
		return "overview"
	case ifdRoleProbability:
		return "probability"
	case ifdRoleThumbnail:
		return "thumbnail"
	default:
		return ""
	}
}
