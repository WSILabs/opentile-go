package leicascn_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestLeicaSCNTIFFTags(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "scn", "Leica-1.scn")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lvl0, lerr := s.Level(0)
	if lerr != nil {
		t.Fatalf("Level(0): %v", lerr)
	}
	tags, ok := lvl0.TIFFTags()
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
