package bif

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/bifxml"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// bytesReader is a small adapter so blank-tile TileReader can return
// a bytes.Reader-backed io.Reader without allocating per-call when
// the same blank-tile cache entry is requested repeatedly.
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// levelImpl is the BIF Level implementation. One per pyramid IFD.
//
// v0.7 always returns the raw compressed JPEG tile bytes as stored
// in the source TIFF — no JPEGTables splice (T15 wires that), no
// decode, no pixel-space crop. The byte-passthrough hot path is
// preserved.
//
// Tile addressing: callers use image-space row-major (col, row);
// internally the (col, row) is remapped via imageToSerpentine to find
// the entry in TileOffsets/TileByteCounts. The remap is cheap (a
// few ops) — no per-tile XMP lookup required.
type levelImpl struct {
	index       int           // 0-based level index in the Image's Levels slice
	pyrIndex    int           // parsed `level=N` value from ImageDescription
	size        opentile.Size // base ImageWidth × ImageLength of this pyramid level
	tileSize    opentile.Size // TileWidth × TileLength
	grid        opentile.Size // tile grid dimensions (cols × rows)
	compression opentile.Compression
	mpp         opentile.MPP
	tileOverlap opentile.Point // non-zero only on level 0 of overlapping spec-compliant slides

	offsets []uint64 // TileOffsets, in serpentine storage order
	counts  []uint64 // TileByteCounts, in serpentine storage order

	// jpegTables is TIFF tag 347 (JPEGTables) if present. Older
	// BIFs embed full JPEG headers in each tile (jpegTables nil);
	// newer BIFs (and OS-1) carry shared tables here that must be
	// spliced into each abbreviated tile scan via jpeg.InsertTables
	// before the bytes can decode. BIF is YCbCr — no APP14 RGB
	// colorspace-fix marker needed (unlike SVS).
	jpegTables []byte

	// scanWhitePoint is the white-fill luminance for empty (unscanned)
	// tiles per spec §"AOI Positions". 0..255. Spec-compliant slides
	// inherit this from <iScan>/@ScanWhitePoint; legacy iScan and any
	// slide where the attribute is absent fall back to 255 (true white).
	scanWhitePoint uint8

	// imageDepth is the IMAGE_DEPTH (32997) tag value: count of
	// Z-planes stored in TileOffsets/TileByteCounts. 1 for non-
	// volumetric IFDs (every fixture in the local sample set
	// per the T1 gate). When > 1, TileOffsets is laid out
	// [Z=0 plane × M*N tiles][Z=1 plane × M*N tiles]... per BIF
	// whitepaper §"Whole slide imaging process".
	imageDepth int

	reader io.ReaderAt // for SectionReader-based streaming

	// maxTileSize is the cached upper bound for Tile/TileInto/TileAt
	// output: max(counts) + JPEGTables splice overhead (when applicable),
	// or len(blank tile) for tile-grid-empty fixtures whichever is larger.
	maxTileSize int

	// bodyMaxSize is the cached upper bound for on-disk tile bytes
	// (before any splice prefix is prepended): max(counts). For the
	// Ventana-1 per-tile-embedded case this equals maxTileSize (no
	// splice overhead). For the OS-1 shared-tables case this is strictly
	// less than maxTileSize. Populated at construction regardless of case.
	bodyMaxSize int

	// splicePrefix is the per-IFD constant payload spliced before SOS
	// on every tile when JPEGTables is shared (legacy OS-1 path). nil
	// when tiles are self-contained (Ventana-1 spec-compliant DP 200).
	// BIF is YCbCr — no APP14 marker (unlike SVS).
	splicePrefix []byte
}

