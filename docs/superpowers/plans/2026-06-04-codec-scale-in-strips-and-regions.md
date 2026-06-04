# Codec-domain Scale in ScaledStrips + ReadRegionScaled — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Fix the broken `idctScale>1` strip geometry, then let `ScaledStrips` and `ReadRegionScaled` use codec-domain `Scale` for JP2K/HTJ2K (not just JPEG), for faster + anti-aliased downsampling.

**Architecture:** The strip iterator already decodes tiles at `WithScale(idctScale)` but computes the intermediate + blit in **full-level** coordinates, so scaled tiles get squished/gapped (confirmed: strip scale-2 diverges from `ReadRegionScaled` by mean 8.26 / max 229; scale-1 matches at mean 0.043). Fix = run the strip geometry in an **effective coarser level** space (`Downsample*s`, `ceil(Size/s)`, `ceil(TileSize/s)`) so the existing s=1 math applies to the scaled tiles. Then generalize the codec gate and reuse the (now-correct) machinery for `ReadRegionScaled`.

**Tech Stack:** Go, the existing `resample` + strip machinery; `DecodeOptions.Scale` (JPEG/JP2K/HTJ2K).

**Spec:** `docs/superpowers/specs/2026-06-04-codec-scale-in-strips-and-regions-design.md` (updated: Finding 1 is a *bug fix*, not a gate change).

---

## Task 1: Fix the broken scaled-strip geometry (correctness)

The bug: `strip_iterator.go` computes `cx0/cx1/cy0/cy1`, `tileLevelX`, and the intermediate size in full-level coordinates while tiles decode to `ceil(TileSize/s)`. Run the geometry on an effective level that is `s×` coarser.

**Files:**
- Modify: `strip_iterator.go` (region computation ~176-218, blit ~245-255), `strip_workers.go` (`tilesForStrip` uses level dims), `strip_geometry.go`
- Test: `strip_scale_test.go` (create)

- [ ] **Step 1: Write the failing regression test.** It asserts strip output at `idctScale=2` matches `idctScale=1` (and `ReadRegionScaled`) within resampling tolerance.

```go
package opentile

import (
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func cmu1(t *testing.T) string {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "svs", "CMU-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return p
}

func assembleStrips(t *testing.T, s *Slide, l0 image.Rectangle, out image.Point, scale int) *decoder.Image {
	it := s.ScaledStrips(l0, out, 64, WithStripIDCTScale(scale))
	defer it.Close()
	full := decoder.NewImage(out.X, out.Y)
	y := 0
	for {
		strip, err := it.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		for r := 0; r < strip.Height; r++ {
			copy(full.Pix[(y+r)*full.Stride:(y+r)*full.Stride+strip.Width*3],
				strip.Pix[r*strip.Stride:r*strip.Stride+strip.Width*3])
		}
		y += strip.Height
	}
	return full
}

func meanAbsDiff(a, b *decoder.Image) float64 {
	var sum, n int
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
		n++
	}
	return float64(sum) / float64(n)
}

func TestStripIDCTScaleCorrect(t *testing.T) {
	s, err := OpenFile(cmu1(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l0 := image.Rect(0, 0, 4096, 4096)
	out := image.Pt(512, 512) // 8x; auto-picks idctScale=2 on the 4x level
	s1 := assembleStrips(t, s, l0, out, 1)
	s2 := assembleStrips(t, s, l0, out, 2)
	if m := meanAbsDiff(s1, s2); m > 2 {
		t.Errorf("strip scale-2 vs scale-1 mean abs diff = %.3f, want <= 2 (scaled blit geometry broken)", m)
	}
}
```

- [ ] **Step 2: Run, verify it fails.**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run TestStripIDCTScaleCorrect -v`
Expected: FAIL — mean ≈ 8.26 (the bug).

- [ ] **Step 3: Implement the effective-coarser-level geometry.** In `strip_iterator.go`, after `it.idctScale` is finalized (~line 82), precompute effective level geometry used by the per-strip region math and `tilesForStrip`:

```go
	// Effective (codec-scaled) source geometry: when idctScale = s, decoded
	// tiles are ceil(TileSize/s) and the assembled intermediate lives at the
	// level resolution divided by s. Run all strip geometry on this virtual
	// s-times-coarser level so the unscaled math stays correct.
	s := it.idctScale
	it.effDownsample = level.Downsample * float64(s)
	it.effLevelW = (level.Size.W + s - 1) / s
	it.effLevelH = (level.Size.H + s - 1) / s
	it.effTileW = (level.TileSize.W + s - 1) / s
	it.effTileH = (level.TileSize.H + s - 1) / s
