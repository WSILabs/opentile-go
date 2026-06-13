package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// sziLevelExpect captures one Level's expected geometry on an SZI
// fixture. SZI is single-image, JPEG-only at the pyramid level, with
// tile size derived from the manifest's TileSize attribute (256 for
// CMU-1.szi, 512 for the Grundium fixture). Levels are emitted
// largest→smallest by reversing the DZI level numbering at the
// formats/szi/ Image layer.
type sziLevelExpect struct {
	W, H         int
	TileW, TileH int
	GridW, GridH int
	Compression  opentile.Compression
}

// sziAssocExpect captures one AssociatedImage's expected shape. SZI
// associated images are always JPEG (label.jpg / macro.jpg /
// thumbnail.jpg packaged inside the ZIP). ByteCount pins the raw
// JPEG byte length so a regression in the v0.16 ZIP-extraction path
// shows up as a hard failure.
type sziAssocExpect struct {
	Type        string
	W, H        int
	Compression opentile.Compression
	ByteCount   int
}

type sziFixture struct {
	filename   string
	levels     []sziLevelExpect
	associated []sziAssocExpect
	// L0 (0,0) tile bytes start with these magic bytes (JPEG SOI =
	// 0xFF 0xD8 0xFF — every SZI tile is a JPEG codestream stored as
	// an entry inside the .szi ZIP container).
	tileMagic []byte
}

