// Package oneframe provides shared infrastructure for reading
// "single big JPEG, virtualised into tile cells" pyramid pages — used
// by NDPI's non-tiled levels (formats/ndpi/oneframe.go pre-v0.6) and
// OME-TIFF's reduced-resolution levels (formats/ometiff/oneframe.go in
// v0.6).
//
// The shared algorithm (single-strip JPEG → SOF padded to MCU
// boundaries → frame extended to tile-aligned size with OOB fill →
// per-tile MCU-aligned crop via libjpeg-turbo) lives here; format
// packages compose it via Options to provide level-specific metadata
// (Index, PyramidIdx, MPP) and optionally override page-derived
// dimensions for corrected-size scenarios.
package oneframe

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"sync"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/jpegturbo"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Options captures the per-level parameters a format package
// supplies when constructing a oneframe.Image.
type Options struct {
	// Index is the level index returned by Image.Index().
	Index int
	// PyramidIdx is the pyramid index (log2 of size reduction)
	// returned by Image.PyramidIndex().
	PyramidIdx int
	// MPP is the microns-per-pixel returned by Image.MPP().
	MPP opentile.SizeMm
	// Size overrides the page's on-disk ImageWidth/ImageLength when
	// non-zero. Callers (e.g. OME with corrected dims) supply this
	// when the on-disk values are placeholders. NDPI passes a zero
	// Size and relies on the page's tag values.
	Size opentile.Size
	// TileSize is the virtualised tile dims for output. Required.
	TileSize opentile.Size
	// FirstStripOnly: when true, a multi-strip page takes strip 0 and
	// ignores the rest. Used by OME-TIFF where multi-plane pages
	// (PlanarConfiguration=2) carry rowsperstrip*samplesperpixel
	// strips per page; Python opentile silently consumes only strip 0
	// (which is plane 0 row 0), and we mirror that for byte parity.
	// NDPI leaves this false and errors on multi-strip pages.
	FirstStripOnly bool
}

// Image is an opentile.Level implementation backed by a single-strip
// JPEG-compressed page. Output tiles are produced via lossless
// MCU-aligned crop of an extended-frame JPEG (cached per level).
type Image struct {
	index       int
	size        opentile.Size
	tileSize    opentile.Size
	grid        opentile.Size
	compression opentile.Compression
	mpp         opentile.SizeMm
	pyrIndex    int

	firstStripOnly bool

	// paddedJPEG: full-level JPEG with SOF rewritten to MCU boundaries.
	paddedJPEGOnce sync.Once
	paddedJPEG     []byte
	paddedJPEGErr  error
	mcuW, mcuH     int

	// extendedFrame: padded JPEG further widened to a tile-aligned size,
	// with the OOB region filled via the DCT callback. Every Tile() crop
	// uses this as the source so per-tile hot path is plain crop.
	extendedOnce sync.Once
	extendedJPEG []byte
	extendedSize opentile.Size
	extendedErr  error

	reader io.ReaderAt
	page   *tiff.Page
}

// New constructs a oneframe.Image over a single-strip JPEG page p.
// Reads ImageWidth / ImageLength from p when opts.Size is zero;
// otherwise uses opts.Size verbatim (for OME corrected dims).
//
// Errors when the page is missing dimension tags, opts.TileSize is
// zero in either axis, or the page can't be reached for read at
// construction time. Tile-payload reads happen lazily on first Tile()
// call.
func New(p *tiff.Page, r io.ReaderAt, opts Options) (*Image, error) {
	if opts.TileSize.W <= 0 || opts.TileSize.H <= 0 {
		return nil, fmt.Errorf("oneframe: tile size must be positive (got %v)", opts.TileSize)
	}
	size := opts.Size
	if size.W == 0 || size.H == 0 {
		iw, ok := p.ImageWidth()
		if !ok {
			return nil, fmt.Errorf("oneframe: ImageWidth missing")
		}
		il, ok := p.ImageLength()
		if !ok {
			return nil, fmt.Errorf("oneframe: ImageLength missing")
		}
		size = opentile.Size{W: int(iw), H: int(il)}
	}
	gridW := (size.W + opts.TileSize.W - 1) / opts.TileSize.W
	gridH := (size.H + opts.TileSize.H - 1) / opts.TileSize.H
	return &Image{
		index:          opts.Index,
		size:           size,
		tileSize:       opts.TileSize,
		grid:           opentile.Size{W: gridW, H: gridH},
		compression:    opentile.CompressionJPEG,
		mpp:            opts.MPP,
		pyrIndex:       opts.PyramidIdx,
		firstStripOnly: opts.FirstStripOnly,
		reader:         r,
		page:           p,
	}, nil
}

