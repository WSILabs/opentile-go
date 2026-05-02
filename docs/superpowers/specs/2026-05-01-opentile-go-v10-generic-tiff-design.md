# opentile-go v0.10 Generic TIFF Design Spec

**Status:** Sealed, 2026-05-01. Sole focus of v0.10: a catch-all
reader for **tiled pyramidal TIFF without vendor metadata**, registered
last in dispatch so it activates on any TIFF that no vendor format
claims. Closes the "we have a tiled WSI TIFF but no specific format
detector for it" gap.

**Predecessors:** v0.1 – v0.9. Public API stable from v0.3; v0.9
shipped the perf milestone (mmap default, TileInto, WarmLevel,
splice template).

**Reference doc:** None — this is a new format in opentile-go's
own design space, motivated by the existence of vendor-stripped
generic tiled pyramidal TIFFs that the existing five vendor
detectors reject.

---

## 1. One-paragraph scope

A new `formats/generic/` package implementing a "**uint8 RGB /
YCbCr / grayscale tiled pyramidal TIFF reader**." Detection runs
LAST in the dispatch order (catch-all); accepts any classic or
BigTIFF whose top-level IFDs include ≥3 tiled pages forming a
coherent pyramid (consistent scale ratios within tolerance).
Pyramid levels must be tiled; **associated images can be
stripped** (single-strip and multi-strip handled via existing
`internal/tifflzw` and `internal/oneframe` reusable code paths).
Heuristic-based associated-image classification with
`"associated"` fallback Kind value. Full metadata pattern via
`generic.MetadataOf(tiler)`. JPEGTables splice reuses v0.9's
in-place `internal/jpeg.InsertPrefixInPlace`.

## 2. Universal task contract: "confirm upstream first"

Same as v0.4 – v0.9: every plan task starts with `Step 0: Confirm
upstream`. For v0.10 generic TIFF, "upstream" is layered:

1. **TIFF 6.0 spec** + **TIFF Tech Notes** for tag semantics — the
   authoritative source on what each tag means and what dimension /
   compression / photometric values are valid.
2. **`internal/tiff`** — opentile-go's own TIFF parser. The format
   reader uses its existing accessors; doesn't introduce new
   low-level parsing.
3. **CMU-1.tiff** — the only real fixture. Validates the pyramid
   reader path; exercises one specific encoder's choices.
4. **vips-emitted reference fixture(s)** (generated during v0.10
   work) — validate against what real-world WSI pipelines actually
   produce.
5. **Existing vendor format readers** — `formats/svs/`, `formats/ome/`,
   `formats/philips/`, `formats/bif/` — each has detection +
   pyramid-level + associated-image patterns we mirror.

When the spec, the parser, and the real fixture disagree, **trust
the real fixture**, document the deviation, file the encoder bug
upstream if it's not us. (Per v0.5 lesson: real fixtures > spec
prose.)

---

## 3. Architectural foundations (sealed)

| § | Decision |
|---|----------|
| 3.1 | **Additive evolution.** New format package; new
exported `"associated"` Kind value (additive to the existing
taxonomy). No breaking changes to existing format readers or
public API. |
| 3.2 | **Catch-all dispatch.** Generic factory registered LAST in
`formats/all/`. Activates only when every vendor factory's
`Supports()` returns false. Avoids stealing files that a specific
format reader should claim. |
| 3.3 | **Reuse existing infrastructure.** JPEGTables splice via
v0.9's `BuildSplicePrefix` + `InsertPrefixInPlace`; multi-strip
LZW via v0.5's `internal/tifflzw`; multi-strip JPEG associated via
v0.6's `internal/oneframe`. No new decode paths in v0.10 — only
new wiring + heuristics + dispatch. |
| 3.4 | **No new cgo.** Same constraint as v0.9. The IFE-derived
discipline holds. |

---

## 4. Detection + pyramid validation

The factory's `Supports(*tiff.File) bool` is the gate. Steps:

1. **Filter to tiled IFDs.** A page qualifies as a candidate
   pyramid level iff `TileWidth` (322) and `TileLength` (323) tags
   are both present and non-zero.
2. **Sort by area, descending.** Largest tiled IFD = candidate
   baseline (level 0).
