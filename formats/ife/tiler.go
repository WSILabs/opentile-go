package ife

import (
	"context"
	"image"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// openIFE is the real construction entry point. It parses every metadata
// block once, builds the level slice (native-first, inverted from
// the file's coarsest-first storage), and returns a format.Reader that
// fields Tile / TileAt requests via direct ReadAt to the cached
// per-tile (offset, size) entries.
func openIFE(r io.ReaderAt, size int64, _ *format.Config) (format.Reader, error) {
	hdr, err := readFileHeader(r, size)
	if err != nil {
		return nil, err
	}
	tt, err := readTileTable(r, hdr.TileTableOffset, size)
	if err != nil {
		return nil, err
	}
	compression, err := compressionFromEncoding(tt.Encoding)
	if err != nil {
		return nil, err
	}
	fileOrder, err := readLayerExtents(r, tt.LayerExtentsOffset, size)
	if err != nil {
		return nil, err
	}

	var totalTiles uint64
	cumulative := make([]uint64, len(fileOrder))
	for i, le := range fileOrder {
		cumulative[i] = totalTiles
		totalTiles += uint64(le.XTiles) * uint64(le.YTiles)
	}
	tiles, err := readTileOffsets(r, tt.TileOffsetsOffset, totalTiles, size)
	if err != nil {
		return nil, err
	}

	// Native-first API order. Reverse the file-storage slice without
	// mutating the underlying memory.
	apiOrder := make([]LayerExtent, len(fileOrder))
	for i, le := range fileOrder {
		apiOrder[len(fileOrder)-1-i] = le
	}

	// Optional metadata block; absent on minimal synthetic files.
	var md Metadata
	var assoc []opentile.AssociatedImage
	var icc []byte
	if hdr.MetadataOffset != NullOffset && hdr.MetadataOffset != 0 {
		md, assoc, icc, err = readMetadata(r, hdr.MetadataOffset, size)
		if err != nil {
			return nil, err
		}
	}

	t := &tiler{
		hdr:              hdr,
		tt:               tt,
		compression:      compression,
		layerExtentsFile: fileOrder,
		layerExtentsAPI:  apiOrder,
		layerCumulative:  cumulative,
		tileOffsets:      tiles,
		r:                r,
		md:               md,
		associated:       assoc,
		icc:              icc,
	}
	t.levels = make([]opentile.Level, len(apiOrder))
	for i := range apiOrder {
		// Compute TileMaxSize for this level: walk the per-level
		// slice of TILE_OFFSETS entries and find the maximum byte
		// length. Sparse entries (Offset == NullTile) carry Size == 0
		// and don't move the max.
		fi := len(apiOrder) - 1 - i
		ext := fileOrder[fi]
		base := cumulative[fi]
		levelTileCount := uint64(ext.XTiles) * uint64(ext.YTiles)
		var maxSize uint32
		for k := uint64(0); k < levelTileCount; k++ {
			if s := tiles[base+k].Size; s > maxSize {
				maxSize = s
			}
		}
		t.levels[i] = &levelImpl{tiler: t, apiIndex: i, maxTileSize: int(maxSize)}
	}
	return t, nil
}

// tiler is the IFE implementation of format.Reader. All fields are
// populated at Open time and immutable thereafter; Tile / TileAt
// reads via the parent r are concurrency-safe (io.ReaderAt's
// contract; *os.File satisfies it).
type tiler struct {
	hdr              FileHeader
	tt               TileTable
	compression      opentile.Compression
	layerExtentsFile []LayerExtent // coarsest-first (storage order)
	layerExtentsAPI  []LayerExtent // native-first (API order)
	layerCumulative  []uint64      // prefix sum of x_tiles*y_tiles in FILE order
	tileOffsets      []TileEntry
	r                io.ReaderAt
	levels           []opentile.Level

	md         Metadata
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format { return opentile.FormatIFE }

func (t *tiler) Images() []opentile.Image {
	return []opentile.Image{opentile.NewSingleImage(t.levels)}
}

func (t *tiler) Levels() []opentile.Level {
	out := make([]opentile.Level, len(t.levels))
	copy(out, t.levels)
	return out
}

func (t *tiler) Level(i int) (opentile.Level, error) {
	if i < 0 || i >= len(t.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levels[i], nil
}

func (t *tiler) Associated() []opentile.AssociatedImage {
	out := make([]opentile.AssociatedImage, len(t.associated))
	copy(out, t.associated)
	return out
}
func (t *tiler) Metadata() opentile.Metadata { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte {
	if len(t.icc) == 0 {
		return nil
	}
	out := make([]byte, len(t.icc))
	copy(out, t.icc)
	return out
}
func (t *tiler) Close() error { return nil }
func (t *tiler) WarmLevel(i int) error {
	if i < 0 || i >= len(t.levels) {
		return opentile.ErrLevelOutOfRange
	}
	if w, ok := t.levels[i].(interface{ warm() error }); ok {
		return w.warm()
	}
	return nil
}

// levelImpl is the IFE implementation of opentile.Level. apiIndex
// indexes layerExtentsAPI (0 = native, len-1 = coarsest); the
// underlying TILE_OFFSETS lookups use the file-storage index
// derived as len-1-apiIndex.
type levelImpl struct {
	tiler       *tiler
	apiIndex    int
	maxTileSize int // max(entry.Size) across this level's TILE_OFFSETS entries
}

func (l *levelImpl) Index() int        { return l.apiIndex }
func (l *levelImpl) PyramidIndex() int { return l.apiIndex }

func (l *levelImpl) extent() LayerExtent { return l.tiler.layerExtentsAPI[l.apiIndex] }

func (l *levelImpl) Size() opentile.Size {
	e := l.extent()
	return opentile.Size{
		W: int(e.XTiles) * TileSidePixels,
		H: int(e.YTiles) * TileSidePixels,
	}
}

func (l *levelImpl) TileSize() opentile.Size {
	return opentile.Size{W: TileSidePixels, H: TileSidePixels}
}

func (l *levelImpl) Grid() opentile.Size {
	e := l.extent()
	return opentile.Size{W: int(e.XTiles), H: int(e.YTiles)}
}

func (l *levelImpl) TileOverlap() image.Point          { return image.Point{} }
func (l *levelImpl) Compression() opentile.Compression { return l.tiler.compression }
func (l *levelImpl) MPP() opentile.SizeMm              { return opentile.SizeMm{} }
func (l *levelImpl) FocalPlane() float64               { return 0 }

// fileIndex maps an apiIndex to the file-storage index. Layers are
// stored coarsest-first; the API exposes native-first. fileIndex is
// always len(api)-1-apiIndex.
func (l *levelImpl) fileIndex() int {
	return len(l.tiler.layerExtentsAPI) - 1 - l.apiIndex
}

// linearIndex is the position of (col, row) into the global
// TILE_OFFSETS array. Iteration order in the file is layers in
// storage order, then row-major within each layer.
func (l *levelImpl) linearIndex(col, row int) (uint64, error) {
	fi := l.fileIndex()
	ext := l.tiler.layerExtentsFile[fi]
	if col < 0 || row < 0 || uint32(col) >= ext.XTiles || uint32(row) >= ext.YTiles {
		return 0, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   opentile.ErrTileOutOfBounds,
		}
	}
	return l.tiler.layerCumulative[fi] + uint64(row)*uint64(ext.XTiles) + uint64(col), nil
}

func (l *levelImpl) TileMaxSize() int { return l.maxTileSize }

// TilePrefix returns nil — this Level type doesn't expose a separable
// per-level splice prefix in v0.13. T2-T4 specializations override
// for the splice-format levels.
//
// Added in v0.13.
func (l *levelImpl) TilePrefix() []byte { return nil }

// TileBodyInto delegates to TileInto (no separation between body
// bytes and full tile output for non-splice levels). T2-T4
// specializations override for the splice-format levels.
//
// Added in v0.13.
func (l *levelImpl) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize equals TileMaxSize for non-splice levels (the body
// IS the full tile output). T2-T4 specializations override.
//
// Added in v0.13.
func (l *levelImpl) TileBodyMaxSize() int { return l.TileMaxSize() }

// warm pre-faults the page-cache pages backing every tile entry on
// this level. Sparse entries (Offset == NullTile) carry no on-disk
// bytes and are skipped. Called via Tiler.WarmLevel.
func (l *levelImpl) warm() error {
	fi := l.fileIndex()
	ext := l.tiler.layerExtentsFile[fi]
	base := l.tiler.layerCumulative[fi]
	n := uint64(ext.XTiles) * uint64(ext.YTiles)
	for k := uint64(0); k < n; k++ {
		e := l.tiler.tileOffsets[base+k]
		if e.Offset == NullTile || e.Size == 0 {
			continue
		}
		if err := tiff.TouchPages(l.tiler.r, int64(e.Offset), int64(e.Size)); err != nil {
			return err
		}
	}
	return nil
}

// Tile allocates a fresh []byte sized exactly to the entry's Size
// and reads tile bytes directly into it. High-RPS callers should
// switch to TileInto with a pooled buffer (no internal alloc; IFE
// tiles are self-contained — no splice needed).
func (l *levelImpl) Tile(col, row int) ([]byte, error) {
	idx, err := l.linearIndex(col, row)
	if err != nil {
		return nil, err
	}
	entry := l.tiler.tileOffsets[idx]
	if entry.Offset == NullTile || entry.Size == 0 {
		return nil, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   opentile.ErrSparseTile,
		}
	}
	buf := make([]byte, entry.Size)
	if _, err := l.tiler.r.ReadAt(buf, int64(entry.Offset)); err != nil {
		return nil, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   err,
		}
	}
	return buf, nil
}

func (l *levelImpl) TileInto(col, row int, dst []byte) (int, error) {
	idx, err := l.linearIndex(col, row)
	if err != nil {
		return 0, err
	}
	entry := l.tiler.tileOffsets[idx]
	if entry.Offset == NullTile || entry.Size == 0 {
		return 0, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   opentile.ErrSparseTile,
		}
	}
	if len(dst) < int(entry.Size) {
		return 0, io.ErrShortBuffer
	}
	if _, err := l.tiler.r.ReadAt(dst[:entry.Size], int64(entry.Offset)); err != nil {
		return 0, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   err,
		}
	}
	return int(entry.Size), nil
}

func (l *levelImpl) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{
			Level: l.apiIndex,
			X:     coord.X,
			Y:     coord.Y,
			Err:   opentile.ErrDimensionUnavailable,
		}
	}
	return l.Tile(coord.X, coord.Y)
}

func (l *levelImpl) TileReader(col, row int) (io.ReadCloser, error) {
	idx, err := l.linearIndex(col, row)
	if err != nil {
		return nil, err
	}
	entry := l.tiler.tileOffsets[idx]
	if entry.Offset == NullTile || entry.Size == 0 {
		return nil, &opentile.TileError{
			Level: l.apiIndex,
			X:     col,
			Y:     row,
			Err:   opentile.ErrSparseTile,
		}
	}
	sr := io.NewSectionReader(l.tiler.r, int64(entry.Offset), int64(entry.Size))
	return io.NopCloser(sr), nil
}

func (l *levelImpl) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		grid := l.Grid()
		for r := 0; r < grid.H; r++ {
			for c := 0; c < grid.W; c++ {
				if ctx.Err() != nil {
					return
				}
				bytes, err := l.Tile(c, r)
				if !yield(opentile.TilePos{X: c, Y: r}, opentile.TileResult{Bytes: bytes, Err: err}) {
					return
				}
			}
		}
	}
}
