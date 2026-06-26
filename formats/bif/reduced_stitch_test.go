package bif

import (
	"os"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestSubtileSourcePreciseCrop pins the precise fractional crop origin
// (cross-level registration): the stored tile packs n=2^shift subtiles per axis
// across its full TileW/TileH, so the quadrant origin is round(q·Dim/n), NOT
// q·(Dim>>shift). Flooring drifts up to (n−1)·frac px when the dimension is not a
// multiple of n — legacy iScan TileH=1360 is exact through L4 (1360=16·85) but
// 1360/32=42.5 drifts ~15 px at L5+, a deep-zoom vertical misregistration.
// TileW=1024 is a power of two so X is exact at every level. CI-safe (synthetic).
func TestSubtileSourcePreciseCrop(t *testing.T) {
	l := &levelImpl{tileSize: opentile.Size{W: 1024, H: 1360}, subtileShift: 5, subtileL0: &Layout{}} // n=32
	for _, c := range []struct {
		col, row, wantSC, wantSR, wantCX, wantCY int
	}{
		{0, 0, 0, 0, 0, 0},
		{1, 1, 0, 0, 32, 43},        // qx=1→1024/32=32 exact; qy=1→round(42.5)=43 (floor gave 42)
		{31, 31, 0, 0, 992, 1318},   // qy=31→round(1317.5)=1318 (floor gave 1302 — the ~15 px drift)
		{32, 32, 1, 1, 0, 0},        // wraps to the next stored tile, quadrant 0
	} {
		sc, sr, cx, cy := l.subtileSource(c.col, c.row)
		if sc != c.wantSC || sr != c.wantSR || cx != c.wantCX || cy != c.wantCY {
			t.Errorf("subtileSource(%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.col, c.row, sc, sr, cx, cy, c.wantSC, c.wantSR, c.wantCX, c.wantCY)
		}
	}
	// The bottom subtile must not read past the stored tile.
	if _, _, _, cy := l.subtileSource(0, 31); cy+(1360>>5) > 1360 {
		t.Errorf("bottom subtile cropY %d + unitH %d exceeds TileH 1360", cy, 1360>>5)
	}
}

// openVentana1 opens the local Ventana-1.bif DP fixture for internal tests,
// skipping cleanly when absent.
func openVentana1(t *testing.T) *Tiler {
	t.Helper()
	path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/Ventana-1.bif"
	if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
		path = dir + "/bif/Ventana-1.bif"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("Ventana-1.bif not present")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	tl, err := New().Open(tf, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// TestReducedDPLevelsStitched verifies the SUBTILE model on DP reduced levels
// (#83): they report Overlapping=true, Size = floorHalve(L0 hull), and the
// compositing units are L0 frames placed at their compacted position scaled by
// 1/2ⁱ (UnitSize = TileSize>>i, SubtileSource maps a frame to its stored tile +
// quadrant). Anchor values from the Phase-0 probe: L0 frame 22 X = 22408.
func TestReducedDPLevelsStitched(t *testing.T) {
	tl := openVentana1(t)
	if !tl.levelImpls[0].overlapping {
		t.Fatal("L0 should be overlapping (sanity)")
	}
	assertSubtileLevel(t, tl, 1, 11716, 10752, 512, 512)
	assertSubtileLevel(t, tl, 2, 5858, 5376, 256, 256)

	// L1 unit = L0 frame 22 placed at its compacted X (22408) >> 1 = 11204,
	// sourced from stored tile 11 quadrant 0 (frame 22 is even).
	if x, _, ok := tl.TileOrigin(1, 22, 0); !ok || x != 11204 {
		t.Errorf("L1 frame (22,0) origin X = %d (ok=%v), want 11204 (L0 22408>>1)", x, ok)
	}
	if sc, sr, cx, cy := tl.SubtileSource(1, 22, 0); sc != 11 || sr != 0 || cx != 0 || cy != 0 {
		t.Errorf("L1 SubtileSource(22,0) = (%d,%d,%d,%d), want (11,0,0,0)", sc, sr, cx, cy)
	}
	// Odd frame 23 sources the same stored tile 11, quadrant 1 (cropX 512).
	if sc, sr, cx, cy := tl.SubtileSource(1, 23, 0); sc != 11 || sr != 0 || cx != 512 || cy != 0 {
		t.Errorf("L1 SubtileSource(23,0) = (%d,%d,%d,%d), want (11,0,512,0)", sc, sr, cx, cy)
	}
}

// openOS1 opens the local OS-1.bif legacy-iScan fixture (PHI; local-only, never
// in CI), skipping cleanly when absent.
func openOS1(t *testing.T) *Tiler {
	t.Helper()
	path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/OS-1.bif"
	if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
		path = dir + "/bif/OS-1.bif"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("OS-1.bif not present")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	tl, err := New().Open(tf, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// TestReducedLegacyLevelsStitched verifies the SUBTILE model engages on legacy
// iScan reduced levels too (#80): Overlapping=true, Size = floorHalve(L0 hull),
// subtile units = TileSize>>i. Legacy tiles are 1024×1360, so units halve on
// both axes. The pixel-correctness (vs L0-downsampled) is gated by
// TestReducedContentMatchesDownsampledL0.
func TestReducedLegacyLevelsStitched(t *testing.T) {
	tl := openOS1(t)
	if tl.gen != GenerationLegacyIScan {
		t.Fatalf("generation = %v, want legacy-iscan", tl.gen)
	}
	if !tl.levelImpls[0].overlapping {
		t.Fatal("L0 should be overlapping (legacy joints reconstructed the hull)")
	}
	assertSubtileLevel(t, tl, 1, 52968, 47062, 512, 680)
	assertSubtileLevel(t, tl, 2, 26484, 23531, 256, 340)
}

// assertSubtileLevel checks a reduced level is a subtile level with the expected
// Size and subtile unit size.
func assertSubtileLevel(t *testing.T, tl *Tiler, level, wantW, wantH, wantUW, wantUH int) {
	t.Helper()
	l := tl.levelImpls[level]
	if !l.overlapping {
		t.Errorf("L%d Overlapping = false, want true", level)
	}
	if l.subtileL0 == nil || l.subtileShift != uint(level) {
		t.Errorf("L%d subtile: L0=%v shift=%d, want non-nil shift=%d", level, l.subtileL0 != nil, l.subtileShift, level)
	}
	if l.size.W != wantW || l.size.H != wantH {
		t.Errorf("L%d Size = %dx%d, want %dx%d (floorHalve L0 hull)", level, l.size.W, l.size.H, wantW, wantH)
	}
	if uw, uh := tl.UnitSize(level); uw != wantUW || uh != wantUH {
		t.Errorf("L%d UnitSize = %dx%d, want %dx%d", level, uw, uh, wantUW, wantUH)
	}
}