3. **Validate ≥3 candidates** (Q2). Fewer → reject.
4. **Validate scale ratios** (Q1):
   - For each consecutive pair (L[i], L[i+1]):
     - `ratio_W = W[i] / W[i+1]`, `ratio_H = H[i] / H[i+1]`
     - **Inter-axis** (within transition): `|ratio_W - ratio_H| / ratio_W ≤ 0.02` (±2%)
     - Both ratios must be > 1 (decreasing dims)
   - For all transitions:
     - **Inter-level** (across transitions): `|ratio[i] - ratio[i+1]| / ratio[i] ≤ 0.05` (±5%)
   - Any check failure → reject the offending IFD; if doing so
     drops the candidate set below 3, reject the file
5. **Validate uint8 RGB / YCbCr / grayscale** on every accepted
   candidate:
   - `BitsPerSample` (258) = 8 (all samples)
   - `SampleFormat` (339) = 1 (unsigned int) or absent (default 1)
   - `PhotometricInterpretation` (262) ∈ {1 (MinIsBlack-grayscale),
     2 (RGB), 6 (YCbCr)}
   - Reject palette (3), CMYK (5), MinIsWhite (0), bit-depth ≠ 8
6. **Validate compression** ∈ {1 (None), 5 (LZW), 7 (JPEG), 8
   (Deflate / Adobe Deflate), 33003 (JPEG 2000)}. Reject everything
   else (JBIG, CCITT, PackBits, ZSTD, etc.).