// opentile.Level accessors.
func (l *Image) Index() int                        { return l.index }
func (l *Image) PyramidIndex() int                 { return l.pyrIndex }
func (l *Image) Size() opentile.Size               { return l.size }
func (l *Image) TileSize() opentile.Size           { return l.tileSize }
func (l *Image) Grid() opentile.Size               { return l.grid }
func (l *Image) Compression() opentile.Compression { return l.compression }
func (l *Image) MPP() opentile.SizeMm              { return l.mpp }
func (l *Image) FocalPlane() float64               { return 0 }
func (l *Image) TileOverlap() image.Point          { return image.Point{} }

// TileAt is the multi-dim entry point. NDPI/OME OneFrame levels
// are 2D-only; non-zero Z/C/T yields ErrDimensionUnavailable.
func (l *Image) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

// TileMaxSize returns a generous upper bound for compressed tile
// output. OneFrame's libjpeg-turbo crop output size depends on
// entropy coding; we return tileSize.W * tileSize.H as a worst-case
// bound (one byte per pixel) consistent with NDPI striped's
// approach.
func (l *Image) TileMaxSize() int { return l.tileSize.W * l.tileSize.H }

// warm pre-faults the page-cache pages backing the page's strips.
// OneFrame stores the level as a single (or per-channel) TIFF strip;
// touching all bytes of every strip is what warmup means here.
func (l *Image) warm() error {
	offs, err := l.page.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return err
	}
	counts, err := l.page.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return err
	}
	if len(offs) != len(counts) {
		// Defensive — TIFF rules require parity but we don't want to
		// panic during a hint operation.
		n := len(offs)
		if len(counts) < n {
			n = len(counts)
		}
		offs, counts = offs[:n], counts[:n]
	}
	for i, off := range offs {
		if err := tiff.TouchPages(l.reader, int64(off), int64(counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// TileInto writes the tile bytes into dst. The OneFrame internal
// scratch (extended frame + libjpeg-turbo crop output) still
// allocates; dst receives the final copy.
func (l *Image) TileInto(x, y int, dst []byte) (int, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return 0, err
	}
	if len(dst) < len(b) {
		return 0, io.ErrShortBuffer
	}
	return copy(dst, b), nil
}

// Tile returns the JPEG bytes for the tile at (x, y). Out-of-bounds
// coordinates yield ErrTileOutOfBounds (wrapped in opentile.TileError).
// All in-bounds reads share the lazily-built extended frame; the
// per-call work is one libjpeg-turbo Crop.
func (l *Image) Tile(x, y int) ([]byte, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	frame, err := l.getExtendedFrame()
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	region := jpegturbo.Region{X: x * l.tileSize.W, Y: y * l.tileSize.H, Width: l.tileSize.W, Height: l.tileSize.H}
	out, err := jpegturbo.Crop(frame, region)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return out, nil
}

// TileReader returns a streaming reader for the tile at (x, y). Always
// materialises the bytes via Tile() — the JPEG-domain crop forces a
// full materialise before anything can stream.
func (l *Image) TileReader(x, y int) (io.ReadCloser, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// Tiles iterates every tile position in row-major order.
func (l *Image) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		for y := 0; y < l.grid.H; y++ {
			for x := 0; x < l.grid.W; x++ {
				if err := ctx.Err(); err != nil {
					yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Err: err})
					return
				}
				b, err := l.Tile(x, y)
				if !yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}

// getPaddedJPEG reads the level's JPEG payload once and returns a slice
// where the SOF dimensions are rounded up to MCU boundaries (safe for
// tjTransform's TJXOPT_PERFECT). Cached for the level's lifetime.
func (l *Image) getPaddedJPEG() ([]byte, error) {
	l.paddedJPEGOnce.Do(func() {
		l.paddedJPEG, l.paddedJPEGErr = l.buildPaddedJPEG()
	})
	return l.paddedJPEG, l.paddedJPEGErr
}

func (l *Image) buildPaddedJPEG() ([]byte, error) {
	// Single-strip JPEG pages use StripOffsets / StripByteCounts.
	offsets, err := l.page.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("oneframe: page missing StripOffsets: %w", err)
	}
	counts, err := l.page.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("oneframe: page missing StripByteCounts: %w", err)
	}
	if !l.firstStripOnly && (len(offsets) != 1 || len(counts) != 1) {
		return nil, fmt.Errorf("oneframe: page expected 1 strip, got %d offsets / %d counts", len(offsets), len(counts))
	}
	if len(offsets) == 0 || len(counts) == 0 {
		return nil, fmt.Errorf("oneframe: page has no strips")
	}
	buf := make([]byte, counts[0])
	if err := tiff.ReadAtFull(l.reader, buf, int64(offsets[0])); err != nil {
		return nil, fmt.Errorf("oneframe: read JPEG: %w", err)
	}
	// Determine MCU size from SOF inside buf.
	var sof *jpeg.SOF
	for seg, err := range jpeg.Scan(bytes.NewReader(buf)) {
		if err != nil {
			return nil, fmt.Errorf("%w: %w", opentile.ErrBadJPEGBitstream, err)
		}
		if seg.Marker == jpeg.SOF0 {
			sof, err = jpeg.ParseSOF(seg.Payload)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", opentile.ErrBadJPEGBitstream, err)
			}
			break
		}
	}
	if sof == nil {
		return nil, fmt.Errorf("%w: SOF not found in oneframe page", opentile.ErrBadJPEGBitstream)
	}
	mcuW, mcuH := sof.MCUSize()
	l.mcuW, l.mcuH = mcuW, mcuH
	paddedW := roundUp(l.size.W, mcuW)
	paddedH := roundUp(l.size.H, mcuH)
	if paddedW > 0xFFFF || paddedH > 0xFFFF {
		return nil, fmt.Errorf("%w: oneframe page %dx%d exceeds SOF uint16 range", opentile.ErrBadJPEGBitstream, paddedW, paddedH)
	}
	if paddedW == l.size.W && paddedH == l.size.H {
		return buf, nil
	}
	rewrote, err := jpeg.ReplaceSOFDimensions(buf, uint16(paddedW), uint16(paddedH))
	if err != nil {
		return nil, fmt.Errorf("oneframe: pad SOF: %w", err)
	}
	return rewrote, nil
}