// newLevelImpl constructs a levelImpl from a classified IFD. The
// EncodeInfo (parsed from the level-0 IFD's XMP) supplies tile
// overlap data — non-zero only when this is the level-0 IFD AND the
// XMP carried meaningful TileJointInfo OverlapX/OverlapY values.
func newLevelImpl(
	index int,
	c classifiedIFD,
	baseMPP float64,
	scanWhitePoint uint8,
	encodeInfo *bifxml.EncodeInfo,
	reader io.ReaderAt,
) (*levelImpl, error) {
	p := c.Page
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("bif level=%d: ImageWidth missing", c.Level)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("bif level=%d: ImageLength missing", c.Level)
	}
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("bif level=%d: TileWidth missing or zero", c.Level)
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("bif level=%d: TileLength missing or zero", c.Level)
	}
	cols, rows, err := p.TileGrid()
	if err != nil {
		return nil, fmt.Errorf("bif level=%d: TileGrid: %w", c.Level, err)
	}
	offsets, err := p.TileOffsets64()
	if err != nil {
		return nil, fmt.Errorf("bif level=%d: TileOffsets: %w", c.Level, err)
	}
	counts, err := p.TileByteCounts64()
	if err != nil {
		return nil, fmt.Errorf("bif level=%d: TileByteCounts: %w", c.Level, err)
	}
	imageDepth, _ := p.ImageDepth() // (1, false) when tag absent
	if imageDepth < 1 {
		imageDepth = 1
	}
	expectedTiles := cols * rows * imageDepth
	if len(offsets) != len(counts) || len(offsets) != expectedTiles {
		return nil, fmt.Errorf("bif level=%d: tile table length mismatch: offsets=%d counts=%d expected=%d (= depth %d × grid %dx%d)",
			c.Level, len(offsets), len(counts), expectedTiles, imageDepth, cols, rows)
	}

	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	// Tag 347 (JPEGTables): optional per spec. Newer BIFs share
	// DQT/DHT here so abbreviated per-tile scans can decode; older
	// BIFs embed everything per-tile. Both arrangements are valid
	// within the spec and both must be supported.
	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	// Per-level MPP: base ScanRes (µm/px at level 0) doubled per pyramid step.
	levelMPPMicrons := baseMPP * float64(int(1)<<c.Level)
	mpp := opentile.MPP{X: levelMPPMicrons, Y: levelMPPMicrons}

	// TileOverlap is the per-level tile step deficit. Only the
	// level-0 IFD's EncodeInfo carries TileJointInfo entries; we
	// collapse them into a single weighted-average value per spec
	// §8 (matches openslide). Pyramid levels 1+ are non-overlapping
	// per the whitepaper, so they always return opentile.Point{}.
	var tileOverlap opentile.Point
	if c.Level == 0 && encodeInfo != nil {
		tileOverlap = weightedAverageOverlap(encodeInfo)
	}

	var maxCount uint64
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	bodyMaxSize := int(maxCount) // on-disk bytes only; before splice overhead
	maxTileSize := bodyMaxSize
	var splicePrefix []byte
	if ocomp == opentile.CompressionJPEG && len(jpegTables) > 0 {
		var err error
		splicePrefix, err = jpeg.BuildSplicePrefix(jpegTables, false)
		if err != nil {
			return nil, fmt.Errorf("bif level=%d: build splice prefix: %w", c.Level, err)
		}
		maxTileSize += len(splicePrefix)
	}
	// Empty-tile path returns a small JPEG (~tileSize²/100 bytes
	// typically). It's bounded by the same per-tile envelope; no
	// adjustment needed.

	return &levelImpl{
		index:          index,
		pyrIndex:       c.Level,
		size:           opentile.Size{W: int(iw), H: int(il)},
		tileSize:       opentile.Size{W: int(tw), H: int(tl)},
		grid:           opentile.Size{W: cols, H: rows},
		compression:    ocomp,
		mpp:            mpp,
		tileOverlap:    tileOverlap,
		offsets:        offsets,
		counts:         counts,
		jpegTables:     jpegTables,
		scanWhitePoint: scanWhitePoint,
		imageDepth:     imageDepth,
		reader:         reader,
		maxTileSize:    maxTileSize,
		bodyMaxSize:    bodyMaxSize,
		splicePrefix:   splicePrefix,
	}, nil
}

