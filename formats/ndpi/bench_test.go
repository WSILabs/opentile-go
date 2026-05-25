package ndpi_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPISmokeAllLevels opens the NDPI slide named by env NDPI_BENCH_SLIDE,
// hashes four sampled tiles per level (including edge tiles), and fails if
// it doesn't finish in 30 seconds. The intent is to catch architectural
// regressions (e.g., a >3000x slowdown from missing McuStarts rewrite)
// before they reach the fixture generator.
func TestNDPISmokeAllLevels(t *testing.T) {
	slide := os.Getenv("NDPI_BENCH_SLIDE")
	if slide == "" {
		t.Skip("NDPI_BENCH_SLIDE not set")
	}
	deadline := time.Now().Add(30 * time.Second)

	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatalf("OpenFile(%s): %v", slide, err)
	}
	defer tiler.Close()

	for i, lvl := range tiler.Levels() {
		grid := lvl.Grid
		// Sample four positions: (0,0), interior, right edge row, bottom edge col.
		positions := [][2]int{
			{0, 0},
			{grid.W / 2, grid.H / 2},
			{grid.W - 1, grid.H / 2},
			{grid.W / 2, grid.H - 1},
		}
		for _, p := range positions {
			if time.Now().After(deadline) {
				t.Fatalf("smoke test exceeded 30s deadline at level %d tile %v", i, p)
			}
			b, err := tiler.RawTile(i, p[0], p[1])
			if err != nil {
				t.Fatalf("level %d tile %v: %v", i, p, err)
			}
			if len(b) == 0 {
				t.Fatalf("level %d tile %v: empty bytes", i, p)
			}
		}
	}
}

// BenchmarkNDPITileAllLevels reports per-tile time for all pyramid levels of
// the NDPI slide named by env NDPI_BENCH_SLIDE. Used to gate the v0.2
// architectural-regression fix: L0 of CMU-1.ndpi must be ≤ 5ms/tile on M4
// (Python opentile baseline is ~1ms).
//
// Invoke with:
//
//	NDPI_BENCH_SLIDE=$PWD/sample_files/ndpi/CMU-1.ndpi \
//	  go test ./formats/ndpi -bench=Tile -benchtime=1x -run=^$ -v
func BenchmarkNDPITileAllLevels(b *testing.B) {
	slide := os.Getenv("NDPI_BENCH_SLIDE")
	if slide == "" {
		b.Skip("NDPI_BENCH_SLIDE not set")
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		b.Fatalf("OpenFile(%s): %v", slide, err)
	}
	defer tiler.Close()

	for i, lvl := range tiler.Levels() {
		grid := lvl.Grid
		// Deterministic interior tile (avoid edge-case overhead in the headline
		// number; edge tiles are exercised by the smoke test).
		x := grid.W / 2
		y := grid.H / 2
		name := fmt.Sprintf("L%d_%dx%d_tile_%dx%d", i, grid.W, grid.H, lvl.TileSize.W, lvl.TileSize.H)
		b.Run(name, func(b *testing.B) {
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				bytes, err := tiler.RawTile(i, x, y)
				if err != nil {
					b.Fatalf("RawTile(%d,%d): %v", x, y, err)
				}
				if len(bytes) == 0 {
					b.Fatalf("empty tile bytes")
				}
			}
		})
	}
}
