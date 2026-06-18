# Generic TIFF (catch-all)

Catch-all reader for tiled pyramidal TIFF files without vendor metadata. Activates on any TIFF whose IFD layout passes the v0.10 pyramid validator (≥3 tiled IFDs forming a coherent geometric chain) and whose IFDs sit on the photometric/sample/compression whitelist. Registered LAST in the dispatch order so vendor format detectors (SVS, NDPI, Philips, OME, BIF) get first crack at any TIFF.

**v0.10 is the first opentile-go format with no upstream Python opentile counterpart.** Upstream's factory list is enumerated vendor formats only — files outside that list raise `NotSupportedTilerError`. The generic reader fills that gap for opentile-go consumers who want any structurally valid pyramid TIFF to "just work."

## Format basics

- **TIFF dialect**: classic TIFF or BigTIFF; both read transparently via `internal/tiff`.
- **Detection**: structural, not vendor-tag-based. `Factory.Supports(file)` calls `internal/tiff.ClassifyPyramid` against the file's top-level IFDs and returns true iff the validator finds a coherent pyramid. Conservative: when in doubt, return false.
- **Pyramid layout**: top-level IFDs only (SubIFDs are OME-specific). The validator's greedy-chain algorithm sorts tiled IFDs by area (largest-first), then walks them in order accepting each next IFD whose downsample ratio matches the running chain ratio within tolerance.
- **Validation thresholds (sealed in v0.10 spec Q1/Q2/Q7)**:
    - `MinLevels = 3` — single-level and 2-level files are rejected (`ErrPyramidTooFewLevels`).
    - `InterAxisTolerance = ±2%` — anisotropic downsampling within a level is rejected (legitimate WSI never anisotropic).
    - `InterLevelTolerance = ±5%` — drift between consecutive scale ratios bounded; tolerates ceil/floor rounding on odd dimensions.
    - `MaxLeftoverTiled = 2`, `LeftoverTiledMaxAreaRatio = 1%` — files with ≥2 leftover tiled IFDs above 1% of baseline area reject as multi-pyramid (`ErrPyramidMultiplePyramid`); OME's reader handles those legitimately.