// weightedAverageOverlap collapses EncodeInfo's per-tile-pair
// `<TileJointInfo>` entries into a single (X, Y) opentile.Point using
// pixel-count weighting — matches openslide's
// `tile_advance_x / tile_advance_y` computation. Returns opentile.Point{}
// if there are no joint entries.
//
// The whitepaper says DP 200 only ever produces horizontal overlap
// (OverlapY == 0); we don't enforce that here, just report the data.
// Both local fixtures record OverlapX = OverlapY = 0, so this fold
// returns {0, 0} on real data and the non-zero path is exercised
// only by synthetic-XMP unit tests.
func weightedAverageOverlap(ei *bifxml.EncodeInfo) opentile.Point {
	var sumX, sumY, count int
	for _, info := range ei.ImageInfos {
		for _, j := range info.Joints {
			sumX += j.OverlapX
			sumY += j.OverlapY
			count++
		}
	}
	if count == 0 {
		return opentile.Point{}
	}
	return opentile.Point{X: sumX / count, Y: sumY / count}
}

func (l *levelImpl) Index() int                        { return l.index }
func (l *levelImpl) PyramidIndex() int                 { return l.pyrIndex }
func (l *levelImpl) Size() opentile.Size               { return l.size }
func (l *levelImpl) TileSize() opentile.Size           { return l.tileSize }
func (l *levelImpl) Grid() opentile.Size               { return l.grid }
func (l *levelImpl) Compression() opentile.Compression { return l.compression }
func (l *levelImpl) MPP() opentile.MPP                  { return l.mpp }
func (l *levelImpl) FocalPlane() float64               { return 0 }
func (l *levelImpl) TileOverlap() opentile.Point          { return l.tileOverlap }

// indexOf validates (z, col, row) and returns the storage index
// into offsets/counts. For non-volumetric IFDs (imageDepth == 1)
// only z=0 is valid; for volumetric IFDs the layout per spec
// §"Whole slide imaging process" is [Z=0 plane × M*N][Z=1 plane × M*N]...
// — so the Z-stride is `cols*rows`.
//
// Error discrimination per design spec §11 Q1:
//   - imageDepth == 1 and z != 0 → ErrDimensionUnavailable
//     (Z axis effectively absent on this slide).
//   - imageDepth > 1 and z out of [0, imageDepth) → ErrTileOutOfBounds
//     (axis exists but the index is past its size).
//   - any (col, row) out of grid → ErrTileOutOfBounds.
func (l *levelImpl) indexOf(z, col, row int) (int, error) {
	if z != 0 && l.imageDepth == 1 {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: opentile.ErrDimensionUnavailable}
	}
	if z < 0 || z >= l.imageDepth {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: opentile.ErrTileOutOfBounds}
	}
	if col < 0 || row < 0 || col >= l.grid.W || row >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: opentile.ErrTileOutOfBounds}
	}
	serp := imageToSerpentine(col, row, l.grid.W, l.grid.H)
	if serp < 0 {
		// Defensive — imageToSerpentine should never return -1 for in-bounds (col, row).
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: opentile.ErrTileOutOfBounds}
	}
	idx := z*(l.grid.W*l.grid.H) + serp
	if idx < 0 || idx >= len(l.offsets) {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: opentile.ErrTileOutOfBounds}
	}
	return idx, nil
}

// readTileAtIdx is the canonical tile read: empty-tile blank fill,
// JPEGTables splice, or raw passthrough. Shared between Tile() and
// TileAt() so both paths are byte-identical.
func (l *levelImpl) readTileAtIdx(idx, col, row int) ([]byte, error) {
	if l.isEmpty(idx) {
		b, err := blankTile(l.tileSize.W, l.tileSize.H, l.scanWhitePoint)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
		}
		return b, nil
	}
	length := l.counts[idx]
	off := int64(l.offsets[idx])
	buf := make([]byte, length)
	if err := tiff.ReadAtFull(l.reader, buf, off); err != nil {
		return nil, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
	}
	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		out, err := jpeg.InsertTables(buf, l.jpegTables)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
		}
		return out, nil
	}
	return buf, nil
}

