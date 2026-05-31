//go:build openslidebench

package openslideshim

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadClose(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %s", path)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	w, h := s.LevelDimensions(0)
	if w <= 0 || h <= 0 {
		t.Fatalf("LevelDimensions = %dx%d, want positive", w, h)
	}
	buf := make([]uint32, 256*256)
	if err := s.ReadRegion(buf, 0, 0, 0, 256, 256); err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
}
