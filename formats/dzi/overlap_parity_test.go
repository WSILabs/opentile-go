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

// TestDZIOverlapScaledStripsParityVsZero proves the ScaledStrips path crops
// overlap pixels correctly: the overlap=1 fixture must produce the same pixels
// as the overlap=0 fixture at the same target scale. A wrong crop/placement in
// the strip compositor would shift content cumulatively and spike MAD.
// Local-only: skips when the fixtures are absent.
func TestDZIOverlapScaledStripsParityVsZero(t *testing.T) {
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

	pyr0 := s0.Pyramid(0)
	pyr1 := s1.Pyramid(0)

	l0_0, err := pyr0.Level(0)
	if err != nil {
		t.Fatalf("Level(0) on overlap=0: %v", err)
	}
	l0_1, err := pyr1.Level(0)
	if err != nil {
		t.Fatalf("Level(0) on overlap=1: %v", err)
	}

	// Use the overlap=0 L0 size as the canonical source extent; both fixtures
	// cover the same slide so their L0 sizes are equal. Scale to ~1024 wide to
	// keep the test fast while exercising multiple strips.
	src0 := opentile.Region{Origin: opentile.Point{}, Size: l0_0.Size}
	src1 := opentile.Region{Origin: opentile.Point{}, Size: l0_1.Size}

	// Compute a shared output size (~1024 wide, proportional height) based on
	// the overlap=0 L0 extent.
	const targetW = 1024
	outW := targetW
	outH := int(int64(l0_0.Size.H) * targetW / int64(l0_0.Size.W))
	if outH < 1 {
		outH = 1
	}
	out := opentile.Size{W: outW, H: outH}
	stripH := 64

	img0, err := assembleScaledStrips(t, pyr0, src0, out, stripH)
	if err != nil {
		t.Fatalf("assembleScaledStrips overlap=0: %v", err)
	}
	img1, err := assembleScaledStrips(t, pyr1, src1, out, stripH)
	if err != nil {
		t.Fatalf("assembleScaledStrips overlap=1: %v", err)
	}

	mad := regionMAD(img0, img1)
	t.Logf("ScaledStrips overlap0-vs-overlap1: assembled %dx%d vs %dx%d, MAD=%.2f",
		img0.Width, img0.Height, img1.Width, img1.Height, mad)
	if mad > 4.0 {
		t.Errorf("ScaledStrips overlap0-vs-overlap1 MAD=%.2f, want <=4 (overlap crop wrong)", mad)
	}
}

// assembleScaledStrips runs ScaledStrips and concatenates all strips top-to-bottom
// into a single *decoder.Image. It returns an error if the iterator fails.
func assembleScaledStrips(t *testing.T, pyr *opentile.Pyramid, src opentile.Region, out opentile.Size, stripHeight int) (*decoder.Image, error) {
	t.Helper()
	it := pyr.ScaledStrips(src, out, stripHeight)
	defer it.Close()

	var rows []*decoder.Image
	totalH := 0
	for {
		strip, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, strip)
		totalH += strip.Height
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Allocate output image and blit strips top-to-bottom.
	w := rows[0].Width
	bpp := 3
	if rows[0].Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	stride := w * bpp
	pix := make([]byte, totalH*stride)
	yOff := 0
	for _, s := range rows {
		for row := 0; row < s.Height; row++ {
			copy(pix[(yOff+row)*stride:], s.Pix[row*s.Stride:row*s.Stride+w*bpp])
		}
		yOff += s.Height
	}
	return &decoder.Image{
		Pix:    pix,
		Stride: stride,
		Width:  w,
		Height: totalH,
		Format: rows[0].Format,
	}, nil
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
