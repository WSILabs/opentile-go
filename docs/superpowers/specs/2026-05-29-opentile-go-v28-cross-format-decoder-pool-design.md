# opentile-go v0.28 — cross-format decoder-handle pool

**Status:** sealed 2026-05-29.
**Work branch:** `feat/v0.28`.
**Headline:** Eliminates per-tile `decoder.Factory.New() / dec.Close()`
churn — `tjDestroy` measured at ~240 µs/call in the v0.27 profile,
`tjInit` estimated at ~50 µs/call — across every format that routes
through `Slide.ImageDecodedTile`. Introduces a small fixed-size
pool of long-lived `decoder.Decoder` instances per `(Slide, codec)`,
sized `min(runtime.NumCPU(), 8)`. NDPI's existing per-`strippedImage`
handle (v0.27) migrates to the same shared primitive, gaining
multi-core parallelism on `Slide.DecodedTile` calls; non-NDPI formats
(SVS, OME-TIFF tiled, BIF, Leica SCN, IFE, SZI, COG-WSI, generictiff,
Philips) inherit the win automatically via the v0.27 slow-path
dispatch. Tightens the NDPI throughput gate from ≥130 to ≥220 Mpix/s
and adds bench-svs / bench-ndpi-mt measurement coverage.

## 1. Scope

### 1.1. New shared package `internal/decoderhandle`

A small bounded pool of `decoder.Decoder` instances. Members are
lazy-initialised (first `Borrow` calls `factory.New()`); pool capacity
caps the maximum simultaneously-borrowed members; `Borrow` blocks when
all members are in use; `Return` is non-blocking; `Close` drains and
tears down every member.

```go
// internal/decoderhandle/handle.go (NEW)

type Pool struct {
    factory     decoder.Factory
    capacity    int                   // max concurrent Borrows; immutable post-New
    initMu      sync.Mutex            // guards lazy-create + outstanding + closed
    items       chan decoder.Decoder  // buffered, cap = capacity
    outstanding int                   // count of members held by callers (issued
                                       // via Borrow's lazy-create branch and not
                                       // yet returned); ensures factory.New() is
                                       // called at most `capacity` times total
    closed      bool
}

var (
    ErrClosed    = errors.New("decoderhandle: pool closed")
    ErrNoFactory = errors.New("decoderhandle: no factory registered for compression")
)

func New(fac decoder.Factory, capacity int) *Pool
func (p *Pool) Borrow() (decoder.Decoder, error)
func (p *Pool) Return(d decoder.Decoder)
func (p *Pool) Close() error
```

Concurrency model: members are not concurrent-safe (libjpeg-turbo
`tjhandle` is single-threaded), so `Borrow` grants exclusive access
for the lifetime of the borrow. Multiple goroutines can call `Borrow`
concurrently; the pool blocks them on `<-p.items` once `outstanding ==
capacity`.

### 1.2. Migrate `formats/ndpi.decoderHandle` to the shared type

`formats/ndpi/decoder_handle.go` and its test file are **deleted**.
The shared type `decoderhandle.Pool` replaces the v0.27 `decoderHandle`
struct. Mechanical edit to `formats/ndpi/stripped.go`:

```go
type strippedImage struct {
    // ... v0.27 fields unchanged ...
    pixelCache    *pixelFrameCache
    decHandle     *decoderhandle.Pool  // was *decoderHandle
    decHandleOnce sync.Once
}

func (l *strippedImage) ensureDecHandle() {
    l.decHandleOnce.Do(func() {
        tag := opentile.CompressionToTIFFTag(l.compression)
        fac, _ := decoder.GetByCompressionTag(tag)
        if fac == nil { return }
        cap := runtime.NumCPU()
        if cap > 8 { cap = 8 }
        l.decHandle = decoderhandle.New(fac, cap)
    })
}
```

`strippedImage` continues to own its own pool instance per level
(rather than sharing the Slide-level pool) so the layer boundary
between `formats/ndpi` and `opentile` is preserved. The shared **type**
is what unifies; multiple instances of it within one Slide are fine
and the memory overhead is bounded by capacity × per-decoder work
buffer.

NDPI fast path code becomes:

```go
// Was: out, err := l.decHandle.Decode(jpegBytes, opts)
dec, err := l.decHandle.Borrow()
if err != nil { return nil, err }
defer l.decHandle.Return(dec)
out, err := dec.Decode(jpegBytes, opts)
```

Same shape inside `pixelCache.getOrLoad`'s load callback and inside
`decodedTileViaCrop`.

