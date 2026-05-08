package philipstiff

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"math"
	"sync"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/jpegturbo"
	"github.com/cornish/opentile-go/internal/tiff"
)

// tiledImage is the Philips Level implementation. Mirrors v0.2's SVS
// tiledImage shape with two Philips-specific differences:
//
//  1. Sparse tiles. Philips deliberately leaves TileByteCounts[idx]=0
//     for tiles in scanner-skipped background regions. Tile() returns a
//     cached "blank tile" derived from the first valid frame, with
//     every DCT coefficient overwritten to a luminance fill. Direct port
//     of PhilipsLevelTiffImage._read_frame / _create_blank_tile
//     (philips_tiff_image.py:143-198).
//  2. JPEGTables splice without APP14. Philips encodes standard YCbCr,
//     so unlike SVS no Adobe APP14 colorspace marker is needed —
//     plain InsertTables (no APP14) is the byte-for-byte parity match.
//
// The reported Size() is the post-correction dimension from
// computeCorrectedSizes, NOT the on-disk page.imagewidth/imagelength.
// Philips's on-disk dimensions for non-baseline levels are placeholders
// (see formats/philipstiff/dimensions.go for the algorithm).
type tiledImage struct {
	index       int
	size        opentile.Size
	tileSize    opentile.Size
	grid        opentile.Size
	compression opentile.Compression
	mpp         opentile.SizeMm
	pyrIndex    int

	offsets     []uint64
	counts      []uint64
	jpegTables  []byte
	reader      io.ReaderAt
	cfg         *opentile.Config
	maxTileSize int // upper bound for Tile/TileInto output
	// bodyMaxSize is the cached upper bound for on-disk tile bytes:
	//   max(counts). Strictly less than maxTileSize when the level
	// carries shared JPEGTables (splice path). Used by TileBodyMaxSize.
	// Sparse tiles (counts[i]==0) are excluded from the max; blank
	// tile bytes fit within maxTile already.
	bodyMaxSize  int
	splicePrefix []byte // tablesMid (no APP14 — Philips is YCbCr); nil for non-splice levels

	// Lazy-built blank tile for sparse positions. Computed once on the
	// first sparse-tile read; subsequent reads return the cached bytes.
	// sync.Once ensures concurrent Tile() calls observe a single
	// well-defined blank tile.
	blankOnce sync.Once
	blank     []byte
	blankErr  error
}

