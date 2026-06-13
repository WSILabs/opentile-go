package opentile_test

import (
	"errors"
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

func assembleStrips(t *testing.T, s *opentile.Slide, l0 opentile.Region, out opentile.Size, scale int) *decoder.Image {
	t.Helper()
	it := s.ScaledStrips(l0, out, 64, opentile.WithStripIDCTScale(scale))
	defer it.Close()
	full := decoder.NewImage(out.W, out.H)
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
	l0 := opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: 4096, H: 4096}}
	out := opentile.Size{W: 512, H: 512}
	s1 := assembleStrips(t, s, l0, out, 1)
	s2 := assembleStrips(t, s, l0, out, 2)
	if m := meanAbsDiff(s1, s2); m > 2 {
		t.Errorf("strip scale-2 vs scale-1 mean abs diff = %.3f, want <= 2 (scaled blit geometry broken)", m)
	}
}

func jp2kSVS(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "svs", "JP2K-33003-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return p
}

// TestStripCodecScaleJP2K: a JP2K source at a between-level downsample must
// (a) actually engage codec-domain scale (>1) — not just JPEG — and
// (b) stay correct (match the spatial-only scale-1 output).
func TestStripCodecScaleJP2K(t *testing.T) {
	s, err := opentile.OpenFile(jp2kSVS(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// 2x downsample of a 2048-region: between L0 (down 1) and L1 (down 4),
	// so bestLevel is L0 and the residual is 2 -> codec scale 2.
	l0 := opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: 2048, H: 2048}}
	out := opentile.Size{W: 1024, H: 1024}

	// (a) auto codec scale engages.
	it := s.ScaledStrips(l0, out, 64)
	got := it.IDCTScaleForTest()
	it.Close()
	if got <= 1 {
		t.Fatalf("JP2K auto codec scale = %d, want > 1 (gate should not be JPEG-only)", got)
	}

	// (b) correctness vs the spatial-only path.
	auto := assembleStrips(t, s, l0, out, 0) // 0 = auto codec scale
	spatial := assembleStrips(t, s, l0, out, 1)
	if m := meanAbsDiff(auto, spatial); m > 3 {
		t.Errorf("JP2K codec-scale vs scale-1 mean abs diff = %.3f, want <= 3", m)
	}
}

// TestReadRegionScaledCodecScale: ReadRegionScaled on a JP2K between-level
// target must stay correct — equal to the verified spatial-only strip path.
// (It now routes through the codec-scaling strip machinery internally.)
func TestReadRegionScaledCodecScale(t *testing.T) {
	s, err := opentile.OpenFile(jp2kSVS(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.ReadRegionScaled(opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: 2048, H: 2048}}, opentile.Size{W: 1024, H: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ref := assembleStrips(t, s, opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: 2048, H: 2048}}, opentile.Size{W: 1024, H: 1024}, 1)
	if got.Width != ref.Width || got.Height != ref.Height {
		t.Fatalf("dims %dx%d vs %dx%d", got.Width, got.Height, ref.Width, ref.Height)
	}
	if m := meanAbsDiff(got, ref); m > 3 {
		t.Errorf("ReadRegionScaled vs strip-scale-1 reference mean abs diff = %.3f, want <= 3", m)
	}
}
