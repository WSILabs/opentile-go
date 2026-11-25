# opentile-go v0.9 Performance Implementation Plan

> **For agentic workers:** Sequential in-thread execution per the v0.7
> closeout / v0.8 IFE precedent (the user is on remote control). Each
> task ends with a commit; batch boundaries are controller checkpoints.
> Tasks use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the perf recommendations from the project-internal
SVS-perf doc (`docs/opentile-go-svs-perf.md` §A) as a single
coordinated milestone:

- A.1 mmap-backed `OpenFile` (default; `WithBacking(BackingPread)`
  opt-out)
- A.2 `Level.TileInto(x, y, dst) (int, error)` + `Level.TileMaxSize() int`
- A.3 pre-built JPEG splice template per level (internal-only)
- A.4 `Tiler.WarmLevel(i int) error`
- A.5 concurrency-contract docs (incremental, inside A.1–A.4 commits)

All seven sign-off questions sealed in the design spec's §11 sign-off
log on 2026-05-01. Three sealed decisions worth restating up front:

- **Cross-platform mmap via `golang.org/x/exp/mmap`** (Linux + macOS +
  Windows in one dep).
- **Default `OpenFile` switches to mmap** with auto-fallback to
  `pread` on mmap failure; explicit `WithBacking(BackingPread)`
  opt-out.
- **Mandatory pre-flight benchmark gate** before any optimization
  lands; regressions on any format revert.

**Architecture:** Additive interface evolution. New top-level option
(`WithBacking`); new `Level` methods (`TileInto`, `TileMaxSize`); new
`Tiler` method (`WarmLevel`). No new cgo. `golang.org/x/exp/mmap` is
the only new dep.

**Tech Stack:** Go 1.23+, libjpeg-turbo (existing), `golang.org/x/exp/mmap`
(new). No new transitive surface.

**Spec:** [`docs/superpowers/specs/2026-05-01-opentile-go-v09-perf-design.md`](../specs/2026-05-01-opentile-go-v09-perf-design.md)
(7 sealed decisions, sign-off log §11).

**Reference doc:** [`docs/opentile-go-svs-perf.md`](../../opentile-go-svs-perf.md)
— project-internal SVS perf recommendations dropped 2026-05-01.

**Branch:** `feat/v0.9` (off `main` post-v0.8 merge).

**Sample slides:** Same as v0.8 — no new fixtures needed. Baseline
benchmark walks the existing parity slate.

**Python venv / parity oracle:** N/A. v0.9 is purely internal perf
work; no new format-correctness oracle. The existing `make parity`
oracle continues to gate byte-equality across all changes.

---

## Universal task contract: "confirm upstream first"

Every task starts with `Step 0: Confirm upstream` — names the
upstream rule that governs the behavior, states it, includes a
verification command. No task body proceeds until that command has
been run.

For v0.9 perf, "upstream" sources are layered:

1. **`docs/opentile-go-svs-perf.md`** — the reference doc dropped
   2026-05-01. Each §A item's implementation must match the doc's
   "what" / "impact" / "risks" framing or surface the deviation.
2. **`docs/superpowers/specs/2026-05-01-opentile-go-v09-perf-design.md`**
   — the project-internal design spec; the §11 sign-off log records
   the seven sealed decisions.
3. **The Go runtime + stdlib** — `syscall.Mmap` semantics (Linux,
   macOS), `pread(2)` behavior, `sync.Pool` discipline,
   `golang.org/x/exp/mmap` package docs. Read before guessing.
4. **Existing format packages** — `formats/{svs,ndpi,ome,philips,bif,ife}/`.
   `Level.Tile` impls share a similar shape; A.2's per-format
   reality (NDPI striped + OME OneFrame internal scratch) is real
   and needs honest treatment.
5. **`formats/ndpi/bench_test.go`** — pre-v0.9 NDPI per-tile
   throughput regression gate. Extended in T1 to cover every format.

When the doc and the existing impl disagree, follow the doc but
flag the deviation in the relevant task's commit message.

---

## Batch A — Baseline benchmark gate (1 task)

**Goal:** establish profiled per-format `Tile()` throughput +
allocation rate before any optimization lands. Without this, "the
mmap commit improved performance" is a guess.

