# opentile-go v0.30 — read-path memory-budget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound the NDPI `ScaledStrips` read path so DZI conversion peak memory stays flat (~2 GB) regardless of slide width or DZI tile size, eliminating the `wsitools convert --to dzi` OOM.

**Architecture:** A per-`Slide` byte budget (`WithMemoryBudget` option / `OPENTILE_READ_MEMORY_BUDGET` env, default 1 GiB) governs the dominant consumer — the `StripIterator` decoded-tile cache (C1) — by re-expressing its count cap as `byteBudget/bytesPerTile`. The only unbounded term, NDPI `framesByKey` (C3), gets a self-contained byte-bounded LRU. C2 (`pixelCache`, already count-bounded and empirically the smallest term) is left as-is; C4 (transient output strip buffers) is addressed by recommending `GOMEMLIMIT`, which the library honours but never sets below its floor. A new peak-RSS gate (`cmd/bench/ndpi-strips`) guards the whole class of regression.

**Tech Stack:** Go 1.23+, `container/list` (LRU), `runtime`/`runtime/debug` (mem stats + `GOMEMLIMIT`), libjpeg-turbo via existing `internal/jpegturbo`. No new dependencies.

**Priority order (from the heap profile, design doc §0.3):** C1 ≫ C3 > C4 > C2. Build the gate first, then the C3 safety fix, then the budget knob + C1 dominant win, then `GOMEMLIMIT` honouring and docs.

**Source of truth:** `docs/superpowers/specs/2026-05-30-opentile-go-v30-ndpi-memory-budget-design.md` (REVISED post-profile). Sealed decisions: per-Slide budget; ~1 GiB default; env+option surface; graceful lookahead via byte-derived cap; one milestone, C3-then-C1 first.

**Measured baselines (worst case, no backpressure, `cmd/bench/ndpi-strips`):**
- CMU-1 @ dziTile 256: live ~2.4 GB, peak HeapInuse 4942 MiB
- OS-2 @ dziTile 256: live ~6.3 GB, peak HeapInuse 12895 MiB
- OS-2 @ dziTile 1024: live ~8.7 GB, peak HeapInuse 15125 MiB
- `GOMEMLIMIT=3GiB` on CMU-1: peak 2978 MiB at zero throughput cost (live fits); on OS-2: thrash (live exceeds limit).

---

## Task 1: Promote `cmd/bench/ndpi-strips` to a peak-RSS gate

The harness already exists (created during the investigation). Make it gate-ready: a `-maxpeak` threshold that exits non-zero when exceeded, and a `make` target. Thresholds are placeholders here and finalized in Task 6 from post-fix measurement.

**Files:**
- Modify: `cmd/bench/ndpi-strips/main.go`
- Modify: `Makefile`

- [ ] **Step 1: Add a `-maxpeak` flag and non-zero exit on breach**

In `cmd/bench/ndpi-strips/main.go`, add to the flag block (after `peakProf`):

```go
	maxPeak := flag.Int64("maxpeak", 0, "fail (exit 1) if peak HeapInuse MiB exceeds this (0 = report only)")
```

Replace the final reporting block (the `fmt.Printf("PEAK ...")` and the `peakProf` print) with:

```go
	peakMiB := int64(peakHeapInuse >> 20)
	fmt.Printf("PEAK HeapInuse=%d MiB  Sys=%d MiB\n", peakMiB, peakSys>>20)
	if *peakProf != "" {
		fmt.Printf("peak inuse_space profile -> %s\n", *peakProf)
	}
	if *maxPeak > 0 && peakMiB > *maxPeak {
		fmt.Fprintf(os.Stderr, "FAIL: peak %d MiB > maxpeak %d MiB\n", peakMiB, *maxPeak)
		os.Exit(1)
	}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/`
Expected: builds clean (libturbojpeg linker warnings are benign).

- [ ] **Step 3: Verify the gate fails when over budget and passes when under**

Run: `/tmp/ndpi-strips -in sample_files/ndpi/CMU-1.ndpi -dzitile 256 -maxpeak 100; echo "exit=$?"`
Expected: prints `FAIL: peak ... > maxpeak 100` and `exit=1` (current code is unbounded, so peak ≫ 100).

Run: `/tmp/ndpi-strips -in sample_files/ndpi/CMU-1.ndpi -dzitile 256 -maxpeak 100000; echo "exit=$?"`
Expected: `exit=0` (peak well under 100 GB).

- [ ] **Step 4: Add a `bench-ndpi-mem` make target**

