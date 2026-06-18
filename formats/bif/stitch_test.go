package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// singleAOIEncodeInfo builds a Ver=2 EncodeInfo for one AOI: a cols×rows grid
// with row-major Frames, uniform RIGHT overlap=ox between horizontally adjacent
// tiles and uniform DOWN overlap=oy between vertically adjacent tiles. Tile
// indices are row-major (matching the Frame storage order) for test simplicity.
func singleAOIEncodeInfo(cols, rows, ox, oy int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	idx := func(c, r int) int { return r*cols + c }
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			ii.Frames = append(ii.Frames, bifxml.Frame{Col: c, Row: r})
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: idx(c, r), Tile2: idx(c+1, r), OverlapX: ox, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "DOWN", Tile1: idx(c, r), Tile2: idx(c, r+1), OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii}}
}

func TestBuildDPLayoutHorizontalOverlap(t *testing.T) {
	// 3 cols × 2 rows, tile 1024, RIGHT overlap 120, no vertical overlap.
	ei := singleAOIEncodeInfo(3, 2, 120, 0)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// Column origins: 0, 1024-120=904, 904+904=1808. Width = 1808+1024 = 2832,
	// rounded up to a tile multiple (3*1024=3072) by top/right white padding.
	wantCols := []int{0, 904, 1808}
	for c, wantX := range wantCols {
		x, _, ok := l.TileOrigin(c, 0)
		if !ok || x != wantX {
			t.Errorf("TileOrigin(%d,0).X = (%d,%v), want %d", c, x, ok, wantX)
		}
	}
	if l.Width != 3072 {
		t.Errorf("Width = %d, want 3072 (content 2832 padded up to tile multiple)", l.Width)
	}
	if l.Height != 2048 {
		t.Errorf("Height = %d, want 2048", l.Height)
	}
}

func TestBuildDPLayoutTwoAOIsWithOrigins(t *testing.T) {
	// Two single-tile AOIs (1024 tiles, no internal overlap). AOI0 at origin
	// (0,0), AOI1 at OriginX=1024 → side by side, total 2048×1024.
	mk := func(aoiIdx int) bifxml.ImageInfo {
		return bifxml.ImageInfo{AOIScanned: true, AOIIndex: aoiIdx, NumCols: 1, NumRows: 1,
			Frames: []bifxml.Frame{{Col: 0, Row: 0}}}
	}
	ei := &bifxml.EncodeInfo{Ver: 2,
		ImageInfos: []bifxml.ImageInfo{mk(0), mk(1)},
		AoiOrigins: []bifxml.AoiOrigin{{Index: 0, OriginX: 0, OriginY: 0}, {Index: 1, OriginX: 1024, OriginY: 0}},
	}
	// NOTE: grid Cols/Rows here is the *image* grid the level reports. With two
	// 1-tile AOIs side by side the image grid is 2×1.
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	if l.Width != 2048 || l.Height != 1024 {
		t.Fatalf("dims = %dx%d, want 2048x1024", l.Width, l.Height)
	}
}

func TestBuildDPLayoutGatingDeclinesToNaive(t *testing.T) {
	// Ver<2 → naive; legacy generation → naive even with joints.
	verLow := singleAOIEncodeInfo(2, 1, 120, 0)
	verLow.Ver = 1
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: verLow, Generation: GenerationSpecCompliant})
	if x, _, _ := l.TileOrigin(1, 0); x != 1024 {
		t.Errorf("Ver<2 must fall back to naive (x=1024), got %d", x)
	}
	legacy := singleAOIEncodeInfo(2, 1, 120, 0)
	l2 := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: legacy, Generation: GenerationLegacyIScan})
	if x, _, _ := l2.TileOrigin(1, 0); x != 1024 {
		t.Errorf("legacy generation must fall back to naive (x=1024), got %d", x)
	}
}

func TestBuildDPLayoutDropsLowConfidenceJoint(t *testing.T) {
	ei := singleAOIEncodeInfo(2, 1, 120, 0)
	ei.ImageInfos[0].Joints[0].Confidence = 50 // not trusted
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// Only low-confidence joints → no confident joint → declines to naive.
	if x, _, _ := l.TileOrigin(1, 0); x != 1024 {
		t.Errorf("low-confidence joint must not compact (x=1024), got %d", x)
	}
}

func TestBuildLayoutNaiveNoEncodeInfo(t *testing.T) {
	in := StitchInput{Cols: 3, Rows: 2, TileW: 1024, TileH: 1024, EncodeInfo: nil, Generation: GenerationLegacyIScan}
	l := BuildLayout(in)
	if l.Width != 3*1024 || l.Height != 2*1024 {
		t.Fatalf("dims = %dx%d, want 3072x2048", l.Width, l.Height)
	}
	x, y, ok := l.TileOrigin(2, 1)
	if !ok || x != 2*1024 || y != 1*1024 {
		t.Errorf("TileOrigin(2,1) = (%d,%d,%v), want (2048,1024,true)", x, y, ok)
	}
	if _, _, ok := l.TileOrigin(3, 0); ok {
		t.Errorf("TileOrigin(3,0) should be out of grid")
	}
}

func TestLayoutTilesIntersecting(t *testing.T) {
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 100, TileH: 100, Generation: GenerationLegacyIScan})
	got := l.TilesIntersecting(50, 50, 100, 100) // spans tiles (0,0)(1,0)(0,1)(1,1)
	if len(got) != 4 {
		t.Fatalf("intersecting = %d tiles, want 4: %+v", len(got), got)
	}
}
