package svs

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"math"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// tiledImage is the SVS Level implementation for tiled pages.
//
// v0.2 returns each tile as a standalone valid JPEG: for JPEG-compressed
// tiles the shared JPEGTables (DQT/DHT) are spliced before SOS and an APP14
// "Adobe" segment is inserted to advertise the non-standard RGB colorspace
// Aperio uses. Matches Python opentile's SvsTiledImage.get_tile output
// byte-for-byte. JP2K-compressed tiles are passthrough (self-contained
// codestream, no shared tables).
type tiledImage struct {
	index       int
	size        opentile.Size
	tileSize    opentile.Size
	grid        opentile.Size
	compression opentile.Compression
	mpp         opentile.SizeMm
	pyrIndex    int

	offsets    []uint64
	counts     []uint64
	jpegTables []byte // TIFF tag 347 payload (SOI..DQT/DHT..EOI); nil for non-JPEG pages
	reader     io.ReaderAt

	// maxTileSize is the cached upper bound for Tile/TileInto output:
	//   max(counts) + JPEGTables splice overhead (when applicable).
	// Computed once at level open in newTiledImage.
	maxTileSize int

	// splicePrefix is the constant-per-level payload inserted between
	// SOI and SOS on every tile read: tablesMid + adobeAPP14. nil
	// when the level doesn't need a splice (non-JPEG, or JPEG with no
	// shared JPEGTables). v0.9 in-place splicer uses this; legacy
	// Tile() retains the alloc-per-call jpeg.InsertTablesAndAPP14
	// path for backward compat.
	splicePrefix []byte

	cfg *opentile.Config
}

func newTiledImage(
	index int,
	p *tiff.Page,
	baseSize opentile.Size,
	baseMPP float64,
	r io.ReaderAt,
	cfg *opentile.Config,
) (*tiledImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ImageLength missing")
	}
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("TileWidth missing or zero")
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("TileLength missing or zero")
	}
	gx, gy, err := p.TileGrid()
	if err != nil {
		return nil, err
	}
	offsets, err := p.TileOffsets64()
	if err != nil {
		return nil, err
	}
	counts, err := p.TileByteCounts64()
	if err != nil {
		return nil, err
	}
	if len(offsets) != len(counts) || len(offsets) != gx*gy {
		return nil, fmt.Errorf("tile table length mismatch: offsets=%d counts=%d grid=%dx%d", len(offsets), len(counts), gx, gy)
	}
	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	// Cache JPEGTables (tag 347) once; only populated for JPEG-compressed
	// pages. JP2K pages have no shared tables — every codestream is
	// self-contained.
	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	// Pyramid index: log2(baseSize.W / iw), rounded to nearest int.
	var pyr int
	if baseSize.W > 0 {
		pyr = int(math.Round(math.Log2(float64(baseSize.W) / float64(iw))))
		if pyr < 0 {
			pyr = 0
		}
	}

	scale := float64(1)
	if iw > 0 {
		scale = float64(baseSize.W) / float64(iw)
	}
	mpp := opentile.SizeMm{W: baseMPP * scale / 1000.0, H: baseMPP * scale / 1000.0}

	// Cache the upper-bound output size for Tile/TileInto. For JPEG
	// pages with shared JPEGTables, the splice prepends (SOI + APP14 +
	// DQT + DHT) — InsertTablesAndAPP14 inserts at most
	// len(jpegTables) + 16 bytes (the +16 covers the APP14 marker
	// segment; see internal/jpeg.InsertTablesAndAPP14). For JP2K /
	// other compressions the tile bytes are returned verbatim.
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
		splicePrefix, err = jpeg.BuildSplicePrefix(jpegTables, true)
		if err != nil {
			return nil, fmt.Errorf("svs: build splice prefix: %w", err)
		}
		maxTileSize += len(splicePrefix)
	}

	return &tiledImage{
		index:        index,
		size:         opentile.Size{W: int(iw), H: int(il)},
		tileSize:     opentile.Size{W: int(tw), H: int(tl)},
		grid:         opentile.Size{W: gx, H: gy},
		compression:  ocomp,
		mpp:          mpp,
		pyrIndex:     pyr,
		offsets:      offsets,
		counts:       counts,
		jpegTables:   jpegTables,
		reader:       r,
		maxTileSize:  maxTileSize,
		splicePrefix: splicePrefix,
		cfg:          cfg,
	}, nil
}