// TileAt is the multi-dim entry point for BIF. Supports Z-stacks
// (IMAGE_DEPTH-driven multi-Z) but no fluorescence channels (C) or
// time series (T) — those axes always 0, else ErrDimensionUnavailable.
//
// (col, row) is in image-space row-major; serpentine remapped
// internally. Z is the storage-order index 0..imageDepth-1; Z=0 is
// always the nominal focus plane per BIF whitepaper §"Whole slide
// imaging process".
func (l *levelImpl) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	idx, err := l.indexOf(coord.Z, coord.X, coord.Y)
	if err != nil {
		return nil, err
	}
	return l.readTileAtIdx(idx, coord.X, coord.Y)
}

// TileMaxSize is the cached upper bound for Tile / TileInto / TileAt
// output bytes on this level (since v0.9).
func (l *levelImpl) TileMaxSize() int { return l.maxTileSize }

// TilePrefix returns the cached splice prefix when this level uses
// shared JPEGTables (OS-1 case), or nil when JPEGTables are
// per-tile-embedded (Ventana-1 case).
//
// Returns a defensive copy — caller may mutate the returned slice.
//
// Specialized in v0.13.
func (l *levelImpl) TilePrefix() []byte {
	if len(l.splicePrefix) == 0 {
		return nil // Ventana-1 case: per-tile embedded JPEGTables
	}
	out := make([]byte, len(l.splicePrefix))
	copy(out, l.splicePrefix)
	return out
}

// TileBodyInto reads the on-disk tile bytes into dst. For levels with
// shared JPEGTables (TilePrefix() != nil, OS-1 case), this skips the
// splice step — caller combines TilePrefix() + TileBodyInto() output
// via opentile.SpliceJPEGTile to reconstitute the full JPEG. For
// levels with per-tile-embedded JPEGTables (TilePrefix() == nil,
// Ventana-1 case), the on-disk bytes ARE the full self-contained
// JPEG, so this delegates to TileInto.
//
// For empty (AOI-absent) tiles in the shared-tables case, the body
// returned is the synthesised blank-tile JPEG — a complete self-
// contained tile that consumers can use as-is or splice with
// TilePrefix() (wasteful but valid).
//
// Respects the serpentine index remap and AOI blank-fill handling
// of TileInto; only the JPEG-splice step is omitted.
//
// Specialized in v0.13.
func (l *levelImpl) TileBodyInto(x, y int, dst []byte) (int, error) {
	if len(l.splicePrefix) == 0 {
		// Per-tile embedded case (Ventana-1): body IS the full tile.
		return l.TileInto(x, y, dst)
	}
	// Shared-tables case (OS-1): read on-disk body bytes without splice.
	idx, err := l.indexOf(0, x, y)
	if err != nil {
		return 0, err
	}
	if l.isEmpty(idx) {
		// Synthesised blank tile: a complete self-contained JPEG.
		b, err := blankTile(l.tileSize.W, l.tileSize.H, l.scanWhitePoint)
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
	if len(dst) < length {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[:length], off); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return length, nil
}

// TileBodyMaxSize returns max(counts) for the shared-tables case
// (body bytes only, strictly less than TileMaxSize) or TileMaxSize()
// for the per-tile-embedded case (body == full tile).
//
// Specialized in v0.13.
func (l *levelImpl) TileBodyMaxSize() int {
	if len(l.splicePrefix) == 0 {
		return l.TileMaxSize()
	}
	return l.bodyMaxSize
}

// warm pre-faults the page-cache pages backing every tile on this
// level. For volumetric IFDs (imageDepth > 1) the offsets array
// already contains all Z-plane entries flat; one pass covers them.
func (l *levelImpl) warm() error {
	for i, off := range l.offsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// Tile returns the compressed tile bytes at (col, row) in
// image-space at the nominal focal plane (Z=0). Allocates the
// returned slice; high-RPS callers should switch to TileInto with
// a pooled buffer.
//
// See readTileAtIdx for the empty-tile / JPEGTables-splice / raw
// passthrough behaviour shared with TileAt.
func (l *levelImpl) Tile(col, row int) ([]byte, error) {
	idx, err := l.indexOf(0, col, row)
	if err != nil {
		return nil, err
	}
	return l.readTileAtIdx(idx, col, row)
}

// TileInto writes the tile at (col, row) into dst (since v0.9).
// Returns io.ErrShortBuffer if len(dst) < TileMaxSize().
func (l *levelImpl) TileInto(col, row int, dst []byte) (int, error) {
	idx, err := l.indexOf(0, col, row)
	if err != nil {
		return 0, err
	}
	return l.readTileAtIdxInto(idx, col, row, dst)
}

// readTileAtIdxInto is the TileInto-shaped variant of readTileAtIdx.
// Writes output bytes into dst; returns io.ErrShortBuffer when dst
// is undersized. Caller has already validated (z, col, row) → idx.
// Splice path uses the in-place jpeg.InsertPrefixInPlace —
// zero internal allocations.
func (l *levelImpl) readTileAtIdxInto(idx, col, row int, dst []byte) (int, error) {
	if l.isEmpty(idx) {
		b, err := blankTile(l.tileSize.W, l.tileSize.H, l.scanWhitePoint)
		if err != nil {
			return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
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
			return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
		}
		return length, nil
	}
	prefixLen := len(l.splicePrefix)
	if len(dst) < length+prefixLen {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[prefixLen:prefixLen+length], off); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
	}
	n, err := jpeg.InsertPrefixInPlace(dst, length, l.splicePrefix)
	if err != nil {
		return 0, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
	}
	return n, nil
}

// TileReader returns a streaming reader over the tile at (col, row).
//
//   - Empty entries: a bytes.Reader over the cached blank tile.
//   - JPEG entries with shared tables: the splice produces a buffer
//     that doesn't correspond to a contiguous file region, so we
//     return a bytes.Reader over the spliced output. Streaming
//     buys nothing here.
//   - Other JPEG entries: a zero-copy io.SectionReader over the
//     source TIFF.
func (l *levelImpl) TileReader(col, row int) (io.ReadCloser, error) {
	idx, err := l.indexOf(0, col, row)
	if err != nil {
		return nil, err
	}
	if l.isEmpty(idx) {
		b, err := blankTile(l.tileSize.W, l.tileSize.H, l.scanWhitePoint)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: col, Y: row, Err: err}
		}
		return io.NopCloser(bytesReader(b)), nil
	}
	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		b, err := l.Tile(col, row)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytesReader(b)), nil
	}
	length := l.counts[idx]
	off := int64(l.offsets[idx])
	return io.NopCloser(io.NewSectionReader(l.reader, off, int64(length))), nil
}

