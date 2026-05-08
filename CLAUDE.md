# opentile-go

Direct Go port of [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Sectra AB) with one cgo dependency (libjpeg-turbo, narrowly scoped to `internal/jpegturbo/`). Reads tiles from WSI (whole-slide imaging) TIFF files used in digital pathology.

## Current milestone — v0.14 (shipped)

- **Scope:** Novel-codec milestone — generic-TIFF reader recognises
  4 new tile compression tag values produced by the user's wsi-tools
  transcoder (WebP, JPEG XL, AVIF, HTJ2K). Plus a wsi-tools
  ImageDescription parser populating standard Metadata fields.
  Additive — no breaking changes. 6 plan tasks single batch.
- **API additions:** 3 new `opentile.Compression` enum values
  (`CompressionWebP`, `CompressionJPEGXL`, `CompressionHTJ2K`);
  validator + reader recognise tags 34712 (registered JP2K),
  50001 (WebP), 50002 (JPEG XL), 60001 (AVIF — existing
  `CompressionAVIF` enum), 60003 (HTJ2K). wsi-tools
  ImageDescription parser hidden behind the existing
  `Tiler.Metadata()` / `generictiff.MetadataOf` accessors.
- **Behavior change:** none. Existing consumers using v0.13
  surfaces see no behavior change.
- **Active limitations:** unchanged from v0.13. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  `docs/deferred.md §1a`): unchanged from v0.13 (additive-only API
  doesn't introduce new deviations).
- **Correctness bar:** `make test` green. TestSlideParity total
  now 28 fixtures (was 24); 4 wsi-tools fixtures + tile magic byte
  pinning + per-fixture geometry.
- **Decoder responsibility:** byte-passthrough contract preserved
  per v0.8 IFE precedent. Consumers bring libwebp / libjxl /
  libavif / OpenJPEG-HTJ2K decoders.
- **Sealed Q-decisions (2):** Q1 four enum values total (3 new +
  existing AVIF; HTJ2K distinct from JP2K); Q2 parse populates
  standard fields only — no wsi-tools-specific public accessor.
- **Deferred forward:** L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9,
  R15. v1.0 cut still pending.
- **Design:** `docs/superpowers/specs/2026-05-08-opentile-go-v14-novel-codecs-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md`
- **Work branch:** `feat/v0.14`

## Previous milestone — v0.13 (shipped 2026-05-08)

Bandwidth-deduplication API — Level.TilePrefix() + TileBodyInto +
TileBodyMaxSize + opentile.SpliceJPEGTile helper. Additive (no
breaking changes). Pattern B savings depend on fixture-author
choice (shared JPEGTables tag 347 vs per-tile-embedded). Cross-
format byte-equality invariant verified; bench harness committed.

## Earlier milestones

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
