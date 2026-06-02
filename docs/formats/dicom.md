# DICOM WSI (VL Whole Slide Microscopy)

> **STATUS — pre-implementation field study, NOT a shipped reader.**
> opentile-go does **not** read DICOM today. This document captures the
> ground-truth structure of a real DICOM WSI series (the `Leica-4`
> fixture, inspected with DCMTK `dcmdump` on 2026-06-02) plus the
> architectural implications for a future `formats/dicom` reader. It
> exists so the eventual implementation starts from observed reality
> rather than the standards-conformant ideal. Every "what we'd need"
> note is speculative until a reader and fixtures land. No public API,
> no `opentile.FormatDICOM` enum, and no `internal/dicom` package exist
> yet.

DICOM WSI is the standardized digital-pathology container defined by
DICOM Supplement 145, **VL Whole Slide Microscopy Image** (WSM), SOP
Class UID `1.2.840.10008.5.1.4.1.1.77.1.6`. Unlike every format
opentile-go currently reads, **a DICOM "slide" is not one file** — it is
a set of SOP-instance files (`.dcm`) in a directory, grouped by
`SeriesInstanceUID`, one instance per pyramid level and one per
associated image. openslide reads DICOM; upstream Python opentile does
**not** (it is TIFF-only via tifffile), so DICOM is firmly in the
"superset of opentile, openslide-like" territory — there is no upstream
opentile code to port. The reference readers are
[`wsidicom`](https://github.com/imi-bigpicture/wsidicom) (Python, same
imi-bigpicture/Sectra lineage as opentile), `pydicom` for low-level
parsing semantics, and openslide's C DICOM support as an independent
implementation.

## Format basics

- **Container**: per SOP instance — a 128-byte preamble, the `DICM`
  magic at offset 128, a group-0002 **File Meta Information** header
  (always Explicit VR Little Endian) carrying the Transfer Syntax UID,
  then the main data set encoded per that transfer syntax. WSM data sets
  are Explicit VR Little Endian in practice.
- **Detection**: `DICM` at byte 128 identifies a DICOM file;
  `SOPClassUID (0008,0016) == VLWholeSlideMicroscopyImageStorage`
  identifies WSM. Assembling the *slide* additionally requires scanning
  sibling `.dcm` files in the directory and grouping by
  `SeriesInstanceUID (0020,000E)`.
- **Mental model**: one series = one slide. VOLUME instances of
  decreasing `TotalPixelMatrix` size are the pyramid levels; LABEL /
  OVERVIEW / THUMBNAIL instances are associated images. Each instance's
  pixel data is **tiled into frames**; one frame = one compressed tile.
- **Tiles → frames**: the image is stored as `NumberOfFrames (0028,0008)`
  frames inside encapsulated `PixelData (7FE0,0010)`. Frame dimensions
  are `Rows (0028,0010)` × `Columns (0028,0011)`. A frame's compressed
  bytes are a complete, self-contained codestream — the natural unit for
  a `RawTile` passthrough.
- **Frame ordering** — `DimensionOrganizationType (0020,9311)`:
  - **TILED_FULL**: implicit raster order; frame index =
    `row*ceil(TotalPixelMatrixColumns/Columns) + col`. No per-frame
    metadata.
  - **TILED_SPARSE**: each frame's grid position is given explicitly in
    the Per-Frame Functional Groups Sequence (see below). **This is what
    Leica emits** — see "Corrected assumptions".
- **Codecs**: selected by Transfer Syntax UID — and they line up with
  codecs opentile-go already owns:

  | Transfer syntax | UID | opentile-go codec |
  |---|---|---|
  | JPEG Baseline | `1.2.840.10008.1.2.4.50` | ✅ `decoder/jpeg` (libjpeg-turbo) |
  | JPEG 2000 | `…4.90` / `…4.91` | ✅ `decoder/jpeg2000` (OpenJPEG) |
  | HTJ2K | `…4.201`–`.203` | ✅ `decoder/htj2k` (openjph) |
  | Uncompressed (native) | `1.2.840.10008.1.2.1` | ✅ `decoder/none` (raw passthrough) |
  | JPEG-LS | `…4.80` / `…4.81` | ❌ no codec |
  | RLE Lossless | `1.2.840.10008.1.2.5` | ❌ no codec |

