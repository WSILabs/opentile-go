# BIF overlap-aware stitching (GH #60) — design

**Status:** design / approved-to-plan
**Issue:** [#60](https://github.com/wsilabs/opentile-go/issues/60) — BIF L0 grid/width and overlap-aware stitching
**Branch:** `fix/bif-l0-grid-width-60`
**Author:** opentile-go maintainers
**Date:** 2026-06-18

---

## 0. Background

A Ventana BIF is a single BigTIFF (or, for legacy iScan, classic-TIFF — see
#37) container holding a tiled image pyramid plus associated images. Unlike
every other format opentile-go reads, **a BIF tile grid is not a faithful
spatial partition of the slide image.** The scanner captures overlapping AOI
("area of interest") sub-images as a grid of physical camera frames; adjacent
frames overlap by a scanner-measured pixel count. The on-disk tiles are those
raw camera frames, stored row-major; the *displayed* slide is the result of
**stitching** them — sliding each frame inward by its measured overlap so the
overlap regions coincide, then compositing.

opentile-go today does **not** stitch. It:

- reports `Level.Size` as the IFD's `ImageWidth × ImageLength` (the padded
  raw-frame extent, e.g. Ventana-1 L0 = `24576 × 22528` = 24×22 frames of
  1024², including a phantom padding column/row), and
- composites `ReadRegion` on a **naive regular grid** (`tileX = col *
  TileSize.W`), placing every frame at its un-shifted grid position.

Both are wrong for stitched output: the naive grid double-counts every overlap
band, so the assembled image is wider/taller than the real slide and shows
seam artifacts (each overlap region appears twice, offset). This is invisible
when you consume **individual raw or decoded tiles** (which #57 already fixed
to be byte-correct and row-major), but it is visible the moment a consumer
asks for stitched pixels — `ReadRegion`, `ReadRegionScaled`, `ScaledStrips`
(DZI), or the derived slide dimensions.

The fix is to make the **stitched-pixel paths pixel-exact** for the DP
generation (whose stitching is fully specified by the Roche BIF whitepaper),
while keeping the **raw/decoded per-tile API unchanged** (consumers that want
the raw frames still get them, with enough metadata to place them).

### What `#57`/`v0.45.3` already established (do not re-litigate)

- TILE_OFFSETS storage is **row-major, top-left origin** — `frameIndex` (from
  the `<Frame XY="C,R">` nodes) or plain row-major. Serpentine is the
  `<TileJointInfo>` stitch-graph numbering, **not** the storage order.
- `Tile(col,row)` / `DecodedTile(col,row)` / `TileAt` return the correct raw
  frame at image-grid `(col,row)`. The byte-level tifffile oracle
  (`TestTifffileParityBIF`) and the bio-formats placement oracle
  (`TestBIFTilePlacementSpatial`) both pass.

This design builds the **stitch layer on top of** that correct per-tile
addressing. It changes nothing about which bytes `Tile(col,row)` returns.

---

## 1. Goals / non-goals

### Goals

1. **DP generation (`GenerationSpecCompliant`): pixel-exact stitched output.**
   `Level.Size`, `ReadRegion`, `ReadRegionInto`, `ReadRegionScaled`, and
   `ScaledStrips` must produce the stitched slide that bio-formats / openslide
   produce, modulo JPEG decoder rounding (±1–2/channel). Derived dimensions
   must match bio-formats **exactly** (integer, no tolerance).
2. **Keep the raw/decoded per-tile API intact.** `Tile`, `TileInto`,
   `TileReader`, `TileAt`, `DecodedTile`, `Tiles`, `TilePrefix`,
   `TileBodyInto`, the splice helpers — all unchanged, byte-for-byte. A
   consumer that wants the raw camera frames (e.g. to re-tile, inspect, or
   debug) still gets them, addressed by image-grid `(col,row)`, with the
   overlap metadata needed to place them.
3. **Layout is computed once, at Open, from the file.** No per-call XML
   parsing; the stitch layout is built during pyramid construction and cached
   on the level.
4. **Legacy generation (`GenerationLegacyIScan`): best-effort, honestly
   documented.** Apply the same engine where the data supports it; where the
   whitepaper disclaims legacy and the file lacks the information bio-formats
   reconstructs from a GPL heuristic, fall back to a documented approximation
   and say so. **Legacy exact overlap is an explicit deferred follow-up** (see
   §E) — the user has flagged that the file likely carries more usable
   information than we currently extract.

### Non-goals

- Blending in overlap regions. The whitepaper specifies hard replacement
  (Tile2 pixels overwrite Tile1 pixels in the overlap band), not alpha
  blending. We replicate the replacement, not invent blending.
- Re-tiling BIF into a regular grid on disk. Out of scope; that is a wsitools
  conversion concern.
- Vertical overlap on DP 200 (`OverlapY` is always 0 there). The engine
  *handles* non-zero `OverlapY` generically so DP 600 / future scanners and
  synthetic tests work, but no DP 200 fixture exercises it.
- Changing the multi-dim (Z) addressing established in v1.0 / #57.

---

## 2. Licensing / clean-room constraints

**This is a hard constraint, not a preference.**

- The **only** source of stitch *algorithm* and *layout semantics* is the
  Roche "Digital Pathology BIF" whitepaper, committed (gitignored sample dir)
  at `sample_files/bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf`. All
  algorithm comments and the spec cite the whitepaper section, never another
  reader's source.
- `bio-formats` `VentanaReader.java` is **GPL v2**; `openslide` is **LGPL
  2.1**. We must **not** translate, paraphrase-from-source, or otherwise create
  a derivative of either. They are used **only as black-box pixel/dimension
  oracles** in tests (feed a slide, compare output pixels/dims) — never read
  for expression to port.
- Algorithms and facts are not copyrightable; expression is. Deriving the
  stitch math from the whitepaper (a license-clean Roche document) is clean.
  The numeric *results* we compare against (bio-formats' output dimensions for
  a given slide) are facts, not expression — comparing to them is fine; copying
  their code to compute them is not.
- The legacy gap (§E) exists **because** bio-formats reaches its legacy
  dimensions via a GPL `columnXAdjust`/`columnYAdjust` heuristic we will not
  port. We reach legacy dims from the whitepaper-clean engine + file data only.

---

## 3. One container, two reconstruction generations

A BIF is **one file format** with **two reconstruction generations**, already
modeled by `bif.Generation` (`classify.go`):

| | `GenerationSpecCompliant` (DP 200/600) | `GenerationLegacyIScan` (Coreo/HT) |
|---|---|---|
| Routing | `<iScan>/@ScannerModel` starts `"VENTANA DP"` | everything else iScan-tagged |
| Stitch spec | fully specified by whitepaper | whitepaper **disclaims** it |
| `EncodeInfo` | `Ver ≥ 2`, full `<TileJointInfo>` + `<Frame>` + `<AoiOrigin>` | may be absent/sparse; no `<Frame>` |
| This design | **exact stitch** (§A–§C) | **best-effort** engine + documented fallback (§C, §E) |

The stitch engine (§A) is generation-agnostic in its math; the **inputs** it
receives differ by generation (the DP path gets complete `EncodeInfo`; legacy
gets whatever is present, with documented fallbacks). Routing stays in
`classifyGeneration` — no new generation enum.

---

## §A. The stitch engine (pure, fixture-free)

A new pure package — proposed `formats/bif/stitch` (or an unexported
`stitch.go` within `formats/bif`; see Q-A1) — turns parsed inputs into a
**layout**: where each image-grid tile lands in stitched output space, and the
stitched image's dimensions. It touches no pixels, no I/O, no `tiff.Page` — it
is a function of `EncodeInfo` + grid geometry + tile size. This makes it
unit-testable from synthetic `EncodeInfo` with no fixtures.

### A.1 Inputs

```go
// StitchInput is the pure, file-free description the engine needs.
type StitchInput struct {
    Cols, Rows int            // image-grid dimensions (the addressing grid)
    TileW, TileH int          // nominal tile pixel size
    EncodeInfo *bifxml.EncodeInfo // joints, frames, AOI origins (may be nil → naive)
    Generation Generation     // affects gating + fallback strictness
}
```

### A.2 Output

```go
// TilePlacement is where one image-grid tile lands in stitched output.
type TilePlacement struct {
    Col, Row int   // image-grid coordinates (addressing)
    X, Y     int   // top-left of this tile in stitched output space (pixels)
}

// Layout is the engine's result: per-tile placement + stitched extent.
type Layout struct {
    Width, Height int             // stitched image dimensions (pixels)
    Placements    []TilePlacement // one per non-empty image-grid tile
    // tileOrigin[(col,row)] → (X,Y); built from Placements for O(1) lookup.
}

func (l *Layout) TileOrigin(col, row int) (x, y int, ok bool)
func (l *Layout) TilesIntersecting(x, y, w, h int) []TilePlacement
```

`TileOrigin` and `TilesIntersecting` are the two queries the compositing layer
(§B) needs: "where does tile (c,r) go?" and "which tiles touch this output
rectangle?".

### A.3 DP algorithm (whitepaper-derived)

Per the whitepaper §"Image Stitching" / §"AOI Positions":

1. **Storage→image position** comes from the `<Frame XY="C,R">` nodes
   (row-major; already captured as `frameIndex` in `level.go`). The engine
   addresses tiles by image `(col,row)`; the byte layer maps to storage.
2. **Per-pair overlap.** Each `<TileJointInfo>` gives `Tile1`, `Tile2`
   (serpentine physical indices), `Direction`, `OverlapX`, `OverlapY`,
   `FlagJoined`, and (to be added — see A.6) `Confidence`. A joint is honored
   only when `FlagJoined == 1` **and** `Confidence == 100` (whitepaper:
   confident joins only; uncertain joins are dropped and the nominal step is
   used). `Tile2`'s pixels replace `Tile1`'s in the overlap band — **hard
   replacement, no blend**.
3. **Cumulative inward shift.** Walk the grid; each step right reduces the X
   advance by that pair's `OverlapX`; each step down reduces the Y advance by
   `OverlapY`. DP 200: `OverlapY == 0` everywhere, so only horizontal
   compaction occurs. A tile's stitched origin is the nominal grid origin minus
   the accumulated upstream overlap in each axis.
4. **AOI placement.** Each AOI's tiles are shifted by its `<AoiOrigin>`
   `OriginX/OriginY` (whitepaper: multiples of tile size). The stitched image
   is the **convex hull (bounding box) of all placed AOIs**, normalized so the
   global min corner is `(0,0)`.
5. **White padding.** The hull is padded with `ScanWhitePoint`-valued pixels
   up to a **tile multiple** on the **top and right** (whitepaper: padding is
   top+right). Empty image-grid positions (offset==0 && bytecount==0) are
   `ScanWhitePoint` fill.
6. **Gating.** The exact path requires `EncodeInfo.Ver ≥ 2` and
   `Generation == GenerationSpecCompliant`. Otherwise → §C fallback.

The engine emits, per non-empty image-grid `(col,row)`, the stitched origin
`(X,Y)`; `Width/Height` is the padded hull. **`Layout.Width/Height` becomes the
level's reported `Size` for the stitched paths** (see §C).

### A.4 Empirically-pinned DP result (Ventana-1)

From the whitepaper math on Ventana-1: content `23432 × 21504` = 23 content
columns × 1024 − per-row horizontal overlap (≈120 px/row cumulative),
22 rows × ... The 24th column is phantom padding (raw-frame extent only). The
plan's first task is a **golden-dimension assertion**: the engine, fed
Ventana-1's `EncodeInfo`, must produce exactly bio-formats' reported
dimensions (read once from the oracle, pinned as a constant). If it doesn't,
the engine is wrong — fail loudly before any pixel work.

### A.5 Legacy (OS-1) result

The same engine, fed OS-1's (sparser) `EncodeInfo`, produces a **best-effort**
`114468 × 98094` vs bio-formats' `105817 × 93978`. The gap is bio-formats'
GPL `columnXAdjust` legacy heuristic, which we will not port. The legacy path
therefore documents its dimensions as **approximate** and is **gated separate
from the DP exactness assertion** (no exact-dim test for legacy). See §E.

### A.6 Required `bifxml` addition

`TileJoint` gains a `Confidence int` field (parsed from `<TileJointInfo
Confidence="...">`), so the engine can apply the whitepaper's confident-join
gate. Purely additive to the parser.

---

## §B. Layout-aware compositing

### B.1 The `regionLayout` hook (lazy type-assertion, MetadataOf pattern)

`imageReadRegionImpl` (`region.go`) currently hard-codes the naive grid
(`txMin = x0 / TileSize.W`, `tileX = tx * TileSize.W`). We introduce an
**optional capability interface** discovered through the existing
`UnwrapReader` chain — exactly the lazy type-assertion provider pattern already
used by `bif.MetadataOf` and the TIFF-tag provider:

```go
// regionLayout is implemented by readers whose tile grid is not a regular
// spatial partition (BIF). Discovered via the UnwrapReader chain at the top
// of imageReadRegionImpl; absent → the existing naive-grid path runs unchanged.
type regionLayout interface {
    // TileOrigin returns the top-left of image-grid tile (col,row) in this
    // level's stitched output space, or ok=false if the tile is absent.
    TileOrigin(level, col, row int) (x, y int, ok bool)
    // TilesIntersecting returns the image-grid tiles whose stitched extent
    // touches the output rectangle [x,y,w,h) at the given level.
    TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int }
    // StitchedSize is the level's stitched dimensions (== Level.Size for BIF).
    StitchedSize(level int) (w, h int, ok bool)
}
```

`imageReadRegionImpl` gains a single branch at the top:

```go
if rl, ok := regionLayoutOf(s.r); ok {
    // layout-aware path: iterate rl.TilesIntersecting(...), decode each via
    // imageDecodedTileInto, blit at rl.TileOrigin(...) with overlap-aware
    // source cropping.
} else {
    // existing naive-grid path, byte-for-byte unchanged.
}
```

Every non-BIF reader returns `ok=false` (does not implement the interface) and
keeps the current code path **bit-identical** — this is the perf-and-
correctness-neutrality bar for the 10 other formats.

### B.2 Overlap-aware blit (hard replacement)

In the layout-aware path, tiles are visited in **stitch order** (the order in
which Tile2-replaces-Tile1 must apply: later tiles overwrite earlier in the
overlap band). For each intersecting tile:

1. `rl.TileOrigin(level, col, row)` → stitched `(tileX, tileY)`.
2. Decode the raw frame (`imageDecodedTileInto` into the reused scratch — same
   pooling as today).
3. The **visible extent** of a tile is its full `TileW × TileH` *minus* the
   overlap bands that the next tile (right/down) will overwrite — OR we paint
   the full tile and rely on stitch-order replacement to overwrite. **Decision
   (Q-B2): paint full tiles in stitch order, let later tiles overwrite.** This
   matches the whitepaper's "Tile2 replaces Tile1" exactly and avoids
   off-by-one band arithmetic. Output region clipping (`maxInt/minInt`
   intersection with `[x0,y0,x1,y1)`) is unchanged.
4. Blit the clipped intersection into `dst` at the layout-derived position.

White-fill semantics: out-of-stitched-bounds pixels and empty-tile positions
fill `ScanWhitePoint` (DP) / `255` (legacy default), consistent with today's
`fillWhite` but using the layout's stitched bounds rather than the naive grid.

### B.3 `ScaledStrips` / `ReadRegionScaled`

These already build on `imageReadRegionImpl` / `imageDecodedTileInto`
internally. Once §B.1/§B.2 land, they inherit stitched correctness with no
further change **provided** they derive their geometry from `Level.Size`
(stitched) rather than re-deriving from grid×tile. Plan includes an audit task
to confirm no scaled path multiplies grid×TileSize directly.

---

## §C. Per-level scaling & dimensions

### C.1 `Level.Size` becomes stitched size for BIF

`newLevelImpl` computes the `Layout` (via §A) and sets `size` to
`Layout.Width × Layout.Height` for the stitched-paths view. Because
`Level.Size` is consumed by both the per-tile API (grid bounds) and the
stitched API (region clipping), we must not break per-tile addressing:

- The **grid** (`Level.Grid`, used by `Tile`/`Tiles`/`indexOf`) stays the
  image-grid `cols × rows` — unchanged.
- **`Level.Size`** changes from raw-frame extent (`ImageWidth × ImageLength`)
  to **stitched extent** (`Layout.Width × Layout.Height`).

This is the one externally-visible behavior change for consumers reading
`Level.Size` on a BIF. It is **correct** (the raw-frame extent was never the
real slide size) but it is a value change — documented in the migration note
and CHANGELOG. Per-tile bytes are unaffected.

### C.2 Pyramid levels 1+

The whitepaper: pyramid levels above 0 are **non-overlapping** (the scanner
writes already-stitched, downsampled levels). So for level ≥ 1 the engine
returns the trivial layout (naive grid) and `Layout.Width/Height` = the IFD's
own `ImageWidth × ImageLength`. Only level 0 carries `TileJointInfo`. Lower
levels' stitched size derives from their own IFD dimensions, and
`ReadRegionScaled` between levels uses the existing codec-domain scale path
(v0.34.1) unchanged.

### C.3 Legacy fallback

For `GenerationLegacyIScan` or `EncodeInfo == nil` / `Ver < 2`: the engine
produces its best-effort layout where `EncodeInfo` data exists, else the naive
grid. `Level.Size` for legacy L0 is the best-effort stitched extent
(documented approximate). No exact-dimension assertion for legacy.

---

## §D. Testing strategy

Five layers, cheapest→most-expensive, all CI-safe except the oracle ones
(which skip without fixtures, as today):

1. **Engine unit tests (fixture-free, CI-safe).** Synthetic `EncodeInfo` →
   `Layout`. Cases: single AOI no overlap (naive); single AOI uniform
   horizontal overlap (compaction math); two AOIs with `<AoiOrigin>` offsets
   (hull + normalization); empty-tile positions; `OverlapY ≠ 0` (DP 600 /
   synthetic); `Confidence < 100` joint dropped; `Ver < 2` → fallback. These
   pin the math with zero file dependency.
2. **DP exact-dimension assertion (oracle-pinned constant, CI-safe).** Pin
   bio-formats' Ventana-1 dimensions as a constant; assert the engine fed
   Ventana-1's `EncodeInfo` reproduces them **exactly**. The `EncodeInfo` for
   this test is captured from the fixture once and committed as a small golden
   XML/JSON, so the test runs without the 227 MB slide. (If capture is
   impractical, gate behind fixture presence.)
3. **Byte-parity per-tile (unchanged).** `TestTifffileParityBIF` and
   `TestBIFTilePlacementSpatial` must stay green — proves the stitch layer did
   not perturb raw-tile addressing.
4. **bio-formats pixel oracle with tolerance (fixture-gated).** Stitched
   `ReadRegion` of a tissue-bearing region vs bio-formats' equivalent crop,
   compared with a **per-channel tolerance** (±2–3) to absorb JVM-JPEG vs
   libjpeg-turbo decode rounding. The tolerance separates "correct placement,
   different decoder" (tiny per-pixel diff) from "wrong placement" (huge diff /
   structural mismatch). Add a structural guard (e.g. mean-abs-diff threshold)
   so a misplacement can't hide under per-pixel tolerance.
