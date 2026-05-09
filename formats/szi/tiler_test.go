package szi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	return filepath.Join(dir, "szi")
}

func TestOpen_CMU1(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}

	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatSZI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatSZI)
	}
}

func TestOpen_Grundium(t *testing.T) {
	path := filepath.Join(testdataDir(t), "scan_618_grundium_SZI.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("scan_618_grundium_SZI.szi not present")
	}

	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatSZI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatSZI)
	}
}
