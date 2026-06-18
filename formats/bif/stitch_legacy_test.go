package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// legacyEI builds a single-AOI legacy EncodeInfo (no Frames) with uniform
// per-gap overlaps: every horizontal join overlaps ox, every vertical join oy.
// Tile1/Tile2 are 1-based serpentine indices (legacy convention). All joins
// FlagJoined, Confidence=100 unless overridden by the caller afterwards.
func legacyEI(cols, rows, ox, oy int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	serp := func(c, r int) int { return imageToSerpentine(c, r, cols, rows) + 1 } // 1-based
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: serp(c, r), Tile2: serp(c+1, r), OverlapX: ox, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "UP", Tile1: serp(c, r), Tile2: serp(c, r+1), OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii}}
}

func TestBuildLegacyLayoutUniformOverlap(t *testing.T) {
	ei := legacyEI(3, 2, 100, 100)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	for col, want := range []int{0, 900, 1800} {
		x, _, ok := l.TileOrigin(col, 0)
		if !ok || x != want {
			t.Errorf("X[%d] = (%d,%v), want %d", col, x, ok, want)
		}
	}
	if _, y, _ := l.TileOrigin(0, 1); y != 900 {
		t.Errorf("Y[1] = %d, want 900", y)
	}
	if l.Width != 2800 || l.Height != 1900 {
		t.Errorf("dims = %dx%d, want 2800x1900", l.Width, l.Height)
	}
}

func TestBuildLegacyLayoutEmptyGapGlobalFill(t *testing.T) {
	ei := legacyEI(3, 1, 100, 0)
	js := ei.ImageInfos[0].Joints[:0:0]
	for _, j := range ei.ImageInfos[0].Joints {
		ac, _ := serpentineToImage(j.Tile1-1, 3, 1)
		bc, _ := serpentineToImage(j.Tile2-1, 3, 1)
		if min(ac, bc) == 1 {
			continue
		}
		js = append(js, j)
	}
	ei.ImageInfos[0].Joints = js
	l := BuildLayout(StitchInput{Cols: 3, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	x2, _, _ := l.TileOrigin(2, 0)
	if x2 != 1800 {
		t.Errorf("X[2] = %d, want 1800 (empty gap1 must use global mean 100, not 0 → else 1900)", x2)
	}
}

func TestBuildLegacyLayoutDeadAndLowConfExcluded(t *testing.T) {
	ei := legacyEI(2, 1, 100, 0)
	ei.ImageInfos[0].Joints[0].FlagJoined = false
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	if x, _, _ := l.TileOrigin(1, 0); x != 1000 {
		t.Errorf("dead-only joins must decline to naive (X[1]=1000), got %d", x)
	}
	ei2 := legacyEI(2, 1, 100, 0)
	ei2.ImageInfos[0].Joints[0].Confidence = 50
	l2 := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei2, Generation: GenerationLegacyIScan})
	if x, _, _ := l2.TileOrigin(1, 0); x != 1000 {
		t.Errorf("sub-cutoff joins must be excluded (X[1]=1000), got %d", x)
	}
}

func TestBuildLegacyLayoutGatingDPUntouched(t *testing.T) {
	ei := legacyEI(2, 1, 100, 0)
	if buildLegacyLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationSpecCompliant}) != nil {
		t.Error("buildLegacyLayout must return nil for GenerationSpecCompliant")
	}
}
