# DICOM WSI Reader — Design

**Date:** 2026-06-02
**Status:** Approved (pending spec review)
**Roadmap:** new format — DICOM VL Whole Slide Microscopy (WSM)

## Goal

Read DICOM WSI series as opentile-go Slides — the first **multi-file**
format in the project. v1 reads the three fixture scanners (Leica GT450,
3DHISTECH, Grundium) for `RawTile`, `DecodedTile`, level/associated-image
enumeration, and cross-format `Metadata`. `ReadRegion` / `ScaledStrips`
inherit transparently once `DecodedTile` works.

## Background and reconnaissance

DICOM WSI is defined by Supplement 145, **VL Whole Slide Microscopy
Image** (SOP Class `1.2.840.10008.5.1.4.1.1.77.1.6`). Unlike every
format opentile-go reads today, **a slide is a set of `.dcm` SOP-instance
files in a directory**, grouped by `SeriesInstanceUID` — one instance per
pyramid level plus one per associated image. openslide reads DICOM;
upstream Python opentile does not (TIFF-only), so there is no upstream
opentile to port. The reference readers are
[`wsidicom`](https://github.com/imi-bigpicture/wsidicom) (same
imi-bigpicture/Sectra lineage), `pydicom`, and openslide's C DICOM
support.

This design rests on three rounds of recon, recorded in
`docs/formats/dicom.md`:

1. **Field study** (`dcmdump` on the Leica-4 series).
2. **Library spike** (`suyashkumar/dicom v1.1.0`, pure Go, MIT): proved
   the library exposes **raw encapsulated frame bytes without forced
   decode** (`PixelDataInfo.Frames[i].EncapsulatedData.Data`), parses
   WSM metadata cheaply with `SkipPixelData` (~110 ms for a 6-instance
   series), and walks the empty Basic Offset Table itself. A separate
   **mmap fragment-offset-walk** reproduced every frame **byte-identical**
   to the library while holding ~16 bytes/frame instead of the library's
   ~50 MB/level frame materialization.
3. **Cross-scanner validation** (3DHISTECH + Grundium): surfaced the
   assumption breaks the design must absorb (below).

### Cross-scanner reality (drives the design)

| | Leica GT450 | 3DHISTECH | Grundium |
|---|---|---|---|
| Organization | **TILED_SPARSE** | **TILED_FULL** | **TILED_FULL** |
| Tile size | 256² | 1024² | 512² |
| Levels / downsample | 3 / 4× | 10 / **2×** | 3 / 4× |
| Base px | 23374×22079 | 57344×60416 | 65536² |
| JPEG 2nd marker | APP0 (JFIF) | COM | DQT |
| Basic Offset Table | empty | empty | empty |
| Label codec | **uncompressed** | JPEG | JPEG |
| Series hygiene | clean | non-WSM / 0×0 instances present | clean |
| File naming | UID | `0000NN.dcm` | semantic |

Implications, all of which the reader must handle from v1:

- **Both organizations are mandatory.** TILED_FULL is the common case
  (2 of 3); TILED_SPARSE is Leica's. Not deferrable.
- **Tile size, level count, and downsample ratio vary** — derive every
  geometry value per-instance from `TotalPixelMatrix`; assume no fixed
  ladder.
- **Frames are opaque JPEG** (SOI-delimited; APP0/COM/DQT all seen) —
  never assume JFIF. Passthrough is unaffected; only matters if we ever
  parse/splice (we do not).
- **Empty BOT is universal** — always build the offset table by walking
  fragments; never rely on the Basic Offset Table.
- **Series hygiene** — filter by `SOPClassUID == WSM` and skip
  instances lacking `TotalPixelMatrix`.

## Sealed decisions

- **Q1 — dependency shape: wrap behind `internal/dicom`.**
  `suyashkumar/dicom` is imported **only** inside `internal/dicom`; no
  library type escapes it. Mirrors the `internal/tiff` pattern, keeps the
  library swappable (or hand-rollable later), and confines the library to
  the cold metadata path. Rejected: importing it throughout
  `formats/dicom` (couples + leaks the 50 MB/level memory model);
  vendoring/forking (re-inherits the maintenance we are buying out of).
- **Q2 — v1 scope: all three fixture scanners** (Leica SPARSE +
  3DHISTECH/Grundium FULL).
- **Q3 — Open contract: `OpenFile` accepts a directory or any one
  `.dcm`** (openslide-style), with two contracts made explicit (below).
  Rejected: a separate `OpenDICOMSeries` entry point (breaks
  format-agnostic auto-detection — the library's core value; a consumer
  with an unknown-format directory must be able to discover DICOM through
  the normal `OpenFile`). An explicit `OpenDICOMSeries(paths []string)`
  remains a clean **additive** future option for the PACS/DICOMweb
  "files not co-located" case; out of scope for v1.
