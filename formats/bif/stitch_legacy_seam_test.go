package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg" // register the JPEG decoder
)

// seamSampleCap bounds decode work: ~40 pairs/fixture × 2 tiles × 3 fixtures.
const seamSampleCap = 40

// horizPair is a horizontally-adjacent interior tile pair (col,row)→(col+1,row)
// that carries a live high-confidence horizontal join, with that join's
// measured OverlapX.
type horizPair struct {
	col, row int
	ov       int // OverlapX from the join
}

// liveHorizontalOverlap scans the joint graph for the high-confidence,
// FlagJoined horizontal join between image-grid tiles (col,row) and
// (col+1,row), returning its OverlapX. Returns ok=false when no such join
// exists. Mirrors buildLegacyLayout's resolution: 1-based serpentine indices
// via serpentineToImage, gate on FlagJoined && Confidence>=legacyConfidenceCutoff.
func liveHorizontalOverlap(tl *Tiler, cols, rows int) map[[2]int]int {
	resolve := func(idx int) (c, r int, ok bool) {
		c, r = serpentineToImage(idx-1, cols, rows)
		if c < 0 {
			return 0, 0, false
		}
		return c, r, true
	}
	out := make(map[[2]int]int)
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
			if ar == br && absDelta(ac, bc) == 1 {
				left := min(ac, bc)
				// Keep the first (or smallest-overlap) live join per gap key.
				if prev, seen := out[[2]int{left, ar}]; !seen || j.OverlapX < prev {
					out[[2]int{left, ar}] = j.OverlapX
				}
			}
		}
	}
	return out
}

// sampleHorizPairs returns up to cap interior horizontal pairs with a usable
// overlap (4 < ov < tileW). Deterministic order (row-major by (col,row)).
func sampleHorizPairs(overlaps map[[2]int]int, cols, rows, tileW, cap int) []horizPair {
	var pairs []horizPair
	for row := 0; row < rows; row++ {
		for col := 0; col+1 < cols; col++ {
			ov, ok := overlaps[[2]int{col, row}]
			if !ok {
				continue
			}
			if ov <= 4 || ov >= tileW {
				continue
			}
			pairs = append(pairs, horizPair{col: col, row: row, ov: ov})
			if len(pairs) >= cap {
				return pairs
			}
		}
	}
	return pairs
}

