package bif

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestMultiZBIFOpens: synthetic 3-Z-plane BIF (1 near + nominal +
// 1 far, Z-spacing=1.5 µm) opens cleanly and the bifImage reports
// the right SizeZ + ZPlaneFocus values per BIF whitepaper §"Whole
// slide imaging process" storage convention.
func TestMultiZBIFOpens(t *testing.T) {
	const tw, th = 32, 32
	const gridW, gridH = 2, 2
	const depth = 3

	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" Z-layers="3" Z-spacing="1.5"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
			tileFill:   0x10, // base; Z=0 gets 0x10, Z=1 → 0x11, Z=2 → 0x12
			imageDepth: depth,
		},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	imgs := tiler.Images()
	if len(imgs) != 1 {
		t.Fatalf("Images: got %d, want 1", len(imgs))
	}
	img := imgs[0]
	if got := img.SizeZ(); got != depth {
		t.Errorf("SizeZ: got %d, want %d", got, depth)
	}
	if got := img.SizeC(); got != 1 {
		t.Errorf("SizeC: got %d, want 1 (BIF has no fluorescence)", got)
	}
	if got := img.SizeT(); got != 1 {
		t.Errorf("SizeT: got %d, want 1 (BIF has no time series)", got)
	}

	// ZPlaneFocus: storage layout is [Z=0 nominal][Z=1 near][Z=2 far]
	// per BIF spec. With Z-spacing=1.5 and depth=3 (nNear=1, nFar=1):
	//   ZPlaneFocus(0) = 0   (nominal)
	//   ZPlaneFocus(1) = -1.5 (one near)
	//   ZPlaneFocus(2) = +1.5 (one far)
	wantFoci := []float64{0, -1.5, +1.5}
	for z, want := range wantFoci {
		if got := img.ZPlaneFocus(z); got != want {
			t.Errorf("ZPlaneFocus(%d): got %v, want %v", z, got, want)
		}
	}
}

// TestMultiZTileAtReadsCorrectPlane: TileAt({Z: k}) reads bytes
// from the correct Z-plane region of the TileOffsets array. Each
// plane in the synthetic fixture has a distinguishable fill byte
// (Z=0 → 0x10, Z=1 → 0x11, Z=2 → 0x12), so reading any tile from
// each plane produces a uniform-fill payload with the expected
// byte value.
func TestMultiZTileAtReadsCorrectPlane(t *testing.T) {
	const tw, th = 32, 32
	const gridW, gridH = 2, 2
	const depth = 3
	const baseFill = 0x40

	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" Z-layers="3" Z-spacing="2"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
			tileFill:   baseFill,
			imageDepth: depth,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lvl, _ := tiler.Level(0)

	for z := 0; z < depth; z++ {
		for r := 0; r < gridH; r++ {
			for c := 0; c < gridW; c++ {
				bytes_, err := lvl.TileAt(opentile.TileCoord{X: c, Y: r, Z: z})
				if err != nil {
					t.Errorf("TileAt(c=%d, r=%d, z=%d): %v", c, r, z, err)
					continue
				}
				wantByte := byte(baseFill + z)
				if len(bytes_) == 0 {
					t.Errorf("TileAt(c=%d, r=%d, z=%d): empty bytes", c, r, z)
					continue
				}
				// All bytes in the tile should be the per-plane fill value.
				for i, b := range bytes_ {
					if b != wantByte {
						t.Errorf("TileAt(c=%d, r=%d, z=%d): byte[%d]=0x%02X, want 0x%02X", c, r, z, i, b, wantByte)
						break
					}
				}
			}
		}
	}
}

// TestMultiZTileAtBoundsCheck: out-of-range Z on a multi-Z fixture
// returns ErrTileOutOfBounds (the axis exists; index is past size).
// Distinct from imageDepth==1 + Z != 0, which returns
// ErrDimensionUnavailable (the axis effectively doesn't exist).
func TestMultiZTileAtBoundsCheck(t *testing.T) {
	const tw, th = 32, 32
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" Z-layers="3" Z-spacing="1"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw, imageLength: th,
			tileWidth: tw, tileLength: th,
			imageDepth: 3,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl, _ := tiler.Level(0)

	// Z=3 (out of [0, 3)) → ErrTileOutOfBounds.
	_, err := lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, Z: 3})
	if err == nil {
		t.Fatal("TileAt(Z=3) on imageDepth=3: got nil err, want ErrTileOutOfBounds")
	}
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("TileAt(Z=3): got %v, want errors.Is(ErrTileOutOfBounds)", err)
	}

	// Z=-1 → ErrTileOutOfBounds.
	_, err = lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, Z: -1})
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("TileAt(Z=-1): got %v, want errors.Is(ErrTileOutOfBounds)", err)
	}

	// C=1 → ErrDimensionUnavailable (BIF has no C axis even on
	// multi-Z slides).
	_, err = lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, C: 1})
	if !errors.Is(err, opentile.ErrDimensionUnavailable) {
		t.Errorf("TileAt(C=1): got %v, want errors.Is(ErrDimensionUnavailable)", err)
	}

	// T=1 → ErrDimensionUnavailable (no T axis).
	_, err = lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, T: 1})
	if !errors.Is(err, opentile.ErrDimensionUnavailable) {
		t.Errorf("TileAt(T=1): got %v, want errors.Is(ErrDimensionUnavailable)", err)
	}
}

// TestSingleZBIFCompatibility: a fixture without IMAGE_DEPTH (or
// with imageDepth=1) reports SizeZ()==1 and any non-zero Z returns
// ErrDimensionUnavailable (axis effectively absent), matching the
// 2D-format compatibility contract.
func TestSingleZBIFCompatibility(t *testing.T) {
	const tw, th = 32, 32
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw, imageLength: th,
			tileWidth: tw, tileLength: th,
			// No imageDepth field → default 0/1 → no IMAGE_DEPTH tag emitted.
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	img := tiler.Images()[0]
	if got := img.SizeZ(); got != 1 {
		t.Errorf("SizeZ: got %d, want 1 (no IMAGE_DEPTH)", got)
	}
	lvl, _ := tiler.Level(0)
	_, err := lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, Z: 1})
	if !errors.Is(err, opentile.ErrDimensionUnavailable) {
		t.Errorf("TileAt(Z=1) on SizeZ=1: got %v, want ErrDimensionUnavailable", err)
	}
}

// TestComputeZPlaneFocusTable: pure unit test of the table builder
// covers odd / even imageDepths and edge cases (depth 1, zero
// spacing).
func TestComputeZPlaneFocusTable(t *testing.T) {
	cases := []struct {
		name       string
		depth      int
		zSpacing   float64
		want       []float64
	}{
		{"depth=1", 1, 1.5, []float64{0}},
		{"depth=3 spacing=1.5", 3, 1.5, []float64{0, -1.5, +1.5}},
		{"depth=5 spacing=1", 5, 1, []float64{0, -1, -2, +1, +2}},
		{"depth=5 spacing=0.5", 5, 0.5, []float64{0, -0.5, -1, +0.5, +1}},
		{"depth=4 even (defensive: nNear=1, nFar=2)", 4, 1, []float64{0, -1, +1, +2}},
		{"depth=3 spacing=0", 3, 0, []float64{0, 0, 0}},
		{"depth=0 (clamped to 1)", 0, 1, []float64{0}},
		{"depth=-1 (clamped to 1)", -1, 1, []float64{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeZPlaneFocusTable(tc.depth, tc.zSpacing)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d, want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ZPlaneFocus[%d]: got %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
