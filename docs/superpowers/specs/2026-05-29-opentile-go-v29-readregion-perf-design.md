# opentile-go v0.29 — ReadRegion allocation-elimination perf milestone

**Status:** sealed 2026-05-29.
**Work branch:** `feat/v0.29`.
**Headline:** Eliminates ~85% of the per-call heap allocation in
`Slide.ReadRegion` across all formats. Three independent, layered
optimizations: skip the `fillWhite` prelude when the requested region
is fully in-bounds; reuse per-tile decode output via a module-level
`sync.Pool` keyed by `(W, H, Format)`; and reuse NDPI pixel-frame
decode buffers within `pixelFrameCache`'s eviction cycle. The
underlying goal is to reduce the 38% multi-thread CPU spent in
`runtime.pthread_cond_signal` (GC sweeping under a 39 GB/run
allocation rate measured on `bench-ndpi-mt`). Pure-additive
optimization — public API unchanged, pixel output bit-identical to
v0.28 by construction.

## 1. Scope

### 1.1. Layer 1 — fillWhite skip

`Slide.imageReadRegionImpl` unconditionally calls `fillWhite(dst)`
before the tile blit loop, costing ~5% of CPU on `bench-ndpi-mt` and
~3% on single-thread. v0.29 moves the clip-to-bounds computation
ahead of the fill and gates the fill on whether any OOB pixels exist:

```go
fullyInBounds := x0 == x && y0 == y && x1 == x+w && y1 == y+h
edgeTileX := x1 == lvl.Size.W && lvl.Size.W%lvl.TileSize.W != 0
edgeTileY := y1 == lvl.Size.H && lvl.Size.H%lvl.TileSize.H != 0
if !fullyInBounds || edgeTileX || edgeTileY {
    fillWhite(dst)
}
```

The `edgeTile*` checks are mandatory: even a fully-in-bounds request
on a level whose dimensions aren't a multiple of `TileSize` can hit
an edge tile that decodes to less than `TileSize.W × TileSize.H` —
the blit only writes the actual decoded extent, and pre-existing dst
contents from a previous use would leak into the result. The
conservative check leaves fillWhite enabled for any request touching
such an edge tile.

### 1.2. Layer 2 — per-tile output sync.Pool

`Slide.ImageDecodedTile` allocates a fresh `*decoder.Image` (sized to
TileSize) for every call. `ReadRegion` borrows this Image, blits it
into `dst`, and discards it. v0.29 adds a module-level
`sync.Map[scratchKey]*sync.Pool` (`opentile/decoded_tile_scratch.go`,
NEW) that caches `*decoder.Image` instances keyed by `(W, H,
PixelFormat)`. `ReadRegion`'s tile loop borrows a scratch Image
once per call, reuses it across every intersecting tile via
`ImageDecodedTileInto(opts.Dst=scratch)`, and returns it on `defer`.

The cross-cutting prereq is that `strippedImage.DecodedTile` (NDPI
v0.27 fast path) must honor `opts.Dst` instead of always allocating
fresh:

```go
var out *decoder.Image
if opts.Dst != nil &&
    opts.Dst.Width == l.tileSize.W &&
    opts.Dst.Height == l.tileSize.H &&
    opts.Dst.Format == outFormat {
    out = opts.Dst
} else {
    out = decoder.NewImageFormat(l.tileSize.W, l.tileSize.H, outFormat)
}
blitFromFrame(pixFrame, left, top, l.tileSize.W, l.tileSize.H, out)
return out, nil
```

`Slide.ImageDecodedTileInto`'s fast-path dispatch is extended to pass
`Dst: dst` into the decoded-tile call, and to detect when the fast
path wrote into `dst` directly (skipping the `copyImageInto` step).
The wrapper-delegation methods (`fileCloser`, `mmapCloser`) forward
`opts` unchanged — no edit there.

### 1.3. Layer 3 — pixelCache frame sync.Pool (NDPI-specific)

