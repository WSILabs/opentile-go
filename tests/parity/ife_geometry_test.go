package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// ifeLevelExpect captures one Level's expected geometry on an IFE
// fixture. Derived from the T1–T4 cervix gate findings (file storage
// is coarsest-first; the API is native-first, so index 0 = native).
type ifeLevelExpect struct {
	W, H         int
	TileW, TileH int
	GridW, GridH int
}

type ifeFixture struct {
	filename string
	format   opentile.Format
	levels   []ifeLevelExpect
	// L0 (0,0) tile bytes start with these magic bytes for the
	// fixture's encoding. JPEG: ff d8 (SOI).
	tileMagic []byte
}

var ifeFixtures = []ifeFixture{
	{
		filename: "cervix_2x_jpeg.iris",
		format:   opentile.FormatIFE,
		levels: []ifeLevelExpect{
			{W: 126976, H: 88576, TileW: 256, TileH: 256, GridW: 496, GridH: 346},
			{W: 63488, H: 44288, TileW: 256, TileH: 256, GridW: 248, GridH: 173},
			{W: 31744, H: 22272, TileW: 256, TileH: 256, GridW: 124, GridH: 87},
			{W: 15872, H: 11264, TileW: 256, TileH: 256, GridW: 62, GridH: 44},
			{W: 7936, H: 5632, TileW: 256, TileH: 256, GridW: 31, GridH: 22},
			{W: 4096, H: 2816, TileW: 256, TileH: 256, GridW: 16, GridH: 11},
			{W: 2048, H: 1536, TileW: 256, TileH: 256, GridW: 8, GridH: 6},
			{W: 1024, H: 768, TileW: 256, TileH: 256, GridW: 4, GridH: 3},
			{W: 512, H: 512, TileW: 256, TileH: 256, GridW: 2, GridH: 2},
		},
		tileMagic: []byte{0xFF, 0xD8}, // JPEG SOI
	},
}

// TestIFEGeometry pins per-fixture expected geometry for IFE files.
// Skipped cleanly when OPENTILE_TESTDIR is unset; otherwise locates
// the fixture under dir/ife/ and asserts level count, dimensions,
// tile size, grid, format identifier, and the first two bytes of
// L0 (0,0)'s tile (encoding-magic check).
func TestIFEGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range ifeFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "ife", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != fx.format {
				t.Errorf("Format = %v, want %v", got, fx.format)
			}
			levels := tiler.Levels()
			if len(levels) != len(fx.levels) {
				t.Fatalf("level count = %d, want %d", len(levels), len(fx.levels))
			}
			for i, exp := range fx.levels {
				lvl := levels[i]
				if got := lvl.Size(); got.W != exp.W || got.H != exp.H {
					t.Errorf("L%d Size = %v, want {W:%d H:%d}", i, got, exp.W, exp.H)
				}
				if got := lvl.TileSize(); got.W != exp.TileW || got.H != exp.TileH {
					t.Errorf("L%d TileSize = %v, want {W:%d H:%d}", i, got, exp.TileW, exp.TileH)
				}
				if got := lvl.Grid(); got.W != exp.GridW || got.H != exp.GridH {
					t.Errorf("L%d Grid = %v, want {W:%d H:%d}", i, got, exp.GridW, exp.GridH)
				}
			}

			// L0 (0,0) — encoding magic check.
			b, err := levels[0].Tile(0, 0)
			if err != nil {
				t.Fatalf("L0 Tile(0,0): %v", err)
			}
			if len(b) < len(fx.tileMagic) {
				t.Fatalf("L0 (0,0): %d bytes returned; want at least %d", len(b), len(fx.tileMagic))
			}
			for i, m := range fx.tileMagic {
				if b[i] != m {
					t.Errorf("L0 (0,0): byte %d = 0x%02x, want 0x%02x", i, b[i], m)
				}
			}

			// 2D dimensions.
			img := tiler.Images()[0]
			if got := img.SizeZ(); got != 1 {
				t.Errorf("SizeZ = %d, want 1", got)
			}
			if got := img.SizeC(); got != 1 {
				t.Errorf("SizeC = %d, want 1", got)
			}
			if got := img.SizeT(); got != 1 {
				t.Errorf("SizeT = %d, want 1", got)
			}

			// Out-of-bounds on the native level surfaces ErrTileOutOfBounds.
			lastLvl := levels[0]
			lastGrid := lastLvl.Grid()
			_, err = lastLvl.Tile(lastGrid.W, 0)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("OOB on L0: got %v, want ErrTileOutOfBounds", err)
			}
		})
	}
}
