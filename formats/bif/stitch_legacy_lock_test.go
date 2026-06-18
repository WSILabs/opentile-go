package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestOS1LegacyNaiveDims LOCKS the legacy iScan BIF (Coreo/HT) L0 dimensions
// after buildLegacyLayout (#63) was wired in. The Roche whitepaper disclaims
// legacy reconstruction ("cannot be reconstructed correctly") but the
// per-gap-average separable model (#63 Phase 0) is the best clean-room
// approach available without porting GPL bio-formats logic.
//
// Pinned values (buildLegacyLayout separable per-gap-average compaction, #63):
//
//	L0: 105818×93924  (per-gap-average overlap reconstruction)
//
// If a future change to buildLegacyLayout (e.g. a spanning-tree or improved
// overlap model) shifts these, update them deliberately after verifying the new
// values are closer to the bio-formats ground truth.
//
// Skipped when OPENTILE_TESTDIR/bif/OS-1.bif is not present (large local-only
// fixture; not in CI fixtures).
func TestOS1LegacyNaiveDims(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "bif", "OS-1.bif")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("OS-1.bif not present: %v", err)
	}

	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer s.Close()

	lvl, err := s.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}

	// Pinned compacted extent (buildLegacyLayout per-gap-average separable model,
	// #63). Bio-formats reports a different compacted size via a GPL heuristic
	// (columnXAdjust) we do not port; these values are the clean-room per-gap
	// average result. Update deliberately if the algorithm improves.
	const wantW, wantH = 105818, 93924
	if lvl.Size.W != wantW || lvl.Size.H != wantH {
		t.Errorf("OS-1 L0 = %dx%d, want %dx%d (legacy naive extent; stitching deferred — see #63)",
			lvl.Size.W, lvl.Size.H, wantW, wantH)
	}
}
