package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestAssociatedImageHasAccessors is a compile-time conformance check that
// the AssociatedImage interface includes the three v1.0 accessors added in
// the API breaking pass (Task 7).
func TestAssociatedImageHasAccessors(t *testing.T) {
	var ai opentile.AssociatedImage
	var _ interface {
		Encoding() (opentile.AssociatedEncoding, bool)
		TIFFTags() (opentile.TIFFTags, bool)
		IFDOffset() (int64, bool)
	} = ai
	t.Log("AssociatedImage carries Encoding/TIFFTags/IFDOffset — compile-time check passed")
}

func TestLevelZeroValue(t *testing.T) {
	// Zero-value sanity check. Level still carries inspection-only
	// metadata fields readable on a zero value (the unexported back-ref
	// is nil — a metadata-only Level can't read pixels).
	var lvl opentile.Level
	if lvl.Index != 0 {
		t.Errorf("zero Level.Index: got %d, want 0", lvl.Index)
	}
}

func TestLevelLiteralFields(t *testing.T) {
	// The exported metadata fields are still populatable via struct
	// literal (the v1.0 receiver-method restructure only added an
	// unexported back-ref; all exported fields remain readable/writable).
	lvl := opentile.Level{
		Index:        2,
		PyramidIndex: 0,
		Size:         opentile.Size{W: 1024, H: 768},
		TileSize:     opentile.Size{W: 256, H: 256},
		Grid:         opentile.Size{W: 4, H: 3},
		Compression:  opentile.CompressionJPEG,
		MPP:          opentile.MPP{X: 0.25, Y: 0.25},
		FocalPlane:   0,
		TileOverlap:  opentile.Point{},
	}
	if lvl.Size.W != 1024 || lvl.TileSize.H != 256 || lvl.Grid.H != 3 {
		t.Errorf("literal fields not preserved: %+v", lvl)
	}
}

func TestPyramidLiteralFields(t *testing.T) {
	img := opentile.Pyramid{
		Name:   "primary",
		Index:  0,
		Levels: []opentile.Level{{Index: 0}, {Index: 1}},
	}
	if img.Name != "primary" || len(img.Levels) != 2 {
		t.Errorf("Pyramid literal not preserved: %+v", img)
	}
}

// TestNavigationReturnsStablePointers is the v1.0 pointer-identity gate:
// navigation (Slide.Level / Slide.Levels / Slide.Pyramid / Pyramid.Level)
// returns the SAME *Level / *Pyramid pointer across calls, backed by the
// Slide's internal navigation cache.
func TestNavigationReturnsStablePointers(t *testing.T) {
	s := openSampleSlide(t)

	// Slide.Level(0) is pointer-stable across calls.
	l0a, err := s.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	l0b, err := s.Level(0)
	if err != nil {
		t.Fatalf("Level(0) again: %v", err)
	}
	if l0a != l0b {
		t.Errorf("Slide.Level(0) not pointer-stable: %p vs %p", l0a, l0b)
	}

	// Slide.Levels()[0] is the same pointer as Slide.Level(0).
	if got := s.Levels()[0]; got != l0a {
		t.Errorf("Slide.Levels()[0] (%p) != Slide.Level(0) (%p)", got, l0a)
	}

	// Slide.Pyramid(0) is pointer-stable.
	p0a := s.Pyramid(0)
	p0b := s.Pyramid(0)
	if p0a != p0b {
		t.Errorf("Slide.Pyramid(0) not pointer-stable: %p vs %p", p0a, p0b)
	}
	if got := s.Pyramids()[0]; got != p0a {
		t.Errorf("Slide.Pyramids()[0] (%p) != Slide.Pyramid(0) (%p)", got, p0a)
	}

	// Pyramid(0).Level(0) is the same *Level as Slide.Level(0).
	pl0, err := p0a.Level(0)
	if err != nil {
		t.Fatalf("Pyramid(0).Level(0): %v", err)
	}
	if pl0 != l0a {
		t.Errorf("Pyramid(0).Level(0) (%p) != Slide.Level(0) (%p)", pl0, l0a)
	}

	// Out-of-range navigation: nil pyramid, error level.
	if got := s.Pyramid(99); got != nil {
		t.Errorf("Slide.Pyramid(99) = %p, want nil", got)
	}
	if _, err := s.Level(9999); err == nil {
		t.Error("Slide.Level(9999): want ErrLevelOutOfRange, got nil")
	}
}
