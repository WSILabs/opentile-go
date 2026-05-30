# opentile-go

Direct Go port of [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Sectra AB) with one cgo dependency (libjpeg-turbo, narrowly scoped to `internal/jpegturbo/`). Reads tiles from WSI (whole-slide imaging) TIFF files used in digital pathology.

## Current milestone — v0.29 (in progress 2026-05-29)

- **Scope:** ReadRegion allocation-elimination perf milestone. Two
  layers shipped (of three planned):
  - **Layer 1:** `imageReadRegionImpl` skips `fillWhite(dst)` when
    the requested region is fully in-bounds and doesn't touch an
    edge tile.
  - **Layer 2:** module-level `sync.Pool` of per-tile decode-output
    `*decoder.Image` buffers, keyed by `(W, H, Format)`. ReadRegion
    borrows once per call via `ImageDecodedTileInto`. Required
    `strippedImage.DecodedTile` to honor `opts.Dst`.
  - **Layer 3 (abandoned):** NDPI pixelCache scratchPool implemented
    then reverted within v0.29. Race detected (evicted-buffer-recycled
    while a cache-hit reader still held the pointer). Deferred
    pending refcount or alternative cache topology.
- **API additions:** none public. Internal: `borrowTileScratch` /
  `returnTileScratch` in new `decoded_tile_scratch.go`.
- **API breaks:** none. RawTile / DecodedTile / ReadRegion /
  ScaledStrips behave bit-identically.
