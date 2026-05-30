# opentile-go v0.30 — read-path memory-budget milestone

**Status:** REVISED post-profile (2026-05-30) — root cause re-attributed
from a real `inuse_space` heap profile; the original geometry-based
hypothesis (C2-dominant) is **falsified** and corrected below.
**Author:** investigation triggered from wsitools `convert --to dzi` OOM
**Work branch (proposed):** `feat/v0.30-memory-budget`

---

## 0. Problem statement

`wsitools convert --to dzi` on Hamamatsu NDPI slides drove a 16 GB Mac
into macOS "Your system has run out of application memory" during
benchmarking. The user associated it with the in-flight opentile-go
v0.26 → v0.29 bump, but **v0.29 is not the cause**.

### 0.1. Evidence v0.29 is exonerated

Built wsitools against both opentile-go versions; identical DZI
conversion, peak RSS sampled at 2 Hz with an external watchdog:

| Fixture (`convert --to dzi`) | opentile-go v0.26.0 | opentile-go v0.29.0 |
|---|---|---|
| CMU-1.ndpi (L0 51200×38144) | **5778 MB** | **5832 MB** |

Statistically identical. The three caches added in v0.27–v0.29
(`borrowTileScratch` `sync.Pool`, NDPI `pixelFrameCache`,
`decoderhandle.Pool`) are individually bounded, and the scratch pool is
not even on the `ScaledStrips` → `ImageDecodedTile` path (it is
`ReadRegion`-only). The bump is a clean drop-in; the memory behaviour
predates it.

### 0.2. The two measurement contexts (don't conflate them)

The original investigation sampled **total wsitools-process RSS**, which
mixes two independent width-proportional consumers:

| Fixture (total wsitools RSS) | L0 dims | peak RSS | shape |
|---|---|---|---|
| CMU-1.ndpi | 51200×38144 | 5.8 GB | climbs through L0 |
| OS-2.ndpi  | 126976×73728 | 6.8 GB → 3.5 GB steady | early L0 transient |

That total = **(A) opentile-go's read-path caches** + **(B) wsitools'
own DZI levelBuilder cascade** (full-width parent rows buffered across
~9 pyramid levels). The two scale with width independently. Attributing
the peak required isolating (A) — see §0.3.

### 0.3. Root cause — measured, not inferred

**Reproduction harness (in-tree):** `cmd/bench/ndpi-strips/` drives the
exact wsitools DZI top-of-cascade iterator — full-L0 `l0Rect`, identity
`outSize`, `stripHeight = dziTile`, Nearest kernel, `workers = NumCPU`,
`lookahead = 2` — and snapshots an `inuse_space` heap profile at the
HeapInuse peak. The consumer **drops** each strip, so the only resident
bytes are opentile-go's own — isolating (A) from wsitools' cascade (B).
This is also the basis of the new peak-RSS gate (§1.5).

**Measured live-set attribution (worst case — no consumer backpressure):**

| # | Term | Where | CMU-1 @256 | CMU-1 @1024 | OS-2 @256 | OS-2 @1024 | Scales with |
|---|---|---|---|---|---|---|---|
| **C1** | StripIterator decoded-tile cache | `strip_cache.go`, `strip_iterator.go:90` | **2159 MB** | **2452 MB** | **5816 MB** | **~6300 MB** | width × workers × (lookahead+1) |
| C3 | NDPI `framesByKey` (UNBOUNDED) | `formats/ndpi/stripped.go:78,514` | 70 MB | 176 MB | 519 MB | 589 MB | level area |
| C2 | NDPI `pixelCache` | `formats/ndpi/pixel_cache.go`, `stripped.go:134` | 118 MB | 230 MB | ~100 MB | 685 MB | band width × count |
| **C4** | in-flight output strip buffers | `strip_iterator.go:154` (`Next`) | 37 MB | 150 MB | 186 MB | **1116 MB** | width × **dziTile** × lookahead |
| | **live set total** | | ~2.4 GB | ~3.0 GB | ~6.3 GB | ~8.7 GB | |
| | **peak HeapInuse** (GOGC≈2×) | | 4942 | 5723 | 12895 | 15125 | |

**Four corrections to the original (geometry-based) hypothesis:**

1. **C1 dominates every fixture** (~2.2 GB CMU-1, ~6 GB OS-2) — it is the
   primary target, not the last layer. It is the per-iterator `tileCache`
   of **decoded `*decoder.Image` tiles** that `decodeWorker` stores; its
   count bound `workers×(lookahead+1)×tilesPerStripWidth` ignores tile
   bytes and grows with level width.