// getExtendedFrame produces the tile-aligned "extended frame" from
// which every output tile is cropped in-bounds. Mirrors upstream
// opentile's NdpiOneFrameImage._read_extended_frame:
//
//  1. Pad the raw level JPEG's SOF up to MCU boundaries
//     (getPaddedJPEG) so libjpeg-turbo accepts it.
//  2. Widen the frame again to a tile-aligned size via
//     CropWithBackground with X=Y=0 and width/height =
//     ceil(size/tile)*tile. The CUSTOMFILTER fills the newly added
//     blocks with a background color.
//
// After this, every Tile(x, y) crop lies wholly inside extendedFrame
// and uses plain Crop — the OOB fill happens once per level, not per
// tile.
func (l *Image) getExtendedFrame() ([]byte, error) {
	l.extendedOnce.Do(func() {
		padded, err := l.getPaddedJPEG()
		if err != nil {
			l.extendedErr = err
			return
		}
		extW := roundUp(l.size.W, l.tileSize.W)
		extH := roundUp(l.size.H, l.tileSize.H)
		if extW > 0xFFFF || extH > 0xFFFF {
			l.extendedErr = fmt.Errorf("%w: extended frame %dx%d exceeds SOF uint16 range",
				opentile.ErrBadJPEGBitstream, extW, extH)
			return
		}
		paddedW := roundUp(l.size.W, l.mcuW)
		paddedH := roundUp(l.size.H, l.mcuH)
		if paddedW == extW && paddedH == extH {
			l.extendedJPEG = padded
			l.extendedSize = opentile.Size{W: extW, H: extH}
			return
		}
		frame, err := jpegturbo.CropWithBackground(padded, jpegturbo.Region{
			X: 0, Y: 0, Width: extW, Height: extH,
		})
		if err != nil {
			l.extendedErr = fmt.Errorf("oneframe: extend frame: %w", err)
			return
		}
		l.extendedJPEG = frame
		l.extendedSize = opentile.Size{W: extW, H: extH}
	})
	if l.extendedErr != nil {
		return nil, l.extendedErr
	}
	return l.extendedJPEG, nil
}

func roundUp(n, to int) int {
	if n%to == 0 {
		return n
	}
	return n + (to - n%to)
}