- [ ] **T1 — `tests/parity/perf_baseline_test.go` + baseline JSON.**
  New file under `-tags benchgate`. Walks every fixture in the
  parity slate (CMU-1.svs, CMU-1.ndpi, Philips-1.tiff, Leica-1.ome.tiff,
  Ventana-1.bif, cervix_2x_jpeg.iris). Per fixture: warm-cache
  `Tile()` RPS (60 sec, 100 goroutines, randomized tile coordinates),
  `allocs/op` via `testing.B.ReportAllocs`, top-5 pprof CPU consumers
  via shellout to `go tool pprof -top -cum`. Result committed under
  `tests/fixtures/v0.9-baseline.json`. Each subsequent batch's tasks
  must re-run the gate and post before/after deltas in the commit
  message; regressions revert.

End-of-batch checkpoint: review baseline JSON, confirm sane numbers,
green-light Batch B.

---

## Batch B — A.1 mmap default + opt-out (3 tasks)

**Goal:** mmap-backed tile reads as the new default, with auto-fallback
to pread and explicit opt-out for callers that need it.

- [ ] **T2 — `internal/tiff/mmap.go` mmap-backed `io.ReaderAt`.**
  Wrap `golang.org/x/exp/mmap.Open(path)` in a struct that satisfies
  `io.ReaderAt + io.Closer`. ~30 LoC + table-driven unit tests. Add
  `golang.org/x/exp/mmap` to `go.mod`. No public API changes yet —
  this is just the building block.

- [ ] **T3 — `opentile.WithBacking(Backing) Option` + `Backing` enum.**
  Two values: `BackingMmap` (default), `BackingPread` (opt-out for
  callers that need `os.Open` semantics). New `ErrMmapUnavailable`
  sentinel for the auto-fallback path. Document the SIGBUS-on-
  truncation contract loudly in the new docstring. ~50 LoC.

- [ ] **T4 — Wire `OpenFile` to use mmap by default + auto-fallback.**
  `OpenFile` resolves the backing per the opt:
  - Default: try mmap; if it returns `ErrMmapUnavailable` (or any
    syscall error), fall back to `os.Open` and log a warning via
    `cfg.OnFallback` (new optional callback).
  - `WithBacking(BackingPread)`: skip mmap entirely; use `os.Open`.
  - `WithBacking(BackingMmap)`: try mmap; if it fails, return the
    error rather than falling back. (For callers who want to know.)
  Update `opentile_test.go` to drive both backings; verify byte-
  identical tile output across the parity slate. Re-run T1's
  baseline; SVS / OME tiled / Philips / BIF / IFE should show
  syscall.pread64 dropping out of the top-5; NDPI striped + OME
  OneFrame mostly unchanged.

End-of-batch checkpoint: `make test` + `make parity` + baseline
re-run all green; mmap is the default with no caller breakage.

---

## Batch C — A.2 TileInto + TileMaxSize (2 tasks)

**Goal:** caller-provided buffer variant for pool-friendly tile
reads.

