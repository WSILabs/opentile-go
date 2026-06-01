package ometiff_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func omeFixture(t *testing.T, name string) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "ome-tiff", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

func TestOMETIFFTags(t *testing.T) {
	s, err := opentile.OpenFile(omeFixture(t, "Leica-1.ome.tiff"))
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
	if dirs, ok := opentile.TIFFDirectoriesOf(s); !ok || len(dirs) == 0 {
		t.Fatalf("TIFFDirectoriesOf empty: %d %v", len(dirs), ok)
	}
}

func TestOMETIFFTagsMultiImage(t *testing.T) {
	s, err := opentile.OpenFile(omeFixture(t, "Leica-2.ome.tiff"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	imgs := s.Images()
	if len(imgs) < 2 {
		t.Skipf("Leica-2 exposed %d images; need >=2 for multi-image check", len(imgs))
	}
	// Tags must be retrievable for a NON-ZERO image index.
	last := len(imgs) - 1
	tags, ok := s.ImageLevelTIFFTags(last, 0)
	if !ok {
		t.Fatalf("ImageLevelTIFFTags(%d, 0) ok=false — multi-image not handled", last)
	}
	if _, ok := tags.Tag(256); !ok {
		t.Errorf("image %d level 0 missing ImageWidth (256)", last)
	}
}
