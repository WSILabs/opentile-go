# COG-WSI

Cloud Optimized GeoTIFF for Whole-Slide Imaging — a strict profile that layers WSI-domain semantics on top of the GDAL [Cloud Optimized GeoTIFF (COG)](https://www.cogeo.org/) structure. File extension `.tiff`. Spec v0.1 authored by the user as part of the wsi-tools converter pipeline; spec lives at [`docs/specs/2026-05-20-cog-wsi-format.md`](../specs/2026-05-20-cog-wsi-format.md).

**v0.19 is the fifth opentile-go format beyond upstream Python opentile's coverage** (after BIF in v0.7, IFE in v0.8, generic-TIFF in v0.10, Leica SCN in v0.11, and SZI in v0.16). Closes GH issues [#5](https://github.com/wsilabs/opentile-go/issues/5) and [#6](https://github.com/wsilabs/opentile-go/issues/6).

## Format basics

- **TIFF dialect**: classic TIFF or BigTIFF; either is detected automatically (header-length dispatch picks the ghost-area offset).
- **Detection**: a `COG_WSI_VERSION=<x.y>` token in the GDAL ghost area immediately following the TIFF header. Detected via `Factory.Supports(*tiff.File)` — runs after vendor-specific factories (SVS, NDPI, etc.) and registered **before** `generictiff.Factory` so a conformant COG-WSI file is claimed here rather than slipping into the generic catch-all.
- **Architecture**: GDAL Cloud Optimized GeoTIFF base structure (tiled IFDs, internal overviews, IFDs-before-data layout, COG ghost-area marker) **plus** WSI-domain private tags 65080-65087 and the spec-mandated IFD ordering (base pyramid → overviews → associated images). Generic COG files without the WSI extensions continue to read via the `generic-tiff` reader.
- **Source format preserving**: the writer (user's wsi-tools converter) byte-passthroughs source-format tiles into the COG-WSI container — JPEG, JPEG 2000, LZW, etc. — preserving the on-disk codec exactly. Cross-fixture parity gate confirms tile bytes match the original source format byte-for-byte across all 10 fixtures.
- **Pyramid layout**: spec-mandated. Base pyramid IFDs come first, ordered native-first; overview IFDs follow; associated-image IFDs trail. The `WSILevelIndex` tag (65082) makes the ordering authoritative — the reader trusts the tag rather than re-deriving from geometry.
- **Integer-multiple pyramid ratios**: COG-WSI permits clean integer-multiple downscale chains (1×, 2×, 4×, 8×, 16×, …) per level transition. `internal/tiff/classify_pyramid.go::isIntegerMultipleRatio` accepts these. This is a v0.19 generic-TIFF relaxation (Issue #5 standalone benefit): pre-v0.19 the strict drift check rejected mixed-ratio chains, which caused Aperio / Grundium SVS-style 4×/2×/2× pyramids transcoded through the WSI-tools pipeline to fail validation.

## Architecture: COG + WSI extensions

```
file.tiff
├── TIFF / BigTIFF header (8 or 16 bytes)
├── GDAL ghost area (text key-value block, optional but present in COG-WSI)
│   ├── GDAL_STRUCTURAL_METADATA_SIZE=...
│   ├── LAYOUT=IFDS_BEFORE_DATA
│   ├── BLOCK_ORDER=ROW_MAJOR
│   ├── BLOCK_LEADER=SIZE_AS_UINT4
│   ├── BLOCK_TRAILER=LAST_4_BYTES_REPEATED
│   ├── KNOWN_INCOMPATIBLE_EDITION=NO
│   ├── MASK_INTERLEAVED_WITH_IMAGERY=YES   (when masks present)
│   └── COG_WSI_VERSION=0.1                  ◀ COG-WSI marker
├── IFD chain (IFDs-before-data per COG)
│   ├── IFD 0  WSILevelIndex=0    base pyramid level 0 (native)
│   ├── IFD 1  WSILevelIndex=1    reduced level 1
│   ├── ...
│   ├── IFD N  WSIImageType="overview"      associated overview
│   ├── IFD N+1 WSIImageType="label"        associated label
│   └── IFD N+2 WSIImageType="thumbnail"    associated thumbnail
└── (tile / strip data trailer)
```

`internal/cog/` owns the format-agnostic ghost-area parser (`GhostArea`, `ParseGhostArea`, `ParseCOGWSIVersion`, `ErrGhostAreaMalformed`); `internal/tiff` exposes typed accessors for the 8 WSI private tags (65080-65087); `formats/cogwsi/` owns the COG-WSI-specific layering (detection, validation, metadata, level/image construction). The `cogwsi.Tiler` delegates tile reads to an inner `generictiff.Tiler` via the `UnwrapTiler` convention; the wrapper exists to (a) self-report `Format() == "cog-wsi"`, (b) enforce spec validation at open, and (c) populate canonical metadata from the WSI private tags rather than re-deriving from the underlying TIFF.

## WSI private tags (spec §5.2)

Tags 65080-65087 carry WSI-domain semantics that generic-TIFF + COG don't model:

| Tag ID | Name | Type | Purpose |
|---:|---|---|---|
| 65080 | `WSIImageType` | ASCII | One of `"base"`, `"label"`, `"overview"`, `"thumbnail"`, `"macro"`. Authoritative — overrides heuristic classification. `"macro"` maps to associated-image type `"overview"` per v0.15 canonical-naming alignment. |
| 65081 | `WSIPyramidIndex` | LONG | Reserved for multi-pyramid files; unused in v0.19 single-pyramid fixtures. |
| 65082 | `WSILevelIndex` | LONG | 0-based level within the base pyramid (0 = native). Drives native-first level ordering without re-deriving from IFD geometry. |
| 65083 | `WSIMPP` | DOUBLE | Symmetric microns-per-pixel (when X == Y). |
| 65084 | `WSIMPPX` | DOUBLE | Per-axis microns-per-pixel X. |
| 65085 | `WSIMPPY` | DOUBLE | Per-axis microns-per-pixel Y. |
| 65086 | `WSIMagnification` | DOUBLE | Scanner objective magnification (e.g., 20×, 40×). |
| 65087 | `WSISourceFormat` | ASCII | Source format the COG-WSI was transcoded from (`"svs"`, `"ndpi"`, `"philips-tiff"`, …). Surfaced via `Properties["cog-wsi.source-format"]`. |

Pyramid construction short-circuits when every tiled IFD carries `WSILevelIndex`: the reader trusts the tag rather than running the geometric-scale-chain validator. Mixed files (some IFDs tagged, some not) fail with `ErrNotConformantCOGWSI`.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Ghost-area parse at `Open` | ✅ | `internal/cog/ParseGhostArea`; max read 16 KiB |
| `COG_WSI_VERSION` token dispatch | ✅ | Reader detects v0.1; future versions trigger `ErrNotConformantCOGWSI` until version-aware extensions land |
| Tiled levels (any compression) | ✅ passthrough | Source-format preserving (JPEG / JP2K / LZW / Deflate / WebP / JPEG XL / AVIF / HTJ2K) — same compression matrix as generic-TIFF |
| Native-first level ordering | ✅ | Driven by `WSILevelIndex` tag, not geometric inference |
| `Tile`, `TileInto`, `TileReader` | ✅ | Delegated to inner `generictiff.Tiler`; zero-alloc per v0.9 `TileMaxSize` buffer pattern |
| `TilePrefix` / `TileBodyInto` / `TileBodyMaxSize` (v0.13 splice API) | ✅ | Delegated; behaviour matches generic-TIFF for the underlying codec |
| `Tiles(ctx)` row-major iterator | ✅ | Standard pattern |
| `WarmLevel(i)` page-cache pre-warm | ✅ | Standard v0.9 pattern |
| Cross-format `opentile.Metadata` | ✅ | Populated from WSI private tags (see Metadata below) |
| Associated images: label / overview / thumbnail | ✅ | Classification driven by `WSIImageType`; `"macro"` → type `"overview"` per v0.15 |
| Spec validation at `Open` | ✅ | `ErrNotConformantCOGWSI` returned on ghost-area / IFD-ordering / WSI-tag violations |
| Source-format preservation parity gate | ✅ | Cross-fixture: each `<source>_cog-wsi.tiff` tile bytes vs the original `<source>` confirms bit-exact passthrough |
| BigTIFF | ✅ | Header-length dispatch (8-byte classic vs 16-byte BigTIFF) picks the right ghost-area offset |

## Edge tile semantics

COG-WSI is a TIFF-based format. Tiles are stored at full `TileSize` regardless of position; right-edge and bottom-edge tiles include padding bytes in the unused region. The reader returns the bytes verbatim per the byte-passthrough invariant. Consumers should clip rendered output to the meaningful sub-rect:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
```

Same model as SVS / NDPI / Philips / OME-TIFF / BIF / IFE / Leica SCN / generic TIFF. (SZI is the only opentile-go reader that returns border-sized tiles per spec.)

## Metadata

WSI private tags drive canonical metadata population. The reader populates the cross-format `opentile.Metadata` directly from the tags — no per-source-format dispatch.

**Cross-format `opentile.Metadata`** (read via `tiler.Metadata()`):

- `MicronsPerPixelX` / `MicronsPerPixelY` ← `WSIMPPX` / `WSIMPPY` (tags 65084 / 65085)
- `MicronsPerPixel` ← `WSIMPP` (tag 65083) when X == Y; else 0 per the v0.17 symmetric-MPP convention
- `Magnification` ← `WSIMagnification` (tag 65086)
- `Properties["cog-wsi.source-format"]` ← `WSISourceFormat` (tag 65087)
- `Properties["cog-wsi.wsitools-version"]` ← writer-software identifier when present
- `Writer` ← `"wsitools/<WSIToolsVersion>"` from private tag 65084 (added in v0.20; the file producer — distinct from `ScannerManufacturer` which preserves the source-scanner attribution per spec)
- `Properties["cog-wsi.spec-version"]` ← parsed `COG_WSI_VERSION` ghost-area token (e.g., `"0.1"`)
- `ImageDescription` ← page 0 ImageDescription verbatim (when present; typically empty in v0.1 writer output)

**Generic-TIFF format-specific metadata** is reachable via `generictiff.MetadataOf(t)` — the `UnwrapTiler` convention forwards through the cogwsi wrapper to the inner generic-TIFF Tiler.

**Probe example** (CMU-1-Small-Region_cog-wsi.tiff):

```
Format:           cog-wsi
Levels:           1
Associated:       3 (thumbnail, label, overview)
MicronsPerPixelX: 0.499
MicronsPerPixelY: 0.499
MicronsPerPixel:  0.499 (symmetric)
Magnification:    20
Properties["cog-wsi.source-format"]:    "svs"
Properties["cog-wsi.wsitools-version"]: "0.6.0-dev"
Properties["cog-wsi.spec-version"]:     "0.1"
```

## Spec validation

The COG-WSI spec is the contract; the reader doesn't bend. Files that fail conformance return `cogwsi.ErrNotConformantCOGWSI` at `Open` time. Validation runs in two stages:

1. **`validateGhost`** — required ghost-area keys present, `COG_WSI_VERSION` parses cleanly, `LAYOUT=IFDS_BEFORE_DATA` (the COG cornerstone), `KNOWN_INCOMPATIBLE_EDITION=NO`.
2. **`validateIFDs`** — IFD ordering per spec (base pyramid native-first → overviews → associated images), every tiled IFD carries `WSILevelIndex` or none do (mixed rejected), `WSIImageType` values are within the spec's enumeration.

Generic COG files without the WSI extensions (the "plain COG" case) fall through to `generictiff.Factory` and continue to read as structurally-valid pyramid TIFFs, just without COG-WSI-canonical metadata.

## Associated images

| `WSIImageType` (tag 65080) | `Type()` value | Notes |
|---|---|---|
| `"label"` | `"label"` | Identical to other formats |
| `"overview"` | `"overview"` | Identical to other formats |
| `"thumbnail"` | `"thumbnail"` | Identical to other formats |
| `"macro"` | `"overview"` | Per v0.15 canonical-naming Q5 seal — `"macro"` is a writer-side label; opentile-go exposes it as `"overview"` to align with DICOM PS3.3 + upstream Python opentile + sibling format readers |

Compression of associated images is source-format-preserving — typically JPEG for converted SVS / NDPI / OME-TIFF sources; uncompressed for some Ventana overviews (the Ventana-1 fixture overview is ~13.8 MB uncompressed RGB, preserved through the converter).

## Fixture inventory

10 COG-WSI fixtures wired into `TestSlideParity` (30 → 40 fixtures in v0.19), one transcoded from each source-format fixture opentile-go reads:

| File | Source format | Source fixture |
|---|---|---|
| `CMU-1-Small-Region_cog-wsi.tiff` | SVS | `CMU-1-Small-Region.svs` |
| `CMU-1_cog-wsi.tiff` | SVS | `CMU-1.svs` |
| `JP2K-33003-1_cog-wsi.tiff` | SVS | `JP2K-33003-1.svs` (JPEG 2000 codestream preserved; tile magic = J2K SOC) |
| `scan_620_cog-wsi.tiff` | SVS (Grundium BigTIFF) | `scan_620_.svs` |
| `svs_40x_bigtiff_cog-wsi.tiff` | SVS (Grundium BigTIFF) | `svs_40x_bigtiff.svs` (no label associated) |
| `Philips-1_cog-wsi.tiff` | Philips TIFF | `Philips-1.tiff` (no label associated) |
| `Leica-1_cog-wsi.tiff` | OME-TIFF | `Leica-1.ome.tiff` |
| `Ventana-1_cog-wsi.tiff` | BIF | `Ventana-1.bif` (overview is uncompressed RGB ~13.8 MB) |
| `cervix_2x_jpeg_cog-wsi.tiff` | IFE | `cervix_2x_jpeg.iris` |
| `scan_617_cog-wsi.tiff` | SZI | `scan_617_grundium_SZI.szi` |

`CMU-1-Small-Region_cog-wsi.tiff` is the full-walk fixture; the other 9 are sampled by default. Sources span 7 of opentile-go's 9 reader formats — NDPI and Leica SCN aren't represented in the v0.19 fixture slate (no consumer signal; trigger-driven addition for v0.19+).

## What's not supported

| Capability | Status | Why |
|---|---|---|
| Writing COG-WSI files | ❌ permanent | opentile-go is a reader-only library. Use the user's wsi-tools converter (or any GDAL-based pipeline emitting the COG-WSI extensions) to produce conformant files. |
| Multi-channel (`WSIImageType=base` with C > 1) | ❌ deferred | v0.1 spec single-channel only; multi-channel fluorescence may arrive in spec v0.2+ |
| Multi-Z stacks | ❌ deferred | Same axis as above; v0.1 single-plane only |
| Plain generic COG awareness (`Format() == "cog"`) | ❌ permanently YAGNI | Generic COG files (no WSI extensions) continue to read via `generic-tiff` as structurally-valid pyramid TIFFs. opentile-go is WSI-domain; geospatial COG isn't our domain. Tracked as R21 — **fully retired in v0.19** per the v0.19 brainstorm seal. |
| Standalone CLI for inspection | ❌ — library only | Use `go test ./tests/...` or the user's wsi-tools / opentile-cli wrapper for inspection. |
| Future `COG_WSI_VERSION` ≥ 0.2 | ⚠️ rejects | v0.19 hardcodes v0.1; non-0.1 versions fail with `ErrNotConformantCOGWSI` until version-aware extensions ship. Defensive — better to fail loudly than silently misread a spec the reader doesn't know. |

## Parity

Two layered oracles cover v0.19 COG-WSI correctness:

1. **Sample-tile SHA256 fixtures** (`tests/integration_test.go::TestSlideParity`) — all 10 fixtures. Records per-tile SHA256 hashes in `tests/fixtures/*_cog-wsi.tiff.json`. Catches regressions in our own output.

2. **Geometry pinning + cross-fixture parity** (`tests/parity/cogwsi_geometry_test.go`) — all 10 fixtures. Pins per-level Size / TileSize / Grid / Compression, AssociatedImage Type + sizes, Metadata fields. The cross-fixture parity check confirms each `<source>_cog-wsi.tiff` tile bytes equal the original `<source>` file's tile bytes — proving the writer's source-format-preserving invariant and our reader's byte-passthrough invariant agree.

No upstream byte-equality oracle: COG-WSI is beyond Python opentile's coverage.

## Deviations from upstream Python opentile

Upstream Python opentile doesn't read COG-WSI, so the v0.19 reader is technically a deviation. The interesting one — captured in [`docs/deferred.md` §1a](../deferred.md#1a-deviations-from-upstream-python-opentile) — is:

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| COG-WSI reader | v0.19 | not opt-out-able once registered | First spec-validated COG-profile reader opentile-go ships; pairs WSI-domain semantics (private tags 65080-87 + `COG_WSI_VERSION` marker) with the GDAL Cloud Optimized GeoTIFF base structure. Generic COG files continue to read via `generic-tiff`. |
| Integer-multiple pyramid ratio acceptance | v0.19 | not opt-out-able | Generic-TIFF standalone benefit from Issue #5 part B: pre-v0.19 the strict drift check rejected mixed-ratio chains (e.g., Aperio / Grundium SVS 4×/2×/2×); v0.19 accepts clean integer-multiple ratios per level. |

## Implementation references

- Our package: `formats/cogwsi/`
- Public API: `cogwsi.New() opentile.FormatFactory` + the standard `Tiler` / `Image` / `Level` / `AssociatedImage` interfaces.
- Validation: `cogwsi.ErrNotConformantCOGWSI` sentinel; `validateGhost` + `validateIFDs` in `formats/cogwsi/validation.go`.
- Generic-TIFF unwrap: `cogwsi.Tiler.UnwrapTiler() opentile.Tiler` exposes the inner generic-TIFF Tiler for callers that want `generictiff.MetadataOf`.
- Ghost-area parser: `internal/cog/` — `GhostArea`, `ParseGhostArea`, `ParseCOGWSIVersion`, `ErrGhostAreaMalformed`.
- WSI private tag accessors: `internal/tiff` — 8 typed methods on `*tiff.Page` covering tags 65080-65087.
- v0.19 COG-WSI design: [`docs/superpowers/specs/2026-05-20-opentile-go-v19-cog-wsi-design.md`](../superpowers/specs/2026-05-20-opentile-go-v19-cog-wsi-design.md).
- v0.19 COG-WSI plan: [`docs/superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md`](../superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md).

## References

- **COG-WSI spec v0.1**: [`docs/specs/2026-05-20-cog-wsi-format.md`](../specs/2026-05-20-cog-wsi-format.md) (authored by the user as part of the wsi-tools converter pipeline).
- **wsi-tools converter**: produces COG-WSI files from SVS / NDPI / Philips / OME-TIFF / BIF / IFE / SZI sources; preserves source-format tile bytes bit-exact per spec.
- **GDAL Cloud Optimized GeoTIFF**: <https://gdal.org/drivers/raster/cog.html> — the COG profile + ghost-area conventions COG-WSI extends.
- **cogeo.org**: <https://www.cogeo.org/> — community resources for the COG profile.
- **OGC 21-026**: <https://docs.ogc.org/is/21-026/21-026.html> — GeoTIFF 1.1 / Cloud Optimized GeoTIFF spec (the OGC standardization track).

## Known issues + history

- **R21 (general COG first-class support)** — **fully retired in v0.19**. The COG-WSI reader covers the WSI-context demand; plain COG awareness is permanently YAGNI for opentile-go (we're WSI-domain, not geospatial). Generic COG files continue to read via `generic-tiff` as structurally-valid pyramid TIFFs.
- **Closes Issues [#5](https://github.com/wsilabs/opentile-go/issues/5) and [#6](https://github.com/wsilabs/opentile-go/issues/6)** — #5 was the integer-multiple pyramid ratio relaxation + WSI-tag awareness for generic-tiff; #6 was the dedicated cogwsi reader.
- **scan_620_grundium_TIFF geometry catch-up** (v0.19 T3/T4) — the v0.10 expectation pinned 3 levels; the file actually has 4. Fixed during the integer-multiple ratio relaxation work.
- **Stale-since-v0.17 fixture catch-ups** (v0.19 T3/T4) — `Hamamatsu-1.ndpi` + `OS-2.ndpi` gained `scanner_serial`; `Leica-1.ome.tiff` + `Leica-2.ome.tiff` gained `acquisition_rfc3339`. No reader-side regressions; the fixtures were just stale.

See [`docs/deferred.md`](../deferred.md) §8m for the full v0.19 retirement audit.
