# opentile-go

Direct Go port of [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Sectra AB) with one cgo dependency (libjpeg-turbo, narrowly scoped to `internal/jpegturbo/`). Reads tiles from WSI (whole-slide imaging) TIFF files used in digital pathology.

## Current milestone — v0.19 (shipped 2026-05-20)

- **Scope:** COG-WSI support. Closes user's two GH issues —
  [#5](https://github.com/cornish/opentile-go/issues/5) (generic-
  TIFF WSI-tag awareness + integer-multiple pyramid ratio
  relaxation) and [#6](https://github.com/cornish/opentile-go/issues/6)
  (dedicated `cogwsi` reader). New `formats/cogwsi/` package +
  `internal/cog/` ghost-area parser + WSI private tag readers in
  `internal/tiff` (8 typed accessors on `*tiff.Page` for tags
  65080-65087). Extends `formats/generictiff/` to honor WSI tags
  as authoritative + accept clean integer-multiple pyramid ratios.
  8 plan tasks across single-batch execution.
- **API additions:** `opentile.FormatCOGWSI = "cog-wsi"` +
  `formats/cogwsi/` package (Tiler + Factory + UnwrapTiler) +
  `cogwsi.ErrNotConformantCOGWSI` sentinel + `internal/cog/`
  parser (`GhostArea`, `ParseGhostArea`, `ParseCOGWSIVersion`,
  `ErrGhostAreaMalformed`).
- **API breaks:** none (purely additive; existing API unchanged).
- **Active limitations:** unchanged from v0.18. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  docs/deferred.md §1a): +1 (COG-WSI reader + integer-multiple
  pyramid ratio acceptance). Python opentile doesn't read COG-WSI.
- **Correctness bar:** make test green; TestSlideParity 40 fixtures
  green (30 → 40 with 10 new cog-wsi fixtures);
  TestCOGWSIGeometry green; cross-fixture parity gate green
  (each `<source>_cog-wsi.tiff` vs original source = bit-exact
  tile bytes).
- **Sealed Q-decisions** (per spec + plan §3): cogwsi as wrapper
  around inner generictiff Tiler (Option A; minimal diff);
  ClassifyAssociatedFromPage wrapper preserves existing signature;
  validation at Open returns ErrNotConformantCOGWSI;
  cogwsi.Factory registered AFTER szi + BEFORE generictiff;
  COG_WSI_VERSION 0.1-only (defensive — future versions reject
  loudly); plain (geospatial) COG = permanently YAGNI (R21
  retired).
- **Deferred forward:** R19 (bare DZI), R22 (cross-format Writer
  typed field). L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9,
  R15. v1.0 cut still pending.
- **R21 retired:** general COG first-class support — WSI-context
  portion shipped as COG-WSI; plain geospatial COG isn't our
  domain.
- **Bench reality:** no perf-axis work this milestone; v0.13
  bandwidth-deduplication API delegates through to generictiff
  for cog-wsi files.
- **Fixture catch-ups (incidental):** scan_620_grundium_TIFF
  geometry flipped 3-level → 4-level (real shape; v0.10 pin was
  buggy); Hamamatsu-1.ndpi + OS-2.ndpi gained scanner_serial;
  Leica-1.ome.tiff + Leica-2.ome.tiff gained acquisition_rfc3339.
  No reader-side regressions.
- **Design:** docs/superpowers/specs/2026-05-20-opentile-go-v19-cog-wsi-design.md
- **Plan:** docs/superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md
- **Work branch:** feat/v0.19

## Previous milestone — v0.18 (shipped 2026-05-09)

SVS writer-vendor detection. Closes misattribution bug where
Grundium-written SVS files (and any other non-Aperio writer)
reported ScannerManufacturer="Aperio". v0.18 detects the actual
writer from ImageDescription first-line + TIFF Software/Make
tags; namespaces Properties keys per detected writer. Documents
recognized writer set explicitly. OME-TIFF audited (already
correct; docs extended). 3 plan tasks single batch.

## Previous milestone — v0.17 (shipped 2026-05-09)

Cross-format Metadata expansion — closes R20. Hybrid: typed
additions (MicronsPerPixel + per-axis X/Y; ImageDescription) +
Properties map[string]string for opentile-go-canonical extensions
and vendor-namespaced passthrough. Every format reader updated
to populate the new fields. Format-specific Metadata structs
cleaned up per Q4 Option B.

## Previous milestone — v0.16 (shipped 2026-05-09)

Smart Zoom Image (SZI) reader. Closes R18. New formats/szi/ +
internal/dzi/ packages; opentile.FormatSZI + opentile.CompressionPNG
enums; mmap-aliased ZIP-entry tile fetch; szi.MetadataOf accessor +
szi.Metadata with VendorProperties map; 2 fixtures (CMU-1.szi +
scan_618_grundium 709 MB) wired into TestSlideParity (28 → 30).
Bare DZI (R19) still parked but pre-prepared via internal/dzi.