- **Active limitations:** bench-svs unchanged because it calls
  `DecodedTile` directly (bypasses Layer 1+2's ReadRegion-only
  scope). 11 GB of NDPI pixelCache frame allocations remain (was
  Layer 3's target).
- **Correctness bar:** `make test` green; new tests under
  `-race -count=3`: 5 scratch-pool tests, 2 fillWhite-skip tests
  with synthetic `knownPixelReader`, 2 NDPI fast-path Dst tests.
  v0.27 + v0.28 regression suite green. `make bench-ndpi` ≥270
  Mpix/s (up from 220); `make bench-svs` ≥475 Mpix/s (unchanged).
- **Sealed Q-decisions** (per spec): see design doc. Spec's Layer 3
  Q-decisions effectively void after JIT abandonment.
- **Deferred forward:** Layer 3 (NDPI pixelCache scratchPool) needs
  refcount design or alternative; direct DecodedTile allocation
  pooling; ScaledStrips internal cache profile.
- **Bench reality (v0.29 final, Layer 1+2 only):**
  - bench-ndpi: ~300 Mpix/s single-thread (vs 251 v0.28: **+19%**)
  - bench-ndpi-mt: 593 Mpix/s multi-thread (vs 539 v0.28: **+10%**)
  - bench-svs: unchanged at ~577 Mpix/s (no `ReadRegion`)
  - bench-svs-mt: unchanged at ~2117 Mpix/s
  - Allocation: 38.7 GB → 17 GB on bench-ndpi-mt (**-57%**)
- **Layer 3 lessons:** the v0.27 promise pattern protects
  population but doesn't track readers post-population. Any pool
  reuse of cached buffers needs explicit reader-lifetime tracking
  (refcount) — by-design future work, not a bug to chase.
- **Design:** docs/superpowers/specs/2026-05-29-opentile-go-v29-readregion-perf-design.md
- **Plan:** docs/superpowers/plans/2026-05-29-opentile-go-v29-readregion-perf.md
- **Work branch:** feat/v0.29

## Previous milestone — v0.28 (shipped 2026-05-29)

- **Scope:** Cross-format decoder-handle pool. New
  `internal/decoderhandle.Pool` primitive — a fixed-size pool of
  long-lived `decoder.Decoder` instances per `(Slide, codec)`, sized
  `min(NumCPU, 8)`. NDPI's v0.27 per-`strippedImage` `decoderHandle`
  migrates to the same shared primitive (instance ownership stays
  per-level). `Slide.ImageDecodedTile` slow path replaces per-call
  `fac.New() / dec.Close()` with `pool.Borrow() / pool.Return()`.
  `Slide.Close` drains every cached pool. v0.27 NDPI fast path gains
  multi-core parallelism (was capped at single-thread by mutex).
  Cross-format slow paths (SVS, OME-TIFF tiled, BIF, Leica SCN, IFE,
  SZI, COG-WSI, generictiff, Philips) get measurable bulk throughput
  improvement from eliminated `tjInit + tjDestroy` churn
  (~290 µs/call avoided, dominated by `tjDestroy`).
- **API additions:** none public. Internal: `decoderhandle.Pool`,
  `Slide.decoderFor`, `Slide.handlesMu`, `Slide.handles`,
  `Slide.HandleCountForTest` (test-only via export_test.go).
- **API breaks:** none. RawTile bit-for-bit unchanged.
- **Active limitations:** NDPI handle instance scope stays
  per-strippedImage (no Slide-level consolidation). Pool capacity is
  hardcoded at `min(NumCPU, 8)` — no public knob.
- **Correctness bar:** `make test` green; new pool unit tests
  (`internal/decoderhandle/handle_test.go`, 8 tests) and Slide
  integration tests (`slide_handle_test.go`, 3 tests) pass under
  `-race -count=3`. v0.27 NDPI fast-path tests
  (`TestNDPIFastPathPixelParity`, `TestNDPIFastPathConcurrent`,
  `TestNDPIDecodedTilePathParity`) pass unchanged. `make bench-ndpi`
  ≥220 Mpix/s (tightened from 130). `make bench-svs` ≥566 Mpix/s
  (new gate, 95% of measured 596 baseline).
- **Sealed Q-decisions** (per spec): 10 sealed Qs covering scope,
  primitive migration, concurrency shape, lazy vs eager init, API
  surface, instance scope, bench coverage, gate level, multi-thread
  bench validation, Close lifecycle.
- **Deferred forward:** NDPI Slide-level handle consolidation;
  sync.Pool migration; NDPI oneframe fast path (confirmed
  unprofitable during v0.28 brainstorm); JPEG-frame cache bounding;
  `WithScale != 1` integration. `tests/oracle/` build break stays
  pre-existing.
- **Bench reality:**
  - bench-ndpi: 251 Mpix/s single-thread (v0.27 was 243; ~3% diff
    is noise), 539 Mpix/s multi-thread (2.15× — masked by
    ReadRegion's fillWhite Go-side allocation).
  - bench-svs: 596 Mpix/s single-thread, **2121 Mpix/s
    multi-thread (3.56× single-thread)** — clean validation of the
    pool's deliverable on the slow path.
- **Design:** docs/superpowers/specs/2026-05-29-opentile-go-v28-cross-format-decoder-pool-design.md
- **Plan:** docs/superpowers/plans/2026-05-29-opentile-go-v28-cross-format-decoder-pool.md
- **Work branch:** feat/v0.28

## Previous milestone — v0.27 (shipped 2026-05-28)

- **Scope:** NDPI striped fast pixel path
  (decode-once-per-strip + blit). Closes the ~5× per-thread perf gap
  between opentile-go and openslide on NDPI tile decode. Adds a
  decoded-pixel-frame LRU cache (`formats/ndpi.pixelFrameCache`,
  bounded `max(NumCPU, 16)` with promise-pattern population) plus a
  reusable long-lived decoder handle (`formats/ndpi.decoderHandle`)
  on `strippedImage`. `Slide.ImageDecodedTile` type-asserts on a new
  unexported `decodedTiler` interface; non-NDPI readers, non-striped
  NDPI levels, and `WithScale != 1` calls fall through to the v0.26
  path. Critical wrapper-delegation fix on `fileCloser`/`mmapCloser`
  so the type assertion finds the underlying `*ndpi.tiler`. Measured
  CMU-1.ndpi single-thread: 44.25 s → 8.03 s (44.1 → 243.1 Mpix/s);
  v0.27 is now 0.96× of openslide.
- **API additions:** none public. Internal: unexported `decodedTiler`
  interface; `internal/fastpath.ErrUnsupported` sentinel;
  `(*strippedImage).DecodedTile`, `(*tiler).ImageDecodedTile`,
  `(*fileCloser).ImageDecodedTile`, `(*mmapCloser).ImageDecodedTile`.
  `(*tiler).Close` now releases decoder handles (was no-op).
- **API breaks:** none. RawTile (compressed bytes API) bit-for-bit
  unchanged; `ScaledStrips`/`ReadRegion` inherit the speedup
  transparently.
- **Active limitations:** NDPI oneframe path
  (`internal/oneframe`, Hamamatsu-1.ndpi) still uses the v0.26 slow
  path through the dispatch fallback. RawTile + assembled JPEG-frame
  cache unchanged. `WithScale != 1` keeps the slow path per call.
- **Correctness bar:** `make test` green across 25 packages including
  the 104s fixture suite; new `TestNDPIFastPathPixelParity`,
  `TestNDPIFastPathConcurrent` (32-way fanout), cross-fixture
  `TestNDPIDecodedTilePathParity` (CMU-1, OS-2, Hamamatsu-1 all
  levels) all green; `make bench-ndpi` PASS at 243 Mpix/s (gate is
  ≥130). Plan execution included a foundational pre-gate (T1.1)
  that confirmed the design assumption (decode-then-blit ==
  crop-then-decode at the pixel level) before building anything.
- **Sealed Q-decisions** (per spec): see design doc §3 (10 sealed
  Qs). Q1 architectural lever; Q2 striped only; Q3 purely internal
  API; Q4 small bounded LRU; Q5 edge tiles keep current path;
  Q6 RawTile + JPEG cache unchanged; Q7 mutex-serialized single
  handle (pool deferred); Q8 type-assertion dispatch with
  `ErrUnsupported` fallthrough; Q9 single canonical RGB frame at
  frame resolution; Q10 `WithScale != 1` falls to slow path.
- **Deferred forward:** NDPI oneframe fast path (next obvious lever);
  tactical handle pooling for RawTile + ScaledStrips; JPEG-frame
  cache bounding; `WithScale` integration. Pre-existing
  `tests/oracle/` build break (v0.24 Level API drift) flagged as not
  v0.27 introduced.
- **Bench reality:** opentile-go is now per-thread competitive with
  openslide on NDPI. wsitools' multi-thread `convert --to dzi/szi`
  inherits the win automatically through `ScaledStrips`.
- **Design:** docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md
- **Plan:** docs/superpowers/plans/2026-05-28-opentile-go-v27-ndpi-pixel-cache.md
- **Work branch:** feat/v0.27

## Previous milestone — v0.26 (shipped 2026-05-26)

ScaledStrips iterator — the libvips-speed primitive that dzsave / tile-
server / region-extract tools consume. `*Slide.ScaledStrips` emits
horizontal strips of a slide scaled to a target resolution, with
internal parallel decode workers, per-iterator LRU tile cache, and
lookahead pre-fetch. Per-thread throughput inherits from
`Slide.ImageDecodedTile`, which v0.27 has now made competitive with
openslide on NDPI.

## Previous milestone — v0.20 (shipped 2026-05-20)

- **Scope:** Cross-format `Writer` typed field — closes R22.
  Adds `Writer string` to `opentile.Metadata` carrying the
  file-producer identifier (distinct from `ScannerManufacturer`
  scanner-OEM and `ScannerSoftware []string` broader stack).
  Per-format population in all 10 readers (SVS canonical +
  Grundium detection from v0.18; NDPI; Philips; OME-TIFF
  promoting ome.creator; Leica SCN; BIF; IFE; SZI; Generic-TIFF
  with wsi-tools override; COG-WSI from WSIToolsVersion private
  tag 65084). 5 plan tasks single-batch execution. Pure-additive;
  no API or behavior breakage.
- **API additions:** `opentile.Metadata.Writer string` typed
  field. No new sentinels, packages, or interface methods.
- **API breaks:** none.
- **Active limitations:** unchanged from v0.19. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  docs/deferred.md §1a): unchanged — v0.20 is a typed-field
  surfacing of values opentile-go already reads.
- **Correctness bar:** make test green; TestSlideParity 40 fixtures
  green; TestCrossFormatMetadata extended with per-fixture
  `wantWriterContains` substring assertions (Aperio Image
  Library, Grundium Ocus, NanoZoomer, "4.0.3", Bio-Formats,
  iScan 1.1, GT450, 1.4.0 SCN, "Scan it", wsitools); all green.
  `make cover` ≥80% per package.
- **Sealed Q-decisions** (per spec): typed `Writer` (Q1);
  per-format population at the same site as existing
  ScannerSoftware logic (Q2); Properties keys retained
  backward-compat (Q3); converted-file semantics — writer is the
  converter, scanner attribution preserved (Q5); R23 derived
  bug filed for separate fix (out-of-scope).
- **Deferred forward:** R19 (bare DZI), R23 (re-apply v0.18 SVS
  detectWriter to Grundium-source COG-WSI). L19, L20, L23-L25,
  L26-L29, L30-L34, R4/R6/R9, R15. v1.0 cut still pending.
- **R22 retired:** cross-format Writer typed field — shipped
  (see deferred.md §8n).
- **Bench reality:** no perf-axis work; pure metadata-surfacing
  milestone.
- **Design:** docs/superpowers/specs/2026-05-20-opentile-go-v20-writer-typed-field-design.md
- **Plan:** docs/superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md
- **Work branch:** feat/v0.20

## Previous milestone — v0.19 (shipped 2026-05-20)

COG-WSI support. Closed the user's two GH issues
([#5](https://github.com/wsilabs/opentile-go/issues/5) generic-
TIFF WSI-tag awareness + integer-multiple pyramid ratio
relaxation; [#6](https://github.com/wsilabs/opentile-go/issues/6)
dedicated `cogwsi` reader). New `formats/cogwsi/` package +
`internal/cog/` ghost-area parser + 8 typed WSI private-tag
accessors on `*tiff.Page` (tags 65080-65087). cogwsi wraps an
inner generictiff Tiler (Option A); validation at Open returns
`ErrNotConformantCOGWSI`. Added `opentile.FormatCOGWSI`.
TestSlideParity grew 30 → 40 fixtures. R21 retired — plain
geospatial COG permanently YAGNI for opentile-go.

**Patch v0.19.1 (shipped 2026-05-20):** coverage cleanup —
cogwsi 77.5% → 91.2%; szi 76.8% → 92.8%; oneframe 70.4% →
93.1% (new `internal/oneframe/oneframe_test.go` from scratch).
No API or behavior changes. Flagged `internal/oneframe.warm()`
as dead-code candidate.

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

- Module path: `github.com/wsilabs/opentile-go`
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
