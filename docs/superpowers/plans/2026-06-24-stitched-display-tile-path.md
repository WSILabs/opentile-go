# Stitched Display-Tile Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a clean, non-overlapping `(*Level).StitchedTile` accessor that composites overlapping-tile formats (BIF today) into display tiles, generically over the existing `regionLayout` capability, backed by a per-`Slide` decoded-frame cache — so viewers treat BIF like any other format with no overlap math and no throughput cost.

**Architecture:** The compositing loop already lives in `imageReadRegionImpl` (`region.go`) and BIF already implements the generic `regionLayout` capability. We (1) extract the per-tile composite loop into a shared `compositeStitchedLoop` so the region path and the new stitched path cannot drift; (2) add a byte-bounded promise-pattern decoded-frame cache (`decodedFrameCache`, modeled on NDPI's `pixelFrameCache`) so source frames shared by adjacent display tiles decode once; (3) add `(*Slide).imageStitchedTile` that composites a `TileSize`-aligned rectangle of the stitched image using that cache, plus thin `(*Level).StitchedTile` / `StitchedGrid` receiver methods. Purely additive — `Tile`/`DecodedTile`/`Grid`/`Overlapping` and the 10 non-BIF formats are untouched and bit-identical.

**Tech Stack:** Go 1.23+, the root `opentile` package, `decoder.Image` pixel buffers, `container/list` LRU, `sync` promise pattern. No new cgo. The new cache + composite + `StitchedTile` are pure Go; decode of source frames uses the existing decoder pool (cgo for JPEG-backed BIF, exactly like `DecodedTile`).

**Reference (read before starting):**
- `region.go:9-147` — the `regionLayout` interface, `regionLayoutOf` discovery, and the existing composite loop being extracted.
- `decoded_tile.go:11-87` — `decodedTiler` dispatch and `imageDecodedTile` (the fetch the cache wraps).
- `formats/ndpi/pixel_cache.go` — the promise-pattern LRU this cache is modeled on.
- `level_reads.go` — the established home for `(*Level)` receiver-method reads.
- `slide.go:52-109,196-212` — `Slide` struct, `ensurePyramids` back-ref, `Close`.
- `region_layout_test.go` — the `fakeLayoutReader` pattern the test fake builds on.
- Design spec: `docs/superpowers/specs/2026-06-23-stitched-display-tile-path-design.md`.

**Helpers that already exist (do not re-create):** `ceilDiv(a,b int) int` (`validate.go:322`), `maxInt`/`minInt` (`region.go:241,248`), `blitInto` (`blit.go:12`), `fillWhite` (`region.go:222`), `borrowTileScratch`/`returnTileScratch` (`decoded_tile_scratch.go:36,50`), `newDecodeConfig` + `cfg.format`/`cfg.scale` (`decoded_tile.go`), `decoder.NewImageFormat`, `decoder.ErrUnsupportedScale`.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `frame_cache.go` | `decodedFrameCache`: byte-bounded promise LRU of decoded raw tiles, keyed by `(image,level,col,row,format)`. | Create |
| `frame_cache_test.go` | Fixture-free cache unit tests. | Create |
| `stitched_tile.go` | `compositeStitchedLoop` (shared), `(*Slide).imageStitchedTile`, `(*Slide).frameCacheFor`, `minFrameCacheBytes`. | Create |
| `stitched_tile_test.go` | Fake-reader behavior tests (compose, decode-once, scale, OOB, delegation). | Create |
| `region.go` | Replace the inline composite loop with a `compositeStitchedLoop` call (DRY; parity-guarded). | Modify `:125-146` |
| `slide.go` | Add `frameCache *decodedFrameCache` field; clear it in `Close`. | Modify `:52-78`, `:196-212` |
| `level_reads.go` | Add `(*Level).StitchedTile` and `(*Level).StitchedGrid`. | Modify (append) |
| `bif_stitched_tile_test.go` | Fixture-gated BIF integration test: `StitchedTile == ReadRegion`. | Create |
| `docs/formats/bif.md` | Document `StitchedTile` as the viewer path. | Modify |
| `CHANGELOG.md` | `[Unreleased]` Added entry. | Modify |

---

## Task 1: The decoded-frame cache

**Files:**
- Create: `frame_cache.go`
- Test: `frame_cache_test.go`

A byte-bounded promise-pattern LRU. Mirrors `formats/ndpi/pixel_cache.go` but (a) byte-bounded by `readBudget` rather than count-bounded, (b) keyed by raw tile coordinates, and (c) never evicts an in-flight entry (the v0.47.1 eviction-race lesson).

- [ ] **Step 1: Write the failing test**

Create `frame_cache_test.go`:

```go
package opentile

import (
	"errors"
	"sync"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func frame(w, h int, fill byte) *decoder.Image {
	img := decoder.NewImageFormat(w, h, decoder.PixelFormatRGB)
	for i := range img.Pix {
		img.Pix[i] = fill
	}
	return img
}

func TestDecodedFrameCacheLoadsOnceThenHits(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{image: 0, level: 0, col: 1, row: 2, format: decoder.PixelFormatRGB}
	var loads int
	load := func() (*decoder.Image, error) { loads++; return frame(4, 4, 9), nil }

	a, err := c.getOrLoad(key, load)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.getOrLoad(key, load)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loads = %d, want 1 (second call must hit)", loads)
	}
	if a != b {
		t.Fatal("hit must return the same cached image pointer")
	}
}

func TestDecodedFrameCacheBytesBound(t *testing.T) {
	// Each 100x100 RGB frame is 30000 bytes; budget holds ~3.
	c := newDecodedFrameCache(100 << 10) // 102400 bytes
	for i := 0; i < 10; i++ {
		k := frameCacheKey{col: i, format: decoder.PixelFormatRGB}
		if _, err := c.getOrLoad(k, func() (*decoder.Image, error) { return frame(100, 100, byte(i)), nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.len(); got == 0 || got*30000 > 102400 {
		t.Fatalf("len = %d (%d bytes), want bounded under 102400", got, got*30000)
	}
}

func TestDecodedFrameCacheErrorNotCached(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{col: 5, format: decoder.PixelFormatRGB}
	boom := errors.New("boom")
	if _, err := c.getOrLoad(key, func() (*decoder.Image, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	var second int
	if _, err := c.getOrLoad(key, func() (*decoder.Image, error) { second++; return frame(2, 2, 1), nil }); err != nil {
		t.Fatal(err)
	}
	if second != 1 {
		t.Fatal("after an error the entry must not be cached; load should re-run")
	}
}

func TestDecodedFrameCacheConcurrentSameKeyLoadsOnce(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{col: 7, format: decoder.PixelFormatRGB}
	release := make(chan struct{})
	var mu sync.Mutex
	loads := 0
	load := func() (*decoder.Image, error) {
		mu.Lock()
		loads++
		mu.Unlock()
		<-release // hold the load so concurrent callers must wait on the promise
		return frame(8, 8, 3), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.getOrLoad(key, load) }()
	}
	// Give goroutines time to all block on the promise, then release.
	for c.inflightCountForTest() == 0 {
	}
	close(release)
	wg.Wait()
	if loads != 1 {
		t.Fatalf("loads = %d, want 1 (promise pattern must dedupe)", loads)
	}
}

func TestDecodedFrameCacheDoesNotEvictInflight(t *testing.T) {
	c := newDecodedFrameCache(60 << 10) // ~2 frames of 100x100 (30000 ea)
	hold := make(chan struct{})
	started := make(chan struct{})
	inflightKey := frameCacheKey{col: 99, format: decoder.PixelFormatRGB}

	go func() {
		_, _ = c.getOrLoad(inflightKey, func() (*decoder.Image, error) {
			close(started)
			<-hold
			return frame(100, 100, 1), nil
		})
	}()
	<-started // inflightKey is now in-flight, occupying a slot but 0 accounted bytes

	// Flood with completed frames to blow the byte budget while inflightKey is held.
	for i := 0; i < 6; i++ {
		k := frameCacheKey{col: i, format: decoder.PixelFormatRGB}
		_, _ = c.getOrLoad(k, func() (*decoder.Image, error) { return frame(100, 100, byte(i)), nil })
	}
	if !c.presentForTest(inflightKey) {
		t.Fatal("in-flight entry must not be evicted")
	}
	close(hold)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestDecodedFrameCache -count=1`
Expected: FAIL — `undefined: newDecodedFrameCache`, `frameCacheKey`, etc.

- [ ] **Step 3: Write the implementation**

Create `frame_cache.go`:

```go
package opentile

import (
	"container/list"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// frameCacheKey identifies one decoded raw tile of an (overlapping) level.
// The format is part of the key because the decoded pixel layout depends on it.
type frameCacheKey struct {
	image, level, col, row int
	format                 decoder.PixelFormat
}

// decodedFrameCache is a byte-bounded, promise-pattern LRU of decoded raw
// tiles, held per-Slide and used by imageStitchedTile to composite display
// tiles without re-decoding source frames shared by adjacent tiles.
//
// Concurrency: operations are serialized by mu, but mu is released before the
// load callback runs. Concurrent callers for the same key block on the entry's
// ready chan (the first caller's load runs; the rest share its result).
//
// Eviction: LRU by recency, bounded by maxBytes (sum of decoded frame byte
// lengths). In-flight entries are NEVER evicted (their bytes are 0 until the
// load completes, and skipping them avoids the v0.47.1 eviction-race class).
type decodedFrameCache struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	entries  map[frameCacheKey]*frameCacheEntry
	order    *list.List // values are frameCacheKey; front = MRU
}

type frameCacheEntry struct {
	pix      *decoder.Image
	err      error
	bytes    int64
	key      frameCacheKey
	elem     *list.Element
	ready    chan struct{}
	inflight bool
}

// newDecodedFrameCache constructs an empty cache bounded to maxBytes
// (clamped to >= 1).
func newDecodedFrameCache(maxBytes int64) *decodedFrameCache {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &decodedFrameCache{
		maxBytes: maxBytes,
		entries:  make(map[frameCacheKey]*frameCacheEntry),
		order:    list.New(),
	}
}

// getOrLoad returns the cached decoded frame for key, calling load on a miss.
// Concurrent callers for the same key share the first caller's load.
// On load error the entry is dropped (next call retries) and the error is
// returned to every waiter.
func (c *decodedFrameCache) getOrLoad(key frameCacheKey, load func() (*decoder.Image, error)) (*decoder.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.order.MoveToFront(e.elem)
		ready := e.ready
		c.mu.Unlock()
		<-ready
		return e.pix, e.err
	}
	e := &frameCacheEntry{key: key, ready: make(chan struct{}), inflight: true}
	c.entries[key] = e
	e.elem = c.order.PushFront(key)
	c.mu.Unlock()

	pix, err := load()

	c.mu.Lock()
	if err != nil {
		if cur, ok := c.entries[key]; ok && cur == e {
			delete(c.entries, key)
			c.order.Remove(e.elem)
		}
		c.mu.Unlock()
		e.err = err
		close(e.ready)
		return nil, err
	}
	e.pix = pix
	e.bytes = int64(len(pix.Pix))
	e.inflight = false
	c.curBytes += e.bytes
	c.evictIfOverLocked()
	c.mu.Unlock()
	close(e.ready)
	return pix, nil
}

// evictIfOverLocked must be called with c.mu held. Evicts the least-recently
// used NON-in-flight entries until curBytes <= maxBytes (or none remain).
func (c *decodedFrameCache) evictIfOverLocked() {
	for c.curBytes > c.maxBytes {
		var victim *list.Element
		for el := c.order.Back(); el != nil; el = el.Prev() {
			if ent := c.entries[el.Value.(frameCacheKey)]; ent != nil && !ent.inflight {
				victim = el
				break
			}
		}
		if victim == nil {
			return // everything is in-flight; cannot evict
		}
		k := victim.Value.(frameCacheKey)
		ent := c.entries[k]
		c.order.Remove(victim)
		delete(c.entries, k)
		c.curBytes -= ent.bytes
	}
}

// len reports the current entry count (tests).
func (c *decodedFrameCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// inflightCountForTest reports how many entries are mid-load (tests).
func (c *decodedFrameCache) inflightCountForTest() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.entries {
		if e.inflight {
			n++
		}
	}
	return n
}

// presentForTest reports whether key is currently cached (tests).
func (c *decodedFrameCache) presentForTest(key frameCacheKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[key]
	return ok
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test . -run TestDecodedFrameCache -race -count=1`
Expected: PASS (all 5 subtests, clean under `-race`).

- [ ] **Step 5: Commit**

```bash
git add frame_cache.go frame_cache_test.go
git commit -m "feat(opentile): byte-bounded decoded-frame cache for display tiles"
```

---

## Task 2: Extract the shared composite loop

**Files:**
- Create: `stitched_tile.go` (the `compositeStitchedLoop` helper only; the rest is added in Task 3)
- Modify: `region.go:125-146`
- Test: `stitched_tile_test.go` (helper unit test only; behavior tests added in Task 5)

DRY the per-tile composite/blit so `ReadRegion` and the upcoming `StitchedTile` share one implementation. Behavior must be bit-identical — the existing parity suite is the guard.

- [ ] **Step 1: Write the failing test**

Create `stitched_tile_test.go`:

```go
package opentile

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// fillTile makes a tileW x tileH RGB image whose every pixel is (r,g,0).
func fillTile(w, h int, r, g byte) *decoder.Image {
	img := decoder.NewImageFormat(w, h, decoder.PixelFormatRGB)
	for i := 0; i+2 < len(img.Pix); i += 3 {
		img.Pix[i] = r
		img.Pix[i+1] = g
	}
	return img
}

func TestCompositeStitchedLoopBlitsIntersectingTiles(t *testing.T) {
	// One tile at origin (0,0), 100x100; dst covers stitched rect [0,0,100,100).
	rl := &fakeLayoutReader{originX: 0}
	dst := decoder.NewImageFormat(100, 100, decoder.PixelFormatRGB)
	fillWhite(dst)
	err := compositeStitchedLoop(rl, 0, 0, 0, 0, 0, 100, 100, 100, 100, dst,
		func(col, row int) (*decoder.Image, error) { return fillTile(100, 100, 42, 7), nil })
	if err != nil {
		t.Fatal(err)
	}
	if dst.Pix[0] != 42 || dst.Pix[1] != 7 {
		t.Fatalf("top-left = (%d,%d), want (42,7) — tile not blitted", dst.Pix[0], dst.Pix[1])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestCompositeStitchedLoop -count=1`
Expected: FAIL — `undefined: compositeStitchedLoop`.

- [ ] **Step 3: Write the helper**

Create `stitched_tile.go`:

```go
package opentile

import "github.com/wsilabs/opentile-go/decoder"

// compositeStitchedLoop blits every regionLayout tile intersecting the clipped
// rectangle [x0,y0,x1,y1) into dst, which represents the stitched-space
// rectangle whose top-left is (regionX, regionY) and whose extent is dst's
// dimensions. fetch returns the decoded raw tile for (col,row); callers supply
// either a fresh-decode-into-scratch fetch (ReadRegion) or a cache-backed fetch
// (StitchedTile). dst must already be white-initialized by the caller.
//
// Shared by imageReadRegionImpl and imageStitchedTile so the two compositing
// paths cannot drift.
func compositeStitchedLoop(rl regionLayout, level, regionX, regionY, x0, y0, x1, y1, tileW, tileH int, dst *decoder.Image, fetch func(col, row int) (*decoder.Image, error)) error {
	for _, tp := range rl.TilesIntersecting(level, x0, y0, x1-x0, y1-y0) {
		tileX, tileY, ok := rl.TileOrigin(level, tp.Col, tp.Row)
		if !ok {
			continue
		}
		src, err := fetch(tp.Col, tp.Row)
		if err != nil {
			return err
		}
		ix0 := maxInt(tileX, x0)
		iy0 := maxInt(tileY, y0)
		ix1 := minInt(tileX+tileW, x1)
		iy1 := minInt(tileY+tileH, y1)
		if ix0 >= ix1 || iy0 >= iy1 {
			continue
		}
		blitInto(src, ix0-tileX, iy0-tileY, ix1-ix0, iy1-iy0, dst, ix0-regionX, iy0-regionY)
	}
	return nil
}
```

- [ ] **Step 4: Run the helper test to verify it passes**

Run: `go test . -run TestCompositeStitchedLoop -count=1`
Expected: PASS.

- [ ] **Step 5: Refactor region.go to use the helper**

In `region.go`, replace the layout-aware loop body (currently `:125-146`, the `fillWhite(dst)` through the `for ... return nil`) with:

```go
		fillWhite(dst) // stitched output always white-initialized (overlaps/gaps)
		scratch := borrowTileScratch(lvl.TileSize.W, lvl.TileSize.H, dst.Format)
		defer returnTileScratch(scratch)
		return compositeStitchedLoop(rl, level, x, y, x0, y0, x1, y1,
			lvl.TileSize.W, lvl.TileSize.H, dst,
			func(col, row int) (*decoder.Image, error) {
				if err := s.imageDecodedTileInto(image, level, col, row, scratch, opts...); err != nil {
					return nil, fmt.Errorf("opentile: decode tile (%d,%d) at level %d: %w", col, row, level, err)
				}
				return scratch, nil
			})
```

(Leaves the `StitchedSize`-clipping block at `:114-124` unchanged. `fmt` stays imported — still used elsewhere in `region.go`.)

- [ ] **Step 6: Verify the region path is bit-identical**

Run: `go test . -run 'Region|Layout' -race -count=1`
Expected: PASS (existing region + layout tests unchanged).

Run (if fixtures present locally): `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run TestSlideParity -count=1`
Expected: PASS — the 10 non-BIF formats and BIF stitched region reads are byte-identical (this is the DRY-refactor safety gate).

- [ ] **Step 7: Commit**

```bash
git add stitched_tile.go stitched_tile_test.go region.go
git commit -m "refactor(opentile): extract compositeStitchedLoop shared by region + stitched paths"
```

---

## Task 3: imageStitchedTile + Slide cache plumbing

**Files:**
- Modify: `stitched_tile.go` (add `imageStitchedTile`, `frameCacheFor`, `minFrameCacheBytes`)
- Modify: `slide.go:52-78` (add field), `slide.go:196-212` (clear in Close)
- Test: deferred to Task 5 (needs the fake reader)

- [ ] **Step 1: Add the `frameCache` field to the Slide struct**

In `slide.go`, inside `type Slide struct` (after the `readBudget` field, `:68`), add:

```go
	// Display-tile decoded-frame cache: lazily created on first StitchedTile
	// call, byte-bounded by readBudget, drained by Close. Decode-once-blit-many
	// so adjacent display tiles sharing an overlapping source frame don't
	// re-decode it. Guarded by handlesMu (same lock as the decoder pools).
	frameCache *decodedFrameCache
```

- [ ] **Step 2: Clear the cache in Close**

In `slide.go` `Close` (`:197-200`), extend the locked block:

```go
	s.handlesMu.Lock()
	handles := s.handles
	s.handles = nil
	s.frameCache = nil
	s.handlesMu.Unlock()
```

- [ ] **Step 3: Add `imageStitchedTile` and `frameCacheFor` to stitched_tile.go**

Append to `stitched_tile.go` (and add the imports it needs — `fmt` is NOT needed here; `decoder` already imported):

```go
// minFrameCacheBytes floors the display-tile cache so a small or unset memory
// budget still retains a few source frames for the decode-once-blit-many win.
const minFrameCacheBytes = 64 << 20 // 64 MiB

// frameCacheFor lazily creates the per-Slide decoded-frame cache, sized from
// readBudget (floored at minFrameCacheBytes). Guarded by handlesMu.
func (s *Slide) frameCacheFor() *decodedFrameCache {
	s.handlesMu.Lock()
	defer s.handlesMu.Unlock()
	if s.frameCache == nil {
		budget := s.readBudget
		if budget < minFrameCacheBytes {
			budget = minFrameCacheBytes
		}
		s.frameCache = newDecodedFrameCache(budget)
	}
	return s.frameCache
}

// imageStitchedTile returns a clean, non-overlapping display tile from the
// canonical grid ceil(Size/TileSize). For readers that expose the regionLayout
// capability (overlapping formats such as stitched BIF) it composites the
// stitched-space rectangle [tx*TileW, ty*TileH, TileW, TileH] from the
// per-Slide decoded-frame cache. For every other reader it is exactly
// imageDecodedTile, so callers can use StitchedTile uniformly across formats.
//
// Scale > 1 is unsupported on overlapping levels (use ScaledStrips /
// ReadRegionScaled for scaled traversal); it returns decoder.ErrUnsupportedScale.
func (s *Slide) imageStitchedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	rl, ok := regionLayoutOf(s.r)
	if !ok {
		return s.imageDecodedTile(image, level, tx, ty, opts...)
	}
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}
	cfg := newDecodeConfig(opts)
	if cfg.scale > 1 {
		return nil, decoder.ErrUnsupportedScale
	}
	tileW, tileH := lvl.TileSize.W, lvl.TileSize.H
	x0, y0 := tx*tileW, ty*tileH
	x1, y1 := x0+tileW, y0+tileH
	if sw, sh, ok := rl.StitchedSize(level); ok {
		if x1 > sw {
			x1 = sw
		}
		if y1 > sh {
			y1 = sh
		}
	}
	dst := decoder.NewImageFormat(tileW, tileH, cfg.format)
	fillWhite(dst)
	if x0 >= x1 || y0 >= y1 {
		return dst, nil // fully outside the stitched hull → white tile
	}
	fc := s.frameCacheFor()
	err = compositeStitchedLoop(rl, level, x0, y0, x0, y0, x1, y1, tileW, tileH, dst,
		func(col, row int) (*decoder.Image, error) {
			key := frameCacheKey{image: image, level: level, col: col, row: row, format: cfg.format}
			return fc.getOrLoad(key, func() (*decoder.Image, error) {
				return s.imageDecodedTile(image, level, col, row, opts...)
			})
		})
	if err != nil {
		return nil, err
	}
	return dst, nil
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./...`
Expected: builds clean.

Run: `go build -tags nocgo ./...`
Expected: builds clean (the new code is pure Go; decode itself is gated by the existing nocgo decoder behavior).

- [ ] **Step 5: Commit**

```bash
git add stitched_tile.go slide.go
git commit -m "feat(opentile): imageStitchedTile composites display tiles via the frame cache"
```

---

## Task 4: Public (*Level).StitchedTile + StitchedGrid

**Files:**
- Modify: `level_reads.go` (append two methods)
- Test: deferred to Task 5

- [ ] **Step 1: Add the receiver methods**

Append to `level_reads.go`:

```go
// StitchedTile returns a clean, non-overlapping display tile from the canonical
// grid StitchedGrid() (== ceil(Size/TileSize)). For overlapping levels
// (Overlapping == true: stitched BIF) it composites the stitched image so the
// returned tile is a true partition of Size; for every other format it is
// exactly DecodedTile. Pixels match ReadRegion over the tile's rectangle.
//
// Use this (with StitchedGrid) for display/rendering. Use Tile / DecodedTile +
// Grid only when you want the raw stored (possibly overlapping) tiles, e.g. for
// faithful transcoding. Scale > 1 is unsupported on overlapping levels (use the
// Pyramid's ReadRegionScaled / ScaledStrips); it returns ErrUnsupportedScale.
func (l *Level) StitchedTile(tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	return l.slide.imageStitchedTile(l.PyramidIndex, l.Index, tx, ty, opts...)
}

// StitchedGrid is the canonical display grid, ceil(Size/TileSize) per axis — a
// clean partition of Size. Equals Grid for non-overlapping levels; for an
// overlapping level it is the grid that tiles Size (whereas Grid stays the raw
// overlapping grid). Iterate StitchedGrid with StitchedTile to render.
func (l *Level) StitchedGrid() Size {
	return Size{
		W: ceilDiv(l.Size.W, l.TileSize.W),
		H: ceilDiv(l.Size.H, l.TileSize.H),
	}
}
```

- [ ] **Step 2: Write the StitchedGrid unit test**

Append to `stitched_tile_test.go`:

```go
func TestLevelStitchedGrid(t *testing.T) {
	l := &Level{Size: Size{W: 260, H: 180}, TileSize: Size{W: 100, H: 100}}
	if g := l.StitchedGrid(); g != (Size{W: 3, H: 2}) {
		t.Fatalf("StitchedGrid = %v, want {3,2}", g)
	}
}
```

- [ ] **Step 3: Run it**

Run: `go test . -run TestLevelStitchedGrid -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add level_reads.go stitched_tile_test.go
git commit -m "feat(opentile): public Level.StitchedTile + StitchedGrid display accessors"
```

---

## Task 5: Behavior tests via a fake overlapping reader

**Files:**
- Modify: `stitched_tile_test.go` (add the fake reader + behavior tests)

A fixture-free fake that implements `slideReader` + `regionLayout` + `decodedTiler`, with a deterministic 3×2 overlapping layout (tiles 100×100, origins at multiples of 80) and a decode counter. This pins compose-correctness, decode-once, scale rejection, OOB, and the no-layout delegation — all without a real codec (so it runs under `nocgo` and in CI).

- [ ] **Step 1: Add the fake reader**

First merge these into the file's existing `import` block (created in Task 2 — it already has `"testing"` and the `decoder` import):

```go
	"context"
	"errors"
	"io"
	"iter"
	"sync/atomic"
```

Then append the fake reader to `stitched_tile_test.go`:

```go

// fakeStitchReader: a 3x2 grid of 100x100 tiles, origins at col*80, row*80
// (20px overlap). StitchedSize is 260x180. Each raw tile decodes to a solid
// color (R=col+1, G=row+1) and bumps decodeCount. Implements slideReader +
// regionLayout + decodedTiler. hasLayout=false hides regionLayout (delegation).
type fakeStitchReader struct {
	hasLayout   bool
	decodeCount int64
	tileW       int
	tileH       int
}

const fakeRawCols, fakeRawRows, fakeStride = 3, 2, 80

func newFakeStitchReader(hasLayout bool) *fakeStitchReader {
	return &fakeStitchReader{hasLayout: hasLayout, tileW: 100, tileH: 100}
}

// --- regionLayout (only when hasLayout) ---

func (f *fakeStitchReader) TileOrigin(level, col, row int) (int, int, bool) {
	if col < 0 || col >= fakeRawCols || row < 0 || row >= fakeRawRows {
		return 0, 0, false
	}
	return col * fakeStride, row * fakeStride, true
}

func (f *fakeStitchReader) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	var out []struct{ Col, Row int }
	x1, y1 := x+w, y+h
	for r := 0; r < fakeRawRows; r++ {
		for c := 0; c < fakeRawCols; c++ {
			ox, oy := c*fakeStride, r*fakeStride
			if ox < x1 && ox+f.tileW > x && oy < y1 && oy+f.tileH > y {
				out = append(out, struct{ Col, Row int }{c, r})
			}
		}
	}
	return out
}

func (f *fakeStitchReader) StitchedSize(level int) (int, int, bool) {
	return (fakeRawCols-1)*fakeStride + f.tileW, (fakeRawRows-1)*fakeStride + f.tileH, true
}

// --- decodedTiler ---

func (f *fakeStitchReader) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	atomic.AddInt64(&f.decodeCount, 1)
	return fillTile(f.tileW, f.tileH, byte(tx+1), byte(ty+1)), nil
}

// --- slideReader (only Level / Pyramids are exercised; the rest are stubs) ---

func (f *fakeStitchReader) Format() Format { return FormatBIF }
func (f *fakeStitchReader) Level(image, level int) (Level, error) {
	return Level{Size: Size{W: 260, H: 180}, TileSize: Size{W: f.tileW, H: f.tileH},
		Compression: CompressionJPEG, Overlapping: true}, nil
}
func (f *fakeStitchReader) Pyramids() []Pyramid {
	lvl, _ := f.Level(0, 0)
	return []Pyramid{{Levels: []Level{lvl}}}
}
func (f *fakeStitchReader) AssociatedImages() []AssociatedImage { return nil }
func (f *fakeStitchReader) Metadata() Metadata                  { return Metadata{} }
func (f *fakeStitchReader) ICCProfile() []byte                  { return nil }
func (f *fakeStitchReader) WarmLevel(image, level int) error    { return nil }
func (f *fakeStitchReader) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	return nil, errors.New("unused")
}
func (f *fakeStitchReader) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("unused")
}
func (f *fakeStitchReader) ImageTileMaxSize(image, level int) int      { return 0 }
func (f *fakeStitchReader) ImageTilePrefix(image, level int) []byte    { return nil }
func (f *fakeStitchReader) ImageTileBodyMaxSize(image, level int) int  { return 0 }
func (f *fakeStitchReader) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("unused")
}
func (f *fakeStitchReader) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (f *fakeStitchReader) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[Point, TileResult] {
	return func(yield func(Point, TileResult) bool) {}
}
func (f *fakeStitchReader) Close() error { return nil }
```

`fakeStitchReader` always declares the three `regionLayout` methods, so
`regionLayoutOf` always finds them on it. The delegation case (no `regionLayout`)
uses a separate, hand-written forwarder type (`noLayout`, defined in Step 4) that
forwards `slideReader` + `decodedTiler` but deliberately does NOT declare the
layout methods. (`hasLayout` on `fakeStitchReader` is unused by these tests — it
exists only to document intent; you may drop the field.)

- [ ] **Step 2: Write the compose + decode-once tests**

Append:

```go
func TestStitchedTileComposesAndCountsDecodeOnce(t *testing.T) {
	f := newFakeStitchReader(true)
	s := &Slide{r: f, readBudget: 64 << 20}

	// Canonical grid = ceil(260/100) x ceil(180/100) = 3 x 2.
	lvl, _ := f.Level(0, 0)
	gw := ceilDiv(lvl.Size.W, lvl.TileSize.W)
	gh := ceilDiv(lvl.Size.H, lvl.TileSize.H)
	if gw != 3 || gh != 2 {
		t.Fatalf("canonical grid %dx%d, want 3x2", gw, gh)
	}

	// Compose every display tile; each must be fully painted (no white left
	// inside the hull) and pixels must come from a raw tile color.
	for vy := 0; vy < gh; vy++ {
		for vx := 0; vx < gw; vx++ {
			img, err := s.imageStitchedTile(0, 0, vx, vy)
			if err != nil {
				t.Fatalf("stitched tile (%d,%d): %v", vx, vy, err)
			}
			if img.Width != 100 || img.Height != 100 {
				t.Fatalf("tile (%d,%d) dims %dx%d, want 100x100", vx, vy, img.Width, img.Height)
			}
			// Top-left pixel of an in-hull tile is covered by some raw tile,
			// so R >= 1 (raw colors are col+1) — never the white fill (255 is
			// possible only if uncovered; assert it is a real raw color < 255).
			if img.Pix[0] == 0 {
				t.Fatalf("tile (%d,%d) top-left unpainted", vx, vy)
			}
		}
	}

	// Decode-once: 6 distinct raw tiles exist; across the full traversal each
	// is decoded exactly once thanks to the per-Slide cache.
	if got := atomic.LoadInt64(&f.decodeCount); got != int64(fakeRawCols*fakeRawRows) {
		t.Fatalf("decodeCount = %d, want %d (each raw frame once)", got, fakeRawCols*fakeRawRows)
	}
}

func TestStitchedTileScaleUnsupported(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(true), readBudget: 64 << 20}
	if _, err := s.imageStitchedTile(0, 0, 0, 0, WithScale(2)); !errors.Is(err, decoder.ErrUnsupportedScale) {
		t.Fatalf("scale=2 err = %v, want ErrUnsupportedScale", err)
	}
}

func TestStitchedTileOutOfHullIsWhite(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(true), readBudget: 64 << 20}
	// Canonical tile (5,5) is entirely past StitchedSize (260x180) → all white.
	img, err := s.imageStitchedTile(0, 0, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(img.Pix); i++ {
		if img.Pix[i] != 0xFF {
			t.Fatalf("out-of-hull tile pixel[%d] = %d, want 255 (white)", i, img.Pix[i])
		}
	}
}
```

> Verify `WithScale` is the correct option name in `options.go` before relying on it; the memory of the codebase uses `WithScale(n int)`. If the option is named differently, use the actual name.

- [ ] **Step 3: Run the compose/scale/OOB tests**

Run: `go test . -run 'TestStitchedTile(Composes|Scale|OutOfHull)' -race -count=1`
Expected: PASS.

- [ ] **Step 4: Write the delegation test (no regionLayout → DecodedTile)**

`noLayout` is a hand-written forwarder: it forwards every `slideReader` +
`decodedTiler` method to an inner `*fakeStitchReader` but deliberately does NOT
declare the three `regionLayout` methods, so `regionLayoutOf` misses it and
`StitchedTile` must delegate to `DecodedTile`. (It must NOT embed
`*fakeStitchReader`, because Go would then promote the layout methods.) Append:

```go
type noLayout struct{ inner *fakeStitchReader }

func TestStitchedTileDelegatesWithoutLayout(t *testing.T) {
	s := &Slide{r: &noLayout{newFakeStitchReader(true)}, readBudget: 64 << 20}
	got, err := s.imageStitchedTile(0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.imageDecodedTile(0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != want.Width || got.Pix[0] != want.Pix[0] {
		t.Fatal("without regionLayout, StitchedTile must equal DecodedTile")
	}
}

func (n *noLayout) Format() Format                              { return n.inner.Format() }
func (n *noLayout) Level(i, l int) (Level, error)              { return n.inner.Level(i, l) }
func (n *noLayout) Pyramids() []Pyramid                         { return n.inner.Pyramids() }
func (n *noLayout) AssociatedImages() []AssociatedImage         { return nil }
func (n *noLayout) Metadata() Metadata                          { return Metadata{} }
func (n *noLayout) ICCProfile() []byte                          { return nil }
func (n *noLayout) WarmLevel(i, l int) error                    { return nil }
func (n *noLayout) ImageRawTile(i, l, x, y int) ([]byte, error) { return nil, errors.New("unused") }
func (n *noLayout) ImageRawTileInto(i, l, x, y int, d []byte) (int, error) {
	return 0, errors.New("unused")
}
func (n *noLayout) ImageTileMaxSize(i, l int) int     { return 0 }
func (n *noLayout) ImageTilePrefix(i, l int) []byte   { return nil }
func (n *noLayout) ImageTileBodyMaxSize(i, l int) int { return 0 }
func (n *noLayout) ImageTileBodyInto(i, l, x, y int, d []byte) (int, error) {
	return 0, errors.New("unused")
}
func (n *noLayout) ImageTileReader(i, l, x, y int) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (n *noLayout) ImageRangeTiles(ctx context.Context, i, l int) iter.Seq2[Point, TileResult] {
	return func(yield func(Point, TileResult) bool) {}
}
func (n *noLayout) ImageDecodedTile(i, l, x, y int, o decoder.DecodeOptions) (*decoder.Image, error) {
	return n.inner.ImageDecodedTile(i, l, x, y, o)
}
func (n *noLayout) Close() error { return nil }
```

- [ ] **Step 5: Run all stitched-tile behavior tests**

Run: `go test . -run 'TestStitchedTile|TestCompositeStitchedLoop|TestLevelStitchedGrid' -race -count=1`
Expected: PASS (compose, decode-once, scale, OOB, delegation, grid, helper).

- [ ] **Step 6: Commit**

```bash
git add stitched_tile_test.go
git commit -m "test(opentile): fake-reader behavior tests for StitchedTile (compose/decode-once/scale/OOB/delegation)"
```

---

## Task 6: BIF fixture integration test (gated)

**Files:**
- Create: `bif_stitched_tile_test.go`

Proves the real payoff against a real overlapping slide: `StitchedTile(vx,vy)` is byte-identical to `ReadRegion` over the same canonical rectangle (the spec's equivalence guarantee, with a real JPEG codec and the real BIF layout), and `StitchedGrid` cleanly tiles `Size`. Skips when the fixture or a codec is unavailable, so it never breaks `nocgo`/no-fixture CI.

- [ ] **Step 1: Write the gated test**

Create `bif_stitched_tile_test.go`:

```go
package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestBIFStitchedTileEqualsReadRegion(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "bif", "Ventana-1.bif")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	s, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Find an overlapping level (the stitched L0).
	var lvl *opentile.Level
	for _, l := range s.Levels() {
		if l.Overlapping {
			lvl = l
			break
		}
	}
	if lvl == nil {
		t.Skip("no overlapping level in fixture")
	}

	// StitchedGrid must cleanly tile Size.
	g := lvl.StitchedGrid()
	tw, th := lvl.TileSize.W, lvl.TileSize.H
	if g.W != (lvl.Size.W+tw-1)/tw || g.H != (lvl.Size.H+th-1)/th {
		t.Fatalf("StitchedGrid %v does not tile Size %v with TileSize %v", g, lvl.Size, lvl.TileSize)
	}

	// Sample a handful of interior display tiles (including seam-straddling
	// ones) and assert StitchedTile == ReadRegion over the same rect.
	coords := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {g.W / 2, g.H / 2}}
	for _, c := range coords {
		vx, vy := c[0], c[1]
		if vx >= g.W || vy >= g.H {
			continue
		}
		st, err := lvl.StitchedTile(vx, vy)
		if err != nil {
			t.Fatalf("StitchedTile(%d,%d): %v", vx, vy, err)
		}
		rr, err := lvl.ReadRegion(opentile.Region{
			Origin: opentile.Point{X: vx * tw, Y: vy * th},
			Size:   opentile.Size{W: tw, H: th},
		})
		if err != nil {
			t.Fatalf("ReadRegion(%d,%d): %v", vx, vy, err)
		}
		if len(st.Pix) != len(rr.Pix) {
			t.Fatalf("tile (%d,%d): len %d != region len %d", vx, vy, len(st.Pix), len(rr.Pix))
		}
		for i := range st.Pix {
			if st.Pix[i] != rr.Pix[i] {
				t.Fatalf("tile (%d,%d): pixel %d differs (stitched %d, region %d)", vx, vy, i, st.Pix[i], rr.Pix[i])
			}
		}
	}
}
```

- [ ] **Step 2: Run it (local, with fixtures)**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run TestBIFStitchedTileEqualsReadRegion -count=1`
Expected: PASS (or SKIP if `Ventana-1.bif` is absent locally — acceptable; CI's `bif.tar` fixture exercises it).

- [ ] **Step 3: Commit**

```bash
git add bif_stitched_tile_test.go
git commit -m "test(bif): StitchedTile is byte-identical to ReadRegion over the canonical grid"
```

---

## Task 7: Docs, CHANGELOG, and full verification

**Files:**
- Modify: `docs/formats/bif.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Document the viewer path in bif.md**

In `docs/formats/bif.md`, in the section that explains `Level.Overlapping` / the two-grid contract, add a paragraph:

```markdown
### Display tiles (`StitchedTile`)

For rendering, call `level.StitchedTile(x, y)` over `level.StitchedGrid()`
(== `ceil(Size/TileSize)`) instead of `DecodedTile` over `Grid`. `StitchedTile`
returns clean, non-overlapping tiles composited from the stitched image — the
overlap, seam, and placement handling stays inside opentile-go, so a viewer
treats BIF exactly like SVS/NDPI. `StitchedTile` is defined for every format
(it equals `DecodedTile` when a level is not overlapping), so consumers can call
it uniformly. `Tile` / `DecodedTile` / `Grid` continue to return the raw stored
overlapping tiles for faithful transcoding. `StitchedTile` requires a decoder
for the level's codec and does not support `Scale > 1` (use the pyramid's
`ReadRegionScaled` / `ScaledStrips` for scaled traversal).
```

- [ ] **Step 2: Add the CHANGELOG entry**

In `CHANGELOG.md`, insert a new section above `## [0.49.0]`:

```markdown
## [Unreleased]

### Added

- **`(*Level).StitchedTile` + `(*Level).StitchedGrid`** — a clean,
  non-overlapping display-tile surface for overlapping-tile formats. For a
  stitched BIF level, `StitchedTile(x, y)` composites the stitched image so the
  returned tile is a true partition of `Size`, iterated over
  `StitchedGrid()` (`ceil(Size/TileSize)`); for every other format it equals
  `DecodedTile`, so viewers can call it uniformly without format-specific code.
  Backed by a new per-`Slide` byte-bounded decoded-frame cache
  (decode-once-blit-many; bounded by the read memory budget), so adjacent
  display tiles sharing an overlapping source frame decode it once — throughput
  matches the region API. Built generically over the existing `regionLayout`
  capability, so future overlapping formats (MRXS, DZI/SZI `Overlap>0`) gain
  display tiles by implementing a layout, contributing no compositing code.
  Additive; `Tile` / `DecodedTile` / `Grid` / `Overlapping` and the raw-tile
  surface are unchanged. (Design:
  `docs/superpowers/specs/2026-06-23-stitched-display-tile-path-design.md`.)
```

- [ ] **Step 3: Full verification**

Run: `go vet ./...`
Expected: clean.

Run: `go build ./... && go build -tags nocgo ./...`
Expected: both build clean.

Run: `make test` (i.e. `go test ./... -race -count=1`)
Expected: PASS across all packages (only the pre-existing harmless `ld: warning: ignoring duplicate libraries` linker notes on macOS).

Run (local, fixtures): `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run 'TestSlideParity|TestBIFStitchedTile' -count=1`
Expected: PASS — region/parity bit-identical (Task 2 guard) and BIF equivalence (Task 6).

- [ ] **Step 4: Commit**

```bash
git add docs/formats/bif.md CHANGELOG.md
git commit -m "docs: document StitchedTile display path; CHANGELOG [Unreleased]"
```

---

## Notes for the executor

- **DRY guard:** Task 2's refactor must keep `ReadRegion` bit-identical. The parity suite (`TestSlideParity`) and the region/layout tests are the gate — if any pixel changes there, the extraction is wrong; do not "accept" a diff.
- **No raw-bytes change:** `Tile()` / `TileReader()` must remain untouched and faithful. `StitchedTile` is the *only* new pixel surface; the raw path that wsitools depends on does not move.
- **Consumer impact:** this is purely additive — wsitools/openscope are unaffected until they call `StitchedTile`. (Per the standing lesson that the sibling consumers are not in opentile-go CI, the openscope migration to `StitchedTile` is theirs to make; nothing here breaks the current surface.)
- **Version:** additive MINOR. Ship as the next minor (v0.50.0) following the release cadence after CI is green.
- **Deferred (named in the spec, not built here):** MRXS and DZI/SZI `Overlap>0` `regionLayout` implementations; backing `ReadRegion` with the same cache; `Scale > 1` on `StitchedTile`. Each is its own spec/plan.
```
