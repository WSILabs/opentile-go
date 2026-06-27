package szi

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

func buildOverlapTiler(t *testing.T, w, h, tile, overlap int) *Tiler {
	t.Helper()
	tl := &Tiler{}
	tl.manifest = dzi.Manifest{Width: w, Height: h, TileSize: tile, Overlap: overlap, Format: "jpeg"}
	if err := tl.buildLevels(); err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestSZIOverlapFieldsAndLayout(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	lv, err := tl.Level(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lv.OverlapMode != opentile.OverlapBordered || !lv.Overlapping || lv.TileOverlap.X != 1 {
		t.Errorf("got mode=%v overlapping=%v tov=%v, want bordered/true/{1,1}", lv.OverlapMode, lv.Overlapping, lv.TileOverlap)
	}
	if _, _, ok := tl.StitchedSize(0); !ok {
		t.Error("StitchedSize ok=false, want true for overlap=1")
	}
	if _, _, cx, cy := tl.SubtileSource(0, 1, 1); cx != 1 || cy != 1 {
		t.Errorf("SubtileSource crop=(%d,%d), want (1,1)", cx, cy)
	}
	if sw, sh, ok := tl.StitchedSize(0); !ok || sw != 46000 || sh != 32914 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (46000,32914,true)", sw, sh, ok)
	}
}

func TestSZIZeroOverlapCleanGrid(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 0)
	lv, _ := tl.Level(0, 0)
	if lv.OverlapMode != opentile.OverlapNone || lv.Overlapping {
		t.Errorf("overlap=0: mode=%v overlapping=%v, want none/false", lv.OverlapMode, lv.Overlapping)
	}
	if _, _, ok := tl.StitchedSize(0); ok {
		t.Error("overlap=0 StitchedSize ok=true, want false (fast path)")
	}
}
