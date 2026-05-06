# opentile-go

Direct Go port of [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Sectra AB) with one cgo dependency (libjpeg-turbo, narrowly scoped to `internal/jpegturbo/`). Reads tiles from WSI (whole-slide imaging) TIFF files used in digital pathology.

## Current milestone — v0.10 (shipped)

- **Scope:** Generic-TIFF milestone — first catch-all reader for tiled pyramidal TIFFs without vendor metadata. Fills the gap upstream Python opentile leaves (its factory list is enumerated vendor formats only). 15 tasks across Batches A–E shipped: structural pyramid validator (`internal/tiff.ClassifyPyramid`), heuristic associated-image classifier (`formats/generic.ClassifyAssociated`), Factory + Detection wired LAST in dispatch order, tiledImage Level (zero-alloc TileInto on JPEG-splice + LZW/JP2K/Deflate/None passthrough), AssociatedImage with multi-strip JPEG concat + multi-strip LZW re-encode paths, Tiler with format-specific Metadata, integration + geometry + cross-backing parity tests.
- **Headline coverage:** the v0.10 generic reader ships with full-walk SHA fixtures for `CMU-1.tiff` (9-level JPEG pyramid, 195 MB) and `CMU-1.stripped.tiff` (3-level pyramid + thumbnail/label/macro associated, 169 MB). Multi-strip JPEG concat verified empirically against libtiff's RST-marker layout (46 strips → 143,874 bytes valid JPEG). Multi-strip LZW reuses the SVS lzwlabel pattern.
- **API additions:** `opentile.FormatGeneric` Format constant (`"generic-tiff"`); `opentile.CompressionDeflate` Compression enum value; `formats/generic` package (Factory + Tiler + Level + AssociatedImage + Metadata); `internal/tiff.ClassifyPyramid` + `PyramidLevelInfo` (exported for reuse); new `"associated"` AssociatedImage Kind value as the heuristic-classifier fallback (documented in `image.go`'s interface docstring).
- **Behavior change:** `opentile.OpenFile` now routes pyramidal TIFFs without vendor metadata to the generic reader. Vendor detection order unchanged; the generic factory activates only when no vendor factory claims the file. Validation thresholds sealed: `MinLevels=3`, ±2% inter-axis, ±5% inter-level, ≥2 leftover tiled IFDs above 1% of baseline → reject as multi-pyramid.
- **Active limitations:** L4, L5, L14 Permanent (v0.6); L19, L20 v0.7-deferred; L23, L24, L25 v0.8-deferred; **L26, L27, L28, L29 new** (generic-TIFF design Q-decisions: stripped pyramid IFDs, multi-pyramid rejection, multi-strip JPEG `PlanarConfiguration=2`, pluggable associated-image classifier). Two v0.11 candidates surfaced mid-stream from real Grundium fixtures (single-level tiled TIFFs and mixed-ratio pyramid chains).
- **Deviations from upstream Python opentile** (canonical list at `docs/deferred.md §1a`): everything from v0.9 plus two v0.10 entries (generic-TIFF reader, `"associated"` Kind value).
- **Correctness bar:** `TestSlideParity` extended to 19 fixtures (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE + 2 generic). New `tests/parity/generic_geometry_test.go` pins per-fixture geometry + per-associated-image kind/size/compression/byte count + cross-backing parity (mmap default vs pread). Unit tests in `formats/generic/*_test.go` cover validator, classifier, factory, Level, AssociatedImage, and Tiler against synthetic + real fixtures.
- **T2 lesson:** initial heuristic classifier required width > height for "label". Probing CMU-1.stripped.tiff revealed its label is 387×463 (PORTRAIT). Revised heuristic: LZW compression alone is the dominant signal for label, not aspect ratio. Recorded as a memory; reinforces the "verify before asserting" feedback rule.
- **T8 lesson:** initial draft marked multi-strip JPEG as unsupported (Tier-3 deferred). Probing CMU-1.stripped.tiff revealed thumbnail (46 strips) and macro (27 strips) are both multi-strip JPEG. Verified that libtiff's default layout (single JPEG split at restart-marker boundaries) reproduces the original via simple concat — same pattern SVS / Philips / OME use. Path now supported.
- **Deferred forward:** L19, L20, L23, L24, L25, L26-L29, R4/R6/R9, R15, R16, plus v0.11 candidates (single-level tiled TIFFs + mixed-ratio pyramids — Grundium fixtures in hand). Consolidated list at `docs/deferred.md §11`.
- **Design:** `docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md`
- **Format doc:** `docs/formats/generic.md`
- **Work branch:** `feat/v0.10`

## Previous milestone — v0.9 (shipped 2026-05-01)

Sole-focus performance milestone — mmap-backed `OpenFile` default, pool-friendly `TileInto` + `TileMaxSize` API, in-place JPEG splice template, `Tiler.WarmLevel(i)` page-cache pre-warm. 8–145× speedup; 0 allocs on the pool TileInto path across every TIFF format + IFE. Design + plan + perf guide at `docs/superpowers/specs/2026-05-01-opentile-go-v09-perf-design.md`, `docs/superpowers/plans/2026-05-01-opentile-go-v09-perf.md`, `docs/perf.md`.

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