7. **Multi-pyramid rejection** (Q7): if the validated pyramid does
   not consume **all** tiled IFDs in the file (i.e., there are
   leftover tiled IFDs that don't fit the scale chain), reject.
   This forces multi-pyramid TIFFs back to the OME factory or
   leaves them unsupported. Tiled IFDs that aren't part of the
   pyramid → could be associated images, *but* only if there's
   exactly one or two of them at small dimensions; ambiguous
   cases reject the file.

If all checks pass: `Supports() = true`. The validator's results
(pyramid IFDs in order, leftover IFDs for associated classification)
are computed once and cached for `Open()` to reuse.

---

## 5. Pyramid level reads

Level construction reuses v0.9's machinery:

- **`tiledImage` struct** mirrors `formats/svs/tiled.go`'s shape:
  cached `offsets`, `counts`, `jpegTables`, `splicePrefix`,
  `maxTileSize`, plus the tile dimensions + grid + compression +
  pyramid index.
- **`Tile()`**: same shape as SVS — read tile bytes, splice if
  JPEGTables present (no APP14 — generic TIFFs aren't Aperio).
- **`TileInto()`**: zero-alloc on no-splice path; reuses
  `internal/jpeg.InsertPrefixInPlace` on splice path. Same as
  Philips / OME tiled / BIF.
- **`TileMaxSize()`**: cached upper bound = `max(counts) +
  len(splicePrefix)`.
- **`warm()`**: walks offsets/counts via `tiff.TouchPages`. Wired
  through `Tiler.WarmLevel(i)` (v0.9 mechanism).

JP2K compression: tile bytes are self-contained codestreams, no
splice. Same passthrough as SVS JP2K.

LZW / Deflate / None compression: tile bytes are raw stripes / raw
pixels in tiled layout. `Tile()` returns the bytes verbatim — the
consumer gets exactly what's on disk.

---

## 6. Associated-image classification + reads

Non-pyramid IFDs (everything that the validator didn't claim as a
pyramid level) become `Tiler.Associated()` candidates. The
classifier runs heuristics in order; first match wins:

| Heuristic | Maps to `Kind()` |
|---|---|
| Stripped + **LZW** (compression 5) + dims < ~1500×1500 | `"label"` (LZW is the canonical SVS-style label compression) |
| Stripped + JPEG + aspect ratio (max(W,H)/min(W,H)) ≥ 2.0 + larger dim ≥ 1000 | `"macro"` (very-wide JPEG = SVS-style overview) |
| Stripped + JPEG + dims < ~1500×1500 (aspect roughly square) | `"thumbnail"` |
| Tiled-but-not-pyramid + area < 1% baseline + medium dims | `"macro"` |
| Tiled + tiny (area < 0.001 baseline) | `"thumbnail"` |
| Anything else passing the photometric/compression filters from §4 | `"associated"` (Q5 fallback) |

**Heuristic revision history:** initial spec (2026-05-01) had
`width > height` as part of the "label" rule. T2 fixture probe
(2026-05-02) found CMU-1.svs's label is 387×463 — **portrait, not
landscape**. SVS labels are slide-orientation-dependent and aren't
reliably one or the other; the strongest classification signals
turn out to be **compression** (LZW = label) and **aspect-ratio
extreme** (very-wide JPEG = macro). Heuristics rewritten
accordingly.

**`"associated"` is a new Kind value** added to opentile-go's
existing taxonomy (`"label"` / `"overview"` / `"thumbnail"` /
`"macro"` / `"map"` / `"probability"`). Documented in `image.go`'s
`AssociatedImage.Kind()` docstring as "format reader couldn't
classify the image; bytes are still readable."

**Reading associated bytes** dispatches by data shape:

| Shape | Reader path | Reuses |
|---|---|---|
| Single-strip uncompressed/JPEG/LZW/Deflate | Trivial passthrough — read the strip bytes | direct |
| Multi-strip uncompressed | Concatenate strip bytes in offset order | direct |
| Multi-strip LZW | Decode each strip via `internal/tifflzw`, concatenate raw pixels, re-encode as single LZW | reuse `internal/tifflzw` (v0.5 SVS label path) |
| Multi-strip JPEG (single-channel) | One-frame-from-multi-strip JPEG via `internal/oneframe` | reuse `internal/oneframe` (v0.6) |
| Tiled associated images | Read tile bytes via the same generic tile-read path | reuse generic tile reader |

**Out of scope for v0.10:**
- Multi-strip JPEG with `PlanarConfiguration=2` (OME-specific
  quirk — `formats/ome/` handles it; `formats/generic/` rejects)
- ICC profile decoding for associated images (ICC bytes from the
  pyramid IFD are exposed via `Tiler.ICCProfile()`; per-image ICC
  is rare)

---

## 7. Metadata + ICC profile

Per Q8 (full pattern):

- **`Tiler.Metadata()`** populates the cross-format `Metadata`
  struct from standard TIFF tags:
  - `Make` (271) → `ScannerManufacturer`
  - `Model` (272) → `ScannerModel`
  - `Software` (305) → `ScannerSoftware []string` (split on
    semicolons / newlines if delimited)
  - `DateTime` (306) → `AcquisitionDateTime` (TIFF format
    `"YYYY:MM:DD HH:MM:SS"`; `time.Time{}` if unparseable)
  - Magnification: not in standard TIFF; left as 0
- **`Tiler.ICCProfile()`** reads tag 34675 (ICCProfile) verbatim
  from the level-0 IFD.
- **`generic.MetadataOf(tiler) (*generic.Metadata, bool)`**
  exposes IFE-style format-specific extras:
  - `MicronsPerPixel` derived from `XResolution` (282) +
    `ResolutionUnit` (296). ResolutionUnit values: 1=none (no
    conversion possible — return 0), 2=inch (convert via
    25400 µm/inch), 3=cm (convert via 10000 µm/cm).
  - `ImageDescription` (270) verbatim — generic TIFFs may carry
    free-form text here from the encoder.
  - `Compression` per pyramid level (already on `Level`).
  - `RawTags map[uint16][]byte` — raw bytes of any tag the consumer
    wants to inspect, keyed by TIFF tag number. Out of scope for
    v0.10 if it adds significant complexity.

---

## 8. Test fixtures

Per Q9 (hybrid):

**Real fixture (committed gitignored under `sample_files/generic-tiff/`):**
- `CMU-1.tiff` (195 MB) — the user's existing fixture. 9-level
  tiled JPEG pyramid (46000×32914 → 179×128), no associated images.
  Validates the pyramid reader path; doesn't exercise associated
  classification.

**vips-emitted reference fixture (T1 finding: drops associated
images):**
- `vips tiffsave CMU-1.svs /tmp/x.tiff --tile --pyramid` produces
  a structurally-identical-to-CMU-1.tiff output: 9-level tiled
  JPEG pyramid, **no associated images** (vips ignores SVS-
  specific IFDs). Doesn't add new coverage beyond CMU-1.tiff.
  Skipped.

**Stripped-SVS reference fixture (sealed 2026-05-01 mid-design):**
- A small Python script reads `CMU-1-Small-Region.svs`, mutates
  IFD 0's `ImageDescription` to remove the `"Aperio"` prefix, and
  writes the bytes back as `CMU-1-Small-Region.stripped.tiff`. All
  IFDs preserved verbatim (pyramid + label + macro + thumbnail).
  Result: structurally identical to a real SVS but no longer
  claims SVS-format to opentile-go's detector → falls through to
  the generic factory. Validates the heuristic classifier against
  realistic SVS-derived associated-image shapes (multi-strip LZW
  label, single-strip JPEG macro, single-strip uncompressed
  thumbnail). ~1.9 MB; committed under `sample_files/generic-tiff/`
  (gitignored alongside the other binary fixtures).

**Synthetic fixtures (committed under `sample_files/generic-tiff/synth/`,
generated via Python tifffile):**
- `synth-pyramid-jpeg.tiff` — minimal 3-level tiled JPEG pyramid,
  no associated. Smoke fixture.
- `synth-pyramid-with-label.tiff` — 3-level tiled JPEG pyramid +
  multi-strip LZW label (mimics SVS-derived structure).
- `synth-pyramid-with-thumb-macro.tiff` — 3-level pyramid + single-
  strip JPEG macro + single-strip uncompressed RGB thumbnail.
- `synth-bad-pyramid.tiff` — 3 tiled IFDs whose scale ratios DON'T
  match (validator must reject).
- `synth-stripped-only.tiff` — only stripped IFDs, no tiled
  (validator must reject).

A `sample_files/generic-tiff/regen.py` script holds the tifffile
emission code so future contributors can regenerate or extend the
synthetic set.

**Hand-rolled byte buffers in Go test code:**
For tight unit tests where a full TIFF is overkill — e.g.,
testing the inter-axis ratio check directly with synthetic IFD
metadata. ~50-100 LoC in `formats/generic/validator_test.go`.

---

## 9. Active limitations parked for later milestones

These are scoped out for v0.10. Re-triage with §11 backlog post-ship:

- **Stripped pyramid IFDs** (Q4) — v0.11+ if a real-world stripped-
  pyramid TIFF surfaces with a consumer asking.
- **Multi-pyramid generic TIFFs** (Q7) — same; OME covers most
  real-world cases.
- **Pluggable associated-image classifier** (Q6) — `WithAssociatedClassifier`
  Option lands when a consumer needs vendor-aware overrides.
- **Multi-strip JPEG with `PlanarConfiguration=2`** for associated
  images — OME-specific quirk; `formats/generic/` rejects in v0.10.
- **Tiled associated images** — covered by the heuristics if
  size/ratio matches, but `Kind()` may default to `"associated"`
  rather than a specific kind.

These get registered under `docs/deferred.md §2` as L26+ entries
during T11, and consolidated into §11 for post-v0.10 re-triage.

---

## 10. Sign-off log

| Date | § | Decision | Owner |
|------|---|----------|-------|
| 2026-05-01 | 1 | Scope: `formats/generic/`, catch-all, tiled pyramidal TIFF only | Toby |
| 2026-05-01 | 4 Q1 | Scale tolerance: ±2% inter-axis, ±5% inter-level | Toby |
| 2026-05-01 | 4 Q2 | Minimum pyramid: ≥3 levels | Toby |
| 2026-05-01 | 4 Q3 | Tile size consistency: preferred, not required | Toby |
| 2026-05-01 | 4 Q4 | Stripped pyramid IFDs excluded | Toby |
| 2026-05-01 | 6 Q5 | New `"associated"` Kind value as fallback | Toby |
| 2026-05-01 | 6 Q6 | Deterministic heuristics; no override Option | Toby |
| 2026-05-01 | 4 Q7 | Multi-pyramid generic TIFFs rejected | Toby |
| 2026-05-01 | 7 Q8 | Full metadata pattern: `generic.Metadata` + `MetadataOf` | Toby |
| 2026-05-01 | 8 Q9 | Hybrid fixture strategy: tifffile (synth) + vips (real-world) + Go hand-rolled (unit) | Toby |
| 2026-05-01 | 6 | Multi-strip allowed for associated images (Tier 1+2) | Toby |