`formats/ndpi/pixelFrameCache.getOrLoad` allocates a fresh decoded
RGB frame (~3 MB) on each cache miss. The LRU eviction discards
evicted frames to garbage collection. v0.29 adds a `sync.Pool` per
`pixelFrameCache` instance and:

1. Routes evicted entries whose `pix.W/H` match the requested frame
   size into the pool.
2. On miss, pulls a scratch from the pool before falling back to
   fresh allocation.
3. Hands the scratch into the decoder via `opts.Dst`.

`evictIfOverLocked` is changed to return the evicted entries (instead
of dropping silently); the caller routes them to the pool best-effort
(non-matching sizes are GC'd as today). `getOrLoad` becomes
`getOrLoadInto(key, wantW, wantH, load func(scratch *decoder.Image))`
with the load callback receiving the borrowed scratch (or nil).
`strippedImage.DecodedTile`'s `pixelCache` callback passes the
scratch through to the decoder's `opts.Dst` — when nil, the decoder
allocates fresh; when present and same-sized, it writes into the
scratch.

The cache cycles same-sized frames continuously under bench-ndpi-mt
fanout (workers overlap and thrash the LRU), so the pool hit rate is
expected to be very high.

### 1.4. Bench / gate updates

- `make bench-ndpi`: gate raised from ≥ 220 to ≥ 235 Mpix/s (v0.28
  baseline is ~251; ~5% margin for noise).
- `make bench-svs`: gate raised from ≥ 475 to **a value measured
  during plan execution** (set ~95% of post-Layer 2 baseline).
- `make bench-ndpi-mt`: no gate; measurement target ≥ 700 Mpix/s
  (v0.28 baseline 539).
- `make bench-svs-mt`: no gate; measurement target ≥ 2400 Mpix/s
  (v0.28 baseline 2121).

### 1.5. Gate-failure policy

If any v0.29 perf gate regresses or fails to hit its projected
improvement, **stop and surface the result for a JIT (just-in-time)
decision** — do not auto-revert. Possible JIT responses:

- Accept a smaller improvement and tighten the gate to that value
- Investigate via pprof and re-attempt the layer
- Defer the layer to v0.30
- Reframe expectations in CHANGELOG

The default is human judgment, not mechanical reversion. Each layer
phase ends with a measurement step; if the layer's projected
improvement doesn't materialize, the plan halts for a decision
before continuing to the next layer.

## 2. Out of scope

- **Allocation reduction outside ReadRegion's hot path.** `Slide.DecodedTile`
  / `Slide.ImageDecodedTile` return user-owned `*decoder.Image`
  instances; those allocations remain. Direct DecodedTile callers
  that want to manage their own buffers use `*Into` variants
  (already exposed since v0.25).
- **A new `internal/imagepool/` package.** sync.Pool used inline;
  the v0.28 brainstorm rejected a typed pool package as over-
  engineering for the known use cases.
- **`Slide.Close` draining the scratch pools.** sync.Pool auto-shrinks
  under GC; no explicit drain needed. Distinct from v0.28's
  `decoderhandle.Pool` which holds cgo handles (needs deterministic
  teardown).
- **Cross-Slide sharing tuning.** The module-level scratch pool is
  inherently cross-Slide. No per-Slide overrides; no global cap;
  no LRU eviction beyond sync.Pool's GC-driven behavior.
- **Format-specific frame pooling outside NDPI.** Only NDPI's
  `pixelFrameCache` benefits from frame-buffer pooling (Layer 3).
  Other formats either don't have an equivalent intermediate cache
  (SVS, OME-TIFF, BIF) or have a fundamentally different layout
  (SZI ZIP-stored tiles). Layer 2's cross-format scratch pool is
  the only optimization those formats receive — sufficient.
- **`bench-ndpi-mt` re-architecture.** The bench uses `ReadRegion`
  per the user pattern; v0.29 makes it less allocation-heavy but
  doesn't switch to `DecodedTile` (which would be a different
  measurement target).
