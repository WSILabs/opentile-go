# opentile-go v0.7 Design Spec — Ventana BIF (Roche)

**Status:** Spec, 2026-04-27, decisions sealed. **Predecessors:**
v0.1 – v0.6 (all merged to `main`; v0.6 closes upstream Python
opentile's format set). **Research evidence:**
`docs/superpowers/notes/2026-04-27-bif-research.md`.

**Decisions sealed 2026-04-27** (owner sign-off on §4, §6, §7, §8):

| § | Decision |
|---|----------|
| 4 | **Path C — both.** Spec-strict path when `ScannerModel` starts with `"VENTANA DP"` (DP 200, DP 600, future DP scanners); legacy-iScan path for everything else iScan-tagged. |
| 6 | **Expose probability map.** New associated-image kind `"probability"`. |
| 7 | **Triple oracle.** openslide for OS-1, tifffile for both fixtures, geometry sanity for both. |
| 8 | **Overlap as metadata.** New method `Level.TileOverlap() image.Point`; raw-tile-bytes hot path preserved. |

---

## 1. One-paragraph scope

v0.7 adds **Ventana BIF** (Roche / iScan) — the **first format
beyond upstream Python opentile's coverage**. BIF is a BigTIFF
container used by Roche's VENTANA DP 200 (and predecessor iScan
Coreo / iScan HT) whole-slide scanners; the format is publicly
specified by Roche but only the DP 200 generation is documented in
detail. v0.7 introduces three elements not present in prior format
work: **first-class XMP/XML metadata parsing** (per-IFD `<iScan>` /
`<EncodeInfo>` trees carrying tile geometry, AOI origins, overlap
data, and scanner properties); **serpentine-to-row-major tile
reindexing** (TileOffsets is in physical-stage order, not image
order); and the project's **first non-byte-stable correctness bar** —
no upstream Python `opentile` reference, openslide partially
compatible, so the parity-oracle approach has to evolve.

CLAUDE.md previously declared the v0.7 correctness bar as
"pixel-equivalence with openslide on decoded tiles." Research has
shown that bar is incomplete: of our two fixtures, openslide reads
exactly one. **Section 7** revisits the validation strategy.

## 2. Universal task contract

Every task in the v0.7 plan starts with `Step 0: Confirm upstream`,
naming the upstream file path + line range that governs the
behaviour and including a verification command the executor must run
before any production-code edit. Carries forward unchanged from
v0.4 / v0.5 / v0.6.

For BIF, "upstream" means three concurrent references:
1. The Roche BIF whitepaper (canonical for DP 200 only).
2. openslide `src/openslide-vendor-ventana.c` (LGPL 2.1; descriptive
   reference for older iScan slides; **read-for-understanding only,
   never copied verbatim**).
3. The two local fixtures, which together cover both the
   spec-compliant DP 200 path and the older iScan path.

## 3. Fixture inventory

| Fixture | Bytes | Generation | Spec gate | openslide | JPEGTables |
|---------|-------|------------|-----------|-----------|-----------|
| `Ventana-1.bif` | 227 MB | DP 200 (2019, BuildVersion 1.1.0.15854) | passes (`ScannerModel="VENTANA DP 200"`) | **fails** (Direction="LEFT") | absent (per-tile embedded) |
| `OS-1.bif` | 3.6 GB | iScan Coreo (2011, BuildVersion 3.3.1.1) | **fails** (no `ScannerModel`) | passes | present (shared) |

These two fixtures **deliberately cover opposite halves** of the
compatibility matrix. Any v0.7 reader has to handle both — see §4.

Detailed per-IFD probe in `notes/2026-04-27-bif-research.md §2`.

## 4. Scope of accepted slides — Path C (both)

opentile-go accepts **any iScan-tagged BigTIFF** (any TIFF whose
XMP contains an `<iScan>` element). Acceptance is independent of
`ScannerModel`. Internally the reader classifies into one of two
behavioural paths at `Open()` time:

