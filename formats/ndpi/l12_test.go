package ndpi_test

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestL12OOBFillIsWhite locks in the L12 fix: NDPI edge tiles whose
// crop region extends past the image must have their OOB strip filled
// with white (matching Python opentile / PyTurboJPEG.crop_multiple's
// background_luminance=1.0 default), not libjpeg-turbo's mid-gray
// default.
//
// Test target: OS-2.ndpi L5 tile (3, 0). The L5 image is 3968×2304;
// the tile at (3, 0) covers x=3072..4095 in image coordinates, which
// extends 128 px past the right edge. The OOB strip is therefore
// cols 896..1023 of the decoded 1024×1024 tile.
//
// Pre-fix: OOB strip decodes to RGB(128, 128, 128) — Crop's default
// fill (DC=0) — because stripped.go::Tile tried Crop first and Crop
// happened to succeed.
// Post-fix: OOB strip decodes to RGB(~255, ~255, ~255), matching
// Python's CUSTOMFILTER white fill (DC=170 for luminance=1.0).
func TestL12OOBFillIsWhite(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(1024, 1024))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tiler.Close()
	lvl5, err := tiler.Level(5)
	if err != nil {
		t.Fatalf("Level(5): %v", err)
	}
	jpegBytes, err := lvl5.Tile(3, 0)
	if err != nil {
		t.Fatalf("RawTile(5, 3, 0): %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	bnds := img.Bounds()
	if bnds.Dx() != 1024 || bnds.Dy() != 1024 {
		t.Fatalf("decoded dims: got %v, want 1024x1024", bnds)
	}
	// Sample a few OOB pixels (cols 896..1023). White-fill threshold:
	// each channel >= 250 (allow some subsampling slop).
	samples := []struct{ x, y int }{
		{896, 0}, {960, 0},
		{896, 512}, {960, 512},
		{896, 1023}, {1023, 1023},
	}
	for _, s := range samples {
		r, g, b, _ := img.At(s.x, s.y).RGBA()
		// RGBA() returns 16-bit; >> 8 to get 8-bit channels.
		r8, g8, b8 := r>>8, g>>8, b>>8
		if r8 < 250 || g8 < 250 || b8 < 250 {
			t.Errorf("OOB pixel (%d,%d): RGB=(%d,%d,%d), want each >= 250 (white-fill); got mid-gray suggests Crop's default fill, not CropWithBackground",
				s.x, s.y, r8, g8, b8)
		}
	}
	// Belt-and-braces: an in-image pixel (col < 896) must NOT be the
	// OOB-fill colour. If a future regression makes everything white,
	// this guard catches it.
	r, g, b, _ := img.At(100, 100).RGBA()
	r8, g8, b8 := r>>8, g>>8, b>>8
	if r8 >= 250 && g8 >= 250 && b8 >= 250 {
		t.Errorf("in-image pixel (100,100) is unexpectedly white: RGB=(%d,%d,%d)", r8, g8, b8)
	}
}
