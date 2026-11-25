# opentile-go v0.9 Performance Design Spec

**Status:** Draft, 2026-05-01. Sole focus of v0.9: implement the
SVS-perf recommendations from
[`docs/opentile-go-svs-perf.md`](../../opentile-go-svs-perf.md) §A.
All other deferred features (L19, L20, L23, L25, R4/R6/R9, R15, R16)
are pushed to an unscheduled backlog re-triaged post-v0.9.

**Predecessors:** v0.1 – v0.8 (all five TIFF formats + Iris IFE
shipped). Public API stable from v0.3.

**Reference doc:** [`docs/opentile-go-svs-perf.md`](../../opentile-go-svs-perf.md)
— project-internal recommendations (350 lines, dropped 2026-05-01).
§A is opentile-go scope; §B is consumer-side and explicitly out of
opentile-go's scope.

---

## 1. One-paragraph scope

v0.9 lands the §A items as a single coordinated milestone:

- **A.1 `OpenFileMmap` constructor** — memory-mapped backing for tile
  reads. Eliminates one `pread(2)` syscall per `Tile()` call.
  Foundational; other items lean on it.
- **A.2 `Level.TileInto(x, y, dst) (int, error)` + `TileMaxSize()`** —
  caller-provided buffer variant. Lets consumers pool tile buffers,
  removes per-tile allocations.
- **A.3 Pre-built JPEG splice template** — parse `JPEGTables` once at
  level open; per-tile splice becomes memcpy + memcpy. Internal
  optimization; opportunistic per the reference doc's pprof-gating
  guidance.
- **A.4 `Tiler.WarmLevel(i)`** — page-cache pre-warm hook for mmap
  backings. Eliminates first-fault latency for warm-by-design
  workloads.
- **A.5 Concurrency-contract docs** — pin the safe-for-concurrent-use
  guarantee per format (SVS lock-free, NDPI striped per-page mutex,
  etc.) so consumers can reason about scaling.

Out of scope: §B (consumer-side recommendations belong to whatever
software calls opentile-go); the deferred-feature backlog (L19, L20,
L23, L25, R4/R6/R9, R15, R16 — re-triaged post-v0.9).

## 2. Universal task contract: "confirm upstream first"

Same as v0.4–v0.8: every plan task starts with `Step 0: Confirm
upstream`. For v0.9 perf, "upstream" sources are layered:

1. **`docs/opentile-go-svs-perf.md`** — the reference doc. Every
   §A item's implementation must match the doc's "what" / "impact"
   / "risks" framing or surface the deviation.
2. **The Go runtime + stdlib** — `syscall.Mmap` semantics (Linux,
   macOS), `pread(2)` behavior, `sync.Pool` discipline. Read the
   relevant package docs before guessing.
3. **Existing format packages** — `formats/svs/`, `formats/ndpi/`,
   `formats/ome/`, `formats/philips/`, `formats/bif/`, `formats/ife/`.
   `Level.Tile` impls all share a similar shape — additive `TileInto`
   + per-format internal-buffer reality (NDPI striped, OME OneFrame)
   needs honest treatment.
4. **`formats/ndpi/bench_test.go`** — already a regression gate for
   NDPI per-tile throughput. v0.9 extends it to other formats and
   uses it as the v0.9 baseline-then-improvement metric.

When the doc and the existing impl disagree, follow the doc but
flag the deviation in a JIT gate's findings.

---

## 3. Architectural foundations (sealed)

These are the sealed decisions that frame everything else; they're
locked because the alternatives don't make sense given the v0.3
public-API stability invariant.

