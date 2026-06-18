package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// Secondary coarse gate: legacy L0 stitched dims vs openslide (the all-4 oracle;
// bio-formats crashes on 3 of 4 — see design §0). Targets are Phase-0-measured
// constants from `openslide-show-properties` (hardcoded with provenance, like
// the bio-formats spatial oracle). Tolerance covers the model residual + the
// ~30px openslide-vs-bio-formats reader disagreement + the un-modeled per-column
// Y baseline. The PRIMARY correctness gates are placement-fidelity
// (TestLegacyPlacementResidual, TestLegacySeamContinuity), not these dims.
func TestLegacyDimsVsOpenslide(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	type want struct {
		osW, osH       int
		naiveW, naiveH int // for the better-than-naive assertion
	}
	targets := map[string]want{
		"1_19":         {9583, 11645, 10240, 12240},
		"AC1.592":      {25754, 21966, 27648, 23120},
		"S12-18199-1A": {17194, 10349, 18432, 10880},
		"OS-1":         {105813, 93951, 118784, 102000},
	}
	const tolW, tolH = 8, 35
	for name, w := range targets {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "bif", name+".bif")
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", name)
			}
			s, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer s.Close()
			lvl, err := s.Level(0)
			if err != nil {
				t.Fatal(err)
			}
			dW := lvl.Size.W - w.osW
			dH := lvl.Size.H - w.osH
			if dW < -tolW || dW > tolW || dH < -tolH || dH > tolH {
				t.Errorf("%s L0 = %dx%d, openslide = %dx%d (dW=%+d dH=%+d, tol ±%d/±%d)",
					name, lvl.Size.W, lvl.Size.H, w.osW, w.osH, dW, dH, tolW, tolH)
			}
			naiveErrW := w.naiveW - w.osW
			if naiveErrW > 0 && absInt(dW)*10 > naiveErrW {
				t.Errorf("%s width error %d not <10%% of naive error %d", name, absInt(dW), naiveErrW)
			}
		})
	}
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