5. **Legacy honesty test.** Assert OS-1 stitched dims equal the documented
   best-effort constant (`114468 × 98094`), **not** bio-formats' — and a doc
   comment records the gap and the §E deferral. This locks the current behavior
   so a future legacy fix is a deliberate, reviewed change.

`make test` green under `-race`; new packages ≥80% cover (`make cover`).

---

## §E. Deferred: legacy (OS-1) exact overlap

**Explicitly out of scope for this milestone, per user direction:** *"ok,
let's proceed, but we will revisit fixing legacy overlap. clearly you are
missing something."*

The DP engine reaches `114468 × 98094` on OS-1 vs bio-formats' `105817 ×
93978`. bio-formats closes the gap with a GPL `columnXAdjust`/`columnYAdjust`
heuristic we will not port. The user believes the BIF file carries more usable
information than we currently extract (the engine ignores some legacy
`EncodeInfo`/`<iScan>` data, or interprets `AoiOrigin`/overlap differently for
legacy). This milestone:

- ships legacy as documented best-effort (§A.5, §C.3, §D.5),
- locks current legacy behavior with a test so a future improvement is
  deliberate,
- leaves a `TODO(#60-legacy)` and a follow-up issue capturing the hypothesis
  to investigate (what legacy fields we don't yet read; whether OS-1's
  `<TileJointInfo>`/`<AoiOrigin>` fully determine the bio-formats dims without
  a heuristic).

The deferral is a **known open item**, not a silent cap.

---

## Open questions (to settle in the plan or inline)

- **Q-A1:** package placement — new `formats/bif/stitch` subpackage vs
  unexported `stitch.go` in `formats/bif`. *Lean:* unexported file in
  `formats/bif` (the engine needs `Generation` and is BIF-only; a subpackage
  buys nothing and complicates the `bifxml` import). Revisit if it grows.
- **Q-B2:** full-tile paint + stitch-order overwrite (chosen) vs pre-cropped
  visible-band blit. Chosen full-tile for whitepaper fidelity; revisit only if
  a perf profile shows the overlap redraw is material.
- **Q-C1:** golden `EncodeInfo` capture for the CI-safe exact-dim test vs
  fixture-gating it. *Lean:* capture if the XML is small enough to commit
  cleanly (no PHI — `EncodeInfo` is geometry only); else fixture-gate.
- **Q-D1:** exact tolerance value + structural-diff threshold for the pixel
  oracle — pin empirically in the plan's oracle task.

---

## Summary of changes

| Area | Change | Breaking? |
|---|---|---|
| `internal/bifxml` | add `TileJoint.Confidence` | additive |
| `formats/bif` | stitch engine (`Layout`, `TilePlacement`, `TileOrigin`, `TilesIntersecting`); compute at `newLevelImpl` | additive |
| `formats/bif` | `Level.Size` for L0 = stitched extent (was raw-frame extent) | **value change** (documented) |
| `region.go` | `regionLayout` capability interface + layout-aware branch; naive path unchanged for non-BIF | additive (neutral for others) |
| tests | engine units, DP exact-dim, pixel oracle (tolerance), legacy honesty lock | — |
| docs | `docs/formats/bif.md` stitch section; migration note for `Level.Size`; legacy deferral | — |

Per-tile raw/decoded byte output: **unchanged.** DP stitched output:
**pixel-exact.** Legacy stitched output: **best-effort, documented, locked,
deferred for exactness (§E).**
