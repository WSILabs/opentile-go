package ometiff

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"math"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// tiledImage is the OME Level implementation for tiled pages
// (TileWidth tag present, TileOffsets / TileByteCounts arrays carry
// per-tile bytes). Mirrors Philips's tiled.go without the sparse-tile
// blank-tile machinery — OME tile data is dense.
//
// Per the v0.6 T5 audit, OME tile bytes are self-contained
// (jpegtables=None on every fixture page), so JPEGTables splicing is
// only applied when a page does carry shared tables. Both Leica
// fixtures take the no-splice path; the conditional preserves
// correctness on hypothetical OME files that DO carry JPEGTables.
type tiledImage struct {
	index       int
	size        opentile.Size
	tileSize    opentile.Size
	grid        opentile.Size
	compression opentile.Compression
	mpp         opentile.MPP
	pyrIndex    int

	offsets     []uint64
	counts      []uint64
	jpegTables  []byte
	reader      io.ReaderAt
	maxTileSize int // cached upper bound for Tile/TileInto output
	// bodyMaxSize is the cached upper bound for on-disk tile bytes:
	//   max(counts). Strictly less than maxTileSize when the level
	// carries shared JPEGTables (splice path). Used by TileBodyMaxSize.
	bodyMaxSize  int
	splicePrefix []byte // tablesMid (no APP14); nil for non-splice levels
}

func newTiledImage(
	index int,
	p *tiff.Page,
	baseSize opentile.Size,
	baseMPP opentile.MPP,
	r io.ReaderAt,
) (*tiledImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ome: tiled level missing ImageWidth")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ome: tiled level missing ImageLength")
	}
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("ome: tiled level missing TileWidth")
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("ome: tiled level missing TileLength")
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
	if len(offsets) != len(counts) {
		return nil, fmt.Errorf("ome: tile table mismatch: offsets=%d counts=%d", len(offsets), len(counts))
	}
	// Planar configuration: when PlanarConfiguration=2 (separate planes),
	// the tile offset / count arrays carry plane_count * grid entries
	// (one set of tile offsets per channel plane). Python opentile
	// silently uses plane 0 by indexing as y*W + x with no plane factor;
	// we mirror that for byte parity. The other planes are dropped —
	// listed as a deviation alongside multi-image-OME exposure.
	if len(offsets) < gx*gy {
		return nil, fmt.Errorf("ome: tile table too small: offsets=%d, grid=%dx%d (=%d)",
			len(offsets), gx, gy, gx*gy)
	}
	comp, _ := p.Compression()
	ocomp := tiffCompressionToOpentile(comp)

	var jpegTables []byte
	if ocomp == opentile.CompressionJPEG {
		if tb, ok := p.JPEGTables(); ok {
			jpegTables = tb
		}
	}

	size := opentile.Size{W: int(iw), H: int(il)}
	pyr := 0
	if baseSize.W > 0 && size.W > 0 {
		pyr = int(math.Round(math.Log2(float64(baseSize.W) / float64(size.W))))
		if pyr < 0 {
			pyr = 0
		}
	}
	mpp := scaleMPP(baseMPP, baseSize, size)

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
			return nil, fmt.Errorf("ome: build splice prefix: %w", err)
		}
		maxTileSize += len(splicePrefix)
	}

	return &tiledImage{
		index:        index,
		size:         size,
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
func (l *tiledImage) MPP() opentile.MPP                  { return l.mpp }
func (l *tiledImage) FocalPlane() float64               { return 0 }
func (l *tiledImage) TileOverlap() image.Point          { return image.Point{} }

// Tile returns the JPEG bytes for tile (x, y). Out-of-bounds → wrapped
// ErrTileOutOfBounds. Zero-length tile entries (which would mean the
// file is corrupt or sparse) → ErrCorruptTile (OME doesn't have
// Philips's sparse-tile semantics).
//
// JPEGTables splicing is conditional on the page actually carrying
// the tables tag. Both Leica fixtures do not (verified in T5), so
// most OME tile reads fall through with the raw bytes; the splice
// path remains for correctness on OME files that DO carry tables.

// TileAt is the multi-dim entry point. v0.7 OME is 2D-only on the
// read path even when <Pixels SizeZ/SizeC/SizeT> > 1 — multi-Z
// TileAt is deferred to a future format-package milestone (per
// L19 in deferred.md). v0.7's path: surface SizeZ/C/T honestly
// via Image accessors but error loudly here on non-zero coords.
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

func (l *tiledImage) TileMaxSize() int { return l.maxTileSize }

// TilePrefix returns the cached OME splice prefix (DQT + DHT, no
// APP14) or nil if this level doesn't carry shared JPEGTables. Both
// Leica fixtures take the nil path (no JPEGTables tag); the splice
// branch is present for correctness on OME files that do carry tables.
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
// the splice prefix. Caller can call opentile.SpliceJPEGTile with
// TilePrefix() output to reconstitute the full JPEG.
//
// Specialized in v0.13.
func (l *tiledImage) TileBodyInto(x, y int, dst []byte) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	count := int(l.counts[idx])
	if count == 0 {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
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
// shared JPEGTables.
//
// Specialized in v0.13.
func (l *tiledImage) TileBodyMaxSize() int { return l.bodyMaxSize }

// warm pre-faults the page-cache pages backing every tile on this
// level.
func (l *tiledImage) warm() error {
	for i, off := range l.offsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.counts[i])); err != nil {
			return err
		}
	}
	return nil
}

