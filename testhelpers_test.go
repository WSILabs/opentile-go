package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// mustLevel returns the i-th level of image 0, failing the test if the
// level index is out of range. A convenience for the v1.0 receiver-method
// read API, which moved reads off *Slide onto *Level / *Pyramid.
func mustLevel(t *testing.T, s *opentile.Slide, i int) *opentile.Level {
	t.Helper()
	l, err := s.Level(i)
	if err != nil {
		t.Fatalf("Level(%d): %v", i, err)
	}
	return l
}

// mustImageLevel returns the level at (image, level), failing the test if
// either index is out of range.
func mustImageLevel(t *testing.T, s *opentile.Slide, image, level int) *opentile.Level {
	t.Helper()
	p := s.Pyramid(image)
	if p == nil {
		t.Fatalf("Pyramid(%d): nil", image)
	}
	l, err := p.Level(level)
	if err != nil {
		t.Fatalf("Pyramid(%d).Level(%d): %v", image, level, err)
	}
	return l
}
