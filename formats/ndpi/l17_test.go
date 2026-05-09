package ndpi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestL17NDPILabelFullHeight locks in the L17 fix: NDPI label cropH
// uses the FULL image height (matching Python's crop_parameters[3] =
// page.shape[0]), not the MCU-floored height. libjpeg-turbo's
// TJXOPT_PERFECT accepts the partial last MCU row when the crop ends
// exactly at the image edge — same way Python's plain crop path
// (NOT CUSTOMFILTER, because __need_fill_background returns False
// when crop_y + crop_h == image_h) handles ragged labels.
//
// Pre-fix: OS-2.ndpi label is 344x392 (392 = mcuH×49 with mcuH=8);
// Hamamatsu-1.ndpi label is 640x728 (728 = 8×91). Last partial MCU
// row dropped.
//
// Post-fix: OS-2 → 344x396; Hamamatsu-1 → 640x732. Matches Python
// opentile byte-for-byte (label parity is currently skipped via L10
// because Python multi-strip labels return strip 0, but for NDPI
// labels the L10 carve-out doesn't apply; sizes must match).
func TestL17NDPILabelFullHeight(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	cases := []struct {
		slide string
		wantW int
		wantH int
	}{
		{"OS-2.ndpi", 344, 396},
		{"Hamamatsu-1.ndpi", 640, 732},
	}
	for _, tc := range cases {
		t.Run(tc.slide, func(t *testing.T) {
			slide := filepath.Join(dir, "ndpi", tc.slide)
			if _, err := os.Stat(slide); err != nil {
				t.Skipf("slide not present: %v", err)
			}
			tiler, err := opentile.OpenFile(slide)
			if err != nil {
				t.Fatal(err)
			}
			defer tiler.Close()
			var label opentile.AssociatedImage
			for _, a := range tiler.Associated() {
				if a.Type() == "label" {
					label = a
					break
				}
			}
			if label == nil {
				t.Fatalf("%s: no label associated image found", tc.slide)
			}
			got := label.Size()
			if got.W != tc.wantW || got.H != tc.wantH {
				t.Errorf("%s label size: got %dx%d, want %dx%d",
					tc.slide, got.W, got.H, tc.wantW, tc.wantH)
			}
		})
	}
}
