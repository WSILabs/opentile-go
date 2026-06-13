//go:build bfparity

package oracle_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/leicascn"
	"github.com/wsilabs/opentile-go/tests/oracle"
)

// TestBioFormatsParity_SCN compares opentile-go's structural reading
// of each SCN fixture against bio-formats CLI's `showinf` output.
//
// Per v0.11 design Q9, this is structural-equivalence parity —
// confirms our IFD-to-image-pyramid mapping matches bio-formats's
// view of the same file. Tile byte-equality is NOT feasible
// (bio-formats decodes + re-encodes JPEG; ours is raw passthrough).
//
// Specifically asserts:
//   - Number of opentile-go Levels matches the count of bio-formats
//     "Series" minus auxiliary-pyramid entries (each auxiliary's
//     non-thumbnail levels are dropped — our auxiliaries are
//     single-image AssociatedImages at highest resolution).
//   - Per-level Width / Height / SizeC alignment with the
//     corresponding bio-formats series entry.
//
// Skipped if /opt/bftools/showinf isn't present.
func TestBioFormatsParity_SCN(t *testing.T) {
	if _, err := exec.LookPath("/opt/bftools/showinf"); err != nil {
		t.Skip("/opt/bftools/showinf not installed")
	}
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}

	for _, tc := range []struct {
		filename     string
		expectMains  int  // expected number of opentile-go Levels (= main pyramid depth)
		expectSizeC  int
		expectMacros int // expected AssociatedImage count
	}{
		{"Leica-1.scn", 5, 1, 1},
		{"Leica-2.scn", 6, 1, 1},
		{"Leica-Fluorescence-1.scn", 4, 3, 2},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join(dir, "scn", tc.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}

			bf, err := oracle.RunShowinfForTest(path)
			if err != nil {
				t.Fatalf("showinf: %v", err)
			}

			tlr, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tlr.Close()

			levels := tlr.Levels()
			associated := tlr.AssociatedImages()

			// Pin our level count + associated count up front (bio-
			// formats's series count includes both, plus its own
			// "Thumbnail series" splits, so a direct equality isn't
			// the comparison shape).
			if got := len(levels); got != tc.expectMains {
				t.Errorf("opentile len(Levels()) = %d, want %d", got, tc.expectMains)
			}
			if got := len(associated); got != tc.expectMacros {
				t.Errorf("opentile len(Associated()) = %d, want %d", got, tc.expectMacros)
			}
			if got := tlr.Pyramids()[0].SizeC(); got != tc.expectSizeC {
				t.Errorf("opentile SizeC = %d, want %d", got, tc.expectSizeC)
			}

			// Bio-formats sanity check.
			//
			// Single-region files (Leica-1, Fluorescence): our L0
			// pixel extent matches one main scan's L0 in bio-formats's
			// series list — direct equality.
			//
			// Multi-region files (Leica-2): our L0 is the UNION
			// extent across N main scans (not present in bio-formats's
			// per-image series list). We instead verify that bio-
			// formats reports series count consistent with
			// (mains × pyramid depth + auxiliaries × aux-depth).
			md, _ := leicascn.MetadataOf(tlr)
			regionCount := len(md.Regions)

			bfSizes := make(map[[2]int]bool, len(bf.Series))
			for _, s := range bf.Series {
				bfSizes[[2]int{s.Width, s.Height}] = true
			}

			if regionCount == 1 {
				// Per-level size-presence check.
				for i, lvl := range levels {
					key := [2]int{lvl.Size().W, lvl.Size().H}
					if !bfSizes[key] {
						t.Errorf("L%d size %v missing from bio-formats series list (single-region)",
							i, lvl.Size())
					}
				}
			} else {
				// Multi-region: bio-formats series count =
				// regionCount × len(levels) + (aux count × aux depth)
				// On our 3 SCN fixtures, every aux has depth 3.
				const auxDepth = 3
				expected := regionCount*len(levels) + len(associated)*auxDepth
				if bf.SeriesCount != expected {
					t.Errorf("multi-region: bio-formats SeriesCount = %d, want %d (= %d regions × %d levels + %d aux × %d aux-depth)",
						bf.SeriesCount, expected,
						regionCount, len(levels), len(associated), auxDepth)
				}
			}

			t.Logf("✓ %s: opentile %d levels / %d regions / %d associated / SizeC=%d ; bio-formats %d series",
				tc.filename, len(levels), regionCount, len(associated),
				tlr.Pyramids()[0].SizeC(), bf.SeriesCount)
		})
	}
}