Add to `Makefile` (near the existing `bench-ndpi` target). The target runs **with `GOMEMLIMIT` set** to match the recommended deployment, sweeping both DZI tile sizes. `OPENTILE_TESTDIR` defaults to `$(PWD)/sample_files` per the existing convention — mirror however `bench-ndpi` resolves fixtures.

```makefile
# Peak-RSS gate for the NDPI ScaledStrips (DZI) path. Runs the
# no-backpressure worst case under GOMEMLIMIT=2GiB (the recommended
# deployment config). Thresholds are intentionally HIGHER than real
# wsitools RSS because this harness drops strips (no consumer
# backpressure) — it bounds the library's worst case, not the app's.
SLIDES ?= $(PWD)/sample_files
bench-ndpi-mem: ## NDPI ScaledStrips peak-RSS gate (DZI path)
	go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in $(SLIDES)/ndpi/CMU-1.ndpi -dzitile 256  -maxpeak $(MAXPEAK_CMU)
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in $(SLIDES)/ndpi/OS-2.ndpi  -dzitile 256  -maxpeak $(MAXPEAK_OS2)
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in $(SLIDES)/ndpi/OS-2.ndpi  -dzitile 1024 -maxpeak $(MAXPEAK_OS2)
# Placeholders — finalized in Task 6 from post-fix measurement.
MAXPEAK_CMU ?= 99999
MAXPEAK_OS2 ?= 99999
```

- [ ] **Step 5: Run the target to confirm wiring (placeholder thresholds → pass)**

Run: `make bench-ndpi-mem`
Expected: three runs print their PEAK lines and all exit 0 (placeholders are huge). Note the printed peaks — they are the pre-fix baselines.

- [ ] **Step 6: Commit**

```bash
git add cmd/bench/ndpi-strips/main.go Makefile
git commit -m "test(bench): promote ndpi-strips to a peak-RSS gate with make target"
```

---

## Task 2: Bound NDPI `framesByKey` (C3) with a byte-bounded LRU