func newTiledImage(
	index int,
	p *tiff.Page,
	correctedSize opentile.Size,
	baseSize opentile.Size,
	baseMPP opentile.SizeMm,
	r io.ReaderAt,
	cfg *opentile.Config,
) (*tiledImage, error) {
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("TileWidth missing or zero")
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("TileLength missing or zero")
	}
	offsets, err := p.TileOffsets64()
	if err != nil {
		return nil, err
	}
	counts, err := p.TileByteCounts64()
	if err != nil {
		return nil, err
	}
	if len(offsets) != len(counts) {
		return nil, fmt.Errorf("tile table length mismatch: offsets=%d counts=%d",
			len(offsets), len(counts))
	}
	// Tile grid uses CORRECTED dims (not on-disk), matching Python
	// opentile's tiled_size = image_size.ceil_div(tile_size). On-disk
	// pages may carry more tile entries than gx*gy when the on-disk
	// imagewidth/imagelength placeholders happen to round up further
	// than the corrected dims; those trailing entries are unused but
	// must remain in the offsets/counts arrays for index compatibility
	// with NativeTiledTiffImage._tile_point_to_frame_index.
	gx := ceilDiv(correctedSize.W, int(tw))
	gy := ceilDiv(correctedSize.H, int(tl))
	if gx*gy > len(offsets) {
		return nil, fmt.Errorf("philips: corrected grid %dx%d exceeds on-disk tile table (%d entries)",
			gx, gy, len(offsets))
	}
	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	// Pyramid index: log2(baseSize.W / correctedSize.W), rounded.
	pyr := 0
	if baseSize.W > 0 && correctedSize.W > 0 {
		pyr = int(math.Round(math.Log2(float64(baseSize.W) / float64(correctedSize.W))))
		if pyr < 0 {
			pyr = 0
		}
	}

	// Per-axis MPP scaling. baseMPP is microns/pixel at the baseline;
	// each level's MPP scales by the level's reduction factor.
	scaleW, scaleH := 1.0, 1.0
	if correctedSize.W > 0 {
		scaleW = float64(baseSize.W) / float64(correctedSize.W)
	}
	if correctedSize.H > 0 {
		scaleH = float64(baseSize.H) / float64(correctedSize.H)
	}
	mpp := opentile.SizeMm{
		W: baseMPP.W * scaleW,
		H: baseMPP.H * scaleH,
	}

	var maxCount uint64
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	bodyMaxSize := int(maxCount)
	maxTileSize := bodyMaxSize
	var splicePrefix []byte
	if ocomp == opentile.CompressionJPEG && len(jpegTables) > 0 {
		var err error
		splicePrefix, err = jpeg.BuildSplicePrefix(jpegTables, false)
		if err != nil {
			return nil, fmt.Errorf("philips: build splice prefix: %w", err)
		}
		maxTileSize += len(splicePrefix)
	}
	// Sparse blank-tile path: blank-tile bytes are sized similarly
	// (post-FillFrame they fit within a single tile envelope) so the
	// max above already covers them.

	return &tiledImage{
		index:        index,
		size:         correctedSize,
		tileSize:     opentile.Size{W: int(tw), H: int(tl)},
		grid:         opentile.Size{W: gx, H: gy},
		compression:  ocomp,
		mpp:          mpp,
		pyrIndex:     pyr,
		offsets:      offsets,
		counts:       counts,
		jpegTables:   jpegTables,
		reader:       r,
		cfg:          cfg,
		maxTileSize:  maxTileSize,
		bodyMaxSize:  bodyMaxSize,
		splicePrefix: splicePrefix,
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

// Tile returns the tile at (x, y) as a standalone valid JPEG. Drives
// upstream's NativeTiledTiffImage.get_tile shape: the per-tile bytes
// (or the cached blank tile for sparse positions) always have the
// page-level JPEGTables spliced before SOS. No APP14 — Philips uses
// standard YCbCr.
//
// Note that the blank-tile path produces a JPEG with duplicate DQT/DHT
// segments (one set inside the FillFrame output, one from the splice
// here). This matches Python opentile byte-for-byte:
// _create_blank_tile splices tables once before fill_frame, and
// NativeTiledTiffImage.get_tile splices them again unconditionally.
// JPEG decoders accept duplicate tables; later definitions override
// earlier ones.

// TileAt is the multi-dim entry point. Philips is 2D-only.
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

func (l *tiledImage) TileMaxSize() int { return l.maxTileSize }

// TilePrefix returns the cached Philips splice prefix (DQT + DHT, no
// APP14) or nil if this level doesn't carry shared JPEGTables.
//
// Returns a defensive copy — caller may mutate the returned slice.
//
// Specialized in v0.13.
func (l *tiledImage) TilePrefix() []byte {
	if len(l.splicePrefix) == 0 {
		return nil
	}
	out := make([]byte, len(l.splicePrefix))
	copy(out, l.splicePrefix)
	return out
}

// TileBodyInto reads the on-disk tile bytes into dst WITHOUT applying
// the splice prefix. For sparse tiles (counts[idx]==0), the blank tile
// bytes are returned verbatim — the blank tile IS a complete JPEG
// (produced by FillFrame on a spliced source), so it is its own body.
//
// Specialized in v0.13.
func (l *tiledImage) TileBodyInto(x, y int, dst []byte) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x

	if l.counts[idx] == 0 {
		// Sparse: serve the cached blank tile as the body. The blank
		// tile is a complete self-contained JPEG (already spliced during
		// construction via FillFrame), so body == full tile for this case.
		b, err := l.blankTile()
		if err != nil {
			return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		if len(dst) < len(b) {
			return 0, io.ErrShortBuffer
		}
		return copy(dst, b), nil
	}

	count := int(l.counts[idx])
	if len(dst) < count {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[:count], int64(l.offsets[idx])); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return count, nil
}

// TileBodyMaxSize returns max(counts) — the upper bound on on-disk tile
// body bytes. Strictly less than TileMaxSize when the level carries
// shared JPEGTables (splice path). Note: sparse blank tiles fit within
// the same envelope, so no separate comparison is needed.
//
// Specialized in v0.13.
func (l *tiledImage) TileBodyMaxSize() int { return l.bodyMaxSize }

// warm pre-faults the page-cache pages backing every tile on this
// level. Sparse-tile entries (counts[i] == 0) carry no on-disk
// bytes; TouchPages skips them via its length<=0 short-circuit.
func (l *tiledImage) warm() error {
	for i, off := range l.offsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// Tile keeps the v0.8-and-earlier fast path: sparse-fill check, read,
// splice. TileInto is the pool-friendly variant.
func (l *tiledImage) Tile(x, y int) ([]byte, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x

	var raw []byte
	if l.counts[idx] == 0 {
		b, err := l.blankTile()
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		raw = make([]byte, len(b))
		copy(raw, b)
	} else {
		length := l.counts[idx]
		off := int64(l.offsets[idx])
		raw = make([]byte, length)
		if err := tiff.ReadAtFull(l.reader, raw, off); err != nil {
			return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
	}

	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		out, err := jpeg.InsertTables(raw, l.jpegTables)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		return out, nil
	}
	return raw, nil
}

// TileInto writes tile bytes (sparse blank-fill or on-disk) into
// dst. Splice path uses the in-place jpeg.InsertPrefixInPlace —
// zero internal allocations on the common case. Sparse blank tiles
// are still derived from the cached blank buffer (one alloc on
// first sparse access; subsequent reads reuse the cache).
func (l *tiledImage) TileInto(x, y int, dst []byte) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x

	if l.counts[idx] == 0 {
		// Sparse: serve the cached blank tile. The blank tile has
		// already been spliced (it's a complete JPEG produced via
		// FillFrame on a spliced source); copy verbatim into dst.
		b, err := l.blankTile()
		if err != nil {
			return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		if len(dst) < len(b) {
			return 0, io.ErrShortBuffer
		}
		return copy(dst, b), nil
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
// Always materialises the bytes via Tile() — Philips's JPEGTables
// splice and sparse-tile dispatch make a true zero-copy
// SectionReader path infeasible.
func (l *tiledImage) TileReader(x, y int) (io.ReadCloser, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
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

// blankTile returns the lazy-built blank tile for this level, computing
// it on the first call. Subsequent calls observe the cached value.
//
// Mirrors PhilipsLevelTiffImage._create_blank_tile: locate the first
// non-sparse entry, read its raw bytes, splice JPEGTables (no APP14),
// and run through FillFrame(luminance=1.0) to overwrite all DCT
// coefficients with a white fill.
func (l *tiledImage) blankTile() ([]byte, error) {
	l.blankOnce.Do(func() {
		l.blank, l.blankErr = l.computeBlankTile()
	})
	return l.blank, l.blankErr
}

func (l *tiledImage) computeBlankTile() ([]byte, error) {
	if l.compression != opentile.CompressionJPEG {
		return nil, fmt.Errorf("philips: blank tile requires JPEG compression (got %v)", l.compression)
	}
	// First non-sparse entry → seed for the fill.
	seed := -1
	for i, c := range l.counts {
		if c != 0 {
			seed = i
			break
		}
	}
	if seed < 0 {
		return nil, fmt.Errorf("philips: no valid frames in level (all tiles sparse)")
	}
	buf := make([]byte, l.counts[seed])
	if err := tiff.ReadAtFull(l.reader, buf, int64(l.offsets[seed])); err != nil {
		return nil, fmt.Errorf("philips: read seed frame: %w", err)
	}
	if len(l.jpegTables) > 0 {
		spliced, err := jpeg.InsertTables(buf, l.jpegTables)
		if err != nil {
			return nil, fmt.Errorf("philips: splice tables for blank seed: %w", err)
		}
		buf = spliced
	}
	out, err := jpegturbo.FillFrame(buf, jpegturbo.DefaultBackgroundLuminance)
	if err != nil {
		return nil, fmt.Errorf("philips: FillFrame: %w", err)
	}
	return out, nil
}

// ceilDiv returns ⌈a/b⌉ for non-negative ints. Defined here rather than
// in opentile core because the only Philips caller is the corrected-dim
// grid computation in newTiledImage.
func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	q := a / b
	if a%b != 0 {
		q++
	}
	return q
}

// tiffCompressionToOpentile maps TIFF tag 259 numeric values to the
// opentile.Compression enum. Philips slides we've seen carry only
// JPEG (7) and uncompressed (1, label/macro). Unknown codes degrade
// gracefully to CompressionUnknown.
func tiffCompressionToOpentile(tiffCode uint32) opentile.Compression {
	switch tiffCode {
	case 1:
		return opentile.CompressionNone
	case 7:
		return opentile.CompressionJPEG
	}
	return opentile.CompressionUnknown
}
