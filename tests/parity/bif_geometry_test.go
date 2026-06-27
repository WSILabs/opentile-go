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
			// L0 Size is the STITCHED content hull (23432 = 23 content cols × 1024
			// − 120 cumulative horizontal overlap), NOT the raw 24×21 frame grid
			// extent (24576) — the 24th column is phantom padding. Grid stays 24×21
			// (raw frame addressing unchanged). See GH #60 / stitch.go.
			{W: 23432, H: 21504, TileW: 1024, TileH: 1024, GridW: 24, GridH: 21, OverlapX: 2, OverlapY: 0},
			// L1-L7 derive from the L0 stitched hull via floor-halving (#78):
			// Size = floor(L0_size / 2^i), matching bio-formats behaviour.
			// Grid stays at the raw frame count (12, 6, 3, 2, 1, 1, 1).
			{W: 11716, H: 10752, TileW: 1024, TileH: 1024, GridW: 12, GridH: 11},
			{W: 5858, H: 5376, TileW: 1024, TileH: 1024, GridW: 6, GridH: 6},
			{W: 2929, H: 2688, TileW: 1024, TileH: 1024, GridW: 3, GridH: 3},
			{W: 1464, H: 1344, TileW: 1024, TileH: 1024, GridW: 2, GridH: 2},
			{W: 732, H: 672, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
			{W: 366, H: 336, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
			{W: 183, H: 168, TileW: 1024, TileH: 1024, GridW: 1, GridH: 1},
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
			// L0 Size is the STITCHED content hull (105936×94125) — per-gap-average
			// in-axis overlap PLUS the #68 cross-axis per-column/per-row drift
			// baselines, reconstructed from the TileJointInfo graph (#63 + #68). The
			// hull is slightly LARGER than openslide's nominal extent (105813×93951):
			// honoring the scanner-stage skew places tiles in a faint parallelogram,
			// so the bounding box grows by the integrated drift span (~120 px wide,
			// ~190 px tall). Grid stays 116×75 (raw frame addressing unchanged).
			// Reduced levels (#80 subtile model, v0.56.0): Size = L0 hull
			// floor-halved; each L0 frame composited at its compacted position via
			// the subtile path. Grid stays the raw frame grid; tile bytes unchanged.
			{W: 105936, H: 94125, TileW: 1024, TileH: 1360, GridW: 116, GridH: 75, OverlapX: 18, OverlapY: 26},
			{W: 52968, H: 47062, TileW: 1024, TileH: 1360, GridW: 58, GridH: 38},
			{W: 26484, H: 23531, TileW: 1024, TileH: 1360, GridW: 29, GridH: 19},
			{W: 13242, H: 11765, TileW: 1024, TileH: 1360, GridW: 15, GridH: 10},
			{W: 6621, H: 5882, TileW: 1024, TileH: 1360, GridW: 8, GridH: 5},
			{W: 3310, H: 2941, TileW: 1024, TileH: 1360, GridW: 4, GridH: 3},
			{W: 1655, H: 1470, TileW: 1024, TileH: 1360, GridW: 2, GridH: 2},
			{W: 827, H: 735, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 413, H: 367, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 206, H: 183, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
		},
		scanRes:       0.2325,
		generation:    "legacy-iscan",
		hasICC:        false,
		encodeInfoVer: 2,
		overviewWxH:   [2]int{1008, 3008},
		hasThumbnail:  true,
	},
	{
		// OS-2 is a MULTI-AOI legacy iScan slide (#67): three AoiOrigins, two
		// scanned tissue areas placed at their own Pos-X/Pos-Y anchors (Y measured
		// from each AOI's bottom → Y-flipped during layout). L0 Size is the union
		// hull across all scanned AOIs (114951×76389); reduced levels floor-halve.
		// Grid stays the raw frame grid (125×61). PHI/local-only fixture — skips in
		// CI. See buildLegacyLayout multi-AOI path + docs/formats/bif.md.
		filename: "OS-2.bif",
		levels: []bifLevelExpect{
			{W: 115060, H: 76560, TileW: 1024, TileH: 1360, GridW: 125, GridH: 61, OverlapX: 31, OverlapY: 31},
			{W: 57530, H: 38280, TileW: 1024, TileH: 1360, GridW: 63, GridH: 31},
			{W: 28765, H: 19140, TileW: 1024, TileH: 1360, GridW: 32, GridH: 16},
			{W: 14382, H: 9570, TileW: 1024, TileH: 1360, GridW: 16, GridH: 8},
			{W: 7191, H: 4785, TileW: 1024, TileH: 1360, GridW: 8, GridH: 4},
			{W: 3595, H: 2392, TileW: 1024, TileH: 1360, GridW: 4, GridH: 2},
			{W: 1797, H: 1196, TileW: 1024, TileH: 1360, GridW: 2, GridH: 1},
			{W: 898, H: 598, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 449, H: 299, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
			{W: 224, H: 149, TileW: 1024, TileH: 1360, GridW: 1, GridH: 1},
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
				// OverlapMode must agree with Overlapping (the v0.60.1 fix): a
				// stitched/overlapping BIF level reports OverlapStitched, a
				// non-overlapping one reports OverlapNone.
				wantMode := opentile.OverlapNone
				if lvl.Overlapping {
					wantMode = opentile.OverlapStitched
				}
				if lvl.OverlapMode != wantMode {
					t.Errorf("L%d OverlapMode: got %v, want %v (Overlapping=%v)", i, lvl.OverlapMode, wantMode, lvl.Overlapping)
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
			tile, err := mustLevel(t, tiler, 0).Tile(0, 0)
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
			for range mustLevel(t, tiler, 0).Tiles(context.Background()) {
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
