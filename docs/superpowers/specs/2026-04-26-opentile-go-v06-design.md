# opentile-go v0.6 Design Spec — OME TIFF support

**Status:** Draft, 2026-04-26.
**Predecessors:** v0.1, v0.2, v0.3, v0.4, v0.5 (all merged to `main`; v0.5 tagged `v0.5.0`).

## 1. One-paragraph scope

v0.6 adds **OME TIFF** support as the fourth format opentile-go
handles. After v0.6, opentile-go covers four of upstream Python
opentile 0.20.0's five formats (the fifth, 3DHistech TIFF, is parked
behind GH issue #2 — never encountered in the wild). With v0.6 done,
opentile-go has complete upstream coverage modulo parked formats; v0.7
(BIF) ventures beyond upstream. The OME port introduces three new
elements not present in prior format work: **SubIFD pyramid levels**
(reduced levels live in TIFF SubIFDs of the base page, not as
top-level IFDs); **OME-XML metadata parsing** (a namespaced XML schema
carrying `<Image>/<Pixels>` elements); and a **first-class multi-image
public API** — `Tiler.Images()` + new `Image` interface — so OME files
that carry several main pyramids (Leica-2.ome.tiff has 4) expose all
of them rather than silently dropping all but one. The new Image API is
additive: SVS / NDPI / Philips return a single-element `Images()`
slice and existing `Tiler.Levels()` / `Level(i)` keep working as
shortcuts to `Images()[0]`.

## 2. Universal task contract

Every task in the v0.6 plan starts with `Step 0: Confirm upstream`,
naming the upstream file path + line range that governs the
behavior and including a verification command the executor must run
before any production-code edit. Same contract v0.4 / v0.5 used.
Carries forward unchanged.

## 3. Public API extension — `Image` interface

The biggest new design call in v0.6. We need to expose multiple main
pyramids per Tiler for OME multi-image files. The shape:

```go
// Image represents one main pyramid in a Tiler. Single-image formats
// (SVS, NDPI, Philips) expose exactly one Image; OME-TIFF can expose
// multiple. Within an Image the Levels are ordered from highest
// resolution (Index 0 = baseline) downwards.
type Image interface {
    Index() int                     // 0-based document order
    Name() string                   // OME-XML Image Name (or "" for non-OME)
    Levels() []Level
    Level(i int) (Level, error)
    MPP() SizeMm                    // base-level microns/pixel
}

type Tiler interface {
    Format() Format
    Images() []Image                // NEW — always >= 1 entry
    Levels() []Level                // shortcut for Images()[0].Levels()
    Level(i int) (Level, error)     // shortcut for Images()[0].Level(i)
    Associated() []AssociatedImage
    Metadata() Metadata
    ICCProfile() []byte
    Close() error
}
```

**Why this shape:**

- **Additive.** Existing callers using `Levels()` / `Level(i)` keep
  working unchanged — those become shortcuts to `Images()[0]`.
- **Deliberate divergence from upstream.** Python opentile's "one
  level series per file" assumption silently drops 3 of 4 main
  pyramids in Leica-2.ome.tiff via an unintentional last-wins loop
  in its base `Tiler.__init__`. Encoding that in our port would bake
  in an upstream oversight. Exposing all images is the correct
  design.
- **Cheap retrofit for prior formats.** SVS/NDPI/Philips Tiler
  implementations gain a trivial `Images()` returning a one-element
  slice wrapping their existing single pyramid.

The `Image` interface is in opentile package (`image.go` or new
`image_iface.go`). The single-Image wrapper for SVS/NDPI/Philips lives
inside each format's tiler.

### 3.1 Backwards compatibility

`Tiler.Levels()` and `Tiler.Level(i)` are unchanged — they delegate to
`Images()[0]`. No existing call site breaks.

`Tiler` interface gains a method (`Images()`), which is technically a
breaking change for any external Tiler implementer. We have none; the
v0.3 "API stable" rule covers user-visible names, not internal
implementer surface. Documented as part of v0.6 deviation notes.

## 4. Format quirks worth front-loading

### 4.1 Detection

Upstream test (`tifffile/tifffile.py`):
```python
self.is_ome  # pages[0].description starts with the OME-XML root
```