### 1.3. Slide-level pool cache for the slow path

`Slide` gains a per-codec pool cache:

```go
// opentile/slide.go — Slide struct extension
type Slide struct {
    r          slideReader
    handlesMu  sync.Mutex
    handles    map[uint16]*decoderhandle.Pool  // keyed by TIFF compression tag
}

// opentile/slide_decoder_cache.go (NEW)
func (s *Slide) decoderFor(tag uint16) (*decoderhandle.Pool, error)
```

`decoderFor` lazily creates a Pool for each codec tag the first time
it's needed and caches it under `handlesMu`. Pools are torn down by
`Slide.Close`.

### 1.4. Replace the v0.27 slow-path Decoder lifecycle

`Slide.ImageDecodedTile` and `Slide.ImageDecodedTileInto` replace
their per-call `fac.New() / dec.Close()` with `pool.Borrow() / pool.Return()`:

```go
// Was (v0.27):
dec := fac.New()
defer dec.Close()
return dec.Decode(compressed, ...)

// Becomes (v0.28):
pool, err := s.decoderFor(tag)
if err != nil { return nil, err }
dec, err := pool.Borrow()
if err != nil { return nil, err }
defer pool.Return(dec)
return dec.Decode(compressed, ...)
```

`Slide.Close` grows a drain loop that calls `Close` on every cached
pool before delegating to `s.r.Close()`.

### 1.5. NDPI's per-`strippedImage` pool gains multi-core throughput

The v0.27 NDPI fast path used a single mutex-serialized decoder, which
capped aggregate throughput at single-thread decode rate even under
`ScaledStrips`' NumCPU worker fanout. Migrating to `decoderhandle.Pool`
lifts that cap: NumCPU=8+ workers can now decode in parallel up to
the pool capacity. Single-thread throughput is unchanged (mutex
vs. channel-receive overhead is ~10 ns vs ~5 ns — negligible against
~50–300 µs decode).

### 1.6. New bench coverage

- **`cmd/bench/ndpi/main.go`** grows a `-goroutines N` flag (default 1
  preserves v0.27 single-thread semantics). When N > 1, the tile loop
  fans out to N goroutines sharing one `*Slide`.
- **`cmd/bench/svs/main.go`** NEW: SVS equivalent of bench-opentile.
  Iterates every L0 tile via `Slide.DecodedTile`. Same flags as the
  NDPI bench.
- **`Makefile`** gains `bench-ndpi-mt`, `bench-svs`, `bench-svs-mt`
  targets. `bench-svs` becomes a gated target after its baseline is
  measured during plan execution.

### 1.7. Tighten the NDPI throughput gate

`Makefile`: `MIN_NDPI_MPIXS` raised from 130 to **220** Mpix/s. The
v0.27 gate at 130 was set to the brief's "≥2× of openslide acceptable
floor" before the real v0.27 numbers were known; post-v0.27 we land
at ~243 Mpix/s, so the 130 gate was loose enough to hide a 40%
regression silently. Observed run-to-run variance is ~5%; 220 leaves
~10% margin from the 243 baseline.

## 2. Out of scope

- **NDPI consolidating its handle into the Slide-level pool.** Would
  require giving `formats/ndpi` a back-reference to `Slide`,
  crossing a layer boundary that the rest of the codebase respects.
  Deferred indefinitely unless multi-codec slides emerge that would
  benefit from cross-level sharing.
- **`sync.Pool` instead of fixed-channel pool.** Auto-shrink under GC
  pressure is appealing but makes teardown semantics non-deterministic
  (bench reproducibility suffers, `Close` correctness harder to test).
  Revisit in v0.29+ if memory pressure becomes a measured concern.
- **NDPI oneframe fast path.** Earlier brainstorming flagged this as
  v0.28 candidate, then the fixture survey showed oneframe fires only
  on tiny levels (≤1 MB RGB for CMU-1 L3; not at all on OS-2 or
  Hamamatsu, which are entirely striped). Tile counts are too small
  for a 5× per-tile win to be measurable. Deferred indefinitely —
  not a perf target.
- **NDPI oneframe + OME-TIFF reduced-resolution levels via
  `internal/oneframe`.** Same reasoning as above. OME-TIFF's main
  pyramid uses the tiled path (not oneframe); oneframe in OME only
  fires on macro/label pages which are read once for thumbnail
  display.