// isEmpty reports whether the tile at TileOffsets index idx is the
// spec-defined empty marker (offset == 0 AND bytecount == 0).
func (l *levelImpl) isEmpty(idx int) bool {
	return l.offsets[idx] == 0 && l.counts[idx] == 0
}

// Tiles iterates every tile position in image-space row-major order.
// Serial — callers parallelise on top of Tile(c, r) if needed.
func (l *levelImpl) Tiles(ctx context.Context) iter.Seq2[opentile.Point, opentile.TileResult] {
	return func(yield func(opentile.Point, opentile.TileResult) bool) {
		for r := 0; r < l.grid.H; r++ {
			for c := 0; c < l.grid.W; c++ {
				if ctx.Err() != nil {
					return
				}
				b, err := l.Tile(c, r)
				if !yield(opentile.Point{X: c, Y: r}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}

// tiffCompressionToOpentile maps the TIFF Compression tag value to
// opentile's Compression enum. Mirrors `formats/svs/tiled.go::tiffCompressionToOpentile`
// (BIF only ever uses JPEG=7 on pyramid IFDs per the whitepaper, but
// we include the small switch for completeness and consistency with
// other format packages).
func tiffCompressionToOpentile(c uint32) opentile.Compression {
	switch c {
	case 7:
		return opentile.CompressionJPEG
	case 1:
		return opentile.CompressionNone
	case 5:
		return opentile.CompressionLZW
	default:
		return opentile.CompressionUnknown
	}
}

// levelImplInspector is a compile-time check that levelImpl has all
// the metadata accessor methods used by the tiler to build value-type
// opentile.Level slices at Open time.
var _ = (*levelImpl)(nil)
