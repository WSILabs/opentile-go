package dzi

import (
	"fmt"
	"math"
)

// MaxLevel returns the deepest pyramid level index for an image of
// the given dimensions. Per Microsoft spec: each level is
// 2^level × 2^level (logical), and the image is laid out at the
// smallest level whose 2^level dimension is >= max(width, height).
//
// Examples (from spec page 13):
//
//	max(W,H) = 234298 → MaxLevel = ceil(log2(234298)) = 18
//	max(W,H) = 2967   → MaxLevel = ceil(log2(2967))   = 12
//	max(W,H) = 1      → MaxLevel = 0
//
// Total level count = MaxLevel(w, h) + 1.
func MaxLevel(width, height int) int {
	if width < 1 || height < 1 {
		return 0
	}
	max := width
	if height > max {
		max = height
	}
	if max == 1 {
		return 0
	}
	return int(math.Ceil(math.Log2(float64(max))))
}

// LevelDims returns the pixel dimensions of the given level for an
// image whose deepest level has the given full-resolution dims.
//
// The deepest level (MaxLevel) is at the full Width/Height. Each
// level above (toward 0) halves the previous level's dimensions,
// rounding up.
//
// Examples for a 2220×2967 image (MaxLevel = 12):
//
//	level 12: 2220×2967  (full)
//	level 11: 1110×1484
//	level 10:  555× 742
//	...
//	level  0:    1×   1
func LevelDims(width, height, level int) (w, h int) {
	maxLevel := MaxLevel(width, height)
	if level >= maxLevel {
		return width, height
	}
	if level < 0 {
		return 1, 1
	}
	// Halve from full dims down to the requested level.
	delta := maxLevel - level
	w = width
	h = height
	for i := 0; i < delta; i++ {
		w = (w + 1) / 2
		h = (h + 1) / 2
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
	}
	return w, h
}

// GridDims returns the tile-grid dimensions (cols × rows) for a
// level whose pixel dimensions are levelW × levelH and whose tile
// size is tileSize.
//
//	cols = ceil(levelW / tileSize)
//	rows = ceil(levelH / tileSize)
func GridDims(levelW, levelH, tileSize int) (cols, rows int) {
	if tileSize <= 0 {
		return 0, 0
	}
	cols = (levelW + tileSize - 1) / tileSize
	rows = (levelH + tileSize - 1) / tileSize
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

// TilePath returns the on-disk path of a tile within a DZI pyramid
// rooted at rootDir. Paths follow the Microsoft spec convention
// "<rootDir>/<level>/<col>_<row>.<format>" — note column-then-row,
// NOT row-then-column.
//
// Example: TilePath("CMU-1_files", 12, 5, 8, "jpeg") returns
// "CMU-1_files/12/5_8.jpeg".
func TilePath(rootDir string, level, col, row int, format string) string {
	return fmt.Sprintf("%s/%d/%d_%d.%s", rootDir, level, col, row, format)
}
