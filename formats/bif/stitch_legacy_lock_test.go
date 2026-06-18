package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestOS1LegacyNaiveDims LOCKS the current legacy (GenerationLegacyIScan)
// behavior: opentile-go does NOT yet stitch overlapping legacy iScan BIF
// (Coreo/HT) — the DP stitch engine is gated to GenerationSpecCompliant, so
// legacy gets the naive (un-compacted) layout and L0 reports the raw frame-grid
// extent.
//
// bio-formats reaches a smaller compacted size via a GPL columnXAdjust
// heuristic we will not port; the Roche whitepaper disclaims legacy
// reconstruction ("cannot be reconstructed correctly"). Locking this so a
// future legacy-stitching fix (#60-legacy) is a deliberate, reviewed change —
// not silent drift. See design §E.
//
// Skipped when OPENTILE_TESTDIR/bif/OS-1.bif is not present (large local-only
// fixture; not in CI fixtures).
//
// Pinned values (naive extent = Cols×TileW × Rows×TileH, no overlap
// compaction):
//
//	L0: 118784×102000  (116 cols × 1024 = 118784; 75 rows × 1360 = 102000)
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

	// Pinned naive extent (no overlap compaction; DP stitch engine is gated to
	// spec-compliant DP slides only). A future #60-legacy implementation that
	// compacts legacy iScan frames should update these constants — deliberately,
	// after verifying against bio-formats or another authoritative oracle.
	const wantW, wantH = 118784, 102000
	if lvl.Size.W != wantW || lvl.Size.H != wantH {
		t.Errorf("OS-1 L0 = %dx%d, want %dx%d (legacy naive extent; stitching deferred #60-legacy)",
			lvl.Size.W, lvl.Size.H, wantW, wantH)
	}
}
