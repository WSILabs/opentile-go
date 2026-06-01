# SVS performance recommendations for opentile-go and its consumers

**Audience.** Maintainers of [`opentile-go`](https://github.com/wsilabs/opentile-go) and authors of HTTP / desktop / pipeline consumers that read SVS pyramids through it.

**Scope.** Aperio SVS files specifically. Most ideas transfer to NDPI but NDPI's striped path has different concurrency characteristics (per-page mutex) and is called out where it matters.

**Status.** Recommendations, not a binding plan. Pick what's useful for your workload. Predates the v0.26+ work; for the `ScaledStrips` region/DZI iterator and byte-bounded memory budgeting (v0.30) plus the cross-format benchmark suite, see [`perf.md`](./perf.md).

---

## Background — what opentile-go does today

For an SVS file, opening via `opentile.OpenFile(path)` does the following, once:

1. Opens the OS file (`os.Open`), keeps the handle alive for the Tiler's lifetime.
2. Parses the TIFF header and walks every IFD eagerly into a `[]*Page`.
3. For each pyramid level, reads `TileOffsets`, `TileByteCounts`, and `JPEGTables` (tag 347) and caches them as fields on the level struct.
4. Builds the level list, wires associated images (thumbnail/label/macro), records ICC profile + Aperio metadata.

After that, `Level.Tile(x, y) ([]byte, error)` is:

```
idx    := y*grid.W + x        // O(1) array math
length := counts[idx]          // cached lookup
off    := offsets[idx]         // cached lookup
buf    := make([]byte, length) // allocation
ReadAtFull(reader, buf, off)   // single pread(2) syscall
+ optional jpeg.InsertTablesAndAPP14(buf, jpegTables)  // if JPEG-compressed
```

**Key facts:**

- The hot path has zero internal locks. SVS Tile() is lock-free.
- `*os.File.ReadAt` uses `pread(2)`, which doesn't touch the file's seek offset. Multiple goroutines can call it concurrently against the same FD; the kernel I/O scheduler is the only point of serialization.
- The Tiler is documented as concurrent-safe for accessors. `Close()` must not race with in-flight reads.

**Therefore, at the simplest scale, opentile-go is already fast and correctly concurrent.** The optimizations below address two regimes the hot path doesn't:

- **High-RPS HTTP serving** (1000+ tile/s warm cache) — bottleneck shifts to syscall + allocation overhead.
- **Cold-cache latency** (first user touching a fresh region) — bottleneck is one I/O round-trip per tile, paid sequentially or in small bursts.

---

## Where time actually goes at scale

For each `Tile()` call, the per-tile costs are:

| Cost | Magnitude | Ducks at low RPS / single user? |
|---|---|---|
| Bounds check | ~5 ns | yes |
| Two array lookups (offsets, counts) | ~5 ns | yes |
| `make([]byte, length)` | ~50–200 ns + GC | yes (GC tail) |
| `pread(2)` syscall | ~1–10 µs (warm cache) to 1+ ms (cold) | depends |
| Memcpy of tile bytes from kernel | ~1 µs / 17 KB | yes |
| `InsertTablesAndAPP14` (parse + splice) | ~500 ns – 2 µs | yes (JPEG only) |
| Second `make` for spliced output | ~50–200 ns + GC | yes (JPEG only) |

At 1700 RPS, you do all of this 1700 times per second. The total adds up to small numbers but persistent allocation pressure and steady kernel-mode time.

To know which costs actually matter for your workload, profile a 1-minute warm-cache run with `go tool pprof` and look at:

- **`runtime.mallocgc`** → allocation pressure → §A.2 (TileInto) + caller-side pool
- **`syscall.pread64`** → syscall-bound → §A.1 (mmap)
- **`jpeg.InsertTablesAndAPP14`** → splice-bound → §A.3 (pre-built template)

---

## Section A — Changes inside `opentile-go`

Each item has: **what** / **expected impact** / **effort** / **risks**.

### A.1 Add `OpenFileMmap` constructor (highest leverage)

**What.** A constructor parallel to `OpenFile` that maps the file read-only via `syscall.Mmap` and wraps the resulting byte slice in an `io.ReaderAt` adapter. The existing tile read path (`tiff.ReadAtFull` → `r.ReadAt`) becomes a userspace memcpy from RAM instead of a `pread(2)` syscall.

```go
// In opentile.go (sketch — not committed code)
func OpenFileMmap(path string, opts ...Option) (Tiler, error) { ... }

// In internal/tiff/mmap.go (sketch)
type MmapFile struct { data []byte }
func (m *MmapFile) ReadAt(p []byte, off int64) (int, error) {
    if off >= int64(len(m.data)) { return 0, io.EOF }
    n := copy(p, m.data[off:])
    if n < len(p) { return n, io.EOF }
    return n, nil
}
func (m *MmapFile) Close() error { return syscall.Munmap(m.data) }
```

**Impact.**

- Eliminates one syscall per `Tile()` call. At 1600 RPS that's ~1600 fewer context switches per second.
- Once the kernel has paged in the relevant SVS regions, tile reads hit RAM at memory-bandwidth speed.
- For sequential / clustered access patterns (typical viewers), pages adjacent to a touched tile are pre-faulted by kernel readahead, making subsequent tiles in the same region effectively free.
- Enables future zero-copy returns (§A.2 variant + §A.5 contract).

**Effort.** ~150 LoC plus tests. Helper file `internal/tiff/mmap.go`. Top-level `opentile.OpenFileMmap` mirrors `OpenFile`. Closing the Tiler must unmap.

**Risks.**

- **SIGBUS** if the file is truncated underneath you. WSI files don't get truncated in practice — note this in the docs and don't try to recover. Loud failure beats silent corruption.
- **Address-space pressure on 32-bit** — irrelevant for pathology workloads, but document.
- Don't use `MAP_POPULATE` by default. For multi-GB SVS, populating the whole file at open is wasteful. Plain `mmap` lets the page-fault handler do the work lazily — that's what you want.
- Windows + macOS support: Linux `syscall.Mmap` works on all three, but page-fault behavior and SIGBUS semantics differ. Test on each.

### A.2 Add `TileInto(x, y int, dst []byte) (int, error)`

**What.** Caller-provided buffer variant of `Tile()`. Writes the (optionally spliced) tile bytes into `dst`, returns bytes written. Returns `io.ErrShortBuffer` if `len(dst)` is insufficient. Add `Level.TileMaxSize() int` so callers can size pool buckets correctly.

```go
type Level interface {
    Tile(x, y int) ([]byte, error)              // existing
    TileInto(x, y int, dst []byte) (int, error) // new
    TileMaxSize() int                            // new
    // ...
}
```

`Tile()` becomes a thin wrapper that allocates and calls `TileInto`.

**Impact.**

- Lets high-RPS callers pool tile buffers via `sync.Pool`, eliminating both per-tile allocations (the read buffer + the JPEG-splice output) and corresponding GC pressure.
- For desktop viewers doing per-frame paints, lets a single per-paint scratch buffer service many tiles.
- At 1700 RPS × ~17 KB/tile, that's ~30 MB/s of allocation churn that goes away when callers pool.

**Effort.** ~50 LoC plus a parametrized roundtrip test pinning `Tile()` and `TileInto()` to the same byte output.

**Risks.**

- API surface grows. Keep both methods in sync; have `Tile()` delegate to `TileInto` so the implementation lives in one place.
- Callers that under-size the buffer get `io.ErrShortBuffer`. Document `TileMaxSize()` clearly. For SVS, max size is bounded by `max(counts[i]) + len(jpegTables) + APP14_overhead`; compute once at level open.

### A.3 Pre-build the per-level JPEG splice template

**What.** Today, `jpeg.InsertTablesAndAPP14(buf, jpegTables)` parses the JPEG tables blob (DQT/DHT) on every `Tile()` call. The tables are constant per level — parse them once at `newTiledImage` time, build a prefix buffer (`SOI + APP14 + DQT + DHT`), store it on the level struct. Per-tile splice becomes "memcpy prefix" + "memcpy tile_payload[2:]" (skip the per-tile SOI), no parsing.

**Impact.**

- Halves CPU time in the JPEG-splice path. Worth doing only if pprof shows `InsertTablesAndAPP14` as hot — at >1k RPS warm cache, plausibly visible.
- Side effect: the prefix buffer can be returned via a new `TilePrefix() []byte` accessor so callers that want to assemble bytes directly into a network buffer get them.

**Effort.** ~40 LoC plus a byte-for-byte equivalence test against the current `InsertTablesAndAPP14` output.

**Risks.**

- The prefix buffer is shared across goroutines reading the same level. Treat as immutable — store as a `[]byte` field never reassigned, never sliced-mutably.

### A.4 `WarmLevel(i int) error` hook

**What.** A method on Tiler (or Level) that, under mmap, walks the tile-offset table for level `i` and touches one byte per memory page covering the tile data. Forces the kernel to populate the page cache. No-op under non-mmap backings (or could fall back to a series of small `pread`s).

```go
type Tiler interface {
    // ...existing...
    WarmLevel(i int) error  // new (optional, can be on Level instead)
}
```

**Impact.**

- Eliminates first-fault latency for warm-by-design workloads (server-side pre-warming, desktop viewer pre-loading the slide the user is about to open).
- Negligible for casual single-shot use.

**Effort.** ~30 LoC. Requires §A.1 to be useful. Trivial under non-mmap (no-op).

**Risks.** None of substance — it's a hint. Could potentially blow the page cache on a memory-constrained host if called for every level of every slide; that's the caller's policy choice.

### A.5 Tighten the concurrency contract docs

**What.** Update the docstring on `Tiler` to specify exactly:

- Returned `Tile()` byte slices are caller-owned (current behavior — caller may modify them).
- For SVS specifically: zero internal locks; goroutine count is limited by OS file descriptors and page cache capacity, not opentile.
- For NDPI striped path: per-page mutex; concurrent reads of *different* pages are concurrent, concurrent reads of the *same* page serialize.
- `Close()` must not race with in-flight tile reads. (Already documented; keep.)
- If §A.1 lands and any borrowed-slice API is added later, document the alias-the-mapping rule explicitly.

**Impact.** Documentation only. Lets consumers reason about their scaling budget without reading source.

**Effort.** ~20 lines across `tiler.go`, `formats/svs/svs.go`, `formats/ndpi/striped.go`.

**Risks.** None.

### Section A sequencing recommendation

| Order | Item | Why first/last |
|---|---|---|
| 1 | A.1 mmap | Foundational; biggest single win; nothing depends on it |
| 2 | A.2 TileInto | Pairs with A.1 to give zero-syscall, zero-alloc tile reads |
| 3 | A.3 splice template | Opportunistic CPU win; only if pprof confirms |
| 4 | A.4 WarmLevel | Optional polish; depends on A.1 |
| 5 | A.5 docs | Anytime; cheap |

---

## Section B — Changes any consumer can adopt

These assume the corresponding upstream changes from §A have landed. Take what's there.

### B.1 Switch to `OpenFileMmap`

**What.** Replace `opentile.OpenFile(path)` with `opentile.OpenFileMmap(path)` in your slide-handle pool / Tiler factory. Optionally fall back to `OpenFile` if mmap returns an error (Windows? unusual filesystem? old opentile-go version that doesn't have it?).

```go
// Pseudo-Go for any consumer
tiler, err := opentile.OpenFileMmap(path)
if err != nil {
    if errors.Is(err, opentile.ErrMmapUnavailable) {
        tiler, err = opentile.OpenFile(path)
    }
    if err != nil { return err }
}
```

**Impact.** Inherits all the mmap benefits from §A.1 with no other code changes on your side.

**Effort.** ~15 LoC including the fallback.

**Risks.** Same SIGBUS concern as §A.1. If your slide files are read-only on disk (a `chattr +i` flag, a read-only mount, or an immutable object-storage path), the door is closed entirely. Otherwise, document the assumption.

### B.2 Pool tile buffers via `sync.Pool` + `TileInto`

**What.** Maintain a `*sync.Pool` of `[]byte` buffers keyed by power-of-two size buckets (16 KiB, 32 KiB, 64 KiB, ...). The serving path becomes:

```go
// Pseudo-Go
size := lvl.TileMaxSize()
bucket := nextPow2(size)
buf := pool.Get(bucket)        // pool.Get returns a []byte of cap >= bucket
defer pool.Put(buf, bucket)

n, err := lvl.TileInto(x, y, buf[:cap(buf)])
if err != nil { ... }

// Use buf[:n] for the network write or downstream consumer.
// If you also have a tile cache, the cache stores its own copy.
```

**Tradeoff with an LRU.** If your consumer has a tile cache (e.g., an LRU keyed by tile coordinates), the cache stores its own bytes — the pooled buffer can be returned right after the cache stores the copy. That's two allocations per cache miss (pooled buffer reused, plus cache's own bytes) and zero on cache hit. A pool-aware LRU that returns reference-counted pool slices is more complex and rarely worth it.

**Impact.**

- Reduces per-tile allocations from ~2 to 1 (or 0 on cache hit).
- For 1700 RPS warm cache, ~30 MB/s less GC pressure. Measurable but not earth-shattering.
- Dominant CPU savings come not from the bytes but from skipping the GC scan of those bytes. Translates to lower p99 latency under sustained load (fewer GC pause spikes).

**Effort.** ~80 LoC including the pool wrapper and tests. The pool buckets need a sane policy for buffers that come back larger than the bucket (e.g., a tile that was unusually big) — typically discard rather than re-pool to an over-sized bucket.

**Risks.**

- Lifecycle bugs are easy: returning a buffer to the pool while another goroutine still holds a reference → corruption. The discipline is `defer pool.Put()` immediately after `pool.Get()`. Don't store pooled buffers in long-lived data structures.

### B.3 Pre-warm at startup

**What.** Optional flag (`--prewarm`, `WSI_PREWARM=1`, etc.) that, after the slide-handle pool is initialized, walks the slide directory and calls `WarmLevel(0)` (or a configurable level) for every slide. Implementation depends on §A.4.

**Impact.**

- First-request-for-a-slide latency drops from "open file + parse IFDs + page-in tile data + read tile" to "memory access."
- Cost: server startup time + RSS proportional to how much you warm.

**Effort.** ~30 LoC plus docs.

**Risks.** Uses RAM. If your slide corpus is much larger than your physical RAM, pre-warming everything blows the page cache faster than you can use it. Make it opt-in. For corpora that fit comfortably in cache, it's a free latency win.

### B.4 Tune the tile cache to available RAM

**What.** If your consumer has a tile cache (LRU on the spliced JPEG bytes — a common pattern for HTTP servers), check whether its current size is conservative.

**Heuristic.** A tile cache of ~10–20% of available RAM is usually fine on a dedicated server. For a multi-tenant box, much smaller. For a desktop viewer, smaller still — the OS page cache (under §A.1 mmap) is doing most of the work; the in-process LRU mainly amortizes the JPEG-splice cost.

**Impact.**

- More cache hits → less opentile work → lower CPU + GC + syscall load.
- The win is sublinear: doubling the cache size doesn't double the hit rate; it gives you a fraction more of your hot-set fitting.

**Effort.** Two-character config change. Better: auto-tune to a fraction of `runtime.MemStats.Sys` or platform-available RAM.

**Risks.** None on the algorithm side. Operationally, you don't want this to balloon RSS unexpectedly — keep an env override prominent.

### B.5 Expose perf metrics

**What.** Surface internal counters on a `/stats` or `/metrics` endpoint:

- Per-level cumulative tile-read count + bytes
- Pool hit/miss counts (if you adopted §B.2)
- Tile-cache hit ratio (if you have one)
- Page-cache stats from `/proc/self/io` (under mmap, useful for "are we hitting RAM or going to disk")
- p50/p95/p99 latency for tile reads (rolling window — circular buffer of N samples is enough; no need for histograms)

**Impact.** Doesn't speed anything up directly, but makes "where is the time going" answerable from a curl. Closes the loop on every other change here.

**Effort.** ~80 LoC + tests. If you already use Prometheus, this fits naturally as histogram + counter metrics; otherwise a JSON endpoint is fine.

**Risks.** Each tile read adds a few atomic increments. At 1700 RPS that's noise.

### Section B sequencing recommendation

| Order | Item | Why |
|---|---|---|
| 1 | B.1 OpenFileMmap | One-line change; immediate measurable benefit |
| 2 | B.5 metrics | Visibility for everything else; do it before tuning |
| 3 | B.2 buffer pool | Real CPU/GC win; depends on §A.2 |
| 4 | B.4 cache size | Independent; tune once you have the metrics |
| 5 | B.3 pre-warm | Least benefit per unit effort; requires §A.4 |

---

## What not to bother with

Listed because each comes up repeatedly in tile-server perf discussions and is usually a wash or a regression for SVS workloads:

- **`posix_fadvise(WILLNEED)` hints.** The Linux readahead heuristic already handles sequential viewer access well. Manual hints rarely help and can hurt by evicting other cached pages.
- **Batch tile API (`Tiles([]TilePos)`).** In a Go HTTP server, the per-request goroutines already give you concurrency for free. Batch APIs help mainly single-threaded callers (a desktop viewer doing its own paint loop might want it for prefetch ordering — but that's a viewer concern, not an opentile concern). Networked storage backends (S3, GCS) where each `pread` has high overhead are the other case.
- **SIMD memcpy.** Go's `copy()` already calls `runtime.memmove`, which is SIMD-optimized.
- **Custom JPEG decoder in-process.** Out of scope; opentile passes through JPEG bytes without decoding. If you need decoded RGB on the server side (e.g., for transcoding), that's a separate workstream — and the current `internal/jpegturbo` already provides a path.
- **Replacing `pread` with `io_uring`.** Marginal at best for the tile-by-tile pattern; the win is in big sequential reads, which mmap already addresses better.

---

## A note on NDPI

Most of §A.1 (mmap), §A.4 (WarmLevel), and all of §B apply unchanged. NDPI's striped path has per-page mutexes; concurrency is across pages, not within one. §A.2 (TileInto) is harder — the striped reassembly buffer is already an internal scratch space, so the API would need to be designed differently. §A.3 (splice template) doesn't apply directly because NDPI tiles are reassembled, not table-spliced.

If your consumer mixes SVS and NDPI in the same harness, §A.1 still wins for both. Don't expect linear scaling under NDPI for tiles within the same page.

---

## Measuring

Before adopting any of this, get a baseline:

```bash
# Warm cache, 60 seconds, 100+ concurrent goroutines hammering one slide.
# Capture pprof CPU + alloc profiles.
go test -run=^$ -bench=BenchmarkTile -benchtime=60s \
    -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof -top cpu.prof | head -20
go tool pprof -alloc_space mem.prof | head -20
```

Look for the three signals listed in §"Where time actually goes":

- `runtime.mallocgc` ≥ 5% of CPU → adopt §A.2 + §B.2
- `syscall.pread64` (or `syscall.Syscall6`) ≥ 5% of CPU → adopt §A.1 + §B.1
- `jpeg.InsertTablesAndAPP14` ≥ 5% of CPU → adopt §A.3

Without numbers, **all of this is theoretical.** Profile first; pick from this list second. Most workloads will benefit from at least §A.1 + §B.1; the rest depend on what your profiler actually shows.