| § | Decision |
|---|----------|
| 3.1 | **Additive evolution only.** New constructors (`OpenFileMmap`),
new `Level` methods (`TileInto`, `TileMaxSize`), new `Tiler` method
(`WarmLevel`). Existing `OpenFile` / `Tile()` / etc. unchanged. No
caller breakage. |
| 3.2 | **Cross-format coverage where the architecture allows.**
A.1 (mmap) is `io.ReaderAt`-level — applies to every format.
A.4 (WarmLevel) iterates `TileOffsets` arrays — applies to every
format. A.5 (docs) covers every format's concurrency story. A.2
(TileInto) and A.3 (splice template) have format-specific
caveats addressed below. |
| 3.3 | **No new cgo.** mmap is `syscall.Mmap`; no libavif, no
`io_uring`, no SIMD adventures. Reference doc's "What not to bother
with" stands. |
| 3.4 | **Profile-driven, not theoretical.** v0.9 ships a baseline
benchmark gate before any optimization lands. Each §A item proves
its value via a before/after pprof snapshot or is reverted. The
reference doc's "Without numbers, all of this is theoretical." is
the milestone's correctness bar (alongside the existing `make
test` / `make cover` / `make parity` gates). |

## 4. A.1 — `OpenFileMmap` constructor

Mirror `OpenFile`'s shape. Wraps a `syscall.Mmap` byte slice in an
`io.ReaderAt` adapter; the existing tile read path (`r.ReadAt`)
becomes a userspace memcpy from RAM rather than a `pread(2)` syscall.

```go
// In opentile.go (sketch)
func OpenFileMmap(path string, opts ...Option) (Tiler, error) { ... }