- **Public API additions.** No new exported types, functions, or
  methods. Pure internal optimization.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Scope | **Three layers (fillWhite-skip + per-tile output pool + pixelCache frame pool).** Confirmed via allocation profile showing `decoder.NewImageFormat` at 99% of allocations. |
| Q2 | Pool primitive | **`sync.Pool`.** Auto-shrinks under GC; no Slide.Close drain needed; matches stateless-buffer use case. |
| Q3 | Scratch pool scope | **Module-level, cross-Slide.** `sync.Map[scratchKey]*sync.Pool` keyed by `(W, H, Format)`. Per-Slide pool would silo same-shape buffers. |
| Q4 | `strippedImage.DecodedTile` Dst plumbing | **Honor opts.Dst when dimension+format match; fall back to allocation otherwise.** Defensive; v0.28 callers passing arbitrary Dst don't panic. |
| Q5 | `getOrLoad` refactor scope | **New method `getOrLoadInto(key, wantW, wantH, load func(scratch))`** with the load callback receiving the borrowed scratch. Old `getOrLoad` retained for callers that don't want pooling (currently none — v0.29 migrates the only caller). |
| Q6 | Eviction-to-pool semantics | **Best-effort.** Evicted entries with matching size route to pool; mismatches are GC'd. Avoids pool pollution with leftover variable-size frames. |
| Q7 | Bench gate tightening | **Layer-by-layer.** Each layer's phase ends with a measurement; the gate is bumped to ~95% of measured value before proceeding to the next layer. Catches regressions immediately. |
| Q8 | Gate failure policy | **JIT decision, not auto-revert.** If a layer's projected improvement doesn't materialize, halt and decide per Q-decision §1.5. |
| Q9 | Test fixture strategy | **Fixture-driven integration tests reuse v0.28 patterns.** Layer 1: synthetic minimalReader test double (no fixtures). Layer 2 + 3: real fixtures via `OPENTILE_TESTDIR` env. |
| Q10 | Layer commit boundary | **One commit per layer + one commit per measurement.** ~7 commits across the implementation; each layer self-contained and revertable. |

## 4. Architecture

### 4.1. Three layers, each shippable independently

```
Layer 1: fillWhite skip                      [opentile/slide_region.go]
─────────────────────────────
   Single conditional gate around the existing fillWhite call.
   Win: ~3-5% across all formats. Bounded, zero-risk.

Layer 2: per-tile output sync.Pool          [opentile/decoded_tile_scratch.go (NEW) +
─────────────────────────────                 opentile/slide_region.go +
                                              opentile/slide_decoded_tile.go +
                                              formats/ndpi/stripped.go]
   Module-level scratch pool; ReadRegion borrows once per call.
   Hard prereq: strippedImage.DecodedTile honors opts.Dst.
   Win: cross-format. Eliminates 22 GB / 56% of bench-ndpi-mt allocs.

Layer 3: pixelCache frame sync.Pool          [formats/ndpi/pixel_cache.go +
─────────────────────────────                 formats/ndpi/stripped.go]
   Per-cache (per-strippedImage) sync.Pool of evicted RGB frames.
   getOrLoad refactored to getOrLoadInto with load callback receiving
   scratch.
   Win: NDPI-specific. Eliminates 11.4 GB / 29% of bench-ndpi-mt allocs.
```

### 4.2. Components added in v0.29