## Earlier milestones

- v0.15 (2026-05-08): Naming cleanup — AssociatedImage.Kind() →
  Type() (DICOM convention); generic-TIFF + Leica SCN emitted
  "macro" → "overview" (aligns with DICOM + Python opentile + 6
  sibling format readers). IFE preserves both. Breaking change;
  pre-1.0; sole-consumer sign-off.
- v0.14 (2026-05-08): Novel-codec milestone — generic-TIFF reader
  recognises 4 new tile compression tag values (WebP / JPEG XL /
  AVIF / HTJ2K) produced by the user's wsi-tools transcoder. Plus
  a wsi-tools ImageDescription parser. Additive — no breaking
  changes.
- v0.13 (2026-05-08): Bandwidth-deduplication API — Level.TilePrefix
  + TileBodyInto + TileBodyMaxSize + opentile.SpliceJPEGTile helper.
  Additive (no breaking changes).
- v0.12 (2026-05-07): Naming cleanup — striped→stripped; Format-
  Philips→FormatPhilipsTIFF; FormatOME→FormatOMETIFF; package
  renames formats/philips→philipstiff and formats/ome→ometiff.
  First milestone using subagent-driven-development skill.
- v0.11 (2026-05-06): Leica SCN reader; first real-fixture multi-
  channel; first multi-region "discontinuous scanning" reader.
- v0.10 (2026-05-06): generic-TIFF catch-all reader.
- v0.9 (2026-05-01): perf milestone — mmap default + TileInto +
  WarmLevel + splice template.
- v0.8: Iris IFE — first non-TIFF format.
- v0.7: Ventana BIF — first format beyond upstream coverage.
- v0.6: OME-TIFF — closes upstream Python opentile's format set.
- v0.5: Philips TIFF (third format).
- v0.4: NDPI completeness (L12 OOB-fill + L17 label crop).
- v0.3: polish, settled API, public-API frozen from this point.
- v0.2: NDPI + BigTIFF + associated images + Python parity oracle.
- v0.1: Aperio SVS tiled-level passthrough.

## Invariants

- **Public API stable from v0.3.** Adding new exported names is fine; renaming, moving, or removing is a breaking change that requires a major-version bump (or, until we have external users, an explicit owner sign-off).
- **Don't guess format behavior — read upstream.** This is a **direct port** of Python opentile (which delegates format details to tifffile). Whenever classification, layout, tag semantics, or edge-case handling is unclear: **read `imi-bigpicture/opentile` first, then `cgohlke/tifffile`**. Guessed behavior cost v0.2 five separate debugging cycles (NDPI IFD layout, NDPI metadata tag numbers, NDPI StripOffsets tag, NDPI striped vs. oneframe gate, APP14 byte values) — every one fixed by reading the actual upstream source. The v0.4 plan elevates this to a structural per-task `Step 0: Confirm upstream` action that the executor must run before any production-code edit. The rule: if you catch yourself reasoning from first principles about a WSI format quirk, stop and find the upstream code that handles it. Port directly, adapt for Go idioms, but preserve the logic.
- **No cutting corners; no active users yet.** Complete things we know are broken before moving on. When a bug is identified, the rule is: fix it, don't defer. Plan thoroughly for v0.3+ rather than race.
- **Architectural placement of ported logic:** format-specific quirks belong in the format package (`formats/ndpi/`, `formats/svs/`), not `internal/tiff`. `internal/tiff` stays a generic TIFF/BigTIFF/NDPI-IFD parser. Examples: NDPI page-series grouping, SVS ImageDescription quirks, Philips sparse-tile filling.
- **cgo is narrowly scoped.** `internal/jpegturbo/` is the only package linking libjpeg-turbo. Under `nocgo` build tag, format paths that need it return `ErrCGORequired`; the rest works.
- **Direct port under Apache 2.0** with attribution retained in `NOTICE`. Not affiliated with or endorsed by Sectra AB or the BigPicture project.
- **Parity with upstream is the correctness bar.** Upstream's pytest cases are ported to Go tests; a fixture-backed integration suite compares tile bytes against a committed snapshot. An opt-in `//go:build parity` harness that shells out to Python opentile is v0.2.
- **Lock-free hot path for metadata.** Parsed IFDs, per-tile offset/length arrays, and metadata are populated at `Open()` time and immutable thereafter. `Tile()` is safe to call concurrently from many goroutines — the shared-state caches in `formats/ndpi/striped.go` (per-frame assembly cache) and `formats/ndpi/oneframe.go` (extended-frame cache) use double-checked locking and `sync.Once` respectively and produce byte-deterministic results regardless of which goroutine populates them first.

## Conventions