- **Refactoring `decoder.Factory` to pool internally.** Wider blast
  radius; changes the `Decoder.Close` contract semantics ("release"
  becomes "return to pool"). Not needed once Slide-level pooling is
  in place; defer pending a measured reason.
- **Public API additions.** No new exported symbols. No new public
  options for pool size or behavior. Pure internal optimization.
- **Eager pool initialization at Slide.Open time.** Lazy is cheaper
  for slides that don't hit `DecodedTile`. Capacity is a knob inside
  `decoderhandle.New`; not configurable from outside.
- **Cross-Slide pool sharing.** Each Slide owns its handles; no
  process-global pool. Avoids ownership/teardown complexity.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Scope of handle-reuse change | **Minimal — Slide slow path only.** Extract NDPI's decoderHandle to `internal/decoderhandle`; have `Slide.ImageDecodedTile` cache one Pool per (Slide, compression). Single primitive shared between NDPI fast path and Slide slow path. |
| Q2 | NDPI primitive migration | **Migrate — one primitive everywhere.** Move type to shared package; delete `formats/ndpi/decoder_handle.go`. NDPI imports the shared package; instance ownership stays per-`strippedImage`. |
| Q3 | Concurrency shape | **Fixed-size pool, `min(NumCPU, 8)` per (Slide, codec).** Buffered channel of N members; lazy-init on first Borrow; member lifetime tied to pool. Unlocks multi-core throughput; bounds memory. |
| Q4 | Lazy vs eager pool members | **Lazy.** Members created on first Borrow with `outstanding` counter under `initMu`. Avoids paying for unused capacity. |
| Q5 | Public API surface | **Zero additions.** All new types unexported / in `internal/`. No new options or knobs. |
| Q6 | NDPI handle instance scope | **Per-`strippedImage` instance** of the shared Pool type. Avoids cross-layer back-reference from `formats/ndpi` to `opentile`. |
| Q7 | Bench coverage for non-NDPI | **Add bench-svs.** Single-thread + multi-thread variants on `CMU-1.svs`. Establishes the cross-format win as a concrete number; bench-svs becomes gated after baseline measurement during plan execution. |
| Q8 | NDPI bench gate level | **Tighten from 130 to 220 Mpix/s.** v0.27 lands at ~243; 220 leaves ~10% margin for run-to-run variance, catches real regressions. |
| Q9 | Bench-ndpi-mt | **Add as positive-validation measurement.** No hard gate. Pre-v0.28: ≈ single-thread (mutex-capped). Post-v0.28: 3–8× single-thread on multi-core. If it doesn't show meaningful speedup, the pool isn't doing its job. |
| Q10 | `Slide.Close` lifecycle | **Drains every cached Pool before delegating to `s.r.Close`.** First-error semantics for collected errors; idempotent. |

## 4. Architecture

### 4.1. Dispatch flow

```
Slide.ImageDecodedTile(image, level, tx, ty, opts)
 ├─ (fast) if s.r.(decodedTiler) ok → NDPI fast path
 │            └─ strippedImage.pixelCache.getOrLoad
 │                  └─ on miss: strippedImage.decHandle.Borrow / Return
 │                              (NEW: pool-borrowed; v0.27 mutex-serialized)
 │
 └─ (slow) fallback: Slide.decoderFor(tag).Borrow / Return
                     (NEW: pool-borrowed per (Slide, codec))
```

### 4.2. Components added in v0.28

