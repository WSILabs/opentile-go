# opentile-go v0.5 Design Spec — Philips TIFF support

**Status:** Draft, 2026-04-26.
**Predecessors:** v0.1, v0.2, v0.3, v0.4 (all merged to `main`; v0.4 tagged `v0.4.0`).

## 1. One-paragraph scope

v0.5 adds **Philips TIFF** support as the third format opentile-go
handles, paralleling the v0.2 work that added Hamamatsu NDPI. After
v0.5, opentile-go will cover three of upstream Python opentile
0.20.0's five formats; OME TIFF (R7) is queued for v0.6 and 3DHistech
TIFF (R6) for v0.7.

Philips TIFF is a TIFF-based WSI format used by Philips IntelliSite
Pathology Solution scanners. We have four local fixtures
(`sample_files/phillips-tiff/Philips-{1,2,3,4}.tiff`, 277 MB to 3.1
GB), all open cleanly under Python opentile. Implementation re-uses
the v0.2-onwards NDPI / SVS infrastructure: `internal/tiff` parses
the file, `formats/philips/` adds the format-specific bits, the
parity oracle drives byte-equality with Python opentile across all 4
fixtures.

## 2. Universal task contract

Every task in the v0.5 plan starts with `Step 0: Confirm upstream`,
naming the upstream file path + line range that governs the
behaviour and including a verification command the executor must run
before any production-code edit. Same contract v0.4 introduced and
that drove three meta-findings during execution (T2 Map-page
fixtures, T3 L12 Case D, T4 R4 mechanism audit). Carries forward
unchanged.

## 3. Format quirks worth front-loading

These are the things that distinguish Philips TIFF from SVS / NDPI
and need design attention:

### 3.1 Detection

Upstream test (`tifffile/tifffile.py:10267-10271`):
```python
self.software[:10] == 'Philips DP' and self.description[-16:].strip().endswith('</DataObject>')
```

The `Software` tag (305) starts with literal "Philips DP", AND the
ImageDescription tag is an XML blob ending in `</DataObject>`. Both
checks together give a low false-positive identifier — the closing
tag pins this as the Philips DP DICOM-XML carrier specifically.

### 3.2 Image dimensions need post-parse correction

Philips's pyramid levels have **incorrect `ImageWidth` / `ImageLength`
tags on disk** for every level past the baseline. Tifffile
transparently fixes this in `_philips_load_pages`
(`tifffile.py:6477-6535`):

1. Read the base page's true dimensions and the DICOM_PIXEL_SPACING
   attribute series from the ImageDescription XML.
2. The first DICOM_PIXEL_SPACING entry sets the baseline scale.
3. Each subsequent entry gives the relative pixel-spacing ratio for a
   reduced level; the on-disk `imagewidth` / `imagelength` are
   replaced with `ceil(base / ratio)`.

The on-disk values are essentially placeholders — using them
unmodified produces wrong pyramid geometry.

**Implementation choice** for our port: don't mutate IFD entries in
`internal/tiff` (architectural invariant: format-specific quirks live
in format packages). Instead, `formats/philips/` exposes corrected
dimensions via a wrapper struct that holds the page reference plus an
override `Size`. The wrapper is what the rest of the package sees.

### 3.3 Sparse tiles

Philips levels can have tiles whose `TileByteCounts[idx] == 0`, but
unlike the SVS corrupt-edge case (R4), this is **expected
behaviour** — the slide simply doesn't have data for that tile,
typically because the scanner skipped a region of background. Upstream
substitutes a "blank tile" derived from the first valid frame on the
same page, with all DCT coefficients overwritten to produce a
solid-colour (white) output:

```python
# opentile/formats/philips/philips_tiff_image.py:143-173
def _create_blank_tile(self, luminance=1.0):
    valid_frame_index = next(i for i, n in enumerate(databytecounts) if n != 0)
    tile = self._read_frame(valid_frame_index)
    if jpegtables: tile = Jpeg.add_jpeg_tables(tile, jpegtables, False)
    tile = self._jpeg.fill_frame(tile, luminance)
    return tile
```

`Jpeg.fill_frame` is a libjpeg-turbo tjTransform with a CUSTOMFILTER
that overwrites every DCT coefficient (not just OOB blocks like our
existing `CropWithBackground`). New machinery for our cgo wrapper —
distinct enough from `Crop` / `CropWithBackground` to warrant its own
entry point. Calling it `FillFrame`.

The blank tile is computed once per level (cached on first sparse
hit), then returned for every sparse position thereafter.

### 3.4 Series classification

Upstream's `_is_*_series` predicates (`philips_tiff_tiler.py:111-137`)
do **substring search on each series's first-page ImageDescription**:
"Macro" → overview, "Label" → label, "Thumbnail" → thumbnail. Index 0
is always the level series. No positional shortcuts; entirely
description-driven. Our port mirrors this directly — no
classification ambiguity to worry about.

### 3.5 Metadata is DICOM-XML in the ImageDescription

Unlike Aperio's pipe-delimited `key=value` description and NDPI's
binary tag soup, Philips puts a full **DICOM-attribute XML document**
in the ImageDescription tag:

```xml
<DataObject>
  <Attribute Name="DICOM_PIXEL_SPACING" Group="..." Element="...">"0.000247746" "0.000247746"</Attribute>
  <Attribute Name="DICOM_ACQUISITION_DATETIME" ...>20210101120000.000</Attribute>
  <Attribute Name="DICOM_MANUFACTURER" ...>PHILIPS</Attribute>
  ...
</DataObject>
```

