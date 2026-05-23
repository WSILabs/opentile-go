package parity

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestTileEqualsTileInto pins the v0.9 contract that Tile() and
// TileInto() return byte-identical output for every fixture across
// every level. If they ever diverge the TileInto impl has a bug;
// revert rather than reconcile.
func TestTileEqualsTileInto(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	fixtures := []struct{ subdir, name string }{
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
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			rng := rand.New(rand.NewSource(0))
			for li, lvl := range tiler.Levels() {
				grid := lvl.Grid()
				if grid.W == 0 || grid.H == 0 {
					continue
				}
				maxSize := lvl.TileMaxSize()
				if maxSize <= 0 {
					t.Errorf("L%d TileMaxSize = %d, want > 0", li, maxSize)
					continue
				}

				// Sample up to 32 random positions per level.
				positions := []struct{ x, y int }{
					{0, 0}, {grid.W - 1, 0}, {0, grid.H - 1}, {grid.W - 1, grid.H - 1},
				}
				for i := 0; i < 28 && grid.W > 0 && grid.H > 0; i++ {
					positions = append(positions, struct{ x, y int }{rng.Intn(grid.W), rng.Intn(grid.H)})
				}

				buf := make([]byte, maxSize)
				for _, p := range positions {
					a, errA := lvl.Tile(p.x, p.y)
					n, errB := lvl.TileInto(p.x, p.y, buf)
					if (errA == nil) != (errB == nil) {
						t.Errorf("L%d (%d,%d): Tile err=%v TileInto err=%v", li, p.x, p.y, errA, errB)
						continue
					}
					if errA != nil {
						continue
					}
					if !bytes.Equal(a, buf[:n]) {
						t.Errorf("L%d (%d,%d): Tile %d bytes != TileInto %d bytes",
							li, p.x, p.y, len(a), n)
					}
				}
			}
		})
	}
}

// TestTileIntoShortBuffer pins io.ErrShortBuffer behavior across
// formats. Calling TileInto with a tiny dst must error rather than
// truncate output silently.
func TestTileIntoShortBuffer(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	// CMU-1.svs is the smallest universally-present fixture; one
	// case is enough — the io.ErrShortBuffer path is identical
	// across formats.
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	tiler, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tiler.Close()
	lvl, _ := tiler.Level(0)

	tiny := make([]byte, 1)
	_, err = lvl.TileInto(0, 0, tiny)
	if !errors.Is(err, io.ErrShortBuffer) {
		t.Errorf("TileInto with len(dst)=1: got %v, want io.ErrShortBuffer", err)
	}
}
