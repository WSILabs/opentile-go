package szi

import (
	"archive/zip"
	"context"
	"fmt"
	stdimage "image"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

// level is the opentile.Level implementation for one SZI/DZI
// pyramid level.
//
// SZI tiles are self-contained JPEG / PNG files stored as
// uncompressed (zip.Store) ZIP entries. Each Tile call resolves
// (x, y) to a ZIP entry via dzi.TilePath + the Tiler's central-
// directory map, then reads the entry's bytes verbatim.
//
// SZI/DZI tiles do not carry a shared splice prefix (each tile is
// a complete bitstream), so TilePrefix returns nil and TileBodyInto
// is functionally equivalent to TileInto per the v0.13
// non-applicable convention.
type level struct {
	t           *Tiler
	dziLevel    int // DZI-side level index (0 = 1×1; MaxLevel = full)
	openTileIdx int // opentile-side level index (0 = full resolution)
	pyrIndex    int // position in the pyramid (always equals openTileIdx for single-image SZI)

	width    int // pixel width at this level
	height   int // pixel height at this level
	cols     int
	rows     int
	tileSize int // standard tile dimension (manifest TileSize, typically 256 / 512)

	compression opentile.Compression
}

// Index returns the opentile-side level index (0 = highest
// resolution).
func (l *level) Index() int { return l.openTileIdx }

// PyramidIndex returns the position of this level within its
// parent Image's pyramid. Always equal to Index() for SZI
// (single-image format).
func (l *level) PyramidIndex() int { return l.pyrIndex }

// Size returns the level's pixel dimensions.
func (l *level) Size() opentile.Size { return opentile.Size{W: l.width, H: l.height} }

// TileSize returns the nominal tile dimension for this level.
// Border tiles encode smaller pixel regions, but TileSize is the
// upper bound and the value used to derive the grid.
func (l *level) TileSize() opentile.Size { return opentile.Size{W: l.tileSize, H: l.tileSize} }

// Grid returns the tile-grid dimensions (cols × rows).
func (l *level) Grid() opentile.Size { return opentile.Size{W: l.cols, H: l.rows} }

// TileOverlap returns the inter-tile pixel overlap. SZI-supported
// DZI manifests typically declare Overlap=0; this is currently
// hardcoded zero per Q-decision (full overlap support is deferred).
func (l *level) TileOverlap() stdimage.Point { return stdimage.Point{} }

// Compression reports the codec of the on-disk tile bitstream
// (JPEG or PNG, per the manifest's Format attribute).
func (l *level) Compression() opentile.Compression { return l.compression }

// MPP returns zero. SZI surfaces resolution metadata at the
// Tiler.Metadata level (via scan-properties.xml), not per-level.
// T4 may revisit if scan-properties exposes a per-level MPP scale.
func (l *level) MPP() opentile.MPP { return opentile.MPP{} }

// FocalPlane returns 0 — SZI is single-Z brightfield.
func (l *level) FocalPlane() float64 { return 0 }

// TileMaxSize returns a conservative upper bound on tile bytes
// produced by Tile / TileInto. Real on-disk JPEG / PNG tiles are
// far smaller; this overstates safely. Used by buffer-pool
// consumers that want a single pool sized for the worst case.
func (l *level) TileMaxSize() int {
	// tileSize × tileSize × 4 bytes/pixel: conservative upper bound
	// for an uncompressed RGBA tile. Compressed tiles are smaller.
	return l.tileSize * l.tileSize * 4
}

// TilePrefix returns nil. SZI/DZI tiles are complete self-contained
// bitstreams; there is no shared per-level prefix to deduplicate.
func (l *level) TilePrefix() []byte { return nil }

// TileBodyInto delegates to TileInto (TilePrefix is nil; the
// "body" is the entire tile).
func (l *level) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize delegates to TileMaxSize (TilePrefix is nil).
func (l *level) TileBodyMaxSize() int { return l.TileMaxSize() }

// Tile returns the raw on-disk tile bytes (a complete JPEG or PNG
// file). Allocates a fresh slice; high-RPS callers should prefer
// TileInto with a pooled buffer.
func (l *level) Tile(x, y int) ([]byte, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return nil, err
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	defer rc.Close()
	buf := make([]byte, entry.UncompressedSize64)
	if _, err := io.ReadFull(rc, buf); err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return buf, nil
}

// TileInto reads tile (x, y) into dst and returns the byte count.
// dst must be at least the entry's UncompressedSize64; if smaller,
// returns 0, io.ErrShortBuffer.
func (l *level) TileInto(x, y int, dst []byte) (int, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return 0, err
	}
	size := int(entry.UncompressedSize64)
	if len(dst) < size {
		return 0, io.ErrShortBuffer
	}
	rc, err := entry.Open()
	if err != nil {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	defer rc.Close()
	n, err := io.ReadFull(rc, dst[:size])
	if err != nil {
		return n, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileAt is the multi-dim entry point. SZI is 2D-only; non-zero
// Z/C/T yields ErrDimensionUnavailable.
func (l *level) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{
			Level: l.openTileIdx,
			X:     coord.X,
			Y:     coord.Y,
			Err:   opentile.ErrDimensionUnavailable,
		}
	}
	return l.Tile(coord.X, coord.Y)
}

// TileReader returns a streaming reader for tile (x, y). The
// underlying ZIP entry is uncompressed-stored, so this is a
// thin reader over the file region (no inflate).
func (l *level) TileReader(x, y int) (io.ReadCloser, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return nil, err
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return rc, nil
}

// Tiles iterates all tile positions in row-major order.
func (l *level) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		for y := 0; y < l.rows; y++ {
			for x := 0; x < l.cols; x++ {
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

// tileEntry resolves (x, y) to the corresponding ZIP entry.
// Out-of-grid coords return ErrTileOutOfBounds wrapped in
// TileError. Missing entries within the addressable range
// return ErrCorruptArchive (per Q2: SZI spec forbids sparse
// images, so a missing tile in valid range is a corrupt file).
func (l *level) tileEntry(x, y int) (*zip.File, error) {
	if x < 0 || x >= l.cols || y < 0 || y >= l.rows {
		return nil, &opentile.TileError{
			Level: l.openTileIdx,
			X:     x,
			Y:     y,
			Err:   opentile.ErrTileOutOfBounds,
		}
	}
	path := dzi.TilePath(l.t.filesDir, l.dziLevel, x, y, l.t.manifest.Format)
	entry, ok := l.t.entries[path]
	if !ok {
		return nil, &opentile.TileError{
			Level: l.openTileIdx,
			X:     x,
			Y:     y,
			Err:   fmt.Errorf("missing tile %s: %w", path, ErrCorruptArchive),
		}
	}
	return entry, nil
}
