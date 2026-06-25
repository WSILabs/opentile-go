package bif

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

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

// TestReducedDPLevelsStitched verifies that DP reduced levels (L1+) are compacted
// by downsampling the L0 layout (#83): they report Overlapping=true and their
// tile placements are the L0 compacted origins scaled by 1/2^i — not the naive
// raw grid. Anchor values come from the Phase-0 geometry probe on Ventana-1.
func TestReducedDPLevelsStitched(t *testing.T) {
	tl := openVentana1(t)

	l0 := tl.levelImpls[0]
	if !l0.overlapping {
		t.Fatal("L0 should be overlapping (sanity)")
	}

	// L1: rightmost real column (11) is compacted inward to 11204 (= L0 frame
	// column 22's compacted X 22408, halved), not the naive 11264.
	l1 := tl.levelImpls[1]
	if !l1.overlapping {
		t.Errorf("L1 Overlapping = false, want true (#83: reduced DP levels carry compacted overlap)")
	}
	x, _, ok := l1.layout.TileOrigin(11, 0)
	if !ok {
		t.Fatal("L1 tile (11,0) has no placement")
	}
	if x != 11204 {
		t.Errorf("L1 tile (11,0) X = %d, want 11204 (L0 frame 22 X 22408 >> 1); naive would be 11264", x)
	}
	// The compacted layout raster (12228) must stay below the naive raster
	// (12288) — that's what makes Overlapping true — while Size clips to 11716.
	if l1.layout.Width >= l1.grid.W*l1.tileSize.W {
		t.Errorf("L1 layout.Width = %d, want < naive %d", l1.layout.Width, l1.grid.W*l1.tileSize.W)
	}
	if l1.size.W != 11716 || l1.size.H != 10752 {
		t.Errorf("L1 Size = %dx%d, want 11716x10752 (floorHalve L0 hull)", l1.size.W, l1.size.H)
	}

	// L2 likewise compacted (30px cumulative; tile (5,0) X = 5090, naive 5120).
	l2 := tl.levelImpls[2]
	if !l2.overlapping {
		t.Errorf("L2 Overlapping = false, want true")
	}
	if x, _, ok := l2.layout.TileOrigin(5, 0); !ok || x != 5090 {
		t.Errorf("L2 tile (5,0) X = %d (ok=%v), want 5090", x, ok)
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

// TestReducedLegacyLevelsStitched verifies that legacy iScan reduced levels
// (L1+) are compacted by downsampling the L0 layout (#80): Overlapping=true,
// Size = floorHalve(L0 hull) (the openslide content extent — L1 52909 is within
// ~0.1% of openslide's ~52907), and the layout raster is compacted ~11% below
// the raw frame grid. Legacy overlap is dense, so unlike DP nearly every column
// is compacted (the per-gap-average model places frames inward on both axes).
func TestReducedLegacyLevelsStitched(t *testing.T) {
	tl := openOS1(t)
	if tl.gen != GenerationLegacyIScan {
		t.Fatalf("generation = %v, want legacy-iscan", tl.gen)
	}
	l0 := tl.levelImpls[0]
	if !l0.overlapping {
		t.Fatal("L0 should be overlapping (legacy joints reconstructed the hull)")
	}

	l1 := tl.levelImpls[1]
	if !l1.overlapping {
		t.Errorf("L1 Overlapping = false, want true (#80: legacy reduced levels carry dense frame overlap)")
	}
	if l1.size.W != 52909 || l1.size.H != 46962 {
		t.Errorf("L1 Size = %dx%d, want 52909x46962 (floorHalve L0 hull; openslide ~52907 wide)", l1.size.W, l1.size.H)
	}
	naiveW := l1.grid.W * l1.tileSize.W
	if l1.layout.Width >= naiveW {
		t.Errorf("L1 layout.Width = %d, want < naive %d (dense ~11%% compaction)", l1.layout.Width, naiveW)
	}
	// Dense overlap → a double-digit-percent compaction (probe: ~10.8%).
	compactPct := 100 * float64(naiveW-l1.layout.Width) / float64(naiveW)
	if compactPct < 8 || compactPct > 14 {
		t.Errorf("L1 X compaction = %.1f%%, want ~11%% (legacy frame overlap)", compactPct)
	}

	l2 := tl.levelImpls[2]
	if !l2.overlapping {
		t.Errorf("L2 Overlapping = false, want true")
	}
	if l2.size.W != 26454 || l2.size.H != 23481 {
		t.Errorf("L2 Size = %dx%d, want 26454x23481", l2.size.W, l2.size.H)
	}
}