## Fixture inventory — `sample_files/dicom/`

| Fixture | Form | Notes |
|---|---|---|
| `Leica-4/` (extracted) | directory of 6 `.dcm` | **Leica GT450** export via "Leica ScnUtility" 1.0.1; the inspected reference (below) |
| `Leica-4.zip` | 81 MB | zipped form of the above |
| `3DHISTECH-1.zip` | 345 MB | 3DHISTECH-scanned DICOM WSI (uninspected) |
| `scan_621_grundium_dicom.zip` | 340 MB | Grundium-scanned DICOM WSI (uninspected) |

A broader public corpus exists at the NCI **Imaging Data Commons (IDC)**
and openslide-testdata.

## `Leica-4` — the inspected reference series

Six instances, one `SeriesInstanceUID`. Scanner: Leica Biosystems
**GT450** (the same scanner family behind our SVS GT450 fixtures),
DeviceSerialNumber `SS12143`, SoftwareVersions `1.0.1`, writer AE Title
`Leica ScnUtility`. All frames JPEG Baseline **except the label**, which
is uncompressed.

| Instance | `ImageType (0008,0008)` | Size (px) | Frames (grid) | Tile | Encoding (PhotometricInterp) | Role |
|---|---|---:|---|---|---|---|
| 65 MB | `DERIVED\PRIMARY\VOLUME\RESAMPLED` | 23374×22079 | 8004 (92×87) | 256×256 | JPEG Baseline (**RGB**) | **Level 0** — 1.05 µm/px |
| 16 MB | `…VOLUME\RESAMPLED` | 5843×5519 | 506 (23×22) | 256×256 | JPEG Baseline (RGB) | **Level 1** — 4.2 µm/px |
| 13 MB | `…VOLUME\RESAMPLED` | 1460×1379 | 36 (6×6) | 256×256 | JPEG Baseline (RGB) | **Level 2** — 16.8 µm/px |
| 13 MB | `…THUMBNAIL\RESAMPLED` | 1920×1813 | 1 (whole) | — | JPEG Baseline | associated **thumbnail** |
| 980 KB | `ORIGINAL\PRIMARY\LABEL\NONE` | 608×547 | 1 (whole) | — | **uncompressed** (native RGB) | associated **label** |
| 128 KB | `ORIGINAL\PRIMARY\OVERVIEW\NONE` | 1491×605 | 1 (whole) | — | JPEG Baseline | associated **overview** (macro) |

The VOLUME pyramid is a clean 4× ladder (23374 → 5843 → 1460).
`ImageType` cleanly separates levels (VOLUME) from associated images
(LABEL / OVERVIEW / THUMBNAIL → our `AssociatedImage.Type()`).
`ObjectiveLensPower` = 40; `ImagedVolumeWidth/Height` ≈ 24.6 × 23.2 mm;
`PixelSpacing` is per-instance.

### TILED_SPARSE position mechanism (as observed)

Every instance — even the single-frame label — declares
`TILED_SPARSE`. Per-frame grid position lives in:

```
(5200,9230) PerFrameFunctionalGroupsSequence
  └─ item[frame]
       └─ (0048,021A) PlanePositionSlideSequence
            └─ item  (#=5)
                 (0040,072A) XOffsetInSlideCoordinateSystem   DS  (mm)
                 (0040,073A) YOffsetInSlideCoordinateSystem   DS  (mm)
                 (0040,074A) ZOffsetInSlideCoordinateSystem   DS
                 (0048,021E) ColumnPositionInTotalImagePixelMatrix  SL  (1-based px)
                 (0048,021F) RowPositionInTotalImagePixelMatrix     SL  (1-based px)
```

The integer **`ColumnPositionInTotalImagePixelMatrix` /
`RowPositionInTotalImagePixelMatrix`** are 1-based **pixel** coordinates
of each tile's top-left corner. Tile index =
`tx = (col-1)/Columns`, `ty = (row-1)/Rows` (e.g. col 257, row 1281 with
256-px tiles → `tx=1, ty=5`). A future reader builds the
`(tx,ty) → frameIndex` map once at Open. Note: although declared SPARSE,
these grids are **dense** (frames == full grid), so no blank-fill is
actually needed for `Leica-4` — but frame order is not guaranteed raster,
so the position map must still be read.