| File | Purpose | Status |
|---|---|---|
| `opentile/decoded_tile_scratch.go` | `borrowTileScratch` / `returnTileScratch` + `scratchKey` + `tileScratchPool sync.Map`. Module-level scratch pool, format-and-size keyed. | NEW |
| `opentile/decoded_tile_scratch_test.go` | Unit tests: reuse on Borrow-after-Return, size-keyed separation, 32-way concurrent. | NEW |
| `opentile/slide_region.go` | Layer 1 fillWhite gate + Layer 2 scratch borrow/return + scratch-into-DecodedTileInto call. | EXTENDED |
| `opentile/slide_region_test.go` | Layer 1 tests: fully-in-bounds skip + edge-region force-fill. Synthetic `knownPixelReader`. | EXTENDED |
| `opentile/slide_decoded_tile.go` | ImageDecodedTileInto's fast-path dispatch: pass `Dst: dst`, skip copyImageInto when fast path wrote directly. | EXTENDED |
| `formats/ndpi/stripped.go` | `strippedImage.DecodedTile` honors `opts.Dst` (size+format-checked). v0.27 fast-path Crop callback passes scratch through `opts.Dst`. | EXTENDED |
| `formats/ndpi/stripped_decodedtile_test.go` | New `TestNDPIFastPathHonorsDst` + `TestNDPIFastPathDstWrongSizeFallsBackToAlloc`. | EXTENDED |
| `formats/ndpi/pixel_cache.go` | Add `scratchPool sync.Pool` field; refactor `getOrLoad` → `getOrLoadInto` (load callback receives scratch); `evictIfOverLocked` returns evicted entries for pool recycling. | EXTENDED |
| `formats/ndpi/pixel_cache_test.go` | New `TestPixelCacheRecyclesEvictedFrames` + `TestPixelCacheConcurrentScratchSafe`. | EXTENDED |
| `Makefile` | Raise `MIN_NDPI_MPIXS` 220 → 235. Raise `MIN_SVS_MPIXS` 475 → (set in Phase 4 from measured value). | EXTENDED |
| `CHANGELOG.md` | v0.29.0 entry: per-layer measured numbers, sealed Qs summary. | EXTENDED |
| `CLAUDE.md` | Promote v0.29 to current milestone; demote v0.28. | EXTENDED |

### 4.3. Lock order

Layer 2 introduces no new locks (sync.Map + sync.Pool are stdlib;
internal locking is hidden).

Layer 3 modifies `pixelFrameCache`'s existing locking:

- `pixelCache.mu` (existing) — guards `entries`, `order`, eviction.
  Released before the load callback runs.
- `pixelCache.scratchPool` (NEW) — sync.Pool, internal locking.
  Accessed only from `getOrLoadInto` after `pixelCache.mu` is
  released.

Both routes preserve the v0.27 promise pattern; no deadlock paths
introduced.

### 4.4. Concurrency invariants

- **Scratch ownership is exclusive.** A buffer is either in the pool
  (no goroutine holds a pointer) or borrowed by exactly one goroutine
  (the one that called Borrow). Return passes ownership back. No
  double-Borrow, no double-Return.
- **sync.Pool's auto-shrink is benign.** Cold-cache scenarios pay an
  allocation (same as v0.28 behavior); steady-state benches see pool
  hits.
- **Per-`pixelFrameCache` instance pool** — no cross-level cross-talk.
  Each `strippedImage` owns its own pool.
- **Eviction-to-pool is best-effort** — if multiple goroutines
  concurrently evict the same key, only the entries that survive the
  v0.27 cache mutex's serialization are routed to the pool. No
  double-Put.

### 4.5. Performance projection (computed from v0.28 alloc profile)

bench-ndpi-mt allocation reduction:

| Source | v0.28 | v0.29 (projected) |
|---|---|---|
| Per-tile output (Layer 2) | 22 GB | ~0 |
| pixelCache frame (Layer 3) | 11.4 GB | ~1 GB (cold path only) |
| ReadRegion dst (user-owned) | 5 GB | 5 GB (unchanged) |
| Other | 0.6 GB | 0.6 GB |
| **Total** | **39 GB** | **~7 GB** |

The 32 GB reduction should drop GC sweep cost from ~38% of
multi-thread CPU to a small fraction. Projected throughput:

| Bench | v0.28 | v0.29 (projected) | Rationale |
|---|---|---|---|
| bench-ndpi (single) | 251 Mpix/s | ~260 Mpix/s | Layer 1 + Layer 2 GC reduction |
| bench-ndpi-mt | 539 Mpix/s | ~700-900 Mpix/s | Layer 1 + 2 + 3 combined |
| bench-svs (single) | 596 Mpix/s | ~620 Mpix/s | Layer 2 only (no NDPI cache) |
| bench-svs-mt | 2121 Mpix/s | ~2400-2800 Mpix/s | Layer 2 GC reduction |