2. **C2 is the *smallest* term (~0.1–0.7 GB), not the 3.1 GB OS-2
   dominator the draft claimed.** The geometry overestimated it ~5–10×.
   The draft's "OS-2 = C2" conclusion was an artifact of measuring total
   RSS (it was seeing wsitools' cascade (B), which the iterator knob can't
   touch — hence the "OS-2 unchanged" result in the draft's experiment).
3. **C4 is a fourth term the three-cache model omitted:** the output
   strip buffer is **irreducible working memory**, not a cache —
   negligible at dziTile 256 but **>1 GB at dziTile 1024** on a wide
   slide (DZI tiles are routinely 512/1024, not just the 256 default).
   It can only be bounded by reducing lookahead, not by eviction.
4. **GOGC=100 roughly doubles live→peak.** Much of the "5.8 GB" is GC
   headroom, not live cache (CMU-1 live ~2.2 GB → HeapInuse 4.9 GB). This
   makes `GOMEMLIMIT` a first-class lever (§1.6), not a footnote.

---

## 1. Scope

A single unifying change: **bound the read path by a byte budget** —
across the decoded-tile cache (C1), the two NDPI caches (C3, C2), and the
lookahead window that governs irreducible strip buffers (C4) — plus
honouring `GOMEMLIMIT` to clamp GC headroom. One per-Slide budget,
arbitrated across consumers in priority order **C1 ≫ C3 > C4 > C2**.

### 1.1. Layer 1 — byte-budget the StripIterator tile cache (C1) *[dominant win]*

`cacheCapacity = workers × (lookahead+1) × tilesPerStripWidth`
(`strip_iterator.go:90`) is a tile *count* that ignores tile *bytes* and
grows with level width. Re-express as a byte budget.

- Track running byte total (`Σ len(entry.img.Pix)` for ready entries);
  evict LRU (refCount==0) until `total ≤ budget`.
- The existing `tileCache` already has refCount pinning + LRU
  (`strip_cache.go`); add a byte counter updated under the same `mu` and
  switch `evictLocked`'s `len >= capacity` test to a byte test.
- **Floor:** `≥ workers` ready tiles (one in-flight result per worker)
  so eviction never starves a producing worker into livelock.

### 1.2. Layer 2 — bound `framesByKey` (C3) *[only unbounded term; cheapest correctness fix]*

C3 is the **only term with no ceiling at all** — it grows with level
area on an arbitrarily large slide, so it is the genuine hard-OOM risk on
untested wide fixtures (Hamamatsu-1). On the single-pass row-major DZI
traversal it provides ~zero benefit: each assembled frame is read once on
the `pixelCache` *miss* (`stripped.go:312` `getOrLoad` → `getFrame`);
every later tile in that frame hits `pixelCache` and never re-reads C3
(verified in code).

- Convert `framesByKey` from an unbounded `map[frameKey][]byte` to a
  small bounded LRU (same structure as `pixelFrameCache`).
- Keep capacity ≥ one frame-row so the slow `Tile()`-only adjacent-tile
  reuse (R4) doesn't regress.

### 1.3. Layer 3 — graceful lookahead under pressure (C4 + C1 interaction)

The output strip buffer (C4) and C1 both scale with the lookahead window.
Bound the window by **bytes**, degrading lookahead gracefully (Q4-sealed):

- Compute the largest lookahead that fits `budget − (C1 floor + C2 + C3)`
  given the level's per-strip-buffer bytes (`outSize.X × stripHeight × 3`)
  and per-tile-row bytes.
- On a very wide level at dziTile 1024, this naturally drops lookahead
  toward 0–1 (one or two strip buffers in flight) instead of letting C4
  balloon past 1 GB.
- Floor lookahead at 0 (never below a single in-flight strip).

### 1.4. Layer 4 — byte-budget the `pixelCache` (C2) *[smallest; possibly skip]*

C2 is empirically the smallest term (~0.1–0.7 GB). Switching its bound
from count (`max(NumCPU,16)`) to bytes is low-risk and keeps it from
growing on very wide bands, but the win is marginal.

- Track byte total; evict LRU until `≤ C2 share`; floor at
  `≥ (lookahead+1)` distinct in-flight frames (NOT `workers` — on a
  row-major pass many workers share one full-width band, so the true
  concurrency floor is the lookahead window, ~3, not NumCPU; this
  corrects the draft's R1).
- **Open for the plan:** whether C2 is worth bounding at all once C1/C4
  are, or whether C2 and C3 should share one frame budget (they cache the
  same frames decoded vs compressed — §6).

### 1.5. The budget knob *(sealed)*

- **Scope: per-Slide** (Q1). One slide per `convert` invocation in
  practice ⇒ per-Slide ≈ process-wide, avoids a global mutable singleton,
  fits the immutable-after-Open invariant.
- **Default: ~1 GiB live** (Q2), targeting ~2 GB peak under GOGC≈2×.
  Indicative split: ~700 MB C1, ~150 MB C3, ~150 MB C2; C4/lookahead
  throttled against whatever remains.
- **Surface: both** (Q3) — env `OPENTILE_READ_MEMORY_BUDGET` (bytes) and
  `WithMemoryBudget(bytes)` option; precedence option > env > default.
  Additive, non-breaking.

### 1.6. `GOMEMLIMIT` — honour, never set below floor

Measured: `GOMEMLIMIT=3GiB` cut CMU-1 peak 40% (4942→2978 MiB) at **zero
throughput cost** when the live set fits under it; but on OS-2 (live
~6.3 GB) a 3 GiB limit **thrashed** (GC death-spiral, 120/288 strips in
>180 s vs 60 s unconstrained).

- The library **honours** an externally-set `GOMEMLIMIT` as a ceiling
  hint and may shrink its default budget to keep live set comfortably
  under it.
- The library **must not set** `GOMEMLIMIT` itself below its own floor
  (`C1 floor + one strip buffer`), or it risks the death-spiral.
- **Recommend (wsitools docs):** set `GOMEMLIMIT≈2GiB` once caches are
  byte-bounded — that pairing is what actually pins peak RSS.

### 1.7. Bench / gate updates

- New **peak-RSS gate** built on `cmd/bench/ndpi-strips`: drive the DZI
  descent, assert peak HeapInuse ≤ threshold on CMU-1 + OS-2 at dziTile
  256 **and 1024**. This is the regression guard that would have caught
  this class of issue.
- Re-run throughput benches; assert no regression beyond noise (eviction
  must not re-introduce decode churn — §3 R1).

---

## 2. Out of scope — but note the wsitools cascade is load-bearing

- **wsitools' DZI levelBuilder cascade (B).** This is a *co-dominant*
  width-proportional consumer on wide slides (the other half of the
  6.8 GB OS-2 total) and is **entirely outside the library fix**. A
  library-only v0.30 will *not* by itself guarantee the 16 GB Mac stops
  panicking on OS-2/Hamamatsu. The wsitools-side work (bounded cascade /
  column-band streaming, `--read-workers`/`--lookahead` flags, peak-RSS
  guard in `bench-dzi.sh`, `GOMEMLIMIT`) is tracked separately but is
  **not optional** for the end-to-end OOM — flag this to the user when
  scoping wsitools v0.21.
- **The v0.29 Layer-3 refcount work.** Orthogonal; byte-budgeting here
  does not reintroduce that sharing.
- **Non-NDPI formats.** C1/C4 are format-agnostic and fixed for all;
  C2/C3 are NDPI-specific. SVS et al. are already bounded on tile paths.

---

## 3. Risks & invariants

- **R1 — eviction thrash re-introduces decode churn.** If a per-cache
  byte share is below its in-flight working set, concurrent producers
  evict each other → re-decode storm. Mitigation: floor C1 at `≥ workers`
  ready tiles and C2 at `≥ (lookahead+1)` frames (the real concurrency
  floor — many workers share one full-width band, so it is the lookahead
  window, not NumCPU; corrects the draft's `≥ workers × frameBytes`).
- **R2 — `GOMEMLIMIT` death-spiral (empirically demonstrated).** A limit
  below the live working set thrashes catastrophically. The library must
  never set a limit below its floor and should shrink budgets to stay
  under any external limit. (§1.6)
- **R3 — promise-pattern correctness.** Byte-budget eviction in
  `pixelFrameCache.getOrLoad` must preserve the invariant that an evicted
  in-flight entry still notifies waiters (`pixel_cache.go:101-120`): evict
  by the same `evictIfOverLocked` discipline, never free an entry whose
  `ready` chan is open and awaited.
- **R4 — lock order / `framesByKey` slow-path reuse.** No new locks;
  byte counters update under each cache's existing mutex. Bounding C3 must
  not regress the `Tile()`-only adjacent-tile consumer — keep capacity ≥
  one frame-row.

---

## 4. Sealed decisions

1. Fix lives in the library; wsitools cascade tracked separately **but
   flagged load-bearing** for the end-to-end OOM. (§2)
2. Mechanism = byte budget across C1/C3/C2 + byte-bounded lookahead for
   C4, plus honouring `GOMEMLIMIT`. (§1)
3. **Layer priority C1 ≫ C3 > C4 > C2** — corrected from the draft's
   C3/C2-first ordering by the heap profile. (§0.3)
4. **Q1 — per-Slide** budget. (§1.5)
5. **Q2 — ~1 GiB live** default (~2 GB peak target). (§1.5)
6. **Q3 — both** env var + `WithMemoryBudget` option. (§1.5)
7. **Q4 — graceful-degrade** lookahead under pressure. (§1.3)
8. **Q5 — one milestone**, but build + gate the safety layers first:
   **C3 (only unbounded term) then C1 (dominant win)**, both behind the
   peak-RSS gate, so either can ship early if C2/C4 polish slips.
9. Throughput must not regress beyond noise; eviction floored to protect
   in-flight tiles/frames. (§1.7, R1)

**Open for the plan (not blocking):**
- Whether C2 is worth bounding at all, or should share one frame budget
  with C3 (§1.4, §6).
- Exact intra-budget split and how C4/lookahead arbitration reads the
  remaining budget at iterator construction.

---

## 5. Testing & parity gate

- **C1:** unit test the tile cache evicts by bytes and honours the
  `≥ workers` floor under concurrent producers (`-race`); iterator output
  byte-identical to v0.29.
- **C3:** unit test `framesByKey` LRU respects capacity; parity test
  bounded vs unbounded produce byte-identical tiles on a multi-frame level
  (single-pass + a revisiting access pattern, R4).
- **C2:** unit test eviction by bytes; floor honoured under `(lookahead+1)`
  concurrent misses on distinct frames (no livelock, `-race`); decoded
  tiles unchanged vs v0.29.
- **C4/lookahead:** unit test the byte-derived lookahead clamps correctly
  across narrow and very-wide levels at dziTile 256/512/1024.
- **Integration / gate:** peak-RSS gate (§1.7) on CMU-1 + OS-2 at dziTile
  256 **and 1024**; thresholds set from post-fix measurement with headroom.
- **Throughput gate:** `bench-ndpi` single + mt within noise of v0.29
  (251/539 Mpix/s baselines).
- **Regression:** existing v0.27/v0.29 NDPI pixel-parity + DecodedTile
  tests green under `-race -count=1`.

### 5.1. Success criteria

- DZI descent (`cmd/bench/ndpi-strips`, no backpressure) peak HeapInuse on
  CMU-1 **and** OS-2 ≤ ~2 GB **at dziTile 256 and 1024**, independent of
  slide width — verified with the same harness used in this investigation.
- Hamamatsu-1.ndpi (6.4 GB, widest fixture, untested) completes under the
  budget on a 16 GB machine — first new fixture to validate.
- No throughput regression beyond noise.
- C3 footprint no longer scales with level area; C1 no longer scales with
  width past the budget.
- **End-to-end note:** the library gate proves (A) is bounded; the full
  `wsitools convert` OOM on wide slides additionally requires the wsitools
  cascade (B) work + `GOMEMLIMIT` (§2, §1.6).

---

## 6. Open items / follow-ups

- wsitools cascade bounding / column-band streaming + stopgap flags +
  `bench-dzi.sh` peak-RSS guard + `GOMEMLIMIT` doc (separate repo,
  load-bearing — see §2).
- Whether `pixelCache` (C2, decoded) and `framesByKey` (C3, compressed)
  should share one frame budget — they cache the same frames at different
  stages, so double-budgeting may be wasteful.
- Whether C2 is needed at all once C1 is byte-bounded and lookahead is
  byte-derived.
- Whether to keep `cmd/bench/ndpi-strips` as a committed profiling/gate
  tool or fold it into the existing `cmd/bench/ndpi` harness.

## 7. References

- Reproduction + profile: `cmd/bench/ndpi-strips/` (this session
  2026-05-30); peak `inuse_space` profiles in `/tmp/{cmu1,os2}-{256,1024}.prof`.
- Caches: `strip_cache.go` + `strip_iterator.go:84-94,154` (C1/C4),
  `formats/ndpi/stripped.go` (C2/C3), `formats/ndpi/pixel_cache.go`
  (LRU + promise pattern).
- Prior milestones: v0.27 pixel-cache design, v0.29 ReadRegion-perf design
  (`docs/superpowers/specs/`).
- wsitools DZI descent: `../wsitools/cmd/wsitools/convert_dzi_descent.go`.