```

Add those fields to the `StripIterator` struct. Then replace every use of `it.sourceLevel.Downsample` / `.Size.W/H` / `.TileSize.W/H` in the per-strip region computation (`strip_iterator.go` ~177-218) and in `tilesForStrip` (`strip_workers.go` ~98-134) with the `eff*` values. The tile *indices* (`tx,ty`) are unchanged (tiles are still indexed on the full grid; `tx*it.effTileW` is the scaled position). The blit at ~245-255 uses `tileLevelX := tx * it.effTileW` and clips against the effective region. Show each edit fully when implementing.

- [ ] **Step 4: Run, verify it passes** (and the other strip tests stay green).

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run 'TestStrip|TestScaledStrips' -race -v`
Expected: PASS, mean ≤ 2.

- [ ] **Step 5: Commit.**

```bash
git add strip_iterator.go strip_workers.go strip_geometry.go strip_scale_test.go
git commit -m "fix(strips): correct idctScale>1 geometry (was squishing scaled tiles)"
```

---

## Task 2: Generalize the codec-scale gate to JP2K/HTJ2K

**Files:**
- Modify: `strip_geometry.go` (`autoIDCTScale`), `strip_iterator.go`/`strip_options.go` (rename `idctScale`→`codecScale` for honesty)
- Test: `strip_scale_test.go`

- [ ] **Step 1: Write the failing test** — a JP2K source (`JP2K-33003-1.svs`) should auto-select a codec scale > 1 at a between-level target and match the spatial reference.

```go
func TestStripCodecScaleJP2K(t *testing.T) {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "svs", "JP2K-33003-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	s, err := OpenFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lv := s.Levels()
	l0 := image.Rect(0, 0, lv[0].Size.W, lv[0].Size.H)
	out := image.Pt(lv[0].Size.W/8, lv[0].Size.H/8)
	withScale := assembleStrips(t, s, l0, out, 0) // 0 = auto codec scale
	withoutScale := assembleStrips(t, s, l0, out, 1)
	if m := meanAbsDiff(withScale, withoutScale); m > 3 {
		t.Errorf("JP2K auto codecScale vs scale-1 mean diff = %.3f, want close", m)
	}
}
```

- [ ] **Step 2: Run, verify it passes trivially OR fails.** Currently `autoIDCTScale` returns 1 for JP2K, so `withScale` == `withoutScale` → the test passes but doesn't prove codec scale engaged. Strengthen: assert the iterator actually selected scale > 1 (add a test accessor `it.codecScale` or check via a debug hook). Run and confirm the *current* behavior is scale=1 (no codec downscale) before the change.

- [ ] **Step 3: Implement.** Replace the gate in `autoIDCTScale` (`strip_geometry.go:52`):

```go
func scaleCapable(c Compression) bool {
	switch c {
	case CompressionJPEG, CompressionJP2K, CompressionHTJ2K:
		return true
	default:
		return false
	}
}
```
and change `if level.Compression != CompressionJPEG { return 1 }` to `if !scaleCapable(level.Compression) { return 1 }`. Rename `idctScale`→`codecScale` across `strip_iterator.go`, `strip_workers.go`, `strip_geometry.go`, `strip_options.go`; keep `WithStripIDCTScale` as the public name (a deprecated alias is unnecessary — additive `WithStripCodecScale` optional) to avoid an API break.

