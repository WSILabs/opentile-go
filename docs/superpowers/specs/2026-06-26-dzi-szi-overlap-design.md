# DZI / SZI Overlap > 0 Support — Design

**Date:** 2026-06-26
**Status:** Approved (brainstorming) → ready for implementation plan
**Scope:** Read tiles correctly from Deep Zoom Image (`formats/dzi`) and Smart
Zoom Image (`formats/szi`) pyramids whose manifest declares `Overlap > 0`.

---

## Goal

Today both readers reject `Overlap > 0` at open time with
`internal/dzi.ErrOverlapNotSupported` (added v0.52.0 so an overlapped DZI fails
loudly instead of being silently mis-rendered). This milestone implements the
standard DZI/OpenSeadragon overlap model so those files read correctly:
composited region/strip/stitched-tile output is overlap-free and pixel-faithful
to the source, while raw per-tile reads remain a faithful byte passthrough.

`Overlap = 0` behaviour is unchanged and byte-identical.

## Background: the DZI overlap model (measured, not assumed)

A DZI manifest carries a single slide-wide `Overlap` scalar (and `TileSize`,
`Format`, `Size`). The deepest level is at full resolution; each shallower level
halves dims (round up); the tile grid is `ceil(levelDim / TileSize)` per axis —
all unchanged from `Overlap = 0`.

With `Overlap = ov`, each **stored** tile additionally carries `ov` redundant
pixels on every edge that has a neighbour: left if `col > 0`, top if `row > 0`,
right if not the last column, bottom if not the last row. The tile's **content**
cell is unchanged — `[col·T, row·T]` of size up to `T×T`, clipped to the level
remainder at the right/bottom edges. The content sits at offset
`(col>0?ov:0, row>0?ov:0)` inside the stored tile.

Confirmed against two libvips-generated DZIs of the same `CMU-1.svs`
(`sample_files/dzi/CMU-1_dzi_libvips_overlap_{0,1}.dzi`, identical except
`Overlap`), level 16 (Size 46000×32914, T=256, grid 180×129):

| tile (c,r) | overlap=0 dims | overlap=1 dims | content rect within stored tile |
|---|---|---|---|
| (0,0) corner | 256×256 | 257×257 | Origin (0,0), Size (256,256) |
| (1,0) top edge | 256×256 | 258×257 | Origin (1,0), Size (256,256) |
| (0,1) left edge | 256×256 | 257×258 | Origin (0,1), Size (256,256) |
| (1,1) interior | 256×256 | 258×258 | Origin (1,1), Size (256,256) |
| (179,0) right edge | 176×256 | 177×257 | Origin (1,0), Size (176,256) |
| (0,128) bottom edge | 256×146 | 257×147 | Origin (0,1), Size (256,146) |
| (179,128) corner | 176×146 | 177×147 | Origin (1,0), Size (176,146) |

Because both files encode the same image, a correct reader's composited output
from overlap=1 must match overlap=0 (and the source) — the validation oracle.

## API surface

### New: `OverlapMode` enum (additive)

```go
// OverlapMode classifies how a level's stored/decoded tiles relate to its
// content grid.
type OverlapMode int

const (
    // OverlapNone: tiles are a clean partition of Size. Grid tiles Size;
    // per-tile reads are verbatim content cells; verbatim tile-copy is safe.
    OverlapNone OverlapMode = iota

    // OverlapBordered: stored/decoded tiles carry a redundant overlap border
    // (DZI/SZI Overlap>0). Grid STILL tiles Size (content cells partition it);
    // crop each decoded tile to TileContentRect, or use the region API.
    OverlapBordered

    // OverlapStitched: the stitch layout compacted the grid (BIF). Grid does
    // NOT tile Size (Grid.W×TileSize.W > Size.W); per-tile reads are raw
    // overlapping frames at stored positions; use the region API.
    OverlapStitched
)
```

### `Level` field changes

- **New:** `OverlapMode OverlapMode` — the precise per-level signal.
- **Retained, redefined:** `Overlapping bool` becomes a convenience equal to
  `OverlapMode != OverlapNone`. The value is **unchanged for every
  currently-readable slide** (BIF→`true` via `OverlapStitched`; all others→
  `false`); only the new DZI/SZI-overlap capability reports `true`. The "Grid
  does NOT tile Size" property moves from this bool to
  `OverlapMode == OverlapStitched`; its doc and the `Grid` doc are reworded.
  wsitools' existing `!Overlapping` "safe to verbatim-copy" gate stays correct
  (`OverlapBordered ⇒ Overlapping=true ⇒ don't copy`).
- **Reused:** `TileOverlap Point` carries the overlap magnitude `{ov, ov}`
  for `OverlapBordered` (DZI's single `Overlap` attribute → equal X and Y;
  always non-zero when bordered). Note it is **not** a universal overlap test:
  BIF L0 stitched levels carry a magnitude, but BIF *reduced* stitched levels
  report `{0,0}` (per-frame placement is authoritative, not a single
  magnitude). The reliable "tiles overlap / don't verbatim-copy" gate is
  therefore `OverlapMode != OverlapNone` (i.e. `Overlapping`), **not**
  `TileOverlap != {0,0}`.