| File | Purpose | Status |
|---|---|---|
| `internal/decoderhandle/handle.go` | `Pool` type; lazy-init; Borrow/Return/Close; lock-order discipline | NEW |
| `internal/decoderhandle/handle_test.go` | Sequential, concurrent, lazy-creation, Close-race, double-Close, no-factory tests | NEW |
| `opentile/slide_decoder_cache.go` | `Slide.decoderFor(tag)` accessor; per-(Slide, codec) cache under `handlesMu` | NEW |
| `opentile/slide.go` | Add `handlesMu sync.Mutex` and `handles map[uint16]*decoderhandle.Pool` to `Slide`; extend `Close` with pool drain | EXTENDED |
| `opentile/slide_decoded_tile.go` | Replace `fac.New() / dec.Close()` in both `ImageDecodedTile` and `ImageDecodedTileInto` slow paths with `pool.Borrow() / pool.Return()` | EXTENDED |
| `opentile/slide_handle_test.go` | Slide-level integration: reuse, per-codec separation, Close-releases, concurrent | NEW |
| `formats/ndpi/stripped.go` | Field type change: `decHandle *decoderhandle.Pool`; `ensureDecHandle` calls `decoderhandle.New`; fast-path calls `Borrow`/`Return` | EXTENDED |
| `formats/ndpi/decoder_handle.go` | DELETE — superseded by `internal/decoderhandle` | REMOVED |
| `formats/ndpi/decoder_handle_test.go` | DELETE — tests subsumed by `internal/decoderhandle/handle_test.go` | REMOVED |
| `cmd/bench/ndpi/main.go` | Add `-goroutines N` flag; default 1 preserves v0.27 behavior | EXTENDED |
| `cmd/bench/svs/main.go` | NEW SVS bench: single-thread + `-goroutines` flag; iterates L0 via `Slide.DecodedTile` | NEW |
| `cmd/bench/svs/README.md` | Build / run instructions; expected numbers | NEW |
| `Makefile` | Bump `MIN_NDPI_MPIXS` 130 → 220; add `bench-ndpi-mt`, `bench-svs`, `bench-svs-mt` targets; `bench-svs` becomes gated after baseline | EXTENDED |
| `CHANGELOG.md` | v0.28.0 entry: measured pre/post numbers, sealed Qs, scope | EXTENDED |
| `CLAUDE.md` | Promote v0.28 to current milestone; demote v0.27 | EXTENDED |

### 4.3. Lock order

Acquire in this order; release in reverse. Consistent across every
call path so deadlock is structurally impossible.

1. `Slide.handlesMu` — short critical section: map lookup + lazy Pool
   creation. Released **before** `pool.Borrow()`.
2. `Pool.initMu` — short critical section inside `Borrow`: lazy member
   creation + `outstanding` counter update. Released **before** the
   decode call.
3. Nothing held during the cgo `decoder.Decode` call. Decode is lock-
   free against `Slide`, `Pool`, and v0.27's `pixelCache` / `frameMu`
   state.

For NDPI fast path: `pixelCache.mu` (v0.27, released before load) →
`frameMu` (v0.27, existing) → `Pool.initMu` (v0.28, inside Borrow).
All independent; all released before decode.

### 4.4. Concurrency invariants

- **Borrow racing with Close.** Goroutines blocked in `<-p.items`
  receive `(_, false)` from the closed channel and return `ErrClosed`.
  Goroutines past the `closed` check in Borrow's lazy-create branch
  may return a fresh Decoder that the closing goroutine never sees;
  the caller's `defer pool.Return(dec)` lands in `Return`'s closed-
  pool branch and closes the Decoder directly. No leak.
- **Return after Close.** `Return` re-checks `closed` under `initMu`
  and closes the Decoder directly. No double-close (Return owns the
  Decoder from Borrow onward; no other goroutine holds a reference).
- **Slide.Close racing with in-flight ImageDecodedTile.** Same
  contract as v0.27: `Slide.Close must not race with in-flight tile
  reads` (CLAUDE.md / `slide.go:48`). v0.28 doesn't relax this. A
  racing caller surfaces as `ErrClosed` from Borrow or as a clean
  error from Decode on an already-closed Decoder.
- **No dangling tjhandles.** Once `Pool.Close` returns, every Decoder
  the Pool ever issued has either been Returned-and-closed during
  the drain loop, or been Returned-and-closed by `Return`'s closed-
  pool branch. Decoders held mid-Decode complete and route through
  `Return`'s closed branch on `defer`.

### 4.5. Performance projection

Per-call cost eliminated: ~240 µs `tjDestroy` (measured in v0.27
profile: 7.18 s / 29800 tiles) + ~50 µs `tjInit` (estimated) =
**~290 µs/call avoided**. The dominant component is `tjDestroy`; the
total is rounded for headline use.

Single-thread bulk slide (30K tiles):
- v0.27 slow-path slide (every non-NDPI format): ~7 s saved (~15% improvement on bulk tile decoding).
- v0.27 NDPI fast-path slide: ~unchanged. The v0.27 path already eliminated the per-tile churn via NDPI's existing handle. Only edge tiles and oneframe slow-path fallbacks (small fraction) see the improvement.

Multi-thread (NumCPU=13 workers on Apple Silicon):
- v0.27 capped aggregate `Slide.DecodedTile` throughput at single-thread (~245 Mpix/s on CMU-1.ndpi) due to mutex.
- v0.28 scales aggregate up to pool capacity (= 8 on this machine) ≈ **~6–8× single-thread**.

