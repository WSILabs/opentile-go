# True-Content-Extent Pyramid (`Level.Size`/`Downsample`) — Design

**Status:** Draft for review (no code written)
**Date:** 2026-06-24
**Closes (partial):** #78 — for DP-BIF and IFE. Legacy-BIF is deferred to #80
(coupled to per-level stitching); the IFE Magnification/MPP-from-wrong-level bug
is separate (#81).

---

## Goal

Make `Level.Size` report the **true content extent** at every pyramid level for
the two formats that currently report a tile-grid-padded (or overlap-compacted-
only-at-L0) extent — **DP-generation BIF** and **IFE** — so the pyramid's
inter-level scale is exactly ~2× and a consumer can derive
`downsample = Size[0]/Size[i]` and traverse levels without per-level offset/scale
correction. `Downsample` is corrected in lockstep. Pixels still come from the
stored tiles; only the *geometry* is corrected.

## Background

`Level.Size` is documented as "the pixel dimensions of this level." For 10 of the
12 formats it already equals the true content extent (TIFF readers source it from
the per-level IFD `ImageWidth/Length`, which is content; tiles pad internally).
**BIF and IFE are the two deviations**, each sourcing `Size` from a padded/derived
grid extent instead:

- **BIF** — its per-level IFD `ImageWidth` is the *padded frame grid*
  (Ventana-1 L1 = `12×1024 = 12288`, not the `11716` content). v0.46 overrides
  this with the derived stitched hull **only at L0**; L1+ fall through to the
  padded tag. Result: the L0→L1 ratio is off by the overlap fraction (Ventana-1
  `23432/12288 = 1.907`), and the pyramid mixes two semantics (L0 content,
  L1+ grid) — an internal self-contradiction.
- **IFE** — no TIFF IFDs; the reader *computes* `Size = XTiles·256` (the grid)
  per level, and the ceil-to-tile padding doesn't halve cleanly, so ratios drift
  off 2× at coarse levels (e.g. a level reporting `512` whose content is `~346`).

The unifying root: **`Size` sourced from a padded extent, not true content.**

### Downstream effect

A consumer that builds a pyramid from `Size` (the natural thing) gets a
registration error that grows toward the bottom-right when crossing levels
(top-left stays anchored). Observed in openscope: Ventana-1 shifts horizontally
at the 20×→40× (L1→L0) step; IFE drifts at coarse levels. Per-tile pixel output is
correct — only cross-level `Size` math is wrong.

### Phase-0 oracle probe (what decided this design)

- **Ground truth = `hull/2^i` content extent**, confirmed by **both** reference
  readers: bio-formats `sizeX` for Ventana-1 DP (`23432→11716→5858→…`, floor-
  halving) and **both** bio-formats and openslide for OS-1 legacy
  (`105817/105813 → 52908/52907 → …`, clean 2.0). The other 10 formats already
  do this — so this is the library's own convention, not a new definition.
- **Both oracles fully composite every level internally** and expose only a
  content-extent pyramid with exact downsamples; the consumer never sees a frame
  grid or overlap at any level. That is the target behavior.
- **Rounding differs between the references** (bio-formats floors, openslide
  rounds/ceils — ≤1px at odd levels). "Within rounding" is literally how the two
  oracles differ from each other.
- **Legacy-BIF L1+ tiles genuinely overlap** — pixel-confirmed: an adjacent-tile
  match-offset sweep found a near-exact overlap band at OS-1 L1 (~55px, min
  MAD 4.82) with controls validating the method (OS-1 L0 positive: ~115px,
  MAD 6.25; Ventana-1 L1 negative: none). So legacy reduced levels preserve the
  overlapping frame structure and need per-level stitching (#80) — they are
  **out of scope here** (a Size-only change would give the right ratio but
  doubled/shifted pixels). DP-BIF L1+ do **not** overlap (the 1-column L0 overlap
  vanishes at half-res), and IFE is non-overlapping — both are safe to fix with
  geometry only.

## Scope

In: **DP-generation BIF** (`Size`/`Downsample` for L1+) and **IFE** (all levels).
Out: legacy-BIF (→ #80), IFE Magnification/MPP (→ #81), any non-BIF/IFE format
(must stay byte-identical).

## Design

**Principle:** `Level.Size` = true content extent at every level; `Downsample`
exact; `Grid` stays the raw **stored** tile grid; **pixels are still read from the
stored tiles**; region/`StitchedTile`/`DecodedTile` reads clip to the corrected
`Size`. The padded raster extent remains recoverable as `Grid × TileSize`.

### BIF (DP generation only)

The BIF reader already derives the stitched hull at L0 (`buildDPLayout`) and
distinguishes DP from legacy. Extend the L0 derivation to the reduced levels —
**for a DP-stitched pyramid only**:

- `Size[i]` for `i ≥ 1` is derived by **iterative floor-halving from the L0 hull**:
  `Size[i] = floor(Size[i-1] / 2)` per axis, seeded by `Size[0] =` the L0 stitched
  hull. This reproduces the bio-formats `sizeX` chain exactly
  (`23432,11716,5858,2929,1464,732,366,183`); DP heights are unchanged (no Y
  overlap → the padded IFD height already equalled `hull/2^i`).
- `Downsample[i]` becomes exact `2^i` (it is computed from `Size`:
  `l0Hull.W / Size[i].W`).
- **The same content extent drives the `regionLayout` capability**, not just the
  `Level.Size` metadata (see the **Consistency invariant** below). The per-level
  `StitchedSize(level)` returned to the region/`StitchedTile`/`ScaledStrips`
  compositing path must equal the corrected `Level.Size`, computed from one
  source. (Today both happen to equal the padded grid at L1+; correcting only
  `Level.Size` would desync them.)
- **Legacy** (`buildLegacyLayout` pyramids) and **non-overlapping** BIF: **no
  change** — `Size[i]` and `StitchedSize(level)` keep their current values.
  Legacy is gated behind #80.
- `Grid` is unchanged (the raw stored grid). DP L1+ are non-overlapping, so
  `Grid == ceil(Size/TileSize)` (Ventana-1 L1: `ceil(11716/1024)=12=Grid`) →
  `Overlapping` stays **false** and the contract holds. Reads place tiles at
  `col·tile` and clip to `Size`; since DP L1+ don't overlap, the content is the
  top-left `Size` region and clipping is correct.

#### Consistency invariant (BIF)

For BIF, `Level.Size[i]`, `regionLayout.StitchedSize(i)`, and
`StitchedGrid() = ceil(Size/TileSize)` are **derived from one per-level
content-extent value** and must agree. This guarantees the four read surfaces are
mutually consistent at every level:

- `ReadRegion` clips to `min(Level.Size, StitchedSize)` — consistent once equal.
- **`StitchedTile` clips to `StitchedSize(level)` only** (it does not consult
  `Level.Size`), so `StitchedSize` *must* be the content extent — otherwise a
  display tile composites overscan past where `Level.Size`/`StitchedGrid` say
  content ends. For the partial last column this yields `content + white-fill`,
  matching openslide/bio-formats (which never return the overscan).
- `ReadRegionScaled` / `ScaledStrips` inherit `StitchedSize` for their bounds.

This is the load-bearing detail the v0.46 stitch already honors at L0 (where
`Level.Size == StitchedSize == hull`); the fix extends that equality to DP L1+.

### IFE (all levels)

IFE carries the exact geometry in-file: `TILE_TABLE.x_extent/y_extent` ("image
width/height in pixels at top resolution layer", spec §TILE_TABLE) and a per-layer
`LayerExtent.Scale` with the spec's downsample formula `downsample = max_scale /
scale`. Replace the padded `XTiles·256` `Size` with a scale-derived extent:

- **Always (fixes the drift, even on non-conformant files):** derive the pyramid
  from the per-layer `scale`:
  - `Downsample[i] = max_scale / scale[i]` (spec formula; `max_scale` = finest =
    L0).
  - `Size[i] = round(Size[0] / Downsample[i])` per axis.
- **Anchor L0 to true content when available:** if `x_extent/y_extent` are valid
  pixel dimensions, set `Size[0] = (x_extent, y_extent)`; otherwise keep the
  current `Size[0] = (XTiles[0]·256, YTiles[0]·256)` padded base.
  - **Validity test:** `x_extent` is pixels iff it lies in
    `((XTiles[0]-1)·256, XTiles[0]·256]` (content fits the L0 tile grid with a
    partial last tile). The cervix fixture stores *tile counts* there
    (`reader.go:63` note), which fails this test → falls back to the padded base
    but still gets exact *ratios* from `scale`. GT450 stores pixels → fully
    content-exact.
- IFE is non-overlapping, so `Grid = XTiles = ceil(content/256)` → `Overlapping`
  stays false; naive tile placement is already correct; reads clip to the
  corrected `Size`.

### API representation

Redefine `Level.Size` / `Level.Downsample` **in place** (no new field). This is
the library's existing convention (the value becomes correct, matching the other
10 formats and both reference readers); the old padded value was a bug. The padded
raster extent is still exactly `Grid × TileSize` for any consumer that needs it.

### Rounding

Match each format's reference: **BIF — floor-halving** (bio-formats, the sole DP
oracle); **IFE — `round`** of the scale-derived extent (the spec's
`max_scale/scale` formula). Documented; differences are ≤1px and below
registration-relevant magnitude.

## Components / files

| File | Change |
|---|---|
| `formats/bif/` (level build + `levelImpl.StitchedSize`, `bif.go`/`level.go`) | For DP pyramids, compute one per-level content extent (iterative floor-halving from the L0 hull) and use it for **both** `Level.Size`/`Downsample` **and** the `levelImpl`'s `StitchedSize(level)` (the `regionLayout` value). Legacy/non-overlapping untouched. |
| `formats/ife/tiler.go:70-96` | Replace `Size = XTiles·256` / `Downsample = l0Width/levelW` with `scale`-derived `Downsample = max_scale/scale` + `Size = round(Size0/Downsample)`, anchoring `Size0` to `x_extent/y_extent` when those pass the pixel-validity test. |
| `formats/ife/reader.go` | Already parses `Scale` + `XExtent/YExtent`; possibly expose `max_scale` / a small helper. No new parsing. |
| Tests (BIF + IFE) | New per-level `Size`/`Downsample` assertions; regression guards (see below). |
| `docs/`, `CHANGELOG.md` | Document the corrected `Size` semantics + the consumer note. |

## Error handling / edge cases

- **DP single-tile / non-overlapping levels:** `Size` already correct → no
  derivation applied.
- **IFE `scale` strictly increasing** is already validated at parse
  (`reader.go`); `max_scale` is the last (finest) entry.
- **IFE non-conformant `x_extent`** (cervix tile-counts): pixel-validity test
  fails → padded L0 base, exact ratios preserved.
- **Reads at corrected `Size`:** DP L1+ and IFE are non-overlapping, so the
  content is the top-left `Size` region of the stored grid; clipping is correct
  (the dropped pixels are padding/overscan). Verified by the non-overlap pixel
  controls.
- **Legacy BIF:** explicitly unchanged — a regression test pins legacy `Size` to
  its current value so this change cannot silently alter it.

## Testing strategy

- **BIF DP (fixture-gated, Ventana-1):** assert per-level `Size` equals the
  bio-formats `sizeX` chain (`23432,11716,5858,…`) and heights
  (`21504,10752,…`); `Downsample[i] == 2^i` (exact); L0 unchanged.
- **BIF legacy regression (fixture-gated, OS-1):** assert per-level `Size` is
  **unchanged** from current (raw grid) — proves legacy is untouched (deferred to
  #80).
- **IFE ratios (cervix, local):** assert `Size[i-1]/Size[i] ≈ 2` within rounding
  and `Downsample[i] == max_scale/scale[i]`; the drift (current coarse-level
  ratios ≠ 2) is gone.
- **IFE x_extent anchor (synthetic unit test):** construct `LAYER_EXTENTS` +
  `TILE_TABLE` with a pixel `x_extent` not divisible by 256 and assert
  `Size[0] == x_extent` and exact downsample chain (GT450 behavior, since GT450
  isn't a local fixture).
- **Cross-format regression:** `TestSlideParity` byte-identical for the other 10
  formats (their `Size`/`Downsample` are untouched).
- **BIF consistency invariant (fixture-gated, Ventana-1):** at every level
  `Level.Size == regionLayout StitchedSize == ` the value `StitchedGrid` is
  derived from; and `StitchedTile` over `StitchedGrid()` at DP L1 returns a last
  partial column that is `content + white-fill` (clipped to the corrected
  extent), not overscan. Guards the desync this review caught.
- **Read correctness:** `ReadRegion`/`DecodedTile` at DP L1 and an IFE coarse
  level still return correct content after the `Size` clip.

## Consumer impact

DP-BIF L1+ and IFE `Size`/`Downsample` values **change** (to correct ones).
Downstream consumers that built pyramids from the old padded values — notably
**wsitools** (DZI/output dims) — will see shifted output (a fix, but byte-
changing). wsitools/openscope are not in opentile-go CI, so this needs explicit
consumer coordination per the standing lesson. openscope is the beneficiary
(its zoom registration is fixed for DP-BIF + IFE).

## Out of scope / deferred

- **Legacy-BIF reduced-level geometry** → blocked on #80 (per-level stitching;
  pixel-confirmed overlap). A Size-only change there would mis-place pixels.
- **IFE Magnification/MPP-from-wrong-level** → #81 (separate metadata bug).
- Any change to `Grid`, tile placement, or the 10 already-correct formats.

## Open questions

1. **BIF derivation seat:** confirm the exact build site (`formats/bif/bif.go`
   level construction vs `level.go`) and that the L0 hull is available before L1+
   `Size` is assigned (compute L0 first, then derive). Resolved in the plan.
2. **IFE `Downsample` field type:** current `Downsample` is `float64`;
   `max_scale/scale` is exact-enough as `float64`. Confirm no precision surprise
   vs the integer-power expectation (round for display only, keep float).
3. **Legacy regression-pin fixture availability in CI:** OS-1 is in the public
   wsi-fixtures corpus (BIF tar) — confirm the legacy-unchanged test runs in CI,
   else gate it local-only.
