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
	return legacyEICross(cols, rows, ox, oy, 0, 0)
}

// legacyEICross is legacyEI plus CROSS-axis overlap components (#68): every
// horizontal (RIGHT) join also carries OverlapY=crossY (the per-column vertical
// drift), and every vertical (UP) join also carries OverlapX=crossX (the per-row
// horizontal drift). crossX=crossY=0 reduces to the original separable layout.
func legacyEICross(cols, rows, ox, oy, crossX, crossY int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	serp := func(c, r int) int { return imageToSerpentine(c, r, cols, rows) + 1 } // 1-based
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: serp(c, r), Tile2: serp(c+1, r), OverlapX: ox, OverlapY: crossY, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "UP", Tile1: serp(c, r), Tile2: serp(c, r+1), OverlapX: crossX, OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii},
		AoiOrigins: []bifxml.AoiOrigin{{Index: 0, OriginX: 0, OriginY: 0}}}
}

// TestBuildLegacyLayoutCrossAxisDrift pins the #68 cross-axis model: horizontal
// joins carry a vertical OverlapY that drifts each column up, vertical joins
// carry a horizontal OverlapX that drifts each row left. A join's displacement
// vector is (tw−OverlapX, −OverlapY) horizontally and (−OverlapX, th−OverlapY)
// vertically, so tile (c,r) lands at (X[c]+xRow[r], Y[r]+yCol[c]). Cross
// baselines are normalized so their min is 0.
func TestBuildLegacyLayoutCrossAxisDrift(t *testing.T) {
	// 3×2, tile 1000, in-axis overlap 100 both axes, crossX=6 (per row-gap),
	// crossY=4 (per col-gap).
	ei := legacyEICross(3, 2, 100, 100, 6, 4)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	// In-axis: x=[0,900,1800], y=[0,900].
	// yCol raw = [0,-4,-8] → min-normalized [8,4,0]; xRow raw=[0,-6] → [6,0].
	// position(c,r) = (x[c]+xRow[r], y[r]+yCol[c]); min corner already (0,0).
	for _, c := range []struct {
		col, row, wantX, wantY int
	}{
		{0, 0, 6, 8},     // x 0+6, y 0+8
		{1, 0, 906, 4},   // x 900+6, y 0+4
		{2, 0, 1806, 0},  // x 1800+6, y 0+0  (the (0,0)-normalization anchor in Y)
		{0, 1, 0, 908},   // x 0+0  (anchor in X), y 900+8
		{2, 1, 1800, 900}, // x 1800+0, y 900+0
	} {
		x, y, ok := l.TileOrigin(c.col, c.row)
		if !ok || x != c.wantX || y != c.wantY {
			t.Errorf("TileOrigin(%d,%d) = (%d,%d,%v), want (%d,%d,true)", c.col, c.row, x, y, ok, c.wantX, c.wantY)
		}
	}
	// Hull: width = 1806+1000 = 2806; height = y[1] + max(yCol) + th = 900+8+1000.
	if l.Width != 2806 || l.Height != 1908 {
		t.Errorf("dims = %dx%d, want 2806x1908", l.Width, l.Height)
	}
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

// TestBuildLegacyLayoutMultiAOI exercises the multi-AOI path (#67): two scanned
// AOIs at separate origins plus one UNSCANNED AOI (skipped), each a 2×1 grid
// with a RIGHT overlap 100 (tile 1000 → within-AOI advance 900). Each AOI's
// tiles land at its own Pos-X; the unscanned region has no placement.
func TestBuildLegacyLayoutMultiAOI(t *testing.T) {
	mk := func(scanned bool, cols, posX, ox int) bifxml.ImageInfo {
		ii := bifxml.ImageInfo{AOIScanned: scanned, NumCols: cols, NumRows: 1, PosX: posX, PosY: 0}
		if scanned {
			serp := func(c int) int { return imageToSerpentine(c, 0, cols, 1) + 1 }
			for c := 0; c+1 < cols; c++ {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: serp(c), Tile2: serp(c + 1), OverlapX: ox, Confidence: 100})
			}
		}
		return ii
	}
	ei := &bifxml.EncodeInfo{Ver: 2,
		ImageInfos: []bifxml.ImageInfo{mk(true, 2, 0, 100), mk(false, 3, 0, 0), mk(true, 2, 5000, 100)},
		AoiOrigins: []bifxml.AoiOrigin{
			{Index: 0, OriginX: 0, OriginY: 0},
			{Index: 1, OriginX: 2048, OriginY: 0},    // unscanned → global cols 2..4, no tiles
			{Index: 2, OriginX: 5120, OriginY: 0},    // start_col = 5 → global cols 5,6
		},
	}
	l := BuildLayout(StitchInput{Cols: 7, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	for _, c := range []struct {
		col, wantX int
		wantOK     bool
	}{
		{0, 0, true}, {1, 900, true}, // AOI0 at Pos-X 0
		{5, 5000, true}, {6, 5900, true}, // AOI2 at Pos-X 5000
		{2, 0, false}, // unscanned AOI region — no placement
	} {
		x, _, ok := l.TileOrigin(c.col, 0)
		if ok != c.wantOK || (ok && x != c.wantX) {
			t.Errorf("TileOrigin(%d,0) = (%d,%v), want (%d,%v)", c.col, x, ok, c.wantX, c.wantOK)
		}
	}
	if l.Width != 6900 {
		t.Errorf("Width = %d, want 6900 (AOI2 right edge 5900+1000)", l.Width)
	}
}

func TestBuildLegacyLayoutGatingDPUntouched(t *testing.T) {
	ei := legacyEI(2, 1, 100, 0)
	if buildLegacyLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationSpecCompliant}) != nil {
		t.Error("buildLegacyLayout must return nil for GenerationSpecCompliant")
	}
}
