// Package bench holds the shared benchmark fixture matrix and helpers
// used by both the Go-benchmark layer (bench/*_test.go) and the
// cross-language compare harness (cmd/bench/compare).
package bench

import (
	"os"
	"path/filepath"
	"time"
)

// FormatEntry describes one format's representative benchmark fixture
// and which external references can also read it.
type FormatEntry struct {
	Format    string // opentile-go format id (matches Slide.Format())
	Fixture   string // path relative to OPENTILE_TESTDIR
	Openslide bool   // openslide can read it (competitive on ReadRegion)
	Python    bool   // python opentile can read it (competitive on Tile)
}

// Matrix is the canonical benchmark fixture set: one representative
// fixture per supported format. Overlap flags drive the competitive
// axes; the harness still skips gracefully if a reader can't open a
// given fixture.
var Matrix = []FormatEntry{
	{"svs", "svs/CMU-1.svs", true, true},
	{"ndpi", "ndpi/CMU-1.ndpi", true, true},
	{"philips-tiff", "philips-tiff/Philips-1.tiff", true, true},
	{"ome-tiff", "ome-tiff/Leica-1.ome.tiff", false, true},
	{"bif", "bif/Ventana-1.bif", true, false},
	{"ife", "ife/cervix_2x_jpeg.iris", false, false},
	{"generic-tiff", "generic-tiff/CMU-1.tiff", true, false},
	{"leica-scn", "scn/Leica-1.scn", true, false},
	{"szi", "szi/CMU-1.szi", false, false},
	{"cog-wsi", "cog-wsi/CMU-1_cog-wsi.tiff", false, false},
}

// testDir returns the fixture root. Honors OPENTILE_TESTDIR (the
// Makefile sets it absolutely); defaults to ./sample_files.
func testDir() string {
	if d := os.Getenv("OPENTILE_TESTDIR"); d != "" {
		return d
	}
	return "sample_files"
}

// FixturePath resolves an entry's fixture path and reports whether the
// file exists on disk.
func FixturePath(rel string) (string, bool) {
	p := filepath.Join(testDir(), rel)
	if _, err := os.Stat(p); err != nil {
		return p, false
	}
	return p, true
}

// MpixPerSec returns megapixels/second for pixels processed over d.
// Returns 0 for non-positive d (no division by zero).
func MpixPerSec(pixels int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(pixels) / d.Seconds() / 1e6
}