- **Spec-compliant path** when `<iScan>/@ScannerModel` starts with
  the literal prefix `"VENTANA DP"` (current values: `"VENTANA DP
  200"`, `"VENTANA DP 600"`; the prefix future-proofs against new
  DP scanners). Behaviour follows the Roche BIF whitepaper:
  probability-map IFD, ScanWhitePoint filling, AOI-origin metadata,
  all four `Direction` values accepted. Validated against the
  whitepaper + tifffile pixel oracle.
- **Legacy-iScan path** for everything else (missing ScannerModel,
  iScan Coreo, iScan HT, anything not matching the `"VENTANA DP"`
  prefix). Behaviour follows openslide's existing reader.
  Validated against openslide pixel-equivalence.

The two paths share most code; they differ in (a) IFD-1 role
(probability vs. thumbnail), (b) acceptance of `Direction="LEFT/DOWN"`
in `<TileJointInfo>`, (c) ScanWhitePoint-fill on empty tiles. Both
paths produce a fully-working `Tiler`; neither path rejects on
classification.

This is per-fixture verifiable — both `sample_files/ventana-bif/*.bif`
fixtures must round-trip cleanly. Rejecting either is a bug.

**DP 600 caveat.** No DP 600 spec document or fixture is in hand at
v0.7 implementation time. The current spec-compliant path treats DP
600 as identical to DP 200; if a real DP 600 fixture surfaces a
behavioural difference (e.g., Z-stack default, different overlap
distribution, new XMP attributes), it gets pinned to the project's
deviations registry and a follow-up milestone fixes it. The same
caveat applies to any future `"VENTANA DP *"` scanner that picks up
the prefix gate without an explicit validation pass. Tracked as an
active limitation in §10.

## 5. Detection and classification

### 5.1 Vendor detection (run by the format dispatcher)

A TIFF is a BIF candidate iff: (a) BigTIFF, AND (b) at least one
IFD's XMLPacket (tag 700) contains the literal substring
`"<iScan"` (matching opening tag, with optional whitespace).

Mirrors openslide. Discriminates cleanly against our other fixtures:
SVS uses tag 270 ImageDescription, not 700; OME has `<OME` in
ImageDescription/XMLPacket; Philips has `<DataObject ObjectType="DPUfsImport">`;
NDPI is non-BigTIFF.

### 5.2 Generation classification (post-detection)

Once vendor=ventana is established, parse IFD 0's XMP → `<iScan>` →
`ScannerModel`:

- `strings.HasPrefix(scannerModel, "VENTANA DP")` →
  `bif.GenerationSpecCompliant`. Catches `"VENTANA DP 200"`,
  `"VENTANA DP 600"`, and any future `"VENTANA DP *"` scanner. See
  §4 caveat — DP scanners other than DP 200 are unverified against
  fixtures; difference detection is an explicit follow-up.
- everything else (attribute missing, `"VENTANA iScan Coreo"`,
  `"VENTANA iScan HT"`, unknown vendor strings) →
  `bif.GenerationLegacyIScan`.

Generation tagging is internal; the raw `ScannerModel` string is
mirrored verbatim into `Tiler.Metadata()` under
`ventana.scanner_model` for downstream tools that need to
discriminate further.

### 5.3 IFD classification

The whitepaper says IFD 0 = label, IFD 1 = probability, IFD 2 =
scan, IFD 3+ = pyramid. **OS-1 violates this layout** (IFD 0 = label,
IFD 1 = thumbnail, IFD 2..11 = pyramid). Discriminate by
ImageDescription content, not by index:

| ImageDescription | Role |
|------------------|------|
| `Label_Image` (DP 200) or `Label Image` (legacy) | associated image, kind="overview" |
| `Probability_Image` | associated image, kind="probability" *(see §6)* |
| `Thumbnail` | associated image, kind="thumbnail" |
| `level=N mag=M quality=Q` | pyramid level N |
| anything else | warn + skip |

