package opentile

import "testing"

func makeLevel(mode OverlapMode, w, h, t, ov int) Level {
	cols := (w + t - 1) / t
	rows := (h + t - 1) / t
	tov := Point{}
	if mode == OverlapBordered {
		tov = Point{X: ov, Y: ov}
	}
	return Level{
		Size: Size{W: w, H: h}, TileSize: Size{W: t, H: t},
		Grid: Size{W: cols, H: rows}, OverlapMode: mode, TileOverlap: tov,
	}
}

func TestTileContentRectBordered(t *testing.T) {
	// CMU-1 level-16 geometry: 46000x32914, T=256, ov=1, grid 180x129.
	l := makeLevel(OverlapBordered, 46000, 32914, 256, 1)
	cases := []struct {
		c, r, ox, oy, w, h int
	}{
		{0, 0, 0, 0, 256, 256},     // corner: no left/top overlap
		{1, 1, 1, 1, 256, 256},     // interior: +1 left/top
		{179, 0, 1, 0, 176, 256},   // right edge: clipped width 176
		{0, 128, 0, 1, 256, 146},   // bottom edge: clipped height 146
		{179, 128, 1, 1, 176, 146}, // far corner
	}
	for _, c := range cases {
		got, ok := l.TileContentRect(c.c, c.r)
		if !ok {
			t.Errorf("(%d,%d): ok=false, want true", c.c, c.r)
			continue
		}
		want := Region{Origin: Point{X: c.ox, Y: c.oy}, Size: Size{W: c.w, H: c.h}}
		if got != want {
			t.Errorf("(%d,%d) = %+v, want %+v", c.c, c.r, got, want)
		}
	}
}

func TestTileContentRectNoneIsFullCell(t *testing.T) {
	l := makeLevel(OverlapNone, 46000, 32914, 256, 0)
	got, ok := l.TileContentRect(1, 1)
	if !ok || got != (Region{Origin: Point{}, Size: Size{W: 256, H: 256}}) {
		t.Errorf("None interior = %+v ok=%v, want full cell at origin", got, ok)
	}
	// last column clips, origin stays (0,0)
	got, _ = l.TileContentRect(179, 0)
	if got.Origin != (Point{}) || got.Size.W != 176 {
		t.Errorf("None right-edge = %+v, want origin(0,0) width 176", got)
	}
}

func TestTileContentRectStitchedAndOOB(t *testing.T) {
	st := makeLevel(OverlapStitched, 1000, 1000, 256, 0)
	if _, ok := st.TileContentRect(0, 0); ok {
		t.Error("stitched: ok=true, want false (use region API)")
	}
	bd := makeLevel(OverlapBordered, 1000, 1000, 256, 1)
	if _, ok := bd.TileContentRect(99, 0); ok {
		t.Error("out-of-grid: ok=true, want false")
	}
	if _, ok := bd.TileContentRect(-1, 0); ok {
		t.Error("negative col: ok=true, want false")
	}
}
