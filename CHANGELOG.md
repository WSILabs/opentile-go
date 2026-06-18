# Changelog

All notable changes to opentile-go are recorded here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) loosely;
versioning is semantic (`MAJOR.MINOR.PATCH`).

The single source of truth for "what was deferred and why" is
[`docs/deferred.md`](docs/deferred.md). This file is the curated
front-page summary; the deferred file has the full reasoning,
upstream references, and retirement audit per milestone.

## [0.48.0] — 2026-06-18

`Level.Overlapping` — stitched-tile contract signal for re-tiling consumers.

### Added

- **`Level.Overlapping bool`** (#71) — explicit signal that a level's stored
  tiles overlap, so `Grid` does **not** tile `Size` (`Grid.W × TileSize.W >
  Size.W`). True only for stitched BIF levels (#60); false for every other
  format and for non-overlapping BIF levels. Consumers that re-tile / reassemble
  pixels must route through the region API (`ReadRegion` / `ReadRegionScaled` /
  `ScaledStrips`) when `Overlapping` is true, and gate any verbatim per-tile-copy
  fast path on `!Overlapping` — the per-tile accessors (`Tile` / `DecodedTile` /
  `Grid` iteration) return the raw overlapping tiles, not a partition of the
  stitched image. Documented the contract on `Level.Grid` / `Level.Overlapping`
  and in `docs/formats/bif.md`. Additive; no behavior change.

## [0.47.1] — 2026-06-18

### Fixed

- **`ScaledStrips` / `ReadRegionScaled`: spurious "tile missing from cache"
  race.** The strip iterator's per-tile read did `reserve()` then `waitGet()` in
  two separate cache-lock holds; between them a concurrent reservation (lookahead
  prefetch) or `put()` (decode worker) could `evictLocked` the just-produced,
  unpinned entry, so `waitGet()` reported the tile missing and the read failed.
  The consumer now reserves-and-pins atomically (`reserveOrAcquire`) and holds
  the pin through `waitGet` + blit, so the entry can't be evicted out from under
  it. Pre-existing intermittent CI flake; no API change. The cache's
  `acquire`/`release` refcount pin (previously unused by the read path) now
  guards the read.

## [0.47.0] — 2026-06-18

Rendered slide-preview APIs — `RenderThumbnail` and `RenderMacro`.

### Added

- **`RenderThumbnail`** — `Slide.RenderThumbnail(bounds Size, opts...)` and
  `Pyramid.RenderThumbnail(...)` render a whole-slide thumbnail/overview from the
  image pyramid (a thin, aspect-preserving convenience over `ReadRegionScaled`).
  A zero axis in `bounds` is unconstrained, so one `Size` expresses fit-box
  (`{256,256}`), fit-width (`{256,0}`), and fit-height (`{0,256}`). Never
  upscales past L0; best-level-sourced + Lanczos-resampled; for BIF the render is
  correctly stitched. It is a *rendered* image — distinct from the embedded
  `AssociatedThumbnail`/`AssociatedOverview` on `AssociatedImages()`. Additive.
- **`RenderMacro`** — `Slide.RenderMacro(bounds Size, opts...)` synthesizes a
  macro-style orientation image: a slide-shaped canvas (the non-label scan area,
  ~50×25 mm) with the whole-slide tissue composited at its **true physical size**
  (from `Metadata.MPP`, falling back to `10/Magnification` — 40×→0.25, 20×→0.5;
  error if neither) and centred. For slides that don't embed a macro/overview.
  Centred only (true on-slide position is a future enhancement). Additive.

## [0.46.0] — 2026-06-18

BIF overlap-aware tile stitching — DP-generation (#60) and legacy iScan (#63).

### Added

- **BIF: overlap-aware tile stitching** (#60). New `formats/bif/stitch.go` pure
  stitch engine computes per-tile placed layout from the `<EncodeInfo>` XMP joints
  (whitepaper-derived; bio-formats is a black-box dimension oracle only). For
  spec-compliant DP slides (VENTANA DP 200 / DP 600) `Level.Size` now reports the
  stitched content hull; `ReadRegion`, `ReadRegionScaled`, and `ScaledStrips` (DZI)
  composite correctly stitched output via a new internal `regionLayout` capability
  interface. Level 0 is the only overlapping level; higher pyramid levels use the
  naive regular-grid layout (whitepaper page 16).

### Changed

- **BIF: `Level.Size` at L0 is now the stitched content extent** (#60). For
  Ventana-1 (DP 200) this changes 24576×21504 → 23432×21504. `Level.Grid` and
  all per-tile APIs are unchanged. See
  [docs/migrations/2026-06-18-bif-level-size-stitched.md](migrations/2026-06-18-bif-level-size-stitched.md).
- **Validate: `tile-grid-mismatch` relaxed to flag only under-coverage** (#60).
  The check now requires `Grid >= ceil(Size/Tile)` (the grid must cover the image);
  a larger grid is allowed (it is padding). BIF's phantom extra column no longer
  triggers a false-positive validation finding.

### Fixed

- **BIF: `ReadRegion` / `ReadRegionScaled` / `ScaledStrips` seam artefacts** (#60).
  For DP-generation slides the raw frame-grid boundary (24×21 grid, 1024 px pitch)
  produced visible seam lines in region/DZI output because adjacent frames were
  placed at their naive grid positions without overlap compaction. The stitching
  engine eliminates these artefacts; output is now pixel-exact vs bio-formats.

- **BIF: legacy iScan (Coreo / HT) overlap-aware stitching** (#63). `buildLegacyLayout`
  reconstructs per-tile placement from the `TileJointInfo` overlap graph using a
  separable per-column/row-gap-average model: X[col] and Y[row] accumulate per-gap
  average overlaps (Confidence ≥ 98 joins; global-mean fill for empty gaps). The
  implementation is clean-room — derived from the whitepaper geometry and the file's
  own `TileJointInfo` data; bio-formats and openslide are black-box dimension oracles
  only. Validated by placement-fidelity gates (`TestLegacyPlacementResidual`,
  `TestLegacySeamContinuity`, `TestLegacyDimsVsOpenslide`). Width is clean-room-exact
  for all 4 tested fixtures; height carries a ~0.05% residual (the per-column
  `columnYAdjust` Y-baseline used by openslide is GPL-shaped and not replicated).
  Multi-AOI untested (all 4 fixtures are single-AOI).

### Changed (legacy stitching)

- **BIF: legacy iScan L0 `Level.Size` is now the stitched content extent** (#63).
  Was the naive raw-frame-grid extent (`Grid.W × TileSize.W`, `Grid.H × TileSize.H`),
  which over-stated the real content area by the cumulative overlap. Now matches the
  stitched hull consistent with the DP path. See
  [docs/migrations/2026-06-18-bif-level-size-stitched.md](migrations/2026-06-18-bif-level-size-stitched.md).
  `ReadRegion`, `ReadRegionScaled`, and `ScaledStrips` now produce stitched legacy
  output; per-tile raw/decoded APIs are unchanged.

## [0.45.3] — 2026-06-17

Fix: BIF tiles were placed serpentine, scrambling multi-tile levels.

### Fixed

- **BIF: `TILE_OFFSETS` is row-major, not serpentine** (#57). The reader mapped
  image `(col,row)` → tile-offset index via a serpentine (boustrophedon,
  bottom-up) remap, but real VENTANA DP 200 *and* legacy iScan files store tiles
  **row-major, top-left** — the order the `<Frame>` nodes declare. The remap
  vertically scrambled every multi-tile level (single-tile top levels looked
  fine, hiding it). `indexOf` now maps row-major, honoring the `<Frame>` nodes
  when they form a complete per-tile permutation. Verified against bio-formats
  ground truth. Serpentine is kept (`serpentine.go`) as the `TileJointInfo`
  stitch-graph numbering it actually is, scoped out of pixel-storage indexing.
- **BIF: spatial placement oracle** (#59). Added `TestBIFTilePlacementSpatial`,
  which checks tile *placement* against bio-formats (the property the old
  per-index byte-parity test never verified, which is why #57 shipped). The
  tifffile oracle now maps row-major too; the `Ventana-1`/`OS-1` parity fixtures
  were regenerated.
- **BIF: docs/comments corrected** (#58) — `docs/formats/bif.md` and the
  `level.go`/`detection.go`/`serpentine.go` comments now state `TILE_OFFSETS` is
  row-major and that serpentine is the `TileJointInfo` numbering only.

A separate L0 grid/width discrepancy (padded `ImageWidth` → phantom tile column)
is tracked as #60.

## [0.45.2] — 2026-06-17

Fix: `Validate`'s missing-metadata check no longer false-positives on slides
whose MPP is slide-level only.

### Fixed

- **`Validate`: evaluate MPP at the slide level, not per-`Level`** (#55). The
  `missing-metadata` check tested `Level.MPP` per level, so it fired on every
  level of ndpi, leica-scn, dicom, cog-wsi/generic-tiff, ife, and szi — readers
  that populate the slide-level `Metadata.MPP` but leave per-level `Level.MPP`
  zero. The check now consults the slide-level `Metadata.MPP` (or any per-level
  MPP) and emits at most one `missing-metadata` finding per pyramid, only when
  the slide carries no MPP anywhere; a genuinely resolution-less slide still
  flags. (The per-level `Level.MPP` data gap is a separate, deferred follow-up.)

## [0.45.1] — 2026-06-16

Fix: JPEG 2000 decoder no longer misreads RGB codestreams as YCbCr.

### Fixed

- **`decoder/jpeg2000`: decide RGB vs YCbCr from the codestream** (#53). The
  decode shim assumed any 3-component codestream with `color_space != SRGB` was
  YCbCr. A raw J2K codestream (`FF4F`) carries no colorspace (it lives in JP2
  boxes), so OpenJPEG reports `UNSPECIFIED` and the heuristic misclassified every
  raw 3-component J2K as YCbCr — including standard RGB / RGB-MCT codestreams that
  OpenJPEG had already decoded to RGB, applying a spurious YCbCr→RGB step and
  producing wrong colors (e.g. wsitools' JP2K-encoder output). The decoder now
  decides from the codestream via the pure-Go `internal/j2kheader` parser: COD
  MCT flag set → already RGB (no conversion); explicit sRGB box → RGB; sYCC box →
  YCbCr; ambiguous (no MCT, no decisive box — the Aperio 33003 convention) →
  YCbCr. Aperio 33003 decoding is byte-identical to before; `nojp2k`/`nocgo`
  builds unaffected.

## [0.45.0] — 2026-06-16

Additive: a structural WSI validator. New, no breaking changes.

### Added

- **`Validate` API** (`opentile.ValidateFile`, `opentile.Validate`,
  `(*Slide).Validate`) — structural WSI validation (tiers 0 + 1: openability and
  no-decode structural integrity). Returns a findings-as-data `Report`
  (`Finding`/`Severity`/`CheckCode`, `report.OK()`/`Worst()`), rolling repeated
  problems up by category with a count. Active checks: unopenable, out-of-bounds
  tile/strip/frame offsets (64-bit-correct: classic TIFF, BigTIFF, NDPI),
  tile-grid mismatch, inconsistent pyramid, and missing metadata. Wired for all
  11 format readers (TIFF formats share `internal/tiffvalidate`; IFE/SZI/DICOM
  check their own byte ranges; COG-WSI delegates to the inner TIFF reader).
  Decode-free (nocgo-safe). The `orphan-ifd` and `non-conformant-format` check
  codes are defined but reserved (not emitted in v1 — see `docs/validate.md`).
  Pixel-correctness and full spec-conformance are explicitly out of scope. Tier 2
  (pixel-decode checks) is reserved via the `ValidateOption` seam, not built.
  Also adds `opentile.FormatUnknown` (the named `Format` zero value).

## [0.44.1] — 2026-06-16

Maintenance: the DICOM metadata parser moves to the maintained `WSILabs/dicom`
fork, retiring the HTJ2K SIGSEGV workaround. Transitive dependency change only —
no opentile-go public API or behavior change.

### Changed

- **DICOM dependency migrated to `github.com/WSILabs/dicom`** (a maintained
  pure-Go fork of `github.com/suyashkumar/dicom`, replacing the now-unmaintained
  upstream). Transitive only — no opentile-go public API change, and the
  one-cgo-dep invariant is unaffected (the fork remains pure Go). A consumer's
  module graph now pulls `WSILabs/dicom v1.1.0-wsilabs.1` instead of
  `suyashkumar/dicom v1.1.0`.

### Removed

- **HTJ2K transfer-syntax SIGSEGV workaround** (`internal/dicom/htj2k_compat.go`).
  Upstream `suyashkumar/dicom v1.1.0` crashed (nil `ByteOrder`) on the unknown
  HTJ2K transfer syntaxes (`1.2.840.10008.1.2.4.201`–`.203`), so opentile-go
  proxy-substituted the meta-header UID before parsing and restored it after.
  The fork recognizes HTJ2K directly, so `ParseInstance` is now a plain
  `dicom.ParseFile`. The `recover()` guard stays as general defense (backed by
  the parser fuzz test).

## [0.44.0] — 2026-06-16

Additive: WebP gains codec-domain scaled decode (#11), completing the feasible
codec set under the `DecodeOptions.Scale` contract.

### Added

- **WebP codec-domain scaled decode** (#11). The webp decoder now honors
  `DecodeOptions.Scale ∈ {1,2,4,8}` (→ `ceil(dim/Scale)`) via libwebp's internal
  rescaler (`WebPDecoderConfig.use_scaling`), giving a faster, anti-aliased,
  seam-free downscale instead of full-decode-then-box — matching the jpeg /
  jpeg2000 / htj2k contract. Other factors return `ErrUnsupportedScale` (the
  consumer falls back to spatial reduction). Scale=1 output is byte-identical to
  before. JPEG XL stays spatial-fallback by design: its only header-level
  reduced-resolution path is the 1/8 VarDCT DC image, whose libjxl API is
  deprecated/removed and yields only Scale=8 (documented at the decode site).

## [0.43.0] — 2026-06-15

Additive: the codestream inspector (#41) gains `CodestreamInfo.ChromaSubsampling`.

### Added

- **`CodestreamInfo.ChromaSubsampling`** (#41 follow-up, on wsitools feedback).
  The codestream inspector now reports chroma subsampling (`4:4:4` / `4:2:2` /
  `4:2:0` / `4:4:0` / `4:1:1`, `none` for grayscale, `unknown` where unavailable),
  so a frame-copy consumer can distinguish DICOM YBR_FULL_422 (4:2:2) from
  YBR_FULL (4:4:4) — a dciodvfy conformance distinction — letting wsitools retire
  both its `jp2kmeta` and `jpegmeta` raw-byte parsers. JPEG reads it from the SOF
  component sampling factors (`tjDecompressHeader3`, previously discarded);
  JPEG 2000 / HTJ2K from the SIZ per-component XRsiz/YRsiz (verified: a 4:2:2
  J2K codestream reports `Subsampling422`); JPEG XL reports
  `SubsamplingUnknown`. Additive — a new field + `ChromaSubsampling` enum.

## [0.42.1] — 2026-06-15

Pre-adoption rename of the v0.42.0 codestream-inspection API (#41). Marked
breaking for discoverability, but the API was one release old with no consumers.

### Changed (BREAKING)

- **Renamed the #41 codestream-inspection API** for self-documentation:
  `decoder.Prober` → `decoder.CodestreamInspector` and its method `Probe` →
  `Inspect`. The `CodestreamInfo` / `Lossless` / `ColorEncoding` types are
  unchanged. This corrects the name introduced one release earlier in v0.42.0,
  before any consumer adopted it; the consumer path is now
  `f.(decoder.CodestreamInspector)` → `insp.Inspect(src)`.

## [0.42.0] — 2026-06-15

Two additive features — a header-only decoder codestream `Probe` (#41, new
public `decoder.Prober` API) and a synthesized BIF `label` associated image
(#19). No breaking changes.

### Added

- **Decoder codestream `Probe` — header-only codec metadata** (#41). New
  optional `decoder.Prober` interface (`Probe(src) (CodestreamInfo, error)`)
  implemented by the jpeg, jpeg2000, htj2k, and jpegxl factories. It returns
  components, bit depth, reversibility, color encoding, and boxed-vs-raw from a
  codestream **header alone**, without decoding the frame — so a frame-copying
  consumer (e.g. wsitools `convert --to dicom`) can derive a DICOM
  TransferSyntax + PhotometricInterpretation per tile without re-decoding or
  re-shipping a codestream parser (notably for JPEG XL). `CodestreamInfo.Lossless`
  is tri-state because libjxl exposes no header-only reversibility flag, so JXL
  reports `LosslessUnknown` while J2K/HTJ2K report it from the COD transform.
  Codecs without a meaningful header (none/lzw/deflate/webp/avif) don't
  implement `Prober`. J2K/HTJ2K share a pure-Go `internal/j2kheader` SIZ/COD/box
  parser; `Probe` respects the `nojp2k`/`nohtj2k`/`nojxl`/`nocgo` build tags
  (a compiled-out codec's factory simply isn't a `Prober`). Verified end-to-end
  on the 3DHISTECH DICOM HTJ2K/JP2K frames.

- **BIF: synthesized `label` associated image** (#19). BIF exposed only the whole
  `Label_Image` as `overview`, with no `label` — unlike NDPI, which synthesizes a
  macro-crop label. BIF now also exposes `AssociatedImage.Type() == "label"`: the
  **top 1/3** of the overview (the printed-label band; the Roche whitepaper
  reserves the top 25 mm of every 75 mm slide for the label). The crop is
  pixel-domain (the overview can be uncompressed RGB on DP 200 or tiled JPEG on
  legacy iScan), so the label reports `Compression() == none` and is synthesized
  (`Encoding()`/`TIFFTags()`/`IFDOffset()` → false). `overview` is unchanged (the
  full Label_Image), giving BIF and NDPI a consistent "where is the label"
  answer. Verified on Ventana-1 (DP 200) + OS-1 / legacy classic-TIFF iScan
  slides; the label crop visually captures the printed ID + barcode band.

## [0.41.3] — 2026-06-15

BIF legacy-slide read fix. No public API changes.

### Fixed

- **BIF: detect legacy classic-TIFF iScan slides** (#37). Legacy iScan scanners
  (Coreo / HT, ~2010-2012, XMP `BuildVersion="3.x"`) wrote BIF as *classic*
  (non-BigTIFF) little-endian TIFF. Detection required BigTIFF in addition to the
  `<iScan` XMP marker, so these slides weren't claimed by the BIF reader — most
  fell through to the generic-TIFF reader, which reads tiles row-major while BIF
  stores them serpentine + vertically flipped (`TileOffsets[0]` = bottom-left,
  odd stage rows right-to-left), producing the scrambled "corrupt BIF" symptom in
  viewers; some failed to open entirely. `Detect()` now keys on the `<iScan` XMP
  marker alone (matching openslide's `INITIAL_XML_ISCAN` rule), with no BigTIFF
  requirement. The reader's serpentine / blank-tile / associated-image machinery
  is container-agnostic, so it reads classic-TIFF iScan slides once detected
  (verified end-to-end on real legacy slides). No false positives across the
  non-BIF fixtures.

## [0.41.2] — 2026-06-15

OME-TIFF associated-image + strip-level read fixes. No public API changes.

### Fixed

- **OME-TIFF associated images decode for non-JPEG + multi-strip codecs** (#23).
  The OME reader mis-decoded associated images two ways, while the generic-TIFF
  / SVS readers handled the same on-disk IFDs correctly — surfaced by wsitools'
  faithful associated-image writes (verbatim multi-strip strips +
  Predictor/JPEGTables): (1) an LZW associated reported `Compression() = unknown`
  → `Decode` failed (`tiffCompressionToOpentile` mapped only None/JPEG); now maps
  LZW / Deflate / JP2K, mirroring `formats/generictiff`; (2) a multi-strip
  associated truncated to its first strip — `Decode()` routed through the
  strip-0-only `Bytes()` (a Python-parity quirk for the Leica planar=2 macro), so
  a multi-strip JPEG thumbnail/overview decoded to only `RowsPerStrip` rows.
  `Decode()` now dispatches like generictiff (`tiffstrip.Decode` for
  LZW/None/Deflate; concat+SOF-patch or per-strip-stack for multi-strip JPEG);
  the Leica planar=2 and single-strip JPEG paths are unchanged, so existing OME
  behavior and parity are byte-for-byte preserved. Verified byte-identical to the
  generic-TIFF reader.

### Tests / Docs

- **OME-TIFF strip-based (OneFrame) level coverage + boundary docs** (#24). Added
  a synthetic in-tree fixture (tiled JPEG base + strip-based JPEG SubIFD level)
  exercising the OME OneFrame dispatch end-to-end, plus a negative test pinning
  that a *pure* strip-based OME (non-tiled base) is rejected (the OneFrame tile
  size derives from the base page's `TileWidth`). `docs/formats/ometiff.md` gains
  a "Strip-based (non-tiled) levels — the OneFrame boundary" section (the
  whole-frame-per-tile memory caveat; the JPEG-only + mixed-vs-pure-strip
  boundary; the deliberate non-goal that bare non-OME strip-only TIFFs return
  `ErrUnsupportedFormat`).
- New CC0 fixture `ome-tiff/CMU-1-Small-Region.ome.tiff` published in
  WSILabs/wsi-fixtures release v7 (wired into the parity + associated-decode
  gates as the #23 regression case).

## [0.41.1] — 2026-06-14

Bug-fix release. No public API changes. Fixes two decode paths that
broke on Aperio ImageScope export files (which re-encode the pyramid
and associated images in non-Aperio codecs), plus an internal-spelling
cleanup and public CI fixtures for both.

### Fixed

- **Tiled LZW / uncompressed / Deflate levels now decode via `DecodedTile`** (#28).
  The allocating `DecodedTile` path didn't hand the decoder a sized `Dst`, so
  tile codecs that carry no intrinsic dimensions (LZW, uncompressed, Deflate)
  errored with "Dst is required" — while `RawTile`, `ReadRegion`, and
  `DecodedTileInto` worked. A tiled TIFF tile is always `TileWidth×TileLength`,
  so the level's `TileSize` is now passed as the decode output size. Surfaced by
  ImageScope-exported SVS/TIFF files.
- **SVS associated images decode for non-JPEG thumbnails** (#29). ImageScope
  re-exports a slide's associated images (thumbnail / label / overview) in
  whatever codec matches the pyramid, so an LZW or uncompressed thumbnail is
  routine — not just canonical Aperio JPEG. The associated-image dispatcher
  routed thumbnail/overview to the striped-JPEG reassembler by image *role*, so
  a non-JPEG thumbnail hit libjpeg ("Could not determine subsampling — corrupt
  input data"). Dispatch is now by the IFD's actual Compression tag (per-IFD, so
  a mixed-codec set — e.g. uncompressed thumbnail + LZW label + JPEG overview —
  is handled correctly); `Bytes()` multi-strip restitch extended to uncompressed
  alongside the existing LZW path.

### Changed

- **Internal `striped` → `stripped` spelling normalized** (no API change — every
  renamed identifier is unexported). The TIFF spec word is `strip`
  (`StripOffsets` / `StripByteCounts` / `RowsPerStrip`); `striped` derives from
  `stripe` (a colour band) and was the legacy v0.2 misspelling the v0.12 rename
  retired. Also repaired stale `formats/ndpi/striped.go` doc/filename references
  (the file has been `stripped.go` since v0.12) and added a `CLAUDE.md`
  convention note so it stops recurring.

### Tests / CI

- CI now runs an integration job against the public WSILabs/wsi-fixtures corpus
  (#25/#26/#27); the macOS HTJ2K variant is push-only to keep PR turnaround fast.
- Four `590_crop` Aperio ImageScope export fixtures (JP2K / JPEG-q70 SVS;
  LZW / uncompressed TIFF) are published in wsi-fixtures release v6 (CC-BY-4.0)
  and wired into the parity suite — covering non-self-describing tiled levels and
  mixed-codec associated-image sets that the JPEG-only fixtures don't reach.

## [0.41.0] — 2026-06-13

### Changed (BREAKING) — v1.0 API restructure

See [docs/migrations/2026-06-12-v1-api-breaking-pass.md](docs/migrations/2026-06-12-v1-api-breaking-pass.md) for a full before/after table and migration instructions.

- **`opentile.Image` → `opentile.Pyramid`**: the multi-resolution image container type is renamed; `slide.Images()` / `slide.Image(i)` become `slide.Pyramids()` / `slide.Pyramid(i)`.
- **`slide.Associated()` → `slide.AssociatedImages()`**: the associated-image list accessor is renamed for consistency with `Pyramids`.
- **Reads moved to `*Level` and `*Pyramid`**: all tile-read and region-read methods (`RawTile`, `DecodedTile`, `ReadRegion`, `TileReader`, `Tiles`, `TileInto`, `TileBodyInto`, `TilePrefix`, `Warm`, `TIFFTags`) are now methods on `*Level`; cross-level reads (`ReadRegionScaled`, `ScaledStrips`, `BestLevelForDownsample`) are methods on `*Pyramid`. The corresponding `Slide.ImageRawTile` / `Slide.ImageDecodedTile` / `Slide.ImageReadRegion` / `Slide.LevelTIFFTags` / … twin methods are removed.
- **`AssociatedImage.Encoding()` / `.TIFFTags()` / `.IFDOffset()`**: these three accessors move from Slide-level methods (`slide.AssociatedEncoding(a)`, `slide.AssociatedTIFFTags(a)`, `slide.AssociatedIFDOffset(a)`) onto the `AssociatedImage` interface itself.
- **`AssociatedImage.Type()` returns `AssociatedType`** (string-underlying): callers that store or pass the result as a bare `string` need an explicit conversion; `==` comparisons against the exported constants (`opentile.AssociatedLabel`, `AssociatedOverview`, etc.) are unchanged.
- **MPP unified to microns**: `level.MPP` and `metadata.MPP` are now `opentile.MPP{X, Y float64}` (microns). The old `SizeMm`-typed `MPP` field and the `MicronsPerPixel` / `MicronsPerPixelX` / `MicronsPerPixelY` float64 fields on `Metadata` are removed.
- **Geometry unified**: `TilePos` is removed; the `Tiles` iterator now yields `Point`. `ReadRegion` takes an `opentile.Region` struct (`Origin Point`, `Size Size`) instead of loose `level, x, y, w, h int` parameters. The stdlib `image.Point` / `image.Rectangle` types no longer appear in the public surface.
- **`TIFFDirectoriesOf(s)` → `s.TIFFDirectories()`**: the package-level function becomes a method on `*Slide`.

## [0.40.0] — 2026-06-12

### Changed — `AssociatedSource` → `AssociatedEncoding` (pre-1.0 vocabulary)

- The type `AssociatedSource` (added v0.39.0) is renamed **`AssociatedEncoding`**,
  and the accessor `Slide.AssociatedSourceOf(a)` is renamed **`Slide.AssociatedEncoding(a)`**
  (the "Of" suffix is dropped — it was the only `*Slide` method carrying it;
  "Of" is reserved for package-level functions taking a `*Slide`). The name now
  pairs with `AssociatedImage.Decode` as the encode/decode duality. Breaking,
  but the type is one release old and no consumer depends on it yet. Fields and
  behavior are unchanged.

### Added — exported `AssociatedImage.Type()` constants

- `opentile.AssociatedLabel` / `AssociatedOverview` / `AssociatedThumbnail` /
  `AssociatedMap` / `AssociatedProbability` / `AssociatedMacro` /
  `AssociatedGeneric` — the standard `Type()` string values as named constants,
  so consumers can switch/compare without hardcoding literals. Untyped string
  constants (compare directly against `a.Type()`); the value set stays open.

### Notes

- Design record for the broader pre-1.0 API cleanup (receiver-method
  restructure, `opentile.Image`→`Pyramid`, multi-dimensional Z/C/T model):
  `docs/superpowers/specs/2026-06-12-v1-api-vocabulary-and-multidim-design.md`.

## [0.39.0] — 2026-06-12

### Added — `Slide.AssociatedSourceOf` (faithful no-re-encode source) (#22)

- New type `AssociatedSource` and method `Slide.AssociatedSourceOf(a
  AssociatedImage) (AssociatedSource, bool)` expose an associated image's
  **on-disk encoded source** — the strip bytes in document order plus the TIFF
  tags (`Compression`, `Predictor`, `JPEGTables`, `RowsPerStrip`, `Samples`,
  `Photometric`) a consumer must set to re-emit them into a fresh standalone
  single-IFD TIFF with **no re-encode**. Unlike `Bytes()` (whose JPEG output is
  abbreviated and depends on the source IFD's `JPEGTables`) or `Decode()` (which
  decodes to pixels), this is a byte-identical copy path — for tools that want
  to extract a label/overview as its own TIFF without round-tripping pixels.
- `ok=false` for associated images with no faithful single-IFD strip
  representation: self-contained JPEGs (NDPI/Leica overview — use `Bytes()`),
  DICOM frames, OME planar (`PlanarConfiguration=2`) pages, tiled associated
  images, and NDPI's synthesized label. Implemented across SVS (LZW label +
  JPEG associated), generic-TIFF (and COG-WSI via delegation), BIF, Philips,
  OME-TIFF (strip, non-planar), and the NDPI map page.
- Discovered by type assertion on an unexported `associatedSourcer` capability
  — **additive, no breaking change** to the `AssociatedImage` interface.

## [0.38.1] — 2026-06-12

### Fixed — native DICOM associated images decoded RGB as grayscale (#21)

- `AssociatedImage.Decode()` on a **native (uncompressed)** DICOM associated
  image inferred `SamplesPerPixel` from the frame byte-length, which breaks on
  the ≤1-byte even-length PixelData pad (PS3.5 §7.1.1): when `w*h*samples` is
  odd the modulo fails and it fell back to `samples=1`, collapsing interleaved
  RGB to grayscale. Now uses the instance's authoritative `SamplesPerPixel`
  (0028,0002) / `PhotometricInterpretation` (0028,0004), tolerates + trims the
  pad byte, and falls back to a pad-aware length inference. Encapsulated
  (JPEG / JP2K / HTJ2K) associated images are unaffected. This is the read-back
  path for wsitools' DICOM writer (LZW labels re-stored as native DICOM).

## [0.38.0] — 2026-06-12

### Added — `AssociatedImage.Decode` (faithful decoded associated images) (#20)

- New interface method `AssociatedImage.Decode(opts decoder.DecodeOptions)
  (*decoder.Image, error)` returns faithfully-decoded RGB(A) pixels for every
  associated image (label / overview / thumbnail / macro / map / probability /
  associated) across every codec — JPEG, JPEG 2000, HTJ2K, WebP, AVIF, JPEG XL,
  and **LZW (incl. Predictor=2)**, Deflate, and uncompressed. Honors
  `Format` (RGB/RGBA); `Scale` on codec-backed images. Consumers no longer need
  to re-parse the source file (e.g. wsitools' DICOM writer for LZW labels).
  Additive: a new method on the public `AssociatedImage` interface.
- New `internal/tiffstrip` (strip decode + Predictor=2 + sample→RGB) and
  `internal/assocdecode` (codec-registry path); `tiff.Page.Predictor()`.

### Fixed

- **`internal/tifflzw` writer corrupted large LZW streams** (>~47 KB output):
  at the code-table-full point the TIFF off-by-one width bump fired before the
  Clear, writing the Clear at width 13 while readers cap at 12 — desyncing and
  truncating. Reorder to emit Clear before the width bump. This is the GH #20
  "LZW decodes only a fraction for large images" bug (it was the writer; the
  reader and tifffile fail identically on streams it produced). Affects the
  multi-strip-LZW re-encode in `Bytes()` for large labels (SVS / generic-TIFF
  label fixtures' `associated` SHA regenerated to the now-valid bytes).
- Faithful decode of associated images that `Bytes()` couldn't deliver:
  generic-TIFF stripped JPEG (splice JPEGTables + concat-or-stack), BIF
  None/LZW (strip path), SVS thumbnail (per-strip decode + stack fallback for
  Aperio restart-marker layouts), OME planar (PlanarConfiguration=2 multi-strip
  JPEG reassembled per channel).

> NOTE: existing wsitools-generated `cog-wsi` fixtures carry a label encoded by
> the same LZW writer bug (wsitools' `cogwsiwriter` has its own copy) — it is
> objectively corrupt (tifffile rejects it too) and must be regenerated after
> the same fix lands in wsitools.

## [0.37.0] — 2026-06-06

### Changed — OpenJPEG/JPEG2000 decode is now optional (`nojp2k`) (#17)

- `decoder/jpeg2000` gains a `nojp2k` build tag, mirroring the existing
  `nojxl`/`nowebp`/`noavif`/`nohtj2k` pattern: `-tags nojp2k` excludes the
  `libopenjp2` cgo file and falls back to the existing stub factory (JP2K tiles,
  TIFF compression 33003/34712, return `ErrCodecUnavailable`).
- **libjpeg-turbo is now the only codec linked under every cgo build.** JPEG2000
  is a legacy Aperio codec, rarely seen in modern WSI; making it opt-out enables
  JPEG-only minimal installs. Additive — default builds are unchanged.

## [0.36.0] — 2026-06-05

### Fixed — htj2k cgo build on non-macOS toolchains (#16)

- `decoder/htj2k` hardcoded `#cgo LDFLAGS: -lc++` (Apple clang's libc++),
  which broke GNU-toolchain cgo builds (Linux gcc, Windows mingw-w64) with
  `ld: cannot find -lc++` — blocking the openscope Windows sidecar. Made
  the C++ runtime flag platform-conditional: `-lc++` on darwin, `-lstdc++`
  elsewhere. (Linux CI was masking it via `-tags nohtj2k`.)

### Added — `Slide.AssociatedIFDOffset` (#15)

- `func (s *Slide) AssociatedIFDOffset(a AssociatedImage) (offset int64, ok bool)`
  maps a typed associated image back to its source IFD byte offset, for
  raw-TIFF in-place editing (wsitools' associated-image editor). Opt-in per
  format — implemented for **SVS** and **generic-TIFF**; other TIFF readers
  and formats with synthesized associated images (no on-disk IFD) return
  `ok=false`, as do non-TIFF slides. Additive; no API break.
- Internal: `internal/tiff` now retains each IFD's byte offset
  (`tiff.Page.IFDOffset()`), populated during the IFD walk.

## [0.35.0] — 2026-06-04

### Changed (breaking) — one "type" vocabulary for image classification

- Completed the v0.15 `Kind()`→`Type()` rename that the public TIFF-tag
  API (v0.31) had missed. The associated-image classification vocabulary
  is now uniformly **"type"**, never "kind".
- **Breaking (public API):** in the v0.31 `TIFFDirectoriesOf` surface —
  - `DirectoryKind` type → **`DirectoryType`**
  - `TIFFDirectory.Kind` field → **`TIFFDirectory.Type`**
  - `TIFFDirectory.Associated` field → **`TIFFDirectory.AssociatedType`**
  - Value names (`DirOther` / `DirLevel` / `DirAssociated`) are unchanged.
  - **Consumer migration:** `d.Kind` → `d.Type`, `d.Associated` →
    `d.AssociatedType`. Only affects code using `TIFFDirectoriesOf`
    (added v0.31, ~3 days prior); no other public symbol changed.
- Non-breaking: the `AssociatedImage.Type()` godoc no longer calls the
  values "kinds"; internal naming (`associated.go` fields/params, the
  `kindFromIFDRole`/`normaliseAssociatedKind` helpers) and format/README
  docs now say "type". NDPI's internal `pageKind` page-role enum is left
  as-is — it classifies pages, not associated images.

## [0.34.1] — 2026-06-04

### Fixed — strips: corrupted output at between-level downsamples

- **`ScaledStrips` / `ReadRegionScaled` produced squished/gapped output**
  whenever the codec-domain scale factor (`idctScale`) was > 1 — which
  `autoIDCTScale` auto-selects for any source whose target downsample falls
  *between* pyramid levels (a common case for DZI generation). Decoded tiles
  are `ceil(TileSize/scale)`, but the strip region, intermediate, and blit
  positions were computed in **full-level** coordinates, so scaled tiles were
  dropped at full-level positions covering only `1/scale` of their footprint.
  Fixed by running all strip geometry on an effective `scale`-times-coarser
  virtual level. (Regression: strip scale-2 vs scale-1 mean abs diff
  8.26 → 0.07.)

### Added — strips + regions: codec-domain Scale for JP2K/HTJ2K

- **`ScaledStrips` and `ReadRegionScaled` now use codec-domain downscale for
  JP2K and HTJ2K sources**, not just JPEG. `autoIDCTScale`'s gate widened from
  `CompressionJPEG` to a scale-capable predicate (`jpeg | jpeg2000 | htj2k`),
  riding the `DecodeOptions.Scale` contract those decoders gained in v0.33/
  v0.34. `ReadRegionScaled` (which previously did full-level read + spatial
  resample for every codec) now routes the common case through the strip
  machinery. Faster + anti-aliased downsampling on SVS-JP2K and DICOM
  JP2K/HTJ2K. `WithScale` / `WithStripIDCTScale` docs corrected (no longer
  "JPEG only").

## [0.34.0] — 2026-06-04

### Added — dicom: JPEG 2000 + HTJ2K transfer-syntax decode (v0.34)

- **DICOM WSM levels in JPEG 2000 (`.90` / `.91`) now decode.** Mapped the
  JP2K transfer syntaxes to `CompressionJP2K` in `compressionForSyntax`;
  `DecodedTile` dispatches to the OpenJPEG decoder. Frame extraction was
  already codec-agnostic (the mmap fragment-offset-walk returns the raw J2K
  codestream), so this was a one-line mapping on top of the decoder hardening
  from #7/#8/#10. Colour is handled by OpenJPEG from the codestream (no
  `PhotometricInterpretation` plumbing) — verified within 1 LSB against the
  original JPEG via a lossless `gdcmconv --j2k` transcode fixture.
- **DICOM WSM levels in HTJ2K (`.201` / `.202` / `.203`) now decode** (subject
  to the `nohtj2k` build tag). Same one-line `compressionForSyntax` mapping to
  `CompressionHTJ2K` → openjph decoder. Required a parser fix: `suyashkumar/`
  `dicom` v1.1.0 (latest) doesn't know the HTJ2K syntaxes and **SIGSEGVs** (it
  derives a nil byte order), which blocked reading *any* HTJ2K DICOM. Since
  HTJ2K data sets use Explicit VR Little Endian — the same encoding as JPEG
  2000 `.91` — `internal/dicom` substitutes a `.91` proxy UID in the meta
  header for the cold-path parse and records the real syntax. `ParseInstance`
  also gained a `recover` guard so malformed DICOM returns an error instead of
  crashing. Colour verified within 5 LSB vs the original JPEG (ojph_compress +
  pydicom lossless transcode fixture).

## [0.33.0] — 2026-06-04

### Added — dicom: `ListWSMSeries` series enumeration (#13)

- **`dicom.ListWSMSeries(path) ([]SeriesInfo, error)`** enumerates the WSM
  series under a path **without opening a slide**, returning one `SeriesInfo`
  (`SeriesUID`, `LevelCount`, `InstanceCount`, `Manufacturer`, `Model`,
  `Magnification`) per distinct series, sorted by `SeriesUID`. A single `.dcm`
  path returns one anchored entry. Lets a caller (CLI) detect a multi-series
  directory (`len() > 1`) and refuse with an actionable error instead of
  trusting `OpenSeries`' silent dominant-pick (which is unchanged). The scan is
  metadata-only with a DICM-magic pre-filter and a bounded worker pool; it does
  not truncate (correctness over a partial fast answer). `DICOMDIR` fast-path
  deferred. Docs now recommend single-`.dcm` open as the precise, unambiguous
  entry point.

### Added — decoder/jpeg2000 + decoder/htj2k: codec-domain scaled decode (#10, #12, #11)

- **`DecodeOptions.Scale ∈ {1,2,4,8}` now honored** by the JPEG 2000 and
  HTJ2K decoders via DWT resolution-level decode (1/2^log2(Scale)),
  matching the `jpeg` decoder's contract (output `ceil(srcDim/Scale)`,
  else `ErrUnsupportedScale`). Decoding to a lower wavelet resolution
  skips the high-frequency subbands — **faster** than a full decode,
  **anti-aliased** (the wavelet low-pass is a real reconstruction filter,
  not a box average), and **seam-free** (per-tile). JP2K uses OpenJPEG
  `cp_reduce`; HTJ2K uses openjph `restrict_input_resolution`. When a
  codestream has fewer decomposition levels than requested (the lib then
  *fails* rather than clamps), the decoder reduces to the max available
  and **box-finishes** the residual factor to land on exact dims (new
  `internal/boxhalve`). Measured ~5× (JP2K) / ~4× (HTJ2K) faster than
  full-decode-then-box at 2×. Composes with the #7 chroma-subsampling
  fix. Unblocks wsitools' codec-agnostic `downsample` (try `Scale:N`,
  fall back to full-decode + spatial reduce). The umbrella #11 remains
  open for the later `webp` / `jpegxl` items.

## [0.32.2] — 2026-06-03

### Changed — resample/lanczos: separable + weight-cached (perf, output-equivalent) (#9)

- **Lanczos resampling is now separable.** `lanczosInto` was a naive
  non-separable 2-D convolution — `O(out·scale²)` with two `math.Sin` per
  source pixel in the hot loop, too slow to be a default downsample kernel.
  Rewritten as two 1-D passes (horizontal then vertical) over a float
  intermediate, with per-axis weight tables precomputed once and
  pre-normalized to sum 1 (mathematically identical to the old 2-D `wSum`
  normalization). All `sin()` calls now happen once per output position,
  never in the resample loops. **Output-equivalent to within 1 LSB per
  channel** (RGB/RGBA, integer and non-integer ratios, edge clamping,
  upsampling — see `TestLanczosSeparableEquivalence`). Public API,
  `Kernel` constants, and `WithResampleKernel`/`WithStripKernel`
  unchanged. Measured ~27× faster at 2× and ~33× at 4× downsample;
  unblocks `lanczos3` as a default `downsample` kernel downstream.

## [0.32.1] — 2026-06-02

Consumer-reported decoder fixes (wsitools + openscope), all memory-safety
or output-contract bugs in the JPEG 2000 / HTJ2K decode paths.

### Fixed — decoder/jpeg2000: heap OOB read on chroma-subsampled tiles (#7, #8)

- **Out-of-bounds read on 4:2:2 / 4:2:0 codestreams.** The RGB packing
  loop indexed all three component planes with a single luma index
  `i ∈ [0, w·h)`, but subsampled chroma planes hold fewer than `w·h`
  samples. For Aperio SVS JP2K (`Compression=33003`, 4:2:2) this read
  2× past the end of each chroma plane — intermittent SIGBUS and silent
  colour corruption (nondeterministic under a concurrent decode pool).
  Now each component is indexed by its own geometry
  (`(y/dy)·w + (x/dx)`), nearest-neighbour upsampling subsampled planes
  (mirrors OpenJPEG's own `opj_decompress`). Added guards: require ≥3
  components, floor `dx/dy` at 1, clamp the per-plane index. The YCbCr→RGB
  colour math is unchanged, so 4:4:4 / RGB output is bit-identical.
  Regression test decodes a real 4:2:2 tile and matches an independent
  `opj_decompress` reference (max per-channel diff 1/255 over the tile).

### Fixed — decoder/jpeg2000: honor `opts.Format` (RGBA output) (#8)

- **`PixelFormatRGBA` now accepted** by the JPEG 2000 decoder. Previously
  `Decode` always returned `PixelFormatRGB` regardless of `opts.Format` —
  the lone codec ignoring it. It now allocates `NewImageFormat(w, h,
  opts.Format)` and writes opaque alpha (`0xFF`) when RGBA is requested.
  RGB path byte-identical. Brings `decoder/jpeg2000` to parity with the
  other codecs.

### Fixed — decoder/htj2k: subsampling-safe component read

- **Defensive bound against a latent OOB on subsampled HTJ2K.** The decode
  packing loop read `w` samples from each pulled component line, correct
  only for 4:4:4. A horizontally-subsampled line (`line->size < w`) would
  over-read. Not reachable today (wsi-tools emits 4:4:4) but possible for
  subsampled DICOM HTJ2K. Reads are now bounded by openjph's authoritative
  `line->size` (`sx = x·size/w`): identity when `size == w` (4:4:4
  unchanged), in-bounds horizontal upsample when `size < w`. Full
  subsampled-decode correctness (incl. 4:2:0 vertical) is validated with a
  real fixture in the planned DICOM JP2K/HTJ2K milestone.

## [0.32.0] — 2026-06-02

DICOM WSI reader (opentile-go's 11th format and first multi-file format)
plus an HTJ2K RGBA-output fix.

### Fixed — decoder/htj2k: RGBA output support

- **`PixelFormatRGBA` now accepted** by the HTJ2K decoder. Previously
  `Decode` with `opts.Format == PixelFormatRGBA` returned
  `ErrUnsupportedFormat`; it now decodes RGB via the C shim (`wsi_htj2k_decode`,
  which always emits packed RGB888) into a scratch buffer, then expands
  RGB→RGBA in Go with opaque alpha (`0xFF`). The `opts.Dst` path is
  also supported — a caller-provided `*decoder.Image` is validated
  (dimensions must match) and filled in place. The existing RGB path is
  byte-identical to before. Brings `decoder/htj2k` to parity with the
  other codec decoders (`jpeg`, `jpeg2000`, `webp`, `avif`, `jpegxl`)
  which all support RGBA.

### Added — DICOM WSI reader (the 11th format)

- **First multi-file format** opentile-go reads. `OpenFile` accepts a
  directory (scans all WSM `.dcm` instances) or any single `.dcm`
  (bounded sibling-scan within the same directory for the same
  `SeriesInstanceUID`). The standard `Open(io.ReaderAt, size)` entry
  point cannot reach DICOM.
- **TILED_FULL + TILED_SPARSE** both supported. FULL: raster frame-index
  (`ty*tilesAcross + tx`). SPARSE: per-frame `ColumnPosition /
  RowPosition` map built at Open from the Per-Frame Functional Groups
  Sequence. TILED_FULL is the common case (3DHISTECH + Grundium);
  TILED_SPARSE is Leica's organization.
- **JPEG Baseline levels** (libjpeg-turbo via the existing decoder pool)
  and **JPEG + uncompressed associated images** (label/overview/thumbnail
  keyed by `ImageType` LABEL/OVERVIEW/THUMBNAIL tokens). Mixed encoding
  within one series is handled (Leica GT450 label is uncompressed native
  RGB; JPEG for all other instances).
- **`internal/dicom`** wraps `github.com/suyashkumar/dicom` (pure Go —
  no new cgo) for cold-path attribute parsing (preamble + `DICM` magic,
  group-0002 meta header, nested undefined-length SQ traversal for
  functional-group sequences, encapsulated PixelData fragment header).
  The hot path uses an own **mmap fragment-offset-walk** (~16 bytes/frame
  overhead), building a frozen byte-offset table at Open — byte-identical
  to the library on all three scanner types.
- **Verified on Leica GT450 / 3DHISTECH / Grundium** (all three fixture
  scanners). Series hygiene: non-WSM instances and zero/missing
  `TotalPixelMatrix` entries (present in 3DHISTECH series) are filtered.
- **`opentile.FormatDICOM`** added. Full `Levels()` / `Metadata()` /
  `RawTile` / `DecodedTile` / `ReadRegion` / `ScaledStrips` available.
  `TIFFDirectoriesOf` returns `ok=false` (DICOM is not TIFF).
- **Deferred:** concatenations, multi-fragment-per-frame, JP2K / HTJ2K /
  JPEG-LS / RLE transfer syntaxes, multi-optical-path / Z-stack /
  multi-pyramid series, DICOMweb / PACS, raw DICOM-attribute API.

## [0.31.1] — 2026-06-02

Documentation + internal-structure release. **No code, public API, or
behavior change** — the exported surface is identical to 0.31.0
(`make test` green under `-race`; `RawTile` / `DecodedTile` /
`ReadRegion` / `ScaledStrips` / TIFF-tag APIs all byte-stable).

### Changed — documentation / positioning

- Repositioned the project framing in `README.md`, `CLAUDE.md`, and
  `NOTICE`: opentile-go is no longer described as a "direct port of
  opentile" but as having **begun as a Go port of opentile and grown
  into a superset that incorporates openslide-like decoded-region
  reading** (`ReadRegion` / scaled-strip DZI, associated images,
  memory-budget control, raw vendor TIFF-tag access) across 10 WSI
  formats. The Sectra AB Apache-2.0 attribution in `NOTICE` is retained
  (a license obligation); algorithmic provenance comments are unchanged.
- Corrected the stale "one cgo dependency" description. cgo now spans
  **libjpeg-turbo + OpenJPEG (required under any cgo build)** plus
  **optional libjxl / libwebp / libavif / openjph** (each
  `no<codec>`-disableable; CI builds `nohtj2k`) and **libopenslide**
  (benchmark-only, `openslidebench` tag). Raw-tile reads remain pure Go;
  `nocgo` / `CGO_ENABLED=0` builds return `ErrCGORequired` for decode
  paths only.
- Dropped the v1.0 / API-freeze ceremony from `CLAUDE.md` (the "public
  API frozen / requires a major-version bump" invariant, the v0.3
  "frozen" milestone phrasing, the "v1.0 cut pending" note). Replaced
  with a lighter practical-compat note: don't gratuitously break the
  exported surface because `wsitools` + `openscope` import it directly —
  no version ceremony, refactor freely.

### Changed — internal structure (no API impact)

- Reorganized the flat root `package opentile`. Renamed the mislabeled
  `slide_*` files so the `slide_` prefix means *core Slide* only —
  `slide_region.go` → `region.go`, `slide_region_scaled.go` →
  `region_scaled.go`, `slide_decoded_tile.go` → `decoded_tile.go`,
  `slide_scaled_strips.go` → `strips.go`, plus `opentile.go` →
  `open.go`, `format_types.go` → `format.go`, `tiff_tags.go` →
  `tifftags.go` (and matching `_test.go` files). Split the 523-line
  `strip_iterator.go` into `strip_iterator.go` (the `StripIterator` type
  + `Next`/`Strips`/`Close`), `strip_workers.go` (decode-worker pool),
  and `strip_geometry.go` (resample + sizing/level-selection helpers).
  All `git mv` + verbatim relocation — no exported symbol renamed or
  moved, no logic changed. 39 packages green under `-race`.

## [0.31.0] — 2026-06-01

Raw TIFF tag exposure (the headline, public API), plus a standing
cross-format benchmark suite and the restored byte-parity oracle.

### Added — raw TIFF tags (public)

Consumers can read raw TIFF tags — including vendor/private tags not
surfaced as typed `Metadata` fields — anchored to the semantic level or
associated image they already hold:

- `TIFFTag{Number, Name, Type, Count, Raw []byte}` with typed getters
  `ASCII()` / `Uints()` / `Rationals()`; `TIFFType` constants;
  `Rational`. `TIFFTags` with `Tag(number)` / `ByName`.
- Entity-anchored access, keyed by the same `(image, level)` coordinates
  as `ImageRawTile`: `Slide.LevelTIFFTags(level)`,
  `Slide.ImageLevelTIFFTags(image, level)`,
  `Slide.AssociatedTIFFTags(a)`.
- `TIFFDirectoriesOf(s) ([]TIFFDirectory, bool)` — completeness view over
  every IFD with structured identity (`DirLevel` / `DirAssociated` /
  `DirOther`), reaching orphan IFDs (NDPI Map page, OME SubIFDs, SCN
  region/XML IFDs).

Implemented for all 8 TIFF-based formats (SVS, NDPI, Philips, OME-TIFF
multi-image, BIF, generic-TIFF, Leica-SCN, COG-WSI). Lazy (decoded on
call, nothing at `Open`) via a type-assertion provider through the
`UnwrapReader` chain; COG-WSI inherits it for free via its delegation to
the inner generic-TIFF reader. Pixel-pointer tags
(`StripOffsets`/`StripByteCounts`/`TileOffsets`/`TileByteCounts`) are
excluded; the tag-name dictionary is best-effort (`Number` is always
authoritative). Non-TIFF formats (IFE, SZI) return `ok=false`.
`internal/tiff.Page` gains a generic `RawTags()` enumerator.

### Added — benchmark suite (internal/tooling)

Standing cross-format benchmark suite (`bench/`): `go test ./bench/
-bench BenchmarkRead` over all 10 formats × Tile/DecodedTile/ReadRegion ×
single/parallel (Mpix/s + allocs/op); `make bench-all` per-format
throughput gate; `make bench-compare` competitive report vs **openslide**
(in-process via a build-tagged in-house cgo shim, `//go:build
openslidebench`) and **python opentile** (subprocess). The shipping
library keeps its single cgo dependency. Measured: opentile-go ReadRegion
3–12× openslide; RawTile fetch 5–17× python opentile.

### Fixed — byte-parity oracle

`tests/oracle` (`-tags parity`) builds again after Level/Image struct API
drift (method-style → field access; `Level.Tile` → `Slide.RawTile`/
`ImageRawTile`). Full parity suite green on Python ≤3.12 (3.14 breaks
ome-types/xsdata OME-XML parsing); opentile-go verified byte-identical to
tifffile (raw TIFF) and python opentile across formats. The `-tags
bfparity` Leica/Bio-Formats oracle remains separate (uses the removed
`Image.SizeC`).

### Public API

- **Additions:** `TIFFTag`, `TIFFType` (+constants), `Rational`,
  `TIFFTags`, `TIFFDirectory`, `DirectoryKind`, `TIFFDirectoriesOf`,
  `Slide.LevelTIFFTags`, `Slide.ImageLevelTIFFTags`,
  `Slide.AssociatedTIFFTags`. All additive.
- **No breaking changes.** RawTile / DecodedTile / ReadRegion /
  ScaledStrips unchanged.

### Tests

- `TestTIFFTagsAllFormats` — cross-format sufficiency gate over all 8
  TIFF formats; `TestTIFFTagsNonTIFFExcluded`; `TestTIFFTagFidelity`.
  Per-format provider tests. `make test` green under `-race`.

### Design / Plans

- `docs/superpowers/specs/2026-05-31-tiff-tag-exposure-design.md`
- `docs/superpowers/plans/2026-05-31-tiff-tag-exposure.md` (+ `-all-readers.md`)
- `docs/superpowers/specs/2026-05-30-comprehensive-benchmark-suite-design.md`

## [0.30.0] — 2026-05-30

Read-path memory-budget milestone. Bounds NDPI `ScaledStrips` (the DZI
conversion path) peak memory to ~2 GB regardless of slide width,
closing a `wsitools convert --to dzi` out-of-memory that drove a 16 GB
Mac into a system memory panic on wide Hamamatsu NDPI slides. The OOM
predates and is independent of v0.29 (verified: v0.26 and v0.29 had
statistically identical peaks).

### Corrected root cause (heap-profiled, not geometry)

An in-tree `inuse_space` profile (`cmd/bench/ndpi-strips`) falsified the
original geometry-based hypothesis. The dominant consumer is **C1, the
per-iterator `StripIterator` decoded-tile cache** (~2 GB on CMU-1,
~6 GB on OS-2) — its capacity was a tile *count* that grew with slide
width. The NDPI `pixelCache` (C2), originally suspected as the OS-2
dominator, is empirically the *smallest* term (~0.1–0.7 GB); the earlier
"OS-2 = C2" reading was an artifact of measuring total wsitools RSS,
which also includes wsitools' own DZI level-builder cascade (a separate
width-proportional consumer outside this library).

### Measured peak HeapInuse (worst case, `GOMEMLIMIT=2GiB`)

| Fixture           | before    | after     |
|---|---|---|
| CMU-1 @ dziTile 256  | 2633 MiB | **1948 MiB** |
| OS-2  @ dziTile 256  | 6643 MiB | **2037 MiB** |
| OS-2  @ dziTile 1024 | 7852 MiB | **2751 MiB** |

OS-2 (2.5× wider than CMU-1) now peaks at ~the same level — peak is
slide-width-independent at the default tile size. ScaledStrips
throughput is unchanged-to-improved (OS-2 @256: 157 → 241 Mpix/s).
`bench-ndpi` (ReadRegion path) unchanged at ~293 Mpix/s.

### Added

- **`WithMemoryBudget(bytes int64) Option`** (public) — per-Slide
  read-path live-memory budget; governs the C1 decoded-tile cache.
  Default 1 GiB.
- **`OPENTILE_READ_MEMORY_BUDGET`** env var (bytes) — same knob without
  recompiling. Precedence: option > env > default.
- `cmd/bench/ndpi-strips` peak-memory gate (asserts `HeapInuse`) +
  `make bench-ndpi-mem` target
  (runs the no-backpressure worst case under `GOMEMLIMIT=2GiB`,
  CMU-1 + OS-2 at dziTile 256/1024). The regression guard for this
  class of issue.

### Changed

- **C1** — `StripIterator` tile-cache capacity is now byte-derived
  (`budget / bytesPerTile`, floored at `max(workers, 8)`, capped at the
  original count formula) instead of an unbounded width-proportional
  count. Accounts for `idctScale`.
- **C3** — NDPI `framesByKey` (previously an **unbounded** assembled-
  JPEG-frame map) is now a 128 MiB byte-bounded LRU (`frameByteLRU`).
  On the single-pass DZI traversal it provided ~zero benefit while
  retaining most of the compressed level.
- Default budget now **honours `GOMEMLIMIT`**: when set and no explicit
  budget was given, the default shrinks to ≤ half the limit (floor
  128 MiB) so live set + GC headroom fit under the runtime ceiling. The
  library never *sets* `GOMEMLIMIT`.
- `tileCache.evictLocked` no longer evicts **in-flight** entries
  (`ready != nil`) — only produced, unpinned ones. The smaller cache
  exposed a latent deadlock: an evicted in-flight entry orphaned a
  concurrent `waitGet` waiter on a `ready` channel that never closed
  (reliably deadlocked `TestScaledStripsCrossFormat/SVS`).
- `StripIterator.tileReqs` is never closed; workers shut down via the
  cancel context. Removes a send-on-closed-channel race on shutdown.

### Public API

- **One addition:** `WithMemoryBudget` (additive, non-breaking).
- **No breaking changes.** `RawTile`, `DecodedTile`, `ReadRegion`, and
  `ScaledStrips` outputs are byte-identical to v0.29.

### Tests

- `formats/ndpi/frame_cache_test.go` (NEW): 4 byte-LRU tests.
- `options_budget_test.go` (NEW): 6 budget-resolution tests incl.
  `GOMEMLIMIT` shrink + explicit-verbatim.
- `strip_iterator_budget_test.go` (NEW): 4 capacity-helper tests.
- `ScaledStrips` suite green under `-race -count=3` (the deadlock
  reproducer); full `opentile` + `ndpi` packages green under `-race`.

### Deferred forward

- **C2** (`pixelCache`) byte-budgeting — already count-bounded and the
  smallest term; threading the per-Slide budget through the
  format-dispatch bridge wasn't worth it for v0.30 (design doc §1.4).
- **C4** — the irreducible full-width output strip buffer scales with
  width × DZI-tile-size. At dziTile 1024 on very wide slides it is the
  residual term (Hamamatsu-1, 188160 wide, peaks ~3.5 GB @1024 — still
  far below the OOM threshold and completes cleanly). The reported OOM
  used the default 256 tiles, now ~2 GB.
- `StripIterator.Next` does not `acquire`-pin tiles around `waitGet`, so
  a produced tile evicted in the `reserve→false`→`waitGet` window can
  surface a spurious "tile missing" error (pre-existing; not observed in
  any `-race` run). Fix: pin via `acquire`/`release` or re-reserve.
- Workers now strictly require `Close()` to exit (was: exited on the old
  channel close). `Close()` is contractually mandatory.
- wsitools' DZI level-builder cascade is co-dominant on wide slides and
  outside this library fix — tracked in wsitools (design doc §2).

### Design / Plan

- Design: `docs/superpowers/specs/2026-05-30-opentile-go-v30-ndpi-memory-budget-design.md`
- Plan: `docs/superpowers/plans/2026-05-30-opentile-go-v30-ndpi-memory-budget.md`

## [0.29.0] — 2026-05-29

ReadRegion allocation-elimination perf milestone — Layers 1 + 2 of
the v0.29 spec's three-layer design. Layer 3 (NDPI pixelCache
scratchPool) was implemented and then reverted within v0.29:
surfaced a real race when buffers shared via the scratch pool got
evicted while a cache-hit reader still held the pointer. Deferred
to a future milestone pending refcounting or a different cache
topology.

### Measured throughput (Apple Silicon, 13 cores)

| Bench               | v0.28          | v0.29          | Delta |
|---|---|---|---|
| bench-ndpi (single) | 251 Mpix/s     | ~300 Mpix/s    | +19% |
| bench-ndpi-mt       | 539 Mpix/s     | 593 Mpix/s     | +10% |
| bench-svs (single)  | 596 Mpix/s     | ~577 Mpix/s    | noise |
| bench-svs-mt        | 2121 Mpix/s    | 2117 Mpix/s    | noise |

bench-svs is unchanged because it calls `Slide.DecodedTile` directly
(bypassing `ReadRegion`); Layers 1 and 2 only optimize the
`ReadRegion` path.

### Allocation reduction (bench-ndpi-mt, alloc_space)

| Source                                 | v0.28    | v0.29    |
|---|---|---|
| Per-tile output (Layer 2)              | 22 GB    | ~0       |
| pixelCache frame (Layer 3 — abandoned) | 11.4 GB  | 11.4 GB  |
| Total `decoder.NewImageFormat`         | 38.7 GB  | ~17 GB   |

57% allocation reduction; the remaining 11 GB is in the NDPI
pixelCache and remains for a future milestone.

### Added (internal only)

- `borrowTileScratch(w, h int, format decoder.PixelFormat) *decoder.Image`
  — module-level `sync.Pool` accessor; lazy per-(W, H, Format).
- `returnTileScratch(img *decoder.Image)` — paired return; nil-safe.

### Changed

- **Layer 1** — `Slide.imageReadRegionImpl`: clip-to-bounds
  computation moved ahead of `fillWhite`; fillWhite gated on
  `!fullyInBounds || edgeTileX || edgeTileY`. Saves ~5% on multi-
  thread and ~16% on single-thread NDPI ReadRegion calls.
- **Layer 2** — `Slide.imageReadRegionImpl` borrows a scratch tile-
  Image once per call and uses `ImageDecodedTileInto` across the
  tile loop. `Slide.ImageDecodedTileInto` fast-path dispatch passes
  `Dst: dst` into the decoded-tile call; skips `copyImageInto` when
  the fast path wrote directly into `dst`.
  `formats/ndpi.strippedImage.DecodedTile` honors `opts.Dst` when
  caller-provided dimensions+format match.
- `Makefile`: `MIN_NDPI_MPIXS` tightened from 220 to 270 Mpix/s
  (~90% of measured v0.29 single-thread baseline).

### Public API

- **No additions.** No new exported types, functions, or methods.
- **No breaking changes.** RawTile, DecodedTile, ReadRegion,
  ScaledStrips, and every format reader behave bit-identically.

### Tests

- `decoded_tile_scratch_test.go` (NEW): 5 pool unit tests under
  `-race -count=3`.
- `slide_region_layer1_test.go` (NEW, package `opentile`): 2 Layer 1
  tests with synthetic `knownPixelReader`.
- `formats/ndpi/stripped_decodedtile_test.go` (extended): 2 NDPI
  prereq tests.

### Layer 3 abandonment

Layer 3 — NDPI `pixelFrameCache` scratchPool — was implemented and
then reverted within v0.29. The race: when a cached frame's
`*decoder.Image` is recycled to the pool on eviction, a goroutine
still holding the pointer (from a previous cache hit) can read the
buffer concurrently with a new decoder writing into it. Caught by
an aggressive new concurrent test; not by existing parity tests
(small race window, low contention). Path forward (deferred):
refcount each cached entry with explicit `release()` callback, or
adopt an immutable-frames + per-blit-owned-scratch topology. Layer
3's allocation target (~11 GB of pixelCache frames) remains
available for a future milestone.

### Out of scope (deferred forward)

- **Layer 3** (NDPI pixelCache scratch pool) — abandoned with race
  detected; needs refcount design or alternative.
- **`Slide.DecodedTile` allocation reduction** — direct callers
  receive user-owned `*decoder.Image`; would require API changes.
- **`ScaledStrips` allocation profile** — has its own internal tile
  cache + worker pool; not profiled in v0.29.

### Pre-existing issues out of scope

- `tests/oracle/` build break (v0.24 Level API drift). Same as
  v0.27 / v0.28; not v0.29-introduced.

## [0.28.0] — 2026-05-29

Cross-format decoder-handle pool. Eliminates per-tile
`decoder.Factory.New() / dec.Close()` churn across every format
routing through `Slide.ImageDecodedTile`. Introduces a fixed-size
pool of long-lived `decoder.Decoder` instances per `(Slide, codec)`,
sized `min(NumCPU, 8)`. NDPI's v0.27 per-`strippedImage` handle
migrates to the same shared primitive, gaining multi-core parallelism
on `Slide.DecodedTile` calls.

### Measured throughput (Apple Silicon, 13 cores)

| Bench               | Mode           | Throughput     | Notes |
|---|---|---|---|
| bench-ndpi          | single-thread  | 251.0 Mpix/s   | v0.27 was 243; ~3% diff is run-to-run noise |
| bench-ndpi-mt       | multi-thread   | 539.5 Mpix/s   | ~2.15× single-thread |
| bench-svs           | single-thread  | 595.8 Mpix/s   | new bench in v0.28 |
| bench-svs-mt        | multi-thread   | 2121 Mpix/s    | ~3.56× single-thread |

NDPI single-thread is unchanged (v0.27 already used a long-lived
handle). NDPI multi-thread shows 2.15× because the bench uses
`ReadRegion`, where Go-side `fillWhite` allocation dominates wall
time. SVS multi-thread shows 3.56× — a cleaner test of the pool's
deliverable because the SVS bench calls `DecodedTile` directly and
SVS routes through the v0.26 slow path (no v0.27 fast-path
optimization), so every call exercises the new pool.

CPU profile shape: pre-v0.28 the slow path showed `tjDestroy`
(240 µs/call) + `tjInit` (~50 µs/call) as significant hot spots.
Post-v0.28 both are gone from per-tile cost; `tjDecompress2`
remains the dominant cgo function.

### Added (internal only)

- `internal/decoderhandle.Pool` — fixed-size pool of long-lived
  decoder.Decoder instances. Lazy member creation; mutex-guarded
  outstanding counter; channel-based borrow/return. Replaces
  v0.27's `formats/ndpi.decoderHandle`. 8 unit tests covering
  sequential reuse, concurrent fanout, lazy creation, Close races,
  double-close, factory-returns-nil.
- `(*Slide).decoderFor(tag uint16)` (unexported) — Slide-level pool
  cache accessor under `Slide.handlesMu`.
- `(*Slide).HandleCountForTest()` (test-only via export_test.go) —
  exposes pool-cache map length for integration tests.
- `cmd/bench/svs/` — single-thread + multi-thread SVS tile-decode
  benchmark plus README.

### Changed

- `(*Slide).ImageDecodedTile` and `(*Slide).ImageDecodedTileInto`
  slow paths replaced `fac.New() / dec.Close()` with
  `pool.Borrow() / pool.Return()`. v0.27 NDPI fast-path dispatch
  unchanged in shape (also migrated to the shared pool type).
- `(*Slide).Close` drains every cached decoder pool before
  delegating to `s.r.Close`. First-error semantics; idempotent.
- `formats/ndpi/strippedImage.decHandle` retypes from
  `*decoderHandle` to `*decoderhandle.Pool`. NDPI fast-path code
  switches from direct `dec.Decode` to `pool.Borrow() /
  pool.Return()`. NDPI single-thread bench unchanged; NDPI
  multi-thread now lifts the v0.27 single-mutex cap.
- `formats/ndpi/decoder_handle.go` and `decoder_handle_test.go`
  **deleted** — superseded by the shared package.
- `cmd/bench/ndpi/main.go` gained `-goroutines N` flag.
- `Makefile`: `MIN_NDPI_MPIXS` tightened from 130 to 220 Mpix/s
  (catches the regression class the prior loose gate hid). New
  `MIN_SVS_MPIXS=566` (95% of measured 596 baseline). New targets:
  `bench-ndpi-mt`, `bench-svs`, `bench-svs-mt`.

### Public API

- **No additions.** No new exported types, functions, or methods.
- **No breaking changes.** RawTile, ScaledStrips, ReadRegion,
  DecodedTile, and every format reader behave identically.

### Tests

- `internal/decoderhandle/handle_test.go` — 8 pool unit tests under
  `-race -count=3`.
- `slide_handle_test.go` (`opentile_test` package) — Slide-level
  integration: handle reuse, Close releases handles, 32-goroutine
  fanout safety.
- `export_test.go` — test-only `HandleCountForTest` accessor for
  pool-cache map shape.

### Out of scope (deferred forward)

- **NDPI handle instance consolidation** (sharing one Slide-level
  pool across all NDPI levels) — would require `formats/ndpi`
  knowing about `Slide`. Deferred indefinitely.
- **sync.Pool migration** — fixed-channel pool gives deterministic
  teardown; revisit if memory pressure surfaces.
- **NDPI oneframe fast path** — confirmed during v0.28 brainstorm
  that oneframe fires only on tiny levels (<1 MB RGB; only CMU-1
  L3 at 4 tiles total). Not worth a perf milestone.
- **`tests/oracle/`** build break (v0.24 Level API drift) — still
  pre-existing, still out of scope.

## [0.27.0] — 2026-05-28

NDPI striped fast pixel path (decode-once-per-strip + blit). Closes
the ~5× per-thread perf gap between opentile-go and openslide on
NDPI tile decode by adding a decoded-pixel-frame LRU cache plus a
reusable decoder handle inside `formats/ndpi/strippedImage`, dispatched
from `Slide.ImageDecodedTile` via an unexported `decodedTiler`
interface.

### Measured throughput (Apple Silicon, CMU-1.ndpi L0, 29,800 tiles, single-thread)

| Build           | Wall    | Throughput   | vs openslide |
|-----------------|---------|--------------|--------------|
| openslide 4.0.0 | 8.38 s  | 233.0 Mpix/s | 1.00×        |
| v0.26           | 44.25 s | 44.1 Mpix/s  | 5.28× slower |
| **v0.27**       | **8.03 s** | **243.1 Mpix/s** | **0.96× (faster)** |

CPU profile: v0.26 spent 78.5% in `tjTransform` (lossless JPEG crop)
+ 16.9% in `tjDestroy` (per-tile decoder-handle churn) + 3.3% in
actual decode. v0.27 eliminates both: each strip is decoded once via
a long-lived decoder handle, and per-tile requests blit a region out
of the cached pixels. The new path's hot spots are pixel `memmove`
(20.4%) and Go-side blit (2.8%).

### Added (internal only)

- `internal/fastpath.ErrUnsupported` — dispatch sentinel signalling
  slow-path fallback. Tiny shared package; not exposed publicly.
- `formats/ndpi.pixelFrameCache` — bounded LRU of decoded RGB frames,
  capacity = `max(runtime.NumCPU(), 16)`. Promise pattern: concurrent
  goroutines for the same key share one decode.
- `formats/ndpi.decoderHandle` — single long-lived `decoder.Decoder`
  per `strippedImage`, serialized by mutex.
- `(*strippedImage).DecodedTile` — fast pixel path with edge-tile
  fallback to the existing `CropWithBackgroundLuminanceOpts` path.
- `(*tiler).ImageDecodedTile` — NDPI reader dispatch; returns
  `fastpath.ErrUnsupported` for non-striped levels (oneframe,
  associated images).
- `cmd/bench/ndpi/` — committed bench programs (Go subject + C
  openslide reference) for regression tracking. `make bench-ndpi`
  enforces ≥130 Mpix/s on CMU-1.ndpi.

### Changed

- `(*Slide).ImageDecodedTile` and `(*Slide).ImageDecodedTileInto`
  now type-assert on `decodedTiler` and dispatch to the fast path
  when supported. Non-NDPI formats, non-striped NDPI levels, and
  `WithScale != 1` calls keep the v0.26 behavior exactly.
- `(*fileCloser).ImageDecodedTile` and `(*mmapCloser).ImageDecodedTile`
  — delegation methods so the type assertion succeeds on the
  reader-wrapper types `Slide.r` points at.
- `formats/ndpi.tiler.Close` — releases each `strippedImage`'s
  long-lived decoder handle. Was a no-op `return nil` in v0.26.

### Public API

- **No additions.** No new exported types, functions, or methods on
  any v0.3+ public surface.
- **No breaking changes.** RawTile (compressed bytes API) is bit-for-
  bit unchanged; `ScaledStrips`, `ReadRegion`, and all format readers
  inherit the speedup transparently.

### Tests

- `formats/ndpi/stripped_pixel_parity_smoke_test.go` — foundational
  v0.27 design-gate test.
- `formats/ndpi/stripped_decodedtile_test.go` — end-to-end fast-path
  pixel parity (`TestNDPIFastPathPixelParity`) + 32-way concurrency
  stress (`TestNDPIFastPathConcurrent`).
- `formats/ndpi/pixel_cache_test.go` — cache hit/miss/eviction/
  promise/error/thrash unit coverage.
- `formats/ndpi/decoder_handle_test.go` — handle lifecycle +
  concurrent decode + double-close.
- `tests/ndpi_decodedtile_parity_test.go` — cross-fixture DecodedTile
  parity over CMU-1.ndpi, OS-2.ndpi, Hamamatsu-1.ndpi (all levels,
  including the oneframe-path fixture which exercises the slow-path
  fallback).

### Out of scope (deferred forward)

- **NDPI oneframe path** (`internal/oneframe`) — same algorithmic
  opportunity, structurally similar. Hamamatsu-1.ndpi still uses the
  v0.26 slow path through the dispatch fallback. Likely v0.28 if
  benched real-world workloads justify continuing.
- **Tactical decoder-handle pooling for the RawTile path** —
  ~7s of `tjTransform`/`tjDestroy` churn remains on RawTile-driven
  workloads (wsitools splice template path). v0.27 didn't touch
  RawTile; mostly invisible to current consumers.
- **`ScaledStrips` decoder-handle pool** — current implementation
  uses one mutex-serialized handle per `strippedImage`; under heavy
  NumCPU fanout this could become a bottleneck. Estimated ~210 ms of
  queue time per worker on the CMU-1 bench.
- **JPEG-frame cache bounding** — unchanged from v0.26. Pre-existing
  unbounded growth (~200 MB on CMU-1 L0); CLAUDE.md `stripped.go`:67-73
  documents as acceptable.
- **`WithScale != 1` integration with the pixel cache** — currently
  falls through to the slow path for that single call. Not blocking;
  flagged for v1.0 API review.

### Pre-existing issues out of scope

- `tests/oracle/` (Python opentile parity oracle) has stale code
  referring to v0.24-removed Level methods. Was already broken on
  `main` before v0.27; not a regression. The snapshot-based
  `tests/parity/` suite continues to pass over the full 40-fixture
  set.

## [0.26.0] — 2026-05-26

Adds high-throughput strip iterator to `*Slide`. The libvips-speed
primitive that dzsave / tile-server / region-extract tools consume.

### Added

- `(*Slide).ScaledStrips(l0Rect, outSize, stripHeight, opts ...StripOption) *StripIterator` —
  iterate a slide's L0 rectangle scaled to outSize, in horizontal
  strips. Internally manages parallel decode workers + per-iterator
  tile cache + lookahead pre-fetch.
- `*StripIterator` with `Next()`, `Close()`, `Strips()` methods.
- `StripOption` functional-options type:
  - `WithStripWorkers(n int)` — parallel decode workers (default NumCPU).
  - `WithStripLookahead(strips int)` — pre-fetch depth (default 2).
  - `WithStripIDCTScale(scale int)` — JPEG IDCT override (default auto).
  - `WithStripKernel(k resample.Kernel)` — resample kernel (default Lanczos).
  - `WithStripContext(ctx context.Context)` — external cancellation.
- Auto-select source pyramid level via `BestLevelForDownsample` +
  auto IDCT scale (1/2/4/8) for JPEG sources, minimizing wasted
  decode work.

### Unchanged

- All v0.25 APIs (ReadRegion, ReadRegionScaled, BestLevelForDownsample,
  DecodedTile, RawTile, splice-prefix family).
- Format readers (purely additive; no internal changes).

### Out of scope (deferred)

- Public `*Cache` type for cross-iterator sharing (per-iterator
  cache only in v0.26). v0.27+ if a tile-server consumer needs it.
- Multi-image variant `ImageScaledStrips(image, ...)`. Future
  release.
- Go 1.23 range-over-function adapter. Trivial to add later.
- Benchmarks vs libvips dzsave. Follow-up after the first
  working implementation.

## [0.25.0] — 2026-05-25

Adds arbitrary-rectangle region reads to `*Slide`. The
openslide-equivalent decoded-pixel surface most pathology consumers
expect. Purely additive over v0.24 — no breaking changes.

### Added

- `(*Slide).ReadRegion(level, x, y, w, h int, opts ...DecodeOption) (*decoder.Image, error)` —
  decoded pixels for an arbitrary rectangle at the given level. ALL
  FOUR coords/dims at the level's own resolution. Out-of-bounds areas
  filled with white (0xFF, 0xFF, 0xFF).
- `(*Slide).ReadRegionInto(level, x, y int, dst *decoder.Image, opts ...DecodeOption) error` —
  fill caller-provided Image.
- `(*Slide).ImageReadRegion`, `(*Slide).ImageReadRegionInto` —
  multi-image variants for OME-TIFF.
- `(*Slide).ReadRegionScaled(l0x, l0y, l0w, l0h, outW, outH int, opts ...DecodeOption) (*decoder.Image, error)` —
  read an L0 rectangle and resample to target output dimensions.
  Picks the best source pyramid level automatically.
- `(*Slide).ReadRegionScaledInto`, `(*Slide).ImageReadRegionScaled`,
  `(*Slide).ImageReadRegionScaledInto` — variants.
- `(*Slide).BestLevelForDownsample(downsample float64) int` —
  level selection helper matching openslide.best_level_for_downsample
  semantics.
- `(*Slide).ImageBestLevelForDownsample` — multi-image variant.
- `WithResampleKernel(k resample.Kernel) DecodeOption` — kernel
  selection for ReadRegionScaled (default Lanczos). No-op on
  ReadRegion.
- `ErrRegionEmpty` sentinel — returned when the requested rectangle
  has no in-bounds pixels.
- `Level.Downsample float64` field — populated at Open time as L0
  Size.W / level Size.W. Used by BestLevelForDownsample and
  ReadRegionScaled.

### Coordinate convention

opentile-go diverges from openslide.read_region's mixed-coord
convention (L0 origin + level-resolution size). Instead:

- `ReadRegion` uses **uniform level coords**: all four args at the
  requested level's resolution.
- `ReadRegionScaled` uses **uniform L0 coords + arbitrary output
  resolution**: the actual openslide use case in cleaner shape.

Translating from openslide-python:

```python
# Old: slide.read_region((l0_x, l0_y), level, (w, h))
# New (opentile-go):
slide.ReadRegion(level, l0_x // int(level.Downsample), l0_y // int(level.Downsample), w, h)
```

The division is intentional — explicit at the call site rather than
buried in mixed-convention parameters.

### Unchanged

- All v0.24 APIs (RawTile, DecodedTile, splice-prefix family).
- Format readers (no changes — region reads compose over DecodedTile).
- File output bytes from format readers — region reads are decode-only.

[0.26.0]: https://github.com/WSILabs/opentile-go/releases/tag/v0.26.0
[0.25.0]: https://github.com/WSILabs/opentile-go/releases/tag/v0.25.0

## [0.24.0] — 2026-05-25

**BREAKING:** Level and Image are now value-type structs. Tile reads
moved to *Slide. New DecodedTile methods dispatch through the decoder/
package.

### Removed (BREAKING)

- Level interface — replaced by value-type struct.
  - `Level.Tile(tx, ty int) ([]byte, error)` — use `*Slide.RawTile`.
  - `Level.TileInto(tx, ty int, dst []byte) (int, error)` — use `*Slide.RawTileInto`.
  - `Level.TileAt(coord TileCoord) ([]byte, error)` — use `*Slide.RawTile`.
  - `Level.Size() Size` — use field `level.Size`.
  - `Level.TileSize() Size` — use field `level.TileSize`.
  - `Level.Grid() Size` — use field `level.Grid`.
  - `Level.Compression() Compression` — use field `level.Compression`.
  - `Level.Index() int` — use field `level.Index`.
  - `Level.PyramidIndex() int` — use field `level.PyramidIndex`.
  - `Level.MPP() SizeMm` — use field `level.MPP`.
  - `Level.FocalPlane() float64` — use field `level.FocalPlane`.
  - `Level.TileOverlap() image.Point` — use field `level.TileOverlap`.
  - `Level.TileMaxSize() int` — use `*Slide.TileMaxSize`.
  - `Level.TilePrefix() []byte` — use `*Slide.TilePrefix`.
  - `Level.TileBodyMaxSize() int` — use `*Slide.TileBodyMaxSize`.
  - `Level.TileBodyInto(tx, ty int, dst []byte) (int, error)` — use `*Slide.TileBodyInto`.
  - `Level.TileReader(tx, ty int) (io.ReadCloser, error)` — use `*Slide.TileReader`.
  - `Level.Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult]` — use `*Slide.RangeTiles`.
- Image interface — replaced by value-type struct.
  - `Image.Name() string` — use field `image.Name`.
  - `Image.Index() int` — use field `image.Index`.
  - `Image.Levels() []Level` — use field `image.Levels`.

### Added

- Value-type `opentile.Level` struct with inspection fields (Index,
  PyramidIndex, Size, TileSize, Grid, Compression, MPP, FocalPlane,
  TileOverlap).
- Value-type `opentile.Image` struct (Name, Index, Levels).
- `(*Slide).RawTile(level, tx, ty int) ([]byte, error)` — raw tile
  bytes at the given level.
- `(*Slide).RawTileInto(level, tx, ty int, dst []byte) (int, error)` —
  fill caller-provided buffer with raw tile bytes.
- `(*Slide).ImageRawTile(image, level, tx, ty int) ([]byte, error)` —
  multi-image variant.
- `(*Slide).ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error)` —
  multi-image fill variant.
- `(*Slide).DecodedTile(level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error)` —
  raw tile → decoded RGB (or RGBA) pixels via the v0.22 decoder
  registry. Requires a blank-import of one of the decoder subpackages
  (e.g., `decoder/jpeg`) or `decoder/all`.
- `(*Slide).DecodedTileInto(level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error` —
  decode into caller-provided destination Image.
- `(*Slide).ImageDecodedTile` and `(*Slide).ImageDecodedTileInto` —
  multi-image variants.
- `opentile.DecodeOption` functional-options type with `WithFormat` and
  `WithScale` helpers.
- `opentile.ErrCodecNotRegistered` sentinel error returned when
  DecodedTile is called and no decoder is registered for the level's
  Compression. Error message includes the rebuild diagnostic.

### Migration guide

| Before (v0.23)                              | After (v0.24)                            |
|---------------------------------------------|------------------------------------------|
| `slide.Levels()[i].Tile(tx, ty)`            | `slide.RawTile(i, tx, ty)`               |
| `slide.Levels()[i].TileInto(tx, ty, dst)`   | `slide.RawTileInto(i, tx, ty, dst)`      |
| `slide.Levels()[i].Size()`                  | `slide.Levels()[i].Size`                 |
| `slide.Levels()[i].Compression()`           | `slide.Levels()[i].Compression`          |
| (and similar for TileSize, Grid, MPP, etc.) | field access                             |
| (no v0.23 equivalent)                       | `slide.DecodedTile(i, tx, ty)` — new     |

To enable DecodedTile, blank-import a decoder subpackage:

```go
import _ "github.com/wsilabs/opentile-go/decoder/all"  // or specific codecs
```

### Unchanged

- `*Slide` construction (OpenFile, Open).
- Format detection and dispatch (no format-package public surface
  change beyond the Reader interface in `internal/format`).
- Raw tile bytes — byte-identical to v0.23 output (verified by parity
  tests).
- pre-v0.24 binaries continue to function; only source-level
  migration is required.

### Why this change

Per the strategic-direction sketch (wsitools/docs/strategic-direction.md
§1), Level-as-value-type lets *Slide grow new methods (DecodedTile,
ReadRegion, ScaledStrips) without requiring every format package to
implement them. This is the second installment of the v1.0 redesign;
v0.25 will add ReadRegion, v0.26 ScaledStrips with parallel decode +
cache + lookahead.

## [0.23.0] — 2026-05-24

**BREAKING:** Replaced the public `Tiler` interface with a `*Slide` struct.

### Removed (BREAKING)

- `opentile.Tiler` interface — gone entirely; no deprecation shim.
- `opentile.OpenTiler(path string) (Tiler, error)` — use
  `opentile.OpenFile(path string) (*Slide, error)`.
- `opentile.FormatFactory` interface — format packages no longer
  implement this; registration is via `format.Register` in `init()`.
- `opentile.Register(f FormatFactory)` — replaced by
  `internal/format.Register(name, match, opener)`.
- `opentile.RawUnsupported` struct (and its `SupportsRaw` / `OpenRaw`
  methods) — the raw-vs-TIFF dispatch distinction is gone; every
  format registers a single `Match + Opener` pair.
- `opentile.Formats() []Format` — no replacement; iterate
  `(*Slide).Format()` on open slides.

### Fixed

- `*opentile.Config` accessor methods (`TileSize`, `CorruptTilePolicy`,
  `NDPISynthesizedLabel`, `Backing`) are now nil-safe on the zero
  `&Config{}` — they previously panicked on the nil internal pointer.
  Pre-existing latent bug surfaced by v0.23's dual-registration
  refactor.

### Added

- `opentile.Slide` struct — new canonical handle for an open slide.
- `opentile.OpenFile(path) (*Slide, error)` — open by path.
- `opentile.Open(r io.ReaderAt, size int64, opts ...Option) (*Slide, error)`
  — open from any ReaderAt; identical option set to old `OpenTiler`.
- `(*Slide).Close() error`
- `(*Slide).Format() Format`
- `(*Slide).Metadata() Metadata`
- `(*Slide).Levels() []Level`
- `(*Slide).Level(i int) (Level, error)`
- `(*Slide).Images() []Image`
- `(*Slide).Associated() []AssociatedImage`
- `(*Slide).ICCProfile() []byte`
- `(*Slide).WarmLevel(i int) error`
- `internal/format` package — unexported `Reader` interface, `Register`,
  `OpenAny`, `ErrUnknownFormat`; consumed by format packages and
  `opentile.Open` / `opentile.OpenFile`.

### Migration guide

The shape of the API is unchanged — method signatures on `*Slide`
mirror the previous `Tiler` interface exactly. Migrating a consumer
is a mechanical rename:

| Before                                  | After                                |
| --------------------------------------- | ------------------------------------ |
| `opentile.Tiler`                        | `*opentile.Slide`                    |
| `opentile.OpenTiler(path)`              | `opentile.OpenFile(path)`            |
| `t.RawTile(level, tx, ty)`              | unchanged                            |
| `t.Levels()`, `t.Metadata()`, etc.      | unchanged                            |
| `t.Close()`                             | unchanged                            |

For each consumer, the migration is:
1. Bump opentile-go to v0.23.0.
2. Search-and-replace `opentile.OpenTiler(` → `opentile.OpenFile(`.
3. Search-and-replace declared types `opentile.Tiler` →
   `*opentile.Slide`.
4. Compile; fix any straggler references.

### Why this change

The old `Tiler` interface forced every format implementer to grow in
lockstep with the public API. Adding decoded-tile methods, region
reads, or a strip iterator would require every format to implement
them or break the interface contract. Collapsing to `*Slide` with a
format reader as an unexported interface lets future methods land on
`*Slide` without touching format implementations.

This is the first installment of the v1.0 redesign sketched in
`docs/strategic-direction.md`. v0.24+ will add decoded-tile access;
v0.25+ adds region reads; the strip iterator with parallel decode +
cache + lookahead is the v1.0 milestone.

## [0.22.1] — 2026-05-24

Patch release fixing `decoder/htj2k` cgo compilation against openjph 0.27+.

### Fixed

- `decoder/htj2k` cgo path now properly compiles against openjph 0.27+. v0.22.0
  inlined C++ headers in cgo's C preamble, which failed with "cstdlib not found"
  when openjph was present. Moved C++ decode logic to a separate `shim.cpp` with
  proper Go build constraint and `CXXFLAGS`/`LDFLAGS` configuration. Decode is now
  real and pixel-correct (lossless round-trip tested). The `nocgo` and `nohtj2k`
  opt-out stubs are unchanged.

## [0.22.0] — 2026-05-23

Decoder + resample lift from wsitools. Adds the read-side codec layer
that opentile-go v1.0's `*Slide` decoded-pixel methods will consume.
Pure addition — no public API change, no behavior change for existing
consumers.

### Added

- New `decoder/` package: public `Decoder` interface, `DecodeOptions`,
  `Factory` interface, registry (`Register`, `Get`, `GetByCompressionTag`,
  `Registered`), and `Image` + `PixelFormat` value types.
- 9 codec subpackages registering against the registry at `init()`:
  - Pure-Go: `decoder/none`, `decoder/lzw`, `decoder/deflate`.
  - cgo: `decoder/jpeg` (libjpeg-turbo, with IDCT-time scale factor),
    `decoder/jpeg2000` (openjp2), `decoder/jpegxl` (libjxl),
    `decoder/avif` (libavif), `decoder/webp` (libwebp), `decoder/htj2k`
    (openjph).
- `decoder/all` — blanket side-effect import for "every codec
  available."
- `resample/` package: pure-Go Nearest, Bilinear, Lanczos, and Box
  (area-averaging) resamplers operating on `decoder.Image`.
- Per-codec build-tag opt-outs (`nojxl`, `noavif`, `nowebp`, `nohtj2k`)
  + master `nocgo`. Disabled codecs register a stub that returns
  `decoder.ErrCodecUnavailable` with a precise rebuild diagnostic.

### Unchanged

- All format readers (`formats/svs/`, `formats/philipstiff/`, etc.) and
  the public `Tiler` interface.
- `internal/jpegturbo/`, `internal/tifflzw/`, `internal/jpeg/` —
  untouched.

## [0.21.0] — 2026-05-23

Relocation release: repository moved from `github.com/cornish/opentile-go`
to `github.com/wsilabs/opentile-go`. No behavior change; this release
exists so consumers can update their import paths. The old path
continues to redirect at the HTTPS layer for existing clones, but Go
module consumers must update their `go.mod` and import statements:

```diff
- github.com/cornish/opentile-go
+ github.com/wsilabs/opentile-go
```

Minor bump (rather than patch) reflects that this is a source-breaking
change for module consumers, even though the binary behavior is
unchanged.

Active limitations after v0.21.0: same as v0.20 (no new L items in
either v0.20.x patch or this relocation release). See
`docs/deferred.md` §11 consolidated backlog.

## [0.20.1] — 2026-05-20

Closes R23. Surgical fix to scanner attribution for Grundium-source
COG-WSI files. Pre-fix the cogwsi reader read the standard TIFF
Make tag verbatim (`"Aperio"` — preserved from the source SVS's
format-vendor label) and missed the writer-vendor info encoded in
the comma-suffix of the preserved Software tag (`"Aperio Image,
Grundium Ocus"`). v0.18's SVS reader already parses this for
direct SVS opens; v0.20.1 reuses that detection for COG-WSI.

### Fixed

- Grundium-source COG-WSI fixtures (`scan_617_cog-wsi.tiff`,
  `scan_620_cog-wsi.tiff`, `svs_40x_bigtiff_cog-wsi.tiff`) now
  correctly report `ScannerManufacturer = "Grundium"`, `ScannerModel
  = "Ocus"`. Pre-fix they reported `ScannerManufacturer = "Aperio"`
  (the format-vendor label, not the writer-vendor).
- Fixture JSONs for the 3 affected COG-WSI fixtures updated to
  match.

### Changed

- `svs.WriterVendor` (struct with `Manufacturer` / `Model` /
  `Softwares` fields) and `svs.DetectWriter(firstLine string)
  WriterVendor` are now **exported** from `formats/svs`. The
  cogwsi reader imports and reuses them. Internal SVS callers
  updated for the new export-cased identifiers.

### Notes

- Future-proof: any future SVS writer-vendor pattern `svs.
  DetectWriter` learns automatically applies to COG-WSI files
  with `Properties["cog-wsi.source-format"] == "svs"`.
- Canonical Aperio COG-WSI files (`CMU-1_cog-wsi.tiff`,
  `CMU-1-Small-Region_cog-wsi.tiff`, `JP2K-33003-1_cog-wsi.tiff`)
  are untouched — first-line pattern `"Aperio Image Library
  v..."` still detects as Aperio.
- Non-SVS-source COG-WSI files (`Leica-1_cog-wsi.tiff`,
  `Philips-1_cog-wsi.tiff`, `Ventana-1_cog-wsi.tiff`,
  `cervix_2x_jpeg_cog-wsi.tiff`) are untouched — the SVS
  detection path only triggers on `source-format == "svs"`.
- cgo footprint unchanged.

## [0.20.0] — 2026-05-20

Cross-format Writer typed field — closes R22. Adds `Writer string`
to `opentile.Metadata` carrying the file-producer identifier.
Pure-additive; no API breakage; no behavior changes for existing
consumers.

### Added

- **`opentile.Metadata.Writer string`** — typed field for the file
  producer (the software that wrote the file). Distinct from:
  - `ScannerManufacturer` (scanner OEM — who made the hardware)
  - `ScannerSoftware []string` (broader software stack — may include
    both writer and scanner software)
- Per-format Writer population:
  - **SVS Aperio canonical**: `"Aperio Image Library v11.2.1"` (full
    SoftwareLine; preserves version)
  - **SVS Grundium / non-canonical**: `"Grundium Ocus"` (comma-suffix
    writer from v0.18 detection)
  - **NDPI**: format-specific Model identifier (e.g. `"NanoZoomer"`)
  - **Philips TIFF**: raw `DICOM_SOFTWARE_VERSIONS` field (may include
    surrounding double-quotes)
  - **OME-TIFF**: `"OME Bio-Formats X.Y.Z"` (Creator attribute
    promoted from `Properties["ome.creator"]`)
  - **Leica SCN**: primary image's `<device version>`
  - **BIF**: iScan `BuildVersion`
  - **IFE**: ImageDescription first line (Iris encoder identifier)
  - **SZI**: `"<SoftwareName> <SoftwareVersion>"` combined (empty when
    SoftwareName absent — e.g., Grundium SZI)
  - **Generic-TIFF (no wsi-tools)**: TIFF `Software` tag value
  - **Generic-TIFF (wsi-tools)**: `"wsitools/<version>"` (overrides
    Software-derived value when wsi-tools parser triggers)
  - **COG-WSI**: `"wsitools/<WSIToolsVersion>"` from private tag 65084
    (file producer; source scanner stays in ScannerManufacturer per spec)
- `TestCrossFormatMetadata` extended with per-fixture `wantWriterContains`
  substring assertions.

### Notes

- **Backward-compat**: Properties keys (`ome.creator`,
  `cog-wsi.wsitools-version`) continue to populate as before. The
  new typed `Writer` field is the primary surface; Properties keys
  remain accessible at zero extra cost.
- **Q5 semantics**: for converted files (OME-TIFF via Bio-Formats;
  COG-WSI via wsitools; wsi-tools-converted generic-TIFF), Writer
  is the converter and ScannerManufacturer/Model/Software preserve
  the source scanner attribution per format spec.
- **R23 derived bug**: Grundium-source COG-WSI files report
  `ScannerManufacturer = "Aperio"` (pre-v0.18 attribution leaked
  through the wsitools-preserved TIFF Make tag). Filed as R23 in
  the deferred backlog for a separate fix; out of v0.20 scope.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.19.1] — 2026-05-20

Coverage cleanup patch. No API changes; no behavior changes. Three
previously sub-80%-coverage packages brought above the
`make cover` gate via targeted test additions.

### Changed

- `formats/cogwsi` coverage 77.5% → 91.2%. Targeted tests for
  `Factory.Format`, `Tiler.Level`, `Tiler.WarmLevel`,
  `Tiler.UnwrapTiler`, plus error branches in `openCOGWSI`.
- `formats/szi` coverage 76.8% → 92.8%. New `factory_test.go` +
  `errors_test.go` + `image_test.go`. Synthetic SZI byte payloads
  via `archive/zip.Writer` exercise the missing-manifest /
  missing-scan-properties / malformed-XML / multi-root-folder
  error branches.
- `internal/oneframe` coverage 70.4% → 93.1%. **New
  `internal/oneframe/oneframe_test.go`** — this internal package
  previously had no dedicated tests; coverage flowed entirely
  through NDPI striped + OME OneFrame integration. v0.19.1 T3
  adds focused unit tests for v0.13 splice stubs (TilePrefix /
  TileBodyInto / TileBodyMaxSize), error branches
  (OOB / dimension-unavailable / short-buffer), tile iterator,
  and constructor validation.

### Notes

- `internal/oneframe.warm()` is defined but never called in the
  current codebase. Flagged as a dead-code candidate for a future
  cleanup OR intentional reservation for a future warm-strategy
  feature.
- All 22 packages now ≥80% per CLAUDE.md's `make cover` gate.
- Side benefit: `formats/generictiff` coverage jumped 83.0% →
  87.6% because new cogwsi tests exercise the generictiff-
  delegated path.
- cgo footprint unchanged.

## [0.19.0] — 2026-05-20

COG-WSI support — closes user's two GH issues (#5 + #6). New
`formats/cogwsi/` package + `internal/cog/` ghost-area parser +
WSI private tag readers in `internal/tiff`. Extends `formats/
generictiff/` to honor WSI tags as authoritative + accept clean
integer-multiple pyramid ratios (Issue #5 standalone benefit).

### Added

- New `opentile.FormatCOGWSI = "cog-wsi"` enum value.
- New `formats/cogwsi/` reader with ghost-area dispatch + spec
  validation + canonical metadata via WSI private tags.
- New `cogwsi.ErrNotConformantCOGWSI` sentinel returned at `Open`
  on spec violations (ghost-area / IFD-ordering / WSI-tag).
- New `internal/cog/` package: GDAL ghost-area parser
  (`GhostArea`, `ParseGhostArea`, `ParseCOGWSIVersion`,
  `ErrGhostAreaMalformed`). Designed for COG-WSI's primary use +
  plain-COG forward-compat.
- New `internal/tiff` WSI private tag readers (tag IDs 65080-65087
  per COG-WSI spec §5.2). 8 typed accessors on `*tiff.Page`;
  added `Page.doubleTag` helper (mirrors the existing Float32
  accessor pattern).
- `formats/generictiff/` extended: WSIImageType-aware
  classification (Issue #5 part A; wrapper function
  `ClassifyAssociatedFromPage` preserves the existing signature)
  + integer-multiple pyramid ratio acceptance (Issue #5 part B;
  `isIntegerMultipleRatio` predicate).
- `cogwsi.Tiler.UnwrapTiler() opentile.Tiler` — exposes the inner
  generic-TIFF Tiler so callers that use type-asserted format-
  specific helpers (e.g., `generictiff.MetadataOf`) work through
  the wrapper.
- 10 new test fixtures wired into TestSlideParity
  (30 → 40 fixtures): wsitools-converted from every source
  format opentile-go reads (5 SVS, 1 Philips TIFF, 1 OME-TIFF,
  1 BIF, 1 IFE, 1 SZI). `CMU-1-Small-Region_cog-wsi.tiff`
  full-walk; the other 9 sampled by default.
- `tests/parity/cogwsi_geometry_test.go` per-fixture geometry pin.
- Cross-fixture parity gate: each `<source>_cog-wsi.tiff` vs the
  original `<source>` confirms writer preserves bit-exact
  geometry + tile bytes per spec.
- `docs/formats/cogwsi.md` — full format doc.

### Changed

- `internal/tiff/classify_pyramid.go::buildPyramidChain` now
  accepts clean integer-multiple step ratios (1×, 2×, 4×, 8×,
  16×, …). Pre-v0.19 the strict drift check rejected mixed-ratio
  chains. Standalone benefit: Aperio / Grundium SVS-style
  4×/2×/2× chains transcoded through wsi-tools now read cleanly
  via generic-tiff when no vendor reader claims them.
- `formats/generictiff/classifier.go` gains
  `ClassifyAssociatedFromPage(*tiff.Page) Kind` wrapper for WSI-
  tag dispatch (existing `ClassifyAssociated` signature
  preserved).
- `formats/all/all.go` registers `cogwsi.Factory` AFTER `szi`
  and BEFORE `generictiff.Factory` (format-specific detector wins
  over generic catch-all).
- Fixture catch-ups (no reader-side regressions):
  - `scan_620_grundium_TIFF.tiff` geometry expectation flipped
    from buggy 3-level (v0.10 pin) to real 4-level. Surfaced by
    the integer-multiple ratio relaxation.
  - `Hamamatsu-1.ndpi` + `OS-2.ndpi` gained `scanner_serial`
    fields (stale since v0.17's NDPI metadata expansion).
  - `Leica-1.ome.tiff` + `Leica-2.ome.tiff` gained
    `acquisition_rfc3339` fields (stale since v0.17's OME-TIFF
    metadata expansion).

### Removed / retired

- R21 (general COG first-class support) — **fully retired**.
  COG-WSI shipped in v0.19 covers the WSI-context demand; general
  COG awareness is permanently YAGNI for opentile-go (we're WSI-
  domain, not geospatial). Generic COG files continue to read via
  `generic-tiff` as structurally-valid pyramid TIFFs.

### Notes

- **Spec-validation strictness:** COG-WSI files that fail
  conformance return `cogwsi.ErrNotConformantCOGWSI`. The spec is
  the contract; we don't bend.
- **Cross-format parity:** the writer (user's wsitools) and our
  reader agree on byte-passthrough semantics — confirmed by the
  cross-fixture parity gate across all 10 fixtures.
- **v0.18 SVS writer detection** carries through COG-WSI: when a
  COG-WSI file's source was Grundium SVS, `ScannerManufacturer`
  reports "Grundium" (via the preserved Make/Model TIFF tags).
- **Future `COG_WSI_VERSION` ≥ 0.2** rejects with
  `ErrNotConformantCOGWSI` until version-aware extensions ship.
  Defensive — better to fail loudly than silently misread.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.18.0] — 2026-05-09

SVS writer-vendor detection — closes a misattribution bug where
SVS files written by non-Aperio scanners (Grundium observed)
incorrectly reported `ScannerManufacturer = "Aperio"`. v0.18
detects the actual writer from ImageDescription first-line + TIFF
Software/Make tags; namespaces Properties keys per detected writer.

### Added

- `formats/svs/metadata.go::detectWriter()` — heuristic parser
  for the SVS ImageDescription first-line writer marker.
- "Recognized SVS writers" documentation in `docs/formats/svs.md`
  listing the supported writer first-line patterns + their
  detected vendor/model + Properties namespacing + status.
- "OME-XML writer attribution" documentation in
  `docs/formats/ometiff.md` clarifying the separation between
  `ome.creator` (writer) and `ScannerManufacturer` (scanner OEM).

### Fixed

- **SVS misattribution bug:** Grundium-written SVS files
  (scan_620_.svs, svs_40x_bigtiff.svs) now correctly report
  `ScannerManufacturer = "Grundium"`, `ScannerModel = "Ocus"`.
  Properties keys namespace under `grundium.<key>` instead of
  the misleading `aperio.<key>`.
- `ScannerSoftware` for SVS files no longer jams the first-line
  banner into a single string when the comma-suffix pattern
  is present; now sensibly split (e.g., `["Aperio Image", "Grundium Ocus"]`).

### Changed

- Fixture JSON parity files updated for Grundium SVS:
  `tests/fixtures/scan_620_.json` and `tests/fixtures/svs_40x_bigtiff.json`
  metadata.scanner_manufacturer flipped from "Aperio" → "Grundium".

### Notes

- **Narrow break:** consumer code hardcoding "Aperio" expectations
  on Grundium SVS files now sees correct attribution and may need
  updating. This is a bug fix; consumers should read
  `ScannerManufacturer` rather than assuming.
- **Standardized SVS keys** (MPP, AppMag, ScanScope ID, User,
  Date, Time) continue to populate cross-format Metadata regardless
  of writer.
- **Vendor-specific keys** namespace under the writer's lowercase
  first word: `aperio.<key>` for Aperio-written, `grundium.<key>`
  for Grundium-written, `svs.<key>` for undetected fallback.
- **Future writers** (3DHistech via SVS export; others) follow the
  same pattern automatically — the fallback namespace ensures
  parsing doesn't break for unrecognized writers.
- **OME-TIFF** writer attribution unchanged (was already correct);
  documentation extended.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.17.0] — 2026-05-09

Cross-format Metadata expansion — closes R20. Typed additions
(MicronsPerPixel + per-axis X/Y; ImageDescription) plus a flat
Properties map[string]string for opentile-go-canonical extensions
and vendor-namespaced passthrough. Mirrors OpenSlide's flat-
property convention where it's standard; falls back to typed
fields for the well-precedented WSI cross-cutting fields.

### Added

- 4 new typed fields on `opentile.Metadata`:
  - `MicronsPerPixel float64` (populated when X == Y; zero
    otherwise)
  - `MicronsPerPixelX float64`
  - `MicronsPerPixelY float64`
  - `ImageDescription string` (structured per-format description)
- `Properties map[string]string` for additional cross-format
  metadata. Two key conventions:
  - opentile-go-canonical (lowercase-with-hyphens): see new
    constants below
  - vendor-namespaced (`<format>.<key>`): vendor-specific fields
    surfaced as-is, e.g., `aperio.AppMag`, `philips.PIM_DP_*`,
    `ventana.<key>`, `hamamatsu.SourceLens`, `ome.creator`,
    `leica.barcode`, `iris.<key>`, `wsi-tools.codec`
- 5 canonical key constants:
  - `opentile.PropertyCaseNumber = "case-number"`
  - `opentile.PropertyUserName = "user-name"`
  - `opentile.PropertyScannedAreaMM2 = "scanned-area-mm2"`
  - `opentile.PropertyScanDurationSec = "scan-duration-seconds"`
  - `opentile.PropertyComments = "comments"`
- 2 helper methods:
  - `Metadata.SetMPPSymmetric()` — derives plain MPP from per-axis
    when X == Y (strict equality)
  - `Metadata.SetProperty(key, value string)` — nil-safe Properties
    setter (lazily initializes the map)
- New `tests/parity/cross_format_metadata_test.go` — cross-format
  metadata parity gate (12 fixtures across 9 formats; asserts the
  expected populated fields per probe-confirmed truth).

### Changed

- Every format reader (SVS, NDPI, Philips, OME-TIFF, BIF, IFE,
  Leica SCN, Generic TIFF, SZI) now populates the new typed fields
  + canonical Properties keys where source data is present.
- `leicascn.Tiler.Metadata()` now populates from SCN-XML view scale
  (was previously empty).
- Format-specific Metadata structs (`szi.Metadata`, `bif.Metadata`,
  `generictiff.Metadata`, `ife.Metadata`) lose cross-format-canonical
  duplicates per Q4 Option B; raw native representations preserved
  (e.g., SZI's `ElapsedTime` "XhYmZs" string + `VendorProperties`
  for SZI's spec-defined open-ended properties).
- Behavior change vs v0.16 SZI: anisotropic SZI now leaves
  `MicronsPerPixel = 0` (was averaging X / Y); per Q2 smart-MPP-only-
  when-X==Y. CMU-1.szi and Grundium fixture are both isotropic so
  observable behavior is unchanged on the wired fixtures.

### Notes

- **No break for existing consumers reading via Tiler.Metadata().**
  New fields default to zero values; existing typed fields
  (Magnification, ScannerManufacturer, etc.) unchanged.
- **Narrow break for struct-literal construction of format-specific
  Metadata structs.** E.g., `szi.Metadata{MicronsPerPixel: 0.4}` no
  longer compiles — set on the embedded opentile.Metadata instead
  (`szi.Metadata{Metadata: opentile.Metadata{MicronsPerPixel: 0.4}}`),
  or use `SetMPPSymmetric()` from the per-axis fields. Surface is
  narrow — mostly internal/test code.
- **Hybrid design rationale:** typed fields land at OpenSlide's
  precedent (MPP, comment); Properties map handles opentile-go
  originals (case-number, user-name, etc.). See spec §1 for the
  authority audit comparing OpenSlide / Python opentile.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.16.0] — 2026-05-09

Smart Zoom Image (SZI) support — closes R18. New formats/szi/
package backed by new shared internal/dzi/ core (DZI manifest
parser + tile-coordinate math, designed for additive bare-DZI
support in v0.17+). Driven by user's wsi-tools / viewer pipeline
targeting Grundium-scanner output.

### Added

- **`opentile.FormatSZI`** new enum value (`"szi"`).
- **`opentile.CompressionPNG`** new enum value (`"png"`). DZI's
  Format attribute admits both jpeg and png; opentile-go now
  accurately reports the codec on PNG-tiled SZI/DZI files.
- **`internal/dzi/`** new package: pure DZI manifest XML parser
  (`Manifest`, `ParseManifest`) + tile-coordinate math (`MaxLevel`,
  `LevelDims`, `GridDims`, `TilePath`). No I/O; designed to underpin
  multiple storage backends.
- **`formats/szi/`** new package: SZI Tiler with eager ZIP
  central-directory parse, mmap-aliased tile fetch via SectionReader
  on uncompressed-stored entries, full pyramid (Image / Level / Tile
  / TileInto / TileReader / TilePrefix / TileBodyInto), and
  associated images (`macro.jpg` → `Type() == "overview"`;
  `label.jpg`; `thumbnail.jpg`).
- **`szi.Metadata`** struct + **`szi.MetadataOf(t)`** accessor for
  format-specific scan-properties.xml fields including
  `VendorProperties map[string]string` for open-ended `vendor.<key>`
  custom properties (mirrors v0.6+/Philips/OME/IFE/SCN precedent).
- 2 new fixtures wired into TestSlideParity:
  - `CMU-1.szi` (1.5 MB, from smartinmedia/SZI-Format spec repo)
  - `scan_618_grundium_SZI.szi` (709 MB, Grundium-produced)
- TestSlideParity total: **30 fixtures** (was 28).

### Notes

- **Sparse SZI files are not supported** per the spec page 4
  (verbatim: *"sparse images and collections are not supported in
  the SZI format"*). A missing tile in the addressable grid
  returns a corrupt-archive error. Breadcrumbs left for a future
  additive `ErrTileMissing` sentinel + opt-in lenient mode if a
  sparse-SZI fixture surfaces.
- **Bare DZI** (filesystem-backed, no ZIP wrapper) is deferred to
  v0.17+ pending consumer signal. The `internal/dzi/` extraction
  pre-pares this without compromise to SZI.
- **DZC collections** (Morton-laid-out shared thumbnails) are
  permanently out of scope — multi-image; opentile-go reads
  single-WSI files only.
- Optional `vendor/` folder content is not surfaced through the
  public API in v0.16; deferred until consumer signal.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.15.0] — 2026-05-08

Naming-cleanup milestone — renames the `AssociatedImage.Kind()`
method to `Type()` (DICOM ImageType convention) and aligns every
format except Iris IFE on `"overview"` as the canonical name for
the wide-field slide image. Breaking change; pre-1.0; sole-consumer
sign-off granted.

### Breaking changes

- **`AssociatedImage.Kind()` renamed → `Type()`.** Every format
  reader's implementation, every test call site, and the public
  interface in `image.go` updated in lockstep.
- **`formats/generictiff` constants renamed:**
  `KindLabel` → `TypeLabel`, `KindMacro` → `TypeOverview`,
  `KindThumbnail` → `TypeThumbnail`, `KindAssociated` → `TypeAssociated`.
- **Generic-TIFF and Leica SCN emitted-value flip.** Pre-v0.15,
  these two readers emitted `Type() == "macro"` (drift introduced
  in v0.10 / v0.11). v0.15 flips them to `"overview"`, matching:
  - DICOM PS3.3 / Supplement 145 (Image Type 0008,0008 value 3 =
    `OVERVIEW`)
  - Upstream Python opentile (the project we directly port; uses
    `"overview"` everywhere, mapping native OME-XML `"macro"` to
    `"overview"` via the OME tiler)
  - opentile-go's own SVS / NDPI / Philips / OME-TIFF / BIF readers
    (which already emitted `"overview"` from v0.1 / v0.5 / v0.6 / v0.7)

  **Iris IFE is intentionally exempt** — the IFE spec defines
  `LABEL_MACRO` and `LABEL_OVERVIEW` as distinct kinds, and opentile-
  go's IFE reader preserves both.
- **README OME-TIFF row corrected.** Pre-v0.15 the row claimed the
  format emitted `"macro"`; in fact it emitted `"overview"` since
  v0.6. README was stale, now matches code.

### Consumer migration

```text
Method:
  a.Kind()                            → a.Type()

Constants (formats/generictiff):
  generictiff.KindLabel               → generictiff.TypeLabel
  generictiff.KindMacro               → generictiff.TypeOverview
  generictiff.KindThumbnail           → generictiff.TypeThumbnail
  generictiff.KindAssociated          → generictiff.TypeAssociated

Switch-statement values:
  case "macro":  // generic-TIFF      → case "overview":
  case "macro":  // Leica SCN         → case "overview":
  case "macro":  // Iris IFE          (UNCHANGED — IFE-spec-distinct)
  case "overview": // every other     (UNCHANGED)
```

### Notes

- v0.15 is rename-only. No format-support changes, no new fixtures,
  no perf changes, no behavioral changes for code that already used
  the right name.
- `TestSlideParity` 28 fixtures unchanged from v0.14.
- v1.0 cut still pending.
- cgo footprint unchanged.

## [0.14.0] — 2026-05-08

Novel-codec milestone — generic-TIFF reader recognises four new
tile compression tag values produced by the user's wsi-tools
transcoder (WebP, JPEG XL, AVIF, HTJ2K). Plus opportunistic parsing
of the wsi-tools ImageDescription format to populate standard
Metadata fields. Additive — no breaking changes.

### Added

- **3 new `opentile.Compression` enum values:**
  - `CompressionWebP` (TIFF tag 50001 — libtiff convention)
  - `CompressionJPEGXL` (TIFF tag 50002 — wsi-tools convention)
  - `CompressionHTJ2K` (TIFF tag 60003 — wsi-tools convention).
    Distinct from `CompressionJP2K` because HTJ2K's FBCOT entropy
    coder is incompatible with standard JP2K decoders.
- **5 new TIFF compression tag mappings** in
  `formats/generictiff/tiled.go::tiffCompressionToOpentile`:
  34712 (registered JP2K), 50001 (WebP), 50002 (JPEG XL),
  60001 (AVIF — uses existing `CompressionAVIF`), 60003 (HTJ2K).
- **Validator whitelist** (`internal/tiff.validCompression`)
  accepts the 5 new tag values.
- **wsi-tools ImageDescription parser**
  (`formats/generictiff/wsitools.go`). When the level-0
  ImageDescription starts with `wsi-tools/`, parses the structured
  key=value form to populate Magnification / ScannerManufacturer /
  AcquisitionDateTime / MicronsPerPixel. Lenient on missing /
  malformed values; forward-compatible with future wsi-tools fields
  (unknown keys ignored). Non-wsi-tools ImageDescriptions are
  unaffected.

### Changed

- 4 new test fixtures wired into TestSlideParity:
  - `avif-out.tiff` (AVIF, 2220×2967)
  - `htj2k-out.tiff` (HTJ2K, 2220×2967)
  - `jxl-out.tiff` (JPEG XL, 2220×2967)
  - `webp-out.tiff` (WebP, 2220×2967)
  TestSlideParity total: 28 fixtures (was 24).

### Notes

- **Byte-passthrough contract.** Per the v0.8 IFE precedent for
  AVIF and Iris-proprietary codecs, opentile-go reports each
  tile's Compression() value but doesn't decode. Consumers bring
  their own libwebp / libjxl / libavif / OpenJPEG-HTJ2K decoder.
- **Tag value mappings are wsi-tools-specific** for AVIF (60001)
  and HTJ2K (60003) — not formally registered TIFF codes. Files
  produced by other tooling using different tag values for these
  codecs would not be recognised.
- v0.14 introduced no new active limitations.
- v1.0 cut remains pending.
- cgo footprint unchanged.

## [0.13.0] — 2026-05-08

Bandwidth-deduplication milestone — exposes the JPEG splice prefix
and on-disk tile body bytes separately so client-server consumers
can ship the prefix once per level instead of redundantly on every
tile. Motivated by personal-viewer profiling work; v0.13 implements
the public API + bench harness.

### Added (additive — no breaking changes)

- **`opentile.Level.TilePrefix() []byte`** — returns the constant
  per-level JPEG splice prefix bytes; nil when no shared JPEGTables
  apply.
- **`opentile.Level.TileBodyInto(x, y int, dst []byte) (int, error)`**
  — reads on-disk tile bytes WITHOUT applying the splice. For
  non-splice levels, equivalent to TileInto.
- **`opentile.Level.TileBodyMaxSize() int`** — upper bound on
  TileBodyInto output size (strictly less than TileMaxSize() when
  splice prefix is non-nil; equal otherwise).
- **`opentile.SpliceJPEGTile(prefix, body []byte) ([]byte, error)`**
  — top-level helper that reconstitutes a complete JPEG from a
  level's TilePrefix + one tile's TileBodyInto output. Inserts the
  prefix at the on-disk tile's SOS boundary. Algorithm documented
  for non-Go consumers (web viewer JS reimplementation).
- **`opentile.ErrBadJPEGSplice`** — sentinel error for malformed
  SpliceJPEGTile inputs (empty body, missing SOS marker).
- **`tests/parity/tilebody_bench_test.go`** (build tag `benchgate`)
  — Pattern A vs Pattern B bandwidth comparison harness.

### Changed

- Per-format Level implementations specialized to expose the
  splice prefix where applicable: SVS (T2 — with APP14), Philips
  / OME tiled / leicascn / generictiff (T3 — no APP14), BIF (T4
  — mixed shared / per-tile-embedded). NDPI / IFE / OneFrame
  levels stay at the v0.13 T1 no-op defaults (TilePrefix nil,
  TileBodyInto delegates to TileInto).

### Notes

- **Savings depend on fixture-author choice.** Slides with shared
  JPEGTables (tag 347) get bandwidth deduplication via Pattern B;
  slides with per-tile-embedded JPEGTables (e.g., Ventana-1 BIF,
  this Leica OME/SCN fixture) get 0% Pattern-B savings — Pattern A
  remains correct for those. Honest documentation in `docs/perf.md`.
- **Bench results from T6** (L0 full-walk):
  - CMU-1.svs: 4.3% savings (23,220 tiles, 301B prefix)
  - Philips-1.tiff: 1.5% savings (6,160 tiles, 570B prefix)
  - Leica-1.ome.tiff / Leica-1.scn: 0% (this fixture has no shared tables)
- v0.13 introduced no breaking changes; existing consumers using
  Tile / TileInto see no behavior change.
- v1.0 cut remains pending (sealed Q1 in v0.12). The interface
  evolution adds 3 methods; pre-v1.0 territory still allows
  additions without ceremony.
- TileBorrow zero-copy mmap aliasing remains parked (different
  axis from bandwidth deduplication).

## [0.12.0] — 2026-05-07

Naming-cleanup milestone — breaking-API rename pass consolidating
four deferred items from `docs/deferred.md §11`. No new format
support, no new features, no API additions. The renames pre-pay
the eventual v1.0 naming-cleanliness cost without committing to
v1.0 (per sealed Q1).

### Breaking changes

#### Format constants

| v0.11 | v0.12 | String value |
|---|---|---|
| `opentile.FormatPhilips` | `opentile.FormatPhilipsTIFF` | `"philips"` → `"philips-tiff"` |
| `opentile.FormatOME` | `opentile.FormatOMETIFF` | `"ome"` → `"ome-tiff"` |

Callers comparing against the old string values or identifiers
must update. Mirrors v0.10 / v0.11's `FormatGenericTIFF` /
`FormatLeicaSCN` naming convention. Philips has multiple file
formats (TIFF; iSyntax); OME has multiple file formats (OME-TIFF,
OME-Zarr, OME-NGFF). The bare names were ambiguous.

#### Package import paths

| v0.11 | v0.12 |
|---|---|
| `github.com/wsilabs/opentile-go/formats/philips` | `github.com/wsilabs/opentile-go/formats/philipstiff` |
| `github.com/wsilabs/opentile-go/formats/ome` | `github.com/wsilabs/opentile-go/formats/ometiff` |

The package qualifier follows: `philips.MetadataOf` →
`philipstiff.MetadataOf`; `ome.MetadataOf` → `ometiff.MetadataOf`.

#### NDPI public API

| v0.11 | v0.12 |
|---|---|
| `formats/ndpi.StripeInfo` | `formats/ndpi.StripInfo` |
| `StripeInfo.StripeOffsets` | `StripInfo.StripOffsets` |
| `StripeInfo.StripeByteCounts` | `StripInfo.StripByteCounts` |
| `StripeInfo.StripeW`, `StripeH` | `StripInfo.StripW`, `StripH` |
| `StripeInfo.StripedW`, `StripedH` | `StripInfo.GridW`, `GridH` |

TIFF spec uses bare singular "Strip" (tags 273 `StripOffsets`,
279 `StripByteCounts`); the v0.2 NDPI work used "stripe"
inconsistently. v0.12 renames to the spec-faithful form. The
strip-grid count fields (formerly `StripedW` / `StripedH`) are
renamed to `GridW` / `GridH` to mirror our existing `Level.Grid()`
API and avoid the awkward "Stripped width" reading.

### Changed

- File renames preserving git history:
  - `formats/ndpi/striped.go` → `stripped.go`
  - `formats/ndpi/striped_test.go` → `stripped_test.go`
  - `formats/ndpi/stripes.go` → `strips.go`
  - `formats/philips/*` → `formats/philipstiff/*`
  - `formats/ome/*` → `formats/ometiff/*`
  - `docs/formats/philips.md` → `philipstiff.md`
  - `docs/formats/ome.md` → `ometiff.md`
- Test fixture format strings: `Philips-{1..4}.tiff.json` and
  `Leica-{1,2}.ome.tiff.json` updated to record the new format
  strings.
- `docs/deferred.md` §8f new (v0.12 retirement audit); §11
  backlog rows for "Fix striped → stripped" and "Naming
  corrections" removed.

### Notes

- Public API is more consistent post-rename: every Format constant
  for vendor-disambiguated formats now follows
  `Format<Vendor><Tag>` (FormatGenericTIFF, FormatLeicaSCN,
  FormatPhilipsTIFF, FormatOMETIFF) plus unambiguous-vendor short
  forms (FormatSVS, FormatNDPI, FormatBIF, FormatIFE).
- v1.0 cut not committed (sealed Q1). v0.12 stays in pre-1.0
  territory.
- No new active limitations.
- cgo footprint unchanged.

## [0.11.0] — 2026-05-06

Leica SCN milestone — first format reader exercising `Image.SizeC() > 1`
on a real fixture (Leica-Fluorescence-1.scn's 3-channel separated
fluorescence data), and first multi-region "discontinuous scanning"
reader (Leica-2.scn's 4 disjoint tissue rectangles composited into one
slide canvas). Folded in: two `formats/generictiff` validator-cap
relaxations covering real Grundium scanner output (single-level tiled
TIFFs and mixed-ratio pyramid chains).

### Added

- **`formats/leicascn` package** — Factory + Detection + Tiler +
  Level + AssociatedImage covering all 3 openslide-testdata SCN
  fixtures. SCN is a BigTIFF dialect produced by Leica SCN400 /
  SCN400F scanners; production discontinued ~2015.
- **`opentile.FormatLeicaSCN = "leica-scn"`** — new Format constant.
- **SCN XML schema parser** (`formats/leicascn/scnxml.go`). Parses
  `<scn>/<collection>/<image>` mapping IFD indices to logical
  (image, level, channel) tuples. Hand-rolled walker over
  `xml.Decoder` tokens; mirrors `internal/bifxml`'s lenient style.
- **Multi-region composite Level** (`formats/leicascn/tiled.go`).
  Composites N main scans into one Image canvas with per-tile
  dispatch + cached blank-tile fill for inter-region gaps. Sealed
  Q4 + Q6: consumer never sees the discontinuous-scanning detail.
- **Multi-channel `TileAt`** support via the v0.7 multi-dim API.
  `Level.TileAt(TileCoord{C: c, X: x, Y: y})` reads from the per-
  channel IFD; `Image.SizeC()` + `Image.ChannelName(c)` populated
  from SCN's `<channelSettings>`.
- **`leicascn.Metadata` + `leicascn.MetadataOf`** — format-specific
  metadata: CollectionUUID, Barcode, per-Auxiliary illumination +
  objective, per-Region (main scan) slide-physical layout, per-
  Channel fluorescence filter / exposure / CCD-gain.
- **Bio-formats CLI parity oracle** (`tests/oracle/leicascn_bf_test.go`,
  build tag `bfparity`). Per sealed Q9: structural-equivalence parity
  (series count + per-series dims), NOT byte-equality (bio-formats
  decodes + re-encodes differently from our raw passthrough).
- **`docs/formats/leicascn.md`** — new format-doc page mirroring the
  bif.md / generictiff.md template.

### Changed

- **`internal/tiff.DefaultClassifyPyramidConfig`** relaxed (R1 + R2):
  `MinLevels: 3 → 1` (admits Grundium scan_619 single-level tiled
  TIFFs); `LeftoverTiledMaxAreaRatio: 0.01 → 0.05` (admits Grundium
  scan_620 mixed-ratio chains where the orphan IFD is 1.56% of
  baseline). Both cap-loosenings; v0.10 fixtures classify identically.
- **`docs/deferred.md`** — new §1a deviation entry for the SCN reader;
  new §2 L30-L34 active limitations; new §8e v0.11 retirement audit;
  §11 consolidated backlog extended.
- **README** — Supported-formats table gains a Leica SCN row;
  Format() example string lists `"leica-scn"`; Deviations table
  gets a v0.11 row.

### Deviations from upstream (additive)

One new v0.11 entry in `docs/deferred.md §1a`:

- Leica SCN reader for legacy SCN400 / SCN400F output.

### Test coverage

- `tests/integration_test.go::TestSlideParity` extended to **24
  fixtures** (was 19 post-v0.10): +2 Grundium + 3 SCN. Sample-tile
  SHA fixtures committed for all 5.
- `tests/parity/leicascn_geometry_test.go` — per-fixture geometry
  pinning + per-channel TileAt distinct-bytes check (3 distinct
  channel hashes on Fluorescence) + cross-backing parity (mmap
  default vs pread). Composite L0 union extent for Leica-2 pinned
  at 44956×139277 px.
- `tests/parity/generic_geometry_test.go` extended with rows for
  the two Grundium fixtures.
- `formats/leicascn/*_test.go` — unit tests for parser, classifier,
  composer, factory, AssociatedImage, tiledRegion, compositeLevel,
  blank-tile, Tiler.

### Active limitations

Five new L items, all design-Q-decisions sealed in the v0.11 spec
(see `docs/deferred.md` §2):

- **L30** — SCN multi-Z stack support deferred (no fixture in slate;
  XML schema supports it).
- **L31** — SCN AOI-cropped Tile variant deferred (YAGNI; consumers
  composite via `Metadata.Regions`).
- **L32** — SCN regions with mismatched objective / illumination /
  pyramid depth rejected via `ErrUnsupportedSCN` (fixture-driven).
- **L33** — SCN byte-equality oracle vs bio-formats not feasible
  (permanent; decode + re-encode divergence).
- **L34** — SCN 3-fixture coverage limit (permanent; production
  discontinued ~2015).

### Notes

- v0.11 retired no §2 L items — the milestone adds reader coverage
  rather than closing deferred work.
- Public API remains stable from v0.3: two new exported names
  (`opentile.FormatLeicaSCN`, the `leicascn` package). The v0.7
  multi-dim API is reused without additions. cgo footprint
  unchanged at `internal/jpegturbo/`.
- **Multi-region tile-alignment lesson**: SCN's `<view offsetX/Y>`
  values in nm don't generally tile-align in composite-pixel-space.
  Resolution: tile-snap region offsets DOWN to nearest tile boundary
  at construction. Cost: composite position error ≤ one tile (~128 µm
  at 250 nm/px) — pathology-rendering-acceptable. Surfaced during T8
  implementation; documented in `docs/formats/leicascn.md` and §8e of
  `docs/deferred.md`.
- **Generictiff scan_620 spec divergence**: v0.11 spec said the orphan
  IFD "surfaces as an AssociatedImage". In practice the orphan is
  tiled, and `formats/generictiff`'s associated reader doesn't handle
  tiled associated IFDs (silently dropped per the v0.10 §6 pattern).
  Documented in `docs/formats/generictiff.md`; multi-tile-associated
  remains out-of-scope until a fixture motivates implementation.

## [0.10.0] — 2026-05-05

Generic-TIFF milestone — first catch-all reader for tiled pyramidal
TIFFs without vendor metadata. Fills the gap upstream Python opentile
leaves (its factory list is enumerated vendor formats only). Activates
on any TIFF whose IFD layout passes the v0.10 pyramid validator and
whose IFDs sit on the photometric/sample/compression whitelist. Real-
world WSI authoring outside Aperio / Hamamatsu / Philips is common
(Grundium, Roche legacy iScan, vendor-stripped derivatives, libtiff-
encoded research outputs); a catch-all reader makes opentile-go consume
any structurally valid pyramid TIFF.

### Added

- **`formats/generictiff` package** — Factory + Detection + Tiler +
  Level + AssociatedImage. Registered LAST in the dispatch order
  so vendor format detectors (SVS, NDPI, Philips, OME, BIF) get
  first crack at any TIFF.
- **`opentile.FormatGenericTIFF = "generic-tiff"`** — new Format
  constant returned by the generic Tiler.
- **`opentile.CompressionDeflate`** — new Compression enum value
  for the Deflate (8) / Adobe Deflate (32946) compression types
  the generic reader accepts.
- **`internal/tiff.ClassifyPyramid`** — value-in / value-out
  pyramid validator. Greedy-chain algorithm with ±2% inter-axis
  + ±5% inter-level scale tolerance; multi-pyramid rejection via
  leftover count + area threshold. Exported for reuse by other
  format readers if needed.
- **`internal/tiff.PyramidLevelInfo`** + **`PyramidLevelInfoFromPage`** —
  the small subset of TIFF tags `ClassifyPyramid` needs from each
  IFD, plus a projection helper from `*tiff.Page`.
- **`generictiff.ClassifyAssociated`** — heuristic kind classifier for
  non-pyramid IFDs. LZW = label, wide-aspect JPEG = macro, smaller-
  square JPEG = thumbnail; fallback `KindAssociated`.
- **`generictiff.Metadata`** + **`generictiff.MetadataOf(opentile.Tiler)`** —
  format-specific metadata via the established pattern (mirrors
  `svs.MetadataOf` / `bif.MetadataOf`). `MicronsPerPixel` derived
  from `XResolution` (282) + `ResolutionUnit` (296);
  `ImageDescription` (270) verbatim.
- **`"associated"` AssociatedImage Kind value** — new fallback in
  the `AssociatedImage.Kind()` taxonomy used only by the generic
  reader. Documented in `image.go`'s interface docstring; existing
  vendor format readers continue using `"label"`, `"overview"`,
  `"thumbnail"`, `"macro"`, `"map"`, `"probability"`.
- **Multi-strip associated reader paths** — single-strip
  passthrough; multi-strip uncompressed concat; multi-strip JPEG
  concat (libtiff RST-marker layout); multi-strip LZW decode +
  re-encode (lifted from `formats/svs/lzwlabel.go` pattern).
  Multi-strip Deflate and tiled associated images silently
  dropped per spec §6 — IFD recognised but not exposed.
- **Cross-format `Tiler.Metadata()` via standard TIFF tags** —
  `Make` (271) → ScannerManufacturer; `Model` (272) → ScannerModel;
  `Software` (305) → ScannerSoftware (semicolon/newline-split);
  `DateTime` (306) → AcquisitionDateTime ("YYYY:MM:DD HH:MM:SS").
- **`docs/formats/generictiff.md`** — new format-doc page mirroring
  the bif.md / ife.md template.
- **`scripts/regen-generic-tiff.py`** — Python tifffile-based
  generator producing `CMU-1.stripped.tiff` (multi-level stripped-
  associated derivative of CMU-1.svs) and 4 synthetic test
  fixtures. Re-run when validator thresholds change.

### Changed

- **`opentile.OpenFile` now routes pyramidal TIFFs without vendor
  metadata to the generic reader.** Vendor detection order is
  unchanged (vendor factories run first); the generic factory
  activates only when no vendor factory claims the file.
- **`docs/deferred.md`** — new §1a deviation entries for the
  generic-TIFF reader and the `"associated"` Kind value; new §2
  L26-L29 active limitations; new §8d v0.10 retirement audit;
  §11 consolidated backlog extended.
- **README** — Supported-formats table gains a Generic TIFF row;
  detection paragraph mentions the catch-all dispatch ordering;
  Deviations table gets two new v0.10 rows.

### Deviations from upstream (additive)

Two new v0.10 entries in `docs/deferred.md §1a`:

- Generic-TIFF reader for non-vendor tiled pyramidal TIFFs.
- `"associated"` AssociatedImage Kind value addition.

### Test coverage

- `tests/integration_test.go::TestSlideParity` extended to 19
  fixtures (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE +
  2 generic). Full-walk SHA fixtures committed for both new
  generic files.
- `tests/parity/generic_geometry_test.go` — per-fixture geometry
  pinning + cross-backing byte parity (mmap default vs pread).
  Mirrors the existing `bif_geometry_test.go` / `ife_geometry_
  test.go` pattern.
- `formats/generictiff/*_test.go` — unit tests for the validator,
  classifier, factory, tiledImage Level, associatedImage
  AssociatedImage, and tiler. Real-fixture coverage on
  `CMU-1.tiff` + `CMU-1.stripped.tiff` (T2-generated derivative
  of CMU-1.svs).

### Active limitations

Four new L items, all design-Q-decisions sealed in the v0.10 spec
(see `docs/deferred.md` §2):

- **L26** — stripped pyramid IFDs deferred (fixture-driven; v0.11
  candidate).
- **L27** — multi-pyramid TIFFs reject as out-of-scope (permanent;
  OME's job).
- **L28** — multi-strip JPEG with `PlanarConfiguration=2`
  unsupported (permanent; OME-specific).
- **L29** — pluggable associated-image classifier deferred
  (YAGNI; first consumer ask).

Two additional v0.11 candidates surfaced mid-stream from real
Grundium fixtures (single-level tiled TIFFs and mixed-ratio
pyramid chains); both fixtures parked under
`sample_files/generic-tiff/` for the v0.11 investigation.

### Notes

- v0.10 retired no §2 L items — the milestone adds reader
  coverage rather than closing deferred work.
- Public API remains stable from v0.3: three new exported names
  (`opentile.FormatGenericTIFF`, `opentile.CompressionDeflate`, the
  `generic` package itself) and one new Kind value
  (`"associated"`). cgo footprint unchanged at
  `internal/jpegturbo/`.
- The validator's pyramid-classification logic is value-typed
  (PyramidLevelInfo in / ClassifyPyramidResult out); future
  formats can reuse it without coupling to `*tiff.Page`.

## [0.9.0] — 2026-05-01

Sole-focus performance milestone implementing the §A
recommendations from `docs/opentile-go-svs-perf.md` (the
project-internal SVS perf doc dropped 2026-05-01). Every TIFF
format and Iris IFE now ships with **memory-mapped tile reads**
and a **pool-friendly `TileInto` API** that achieves zero
allocations per tile on the hot path. Cumulative speedups range
from 8× (BIF) to 145× (Cervix IFE) for high-RPS callers; NDPI is
unchanged (CPU-bound libjpeg-turbo transcoding, not I/O).

### Added

- **`opentile.OpenFile` is mmap-backed by default.** Memory-maps
  the slide file via `golang.org/x/exp/mmap` (cross-platform
  Linux + macOS + Windows). Tile reads become userspace memcpy
  from the mapped region; no `pread(2)` syscall per call. The
  kernel page-fault handler brings tile data into the page cache
  lazily on first access; warm-cache reads hit RAM at memory-
  bandwidth speed.
- **`opentile.WithBacking(Backing)` Option** — `BackingMmap`
  (default) and `BackingPread` (the v0.8-and-earlier os.File +
  pread path). Use `WithBacking(BackingPread)` on filesystems that
  don't support mmap, or when you specifically need os.File
  truncation semantics.
- **`opentile.ErrMmapUnavailable`** — sentinel returned when
  `WithBacking(BackingMmap)` is in effect (default) but the
  underlying mmap call fails. Auto-fallback callers retry with
  `WithBacking(BackingPread)`.
- **`Level.TileInto(x, y int, dst []byte) (int, error)`** —
  pool-friendly tile-read API. Writes tile bytes directly into
  the caller's buffer with zero internal allocation on every TIFF
  format and IFE. Returns `io.ErrShortBuffer` if `len(dst) <
  Level.TileMaxSize()`. NDPI / OME OneFrame still allocate their
  internal scratch (per-page assembled frame), but the boundary-
  level allocation is eliminated.
- **`Level.TileMaxSize() int`** — cached upper bound for `Tile()`
  / `TileInto()` output. Sized for `len(largest_tile) + splice
  overhead`; callers use this to size sync.Pool buckets.
- **`Tiler.WarmLevel(i int) error`** — page-cache pre-warm hook.
  Touches one byte per OS page covering level i's tile-data
  ranges. Under mmap: forces kernel readahead. Under pread:
  pread(1) per page (slower; documented as best-effort). Returns
  `ErrLevelOutOfRange` on invalid i.
- **`internal/jpeg.BuildSplicePrefix`** + **`internal/jpeg.InsertPrefixInPlace`**
  — in-place JPEG splice template. Caches the per-level prefix
  (DQT + DHT + optional Adobe APP14) at level open; per-tile
  splice on `TileInto` is now zero-alloc. Byte-identical output
  to the legacy `InsertTables` / `InsertTablesAndAPP14`.
- **`tests/parity/perf_baseline_test.go`** — benchgate-tagged
  baseline harness that captures per-format `Tile()` RPS +
  allocations + MB/s (via `b.SetBytes`) across the parity slate.
  Four committed snapshots (`tests/fixtures/v0.9-{baseline,after-mmap,
  after-tileinto,after-splice}.txt`) document the cumulative
  optimization journey.
- **`docs/perf.md`** — performance-characteristics guide for
  high-RPS / desktop / pipeline consumers.
- **`golang.org/x/exp/mmap`** dependency. Cross-platform mmap;
  Go-team subrepo, single file, frozen API.

### Changed

- **`OpenFile` defaults to mmap-backed I/O.** Existing callers see
  the perf win automatically. SIGBUS on file truncation is the
  documented failure mode under the new default; opt out via
  `WithBacking(BackingPread)` if your storage allows truncation.
- **`Tiler` interface gains `WarmLevel(i)`** — additive.
- **`Level` interface gains `TileInto` + `TileMaxSize`** —
  additive. Existing `Tile()` is now a thin wrapper that
  allocates `TileMaxSize()` bytes and calls `TileInto` on no-
  splice formats; on splice formats `Tile()` retains the legacy
  alloc-per-call path for backward compat.
- **Concurrency-contract docs** on Tiler / Level updated to pin
  per-format lock characteristics, dst ownership rules for
  TileInto, mmap-aliasing semantics, and the
  SIGBUS-on-truncation contract.

### Deviations from upstream (additive)

Three new v0.9 entries in `docs/deferred.md §1a`:

- Default mmap-backed `OpenFile`.
- Pool-friendly `TileInto` + `TileMaxSize` API addition.
- `Tiler.WarmLevel(i)` page-cache pre-warm hook.

### Performance results

Pool-aware `TileInto` (the v0.9 hot-path API) on Apple M4
darwin/arm64 vs the v0.8 `Tile()` baseline:

| Fixture | v0.8 Tile() ns | v0.9 pool TileInto ns | Speedup | Allocs |
|---|---:|---:|---:|---:|
| Cervix IFE | 22,065 | 152 | 145× | 0 |
| Leica OME | 4,286 | 376 | 11× | 0 |
| **CMU-1.svs** | **1,583** | **99.7** | **16×** | **0** |
| **Philips-1** | **6,473** | **425** | **15×** | **0** |
| Ventana-1.bif | 26,003 | 3,225 | 8× | 0 |
| CMU-1.ndpi (par) | 182k | 185k | ~same | 4 |

NDPI is unchanged because the bottleneck is libjpeg-turbo's
DCT-domain crop work, not I/O — mmap doesn't help; pool doesn't
help. For high-RPS NDPI serving, a consumer-side LRU cache on the
spliced JPEG bytes is the right answer.

### Notes

- The plan deferred A.3 (in-place JPEG splice template) at T7
  per a CPU% gate (combined splice cost ~2.5%, below the 5%
  threshold). Reversed at owner review after a bytes/ns analysis
  showed the splice path running at 6× worse throughput-per-byte
  vs no-splice formats — alloc churn matters for tail latency
  under sustained load even when CPU% is modest. Lesson recorded
  in `docs/deferred.md §10a` for future profile-driven gates.
- v0.9 ships with no Python parity oracle changes — every existing
  fixture is byte-identical across both backings (verified by
  `TestOpenFileBackingsByteIdentical`) and across `Tile()` /
  `TileInto()` (verified by `tests/parity/tileinto_test.go`).
- Baseline benchmark JSON (text) snapshots committed for the v0.9
  retirement audit:
  - `tests/fixtures/v0.9-baseline.txt` — pre-mmap (v0.8 numbers)
  - `tests/fixtures/v0.9-after-mmap.txt` — after A.1
  - `tests/fixtures/v0.9-after-tileinto.txt` — after A.2 + pool
  - `tests/fixtures/v0.9-after-splice.txt` — after A.3 in-place splice

- **R4 / R9** ([#1](https://github.com/wsilabs/opentile-go/issues/1)) —
  SVS corrupt-edge reconstruct + JP2K decode/encode. No local SVS slide
  exhibits the corrupt-edge bug; work parked until one motivates it.
- **R6** ([#2](https://github.com/wsilabs/opentile-go/issues/2)) —
  3DHistech TIFF. Niche MRXS conversion target; never encountered in
  the wild. Trigger-driven park.
- **R15** ([#3](https://github.com/wsilabs/opentile-go/issues/3)) —
  Sakura SVSlide. Trigger-driven park.

## [0.8.0] — 2026-05-01

Iris File Extension (IFE) v1.0 support — **the first non-TIFF format
opentile-go reads**, and the first format opentile-go ships with no
Python or external-binary parity oracle. One real fixture
(`cervix_2x_jpeg.iris`, 2.16 GB, JPEG-encoded) round-trips through
`opentile.OpenFile` cleanly: 9 levels native-first, 256×256 tiles,
JPEG SOI markers on every decoded tile.

The plumbing refactor (`FormatFactory.SupportsRaw` + `OpenRaw` +
`RawUnsupported` base) ships alongside; the existing five TIFF
factories embed `RawUnsupported` for backward-compat zero-cost.

### Added

- **Iris IFE format** — `formats/ife/`. Magic-byte sniff
  (`0x49726973` LE) via `Factory.SupportsRaw` runs *before*
  `tiff.Open` in the dispatch loop. FILE_HEADER (38 B) → TILE_TABLE
  (44 B) → LAYER_EXTENTS (16-B header + 12-B entries) →
  TILE_OFFSETS (16-B header + 8-B entries with 40+24-bit
  offset/size encoding) parsing in pure Go via stdlib
  `encoding/binary`. Layer ordering inverted at parse time
  (file is coarsest-first; API exposes native-first). Tile bytes
  are self-contained — no JPEGTables splice, distinct from
  SVS/BIF abbreviated-scan pattern.
- **`FormatFactory.SupportsRaw(io.ReaderAt, int64) bool`** +
  **`FormatFactory.OpenRaw(r, size, *Config) (Tiler, error)`** —
  additive interface evolution for non-TIFF dispatch.
  `RawUnsupported` zero-impl base struct provides
  `SupportsRaw → false` / `OpenRaw → ErrUnsupportedFormat`
  defaults; the existing five TIFF factories embed it. Dispatch
  reorder in `opentile.Open` walks `SupportsRaw` first, then
  falls through to the TIFF path.
- **`opentile.FormatIFE`** constant.
- **`opentile.CompressionAVIF`** + **`opentile.CompressionIRIS`**
  enum values. `CompressionAVIF`: tile bytes are an AVIF image;
  consumer decodes via libavif. `CompressionIRIS`: Iris-proprietary
  codec; opentile-go reports but does not decode (consumer embeds
  an Iris codec or 501s).
- **`opentile.ErrSparseTile`** sentinel (wrapped in `TileError`).
  Returned when a tile position falls within the level grid but
  the format encodes "absent" via a sentinel offset (IFE's
  `NULL_TILE = 0xFFFFFFFFFF` in the 40-bit offset field).
- **`tests/parity/ife_geometry_test.go`** — per-fixture geometry
  pinning (no build tag, runs in `make test`).
- **`tests/fixtures/cervix_2x_jpeg.ife.json`** — sampled-tile SHA
  fixture. `TestSlideParity` now passes 17/17 slides
  (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE).
- **Synthetic-IFE-writer test harness** (`formats/ife/synthetic_test.go`
  + `formats/ife/metadata_test.go`) — hand-rolled IFE byte buffers
  cover layer inversion, sparse tiles, IRIS / AVIF encoding mappings,
  iterator order, open-time error paths, and full METADATA round-trip
  with attributes / images / ICC, without depending on the real fixture.
- **IFE METADATA block parsing** — full reader for METADATA +
  ATTRIBUTES + IMAGE_ARRAY + ICC_PROFILE (skips ANNOTATIONS for
  v0.9+). `Tiler.Metadata()` populates `Magnification` from the
  header f32; `Tiler.ICCProfile()` returns the embedded color
  profile bytes; `Tiler.Associated()` exposes IMAGE_ARRAY entries
  with normalised `Kind()` ("label" / "overview" / "thumbnail" /
  "macro" / "map" / "probability"; unknown titles surface
  lowercased). New `ife.Metadata` struct + `ife.MetadataOf(tiler)`
  accessor for IFE-specific fields: `MicronsPerPixel`,
  `MagnificationFromHeader`, `CodecMajor/Minor/Build`,
  `AttributesFormat`, `AttributesVersion`, and the free-form
  `Attributes map[string]string`. Cervix surfaces 24 attributes
  (every `aperio.*` / `tiff.*` key its source GT450 SVS carried
  before the Iris re-encode) + a 6064-byte ICC profile + a
  1920×1337 JPEG thumbnail.

### Changed

- **`opentile.Open`** dispatch order: `SupportsRaw` walked before
  `tiff.Open`. Backward-compat verified — every existing fixture
  still routes through the TIFF path because every TIFF factory's
  `SupportsRaw` (via `RawUnsupported`) returns false.
- **`FormatFactory` interface** gains two methods (additive). Format
  packages whose files are TIFF-based embed `RawUnsupported`;
  non-TIFF packages override both methods.

### Deviations from upstream

Two new deliberate divergences (see
[`docs/deferred.md` §1a](docs/deferred.md) for full reasoning):

- **Non-TIFF dispatch path** — `FormatFactory.SupportsRaw` +
  `OpenRaw` + `RawUnsupported`. Backward-compatible via embedded
  defaults. The first table-driven non-TIFF dispatch in the
  module.
- **`TILE_TABLE.x_extent` / `y_extent` ignored** for level
  dimensions on IFE. Spec doc claims these are "image
  width/height in pixels at top resolution layer," but the
  cervix fixture stores tile counts (matching `LAYER_EXTENTS.x_tiles`).
  Reader derives image dims from `LAYER_EXTENTS × 256` instead.

### Deferred (v0.9+)

- **L23** — Cross-tool parity vs `tile_server_iris` HTTP output.
  v0.8 ships sample-tile SHA fixtures + synthetic-writer + per-
  fixture geometry as the correctness bar. Cross-language byte-
  equality oracle is a future follow-up.
- **L24** — AVIF + Iris-proprietary tile decode is consumer's
  responsibility (Permanent — design choice). opentile-go reports
  `CompressionAVIF` / `CompressionIRIS` so consumers know the
  codec; linking libavif or an Iris codec would expand the cgo
  footprint past `internal/jpegturbo/` and break the byte-
  passthrough contract.
- **L25** — IFE ANNOTATIONS block parsing. v0.8 validates the
  offset is in-bounds but doesn't parse contents. Cervix has
  `annotations_offset == NULL_OFFSET`, so this is fixture-driven —
  resolved when a real annotated IFE surfaces.

### Notes

- IFE has **no Python parity oracle**. v0.7's tifffile + opentile-py
  oracles can't read IFE; openslide doesn't either. Coverage is
  layered: sample-tile SHAs lock in opentile-go's own output across
  regressions; synthetic-writer tests catch reader bugs without
  depending on a real fixture; per-fixture geometry pinning catches
  dimension regressions. The first cross-tool divergence story we
  hit will be debugged from scratch — acceptable risk for a
  bleeding-edge format.
- The plumbing refactor is the second additive interface evolution
  in two milestones (v0.7 added `Level.TileOverlap` + `TileAt` +
  `Image.Size{Z,C,T}`; v0.8 adds `FormatFactory.SupportsRaw` +
  `OpenRaw`). Both paid for themselves — TileOverlap is non-zero
  on BIF level 0; SupportsRaw is what makes IFE possible.

## [0.7.0] — 2026-04-28

Ventana BIF (Roche / iScan) support — the first opentile-go format
beyond upstream Python opentile's coverage. Two real fixtures
(`Ventana-1.bif` spec-compliant DP 200 + `OS-1.bif` legacy iScan
Coreo) round-trip through `opentile.OpenFile` cleanly. Correctness
is anchored on **tifffile byte-equality** for the spec-compliant
path + **committed sample-tile SHA256 hashes** for both fixtures via
`TestSlideParity`.

### Added

- **Ventana BIF format** — `formats/bif/`. BigTIFF detection via
  `<iScan` substring match in any IFD's XMP. Generation
  classification by `strings.HasPrefix(scannerModel, "VENTANA DP")`
  (DP 200, DP 600, future DP scanners → spec-compliant path; else
  → legacy-iScan path). IFD classification by `ImageDescription`
  content. Pyramid levels sorted by parsed `level=N`. Per-tile
  serpentine remap (image-space (col, row) → physical-stage
  TileOffsets index). Empty-tile path returns a cached blank JPEG
  filled with `<iScan>/@ScanWhitePoint` luminance (default 255 when
  the attribute is absent). Shared JPEGTables (tag 347) spliced via
  `internal/jpeg.InsertTables` (no Adobe APP14 — BIF is YCbCr).
- **`internal/bifxml/`** — stdlib `encoding/xml` walkers for
  `<iScan>` and `<EncodeInfo>` XMP blocks. Lenient parsing; ordinal
  `<AOI<N>>` iteration; out-of-range `ScanWhitePoint` clamped;
  `<EncodeInfo>` Ver < 2 rejected per spec.
- **`Level.TileOverlap() image.Point`** interface method (additive).
  Returns the per-tile-step pixel overlap; non-zero only on BIF
  level 0. Both real fixtures carry non-zero overlap on level 0
  (Ventana-1=(2,0); OS-1=(18,26)) — contrary to the original v0.7
  design spec §10's "fixture-untested" claim. Other formats return
  `image.Point{}`.
- **`bif.MetadataOf(opentile.Tiler) (*Metadata, bool)`** — exposes
  Generation, ScanRes, ScanWhitePoint+Present, ZLayers,
  ImageDescription, AOIs, AOIOrigins, EncodeInfoVer. Walks
  `UnwrapTiler` chains.
- **`opentile.FormatBIF`** constant.
- **`internal/tiff.TagXMP`** (700) + `Page.XMP()`,
  **`TagImageDepth`** (32997) + `Page.ImageDepth()`,
  **`TagDateTime`** (306).
- **AssociatedImage `kind="probability"`** — new kind value joining
  the existing taxonomy. Spec-compliant DP 200 fixtures expose IFD 1
  as the LZW-compressed tissue probability map.
- **`formats/bif/blanktile.go`** — cached JPEG blank-tile generator.
- **Three parity oracles**: `tests/parity/bif_geometry_test.go` (no
  build tag, runs in `make test`); `TestTifffileParityBIF`
  (Ventana-1, byte-equality); `TestOpenslideBIFParity`
  (infrastructure-only in v0.7, `t.Skip`'d for v0.8 follow-up).
- Sampled-tile fixtures for both BIF fixtures. `TestSlideParity` now
  passes 16/16 slides (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF).
- **Multi-dimensional addressing** —
  `Level.TileAt(TileCoord{X, Y, Z, C, T})` plus
  `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus`. Additive;
  2D formats inherit `SingleImage` defaults (`SizeZ/SizeC/SizeT == 1`)
  and `Tile(x, y) == TileAt(TileCoord{X: x, Y: y})` byte-identically.
  New `ErrDimensionUnavailable` sentinel discriminates "axis absent"
  (`SizeZ == 1` + `Z != 0`) from "axis index past size"
  (`ErrTileOutOfBounds`).
- **BIF multi-Z reading** via the `IMAGE_DEPTH` (32997) tag. BIF
  level 0 with `imageDepth > 1` exposes nominal + near + far focus
  planes through `TileAt(TileCoord{Z: z})`; `Image.ZPlaneFocus(z)`
  returns the per-plane Z-spacing offset (Z=0 nominal, Z=1..nNear
  near = negative offsets, Z=nNear+1..N-1 far = positive offsets)
  parsed from `<iScan>/@Z-spacing`. Synthetic fixture coverage in
  `formats/bif/multiz_test.go`; no real volumetric BIF in
  `sample_files/`.
- **OME-TIFF honest dimension reporting** — `Image.SizeZ/SizeC/SizeT`
  reflect `<Pixels SizeZ/SizeT>` and `<Channel>` element count
  (intentionally NOT `<Pixels SizeC>`, which describes per-pixel
  RGB sample count rather than separately-stored channels). Both
  Leica fixtures still report `SizeZ/SizeC/SizeT == 1`.
  `Level.TileAt(TileCoord{Z != 0})` returns
  `ErrDimensionUnavailable` until the per-IFD reader lands as a
  separate format-package milestone (sketched in
  `docs/formats/ometiff.md`).

### Changed

- **`Level` interface** gains `TileOverlap() image.Point` and
  `TileAt(TileCoord) ([]byte, error)` — additive evolution;
  existing concrete level types grow zero-returning /
  delegate-to-`Tile` impls. No caller change required for non-BIF
  formats.
- **`Image` interface** gains `SizeZ/SizeC/SizeT/ChannelName/
  ZPlaneFocus` — additive evolution; `SingleImage` provides
  defaults so 2D formats compile without changes.

### Deviations from upstream Python opentile

One new deliberate divergence (see
[`docs/deferred.md` §1a](docs/deferred.md) for full reasoning):

- **Multi-dimensional WSI API addition** — `TileCoord` +
  `Level.TileAt` + `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus`.
  Additive across all formats. Modern WSI consumers (fluorescence,
  focal-plane viewers, time series) need explicit multi-dim
  addressing; upstream Python opentile is 2D-only.

### Deferred (v0.8+)

- **L19** — openslide pixel-equivalence on BIF
  (infrastructure-only in v0.7; coordinate-system gap between
  opentile-go's padded TIFF grid and openslide's AOI-hull view).
- **L20** — DP 600 (and other future "VENTANA DP *") behavioural
  variance — unverified without a fixture.

### Retired (mid-v0.7)

- **L21** — Volumetric Z-stacks. The v0.7 multi-dim closeout
  introduced cross-format multi-dim addressing; BIF now reads
  the entire `IMAGE_DEPTH` Z-stack natively (Z=0 nominal + nNear
  near planes + nFar far planes). OME surfaces honest dimensions
  via `Image.SizeZ/SizeC/SizeT` and defers `TileAt(z != 0)` to a
  future format-package milestone — that work is not L21; it's
  a fresh OME-package work item gated on a real multi-Z OME
  fixture surfacing.

### Notes

- The original v0.7 design spec (§7) framed openslide
  pixel-equivalence as the primary correctness oracle.
  Mid-implementation we found openslide rejects spec-compliant DP
  200 BIFs (`Direction="LEFT"`) and uses an AOI-hull coordinate
  system that doesn't match opentile-go's padded TIFF view.
  Anecdotal community note: openslide is also believed to misread
  modern BIF generally. The v0.7 correctness bar is therefore
  tifffile + committed sample-tile SHAs, not openslide.
- v0.7 surfaced two correctness bugs caught only by writing the
  integration test (T19): `loadEncodeInfo` was silently swallowing
  the Ver<2 rejection; `bif.MetadataOf` didn't unwrap the file-
  closer Tiler. Both fixed in `49849a4`.

## [0.6.0] — 2026-04-27

OME-TIFF support — the fourth format opentile-go handles, closing the
upstream Python opentile 0.20.0 format set. Output is byte-identical to
**Python opentile 0.20.0 + tifffile** across every sampled tile and
every associated image we expose, on both Leica fixtures.

### Added

- **OME-TIFF format** — `formats/ome/`. Tiled levels with SubIFD-based
  pyramid traversal; OneFrame (non-tiled) levels via the new shared
  `internal/oneframe/` package; macro / label / thumbnail associated
  images; OME-XML metadata via stdlib `encoding/xml`. Two fixtures
  in the parity slate (`Leica-1.ome.tiff`, `Leica-2.ome.tiff`).
- **`Image` interface + `Tiler.Images() []Image`** (additive public API).
  Multi-image OME-TIFF files (Leica-2 carries 4 main pyramids) expose
  every pyramid via `Images()`. Single-image formats (SVS, NDPI,
  Philips) return a one-element slice via the new `opentile.SingleImage`
  helper. Existing `Tiler.Levels()` / `Level(i)` keep working as
  documented shortcuts to `Images()[0]`.
- **`opentile.FormatOME`** constant.
- **`internal/tiff.TagSubIFDs`** (TIFF tag 330) +
  **`Page.SubIFDOffsets()`** accessor.
- **`internal/tiff.File.PageAtOffset(off)`** for SubIFD traversal.
- **`internal/oneframe/`** package — factored from
  `formats/ndpi/oneframe.go` so OME (and later v0.7 BIF) reuse the
  same machinery. New `Options.FirstStripOnly` flag for OME's
  multi-strip planar pages.
- **`internal/jpegturbo` warning tolerance** — distinguishes
  `TJERR_WARNING` from fatal via `tjGetErrorCode`; treats warnings as
  success when `*dst` is populated. Required for OME OneFrame's
  truncated scan data; NDPI parity preserved.
- **`tests/oracle/tifffile_runner.py`** + **`tests/oracle/tifffile_session.go`** —
  new tifffile-based parity oracle covering every Image's tiled levels,
  including the 3 Leica-2 main pyramids opentile-py drops via its
  last-wins loop.
- **Per-format docs** under `docs/formats/` — one .md per format
  (svs, ndpi, philips, ome) with capability matrix, deviations, fix
  history, and upstream references.
- **Canonical `Deviations` section** in `docs/deferred.md` §1a.

### Changed

- **README rewritten** for public consumption. New format-support
  summary table; comprehensive API guide including the multi-image
  `Tiler.Images()` flow; "Deviations" subsection. Drops "Pure-Go"
  claim — opentile-go has one cgo dependency. Builds without cgo
  via `-tags nocgo` (SVS-only / NDPI-striped consumers unaffected).
- **Fixture schema** gained `Images []ImageFixture` for multi-image
  formats. Single-image fixtures unchanged.
- `internal/tiff.Page.scalarU32` falls through to `Values64` for
  BigTIFF LONG8/IFD8 scalar values — discovered while wiring SubIFD
  reads on the Leica fixtures, where `ImageWidth` / `ImageLength`
  were silently failing.

### Deviations from upstream Python opentile

Three new deliberate divergences (see
[`docs/deferred.md` §1a](docs/deferred.md) for full reasoning):

- **Multi-image OME pyramid exposure**: upstream's last-wins loop
  silently drops 3 of 4 main pyramids in `Leica-2.ome.tiff`; we
  expose all of them via `Tiler.Images()`. Use `Tiler.Levels()` for
  first-image-only behaviour.
- **PlanarConfiguration=2 plane-0-only indexing**: matches Python's
  silent flat-indexing into per-channel-tripled offset arrays.
- **First-strip-only on multi-strip OneFrame**: matches Python's
  `_read_frame(0)` behaviour on `rowsperstrip × samplesperpixel`
  planar pages.

### Retired

- **R7** (OME TIFF) — landed end-to-end. `docs/deferred.md §8` has
  the v0.6 retirement audit + the five JIT-gate outcomes (T1
  detection, T2 SubIFD parsing, T3 OneFrame factor decision, T4
  OME-XML schema, T5 tifffile splice-replication harness).

## [0.5.1] — 2026-04-26

### Fixed

- **Module path** — `go.mod` and every Go import statement renamed
  from `github.com/tcornish/opentile-go` to `github.com/wsilabs/opentile-go`,
  matching the actual GitHub repo location. v0.5.0's module path was
  wrong and `go get github.com/wsilabs/opentile-go@v0.5.0` failed for
  downstream consumers; pin to v0.5.1 or later. No public API changes;
  purely a packaging fix.

## [0.5.0] — 2026-04-26

Philips TIFF support — the third format opentile-go handles, paralleling
the v0.2 NDPI add. Output is **byte-identical to Python opentile
0.20.0** on every sampled tile and every associated image we expose,
across all 11 oracle slides (5 SVS + 2 NDPI + 4 Philips).

### Added

- **Philips TIFF format** — pyramid levels with sparse-tile
  blank-tile filling, label / macro / thumbnail associated images,
  DICOM-XML metadata extraction. Surface area: `formats/philips.New()`
  factory (registered by `formats/all`), `philips.MetadataOf(tiler)`
  for format-specific fields (PixelSpacing, BitsAllocated, etc.).
  4 sample fixtures (`Philips-{1,2,3,4}.tiff`, 277 MB to 3.1 GB; one
  is BigTIFF) in the integration + parity slates.
- `opentile.FormatPhilips` constant.
- `internal/jpegturbo.FillFrame` — new cgo entry point. tjTransform
  with an all-blocks CUSTOMFILTER overwriting every DCT coefficient
  to a luminance fill (DC = `LuminanceToDCCoefficient(luminance)`,
  AC = 0 on luma; chroma fully zeroed). Mirrors Python opentile's
  `JpegFiller.fill_image`. Used by Philips's sparse-tile blank-tile
  derivation.
- `internal/jpeg.InsertTables` — JPEGTables splice without APP14,
  sibling to `InsertTablesAndAPP14` used by SVS. Philips encodes
  standard YCbCr so no Adobe APP14 marker is needed.
- `internal/tiff.TagSoftware` constant + `Page.Software()` accessor
  (TIFF tag 305) used by Philips detection.

### Architecture

- DICOM-XML parsing via stdlib `encoding/xml` — first new use of
  the package in the codebase. Stack-based token decoder mirrors
  `ElementTree.iter('Attribute')`, descending into nested
  `<PIM_DP_SCANNED_IMAGES><Array><DataObject>...` wrappers that
  carry per-level Attributes in real fixtures.
- Per-level dimension correction via `formats/philips/dimensions.go`
  — direct port of `tifffile._philips_load_pages`. The first
  `DICOM_PIXEL_SPACING` entry calibrates the baseline mm scale; each
  subsequent entry produces a corrected size for the next tiled
  page, replacing the on-disk placeholder dimensions.
- Tile grid uses CORRECTED dims, not on-disk dims, matching Python's
  `image_size.ceil_div(tile_size)`. On-disk pages may carry more
  tile entries than `gx*gy`; trailing entries are unused but
  preserved for index parity with Python's
  `_tile_point_to_frame_index`.
- Sparse-tile blank tile is computed lazily on first sparse access
  (`sync.Once`); seed = first non-zero `TileByteCounts` entry, run
  through `InsertTables` → `FillFrame(luminance=1.0)`. Output
  byte-identical to Python's `Jpeg.fill_frame` on the same input.

### Retired

- **R5** (Philips TIFF) — landed end-to-end. `docs/deferred.md §7`
  has the v0.5 retirement audit + the three JIT-gate outcomes
  (T1 detection, T2 FillFrame determinism, T3 DICOM XML schema).

## [0.4.0] — 2026-04-26

NDPI completeness milestone. Output is **byte-identical to Python
opentile 0.20.0** on every sampled tile and every associated image we
expose, across all 7 fixtures in the parity oracle.

### Fixed

- **L12** — NDPI edge-tile OOB fill. Was misframed in v0.2 / v0.3 as
  "tjTransform CUSTOMFILTER non-determinism"; root cause re-diagnosed
  as a control-flow bug in `formats/ndpi/striped.go::Tile`. Pre-v0.4
  tried plain `Crop` first and silently returned mid-gray OOB fills
  (DC=0) on tiles where Crop succeeded despite extending past the
  image. Fix: dispatch geometry-first against image size, matching
  Python's `__need_fill_background` gate
  (`turbojpeg.py:839-863`). CMU-1 / OS-2 / Hamamatsu-1 NDPI fixtures
  regenerated; parity oracle's L12 `t.Logf` carve-out removed.
- **L17** — NDPI label `cropH` passes the full image height now,
  matching Python's `_crop_parameters[3] = page.shape[0]`. Pre-v0.4
  we floored the height to a whole-MCU multiple, dropping the last
  partial-MCU row. The pre-v0.4 deferred entry's "needs
  CropWithBackground" advice was wrong — libjpeg-turbo's
  `TJXOPT_PERFECT` accepts the partial last MCU row when the crop
  ends at the image edge.

### Added

- **L6 / R13** — NDPI Map pages (Magnification == -2.0) now surface
  as `AssociatedImage` entries with `Kind() == "map"`. Deliberate
  Go-side extension paralleling the v0.2 NDPI synthesised label
  (L14): upstream Python opentile chose not to surface Map pages
  even though tifffile classifies them as `series.name == 'Map'`
  one layer below.

### Deferred

- **R4** (SVS corrupt-edge reconstruct) and **R9** (JPEG 2000
  decode/encode) parked at
  [#1](https://github.com/wsilabs/opentile-go/issues/1). None of
  our 5 local SVS slides exhibits the corrupt-edge bug; 12 tasks
  of new cgo (libopenjp2 + jpegturbo Decode/Encode) plus a Pillow
  byte-equivalent BILINEAR port plus reconstruct.go for a
  synthetic-fixture-only feature is speculation, not completeness.
  Issue captures the full upstream algorithm, dependency tree,
  byte-parity bar from the v0.4 T1 determinism gate, and trigger
  conditions.

## [0.3.0] — 2026-04-25

Polish milestone over v0.2. Closes the v0.2 review surface (16
limitations + 25+ reviewer suggestions). **Public API frozen** from
this point — every name in `go doc ./...` survives v0.3 → v0.4
unchanged unless explicitly versioned.

### Added

- `ErrTooManyIFDs` sentinel error (A1).
- `Formats() []Format` introspection helper (A3).
- `WithNDPISynthesizedLabel(bool)` opt-out for the Go-side NDPI label
  synthesis (N-5).
- `OpenFile` errors now include the path (A2).
- `Config.TileSize` zero-size semantics documented (A4).
- `opentile/opentiletest/` sibling package for test helpers, mirroring
  stdlib's `httptest` / `iotest` idiom (T1).
- New SVS fixtures: `scan_620_.svs` (270 MB Grundium full-walk) and
  `svs_40x_bigtiff.svs` (4.8 GB Grundium sampled).
- `Makefile` with `test`, `cover`, `parity`, `vet`, `bench` targets.
- `make cover` gate enforcing ≥80% coverage per package.

### Changed

- **Batched parity oracle runner** — one Python subprocess per slide
  rather than per request. Default sample raised from ~10 to ~100
  positions per level; full sweep on all 7 oracle slides is now
  under 10 seconds (~10× faster than v0.2).
- SVS classifier now ports tifffile's `_series_svs` algorithm
  (replaces v0.2's positional one).
- `internal/tiff/walkIFDs` bulk-reads each IFD body in one ReadAt,
  ~2-4× faster on multi-page slides (O1).

### Fixed

- **L1** — SVS `SoftwareLine` had a trailing `\r` (CRLF parsing
  fix in `formats/svs/metadata.go`).
- **L7 + L11** — derive MCU size from SOF instead of hardcoding
  16×16 across NDPI overview crop and SVS associated-image DRI.
- **L10** — SVS LZW label was returning only strip 0 of multi-strip
  labels; now decodes all strips, raster-concatenates, and
  re-encodes as a single LZW stream.
- **L18** — `ConcatenateScans` rejected `ColorspaceFix=true` when
  `JPEGTables` was empty; matches Python's gate now (skip splice +
  APP14 when tables absent — required for Grundium SVS).
- BigTIFF tile offsets widened to uint64 (was rejecting
  `unsupported type 16`).
- `ConcatenateScans` dropped EOI assertions to match upstream's
  unconditional `frame[-2:] = end_of_image()` overwrite.

### Documented (no behaviour change)

- D1 — `decodeASCII` NUL-terminator tolerance.
- D2 — `decodeInline` `*byteReader` rationale.
- D3 — `Metadata.AcquisitionDateTime` `IsZero()` sentinel.
- I2 — `walkIFDs` overlapping-IFD detection limit.
- I7 — `ReplaceSOFDimensions` byte-scan invariant.
- N-6 — `CropWithBackground` chroma-DC=0 visual behaviour.
- N-9 — NDPI sniff cross-cutting peek rationale.
- O2 — `int(e.Count)` 32-bit truncation note.

## [0.2.0] — 2026-04-21

Second functional milestone. Adds NDPI support (the second WSI
format), BigTIFF, associated images on both formats, and the Python
parity oracle infrastructure that has guided every release since.

### Added

- **Hamamatsu NDPI format** — striped + one-frame pyramid levels,
  including the 64-bit offset extension for slides > 4 GB.
  Associated images (overview + synthesised label).
  `ndpi.MetadataOf` for source-lens / focal-offset / scanner serial.
- **BigTIFF** support across `internal/tiff`, transparent to format
  packages.
- **SVS associated images** — label, overview, thumbnail surfaced
  via `Tiler.Associated()`.
- **`internal/jpeg`** — pure-Go marker library with `ConcatenateScans`
  byte-identical to Python opentile's `jpeg.concatenate_scans`,
  plus `InsertTablesAndAPP14`, `NDPIStripeJPEGHeader`, `LumaDCQuant`
  / `LuminanceToDCCoefficient`.
- **`internal/jpegturbo`** — cgo wrapper over libjpeg-turbo for
  lossless MCU-aligned crop with CUSTOMFILTER-driven white-fill OOB
  for edge tiles. Builds without cgo via `-tags nocgo` (returns
  `ErrCGORequired`).
- **Python parity oracle** under `//go:build parity`
  (`tests/oracle/`), byte-comparing every `Level.Tile` and
  `Associated.Bytes` against Python opentile 0.20.0 across all 5
  sample slides.

### Architecture invariants

- Format-specific quirks live in format packages, not `internal/tiff`.
- cgo narrowly scoped to `internal/jpegturbo/`.
- Lock-free hot path for metadata; `Tile()` is concurrent-safe.
- Parity with upstream is the correctness bar.

## [0.1.0] — 2026-04-19

Initial functional milestone. Aperio SVS tiled-level passthrough.

### Added

- SVS pyramid levels (JPEG and JPEG 2000 compressions).
- TIFF parser in `internal/tiff` (classic TIFF only at this point).
- Public `Tiler` / `Level` / `AssociatedImage` interfaces.
- Three real-slide fixtures: CMU-1-Small-Region.svs, CMU-1.svs (JPEG),
  JP2K-33003-1.svs (JP2K passthrough).

[Unreleased]: https://github.com/wsilabs/opentile-go/compare/v0.24.0...HEAD
[0.24.0]: https://github.com/WSILabs/opentile-go/releases/tag/v0.24.0
[0.23.0]: https://github.com/WSILabs/opentile-go/releases/tag/v0.23.0
[0.22.1]: https://github.com/wsilabs/opentile-go/releases/tag/v0.22.1
[0.22.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.22.0
[0.21.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.21.0
[0.20.1]: https://github.com/wsilabs/opentile-go/releases/tag/v0.20.1
[0.20.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.20.0
[0.19.1]: https://github.com/wsilabs/opentile-go/releases/tag/v0.19.1
[0.19.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.19.0
[0.18.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.18.0
[0.17.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.17.0
[0.16.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.16.0
[0.15.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.15.0
[0.14.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.14.0
[0.13.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.13.0
[0.12.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.12.0
[0.11.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.11.0
[0.10.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.10.0
[0.9.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.9.0
[0.8.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.8.0
[0.7.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.7.0
[0.6.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.6.0
[0.5.1]: https://github.com/wsilabs/opentile-go/releases/tag/v0.5.1
[0.5.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.5.0
[0.4.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.4.0
[0.3.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.3.0
[0.2.0]: https://github.com/wsilabs/opentile-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/wsilabs/opentile-go/tree/feat/v0.1
