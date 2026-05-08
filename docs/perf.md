# Performance characteristics

This document describes how opentile-go reads tiles efficiently and
how to get the best throughput as a consumer. Targeted at HTTP tile-
server authors, desktop viewers, and pipeline operators serving 100+
RPS or scanning slides at high parallelism.

## tl;dr

- `opentile.OpenFile(path)` is **memory-mapped by default since v0.9**.
- Use `Level.TileInto(x, y, dst)` with a `sync.Pool` of `[]byte`
  buffers for high-RPS callers — zero allocations per tile on every
  TIFF format and Iris IFE.
- `Level.TileMaxSize()` tells you how big each pooled buffer needs
  to be.
- For predictable warm-cache latency on slides you're about to read
  intensively, call `Tiler.WarmLevel(i)` once at slide-open time.
- The legacy `Level.Tile(x, y) ([]byte, error)` API is unchanged and
  fully supported. Use it for casual scripts and one-shot reads.

## Default I/O backing: memory-mapped

Since v0.9, `opentile.OpenFile(path)` returns a Tiler whose tile
reads are backed by `mmap(2)` (Linux/macOS) or `CreateFileMapping`
(Windows) under the hood. The benefits:

- **No `pread(2)` syscall per `Tile()` call.** Tile reads become
  userspace `memcpy` from the mapped region.
- **Lazy paging.** The kernel page-fault handler brings tile data
  into the page cache on first access; warm-cache reads hit RAM at
  memory-bandwidth speed.
- **Free readahead.** Sequential viewer access patterns benefit
  from kernel readahead at no cost to the application.

### Failure modes

- **SIGBUS on file truncation.** If the underlying file is
  truncated or rewritten while a Tiler is open, subsequent tile
  reads through the mapping raise SIGBUS in the calling thread.
  WSI files don't get truncated under normal use; if your storage
  allows it, opt out via `WithBacking(BackingPread)`.
- **mmap unavailable.** Some FUSE mounts and network filesystems
  don't support memory-mapping. `OpenFile` returns
  `ErrMmapUnavailable` wrapping the underlying error; retry with
  `WithBacking(BackingPread)` to fall back to the os.File + pread
  path.

### Opting out

```go
tiler, err := opentile.OpenFile(path, opentile.WithBacking(opentile.BackingPread))
```

The pread path is exactly v0.8's behavior (one syscall per tile).
Slower in steady state, but doesn't risk SIGBUS on truncation and
works on filesystems that don't support mmap.

## Pool-friendly tile reads: `TileInto`

The `Level.Tile(x, y)` API allocates a fresh `[]byte` on every
call. At 1700 RPS with ~17 KB tiles, that's 30 MB/s of allocation
churn — 1,700 GC scans per second. Tail-latency spikes follow.

`Level.TileInto(x, y, dst)` writes into a caller-provided buffer:

```go
maxSize := lvl.TileMaxSize()
pool := &sync.Pool{
    New: func() any {
        buf := make([]byte, maxSize)
        return &buf
    },
}

// Per-request handler:
bufPtr := pool.Get().(*[]byte)
defer pool.Put(bufPtr)
n, err := lvl.TileInto(x, y, *bufPtr)
if err != nil { /* ... */ }
// Use (*bufPtr)[:n] — write to network response, etc.
```

Use `TileMaxSize()` to size pool buckets. Adjacent levels typically
have similar `TileMaxSize` values; one pool per Tiler (sized by max
across levels) is usually enough.

**Returns `io.ErrShortBuffer`** if `len(dst) < TileMaxSize()`. No
I/O happens in that case; the call returns immediately.

### When to use which

| Use case | API | Why |
|---|---|---|
| Casual script, one-shot tile read | `Tile(x, y)` | simpler |
| HTTP tile-server, viewer paint loop | `TileInto` + sync.Pool | zero allocs |
| Tile cache (LRU) — caller stores its own copy | either | the cache's allocation dominates either way |

## Pre-warming the page cache

