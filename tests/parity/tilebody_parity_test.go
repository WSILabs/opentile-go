package parity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestTileBodyReconstitutionInvariant_AllFormats walks one fixture
// per format, reads 5 sampled tiles per L0, and confirms the v0.13
// invariant:
//
//	SpliceJPEGTile(Level.TilePrefix(), Level.TileBodyInto(p)) ==byte== Level.Tile(p)
//
// Catches any format that implements TilePrefix/TileBodyInto
// inconsistently with what the existing Tile() output is.
//
// Skipped cleanly when OPENTILE_TESTDIR is unset OR the slide isn't
// on disk.
func TestTileBodyReconstitutionInvariant_AllFormats(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	for _, tc := range []struct {
		subdir, name string
	}{
		{"svs", "CMU-1-Small-Region.svs"},
		{"ndpi", "CMU-1.ndpi"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"bif", "Ventana-1.bif"}, // per-tile embedded — TilePrefix nil
		{"bif", "OS-1.bif"},      // shared — TilePrefix non-nil
		{"ife", "cervix_2x_jpeg.iris"},
		{"generic-tiff", "CMU-1.tiff"},
		{"scn", "Leica-1.scn"},
		{"scn", "Leica-2.scn"}, // multi-region; tests blank-tile via spec Q4
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer tiler.Close()

			lvl := tiler.Levels()[0]
			grid := lvl.Grid()
			if grid.W == 0 || grid.H == 0 {
				t.Skip("L0 has empty grid")
			}
			prefix := lvl.TilePrefix()
			bodyBuf := make([]byte, lvl.TileBodyMaxSize())

			positions := []struct{ x, y int }{
				{0, 0},
				{grid.W - 1, 0},
				{0, grid.H - 1},
				{grid.W - 1, grid.H - 1},
				{grid.W / 2, grid.H / 2},
			}
			for _, p := range positions {
				full, errFull := lvl.Tile(p.x, p.y)
				n, errBody := lvl.TileBodyInto(p.x, p.y, bodyBuf)
				if (errFull == nil) != (errBody == nil) {
					t.Errorf("(%d,%d): Tile err=%v, TileBodyInto err=%v", p.x, p.y, errFull, errBody)
					continue
				}
				if errFull != nil {
					continue // skip empty/missing tiles consistently
				}
				reconstituted, err := opentile.SpliceJPEGTile(prefix, bodyBuf[:n])
				if err != nil {
					t.Errorf("(%d,%d): SpliceJPEGTile: %v", p.x, p.y, err)
					continue
				}
				if !bytes.Equal(full, reconstituted) {
					t.Errorf("(%d,%d): reconstituted (%d bytes) != Tile() (%d bytes)",
						p.x, p.y, len(reconstituted), len(full))
				}
			}
		})
	}
}