- Module path: `github.com/cornish/opentile-go`
- Go 1.23+ (for `iter.Seq2`)
- `internal/tiff` and `internal/jpeg` are internal — both shaped for opentile's needs, not general-purpose libraries. `internal/jpegturbo` is the only cgo package in the module.
- Format subpackages (`formats/svs/`, `formats/ndpi/`, …) are public; `formats/all` is the umbrella registration package
- `io.ReaderAt` + `int64` size is the core input (stdlib `*os.File` satisfies concurrent-use semantics)
- Public tile methods: `Level.Tile(x, y int)` returns raw compressed bytes; `Level.TileReader(x, y)` streams via `io.SectionReader`; `Level.Tiles(ctx)` is serial row-major via `iter.Seq2`

## Sample slides

Local slides live in `/sample_files/` (gitignored). v0.6 fixture set:
- `sample_files/svs/CMU-1-Small-Region.svs` (1.9 MB, JPEG) — primary fixture
- `sample_files/svs/CMU-1.svs` (177 MB, JPEG) — full-slide fixture
- `sample_files/svs/JP2K-33003-1.svs` (63 MB, JPEG 2000 passthrough) — proves JP2K path works without a codec
- `sample_files/svs/scan_620_.svs` (270 MB, BigTIFF JPEG, Grundium) — full-walk fixture exercising L18 (no shared JPEGTables)
- `sample_files/svs/svs_40x_bigtiff.svs` (4.8 GB, BigTIFF JPEG, Grundium) — sampled fixture; first BigTIFF SVS in the suite
- `sample_files/ndpi/CMU-1.ndpi` (188 MB) — small NDPI fixture
- `sample_files/ndpi/OS-2.ndpi` (931 MB) — medium NDPI with multiple series + a Map page
- `sample_files/ndpi/Hamamatsu-1.ndpi` (6.6 GB) — **NDPI 64-bit offset extension**; sampled fixture; carries a Map page
- `sample_files/philips-tiff/Philips-1.tiff` (311 MB, 8 levels) — Hamamatsu-scanned, no associated images
- `sample_files/philips-tiff/Philips-2.tiff` (872 MB, 10 levels) — 3D Histech-scanned, Macro-only
- `sample_files/philips-tiff/Philips-3.tiff` (3.1 GB, 9 levels, BigTIFF) — Hamamatsu-scanned, Macro + Label
- `sample_files/philips-tiff/Philips-4.tiff` (277 MB, 9 levels) — Philips-scanned, exercises sparse-tile blank-tile path heavily
- `sample_files/ome-tiff/Leica-1.ome.tiff` (689 MB, 5 levels, BigTIFF) — single main pyramid + macro
- `sample_files/ome-tiff/Leica-2.ome.tiff` (1.2 GB, 6 levels × 4 main pyramids, BigTIFF) — multi-image OME; exercises the v0.6 multi-image deviation
- `sample_files/bif/Ventana-1.bif` (227 MB) — DP 200 spec-compliant; tifffile parity oracle target
- `sample_files/bif/OS-1.bif` (3.6 GB) — legacy iScan Coreo; sampled fixture
- `sample_files/ife/cervix_2x_jpeg.iris` (2.16 GB, 9 levels, JPEG) — first non-TIFF fixture; downloaded from Iris's public S3 bucket; SHA256 `b080859913d2…`. Sampled fixture (cervix is too large for full-walk under the 5 MB per-fixture cap)
- `sample_files/szi/CMU-1.szi` (1.5 MB, 16 levels, JPEG) — canonical CMU-1 re-encoded as SZI by smartinmedia spec authors; first ZIP-backed fixture
- `sample_files/szi/scan_618_grundium_SZI.szi` (709 MB, 19 levels, JPEG) — Grundium-scanner-produced SZI; sampled fixture

## Commands

The Makefile bundles every gate. Prefer it over typing the env-var dance manually:

```sh
make test     # go test ./... -race -count=1
make cover    # ≥80% per package; OPENTILE_TESTDIR auto-set
make parity   # batched parity oracle vs Python opentile 0.20.0
make vet      # go vet ./...
make bench    # NDPI per-tile throughput regression gate
```

Direct invocations (when the Makefile-implicit env defaults aren't right):

```sh
# regenerate parity fixtures from real slides (walks svs/ and ndpi/)
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run TestGenerateFixtures -generate -v

# byte-parity vs Python opentile 0.20.0 with custom Python interpreter
OPENTILE_ORACLE_PYTHON=/private/tmp/opentile-py/bin/python \
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v
```

## Execution mode

Plan execution uses `superpowers:subagent-driven-development`: one fresh implementer subagent per plan task, followed by a spec-compliance review subagent and a code-quality review subagent. Tasks are batched 4–6 at a time; after each batch, execution halts for a controller checkpoint before the next batch begins.
