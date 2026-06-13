package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/leicascn"
)

// scnLevelExpect captures one Level's expected geometry on a SCN
// fixture. Mirrors ifeLevelExpect / bifLevelExpect / genericLevelExpect.
type scnLevelExpect struct {
	W, H         int
	TileW, TileH int
	GridW, GridH int
}

// scnAssocExpect captures one AssociatedImage's expected shape.
// All SCN auxiliaries are Type="overview" per v0.15 (auxiliaries
// are full-slide low-mag — overview semantics — not chip-level
// macro per the IFE distinction).
type scnAssocExpect struct {
	W, H        int
	Compression opentile.Compression
}

type scnFixture struct {
	filename       string
	levels         []scnLevelExpect
	associated     []scnAssocExpect
	sizeC          int
	channelNames   []string // populated when sizeC > 1
	regionCount    int      // number of main scans (composite regions)
	scannerModel   string
	expectBarcode  string
	expectAuxIllum []string // per-auxiliary illumination source
}

var scnFixtures = []scnFixture{
	{
		filename: "Leica-1.scn",
		levels: []scnLevelExpect{
			{W: 36832, H: 38432, TileW: 512, TileH: 512, GridW: 72, GridH: 76},
			{W: 9208, H: 9608, TileW: 512, TileH: 512, GridW: 18, GridH: 19},
			{W: 2302, H: 2402, TileW: 512, TileH: 512, GridW: 5, GridH: 5},
			{W: 576, H: 600, TileW: 512, TileH: 512, GridW: 2, GridH: 2},
			{W: 144, H: 150, TileW: 512, TileH: 512, GridW: 1, GridH: 1},
		},
		associated: []scnAssocExpect{
			{W: 101, H: 291, Compression: opentile.CompressionJPEG},
		},
		sizeC:          1,
		regionCount:    1,
		scannerModel:   "Leica SCN400",
		expectBarcode:  "MDQwNTA2MjlD",
		expectAuxIllum: []string{"brightfield"},
	},
	{
		filename: "Leica-2.scn",
		// Composite L0 union extent of 4 main scans (39168×26048,
		// 39360×23360 ×2, 39168×26048) stacked vertically. After
		// tile-snapping per-region offsets, the union extent is
		// pinned at probe time.
		levels: []scnLevelExpect{
			{W: 44956, H: 139277, TileW: 512, TileH: 512, GridW: 88, GridH: 273},
			{W: 11239, H: 34819, TileW: 512, TileH: 512, GridW: 22, GridH: 69},
			{W: 2810, H: 8705, TileW: 512, TileH: 512, GridW: 6, GridH: 18},
			{W: 702, H: 2176, TileW: 512, TileH: 512, GridW: 2, GridH: 5},
			{W: 176, H: 543, TileW: 512, TileH: 512, GridW: 1, GridH: 2},
			{W: 44, H: 134, TileW: 512, TileH: 512, GridW: 1, GridH: 1},
		},
		associated: []scnAssocExpect{
			{W: 101, H: 291, Compression: opentile.CompressionJPEG},
		},
		sizeC:          1,
		regionCount:    4,
		scannerModel:   "Leica SCN400",
		expectBarcode:  "",
		expectAuxIllum: []string{"brightfield"},
	},
	{
		filename: "Leica-Fluorescence-1.scn",
		levels: []scnLevelExpect{
			{W: 4737, H: 6338, TileW: 512, TileH: 512, GridW: 10, GridH: 13},
			{W: 1184, H: 1584, TileW: 512, TileH: 512, GridW: 3, GridH: 4},
			{W: 296, H: 396, TileW: 512, TileH: 512, GridW: 1, GridH: 1},
			{W: 74, H: 99, TileW: 512, TileH: 512, GridW: 1, GridH: 1},
		},
		associated: []scnAssocExpect{
			{W: 101, H: 291, Compression: opentile.CompressionJPEG}, // brightfield macro
			{W: 101, H: 291, Compression: opentile.CompressionJPEG}, // fluorescence macro
		},
		sizeC:          3,
		channelNames:   []string{"405|Empty", "L5|Empty", "TX2|Empty"},
		regionCount:    1,
		scannerModel:   "Leica SCN400F",
		expectBarcode:  "",
		expectAuxIllum: []string{"brightfield", "fluorescence"},
	},
}

