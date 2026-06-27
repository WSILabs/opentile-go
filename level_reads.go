package opentile

import (
	"context"
	"io"
	"iter"

	"github.com/wsilabs/opentile-go/decoder"
)

// This file holds the v1.0 receiver-method read API on *Level. Each
// method delegates to the owning Slide's Image* read method using the
// level's own (PyramidIndex, Index) coordinates, which ensurePyramids
// guarantees are populated. A *Level obtained via navigation (Slide.Level,
// Slide.Levels, Pyramid.Level, Pyramid.Levels) carries its back-reference;
// calling these on a zero-value Level (slide == nil) panics, by design —
// a metadata-only Level cannot read pixels.

// Tile returns the compressed tile bytes at tile coordinates (tx, ty).
func (l *Level) Tile(tx, ty int) ([]byte, error) {
	return l.slide.r.ImageRawTile(l.PyramidIndex, l.Index, tx, ty)
}

// TileInto fills dst with the compressed tile bytes at (tx, ty) and
// returns the byte count written.
func (l *Level) TileInto(tx, ty int, dst []byte) (int, error) {
	return l.slide.r.ImageRawTileInto(l.PyramidIndex, l.Index, tx, ty, dst)
}

// TileReader returns a streaming reader for the compressed tile at
// (tx, ty).
func (l *Level) TileReader(tx, ty int) (io.ReadCloser, error) {
	return l.slide.r.ImageTileReader(l.PyramidIndex, l.Index, tx, ty)
}

// TileMaxSize returns the upper bound on compressed tile byte size at
// this level — for sizing buffers passed to TileInto.
func (l *Level) TileMaxSize() int {
	return l.slide.r.ImageTileMaxSize(l.PyramidIndex, l.Index)
}

// TilePrefix returns the shared codec prefix (e.g. JPEG tables) for this
// level, or nil if the codec has no prefix.
func (l *Level) TilePrefix() []byte {
	return l.slide.r.ImageTilePrefix(l.PyramidIndex, l.Index)
}

// TileBodyInto fills dst with the tile body bytes (no prefix) at (tx, ty).
func (l *Level) TileBodyInto(tx, ty int, dst []byte) (int, error) {
	return l.slide.r.ImageTileBodyInto(l.PyramidIndex, l.Index, tx, ty, dst)
}

// TileBodyMaxSize returns the upper bound on tile-body (no prefix) byte
// size at this level.
func (l *Level) TileBodyMaxSize() int {
	return l.slide.r.ImageTileBodyMaxSize(l.PyramidIndex, l.Index)
}

// DecodedTile returns the decoded pixels for the tile at (tx, ty).
func (l *Level) DecodedTile(tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.imageDecodedTile(l.PyramidIndex, l.Index, tx, ty, opts...)
}

// DecodedTileInto decodes the tile at (tx, ty) into the caller-provided
// dst.
func (l *Level) DecodedTileInto(tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	return l.slide.imageDecodedTileInto(l.PyramidIndex, l.Index, tx, ty, dst, opts...)
}

// ReadRegion returns the decoded pixels for rectangle r at this level's
// own resolution.
func (l *Level) ReadRegion(r Region, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.imageReadRegion(l.PyramidIndex, l.Index, r, opts...)
}

// ReadRegionInto decodes a region into the caller-provided dst. origin is
// at this level's resolution; the region size is taken from dst.
func (l *Level) ReadRegionInto(origin Point, dst *decoder.Image, opts ...DecodeOption) error {
	return l.slide.imageReadRegionInto(l.PyramidIndex, l.Index, origin, dst, opts...)
}

// Tiles returns a row-major range-over-function iterator over all tiles
// at this level.
func (l *Level) Tiles(ctx context.Context) iter.Seq2[Point, TileResult] {
	return l.slide.r.ImageRangeTiles(ctx, l.PyramidIndex, l.Index)
}

// Warm pre-warms the page cache for this level. Hint-only.
func (l *Level) Warm() error {
	return l.slide.r.WarmLevel(l.PyramidIndex, l.Index)
}

// TIFFTags returns the parsed TIFF tags of this level's backing IFD.
// ok=false for non-TIFF formats.
func (l *Level) TIFFTags() (TIFFTags, bool) {
	return l.slide.imageLevelTIFFTags(l.PyramidIndex, l.Index)
}

