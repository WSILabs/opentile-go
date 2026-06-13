package ndpi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func ndpiFixture(t *testing.T, name string) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "ndpi", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

func TestNDPITIFFTags(t *testing.T) {
	s, err := opentile.OpenFile(ndpiFixture(t, "CMU-1.ndpi"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("LevelTIFFTags(0) ok=false")
	}
	if _, ok := tags.Tag(256); !ok {
		t.Error("missing ImageWidth (256)")
	}
	if _, ok := tags.Tag(324); ok {
		t.Error("TileOffsets (324) should be filtered")
	}
	dirs, ok := s.TIFFDirectories()
	if !ok || len(dirs) == 0 {
		t.Fatalf("TIFFDirectories empty: %d %v", len(dirs), ok)
	}
}

func TestNDPIMapPageInDirectories(t *testing.T) {
	s, err := opentile.OpenFile(ndpiFixture(t, "OS-2.ndpi"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dirs, ok := s.TIFFDirectories()
	if !ok {
		t.Fatal("TIFFDirectories ok=false")
	}
	// OS-2 carries a Map page; it must appear somewhere in the enumeration
	// (as DirOther or DirAssociated). Just assert the enumeration has more
	// directories than levels (i.e. extra IFDs are surfaced).
	var levelCount int
	for _, d := range dirs {
		if d.Type == opentile.DirLevel {
			levelCount++
		}
	}
	if len(dirs) <= levelCount {
		t.Errorf("expected extra non-level IFDs (overview/map) in %d dirs (levels=%d)", len(dirs), levelCount)
	}
}