var sziFixtures = []sziFixture{
	{
		// CMU-1.szi: small full-walk fixture. 13 levels, TileSize=256,
		// JPEG-compressed throughout. Three associated images
		// (overview / label / thumbnail) extracted from the ZIP and
		// surfaced byte-identical to the source JPEGs (no re-encoding).
		filename: "CMU-1.szi",
		levels: []sziLevelExpect{
			{W: 2220, H: 2967, TileW: 256, TileH: 256, GridW: 9, GridH: 12, Compression: opentile.CompressionJPEG},
			{W: 1110, H: 1484, TileW: 256, TileH: 256, GridW: 5, GridH: 6, Compression: opentile.CompressionJPEG},
			{W: 555, H: 742, TileW: 256, TileH: 256, GridW: 3, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 278, H: 371, TileW: 256, TileH: 256, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 139, H: 186, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 70, H: 93, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 35, H: 47, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 18, H: 24, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 9, H: 12, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 5, H: 6, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 3, H: 3, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 2, H: 2, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 1, H: 1, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: []sziAssocExpect{
			{Type: "overview", W: 1683, H: 610, Compression: opentile.CompressionJPEG, ByteCount: 99466},
			{Type: "label", W: 462, H: 463, Compression: opentile.CompressionJPEG, ByteCount: 45693},
			{Type: "thumbnail", W: 270, H: 374, Compression: opentile.CompressionJPEG, ByteCount: 50888},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// scan_618_grundium_SZI.szi: large sampled fixture. 19 levels,
		// TileSize=512, JPEG-compressed throughout. Produced by a
		// Grundium Ocus scanner; the level count exercises the deep-
		// pyramid math in internal/dzi (DZI L18 = full-resolution,
		// reverse-mapped to opentile L0 = full-resolution).
		filename: "scan_618_grundium_SZI.szi",
		levels: []sziLevelExpect{
			{W: 147456, H: 81920, TileW: 512, TileH: 512, GridW: 288, GridH: 160, Compression: opentile.CompressionJPEG},
			{W: 73728, H: 40960, TileW: 512, TileH: 512, GridW: 144, GridH: 80, Compression: opentile.CompressionJPEG},
			{W: 36864, H: 20480, TileW: 512, TileH: 512, GridW: 72, GridH: 40, Compression: opentile.CompressionJPEG},
			{W: 18432, H: 10240, TileW: 512, TileH: 512, GridW: 36, GridH: 20, Compression: opentile.CompressionJPEG},
			{W: 9216, H: 5120, TileW: 512, TileH: 512, GridW: 18, GridH: 10, Compression: opentile.CompressionJPEG},
			{W: 4608, H: 2560, TileW: 512, TileH: 512, GridW: 9, GridH: 5, Compression: opentile.CompressionJPEG},
			{W: 2304, H: 1280, TileW: 512, TileH: 512, GridW: 5, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 1152, H: 640, TileW: 512, TileH: 512, GridW: 3, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 576, H: 320, TileW: 512, TileH: 512, GridW: 2, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 288, H: 160, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 144, H: 80, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 72, H: 40, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 36, H: 20, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 18, H: 10, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 9, H: 5, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 5, H: 3, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 3, H: 2, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 2, H: 1, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 1, H: 1, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: []sziAssocExpect{
			{Type: "overview", W: 1200, H: 400, Compression: opentile.CompressionJPEG, ByteCount: 960000},
			{Type: "label", W: 1200, H: 848, Compression: opentile.CompressionJPEG, ByteCount: 2035200},
			{Type: "thumbnail", W: 1152, H: 640, Compression: opentile.CompressionJPEG, ByteCount: 1474560},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
}

// TestSZIGeometry pins per-fixture expected geometry for SZI files.
// Skipped cleanly when OPENTILE_TESTDIR is unset; otherwise locates
// the fixture under dir/szi/ and asserts level count, dimensions,
// tile size, grid, compression, format identifier, the L0 (0,0)
// encoding magic, and per-associated-image type / size / compression
// / byte count.
func TestSZIGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range sziFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "szi", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != opentile.FormatSZI {
				t.Errorf("Format = %v, want %v", got, opentile.FormatSZI)
			}
			levels := tiler.Levels()
			if len(levels) != len(fx.levels) {
				t.Fatalf("level count = %d, want %d", len(levels), len(fx.levels))
			}
			for i, exp := range fx.levels {
				lvl := levels[i]
				if got := lvl.Size; got.W != exp.W || got.H != exp.H {
					t.Errorf("L%d Size = %v, want {W:%d H:%d}", i, got, exp.W, exp.H)
				}
				if got := lvl.TileSize; got.W != exp.TileW || got.H != exp.TileH {
					t.Errorf("L%d TileSize = %v, want {W:%d H:%d}", i, got, exp.TileW, exp.TileH)
				}
				if got := lvl.Grid; got.W != exp.GridW || got.H != exp.GridH {
					t.Errorf("L%d Grid = %v, want {W:%d H:%d}", i, got, exp.GridW, exp.GridH)
				}
				if got := lvl.Compression; got != exp.Compression {
					t.Errorf("L%d Compression = %v, want %v", i, got, exp.Compression)
				}
			}

			// L0 (0,0) — encoding magic check.
			b, err := tiler.RawTile(0, 0, 0)
			if err != nil {
				t.Fatalf("L0 RawTile(0,0): %v", err)
			}
			if len(b) < len(fx.tileMagic) {
				t.Fatalf("L0 (0,0): %d bytes returned; want at least %d", len(b), len(fx.tileMagic))
			}
			for i, m := range fx.tileMagic {
				if b[i] != m {
					t.Errorf("L0 (0,0): byte %d = 0x%02x, want 0x%02x", i, b[i], m)
				}
			}

			// Out-of-bounds on level 0 surfaces ErrTileOutOfBounds.
			grid := levels[0].Grid
			_, err = tiler.RawTile(0, grid.W, 0)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("OOB on L0: got %v, want ErrTileOutOfBounds", err)
			}

			// Associated images.
			associated := tiler.AssociatedImages()
			if len(associated) != len(fx.associated) {
				t.Fatalf("associated count = %d, want %d", len(associated), len(fx.associated))
			}
			for i, exp := range fx.associated {
				a := associated[i]
				if a.Type() != exp.Type {
					t.Errorf("associated[%d] Type = %q, want %q", i, a.Type(), exp.Type)
				}
				if got := a.Size(); got.W != exp.W || got.H != exp.H {
					t.Errorf("associated[%d] Size = %v, want {W:%d H:%d}", i, got, exp.W, exp.H)
				}
				if got := a.Compression(); got != exp.Compression {
					t.Errorf("associated[%d] Compression = %v, want %v", i, got, exp.Compression)
				}
				bytes, err := a.Bytes()
				if err != nil {
					t.Errorf("associated[%d] Bytes(): %v", i, err)
					continue
				}
				if len(bytes) != exp.ByteCount {
					t.Errorf("associated[%d] Bytes() length = %d, want %d", i, len(bytes), exp.ByteCount)
				}
			}
		})
	}
}
