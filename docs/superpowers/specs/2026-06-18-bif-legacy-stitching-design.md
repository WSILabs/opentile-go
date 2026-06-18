# BIF legacy (iScan) overlap-aware stitching — design

**Status:** design / approved-to-plan
**Issues:** [#63](https://github.com/WSILabs/opentile-go/issues/63) (clean-room placement characterization), [#60](https://github.com/WSILabs/opentile-go/issues/60) (L0 width)
**Builds on:** PR #64 (DP-generation stitching) — `docs/superpowers/specs/2026-06-18-bif-overlap-stitching-design.md`
**Date:** 2026-06-18

---

## 0. Background

PR #64 made **DP-generation** BIF (ScannerModel `"VENTANA DP*"`) stitched output
pixel-exact, but **legacy iScan** BIF (Coreo/HT — `OS-1.bif`, `S12-18199-1A.bif`,
`AC1.592.bif`, `1_19.bif`) is deliberately gated off the stitch engine: it falls
to the naive regular-grid layout, so `Level.Size` over-states the real content
by the cumulative tile overlap (OS-1: naive 118784×102000 vs bio-formats
105817×93978 — **+12967 / +8022 px**, ~11% / ~8% too large) and `ReadRegion` /
`ScaledStrips` show seam artifacts.

Legacy differs structurally from DP (per [#63](https://github.com/WSILabs/opentile-go/issues/63),
a clean-room characterization derived purely from observing `<EncodeInfo>` bytes
in the 5 fixtures):

- **No `<Frame>` nodes.** DP files carry explicit per-tile grid positions;
  legacy files do not. Placement must come from the `<TileJointInfo>` stitch
  graph alone.
- **Substantial, per-file overlap** (~110px on OS-1, ~73px on the others) — not
  the ~0–24px of DP. There is no canonical legacy overlap constant.
- **Fragmented, partially-dead graph.** OS-1: 17209 joints, **7035 live /
  10174 dead** (`FlagJoined=0`), confidence 84–100. Live-join fraction across
  fixtures swings 11%→100%.

### Phase-0 empirical study (this design's foundation)

A throwaway alignment study (`formats/bif`, against the real OS-1 fixture, with
bio-formats `showinf` as a black-box dims oracle) tested candidate models:

| model | OS-1 dims | Δ vs bio-formats (105817×93978) |
|---|---|---|
| naive (current behavior) | 118784×102000 | **+12967 / +8022** |
| single-root spanning-tree propagation | ≈118784×102000 | +12967 / +8022 (only ~456/8700 tiles reachable — graph too sparse to traverse) |
| per-column-gap average overlap (int) | 106206×93957 | +389 / −21 |
| per-gap average **+ global-mean fill for empty gaps** (int) | 105870×93957 | **+53 / −21** |
| per-gap average + global-fill (**float**, round once) | 105818×93924 | **+1 / −54** |
| **bio-formats** | **105817×93978** | — |

**Conclusions that fix the architecture:**

1. **Graph propagation is the wrong model.** The live-join graph is too
   fragmented to traverse from a root (≈5% reachable on OS-1); a position graph
   cannot be built. The usable signal is the *aggregate per-gap overlap
   statistic*, not a traversable graph.
2. **Per-column-gap average overlap, accumulated, reproduces width clean-room
   to ±1px** (float, with empty gaps filled by the global mean). This is
   derived entirely from the file's own joint overlaps — no GPL code.
3. **Height is the irreducible residual** (~20–54px ≈ 0.05% on a 94000px axis).
   bio-formats reaches its exact height via a per-column Y term (`columnYAdjust`
   + max-over-columns) that is heuristic-shaped; replicating it bit-exactly
   approaches porting the GPL behavior, which is out of scope.

The agreed correctness bar (decision record below) is therefore **near-exact,
clean-room**: width exact to ±1px, height to ≤0.1%, far better than naive.

---

## 1. Goals / non-goals

### Goals

1. **Legacy BIF stitched output is near-exact vs bio-formats**, clean-room:
   `Level.Size` (L0), `ReadRegion`, `ReadRegionScaled`, `ScaledStrips` reflect
   the compacted (stitched) geometry instead of the naive raw-frame extent.
   Bar: width within ±~5px, height within ≤0.1% of bio-formats, and
   dramatically better than naive, across **all 4 legacy fixtures**.
2. **Reuse the PR #64 machinery.** The legacy path emits the same `*Layout`,
   so the `regionLayout` capability, the layout-aware `ReadRegion` branch, and
   the layout-aware `ScaledStrips` all work unchanged.
3. **Per-tile raw/decoded bytes unchanged**; the DP path and the 10 non-BIF
   formats are byte-identical.

### Non-goals

- **Bit-exact height.** The ~0.05% height residual (bio-formats' per-column
  `columnYAdjust`) is deliberately not replicated — it is the GPL-shaped
  heuristic the chosen bar excludes. Documented, not chased.
- **Per-tile pixel parity with bio-formats** (a DP-style pixel oracle). The
  Y-axis variance (#63: per-row σ 7–70× the X-axis; mid-column divergence up to
  187px) means per-tile Y cannot be hit clean-room. Legacy is validated on
  **dimensions**, not per-pixel.
- **Multi-AOI legacy.** All 4 fixtures are single-AOI; the `AoiOrigin` path is
  applied defensively but cannot be validated against a real file.
- Changing DP behavior or per-tile addressing.

---

## 2. Licensing / clean-room

Same hard constraint as PR #64. The legacy algorithm is derived from (a) the
BIF file's own `<TileJointInfo>` overlap statistics and (b) the clean-room
characterization in #63 — **not** from `bio-formats` `VentanaReader.java` (GPL)
or `openslide` (LGPL). bio-formats is used **only as a black-box dimensions
oracle** in tests (run `showinf`, read the reported series dims; never read or
translate its source). The deliberately-unreplicated `columnYAdjust` behavior
is named only to document *why* height is approximate.

---

## 3. The algorithm — `buildLegacyLayout`

A **separable per-axis** model: tile `(col, row)` lands at `(X[col], Y[row])`,
where `X[]`/`Y[]` are computed independently from the column-gap / row-gap
overlap statistics. (Phase 0 proved the live-join graph is non-traversable, so
no per-tile graph positions; the separable model is both correct-to-bar and far
simpler.)

### 3.1 Resolve joints to grid positions

Legacy files have no `<Frame>` nodes, so map each joint's `Tile1`/`Tile2`
(physical serpentine indices, **1-based** — validated: 17209/17209 OS-1 joints
resolve in-grid at base-1) to image `(col, row)` via the existing
`serpentineToImage(idx-1, cols, rows)`. `cols, rows` come from the IFD
`TileGrid()`. Drop joints that resolve out of grid.

### 3.2 Per-gap overlap accumulation (each axis)

For the X axis:

```
colSum[g], colN[g] = 0            // g = column-gap index 0..cols-2
for each joint j:
    if !j.FlagJoined or j.Confidence < cutoff: continue
    resolve a=(ac,ar), b=(bc,br)  // skip if either out of grid
    if ar == br and |ac-bc| == 1: // horizontal adjacency → column gap
        g = min(ac,bc); colSum[g] += j.OverlapX; colN[g]++

globalMeanX = mean of all qualifying OverlapX        // for empty-gap fill
X[0] = 0
for col in 1..cols-1:
    g = col-1
    ovX = colN[g]>0 ? colSum[g]/colN[g] : globalMeanX  // FLOAT mean
    X[col] = X[col-1] + (tileW - ovX)                  // accumulate in float
// round X[] once at the end
width = round(X[cols-1]) + tileW
```

Y axis is symmetric over **vertical** adjacencies (`ac==bc && |ar-br|==1`),
`OverlapY`, `tileH`, `globalMeanY`.

Key details (all Phase-0-validated):
- **Float accumulation, round once** — integer per-gap division accumulated
  over ~115 gaps drifts ~50px (the +389→+53→+1 progression).
- **Empty-gap global-mean fill** — gaps with no qualifying join take the global
  mean overlap, not 0 (closed +389→+53 on OS-1: 3 empty column-gaps × ~112px).
- **Confidence cutoff** — pinned constant (default proposal **98**; Phase 0:
  98 vs 84 move dims ≤~12px, so the exact value is non-critical; justify from
  the cross-fixture data in the plan).
- **Per-file** — every statistic is computed from *this file's* joints; no
  hardcoded overlap.

### 3.3 Emit the Layout

Build the same `*Layout` as the DP path: for every in-grid `(col,row)`, a
`TilePlacement{Col, Row, X: X[col], Y: Y[row]}`; `Width`/`Height` as above.
(No hull-normalization needed for single-AOI since `X[0]=Y[0]=0`; the
`AoiOrigin` offset + normalization from the DP path is reused if/when multi-AOI
appears.) The legacy layout populates placements for **all** grid `(col,row)`
(unlike DP, where the frame set may be a sub-grid) — legacy tiles are stored
row-major over the full grid.

### 3.4 Gating

`BuildLayout` selects `buildLegacyLayout` when:
`Generation == GenerationLegacyIScan` **and** `EncodeInfo != nil` **and** there
is ≥1 live joint. Otherwise → naive (unchanged). DP slides
(`GenerationSpecCompliant`) keep `buildDPLayout`. Pyramid levels ≥1 carry no
joints → naive (they are pre-stitched, like DP).

---

## 4. Integration

- **`BuildLayout` dispatch** (`formats/bif/stitch.go`): add the legacy branch
  before the naive fallback. DP branch untouched.
- **`Level.Size` (L0)** becomes the legacy stitched extent — same wiring as PR
  #64 (`newLevelImpl` already sets L0 `size` from `layout.Width/Height`; legacy
  now produces a non-naive layout so the value changes automatically). Levels
  ≥1 keep their IFD extent. `Grid` stays the raw frame grid.
- **`regionLayout` reuse**: the legacy `*Layout` flows through the existing
  `TileOrigin`/`TilesIntersecting`/`StitchedSize` accessors, so `ReadRegion` /
  `ReadRegionScaled` / `ScaledStrips` composite legacy stitched output with no
  new code. Per-tile decode addressing: legacy storage is row-major (#57), so
  `indexOf(col,row)` aligns with the layout's `(col,row)` keys.
- **Validate**: the `tile-grid-mismatch` cover-not-equal relaxation from PR #64
  already accommodates a stitched Size smaller than `Grid × Tile`.

No public API change. The only externally-visible effect is legacy BIF L0
`Level.Size` (and the region/scaled outputs) now reporting/producing the
stitched geometry.

---

## 5. Correctness bar & testing

### 5.1 Dimensions oracle (the headline gate)

For each of the 4 legacy fixtures, pin bio-formats' L0 dims (from `showinf`,
captured once) and assert the reader's L0 `Level.Size` is within tolerance:
**width ±5px, height ±0.1%** (and assert it is `< 0.5 ×` the naive-vs-bioformats
error, i.e. dramatically better than naive). Fixture-gated (local PHI; skips in
CI). OS-1 target: 105817×93978.

The plan must first **capture each fixture's bio-formats dims** (the other 3
are not yet measured — only OS-1). If any fixture's near-exact model lands
outside tolerance, that's a finding to resolve before shipping (the bar is
"near-exact on all 4", not "OS-1 only").

