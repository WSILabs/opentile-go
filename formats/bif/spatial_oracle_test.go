package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// meanLuma returns the average byte value over an image's pixel buffer — a
// cheap brightness proxy (tissue is darker than the near-white background).
func meanLuma(pix []byte) float64 {
	if len(pix) == 0 {
		return 0
	}
	var sum int64
	for _, b := range pix {
		sum += int64(b)
	}
	return float64(sum) / float64(len(pix))
}

// TestBIFTilePlacementSpatial is a SPATIAL/placement oracle (GH #59): it checks
// that tiles land in the right positions, not merely that their bytes are read
// intact (which TestTifffileParityBIF already covers per-index). It encodes the
// bio-formats VENTANA ground truth for Ventana-1.bif at L4 (2×2 tiles): the
// tissue sits in the TOP row, background in the bottom row —
//
//	bio-formats L4 mean brightness: TL≈172 TR≈191 (tissue) / BL≈227 BR≈236 (bg)
//
// Under the old serpentine remap opentile flipped this (tissue at the bottom),
// so this test fails before the GH #57 fix and passes after. openslide can't be
// the reference here — it rejects this DP 200 file ("Bad direction LEFT").
func TestBIFTilePlacementSpatial(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(dir, "bif", "Ventana-1.bif")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("Ventana-1.bif not present: %v", err)
	}

	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer s.Close()

	// L4 is the smallest multi-tile level (2×2) — single-tile top levels hide
	// ordering bugs, which is how #57 went unnoticed.
	lvl, err := s.Pyramid(0).Level(4)
	if err != nil {
		t.Fatalf("Level(4): %v", err)
	}
	if lvl.Grid.W < 2 || lvl.Grid.H < 2 {
		t.Skipf("L4 grid %dx%d is not multi-tile; fixture changed", lvl.Grid.W, lvl.Grid.H)
	}

	luma := func(col, row int) float64 {
		img, err := lvl.DecodedTile(col, row)
		if err != nil {
			t.Fatalf("DecodedTile(%d,%d): %v", col, row, err)
		}
		return meanLuma(img.Pix)
	}

	topMean := (luma(0, 0) + luma(1, 0)) / 2    // tissue → darker
	bottomMean := (luma(0, 1) + luma(1, 1)) / 2 // background → lighter

	// Correct (row-major) placement: tissue in the top row, so the top row is
	// clearly darker than the bottom. The old serpentine remap inverts this.
	if topMean >= bottomMean {
		t.Fatalf("tile placement looks vertically flipped (serpentine): top mean %.0f >= bottom mean %.0f; "+
			"bio-formats ground truth has tissue (darker) in the TOP row", topMean, bottomMean)
	}
}
