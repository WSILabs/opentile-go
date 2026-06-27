package dzi

import (
	"testing"

	idzi "github.com/wsilabs/opentile-go/internal/dzi"
	opentile "github.com/wsilabs/opentile-go"
)

// buildOverlapTiler constructs a Tiler directly from a synthetic manifest, no
// filesystem needed — exercises the layout math only.
func buildOverlapTiler(t *testing.T, w, h, tile, overlap int) *Tiler {
	t.Helper()
	tl := &Tiler{}
	tl.manifest = idzi.Manifest{Width: w, Height: h, TileSize: tile, Overlap: overlap, Format: "jpeg"}
	tl.filesDir = "/nonexistent"
	tl.buildLevels()
	return tl
}

func TestDZIOverlapLevelFields(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	lv, err := tl.Level(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lv.OverlapMode != opentile.OverlapBordered || !lv.Overlapping {
		t.Errorf("mode=%v overlapping=%v, want bordered/true", lv.OverlapMode, lv.Overlapping)
	}
	if lv.TileOverlap.X != 1 || lv.TileOverlap.Y != 1 {
		t.Errorf("TileOverlap=%v, want {1,1}", lv.TileOverlap)
	}
}

func TestDZIZeroOverlapCleanGrid(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 0)
	lv, _ := tl.Level(0, 0)
	if lv.OverlapMode != opentile.OverlapNone || lv.Overlapping {
		t.Errorf("overlap=0: mode=%v overlapping=%v, want none/false", lv.OverlapMode, lv.Overlapping)
	}
	if _, _, ok := tl.StitchedSize(0); ok {
		t.Error("overlap=0 StitchedSize ok=true, want false (fast path)")
	}
}

func TestDZISubtileSourceCropsBorder(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	sc, sr, cx, cy := tl.SubtileSource(0, 1, 1)
	if sc != 1 || sr != 1 || cx != 1 || cy != 1 {
		t.Errorf("SubtileSource(0,1,1) = (%d,%d,%d,%d), want (1,1,1,1)", sc, sr, cx, cy)
	}
	if _, _, cx, cy := tl.SubtileSource(0, 0, 0); cx != 0 || cy != 0 {
		t.Errorf("SubtileSource(0,0,0) crop = (%d,%d), want (0,0)", cx, cy)
	}
	x, y, ok := tl.TileOrigin(0, 1, 1)
	if !ok || x != 256 || y != 256 {
		t.Errorf("TileOrigin(0,1,1) = (%d,%d,%v), want (256,256,true)", x, y, ok)
	}
	uw, uh := tl.UnitSize(0)
	if uw != 256 || uh != 256 {
		t.Errorf("UnitSize(0) = (%d,%d), want (256,256)", uw, uh)
	}
	sw, sh, ok := tl.StitchedSize(0)
	if !ok || sw != 46000 || sh != 32914 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (46000,32914,true)", sw, sh, ok)
	}
}
