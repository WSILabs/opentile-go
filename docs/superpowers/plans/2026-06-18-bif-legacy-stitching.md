# BIF legacy (iScan) overlap-aware stitching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **GIT DISCIPLINE (mandatory for every subagent):** Work ONLY on the current branch `feat/bif-legacy-stitching`. NEVER run `git checkout <sha>`, `git switch --detach`, `git reset --hard`, `git rebase`, `git push --force`, or delete branches. Commit on the current branch only. If you think you need to change branches or rewrite history, STOP and report instead.

**Goal:** Stitch legacy iScan BIF (Coreo/HT — no `<Frame>` nodes) near-exact to bio-formats/openslide, clean-room, by reconstructing tile placement from the file's own `<TileJointInfo>` overlap statistics, reusing PR #64's stitch/compositing machinery.

**Architecture:** A new `buildLegacyLayout` in the existing pure stitch engine computes a **separable per-axis** layout: tile `(col,row)` → `(X[col], Y[row])`, where `X[]`/`Y[]` accumulate per-column-gap / per-row-gap **average** overlaps (float, global-mean fill for empty gaps) over the live, high-confidence joins. It emits the same `*Layout` as the DP path, so `Level.Size`, `ReadRegion`, `ReadRegionScaled`, and `ScaledStrips` inherit stitched output unchanged. Validated by **placement-fidelity** gates (per-join residual + seam-continuity pixel MAD), with a dims cross-check against openslide as a secondary coarse gate.

**Tech Stack:** Go 1.25 (builtin `min`/`max`), existing `internal/bifxml` joints, `serpentineToImage`, the `regionLayout` machinery from PR #64, `decoder/all` (JPEG) for the seam check, `openslide-show-properties` (provenance only — targets are hardcoded constants).

**Spec:** `docs/superpowers/specs/2026-06-18-bif-legacy-stitching-design.md`