Expected `bench-ndpi-mt` post-v0.28: ~1500–2000 Mpix/s aggregate (~6–8× of 243).
Expected `bench-svs` (single-thread, CMU-1.svs): baseline measured during
plan execution; ~15% improvement vs pre-v0.28.
Expected `bench-svs-mt`: ~6–8× single-thread.

## 5. Testing & parity gate

### 5.1. `internal/decoderhandle/handle_test.go` — pool unit tests

```go
TestPoolSequential              — N Borrow/Decode/Return; assert
                                  factory.New() called exactly 1 time
                                  (single member reused).
TestPoolConcurrent              — 32 goroutines, capacity=4; assert no
                                  race under -race, factory.New() called
                                  ≤ 4 times, every Decode succeeds.
TestPoolLazyCreation            — capacity=8, only 3 distinct borrows
                                  ever in flight; assert factory.New()
                                  called exactly 3 times (not 8).
TestPoolBorrowAfterClose        — Close, then Borrow; expect ErrClosed.
TestPoolReturnAfterClose        — Borrow, Close, Return; expect the
                                  Decoder is Closed exactly once.
TestPoolCloseRacesWithBorrow    — goroutine waits in Borrow; concurrent
                                  Close; assert ErrClosed propagated,
                                  no deadlock. -race -count=10.
TestPoolDoubleClose             — Close, Close; no panic, no error.
TestPoolNoDecoderFactory        — pool over a factory whose New()
                                  returns nil; assert propagation as
                                  error, not panic.
```

Tests use a fake `decoder.Decoder` that counts `New`/`Decode`/`Close`
calls and validates lifecycle invariants without touching libjpeg-
turbo. One real-codec smoke test (using `decoder/jpeg`) confirms the
end-to-end path works.

### 5.2. `opentile/slide_handle_test.go` — Slide integration

```go
TestSlideDecoderHandleReuse     — Open slide, DecodedTile × 100;
                                  exactly 1 underlying decoder.New().
TestSlideDecoderHandlePerCodec  — Two-codec slide; separate Pools per
                                  codec; no cross-codec sharing.
TestSlideCloseReleasesHandles   — Open → DecodedTile → Close; assert
                                  every Decoder is Closed exactly once.
TestSlideHandleConcurrent       — 32 goroutines hitting one Slide.
                                  DecodedTile under -race; no leak.
```

Uses an instrumented factory registered under a test compression tag
plus a fixture-backed real-codec end-to-end test.

### 5.3. NDPI regression coverage

The v0.27 test suite is the regression witness:

- `TestNDPIFastPathPixelParity` (in `stripped_decodedtile_test.go`)
- `TestNDPIFastPathConcurrent` (32-way fanout, in same file)
- `TestNDPIDecodeBlitParityFoundational` (in `stripped_pixel_parity_smoke_test.go`)
- `TestNDPIDecodedTilePathParity` (cross-fixture, in `tests/ndpi_decodedtile_parity_test.go`)
- `formats/ndpi/pixel_cache_test.go` (cache hit/miss/eviction/promise/thrash)

All run unchanged in intent. The only mechanical edit is **deleting**
`formats/ndpi/decoder_handle_test.go` (the type moved; its tests are
subsumed by `internal/decoderhandle/handle_test.go`).

### 5.4. TestSlideParity + Python oracle

40-fixture parity suite: **unchanged**. v0.28 is a decoder-lifecycle
refactor; pixel-output bit-identity is preserved.

### 5.5. Performance gates

| Gate | Threshold | Type | Rationale |
|---|---|---|---|
| `make bench-ndpi` | ≥ 220 Mpix/s | Hard (tightened from 130) | v0.27 lands at ~243; 220 = ~10% margin from baseline, ~5% from worst observed run. Catches regressions ≥5%. |
| `make bench-ndpi-mt` | none | Measurement | Multi-thread NDPI; positive validation that the pool unlocks parallelism. Pre-v0.28: ≈ single-thread. Post-v0.28: ~6–8× single-thread. |
| `make bench-svs` | measured-baseline × 0.95 | Hard (gate value set as a plan step after measuring post-v0.28 throughput) | Single-thread non-NDPI; the actual measured v0.28 deliverable. Plan task: run bench-svs once post-implementation, record throughput, set `MIN_SVS_MPIXS` Makefile var to 95% of measured, commit. |
| `make bench-svs-mt` | none | Measurement | Multi-thread SVS; validates cross-format pool win. |

### 5.6. Coverage

`make cover` ≥80% per package (existing bar). New `internal/decoderhandle/`
exceeds easily (small surface, broad test set). NDPI package coverage
unchanged (deleted tests replaced by shared-package equivalents).

