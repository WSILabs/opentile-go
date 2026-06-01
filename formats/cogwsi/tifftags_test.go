package cogwsi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// COG-WSI wraps an inner generic-TIFF reader and exposes it via
// UnwrapReader, so the TIFF-tag provider is reached through the unwrap
// chain with no COG-WSI-specific code. This confirms that delegation.
func TestCOGWSITIFFTags(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "cog-wsi", "CMU-1_cog-wsi.tiff")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("LevelTIFFTags(0) ok=false — COG-WSI delegation to inner generic-TIFF not reaching the provider")
	}
	if _, ok := tags.Tag(256); !ok {
		t.Error("missing ImageWidth (256)")
	}
	if _, ok := tags.Tag(324); ok {
		t.Error("TileOffsets (324) should be filtered")
	}
	dirs, ok := opentile.TIFFDirectoriesOf(s)
	if !ok || len(dirs) == 0 {
		t.Fatalf("TIFFDirectoriesOf empty: %d %v", len(dirs), ok)
	}
}
