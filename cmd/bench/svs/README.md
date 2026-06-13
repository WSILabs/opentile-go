# cmd/bench/svs

Single-thread + multi-thread SVS tile-decode throughput benchmark.
Companion to cmd/bench/ndpi. v0.28+ measurement target for the
cross-format decoder-handle pool.

## Build

```sh
go build -o /tmp/bench-svs ./cmd/bench/svs/
```

## Run

```sh
# Single-thread (gated)
/tmp/bench-svs -in sample_files/svs/CMU-1.svs

# Multi-thread (measurement only)
/tmp/bench-svs -in sample_files/svs/CMU-1.svs -goroutines $(sysctl -n hw.ncpu)

# With CPU profile
/tmp/bench-svs -in sample_files/svs/CMU-1.svs -cpuprofile /tmp/cpu.prof
go tool pprof -top /tmp/cpu.prof
```

## Expected numbers (Apple Silicon, CMU-1.svs)

| Build              | Mode           | Throughput (approx) |
|---|---|---|
| v0.27 baseline     | single-thread  | pre-pool baseline   |
| v0.28              | single-thread  | ~10–15% above v0.27 |
| v0.28              | multi-thread   | several× single-thread |

The Makefile `bench-svs` target enforces a single-thread floor
(`MIN_SVS_MPIXS`, set to 95% of the measured post-v0.28 baseline).
`bench-svs-mt` is measurement only — no gate, just a number that
demonstrates the pool unlocks parallelism on the SVS slow path.

## What this bench actually measures

`Level.DecodedTile(tx, ty)` for every tile of L0. SVS uses the
v0.26 slow path (not v0.27's NDPI fast path), so every call routes
through `Slide.ImageDecodedTile`'s slow-path Borrow/Return — making
this the most direct measurement of v0.28's cross-format deliverable.