`Tiler.WarmLevel(i int) error` touches one byte per OS page covering
level `i`'s tile-data ranges. Under the v0.9 default mmap backing,
this forces the kernel to populate the page cache lazily on first
call — subsequent `Tile()` / `TileInto()` reads on level `i` hit
RAM at memory-bandwidth speed regardless of access pattern.

Useful for:

- **Slide-server pre-warm at startup** — walk the slide directory,
  open each Tiler, call `tiler.WarmLevel(0)` (and maybe L1) for
  each. First-request latency on every slide drops to memory access
  speed.
- **Desktop viewer slide-open** — pre-warm the slide the user just
  opened so the first tiles are instant.

Best-effort — returns `ErrLevelOutOfRange` if `i` is out of bounds,
or the first I/O error encountered while touching pages. Callers
that want to ignore errors (it's a hint) can discard the result.

Under `BackingPread`, `WarmLevel` does pread(1) per page —
considerably slower, but the warm-up effect (kernel page cache
population) is the same.

## Concurrency

- **Tile reads are concurrent-safe** on every format. SVS / Philips /
  OME tiled / BIF / IFE have no internal locks on the tile hot path.
  NDPI's striped reader takes a per-page mutex on its assembled-frame
  cache; concurrent reads of *different* pages run in parallel,
  concurrent reads of the *same* page serialize. OME OneFrame is
  similar.
- **Bytes returned by `Tile()` are caller-owned.** opentile-go does
  not retain a reference, and callers may modify the returned slice.
- **Bytes written by `TileInto` into `dst` remain caller-owned.**
  opentile-go writes once and never reads `dst` after return.
- **`Close()` must not race with in-flight tile reads.** Under
  `BackingMmap` this is non-negotiable: closing unmaps the file, and
  subsequent reads through the mapping raise SIGBUS. Sequence Close
  after a wait group on outstanding readers.

## Per-format performance characteristics

Measurements on Apple M4 (darwin/arm64) under warm-cache pool TileInto:

| Format | Tile dims | Pool TileInto ns/op | Allocs | Notes |
|---|---|---:|---:|---|
| Iris IFE | 256×256 | 152 | 0 | Self-contained tiles, no splice |
| OME tiled | 256×256 | 376 | 0 | Leica fixtures have no JPEGTables |
| **SVS** | 240×240 | **99.7** | **0** | In-place splice (v0.9 T8) |
| **Philips** | 512×512 | **425** | **0** | In-place splice (v0.9 T8) |
| Ventana BIF | 1024×1024 | 3,225 | 0 | Larger tiles, more memcpy |
| NDPI striped | 512×512 | 185k (parallel) | 4 | CPU-bound libjpeg-turbo crop |

NDPI is the outlier. Per-tile work includes a libjpeg-turbo
`tjTransform` pass (DCT-domain crop), which is genuinely CPU-bound
software work. mmap doesn't help (the bottleneck isn't I/O); pool
doesn't help (the internal scratch is the assembled frame, which is
already cached). For high-RPS NDPI serving, a consumer-side LRU
cache on the spliced JPEG bytes is the right answer.

## Reproducing the benchmarks

```bash
OPENTILE_TESTDIR=$PWD/sample_files \
  go test -tags benchgate -bench=BenchmarkTile -benchmem -count=1 \
    -run=^$ ./tests/parity/
```

For pprof-based investigation:

```bash
OPENTILE_TESTDIR=$PWD/sample_files \
  go test -tags benchgate -bench=BenchmarkTile -benchmem -count=1 \
    -benchtime=60s -cpuprofile=/tmp/cpu.prof -memprofile=/tmp/alloc.prof \
    -run=^$ ./tests/parity/
go tool pprof -top -cum /tmp/cpu.prof | head -20
```

Baseline files committed to the repo (timestamped pre-/post each
v0.9 task):

