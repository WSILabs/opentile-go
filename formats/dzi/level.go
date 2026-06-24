package dzi

import (
	"context"
	"io"
	"iter"
	"os"

	opentile "github.com/wsilabs/opentile-go"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// level is the per-level tile-read engine for a bare DZI pyramid. Tiles are
// individual files under <filesDir>/<dziLevel>/<col>_<row>.<format>, read via
// os.ReadFile. Overlap is always 0 (Overlap>0 is rejected at open), so each
// stored tile is exactly its grid cell.
type level struct {
	filesDir string // absolute path to <base>_files
	format   string // manifest Format ("jpeg" / "png"), the tile file extension

	dziLevel    int // DZI-side level index (MaxLevel = full resolution)
	openTileIdx int // opentile-side level index (0 = full resolution)

	width    int
	height   int
	cols     int
	rows     int
	tileSize int
}

// tilePath resolves (x, y) to the on-disk tile file path.
func (l *level) tilePath(x, y int) string {
	return idzi.TilePath(l.filesDir, l.dziLevel, x, y, l.format)
}

// inBounds reports whether (x, y) is within the level's tile grid.
func (l *level) inBounds(x, y int) bool {
	return x >= 0 && x < l.cols && y >= 0 && y < l.rows
}

// Tile returns the raw on-disk tile bytes (a complete JPEG / PNG file).
func (l *level) Tile(x, y int) ([]byte, error) {
	if !l.inBounds(x, y) {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	b, err := os.ReadFile(l.tilePath(x, y))
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return b, nil
}

// TileInto reads tile (x, y) into dst and returns the byte count. dst must be
// at least the tile's on-disk size; otherwise returns 0, io.ErrShortBuffer.
func (l *level) TileInto(x, y int, dst []byte) (int, error) {
	if !l.inBounds(x, y) {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	f, err := os.Open(l.tilePath(x, y))
	if err != nil {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	size := int(info.Size())
	if len(dst) < size {
		return 0, io.ErrShortBuffer
	}
	n, err := io.ReadFull(f, dst[:size])
	if err != nil {
		return n, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileReader returns a streaming reader over the tile file. The caller closes it.
func (l *level) TileReader(x, y int) (io.ReadCloser, error) {
	if !l.inBounds(x, y) {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	f, err := os.Open(l.tilePath(x, y))
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return f, nil
}

// TileMaxSize returns a conservative upper bound on tile bytes: tileSize² × 4
// (an uncompressed RGBA tile). Compressed JPEG/PNG tiles are far smaller.
func (l *level) TileMaxSize() int { return l.tileSize * l.tileSize * 4 }

// Tiles iterates all tile positions in row-major order.
func (l *level) Tiles(ctx context.Context) iter.Seq2[opentile.Point, opentile.TileResult] {
	return func(yield func(opentile.Point, opentile.TileResult) bool) {
		for y := 0; y < l.rows; y++ {
			for x := 0; x < l.cols; x++ {
				if err := ctx.Err(); err != nil {
					yield(opentile.Point{X: x, Y: y}, opentile.TileResult{Err: err})
					return
				}
				b, err := l.Tile(x, y)
				if !yield(opentile.Point{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}