- **Compression whitelist (sealed in v0.10 spec §4.6)**: JPEG (7), JP2K (33003), LZW (5), Deflate (8) / Adobe Deflate (32946), None (1). Other values yield `ErrPyramidCompression`.
- **Photometric whitelist**: RGB (2), YCbCr (6), grayscale BlackIsZero (1) / WhiteIsZero (0). 8-bit per sample only (`BitsPerSample = 8`).
- **JPEG splice**: pyramid IFDs that carry shared `JPEGTables` (tag 347) get the v0.9 in-place splice template applied per tile (`internal/jpeg.BuildSplicePrefix` + `InsertPrefixInPlace`); zero-alloc on `TileInto`. JPEG IFDs without shared tables and non-JPEG compressions pass through verbatim.
- **Storage order**: TileOffsets is in standard image-space row-major order — no per-frame stripe reassembly (NDPI) or other vendor-specific tile reordering. (BIF is also row-major since v0.45.3; see #57.)

## Fixture inventory

| File | Bytes | Origin | Levels | Associated images |
|---|---:|---|---:|---|
| `CMU-1.tiff` | 195 MB | tifffile-stripped derivative of `CMU-1.svs` | 9 (JPEG) | 0 (associated IFDs dropped during the strip) |
| `CMU-1.stripped.tiff` | 169 MB | T2-generated derivative; pyramid preserved + 3 stripped associated IFDs re-encoded | 3 (JPEG, 4× scale) | thumbnail (JPEG, 46-strip), label (LZW, multi-strip), macro (JPEG, 27-strip) |

Both fixtures live under `sample_files/generic-tiff/`. The first exercises the pyramid-only fast path; the second exercises the multi-strip JPEG concat (T8) and multi-strip LZW re-encode (T8) reader paths against fixture-derived ground truth.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Pyramid detection + validation | ✅ | `internal/tiff.ClassifyPyramid` with sealed thresholds; covered by `formats/generictiff/classifier_test.go` against synthetic + real fixtures |
| Tiled JPEG / JP2K / LZW / Deflate / None pyramid levels | ✅ | `tiledImage` Level passes through verbatim; JPEG with shared `JPEGTables` uses the v0.9 in-place splice template (zero-alloc TileInto) |
| Multi-strip associated images | ✅ | Single-strip passthrough; multi-strip uncompressed concat; multi-strip JPEG concat (libtiff RST-marker layout); multi-strip LZW decode + re-encode (lifted from `formats/svs/lzwlabel.go` pattern) |
| Heuristic associated-image classifier | ✅ | LZW = label, wide-aspect JPEG = overview, smaller-square JPEG = thumbnail; fallback `TypeAssociated` ("associated") |
| Format-specific metadata via `generictiff.MetadataOf` | ✅ | `MPP.X`/`MPP.Y` (from XResolution + ResolutionUnit), `ImageDescription` verbatim |
| Cross-format Metadata via `Tiler.Metadata()` | ✅ | `Make` (271) → ScannerManufacturer; `Model` (272) → ScannerModel; `Software` (305) → ScannerSoftware (delimiter-split); `DateTime` (306) → AcquisitionDateTime |
| ICC profile passthrough | ✅ | `Tiler.ICCProfile()` returns level-0 IFD's tag 34675 verbatim (nil if absent) |
| `WarmLevel(i)` page-cache pre-warm | ✅ | Standard v0.9 pattern via the `tiledImage.warm()` helper |
| Cross-backing parity (mmap default vs pread) | ✅ | `tests/parity/generic_geometry_test.go::TestGenericOpenFileBackingsByteIdentical` |
| Concurrent `Tile()` / `TileInto()` from many goroutines | ✅ | Standard v0.1 invariant — parsed IFDs and tile offset/length arrays populate at `Open` and are immutable thereafter |

## Edge tile semantics

Tiles are stored at full `TileSize` regardless of position; right-edge and bottom-edge tiles include padding bytes in the unused region (the TIFF tile format stores them this way). This applies regardless of compression — JPEG, JP2K, LZW, Deflate, None, and the v0.14 wsi-tools-introduced WebP / JPEG XL / AVIF / HTJ2K codecs all follow the same TIFF tile boundary convention. opentile-go returns the bytes verbatim per the byte-passthrough invariant. Consumers should clip rendered output to the meaningful sub-rect:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
```

SZI/DZI is the exception — its readers return border-sized tiles per spec; see `docs/formats/szi.md`.

## What's not supported

| Capability | Status | Tracking |
|---|---|---|
| Single-level tiled TIFFs | ❌ — rejected by `MinLevels=3` validator gate | `docs/deferred.md` §11 v0.11 candidate (Grundium scan_619 fixture in hand) |
| Mixed-ratio pyramid chains (e.g., 4× then 2×/2×/2×) | ❌ — rejected as multi-pyramid above the 1% leftover cap | `docs/deferred.md` §11 v0.11 candidate (Grundium scan_620 fixture in hand) |
| Stripped pyramid IFDs | ❌ — stripped IFDs route to associated-image classifier | `docs/deferred.md` §2 L26 — fixture-driven |
| Multi-pyramid TIFFs | ❌ — `ErrPyramidMultiplePyramid` (OME's job) | `docs/deferred.md` §2 L27 — permanent |
| Multi-strip JPEG with `PlanarConfiguration=2` (each strip an independent JPEG) | ❌ — OME-specific quirk | `docs/deferred.md` §2 L28 — permanent |
| Multi-strip Deflate associated images | ❌ — `errUnsupportedAssociatedShape`; silently dropped from `Associated()` | re-encode path not implemented in v0.10; flate writers compose differently from the LZW pattern |
| Tiled associated images | ❌ — silently dropped from `Associated()` | rare; OME emits these but its own reader handles them |
| Pluggable associated-image classifier | ❌ — heuristic only | `docs/deferred.md` §2 L29 — YAGNI |
| Magnification | ❌ — always 0 | No standard TIFF tag for it; consumers can derive from `MPP.Symmetric()` if desired |

## Parity

Generic TIFF has no upstream Python opentile counterpart, so v0.7's tifffile + opentile parity oracles don't port. Three correctness bars:

1. **Sampled-tile SHA256 fixtures** (`tests/integration_test.go::TestSlideParity`) — both fixtures, full-walk under the 5 MB JSON cap. Records every (level, x, y) tile's SHA256 in `tests/fixtures/CMU-1.tiff.json` (2.6 MB) and `CMU-1.stripped.tiff.json` (2.0 MB). Regenerate via `OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run TestGenerateFixtures -generate -v`.

2. **Geometry pinning + cross-backing byte parity** (`tests/parity/generic_geometry_test.go`) — both fixtures, no build tag, runs in `make test`. Pins per-level Size / TileSize / Grid / Compression, the L0 (0,0) JPEG SOI marker, per-associated-image type / size / compression / byte count, and confirms tile bytes are byte-identical across mmap (default) and pread backings.

3. **Unit tests** (`formats/generictiff/*_test.go`) — synthetic + real-fixture coverage on the validator (`classify_pyramid_test.go`), the heuristic classifier (`classifier_test.go`), Factory + Detection (`generic_test.go`), tiledImage Level (`tiled_test.go`), and associatedImage AssociatedImage (`associated_test.go`).

The pinned `ByteCount` on each associated image in the geometry test (143,874 / 368,759 / 87,345) is the regression gate for the multi-strip JPEG concat and multi-strip LZW re-encode reader paths — a drift here indicates the relevant T8 logic changed behavior.

## Deviations from upstream Python opentile

Upstream Python opentile doesn't have a generic-TIFF reader, so every v0.10 behaviour in this package is technically a deviation. The interesting ones — captured in [`docs/deferred.md` §1a](../deferred.md#1a-deviations-from-upstream-python-opentile) — are:

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Generic-TIFF reader for non-vendor tiled pyramidal TIFFs | v0.10 | not opt-out-able once registered; any TIFF that no vendor factory claims AND that passes the validator routes here | Real-world WSI authoring outside Aperio / Hamamatsu / Philips is common (Grundium, Roche legacy iScan, vendor-stripped derivatives, libtiff-encoded research outputs); a catch-all reader makes opentile-go consume any structurally valid pyramid TIFF |
| `"associated"` AssociatedImage Type value addition | v0.10 | iterate `Associated()` and skip the type | Generic TIFFs may carry non-pyramid IFDs the heuristic classifier can't confidently match to label / overview / thumbnail; surfacing them as `"associated"` lets the consumer access Bytes() / Size() without a wrong-but-plausible type label |

## v0.15 — Type() rename + value alignment

`Tiler.Associated()` for generic TIFFs emits one of: `"label"`, `"overview"`, `"thumbnail"`, `"associated"` (heuristic-fallback).

The wide-field slide image (when the heuristic classifier identifies one) is emitted as `"overview"` from v0.15 onward, matching DICOM PS3.3 + upstream Python opentile + opentile-go's other format readers. Pre-v0.15 (v0.10–v0.14) this was emitted as `"macro"`.

Additionally, the `AssociatedImage.Kind()` method was renamed to `Type()` (DICOM ImageType convention), and the `formats/generictiff` constants were renamed from `KindXxx` to `TypeXxx`.

**Consumer migration:** where you switch on `Type()` for generic-TIFF associated images, replace `case "macro":` with `case "overview":`. Update any references to `generictiff.KindLabel` → `generictiff.TypeLabel`, `generictiff.KindMacro` → `generictiff.TypeOverview`, etc.

## v0.14 — novel tile codecs

opentile-go's generic-TIFF reader recognises four additional TIFF
compression tag values produced by the user's `wsi-tools` transcoder
plus the registered JP2K code:

| Tag | Codec | opentile.Compression | Magic bytes |
|---:|---|---|---|
| 34712 | JP2K (registered) | `CompressionJP2K` | `FF 4F FF 51` |
| 50001 | WebP | `CompressionWebP` | `52 49 46 46` (RIFF) |
| 50002 | JPEG XL | `CompressionJPEGXL` | `FF 0A` |
| 60001 | AVIF | `CompressionAVIF` | `00 00 00 20 66 74 79 70 61 76 69 66` |
| 60003 | HTJ2K | `CompressionHTJ2K` | `FF 4F FF 51` (J2K SOC + SIZ) |

### Decoder responsibility

opentile-go ships byte-passthrough — we don't decode tiles. Per-codec
consumer responsibility:

- `CompressionWebP` → libwebp or `golang.org/x/image/webp`
- `CompressionJPEGXL` → libjxl (cgo) or stdlib `image/jxl` when available
- `CompressionAVIF` → libavif (cgo) or stdlib `image/avif` when available
- `CompressionHTJ2K` → OpenJPEG 2.5+, OpenHTJ2K, or Kakadu
- `CompressionJP2K` → OpenJPEG (any recent version)

Magic-byte validation lets consumers sanity-check their decoder
dispatch before paying the decode cost.

### wsi-tools ImageDescription parser

When a generic TIFF's level-0 ImageDescription starts with
`wsi-tools/`, opentile-go parses the structured key=value form to
populate the standard cross-format Metadata fields:

- `mag=20x` → `Tiler.Metadata().Magnification`
- `scanner="Aperio"` → `Tiler.Metadata().ScannerManufacturer`
- `date=YYYY-MM-DD` → `Tiler.Metadata().AcquisitionDateTime` (00:00 UTC)
- `mpp=0.499` → `generictiff.MetadataOf(t).MPP.X` / `MPP.Y` (and `MPP.Symmetric()` when isotropic)

The raw ImageDescription remains stored verbatim for consumers who
want full provenance (`source=svs`, `codec=avif`, wsi-tools version).
Non-wsi-tools ImageDescriptions are unaffected.

## Cross-format Metadata mapping (v0.17)

For wsi-tools-tagged generic TIFFs, the v0.17 cross-format Metadata expansion lifts every parsed field onto the cross-format struct. For non-wsi-tools generic TIFFs, only `ImageDescription` (verbatim) is populated.

| Source | cross-format Metadata position |
|---|---|
| wsi-tools `mpp=` | `MPP.X`/`MPP.Y`; `MPP.Symmetric()` non-zero when X == Y |
| wsi-tools `mag=` | `Magnification` |
| wsi-tools `scanner=` | `ScannerManufacturer` |
| wsi-tools `date=` | `AcquisitionDateTime` |
| any non-empty ImageDescription | `ImageDescription` (verbatim) |
| wsi-tools provenance | `Properties["wsi-tools.version"]`, `Properties["wsi-tools.source"]`, `Properties["wsi-tools.codec"]` |
| TIFF `Software` tag (non-wsi-tools); `"wsitools/<version>"` when wsi-tools triggers | `Metadata.Writer` (v0.20; wsi-tools override wins) |

Per Q4 Option B, the format-specific `generictiff.Metadata.MicronsPerPixel` and `ImageDescription` duplicates were retired in v0.17 (the cross-format positions cover them; MPP is now at `opentile.Metadata.MPP.X/Y`).

## Implementation references

- Our package: `formats/generictiff/`
- Public API: `generictiff.New() opentile.FormatFactory` + the existing `Tiler` / `Image` / `Level` / `AssociatedImage` interfaces.
- Our metadata accessor: `generictiff.MetadataOf(opentile.Tiler) (*Metadata, bool)` — exposes `MPP.X`/`MPP.Y` and `ImageDescription` via the embedded cross-format `opentile.Metadata`.
- Pyramid validator: `internal/tiff/classify_pyramid.go` — `ClassifyPyramid(infos, cfg)` value-in / value-out helper. Reusable by other format readers if needed.
- Heuristic classifier: `formats/generictiff/classifier.go` — `ClassifyAssociated(ifd, baseline)`. Already exported so a future v0.10+ Option (L29) can substitute a consumer-supplied classifier.
- Fixture generator: `scripts/regen-generic-tiff.py` — produces `CMU-1.stripped.tiff` from the SVS + 4 synthetic test fixtures (synth-pyramid-jpeg, synth-pyramid-with-label, synth-bad-pyramid, synth-stripped-only). Re-run when the validator thresholds change.
- v0.10 generic-TIFF design: [`docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md`](../superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md).
- v0.10 generic-TIFF plan: [`docs/superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md`](../superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md).

## Known issues + history

- **Heuristic was originally aspect-ratio-based for label**; revised at T2 after probing CMU-1.stripped.tiff and discovering its label is 387×463 (PORTRAIT, taller than wide). The dominant signal for label is now LZW compression alone, not aspect ratio. See `formats/generictiff/classifier.go` source comments.
- **Multi-strip JPEG was originally marked unsupported** (T8 initial draft). Probing CMU-1.stripped.tiff revealed the thumbnail (46 strips) and macro (27 strips) are multi-strip JPEG, and that libtiff's default layout (a single JPEG split at restart-marker boundaries) reproduces the original JPEG via simple concat. Verified empirically against the fixture: 46 strips concatenate to a valid JPEG (SOI...EOI, 143,874 bytes). The PlanarConfiguration=2 case (each strip is its own JPEG) remains excluded by spec — OME-specific quirk.
- **Two Grundium fixtures rejected** (scan_619 single-level, scan_620 mixed-ratio); both legitimate WSI outputs the v0.10 sealed thresholds reject. Diagnosis + v0.11 resolution paths in [`docs/deferred.md`](../deferred.md) §11 "Note on Grundium TIFFs".

See [`docs/deferred.md`](../deferred.md) §8d for the v0.10 retirement audit.