// bandMAD computes the mean-abs-diff (averaged over R,G,B) between two equal-
// width vertical bands of two RGB images, over the common height. bandWidth is
// the number of columns; aCol0/bCol0 are the left edges of each band.
func bandMAD(a, b *decoder.Image, aCol0, bCol0, bandWidth int) float64 {
	h := a.Height
	if b.Height < h {
		h = b.Height
	}
	var sum int64
	var n int64
	for y := 0; y < h; y++ {
		aRow := a.Pix[y*a.Stride:]
		bRow := b.Pix[y*b.Stride:]
		for x := 0; x < bandWidth; x++ {
			ai := (aCol0 + x) * 3
			bi := (bCol0 + x) * 3
			for ch := 0; ch < 3; ch++ {
				d := int(aRow[ai+ch]) - int(bRow[bi+ch])
				if d < 0 {
					d = -d
				}
				sum += int64(d)
			}
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

// TestLegacySeamContinuity is the strongest intrinsic placement gate for legacy
// BIF stitching (#63). The legacy layout removes each gap's measured OverlapX,
// so a horizontally-adjacent pair's overlap band should hold the SAME image
// content: tile A's rightmost `ov` columns must match tile B's leftmost `ov`
// columns. We assert (1) the stitched-alignment overlap-band MAD is small in
// absolute terms, and (2) it is dramatically lower than the naive (no-overlap)
// control — proving the stitch aligns real content, not just a bounding box.
//
// S12-18199-1A is intentionally skipped: only ~11% live joins → too few
// interior horizontal pairs to sample meaningfully.
//
// Observed (OPENTILE_TESTDIR=$PWD/sample_files, libjpeg-turbo, n=40 each):
//
//	OS-1:    stitchMAD=7.32   naiveMAD=33.40  (ratio 0.22)
//	AC1.592: stitchMAD=21.13  naiveMAD=47.68  (ratio 0.44)
//	1_19:    stitchMAD=15.73  naiveMAD=52.16  (ratio 0.30)
//
// The ratio (stitch ÷ naive) is the load-bearing signal: the stitched-alignment
// band is 2.3–4.5× closer than the naive no-overlap control on every fixture,
// proving the placement aligns real content rather than just hitting a bounding
// box. The ABSOLUTE stitchMAD runs higher than a clean re-encode would (single
// digits) because legacy iScan placement is per-gap-AVERAGE: buildLegacyLayout
// removes each column's mean OverlapX (rounded to int), so an individual pair's
// own measured OverlapX — what bandMAD uses — can sit a pixel or two off the
// average, smearing the band; legacy scans also carry genuine sub-pixel
// registration drift the separable model can't undo. Absolute bound pinned to
// 30 (above the worst observed 21.13 with margin, still well below every
// naiveMAD); ratio bound stitch < 0.6 × naive is the primary gate.
func TestLegacySeamContinuity(t *testing.T) {
	// S12-18199-1A excluded (too few live horizontal joins; see doc above).
	const absBound = 30.0  // above worst observed stitchMAD (21.13), below every naiveMAD
	const ratioBound = 0.6 // stitchMAD must be < 0.6 × naiveMAD (primary gate)
	for _, name := range []string{"OS-1", "AC1.592", "1_19"} {
		t.Run(name, func(t *testing.T) {
			tl := openLegacyTiler(t, name)
			l0 := tl.levelImpls[0]
			if l0.layout == nil {
				t.Fatalf("%s: nil layout (legacy stitching not engaged)", name)
			}
			cols, rows := l0.grid.W, l0.grid.H
			tileW := l0.tileSize.W

			overlaps := liveHorizontalOverlap(tl, cols, rows)
			pairs := sampleHorizPairs(overlaps, cols, rows, tileW, seamSampleCap)
			if len(pairs) == 0 {
				t.Skipf("%s: no live interior horizontal pairs with usable overlap", name)
			}

			fac, ok := decoder.Get("jpeg")
			if !ok {
				t.Fatal("jpeg decoder not registered")
			}
			dec := fac.New()
			defer dec.Close()
			decodeTile := func(col, row int) (*decoder.Image, bool) {
				b, err := l0.Tile(col, row)
				if err != nil {
					return nil, false
				}
				img, err := dec.Decode(b, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
				if err != nil {
					return nil, false
				}
				return img, true
			}

			var sumStitch, sumNaive float64
			var n int
			for _, p := range pairs {
				a, aok := decodeTile(p.col, p.row)
				if !aok {
					continue
				}
				b, bok := decodeTile(p.col+1, p.row)
				if !bok {
					continue
				}
				ov := p.ov
				if ov >= a.Width || ov >= b.Width {
					continue
				}
				// Stitched alignment: A's right band [W-ov,W) vs B's left band [0,ov).
				stitch := bandMAD(a, b, a.Width-ov, 0, ov)
				// Naive control: A's right band vs B's RIGHT band [W-ov,W).
				naive := bandMAD(a, b, a.Width-ov, b.Width-ov, ov)
				sumStitch += stitch
				sumNaive += naive
				n++
			}
			if n == 0 {
				t.Skipf("%s: no decodable overlapping pairs sampled", name)
			}
			meanStitchMAD := sumStitch / float64(n)
			meanNaiveMAD := sumNaive / float64(n)
			t.Logf("%s: n=%d stitchMAD=%.2f naiveMAD=%.2f", name, n, meanStitchMAD, meanNaiveMAD)

			if meanStitchMAD >= absBound {
				t.Errorf("%s: meanStitchMAD = %.2f, want < %.1f (overlap bands don't match — placement wrong?)",
					name, meanStitchMAD, absBound)
			}
			if meanStitchMAD >= ratioBound*meanNaiveMAD {
				t.Errorf("%s: meanStitchMAD = %.2f, want < %.2f (%.2f × naiveMAD %.2f) — stitch not clearly aligning content",
					name, meanStitchMAD, ratioBound*meanNaiveMAD, ratioBound, meanNaiveMAD)
			}
		})
	}
}