### 5.7. What's explicitly NOT tested

- Format-by-format byte equivalence under pool. The pool returns the
  same `decoder.Decoder` type as `fac.New()`; output bytes can't
  differ.
- Codec implementations themselves (untouched).
- v0.27 NDPI fast-path semantics (preserved by construction; v0.27's
  test suite is the regression witness).

## 6. Performance projection & success criteria

Reference numbers (Apple Silicon, 13 cores, single-thread unless noted):

| Path | Wall | Throughput |
|---|---|---|
| **v0.27 baseline** | | |
| `bench-ndpi` (CMU-1.ndpi L0, 29800 tiles) | 8.03 s | 243.1 Mpix/s |
| **v0.28 projections** | | |
| `bench-ndpi` (single-thread, regression check) | ~8 s | ~240 Mpix/s |
| `bench-ndpi-mt` (NumCPU=13 → pool cap 8) | ~1.3 s | ~1500–2000 Mpix/s |
| `bench-svs` (CMU-1.svs, post-v0.28) | measured during plan | ~15% above pre-v0.28 baseline |
| `bench-svs-mt` (NumCPU=13 → pool cap 8) | measured during plan | ~6–8× single-thread |

### Success criteria

- **Mandatory:** All v0.27 tests + new pool/Slide tests green under
  `-race -count=1`. TestSlideParity zero divergence.
- **Mandatory:** `make bench-ndpi` ≥ 220 Mpix/s.
- **Mandatory:** `make bench-ndpi-mt` ≥ 3× single-thread (the pool
  visibly unlocks parallelism, not just adds code).
- **Mandatory:** `make bench-svs` gate (set during plan execution
  after baseline measurement) passes.
- **Acceptable:** Cross-format perf improvement ≥10% on `bench-svs`
  vs pre-v0.28 baseline.
- **Stretch:** ≥6× multi-thread on `bench-ndpi-mt` (full pool
  utilization).

## 7. Open items and follow-ups

- **NDPI handle instance consolidation.** Sharing one Slide-level
  pool across all NDPI levels (instead of per-strippedImage) would
  marginally reduce memory but requires `formats/ndpi` knowing about
  `Slide`. Deferred indefinitely.
- **`ScaledStrips` pool-cap sensitivity.** Pool cap = 8 means
  ScaledStrips with NumCPU > 8 will queue some workers. Acceptable
  per design discussion (queue wait ~5 × 300 µs ≈ 1.5 ms p99).
  Revisit if real-world workloads show contention.
- **sync.Pool migration.** If memory pressure becomes a measured
  concern (many concurrent Slides on a long-running server), switch
  `decoderhandle.Pool` from fixed-channel to `sync.Pool`. Trade-off:
  auto-shrink under GC vs deterministic teardown.
- **JPEG-frame cache bounding** (v0.27 deferred item). Independent
  of v0.28; unchanged.
- **NDPI oneframe path** (originally a v0.28 candidate, dropped after
  fixture survey showed negligible impact). Will not be revisited
  unless a workload surfaces with measurable oneframe-path tile
  volume.
- **`tests/oracle/`** build break from v0.24 Level-method drift. Pre-
  existing; flagged but out of v0.28 scope.

## 8. References

- v0.27 spec (the foundation this builds on):
  `docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md`
- v0.27 plan:
  `docs/superpowers/plans/2026-05-28-opentile-go-v27-ndpi-pixel-cache.md`
- v0.27 CHANGELOG entry: `CHANGELOG.md` §[0.27.0]
- v0.27 `formats/ndpi/decoder_handle.go` (the primitive being
  extracted): `formats/ndpi/decoder_handle.go`
- v0.27 dispatch site:
  `opentile/slide_decoded_tile.go:36-85` (`ImageDecodedTile` + `Into`)
- v0.27 NDPI strippedImage fast path:
  `formats/ndpi/stripped.go` (DecodedTile + pixelCache + decHandle)
- v0.27 spec §2 ("`ScaledStrips` decoder-handle pool" — flagged as
  the v0.28 lever): `docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md` §2
- v0.27 bench infrastructure (the model for `cmd/bench/svs/`):
  `cmd/bench/ndpi/main.go`, `cmd/bench/ndpi/openslide_ref/bench-openslide.c`, `cmd/bench/ndpi/README.md`
- v0.27 perf gate (the model for `bench-svs` gating + `bench-ndpi`
  tightening): `Makefile` `bench-ndpi` target
