# opentile-go v0.16 — Smart Zoom Image (SZI) reader

**Status:** sealed 2026-05-08.
**Work branch:** `feat/v0.16`.
**Headline:** Add Smart Zoom Image (SZI) format support — ZIP-wrapped Microsoft Deep Zoom pyramids with `scan-properties.xml` and an `associated_images/` directory. New `formats/szi/` package backed by a new shared `internal/dzi/` core (DZI manifest parsing + level/tile-coordinate math) designed for additive bare-DZI support in a later milestone. Driven by the user's wsi-tools / viewer pipeline targeting Grundium-scanner output.

## 1. Scope

### 1.1. New `internal/dzi/` package

Pure-function DZI manifest parsing + tile-coordinate math, no I/O:

- `dzi.Manifest` struct: `Format string`, `Overlap int`, `TileSize int`, `Width int`, `Height int`, plus parsed XML namespace.
- `dzi.ParseManifest(xml []byte) (Manifest, error)` — XML decoder accepting Microsoft Deep Zoom namespace `http://schemas.microsoft.com/deepzoom/2008`.
- `dzi.MaxLevel(w, h int) int` — `ceil(log2(max(w,h)))`. Total levels = `MaxLevel(w,h) + 1`.
- `dzi.LevelDims(maxLevel, level, width, height int) (w, h int)` — per-level width/height (each level halves the previous, snapping up).
- `dzi.GridDims(levelW, levelH, tileSize int) (cols, rows int)` — `ceil(levelW/tileSize)` × `ceil(levelH/tileSize)`.
- `dzi.TilePath(rootDir string, level, col, row int, format string) string` — builds the on-disk tile path `<rootDir>/<level>/<col>_<row>.<format>`. Note column-then-row order per the Microsoft spec.

