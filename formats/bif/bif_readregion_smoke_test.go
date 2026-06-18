package bif_test

import (
	"os"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestBIFReadRegionStitchedSmoke verifies that the layout-aware ReadRegion
// compositing path (regionLayout interface) engages correctly for BIF.
// Reads a 512×512 interior region from Ventana-1 L0 and asserts:
//   - no error
//   - output dimensions are 512×512
//   - L0 stitched size is the expected 23432×21504 content hull
//   - the interior pixel buffer is not entirely white (tissue is present)
//
// Skipped when Ventana-1.bif is not present locally.
func TestBIFReadRegionStitchedSmoke(t *testing.T) {
	path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/Ventana-1.bif"
	if _, err := os.Stat(path); err != nil {
		t.Skip("Ventana-1.bif not present")
	}
	s, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lvl, err := s.Level(0)
	if err != nil {
		t.Fatal(err)
	}
	// stitched L0 size should be 23432×21504 (compacted content hull, not padded IFD extent)
	if lvl.Size.W != 23432 || lvl.Size.H != 21504 {
		t.Fatalf("L0 size = %dx%d, want 23432x21504", lvl.Size.W, lvl.Size.H)
	}
	// Read a 512×512 region from the interior (well inside the tissue area).
	reg := opentile.Region{Origin: opentile.Point{X: 5000, Y: 5000}, Size: opentile.Size{W: 512, H: 512}}
	img, err := lvl.ReadRegion(reg)
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Width != 512 || img.Height != 512 {
		t.Fatalf("region dims = %dx%d, want 512x512", img.Width, img.Height)
	}
	// Not all white (interior should have tissue/structure). Count non-0xFF pixels.
	// Stride may be >= 3*Width, so walk row by row.
	nonWhite := 0
	bpp := img.Stride / img.Width // bytes per pixel (3 for RGB, 4 for RGBA)
	for row := 0; row < img.Height; row++ {
		rowBase := row * img.Stride
		for col := 0; col < img.Width; col++ {
			off := rowBase + col*bpp
			if img.Pix[off] != 0xFF || img.Pix[off+1] != 0xFF || img.Pix[off+2] != 0xFF {
				nonWhite++
			}
		}
	}
	if nonWhite == 0 {
		t.Error("interior ReadRegion is entirely white — compositing likely broken")
	}
	t.Logf("interior 512×512 region: %d/%d pixels are non-white (tissue present)", nonWhite, img.Width*img.Height)
}
