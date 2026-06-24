# Stitched Display-Tile Path — Design

**Status:** Draft for review (no code written)
**Date:** 2026-06-23
**Topic:** A clean, non-overlapping "display tile" surface for overlapping-tile
formats, generalized over the existing `regionLayout` capability — BIF as the
first implementer, MRXS and DZI/SZI-overlap as named future ones.

---

## Goal

Give tile-based viewers (openscope) a clean, non-overlapping tile grid for
overlapping-tile formats, so the viewer treats BIF (and future MRXS, DZI-overlap)
identically to SVS/NDPI — no overlap math, no seam handling, no format-specific
code. Contain *all* overlap/stitch complexity inside opentile-go, behind a single
additive accessor, without compromising throughput and without disturbing the
raw-tile surface that transcoders (wsitools) depend on.

## Background / problem

After the v0.46 BIF stitching milestone, an overlapping BIF level reports
`Size` = the stitched content hull, but `Grid` stays the **raw overlapping**
tile grid (`Grid.W × TileSize.W > Size.W`), with `Overlapping = true` as the
signal (`image.go:27-48`). The per-tile accessors (`Tile`, `DecodedTile`,
`Tiles`) return the raw overlapping camera frames at their stored positions; only
the region API (`ReadRegion` / `ReadRegionScaled` / `ScaledStrips`) composites the
stitched image.

This forces a tile-based viewer to either (a) route through the region API and do
its own tile-rect math, or (b) understand overlap and composite seams itself.
openscope currently assumes `DecodedTile` yields non-overlapping tiles, so BIF
renders wrong. The viewer is being asked to learn a format quirk that the library
already knows how to resolve.

**Two legitimate consumers want opposite things and both must keep working:**

- **Transcoders / faithful extractors** (wsitools verbatim tile copy) need the
  *raw* overlapping tiles and the `Overlapping` signal. This is exactly why
  v0.48's `Level.Overlapping` exists.
- **Viewers** (openscope) need *clean, composited* tiles on a grid that tiles
  `Size`.

So the answer cannot be "make `DecodedTile` composite" — that breaks the raw path.
It must be an *additional* view that coexists with the raw one.

## Precedent: NDPI already does this

An NDPI level is one big JPEG per level; "tiles" are synthesized. NDPI presents a
clean grid (`Grid = ceil(Size/TileSize)`, `Overlapping = false`) by
decode-once-per-frame + blit, backed by a bounded decoded-frame LRU
(`formats/ndpi/pixel_cache.go`, the `pixelFrameCache` / promise pattern) so
adjacent tiles sharing a source frame decode it once. NDPI is proof that a backing
store which doesn't match the consumer's tile grid can be presented as clean tiles,
cheaply. BIF is the same shape; v0.46 simply chose (correctly, for transcoders) to
also expose the raw tiles.

## What we already have (the seam exists)

- **`regionLayout`** (`region.go:14-18`) — the optional capability a reader
  implements when its tile grid is not a regular spatial partition of the level:
  ```go
  type regionLayout interface {
      TileOrigin(level, col, row int) (x, y int, ok bool)
      TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int }
      StitchedSize(level int) (w, h int, ok bool)
  }
  ```
  Discovered generically via `regionLayoutOf` walking the `UnwrapReader` chain
  (`region.go:22-35`). **BIF already implements it** (`formats/bif`); no other
  format does yet.
- **The composite-blit loop** already lives in `imageReadRegionImpl`
  (`region.go:113-146`): for each intersecting tile it gets the stitched origin,
  decodes the raw tile, and blits the in-bounds intersection. A `TileSize`-aligned
  rectangle is just a tile-sized `ReadRegion`.
- **The one gap:** `ReadRegion` deliberately does **not** cache decoded tiles
  across calls (`region.go:47-49`). A naive per-display-tile `ReadRegion` loop
  would re-decode every shared source frame (a frame at a 4-tile corner decoded up
  to 4×, re-decoded each row). That is the only thing standing between "correct"
  and "fast."

## Chosen approach

**Root-owned generic display-tile compositing, driven by the existing
`regionLayout`, plus one new bounded decoded-frame cache.** A format gains a clean
display-tile grid *solely* by implementing `regionLayout` — it contributes **zero**
compositing code. BIF, already a `regionLayout` implementer, needs **no new
format-package code**; the entire feature lives in the root `opentile` package.

This refines the earlier "Option 2 / per-format `stitchedTiler` interface" sketch:
because the composite loop and `regionLayout` are already generic, there is no need
for a per-format dispatch interface analogous to `decodedTiler`. The display-tile
path is generic root code over `regionLayout`.

### Public surface (additive only)

```go
// StitchedTile returns a clean, non-overlapping tile from the canonical display
// grid ceil(Size/TileSize). For overlapping levels (stitched BIF, future MRXS /
// DZI-overlap) it composites the stitched image; for every other format it is
// exactly DecodedTile. Pixels are identical to ReadRegion over the tile's rect.
func (l *Level) StitchedTile(x, y int, opts ...DecodeOption) (*decoder.Image, error)

// StitchedGrid is the canonical display grid, ceil(Size/TileSize). Equals Grid
// for non-overlapping levels; for overlapping levels it is the clean grid that
// tiles Size (whereas Grid stays the raw overlapping grid).
func (l *Level) StitchedGrid() Size
```

