# Migration: BIF L0 `Level.Size` is now the stitched content extent

Prior to this change (#60), BIF `Level.Size` for spec-compliant DP slides (VENTANA
DP 200 / DP 600) reported the raw frame-grid extent — the padded TIFF `ImageWidth` /
`ImageLength`, which includes a phantom extra tile column produced by scanner
padding. This release ships overlap-aware stitching for DP-generation slides;
`Level.Size` at level 0 now reports the actual stitched content hull.

## What changed

| Property | Before (#60) | After (#60) |
|----------|-------------|-------------|
| `Level.Size` at L0 (Ventana-1, DP 200) | `24576 × 21504` (raw 24×21 frame grid) | `23432 × 21504` (stitched content hull) |
| `Level.Grid` at L0 | `{W:24, H:21}` | `{W:24, H:21}` — **unchanged** |
| Per-tile raw / decoded bytes | unchanged | unchanged |
| `ReadRegion` / `ReadRegionScaled` / `ScaledStrips` output | seam-artifacted at raw frame grid; frame boundaries visible | correctly stitched; overlapping frame borders resolved by hard replacement (later frame over earlier; no blending) |

The `Level.Grid` field and all per-tile APIs (`Tile`, `DecodedTile`, `TileReader`,
`Tiles`, `TileAt`) are **unaffected** — they continue to address raw camera frames
by `(col, row)`.

## Who is affected

Consumers that derive slide pixel dimensions from BIF `Level.Size` now get the
correct stitched size. For Ventana-1 the width shrinks from 24576 to 23432 pixels
(the 24th grid column is phantom padding — no real tissue). Any cached BIF
dimensions in wsitools, openscope, or other consumers that may have stored the old
raw extent should be invalidated and regenerated.

`ReadRegion` and `ScaledStrips` (DZI conversion) output is now correctly stitched
for DP-generation slides; the old output included seam artefacts where raw frame
boundaries produced visible lines at the 1024-pixel grid spacing.

## Legacy iScan slides are unchanged

Legacy iScan BIF slides (Coreo / HT, e.g. OS-1.bif) are **not affected**. The DP
stitch engine is gated to spec-compliant DP slides only. Legacy slides continue to
report the naive frame-grid extent as `Level.Size`. Overlap compaction for legacy
iScan slides is a known limitation, deferred: the clean-room placement-
reconstruction characterization is tracked in [#63](https://github.com/WSILabs/opentile-go/issues/63)
and the padded-width issue in [#60](https://github.com/WSILabs/opentile-go/issues/60).

## Validate `tile-grid-mismatch` relaxed

The Validate engine's `tile-grid-mismatch` check was tightened to require
`Grid >= ceil(Size/Tile)` (the grid must at least cover the image), but a larger
grid is now allowed (it is padding). BIF DP slides have `Grid.W=24` covering the
stitched width of 23432 (23 content columns + 1 phantom pad column = 24 ×
1024 = 24576 ≥ 23432), which passes the relaxed check.
