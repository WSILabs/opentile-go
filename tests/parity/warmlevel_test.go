package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestWarmLevelBoundaries pins the v0.9 contract:
//
//   - WarmLevel(-1) and WarmLevel(N) where N == len(Levels())
//     return ErrLevelOutOfRange (or err.Is(...) match).
//   - WarmLevel on a valid level returns nil.
//   - WarmLevel on every fixture's level 0 succeeds without error.
//
// The actual page-cache effect can't be observed portably from Go;
// this test exists to gate the no-op fallback behavior + error
// contract. The cold-cache speedup is captured in the benchgate
// bench (see perf_baseline_test.go) when run with cache flushed.
func TestWarmLevelBoundaries(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	fixtures := []struct{ subdir, name string }{
		{"svs", "CMU-1-Small-Region.svs"},
		{"ndpi", "CMU-1.ndpi"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"bif", "Ventana-1.bif"},
		{"ife", "cervix_2x_jpeg.iris"},
	}

	for _, fx := range fixtures {
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

			levels := tiler.Levels()
			if err := tiler.WarmLevel(0); err != nil {
				t.Errorf("WarmLevel(0): %v", err)
			}
			// Last level — typically tiny, fast.
			if err := tiler.WarmLevel(len(levels) - 1); err != nil {
				t.Errorf("WarmLevel(last): %v", err)
			}

			if err := tiler.WarmLevel(-1); !errors.Is(err, opentile.ErrLevelOutOfRange) {
				t.Errorf("WarmLevel(-1): got %v, want ErrLevelOutOfRange", err)
			}
			if err := tiler.WarmLevel(len(levels)); !errors.Is(err, opentile.ErrLevelOutOfRange) {
				t.Errorf("WarmLevel(N): got %v, want ErrLevelOutOfRange", err)
			}
		})
	}
}