`Grid`, `Overlapping`, `Tile`, `DecodedTile`, `Tiles`, and raw `TileReader` are
**unchanged** — the raw view stays exactly as v0.48 defined it. Both views are
live on the same open `Slide`; no flag, no reopen. (`StitchedTileInto`, mirroring
`DecodedTileInto`, is an optional convenience — see Open Questions.)

### Dispatch / control flow

`StitchedTile(x, y)`:

1. `rl, ok := regionLayoutOf(s.r)`.
2. If `!ok` (ordinary format) → delegate to `DecodedTile(x, y, opts...)`. This
   makes `StitchedTile` **total** across all 11 formats, so a viewer can call it
   uniformly and stay format-agnostic.
3. If `ok` → composite the rectangle `[x·TileW, y·TileH, TileW, TileH]` clipped to
   `StitchedSize(level)`, using the **same** `TilesIntersecting` / `TileOrigin` /
   blit logic as `imageReadRegionImpl`, but pulling each raw tile from the new
   decoded-frame cache instead of a fresh `imageDecodedTileInto`.

The composite inner loop (`region.go:128-145`) is factored into a shared helper so
`StitchedTile` and `imageReadRegionImpl` cannot drift.

### The decoded-frame cache (the only real new machinery)

A per-`Slide`, bounded, decode-once-blit-many cache, modeled on NDPI's
`pixelFrameCache` + `frameByteLRU`:

- **Keyed** by `(image, level, col, row)` — the raw stored frame.
- **Value** is the decoded `*decoder.Image` for that raw frame (at the requested
  pixel format).
- **Population** via the promise pattern: first caller for a key decodes (through
  the existing decoder-handle pool, `s.decoderFor`); concurrent callers for the
  same key block on a `ready` channel and share the result. (Mirrors
  `pixel_cache.go` `getOrLoad`.)
- **Bounding** by byte budget derived from `readBudget` (`WithMemoryBudget` /
  `OPENTILE_READ_MEMORY_BUDGET`), an LRU like `frameByteLRU` (128 MiB-style
  default), never evicting an in-flight entry — applying the v0.47.1 eviction-race
  lesson (do not evict reserved/`ready`-open entries).
- **Lifetime** is the `Slide` (long-lived), so panning across many `StitchedTile`
  calls reuses decoded frames; drained in `Slide.Close`.

With this cache, each raw camera frame decodes once per cache lifetime; a display
tile straddling a seam touches 1–4 cached frames, each a cheap blit. Throughput
matches `ScaledStrips` (already 3–12× openslide on decode).

## Performance

- **Decode-once-blit-many**, identical in shape to NDPI's fast path. The corner
  case (a frame shared by up to 4 display tiles) decodes once, not four times — the
  cache is what removes the `region.go:47-49` re-decode cliff.
- **No regression for existing paths.** `ReadRegion` / `ScaledStrips` /
  `DecodedTile` / `Tile` are untouched. `StitchedTile` is new code on a new method.
- **Memory** is bounded by `readBudget`, same knob as the strip cache. (Whether the
  display-tile cache and the strip cache share one budget pool or hold separate
  sub-budgets is an Open Question.)

## Error handling / edge cases