- `tests/fixtures/v0.9-baseline.txt` — pre-mmap (v0.8 numbers)
- `tests/fixtures/v0.9-after-mmap.txt` — after A.1 mmap
- `tests/fixtures/v0.9-after-tileinto.txt` — after A.2 TileInto + pool
- `tests/fixtures/v0.9-after-splice.txt` — after A.3 in-place splice

## Pattern A vs Pattern B: bandwidth deduplication (v0.13)

v0.13 exposes a second tile-read pattern aimed at client-server
consumers that stream tile bytes over a network. In that setting,
the JPEG splice prefix (DQT + DHT tables, and optionally APP14) is
identical for every tile on a level. Shipping it once per session
rather than once per tile saves bandwidth on slides whose encoder
used shared JPEGTables (TIFF tag 347).

### The two patterns

**Pattern A — full tile, always correct:**

```go
data, err := lvl.Tile(x, y)
// or zero-alloc:
n, err := lvl.TileInto(x, y, dst)
```

Returns a complete, self-contained JPEG for every tile. This is the
only correct choice for non-JPEG compressions, for levels whose
`TilePrefix()` returns nil, and for any consumer that needs a
stand-alone JPEG without post-processing.

**Pattern B — prefix once, body per tile, splice on client:**

```go
// Once per level (send over the wire once per session):
prefix := lvl.TilePrefix() // nil if no shared tables

// Per tile (cheaper wire transfer when prefix != nil):
n, err := lvl.TileBodyInto(x, y, dst)
body := dst[:n]

// Client-side reconstitution (Go example; JS reimplementation possible):
jpeg, err := opentile.SpliceJPEGTile(prefix, body)
```

`TileBodyMaxSize()` is the pool-buffer size analogue for `TileBodyInto`
(always ≤ `TileMaxSize()`; strictly less when the prefix is non-nil).

### When Pattern B helps

Pattern B saves bandwidth only when the Level's `TilePrefix()` is
non-nil — that is, when the slide's encoder stored shared JPEGTables
in TIFF tag 347 and opentile-go's splice path prepends them to every
tile. The savings scale with prefix size × tile count:

- SVS (Aperio): 301B prefix × 23,220 tiles on CMU-1.svs L0 → 4.3%
  wire-size reduction.
- Philips TIFF: 570B prefix × 6,160 tiles on Philips-1.tiff L0 →
  1.5% reduction.
- Ventana BIF (OS-1): 570B prefix, comparable savings to Philips.

Pattern B gives 0% savings on:

- Slides with **per-tile-embedded JPEGTables** (e.g., Ventana-1 BIF
  in our fixture set — the tile bytes already include tables inside
  each JPEG stream). `TilePrefix()` returns nil; `TileBodyInto` is
  equivalent to `TileInto` on those levels.
- **Non-JPEG compressions** (JP2K, LZW, Deflate, None). The splice
  concept is JPEG-specific; `TilePrefix()` always returns nil for
  these levels.
- **OneFrame-style packed-image levels** (NDPI and OME's packed
  levels) — the tile bytes are DCT-domain crops from a full-page
  JPEG; there is no shared prefix to deduplicate.
- Slides where the encoder embedded tables per-tile despite sharing
  them at the TIFF layer. Savings are fixture-author-dependent.

### Running the bench harness

The v0.13 bandwidth comparison harness is at
`tests/parity/tilebody_bench_test.go` under build tag `benchgate`:

```bash
OPENTILE_TESTDIR=$PWD/sample_files \
  go test -tags benchgate -run=^$ \
    -bench=BenchmarkTileBodyBandwidth -benchmem -count=1 \
    ./tests/parity/
```

The harness walks L0 of each fixture, accumulates Pattern-A bytes
(full `Tile()` output) and Pattern-B bytes (`len(prefix) +
sum(TileBodyInto)`), and reports the per-fixture savings percentage.

### Summary

Pattern B is an optional bandwidth optimization for client-server
consumers. Savings are real but fixture-author-dependent — don't
assume them without profiling. Pattern A always works, always
correct. When in doubt, start with Pattern A.
