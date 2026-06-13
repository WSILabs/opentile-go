package svs_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func svsFixture(t *testing.T) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "svs", "CMU-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

func TestSVSLevelTIFFTags(t *testing.T) {
	s, err := opentile.OpenFile(svsFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("LevelTIFFTags(0) ok=false on SVS")
	}
	desc, ok := tags.Tag(270) // ImageDescription (Aperio header)
	if !ok {
		t.Fatal("level 0 missing ImageDescription (270)")
	}
	if v, ok := desc.ASCII(); !ok || len(v) == 0 {
		t.Fatalf("ImageDescription ASCII empty: %q %v", v, ok)
	}
	if _, ok := tags.Tag(324); ok {
		t.Fatal("TileOffsets (324) should be filtered out")
	}
	dirs, ok := s.TIFFDirectories()
	if !ok || len(dirs) == 0 {
		t.Fatalf("TIFFDirectories empty: %d %v", len(dirs), ok)
	}
}
