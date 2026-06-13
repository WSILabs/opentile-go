# Smart Zoom Image (SZI)

Smart In Media's open WSI format — a ZIP container around a Microsoft Deep Zoom Image (DZI) pyramid plus SZI-specific scan metadata and associated images. File extension `.szi`. Spec authored 2018 by [smartinmedia / pathozoom](https://github.com/smartinmedia/SZI-Format) and shipped under LGPL (reference reader/writer) + CC-BY (the spec PDF). Targeted by Grundium scanner output and other open-pipeline pathology workflows.

**v0.16 is the fourth opentile-go format beyond upstream Python opentile's coverage** (after BIF in v0.7, IFE in v0.8, generic-TIFF in v0.10, and Leica SCN in v0.11). Upstream doesn't read SZI.

## Format basics

- **Container**: a standard ZIP archive (PK signature). Single top-level directory inside the ZIP holds everything: a DZI manifest XML (`<name>.dzi`), a tile pyramid tree (`<name>_files/<level>/<col>_<row>.<jpeg|png>`), an optional `scan-properties.xml` for SZI-specific scan metadata, an optional `associated_images/` folder, and an optional `vendor/` folder.
- **Detection**: first 4 bytes equal `PK\x03\x04` (ZIP local-file-header magic). Implemented via `Factory.SupportsRaw(io.ReaderAt, int64) bool` — runs *before* `tiff.Open` in `opentile.Open`'s dispatch loop, so SZI files never get parsed as TIFF.
- **ZIP storage method**: per-spec, all entries are stored uncompressed (compression method 0). This lets opentile-go return tile bytes via `io.SectionReader` aliasing the ZIP file directly — no decompression, no copy on the hot path. v0.9's mmap-aliased fast path is preserved.
- **Pyramid shape**: bog-standard Microsoft Deep Zoom. Each level `L` is `2^L × 2^L` virtual maximum dimensions; the actual stored level dimensions halve from level `MaxLevel` (native, level 0 in opentile-go's API order) down to `1×1` (DZI level 0). Native size at level `MaxLevel = ceil(log2(max(W, H)))`. Tile size is `Manifest.TileSize` (typically 256 or 512); tile path on disk is `<name>_files/<L>/<col>_<row>.<ext>` (note **column-then-row**, per the DZI spec verbatim).
- **API ordering**: opentile-go exposes levels **native-first** (`Levels()[0]` = highest resolution), inverting the DZI on-disk ordering once at `Open` time.
- **Compression**: per-archive (not per-tile). DZI's `Format` attribute admits `jpeg` or `png`; v0.16 surfaces both via `Compression() == CompressionJPEG` or `CompressionPNG` (new in v0.16). Tile bytes are **self-contained** — each is a complete JPEG or PNG bytestream. No JPEGTables splice (unlike SVS / BIF).
- **Sparse tiles**: explicitly **not supported** by the SZI spec (page 4: *"sparse images and collections are not supported in the SZI format"*). A missing tile in the addressable grid returns `ErrCorruptArchive`. Bare DZI's sparse-tile concept isn't applicable to SZI's pre-defined grid.

## Architecture: ZIP-around-DZI

```
CMU-1.szi (ZIP)
└── CMU-1/
    ├── CMU-1.dzi                # DZI XML manifest (Format, TileSize, Overlap, Size)
    ├── CMU-1_files/             # Tile pyramid root
    │   ├── 0/                   # DZI level 0 (1×1 px)
    │   │   └── 0_0.jpeg
    │   ├── 1/                   # DZI level 1 (2×2 px)
    │   │   └── 0_0.jpeg
    │   ├── ...
    │   └── 15/                  # DZI MaxLevel (native; opentile-go Level 0)
    │       ├── 0_0.jpeg         # column 0, row 0
    │       ├── 0_1.jpeg         # column 0, row 1
    │       ├── 1_0.jpeg
    │       └── ...
    ├── scan-properties.xml      # Optional; SZI-specific scan metadata
    ├── associated_images/       # Optional
    │   ├── label.jpg
    │   ├── macro.jpg            # Type() == "overview" per v0.15
    │   └── thumbnail.jpg
    └── vendor/                  # Optional; opaque per-vendor data; ignored in v0.16
```

The `internal/dzi/` package owns the format-agnostic core (manifest XML parser + level-derivation + tile-coordinate math); `formats/szi/` owns the SZI-specific layering (ZIP central-directory walk, scan-properties.xml parser, associated-image classification).

## Fixture inventory

| File | Bytes | Manifest size | Levels | TileSize | Source |
|---|---:|---|---:|---:|---|
| `CMU-1.szi` | 1.5 MB | 32,914 × 27,243 | 16 | 256 | smartinmedia / SZI-Format spec repo |
| `scan_618_grundium_SZI.szi` | 709 MB | 147,456 × 81,920 | 19 | 512 | Grundium scanner |

CMU-1 is the canonical CC-BY-licensed Aperio reference slide re-encoded as SZI by the spec authors. scan_618 is a real Grundium-scanner-produced SZI exercising the high-level pyramid + scan-properties.xml metadata.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| ZIP central-directory parse at `Open` | ✅ | Eager — the lock-free hot-path invariant (v0.9) is preserved |
| Mmap-aliased tile fetch via uncompressed-stored entries | ✅ | Each tile is `io.NewSectionReader(file, off, len)` — no inflate, no copy |
| DZI manifest XML parse (`Format`, `TileSize`, `Overlap`, `Size`) | ✅ | `internal/dzi/manifest.go`; pure stdlib `encoding/xml` |
| Tile-coordinate math (`MaxLevel`, `LevelDims`, `GridDims`, `TilePath`) | ✅ | `internal/dzi/coords.go`; pure functions, zero I/O |
| Native-first level ordering | ✅ | DZI on-disk order is coarsest-first; opentile-go inverts at `Open` |
| `Tile`, `TileInto`, `TileReader` | ✅ | `TileInto` zero-alloc per the v0.9 `TileMaxSize`-sized buffer pattern |
| `TilePrefix` / `TileBodyInto` / `TileBodyMaxSize` (v0.13 splice API) | ✅ no-op | SZI tiles are self-contained JPEG/PNG; `TilePrefix()` returns `nil`; `TileBodyInto` delegates to `TileInto` per the v0.13 non-applicable convention |
| `Tiles(ctx)` row-major iterator | ✅ | Standard pattern |
| `WarmLevel(i)` page-cache pre-warm | ✅ | Standard v0.9 pattern |
| Compression detection (JPEG / PNG) | ✅ | `CompressionPNG` is **new in v0.16**; reports the codec accurately |
| Cross-format `opentile.Metadata` from `scan-properties.xml` | ✅ | `ScannerManufacturer`, `ScannerModel`, `Magnification`, `ScannerSerial`, `AcquisitionDateTime`, `ScannerSoftware` |
| Format-specific `szi.Metadata` via `szi.MetadataOf(tiler)` | ✅ | See "Metadata" below |
| `VendorProperties map[string]string` for `vendor.<key>` properties | ✅ | Per-spec open-ended namespace; surfaced verbatim including the dotted prefix |
| Associated images: `label.jpg`, `macro.jpg`, `thumbnail.jpg` | ✅ | `macro.jpg` filename → `Type() == "overview"` per v0.15 alignment |
| Cross-backing parity (mmap default vs pread) | ✅ | `tests/parity/szi_geometry_test.go` |

## Edge tile semantics

Per the DZI spec referenced by SZI: *"border tiles (outermost right and bottom of the image) may have a different size from the standard tile to match the image width/height."* opentile-go honors this — corner tiles decode to their actual content dimensions. **Example:** `CMU-1.szi`'s L0 corner tile is a 172×151 JPEG (image bounds 2220×2967, TileSize 256, corner = 9×12 grid cell). NOT 256×256 padded.

The same per-tile content formula applies as on padded formats; on SZI it matches the decoded JPEG/PNG dimensions exactly:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
// On SZI: this matches the actual decoded tile dimensions.
// On TIFF-based formats: the decoded tile is at full TileSize and
// the consumer must clip — see docs/formats/{svs,ndpi,...}.md.
```

This is one of the few places SZI's behavior differs from the TIFF-based formats opentile-go reads. Other format readers (SVS, NDPI, Philips, OME-TIFF, BIF, IFE, Leica SCN, generic TIFF) return full-tile-padded edge tiles per the underlying file format.

## What's not supported

| Capability | Status | Why |
|---|---|---|
| Sparse SZI files | ❌ explicitly out per spec | SZI spec page 4: *"sparse images and collections are not supported in the SZI format."* A missing tile returns `ErrCorruptArchive`. Breadcrumbs left for an opt-in lenient mode + `ErrTileMissing` sentinel if a real sparse-SZI fixture surfaces |
| DZC (Deep Zoom Collection) files | ❌ permanent | DZC is a multi-image format with Morton-laid-out shared thumbnails; opentile-go reads single-WSI files only |
| Bare DZI (filesystem-backed, no ZIP wrapper) | ❌ deferred to v0.17+ | The `internal/dzi/` extraction pre-pares it; trigger is consumer demand or owner sign-off (R19) |
| `vendor/` folder content surfacing | ❌ deferred | The folder is per-spec opaque per-vendor data. v0.16 ignores it; deferred until consumer signal |
| DZI JSON manifest variant | ❌ — XML-only | OpenSeadragon supports both XML and JSON manifests; SZI's spec mandates XML, so v0.16 is XML-only. JSON is a follow-on if a fixture demands |
| Non-zero DZI `Overlap` attribute | ⚠️ — passthrough | opentile-go's contract is no-overlap; SZI's spec mandates `Overlap=0`. CMU-1 and scan_618 both honor this. A non-zero-overlap file would pass through with whatever the on-disk geometry says (consumer beware) |
| Compressed-stored ZIP entries | ❌ rejected at `Open` | SZI spec mandates uncompressed-stored (method 0). A deflate-stored entry breaks the mmap-aliased fast path; reader rejects on the spec mandate |

## Metadata

`scan-properties.xml` carries SZI-specific scan metadata. The reader populates two layers:

**Cross-format `opentile.Metadata`** (read via `tiler.Metadata()`):

- `ScannerManufacturer` ← `<VendorName>`
- `ScannerModel` ← `<ScannerName>`
- `Magnification` ← `<ObjectiveMagnification>`
- `ScannerSerial` ← `<ScannerSerialNo>`
- `AcquisitionDateTime` ← `<TimeStart>`
- `ScannerSoftware` ← `"<SoftwareName> <SoftwareVersion>"` (single-element slice)
- `MPP.X` / `MPP.Y` ← `<MicronsPerPixelX>` / `<MicronsPerPixelY>` (added in v0.17)
- `MPP.Symmetric()` ← X when X == Y, else 0 (Q2 smart-MPP gate; added in v0.17)
- `ImageDescription` ← empty for SZI (use `Properties[PropertyComments]` for free-text)
- `Properties[PropertyCaseNumber]` ← `<CaseNumber>` (added in v0.17)
- `Properties[PropertyUserName]` ← `<UserName>` (added in v0.17)
- `Properties[PropertyScannedAreaMM2]` ← `<ScannedArea>` (added in v0.17)
- `Properties[PropertyScanDurationSec]` ← parsed `<ElapsedTime>` "XhYmZs" → seconds (added in v0.17)
- `Properties[PropertyComments]` ← `<Comments>` (added in v0.17)
- `Writer` ← `"<SoftwareName> <SoftwareVersion>"` combined (added in v0.20; empty when SoftwareName absent, e.g., Grundium SZI)

**SZI-specific `szi.Metadata`** (read via `szi.MetadataOf(tiler)`):

- Embedded `opentile.Metadata` (so consumers read both layers from one value; cross-format-canonical fields above flow through field promotion)
- `Version`, `Date` — `<image>` element attributes
- `SoftwareName`, `SoftwareVersion`
- `TimeStart`, `TimeEnd`, `ElapsedTime` (raw "XhYmZs" string preserved per Q4 Option B)
- `ScanJobName`, `ScannerSerialNo`
- `CameraName`, `SensorPixelSize` (µm)
- `ScanWidth`, `ScanHeight` (mm)
- **`VendorProperties map[string]string`** — open-ended `vendor.<key>` properties per spec page 9: *"Just add your scanner name before the field name, separated by a dot, e.g., 'vendor.MicronsX' or 'ScanCompany.FilterName'."* Keys surface as-is including the dotted prefix.

v0.17 cleanup (per Q4 Option B): the format-specific `szi.Metadata` no longer carries `MicronsPerPixel`, `MicronsPerPixelX/Y`, `Comments`, `UserName`, `CaseNumber`, or `ScannedArea` — those are cross-format-canonical and flow through the embedded `opentile.Metadata` (now as `MPP.X`/`MPP.Y`). Behavior change vs v0.16: anisotropic SZI now leaves `MPP.Symmetric() = 0` (was previously averaging X / Y); per Q2 smart-MPP-only-when-X==Y.

Notes:

- The XML parser is lenient: missing fields land as zero values; malformed numerics are silently skipped.
- The namespace varies in real-world SZI files (spec page lists `http://www.pathozoom.com/SZI`, the CMU-1 fixture uses lowercase `http://www.pathozoom.com/szi`); the parser matches local element names regardless of namespace.

## Associated images

SZI's optional `associated_images/` folder carries up to three named JPEGs:

| Filename | `Type()` value | Notes |
|---|---|---|
| `label.jpg` | `"label"` | Identical to other formats |
| `macro.jpg` | `"overview"` | Filename is `macro.jpg` (per spec) but exposed as `"overview"` per the v0.15 canonical-naming Q5 seal — aligns with DICOM PS3.3 + upstream Python opentile + 6 sibling format readers |
| `thumbnail.jpg` | `"thumbnail"` | Identical to other formats |

All entries are JPEG; consumers receive raw JPEG bytes via `AssociatedImage.Bytes()`.

## Parity

Two layered oracles cover v0.16 SZI correctness, both running in `make test`:

1. **Sample-tile SHA256 fixtures** (`tests/integration_test.go::TestSlideParity`) — both fixtures. Records per-tile SHA256 hashes in `tests/fixtures/CMU-1.szi.json` and `tests/fixtures/scan_618_grundium_SZI.szi.json`. Catches regressions in our own output.

2. **Geometry pinning + cross-backing parity** (`tests/parity/szi_geometry_test.go`) — both fixtures. Pins per-level Size / TileSize / Grid / Compression, AssociatedImage Type + sizes, Metadata fields (ScannerManufacturer, ScannerModel, Magnification, MPP, VendorProperties presence), and tile-byte equality across mmap / pread backings.

No upstream byte-equality oracle: SZI is beyond Python opentile's coverage. The smartinmedia reference reader is read-for-understanding only.

## Deviations from upstream Python opentile

Upstream Python opentile doesn't read SZI, so every v0.16 behaviour in this package is technically a deviation. The interesting one — captured in [`docs/deferred.md` §1a](../deferred.md#1a-deviations-from-upstream-python-opentile) — is:

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Smart Zoom Image (SZI) reader | v0.16 | not opt-out-able once registered | First ZIP-backed format opentile-go reads; first format to surface `CompressionPNG`. Spec-mandated uncompressed-stored ZIP entries preserve the v0.9 mmap-aliased fast path |

## Implementation references

- Our package: `formats/szi/`
- Public API: `szi.New() opentile.FormatFactory` + the standard `Tiler` / `Image` / `Level` / `AssociatedImage` interfaces.
- Our metadata accessor: `szi.MetadataOf(opentile.Tiler) (*Metadata, bool)` — exposes the SZI-specific fields above + `VendorProperties` map in addition to the embedded `opentile.Metadata` cross-format fields.
- DZI shared core: `internal/dzi/` — `Manifest`, `ParseManifest`, `MaxLevel`, `LevelDims`, `GridDims`, `TilePath`. Pure-function shape; designed to underpin a future `formats/dzi/` filesystem-backed reader without compromise to either side.
- v0.16 SZI design: [`docs/superpowers/specs/2026-05-08-opentile-go-v16-szi-design.md`](../superpowers/specs/2026-05-08-opentile-go-v16-szi-design.md).
- v0.16 SZI plan: [`docs/superpowers/plans/2026-05-08-opentile-go-v16-szi.md`](../superpowers/plans/2026-05-08-opentile-go-v16-szi.md).

## References

- **SZI spec PDF**: `sample_files/szi/SZI format description - 2018-11-24.pdf` (17 pages; gitignored). Authored by smartinmedia / pathozoom; LGPL (reference code) + CC-BY (spec).
- **smartinmedia/SZI-Format repo**: <https://github.com/smartinmedia/SZI-Format> — reference reader/writer + CMU-1 spec fixture.
- **Microsoft Deep Zoom File Format Overview**: <https://learn.microsoft.com/en-us/previous-versions/windows/silverlight/dotnet-windows-silverlight/cc645077(v=vs.95)> — the underlying DZI format authored by Microsoft for Silverlight; documents tile-naming convention (`<col>_<row>.<format>`), level derivation rule, and the DZC collections format (out-of-scope for opentile-go).
- **OpenSeadragon DZI tile-source**: <https://openseadragon.github.io/examples/tilesource-dzi/> — practical reference for DZI manifest variants (XML and JSON) and typical Format/TileSize/Overlap defaults.

## Known issues + history

- **`MicronsPerPixel` is on `szi.Metadata` only**, not on the cross-format `opentile.Metadata`. Tracked as `docs/deferred.md` §11 R20 — every format reader has an MPP equivalent in its format-specific metadata, but the cross-format struct doesn't yet expose one. v0.16 didn't expand cross-format API in scope.
- **`Comments` (free-form SZI scan note) is on `szi.Metadata` only**, for the same reason.
- **`vendor/` folder content is per-spec opaque** and ignored in v0.16. If a real consumer needs typed access to a specific vendor's content, the path is a v0.17+ format-specific accessor mirroring `szi.Metadata.VendorProperties`.

See [`docs/deferred.md`](../deferred.md) §8j for the full v0.16 retirement audit.
