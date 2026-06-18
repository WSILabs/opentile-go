package bif

import "testing"

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