The XML is namespaceless (no `xmlns` declarations beyond what's
implicit), which means stdlib `encoding/xml` works out of the box.
First v0.5 use of `encoding/xml` in our codebase; a small new
dependency on the standard library.

## 4. Themes

### Theme A — JIT verification gates (mirror v0.4)

| # | What | Decides |
|---|---|---|
| T1 | Confirm `is_philips` matches our 4 fixtures | Detection works on real files |
| T2 | `tjTransform`-with-blank-CUSTOMFILTER determinism check | Whether `FillFrame` byte-parity is achievable |
| T3 | DICOM XML schema audit across 4 fixtures | Parser needs to handle full or subset |

### Theme B — Plumbing

| What | Where |
|---|---|
| `is_philips` detection | `internal/tiff` exposes a `Software()` accessor; format detection in `formats/philips/Factory.Supports` |
| Image-dimension correction | `formats/philips/dimensions.go` — reads DICOM_PIXEL_SPACING entries, computes corrected `(W, H)` per level |
| `internal/jpegturbo.FillFrame` | New cgo entry point; mirrors upstream's `JpegFiller` |

### Theme C — Format package

| File | Responsibility |
|---|---|
| `formats/philips/philips.go` | Factory + Open + classifier wiring |
| `formats/philips/series.go` | Tifffile-style series classification (Baseline / Label / Macro / Thumbnail) |
| `formats/philips/tiled.go` | Pyramid-level Image (sparse-tile aware) |
| `formats/philips/associated.go` | Label / Macro / Thumbnail AssociatedImage |
| `formats/philips/metadata.go` | DICOM-XML parser via stdlib `encoding/xml` |

### Theme D — Test surface

| What | Where |
|---|---|
| Unit tests | Per-file `*_test.go` mirroring SVS / NDPI patterns |
| Integration | `tests/integration_test.go` slideCandidates extended with the 4 Philips fixtures |
| Parity oracle | `tests/oracle/parity_test.go` slideCandidates extended; runner already supports `kind="label"/"overview"/"thumbnail"` (Philips uses the same kinds) |
| Fixtures | `tests/fixtures/Philips-{1,2,3,4}.json`, sampled mode for the bigger ones |

### Theme E — Polish + ship

Mirrors v0.4's closing batch. Retirement audit (R5 → ✅), README +
CLAUDE.md milestone bump, final `make cover` / `go vet` / `-race`
sweep, `make parity`, tag v0.5.0.

## 5. Out-of-scope (deferred)

- **OME TIFF** (R7) — v0.6. Drafted as its own plan after v0.5 ships,
  informed by what v0.5 teaches us.
- **3DHistech TIFF** (R6) — v0.7. Smallest upstream port (~200 LOC);
  needs a real fixture sourced (the `sample_files/mrxs/` zips are
  multi-file MRXS, a different format upstream doesn't support).
- **R4 / R9** (SVS corrupt-edge reconstruct + JP2K decode/encode) —
  parked at [#1](https://github.com/wsilabs/opentile-go/issues/1)
  pending a real corrupt-edge slide.
- **Map pages on Philips** — Philips doesn't have a Map-page
  equivalent (no upstream `_is_map_series` predicate). The
  `KindMap` we added in v0.4 stays NDPI-specific.

## 6. Forward-looking hooks for v0.6 (OME)

These are things v0.5 can do cheaply that will make v0.6 easier:

- **`encoding/xml` patterns.** v0.5 introduces XML parsing via stdlib
  for DICOM-attribute documents. OME TIFF uses a more complex
  XML schema (OME-XML) but the underlying parsing approach
  generalises. Concretely: keep the XML parsing helpers in a small
  `internal/wsixml/` (or similar) package if the patterns repeat,
  vs. inlining in `formats/philips/metadata.go`. Decide based on
  what the v0.5 implementer actually needs.
- **Wrapper-page pattern.** v0.5 introduces "page with corrected
  dimensions" wrappers. OME may also need wrappers for sub-IFD
  pyramid levels (it stores reduced resolutions in TIFF SubIFDs,
  not as top-level IFDs). The wrapper API shape v0.5 lands could
  be reusable.
- **Multiple associated-image kinds with description-substring
  classifiers.** SVS, Philips, and OME all classify by substring
  match. v0.5 may surface a shared helper if the patterns are
  redundant; if not, leave the duplication and revisit in v0.6.

None of these are mandatory for v0.5 done-when. They're notes for
when v0.6 starts.

## 7. Branch + workflow

- Branch: `feat/v0.5` from `main` after merging `0a13be7` (the v0.4
  merge commit).
- Spec: this document.
- Plan: `docs/superpowers/plans/2026-04-26-opentile-go-v05.md`
  (same commit).
- Execution: same `superpowers:subagent-driven-development` pattern
  v0.4 used. Universal `Step 0: Confirm upstream` enforced by
  reviewers.

## 8. Done-when

- All 4 Philips fixtures open cleanly, produce levels + label +
  overview + thumbnail.
- Output is byte-identical to Python opentile 0.20.0 on every
  sampled tile and every associated image we expose, across all 4
  Philips fixtures (oracle slate extended).
- `TestSlideParity` green on the existing 8 fixtures + 4 new ones.
- `make cover` clears 80% per package; new `formats/philips/`
  package included in the gate.
- R5 marked ✅ landed in `docs/deferred.md §1`. New `Retired in
  v0.5` subsection in §6 paralleling §5 (v0.3) and the
  v0.4 retirements.
- `CHANGELOG.md` gains a `[0.5.0]` entry.
- README + CLAUDE.md reflect v0.5 as the current milestone.
