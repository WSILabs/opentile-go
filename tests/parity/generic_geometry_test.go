package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/generictiff"
)

// genericLevelExpect captures one Level's expected geometry on a
// generic-TIFF fixture. Mirrors ifeLevelExpect / bifLevelExpect but
// adds Compression because the v0.10 generic reader supports a
// mixed-compression whitelist (JPEG / JP2K / LZW / Deflate / None).
type genericLevelExpect struct {
	W, H         int
	TileW, TileH int
	GridW, GridH int
	Compression  opentile.Compression
}

// genericAssocExpect captures one AssociatedImage's expected shape.
// ByteCount pins Bytes()'s length so a regression in the multi-strip
// JPEG concat (T8) or multi-strip LZW re-encode (T8) shows up as a
// hard failure, not a silent drift.
type genericAssocExpect struct {
	Type        string
	W, H        int
	Compression opentile.Compression
	ByteCount   int
}

type genericFixture struct {
	filename   string
	levels     []genericLevelExpect
	associated []genericAssocExpect
	// L0 (0,0) tile bytes start with these magic bytes (JPEG SOI =
	// 0xFF, 0xD8 — every committed generic fixture is JPEG-compressed
	// at the pyramid level).
	tileMagic []byte
}

var genericFixtures = []genericFixture{
	{
		// CMU-1.tiff: tifffile-stripped derivative of CMU-1.svs with
		// Aperio metadata removed but the original 9-level JPEG pyramid
		// preserved. No associated images in this variant.
		filename: "CMU-1.tiff",
		levels: []genericLevelExpect{
			{W: 46000, H: 32914, TileW: 256, TileH: 256, GridW: 180, GridH: 129, Compression: opentile.CompressionJPEG},
			{W: 23000, H: 16457, TileW: 256, TileH: 256, GridW: 90, GridH: 65, Compression: opentile.CompressionJPEG},
			{W: 11500, H: 8228, TileW: 256, TileH: 256, GridW: 45, GridH: 33, Compression: opentile.CompressionJPEG},
			{W: 5750, H: 4114, TileW: 256, TileH: 256, GridW: 23, GridH: 17, Compression: opentile.CompressionJPEG},
			{W: 2875, H: 2057, TileW: 256, TileH: 256, GridW: 12, GridH: 9, Compression: opentile.CompressionJPEG},
			{W: 1437, H: 1028, TileW: 256, TileH: 256, GridW: 6, GridH: 5, Compression: opentile.CompressionJPEG},
			{W: 718, H: 514, TileW: 256, TileH: 256, GridW: 3, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 359, H: 257, TileW: 256, TileH: 256, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 179, H: 128, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		tileMagic: []byte{0xFF, 0xD8},
	},
	{
		// CMU-1.stripped.tiff: T2-generated derivative re-encoding the
		// 3 associated IFDs (thumbnail / label / macro) as STRIPPED
		// TIFFs to exercise the multi-strip readers (T8). The pyramid
		// is preserved at 4× scale steps so only 3 levels survive
		// (matching the source SVS's 3-level chain).
		filename: "CMU-1.stripped.tiff",
		levels: []genericLevelExpect{
			{W: 46000, H: 32914, TileW: 256, TileH: 256, GridW: 180, GridH: 129, Compression: opentile.CompressionJPEG},
			{W: 11500, H: 8228, TileW: 256, TileH: 256, GridW: 45, GridH: 33, Compression: opentile.CompressionJPEG},
			{W: 2875, H: 2057, TileW: 256, TileH: 256, GridW: 12, GridH: 9, Compression: opentile.CompressionJPEG},
		},
		associated: []genericAssocExpect{
			// thumbnail: 46-strip JPEG → concat-strip path (T8); pinned
			// byte count is the libtiff-default RST-marker layout's
			// concatenated length, equal to the original SVS thumbnail
			// JPEG (143,874 bytes).
			{Type: generictiff.TypeThumbnail, W: 1024, H: 732, Compression: opentile.CompressionJPEG, ByteCount: 143874},
			// label: multi-strip LZW → decode-each + re-encode-as-single
			// LZW (T8). Byte count varies with the LZW writer's coding;
			// our internal/tifflzw writer produces 368,759 bytes.
			//
			// (continued below — original list left unchanged; new
			// fixtures added after CMU-1.stripped's macro entry.)
			// A drift here would indicate the LZW writer's behavior
			// changed and parity needs re-checking.
			{Type: generictiff.TypeLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			// macro: 27-strip JPEG → concat-strip path.
			{Type: generictiff.TypeOverview, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 87345},
		},
		tileMagic: []byte{0xFF, 0xD8},
	},
	{
		// scan_619 (v0.11): single-IFD tiled BigTIFF from a Grundium
		// Ocus scanner. Exercises the v0.11 R1 relaxation
		// (MinLevels=1 — admits single-level pyramids). One pyramid
		// level, no associated images.
		filename: "scan_619_grundium_pyramid_TIFF.tif",
		levels: []genericLevelExpect{
			{W: 43008, H: 27136, TileW: 512, TileH: 512, GridW: 84, GridH: 53, Compression: opentile.CompressionJPEG},
		},
		tileMagic: []byte{0xFF, 0xD8},
	},
	{
		// scan_620 (v0.11): 4-IFD mixed-ratio chain (1×, 4×, 8×, 16×)
		// from a Grundium Ocus scanner. Exercises the v0.11 R2
		// relaxation (LeftoverTiledMaxAreaRatio=0.05 — admits the
		// 8× orphan, which is 1.56% of baseline). The greedy chain
		// picks the longest geometric chain (L0+L1+L3 at 4×/4×); the
		// orphan IFD2 is silently dropped from Associated() because
		// generictiff's associated reader doesn't handle tiled IFDs
		// in v0.11 (documented divergence from the spec wording —
		// see docs/formats/generictiff.md).
		filename: "scan_620_grundium_TIFF.tif",
		levels: []genericLevelExpect{
			// v0.19 (Issue #5 part B): T3's integer-multiple ratio
			// acceptance restored the L2 IFD (6144×4096) that the
			// pre-v0.19 strict drift check dropped as a 4× orphan
			// after the initial 4× step. The chain now reads as the
			// real-on-disk 49152 → 12288 → 6144 → 3072 (4-level),
			// not the pre-fix 3-level 49152 → 12288 → 3072.
			{W: 49152, H: 32768, TileW: 512, TileH: 512, GridW: 96, GridH: 64, Compression: opentile.CompressionJPEG},
			{W: 12288, H: 8192, TileW: 512, TileH: 512, GridW: 24, GridH: 16, Compression: opentile.CompressionJPEG},
			{W: 6144, H: 4096, TileW: 512, TileH: 512, GridW: 12, GridH: 8, Compression: opentile.CompressionJPEG},
			{W: 3072, H: 2048, TileW: 512, TileH: 512, GridW: 6, GridH: 4, Compression: opentile.CompressionJPEG},
		},
		tileMagic: []byte{0xFF, 0xD8},
	},
	{
		// avif-out.tiff (v0.14): wsi-tools transcode of CMU-1-Small-
		// Region.svs to AVIF tile codec. Tag 60001 → CompressionAVIF.
		// Single-level pyramid + 3 stripped associated images
		// preserved from the source SVS. Tile magic is the AVIF
		// ftyp box header (`....ftypavif`).
		filename: "avif-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionAVIF},
		},
		associated: []genericAssocExpect{
			{Type: generictiff.TypeThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 194919},
			{Type: generictiff.TypeLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: generictiff.TypeOverview, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86655},
		},
		tileMagic: []byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70, 0x61, 0x76, 0x69, 0x66},
	},
	{
		// htj2k-out.tiff (v0.14): tag 60003 → CompressionHTJ2K.
		// Tile magic is the JPEG 2000 codestream SOC + SIZ marker
		// pair (FF 4F FF 51).
		filename: "htj2k-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionHTJ2K},
		},
		associated: []genericAssocExpect{
			{Type: generictiff.TypeThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 194919},
			{Type: generictiff.TypeLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: generictiff.TypeOverview, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86655},
		},
		tileMagic: []byte{0xFF, 0x4F, 0xFF, 0x51},
	},
	{
		// jxl-out.tiff (v0.14): tag 50002 → CompressionJPEGXL.
		// Tile magic is the JPEG XL naked-codestream signature
		// (FF 0A).
		filename: "jxl-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionJPEGXL},
		},
		associated: []genericAssocExpect{
			{Type: generictiff.TypeThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 194919},
			{Type: generictiff.TypeLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: generictiff.TypeOverview, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86655},
		},
		tileMagic: []byte{0xFF, 0x0A},
	},
	{
		// webp-out.tiff (v0.14): tag 50001 → CompressionWebP.
		// Tile magic is the RIFF container header ("RIFF").
		filename: "webp-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionWebP},
		},
		associated: []genericAssocExpect{
			{Type: generictiff.TypeThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: 194919},
			{Type: generictiff.TypeLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: 368759},
			{Type: generictiff.TypeOverview, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: 86655},
		},
		tileMagic: []byte{0x52, 0x49, 0x46, 0x46},
	},
}

