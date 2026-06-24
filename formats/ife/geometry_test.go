package ife

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestIFEGeometryScaleRatios(t *testing.T) {
	// 3 layers, native-first: scales 4,2,1 → downsamples 1,2,4.
	api := []LayerExtent{
		{XTiles: 8, YTiles: 6, Scale: 4},
		{XTiles: 4, YTiles: 3, Scale: 2},
		{XTiles: 2, YTiles: 2, Scale: 1},
	}
	// x_extent valid pixels (not a multiple of 256): in ((8-1)*256, 8*256] = (1792,2048].
	tt := TileTable{XExtent: 1900, YExtent: 1400}
	sizes, downs := ifeGeometry(api, tt)
	if sizes[0] != (opentile.Size{W: 1900, H: 1400}) {
		t.Fatalf("L0 size = %v, want 1900x1400 (x_extent anchor)", sizes[0])
	}
	wantDown := []float64{1, 2, 4}
	for i := range downs {
		if downs[i] != wantDown[i] {
			t.Errorf("L%d downsample = %v, want %v", i, downs[i], wantDown[i])
		}
	}
	// Exact 2x ratios (round of 1900/2=950, /4=475).
	if sizes[1] != (opentile.Size{W: 950, H: 700}) || sizes[2] != (opentile.Size{W: 475, H: 350}) {
		t.Fatalf("scaled sizes = %v %v, want 950x700 475x350", sizes[1], sizes[2])
	}
}

func TestIFEGeometryInvalidExtentFallsBack(t *testing.T) {
	api := []LayerExtent{
		{XTiles: 8, YTiles: 6, Scale: 2},
		{XTiles: 4, YTiles: 3, Scale: 1},
	}
	// x_extent carries TILE COUNTS (cervix non-conformance): 8 is not in (1792,2048].
	tt := TileTable{XExtent: 8, YExtent: 6}
	sizes, downs := ifeGeometry(api, tt)
	// Falls back to padded L0 (8*256 x 6*256); ratios still exact.
	if sizes[0] != (opentile.Size{W: 2048, H: 1536}) {
		t.Fatalf("L0 size = %v, want padded 2048x1536 fallback", sizes[0])
	}
	if downs[1] != 2 || sizes[1] != (opentile.Size{W: 1024, H: 768}) {
		t.Fatalf("L1 down=%v size=%v, want 2 / 1024x768", downs[1], sizes[1])
	}
}

func TestValidPixelExtent(t *testing.T) {
	if !validPixelExtent(1900, 8) { // (1792,2048]
		t.Error("1900 with 8 tiles should be valid pixels")
	}
	if validPixelExtent(8, 8) { // tile-count, not pixels
		t.Error("8 with 8 tiles should be invalid (tile count)")
	}
	if !validPixelExtent(2048, 8) { // exact multiple is valid
		t.Error("2048 with 8 tiles should be valid")
	}
	if validPixelExtent(1792, 8) { // == (tiles-1)*256, boundary excluded
		t.Error("1792 with 8 tiles should be invalid")
	}
}
