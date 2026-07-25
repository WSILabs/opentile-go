package dzi

import (
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strconv"

	opentile "github.com/wsilabs/opentile-go"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// level is the per-level tile-read engine for a bare DZI pyramid. Tiles are
// individual files under <filesDir>/<dziLevel>/<col>_<row>.<format>, read via
// os.ReadFile. When overlap>0, content cells must be border-cropped from the
// stored tile; the regionLayout/subtileLayout methods on Tiler delegate here.
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
	overlap  int // manifest Overlap; 0 = clean grid (fast path)
}

// tileOrigin returns the content cell's top-left in level pixels.
func (l *level) tileOrigin(col, row int) (x, y int, ok bool) {
	if !l.inBounds(col, row) {
		return 0, 0, false
	}
	return col * l.tileSize, row * l.tileSize, true
}

// tilesIntersecting returns the content cells overlapping [x,y,x+w,y+h).
func (l *level) tilesIntersecting(x, y, w, h int) []struct{ Col, Row int } {
	if w <= 0 || h <= 0 {
		return nil
	}
	c0, r0 := x/l.tileSize, y/l.tileSize
	c1, r1 := (x+w-1)/l.tileSize, (y+h-1)/l.tileSize
	if c0 < 0 {
		c0 = 0
	}
	if r0 < 0 {
		r0 = 0
	}
	if c1 >= l.cols {
		c1 = l.cols - 1
	}
	if r1 >= l.rows {
		r1 = l.rows - 1
	}
	var out []struct{ Col, Row int }
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			out = append(out, struct{ Col, Row int }{c, r})
		}
	}
	return out
}

// stitchedSize gates the composite path: ok only when this level has overlap.
func (l *level) stitchedSize() (w, h int, ok bool) {
	return l.width, l.height, l.overlap > 0
}

// unitSize is the content cell size (TileSize × TileSize). Edge clipping is
// handled by the compositor's region clamp.
func (l *level) unitSize() (w, h int) { return l.tileSize, l.tileSize }

// subtileSource maps a content cell to its (same) stored tile plus the overlap
// crop origin within the decoded tile.
func (l *level) subtileSource(col, row int) (srcCol, srcRow, cropX, cropY int) {
	ox, oy, _, _ := idzi.ContentRect(col, row, l.width, l.height, l.tileSize, l.overlap)
	return col, row, ox, oy
}

// tilePath resolves (x, y) to the on-disk tile file path, using OS-native
// separators (filepath.Join) — the bare-DZI reader reads these via os.Open,
// unlike SZI which uses internal/dzi.TilePath for '/'-separated ZIP entries.
func (l *level) tilePath(x, y int) string {
	return filepath.Join(l.filesDir, strconv.Itoa(l.dziLevel), fmt.Sprintf("%d_%d.%s", x, y, l.format))
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