func (l *tiledImage) Index() int                        { return l.index }
func (l *tiledImage) PyramidIndex() int                 { return l.pyrIndex }
func (l *tiledImage) Size() opentile.Size               { return l.size }
func (l *tiledImage) TileSize() opentile.Size           { return l.tileSize }
func (l *tiledImage) Grid() opentile.Size               { return l.grid }
func (l *tiledImage) Compression() opentile.Compression { return l.compression }
func (l *tiledImage) MPP() opentile.SizeMm              { return l.mpp }
func (l *tiledImage) FocalPlane() float64               { return 0 }
func (l *tiledImage) TileOverlap() image.Point          { return image.Point{} }

// indexOf computes the row-major tile index for (x, y) and validates the
// tile entry's byte count. Out-of-grid coords yield ErrTileOutOfBounds;
// a zero-length tile entry (which the SVS spec uses to signal a corrupt
// or missing edge tile) yields ErrCorruptTile. Both are wrapped in
// opentile.TileError. Tile and TileReader rely on the zero-length check
// happening here, so they don't need to repeat it.
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

// Tile returns the tile at (x, y) as a standalone valid JPEG (for
// JPEG-compressed pages) or the raw JP2K codestream (for JP2K pages).
//
// For JPEG pages, the per-tile TIFF payload is an abbreviated scan without
// DQT/DHT tables — usable only alongside the page-level JPEGTables tag.
// Tile() splices tables[2:-2] and an Adobe APP14 RGB colorspace marker
// before SOS so the returned bytes decode as a self-contained JPEG. The
// output matches Python opentile's SvsTiledImage.get_tile byte-for-byte.
// TileAt is the multi-dim entry point. SVS is 2D-only, so any
// non-zero Z, C, or T yields ErrDimensionUnavailable; otherwise
// delegates to Tile(x, y).
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

func (l *tiledImage) TileMaxSize() int { return l.maxTileSize }

// TilePrefix returns nil — this Level type doesn't expose a separable
// per-level splice prefix in v0.13. T2-T4 specializations override
// for the splice-format levels.
//
// Added in v0.13.
func (l *tiledImage) TilePrefix() []byte { return nil }

// TileBodyInto delegates to TileInto (no separation between body
// bytes and full tile output for non-splice levels). T2-T4
// specializations override for the splice-format levels.
//
// Added in v0.13.
func (l *tiledImage) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize equals TileMaxSize for non-splice levels (the body
// IS the full tile output). T2-T4 specializations override.
//
// Added in v0.13.
func (l *tiledImage) TileBodyMaxSize() int { return l.TileMaxSize() }

// warm pre-faults the page-cache pages backing every tile on this
// level. Called via Tiler.WarmLevel.
func (l *tiledImage) warm() error {
	for i, off := range l.offsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// Tile keeps the v0.8-and-earlier fast path: read tile bytes, splice
// JPEGTables if needed, return the result. Allocates only the
// final output (one alloc on no-splice; one read scratch + one
// spliced output on the JPEG-splice path).
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
		out, err := jpeg.InsertTablesAndAPP14(buf, l.jpegTables)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		return out, nil
	}
	return buf, nil
}

// TileInto reads tile bytes directly into dst, with zero internal
// allocations on every path:
//
//   - No-splice (JP2K / non-JPEG SVS): single ReadAt fills dst.
//   - JPEG-splice: tile bytes are read into dst at offset prefixLen,
//     then the splice prefix (cached at level open) is spliced in
//     place via jpeg.InsertPrefixInPlace. No scratch buffer; no
//     intermediate output buffer.
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

// TileReader returns an io.ReadCloser carrying the same bytes as Tile.
//
// For JP2K pages this is a zero-copy io.SectionReader over the underlying
// TIFF file. For JPEG pages it is a bytes.Reader over the spliced buffer —
// the JPEGTables + APP14 splice is an unavoidable pre-pend that cannot be
// expressed as a pure offset/length window, so streaming buys nothing
// compared to Tile(). Deliberate v0.2 correctness trade-off.
func (l *tiledImage) TileReader(x, y int) (io.ReadCloser, error) {
	idx, err := l.indexOf(x, y)
	if err != nil {
		return nil, err
	}
	length := l.counts[idx]
	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		b, err := l.Tile(x, y)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	sr := io.NewSectionReader(l.reader, int64(l.offsets[idx]), int64(length))
	return io.NopCloser(sr), nil
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
