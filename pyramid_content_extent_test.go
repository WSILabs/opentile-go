package opentile_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// openFixture opens dir/<sub>/<name> via the public API, skipping when the
// fixture corpus or the file is absent.
func openFixture(t *testing.T, sub, name string) *opentile.Slide {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(dir, sub, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return s
}

func ceilDivT(a, b int) int { return (a + b - 1) / b }

func TestBIFVentanaContentExtent(t *testing.T) {
	s := openFixture(t, "bif", "Ventana-1.bif")
	defer s.Close()
	wantW := []int{23432, 11716, 5858, 2929, 1464, 732, 366, 183}
	wantH := []int{21504, 10752, 5376, 2688, 1344, 672, 336, 168}
	levels := s.Levels()
	if len(levels) != len(wantW) {
		t.Fatalf("level count %d, want %d", len(levels), len(wantW))
	}
	for i, l := range levels {
		if l.Size.W != wantW[i] || l.Size.H != wantH[i] {
			t.Errorf("L%d Size = %dx%d, want %dx%d", i, l.Size.W, l.Size.H, wantW[i], wantH[i])
		}
		// Downsample is the literal L0/level width ratio (self-consistent with
		// Size, the openslide convention) AND within rounding of 2^i — the #78
		// goal. The bug had L1 at 1.907; now L1–L3 are exactly 2/4/8 and floor-
		// halved L4+ drift <0.05% (e.g. 23432/1464 = 16.005), not a forced 2^i.
		wantSelf := float64(levels[0].Size.W) / float64(l.Size.W)
		if l.Downsample != wantSelf {
			t.Errorf("L%d Downsample = %v, want self-consistent %v (== Size0/Size_i)", i, l.Downsample, wantSelf)
		}
		pow := math.Pow(2, float64(i))
		if math.Abs(l.Downsample/pow-1) > 0.001 {
			t.Errorf("L%d Downsample = %v drifts >0.1%% from 2^%d=%v (#78: pyramid must be ~2x)", i, l.Downsample, i, pow)
		}
		g := l.StitchedGrid()
		if g.W != ceilDivT(l.Size.W, l.TileSize.W) || g.H != ceilDivT(l.Size.H, l.TileSize.H) {
			t.Errorf("L%d StitchedGrid %v != ceil(Size %v / Tile %v)", i, g, l.Size, l.TileSize)
		}
	}
}

func TestBIFVentanaStitchedTileClipsOverscan(t *testing.T) {
	s := openFixture(t, "bif", "Ventana-1.bif")
	defer s.Close()
	l1, err := s.Level(1)
	if err != nil {
		t.Fatal(err)
	}
	g := l1.StitchedGrid() // W = ceil(11716/1024) = 12
	img, err := l1.StitchedTile(g.W-1, 0)
	if errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Skip("decode unavailable (nocgo)")
	}
	if err != nil {
		t.Fatal(err)
	}
	// Last column origin = (g.W-1)*TileW = 11*1024 = 11264; content ends at
	// Size.W = 11716, so local columns >= 11716-11264 = 452 are overscan → white.
	contentCols := l1.Size.W - (g.W-1)*l1.TileSize.W
	bpp := 3
	for _, y := range []int{0, img.Height / 2, img.Height - 1} {
		for _, x := range []int{contentCols + 8, img.Width - 1} {
			o := y*img.Stride + x*bpp
			if img.Pix[o] != 0xFF || img.Pix[o+1] != 0xFF || img.Pix[o+2] != 0xFF {
				t.Fatalf("overscan pixel (%d,%d) = %d,%d,%d, want white 255 (StitchedSize must == Size)",
					x, y, img.Pix[o], img.Pix[o+1], img.Pix[o+2])
			}
		}
	}
}

func TestBIFLegacySizeUnchanged(t *testing.T) {
	s := openFixture(t, "bif", "OS-1.bif")
	defer s.Close()
	// Legacy iScan is deferred to #80 — its reduced-level Size must stay the raw
	// (un-compacted) grid extent. Pin L0/L1 so this change can't silently alter
	// legacy. (105818 hull at L0; 59392 = 58*1024 raw grid at L1.)
	levels := s.Levels()
	if levels[0].Size.W != 105818 {
		t.Errorf("OS-1 L0 Size.W = %d, want 105818 (unchanged hull)", levels[0].Size.W)
	}
	if levels[1].Size.W != 59392 {
		t.Errorf("OS-1 L1 Size.W = %d, want 59392 (raw grid, legacy untouched — see #80)", levels[1].Size.W)
	}
}