// In internal/tiff/mmap.go (sketch)
type mmapFile struct {
    data []byte
    f    *os.File
}
func (m *mmapFile) ReadAt(p []byte, off int64) (int, error) { ... }
func (m *mmapFile) Close() error { /* munmap + file close */ }
```

**Closure.** The Tiler returned by `OpenFileMmap` owns both the
mapping and the underlying `*os.File`; `Tiler.Close()` munmaps and
closes the file. Munmap-while-in-flight-read is a SIGBUS waiting to
happen — the existing v0.3 docs already say "Close must not race
with in-flight reads"; v0.9 reinforces this in A.5.

**SIGBUS strategy.** WSI files don't get truncated underneath us in
practice. Loud failure (signal handler doesn't try to recover; the
process crashes, oncall investigates) beats silent corruption. v0.9
documents the assumption; doesn't add a SIGBUS recovery path.

**MAP_POPULATE.** Off by default. Multi-GB SVS files don't benefit
from eager population. Lazy page-fault is what we want; A.4
(`WarmLevel`) is the explicit eager-warm hook for callers that want
it.

**Error propagation.** New sentinel `ErrMmapUnavailable` for the
"this platform doesn't support mmap, fall back to OpenFile" path.
Returned only when the platform genuinely can't mmap; not for
runtime mmap failures (those propagate the underlying errno).

## 5. A.2 — `Level.TileInto(x, y, dst) (int, error)` + `TileMaxSize()`

Add two methods to the `Level` interface:

```go
type Level interface {
    // existing...
    Tile(x, y int) ([]byte, error)
    TileAt(coord TileCoord) ([]byte, error)
    // new in v0.9:
    TileInto(x, y int, dst []byte) (int, error)
    TileMaxSize() int
}
```

`Tile()` becomes a thin wrapper that allocates `TileMaxSize()` bytes
and calls `TileInto`. The implementation lives in one place per
format.

**`TileMaxSize()` semantics.** The maximum byte length any tile on
this level can return through `TileInto`. Computed at level-open
time and cached; for SVS / BIF that's `max(counts[i]) +
len(prefix)` (where `prefix` is the spliced header from A.3 or, in
its absence, `len(jpegTables) + APP14_overhead`). For Philips it's
the same logic plus `len(blank_tile_jpeg)` since the sparse-fill
tile is uniform-size. For IFE it's `max(counts[i])` (tiles are
self-contained — no splice). For NDPI striped + OME OneFrame, see
the format-specific note below.

**`io.ErrShortBuffer` contract.** If `len(dst) < TileMaxSize()`,
`TileInto` returns `0, io.ErrShortBuffer` without doing any I/O.
Document the relationship in `Level.TileMaxSize()` so callers
understand it's a sizing hint, not a per-tile exact length.

**Per-format implementation reality:**

| Format | TileInto path | Pool-friendly? |
|---|---|---|
| SVS | tile bytes ReadAt → optional splice → output buffer | yes; `dst` receives the spliced output |
| Philips | sparse-fill check → ReadAt → splice → output | yes |
| OME tiled | ReadAt → optional splice → output | yes |
| BIF | ReadAt → splice (per-page tables) → serpentine remap is index-side | yes |
| IFE | ReadAt → output (no splice) | yes |
| NDPI striped | per-page assembled-frame is internal scratch; `TileInto` copies a slice out | yes for the output, no for the internal buffer (already shared via per-page mutex) |
| OME OneFrame | extended-frame is internal scratch; `TileInto` copies out | same as NDPI striped |

For the two internal-scratch formats (NDPI striped, OME OneFrame),
`TileInto` is a thin wrapper: still allocates internally, copies
into `dst` at the end. The per-tile allocation savings are smaller
there but the API stays uniform. Callers benefit at the boundary
even if the format internally still allocates.

## 6. A.3 — Pre-built JPEG splice template per level

Parse `JPEGTables` (and APP14 if applicable) at level-open time;
build a prefix buffer (`SOI + APP14? + DQT + DHT`) once; per-tile
splice becomes "memcpy prefix into dst" + "memcpy tile_payload[2:]
into dst" (skipping the per-tile SOI marker).

**Where the prefix lives.** Field on the per-format level struct
(unexported). Treated as immutable — set at level open, never
reassigned, never sliced-mutably. Cross-goroutine safe under v0.3's
"populated at Open() time and immutable thereafter" hot-path
contract.

**Optional `Level.TilePrefix() []byte` accessor.** Out of scope for
v0.9 unless an §B consumer is concretely waiting for it.
Reference-doc framing is "side effect" — leave as a follow-on
unless a real consumer signals demand. v0.9 ships the internal
optimization; the public accessor lands when someone needs it.

**Formats:**
- **SVS** (JPEG + APP14, the existing
  `jpeg.InsertTablesAndAPP14` path): yes.
- **Philips** (JPEG, the existing `jpeg.InsertTables` path,
  no APP14): yes.
- **BIF** (per-IFD JPEG tables, splice on the spec-compliant path):
  yes; per-IFD prefix because each pyramid level has its own
  JPEGTables.
- **OME tiled** (per-IFD JPEG tables when present): yes.
- **NDPI striped, OME OneFrame**: not applicable. NDPI tiles are
  reassembled, not table-spliced; OME OneFrame is a one-shot read.
- **IFE**: not applicable. Tiles are self-contained.

## 7. A.4 — `Tiler.WarmLevel(i int) error`

Method on `Tiler` (chosen over `Level` because pre-warming is a
slide-level operation; iterating multiple levels via
`for _, l := range t.Levels() { l.WarmLevel() }` is awkward when
the natural caller usage is "open this slide, prewarm it all").

**Mmap backing.** Walks `TileOffsets` for level `i`; touches one
byte per `os.Getpagesize()`-sized region covering the tile-data
spans. Forces the kernel to populate the page cache lazily.

**Non-mmap backing.** Falls back to `pread(2)` of one byte per page
region — same effect, slower (it's still a syscall per page, but
typically the kernel readahead already kicked in by the second page,
so this isn't catastrophic). Documented as best-effort under
`OpenFile`-backed Tilers.

**Error policy.** Returns the first I/O error encountered (or nil).
Callers that want to ignore errors (it's a hint, after all) can
discard the result.

**Concurrent-warming.** WarmLevel itself is `O(npages)` and serial;
parallelism across multiple levels or multiple slides is the
caller's job.

## 8. A.5 — Concurrency-contract docs

Update the docstring on `Tiler` (and the format-specific `Level`
impls) to specify exactly:

- Returned `Tile()` byte slices are caller-owned (existing behavior;
  caller may modify them; opentile-go never reads them after return).
- `TileInto`'s `dst` slice contract: caller-allocated, opentile-go
  writes to it; caller still owns it after return.
- For SVS / Philips / OME tiled / BIF / IFE: zero internal locks on
  Tile / TileInto; goroutine count limited only by OS file
  descriptors (under `OpenFile`) or address-space ceiling (under
  `OpenFileMmap`) and page-cache capacity.
- For NDPI striped path: per-page mutex; concurrent reads of
  *different* pages are concurrent, concurrent reads of the *same*
  page serialize on the assembled-frame cache lock.
- For OME OneFrame: same as NDPI striped — internal scratch + lock.
- `Close()` must not race with in-flight reads (already documented
  in v0.3; reinforced for mmap).
- mmap-backed Tilers: tile bytes returned by `Tile()` are
  freshly-allocated copies (the existing contract). `TileInto`
  writes into the caller's buffer. Neither aliases the underlying
  mapping. Documenting this lets future zero-copy work (out of
  scope for v0.9) define a separate borrowed-slice API without
  ambiguity.

## 9. Active limitations parked for later milestones

These are scoped out for v0.9 deliberately. None are perf-related;
they go into the unscheduled backlog (`docs/deferred.md` §11 once
written) for re-triage post-v0.9:

- **L19** — openslide BIF pixel-equivalence (research-driven;
  v0.7 work item).
- **L20** — DP 600 unverified (fixture-driven; v0.7 work item).
- **L23** — IFE cross-tool parity vs `tile_server_iris`
  (trigger-driven; v0.8 work item).
- **L25** — IFE ANNOTATIONS block parsing (fixture-driven; v0.8
  work item).
- **R4 / R9** — SVS corrupt-edge reconstruct + JP2K decode/encode
  (parked at issue #1).
- **R6** — 3DHistech TIFF support (parked at issue #2).
- **R15** — Sakura SVSlide support (parked at issue #3).
- **R16** — Leica SCN support (no issue; mentioned as a v0.8
  candidate, not picked up).
- **`Level.TilePrefix() []byte` accessor** — A.3 follow-on; lands
  when an §B consumer concretely needs it.
- **Zero-copy `TileBorrow(x, y) ([]byte, func(), error)`** —
  borrowed-slice variant that aliases the mmap, paired with a
  release callback. The reference doc hints at this but doesn't
  scope it; v0.9 leaves the door open via A.5's doc tightening
  but doesn't ship the API.

## 10. Open questions for sign-off

Each provisional answer reflects what I'd choose absent input;
walked through one at a time per the v0.7 / v0.8 sign-off pattern.

| § | Question | Provisional answer |
|---|----------|---------------------|
| Q1 | Windows mmap support? Stdlib's `syscall.Mmap` is Linux/macOS only; full cross-platform support would add `golang.org/x/exp/mmap` (single-file dep, no transitive surface). | **All three platforms via `golang.org/x/exp/mmap`** (sealed 2026-05-01). Single Go-team subrepo dep; same `syscall.Mmap` path on Linux/macOS, canonical `CreateFileMapping`/`MapViewOfFile` on Windows. ~30 LoC of glue in `internal/tiff/mmap.go`; the package's `*ReaderAt` already implements `io.ReaderAt + io.Closer`. Avoids ~150 LoC of per-platform code we'd otherwise have to maintain without Windows CI. |
| Q2 | Default `OpenFile` switches to mmap, or strictly opt-in via `OpenFileMmap`? | **Default switches to mmap** (sealed 2026-05-01). The perf win is universal; existing callers should get it automatically. Three implications: (a) `OpenFile` is mmap-backed by default; (b) auto-fallback to `pread` if mmap fails for the file (FUSE mount, weird fs); (c) explicit opt-out via `WithBacking(BackingPread)` option for the rare caller that wants `os.Open` semantics. SIGBUS-on-truncation documented in the `OpenFile` docstring as the explicit failure mode (WSI files don't get truncated in practice; loud crash beats silent corruption). v0.3 API-stability invariant honored via owner sign-off (this turn). |
| Q3 | Pre-flight benchmark gate (Batch A) — establish a baseline pprof + per-format throughput before any optimization lands, mirror the v0.8 JIT-verification gates? | **Yes** (sealed 2026-05-01). `tests/parity/perf_baseline_test.go` gated by `-tags benchgate` (off by default in `make test`); walks every fixture in the parity slate; captures warm-cache `Tile()` RPS, `allocs/op`, and top-5 pprof CPU consumers per fixture. Baseline JSON committed under `tests/fixtures/v0.9-baseline.json`. Each task's commit must re-run the gate and show before/after for the relevant metric; regressions on any format are reverted before that task is considered complete. |
| Q4 | Consumer-side `Level.TilePrefix() []byte` accessor (A.3 side effect from the reference doc)? | **No** (sealed 2026-05-01). Internal-only optimization for v0.9. Public accessor lands when an §B consumer concretely asks for it — adding methods is additive; removing them is breaking. YAGNI applies. |
| Q5 | A.4 WarmLevel — `Tiler.WarmLevel(i)` or `Level.Warm()` (no args)? | **`Tiler.WarmLevel(i int) error`** (sealed 2026-05-01). Caller ergonomics over OOP cohesion — most common caller usage is "warm L0 (and maybe L1) at slide-open time," which is one line per level. Invalid `i` returns `ErrLevelOutOfRange`. A `Tiler.Warm()` shorthand for "warm every level" can land additively in v0.10 if a use case surfaces. |
| Q6 | A.2 `TileInto` semantics on the two internal-scratch formats (NDPI striped + OME OneFrame): copy from the existing internal buffer (transparent; same perf as `Tile()`) or error/skip? | **Copy from internal buffer** (sealed 2026-05-01). Uniform API across all six formats; pool savings at the boundary still apply even if the format internally still allocates (amortized scratch). NDPI striped's pre-existing per-page mutex behavior is unchanged. |
| Q7 | Sequencing — A.1 first (foundational), then A.2 + A.3 + A.4 in any order, then A.5 docs? Or should A.5 (concurrency-contract docs) land alongside A.1 (since the new mmap contract is documented in A.5)? | **Docs land incrementally inside A.1–A.4 commits** (sealed 2026-05-01); the plan's final task is a cross-cutting review pass to patch any gaps in the cumulative concurrency-contract docs. Each implementation commit owns the docstring updates relevant to its change. |

After sign-off, this becomes the executable spec; a follow-up plan
doc lays out the per-task batches.

## 11. Sign-off log

| Date | § | Decision | Owner |
|------|---|----------|-------|
| 2026-05-01 | 3 | Architectural foundations sealed (additive only, cross-format where possible, no new cgo, profile-driven) | Toby (implicit on scoping) |
| 2026-05-01 | 10 Q1 | Windows + Linux + macOS via `golang.org/x/exp/mmap` (single Go-team subrepo dep) | Toby |
| 2026-05-01 | 10 Q2 | Default `OpenFile` switches to mmap; auto-fallback to pread on mmap failure; explicit `WithBacking(BackingPread)` opt-out for the pread path | Toby |
| 2026-05-01 | 10 Q3 | Mandatory pre-flight benchmark gate (Batch A); regressions revert | Toby |
| 2026-05-01 | 10 Q4 | A.3 splice template is internal-only; no public `TilePrefix()` accessor in v0.9 | Toby |
| 2026-05-01 | 10 Q5 | `Tiler.WarmLevel(i int) error`; `ErrLevelOutOfRange` on invalid `i`; `Tiler.Warm()` shorthand deferred until requested | Toby |
| 2026-05-01 | 10 Q6 | `TileInto` on NDPI striped + OME OneFrame copies from internal scratch buffer; uniform API across formats | Toby |
| 2026-05-01 | 10 Q7 | Docstring updates land incrementally inside A.1–A.4 commits; final task is a cross-cutting review pass | Toby |

(Rest of the table fills in as the open questions are signed off.)