- [ ] **T5 — `Level.TileInto(x, y int, dst []byte) (int, error)` +
  `Level.TileMaxSize() int` interface evolution.** Add both methods
  to the `Level` interface in `image.go`. Update `SingleImage` to
  delegate `Tile(x, y) → TileInto(x, y, make([]byte, l.TileMaxSize()))`.
  Per-format level impls grow `TileInto` (writes to caller's `dst`)
  and `TileMaxSize` (computed at level-open time, cached as a field):
  - **SVS / Philips / OME tiled / BIF / IFE**: ReadAt → splice (if
    applicable) → write to `dst`. `TileMaxSize` = `max(counts[i]) +
    len(prefix)` (prefix from A.3 lands in T8; for now use
    `len(jpegTables) + APP14_overhead`).
  - **NDPI striped + OME OneFrame**: existing internal-scratch path
    runs unchanged; `TileInto` does the final copy from scratch into
    `dst`. `TileMaxSize` = scratch tile size (already computed).
  Existing `Tile()` becomes a thin wrapper that allocates
  `TileMaxSize()` bytes and calls `TileInto`.

- [ ] **T6 — Cross-format byte-equivalence test** under
  `tests/parity/tileinto_test.go` (no build tag; runs in `make test`).
  Walks every fixture; for each level, samples ~50 (col, row);
  asserts `Tile(x, y)` and `TileInto(x, y, buf)` produce byte-
  identical output. `io.ErrShortBuffer` test for under-sized `dst`.
  Re-run T1 baseline with a `sync.Pool`-using benchmark variant;
  expect `allocs/op` drop on every format except NDPI striped + OME
  OneFrame (where internal scratch dominates).

End-of-batch checkpoint: `make test` + `make parity` + baseline
re-run; pool-aware benchmark shows the alloc drop.

---

## Batch D — A.3 splice template (2 tasks, conditional)

**Goal:** parse JPEGTables once at level-open time; per-tile splice
becomes memcpy + memcpy. Internal-only optimization (no public
`TilePrefix()` accessor — Q4 sealed).

**Conditional**: only land if T1's baseline pprof shows
`jpeg.InsertTablesAndAPP14` ≥ 5% of CPU on at least one format. If
the per-tile splice is already cheap, skip Batch D entirely (per
the reference doc's "Worth doing only if pprof shows it as hot"
guidance). Decision point at the start of T7.

- [ ] **T7 — Profile-driven decision: do we need A.3?**
  Run T1's pprof analysis on the SVS / Philips / BIF parity fixtures
  (the three formats that splice). If `jpeg.InsertTablesAndAPP14` is
  hot (≥ 5% of CPU on warm-cache 1k+ RPS), proceed to T8. If not,
  document the decision in `docs/deferred.md` and skip to Batch E.
  No code change; this task's deliverable is a commit message
  explaining the decision.

- [ ] **T8 — Per-level prefix-buffer cache.** (Conditional on T7.)
  Parse `JPEGTables` (and APP14 if applicable) at
  `newTiledImage`-equivalent time per format; build a prefix buffer
  (`SOI + APP14? + DQT + DHT`) once; store as an unexported `[]byte`
  field on the level struct. Per-tile splice in `TileInto` becomes:
  1. memcpy `prefix` into `dst`
  2. memcpy `tile_payload[2:]` into `dst[len(prefix):]` (skip the
     per-tile SOI marker)
  Affects SVS, Philips, OME tiled, BIF (per-IFD prefix because each
  pyramid level has its own JPEGTables). NDPI striped + OME OneFrame
  + IFE: not applicable.
  Byte-for-byte equivalence test against the current
  `InsertTablesAndAPP14` output via `tests/parity/splice_test.go`.

End-of-batch checkpoint: `make parity` byte-equality on every fixture
that uses splicing; T1 baseline shows `InsertTablesAndAPP14` dropping
out of top-5.

---

## Batch E — A.4 WarmLevel + final docs (2 tasks)

**Goal:** page-cache pre-warm hook + cumulative docs review.

- [ ] **T9 — `Tiler.WarmLevel(i int) error`.**
  New method on the `Tiler` interface. Walks `TileOffsets[i]`;
  touches one byte per `os.Getpagesize()`-sized region covering
  the tile-data spans. Under mmap: forces the kernel to populate
  the page cache. Under pread: same effect via `pread(1, off)`
  per page (slower; documented as best-effort). Returns
  `ErrLevelOutOfRange` on invalid `i`. ~80 LoC + table-driven test.
  Re-run T1 baseline with a "cold-cache" variant (drop kernel
  caches, then time first 100 tile reads on level 0 with vs without
  WarmLevel); pre-warm should bring the first-read latency in line
  with subsequent reads.

- [ ] **T10 — Cumulative concurrency-contract docs review (A.5).**
  Cross-cutting pass over `tiler.go`, `image.go`, and per-format
  level docstrings. Patch any gaps in:
  - mmap concurrency story (default-mmap implications, SIGBUS-on-
    truncation, no internal locks on tile reads).
  - `TileInto`'s `dst` ownership contract (caller-allocated, opentile
    writes, caller still owns).
  - Per-format concurrency notes (SVS / Philips / OME tiled / BIF /
    IFE: lock-free; NDPI striped: per-page mutex; OME OneFrame:
    extended-frame cache lock).
  - `WarmLevel` semantics (best-effort; under mmap = real;
    under pread = serial pread-per-page).
  - `Close()` must not race with in-flight reads (existing; reinforce
    for mmap).
  No behavior change; pure-docs commit. ~60 lines of edits across
  ~8 files.

End-of-batch checkpoint: `make vet` + `make test` + `make parity`
all green; baseline JSON shows the cumulative perf wins; docs ready
for merge.

---

## Batch F — Ship (3 tasks)

**Goal:** release prep.