// TestSCNGeometry pins per-fixture geometry for Leica SCN files.
// Mirrors bif_geometry_test.go / ife_geometry_test.go /
// generic_geometry_test.go.
func TestSCNGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range scnFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "scn", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != opentile.FormatLeicaSCN {
				t.Errorf("Format = %v, want %v", got, opentile.FormatLeicaSCN)
			}

			// Per-level geometry.
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
				if got := lvl.Compression; got != opentile.CompressionJPEG {
					t.Errorf("L%d Compression = %v, want JPEG", i, got)
				}
			}

			// L0 (0, 0) JPEG SOI marker.
			b, err := mustLevel(t, tiler, 0).Tile(0, 0)
			if err != nil {
				t.Fatalf("L0 RawTile(0,0): %v", err)
			}
			if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
				t.Errorf("L0 (0,0) first 2 = % x, want FF D8 (JPEG SOI)", b[:2])
			}

			// Out-of-bounds.
			grid := levels[0].Grid
			_, err = mustLevel(t, tiler, 0).Tile(grid.W, 0)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("OOB on L0: got %v, want ErrTileOutOfBounds", err)
			}

			// Associated.
			assocs := tiler.AssociatedImages()
			if len(assocs) != len(fx.associated) {
				t.Errorf("associated count = %d, want %d", len(assocs), len(fx.associated))
			} else {
				for i, exp := range fx.associated {
					a := assocs[i]
					if a.Type() != "overview" {
						t.Errorf("associated[%d] Type = %q, want %q", i, a.Type(), "overview")
					}
					if got := a.Size(); got.W != exp.W || got.H != exp.H {
						t.Errorf("associated[%d] Size = %v, want {W:%d H:%d}", i, got, exp.W, exp.H)
					}
					if got := a.Compression(); got != exp.Compression {
						t.Errorf("associated[%d] Compression = %v, want %v", i, got, exp.Compression)
					}
				}
			}

			// Format-specific metadata.
			md, ok := leicascn.MetadataOf(tiler)
			if !ok {
				t.Fatal("MetadataOf returned !ok on a SCN Tiler")
			}
			if got := md.ScannerManufacturer; got != "Leica" {
				t.Errorf("ScannerManufacturer = %q, want %q", got, "Leica")
			}
			if got := md.ScannerModel; got != fx.scannerModel {
				t.Errorf("ScannerModel = %q, want %q", got, fx.scannerModel)
			}
			if got := md.Barcode; got != fx.expectBarcode {
				t.Errorf("Barcode = %q, want %q", got, fx.expectBarcode)
			}
			if got := len(md.Regions); got != fx.regionCount {
				t.Errorf("len(Regions) = %d, want %d", got, fx.regionCount)
			}
			if got := len(md.Auxiliaries); got != len(fx.expectAuxIllum) {
				t.Errorf("len(Auxiliaries) = %d, want %d", got, len(fx.expectAuxIllum))
			} else {
				for i, want := range fx.expectAuxIllum {
					if got := md.Auxiliaries[i].IlluminationSource; got != want {
						t.Errorf("Auxiliaries[%d].IlluminationSource = %q, want %q",
							i, got, want)
					}
				}
			}
		})
	}
}

// TestSCNOpenFileBackingsByteIdentical confirms tile bytes are byte-
// identical across mmap (default) and pread backings for the SCN
// reader. Mirrors v0.10's TestGenericOpenFileBackingsByteIdentical.
func TestSCNOpenFileBackingsByteIdentical(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "scn", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}

			mmapTiler, err := opentile.OpenFile(path)
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
				t.Fatalf("level count differs: mmap=%d pread=%d",
					len(mmapLevels), len(preadLevels))
			}

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
					a, errA := mustLevel(t, mmapTiler, i).Tile(p.x, p.y)
					b, errB := mustLevel(t, preadTiler, i).Tile(p.x, p.y)
					if (errA == nil) != (errB == nil) {
						t.Errorf("L%d (%d,%d): mmap err=%v, pread err=%v",
							i, p.x, p.y, errA, errB)
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
