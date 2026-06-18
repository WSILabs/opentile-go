package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// singleAOIEncodeInfo builds a Ver=2 EncodeInfo for one AOI: a cols×rows grid
// with row-major Frames, uniform RIGHT overlap=ox between horizontally adjacent
// tiles and uniform DOWN overlap=oy between vertically adjacent tiles.
//
// Tile1/Tile2 are emitted as 1-BASED SERPENTINE physical indices, matching the
// Roche whitepaper (page 13) and the real Ventana-1 fixture — NOT the row-major
// Frame storage index (the engine resolves them via serpentineToImage, so the
// helper must speak the same language). RIGHT/DOWN are valid spec direction
// labels (the real DP-200 serpentine happens to emit only LEFT/UP, but the
// engine handles all four uniformly); using RIGHT/DOWN here keeps coverage of
// both horizontal/vertical labels.
func singleAOIEncodeInfo(cols, rows, ox, oy int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	serp := func(c, r int) int { return imageToSerpentine(c, r, cols, rows) + 1 } // 1-based
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			ii.Frames = append(ii.Frames, bifxml.Frame{Col: c, Row: r})
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: serp(c, r), Tile2: serp(c+1, r), OverlapX: ox, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "DOWN", Tile1: serp(c, r), Tile2: serp(c, r+1), OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii}}
}

func TestBuildDPLayoutHorizontalOverlap(t *testing.T) {
	// 3 cols × 2 rows, tile 1024, RIGHT overlap 120, no vertical overlap.
	ei := singleAOIEncodeInfo(3, 2, 120, 0)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// Column origins: 0, 1024-120=904, 904+904=1808. Width is the compacted
	// content hull = 1808+1024 = 2832 (no tile-multiple rounding — the stitched
	// content extent is the hull itself; see finalizeExtent / bio-formats
	// ground truth that Ventana-1 reports the un-rounded 23432).
	wantCols := []int{0, 904, 1808}
	for c, wantX := range wantCols {
		x, _, ok := l.TileOrigin(c, 0)
		if !ok || x != wantX {
			t.Errorf("TileOrigin(%d,0).X = (%d,%v), want %d", c, x, ok, wantX)
		}
	}
	if l.Width != 2832 {
		t.Errorf("Width = %d, want 2832 (compacted content hull, no rounding)", l.Width)
	}
	if l.Height != 2048 {
		t.Errorf("Height = %d, want 2048", l.Height)
	}
}

// TestBuildDPLayoutSingleAOIOriginShift exercises the DP AOI-origin path
// directly: one AOI with a confident RIGHT joint (so buildDPLayout engages,
// not the naive fallback) AND a non-zero AoiOrigin. It proves (a) the origin
// offset is applied and (b) finalizeExtent normalizes the min corner back to
// (0,0), while (c) the overlap compaction still applies.
func TestBuildDPLayoutSingleAOIOriginShift(t *testing.T) {
	ei := singleAOIEncodeInfo(2, 1, 120, 0) // 2×1, RIGHT overlap 120, has a confident joint
	ei.ImageInfos[0].AOIIndex = 0
	ei.AoiOrigins = []bifxml.AoiOrigin{{Index: 0, OriginX: 5120, OriginY: 3072}}
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// After origin shift both tiles move by (5120,3072); normalization pulls the
	// min corner back to (0,0), so tile (0,0) lands at (0,0).
	x0, y0, ok := l.TileOrigin(0, 0)
	if !ok || x0 != 0 || y0 != 0 {
		t.Fatalf("TileOrigin(0,0) = (%d,%d,%v), want (0,0,true) after normalization", x0, y0, ok)
	}
	// RIGHT overlap still compacts col 1 to col0X + tileW - overlap = 904.
	x1, _, ok := l.TileOrigin(1, 0)
	if !ok || x1 != 904 {
		t.Errorf("TileOrigin(1,0).X = (%d,%v), want 904 (compaction preserved through origin path)", x1, ok)
	}
}

func TestBuildDPLayoutTwoAOIsWithOrigins(t *testing.T) {
	// Two single-tile AOIs (1024 tiles, no internal overlap). AOI0 at origin
	// (0,0), AOI1 at OriginX=1024 → side by side, total 2048×1024.
	// NOTE: with no joints, hasConfidentJoint is false → this routes through the
	// NAIVE fallback (the end-to-end dims still hold). The AOI-origin DP code
	// path itself is covered by TestBuildDPLayoutSingleAOIOriginShift above.
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
	// Ver<2 → naive (no DP compaction, no legacy compaction — Ver<2 implies a
	// spec-compliant DP file that is malformed; legacy slides that arrive here
	// have their own buildLegacyLayout path below).
	verLow := singleAOIEncodeInfo(2, 1, 120, 0)
	verLow.Ver = 1
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: verLow, Generation: GenerationSpecCompliant})
	if x, _, _ := l.TileOrigin(1, 0); x != 1024 {
		t.Errorf("Ver<2 must fall back to naive (x=1024), got %d", x)
	}
	// Legacy generation with live joins → buildLegacyLayout compacts (#63).
	// singleAOIEncodeInfo sets Confidence=100 >= legacyConfidenceCutoff(98), so
	// the overlap is trusted: X[1] = 1024 - 120 = 904.
	legacy := singleAOIEncodeInfo(2, 1, 120, 0)
	l2 := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: legacy, Generation: GenerationLegacyIScan})
	if x, _, _ := l2.TileOrigin(1, 0); x != 904 {
		t.Errorf("legacy generation with live joints uses buildLegacyLayout (x=904), got %d", x)
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
