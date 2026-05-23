package parity

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// slideCandidates2D lists every fixture in our local sample set
// that should report SizeZ/SizeC/SizeT == 1 — i.e., 2D pathology
// slides. Inlined here rather than imported from tests_test (a
// separate package) because the duplication is small and avoids a
// public-helper refactor that no third consumer needs yet.
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

// TestMultiDimCompat2D pins the v0.7 multi-dim API's backward-
// compatibility contract: every existing 2D fixture reports
// SizeZ/SizeC/SizeT == 1, and Tile(x, y) ≡ TileAt(TileCoord{X: x,
// Y: y}) byte-identically on the level-0 (0, 0) tile of every
// Image.
//
// A failure here is a regression in the v0.7 multi-dim work — not
// a fixture-update opportunity. Existing TestSlideParity hashes
// stay green because they hash Tile(x, y) output and Tile is
// unchanged behaviorally.
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

			imgs := tiler.Images()
			if len(imgs) == 0 {
				t.Fatal("Images: empty slice")
			}
			for ii, img := range imgs {
				// 2D-format invariants: every dimension is 1.
				if got := img.SizeZ(); got != 1 {
					t.Errorf("image %d SizeZ: got %d, want 1 (2D fixture)", ii, got)
				}
				if got := img.SizeC(); got != 1 {
					t.Errorf("image %d SizeC: got %d, want 1 (no fluorescence in v0.7 fixture set)", ii, got)
				}
				if got := img.SizeT(); got != 1 {
					t.Errorf("image %d SizeT: got %d, want 1 (no time series in v0.7 fixture set)", ii, got)
				}
				if got := img.ChannelName(0); got != "" {
					t.Errorf("image %d ChannelName(0): got %q, want \"\" (brightfield)", ii, got)
				}
				if got := img.ZPlaneFocus(0); got != 0 {
					t.Errorf("image %d ZPlaneFocus(0): got %v, want 0 (nominal)", ii, got)
				}

				// Exercise every level of the image so the
				// 2D-delegate Level.TileAt impl is covered for all
				// concrete level types (OME OneFrame L2+, NDPI
				// striped, Philips tiled, etc.) — not just L0.
				for li, lvl := range img.Levels() {
					a, errA := lvl.Tile(0, 0)
					b, errB := lvl.TileAt(opentile.TileCoord{X: 0, Y: 0})
					if errA != nil || errB != nil {
						t.Errorf("image %d L%d: Tile err=%v, TileAt err=%v", ii, li, errA, errB)
						continue
					}
					if !bytes.Equal(a, b) {
						t.Errorf("image %d L%d: Tile(0,0) and TileAt({X:0,Y:0}) bytes differ (lengths %d/%d)",
							ii, li, len(a), len(b))
					}
					// Non-zero Z/C/T on a 2D Image: ErrDimensionUnavailable.
					_, err := lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, Z: 1})
					if !errors.Is(err, opentile.ErrDimensionUnavailable) {
						t.Errorf("image %d L%d TileAt(Z=1): got %v, want errors.Is(ErrDimensionUnavailable)", ii, li, err)
					}
					_, err = lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, C: 1})
					if !errors.Is(err, opentile.ErrDimensionUnavailable) {
						t.Errorf("image %d L%d TileAt(C=1): got %v, want errors.Is(ErrDimensionUnavailable)", ii, li, err)
					}
					_, err = lvl.TileAt(opentile.TileCoord{X: 0, Y: 0, T: 1})
					if !errors.Is(err, opentile.ErrDimensionUnavailable) {
						t.Errorf("image %d L%d TileAt(T=1): got %v, want errors.Is(ErrDimensionUnavailable)", ii, li, err)
					}
				}
			}
		})
	}
}