**Phase-0-validated facts the tasks rely on (do not re-derive):**
- All 4 legacy fixtures are single-AOI, tile **1024×1360**, serpentine indices **1-based**.
- openslide L0 targets: `1_19` 9583×11645, `AC1.592` 25754×21966, `S12-18199-1A` 17194×10349, `OS-1` 105813×93951 (bio-formats: OS-1 105817×93978; crashes on the other 3).
- Per-gap-float model lands within **width ≤+5 / height ≤−27** of openslide on all 4.
- Per-join residual (model placement vs each join's own offset) on OS-1: X median 0.3 / p99 3.1 / max 55.9; Y median 0.5 / p99 1.8 / max 3.3.
- Fixtures are local-only PHI (`OS-1.bif`, `S12-18199-1A.bif`, `AC1.592.bif`, `1_19.bif`); all fixture-gated tests skip without them (always in CI).

---

## File Structure

- `formats/bif/stitch.go` — MODIFY: add `buildLegacyLayout` + `hasLiveJoint` + `legacyConfidenceCutoff`; insert legacy branch into `BuildLayout`.
- `formats/bif/stitch_legacy_test.go` — CREATE: fixture-free engine unit tests (CI-safe).
- `formats/bif/stitch_legacy_placement_test.go` — CREATE (`package bif`): per-join residual gate over the 4 fixtures (internal access to joints + layout).
- `formats/bif/stitch_legacy_seam_test.go` — CREATE (`package bif_test`): seam-continuity pixel-MAD gate (decode via public API).
- `formats/bif/stitch_legacy_dims_test.go` — CREATE (`package bif_test`): dims cross-check vs openslide constants (all 4) + replaces `TestOS1LegacyNaiveDims`.
- `formats/bif/stitch_legacy_lock_test.go` — MODIFY/REMOVE: the old naive lock test is superseded by the dims test (delete it there to avoid a duplicate/contradictory assertion).
- `docs/formats/bif.md`, `CHANGELOG.md`, `docs/migrations/2026-06-18-bif-level-size-stitched.md` — MODIFY: legacy is now stitched near-exact.

---

## Task 1: `buildLegacyLayout` engine + dispatch + fixture-free unit tests

**Files:**
- Modify: `formats/bif/stitch.go`
- Test: `formats/bif/stitch_legacy_test.go`

- [ ] **Step 1: Write the failing tests**

Create `formats/bif/stitch_legacy_test.go`:

```go
package bif

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// legacyEI builds a single-AOI legacy EncodeInfo (no Frames) with uniform
// per-gap overlaps: every horizontal join overlaps ox, every vertical join oy.
// Tile1/Tile2 are 1-based serpentine indices (legacy convention). All joins
// FlagJoined, Confidence=100 unless overridden by the caller afterwards.
func legacyEI(cols, rows, ox, oy int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	serp := func(c, r int) int { return imageToSerpentine(c, r, cols, rows) + 1 } // 1-based
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: serp(c, r), Tile2: serp(c+1, r), OverlapX: ox, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "UP", Tile1: serp(c, r), Tile2: serp(c, r+1), OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii}}
}

func TestBuildLegacyLayoutUniformOverlap(t *testing.T) {
	// 3×2 grid, tile 1000×1000, every gap overlaps 100 → step 900.
	ei := legacyEI(3, 2, 100, 100)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	// X[col] = col*900: 0, 900, 1800 → width 1800+1000 = 2800.
	for col, want := range []int{0, 900, 1800} {
		x, _, ok := l.TileOrigin(col, 0)
		if !ok || x != want {
			t.Errorf("X[%d] = (%d,%v), want %d", col, x, ok, want)
		}
	}
	// Y[row] = row*900: 0, 900 → height 900+1000 = 1900.
	if _, y, _ := l.TileOrigin(0, 1); y != 900 {
		t.Errorf("Y[1] = %d, want 900", y)
	}
	if l.Width != 2800 || l.Height != 1900 {
		t.Errorf("dims = %dx%d, want 2800x1900", l.Width, l.Height)
	}
}

func TestBuildLegacyLayoutEmptyGapGlobalFill(t *testing.T) {
	// 3×1, tile 1000. Only the gap 0→1 has a join (overlap 100); gap 1→2 has
	// none → must take the global mean (100), NOT 0.
	ei := legacyEI(3, 1, 100, 0)
	// Remove the second horizontal join (the gap-1 join) to leave gap 1 empty.
	js := ei.ImageInfos[0].Joints[:0:0]
	for _, j := range ei.ImageInfos[0].Joints {
		ac, _ := serpentineToImage(j.Tile1-1, 3, 1)
		bc, _ := serpentineToImage(j.Tile2-1, 3, 1)
		if min(ac, bc) == 1 { // drop the gap-1 (col1↔col2) join
			continue
		}
		js = append(js, j)
	}
	ei.ImageInfos[0].Joints = js
	l := BuildLayout(StitchInput{Cols: 3, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	// gap0 = 100 (measured), gap1 = 100 (global-fill) → X = 0,900,1800.
	x2, _, _ := l.TileOrigin(2, 0)
	if x2 != 1800 {
		t.Errorf("X[2] = %d, want 1800 (empty gap1 must use global mean 100, not 0 → else 1900)", x2)
	}
}

func TestBuildLegacyLayoutDeadAndLowConfExcluded(t *testing.T) {
	ei := legacyEI(2, 1, 100, 0) // one horizontal join, overlap 100
	ei.ImageInfos[0].Joints[0].FlagJoined = false // dead → excluded → no overlap data → naive-ish
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationLegacyIScan})
	// No live joints at all → buildLegacyLayout declines → naive: X[1]=1000.
	if x, _, _ := l.TileOrigin(1, 0); x != 1000 {
		t.Errorf("dead-only joins must decline to naive (X[1]=1000), got %d", x)
	}
	// Low confidence: same — sub-cutoff joins excluded; if it's the only join → naive.
	ei2 := legacyEI(2, 1, 100, 0)
	ei2.ImageInfos[0].Joints[0].Confidence = 50
	l2 := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei2, Generation: GenerationLegacyIScan})
	if x, _, _ := l2.TileOrigin(1, 0); x != 1000 {
		t.Errorf("sub-cutoff joins must be excluded (X[1]=1000), got %d", x)
	}
}

func TestBuildLegacyLayoutGatingDPUntouched(t *testing.T) {
	// A DP-generation slide must NOT take the legacy path.
	ei := legacyEI(2, 1, 100, 0)
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// DP path: this synthetic EI has no Frames, so buildDPLayout's frame-based
	// resolution still runs on serpentine indices; regardless, the legacy branch
	// must not fire for GenerationSpecCompliant. Assert legacy didn't claim it by
	// checking the gating helper directly.
	if buildLegacyLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1000, TileH: 1000, EncodeInfo: ei, Generation: GenerationSpecCompliant}) != nil {
		t.Error("buildLegacyLayout must return nil for GenerationSpecCompliant")
	}
	_ = l
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./formats/bif/ -run TestBuildLegacyLayout -v`
Expected: FAIL — `buildLegacyLayout` undefined; uniform-overlap test sees naive (X[1]=1000 not 900).

- [ ] **Step 3: Implement `buildLegacyLayout` + dispatch**

In `formats/bif/stitch.go`, change `BuildLayout` to insert the legacy branch:

```go
func BuildLayout(in StitchInput) *Layout {
	if dp := buildDPLayout(in); dp != nil {
		return dp
	}
	if lg := buildLegacyLayout(in); lg != nil {
		return lg
	}
	return buildNaiveLayout(in)
}
```

Add (anywhere after `buildDPLayout`):

```go
// legacyConfidenceCutoff is the minimum TileJointInfo Confidence trusted when
// reconstructing legacy iScan placement. Phase 0: the value is non-critical
// (98 vs 0 move OS-1 dims by a few px); 98 keeps only high-confidence joins.
const legacyConfidenceCutoff = 98

// buildLegacyLayout reconstructs tile placement for legacy iScan BIF (Coreo/HT),
// which carry no <Frame> nodes — the only position signal is the TileJointInfo
// overlap graph. The graph is too fragmented to traverse per-tile (Phase 0:
// ~5% reachable from a root), so we use a SEPARABLE per-axis model derived from
// the aggregate per-gap overlap statistics (this is #63's recommended
// "accumulate per-gap", NOT a single global average): tile (col,row) lands at
// (X[col], Y[row]) where X[]/Y[] accumulate (tile - perGapAvgOverlap) across
// gaps, in float, with empty gaps taking the global mean overlap. Clean-room —
// derived from the file's own joints; bio-formats/openslide are test oracles
// only. Declines (nil → naive) unless this is a legacy slide with live joints.
func buildLegacyLayout(in StitchInput) *Layout {
	ei := in.EncodeInfo
	if in.Generation != GenerationLegacyIScan || ei == nil || len(ei.ImageInfos) == 0 {
		return nil
	}
	if !hasLiveJoint(ei) {
		return nil
	}
	cols, rows, tw, th := in.Cols, in.Rows, in.TileW, in.TileH
	resolve := func(idx int) (c, r int, ok bool) {
		c, r = serpentineToImage(idx-1, cols, rows) // legacy: 1-based serpentine
		if c < 0 {
			return 0, 0, false
		}
		return c, r, true
	}
	// Per-gap overlap sums (g = lower index of the gap) + global means.
	colSum := make([]float64, cols)
	colN := make([]int, cols)
	rowSum := make([]float64, rows)
	rowN := make([]int, rows)
	var gXs, gYs float64
	var gXn, gYn int
	for _, ii := range ei.ImageInfos {
		for _, j := range ii.Joints {
			if !j.FlagJoined || j.Confidence < legacyConfidenceCutoff {
				continue
			}
			ac, ar, aok := resolve(j.Tile1)
			bc, br, bok := resolve(j.Tile2)
			if !aok || !bok {
				continue
			}
			if ar == br && absDelta(ac, bc) == 1 { // horizontal gap
				g := min(ac, bc)
				colSum[g] += float64(j.OverlapX)
				colN[g]++
				gXs += float64(j.OverlapX)
				gXn++
			}
			if ac == bc && absDelta(ar, br) == 1 { // vertical gap
				g := min(ar, br)
				rowSum[g] += float64(j.OverlapY)
				rowN[g]++
				gYs += float64(j.OverlapY)
				gYn++
			}
		}
	}
	gX := 0.0
	if gXn > 0 {
		gX = gXs / float64(gXn)
	}
	gY := 0.0
	if gYn > 0 {
		gY = gYs / float64(gYn)
	}
	// Accumulate per-axis positions in float; round each column/row once.
	X := make([]int, cols)
	acc := 0.0
	for col := 1; col < cols; col++ {
		ov := gX
		if colN[col-1] > 0 {
			ov = colSum[col-1] / float64(colN[col-1])
		}
		acc += float64(tw) - ov
		X[col] = int(acc + 0.5)
	}
	Y := make([]int, rows)
	acc = 0.0
	for row := 1; row < rows; row++ {
		ov := gY
		if rowN[row-1] > 0 {
			ov = rowSum[row-1] / float64(rowN[row-1])
		}
		acc += float64(th) - ov
		Y[row] = int(acc + 0.5)
	}
	l := newLayout(cols, rows, tw, th)
	maxX, maxY := 0, 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			l.origin[[2]int{col, row}] = TilePlacement{Col: col, Row: row, X: X[col], Y: Y[row]}
		}
	}
	for col := 0; col < cols; col++ {
		if X[col]+tw > maxX {
			maxX = X[col] + tw
		}
	}
	for row := 0; row < rows; row++ {
		if Y[row]+th > maxY {
			maxY = Y[row] + th
		}
	}
	l.Width = maxX
	l.Height = maxY
	return l
}

// hasLiveJoint reports whether any joint is FlagJoined with confidence at or
// above the legacy cutoff (so buildLegacyLayout has overlap data to use).
func hasLiveJoint(ei *bifxml.EncodeInfo) bool {
	for _, ii := range ei.ImageInfos {
		for _, j := range ii.Joints {
			if j.FlagJoined && j.Confidence >= legacyConfidenceCutoff {
				return true
			}
		}
	}
	return false
}

// absDelta returns |a-b|.
func absDelta(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./formats/bif/ -run TestBuildLegacyLayout -v && go vet ./formats/bif/`
Expected: PASS (all 4 legacy engine tests); vet clean. Also run `go test ./formats/bif/ -run 'TestBuildDPLayout|TestVentana1'` to confirm DP path is untouched.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch.go formats/bif/stitch_legacy_test.go
git commit -m "feat(bif): buildLegacyLayout (per-gap-average separable model) + dispatch (#63)"
```

---

## Task 2: Per-join residual placement gate (fixture-gated, no decode)

**Files:**
- Test: `formats/bif/stitch_legacy_placement_test.go` (CREATE, `package bif`)

This is placement gate (a): for every live high-confidence join, the layout's relative placement of the adjacent pair must be within a tight residual of the join's own measured offset. Internal package access (needs `encodeInfo` + the level's `layout`).

- [ ] **Step 1: Write the failing test**

Create `formats/bif/stitch_legacy_placement_test.go`:

```go
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
					if ar == br && absDelta(ac, bc) == 1 { // horizontal
						// expected layout gap = tw - OverlapX; actual = |bx-ax|.
						got := absDelta(ax, bx)
						resid = append(resid, float64(absDelta(got, tw-j.OverlapX)))
					}
					if ac == bc && absDelta(ar, br) == 1 { // vertical
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
			max := resid[len(resid)-1]
			t.Logf("%s: residual n=%d p99=%.1f max=%.1f", name, len(resid), p99, max)
			if p99 > 8 {
				t.Errorf("%s: residual p99 = %.1f, want <= 8 (per-tile drift too high — averaging trap)", name, p99)
			}
			if max > 64 {
				t.Errorf("%s: residual max = %.1f, want <= 64", name, max)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it passes (engine already built)**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run TestLegacyPlacementResidual -v`
Expected: PASS on all present fixtures (skips absent ones). If a fixture exceeds the bound, that is a real finding — STOP and report (do not loosen the bound to force a pass; investigate whether that fixture needs a different cutoff or has pathological gaps).

- [ ] **Step 3: (no production change — this is a gate over Task 1's engine)**

If the test reveals a fixture failing the residual bound, return to Task 1's `buildLegacyLayout` (e.g. revisit the confidence cutoff) under systematic debugging. Otherwise proceed.

- [ ] **Step 4: vet**

Run: `go vet ./formats/bif/`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch_legacy_placement_test.go
git commit -m "test(bif): legacy per-join residual placement gate (#63)"
```

---

## Task 3: Seam-continuity pixel gate (fixture-gated, decode, sampled)

**Files:**
- Test: `formats/bif/stitch_legacy_seam_test.go` (CREATE, `package bif_test`)

Placement gate (b): decode a sample of adjacent tile pairs and verify their **overlap bands** match at the layout's computed positions (low pixel MAD), and that this is dramatically better than the naive (no-overlap) placement on the same pairs.

- [ ] **Step 1: Write the failing test**

Create `formats/bif/stitch_legacy_seam_test.go`:

```go
package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	bif "github.com/wsilabs/opentile-go/formats/bif"
)

// TestLegacySeamContinuity decodes a sample of horizontally-adjacent tile pairs
// and checks that the overlap band (the right edge of the left tile vs the left
// edge of the right tile, sized by their measured OverlapX) holds matching
// pixels — i.e. the layout placed them where the shared content actually is.
// Compares against the naive (full-tile, no-overlap) assumption to prove the
// stitch does real work.
func TestLegacySeamContinuity(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, name := range []string{"OS-1", "AC1.592", "1_19"} { // 3 with decent live fraction
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
			md, ok := bif.MetadataOf(s)
			if !ok {
				t.Fatalf("not a bif slide")
			}
			_ = md
			lvl, err := s.Level(0)
			if err != nil {
				t.Fatal(err)
			}
			// Sample horizontal interior pairs across the grid (skip edges where
			// tiles may be unscanned/blank). Decode each, measure overlap-band MAD
			// using the level's reported OverlapX (Level.TileOverlap or, if not
			// surfaced, a fixed sample overlap ~110px — see note).
			pairs := sampleHorizPairs(lvl.Grid, 40)
			var sumStitch, sumNaive float64
			var n int
			for _, p := range pairs {
				a, errA := lvl.DecodedTile(p.X, p.Y, opentile.WithFormat(decoder.PixelFormatRGB))
				b, errB := lvl.DecodedTile(p.X+1, p.Y, opentile.WithFormat(decoder.PixelFormatRGB))
				if errA != nil || errB != nil || a == nil || b == nil {
					continue
				}
				ov := overlapXForPair(s, p.X, p.Y) // from the join graph; skip if none
				if ov <= 4 || ov >= a.Width {
					continue
				}
				// stitched: a's right ov columns vs b's left ov columns.
				sumStitch += bandMAD(a, a.Width-ov, b, 0, ov)
				// naive: no overlap → compare a's right ov vs b's right ov (mismatched content).
				sumNaive += bandMAD(a, a.Width-ov, b, b.Width-ov, ov)
				n++
			}
			if n == 0 {
				t.Skipf("%s: no decodable overlapping pairs sampled", name)
			}
			meanStitch := sumStitch / float64(n)
			meanNaive := sumNaive / float64(n)
			t.Logf("%s: n=%d stitch-band MAD=%.1f naive-band MAD=%.1f", name, n, meanStitch, meanNaive)
			if meanStitch > 12 {
				t.Errorf("%s: stitched overlap-band MAD = %.1f, want <= 12 (placement likely wrong)", name, meanStitch)
			}
			if meanStitch >= meanNaive {
				t.Errorf("%s: stitched MAD (%.1f) not better than naive (%.1f) — stitch not aligning content", name, meanStitch, meanNaive)
			}
		})
	}
}
```

NOTE for the implementer: `sampleHorizPairs`, `overlapXForPair`, and `bandMAD` are helpers you must write in the same test file:
- `sampleHorizPairs(grid opentile.Size, n int) []opentile.Point` — evenly-spaced interior `(col,row)` with `col+1 < grid.W`.
- `overlapXForPair(s *opentile.Slide, col, row int) int` — the measured `OverlapX` of the live join between `(col,row)` and `(col+1,row)`. Get the joints via `bif.MetadataOf` ONLY if it exposes them; it does NOT today. **Add a minimal internal accessor** instead: in `formats/bif`, export a test-only helper through `export_test.go` (e.g. `func (t *Tiler) LiveHorizontalOverlap(col,row int) (int,bool)`) OR — simpler — make this gate live in `package bif` (like Task 2) where it can read `encodeInfo` directly, and decode via `internal/jpegturbo`/the registered decoder. **Decision:** put the seam gate in `package bif` (internal) to reach the joints, and decode tiles by calling the level's existing decode path. Re-confirm the available internal decode entry (e.g. `l0` has `Tile` → decode via `decoder` package) when implementing; adapt the decode calls accordingly. If internal decode is awkward, keep `package bif_test` and add the `export_test.go` accessor for the per-pair overlap.
- `bandMAD(a *decoder.Image, ax int, b *decoder.Image, bx int, w int) float64` — mean abs diff over a `w`-wide, `min(a.Height,b.Height)`-tall band at column `ax` of `a` vs column `bx` of `b`, averaged over RGB.

The implementer chooses the package placement that compiles cleanly (internal `package bif` is recommended so the joints are reachable without new exported API). Pin the MAD threshold empirically: decode a couple of known-good interior pairs first, observe the stitched-band MAD, set the bound ~2× that (the `<= 12` above is a starting estimate; adjust to the measured value and record it in a comment).

- [ ] **Step 2: Run to verify behavior**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run TestLegacySeam -v`
Expected: PASS — stitched-band MAD small and well below naive-band MAD. If stitched ≈ naive or stitched is large, placement is wrong → STOP and investigate (systematic debugging on Task 1). Record the observed MAD numbers in the test log.

- [ ] **Step 3: (no production change unless the gate fails)**

- [ ] **Step 4: vet + race**

Run: `go vet ./formats/bif/` and `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run TestLegacySeam -race`
Expected: clean / pass.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch_legacy_seam_test.go   # (+ export_test.go if added)
git commit -m "test(bif): legacy seam-continuity pixel gate (#63)"
```

---

## Task 4: Dimensions cross-check + replace the naive lock test

**Files:**
- Test: `formats/bif/stitch_legacy_dims_test.go` (CREATE, `package bif_test`)
- Modify: `formats/bif/stitch_legacy_lock_test.go` (DELETE `TestOS1LegacyNaiveDims` — superseded)

- [ ] **Step 1: Write the failing test**

Create `formats/bif/stitch_legacy_dims_test.go`:

```go
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
// Y baseline.
func TestLegacyDimsVsOpenslide(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	type want struct {
		osW, osH         int
		naiveW, naiveH   int // for the better-than-naive assertion
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
			// Dramatically better than naive: error must be < 10% of the naive error.
			naiveErrW := w.naiveW - w.osW
			if naiveErrW > 0 && abs(dW)*10 > naiveErrW {
				t.Errorf("%s width error %d not <10%% of naive error %d", name, abs(dW), naiveErrW)
			}
		})
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run TestLegacyDimsVsOpenslide -v`
Expected: PASS on all present fixtures.

- [ ] **Step 3: Delete the superseded naive lock test**

In `formats/bif/stitch_legacy_lock_test.go`, remove `TestOS1LegacyNaiveDims` (it asserts the now-wrong naive 118784×102000). If that file then has no remaining tests, delete the file. The new `TestLegacyDimsVsOpenslide` (OS-1 case) is the replacement lock.

- [ ] **Step 4: Run to confirm no dangling references**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run 'TestLegacy|TestOS1' -v && go vet ./formats/bif/`
Expected: PASS; `TestOS1LegacyNaiveDims` no longer present.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch_legacy_dims_test.go formats/bif/stitch_legacy_lock_test.go
git commit -m "test(bif): legacy dims cross-check vs openslide; retire naive lock (#63)"
```

---

## Task 5: Docs, CHANGELOG, migration update

**Files:**
- Modify: `docs/formats/bif.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/migrations/2026-06-18-bif-level-size-stitched.md`

- [ ] **Step 1: Update `docs/formats/bif.md`**

Replace the "legacy not stitched" paragraph (the one referencing #63 and `TestOS1LegacyNaiveDims`) with: legacy iScan BIF **is now stitched** via per-gap-average overlap reconstruction from the `TileJointInfo` graph (no `<Frame>` nodes); near-exact vs openslide (the all-4 oracle; bio-formats crashes on 3/4); width clean-room-exact, height ~0.05% (the per-column `columnYAdjust` Y baseline is not replicated — documented residual); validated by placement-fidelity gates (per-join residual + seam continuity), not dims alone. Keep the clean-room note (whitepaper + file joints; bio-formats/openslide oracles only).

- [ ] **Step 2: Update `CHANGELOG.md` `[Unreleased]`**

Add: `Added` — legacy iScan BIF overlap-aware stitching (per-gap-average separable reconstruction; #63). `Changed` — legacy BIF L0 `Level.Size` now reports the stitched content extent (was the naive raw-frame extent); `ReadRegion`/`ScaledStrips` now produce stitched legacy output. Note the height residual + multi-AOI untested as documented limitations.

- [ ] **Step 3: Update the migration note**

In `docs/migrations/2026-06-18-bif-level-size-stitched.md`, extend the "legacy unchanged" note to: legacy iScan L0 `Level.Size` ALSO changed (now stitched, near-exact) — consumers re-validate cached legacy dims too. Cite #63.

- [ ] **Step 4: Verify the full gate**

Run: `go test ./... -race -count=1` (default build, CI-safe — legacy fixture tests skip) and `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run 'Legacy|Ventana1|BIF' -v` (local full legacy run). `make vet`.
Expected: green; DP golden + `TestBIFTilePlacementSpatial` + non-BIF tests unchanged; all legacy gates pass locally.

- [ ] **Step 5: Commit**

```bash
git add docs/formats/bif.md CHANGELOG.md docs/migrations/2026-06-18-bif-level-size-stitched.md
git commit -m "docs(bif): legacy stitching — docs, CHANGELOG, migration (#63)"
```

---

## Final verification

- [ ] `make test` green under `-race` (default build; legacy fixture tests skip in CI).
- [ ] Locally with fixtures: `TestBuildLegacyLayout*` (CI-safe units), `TestLegacyPlacementResidual` (4 fixtures), `TestLegacySeamContinuity` (sampled), `TestLegacyDimsVsOpenslide` (4 fixtures) all pass.
- [ ] DP path untouched: `TestVentana1DPExactDimensions`, `TestBIFTilePlacementSpatial`, tifffile parity green.
- [ ] Non-BIF formats unchanged: `go test . -run Region` green.
- [ ] `make vet` clean.
- [ ] Final whole-implementation review (subagent-driven final reviewer) before finishing the branch.

---

## Self-review notes (writing-plans skill)

- **Spec coverage:** §3 algorithm → Task 1 (`buildLegacyLayout` + dispatch + units). §4 integration → Task 1 (dispatch; `Level.Size`/regionLayout reuse is automatic via PR #64 wiring — no new code, confirmed in Task 5's regression run). §5.1 placement gates → Tasks 2 (residual) + 3 (seam). §5.2 dims cross-check + §5.4 lock → Task 4. §5.3 engine units → Task 1. §5.5 regression → Task 5 + Final. Docs/§7 → Task 5.
- **Type consistency:** `buildLegacyLayout`/`hasLiveJoint`/`absDelta`/`legacyConfidenceCutoff` defined in Task 1, used in Tasks 2/3. `serpentineToImage(idx-1, …)` (1-based) consistent across Tasks 1–3. `*Layout`/`TileOrigin`/`TilePlacement` are the existing PR #64 types. `openLegacyTiler`/`legacyFixtures` defined in Task 2 (`package bif`); Task 3's `package bif_test` defines its own helpers (different package — no shared symbol, intentional).
- **Known soft spots flagged for the executor (not placeholders):** Task 3's seam helpers + package placement (internal `package bif` recommended to reach joints) and the MAD threshold are to be pinned empirically against decoded pairs during execution — the gate's *logic* (stitched ≪ naive band MAD) is fixed; only the numeric threshold is measured. This is the one genuinely fixture-dependent constant; everything else is concrete.
