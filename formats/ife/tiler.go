package ife

import (
	"context"
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

	levelImpls := make([]*levelImpl, len(apiOrder))
	valueLevels := make([]opentile.Level, len(apiOrder))
	// L0 width comes from apiOrder[0] (finest resolution = first in API order),
	// which maps to fileOrder[len-1] (last entry in coarsest-first storage).
	l0FileIdx := len(apiOrder) - 1
	l0Width := int(fileOrder[l0FileIdx].XTiles) * TileSidePixels
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
		levelW := int(ext.XTiles) * TileSidePixels
		impl := &levelImpl{apiIndex: i, maxTileSize: int(maxSize)}
		levelImpls[i] = impl
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         opentile.Size{W: levelW, H: int(ext.YTiles) * TileSidePixels},
			TileSize:     opentile.Size{W: TileSidePixels, H: TileSidePixels},
			Grid:         opentile.Size{W: int(ext.XTiles), H: int(ext.YTiles)},
			Compression:  compression,
			Downsample:   float64(l0Width) / float64(levelW),
		}
	}
	images := []opentile.Pyramid{{Name: "", Index: 0, Levels: valueLevels}}
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
		levelImpls:       levelImpls,
		images:           images,
	}
	// Wire the back-pointer to the tiler now that t is initialized.
	for _, impl := range levelImpls {
		impl.tiler = t
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
	levelImpls       []*levelImpl      // parallel to images[0].Levels; tile-read logic
	images           []opentile.Pyramid // value-type pyramid/level metadata

	md         Metadata
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format    { return opentile.FormatIFE }
func (t *tiler) Pyramids() []opentile.Pyramid { return t.images }

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *tiler) AssociatedImages() []opentile.AssociatedImage {
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

func (t *tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].warm()
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	return t.levelImpls[level].TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return t.levelImpls[level].Tiles(ctx)
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

func (l *levelImpl) extent() LayerExtent { return l.tiler.layerExtentsAPI[l.apiIndex] }

func (l *levelImpl) grid() opentile.Size {
	e := l.extent()
	return opentile.Size{W: int(e.XTiles), H: int(e.YTiles)}
}

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

func (l *levelImpl) Tiles(ctx context.Context) iter.Seq2[opentile.Point, opentile.TileResult] {
	return func(yield func(opentile.Point, opentile.TileResult) bool) {
		grid := l.grid()
		for r := 0; r < grid.H; r++ {
			for c := 0; c < grid.W; c++ {
				if ctx.Err() != nil {
					return
				}
				bytes, err := l.Tile(c, r)
				if !yield(opentile.Point{X: c, Y: r}, opentile.TileResult{Bytes: bytes, Err: err}) {
					return
				}
			}
		}
	}
}