These are computed from allocation-rate math, not measured. Layer-by-
layer measurement during plan execution catches gaps.

## 5. Testing & parity gate

### 5.1. Layer 1 tests

- `TestReadRegionFullyInBoundsPathSkipsFillWhite` — synthetic
  minimalReader returning 0x42 pixels; pre-sentinel dst with 0xAA;
  request fully-in-bounds region; assert every dst pixel is 0x42
  (not 0xFF or 0xAA).
- `TestReadRegionEdgeRegionForceFillWhite` — same reader; request
  crossing the level edge; assert in-bounds half is 0x42 and OOB
  half is 0xFF.

### 5.2. Layer 2 tests

- `TestTileScratchPoolReuse` — Borrow, Return, Borrow returns the
  same pointer (sync.Pool LIFO).
- `TestTileScratchPoolSizeKeyed` — different sizes don't share
  buffers.
- `TestTileScratchPoolConcurrent` — 32 goroutines under -race,
  100 borrow-return cycles each.
- `TestReadRegionScratchPoolReducesAllocations` — fixture-driven;
  loose `runtime.ReadMemStats`-based witness that pooling reduced
  per-call alloc.
- `TestNDPIFastPathHonorsDst` — fixture-driven; pass dst into
  DecodedTileInto, assert returned image IS dst.
- `TestNDPIFastPathDstWrongSizeFallsBackToAlloc` — defensive; pass
  wrong-size dst, assert allocation fallback, no panic.

### 5.3. Layer 3 tests

- `TestPixelCacheRecyclesEvictedFrames` — capacity=2, cycle 10
  distinct frames, instrument scratchPool to count Get/Put, assert
  steady-state recycling.
- `TestPixelCacheConcurrentScratchSafe` — 32 goroutines hammering
  the cache with frame loads under -race.

### 5.4. v0.27 / v0.28 regression coverage

Unchanged in intent. Specifically:

- `TestNDPIFastPathPixelParity` — bit-identical via the new Dst path
- `TestNDPIFastPathConcurrent` — 32-way fanout under -race
- `TestNDPIDecodedTilePathParity` — cross-fixture (CMU-1, OS-2, Hamamatsu-1)
- `TestSlideDecoderHandleReuse` / `TestSlideHandleConcurrent` (v0.28)
- 40-fixture `TestSlideParity`
- `TestPixelCacheThrash` (v0.27 stress under fanout)

All run unchanged; collectively they catch any pixel corruption from
buffer reuse.

### 5.5. Performance gates

| Gate | v0.28 value | v0.29 value | Type |
|---|---|---|---|
| `make bench-ndpi` | ≥ 220 Mpix/s | **≥ 235 Mpix/s** | Hard (tightened) |
| `make bench-svs` | ≥ 475 Mpix/s | **measured-baseline × 0.95** | Hard (set in Phase 4) |
| `make bench-ndpi-mt` | (none) | (measurement only) | Soft |
| `make bench-svs-mt` | (none) | (measurement only) | Soft |

Gate tightening is layer-by-layer: after each layer ships, run the
benches, bump the gate by the appropriate amount before moving on.
JIT decision (per §1.5) if any layer doesn't move the needle as
projected.

### 5.6. Coverage

`make cover` ≥ 80% per package (existing bar). New
`opentile/decoded_tile_scratch.go` has 3 unit tests covering the
small surface directly. NDPI package coverage is mechanically
preserved (extensions; no deletions).

### 5.7. What's explicitly NOT tested

- Format-by-format byte equivalence under pool. The scratch IS the
  decode target; output bytes are identical to the no-pool path by
  construction. v0.27 / v0.28 parity tests are the regression
  witness.
