// Package parity holds no-build-tag regression tests that assert
// per-fixture geometry without requiring Python tooling. v0.7's
// addition is BIF; future formats can land their own files here.
//
// All tests skip cleanly when OPENTILE_TESTDIR is unset, so this
// suite is part of `make test` (no special tags) without breaking
// CI environments that don't ship the integration fixtures.
package parity

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/bif"
)

// bifLevelExpect captures one Level's expected geometry. Values
// were derived from the T13 real-fixture smoke test (commit
// 439f435); changing them here without a corresponding fixture
// change is a regression signal.
type bifLevelExpect struct {
	W, H               int
	TileW, TileH       int
	GridW, GridH       int
	OverlapX, OverlapY int // image.Point components on this level
}

// bifFixture lists per-fixture expected values for the T22
// geometry gate.
type bifFixture struct {
	filename       string
	levels         []bifLevelExpect
	scanRes        float64 // µm/pixel; baseline mpp at level 0
	generation     string
	hasICC         bool
	encodeInfoVer  int
	overviewWxH    [2]int // expected associated[0] (overview) dimensions
	hasProbability bool
	hasThumbnail   bool
}

var bifFixtures = []bifFixture{
	{
		filename: "Ventana-1.bif",
		levels: []bifLevelExpect{
			{W: 24576, H: 21504, TileW: 1024, TileH: 1024, GridW: 24, GridH: 21, OverlapX: 2, OverlapY: 0},
			{W: 12288, H: 10752, TileW: 1024, TileH: 1024, GridW: 12, GridH: 11},
			{W: 6144, H: 5376, TileW: 1024, TileH: 1024, GridW: 6, GridH: 6},
			{W: 3072, H: 2688, TileW: 1024, TileH: 1024, GridW: 3, GridH: 3},
			{W: 1536, H: 1344, TileW: 1024, TileH: 1024, GridW: 2, GridH: 2},
			{W: 768, H: 672, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
			{W: 384, H: 336, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
			{W: 192, H: 168, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
		},
		scanRes:        0.25,
		generation:     "spec-compliant",
		hasICC:         true,
		encodeInfoVer:  2,
		overviewWxH:    [2]int{1251, 3685},
		hasProbability: true,
	},
	{
		filename: "OS-1.bif",
		levels: []bifLevelExpect{
			{W: 118784, H: 102000, TileW: 1024, TileH: 1360, GridW: 116, GridH: 75, OverlapX: 18, OverlapY: 26},
			{W: 59392, H: 51000, TileW: 1024, TileH: 1360, GridW: 58, GridH: 38},
			{W: 29696, H: 25504, TileW: 1024, TileH: 1360, GridW: 29, GridH: 19},
			{W: 14848, H: 12752, TileW: 1024, TileH: 1360, GridW: 15, GridH: 10},
			{W: 7424, H: 6376, TileW: 1024, TileH: 1360, GridW: 8, GridH: 5},
			{W: 3712, H: 3192, TileW: 1024, TileH: 1360, GridW: 4, GridH: 3},
			{W: 1856, H: 1600, TileW: 1024, TileH: 1360, GridW: 2, GridH: 2},
			{W: 928, H: 800, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 464, H: 400, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 232, H: 200, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
		},
		scanRes:       0.2325,
		generation:    "legacy-iscan",
		hasICC:        false,
		encodeInfoVer: 2,
		overviewWxH:   [2]int{1008, 3008},
		hasThumbnail:  true,
	},
}

func TestBIFGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range bifFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "bif", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			levels := tiler.Levels()
			if len(levels) != len(fx.levels) {
				t.Fatalf("level count: got %d, want %d", len(levels), len(fx.levels))
			}
			for i, want := range fx.levels {
				lvl := levels[i]
				if got := lvl.Size; got.W != want.W || got.H != want.H {
					t.Errorf("L%d Size: got %dx%d, want %dx%d", i, got.W, got.H, want.W, want.H)
				}
				if got := lvl.TileSize; got.W != want.TileW || got.H != want.TileH {
					t.Errorf("L%d TileSize: got %dx%d, want %dx%d", i, got.W, got.H, want.TileW, want.TileH)
				}
				if got := lvl.Grid; got.W != want.GridW || got.H != want.GridH {
					t.Errorf("L%d Grid: got %dx%d, want %dx%d", i, got.W, got.H, want.GridW, want.GridH)
				}
				if got := lvl.TileOverlap; got.X != want.OverlapX || got.Y != want.OverlapY {
					t.Errorf("L%d TileOverlap: got %v, want (%d,%d)", i, got, want.OverlapX, want.OverlapY)
				}
				// Per-level dimensions in the table above are the
				// strict pin. We don't re-check the multiplicative
				// downscale factor here: legacy iScan slides
				// exhibit ±4-pixel rounding wobbles between
				// pyramid steps (OS-1 L1H=51000 → L2H=25504 vs.
				// strict 25500), and the exact-dimension table
				// already catches any unexpected drift.
			}

			// JPEG marker validity on the level-0 (0,0) tile —
			// every BIF pyramid level is JPEG-compressed; output
			// after RawTile() should be a self-decodable JPEG.
			tile, err := tiler.RawTile(0, 0, 0)
			if err != nil {
				t.Fatalf("RawTile(0,0,0): %v", err)
			}
			if len(tile) < 4 || tile[0] != 0xFF || tile[1] != 0xD8 {
				t.Errorf("RawTile(0,0,0) missing SOI: %x", tile[:min(8, len(tile))])
			}
			if tile[len(tile)-2] != 0xFF || tile[len(tile)-1] != 0xD9 {
				t.Errorf("RawTile(0,0,0) missing EOI: %x", tile[len(tile)-min(8, len(tile)):])
			}
			// RangeTiles iterator yields >= one entry on a >=1×1 grid.
			seen := 0
			for range tiler.RangeTiles(context.Background(), 0) {
				seen++
				break
			}
			if seen == 0 {
				t.Error("RangeTiles iterator yielded zero entries")
			}

			// ICC presence per fixture.
			if fx.hasICC {
				icc := tiler.ICCProfile()
				if len(icc) < 40 || string(icc[36:40]) != "acsp" {
					t.Errorf("ICCProfile: got %d bytes / magic mismatch (want acsp)", len(icc))
				}
			} else {
				if got := tiler.ICCProfile(); got != nil {
					t.Errorf("ICCProfile: got %d bytes, want nil", len(got))
				}
			}

			// Generation + ScanRes via MetadataOf.
			bm, ok := bif.MetadataOf(tiler)
			if !ok {
				t.Fatal("bif.MetadataOf: ok=false on a BIF tiler")
			}
			if bm.Generation != fx.generation {
				t.Errorf("Generation: got %q, want %q", bm.Generation, fx.generation)
			}
			if bm.ScanRes != fx.scanRes {
				t.Errorf("ScanRes: got %v, want %v", bm.ScanRes, fx.scanRes)
			}
			if bm.EncodeInfoVer != fx.encodeInfoVer {
				t.Errorf("EncodeInfoVer: got %d, want %d", bm.EncodeInfoVer, fx.encodeInfoVer)
			}

			// AOI origins (when present) tile-aligned.
			tw := levels[0].TileSize.W
			th := levels[0].TileSize.H
			for i, ao := range bm.AOIOrigins {
				if ao.OriginX%tw != 0 {
					t.Errorf("AOIOrigin[%d].OriginX=%d not a multiple of TileW=%d", i, ao.OriginX, tw)
				}
				if ao.OriginY%th != 0 {
					t.Errorf("AOIOrigin[%d].OriginY=%d not a multiple of TileH=%d", i, ao.OriginY, th)
				}
			}

			// Associated images per fixture.
			ai := tiler.AssociatedImages()
			gotTypes := map[opentile.AssociatedType]opentile.Size{}
			for _, a := range ai {
				gotTypes[a.Type()] = a.Size()
			}
			ovS, ovOK := gotTypes[opentile.AssociatedOverview]
			if !ovOK {
				t.Error("missing associated type=overview")
			} else if ovS.W != fx.overviewWxH[0] || ovS.H != fx.overviewWxH[1] {
				t.Errorf("overview size: got %v, want %dx%d", ovS, fx.overviewWxH[0], fx.overviewWxH[1])
			}
			if fx.hasProbability {
				if _, ok := gotTypes[opentile.AssociatedProbability]; !ok {
					t.Error("missing associated type=probability (expected on spec-compliant fixture)")
				}
			}
			if fx.hasThumbnail {
				if _, ok := gotTypes[opentile.AssociatedThumbnail]; !ok {
					t.Error("missing associated type=thumbnail (expected on legacy fixture)")
				}
			}
			_ = bytes.Compare // keep the import
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