C3 is the only **unbounded** term (`map[frameKey][]byte`, never evicted — `formats/ndpi/stripped.go:78,514`). On the single-pass DZI traversal it gives ~zero benefit (each frame is read once on the `pixelCache` miss). Replace the raw map with a small byte-bounded LRU. Self-contained in `formats/ndpi`; no per-Slide budget needed (fixed 128 MiB default keeps OS-2's ~0.6 GB C3 in check while preserving slow-`Tile()` adjacent reuse).

**Files:**
- Create: `formats/ndpi/frame_cache.go`
- Create: `formats/ndpi/frame_cache_test.go`
- Modify: `formats/ndpi/stripped.go` (field + `getFrame` + constructor)

- [ ] **Step 1: Write the failing test for the byte-bounded LRU**

Create `formats/ndpi/frame_cache_test.go`:

```go
package ndpi

import "testing"

func TestFrameByteLRUEvictsByBytes(t *testing.T) {
	// Budget 250 bytes; each entry 100 bytes → at most 2 resident.
	c := newFrameByteLRU(250)
	k := func(i int) frameKey { return frameKey{posX: i} }
	c.put(k(1), make([]byte, 100))
	c.put(k(2), make([]byte, 100))
	if got := c.bytes(); got != 200 {
		t.Fatalf("bytes after 2 puts = %d, want 200", got)
	}
	// Third put must evict the LRU (k1) to stay <= 250.
	c.put(k(3), make([]byte, 100))
	if got := c.bytes(); got != 200 {
		t.Fatalf("bytes after 3 puts = %d, want 200 (one evicted)", got)
	}
	if _, ok := c.get(k(1)); ok {
		t.Fatalf("k1 should have been evicted as LRU")
	}
	if _, ok := c.get(k(3)); !ok {
		t.Fatalf("k3 should be present")
	}
}

func TestFrameByteLRUGetIsMRU(t *testing.T) {
	c := newFrameByteLRU(250)
	k := func(i int) frameKey { return frameKey{posX: i} }
	c.put(k(1), make([]byte, 100))
	c.put(k(2), make([]byte, 100))
	_, _ = c.get(k(1)) // touch k1 → MRU
	c.put(k(3), make([]byte, 100)) // evicts LRU, which is now k2
	if _, ok := c.get(k(2)); ok {
		t.Fatalf("k2 should have been evicted (k1 was touched)")
	}
	if _, ok := c.get(k(1)); !ok {
		t.Fatalf("k1 should survive (was MRU)")
	}
}

func TestFrameByteLRUOversizeEntryStillStored(t *testing.T) {
	// A single entry larger than the budget is stored alone (we never
	// drop the just-inserted entry; correctness > strict bound).
	c := newFrameByteLRU(50)
	c.put(frameKey{posX: 1}, make([]byte, 100))
	if _, ok := c.get(frameKey{posX: 1}); !ok {
		t.Fatalf("oversize entry must still be retrievable")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./formats/ndpi/ -run TestFrameByteLRU -count=1`
Expected: FAIL — `undefined: newFrameByteLRU`.

- [ ] **Step 3: Implement the byte-bounded LRU**

Create `formats/ndpi/frame_cache.go`:

```go
package ndpi

import (
	"container/list"
	"sync"
)

// frameByteLRU is a byte-bounded LRU of assembled JPEG frames keyed by
// frameKey. It replaces the v0.2 unbounded framesByKey map. On the
// single-pass ScaledStrips traversal each frame is read once (via the
// pixelCache miss), so this provides retention only for the slow
// Tile() random-access path; a modest byte budget suffices.
//
// Eviction: LRU by recency until total bytes <= maxBytes. A
// just-inserted entry is never evicted, so a single entry larger than
// maxBytes is stored alone (correctness over a strict bound).
//
// All methods are safe for concurrent use.
type frameByteLRU struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	entries  map[frameKey]*frameLRUEntry
	order    *list.List // values are frameKey; front = MRU
}

type frameLRUEntry struct {
	data []byte
	elem *list.Element
}

func newFrameByteLRU(maxBytes int64) *frameByteLRU {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &frameByteLRU{
		maxBytes: maxBytes,
		entries:  make(map[frameKey]*frameLRUEntry),
		order:    list.New(),
	}
}

// get returns the cached frame and moves it to MRU. ok=false on miss.
func (c *frameByteLRU) get(k frameKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e.elem)
	return e.data, true
}

// put inserts (or replaces) k=data and evicts LRU entries until the
// total is within budget (never evicting the entry just inserted).
func (c *frameByteLRU) put(k frameKey, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		c.curBytes += int64(len(data)) - int64(len(e.data))
		e.data = data
		c.order.MoveToFront(e.elem)
	} else {
		e = &frameLRUEntry{data: data}
		e.elem = c.order.PushFront(k)
		c.entries[k] = e
		c.curBytes += int64(len(data))
	}
	for c.curBytes > c.maxBytes {
		back := c.order.Back()
		if back == nil {
			break
		}
		bk := back.Value.(frameKey)
		if bk == k {
			break // never evict the just-inserted entry
		}
		be := c.entries[bk]
		c.order.Remove(back)
		delete(c.entries, bk)
		c.curBytes -= int64(len(be.data))
	}
}

// bytes returns the current resident byte total (test helper).
func (c *frameByteLRU) bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.curBytes
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./formats/ndpi/ -run TestFrameByteLRU -race -count=1`
Expected: PASS (all three).

- [ ] **Step 5: Wire `getFrame` to use the LRU**

In `formats/ndpi/stripped.go`, replace the `framesByKey` field (lines ~77-78):

```go
	frameMu     sync.Mutex
	framesByKey map[frameKey][]byte
```

with:

```go
	frames *frameByteLRU
```

In `newStrippedImage` (the returned struct literal, ~line 133), replace:

```go
		framesByKey:        make(map[frameKey][]byte),
```

with (128 MiB default keeps OS-2's full-width-band frames bounded while preserving slow-path reuse):

```go
		frames:             newFrameByteLRU(128 << 20),
```

Replace the body of `getFrame` (the `l.frameMu.Lock()` / double-checked map logic, ~lines 514-536) with:

```go
func (l *strippedImage) getFrame(framePos, frameSize opentile.Size) ([]byte, error) {
	key := frameKey{posX: framePos.W, posY: framePos.H, w: frameSize.W, h: frameSize.H}
	if b, ok := l.frames.get(key); ok {
		return b, nil
	}
	frame, err := l.assembleFrame(framePos, frameSize)
	if err != nil {
		return nil, err
	}
	l.frames.put(key, frame)
	return frame, nil
}
```

Remove the now-unused `frameMu sync.Mutex` field if it was separate (it's folded into the LRU's own mutex).

- [ ] **Step 6: Verify the NDPI package builds and existing tests pass**

Run: `go build ./formats/ndpi/ && go test ./formats/ndpi/ -race -count=1`
Expected: builds clean; all existing NDPI tests pass (frame retention change is transparent — `getFrame` still returns byte-identical assembled frames). Watch specifically for `TestNDPIFastPathPixelParity`, `TestNDPIFastPathConcurrent`, `TestNDPIDecodedTilePathParity` — all must stay green.

- [ ] **Step 7: Confirm C3 no longer grows unbounded (profile spot-check)**

Run: `go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/ && /tmp/ndpi-strips -in sample_files/ndpi/OS-2.ndpi -dzitile 256 -peakprof /tmp/os2-c3.prof`
Then: `go tool pprof -inuse_space -top -nodecount=6 /tmp/os2-c3.prof 2>/dev/null | grep assembleFrame`
Expected: `assembleFrame` flat now ≤ ~150 MiB (was 519 MiB) — bounded by the 128 MiB LRU plus in-flight frames. (C1 still dominates until Task 4.)

- [ ] **Step 8: Commit**

```bash
git add formats/ndpi/frame_cache.go formats/ndpi/frame_cache_test.go formats/ndpi/stripped.go
git commit -m "fix(ndpi): bound framesByKey with a 128MiB byte LRU (was unbounded)"
```

---

## Task 3: Add the per-Slide memory budget config (option + env + default)

Introduce the budget knob. It governs C1 (Task 4). Per-Slide scope (Q1): stored on `Slide` and read by `newStripIterator`. Default 1 GiB; env `OPENTILE_READ_MEMORY_BUDGET` (bytes); option `WithMemoryBudget`. Precedence: option > env > default.

**Files:**
- Modify: `options.go` (config field + `WithMemoryBudget` + env resolution)
- Modify: `slide.go` (Slide field)
- Modify: `opentile.go` (set `Slide.readBudget` at all three construction sites)
- Test: `options_budget_test.go` (create)

- [ ] **Step 1: Write the failing test for budget resolution**

Create `options_budget_test.go`:

```go
package opentile

import "testing"

func TestMemoryBudgetDefault(t *testing.T) {
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != defaultReadMemoryBudget {
		t.Fatalf("default budget = %d, want %d", got, defaultReadMemoryBudget)
	}
}

func TestMemoryBudgetOptionOverridesEnv(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "500000000")
	c := newConfig([]Option{WithMemoryBudget(700_000_000)})
	if got := c.resolveMemoryBudget(); got != 700_000_000 {
		t.Fatalf("option should win over env: got %d, want 700000000", got)
	}
}

func TestMemoryBudgetEnvUsedWhenNoOption(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "500000000")
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != 500_000_000 {
		t.Fatalf("env budget = %d, want 500000000", got)
	}
}

func TestMemoryBudgetIgnoresGarbageEnv(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "not-a-number")
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != defaultReadMemoryBudget {
		t.Fatalf("garbage env should fall back to default: got %d", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestMemoryBudget -count=1`
Expected: FAIL — `undefined: defaultReadMemoryBudget`, `WithMemoryBudget`, `resolveMemoryBudget`.

- [ ] **Step 3: Implement config field, option, and env resolution**

In `options.go`, add the import (top of file):

```go
import (
	"os"
	"strconv"
)
```

Add the constant and config fields. After the `Backing` consts, add:

```go
// defaultReadMemoryBudget is the default per-Slide live-memory target
// for the ScaledStrips read path (the decoded-tile cache, C1). ~2 GB
// peak under GOGC=100. Override with WithMemoryBudget or the
// OPENTILE_READ_MEMORY_BUDGET env var (bytes).
const defaultReadMemoryBudget int64 = 1 << 30 // 1 GiB
```

Extend `config` (add fields):

```go
	memoryBudget    int64
	hasMemoryBudget bool
```

Add the option and resolver:

```go
// WithMemoryBudget sets the per-Slide live-memory budget (bytes) for
// the ScaledStrips read path. Governs the decoded-tile cache so peak
// memory stays flat regardless of slide width / DZI tile size. Default
// 1 GiB; also settable via OPENTILE_READ_MEMORY_BUDGET (option wins).
// Values < 1 are ignored (default used).
func WithMemoryBudget(bytes int64) Option {
	return func(c *config) {
		if bytes >= 1 {
			c.memoryBudget = bytes
			c.hasMemoryBudget = true
		}
	}
}

// resolveMemoryBudget returns the effective budget: option > env >
// default. A non-positive or unparseable env value is ignored.
func (c *config) resolveMemoryBudget() int64 {
	if c.hasMemoryBudget {
		return c.memoryBudget
	}
	if v := os.Getenv("OPENTILE_READ_MEMORY_BUDGET"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			return n
		}
	}
	return defaultReadMemoryBudget
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test . -run TestMemoryBudget -count=1`
Expected: PASS (all four).

- [ ] **Step 5: Add the `readBudget` field to Slide and populate it at all open sites**

In `slide.go`, add to the `Slide` struct (after the `handles` field):

```go
	// v0.30: per-Slide read-path memory budget (bytes), resolved at
	// Open from WithMemoryBudget / OPENTILE_READ_MEMORY_BUDGET /
	// default. Read by newStripIterator to size the C1 tile cache.
	readBudget int64
```

In `opentile.go`, set it at the three `&Slide{...}` construction sites:

`Open` (~line 71):
```go
	return &Slide{r: rdr, readBudget: cfg.resolveMemoryBudget()}, nil
```

`openFilePread` (~line 131):
```go
	fc := &fileCloser{slideReader: rdr, f: f}
	return &Slide{r: fc, readBudget: cfg.resolveMemoryBudget()}, nil
```

`openFileMmap` (~line 150):
```go
	mc := &mmapCloser{slideReader: rdr, m: m}
	return &Slide{r: mc, readBudget: cfg.resolveMemoryBudget()}, nil
```

- [ ] **Step 6: Build and run the full package test to confirm no breakage**

Run: `go build ./... && go test . -race -count=1`
Expected: builds clean; package `opentile` tests pass. `readBudget` is set but not yet consumed (Task 4).

- [ ] **Step 7: Commit**

```bash
git add options.go slide.go opentile.go options_budget_test.go
git commit -m "feat: add per-Slide WithMemoryBudget option + OPENTILE_READ_MEMORY_BUDGET env"
```

---

## Task 4: Byte-derive the StripIterator tile-cache cap (C1 — dominant win)

Re-express the C1 cache capacity (`strip_iterator.go:90`) from a tile *count* (`workers×(lookahead+1)×tilesPerStripWidth`) to a byte-derived count (`budget/bytesPerTile`), clamped to a `[max(workers,8), count-formula]` range. This bounds the ~2–6 GB dominant term. The byte-derived cap also implicitly throttles prefetch depth (the lookahead goroutine blocks on cache backpressure once the cap is hit — graceful degradation, Q4).

**Files:**
- Modify: `strip_iterator.go` (the cache-capacity block, ~lines 84-94; add a helper)
- Test: `strip_iterator_budget_test.go` (create)

- [ ] **Step 1: Write the failing test for the cap helper**

Create `strip_iterator_budget_test.go`:

```go
package opentile

import "testing"

func TestStripCacheCapacityByteDerived(t *testing.T) {
	// bytesPerTile for a 512x512 RGB source tile = 786432.
	const bpt = 512 * 512 * 3
	// Budget 100 MiB → 100*1<<20 / 786432 ≈ 133 tiles.
	got := stripCacheCapacity(100<<20, bpt, /*workers*/ 10, /*countFormulaCap*/ 7440)
	if got < 130 || got > 140 {
		t.Fatalf("byte-derived cap = %d, want ~133", got)
	}
}

func TestStripCacheCapacityFlooredAtWorkers(t *testing.T) {
	const bpt = 512 * 512 * 3
	// Tiny budget → would be < workers; must floor at max(workers,8).
	got := stripCacheCapacity(1<<20, bpt, /*workers*/ 12, /*countFormulaCap*/ 7440)
	if got != 12 {
		t.Fatalf("cap = %d, want floor 12 (workers)", got)
	}
}

func TestStripCacheCapacityFlooredAtEight(t *testing.T) {
	const bpt = 512 * 512 * 3
	got := stripCacheCapacity(1<<20, bpt, /*workers*/ 2, /*countFormulaCap*/ 7440)
	if got != 8 {
		t.Fatalf("cap = %d, want floor 8", got)
	}
}

func TestStripCacheCapacityNeverExceedsCountFormula(t *testing.T) {
	const bpt = 512 * 512 * 3
	// Huge budget must not exceed the original count formula cap.
	got := stripCacheCapacity(64<<30, bpt, /*workers*/ 10, /*countFormulaCap*/ 300)
	if got != 300 {
		t.Fatalf("cap = %d, want countFormulaCap 300", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestStripCacheCapacity -count=1`
Expected: FAIL — `undefined: stripCacheCapacity`.

- [ ] **Step 3: Implement the helper**

Add to `strip_iterator.go` (near the bottom, with the other helpers):

```go
// stripCacheCapacity converts a byte budget into a tile-count cap for
// the per-iterator decoded-tile cache (C1). The result is floored at
// max(workers, 8) so each worker always has an in-flight slot and tiny
// budgets don't livelock, and capped at the original count-formula
// value so a generous budget never over-provisions a narrow level.
func stripCacheCapacity(budgetBytes, bytesPerTile int64, workers, countFormulaCap int) int {
	if bytesPerTile < 1 {
		bytesPerTile = 1
	}
	byteCap := int(budgetBytes / bytesPerTile)
	floor := workers
	if floor < 8 {
		floor = 8
	}
	cap := byteCap
	if cap < floor {
		cap = floor
	}
	if cap > countFormulaCap {
		cap = countFormulaCap
	}
	return cap
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test . -run TestStripCacheCapacity -count=1`
Expected: PASS (all four).

- [ ] **Step 5: Wire the helper into `newStripIterator`**

In `strip_iterator.go`, replace the cache-capacity block (currently ~lines 84-94):

```go
	// Cache size heuristic: workers * (lookahead + 1) * tilesPerStripWidth
	// (workers concurrent in-flight tiles per strip × strips in window).
	tilesPerStripWidth := 1
	if level.TileSize.W > 0 {
		tilesPerStripWidth = (level.Size.W + level.TileSize.W - 1) / level.TileSize.W
	}
	cacheCapacity := cfg.workers * (cfg.lookahead + 1) * tilesPerStripWidth
	if cacheCapacity < 8 {
		cacheCapacity = 8
	}
	it.cache = newTileCache(cacheCapacity)
```

with:

```go
	// Cache size: byte-budget-derived, floored at max(workers,8) and
	// capped at the original count formula. The count formula
	// (workers × (lookahead+1) × tilesPerStripWidth) is the upper
	// bound; the byte budget shrinks it on wide levels so the C1
	// decoded-tile cache cannot balloon with slide width (v0.30).
	tilesPerStripWidth := 1
	if level.TileSize.W > 0 {
		tilesPerStripWidth = (level.Size.W + level.TileSize.W - 1) / level.TileSize.W
	}
	countFormulaCap := cfg.workers * (cfg.lookahead + 1) * tilesPerStripWidth
	if countFormulaCap < 8 {
		countFormulaCap = 8
	}
	// Per-tile bytes at the decoded (post-IDCT) resolution the workers
	// actually cache; 3 bytes/px RGB. idctScale shrinks both dims.
	scale := it.idctScale
	if scale < 1 {
		scale = 1
	}
	bytesPerTile := int64((level.TileSize.W/scale)*(level.TileSize.H/scale)) * 3
	if bytesPerTile < 1 {
		bytesPerTile = 1
	}
	budget := s.readBudget
	if budget < 1 {
		budget = defaultReadMemoryBudget
	}
	cacheCapacity := stripCacheCapacity(budget, bytesPerTile, cfg.workers, countFormulaCap)
	it.cache = newTileCache(cacheCapacity)
```

- [ ] **Step 6: Run the full opentile test suite under race**

Run: `go test . -race -count=1`
Expected: PASS. The smaller cache must not change iterator *output* — only retention. Watch the existing ScaledStrips tests (strip iterator parity / ordering).

- [ ] **Step 7: Measure the dominant-win effect (no GOMEMLIMIT, to isolate live-set bound)**

Run: `go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/ && /tmp/ndpi-strips -in sample_files/ndpi/OS-2.ndpi -dzitile 256 -peakprof /tmp/os2-c1.prof`
Expected: peak HeapInuse drops sharply from ~12895 MiB toward ~3–4 GB (live C1 now ≈ budget ≈ 1 GiB; peak ≈ 2× under GOGC). Confirm with: `go tool pprof -inuse_space -top -nodecount=4 /tmp/os2-c1.prof 2>/dev/null | grep -i NewImageFormat` → the `decodeAndStore` cum should now be ≈ budget, not ~6 GB.

- [ ] **Step 8: Measure with GOMEMLIMIT (the recommended config → hits the ~2 GB target)**

Run: `GOMEMLIMIT=2GiB /tmp/ndpi-strips -in sample_files/ndpi/OS-2.ndpi -dzitile 1024`
Expected: peak HeapInuse ≤ ~2200 MiB **and the run completes** (no thrash — live set now fits under the limit, unlike the pre-fix OS-2 thrash). Record the throughput; it should stay within noise of the ~91–157 Mpix/s baseline.

- [ ] **Step 9: Commit**

```bash
git add strip_iterator.go strip_iterator_budget_test.go
git commit -m "feat: byte-derive StripIterator tile-cache cap from per-Slide budget (C1)"
```

---

## Task 5: Honour `GOMEMLIMIT` when deriving the default budget

When the caller has set `GOMEMLIMIT` but did **not** set an explicit budget, shrink the default budget to stay comfortably under the limit (so the library's live set + GC headroom doesn't fight the runtime limit). The library never *sets* `GOMEMLIMIT` and never derives a budget below its own floor.

**Files:**
- Modify: `options.go` (`resolveMemoryBudget`)
- Test: `options_budget_test.go` (add cases)

- [ ] **Step 1: Write the failing test**

Add to `options_budget_test.go`:

```go
import "runtime/debug"

func TestMemoryBudgetShrinksUnderGOMEMLIMIT(t *testing.T) {
	// Set a 1 GiB runtime limit; restore after.
	prev := debug.SetMemoryLimit(1 << 30)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	c := newConfig(nil) // no explicit budget
	got := c.resolveMemoryBudget()
	// Should be <= half the limit (leave headroom for GC + C2/C3 + app),
	// and never below the 128 MiB floor.
	if got > (1<<30)/2 {
		t.Fatalf("budget %d should be <= half of GOMEMLIMIT", got)
	}
	if got < 128<<20 {
		t.Fatalf("budget %d below floor", got)
	}
}

func TestExplicitBudgetIgnoresGOMEMLIMIT(t *testing.T) {
	prev := debug.SetMemoryLimit(1 << 30)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	c := newConfig([]Option{WithMemoryBudget(900_000_000)})
	if got := c.resolveMemoryBudget(); got != 900_000_000 {
		t.Fatalf("explicit budget must be honoured verbatim: got %d", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run "GOMEMLIMIT" -count=1`
Expected: FAIL — default 1 GiB budget is not shrunk under the 1 GiB runtime limit.

- [ ] **Step 3: Implement GOMEMLIMIT-aware default**

In `options.go`, add the import `"runtime/debug"` and a floor constant:

```go
// minReadMemoryBudget is the floor below which budget derivation never
// goes (one worker's worth of in-flight tiles plus a strip buffer).
const minReadMemoryBudget int64 = 128 << 20 // 128 MiB
```

Replace the final `return defaultReadMemoryBudget` in `resolveMemoryBudget` with:

```go
	// No explicit option/env budget: start from the default, but if the
	// process has a GOMEMLIMIT set, shrink to <= half of it so our live
	// set + GC headroom + C2/C3 + the caller's own buffers fit under the
	// runtime ceiling. SetMemoryLimit(-1) reads the current limit
	// without changing it; math.MaxInt64 means "unset".
	budget := defaultReadMemoryBudget
	if limit := debug.SetMemoryLimit(-1); limit != math.MaxInt64 {
		if half := limit / 2; half < budget {
			budget = half
		}
	}
	if budget < minReadMemoryBudget {
		budget = minReadMemoryBudget
	}
	return budget
```

Add `"math"` to the imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test . -run "MemoryBudget|GOMEMLIMIT" -count=1`
Expected: PASS (all budget tests, including the new two).

- [ ] **Step 5: Full suite + commit**

Run: `go test . -race -count=1`
Expected: PASS.

```bash
git add options.go options_budget_test.go
git commit -m "feat: shrink default read budget under GOMEMLIMIT (honour, never set)"
```

---

## Task 6: Finalize gate thresholds, run full gates, update docs

Lock the peak-RSS gate thresholds from post-fix measurement, run every gate, and write the milestone notes.

**Files:**
- Modify: `Makefile` (`MAXPEAK_*` values)
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (new "Current milestone — v0.30" section)

- [ ] **Step 1: Measure post-fix peaks under the gate's GOMEMLIMIT config**

Run:
```bash
go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/
GOMEMLIMIT=2GiB /tmp/ndpi-strips -in sample_files/ndpi/CMU-1.ndpi -dzitile 256
GOMEMLIMIT=2GiB /tmp/ndpi-strips -in sample_files/ndpi/OS-2.ndpi  -dzitile 256
GOMEMLIMIT=2GiB /tmp/ndpi-strips -in sample_files/ndpi/OS-2.ndpi  -dzitile 1024
```
Record each PEAK MiB. Expected: all ≤ ~2200 MiB.

- [ ] **Step 2: Set thresholds with ~15% headroom**

In `Makefile`, set `MAXPEAK_CMU` and `MAXPEAK_OS2` to the measured peaks × 1.15 (rounded up). Replace the placeholder `?= 99999` lines with the computed values (keep them as `?=` so callers can override).

- [ ] **Step 3: Run the memory gate to confirm it passes at the new thresholds**

Run: `make bench-ndpi-mem`
Expected: all three runs exit 0, peaks under thresholds.

- [ ] **Step 4: Validate the widest untested fixture (Hamamatsu-1, the suspected hard-OOM trigger)**

Run: `GOMEMLIMIT=2GiB /tmp/ndpi-strips -in sample_files/ndpi/Hamamatsu-1.ndpi -dzitile 1024 -maxstrips 200`
Expected: completes without OOM/thrash; peak ≤ ~2.2 GB. (Use `-maxstrips` to bound wall-clock on the 6.4 GB file; the L0 transient is what we're validating.) If peak exceeds the OS-2 threshold, note it in the CHANGELOG as a fixture-specific follow-up rather than blocking.

- [ ] **Step 5: Run the throughput gate and full test suite (no regression)**

Run: `make bench-ndpi && make test`
Expected: `bench-ndpi` within noise of the v0.29 baseline (≥270 Mpix/s gate per current Makefile); `make test` green under `-race`. Specifically confirm the v0.27/v0.29 NDPI parity tests pass.

- [ ] **Step 6: Write the CHANGELOG entry**

Add a `## v0.30.0` section to `CHANGELOG.md` covering: per-Slide `WithMemoryBudget` + `OPENTILE_READ_MEMORY_BUDGET` (default 1 GiB); byte-derived C1 cache cap; bounded C3 `framesByKey` (was unbounded); `GOMEMLIMIT`-aware default; new `bench-ndpi-mem` gate. State the measured peak reductions (CMU-1 / OS-2 before→after) and that C2 byte-budgeting + the wsitools cascade are deferred (design doc §2, §6). Note `RawTile`/`DecodedTile`/`ScaledStrips` outputs are byte-identical.

- [ ] **Step 7: Write the CLAUDE.md milestone section**

Add a "## Current milestone — v0.30" block at the top (matching the v0.29 block's structure): scope, API additions (`WithMemoryBudget`, `OPENTILE_READ_MEMORY_BUDGET`, `bench-ndpi-mem`), API breaks (none), the corrected root cause (C1 dominant, not C2), active limitations (C2 deferred; wsitools cascade load-bearing and outside this fix), correctness bar (gates + measured peaks), and design/plan doc paths. Move v0.29 to "Previous milestone."

- [ ] **Step 8: Commit**

```bash
git add Makefile CHANGELOG.md CLAUDE.md
git commit -m "docs(v0.30): finalize peak-RSS gate thresholds, CHANGELOG + CLAUDE.md milestone"
```

---

## Self-review notes (spec coverage)

- **Layer 1 (C1)** → Task 4 (byte-derived cap; the dominant win). ✓
- **Layer 2 (C3)** → Task 2 (byte-bounded LRU; the only unbounded term, built first per Q5 safety-first sequencing). ✓
- **Layer 3 (C4 / graceful lookahead)** → addressed implicitly by Task 4 (byte cap throttles prefetch) + `GOMEMLIMIT` (Task 5) clamping transient strip-buffer headroom. Documented as such; no separate explicit lookahead-math task because the byte-derived cap already degrades prefetch depth. ✓
- **Layer 4 (C2)** → deliberately **deferred** (already count-bounded; empirically the smallest term ~0.1–0.7 GB). Recorded in design doc §1.4/§4-open and CHANGELOG (Task 6). Threading the per-Slide budget into `formats/ndpi` would require extending the `openAnyHook` import-cycle bridge for marginal benefit — explicitly out of scope for v0.30.
- **Budget knob (§1.5)** → Task 3 (option + env + default, per-Slide). ✓
- **GOMEMLIMIT (§1.6)** → Task 5 (honour, never set below floor). ✓
- **Peak-RSS gate (§1.7)** → Task 1 (harness→gate) + Task 6 (thresholds). ✓
- **Throughput no-regression (§5)** → Task 6 Step 5. ✓
- **Parity (§5)** → Tasks 2/4 re-run the v0.27/v0.29 NDPI parity tests; outputs byte-identical. ✓
- **Success criteria (§5.1)** → Task 4 Steps 7-8 + Task 6 Steps 1-4 (CMU-1 + OS-2 ≤ ~2 GB at 256 and 1024; Hamamatsu validation). ✓

**Deviation flagged for plan review:** the design doc §1.5 framed "one budget divided across the three consumers." This plan refines that to: the budget governs **C1 only**; C3 has a fixed self-contained byte cap; C2 is unchanged; C4 is handled by `GOMEMLIMIT`. Rationale: the profile shows C1 is ~90% of the live set, and threading the budget into `formats/ndpi` (C2/C3) costs an `openAnyHook` signature change for marginal effect. This keeps v0.30 surgical while still hitting the ~2 GB target. If you want the single unified pool, it's an additive follow-up.
