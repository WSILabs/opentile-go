package dzi_test

import (
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// TestDZIOverlap0EdgeRegionReadRegion tests that ReadRegion does not fail when
// the requested region touches a right/bottom edge tile whose stored size is
// smaller than TileSize (the DZI "unpadded edge" convention for Overlap=0).
//
// Concrete geometry:
//   - Image 40×40, TileSize=16, Overlap=0 → 3×3 grid.
//   - Last column width  = 40 - 2*16 = 8 < 16 (unpadded).
//   - Last row    height = 40 - 2*16 = 8 < 16 (unpadded).
//
// The bug (pre-fix): imageDecodedTileInto passes a 16×16 scratch as Dst to
// the strict JPEG decoder, which then rejects the 8×16 or 8×8 edge tile with
// "dst 16x16 != decoded 8x16". The fix gates on edge-tile geometry and uses
// imageDecodedTile (natural-size alloc) instead of the scratch for edge tiles.
func TestDZIOverlap0EdgeRegionReadRegion(t *testing.T) {
	const (
		imgW     = 40
		imgH     = 40
		tileSize = 16
		// With TileSize=16: cols = ceil(40/16) = 3, rows = 3.
		// Edge col (col 2): width  = 40 - 2*16 = 8
		// Edge row (row 2): height = 40 - 2*16 = 8
	)
	dir := t.TempDir()
	// writeSyntheticDZI (defined in dzi_test.go) writes JPEG tiles at their
	// clamped (unpadded) sizes — exactly the DZI Overlap=0 convention that
	// causes the bug. The edge tiles are genuinely 8×16, 16×8, and 8×8.
	dziPath := writeSyntheticDZI(t, dir, "edge", imgW, imgH, tileSize, 0)

	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	l0, err := s.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	if l0.Size.W != imgW || l0.Size.H != imgH {
		t.Fatalf("Level(0) size = %dx%d, want %dx%d", l0.Size.W, l0.Size.H, imgW, imgH)
	}
	if l0.OverlapMode != opentile.OverlapNone {
		t.Fatalf("OverlapMode = %v, want OverlapNone", l0.OverlapMode)
	}

	// Read a region that includes the bottom-right edge tiles (col=2 and row=2).
	// Without the fix this returns "dst 16x16 != decoded 8x16" (or similar).
	r := opentile.Region{
		Origin: opentile.Point{X: 24, Y: 24},
		Size:   opentile.Size{W: 16, H: 16},
	}
	img, err := l0.ReadRegion(r)
	switch {
	case errors.Is(err, decoder.ErrCodecUnavailable):
		// nocgo build: JPEG decode unavailable — the path under test
		// (imageDecodedTileInto / imageDecodedTile) is not reachable; skip.
		t.Skip("JPEG codec unavailable (nocgo build) — skipping edge-region decode test")
	case err != nil:
		// This is the bug: pre-fix this is "dst WxH != decoded WxH".
		t.Fatalf("ReadRegion touching edge tiles: %v (pre-fix: dst-size mismatch in strict decoder)", err)
	}
	// The region is fully within the 40×40 image, so output must be 16×16.
	if img.Width != 16 || img.Height != 16 {
		t.Errorf("ReadRegion result = %dx%d, want 16x16", img.Width, img.Height)
	}
}
