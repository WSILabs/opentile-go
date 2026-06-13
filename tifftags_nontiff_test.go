package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestNonTIFFReturnsFalse(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	szi := filepath.Join(dir, "szi", "CMU-1.szi")
	if _, err := os.Stat(szi); err != nil {
		t.Skipf("fixture missing: %s", szi)
	}
	s, err := opentile.OpenFile(szi)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := mustLevel(t, s, 0).TIFFTags(); ok {
		t.Fatal("SZI (non-TIFF) LevelTIFFTags should be ok=false")
	}
	if _, ok := s.TIFFDirectories(); ok {
		t.Fatal("SZI (non-TIFF) TIFFDirectories should be ok=false")
	}
}
