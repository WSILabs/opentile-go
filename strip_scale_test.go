package opentile_test

import (
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func cmu1(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "svs", "CMU-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return p
}

func assembleStrips(t *testing.T, s *opentile.Slide, l0 image.Rectangle, out image.Point, scale int) *decoder.Image {
	t.Helper()
	it := s.ScaledStrips(l0, out, 64, opentile.WithStripIDCTScale(scale))
	defer it.Close()
	full := decoder.NewImage(out.X, out.Y)
	y := 0
	for {
		strip, err := it.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		for r := 0; r < strip.Height; r++ {
			copy(full.Pix[(y+r)*full.Stride:(y+r)*full.Stride+strip.Width*3],
				strip.Pix[r*strip.Stride:r*strip.Stride+strip.Width*3])
		}
		y += strip.Height
	}
	return full
}

func meanAbsDiff(a, b *decoder.Image) float64 {
	var sum, n int
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
		n++
	}
	return float64(sum) / float64(n)
}

// TestStripIDCTScaleCorrect: codec-domain strip scale (idctScale>1) must
// produce the same output as the spatial-only path (scale 1) — it is an
// optimization, not a different result. The bug: scaled tiles were blitted at
// full-level coordinates, squishing them. 8x target on CMU-1's 4x level
// auto-selects idctScale=2.
func TestStripIDCTScaleCorrect(t *testing.T) {
	s, err := opentile.OpenFile(cmu1(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l0 := image.Rect(0, 0, 4096, 4096)
	out := image.Pt(512, 512)
	s1 := assembleStrips(t, s, l0, out, 1)
	s2 := assembleStrips(t, s, l0, out, 2)
	if m := meanAbsDiff(s1, s2); m > 2 {
		t.Errorf("strip scale-2 vs scale-1 mean abs diff = %.3f, want <= 2 (scaled blit geometry broken)", m)
	}
}