// Tile keeps the v0.8-and-earlier fast path: read + splice (when
// applicable) + return. TileInto is the pool-friendly variant.
func (l *tiledImage) Tile(x, y int) ([]byte, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	if l.counts[idx] == 0 {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	buf := make([]byte, l.counts[idx])
	if err := tiff.ReadAtFull(l.reader, buf, int64(l.offsets[idx])); err != nil {
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

// TileInto reads tile bytes directly into dst. Both the no-splice
// path (most OME files — neither Leica fixture carries JPEGTables)
// and the splice path do zero internal allocation.
func (l *tiledImage) TileInto(x, y int, dst []byte) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	length := int(l.counts[idx])
	if length == 0 {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	if l.splicePrefix == nil {
		if len(dst) < length {
			return 0, io.ErrShortBuffer
		}
		if err := tiff.ReadAtFull(l.reader, dst[:length], int64(l.offsets[idx])); err != nil {
			return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		return length, nil
	}
	prefixLen := len(l.splicePrefix)
	if len(dst) < length+prefixLen {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[prefixLen:prefixLen+length], int64(l.offsets[idx])); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	n, err := jpeg.InsertPrefixInPlace(dst, length, l.splicePrefix)
	if err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileReader returns an io.ReadCloser over the tile bytes. When the
// page has no JPEGTables (the common OME case), backed by an
// io.SectionReader for zero-copy streaming. When tables are spliced,
// materialises through Tile() and wraps a bytes.Reader.
func (l *tiledImage) TileReader(x, y int) (io.ReadCloser, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	if l.counts[idx] == 0 {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	if l.compression == opentile.CompressionJPEG && len(l.jpegTables) > 0 {
		b, err := l.Tile(x, y)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	sr := io.NewSectionReader(l.reader, int64(l.offsets[idx]), int64(l.counts[idx]))
	return io.NopCloser(sr), nil
}

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

// scaleMPP scales a base MPP by the per-axis pixel-count ratio between
// the base level and this level. Mirrors Philips / SVS / NDPI.
func scaleMPP(baseMPP opentile.MPP, baseSize, lvlSize opentile.Size) opentile.MPP {
	scaleW, scaleH := 1.0, 1.0
	if lvlSize.W > 0 {
		scaleW = float64(baseSize.W) / float64(lvlSize.W)
	}
	if lvlSize.H > 0 {
		scaleH = float64(baseSize.H) / float64(lvlSize.H)
	}
	return opentile.MPP{
		X: baseMPP.X * scaleW,
		Y: baseMPP.Y * scaleH,
	}
}

// tiffCompressionToOpentile maps TIFF tag 259 numeric values to the
// opentile.Compression enum. OME slides we've seen carry only JPEG
// (7); other codes degrade to CompressionUnknown.
func tiffCompressionToOpentile(tiffCode uint32) opentile.Compression {
	switch tiffCode {
	case 1:
		return opentile.CompressionNone
	case 7:
		return opentile.CompressionJPEG
	}
	return opentile.CompressionUnknown
}