XML-only manifest parsing in v0.16. JSON variant (DZI's alternative manifest form, supported by OpenSeadragon) is deferred until a fixture demands.

### 1.2. New `formats/szi/` package

ZIP-backed Tiler that uses `internal/dzi` for the pyramid structure:

- `szi.Open(name string) (opentile.Tiler, error)` — top-level open.
- `szi.MetadataOf(t opentile.Tiler) (Metadata, bool)` — typed format-specific accessor (mirrors `philipstiff.MetadataOf`, `ometiff.MetadataOf`, `leicascn.MetadataOf` from v0.6 onward).
- `szi.Metadata` struct with all SZI-specific scan-properties.xml fields + `VendorProperties map[string]string` for open-ended `vendor.<key>` custom properties.
- Registers via `formats/all` so `opentile.OpenFile` auto-detects.

### 1.3. New `opentile.FormatSZI` enum value

```go
FormatSZI Format = "szi"
```

Added to `tiler.go` alongside existing format values. Matches the established short-slug convention (`svs`, `ndpi`, `bif`, `ife`).

### 1.4. Cross-format `Metadata` population

`Tiler.Metadata()` returns the standard cross-format struct, populated from `scan-properties.xml`:

| Cross-format field | SZI source | Notes |
|---|---|---|
| `Magnification` | `ObjectiveMagnification` | float |
| `ScannerManufacturer` | `VendorName` | string |
| `ScannerModel` | `ScannerName` | string |
| `AcquisitionDateTime` | `TimeStart` | parsed `yyyy-mm-ddThh:mm:ss` |
| `MicronsPerPixel` | `MicronsPerPixel` if present, else avg of X/Y | float |
| `ImageDescription` | `Comments` field | optional; if empty in fixture, `Metadata.ImageDescription` is empty |
| `ScannerSoftware` | `["<SoftwareName> <SoftwareVersion>"]` | single-element slice |

### 1.5. Format-specific `szi.Metadata` struct

Typed access to all SZI-specific fields not represented in cross-format Metadata:

```go
type Metadata struct {
    Version           string    // <image version="...">
    Date              time.Time // <image date="...">

    UserName          string
    SoftwareName      string
    SoftwareVersion   string

    TimeStart         time.Time
    TimeEnd           time.Time
    ElapsedTime       string  // string format e.g. "0h17m22s" per spec

    CaseNumber        string
    ScanJobName       string

    ScannerSerialNo   string

    CameraName        string
    SensorPixelSize   float64  // µm

    ScannedArea       float64  // mm²
    ScanWidth         float64  // mm
    ScanHeight        float64  // mm

    MicronsPerPixelX  float64
    MicronsPerPixelY  float64

    Comments          string

    // VendorProperties holds open-ended custom properties prefixed
    // with vendor name + dot per the SZI spec page 9 convention
    // (e.g., "vendor.MicronsX", "Grundium.SerialNumber"). Keys
    // surfaced as-is including the dotted prefix.
    VendorProperties  map[string]string
}
```

### 1.6. Associated images

The optional `<root>/associated_images/` folder may contain three JPEGs (per spec):

| Filename | `Type()` value | Notes |
|---|---|---|
| `macro.jpg` | `"overview"` | Per v0.15 alignment — opentile-go canonical name is `"overview"` even though SZI's filename is `macro.jpg` |
| `label.jpg` | `"label"` | direct mapping |
| `thumbnail.jpg` | `"thumbnail"` | direct mapping |

Bytes pass through verbatim (the JPEG is the JPEG; no re-encoding). Geometry pinned via per-fixture geometry test rows (matching the v0.10/v0.14 generic-TIFF fixture pattern).

### 1.7. Detection + registration

`formats/szi/factory.go` implements `opentile.FormatFactory`:

- **Detect**: file extension `.szi` (case-insensitive) **AND** ZIP magic `PK\x03\x04` at offset 0 **AND** nested `.dzi` file present in the ZIP central directory.
- **Open**: parses central directory eagerly; validates SZI structure (one root folder, `.DZI` + `scan-properties.xml` present, `_files/` pyramid folder); returns `*Tiler`.

## 2. Out of scope

- **Bare DZI reader** (filesystem-backed, no ZIP wrapper). Designed for via `internal/dzi/` split, but `formats/dzi/` does not ship in v0.16. Promoted to v0.17+ when a consumer or fixture surfaces.
- **Sparse SZI files** (missing tiles in the addressable grid). Per SZI spec page 4, sparse images and collections are explicitly NOT supported in SZI. v0.16 enforces strict compliance: a missing tile in a valid coordinate range returns `ErrCorruptArchive`. Breadcrumbs left for a future additive `ErrTileMissing` sentinel + opt-in lenient mode if a real sparse-SZI fixture surfaces (Q2-deferred).
- **DZC collections** (Morton-laid-out shared thumbnails). Multi-image; opentile-go reads single-WSI files only. Permanent.
- **JSON DZI manifest variant**. SZI is XML-only per spec. JSON is OpenSeadragon-supported but DZI-bare territory; deferred with bare DZI.
- **Optional `vendor/` folder content**. The SZI spec allows vendors to dump non-standard files into `<root>/vendor/`. v0.16 validates the folder may exist but does not surface its contents through the public API. Consumers wanting access can re-open the file with `archive/zip`. Q4-deferred extension if signal surfaces.
- **Synthetic ZIP64 fixture**. Both available real fixtures are below the 4 GB / 65535-entry ZIP64 trigger. Go's `archive/zip` handles ZIP64 transparently — we get it without engineering. Synthetic ZIP64 fixture is YAGNI until a real one surfaces.
- **Per-tile parity oracle vs. reference SZI viewer**. SZI is byte-passthrough JPEG passthrough; tile bytes are committed via `TestSlideParity` SHA fixtures. No external oracle (Python opentile / OpenSlide / Bio-Formats) reads SZI.
- **v1.0 cut.** Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | SZI alone in v0.16 vs SZI+DZI together vs split? | **A: SZI alone in v0.16; DZI deferred to v0.17+ pending consumer signal.** Architecture pre-prepared via `internal/dzi/` split so DZI is additive. |
| Q2 | Sparse-tile handling | **Strict-spec: missing tile → `ErrCorruptArchive`.** Breadcrumbs documented for future additive `ErrTileMissing` sentinel + opt-in lenient mode if a sparse-SZI fixture surfaces (which the user noted is theoretically space-saving). |
| Q3 | Metadata public API surface | **Option A:** typed `szi.Metadata` struct with all SZI-specific fields + `VendorProperties map[string]string`. Cross-format `Metadata` populated from canonical fields. Mirrors v0.6+/Philips/OME/IFE/SCN precedent. |
| Q4 | `vendor/` folder content surfacing | **Defer.** Document that the folder may exist but doesn't surface to consumers. Q-defer until signal. |
| Q5 | ZIP backing strategy | **Eager**: parse ZIP central directory once at `Open()` time; cache entry → offset/length map for hot-path tile lookups. Honors v0.9 lock-free hot-path invariant. |
| Q6 | `internal/dzi/` package split | **Yes.** Manifest parser + tile-coordinate math live in `internal/dzi/`; `formats/szi/` brings only the ZIP backend + SZI-specific metadata. Pre-pares for future `formats/dzi/` without compromise. |
| Q7 | Fixture coverage | **Both fixtures**: `CMU-1.szi` (1.5 MB, from spec repo) + `scan_618_grundium_SZI.szi` (709 MB, Grundium-produced). TestSlideParity → 30 fixtures (was 28). |
| Q8 | Task count + commit shape | **6 tasks** single batch (matches v0.14 / v0.15 cadence): T1 internal/dzi → T2 ZIP-backed Tiler skeleton + register → T3 Levels/Image/Tiles → T4 associated images + metadata → T5 fixtures + tests → T6 docs + ship. |

## 4. Fixtures

| File | Bytes | Source | Notes |
|---|---:|---|---|
| `sample_files/szi/CMU-1.szi` | 1.5 MB | smartinmedia/SZI-Format reference repo | small smoke fixture; 2220×2967, TileSize=256, 13 levels (0-12), 165 tiles total |
| `sample_files/szi/scan_618_grundium_SZI.szi` | 709 MB | Grundium scanner | full-walk fixture; 147456×81920, **TileSize=512**, 19 levels (0-18), 61,478 tiles total |

Both fixtures are dense (every addressable tile present). Both carry the complete associated set (`label.jpg`, `macro.jpg`, `thumbnail.jpg` — verified 2026-05-08). Both have `scan-properties.xml`. Grundium probe reveals canonical-field values: `VendorName=Grundium`, `ScannerName=Ocus`, `ObjectiveMagnification=40`, `MicronsPerPixel=0.25055239898989901`. CMU-1.szi metadata is the spec-example values (`VendorName=TestCompany`, `ScannerName=Super Scan 2`, etc.) — useful for parser unit tests.

## 5. Test strategy

### 5.1. `internal/dzi/` unit tests

Pure-function tests:
- Manifest parser: golden-XML fixtures (the two CMU-1.dzi + scan_618_.dzi strings, plus a synthetic invalid-XML negative case).
- Level/tile coordinate math: spec's worked example (234,298 px → 19 levels).
- Tile-path formatter: column-then-row order verified on multiple coordinate inputs.

### 5.2. `formats/szi/` unit + integration tests

- Tiler open/close roundtrip on both fixtures.
- Per-level geometry pinning (mirror `tests/parity/generic_geometry_test.go` shape; new `tests/parity/szi_geometry_test.go`).
- Associated-image presence: `Type()` values, sizes, byte counts.
- Metadata population: cross-format fields + szi.Metadata.VendorProperties.
- Negative cases: corrupt ZIP magic, missing `.dzi` manifest, missing `scan-properties.xml`, sparse-tile request → `ErrCorruptArchive`.

### 5.3. Per-tile SHA fixtures

`tests/fixtures/CMU-1.szi.json` + `tests/fixtures/scan_618_grundium_SZI.szi.json` generated via `TestGenerateFixtures` (sample 5 MB cap per fixture; small fixture full-walk, large fixture sampled).

### 5.4. Cross-format `TestSlideParity`

Both fixtures into `slideCandidates`. TestSlideParity total now **30 fixtures** (was 28 post-v0.14).

## 6. Architecture

### 6.1. `internal/dzi/` shape

```
internal/dzi/
├── manifest.go      // Manifest struct + ParseManifest
├── manifest_test.go
├── coords.go        // MaxLevel, LevelDims, GridDims, TilePath
├── coords_test.go
└── doc.go
```

No I/O dependencies; pure data + parsing + arithmetic. Provides a stable foundation for both `formats/szi/` (v0.16) and a future `formats/dzi/` (v0.17+).

### 6.2. `formats/szi/` shape

```
formats/szi/
├── doc.go
├── factory.go       // FormatFactory implementation (Detect, Open)
├── tiler.go         // *Tiler struct; Open, Close, Format, Levels, Images, Associated, Metadata
├── tiler_test.go
├── level.go         // *level struct implementing opentile.Level
├── level_test.go
├── associated.go    // *associatedImage; macro/label/thumbnail readers
├── associated_test.go
├── metadata.go      // Metadata struct; MetadataOf accessor; scan-properties.xml parser
├── metadata_test.go
├── zip.go           // ZIP central-directory eager parse + tile lookup
└── zip_test.go
```

Backing: `*os.File` opened via `opentile.OpenFile` semantics (mmap default per v0.9). Inside the ZIP, each tile entry resolves to an `io.SectionReader` slice over the file (uncompressed-stored entries → direct byte slice; preserves v0.9 mmap-aliased fast path).

### 6.3. Tile lookup hot path

```
Level.Tile(col, row int) ([]byte, error):
  path := dzi.TilePath(rootDir, level, col, row, format)
  entry, ok := zip.entries[path]
  if !ok { return nil, ErrCorruptArchive }
  return zip.readEntry(entry)  // SectionReader over the .szi file
```

Eager central-directory parse populates `zip.entries map[string]*zipEntry` once at `Open()`; tile lookups are pure map reads + a `Read()` on a `SectionReader`. Lock-free per the v0.9 invariant — `entries` map is immutable post-`Open()`.

### 6.4. ZIP entry constraint

SZI spec mandates uncompressed-stored entries (compression method = 0 / store). The reader validates this on `Open()` for tile entries; if a non-stored entry is encountered, `Open()` fails with a descriptive error. (Stored entries support direct byte-slice access without inflating; this is the spec's guarantee that mmap-aliasing works.)

## 7. Plan outline

Single batch, 6 tasks. Plan written separately at `docs/superpowers/plans/2026-05-08-opentile-go-v16-szi.md`:

- **T1**: `internal/dzi/` — manifest parser + coords math + unit tests.
- **T2**: `formats/szi/` skeleton — `*Tiler`, `Open`, `Close`, `Format`, ZIP central-directory eager parse, factory registration, new `opentile.FormatSZI` enum, `formats/all` registration. Tiler can open both fixtures and report Format() correctly; Levels() returns empty until T3.
- **T3**: `formats/szi/` Levels/Image/Tile — implement `opentile.Image` and `opentile.Level`; per-level dims; tile lookup via DZI path → ZIP entry → SectionReader; border-tile sizing.
- **T4**: `formats/szi/` Associated images + scan-properties.xml parser — `*associatedImage` with v0.15 Type() values; `Metadata` struct + `MetadataOf` accessor; cross-format Metadata population; vendor-prefixed property handling.
- **T5**: Tests + fixtures — wire CMU-1 + Grundium into slideCandidates; generate per-tile SHA JSON; new `tests/parity/szi_geometry_test.go`.
- **T6**: Docs + ship — `docs/formats/szi.md` (new); README supported-formats row; `docs/deferred.md §8j` retirement audit; `CHANGELOG.md [0.16.0]`; `CLAUDE.md` milestone bump.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding sample_files, docs)
- `make test` green
- `TestSlideParity` 30 fixtures green (was 28)
- Per-format probe (probed values pre-confirmed 2026-05-08):
  - Both fixtures: `Format() == "szi"`; first L0 tile bytes start with `FF D8 FF` (JPEG SOI).
  - Grundium fixture: `Tiler.Metadata().Magnification == 40`, `ScannerManufacturer == "Grundium"`, `ScannerModel == "Ocus"`, `MicronsPerPixel ≈ 0.25055239898989901`.
  - CMU-1 fixture: `Tiler.Metadata().Magnification == 2.5`, `ScannerManufacturer == "TestCompany"`, `ScannerModel == "Super Scan 2"` (spec-example values).
  - Both fixtures: `len(Tiler.Associated()) == 3` with `Type()` values `{"label", "overview", "thumbnail"}`.
  - `szi.MetadataOf(t).VendorProperties` may be empty on CMU-1 and non-empty on Grundium (Grundium probe will confirm during T4 implementation).

## 9. References

**SZI spec (authoritative):**
- `sample_files/szi/SZI format description - 2018-11-24.pdf` — 17-page V1.2 spec by Martin Weihrauch / Smart In Media. LGPL + CC-BY licensed.
- SZI-Format repo: https://github.com/smartinmedia/SZI-Format

**DZI spec (authoritative):**
- Microsoft Silverlight Deep Zoom File Format Overview (archived at MSDN cc645077): https://learn.microsoft.com/en-us/previous-versions/windows/silverlight/dotnet-windows-silverlight/cc645077(v=vs.95)
- OpenSeadragon DZI tile-source documentation: https://openseadragon.github.io/examples/tilesource-dzi/
- NIST DeepZoom paper: https://isg.nist.gov/deepzoomweb/resources/nist/paper/deepZoom_published004866_10.pdf

## 10. Active limitations introduced

None new. v0.16 is purely additive — new format reader, no behavior change for existing consumers. The four §11 backlog rows (R18 SZI, R19 DZI) — R18 retires here; R19 stays parked.

The §11 deferred-backlog co-design note (R18 / R19) supersedes once this spec ships; deferred §8j entry will explicitly note v0.16 retiring R18 with R19 still parked.

## 11. Lessons feeding into v0.16 execution

- **v0.12 BSD `sed` reliability:** every sed pass paired with a grep audit. Use `Edit` for surgical rewrites when sed silently misses identifiers.
- **v0.13 implementer self-interpretation:** verify with a separate probe before accepting an implementer's interpretation of unexpected results.
- **v0.14 agent-tool transient errors:** when result-delivery fails mid-task, the work is often complete on disk — verify inline (`git status`, `git diff`, `go test`) before re-dispatching.
- **v0.15 plan-vs-code drift:** when the plan asserts a structural detail (e.g., struct field naming), the implementer should still verify by reading the source. T5's `tileMagic`-field claim was wrong; implementer caught it correctly by reading the file first.
- **v0.16-specific caveat:** the SZI spec's worked example uses `ceil(log2(max)) + 1` as the level count; the *highest* level index is `ceil(log2(max))`. T1 implementer must wire this exactly per spec — off-by-one here propagates everywhere downstream.
