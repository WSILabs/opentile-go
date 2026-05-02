//go:build benchgate

// Package parity's benchgate-tagged file is the v0.9 perf baseline
// gate. It captures per-format Tile() throughput + allocation rate
// across the parity slate so each subsequent v0.9 task can show a
// before/after delta in its commit message.
//
// Run with:
//
//	OPENTILE_TESTDIR=$PWD/sample_files \
//	  go test -tags benchgate -bench=BenchmarkTile -benchmem -count=1 \
//	    ./tests/parity/
//
// For deeper pprof investigation (the reference doc's recommended
// 1-minute run):
//
//	OPENTILE_TESTDIR=$PWD/sample_files \
//	  go test -tags benchgate -bench=BenchmarkTile -benchmem -count=1 \
//	    -benchtime=60s -cpuprofile=/tmp/v0.9.cpu.prof \
//	    -memprofile=/tmp/v0.9.alloc.prof \
//	    ./tests/parity/
//	go tool pprof -top -cum /tmp/v0.9.cpu.prof | head -20
//
// Build tag `benchgate` keeps this out of the default `make test` /
// `make cover` runs — it depends on real fixtures + nontrivial
// runtime + meaningful disk warm-up.
package parity

import (
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// benchFixtures lists one representative slide per format. The
// chosen fixtures are the smallest-but-meaningful sample for each:
// CMU-1.svs (177 MB tiled JPEG SVS), CMU-1.ndpi (188 MB striped),
// Philips-1.tiff (311 MB), Leica-1.ome.tiff (689 MB single-pyramid
// OME), Ventana-1.bif (227 MB DP 200), cervix_2x_jpeg.iris (2.16 GB).
// Bench focuses on level 0 — the hot path consumers care about.
var benchFixtures = []struct {
	subdir string
	name   string
}{
	{"svs", "CMU-1.svs"},
	{"ndpi", "CMU-1.ndpi"},
	{"philips-tiff", "Philips-1.tiff"},
	{"ome-tiff", "Leica-1.ome.tiff"},
	{"bif", "Ventana-1.bif"},
	{"ife", "cervix_2x_jpeg.iris"},
}

// BenchmarkTile measures warm-cache Tile() throughput on level 0
// of each fixture in two modes:
//
//   - serial: single-threaded, sequential (col, row) walk.
//     Reflects the baseline cost-per-tile for one consumer.
//   - parallel: testing.B.RunParallel with default GOMAXPROCS,
//     randomized coords. Reflects high-RPS server behavior.
//
// Both modes report ns/op + B/op + allocs/op via -benchmem.
func BenchmarkTile(b *testing.B) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		b.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range benchFixtures {
		b.Run(fx.name, func(b *testing.B) {
			path := filepath.Join(dir, fx.subdir, fx.name)
			if _, err := os.Stat(path); err != nil {
				b.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				b.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			lvl, err := tiler.Level(0)
			if err != nil {
				b.Fatalf("Level(0): %v", err)
			}
			grid := lvl.Grid()
			if grid.W == 0 || grid.H == 0 {
				b.Skipf("%s level 0 has empty grid", fx.name)
			}

			// Warm the page cache by reading a deterministic sample
			// of tiles. For small grids walk everything; for big ones
			// (cervix's 496×346) sample 1024 randomly so the first
			// real bench iteration isn't dominated by cold-cache I/O.
			warmTiles := grid.W * grid.H
			if warmTiles > 1024 {
				warmTiles = 1024
			}
			rng := rand.New(rand.NewSource(0))
			for i := 0; i < warmTiles; i++ {
				x := rng.Intn(grid.W)
				y := rng.Intn(grid.H)
				if _, err := lvl.Tile(x, y); err != nil {
					b.Fatalf("warm Tile(%d,%d): %v", x, y, err)
				}
			}

			// Sample one tile to derive a representative byte count
			// for the throughput metric (b.SetBytes makes the bench
			// framework report MB/s natively). Tile sizes are roughly
			// uniform within a level, so the mid-grid sample is
			// representative within ~10%.
			sample, err := lvl.Tile(grid.W/2, grid.H/2)
			if err != nil {
				b.Fatalf("sample Tile: %v", err)
			}
			tileBytes := int64(len(sample))

			b.Run("serial", func(b *testing.B) {
				b.SetBytes(tileBytes)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					x := i % grid.W
					y := (i / grid.W) % grid.H
					if _, err := lvl.Tile(x, y); err != nil {
						b.Fatalf("Tile(%d,%d): %v", x, y, err)
					}
				}
			})

			b.Run("parallel", func(b *testing.B) {
				b.SetBytes(tileBytes)
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					rng := rand.New(rand.NewSource(rand.Int63()))
					for pb.Next() {
						x := rng.Intn(grid.W)
						y := rng.Intn(grid.H)
						if _, err := lvl.Tile(x, y); err != nil {
							b.Fatalf("parallel Tile: %v", err)
						}
					}
				})
			})

			// Pool-aware TileInto variant: the v0.9 hot-path API.
			// Same access pattern as the parallel Tile() bench above
			// but draws buffers from a sync.Pool sized to TileMaxSize.
			// On formats where Tile() does not internally allocate
			// (SVS / Philips / OME tiled / BIF / IFE), this should
			// drop allocs/op to ~0.
			b.Run("parallel_pool_tileinto", func(b *testing.B) {
				maxSize := lvl.TileMaxSize()
				pool := &sync.Pool{
					New: func() any {
						buf := make([]byte, maxSize)
						return &buf
					},
				}
				b.SetBytes(tileBytes)
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					rng := rand.New(rand.NewSource(rand.Int63()))
					for pb.Next() {
						x := rng.Intn(grid.W)
						y := rng.Intn(grid.H)
						bufPtr := pool.Get().(*[]byte)
						_, err := lvl.TileInto(x, y, *bufPtr)
						pool.Put(bufPtr)
						if err != nil {
							b.Fatalf("parallel_pool_tileinto: %v", err)
						}
					}
				})
			})
		})
	}
}
