package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestStitchedDisplayTileCustomSize verifies a consumer can render BIF display
// tiles at a caller-chosen size (e.g. square 512×512) independent of the level's
// stored TileSize — legacy BIF stores non-square 1024×1360 tiles, which choke
// viewers that assume square. StitchedGridFor(tile) gives the iteration grid and
// StitchedTileInto with a `tile`-sized dst composites the matching rectangle —
// pixel-identical to ReadRegion of the same rect.
func TestStitchedDisplayTileCustomSize(t *testing.T) {
	for _, fx := range []string{"Ventana-1.bif", "OS-1.bif"} {
		t.Run(fx, func(t *testing.T) {
			path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/" + fx
			if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
				path = filepath.Join(dir, "bif", fx)
			}
			if _, err := os.Stat(path); err != nil {
				t.Skip(fx + " absent")
			}
			s, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			l, err := s.Level(1) // an overlapping reduced level
			if err != nil {
				t.Fatal(err)
			}
			if !l.Overlapping {
				t.Fatal("L1 not overlapping")
			}

			disp := opentile.Size{W: 512, H: 512} // square, != stored TileSize
			grid := l.StitchedGridFor(disp)
			wantGrid := opentile.Size{
				W: (l.Size.W + disp.W - 1) / disp.W,
				H: (l.Size.H + disp.H - 1) / disp.H,
			}
			if grid != wantGrid {
				t.Errorf("StitchedGridFor(%v) = %v, want %v", disp, grid, wantGrid)
			}

			// A few interior display tiles must equal ReadRegion of the same rect.
			for _, c := range [][2]int{{grid.W / 3, grid.H / 3}, {grid.W / 2, grid.H / 2}} {
				tx, ty := c[0], c[1]
				dst := decoder.NewImageFormat(disp.W, disp.H, decoder.PixelFormatRGB)
				if err := l.StitchedTileInto(tx, ty, dst); err != nil {
					t.Fatalf("StitchedTileInto(%d,%d) 512×512: %v", tx, ty, err)
				}
				region, err := l.ReadRegion(opentile.Region{
					Origin: opentile.Point{X: tx * disp.W, Y: ty * disp.H},
					Size:   disp,
				}, opentile.WithFormat(decoder.PixelFormatRGB))
				if err != nil {
					t.Fatal(err)
				}
				if mad := exactMAD(dst, region); mad != 0 {
					t.Errorf("display tile (%d,%d): StitchedTileInto != ReadRegion (MAD %.2f)", tx, ty, mad)
				}
			}
		})
	}
}

func exactMAD(a, b *decoder.Image) float64 {
	if a.Width != b.Width || a.Height != b.Height {
		return 1e9
	}
	bpp := 3
	if a.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	var sum, n float64
	for y := 0; y < a.Height; y++ {
		for x := 0; x < a.Width; x++ {
			for c := 0; c < 3; c++ {
				d := int(a.Pix[y*a.Stride+x*bpp+c]) - int(b.Pix[y*b.Stride+x*bpp+c])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
	}
	return sum / n
}
