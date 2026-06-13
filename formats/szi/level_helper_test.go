package szi_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// mustLevel returns the i-th level of image 0, failing the test if the
// level index is out of range. Convenience for the v1.0 receiver-method
// read API (reads moved off *Slide onto *Level / *Pyramid).
func mustLevel(t *testing.T, s *opentile.Slide, i int) *opentile.Level {
	t.Helper()
	l, err := s.Level(i)
	if err != nil {
		t.Fatalf("Level(%d): %v", i, err)
	}
	return l
}