Pyramid levels are sorted by parsed `level=N`, not IFD order — the
whitepaper does not promise IFD order matches level order.

## 6. Probability-map exposure

The whitepaper IFD 1 (DP 200) is an LZW-compressed 8-bit grayscale
*tissue probability map* — a per-pixel floor on whether the scanner
detected tissue at that location. v0.7 surfaces it as an
associated image with `kind="probability"`, joining the existing
`label`, `overview`, `thumbnail`, `macro`, `map` kinds. The slide
author embedded the data; throwing it away is a value loss.
Consumers that don't recognise the new kind skip it gracefully —
the existing `Associated() []AssociatedImage` API already iterates
unknown kinds.

The new kind gets registered in `docs/deferred.md §1a` as a v0.7
deviation from upstream. (Strictly speaking, upstream opentile
doesn't read BIF at all, so this is "v0.7 introduces" rather than
"v0.7 deviates" — but the deviations registry is the canonical
log of *every* opentile-go behaviour with no upstream peer, and that
includes new associated-image kinds.)

## 7. Correctness validation strategy — triple oracle

The CLAUDE.md-stated bar ("pixel-equivalence with openslide on
decoded tiles") only covers OS-1; openslide rejects Ventana-1 on
`Direction="LEFT"`. v0.7 supersedes that bar with three layered
oracles, applied per-fixture according to compatibility:

| Oracle | Builds | Mechanism | OS-1 | Ventana-1 |
|--------|--------|-----------|------|-----------|
| **1. openslide** | `//go:build parity` | `openslide.read_region()` → tile-aligned crop → SHA-256; opentile-go decodes via `internal/jpegturbo` → SHA-256 | ✓ | ✗ (skipped — openslide rejects) |
| **2. tifffile** | `//go:build parity` | `tifffile.TiffFile(...).pages[i].asarray()[y:y+h, x:x+w]` → SHA-256, with serpentine remap applied Go-side before lookup | ✓ | ✓ |
| **3. geometry** | always | tile_count_per_level, tile_dim, JPEG marker validity, pyramid downscale factor ≈ 2×, AOI origin alignment | ✓ | ✓ |

**Caveat shared by oracles 1 and 2.** Neither oracle composes
`<TileJointInfo>` overlap into pixel output (openslide returns the
laid-out grid; tifffile doesn't apply overlap at all). So pixel
oracles validate *raw tile bytes for image-space (col, row)
coordinates*, not the post-overlap appearance. That means our
overlap-metadata exposure (§8) is **not** end-to-end-tested by
oracles 1 or 2. Both our local fixtures record `OverlapX=0`, so the
non-zero overlap path is exercised only by the spec-driven decoder
logic, never by fixture-validated pixels. This is a known limitation
flagged in §10.

Build-tag layout mirrors v0.6:
- `tests/oracle/openslide_bif_test.go` (`//go:build parity`)
- `tests/oracle/tifffile_bif_test.go` (`//go:build parity`)
- `tests/parity/bif_geometry_test.go` (no build tag)

## 8. Tile overlap policy — metadata, on Level

opentile-go's `Level.Tile(c, r) ([]byte, error)` continues to
return raw compressed tile bytes unchanged. Overlap information is
exposed as metadata on the `Level` interface, sufficient for a
client to lay out the raw tiles correctly:

```go
// Level interface (additive — existing methods unchanged):

// TileOverlap returns the pixel overlap between adjacent tiles
// at this level. Tile (c, r) is positioned in image-space at
// (c · (TileSize.X - TileOverlap().X),
//  r · (TileSize.Y - TileOverlap().Y)).
// In the overlap region, tiles further along the row/column
// overwrite earlier tiles (no blending). Returns image.Point{0,0}
// for non-overlapping levels and non-BIF formats.
TileOverlap() image.Point
```

**Why on Level, not Image.** Overlap is per-pyramid-level: only the
base level (IFD 2) has overlap; downsampled pyramid levels (IFD 3+)
are explicitly non-overlapping per the whitepaper. A per-Image
method would force the consumer to know that "overlap only applies
to level 0" as out-of-band knowledge.

**Single-value simplification.** The whitepaper's `<TileJointInfo>`
nodes carry per-tile-pair overlap, which can in principle vary. v0.7
collapses these to a single weighted-average value matching
openslide's existing approach. Justifications: (a) DP 200's spec
mandates `OverlapY=0` always, (b) all our local fixtures record
uniform `OverlapX=0`, (c) downstream consumers expect a regular
grid, (d) per-pair exposure is non-additive complexity that no
known client needs. If a future fixture surfaces meaningfully
varying per-pair overlap, we can add a richer `TileJoints()` method
on a follow-up milestone — `TileOverlap()` remains a valid summary
either way.

**Backward compatibility.** This is an additive method on the
existing `Level` interface (introduced in v0.3, evolved in v0.6).
All existing format implementations grow a `TileOverlap()` method
that returns `image.Point{}`. No caller changes required for
non-BIF formats.

## 9. Implementation outline (assuming recommendations are accepted)

Plan structure mirrors v0.6: 6–7 batches of 4–6 tasks each, each
batch ending with a controller checkpoint. **This section is a
sketch only**; the actual plan doc gets written after §4 / §6 /
§7 / §8 are signed off.

### Phase A — internal plumbing
1. `internal/tiff` XMP tag (700) value retrieval (already supported
   as a generic tag; verify the helper exists or add it).
2. `internal/tiff` IMAGE_DEPTH (32997) tag plumbing — read but
   unused initially (Z-plane support deferred).
3. `internal/bifxml` package — minimal XML tree walker for `<iScan>`
   and `<EncodeInfo>` documents. Stdlib `encoding/xml` is sufficient;
   no need for XPath. Type-safe accessors for the attributes we use.

### Phase B — formats/bif/ skeleton
4. Detection (§5.1) wired into `formats/all` / `bif/openTIler` style
   factory (mirror existing `formats/svs/`, `formats/ome/`).
5. IFD classification (§5.3); pyramid-level ordering by parsed
   `level=N`.
6. Per-level Tile/TileReader implementation: serpentine remap
   (§3 in the notes file) + empty-tile detection + JPEGTables
   composition (both shared and per-tile).
7. ScanWhitePoint-filled blank-tile path for empty offsets (mirrors
   Philips R5).

### Phase C — associated images
8. Label/Probability/Thumbnail dispatch via ImageDescription. Reuse
   existing `internal/tiff` strip-reading machinery.

### Phase D — metadata surface
9. `Tiler.Metadata()` populated with `ventana.*` keys mirroring
   openslide's vendor-properties layout where they exist; spec-only
   attributes (e.g., AOI origins, ScanWhitePoint, overlap counts)
   under `ventana.*` namespace.
10. `Tiler.ICCProfile()` populated from IFD 2 tag 34675.

### Phase E — Level interface integration
11. Extend `Level` interface with `TileOverlap() image.Point` (§8);
    add zero-returning impls to existing SVS / NDPI / Philips / OME
    levels.
12. Per-fixture verification: `Images()` returns one Image; level
    count, dimensions, MPP, TileOverlap match expected values.

### Phase F — parity oracles + tests
13. Oracle 3 (geometry) tests, no build tag.
14. Oracle 2 (tifffile) tests, `//go:build parity`. Both fixtures.
15. Oracle 1 (openslide) tests, `//go:build parity`. OS-1 only.
16. Sampled-tile parity fixtures generated under `tests/`.

### Phase G — docs and deviations
17. Update `docs/deferred.md` §1a with v0.7 deviations:
    - probability-map exposure (if accepted)
    - tile-overlap exposure as metadata (vs upstream — but upstream
      doesn't read BIF, so this is "v0.7 introduces" rather than
      "deviates from")
    - DP 200 ScannerModel non-strict acceptance (we accept legacy
      iScan; spec says don't)
18. New `docs/formats/bif.md` — per-format reader notes (matches
    `docs/formats/ometiff.md` template).
19. CHANGELOG, README format-set update, milestone bump in
    CLAUDE.md.

## 10. Active limitations parked for later milestones

- **Volumetric Z-stacks** (IMAGE_DEPTH). Defer to v0.8+; v0.7
  surfaces only the nominal-focus plane (first M×N tiles per IFD).
  The whitepaper explicitly endorses this fallback for non-Z-aware
  readers.
- **Non-zero tile overlap path is fixture-untested.** Both local
  fixtures have `OverlapX=0`, `OverlapY=0`. The serpentine-decode
  path is fully exercised; the overlap-metadata-population path is
  exercised only by spec-reading and unit tests, never by sampled
  pixel parity. Unblocked by either (a) a real DP 200 slide with
  non-zero overlap, or (b) a hand-constructed synthetic fixture.
- **DP 600 unverified.** v0.7 treats `ScannerModel="VENTANA DP 600"`
  identically to DP 200. No DP 600 spec or fixture is in hand.
  Pinned in CHANGELOG; first DP 600 fixture or spec doc triggers a
  re-validation pass.
- **Color management (ICC profile application).** Out of scope for a
  tile reader; expose via `Tiler.ICCProfile()` only.
- **EncodeInfo Ver < 2 rejection.** Implemented per spec; unverified
  against a fixture (no Ver < 2 fixture available).

## 11. Sign-off log

| Date | § | Decision | Owner |
|------|---|----------|-------|
| 2026-04-27 | 4 | Path C — accept both spec-compliant (DP 200, DP 600) and legacy iScan | Toby |
| 2026-04-27 | 6 | Expose probability map as `kind="probability"` | Toby |
| 2026-04-27 | 7 | Triple oracle: openslide (OS-1) + tifffile (both) + geometry (both) | Toby |
| 2026-04-27 | 8 | Overlap metadata via `Level.TileOverlap() image.Point` | Toby |

Next step: `docs/superpowers/plans/2026-04-27-opentile-go-v07.md`
plan doc follows the v0.6 plan template — 6–7 task batches, 4–6
tasks each, controller checkpoint between batches.

## 12. Appendix — non-obvious gotchas surfaced during research

1. **Spec internal inconsistency on IMAGE_DEPTH tag value**
   (whitepaper page 5 says `0x80BE`, page 6 + Appendix A say
   `0x80E5` = 32997). Use 32997.
2. **Spec-compliant fixture's IFD 0 is not JPEG** (Ventana-1 IFD 0
   has `Compression=1` / NONE despite the whitepaper text saying
   IFD 0 is JPEG). Don't assume IFD 0 compression.
3. **`<TileJointInfo>` `Direction` enum.** Whitepaper allows
   LEFT/RIGHT/UP/DOWN; openslide rejects LEFT/DOWN. Spec-compliant
   slides may carry `LEFT`. opentile-go must accept all four.
4. **OS-1 has no probability map IFD.** Legacy iScan layout differs
   from the spec; classify by ImageDescription content not IFD
   position.
5. **OS-1's IFD 0 is a single 1008×3008 JPEG-tiled "tile"**, not
   striped. The label/macro reader needs to handle both
   strip-based and single-tile JPEG cases.
6. **openslide's reported level dimensions ≠ raw IFD dimensions.**
   For OS-1, openslide reports level 0 = 105813×93951 but the IFD
   shows 118784×102000. The difference is openslide cropping to
   the AOI hull rather than the padded tile rectangle. Need to
   decide whether `Level.ImageSize()` returns padded or cropped
   dimensions. (Suggest: padded, to match the byte-passthrough
   model — empty tiles are still real tiles in the file.)
