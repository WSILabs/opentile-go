package dzi_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestDZIOverlapParityVsZero asserts reading the overlap=1 libvips DZI of CMU-1
// produces the same pixels as the overlap=0 DZI of the SAME slide. Tiles are
// independent JPEGs → bar is low MAD, not bit-exact. Local-only: skips when the
// fixtures are absent. A wrong crop/placement shifts content and spikes MAD.
func TestDZIOverlapParityVsZero(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "/Volumes/Ext/GitHub/opentile-go/sample_files"
	}
	base := filepath.Join(dir, "dzi")
	p0 := filepath.Join(base, "CMU-1_dzi_libvips_overlap_0.dzi")
	p1 := filepath.Join(base, "CMU-1_dzi_libvips_overlap_1.dzi")
	for _, p := range []string{p0, p1} {
		if _, err := os.Stat(p); err != nil {
			t.Skip("CMU-1 dzi fixtures absent")
		}
	}
	s0, err := opentile.OpenFile(p0)
	if err != nil {
		t.Fatal(err)
	}
	defer s0.Close()
	s1, err := opentile.OpenFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	regions := []opentile.Region{
		{Origin: opentile.Point{X: 5120, Y: 5120}, Size: opentile.Size{W: 600, H: 600}},
		{Origin: opentile.Point{X: 5133, Y: 5251}, Size: opentile.Size{W: 517, H: 503}},
		{Origin: opentile.Point{X: 44000, Y: 28000}, Size: opentile.Size{W: 600, H: 500}},
		// Bottom-right edge: exercises unpadded right/bottom edge tiles on the
		// overlap=0 fixture (slide 46000×32914, TileSize=256 — last col width
		// 176, last row height 146). Pre-fix this region errored with
		// "dst 256x256 != decoded 176x146"; now it must also match overlap=1.
		{Origin: opentile.Point{X: 45600, Y: 32500}, Size: opentile.Size{W: 400, H: 414}},
	}
	for li := 0; li < 3; li++ {
		l0, e0 := s0.Level(li)
		l1, e1 := s1.Level(li)
		if e0 != nil || e1 != nil {
			continue
		}
		if l1.OverlapMode != opentile.OverlapBordered {
			t.Fatalf("L%d overlap=1 fixture OverlapMode=%v, want bordered", li, l1.OverlapMode)
		}
		for _, r := range regions {
			rr := scaleRegion(r, li)
			if rr.Origin.X+rr.Size.W > l0.Size.W || rr.Origin.Y+rr.Size.H > l0.Size.H {
				continue
			}
			a, err := l0.ReadRegion(rr)
			if err != nil {
				t.Fatal(err)
			}
			b, err := l1.ReadRegion(rr)
			if err != nil {
				t.Fatal(err)
			}
			if mad := regionMAD(a, b); mad > 4.0 {
				t.Errorf("L%d region %+v: overlap0-vs-overlap1 MAD=%.2f, want <=4 (crop/placement wrong)", li, rr, mad)
			} else {
				t.Logf("L%d region %+v: MAD=%.2f", li, rr, mad)
			}
		}
	}

	// Smoke: the scaled-strip path engages the subtile layout without error on
	// the overlap=1 fixture. (Pixel correctness comes from the ReadRegion parity
	// above + the shared subtileLayout compositor path.)
	scaledStripsSmoke(t, s1)
}

func scaleRegion(r opentile.Region, level int) opentile.Region {
	s := 1 << level
	return opentile.Region{
		Origin: opentile.Point{X: r.Origin.X / s, Y: r.Origin.Y / s},
		Size:   r.Size,
	}
}

func regionMAD(a, b *decoder.Image) float64 {
	bppA, bppB := 3, 3
	if a.Format == decoder.PixelFormatRGBA {
		bppA = 4
	}
	if b.Format == decoder.PixelFormatRGBA {
		bppB = 4
	}
	w, h := a.Width, a.Height
	if b.Width < w {
		w = b.Width
	}
	if b.Height < h {
		h = b.Height
	}
	var sum, n float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				d := int(a.Pix[y*a.Stride+x*bppA+c]) - int(b.Pix[y*b.Stride+x*bppB+c])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// scaledStripsSmoke exercises the scaled-strip iterator on the overlap=1 fixture.
// It asserts the iterator yields at least one strip without error, engaging the
// subtile layout compositor path. This is a no-error smoke, not a pixel gate.
//
// ScaledStrips signature: (p *Pyramid) ScaledStrips(src Region, out Size, stripHeight int, opts ...StripOption) *StripIterator
// StripIterator: Next() (*decoder.Image, error) returns io.EOF when exhausted; Close() error.
func scaledStripsSmoke(t *testing.T, s *opentile.Slide) {
	t.Helper()
	pyr := s.Pyramid(0)
	l0, err := pyr.Level(0)
	if err != nil {
		t.Fatalf("scaledStripsSmoke: Level(0): %v", err)
	}
	// Use a modest output size so the test finishes quickly.
	out := opentile.Size{W: 256, H: 256}
	src := opentile.Region{Origin: opentile.Point{}, Size: l0.Size}
	it := pyr.ScaledStrips(src, out, 64)
	defer it.Close()

	stripCount := 0
	for {
		_, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("scaledStripsSmoke: Next() strip %d: %v", stripCount, err)
		}
		stripCount++
	}
	if stripCount == 0 {
		t.Fatal("scaledStripsSmoke: ScaledStrips yielded 0 strips")
	}
	t.Logf("scaledStripsSmoke: %d strips at 256x(up to 64)px — ScaledStrips smoke passed", stripCount)
}