- [ ] **Step 4: Run, verify pass** (JP2K codec scale engages + matches reference; the Task 1 JPEG test still green).

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run TestStrip -race -v`

- [ ] **Step 5: Commit.**

```bash
git add strip_geometry.go strip_iterator.go strip_workers.go strip_options.go strip_scale_test.go
git commit -m "feat(strips): codec-domain Scale for JP2K/HTJ2K sources (generalize gate)"
```

---

## Task 3: ReadRegionScaled codec-domain scale

**Files:**
- Modify: `region_scaled.go`
- Test: `region_scaled_test.go` (add)

- [ ] **Step 1: Write the failing test** — `ReadRegionScaled` on a JP2K source should match a forced-scale-1 reference within tolerance AND be no slower (a benchmark). The simplest correctness gate: the output is unchanged vs today (codec scale is an internal optimization), so assert `ReadRegionScaled` output for a between-level target stays equal (within tolerance) to the pre-change spatial-only result captured as a golden — OR assert it equals the strip path's output for the same region.

```go
func TestReadRegionScaledCodecScale(t *testing.T) {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "svs", "JP2K-33003-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	s, err := OpenFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lv := s.Levels()
	w, h := lv[0].Size.W, lv[0].Size.H
	got, err := s.ReadRegionScaled(0, 0, w, h, w/8, h/8)
	if err != nil {
		t.Fatal(err)
	}
	// Reference: the strip path (Task 1/2, verified) over the same region.
	ref := assembleStrips(t, s, image.Rect(0, 0, w, h), image.Pt(w/8, h/8), 1)
	if m := meanAbsDiff(got, ref); m > 3 {
		t.Errorf("ReadRegionScaled vs strip reference mean diff = %.3f", m)
	}
}
```

- [ ] **Step 2: Run.** It likely passes already (ReadRegionScaled is correct, just slow). Confirm baseline.

- [ ] **Step 3: Implement codec scale in `ImageReadRegionScaled`.** Between the chosen `level` read and the final resample, pick a `codecScale` for the residual (level→output) via the same `autoIDCTScale` logic, and pass `WithScale(codecScale)` into the `ImageReadRegion` call — but only if the level is `scaleCapable`. The region read must then assemble scaled tiles, which `imageReadRegionImpl` does NOT do today. **Two implementation options — pick the simpler:**
  - **(a) Route through the strip iterator:** call `ScaledStrips(l0Rect, outSize, outSize.Y, ...)` (one strip = the whole region) and take the single strip. Reuses all the now-correct scaled-tile machinery; minimal new code.
  - **(b) New scaled-region assembly** mirroring the strip blit.

  Recommend **(a)** for DRY. Implement `ImageReadRegionScaled` to delegate to a one-strip `ScaledStrips` when the level is scale-capable and the residual ≥ 2; otherwise keep the current path. Show the full delegation when implementing, preserving the white-fill / `ErrRegionEmpty` semantics.

- [ ] **Step 4: Run, verify pass** + the existing region-scaled tests stay green.

- [ ] **Step 5: Commit.**

```bash
git add region_scaled.go region_scaled_test.go
git commit -m "feat(region): codec-domain Scale in ReadRegionScaled (via strip machinery)"
```

---

## Task 4: Doc fix + full gate

**Files:**
- Modify: `decode_options.go` (WithScale doc), `strip_options.go` (WithStripIDCTScale doc), `CHANGELOG.md`

- [ ] **Step 1: Fix the stale `WithScale` doc** (`decode_options.go:40`): replace "(JPEG decoders only)" / "Non-JPEG sources return ErrUnsupportedScale" with: honored by jpeg (IDCT), jpeg2000 + htj2k (DWT resolution), `{1,2,4,8}`, else `ErrUnsupportedScale`. Update `WithStripIDCTScale` doc similarly (no longer JPEG-only).

- [ ] **Step 2: CHANGELOG `[Unreleased]`:** two entries — **Fixed:** `ScaledStrips`/`ReadRegionScaled` corrupted output at between-level downsamples (`idctScale>1` blitted scaled tiles at full-level coordinates); **Added:** codec-domain Scale now used for JP2K/HTJ2K strip + region downsampling, not just JPEG.

- [ ] **Step 3: Full gate.**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
go vet ./...
CGO_ENABLED=0 go build ./...
```
Expected: green.

- [ ] **Step 4: Commit.**

```bash
git add decode_options.go strip_options.go CHANGELOG.md
git commit -m "docs: WithScale honored by jpeg2000/htj2k; CHANGELOG (strip scale fix + codec scale)"
```

---

## Self-Review

- **Spec coverage:** Finding 1 (now a bug fix) → Tasks 1+2. Finding 2 (ReadRegionScaled) → Task 3. Finding 3 (doc) → Task 4. The discovered shipped bug → Task 1 (the new headline).
- **Placeholders:** Task 1 Step 3 and Task 3 Step 3 describe the edits with the exact effective-geometry transform and the delegation strategy but defer the line-by-line edits to implementation (the regression test in each is the gate) — this is direction with the mechanism specified, not a vague placeholder, because the change touches many coordinate sites that are clearer to edit against a failing test than to transcribe blind.
- **Type consistency:** `assembleStrips`/`meanAbsDiff`/`cmu1` helpers shared across tasks; `codecScale`/`scaleCapable`/`eff*` fields consistent; `WithStripIDCTScale` kept as the public name.
