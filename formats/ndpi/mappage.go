package ndpi

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/tiff"
	"github.com/wsilabs/opentile-go/internal/tiffstrip"
)

// mapPage is an NDPI "Map" page (Magnification tag 65421 == -2.0)
// exposed as an AssociatedImage with Type() == "map". Bytes() passes
// the raw strip payload through verbatim — same shape as overviewImage
// but for the Map page type.
//
// Map pages on every Hamamatsu fixture we've seen are 8-bit grayscale
// uncompressed raster (TIFF Compression tag = 1, Photometric = 1
// MinIsBlack, BitsPerSample = 8, SamplesPerPixel = 1). The strip's
// raw bytes are width*height pixel values in row-major order. This
// differs from Macro pages which are JPEG; we read the Compression
// tag at construction time to surface the right value to consumers.
// Downstream consumers decoding the Map page need to expect a single-
// channel image, not RGB.
//
// This is a deliberate Go-side extension. Python opentile 0.20.0 does
// not surface Map pages — its `NdpiTiler` returns False from every
// non-overview series predicate, so Map pages are silently dropped.
// tifffile (which opentile sits on top of) does classify them via
// `series.name == 'Map'` in `_series_ndpi`, so we're closing an
// opentile-level scope decision rather than inventing a new category.
// Parallels the existing v0.2 NDPI label synthesis (L14).
type mapPage struct {
	size        opentile.Size
	compression opentile.Compression
	offset      uint64
	length      uint64
	samples     int
	photometric int
	reader      io.ReaderAt
}

func newMapPage(p *tiff.Page, r io.ReaderAt) (*mapPage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ndpi: map ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ndpi: map ImageLength missing")
	}
	// NDPI Map pages, like Macro pages, use StripOffsets (273) /
	// StripByteCounts (279). Map data is single-strip in every fixture
	// we've seen.
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("ndpi: map offsets: %w", err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("ndpi: map counts: %w", err)
	}
	if len(offsets) != 1 || len(counts) != 1 {
		return nil, fmt.Errorf("ndpi: map expected 1 strip, got offsets=%d counts=%d", len(offsets), len(counts))
	}
	// Compression varies — the Hamamatsu fixtures we have ship Map
	// pages uncompressed (tag 1) but the format doesn't strictly
	// require it. Read the tag and map; unknown codes fall through
	// to CompressionUnknown rather than asserting JPEG like overview /
	// stripped / oneframe do.
	compTag, _ := p.Compression()
	spp, _ := p.SamplesPerPixel()
	photo, _ := p.Photometric()
	samples := int(spp)
	if samples == 0 {
		samples = 1
	}
	return &mapPage{
		size:        opentile.Size{W: int(iw), H: int(il)},
		compression: ndpiCompressionToOpentile(compTag),
		offset:      offsets[0],
		length:      counts[0],
		samples:     samples,
		photometric: int(photo),
		reader:      r,
	}, nil
}

// ndpiCompressionToOpentile maps TIFF tag 259 numeric values to the
// opentile.Compression enum, scoped for NDPI Map pages. NDPI's pyramid
// levels and Macro pages are unconditionally JPEG, so this only
// handles the codes Map pages have been seen with — uncompressed (1)
// and JPEG (7). Additions land here as new fixtures surface them.
func ndpiCompressionToOpentile(tiffCode uint32) opentile.Compression {
	switch tiffCode {
	case 1:
		return opentile.CompressionNone
	case 7:
		return opentile.CompressionJPEG
	}
	return opentile.CompressionUnknown
}

func (m *mapPage) Type() string                      { return "map" }
func (m *mapPage) Size() opentile.Size               { return m.size }
func (m *mapPage) Compression() opentile.Compression { return m.compression }

// Decode returns the decoded map pixels (GH #20). JPEG maps decode via the
// registry; uncompressed maps (the common Hamamatsu case) decode via the
// strip path with sample interpretation.
func (m *mapPage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if m.compression == opentile.CompressionJPEG {
		data, err := m.Bytes()
		if err != nil {
			return nil, err
		}
		return assocdecode.ViaCodec(opentile.CompressionJPEG, data, opts)
	}
	buf := make([]byte, m.length)
	if err := tiff.ReadAtFull(m.reader, buf, int64(m.offset)); err != nil {
		return nil, fmt.Errorf("ndpi: read map strip: %w", err)
	}
	return tiffstrip.Decode(tiffstrip.Params{
		Width:       m.size.W,
		Height:      m.size.H,
		Samples:     m.samples,
		Photometric: m.photometric,
		Compression: int(opentile.CompressionToTIFFTag(m.compression)),
		Strips:      [][]byte{buf},
	}, opts)
}

func (m *mapPage) Bytes() ([]byte, error) {
	buf := make([]byte, m.length)
	if err := tiff.ReadAtFull(m.reader, buf, int64(m.offset)); err != nil {
		return nil, fmt.Errorf("ndpi: read map: %w", err)
	}
	return buf, nil
}
