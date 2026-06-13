package parity

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// slideCandidates2D lists every fixture in our local sample set
// that should have at least one Image with at least one Level — i.e.,
// standard 2D pathology slides.
var slideCandidates2D = []struct {
	subdir string
	name   string
}{
	{"svs", "CMU-1-Small-Region.svs"},
	{"svs", "CMU-1.svs"},
	{"svs", "JP2K-33003-1.svs"},
	{"svs", "scan_620_.svs"},
	{"svs", "svs_40x_bigtiff.svs"},
	{"ndpi", "CMU-1.ndpi"},
	{"ndpi", "OS-2.ndpi"},
	{"ndpi", "Hamamatsu-1.ndpi"},
	{"philips-tiff", "Philips-1.tiff"},
	{"philips-tiff", "Philips-2.tiff"},
	{"philips-tiff", "Philips-3.tiff"},
	{"philips-tiff", "Philips-4.tiff"},
	{"ome-tiff", "Leica-1.ome.tiff"},
	{"ome-tiff", "Leica-2.ome.tiff"},
	{"bif", "Ventana-1.bif"},
	{"bif", "OS-1.bif"},
}

// TestMultiDimCompat2D pins the v0.24 Images()/Levels field API:
// every existing 2D fixture reports at least one Image with at least
// one Level, and level 0 (0,0) is readable via ImageRawTile.
//
// This replaces the v0.7 SizeZ/SizeC/SizeT/TileAt contract which was
// removed when Image became a value-type struct in v0.24.
func TestMultiDimCompat2D(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, fx := range slideCandidates2D {
		t.Run(fx.name, func(t *testing.T) {
			path := filepath.Join(dir, fx.subdir, fx.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			imgs := tiler.Pyramids()
			if len(imgs) == 0 {
				t.Fatal("Pyramids: empty slice")
			}
			for ii, img := range imgs {
				if len(img.Levels) == 0 {
					t.Errorf("image %d has no levels", ii)
					continue
				}
				// Exercise level 0 tile (0,0) for each image — confirms
				// the ImageRawTile dispatch through the full stack.
				b, err := tiler.ImageRawTile(img.Index, 0, 0, 0)
				if err != nil {
					t.Errorf("image %d ImageRawTile(0,0,0): %v", ii, err)
					continue
				}
				if len(b) < 2 {
					t.Errorf("image %d L0 (0,0): got %d bytes, want >= 2", ii, len(b))
				}
			}
		})
	}
}
