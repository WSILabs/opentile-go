# opentile-go

Direct Go port of [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Sectra AB) with one cgo dependency (libjpeg-turbo, narrowly scoped to `internal/jpegturbo/`). Reads tiles from WSI (whole-slide imaging) TIFF files used in digital pathology.

## Current milestone — v0.11 (shipped)

- **Scope:** Leica SCN milestone — first format reader exercising `Image.SizeC() > 1` on a real fixture (Leica-Fluorescence-1.scn's 3-channel separated fluorescence) and first multi-region "discontinuous scanning" reader (Leica-2.scn's 4 disjoint tissue rectangles composited into one slide canvas). 15 tasks across Batches A–E shipped: SCN XML parser, auxiliary-vs-main classifier + multi-main composer, Factory + Detection registered before generictiff, tiledRegion + compositeLevel multi-region dispatch, multi-channel TileAt, blank-tile fill, format-specific Metadata + MetadataOf, bio-formats CLI parity oracle, integration + geometry + cross-backing parity tests. Folded in: two `formats/generictiff` validator-cap relaxations (`MinLevels: 3 → 1` + `LeftoverTiledMaxAreaRatio: 0.01 → 0.05`) covering Grundium scanner output (single-level + mixed-ratio chains).
- **Headline coverage:** all 3 openslide-testdata SCN fixtures end-to-end (Leica-1, Leica-2, Leica-Fluorescence-1) plus 2 Grundium fixtures (scan_619 single-level, scan_620 mixed-ratio). `TestSlideParity` total now 24 fixtures (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE + 4 generic + 3 SCN). Composite L0 union extent on Leica-2 pinned at 44956×139277 px (4 vertically-stacked tissue regions).
- **API additions:** `opentile.FormatLeicaSCN` Format constant (`"leica-scn"`); `formats/leicascn` package (Factory + Tiler + Level + AssociatedImage + Metadata + MetadataOf); v0.7 multi-dim API (`Image.SizeC` / `ChannelName` / `Level.TileAt(TileCoord{C, X, Y})`) reused without additions.
- **Behavior change:** `opentile.OpenFile` now routes Leica SCN files to the new reader (registered after vendor formats, before generictiff). The two generictiff cap relaxations don't affect existing v0.10 fixtures (CMU-1.tiff and CMU-1.stripped.tiff classify identically); the new Grundium files now load.
- **Active limitations:** L4, L5, L14 Permanent (v0.6); L19, L20 v0.7-deferred; L23, L24, L25 v0.8-deferred; L26-L29 v0.10-deferred (generic-TIFF design Q-decisions); **L30, L31, L32, L33, L34 new** (Leica SCN design Q-decisions: multi-Z stack, AOI-cropped Tile variant, mismatched-objective regions, byte-equality oracle, 3-fixture coverage limit).
- **Deviations from upstream Python opentile** (canonical list at `docs/deferred.md §1a`): everything from v0.10 plus one v0.11 entry (Leica SCN reader for legacy SCN400 / SCN400F output).
- **Correctness bar:** `TestSlideParity` extended to 24 fixtures with sample-tile SHA fixtures committed for all 5 new files. New `tests/parity/leicascn_geometry_test.go` pins per-fixture geometry + per-channel TileAt distinct-bytes (3 distinct hashes on Fluorescence) + cross-backing parity (mmap vs pread). New `tests/oracle/leicascn_bf_test.go` (build tag `bfparity`) confirms structural equivalence vs bio-formats CLI for all 3 SCN fixtures. Generic geometry test extended with rows for both Grundium fixtures.
- **T8 lesson (multi-region tile alignment):** SCN's `<view offsetX/Y>` values in nm don't generally tile-align in composite-pixel-space (Leica-2's region 0 lands 71% of a tile off-grid). Resolution: tile-snap region offsets DOWN to nearest tile boundary at `compositeLevel` construction. Cost: composite position error ≤ one tile (~128 µm at 250 nm/px) — pathology-rendering-acceptable. Surfaced during T8 implementation; not anticipated in the design spec. Documented in `docs/formats/leicascn.md` "Position imprecision" + inline in `formats/leicascn/tiled.go`.
- **T12 spec divergence (generictiff scan_620 orphan):** v0.11 spec said the Grundium scan_620 orphan IFD "surfaces as an AssociatedImage". In practice the orphan is a tiled IFD, and `formats/generictiff`'s associated reader doesn't handle tiled associated images (errors with `errUnsupportedAssociatedShape`, silently dropped per the v0.10 §6 pattern). Documented in CHANGELOG and `docs/deferred.md §8e`. Multi-tile-associated remains out-of-scope.
- **Deferred forward:** L19, L20, L23, L24, L25, L26-L29, L30-L34, R4/R6/R9, R15. Consolidated list at `docs/deferred.md §11`.
- **Design:** `docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md`
- **Format doc:** `docs/formats/leicascn.md`
- **Work branch:** `feat/v0.11`

## Previous milestone — v0.10 (shipped 2026-05-06)

Generic-TIFF catch-all reader for tiled pyramidal TIFFs without vendor metadata. New package `formats/generictiff`; `opentile.FormatGenericTIFF` constant; pyramid validator + heuristic associated-image classifier in `internal/tiff.ClassifyPyramid` and `formats/generictiff.ClassifyAssociated`. New `"associated"` AssociatedImage Kind value as classifier fallback. Design + plan: `docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md`, `docs/superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md`. Format doc: `docs/formats/generictiff.md`.

## Earlier milestone — v0.9 (shipped 2026-05-01)

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
