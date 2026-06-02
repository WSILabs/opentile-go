package dicom_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func leica4(t *testing.T) string {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "Leica-4")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return dir
}

func TestOpenLeica4Directory(t *testing.T) {
	s, err := opentile.OpenFile(leica4(t))
	if err != nil {
		t.Fatalf("OpenFile(dir): %v", err)
	}
	defer s.Close()
	if s.Format() != opentile.FormatDICOM {
		t.Fatalf("Format = %v", s.Format())
	}
	levels := s.Levels()
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	if levels[0].Size != (opentile.Size{W: 23374, H: 22079}) {
		t.Errorf("L0 size = %+v", levels[0].Size)
	}
	// A center tile decodes (slow-path JPEG) to the tile size.
	img, err := s.DecodedTile(2, 3, 3)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Errorf("decoded tile = %dx%d, want 256x256", img.Width, img.Height)
	}
}

func TestOpenSingleDcmExpandsToSeries(t *testing.T) {
	dir := leica4(t)
	one, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	s, err := opentile.OpenFile(one[0]) // any one instance
	if err != nil {
		t.Fatalf("OpenFile(.dcm): %v", err)
	}
	defer s.Close()
	if len(s.Levels()) != 3 {
		t.Errorf("sibling-scan levels = %d, want 3", len(s.Levels()))
	}
}
