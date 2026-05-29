# opentile-go v0.27 — NDPI striped fast pixel path (decode-once-per-strip + blit)

**Status:** sealed 2026-05-28.
**Work branch:** `feat/v0.27`.
**Headline:** Closes the per-thread perf gap between opentile-go and openslide on NDPI tile decode. v0.26 measured ~5× slower (44.25 s vs 8.38 s on CMU-1.ndpi, single-threaded; throughput 44.1 vs 233 Mpix/s). The root cause is per-tile `tjTransform` (lossless JPEG-domain crop) plus per-tile decoder-handle churn — 95% of CPU is spent in two libjpeg-turbo passes per tile, while actual decode is 3% of CPU. v0.27 adds a decoded-pixel-frame cache inside `formats/ndpi/strippedImage` parallel to the existing JPEG-frame cache: each strip is decoded once via a reusable decoder handle, and per-tile requests blit a region out of the cached pixels. RawTile (compressed bytes API) is unchanged. Projected single-thread throughput on CMU-1.ndpi: ~150 Mpix/s (1.5× of openslide; brief's stretch target).

## 1. Scope

### 1.1. New decoded-pixel-frame cache on `strippedImage`

A bounded LRU cache of decoded RGB frames lives per `strippedImage` instance, alongside the existing unbounded JPEG-frame cache. Cache shape:

```go
// formats/ndpi/pixel_cache.go (NEW)

// pixelFrameCache is a small bounded LRU of decoded RGB frames keyed by
// (framePos, frameSize). Per-strippedImage instance.
type pixelFrameCache struct {
    mu       sync.Mutex
    capacity int                              // max(runtime.NumCPU(), 16)
    entries  map[frameKey]*pixelFrameEntry
    order    *list.List                        // LRU recency; front = MRU
}

type pixelFrameEntry struct {
    pix   *decoder.Image  // RGB at frame resolution
    elem  *list.Element   // back-pointer into order
    ready chan struct{}   // closed when pix/err populated
    err   error
}
```

The cache uses a **promise / ready-channel pattern**: a goroutine that misses on a key inserts a reserved entry under the cache mutex, releases the mutex, performs the slow decode, then closes the entry's `ready` channel. Concurrent lookups for the same key block on `<-ready` rather than re-decoding. Net effect: one decode per cache miss regardless of fanout.

### 1.2. Reusable decoder handle on `strippedImage`

Adds one long-lived libjpeg-turbo decoder handle per `strippedImage`, replacing today's per-tile `fac.New() ... defer dec.Close()` pattern. The handle is created lazily at first DecodedTile call (via `sync.Once`) and freed when the parent `format.Reader` closes. The decode call is serialized by a mutex because `tjhandle` is not concurrent-safe; this is fine because the pixel cache absorbs most calls (~1 decode per ~16 tile requests in row-major iteration), so the contention window is small.

### 1.3. Optional `decodedTiler` interface for dispatch

```go
// opentile/slide_decoded_tile.go — NEW unexported interface

type decodedTiler interface {
    ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error)
}
```

`Slide.ImageDecodedTile` does a type assertion: if `s.r.(decodedTiler)` matches, the fast path is used; otherwise the existing `s.r.ImageRawTile` + `fac.New().Decode().Close()` fallback runs unchanged. Non-NDPI readers don't grow this method and route through the fallback automatically.

NDPI's `format.Reader` implements `ImageDecodedTile` by looking up the level and delegating to `strippedImage.DecodedTile(tx, ty, opts)`. Non-striped NDPI levels (oneframe, associated images) don't satisfy the interface yet and route through the fallback — they're not affected by v0.27.

### 1.4. Fast-path tile assembly

For an **interior tile** (does not extend past image bounds):

1. Compute `frameSize := l.frameSizeForTile(tx, ty)` and `framePos := l.framePosition(...)` — existing logic.
2. `pixFrame, err := l.pixelCache.getOrLoad(key, loadFn)` where `loadFn`:
   1. Calls `l.getFrame(framePos, frameSize)` — existing JPEG-frame cache; returns assembled JPEG bytes.
   2. Acquires `l.decHandle.mu`, calls `l.decHandle.dec.Decode(jpegBytes, {Format: RGB})`, releases mutex.
3. Allocate a fresh `*decoder.Image` at `tileSize`; blit `pixFrame[tileLeft:tileLeft+tileW, tileTop:tileTop+tileH]` into it.
4. If `cfg.format == RGBA`, blit widens to RGBA; if `cfg.format == RGB`, blit is a per-row `copy()`. `WithScale != 1` falls through to the slow path (see §6).

For an **edge tile** (extends past image bounds): falls through to the existing path — `l.Tile(tx, ty)` (which calls `jpegturbo.CropWithBackgroundLuminanceOpts` and produces the white-fill DC-coefficient pixels Python opentile produces) followed by `l.decHandle.dec.Decode(jpegBytes, cfg)`. Edge tiles are <1% of any level; preserving byte-identity here costs ~nothing.

### 1.5. RawTile path unchanged

`RawTile`, `RawTileInto`, `TilePrefix`, `TileBodyInto`, the `iter.Seq2`-based tile iterator, and the splice-prefix optimization family are all bit-for-bit unchanged from v0.26. The existing JPEG-frame cache continues to be populated lazily by both RawTile and the new fast path (which uses it as the decode source). Bit-determinism of the compressed-bytes API is preserved.

### 1.6. ReadRegion / ScaledStrips inherit the speedup

`Slide.imageReadRegionImpl` and `ScaledStrips`'s per-tile worker call `Slide.ImageDecodedTile` and inherit the new dispatch automatically. No changes to `slide_region.go`, `slide_region_scaled.go`, or `strip_iterator.go`. ScaledStrips' worker fanout (NumCPU goroutines) is the motivation for `capacity = max(NumCPU, 16)` — each worker tends to operate in a different strip simultaneously.

## 2. Out of scope

- **NDPI oneframe path** (`internal/oneframe/oneframe.go`). Hamamatsu-1.ndpi (6.6 GB) takes this path. Same algorithmic opportunity; deferred to follow-up milestone (likely v0.28) once the striped fix is benched and validated. The oneframe `sync.Once`-based cache and the strippedImage cache are structurally similar but distinct codepaths.
- **Tactical handle pooling for the RawTile path** (the existing `tjTransform`-per-tile + `tjDestroy`-per-tile churn that costs ~7 s on the bench). Deferred — measured per the brief in v0.28 if the v0.27 numbers don't hit stretch. Doesn't affect DecodedTile, which gets its own reusable handle in v0.27.
- **`ScaledStrips` decoder-handle pool** (replacing the per-strippedImage mutex with a small fixed `chan *Decoder` for true concurrent decode under NumCPU fanout). Mutex-per-strippedImage is sufficient for v0.27 because the pixel cache absorbs ~94% of decode calls; only ~150 decodes per worker queue through the mutex on a CMU-1 L0 walk. Revisit in v0.28 if NumCPU fanout shows contention.
- **JPEG-frame cache bounding.** Today's `frameMu` + `framesByKey` map is unbounded (CLAUDE.md stripped.go:67-73 documents this as acceptable). v0.27 doesn't change it. Pre-existing latent concern (~200 MB on CMU-1 L0); flagged for later if real-world workloads ever hit it.
- **Public API additions** (no `WithStripPixelCacheMB(n)`, no `Slide.DecodedStrips` iterator, no exported pixelFrameCache type). Q-sealed as purely internal.
- **`WithScale` (IDCT-time downscaling) integration with the pixel cache.** Q-sealed: `Scale != 1` falls through to the existing per-tile decode path for that single call. The cache holds full-resolution frames only. See §6 "open items" for the long-term consideration.
- **`*Slide.RawTile` re-encode** from cached pixels. Q-sealed as the wrong call — would break bit-identity with file-original JPEG bytes (consumers including wsitools' splice template path require bit-identity).
- **Generalized strip-pixel-cache infrastructure** in a new `internal/pixelcache/` package. v0.27 scope is single-format; if oneframe and other formats adopt the same pattern in v0.28+, factor then.
- **`v1.0` cut.** Still pending; v0.27 is a perf milestone, not an API milestone.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Lever choice | **Architectural first** (pixel cache + blit). Rebench after shipping. Tactical (handle pooling for RawTile path) deferred to v0.28 if architectural alone doesn't hit stretch. |
| Q2 | Format scope | **NDPI striped only** (`formats/ndpi/stripped.go`). oneframe deferred. Associated images and other formats unchanged. |
| Q3 | API surface | **Purely internal.** No new public symbols. `ReadRegion`/`DecodedTile`/`ScaledStrips` signatures unchanged. `decodedTiler` interface is unexported. |
| Q4 | Cache policy | **Small bounded LRU**, capacity = `max(runtime.NumCPU(), 16)`. FIFO-by-recency eviction. No public knob. |
| Q5 | Edge tiles | **Keep current path for edge tiles only.** Interior tiles (~99%) take the fast path; edge tiles fall through to `CropWithBackgroundLuminanceOpts` + decode. Preserves Python-opentile pixel parity on edge tiles. |
| Q6 | RawTile + existing JPEG cache | **Leave RawTile and JPEG-frame cache exactly as today.** v0.27 is purely additive on the DecodedTile side. JPEG cache stays unbounded (latent concern; CLAUDE.md stripped.go:67-73 documents it as acceptable). |
| Q7 | Decoder-handle concurrency | **One handle per `strippedImage`, serialized by mutex.** Pixel cache absorbs most decodes so contention window is small. `sync.Pool` / fixed-`chan` upgrade deferred to v0.28 if benched contention shows up. |
| Q8 | Dispatch shape | **Optional unexported `decodedTiler` interface** with type assertion in `Slide.ImageDecodedTile`. Non-NDPI readers don't grow methods; clean fallback. |
| Q9 | Cache content shape | **Single canonical RGB frame at frame resolution** (typically ~4096×256, not level resolution). Caller's `cfg.format` (RGB vs RGBA) applied at blit-out step (widening copy). Multiple callers asking for different formats hit the same cache entry. |
| Q10 | `WithScale != 1` handling | **Fall through to existing slow path** for that call. Cache holds full-resolution frames only. Rare branch (bench doesn't use it; ScaledStrips does its own resample). |

## 4. Architecture

### 4.1. Dispatch flow

```
Slide.ImageDecodedTile(image, level, tx, ty, opts)
 ├─ (fast) if s.r.(decodedTiler) ok → s.r.ImageDecodedTile(image, level, tx, ty, opts)
 │                                       → NDPI reader.ImageDecodedTile
 │                                            → strippedImage.DecodedTile(tx, ty, cfg)
 │                                                 ├─ interior tile → pixelCache.getOrLoad → blit
 │                                                 └─ edge tile     → existing path
 │
 └─ (slow) fallback: s.r.ImageRawTile(...) → fac.New().Decode().Close()
```

### 4.2. Components added in v0.27

| File | Purpose | Status |
|---|---|---|
| `formats/ndpi/pixel_cache.go` | `pixelFrameCache` type, `getOrLoad` promise pattern, LRU eviction | NEW |
| `formats/ndpi/decoder_handle.go` | `decoderHandle` struct with mutex; ctor + Close | NEW |
| `formats/ndpi/stripped.go` | Add `pixelCache *pixelFrameCache`, `decHandle *decoderHandle`, `decHandleOnce sync.Once` fields to `strippedImage`; add `DecodedTile(tx, ty, cfg) (*decoder.Image, error)` method; add `closeResources()` for handle lifecycle | EXTENDED |
| `formats/ndpi/tiler.go` | Extend the `tiler` struct (the NDPI `format.Reader` impl, line 38) with an `ImageDecodedTile(image, level, tx, ty, cfg)` method. Lookup level via existing logic; type-assert to `*strippedImage`. If assertion succeeds, delegate to `strippedImage.DecodedTile`. If it fails (oneframe, associated, etc.), return `fastpath.ErrUnsupported`. | EXTENDED |
| `slide_decoded_tile.go` | Add unexported `decodedTiler` interface. Modify `Slide.ImageDecodedTile` to type-assert on `s.r.(decodedTiler)`. If matched: call `dr.ImageDecodedTile(...)`. If it returns `fastpath.ErrUnsupported` (see new internal package below), fall through to the existing slow path. Any other error propagates. If `s.r` doesn't satisfy the interface (non-NDPI reader), the slow path runs as today. | EXTENDED |
| `internal/fastpath/sentinel.go` | NEW tiny package: `var ErrUnsupported = errors.New("opentile: fast path unsupported")`. Imported by both `opentile` (root) and `formats/ndpi`. Keeps the sentinel out of the public API while letting both packages reference the same value for `errors.Is` comparison. | NEW |
| `formats/ndpi/pixel_cache_test.go` | Unit tests for hit/miss/promise/eviction/concurrent races | NEW |
| `formats/ndpi/stripped_decodedtile_test.go` | Pixel-parity tests (fast vs slow path; interior vs edge); concurrency tests | NEW |
| `cmd/bench/ndpi/main.go` (or `internal/bench/ndpi/`) | Move the bench-opentile program from `/tmp/ndpi-bench/` into the repo for regression tracking. Add `make bench-ndpi` target that fails if throughput < 130 Mpix/s on CMU-1.ndpi | NEW |

### 4.3. Components unchanged

- `internal/jpegturbo/` (Crop, CropWithBackgroundLuminance, FillFrame, etc.) — still called for edge tiles, RawTile callers, associated images, Philips sparse fill. No code change.
- `internal/oneframe/` — out of scope for v0.27.
- `decoder/jpeg/` — `cgoDecoder` continues to be used; the new path holds one instance longer rather than creating new ones per tile.
- All other format readers, all other internal packages.

### 4.4. Lock order

Acquire in this order; release in reverse. Same order across every code path so deadlock is impossible.

1. `pixelCache.mu` — short critical section: map lookup + LRU bookkeeping. Released **before** the slow decode runs.
2. `frameMu` (existing) — short critical section: JPEG-frame map lookup + double-checked populate. Independent of `pixelCache.mu`; called from inside the `loadFn` after `pixelCache.mu` is released.
3. `decHandle.mu` — held during the cgo decode call (~1.4 ms). Acquired from inside `loadFn` after the JPEG frame is in hand.

`headerMu` (existing, for the patched-header cache) is independent of the new locks and acquired only inside the existing assembly path.

## 5. Testing & parity gate

The correctness bar is **byte-identical decoded pixels vs. v0.26** on every NDPI tile, plus byte-parity with Python opentile on the existing oracle.

### 5.1. Layer 1 — pixel parity unit test (foundational)

`formats/ndpi/stripped_decodedtile_test.go` (NEW):

```go
// For every NDPI fixture, for every interior tile, assert that
// strippedImage.DecodedTile(tx, ty) returns the same pixels as the
// current path: Tile() (JPEG) → tjDecompress2.
func TestNDPIDecodedTilePixelParity(t *testing.T) { ... }
```

This is the **hard gate** — TJXOPT_PERFECT is MCU-aligned and IDCT is 8×8-block-local, so decode-then-blit should produce bit-identical pixels to crop-then-decode. If this test fails, the foundational assumption of v0.27 is wrong and the design has to either accept documented ±1 LSB divergence or abandon the fast path.

**Run this test first during plan execution**, before building the cache, decoder-handle, or interface plumbing. Fail-fast on the foundational assumption.

Edge tiles are skipped (or asserted via the fallback identity path — same effect).

### 5.2. Layer 2 — TestSlideParity regression

The existing 40-fixture parity suite covers NDPI fixtures via `s.RawTile()`. Extend to also call `s.DecodedTile()` on NDPI fixtures and compare pixels against a committed snapshot. Catches drift in the fast path during future maintenance. Snapshot regeneration happens once as part of the v0.27 PR.

### 5.3. Layer 3 — Python parity oracle

Existing `tests/oracle` already compares NDPI tile decodes against Python opentile (`go test -tags parity`). Pixel-level oracle, no code change. Rerun under v0.27 and confirm zero divergence.

### 5.4. Concurrency tests

- `make test` already runs `-race -count=1` — catches locking mistakes in the cache.
- New explicit test: launch 32 goroutines hitting `strippedImage.DecodedTile` with overlapping and non-overlapping `(tx, ty)` patterns. Assert no deadlock and pixel identity vs. serial reference.
- Cache thrash test: capacity=2 cache with 10 distinct frames accessed round-robin. Assert correctness still holds under heavy eviction.

### 5.5. Performance gate

`make bench-ndpi` (NEW Makefile target) runs `cmd/bench/ndpi` against CMU-1.ndpi and fails if throughput drops below **130 Mpix/s** on the test machine. This is the brief's "acceptable" target (≤2× of openslide) with a small margin. The stretch target (1.5× = ~155 Mpix/s) is the v0.27 success signal but isn't gated.

The openslide reference number is hardware-dependent and not gated in CI; the gate is on opentile-go's absolute throughput.

### 5.6. Coverage

`make cover` ≥80% per package (existing bar). New `pixelFrameCache` type needs unit tests covering: hit, miss-then-populate, concurrent population (promise wait), eviction order, eviction-during-in-flight-load, error propagation through `ready` chan.

### 5.7. What doesn't need new tests

- RawTile behavior — unchanged, existing tests cover.
- Non-NDPI formats — unchanged, existing tests cover.
- ScaledStrips behavior — calls `Slide.ImageDecodedTile`; inheritance only.
- `internal/jpegturbo` — unchanged.

## 6. Performance projection & success criteria

Reference numbers (Apple Silicon, 13 cores, single-thread, CMU-1.ndpi, 29,800 tiles at 256×256):

| Path | Wall | Throughput | Ratio to openslide |
|---|---|---|---|
| openslide 4.0.0 (C) | 8.38 s | 233.0 Mpix/s | 1.00× |
| opentile-go v0.26 | 44.25 s | 44.1 Mpix/s | 5.28× slower |
| opentile-go v0.27 projection | ~7–8 s | ~150 Mpix/s | ~1.5× slower |

The projection is derived from the v0.26 profile breakdown:
- v0.26 spends 33.45 s in `tjTransform`, 7.18 s in per-tile `tjDestroy`, 1.40 s in `tjDecompress2`.
- v0.27 eliminates `tjTransform` on the interior path entirely (replaced by blit, which is memcpy-cost, ~0.5 s budgeted).
- v0.27 eliminates per-tile `tjDestroy` (one decoder handle for the level lifetime).
- v0.27 decodes ~1900 strip-frames instead of 29,800 tile-sized JPEGs — total decode work is approximately the same (~1.4 s, since the frames are 16× larger but there are 16× fewer of them).
- Edge tiles (~1%) keep the existing path: ~0.4 s.

Sum: 1.4 (decode) + 0.5 (blit) + 0.4 (edge) + ~0.5 (cache + LRU overhead) ≈ 3 s of CPU work in cgo+Go land. Add ~4 s of unmodeled overhead (cgo crossings, map ops, allocations) → projection ~7–8 s.

### Success criteria

- **Foundational:** `TestNDPIDecodedTilePixelParity` passes on every interior tile of CMU-1.ndpi and OS-2.ndpi. Mandatory; no v0.27 ships without this.
- **Acceptable:** Throughput ≥ 100 Mpix/s on CMU-1.ndpi (≤2.5× slower than openslide). Below this, the architectural lever didn't deliver enough; reopen the design.
- **Stretch:** Throughput ≥ 155 Mpix/s on CMU-1.ndpi (≤1.5× slower than openslide). v0.27 success signal.
- **Hard gate (Makefile):** `make bench-ndpi` passes at ≥ 130 Mpix/s. Catches regressions in future commits.
- **Parity:** TestSlideParity (40 fixtures) and the Python parity oracle both green.
- **Coverage:** `make cover` ≥80% per package.

## 7. Open items and follow-ups

- **`WithScale != 1` and the pixel cache.** v0.27 punts: `Scale != 1` falls through to the existing per-tile decode path. Long-term options: (a) keep per-scale-value caches (memory blow-up); (b) downsample cached pixels per call (different result than IDCT scale, semantic shift); (c) bypass cache for `Scale != 1` (current decision). Not blocking v0.27 — flagged for v1.0 API review.
- **NDPI oneframe path.** Same algorithmic opportunity, different fixture family (Hamamatsu-1.ndpi). Likely v0.28 if v0.27 numbers justify continuing. Tracked in deferred.md after v0.27 ships.
- **Tactical handle pooling for RawTile.** ~7 s of `tjTransform`/`tjDestroy` churn on the RawTile-driven bench remains. Mostly irrelevant to wsitools' splice-template path (which iterates strips, not per-tile) but a clean follow-up if needed.
- **`ScaledStrips` decoder-handle pool.** Single mutex around the per-strippedImage decoder is a contention point under NumCPU fanout (estimated ~210 ms of queueing per worker on CMU-1; invisible vs the wall time but visible under heavy concurrent load). Upgrade to a fixed-size `chan *Decoder` pool if v0.28 benchmarks justify.
- **JPEG-frame cache bounding.** Pre-existing unbounded growth (~200 MB on CMU-1 L0); CLAUDE.md stripped.go:67-73 acknowledges. Not made worse by v0.27. Future LRU bound is an independent improvement.
- **Move bench programs into the repo.** The `bench-opentile` and `bench-openslide` programs currently live in `/tmp/ndpi-bench/` (built ad-hoc during the investigation). v0.27 plan should move them into `cmd/bench/ndpi/` or `internal/bench/` and commit them so regression tracking is reproducible. The C reference (`bench-openslide.c`) can live in the same directory with build instructions in a README.

## 8. References

- Investigation brief (handoff from wsitools session): `docs/superpowers/notes/2026-05-28-ndpi-perf-vs-openslide.md`
- v0.26 baseline profile (captured 2026-05-28): `/tmp/ndpi-bench/bench-opentile/cpu.prof`. Top hot spots: `tjTransform` 78.5%, `tjDestroy` 16.9%, `tjDecompress2` 3.3% (98.85% of CPU in cgo). Move into the repo as part of the v0.27 PR if useful for the reviewer.
- v0.26 striped reader: `formats/ndpi/stripped.go`. The cache-extension surface lives here.
- v0.26 `Slide.ImageDecodedTile` dispatch: `slide_decoded_tile.go:36-58`. The fallback path stays as-is; the new fast path is layered in via type assertion.
- v0.26 `imageReadRegionImpl`: `slide_region.go:69-146`. Loops `tx, ty` and calls `s.ImageDecodedTile` per tile — inherits the speedup with no code change.
- v0.26 ScaledStrips: `strip_iterator.go:180-249`. Calls `Slide.ImageDecodedTile` through its `it.cache.waitGet` worker queue — also inherits the speedup.
- openslide NDPI loader (for high-level comparison only — do not copy code): `src/openslide-vendor-hamamatsu.c` in the openslide source tree.
- Python opentile `NdpiStripedImage`: `opentile/formats/ndpi/ndpi_image.py:408-580`. Upstream port reference for the existing striped logic; not changed in v0.27.
