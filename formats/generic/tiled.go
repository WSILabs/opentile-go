package generic

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// tiledImage is the generic-TIFF Level implementation. Mirrors
// SVS/Philips/OME/BIF tiled-level shape; key differences:
//
//   - No APP14 splice. Generic TIFFs aren't Aperio; JPEG tiles encode
//     standard YCbCr (or RGB without the Adobe colorspace marker).
//     Splice prefix is jpeg.BuildSplicePrefix(jpegTables, false).
//   - Compression mapping covers JPEG, JP2K, LZW, Deflate, None
//     per the v0.10 spec §4.6 whitelist.
//   - Generic doesn't carry MPP from any standard TIFF tag at the
//     level-here scope; MPP() returns zero. Resolution-derived MPP
//     is exposed at Tiler.Metadata() / generic.MetadataOf level (T9).
type tiledImage struct {
	index       int
	pyrIndex    int
	size        opentile.Size
	tileSize    opentile.Size
	grid        opentile.Size
	compression opentile.Compression

	offsets    []uint64
	counts     []uint64
	jpegTables []byte // TIFF tag 347; nil for non-JPEG levels or JPEG-without-shared-tables
	reader     io.ReaderAt

	// maxTileSize is the cached upper bound for Tile/TileInto output.
	// max(counts) + len(splicePrefix) on the JPEG-splice path; just
	// max(counts) elsewhere.
	maxTileSize int

	// splicePrefix is the constant per-level payload (DQT/DHT) inserted
	// between SOI and SOS on JPEG tiles. nil when no splice needed
	// (no JPEGTables tag, or non-JPEG compression).
	splicePrefix []byte
}

// newTiledImage constructs a generic-TIFF level from a tiff.Page
// the validator has already classified as a pyramid IFD. pyrIndex
// is the level's position in the validator-ordered Pyramid slice
// (0 = baseline / largest).
func newTiledImage(index, pyrIndex int, p *tiff.Page, r io.ReaderAt) (*tiledImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("generic level=%d: ImageWidth missing", index)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("generic level=%d: ImageLength missing", index)
	}
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("generic level=%d: TileWidth missing or zero", index)
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("generic level=%d: TileLength missing or zero", index)
	}
	gx, gy, err := p.TileGrid()
	if err != nil {
		return nil, fmt.Errorf("generic level=%d: TileGrid: %w", index, err)
	}
	offsets, err := p.TileOffsets64()
	if err != nil {
		return nil, fmt.Errorf("generic level=%d: TileOffsets: %w", index, err)
	}
	counts, err := p.TileByteCounts64()
	if err != nil {
		return nil, fmt.Errorf("generic level=%d: TileByteCounts: %w", index, err)
	}
	if len(offsets) != len(counts) || len(offsets) != gx*gy {
		return nil, fmt.Errorf("generic level=%d: tile-table mismatch: offsets=%d counts=%d grid=%dx%d",
			index, len(offsets), len(counts), gx, gy)
	}

	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	// JPEGTables: cached once per level. Only JPEG-compressed pages
	// can have shared tables; everything else carries self-contained
	// tile bytes.
	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	var maxCount uint64
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	maxTileSize := int(maxCount)

	var splicePrefix []byte
	if ocomp == opentile.CompressionJPEG && len(jpegTables) > 0 {
		var err error
		splicePrefix, err = jpeg.BuildSplicePrefix(jpegTables, false)
		if err != nil {
			return nil, fmt.Errorf("generic level=%d: build splice prefix: %w", index, err)
		}
		maxTileSize += len(splicePrefix)
	}

	return &tiledImage{
		index:        index,
		pyrIndex:     pyrIndex,
		size:         opentile.Size{W: int(iw), H: int(il)},
		tileSize:     opentile.Size{W: int(tw), H: int(tl)},
		grid:         opentile.Size{W: gx, H: gy},
		compression:  ocomp,
		offsets:      offsets,
		counts:       counts,
		jpegTables:   jpegTables,
		reader:       r,
		maxTileSize:  maxTileSize,
		splicePrefix: splicePrefix,
	}, nil
}

func (l *tiledImage) Index() int                        { return l.index }
func (l *tiledImage) PyramidIndex() int                 { return l.pyrIndex }
func (l *tiledImage) Size() opentile.Size               { return l.size }
func (l *tiledImage) TileSize() opentile.Size           { return l.tileSize }
func (l *tiledImage) Grid() opentile.Size               { return l.grid }
func (l *tiledImage) Compression() opentile.Compression { return l.compression }

