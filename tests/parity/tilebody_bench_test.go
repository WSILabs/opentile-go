//go:build benchgate

package parity

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// BenchmarkTileBandwidth_PatternsAB measures total bytes shipped
// when iterating every L0 tile under two patterns:
//
//	Pattern A (current): server splices and ships full JPEG per tile.
//	                     Bytes shipped = sum of len(Tile()).
//	Pattern B (v0.13):   server ships TilePrefix() once + per-tile
//	                     TileBodyInto() output; client reconstitutes.
//	                     Bytes shipped = len(prefix) + sum of body lengths.
//
// Reports per-fixture: Pattern A total, Pattern B total, savings %.
//
// Run via:
//
//	OPENTILE_TESTDIR=$PWD/sample_files \
//	  go test -tags benchgate -bench BenchmarkTileBandwidth \
//	          -benchtime 1x -run '^$' ./tests/parity/
//
// Build-tag-gated; skipped in default `go test ./...`.
func BenchmarkTileBandwidth_PatternsAB(b *testing.B) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		b.Skip("OPENTILE_TESTDIR unset")
	}
	for _, tc := range []struct {
		subdir, name string
	}{
		{"svs", "CMU-1.svs"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"scn", "Leica-1.scn"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				b.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				b.Fatal(err)
			}
			defer tiler.Close()

			lvl := tiler.Levels()[0]
			prefix := lvl.TilePrefix()
			bodyBuf := make([]byte, lvl.TileBodyMaxSize())

			var totalA, totalB int64
			tileCount := 0
			ctx := context.Background()
			for pos, res := range lvl.Tiles(ctx) {
				if res.Err != nil {
					continue
				}
				totalA += int64(len(res.Bytes))
				n, err := lvl.TileBodyInto(pos.X, pos.Y, bodyBuf)
				if err != nil {
					b.Fatal(err)
				}
				totalB += int64(n)
				tileCount++
			}
			totalB += int64(len(prefix)) // prefix shipped once

			savings := 100.0 * float64(totalA-totalB) / float64(totalA)
			b.Logf("%s L0: PatternA=%d bytes, PatternB=%d bytes, savings=%.1f%% (prefix=%d bytes, %d tiles)",
				tc.name, totalA, totalB, savings, len(prefix), tileCount)
		})
	}
}