### 5.2 Fixture-free engine unit tests (CI-safe)

Synthetic joint sets exercising the pure math: uniform per-gap overlap (known
accumulation), empty-gap global-fill, dead-join (`FlagJoined=0`) exclusion,
sub-cutoff confidence exclusion, float-accumulation rounding, single-row /
single-column degenerate grids. These pin the algorithm without fixtures.

### 5.3 Lock-test update

`TestOS1LegacyNaiveDims` (currently asserts the naive 118784×102000) is
replaced by the near-exact assertion (≈105818×93924, within tolerance of
bio-formats). Rename to reflect it now locks the *stitched* legacy extent.

### 5.4 Regression

`make test` green under `-race`; DP golden (`TestVentana1DPExactDimensions`),
`TestBIFTilePlacementSpatial`, tifffile parity, and non-BIF region/scaled tests
all unchanged. The Ventana-1 parity fixture is untouched (DP path unchanged).
Legacy fixtures have no committed parity fixtures (local PHI).

---

## 6. Open questions (settle in the plan or inline)

- **Q1 — confidence cutoff value.** Proposal: 98. Decide from the cross-fixture
  dims sweep (the value that minimizes max error across all 4). Phase 0:
  non-critical (≤12px swing on OS-1).
- **Q2 — tolerance constants.** Proposal: width ±5px, height ±0.1%. Tighten per
  the measured cross-fixture residuals.