// MPP returns zero for generic-TIFF levels. Resolution-derived MPP
// is available via Tiler.Metadata() / generic.MetadataOf when the
// source TIFF carries XResolution / YResolution / ResolutionUnit
// tags; per-level MPP isn't a distinct concept in generic TIFFs.
func (l *tiledImage) MPP() opentile.SizeMm     { return opentile.SizeMm{} }
func (l *tiledImage) FocalPlane() float64      { return 0 }
func (l *tiledImage) TileOverlap() image.Point { return image.Point{} }

func (l *tiledImage) TileMaxSize() int { return l.maxTileSize }

// indexOf computes the row-major tile index for (x, y) and validates
// the entry. Out-of-grid coords yield ErrTileOutOfBounds; a
// zero-length entry yields ErrCorruptTile.
func (l *tiledImage) indexOf(x, y int) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	if l.counts[idx] == 0 {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	return idx, nil
}

// Tile returns the tile bytes at (x, y). Same shape as the SVS path
// but uses jpeg.InsertTables (no APP14) on the splice path. JPEG
// tiles without shared JPEGTables are passthrough; JP2K / LZW /
// Deflate / None all pass through verbatim.
func (l *tiledImage) Tile(x, y int) ([]byte, error) {
	idx, err := l.indexOf(x, y)
	if err != nil {
		return nil, err
	}
	length := l.counts[idx]
	off := int64(l.offsets[idx])
	buf := make([]byte, length)
	if err := tiff.ReadAtFull(l.reader, buf, off); err != nil {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		out, err := jpeg.InsertTables(buf, l.jpegTables)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		return out, nil
	}
	return buf, nil
}

// TileInto writes tile bytes directly into dst with zero internal
// allocation. Splice path uses jpeg.InsertPrefixInPlace (the v0.9
// in-place splicer); no-splice path is a single ReadAt into dst.
func (l *tiledImage) TileInto(x, y int, dst []byte) (int, error) {
	idx, err := l.indexOf(x, y)
	if err != nil {
		return 0, err
	}
	length := int(l.counts[idx])
	off := int64(l.offsets[idx])
	if l.splicePrefix == nil {
		if len(dst) < length {
			return 0, io.ErrShortBuffer
		}
		if err := tiff.ReadAtFull(l.reader, dst[:length], off); err != nil {
			return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		return length, nil
	}
	prefixLen := len(l.splicePrefix)
	if len(dst) < length+prefixLen {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[prefixLen:prefixLen+length], off); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	n, err := jpeg.InsertPrefixInPlace(dst, length, l.splicePrefix)
	if err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileAt is the multi-dim entry point. Generic-TIFF is 2D-only;
// non-zero Z/C/T yields ErrDimensionUnavailable.
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

// TileReader returns an io.ReadCloser over the tile bytes. For the
// no-splice path (JP2K / non-JPEG), backed by io.SectionReader for
// zero-copy streaming. For JPEG-splice tiles, materialises through
// Tile() and wraps a bytes.Reader (the splice can't be expressed as
// a contiguous file region).
func (l *tiledImage) TileReader(x, y int) (io.ReadCloser, error) {
	idx, err := l.indexOf(x, y)
	if err != nil {
		return nil, err
	}
	if l.splicePrefix != nil {
		b, err := l.Tile(x, y)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	length := l.counts[idx]
	off := int64(l.offsets[idx])
	return io.NopCloser(io.NewSectionReader(l.reader, off, int64(length))), nil
}

// Tiles iterates all tiles in row-major order.
func (l *tiledImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
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

// warm pre-faults the page-cache pages backing every tile on this
// level, called via Tiler.WarmLevel.
func (l *tiledImage) warm() error {
	for i, off := range l.offsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// tiffCompressionToOpentile maps the TIFF tag 259 value to opentile's
// Compression enum. v0.10 whitelist:
//
//	1     None       → CompressionNone
//	5     LZW        → CompressionLZW
//	7     JPEG       → CompressionJPEG
//	8     Deflate    → CompressionDeflate (v0.10 addition)
//	32946 AdobeDeflate → CompressionDeflate (same payload as 8)
//	33003 JP2K       → CompressionJP2K
//
// Other values map to CompressionUnknown — those IFDs would fail the
// validator's compression whitelist (spec §4.6) and not become
// pyramid candidates in the first place.
func tiffCompressionToOpentile(comp uint32) opentile.Compression {
	switch comp {
	case 1:
		return opentile.CompressionNone
	case 5:
		return opentile.CompressionLZW
	case 7:
		return opentile.CompressionJPEG
	case 8, 32946:
		return opentile.CompressionDeflate
	case 33003:
		return opentile.CompressionJP2K
	default:
		return opentile.CompressionUnknown
	}
}
