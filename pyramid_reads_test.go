package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestPyramidReadMethods verifies the v1.0 *Pyramid cross-level
// receiver-method API: navigation pointer identity, scaled-region read,
// and best-level selection.
func TestPyramidReadMethods(t *testing.T) {
	s := openSampleSlide(t)

	p := s.Pyramid(0)
	if p == nil {
		t.Fatal("Pyramid(0) returned nil")
	}

	// Pyramid(0).Level(0) is the same *Level as Slide.Level(0).
	pl, err := p.Level(0)
	if err != nil {
		t.Fatalf("Pyramid(0).Level(0): %v", err)
	}
	sl, err := s.Level(0)
	if err != nil {
		t.Fatalf("Slide.Level(0): %v", err)
	}
	if pl != sl {
		t.Errorf("Pyramid(0).Level(0) (%p) != Slide.Level(0) (%p)", pl, sl)
	}

	// ReadRegionScaled produces an image of the requested output size.
	out, err := p.ReadRegionScaled(
		opentile.Region{Size: opentile.Size{W: 64, H: 64}},
		opentile.Size{W: 32, H: 32},
	)
	if err != nil {
		t.Fatalf("Pyramid(0).ReadRegionScaled: %v", err)
	}
	if out.Width != 32 || out.Height != 32 {
		t.Errorf("ReadRegionScaled dims = %dx%d, want 32x32", out.Width, out.Height)
	}

	// BestLevelForDownsample returns a non-nil *Level from this pyramid.
	best := p.BestLevelForDownsample(1.0)
	if best == nil {
		t.Fatal("BestLevelForDownsample(1.0) returned nil")
	}
	if best.PyramidIndex != p.Index {
		t.Errorf("BestLevelForDownsample level PyramidIndex = %d, want %d",
			best.PyramidIndex, p.Index)
	}
}
