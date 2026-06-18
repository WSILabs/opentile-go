package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// TestBuildFrameIndex exercises the <Frame>-node-driven storage mapping. Real
// fixtures fall back to row-major (Ventana-1's 483 frames don't cover its
// padded 504-tile grid — GH #60; OS-1 has no frames), so this synthetic test is
// where the frame-honoring path is verified.
func TestBuildFrameIndex(t *testing.T) {
	// A complete 2×2 permutation in a deliberately NON-row-major order:
	// storage[0]=(1,1) [2]=(1,0) etc. buildFrameIndex must honor the declared
	// order, not assume row-major.
	ei := &bifxml.EncodeInfo{ImageInfos: []bifxml.ImageInfo{{Frames: []bifxml.Frame{
		{Col: 1, Row: 1}, {Col: 0, Row: 1}, {Col: 1, Row: 0}, {Col: 0, Row: 0},
	}}}}
	m := buildFrameIndex(ei, 2, 2, 1)
	if m == nil {
		t.Fatal("a complete permutation should build a frame index")
	}
	for key, want := range map[[3]int]int{
		{0, 1, 1}: 0, {0, 0, 1}: 1, {0, 1, 0}: 2, {0, 0, 0}: 3,
	} {
		if got := m[key]; got != want {
			t.Errorf("frameIndex[%v] = %d, want %d", key, got, want)
		}
	}

	// Frame count != grid tile count → nil (caller falls back to row-major).
	if buildFrameIndex(ei, 3, 3, 1) != nil {
		t.Error("frame count != grid tiles should yield nil")
	}
	// nil EncodeInfo → nil.
	if buildFrameIndex(nil, 2, 2, 1) != nil {
		t.Error("nil EncodeInfo should yield nil")
	}
	// A duplicated position (not a permutation) → nil.
	dup := &bifxml.EncodeInfo{ImageInfos: []bifxml.ImageInfo{{Frames: []bifxml.Frame{
		{Col: 0, Row: 0}, {Col: 0, Row: 0}, {Col: 1, Row: 0}, {Col: 1, Row: 1},
	}}}}
	if buildFrameIndex(dup, 2, 2, 1) != nil {
		t.Error("a non-permutation (duplicate position) should yield nil")
	}
	// A position outside the grid → nil.
	oob := &bifxml.EncodeInfo{ImageInfos: []bifxml.ImageInfo{{Frames: []bifxml.Frame{
		{Col: 0, Row: 0}, {Col: 9, Row: 0}, {Col: 1, Row: 0}, {Col: 1, Row: 1},
	}}}}
	if buildFrameIndex(oob, 2, 2, 1) != nil {
		t.Error("a frame outside the grid should yield nil")
	}
}
