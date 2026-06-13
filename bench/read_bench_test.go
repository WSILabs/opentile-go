package bench_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/bench"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// coordGrid returns up to a 16x16 block of interior tile coordinates,
// clipped to [1, Grid-2] on each axis to avoid both the leading edge
// (x=0, y=0) and the trailing edge (x=Grid.W-1, y=Grid.H-1).
// Trailing-edge tiles can have smaller-than-nominal dimensions in
// formats that store edge tiles at their natural size (e.g. SZI/DZI);
// excluding them keeps ReadRegion — which passes a nominal-size scratch
// buffer — from hitting a decoder size-mismatch on the far edge.
// Falls back to (0,0) only when the grid is too small for any interior.
func coordGrid(base opentile.Level) [][2]int {
	// Interior range: [1, Grid-2] (exclusive upper bound at Grid-1).
	xLo, xHi := 1, base.Grid.W-1
	yLo, yHi := 1, base.Grid.H-1
	// Clamp upper bound to keep the block at most 16 wide/tall.
	if xHi > xLo+16 {
		xHi = xLo + 16
	}
	if yHi > yLo+16 {
		yHi = yLo + 16
	}
	var out [][2]int
	for y := yLo; y < yHi; y++ {
		for x := xLo; x < xHi; x++ {
			out = append(out, [2]int{x, y})
		}
	}
	if len(out) == 0 {
		out = [][2]int{{0, 0}}
	}
	return out
}

type pattern struct {
	name string
	read func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error)
}

var patterns = []pattern{
	{"tile", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		b, err := s.RawTile(base.Index, tx, ty)
		_ = b
		return int64(base.TileSize.W) * int64(base.TileSize.H), err
	}},
	{"decodedtile", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		img, err := s.ImageDecodedTile(0, base.Index, tx, ty)
		_ = img
		return int64(base.TileSize.W) * int64(base.TileSize.H), err
	}},
	{"readregion", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		img, err := s.ReadRegion(base.Index, opentile.Region{Origin: opentile.Point{X: tx * base.TileSize.W, Y: ty * base.TileSize.H}, Size: base.TileSize})
		_ = img
		return int64(base.TileSize.W) * int64(base.TileSize.H), err
	}},
}

func BenchmarkRead(b *testing.B) {
	for _, e := range bench.Matrix {
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("fixture missing: %s", path) })
			continue
		}
		s, err := opentile.OpenFile(path)
		if err != nil {
			b.Run(e.Format, func(b *testing.B) { b.Fatalf("open %s: %v", path, err) })
			continue
		}
		base := s.Levels()[0]
		coords := coordGrid(base)
		tilePix := int64(base.TileSize.W) * int64(base.TileSize.H)

		for _, p := range patterns {
			p := p
			b.Run(e.Format+"/"+p.name+"/single", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					c := coords[i%len(coords)]
					if _, err := p.read(s, base, c[0], c[1]); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
			})
			b.Run(e.Format+"/"+p.name+"/parallel", func(b *testing.B) {
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					n := 0
					for pb.Next() {
						c := coords[n%len(coords)]
						n++
						if _, err := p.read(s, base, c[0], c[1]); err != nil {
							b.Error(err)
							return
						}
					}
				})
				b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
			})
		}
		s.Close()
	}
}
