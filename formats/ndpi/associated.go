package ndpi

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/jpegturbo"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// overviewImage is an NDPI "Macro" page exposed as an AssociatedImage with
// Type() == "overview". Its Bytes() passes through the raw JPEG payload
// without modification (no cgo required).
type overviewImage struct {
	size        opentile.Size
	compression opentile.Compression
	offset      uint64
	length      uint64
	reader      io.ReaderAt
}

func newOverviewImage(p *tiff.Page, r io.ReaderAt) (*overviewImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ndpi: overview ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ndpi: overview ImageLength missing")
	}
	// NDPI Macro pages use StripOffsets (273) / StripByteCounts (279) rather
	// than TileOffsets (324) / TileByteCounts (325).
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("ndpi: overview offsets: %w", err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("ndpi: overview counts: %w", err)
	}
	if len(offsets) != 1 || len(counts) != 1 {
		return nil, fmt.Errorf("ndpi: overview expected 1 strip, got offsets=%d counts=%d", len(offsets), len(counts))
	}
	return &overviewImage{
		size:        opentile.Size{W: int(iw), H: int(il)},
		compression: opentile.CompressionJPEG,
		offset:      offsets[0],
		length:      counts[0],
		reader:      r,
	}, nil
}

func (o *overviewImage) Type() string                      { return "overview" }
func (o *overviewImage) Size() opentile.Size               { return o.size }
func (o *overviewImage) Compression() opentile.Compression { return o.compression }

// Decode returns the decoded overview pixels via the registered JPEG decoder (GH #20).
func (o *overviewImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	data, err := o.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(o.compression, data, opts)
}

func (o *overviewImage) Bytes() ([]byte, error) {
	buf := make([]byte, o.length)
	if err := tiff.ReadAtFull(o.reader, buf, int64(o.offset)); err != nil {
		return nil, fmt.Errorf("ndpi: read overview: %w", err)
	}
	return buf, nil
}

// labelImage is the cropped left portion of the macro image, exposed with
// Type() == "label". Upstream default crop is 0.0 → 0.3 of macro width
// (caller-configurable at construction). Requires cgo for the crop.
type labelImage struct {
	overview *overviewImage
	cropFrom int // left pixel offset in source (MCU-aligned)
	cropTo   int // right pixel offset in source (exclusive, MCU-aligned)
	cropH    int // full image height (may be MCU-ragged; libjpeg-turbo handles partial-MCU row at image edge)
}

// newLabelImage returns a labelImage whose Bytes() crops the overview to
// [0, crop * overview.Width) horizontally, snapped down to the nearest MCU
// boundary. mcuW is the MCU width of the overview's JPEG — derive it via
// jpeg.MCUSizeOf on the overview bytes (16 for YCbCr 4:2:0; 8 for the 4:4:4
// or 4:2:2-horizontal case Hamamatsu actually uses on macro pages).
//
// Width is MCU-rounded the way Python opentile's _calculate_crop is
// (int(W * crop / mcuW) * mcuW). Height passes through the FULL image
// height — matching Python's `_crop_parameters[3] = page.shape[0]` at
// `opentile/formats/ndpi/ndpi_image.py:144`. libjpeg-turbo with
// TJXOPT_PERFECT accepts the partial last MCU row when the crop ends
// exactly at the image edge, which is what's happening here:
// PyTurboJPEG's __need_fill_background gate (turbojpeg.py:839-863)
// returns False because `crop_y + crop_h == image_h` (not `>`), so
// Python takes the plain-crop path, not the CUSTOMFILTER path.
//
// Pre-v0.4 we rounded the height down to a whole-MCU multiple, dropping
// the last partial-MCU row and producing a label one MCU shorter than
// Python's. Closes L17.
func newLabelImage(overview *overviewImage, crop float64, mcuW int) *labelImage {
	pixelTo := int(float64(overview.size.W) * crop)
	pixelTo = (pixelTo / mcuW) * mcuW
	if pixelTo <= 0 {
		pixelTo = mcuW
	}
	return &labelImage{
		overview: overview,
		cropFrom: 0,
		cropTo:   pixelTo,
		cropH:    overview.size.H,
	}
}

func (l *labelImage) Type() string                      { return "label" }
func (l *labelImage) Size() opentile.Size               { return opentile.Size{W: l.cropTo - l.cropFrom, H: l.cropH} }
func (l *labelImage) Compression() opentile.Compression { return l.overview.compression }

// Decode returns the decoded synthesized-label pixels (a JPEG crop of the
// macro) via the registered JPEG decoder (GH #20).
func (l *labelImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	data, err := l.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(l.overview.compression, data, opts)
}

func (l *labelImage) Bytes() ([]byte, error) {
	src, err := l.overview.Bytes()
	if err != nil {
		return nil, err
	}
	return jpegturbo.Crop(src, jpegturbo.Region{
		X: l.cropFrom, Y: 0, Width: l.cropTo - l.cropFrom, Height: l.cropH,
	})
}