- sync.Pool internal behavior (stdlib).
- Bench wall-clock numbers themselves — they vary ~5% run-to-run;
  gates have margin built in.

## 6. Performance projection & success criteria

Reference numbers (Apple Silicon, 13 cores, single-thread unless noted):

### Success criteria

- **Mandatory:** All v0.27 + v0.28 tests + new Layer 1/2/3 tests
  green under `-race -count=1`. TestSlideParity zero divergence.
- **Mandatory:** `make bench-ndpi` ≥ 235 Mpix/s.
- **Mandatory:** `make bench-svs` ≥ (measured-baseline × 0.95).
- **Acceptable:** Single-thread bench improvements ≥ +3% over v0.28.
- **Acceptable:** Multi-thread bench improvements ≥ +20% over v0.28.
- **Stretch:** `bench-ndpi-mt` ≥ 900 Mpix/s (3.5× single-thread,
  closing the gap with SVS-mt's 4.16× scaling).

### Gate failure protocol (per §1.5)

If a layer's projected improvement doesn't materialize, halt and
present:

1. The measured numbers
2. A pprof comparison showing where the cost actually moved
3. JIT options: accept-and-document, investigate-and-retry,
   defer-to-v0.30, reframe

Decision is human, not mechanical.

## 7. Open items and follow-ups

- **Same-pool key collision risk.** Two different `*decoder.Image`
  consumers could theoretically end up with pointers to the same
  scratch if Borrow/Return are misused (e.g., goroutine A borrows
  before goroutine B's Return completes for the same key). The
  `defer Return` discipline in ReadRegion prevents this in the
  module's own code, but Layer 2 + 3's reuse is the class of bug to
  watch in code review.
- **Cold-path penalty.** First call after a long idle period sees
  sync.Pool empty (GC reclaimed). Pays an allocation. Steady-state
  benches see this as a single-tile warmup spike. Acceptable.
- **bench-ndpi-mt scaling story.** Even after v0.29, if multi-thread
  scaling stalls below the SVS-mt ratio (3.56×), the residual
  bottleneck is likely (a) ScaledStrips' own per-tile coordination
  or (b) the v0.27 fast-path pixel-cache promise pattern under
  fanout. Both are deferred to a future investigation.
- **pixelCache LRU capacity tuning.** v0.27 hardcoded
  `max(NumCPU, 16)`. Layer 3 reduces *allocation* per miss but
  doesn't change miss rate. If miss rate is the real bottleneck,
  v0.30 could revisit the capacity.

## 8. References

- v0.27 spec (NDPI fast pixel path):
  `docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md`
- v0.28 spec (decoder pool):
  `docs/superpowers/specs/2026-05-29-opentile-go-v28-cross-format-decoder-pool-design.md`
- v0.28 allocation profile capture: bench-ndpi-mt with
  `-memprofile` flag; analyzed via `go tool pprof -sample_index=alloc_space`.
  Showed `decoder.NewImageFormat` at 99.01% of allocations
  (38.7 GB / 39 GB total). This is the smoking gun motivating v0.29.
- v0.28 CPU profile capture: bench-ndpi-mt at NumCPU=13. Showed
  `runtime.pthread_cond_signal` at 38.89% of CPU. The
  allocation-rate hypothesis says this is GC sweep cost; v0.29
  tests that hypothesis by reducing allocation rate.
- v0.27 `pixelFrameCache` (the cache being extended):
  `formats/ndpi/pixel_cache.go`
- v0.27 `strippedImage.DecodedTile` (the fast path being modified):
  `formats/ndpi/stripped.go`
- v0.28 `Slide.ImageDecodedTile` / `ImageDecodedTileInto` (the
  dispatch sites being extended):
  `slide_decoded_tile.go`
- v0.28 `Slide.imageReadRegionImpl` (the host for Layer 1 + Layer 2):
  `slide_region.go`
- v0.28 bench infrastructure (the gate enforcement model):
  `cmd/bench/ndpi/`, `cmd/bench/svs/`, `Makefile`