// StitchedTile returns a clean, non-overlapping display tile from the canonical
// grid StitchedGrid() (== ceil(Size/TileSize)). For overlapping levels
// (Overlapping == true: stitched BIF) it composites the stitched image so the
// returned tile is a true partition of Size; for every other format it is
// exactly DecodedTile. Pixels match ReadRegion over the tile's rectangle.
//
// Use this (with StitchedGrid) for display/rendering. Use Tile / DecodedTile +
// Grid only when you want the raw stored (possibly overlapping) tiles, e.g. for
// faithful transcoding. Scale > 1 is unsupported on overlapping levels (use the
// Pyramid's ReadRegionScaled / ScaledStrips); it returns ErrUnsupportedScale.
func (l *Level) StitchedTile(tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.imageStitchedTile(l.PyramidIndex, l.Index, tx, ty, opts...)
}

// StitchedTileInto is the allocation-free form of StitchedTile: it composites
// the display tile (tx, ty) into the caller-provided dst. The composite is done
// in dst's own pixel format, and dst is white-filled before compositing. Reuse
// one dst across a display-grid traversal to avoid a per-tile allocation.
//
// The DISPLAY TILE SIZE is dst's own dimensions: for an OVERLAPPING level
// (stitched BIF) the composite is region-based, so dst may be any size — a
// viewer can render uniform/square display tiles (e.g. 512×512) even though the
// level stores non-square tiles (legacy BIF is 1024×1360). Pair it with
// StitchedGridFor(dstSize) for the matching iteration grid; the result is
// pixel-identical to ReadRegion over [tx*dst.W, ty*dst.H, dst.W, dst.H]. For a
// NON-overlapping level it behaves like DecodedTileInto, so dst must be the
// level's TileSize (retiling other formats to a custom size is not in scope —
// they store square tiles; use ReadRegion for an arbitrary rectangle).
func (l *Level) StitchedTileInto(tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	return l.slide.imageStitchedTileInto(l.PyramidIndex, l.Index, tx, ty, dst, opts...)
}

// StitchedGrid is the canonical display grid, ceil(Size/TileSize) per axis — a
// clean partition of Size. Equals Grid for non-overlapping levels; for an
// overlapping level it is the grid that tiles Size (whereas Grid stays the raw
// overlapping grid). Iterate StitchedGrid with StitchedTile to render.
func (l *Level) StitchedGrid() Size {
	return Size{
		W: ceilDiv(l.Size.W, l.TileSize.W),
		H: ceilDiv(l.Size.H, l.TileSize.H),
	}
}

// StitchedGridFor is StitchedGrid for a caller-chosen display tile size:
// ceil(Size/tile) per axis. Iterate it with StitchedTileInto using a tile-sized
// dst to render display tiles at a uniform/square size independent of the stored
// (possibly non-square) TileSize — e.g. legacy BIF stores 1024×1360 but a viewer
// can render 512×512. The caller-chosen size is honored on overlapping levels;
// on non-overlapping levels StitchedTileInto still requires dst == TileSize, so
// use StitchedGrid there. Returns the zero Size for a non-positive tile.
func (l *Level) StitchedGridFor(tile Size) Size {
	if tile.W <= 0 || tile.H <= 0 {
		return Size{}
	}
	return Size{
		W: ceilDiv(l.Size.W, tile.W),
		H: ceilDiv(l.Size.H, tile.H),
	}
}

// TileContentRect returns the content sub-rectangle within the decoded tile
// (col,row) — its in-tile origin and size. For OverlapBordered levels the
// origin is the overlap border to skip ((col>0?ov:0, row>0?ov:0)) and the size
// is the content cell clipped at the level's right/bottom edge; a consumer
// crops a decoded tile to this rect to drop the redundant overlap. For
// OverlapNone it is the full clipped cell at origin (0,0) — a universal "where
// is the real content" answer. ok is false for OverlapStitched (Grid does not
// tile Size — use the region API) and for an out-of-grid (col,row).
func (l *Level) TileContentRect(col, row int) (Region, bool) {
	if l.OverlapMode == OverlapStitched {
		return Region{}, false
	}
	if col < 0 || row < 0 || col >= l.Grid.W || row >= l.Grid.H {
		return Region{}, false
	}
	var ox, oy int
	if l.OverlapMode == OverlapBordered {
		if col > 0 {
			ox = l.TileOverlap.X
		}
		if row > 0 {
			oy = l.TileOverlap.Y
		}
	}
	w := l.TileSize.W
	if rem := l.Size.W - col*l.TileSize.W; rem < w {
		w = rem
	}
	h := l.TileSize.H
	if rem := l.Size.H - row*l.TileSize.H; rem < h {
		h = rem
	}
	return Region{Origin: Point{X: ox, Y: oy}, Size: Size{W: w, H: h}}, true
}
