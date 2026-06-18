package bif

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// legacyFixtures: the 4 local-only legacy iScan BIFs (PHI; skip when absent).
var legacyFixtures = []string{"OS-1", "S12-18199-1A", "AC1.592", "1_19"}

// openLegacyTiler opens a legacy fixture as a *Tiler, or skips.
func openLegacyTiler(t *testing.T, name string) *Tiler {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "bif", name+".bif")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("%s not present", name)
	}
	t.Cleanup(func() { f.Close() })
	fi, _ := f.Stat()
	file, err := tiff.Open(f, fi.Size())
	if err != nil {
		t.Fatalf("%s: tiff open: %v", name, err)
	}
	r, err := openFromTIFFFile(file, nil)
	if err != nil {
		t.Fatalf("%s: bif open: %v", name, err)
	}
	return r.(*Tiler)
}

// TestLegacyPlacementResidual asserts the per-gap-average layout places each
// adjacent (live, high-conf) tile pair within a tight residual of that join's
// OWN measured offset — i.e. dims convergence is not hiding per-tile drift.
// Phase-0 OS-1: X p99 3.1 / max 55.9; Y p99 1.8 / max 3.3.
func TestLegacyPlacementResidual(t *testing.T) {
	for _, name := range legacyFixtures {
		t.Run(name, func(t *testing.T) {
			tl := openLegacyTiler(t, name)
			l0 := tl.levelImpls[0]
			lay := l0.layout
			if lay == nil {
				t.Fatalf("%s: nil layout (legacy stitching not engaged)", name)
			}
			cols, rows, tw, th := l0.grid.W, l0.grid.H, l0.tileSize.W, l0.tileSize.H
			resolve := func(idx int) (int, int, bool) {
				c, r := serpentineToImage(idx-1, cols, rows)
				if c < 0 {
					return 0, 0, false
				}
				return c, r, true
			}
			var resid []float64
			for _, ii := range tl.encodeInfo.ImageInfos {
				for _, j := range ii.Joints {
					if !j.FlagJoined || j.Confidence < legacyConfidenceCutoff {
						continue
					}
					ac, ar, aok := resolve(j.Tile1)
					bc, br, bok := resolve(j.Tile2)
					if !aok || !bok {
						continue
					}
					ax, ay, _ := lay.TileOrigin(ac, ar)
					bx, by, _ := lay.TileOrigin(bc, br)
					if ar == br && absDelta(ac, bc) == 1 {
						got := absDelta(ax, bx)
						resid = append(resid, float64(absDelta(got, tw-j.OverlapX)))
					}
					if ac == bc && absDelta(ar, br) == 1 {
						got := absDelta(ay, by)
						resid = append(resid, float64(absDelta(got, th-j.OverlapY)))
					}
				}
			}
			if len(resid) == 0 {
				t.Fatalf("%s: no live high-conf joins resolved", name)
			}
			sort.Float64s(resid)
			p99 := resid[int(0.99*float64(len(resid)-1))]
			maxResid := resid[len(resid)-1]
			t.Logf("%s: residual n=%d p99=%.1f max=%.1f", name, len(resid), p99, maxResid)
			if p99 > 8 {
				t.Errorf("%s: residual p99 = %.1f, want <= 8 (per-tile drift too high — averaging trap)", name, p99)
			}
			if maxResid > 64 {
				t.Errorf("%s: residual max = %.1f, want <= 64", name, maxResid)
			}
		})
	}
}
