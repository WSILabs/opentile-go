//go:build openslidebench

package bench_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/bench"
	"github.com/wsilabs/opentile-go/internal/openslideshim"
)

// BenchmarkOpenslide mirrors BenchmarkRead/<format>/readregion using
// openslide, for the formats openslide can read. 256×256 regions on a
// bounded interior grid, single + parallel.
func BenchmarkOpenslide(b *testing.B) {
	const ts = 256
	for _, e := range bench.Matrix {
		if !e.Openslide {
			continue
		}
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("fixture missing: %s", path) })
			continue
		}
		s, err := openslideshim.Open(path)
		if err != nil {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("openslide cannot open %s: %v", path, err) })
			continue
		}
		w, h := s.LevelDimensions(0)
		// bounded interior grid of 256-tiles, offset 1 in.
		var coords [][2]int64
		for ty := int64(1); ty < 17 && (ty+1)*ts <= h; ty++ {
			for tx := int64(1); tx < 17 && (tx+1)*ts <= w; tx++ {
				coords = append(coords, [2]int64{tx * ts, ty * ts})
			}
		}
		if len(coords) == 0 {
			coords = [][2]int64{{0, 0}}
		}
		const tilePix = int64(ts) * int64(ts)

		b.Run(e.Format+"/readregion/single", func(b *testing.B) {
			buf := make([]uint32, ts*ts)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := coords[i%len(coords)]
				if err := s.ReadRegion(buf, 0, c[0], c[1], ts, ts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
		})
		b.Run(e.Format+"/readregion/parallel", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				buf := make([]uint32, ts*ts) // openslide is thread-safe; per-goroutine buffer
				n := 0
				for pb.Next() {
					c := coords[n%len(coords)]
					n++
					if err := s.ReadRegion(buf, 0, c[0], c[1], ts, ts); err != nil {
						b.Error(err)
						return
					}
				}
			})
			b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
		})
		s.Close()
	}
}
