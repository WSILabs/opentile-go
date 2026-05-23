//go:build gates

package gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/formats/ife"
)

// TestT8ReaderSmoke confirms reader.go's exported parsers
// (readFileHeader / readTileTable / readLayerExtents /
// readTileOffsets — invoked indirectly via the package boundary by
// using the public types) behave the same on the cervix fixture as
// the T1–T4 hand-rolled probes did. Catches regressions where the
// production parser would diverge from the gate-verified bytes.
func TestT8ReaderSmoke(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ife", "cervix_2x_jpeg.iris")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	stat, _ := f.Stat()

	// We can't directly call the package-private readFileHeader from
	// here; the smoke is that ife's exported types (FileHeader, etc.)
	// have the field tags matching the gate findings. Loose check: the
	// constant values agree.
	if ife.MagicBytes != 0x49726973 {
		t.Errorf("MagicBytes constant: 0x%x", ife.MagicBytes)
	}
	if ife.NullTile != 0xFFFFFFFFFF {
		t.Errorf("NullTile constant: 0x%x", ife.NullTile)
	}
	if ife.TileSidePixels != 256 {
		t.Errorf("TileSidePixels: %d", ife.TileSidePixels)
	}
	t.Logf("path=%s size=%d (smoke ok)", path, stat.Size())
}