- **Q4 — codecs: JPEG-baseline + uncompressed only.** Covers all three
  fixtures. JP2K / HTJ2K / JPEG-LS / RLE deferred (no fixture exercises
  them; JPEG-LS and RLE would need new `decoder/*` packages).

## Architecture

Two packages, one boundary.

### `internal/dicom` — cold-path parser (the only importer of the library)

Imports `suyashkumar/dicom`. Parses one SOP instance's metadata
(`SkipPixelData`) into opentile-owned value structs; **no `suyashkumar`
type is exported**. Exposes roughly:

```
type Instance struct {
    Path            string
    SOPClassUID     string
    SeriesUID       string
    ImageType       []string   // VOLUME / LABEL / OVERVIEW / THUMBNAIL …
    TransferSyntax  string
    TotalCols, TotalRows int    // TotalPixelMatrix
    TileRows, TileCols   int    // (0028,0010)/(0028,0011)
    NumFrames       int
    DimOrg          string      // TILED_FULL / TILED_SPARSE
    Photometric     string      // RGB / YBR_FULL_422 …
    PixelSpacingX, PixelSpacingY float64
    ObjectivePower  float64
    Manufacturer, Model, SoftwareVersions, Writer string
    ICCProfile      []byte
    // SPARSE only: per-frame 1-based top-left pixel position
    FramePositions  []FramePos  // nil for TILED_FULL
}
type FramePos struct { Col, Row int }  // ColumnPositionInTotalImagePixelMatrix / Row…

func ParseInstance(path string) (Instance, error)
```

`internal/dicom` parses metadata only. It does **not** read pixel data,
**not** know about tiles, and **not** import the root `opentile` package
(avoiding an import cycle through `formats/all`) — `formats/dicom` owns
the fragment-walk, the tile grid, and the mmap backing choice.

### `formats/dicom` — the reader

Series assembly, Slide-model mapping, the frame→tile map, the mmap
fragment-offset-walk hot path, and `RawTile`/`DecodedTile`. Implements
opentile's reader interface and registers in `formats/all`. Imports
`internal/dicom` (metadata) + `decoder` (codecs) + the existing mmap
backing; **never** imports `suyashkumar/dicom`.

The fragment-offset-walk (proven in the spike) locates the encapsulated
`PixelData` (OB, undefined length), skips the BOT item, and records one
`span{off,len}` per fragment over the mmap'd bytes. v1 assumes **one
fragment per frame** (true for all three fixtures); multi-fragment frames
are a deferred error case.

## Open and detection — with explicit contracts

`OpenFile(path, opts…)`:

- If `path` is a **directory**: glob `*.dcm`, `ParseInstance` each
  (metadata-only), keep `SOPClassUID == WSM` and `TotalPixelMatrix > 0`,
  group by `SeriesUID`. If exactly one WSM series remains, build the
  Slide; if multiple series, v1 selects the series with the most VOLUME
  levels and records the rest in metadata (multi-series-per-directory is
  otherwise out of scope).
- If `path` is a **single `.dcm`**: detect `DICM` @ byte 128 +
  `SOPClassUID == WSM`; then **sibling-scan** the containing directory
  for instances sharing that `SeriesUID`.
- No WSM instance found → `ErrUnsupportedFormat` (so auto-detection
  falls through cleanly to other readers / final error).

**Contract 1 — single-reader asymmetry (documented, by design).** DICOM
is the first format that requires a filesystem *path*. The core
`Open(io.ReaderAt, size, …)` entry point cannot express a multi-file
series and returns `ErrUnsupportedFormat` for DICOM; DICOM is reachable
**only** through `OpenFile`. This is inherent to multi-file formats, not
specific to this option. Documented in `docs/formats/dicom.md` and the
`Open` godoc.

**Contract 2 — sibling-scan bounds (documented, by design).** Opening a
single `.dcm` reads *other* files in the same directory. The scan is
bounded: **same directory only** (non-recursive), **same `SeriesUID`
only**, **WSM-filtered**. Unrelated `.dcm` files and other series are
ignored. Documented so the side effect is predictable, not spooky.

## Slide-model mapping

One series → one Slide, one main `Image`.

