package opentile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestOpenFileBackingsByteIdentical pins the contract that
// BackingMmap and BackingPread produce byte-identical Tile() output
// across every fixture in the parity slate. If they ever diverge
// the mmap path has a bug; revert the change rather than try to
// reconcile.
func TestOpenFileBackingsByteIdentical(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	fixtures := []struct {
		subdir, name string
	}{
		{"svs", "CMU-1-Small-Region.svs"},
		{"svs", "CMU-1.svs"},
		{"ndpi", "CMU-1.ndpi"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"bif", "Ventana-1.bif"},
		{"ife", "cervix_2x_jpeg.iris"},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			path := filepath.Join(dir, fx.subdir, fx.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}

			tMmap, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile (mmap): %v", err)
			}
			defer tMmap.Close()

			tPread, err := opentile.OpenFile(path, opentile.WithBacking(opentile.BackingPread))
			if err != nil {
				t.Fatalf("OpenFile (pread): %v", err)
			}
			defer tPread.Close()

			lvlMmap, _ := tMmap.Level(0)
			lvlPread, _ := tPread.Level(0)
			if lvlMmap.Size() != lvlPread.Size() {
				t.Errorf("Size mismatch: mmap=%v pread=%v", lvlMmap.Size(), lvlPread.Size())
			}

			grid := lvlMmap.Grid()
			// Sample 16 deterministic positions on level 0.
			positions := []struct{ x, y int }{
				{0, 0}, {0, grid.H - 1}, {grid.W - 1, 0}, {grid.W - 1, grid.H - 1},
				{grid.W / 2, grid.H / 2},
				{1, 1}, {grid.W / 4, grid.H / 4}, {3 * grid.W / 4, 3 * grid.H / 4},
			}
			for _, p := range positions {
				if p.x < 0 || p.y < 0 || p.x >= grid.W || p.y >= grid.H {
					continue
				}
				bm, errM := lvlMmap.Tile(p.x, p.y)
				bp, errP := lvlPread.Tile(p.x, p.y)
				if errM != nil || errP != nil {
					t.Errorf("Tile(%d,%d): mmap err=%v pread err=%v", p.x, p.y, errM, errP)
					continue
				}
				if !bytes.Equal(bm, bp) {
					t.Errorf("Tile(%d,%d): mmap %d bytes != pread %d bytes", p.x, p.y, len(bm), len(bp))
				}
			}
		})
	}
}