- [ ] **T11 — `docs/deferred.md` updates.**
  - §1a deviations: add v0.9 entries (default mmap-backing, additive
    interface evolution adding `TileInto` / `TileMaxSize` /
    `WarmLevel`).
  - §11 Backlog: register that L19, L20, L23, L25, R4/R6/R9, R15,
    R16 are unchanged from v0.8 (they were re-triaged at v0.9-start
    per the user's instruction; v0.9 is sole-focus perf).
  - §8c (or new section): v0.9 retirement audit. Mirror v0.8's §8b
    structure. Record per-task baseline-vs-after deltas from T1.

- [ ] **T12 — `docs/formats/perf.md` (new) + README updates.**
  - New `docs/formats/perf.md` (or append to an existing doc):
    "Performance characteristics of opentile-go" — mmap default,
    backing opt-out, TileInto + sync.Pool guidance, WarmLevel,
    measured baselines from T1.
  - README updates: opening line mentions "memory-mapped tile reads
    by default"; usage example shows `opentile.OpenFile(path)` with
    a note about mmap. Deviations table gains the v0.9 entries.

- [ ] **T13 — `CHANGELOG.md [0.9.0]` + `CLAUDE.md` milestone bump.**
  - CHANGELOG: new `[0.9.0]` heading. Added: mmap default,
    `WithBacking`, `BackingMmap`/`BackingPread`, `TileInto`,
    `TileMaxSize`, `WarmLevel`, `ErrMmapUnavailable`,
    `golang.org/x/exp/mmap` dep. Changed: `OpenFile` now
    mmap-backed by default; `Level` interface gains 2 methods
    (additive); `Tiler` interface gains 1 method (additive).
    Notes: A.3 splice template inclusion conditional on T7's
    profile decision.
  - CLAUDE.md: milestone bump v0.8 → v0.9. New scope, active
    limitations (unchanged from v0.8), deviations, sample slides
    (unchanged).

End-of-batch checkpoint: final validation sweep — `make vet`,
`make test`, `make cover`, `make parity`, baseline gate clean.
Hand back for tag + merge + release.

---

## Risk notes

- **Default-mmap silently changes platform-specific failure modes.**
  Existing `OpenFile` callers on FUSE mounts that don't support mmap
  will now silently fall back to pread (with an optional warning via
  `cfg.OnFallback`). Document this loudly; the auto-fallback is
  designed for exactly this case.
- **SIGBUS on file truncation.** WSI files don't get truncated
  under normal use; if they do, the process crashes. Documented in
  `OpenFile`'s docstring and in `docs/formats/perf.md`. Consumers
  that operate on writable storage should opt out via
  `WithBacking(BackingPread)`.
- **`golang.org/x/exp/mmap` is a Go-team subrepo with an `exp/`
  label.** The label is a Go-team versioning convention, not a
  "may break" warning — the package has been stable for 8+ years.
  We pin the version in `go.mod` so future `exp/` API churn (extreme
  edge case) doesn't leak in unexpectedly.
- **Per-format internal scratch is unchanged.** NDPI striped + OME
  OneFrame still allocate per-page assembled-frame buffers. v0.9
  doesn't redesign those paths; `TileInto` for those formats is a
  thin wrapper that copies from scratch.
- **Profile-driven gate may revert tasks.** T8 (A.3) is conditional
  on T7's pprof decision. If real workloads don't hit the splice
  hot path, A.3 is documented-and-skipped. Reference doc anticipated
  this; v0.9 honors it.
- **Baseline gate (T1) requires ~2 GB free RAM** for the cervix
  fixture's mmap warm-up. CI environments without the fixture skip
  cleanly via the existing `OPENTILE_TESTDIR not set` guard.

---

## Out of scope for v0.9

Per the user's "sole focus of v0.9" instruction:

- L19 (openslide BIF pixel-equivalence) — research-driven; v0.7 work
  item.
- L20 (DP 600 unverified) — fixture-driven; v0.7 work item.
- L23 (IFE cross-tool parity vs `tile_server_iris`) — trigger-
  driven; v0.8 work item.
- L25 (IFE ANNOTATIONS block parsing) — fixture-driven; v0.8 work
  item.
- R4 / R9 (SVS corrupt-edge reconstruct + JP2K) — parked at issue
  #1.
- R6 (3DHistech TIFF) — parked at issue #2.
- R15 (Sakura SVSlide) — parked at issue #3.
- R16 (Leica SCN) — no issue; mentioned as a v0.8 candidate.
- §B consumer-side recommendations (sync.Pool buffers, pre-warm at
  startup, tune the tile cache, expose perf metrics) — out of
  opentile-go's scope; for whatever software calls opentile-go.
- `Level.TilePrefix() []byte` accessor — A.3 follow-on; lands when
  an §B consumer concretely needs it.
- Zero-copy `Level.TileBorrow(x, y) ([]byte, func(), error)` —
  mmap-aliasing variant. Door left open via A.5 docs; not
  implemented in v0.9.

These get re-triaged after v0.9 ships; documented in `docs/deferred.md`
§11 (consolidated backlog).
