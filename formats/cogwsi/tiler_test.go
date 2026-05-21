package cogwsi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestOpen_CMU1SmallRegion is the T5 smoke test: opens the small
// COG-WSI fixture via opentile.OpenFile() (which exercises the
// formats/all registration + ghost-area dispatch) and confirms the
// returned Tiler self-reports as FormatCOGWSI.
//
// Skips when OPENTILE_TESTDIR is unset or the fixture is missing,
// to keep `go test ./...` green in CI environments without the
// sample-files directory.
func TestOpen_CMU1SmallRegion(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()
	if got := tlr.Format(); got != opentile.FormatCOGWSI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatCOGWSI)
	}
}
