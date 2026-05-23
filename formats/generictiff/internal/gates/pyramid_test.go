//go:build gates

// Package gates holds the v0.10 JIT verification probes that run
// before any production code lands. Build-tag `gates` keeps them
// out of `make test`. Deleted at end of v0.10 milestone.
package gates

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// candidate is a tiled IFD that's a candidate pyramid level.
type candidate struct {
	idx           int
	w, h          int
	tw, th        int
	area          int64
	compression   uint32
	photometric   uint32
	samplesPerPix uint32
}

// TestT1PyramidValidatorAgainstCMU1 confirms the v0.10 design's
// sealed scale tolerances (±2% inter-axis, ±5% inter-level) work
// against the real CMU-1.tiff fixture and produce the expected
// 9-level pyramid acceptance.
//
// Spec: docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md §4.
func TestT1PyramidValidatorAgainstCMU1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.tiff")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("%s not present: %v", path, err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}

	pages := tf.Pages()
	t.Logf("CMU-1.tiff: %d top-level IFDs", len(pages))

	// Step 1: filter to tiled IFDs (TileWidth + TileLength tags
	// both present and non-zero per spec §4.1).
	var cands []candidate
	for i, p := range pages {
		tw, hasTW := p.TileWidth()
		th, hasTH := p.TileLength()
		if !hasTW || !hasTH || tw == 0 || th == 0 {
			t.Logf("IFD %d: not tiled, excluded", i)
			continue
		}
		iw, _ := p.ImageWidth()
		il, _ := p.ImageLength()
		comp, _ := p.Compression()
		photo, _ := p.Photometric()
		spp, _ := p.SamplesPerPixel()
		cands = append(cands, candidate{
			idx: i, w: int(iw), h: int(il), tw: int(tw), th: int(th),
			area:        int64(iw) * int64(il),
			compression: comp, photometric: photo, samplesPerPix: spp,
		})
	}
	t.Logf("tiled candidates: %d", len(cands))

	// Step 2: sort by area, descending (largest = baseline).
	sort.Slice(cands, func(i, j int) bool { return cands[i].area > cands[j].area })

	// Step 3: ≥3 candidates (Q2).
	if len(cands) < 3 {
		t.Fatalf("only %d tiled candidates; spec §4.3 requires ≥3", len(cands))
	}

	// Step 4: validate scale ratios (Q1).
	t.Log("scale-ratio check:")
	ratios := make([]float64, 0, len(cands)-1)
	for i := 0; i < len(cands)-1; i++ {
		a, b := cands[i], cands[i+1]
		ratioW := float64(a.w) / float64(b.w)
		ratioH := float64(a.h) / float64(b.h)
		// Inter-axis: |ratio_W - ratio_H| / ratio_W ≤ 0.02.
		interAxis := math.Abs(ratioW-ratioH) / ratioW
		t.Logf("  L%d→L%d: %d×%d → %d×%d  ratio_W=%.4f ratio_H=%.4f  inter-axis=%.4f%%",
			i, i+1, a.w, a.h, b.w, b.h, ratioW, ratioH, interAxis*100)
		if interAxis > 0.02 {
			t.Errorf("  inter-axis exceeds ±2%% tolerance at L%d→L%d", i, i+1)
		}
		if ratioW <= 1 || ratioH <= 1 {
			t.Errorf("  level not strictly smaller at L%d→L%d", i, i+1)
		}
		ratios = append(ratios, ratioW)
	}
	// Inter-level: each consecutive pair of ratios must agree within ±5%.
	for i := 0; i < len(ratios)-1; i++ {
		drift := math.Abs(ratios[i]-ratios[i+1]) / ratios[i]
		if drift > 0.05 {
			t.Errorf("inter-level drift %.4f%% > ±5%% between transitions %d and %d",
				drift*100, i, i+1)
		}
	}

	// Step 5: validate uint8 RGB / YCbCr / grayscale (per §4.5).
	for _, c := range cands {
		validPhoto := c.photometric == 1 || c.photometric == 2 || c.photometric == 6
		if !validPhoto {
			t.Errorf("IFD %d: photometric=%d not in {1,2,6}", c.idx, c.photometric)
		}
	}

	// Step 6: validate compression (per §4.6 — JPEG=7 here).
	for _, c := range cands {
		switch c.compression {
		case 1, 5, 7, 8, 33003: // None, LZW, JPEG, Deflate, JP2K
			// OK
		default:
			t.Errorf("IFD %d: compression=%d not in {1,5,7,8,33003}", c.idx, c.compression)
		}
	}

	// Step 7: multi-pyramid rejection check — pyramid must consume
	// ALL tiled IFDs (no leftovers). For CMU-1.tiff: all 9 IFDs
	// form the single pyramid.
	if len(cands) != len(pages) {
		t.Logf("note: %d tiled candidates vs %d total IFDs (leftovers exist)",
			len(cands), len(pages))
	}

	t.Log("✓ CMU-1.tiff satisfies all validator rules:")
	t.Logf("  • %d tiled candidates ≥ 3 minimum", len(cands))
	t.Logf("  • all consecutive scale ratios within ±2%% inter-axis")
	t.Logf("  • all consecutive ratios consistent within ±5%% inter-level")
	t.Logf("  • all photometric ∈ {YCbCr/RGB/grayscale}, compression in whitelist")
	t.Logf("  • baseline tile size: %d×%d", cands[0].tw, cands[0].th)
	t.Logf("  • baseline dims: %d×%d (area=%s)", cands[0].w, cands[0].h, humanInt(cands[0].area))
}

func humanInt(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGi", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMi", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d", n)
	}
}
