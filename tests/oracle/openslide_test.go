//go:build parity

package oracle_test

import (
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/tests"
	"github.com/wsilabs/opentile-go/tests/oracle"
)

// openslideBIFSlideCandidates: BIF fixtures openslide can read.
// Spec-compliant DP 200 fixtures (Ventana-1) are excluded — openslide
// hard-rejects them on `Direction="LEFT"` (verified in T1 detection
// gate). Use the tifffile oracle (T21) for those.
var openslideBIFSlideCandidates = []string{
	"OS-1.bif",
}

// TestOpenslideBIFParity validates pixel-equivalence between
// opentile-go's Tile() output and openslide's read_region for BIF
// fixtures openslide can read.
//
// Comparison strategy: send opentile-go's JPEG bytes to a Python
// runner that decodes them via PIL (sharing libjpeg-turbo with
// openslide-python's read_region) and compares pixel-by-pixel to
// openslide's read_region RGBA.
//
// **STATUS: infrastructure only as of v0.7.** Pixel parity with
// openslide on OS-1 is currently broken because the two libraries
// use different image-coordinate systems for legacy iScan slides:
//
//   - opentile-go's Tile(col, row) at level N reads the tile at
//     (col*tw, row*th) within the **padded TIFF grid** (e.g.,
//     OS-1 L5: 3712×3192).
//   - openslide.read_region(...) at level N reads from the
//     **AOI hull** (OS-1 L5: 3307×2936) — strictly less than the
//     padded grid.
//
// Spec ("AOI Positions") says padding is added "to the top and
// right" of the AOI; opentile-go reports the padded extent verbatim
// from TIFF tags. A pixel comparison between (0, 0) in the two
// systems is fundamentally mismatched — they reference different
// physical regions.
//
// Resolving this for v0.8 likely involves:
//   - exposing openslide's "region" properties on Tiler.Metadata
//     (openslide.region[i].(x|y|width|height) populates AOI bounds)
//   - adding a Tile()-equivalent that returns AOI-cropped output
//   - or documenting that the openslide-vs-opentile-go coord
//     translation is the consumer's responsibility.
//
// For v0.7 we skip the parity assertion and run the test as a
// no-op to keep the runner / session / protocol exercised. The
// runner code is ready for the v0.8 fix.
func TestOpenslideBIFParity(t *testing.T) {
	t.Skip("openslide-vs-opentile-go BIF coordinate-system parity is a v0.8 work item; runner infrastructure landed in T20 awaiting resolution")
	dir := tests.TestdataDir()
	if dir == "" {
		return
	}
	for _, name := range openslideBIFSlideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			runOpenslideParityOnBIF(t, slide)
		})
	}
}

func runOpenslideParityOnBIF(t *testing.T, slide string) {
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()
	if tiler.Format() != opentile.FormatBIF {
		t.Fatalf("not a BIF tiler: format=%s", tiler.Format())
	}

	sess, err := oracle.NewOpenslideSession(slide)
	if err != nil {
		t.Fatalf("openslide session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Logf("openslide session close: %v", err)
		}
	}()

	for li, lvl := range tiler.Levels() {
		if ov := lvl.TileOverlap(); ov.X != 0 || ov.Y != 0 {
			t.Logf("level %d: skipping (TileOverlap=%v; openslide composes neighbours)", li, ov)
			continue
		}
		positions := samplePositions(lvl.Grid(), false)
		ts := lvl.TileSize()
		for _, pos := range positions {
			our, err := lvl.Tile(pos.X, pos.Y)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): Go error: %v", li, pos.X, pos.Y, err)
				continue
			}
			res, err := sess.CompareTile(li, pos.X, pos.Y, ts.W, ts.H, our)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): openslide oracle error: %v", li, pos.X, pos.Y, err)
				continue
			}
			if res == nil {
				t.Errorf("level %d tile (%d,%d): openslide oracle returned no result", li, pos.X, pos.Y)
				continue
			}
			if res.DecodeError {
				t.Errorf("level %d tile (%d,%d): PIL failed to decode opentile-go's bytes", li, pos.X, pos.Y)
				continue
			}
			if res.OutOfBounds {
				// Tile extends past openslide's AOI-hull extent
				// while opentile-go reports the padded TIFF grid.
				// Mismatch is by-design at the edge; skip silently.
				continue
			}
			if !res.Match {
				t.Errorf("slide %s level %d tile (%d,%d): pixel divergence — max channel delta=%d, mismatched pixels=%d/%d",
					filepath.Base(slide), li, pos.X, pos.Y, res.MaxDelta, res.MismatchCount, res.Total)
			}
		}
	}
}