### New: `(Level).TileContentRect(col, row int) (Region, bool)`

A pure `Level`-field computation (no reader callback) returning the content
sub-rectangle **within the decoded tile** for `(col,row)`:

```go
// Defined only for clean-grid modes (OverlapNone, OverlapBordered), where Grid
// tiles Size. Returns ok=false for OverlapStitched (use the region API) and for
// out-of-grid (col,row).
if OverlapMode == OverlapStitched || !inGrid(col,row) {
    return Region{}, false
}
offX = (OverlapMode == OverlapBordered && col > 0) ? TileOverlap.X : 0
offY = (OverlapMode == OverlapBordered && row > 0) ? TileOverlap.Y : 0
w    = min(TileSize.W, Size.W - col*TileSize.W)
h    = min(TileSize.H, Size.H - row*TileSize.H)
return Region{Origin:{offX,offY}, Size:{w,h}}, true
```

- `OverlapNone`: `ok=true`, `Origin (0,0)`, `Size` = clipped content cell (the
  full decoded tile) — a universal "where is the real content" answer.
- `OverlapBordered`: `ok=true`, the cropped content rect (table above).
- `OverlapStitched`: `ok=false` — the Size-partition formula is invalid because
  BIF's Grid does not tile Size; the doc directs callers to the region API.

`ok=false` also when `(col,row)` is out of the level grid.

A consumer recovers every per-side overlap from a decoded tile `W×H` and its
`TileContentRect{(ox,oy),(cw,ch)}`: `left=ox, top=oy, right=W-ox-cw,
bottom=H-oy-ch`.

## Read-path behaviour (DZI/SZI Overlap > 0)

| API | Returns | Notes |
|---|---|---|
| `Tile(c,r)` / `TileReader` / `TileInto` | on-disk **padded** bytes | byte passthrough, unchanged contract |
| `DecodedTile(c,r)` | **padded** pixels (e.g. 258×258) | option 2: decode the raw tile, no crop; pair with `TileContentRect` |
| `StitchedTile(c,r)` | **clean** cropped display tile (`TileSize`, edges white-filled) | composited via the region layout; what a viewer that ignores overlap wants |
| `ReadRegion` / `ReadRegionScaled` / `ScaledStrips` | clean composited pixels | overlap removed |

For DZI the content grid already tiles Size, so `StitchedGrid == Grid`.

## Architecture & components

### Compositing — reuse, ~zero new compositor code

DZI/SZI Overlap>0 slots into the existing `regionLayout` / `subtileLayout`
machinery (region.go, stitched_tile.go, strip_iterator.go, strip_workers.go)
that the BIF work built. The overlap>0 level/tiler implements:

- `regionLayout`: `TileOrigin(level,c,r) = (c·T, r·T)`;
  `TilesIntersecting(level,…)` = standard ceil grid over content cells;
  `StitchedSize(level) = (Size.W, Size.H, overlap > 0)`.
- `subtileLayout`: `UnitSize(level) = (T, T)`;
  `SubtileSource(level,c,r) = (c, r, offX, offY)` — **same** source tile,
  cropped (BIF maps to a *different* source tile; DZI maps to itself).

`compositeStitchedLoop` clips each unit's dest rect to the region/Size bounds, so
edge cells (content `< T`, no right/bottom overlap) are handled by the existing
clipping with no special case.

### Per-level gating keeps Overlap=0 on the fast path

The region path already gates per level via `StitchedSize(level) (w,h,ok)`:
`ok=false` routes to the clean-grid default path even when the reader implements
`regionLayout` (region.go:134). DZI returns `ok = (overlap > 0)`. Therefore
`Overlap = 0` levels never enter the composite path — the existing fast path is
untouched and **byte-identical**. (DZI overlap is manifest-wide, so a slide is
uniformly overlap=0 or overlap>0; no per-level mixing.)

### Shared content-rect math in `internal/dzi`

Both readers depend on one helper (unit-tested against the measured table):

```go
// ContentRect returns the content sub-rectangle (offX, offY, w, h) within the
// stored/decoded tile (col,row) of a level sized levelW×levelH with the given
// tileSize and overlap. offX/offY are the in-tile content offset; w/h the
// content size (clipped at the level's right/bottom edge).
func ContentRect(col, row, levelW, levelH, tileSize, overlap int) (offX, offY, w, h int)
```

`grid = ceil(levelDim/tileSize)` is already in `internal/dzi/coords.go`; the
"last column/row ⇒ no right/bottom overlap" condition is implicit in the content
clip (`w = min(tileSize, levelW - col*tileSize)`), so the helper needs only the
neighbour-presence rule for `offX/offY` (`col>0`, `row>0`).

