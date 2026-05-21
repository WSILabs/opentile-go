package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// cogwsiLevelExpect captures one Level's expected geometry on a
// COG-WSI fixture. COG-WSI preserves the source format's pyramid
// bit-exact per spec — geometry should match what the original
// reader reports for the corresponding source slide.
type cogwsiLevelExpect struct {
	W, H         int
	TileW, TileH int
	GridW, GridH int
	Compression  opentile.Compression
}

// cogwsiAssocExpect captures one AssociatedImage's expected shape.
// Associated-image compression varies — wsitools commonly emits LZW
// for label crops and JPEG for thumbnail / overview.
type cogwsiAssocExpect struct {
	Type        string
	W, H        int
	Compression opentile.Compression
	ByteCount   int
}

type cogwsiFixture struct {
	filename   string
	levels     []cogwsiLevelExpect
	associated []cogwsiAssocExpect
	// L0 (0,0) tile bytes start with these magic bytes (JPEG SOI =
	// 0xFF 0xD8 0xFF; J2K codestream = 0xFF 0x4F 0xFF 0x51).
	tileMagic []byte
}

var cogwsiFixtures = []cogwsiFixture{
	{
		// CMU-1-Small-Region_cog-wsi.tiff: wsitools-converted from
		// CMU-1-Small-Region.svs. Single-level (no pyramid required at
		// that size). JPEG tiles.
		filename: "CMU-1-Small-Region_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 194919},
			{Type: "label", W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: "overview", W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86655},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// CMU-1_cog-wsi.tiff: wsitools-converted from CMU-1.svs. Three
		// levels (4× pyramid). JPEG tiles.
		filename: "CMU-1_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 46000, H: 32914, TileW: 256, TileH: 256, GridW: 180, GridH: 129, Compression: opentile.CompressionJPEG},
			{W: 11500, H: 8228, TileW: 256, TileH: 256, GridW: 45, GridH: 33, Compression: opentile.CompressionJPEG},
			{W: 2875, H: 2057, TileW: 256, TileH: 256, GridW: 12, GridH: 9, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 1024, H: 732, Compression: opentile.CompressionJPEG, ByteCount: 142606},
			{Type: "label", W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: "overview", W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86742},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// JP2K-33003-1_cog-wsi.tiff: wsitools-converted from
		// JP2K-33003-1.svs. JP2K codestream tiles (no codec required —
		// passthrough). L0 (0,0) magic = J2K SOC marker.
		filename: "JP2K-33003-1_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 15374, H: 17497, TileW: 256, TileH: 256, GridW: 61, GridH: 69, Compression: opentile.CompressionJP2K},
			{W: 3843, H: 4374, TileW: 256, TileH: 256, GridW: 16, GridH: 18, Compression: opentile.CompressionJP2K},
			{W: 1921, H: 2187, TileW: 256, TileH: 256, GridW: 8, GridH: 9, Compression: opentile.CompressionJP2K},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 674, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 142754},
			{Type: "label", W: 415, H: 422, Compression: opentile.CompressionLZW, ByteCount: 333589},
			{Type: "overview", W: 1280, H: 421, Compression: opentile.CompressionJPEG, ByteCount: 60127},
		},
		tileMagic: []byte{0xFF, 0x4F, 0xFF, 0x51},
	},
	{
		// scan_617_cog-wsi.tiff: wsitools-converted from a Grundium
		// scan_617 source. 4 levels, 512px tiles, JPEG.
		filename: "scan_617_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 49152, H: 32768, TileW: 512, TileH: 512, GridW: 96, GridH: 64, Compression: opentile.CompressionJPEG},
			{W: 12288, H: 8192, TileW: 512, TileH: 512, GridW: 24, GridH: 16, Compression: opentile.CompressionJPEG},
			{W: 6144, H: 4096, TileW: 512, TileH: 512, GridW: 12, GridH: 8, Compression: opentile.CompressionJPEG},
			{W: 3072, H: 2048, TileW: 512, TileH: 512, GridW: 6, GridH: 4, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 1536, H: 1024, Compression: opentile.CompressionJPEG, ByteCount: 3145734},
			{Type: "label", W: 1200, H: 848, Compression: opentile.CompressionLZW, ByteCount: 830684},
			{Type: "overview", W: 1200, H: 400, Compression: opentile.CompressionJPEG, ByteCount: 117665},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// scan_620_cog-wsi.tiff: wsitools-converted from scan_620_.svs.
		// 4 levels, 512px tiles, JPEG.
		filename: "scan_620_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 49152, H: 32768, TileW: 512, TileH: 512, GridW: 96, GridH: 64, Compression: opentile.CompressionJPEG},
			{W: 12288, H: 8192, TileW: 512, TileH: 512, GridW: 24, GridH: 16, Compression: opentile.CompressionJPEG},
			{W: 6144, H: 4096, TileW: 512, TileH: 512, GridW: 12, GridH: 8, Compression: opentile.CompressionJPEG},
			{W: 3072, H: 2048, TileW: 512, TileH: 512, GridW: 6, GridH: 4, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 1536, H: 1024, Compression: opentile.CompressionJPEG, ByteCount: 3145734},
			{Type: "label", W: 1200, H: 848, Compression: opentile.CompressionLZW, ByteCount: 834864},
			{Type: "overview", W: 1200, H: 400, Compression: opentile.CompressionJPEG, ByteCount: 116344},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// svs_40x_bigtiff_cog-wsi.tiff: wsitools-converted from
		// svs_40x_bigtiff.svs. BigTIFF, 4 levels, 512px tiles, JPEG.
		// No label (source was extracted as JPEG-only overview /
		// thumbnail per wsitools' choices).
		filename: "svs_40x_bigtiff_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 188416, H: 106496, TileW: 512, TileH: 512, GridW: 368, GridH: 208, Compression: opentile.CompressionJPEG},
			{W: 47104, H: 26624, TileW: 512, TileH: 512, GridW: 92, GridH: 52, Compression: opentile.CompressionJPEG},
			{W: 23552, H: 13312, TileW: 512, TileH: 512, GridW: 46, GridH: 26, Compression: opentile.CompressionJPEG},
			{W: 11776, H: 6656, TileW: 512, TileH: 512, GridW: 23, GridH: 13, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 1472, H: 832, Compression: opentile.CompressionJPEG, ByteCount: 2449414},
			{Type: "overview", W: 1200, H: 400, Compression: opentile.CompressionJPEG, ByteCount: 218035},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// Leica-1_cog-wsi.tiff: wsitools-converted from Leica-1.ome.tiff.
		// 5 levels, 512px tiles, JPEG. Single overview associated image
		// (Leica-1 source had a macro image; wsitools surfaces it as
		// `overview` per the v0.15 canonical naming).
		filename: "Leica-1_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 36832, H: 38432, TileW: 512, TileH: 512, GridW: 72, GridH: 76, Compression: opentile.CompressionJPEG},
			{W: 9208, H: 9608, TileW: 512, TileH: 512, GridW: 18, GridH: 19, Compression: opentile.CompressionJPEG},
			{W: 2302, H: 2402, TileW: 512, TileH: 512, GridW: 5, GridH: 5, Compression: opentile.CompressionJPEG},
			{W: 576, H: 600, TileW: 512, TileH: 512, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 144, H: 150, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "overview", W: 1616, H: 4668, Compression: opentile.CompressionJPEG, ByteCount: 864},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// Philips-1_cog-wsi.tiff: wsitools-converted from Philips-1.tiff.
		// 8 levels, 512px tiles, JPEG. Source had no associated images;
		// wsitools faithfully emits none.
		filename: "Philips-1_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 44981, H: 35783, TileW: 512, TileH: 512, GridW: 88, GridH: 70, Compression: opentile.CompressionJPEG},
			{W: 22491, H: 17892, TileW: 512, TileH: 512, GridW: 44, GridH: 35, Compression: opentile.CompressionJPEG},
			{W: 11246, H: 8946, TileW: 512, TileH: 512, GridW: 22, GridH: 18, Compression: opentile.CompressionJPEG},
			{W: 5623, H: 4473, TileW: 512, TileH: 512, GridW: 11, GridH: 9, Compression: opentile.CompressionJPEG},
			{W: 2812, H: 2237, TileW: 512, TileH: 512, GridW: 6, GridH: 5, Compression: opentile.CompressionJPEG},
			{W: 1406, H: 1119, TileW: 512, TileH: 512, GridW: 3, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 703, H: 560, TileW: 512, TileH: 512, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 352, H: 280, TileW: 512, TileH: 512, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: nil,
		tileMagic:  []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// Ventana-1_cog-wsi.tiff: wsitools-converted from Ventana-1.bif.
		// 8 levels, 1024px tiles, JPEG. Single uncompressed overview
		// (wsitools chose uncompressed for the macro image — large byte
		// count is correct).
		filename: "Ventana-1_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 24576, H: 21504, TileW: 1024, TileH: 1024, GridW: 24, GridH: 21, Compression: opentile.CompressionJPEG},
			{W: 12288, H: 10752, TileW: 1024, TileH: 1024, GridW: 12, GridH: 11, Compression: opentile.CompressionJPEG},
			{W: 6144, H: 5376, TileW: 1024, TileH: 1024, GridW: 6, GridH: 6, Compression: opentile.CompressionJPEG},
			{W: 3072, H: 2688, TileW: 1024, TileH: 1024, GridW: 3, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 1536, H: 1344, TileW: 1024, TileH: 1024, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 768, H: 672, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 384, H: 336, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 192, H: 168, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "overview", W: 1251, H: 3685, Compression: opentile.CompressionNone, ByteCount: 13829805},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	{
		// cervix_2x_jpeg_cog-wsi.tiff: wsitools-converted from
		// cervix_2x_jpeg.iris (Iris IFE source). 9 levels, 256px tiles,
		// JPEG. Single thumbnail; no label / overview (IFE source
		// carries no separate label).
		filename: "cervix_2x_jpeg_cog-wsi.tiff",
		levels: []cogwsiLevelExpect{
			{W: 126976, H: 88576, TileW: 256, TileH: 256, GridW: 496, GridH: 346, Compression: opentile.CompressionJPEG},
			{W: 63488, H: 44288, TileW: 256, TileH: 256, GridW: 248, GridH: 173, Compression: opentile.CompressionJPEG},
			{W: 31744, H: 22272, TileW: 256, TileH: 256, GridW: 124, GridH: 87, Compression: opentile.CompressionJPEG},
			{W: 15872, H: 11264, TileW: 256, TileH: 256, GridW: 62, GridH: 44, Compression: opentile.CompressionJPEG},
			{W: 7936, H: 5632, TileW: 256, TileH: 256, GridW: 31, GridH: 22, Compression: opentile.CompressionJPEG},
			{W: 4096, H: 2816, TileW: 256, TileH: 256, GridW: 16, GridH: 11, Compression: opentile.CompressionJPEG},
			{W: 2048, H: 1536, TileW: 256, TileH: 256, GridW: 8, GridH: 6, Compression: opentile.CompressionJPEG},
			{W: 1024, H: 768, TileW: 256, TileH: 256, GridW: 4, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 512, H: 512, TileW: 256, TileH: 256, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
		},
		associated: []cogwsiAssocExpect{
			{Type: "thumbnail", W: 1920, H: 1337, Compression: opentile.CompressionJPEG, ByteCount: 327794},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
}

// TestCOGWSIGeometry pins per-fixture expected geometry for COG-WSI
// files. Skipped cleanly when OPENTILE_TESTDIR is unset; otherwise
// locates the fixture under dir/cog-wsi/ and asserts level count,
// dimensions, tile size, grid, compression, format identifier, the
// L0 (0,0) encoding magic, and per-associated-image kind / size /
// compression / byte count.
func TestCOGWSIGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range cogwsiFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "cog-wsi", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != opentile.FormatCOGWSI {
				t.Errorf("Format = %v, want %v", got, opentile.FormatCOGWSI)
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
				if got := lvl.Compression(); got != exp.Compression {
					t.Errorf("L%d Compression = %v, want %v", i, got, exp.Compression)
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

			// 2D dimensions — COG-WSI is single-image, single-Z/C/T per spec.
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

			// Out-of-bounds on level 0 surfaces ErrTileOutOfBounds.
			grid := levels[0].Grid()
			_, err = levels[0].Tile(grid.W, 0)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("OOB on L0: got %v, want ErrTileOutOfBounds", err)
			}

			// Associated images.
			associated := tiler.Associated()
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
