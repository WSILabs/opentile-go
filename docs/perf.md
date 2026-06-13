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
- For whole-slide scaled output (DZI conversion, region extract), use
  `Pyramid.ScaledStrips(...)`. Its peak memory is byte-bounded since v0.30
  — set `OPENTILE_READ_MEMORY_BUDGET` / `WithMemoryBudget` and
  `GOMEMLIMIT≈2GiB` to keep it ~2 GB regardless of slide width. See
  [ScaledStrips + memory budget](#whole-slide-scaled-output-scaledstrips--memory-budget-v026v030).

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

## Whole-slide scaled output: `ScaledStrips` + memory budget (v0.26–v0.30)

For DZI/deep-zoom builders, libvips-style pipelines, and region
extract, `pyr.ScaledStrips(l0Rect, outSize, stripHeight, opts...)`
streams a slide scaled to a target resolution as horizontal strips. It
runs parallel decode workers, a bounded per-iterator decoded-tile
cache, and lookahead prefetch internally; the caller pulls one
`*decoder.Image` strip per `Next()` until `io.EOF`, then `Close()`.
Per-thread decode throughput inherits from the NDPI/SVS fast paths
(v0.27–v0.28), so multi-threaded `convert --to dzi` scales across cores.

### Memory model (why peak is bounded since v0.30)

Pre-v0.30 the strip cache was sized by a tile *count* that grew with
slide **width**, so peak memory climbed without a ceiling — a wide NDPI
slide could drive a 16 GB machine into a memory panic during DZI
conversion. v0.30 re-expresses the cache cap as a **byte budget**, so
the dominant term is flat regardless of width.

Peak ≈ GC-headroom × (cache budget + frame LRU + pixelCache + strip
buffers):

| Term | Size | Scales with |
|---|---|---|
| Decoded-tile cache (C1) | `WithMemoryBudget`, default **1 GiB** | nothing (bounded) |
| Assembled-frame LRU (C3) | 128 MiB | nothing (bounded) |
| NDPI `pixelCache` (C2) | ~0.5 GB | slide width (deferred byte-bound) |
| Output strip buffer (C4) | `outWidth × stripHeight × 3 × ~(lookahead+2)` | width × tile size |
| GC headroom | ~2× live (`GOGC=100`), or clamped by `GOMEMLIMIT` | — |

### Tuning

- **`opentile.WithMemoryBudget(bytes)`** / **`OPENTILE_READ_MEMORY_BUDGET`**
  (env, bytes): the C1 cache budget. Option > env > default (1 GiB).
- **Set `GOMEMLIMIT`** (e.g. `2GiB`) to clamp GC headroom — it takes the
  peak from ~2× live down toward the live set. opentile-go honours an
  externally-set `GOMEMLIMIT` (its default budget auto-shrinks to ≤ half
  of it) but never sets one itself. A `GOMEMLIMIT` *below* the live
  working set causes GC thrash, so keep it ≥ the budget plus headroom.
- **DZI tile size 256** (the default) gives the lowest peak; 512/1024
  enlarge the irreducible C4 strip buffer (not covered by the budget).

### Measured (widest fixture: Hamamatsu, 188160×101376, 19 Gpix)

Worst case — no consumer backpressure, the strip iterator running flat
out (real `wsitools convert` backpressures and runs lower):

| Config | peak RSS |
|---|---|
| 256 tile + `GOMEMLIMIT=2GiB` (recommended) | ~2.1 GB |
| 256 tile, no env | ~3.3 GB |
| 1024 tile + `GOMEMLIMIT=2GiB` | ~3.5 GB |
| 1024 tile, no env (heaviest) | ~5.8 GB |

Extrapolated to a maximum-size 2″×1″ 40× slide (~24 Gpix): the
recommended config stays ~2.3 GB; the absolute ceiling across all
configs is ~7 GB. Either way it is a fixed ceiling — independent of
slide width at a given tile size — not the unbounded pre-v0.30 climb.

### Gate

`make bench-ndpi-mem` runs the no-backpressure worst case under
`GOMEMLIMIT=2GiB` (CMU-1 + OS-2 at tile 256/1024) and fails if peak
`HeapInuse` exceeds the committed thresholds — the regression guard for
this class of issue. Thresholds are intentionally higher than real
`wsitools` RSS because the harness drops strips (no backpressure).

## Benchmark suite (cross-format)

`bench/` is the standing cross-format benchmark suite (design:
`docs/superpowers/specs/2026-05-30-comprehensive-benchmark-suite-design.md`).
It covers all 10 formats × three patterns (`Tile` compressed-fetch,
`DecodedTile`, `ReadRegion`) × single/parallel. Three entry points:

- **`go test ./bench/ -bench BenchmarkRead`** — the profiling + A/B
  instrument. Each sub-benchmark reports `Mpix/s` alongside `ns/op` and
  `allocs/op`; `-cpuprofile`/`-memprofile` for profiles; `benchstat`
  across `-count` runs for before/after comparison.
- **`make bench-all`** — the throughput regression gate. Local/manual
  (like the other `bench-*` targets — not run in CI, since shared
  runners are too slow/variable for absolute Mpix/s floors). Fails if a
  gated `format/pattern` drops below its floor (~85% of measured
  single-thread baseline; only stable decode/assembly patterns are
  gated). Re-baseline floors after a deliberate speedup.
- **`make bench-compare`** — the on-demand competitive report:
  opentile-go vs **openslide** (ReadRegion, in-process via a build-tagged
  cgo shim) vs **python opentile** (Tile, subprocess). Needs
  libopenslide + a python-opentile interpreter (`OPENTILE_ORACLE_PYTHON`).
  See `cmd/bench/compare/README.md` for the column caveats (the Tile
  column measures compressed-fetch overhead, not decode; multi-region
  SCN ReadRegion isn't apples-to-apples).

The openslide comparison uses an in-house ~40-line cgo shim
(`internal/openslideshim`) gated behind `//go:build openslidebench`, so
the shipping library keeps its single cgo dependency
(`internal/jpegturbo`) — normal builds, `go test ./...`, and CI never
compile it.

### Measured competitive numbers (v0.34.1)

A representative `make bench-compare` run — 10-core Apple Silicon (M-series)
laptop, macOS, one fixture per format, bounded interior tile grid, single
run. **Absolute `Mpix/s` vary with hardware and run-to-run; the ratios are
the stable takeaway.** Reproduce on your own machine with `make
bench-compare` (needs libopenslide + a python-opentile interpreter).

| Format | DecodedTile (Mpix/s) | openslide `read_region` | **ratio** | ReadRegion | openslide | ratio |
|---|--:|--:|:-:|--:|--:|:-:|
| generic-tiff | 1067 | 74 | **14.3×** | 951 | 74 | 12.8× |
| philips-tiff | 779 | 70 | **11.1×** | 698 | 70 | 9.9× |
| svs | 685 | 72 | **9.5×** | 527 | 72 | 7.3× |
| leica-scn | 645 | 73 | **8.9×** | 618 | 73 | 8.5× |
| ndpi | 666 | 207 | **3.2×** | 624 | 207 | 3.0× |
| ome-tiff | 729 | — | — | 688 | — | — |
| ife | 817 | — | — | 616 | — | — |
| szi | 685 | — | — | 508 | — | — |
| cog-wsi | 788 | — | — | 741 | — | — |
| bif | 379 | — | — | 367 | — | — |

(openslide can't read OME-TIFF / IFE / SZI / COG-WSI / BIF here → "—".)

**Decode is the competitive headline: 3–14× openslide** across the
overlapping formats. `DecodedTile` (the v0.27 per-tile fast path that
`ScaledStrips` / DZI consume) edges `ReadRegion`, which layers
fill/scratch machinery over the same decode. NDPI's 3× (vs 9–14×
elsewhere) reflects its single-monolithic-JPEG layout, not a slow
decoder — see the NDPI per-tile-decode note above.

**Raw compressed-tile fetch vs Python opentile is ≈parity** (~0.8–2.6×,
and noisy run-to-run): both opentile-go `RawTile` and python `get_tile`
return the *same compressed bytes*, so for a tiled TIFF it's an
mmap-slice on both sides and the residual is just per-call overhead. The
one structural case is **NDPI raw-tile (~1.1×)**: both libraries must
*transcode* — an NDPI level has no stored per-tile stream — so neither
can slice. (An earlier note claimed "5–17× python opentile"; that figure
was actually the openslide *decode* comparison, mislabeled. Raw-tile vs
python was always parity.)

Caveats: single machine, one fixture per format, single run (not
`benchstat`-averaged). Leica SCN reads are aligned to openslide's
`bounds-x`/`bounds-y` so both backends read the same tissue — without the
offset a fixed coordinate lands openslide in the empty multi-region
margin and reports a meaningless ~50,000 Mpix/s. DICOM (the 11th format)
is multi-file and not in the single-file bench matrix.