Concretely, the first page's ImageDescription begins with `<?xml`,
contains `<OME ` (with the namespace decl), and the namespace string
`http://www.openmicroscopy.org/Schemas/OME/` — both 2 of our fixtures
satisfy this. Detection rule for our port: ImageDescription contains
`<OME ` AND the OME namespace URL.

### 4.2 SubIFD pyramid levels

OME TIFF stores reduced-resolution pyramid levels as **TIFF SubIFDs**
of the base page rather than as top-level IFDs (the SVS / NDPI /
Philips pattern). Concretely the base page carries a `SubIFDs` tag
(TIFF tag 330) whose value is an array of offsets to child IFD
blocks; each child IFD is a complete pyramid level.

For our 2 fixtures (BigTIFF):

- Leica-1.ome.tiff: page 0 = macro (no SubIFDs); page 1 = base level
  with 4 SubIFD offsets → 5 pyramid levels under page 1.
- Leica-2.ome.tiff: 5 series total — series 0 macro, series 1-4 four
  separate WSI pyramids, each with 5 SubIFDs (4 reduction levels).

`internal/tiff` does not currently parse SubIFDs. The plan adds:

- `TagSubIFDs uint16 = 330`.
- `Page.SubIFDOffsets() ([]uint64, bool)` accessor on `tiff.Page`.
- A way to construct a `tiff.Page` from a SubIFD offset (analogous to
  the existing top-level IFD walker).

Architectural invariant preserved: SubIFD parsing is generic TIFF
(documented in TIFF 6.0 spec), so it lives in `internal/tiff`.
Format-specific *use* of SubIFDs lives in `formats/ome/`.

### 4.3 OneFrame (non-tiled) levels

Both fixtures have a low-resolution level whose page is **stripped,
not tiled** (single-strip JPEG, virtualized into tile cells). Python
opentile's `OmeTiffOneFrameImage` directly extends `NdpiOneFrameImage`:

```python
class OmeTiffOneFrameImage(NdpiOneFrameImage, LevelTiffImage):
    """Some ome tiff files have levels that are not tiled, similar
    to ndpi."""
```

Our `formats/ndpi/oneframe.go` (~260 LOC) implements the NDPI
variant. Plan-time T3 gate diffs the NDPI implementation against
what's needed for OME; if ≥80% is shared, factor into
`internal/oneframe/`; otherwise copy into `formats/ome/oneframe.go`
with comments referencing the NDPI sibling. Either is acceptable;
factoring buys clarity if it works cleanly, and v0.7 (BIF) likely
needs the same machinery so the factored package would amortize.

### 4.4 OME-XML metadata

Both fixtures carry an OME-XML document in page 0's ImageDescription:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06" ...>
  <Image ID="Image:0" Name="macro">
    <Pixels PhysicalSizeX="16.43..." PhysicalSizeXUnit="µm"
            PhysicalSizeY="16.43..." PhysicalSizeYUnit="µm"
            SizeX="1616" SizeY="4668" .../>
  </Image>
  <Image ID="Image:1" Name="">
    <Pixels PhysicalSizeX="0.5..." PhysicalSizeXUnit="µm" .../>
  </Image>
  ...