- **Q3 — empty-gap fill source.** Global mean (chosen, Phase-0-validated).
  Alternative (nearest non-empty gap) deferred unless a fixture needs it.
- **Q4 — float vs fixed rounding convention.** `round(accumulate_float)` chosen
  (gave +1px width). Confirm the rounding (round-half-up vs trunc) that best
  matches across fixtures.
- **Q5 — horizontal joins' OverlapY (and vice versa).** The separable model
  uses only OverlapX for column gaps and OverlapY for row gaps. Phase 0 shows
  this suffices for dims; the cross-coupling (stage drift) is what bit-exact
  per-tile would need and is out of scope.

---

## 7. Summary of changes

| Area | Change | Breaking? |
|---|---|---|
| `formats/bif/stitch.go` | `buildLegacyLayout` (per-gap-average separable model) + `BuildLayout` dispatch | additive |
| `formats/bif` | legacy L0 `Level.Size` = stitched extent (was naive) | **value change** (documented; consumers re-validate) |
| tests | dims oracle ×4 fixtures, engine unit tests, lock-test update | — |
| docs | `bif.md` legacy section (now stitched, near-exact, residual documented); CHANGELOG; migration note update | — |

Per-tile bytes, DP path, non-BIF formats: **unchanged.** Legacy stitched
output: **near-exact vs bio-formats (width ±1px, height ≤0.05%), clean-room.**
The height residual and multi-AOI are documented limitations, not silent caps.