### PixelData encapsulation (as observed)

```
(7FE0,0010) OB (PixelSequence)
  (FFFE,E000) item  ← Basic Offset Table, EMPTY (0 bytes)
  (FFFE,E000) item  ← frame 1: complete JFIF JPEG (FF D8 FF E0 …)
  (FFFE,E000) item  ← frame 2: complete JFIF JPEG
  …
```

- The **Basic Offset Table is empty** and there is **no Extended Offset
  Table** (`7FE0,0001`). So there is no O(1) frame seek for free — a
  reader must walk the fragment items once at Open to record each
  frame's byte offset/length, then freeze that table (lock-free hot
  path, same as our other formats).
- Each frame is exactly **one fragment = one complete standalone JFIF
  JPEG**. No shared JPEG tables to splice (unlike some SVS), so `RawTile`
  is a trivial fragment slice. (Multi-fragment-per-frame is a more
  complex case not present here.)

## Corrected assumptions (why we inspected before designing)

Inspecting `Leica-4` moved two items from "deferrable" to "day-one
mandatory" and surfaced two silent-corruption risks. (These were the
*Leica-only* corrections; the cross-scanner section below corrects them
further — most importantly, **TILED_FULL turns out to be the common
case**, not the SPARSE one.)

1. **TILED_SPARSE must be supported from day one** — but it is Leica's
   organization, **not** universal. A Leica DICOM reader must implement
   the Per-Frame position map ("support TILED_FULL first, defer SPARSE"
   would not read Leica at all), *and* must support TILED_FULL, which the
   other two scanners use (see cross-scanner section).
2. **Empty Basic Offset Table** → must scan fragments at Open to build
   the frame index; cannot rely on the BOT.
3. **Mixed encoding within one series** → JPEG frames for
   VOLUME/THUMBNAIL/OVERVIEW but **uncompressed** native pixels for the
   LABEL. The reader needs both the codec path and a raw passthrough in
   the same slide.
4. **JPEG photometric is RGB, not YBR** → the JPEG decode must **not**
   apply a YCbCr→RGB color transform for these frames. Getting it wrong
   yields color-swapped tiles — a real parity-risk knob.
5. **Metadata and per-frame data are nested in functional-group
   sequences** (`PixelSpacing`/`ObjectiveLensPower` under Shared
   Functional Groups → Pixel Measures; positions under Per-Frame
   Functional Groups → Plane Position). The parser cannot be
   top-level-only — it needs real undefined-length SQ traversal (items +
   delimiters).

What held up from the initial speculation: the multi-file-series model,
the 1:1 frame↔raw-tile fit, the `ImageType`→level/associated split, and
that the codecs we already own (JPEG + uncompressed) cover this file
with zero gaps.

## Cross-scanner validation (Leica + 3DHISTECH + Grundium)

A throwaway prototype (`suyashkumar/dicom` for cold metadata + an own
mmap fragment-offset-walk for raw frames) was run against all three
fixture scanners. The offset-walk reproduced every frame **byte-identical**
to the library on all three (Grundium 16384 frames, 3DHISTECH 3304
frames) while holding ~16 bytes/frame instead of the library's ~50
MB/level. The scanners diverge substantially:

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

Lessons the reader must absorb from v1:

- **Both organizations are mandatory; TILED_FULL is the common case**
  (2 of 3). FULL needs a computed raster frame-index
  (`ty*tilesAcross + tx`); SPARSE needs the position map.
- **Tile size, level count, and downsample ratio all vary** — derive
  every geometry value per-instance from `TotalPixelMatrix`; assume no
  fixed ladder.
- **Frames are opaque JPEG** — three different second markers
  (APP0 / COM / DQT). Treat as SOI-delimited; never assume JFIF.
- **Empty BOT is universal** — always build the offset table by walking
  fragments.
- **Series hygiene** — filter by `SOPClassUID == WSM` and skip instances
  with zero/missing `TotalPixelMatrix` (3DHISTECH carries such instances
  in the series).

The full reader design built on these findings:
[`docs/superpowers/specs/2026-06-02-dicom-reader-design.md`](../superpowers/specs/2026-06-02-dicom-reader-design.md).