// TestGenericGeometry pins per-fixture expected geometry for generic-
// TIFF fixtures. Skipped cleanly when OPENTILE_TESTDIR is unset;
// otherwise locates the fixture under dir/generic-tiff/ and asserts
// level count, dimensions, tile size, grid, compression, format
// identifier, the L0 (0,0) encoding magic, and per-associated-image
// kind / size / compression / byte count.
func TestGenericGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range genericFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "generic-tiff", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != opentile.FormatGenericTIFF {
				t.Errorf("Format = %v, want %v", got, opentile.FormatGenericTIFF)
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

// TestGenericOpenFileBackingsByteIdentical confirms tile bytes are
// byte-identical across the mmap (default) and pread backings for
// the generic-TIFF reader. Mirrors v0.9's TestOpenFileBackingsByte
// Identical for the other formats; closes the same loop for the
// new generic reader.
func TestGenericOpenFileBackingsByteIdentical(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, name := range []string{"CMU-1.tiff", "CMU-1.stripped.tiff"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "generic-tiff", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}

			mmapTiler, err := opentile.OpenFile(path) // mmap default
			if err != nil {
				t.Fatalf("OpenFile mmap: %v", err)
			}
			defer mmapTiler.Close()

			preadTiler, err := opentile.OpenFile(path, opentile.WithBacking(opentile.BackingPread))
			if err != nil {
				t.Fatalf("OpenFile pread: %v", err)
			}
			defer preadTiler.Close()

			mmapLevels := mmapTiler.Levels()
			preadLevels := preadTiler.Levels()
			if len(mmapLevels) != len(preadLevels) {
				t.Fatalf("level count differs: mmap=%d pread=%d", len(mmapLevels), len(preadLevels))
			}
			// Sample 4 deterministic positions per level (corners +
			// center) — full-walk parity is covered by TestSlideParity.
			for i, lvl := range mmapLevels {
				grid := lvl.Grid
				if grid.W == 0 || grid.H == 0 {
					continue
				}
				positions := []struct{ x, y int }{
					{0, 0},
					{grid.W - 1, 0},
					{0, grid.H - 1},
					{grid.W - 1, grid.H - 1},
				}
				if grid.W > 2 && grid.H > 2 {
					positions = append(positions, struct{ x, y int }{grid.W / 2, grid.H / 2})
				}
				for _, p := range positions {
					a, errA := mmapTiler.RawTile(i, p.x, p.y)
					b, errB := preadTiler.RawTile(i, p.x, p.y)
					if (errA == nil) != (errB == nil) {
						t.Errorf("L%d (%d,%d): mmap err=%v, pread err=%v", i, p.x, p.y, errA, errB)
						continue
					}
					if errA != nil {
						continue
					}
					if !equalBytes(a, b) {
						t.Errorf("L%d (%d,%d): mmap %d bytes != pread %d bytes",
							i, p.x, p.y, len(a), len(b))
					}
				}
			}
		})
	}
}

// equalBytes is a non-allocating bytes.Equal alias to keep the test
// focused (no extra import).
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
