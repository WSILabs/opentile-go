package dzi

import "testing"

func TestMaxLevel(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want int
	}{
		// Spec worked example (page 13): 234298 → 18.
		{234298, 234298, 18},
		// Spec single-tile range: levels 0-8 are all 1 tile; level 8
		// largest dim is between 129 and 256 px (= 2^7+1 to 2^8).
		{256, 256, 8},
		{129, 129, 8},
		{128, 128, 7},
		// CMU-1.szi dimensions: 2220x2967 → MaxLevel = 12.
		{2220, 2967, 12},
		// Grundium dimensions: 147456x81920 → MaxLevel = 18.
		// log2(147456) ≈ 17.17 → ceil = 18.
		{147456, 81920, 18},
		// Trivial cases.
		{1, 1, 0},
		{2, 1, 1},
	} {
		t.Run("", func(t *testing.T) {
			if got := MaxLevel(tc.w, tc.h); got != tc.want {
				t.Errorf("MaxLevel(%d, %d) = %d, want %d", tc.w, tc.h, got, tc.want)
			}
		})
	}
}

func TestLevelDims_CMU1(t *testing.T) {
	// 2220x2967 image. MaxLevel = 12 (full); each level halves.
	for _, tc := range []struct {
		level int
		w, h  int
	}{
		{12, 2220, 2967}, // full
		{11, 1110, 1484}, // (2220+1)/2 = 1110, (2967+1)/2 = 1484
		{10, 555, 742},
		{9, 278, 371},
		{8, 139, 186},
		{0, 1, 1}, // bottom level always 1×1
	} {
		t.Run("", func(t *testing.T) {
			w, h := LevelDims(2220, 2967, tc.level)
			if w != tc.w || h != tc.h {
				t.Errorf("LevelDims(2220, 2967, %d) = %dx%d, want %dx%d",
					tc.level, w, h, tc.w, tc.h)
			}
		})
	}
}

func TestGridDims(t *testing.T) {
	// CMU-1 L12 = 2220x2967, TileSize=256 → 9x12 grid (9*256=2304, 12*256=3072).
	if cols, rows := GridDims(2220, 2967, 256); cols != 9 || rows != 12 {
		t.Errorf("GridDims(2220, 2967, 256) = %dx%d, want 9x12", cols, rows)
	}
	// Grundium L18 = 147456x81920, TileSize=512 → 288x160.
	if cols, rows := GridDims(147456, 81920, 512); cols != 288 || rows != 160 {
		t.Errorf("GridDims(147456, 81920, 512) = %dx%d, want 288x160", cols, rows)
	}
	// Levels 0-8 have a single tile.
	if cols, rows := GridDims(1, 1, 256); cols != 1 || rows != 1 {
		t.Errorf("GridDims(1, 1, 256) = %dx%d, want 1x1", cols, rows)
	}
}

func TestTilePath(t *testing.T) {
	// Microsoft spec verbatim: "<col>_<row>.<format>" — column FIRST.
	if got := TilePath("CMU-1_files", 12, 5, 8, "jpeg"); got != "CMU-1_files/12/5_8.jpeg" {
		t.Errorf("TilePath = %q", got)
	}
	if got := TilePath("scan_618__files", 18, 287, 159, "jpeg"); got != "scan_618__files/18/287_159.jpeg" {
		t.Errorf("TilePath = %q", got)
	}
}