### Reader changes

- `formats/dzi/tiler.go`, `formats/szi/tiler.go`: remove the `Overlap > 0`
  rejection. (`internal/dzi.ErrOverlapNotSupported` is retained for any genuinely
  unmodelled future case but no longer fires on plain overlap.)
- Carry `overlap int` onto the level/tiler; populate `OverlapMode`,
  `TileOverlap`, `Overlapping` when building each public `Level`.
- Implement `regionLayout` + `subtileLayout` on the tiler (delegating to the
  shared `ContentRect` math), with `StitchedSize` gating on `overlap > 0`.
- Raw `Tile*` and `DecodedTile` need no overlap-specific code — they already
  return the on-disk / decoded padded tile.

## Error handling & edge cases

- **Negative / malformed `Overlap`:** `internal/dzi` manifest parse already
  rejects `Overlap < 0`. Unchanged.
- **`Overlap >= TileSize`:** pathological but not rejected by the spec; the
  content-rect math stays valid (offset and clip are independent of whether
  overlap exceeds the cell). Document as "supported but degenerate"; no special
  casing.
- **Single-tile levels** (grid 1×1, shallow levels): `col=row=0` ⇒ no overlap on
  any side ⇒ stored tile == content. Handled by the `col>0`/`row>0` rule.
- **Right/bottom-edge tiles:** content clipped to remainder; no right/bottom
  overlap. Handled by the content clip.
- **Sparse/missing tiles:** out of scope for this milestone (DZI sparse-image
  support is separately parked); unchanged behaviour.

## Validation & testing

The overlap_0 / overlap_1 CMU-1 pair is the oracle. Tiles are independent JPEGs,
so cross-overlap comparison is low-MAD (≈ JPEG re-encode noise), not bit-exact; a
wrong crop/placement shifts content and spikes MAD.

1. **Cross-overlap region parity (headline, local-only):**
   `overlap_1.ReadRegion(r)` vs `overlap_0.ReadRegion(r)` over regions chosen to
   stress the crop — interior, seam-crossing, tile-aligned vs offset, right/bottom
   edges, multiple levels. Assert low MAD. Same gate for `ReadRegionScaled`,
   `ScaledStrips`, and `StitchedTile`.
2. **`TileContentRect` unit test (CI-safe synthetic):** corner / edge / interior /
   last-row-col cases against the measured table; `ok=false` out of grid;
   `OverlapNone` returns the full clipped cell.
3. **`ContentRect` helper unit test (CI-safe):** the same table at the
   `internal/dzi` level.
4. **Overlap=0 byte-identical regression:** existing overlap_0 reads unchanged
   (guards the fast-path gate `StitchedSize ok=false`).
5. **Field population:** overlap_1 level → `OverlapBordered`, `TileOverlap={1,1}`,
   `Overlapping=true`; overlap_0 → `OverlapNone`, `{0,0}`, `false`.
   `DecodedTile(1,1)` on overlap_1 is 258×258; `StitchedTile(1,1)` is 256×256.
6. **Tiny synthetic overlap=1 DZI for CI:** a small committed fixture (a handful
   of small tiles, overlap=1) exercises the crop/composite + `TileContentRect`
   path in CI without the large CMU-1 pyramid. The CMU-1 pair stays local-only
   (large; gitignored sample_files) for the high-fidelity parity gate.

## Consumer impact

- **Additive for existing slides.** No `Level` value changes for any
  currently-readable slide; `OverlapMode` and `TileContentRect` are new surface.
  wsitools/openscope keep compiling and their `!Overlapping` gates stay correct.
- **New capability:** consumers can now open Overlap>0 DZI/SZI. Those that want
  clean tiles call `StitchedTile`/region APIs; those that want raw tiles use
  `DecodedTile` + `TileContentRect`.
- Adopting `OverlapMode` (to distinguish bordered vs stitched) is optional and
  can happen when a consumer needs the distinction.

## Out of scope / non-goals

- DZI **sparse images** and **DZC collections** (separately parked).
- Re-encoding / writing overlapped DZIs (opentile is a reader).
- Changing `Overlap = 0` behaviour in any way.
- Per-axis differing overlap (DZI has a single scalar; not a real case).

## Files touched (anticipated)

- `image.go` — `OverlapMode` enum + const block; `Level.OverlapMode` field;
  reworded `Overlapping`/`Grid`/`TileOverlap` docs; `TileContentRect` method.
- `internal/dzi/coords.go` (or a new `content.go`) — `ContentRect` helper.
- `formats/dzi/{tiler,level}.go` — drop guard; carry overlap; implement
  `regionLayout`/`subtileLayout`; populate fields.
- `formats/szi/{tiler,level}.go` — same.
- Tests: `formats/dzi/*_test.go`, `formats/szi/*_test.go`, an `internal/dzi`
  helper test, a small CI fixture, and local-only parity tests.
