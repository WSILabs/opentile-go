package bif

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestTilerImplementsRegionLayout(t *testing.T) {
	tl := &Tiler{levelImpls: []*levelImpl{{
		index: 0, grid: opentile.Size{W: 2, H: 1}, tileSize: opentile.Size{W: 100, H: 100},
		layout: buildNaiveLayout(StitchInput{Cols: 2, Rows: 1, TileW: 100, TileH: 100}),
	}}}
	x, y, ok := tl.TileOrigin(0, 1, 0)
	if !ok || x != 100 || y != 0 {
		t.Errorf("TileOrigin(0,1,0) = (%d,%d,%v), want (100,0,true)", x, y, ok)
	}
	w, h, ok := tl.StitchedSize(0)
	if !ok || w != 200 || h != 100 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (200,100,true)", w, h, ok)
	}
	got := tl.TilesIntersecting(0, 0, 0, 200, 100)
	if len(got) != 2 {
		t.Errorf("TilesIntersecting = %d, want 2", len(got))
	}
	// Out-of-range level → ok=false, empty.
	if _, _, ok := tl.TileOrigin(9, 0, 0); ok {
		t.Error("TileOrigin on bad level should be ok=false")
	}
}
