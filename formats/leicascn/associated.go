package leicascn

import (
	"errors"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// errUnsupportedAuxiliary is returned by [newAssociatedImage] when
// the auxiliary's lowest-resolution IFD doesn't fit our v0.11
// reader path. Currently only one shape is unsupported:
//
//   - Auxiliary lowest-res IFD spans more than one tile
//     (multi-tile concat would require a JPEG decode+re-encode pass
//     which we don't implement on the auxiliary path).
//
// Tiler.Open() filters these out silently — the IFD is recognised
// but the auxiliary is not exposed via Associated().
var errUnsupportedAuxiliary = errors.New("leicascn: auxiliary image shape unsupported")

// associatedImage is the Leica SCN AssociatedImage. Bytes are read
// eagerly at construction time (typical < 50 KB for a 101×291 JPEG
// tile, well under the 5 MB cap). All SCN auxiliaries get
// Type() == "overview" (v0.15: aligned with DICOM ImageType +
// Python opentile + 5 sibling format readers; was "macro" pre-v0.15
// per Q8). Format-specific metadata (illumination source, objective
// magnification) is exposed via leicascn.MetadataOf when consumers
// need to disambiguate brightfield-macro vs fluorescence-macro.
type associatedImage struct {
	size        opentile.Size
	compression opentile.Compression
	bytes       []byte
}

func (a *associatedImage) Type() opentile.AssociatedType     { return opentile.AssociatedOverview }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }

// Decode returns the decoded associated-image pixels via the registered
// codec decoder (GH #20).
func (a *associatedImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	data, err := a.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(a.Compression(), data, opts)
}
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.bytes))
	copy(out, a.bytes)
	return out, nil
}

// newAssociatedImage builds an associatedImage from an auxiliary
// <image> element by selecting its lowest-resolution Dimension
// (smallest area, equivalently highest R), reading the (single) tile
// from that IFD, and splicing the IFD's JPEGTables for a self-
// contained JPEG. Returns errUnsupportedAuxiliary if the lowest-res
// IFD is multi-tile (no fixture exhibits this; preserved for safety).
func newAssociatedImage(img Image, file *tiff.File, r io.ReaderAt) (*associatedImage, error) {
	if len(img.Dimensions) == 0 {
		return nil, fmt.Errorf("leicascn: auxiliary %q has no dimensions", img.Name)
	}

	// Pick the lowest-resolution dimension (smallest area). For our
	// 3 fixtures this is always r=2 at 101×291. Equivalent to
	// max(R), but selecting by area is safer if non-monotonic R
	// sequences ever appear.
	lo := img.Dimensions[0]
	for _, d := range img.Dimensions[1:] {
		if int64(d.SizeX)*int64(d.SizeY) < int64(lo.SizeX)*int64(lo.SizeY) {
			lo = d
		}
	}

	pages := file.Pages()
	if lo.IFD < 0 || lo.IFD >= len(pages) {
		return nil, fmt.Errorf("leicascn: auxiliary %q dimension IFD %d out of range",
			img.Name, lo.IFD)
	}
	page := pages[lo.IFD]

	tw, _ := page.TileWidth()
	tl, _ := page.TileLength()
	if tw == 0 || tl == 0 {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d not tiled",
			img.Name, lo.IFD)
	}
	gx, gy, err := page.TileGrid()
	if err != nil {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d TileGrid: %w",
			img.Name, lo.IFD, err)
	}
	if gx*gy != 1 {
		// Multi-tile auxiliary: out of scope for v0.11; silently
		// dropped by the Tiler builder (caller filters this error).
		return nil, fmt.Errorf("%w: auxiliary %q lowest-res IFD %d is %dx%d tiles",
			errUnsupportedAuxiliary, img.Name, lo.IFD, gx, gy)
	}

	offsets, err := page.TileOffsets64()
	if err != nil {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d TileOffsets: %w",
			img.Name, lo.IFD, err)
	}
	counts, err := page.TileByteCounts64()
	if err != nil {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d TileByteCounts: %w",
			img.Name, lo.IFD, err)
	}
	if len(offsets) != 1 || len(counts) != 1 {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d expected 1 tile, got %d offsets / %d counts",
			img.Name, lo.IFD, len(offsets), len(counts))
	}

	// Read the single tile.
	tileBytes := make([]byte, counts[0])
	if err := tiff.ReadAtFull(r, tileBytes, int64(offsets[0])); err != nil {
		return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d ReadAt: %w",
			img.Name, lo.IFD, err)
	}

	// Splice JPEGTables for a self-contained JPEG. SCN tiles are
	// standard YCbCr JPEG (no Adobe APP14 marker); use InsertTables
	// (no APP14) — same path generictiff uses.
	out := tileBytes
	if tables, ok := page.JPEGTables(); ok && len(tables) > 0 {
		spliced, err := jpeg.InsertTables(tileBytes, tables)
		if err != nil {
			return nil, fmt.Errorf("leicascn: auxiliary %q IFD %d JPEG splice: %w",
				img.Name, lo.IFD, err)
		}
		out = spliced
	}

	return &associatedImage{
		size:        opentile.Size{W: int(lo.SizeX), H: int(lo.SizeY)},
		compression: opentile.CompressionJPEG, // SCN auxiliaries are always JPEG (verified across all 3 fixtures)
		bytes:       out,
	}, nil
}