- **Levels**: VOLUME instances sorted by `TotalCols` descending.
  Per-level downsample derived from `TotalCols[0] / TotalCols[i]` (not
  assumed). Each level's tile grid = `ceil(TotalCols/TileCols) ×
  ceil(TotalRows/TileRows)`.
- **Associated images**: `ImageType` token LABEL / OVERVIEW / THUMBNAIL
  → `AssociatedImage.Type()` (DICOM-token-to-opentile mapping; OVERVIEW
  is the macro). Single-frame instances; the "tile" is the whole image.
- **Metadata**: `MicronsPerPixelX/Y` from `PixelSpacing` (mm→µm) or
  `ImagedVolumeWidth/TotalCols`; `Magnification` from `ObjectiveLensPower`;
  `ScannerManufacturer/Model/Software` from Manufacturer/Model/
  SoftwareVersions; `Writer` from the meta header
  (`ImplementationVersionName` / `SourceApplicationEntityTitle`);
  `ICCProfile()` from the Optical Path Sequence.
- **TIFF-tag API**: not applicable (DICOM is not TIFF). `TIFFDirectoriesOf`
  / `LevelTIFFTags` return `ok=false`, exactly like IFE / SZI.

## Frame → tile mapping and the hot path

At Open, per level, build a frozen `(tx,ty) → span{off,len}` table:

- **TILED_FULL**: implicit raster — frame index =
  `ty*ceil(TotalCols/TileCols) + tx`; pair with the walk's spans in
  order.
- **TILED_SPARSE**: from `internal/dicom.Instance.FramePositions`,
  `tx = (Col-1)/TileCols`, `ty = (Row-1)/TileRows`; pair each with its
  fragment span. Grid cells with no frame → blank-fill (cached white
  tile, as in BIF/Leica-SCN).

`RawTile(level, tx, ty)` returns a **zero-copy mmap subslice** of the
fragment; honors the existing `WithBacking` (mmap default / pread)
option; lock-free after Open. `DecodedTile` routes the fragment to
`decoder/jpeg` (baseline) or `decoder/none` (uncompressed) by transfer
syntax, honoring `PhotometricInterpretation` (RGB vs YBR — do **not**
apply YCbCr→RGB for RGB-tagged frames). Memory: one `span` table per
level (~16 B/frame), no pixel bytes retained — preserves the v0.30
memory-budget and lock-free invariants.

## Robustness

Tolerate, without failing Open: malformed `DT`/`DS` values (the Leica
garbage timezone `+429496728900`), empty BOT (always walk fragments),
instances missing/zero `TotalPixelMatrix` (skip), non-WSM instances in
the directory (filter), and mixed codecs within one series (per-instance
transfer syntax). A frame whose transfer syntax is unsupported →
`TileError` on `DecodedTile` (lazy), not at Open.

## Testing and parity

- **`TestSlideParity`**: all three fixtures (sampled tile SHA256), as for
  every other format. Pins per-level Size/TileSize/Grid/Compression,
  associated-image types+sizes, and Metadata fields.
- **Byte-identity unit test**: our offset-walk spans == the library's
  materialized frames for sampled frames per fixture (the spike's
  assertion, retained as a regression guard on the fragment-walk).
- **Geometry/backing parity**: tile bytes identical across mmap and pread
  backings.
- **Future** (ties to roadmap #21): openslide DICOM as an independent
  pixel-tolerance oracle. The Leica-4 GT450 lineage makes a
  DICOM-vs-SVS comparison attractive later.
- **Fixtures**: the three series live under `sample_files/dicom/`
  (zips already present; `Leica-4/` extracted). Only sampled tile hashes
  are committed, not the slides.

## Non-goals (deferred)

Concatenations (a level split across instances via `ConcatenationUID`);
multi-fragment-per-frame; JP2K / HTJ2K / JPEG-LS / RLE transfer syntaxes;
multi-optical-path / Z-stack / multi-pyramid series; DICOMweb/PACS and
the explicit `OpenDICOMSeries(paths)` constructor; a raw-DICOM-attribute
exposure API (the TIFF-tag analog).

## Risks

- **`internal/dicom` boundary surface** — keep the exposed struct minimal
  so the library stays swappable. Mitigated by the no-leak rule.
- **Library memory model** — full parse materializes ~50 MB/level;
  contained by using `SkipPixelData` for the cold path and our own walk
  for the hot path. The library must never be on the per-tile path.
- **Library health** — `suyashkumar/dicom` is the single external runtime
  dependency of its kind; the wrap (Q1) is the insurance.
- **Fixture coverage** — three scanners is good breadth but not
  exhaustive; real-world DICOM is famously non-conformant. Debug-from-
  report for scanners outside the slate, as with Leica SCN.

## References

- `docs/formats/dicom.md` — field study + cross-scanner findings (the
  empirical basis for this design).
- Prototype: `/tmp/dicomproto` (throwaway; not committed). Proved Q1's
  hot/cold split and the cross-scanner assumption breaks.
- DICOM PS3.3 (WSM IOD), PS3.5 (encapsulated pixel data / offset tables),
  PS3.6 (data dictionary).
- `wsidicom`, `pydicom`, openslide DICOM (read-for-understanding only).
- `suyashkumar/dicom` v1.1.0 (MIT, pure Go).