</OME>
```

We parse via stdlib `encoding/xml` (the v0.5 Philips DICOM-XML
walker showed the namespace + element-attribute pattern works).
Fields used:

- **`<Image Name="...">`**: classifies the series. `"macro"`/`"label"`/
  `"thumbnail"` go to associated; everything else (including empty)
  is a main pyramid Image.
- **`<Pixels PhysicalSizeX>`/`<PhysicalSizeY>`** in MICROMETER units:
  microns per pixel for the corresponding image. Used for `Image.MPP()`.
- **`<Pixels SizeX>`/`<SizeY>`**: image dimensions in pixels;
  cross-check vs. the page's TIFF ImageWidth/ImageLength.

Upstream's `metadata` property returns an empty `Metadata()` ("Metadata
parsing not implemented for OmeTiff"). Our `Tiler.Metadata()` matches
that for byte-parity. Per-image MPP / OME-specific fields surface via
`formats/ome.MetadataOf(t)` (paralleling `philips.MetadataOf`).

## 5. Parity strategy — opentile-py + tifffile

For prior formats, byte-parity-with-Python-opentile was a clean
correctness bar because opentile-py exposed every tile our Go side
exposed. For OME multi-image files this breaks: opentile-py drops 3
of 4 main pyramids in Leica-2 due to the last-wins loop. Two
references work better than one:

- **opentile-py (primary, post-splice)**: for the single image in
  Leica-1 and the LAST main pyramid + macro in Leica-2 — what
  opentile-py exposes. Uses the existing parity oracle infrastructure
  unchanged.
- **tifffile (secondary, pre-splice + Python-side splice)**: for the
  3 dropped pyramids in Leica-2 (and as a sanity-check second axis
  for Leica-1). The oracle reads raw tile bytes via tifffile's
  `dataoffsets`/`databytecounts` and splices JPEGTables in Python
  using the same logic opentile-py uses internally. Result is
  byte-equivalent to what opentile-py *would* produce if it exposed
  the dropped images. Same Python venv we already use; ~30 LOC of
  new oracle script.

If both references agree (where both exist), and tifffile-only parity
holds for the dropped images, we have a strictly stronger correctness
bar than current SVS/NDPI/Philips parity.

Bio-Formats / libvips are deferred — they operate at decoded-pixel
level (pixel-equivalence, not byte-equivalence) and are reserved for
v0.7+ formats where compressed-byte references run out.

## 6. Multi-image OME deviation — make it visible

This is a deliberate divergence from upstream Python opentile, the
first such intentional one (NDPI Map pages from v0.4 were paralleling
tifffile's behavior, which upstream just didn't surface). The
divergence needs to be discoverable:

1. **`docs/deferred.md` §1 R7 entry**: status note that v0.6 ships
   with multi-image exposure beyond upstream.
2. **`docs/deferred.md` new section "Deviations from Python opentile"**:
   canonical list, becomes the source of truth for README + future
   deviation entries.
3. **README**: new top-level **"Deviations from upstream Python
   opentile"** section listing existing deviations (L14 NDPI label
   synthesis, R13 NDPI Map pages) plus the new multi-image OME
   exposure. Pulls from §6 of `deferred.md`.
4. **CHANGELOG.md `[0.6.0]` entry**: deviation called out in the
   "Added" / "Differs from upstream" section.

## 7. Themes

### Theme A — JIT verification gates

| # | What | Decides |
|---|---|---|
| T1 | `is_ome` detection across our 2 fixtures + 0 false positives | Detection works on real files |
| T2 | SubIFD parsing audit — verify offsets reachable + decodable as IFDs; cross-check vs. tifffile's reported pyramid | Sets the `internal/tiff` extension's correctness bar |
| T3 | OneFrame factor-or-copy decision | Decides plumbing approach for §4.3 |
| T4 | OME-XML schema audit across both fixtures — Image Name values, PhysicalSize units, missing-field tolerance | Drives the metadata parser's edge cases |
| T5 | tifffile splice-replication harness — confirm tifffile-raw-bytes + Python-side JPEGTables splice == opentile-py output on Leica-1 | Validates the dual-reference parity strategy |

### Theme B — Plumbing

| What | Where |
|---|---|
| `TagSubIFDs` + `Page.SubIFDOffsets()` | `internal/tiff/page.go` |
| Construct `Page` from SubIFD offset | `internal/tiff/file.go` (expose existing IFD-walker machinery) |
| `Image` interface + single-image wrappers in SVS/NDPI/Philips | `image.go` (or new `image_iface.go`); `formats/svs/svs.go`, `formats/ndpi/tiler.go`, `formats/philips/philips.go` |
| OneFrame factor (T3 outcome) | Either `internal/oneframe/` (new) or `formats/ome/oneframe.go` |

### Theme C — Format package

| File | Responsibility |
|---|---|
| `formats/ome/ome.go` | Factory + Open + classifier wiring |
| `formats/ome/series.go` | OME-XML-driven series classification |
| `formats/ome/tiled.go` | Tiled-page Image with SubIFD pyramid traversal |
| `formats/ome/oneframe.go` (or shared) | Non-tiled level Image |
| `formats/ome/associated.go` | Macro / Label / Thumbnail AssociatedImage |
| `formats/ome/metadata.go` | OME-XML parser via stdlib `encoding/xml` |

### Theme D — Test surface

| What | Where |
|---|---|
| Unit tests | Per-file `*_test.go` mirroring v0.5 patterns |
| Integration | `tests/integration_test.go` slideCandidates + 2 OME fixtures; resolveSlide gains `"ome-tiff"` |
| Parity oracle (opentile-py path) | `tests/oracle/parity_test.go` slideCandidates + 2 OME fixtures |
| Parity oracle (tifffile path) | New `tests/oracle/tifffile_runner.py` + Go test driving it; runs on every Image, including those opentile-py drops |
| Fixtures | `tests/fixtures/Leica-1.ome.tiff.json`, `Leica-2.ome.tiff.json`, sampled mode |

### Theme E — README cleanup + deviations doc

Separable from the format port itself but bundled into v0.6 because
the multi-image OME deviation needs the deviations section in place
to land cleanly.

- `docs/deferred.md` gains a new **§ Deviations from upstream Python
  opentile** section. Becomes the single source of truth.
- `README.md` "Status" banner trimmed to one paragraph.
- `README.md` gains a top-level **"Deviations from upstream Python
  opentile"** section (or subsection under Status) summarizing the
  3 deviations in 1-line each with links to `deferred.md`.

### Theme F — Polish + ship

Mirrors v0.5's closing batch. Retirement audit (R7 → ✅), README +
CLAUDE.md milestone bump, CHANGELOG `[0.6.0]` entry, final `make
cover` / `go vet` / `-race` sweep, `make parity`, tag v0.6.0.

## 8. Out-of-scope (deferred)

- **Per-image associated images** — OME spec allows associated images
  per Image, not just per file. Our port surfaces only file-level
  associated (matches upstream). Deferred.
- **Non-uint8/non-RGB/non-JPEG levels** — fixtures are all uint8 RGB
  JPEG. Reject other configurations as `ErrUnsupportedFormat`
  (matches upstream's narrow scope).
- **Bio-Formats / libvips parity** — pixel-equivalence paths reserved
  for v0.7+ when compressed-byte references run out (BIF, SCN).
- **R6 (3DHistech)** — issue #2, parked.
- **R15 (Sakura SVSlide)** — issue #3, parked.
- **R4 / R9** — issue #1, parked.

## 9. Forward-looking hooks for v0.7 (BIF)

Things v0.6 lands cheaply that make v0.7 (Ventana BIF) easier:

- **Image interface + Tiler.Images()** generalizes if BIF turns out
  to also have multi-image cases. Even if it doesn't, the API shape
  is in place.
- **`internal/tiff` SubIFD support** — BIF uses SubIFDs in some
  scanner outputs; the support landing in v0.6 is a direct enabler.
- **OneFrame factoring (if T3 says factor)** — BIF macro/label pages
  are typically stripped, so a shared `internal/oneframe/` pays off
  in two ports rather than one.
- **Deviations doc structure** — v0.7's "openslide as reference, not
  byte-parity-able against opentile-py at all" is itself a deviation
  worth documenting; the v0.6 deviations doc structure carries that
  forward.

## 10. Branch + workflow

- Branch: `feat/v0.6` from `main` after merging `6023f52` (the v0.5
  merge commit).
- Spec: this document.
- Plan: `docs/superpowers/plans/2026-04-26-opentile-go-v06.md`
  (drafted alongside).
- Execution: same `superpowers:subagent-driven-development` pattern
  v0.5 used. Universal `Step 0: Confirm upstream` enforced.

## 11. Done-when

- Both OME fixtures open cleanly. Leica-1 produces 1 Image (5 levels)
  + macro associated. Leica-2 produces 4 Images (5 levels each) + macro.
- Output is byte-identical to Python opentile 0.20.0 on every
  sampled tile + macro that opentile-py exposes (Leica-1 + Leica-2
  series 4 + both macros).
- Output is byte-identical to tifffile-raw-bytes + Python-side
  JPEGTables splice on every Image opentile-py drops (Leica-2 series
  1-3) AND as a redundant correctness check on Leica-1 / Leica-2
  series 4.
- `TestSlideParity` green on the existing 12 fixtures + 2 new ones.
- `make cover` clears 80% per package; new `formats/ome/` package
  included in the gate.
- R7 marked ✅ landed in `docs/deferred.md §1`. New "Deviations from
  upstream Python opentile" section in `deferred.md`. New "Retired in
  v0.6" subsection paralleling v0.5's §7.
- `CHANGELOG.md` gains a `[0.6.0]` entry.
- `README.md` trimmed + gains the "Deviations" section.
- `CLAUDE.md` reflects v0.6 as the current milestone.