- **Gaps** (multi-AOI legacy BIF, deferred #67): the composite loop white-inits the
  output, so display tiles over gaps are white — consistent with `ReadRegion`.
- **Edge tiles**: a display tile past `StitchedSize` is partial; the remainder is
  white. Same as `ReadRegion`.
- **Raw bytes untouched**: `Tile()` always returns faithful compressed bytes; no
  sentinel, no mode. Transcoders are entirely unaffected.
- **`WithScale > 1`**: v1 scope supports `Scale = 1` for `StitchedTile`; scaled
  traversal is already served by `ScaledStrips`/`ReadRegionScaled`. Codec-domain
  scale for display tiles is deferred (Open Questions).
- **nocgo**: `StitchedTile` decodes, so for JPEG-backed BIF it requires cgo exactly
  like `DecodedTile` (returns `ErrCGORequired` under nocgo). A pure-PNG DZI-overlap
  level could work under nocgo since PNG decode is pure Go.

## Generalization beyond BIF

`regionLayout` is already format-agnostic; this design makes it the single seam
where every overlapping format plugs in. A new format implements a layout and
inherits **both** the region API and the display-tile surface — it never writes
compositing, caching, or seam code.

| Format | Overlap model | What it must add | Gets for free |
|---|---|---|---|
| **BIF** (now) | camera-FOV, irregular | already implements `regionLayout` | `StitchedTile` with **no new code** |
| **MRXS** (future) | camera-FOV, irregular | a `regionLayout` from position-record reconstruction (a `buildLayout` analog) | region API + `StitchedTile` |
| **DZI/SZI `Overlap>0`** (future) | symmetric fixed (N px each interior edge) | a trivial `regionLayout` (origin `= col·contentSize`, trim N) | region API + `StitchedTile` |

Two distinct overlap *models* share one compositing/display surface:

- **Symmetric fixed** (DZI/SZI `Overlap=N`): uniform, known a priori; the layout is
  trivial. This also closes a **latent correctness gap today**:
  `formats/szi/level.go:61-64` hardcodes `TileOverlap = {0,0}` and
  `internal/dzi/manifest.go` accepts but ignores positive `Overlap`, so a DZI with
  `Overlap=1` currently **mis-stitches** by ~1 px. Implementing its `regionLayout`
  fixes the region path *and* yields display tiles.
- **Camera-FOV irregular** (BIF, MRXS): reconstructed from joint/position metadata,
  variable per seam; needs a real layout builder.

These future implementations are **named, not specced here** — each is its own
spec/plan. This document only commits to the generic display-tile path and BIF as
the first (zero-code) implementer.

## API surface summary

- **Added (public):** `(*Level).StitchedTile`, `(*Level).StitchedGrid`. (Optional:
  `(*Level).StitchedTileInto`.)
- **Added (internal):** a per-`Slide` decoded-frame cache type; a factored
  composite-blit helper shared with `imageReadRegionImpl`.
- **Changed:** nothing. No existing exported name changes meaning. Purely additive —
  honors the "don't gratuitously break the public API" invariant; wsitools and
  openscope are unaffected until they opt in by calling `StitchedTile`.

## Considered and rejected

- **Option 1 — a per-`Slide` "stitched tiles" mode** (an open-time
  `WithStitchedTiles()` flag that reinterprets `Grid`/`DecodedTile` as canonical and
  disables raw `Tile()` bytes). Gives a marginally more agnostic viewer, but at the
  cost of: `Level`'s per-tile surface having two meanings depending on an open flag;
  a raw-bytes hole (`Tile()` unsupported in mode); and threading the flag through the
  reader factory. The additive accessor reaches ~95% of the agnosticism (via the
  delegating fallthrough) with none of these costs. Rejected.
- **Per-format `stitchedTiler` dispatch interface** (analogous to `decodedTiler`,
  each format implementing `ImageStitchedTile`). Unnecessary: `regionLayout` and the
  composite loop are already generic, so the display-tile path is root code over
  `regionLayout`. A per-format interface would duplicate compositing. Rejected in
  favor of root-owned generic compositing.
- **No new method; document "iterate `ceil(Size/TileSize)` and call `ReadRegion`
  per tile," and add a cache to `ReadRegion`.** Smallest public surface, but
  overloads `ReadRegion` with hidden cross-call caching (contradicting its
  documented contract) and pushes tile-rect math onto every viewer. Rejected as the
  primary path; the cache-backing of `ReadRegion` survives as an optional follow-up
  (Open Questions).

## Testing strategy

- **Equivalence:** `StitchedTile(vx,vy)` pixels are byte-identical to `ReadRegion`
  over the same canonical rect (they share the composite helper) — a cross-check
  test asserting this on BIF fixtures (Ventana-1 DP, OS-1 legacy).
- **Placement fidelity:** reuse the v0.46 per-join residual + seam-continuity MAD
  gates against the stitched output, now exercised through `StitchedTile`.
- **Delegation/agnosticism:** on a non-overlapping format (SVS/NDPI),
  `StitchedTile(x,y)` equals `DecodedTile(x,y)` and `StitchedGrid()` equals `Grid`.
- **Cache:** a decode-counter test proving each raw frame decodes once across a
  full display-grid traversal; bounded-memory assertion under `readBudget`;
  `-race -count` concurrency over the promise pattern; eviction never drops an
  in-flight entry (the v0.47.1 regression class).
- **Edge/gap:** display tiles over the hull edge and over multi-AOI gaps are
  correctly white-filled.

## Out of scope / deferred

- MRXS and DZI/SZI-`Overlap>0` implementations (named future, separate specs).
- Backing `ReadRegion` with the same decoded-frame cache to close its no-cross-call
  gap (optional follow-up; changes a documented contract).
- `WithScale > 1` on `StitchedTile` (use `ScaledStrips`/`ReadRegionScaled`).
- Option 1 display mode (rejected above).

## Open questions

1. **Cache budget topology:** one shared `readBudget` pool across the strip cache
   and the display-tile cache, or separate sub-budgets? (Leaning shared, reusing the
   v0.30 byte-budget machinery.)
2. **`StitchedTileInto`:** add the `Into` variant now for allocation-free viewer
   loops, or defer until a consumer needs it? (Leaning defer — YAGNI — but it's a
   trivial add.)
3. **`StitchedGrid` vs documentation only:** is the helper worth a public method, or
   is documenting `ceil(Size/TileSize)` enough? (Leaning method, for discoverability
   and to remove the `Grid`-is-raw footgun for overlapping levels.)
4. **Consumer rollout:** confirm openscope migrates to `StitchedTile` and that no
   other consumer is relying on the (buggy) current BIF `DecodedTile` behavior — per
   the standing lesson that wsitools/openscope are not exercised by opentile-go CI,
   so consumer-facing contract additions need explicit consumer-impact confirmation.
