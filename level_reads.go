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
	return l.slide.ImageRawTile(l.PyramidIndex, l.Index, tx, ty)
}

// TileInto fills dst with the compressed tile bytes at (tx, ty) and
// returns the byte count written.
func (l *Level) TileInto(tx, ty int, dst []byte) (int, error) {
	return l.slide.ImageRawTileInto(l.PyramidIndex, l.Index, tx, ty, dst)
}

// TileReader returns a streaming reader for the compressed tile at
// (tx, ty).
func (l *Level) TileReader(tx, ty int) (io.ReadCloser, error) {
	return l.slide.ImageTileReader(l.PyramidIndex, l.Index, tx, ty)
}

// TileMaxSize returns the upper bound on compressed tile byte size at
// this level — for sizing buffers passed to TileInto.
func (l *Level) TileMaxSize() int {
	return l.slide.ImageTileMaxSize(l.PyramidIndex, l.Index)
}

// TilePrefix returns the shared codec prefix (e.g. JPEG tables) for this
// level, or nil if the codec has no prefix.
func (l *Level) TilePrefix() []byte {
	return l.slide.ImageTilePrefix(l.PyramidIndex, l.Index)
}

// TileBodyInto fills dst with the tile body bytes (no prefix) at (tx, ty).
func (l *Level) TileBodyInto(tx, ty int, dst []byte) (int, error) {
	return l.slide.ImageTileBodyInto(l.PyramidIndex, l.Index, tx, ty, dst)
}

// TileBodyMaxSize returns the upper bound on tile-body (no prefix) byte
// size at this level.
func (l *Level) TileBodyMaxSize() int {
	return l.slide.ImageTileBodyMaxSize(l.PyramidIndex, l.Index)
}

// DecodedTile returns the decoded pixels for the tile at (tx, ty).
func (l *Level) DecodedTile(tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.ImageDecodedTile(l.PyramidIndex, l.Index, tx, ty, opts...)
}

// DecodedTileInto decodes the tile at (tx, ty) into the caller-provided
// dst.
func (l *Level) DecodedTileInto(tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	return l.slide.ImageDecodedTileInto(l.PyramidIndex, l.Index, tx, ty, dst, opts...)
}

// ReadRegion returns the decoded pixels for rectangle r at this level's
// own resolution.
func (l *Level) ReadRegion(r Region, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.ImageReadRegion(l.PyramidIndex, l.Index, r, opts...)
}

// ReadRegionInto decodes a region into the caller-provided dst. origin is
// at this level's resolution; the region size is taken from dst.
func (l *Level) ReadRegionInto(origin Point, dst *decoder.Image, opts ...DecodeOption) error {
	return l.slide.ImageReadRegionInto(l.PyramidIndex, l.Index, origin, dst, opts...)
}

// Tiles returns a row-major range-over-function iterator over all tiles
// at this level.
func (l *Level) Tiles(ctx context.Context) iter.Seq2[Point, TileResult] {
	return l.slide.ImageRangeTiles(ctx, l.PyramidIndex, l.Index)
}

// Warm pre-warms the page cache for this level. Hint-only.
func (l *Level) Warm() error {
	return l.slide.ImageWarmLevel(l.PyramidIndex, l.Index)
}

// TIFFTags returns the parsed TIFF tags of this level's backing IFD.
// ok=false for non-TIFF formats.
func (l *Level) TIFFTags() (TIFFTags, bool) {
	return l.slide.ImageLevelTIFFTags(l.PyramidIndex, l.Index)
}
