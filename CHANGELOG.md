# Changelog

All notable changes to opentile-go are recorded here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) loosely;
versioning is semantic (`MAJOR.MINOR.PATCH`).

The single source of truth for "what was deferred and why" is
[`docs/deferred.md`](docs/deferred.md). This file is the curated
front-page summary; the deferred file has the full reasoning,
upstream references, and retirement audit per milestone.

## [Unreleased]

Active limitations after v0.14: L4, L5, L14 (Permanent — carried over
from v0.6); L19, L20, L23, L24, L25 (carried forward from v0.7 / v0.8);
L26, L27, L28, L29 (generic-TIFF design Q-decisions, v0.10); L30, L31,
L32, L33, L34 (Leica SCN design Q-decisions, v0.11). v0.14 introduced
no new active limitations — it was a small additive milestone
extending generic-TIFF compression support to 4 novel tile codecs.
See `docs/deferred.md` §11 consolidated backlog. Open work parked in
tracked issues:

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
| `github.com/cornish/opentile-go/formats/philips` | `github.com/cornish/opentile-go/formats/philipstiff` |
| `github.com/cornish/opentile-go/formats/ome` | `github.com/cornish/opentile-go/formats/ometiff` |

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

- **R4 / R9** ([#1](https://github.com/cornish/opentile-go/issues/1)) —
  SVS corrupt-edge reconstruct + JP2K decode/encode. No local SVS slide
  exhibits the corrupt-edge bug; work parked until one motivates it.
- **R6** ([#2](https://github.com/cornish/opentile-go/issues/2)) —
  3DHistech TIFF. Niche MRXS conversion target; never encountered in
  the wild. Trigger-driven park.
- **R15** ([#3](https://github.com/cornish/opentile-go/issues/3)) —
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
  from `github.com/tcornish/opentile-go` to `github.com/cornish/opentile-go`,
  matching the actual GitHub repo location. v0.5.0's module path was
  wrong and `go get github.com/cornish/opentile-go@v0.5.0` failed for
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
  [#1](https://github.com/cornish/opentile-go/issues/1). None of
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

[Unreleased]: https://github.com/cornish/opentile-go/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/cornish/opentile-go/releases/tag/v0.9.0
[0.8.0]: https://github.com/cornish/opentile-go/releases/tag/v0.8.0
[0.7.0]: https://github.com/cornish/opentile-go/releases/tag/v0.7.0
[0.6.0]: https://github.com/cornish/opentile-go/releases/tag/v0.6.0
[0.5.1]: https://github.com/cornish/opentile-go/releases/tag/v0.5.1
[0.5.0]: https://github.com/cornish/opentile-go/releases/tag/v0.5.0
[0.4.0]: https://github.com/cornish/opentile-go/releases/tag/v0.4.0
[0.3.0]: https://github.com/cornish/opentile-go/releases/tag/v0.3.0
[0.2.0]: https://github.com/cornish/opentile-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/cornish/opentile-go/tree/feat/v0.1