## Architectural implications for a future `formats/dicom`

- **Multi-file Open is the defining cost.** The current factory takes a
  single `io.ReaderAt + int64 size`; DICOM needs a directory/series-aware
  entry point (e.g. `OpenFile` accepting a directory or any one `.dcm`
  and scanning siblings, the way openslide does). This is the one place
  DICOM likely forces a **public-API addition** rather than a purely
  internal reader. (The v0.31.1 doc pass deliberately dropped the
  v1.0/API-freeze ceremony, so additive surface growth here is cheap.)
- **`internal/dicom` parser is the long pole.** A WSM-focused parser
  (preamble + `DICM`, group-0002 meta header → transfer syntax,
  Explicit-VR data set, **nested undefined-length SQ traversal**,
  encapsulated PixelData fragment walking). Shaped for opentile's needs
  like `internal/tiff`, not a general DICOM library. The attribute set
  we actually read is small and knowable.
- **`RawTile` fit is clean**: find frame index for `(tx,ty)` via the
  SPARSE position map → slice the fragment at the Open-built offset →
  return bytes. `DecodedTile` routes to the existing pooled decoder by
  transfer syntax; uncompressed instances go through `decoder/none`.
- **TIFF-tag API is N/A** (DICOM is not TIFF) — like IFE/SZI,
  `TIFFDirectoriesOf` would return `ok=false`. A symmetric raw
  DICOM-attribute exposure API would be a separate future opportunity.
- **Deferrable for a first cut**: concatenations (a level split across
  instances via `ConcatenationUID (0020,9161)` — *absent* in `Leica-4`,
  each level is one instance), TILED_FULL (none here), JPEG-LS / RLE
  transfer syntaxes (would need new codec packages), multi-fragment
  frames.
- **Robustness note**: `Leica-4`'s `AcquisitionDateTime` carries a
  garbage timezone suffix (`+429496728900`, ~`0xFFFFFFFF`). The parser
  must tolerate malformed DT/DS values without failing Open.

## Metadata mapping (sketch)

| DICOM source | opentile `Metadata` |
|---|---|
| `PixelSpacing (0028,0030)` (Shared FG → Pixel Measures) or `ImagedVolumeWidth ÷ TotalPixelMatrixColumns` | `MicronsPerPixelX/Y` |
| `ObjectiveLensPower (0048,0112)` | `Magnification` |
| `Manufacturer (0008,0070)` / `ManufacturerModelName (0008,1090)` / `SoftwareVersions (0018,1020)` | `ScannerManufacturer` / `ScannerModel` / `ScannerSoftware` |
| `SourceApplicationEntityTitle (0002,0016)` or `ImplementationVersionName (0002,0013)` | `Writer` |
| Optical Path Sequence → `ICCProfile (0028,2000)` | `ICCProfile()` |
| `ImageType (0008,0008)` token (LABEL/OVERVIEW/THUMBNAIL) | `AssociatedImage.Type()` |

## Parity opportunity

Because `Leica-4` is a **GT450** — the same scanner behind our SVS GT450
fixtures — a future DICOM-vs-SVS pixel comparison on a matched slide
would be a strong *independent* correctness oracle (the two paths share
no decoder lineage). More generally, openslide's C DICOM reader is an
independent implementation suitable for a pixel-tolerance oracle, the
way openslide already serves the benchmark suite.

## References

- DICOM standard: PS3.3 (WSM IOD / Supplement 145), PS3.5 (encoding,
  encapsulated pixel data, Basic/Extended Offset Table), PS3.6 (data
  dictionary).
- `wsidicom` (Apache 2.0): https://github.com/imi-bigpicture/wsidicom —
  the canonical WSI-DICOM reader, same lineage as opentile.
- `pydicom`: low-level parsing reference.
- openslide DICOM support (LGPL 2.1, read-for-understanding only):
  independent C implementation; series assembly + level detection.
- Inspection tooling: DCMTK `dcmdump` (`brew install dcmtk`).
- Fixtures: `sample_files/dicom/` (`Leica-4/` extracted; 3DHISTECH and
  Grundium zips uninspected); NCI Imaging Data Commons for a broader
  corpus.
