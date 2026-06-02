package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestTileMapFull(t *testing.T) {
	// 3x2 grid (cols x rows), raster order, 6 frames.
	m := buildTileMap("TILED_FULL", 3, 2, 256, nil, 6)
	// frame index = ty*cols + tx
	for ty := 0; ty < 2; ty++ {
		for tx := 0; tx < 3; tx++ {
			if got := m[tileKey{tx, ty}]; got != ty*3+tx {
				t.Errorf("(%d,%d) -> %d, want %d", tx, ty, got, ty*3+tx)
			}
		}
	}
}

func TestTileMapSparse(t *testing.T) {
	// two frames: tile (0,5) and (1,5), 256px tiles, 1-based positions.
	pos := []idicom.FramePos{{Col: 1, Row: 1281}, {Col: 257, Row: 1281}}
	m := buildTileMap("TILED_SPARSE", 6, 6, 256, pos, 2)
	if m[tileKey{0, 5}] != 0 {
		t.Errorf("(0,5) -> %d, want 0", m[tileKey{0, 5}])
	}
	if m[tileKey{1, 5}] != 1 {
		t.Errorf("(1,5) -> %d, want 1", m[tileKey{1, 5}])
	}
}
