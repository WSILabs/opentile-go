# opentile-go v0.28 Cross-Format Decoder-Handle Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate per-tile `decoder.Factory.New() / dec.Close()` churn across every format routing through `Slide.ImageDecodedTile` by introducing a small fixed-size pool of long-lived `decoder.Decoder` instances per `(Slide, codec)`, sized `min(NumCPU, 8)`. NDPI's v0.27 per-`strippedImage` `decoderHandle` migrates to the same shared primitive, gaining multi-core parallelism on `Slide.DecodedTile` calls.

**Architecture:** New `internal/decoderhandle.Pool` type (buffered-channel-based, lazy-init, mutex-guarded outstanding counter). NDPI's existing `formats/ndpi/decoder_handle.go` is deleted; `strippedImage.decHandle` retypes to `*decoderhandle.Pool` (instance ownership stays per-level). `Slide` gains a per-codec `handles map[uint16]*decoderhandle.Pool` lazily populated by a new `decoderFor(tag)` accessor; `Slide.ImageDecodedTile` and `Slide.ImageDecodedTileInto` replace `fac.New() / dec.Close()` with `pool.Borrow() / pool.Return()`. `Slide.Close` drains every cached pool.

**Tech Stack:** Go 1.26+, cgo for libjpeg-turbo (existing). `container/list` unused (channel-based pool, not LRU). Pure-Go primitive over `decoder.Factory` / `decoder.Decoder` interfaces. No new external dependencies.

**Reference docs (read before starting):**
- Spec (READ FIRST): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-29-opentile-go-v28-cross-format-decoder-pool-design.md`
- v0.27 spec (the foundation): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md`
- v0.27 NDPI primitive being extracted: `~/GitHub/opentile-go/formats/ndpi/decoder_handle.go`
- v0.27 dispatch site: `~/GitHub/opentile-go/slide_decoded_tile.go:36-85`
- v0.27 NDPI strippedImage fast path: `~/GitHub/opentile-go/formats/ndpi/stripped.go`
- v0.27 Slide struct + Close: `~/GitHub/opentile-go/slide.go` + `~/GitHub/opentile-go/opentile.go`
- v0.27 cmd/bench/ndpi pattern: `~/GitHub/opentile-go/cmd/bench/ndpi/main.go`

**Branch:** `feat/v0.28` on opentile-go. Create from `main` at `90cec57` (the v0.28 spec commit).

**CLAUDE.md invariants worth re-reading:**
- "Public API stable from v0.3." v0.28 adds only unexported / `internal/` symbols.
- "Lock-free hot path for metadata." Tile-read hot path acquires `Pool.initMu` and `Slide.handlesMu` briefly (microseconds vs hundreds of microseconds of decode); contention is negligible.
- "Architectural placement of ported logic." The shared primitive lives in `internal/decoderhandle/`. NDPI keeps its instance ownership (per `strippedImage`); `formats/ndpi` does not gain a back-reference to `Slide`.
- "No cutting corners; no active users yet." Delete the v0.27 NDPI duplicate; do not leave parallel implementations.

---

## File Structure

**New files in opentile-go:**

```
internal/decoderhandle/handle.go         Pool type (factory, capacity, items
                                          chan, outstanding counter, closed
                                          flag). Borrow / Return / Close +
                                          sentinel errors.

internal/decoderhandle/handle_test.go    Pool unit tests using fake Decoder:
                                          sequential, concurrent, lazy
                                          creation, Close races, double-
                                          Close, no-factory. One real-codec
                                          smoke test.

slide_decoder_cache.go                   Slide.decoderFor(tag) accessor and
                                          the handlesMu/handles fields (kept
                                          in this file to keep slide.go
                                          focused). Wired from
                                          slide_decoded_tile.go and Slide.Close.

slide_handle_test.go                     Slide-level integration: reuse,
                                          per-codec separation,
                                          Close-releases, concurrent. Uses
                                          an instrumented test factory +
                                          a real-codec end-to-end test.

cmd/bench/svs/main.go                    SVS equivalent of bench-opentile.
                                          Iterates every L0 tile via
                                          Slide.DecodedTile. -in,
                                          -cpuprofile, -goroutines,
                                          -maxtiles flags.

cmd/bench/svs/README.md                  Build / run instructions; expected
                                          numbers; perf gate description.
```

**Modified files in opentile-go:**

```
opentile.go                              Extend Slide.Close: drain
                                          s.handles map (calling Close on
                                          each Pool) before delegating to
                                          s.r.Close.

slide.go                                 Add to Slide struct:
                                            handlesMu sync.Mutex
                                            handles   map[uint16]*decoderhandle.Pool
                                          (Lazy: nil until first decoderFor.)

slide_decoded_tile.go                    Replace per-call fac.New() /
                                          defer dec.Close() in both
                                          ImageDecodedTile and
                                          ImageDecodedTileInto slow paths
                                          with pool.Borrow() / defer
                                          pool.Return(dec). Type-assertion
                                          fast-path dispatch unchanged.

formats/ndpi/stripped.go                 Field type change:
                                            decHandle *decoderhandle.Pool
                                          (was *decoderHandle). Update
                                          ensureDecHandle to call
                                          decoderhandle.New. Update
                                          DecodedTile + decodedTileViaCrop
                                          + getOrLoad callback to use
                                          Borrow/Return.

cmd/bench/ndpi/main.go                   Add -goroutines N flag (default 1
                                          preserves v0.27 single-thread
                                          behavior). When N > 1, fan tile
                                          iteration to N goroutines sharing
                                          one *Slide.

Makefile                                 Bump MIN_NDPI_MPIXS from 130 to
                                          220. Add bench-ndpi-mt target
                                          (multi-thread NDPI; measurement
                                          only, no gate). Add bench-svs
                                          target (gated by MIN_SVS_MPIXS,
                                          value set during plan execution).
                                          Add bench-svs-mt target
                                          (measurement only).

CHANGELOG.md                             v0.28.0 entry: scope, sealed Qs
                                          summary, measured pre/post numbers.

CLAUDE.md                                Promote v0.28 to "Current
                                          milestone"; demote v0.27 to
                                          "Previous milestone" position.
```

**Deleted files in opentile-go:**

```
formats/ndpi/decoder_handle.go           Superseded by internal/decoderhandle.
formats/ndpi/decoder_handle_test.go      Tests subsumed by handle_test.go.
```

---

# Phase 1 — Build the shared primitive

## Task 1.1: Create `internal/decoderhandle/handle.go` with Pool + tests

**Files:**
- Create: `internal/decoderhandle/handle.go`
- Create: `internal/decoderhandle/handle_test.go`

- [ ] **Step 1: Create the work branch**

Run:
```bash
cd ~/GitHub/opentile-go && git checkout main && git pull && git checkout -b feat/v0.28
```
Expected: branch created at `90cec57` (v0.28 spec commit).

- [ ] **Step 2: Write failing tests for Pool**

Create `internal/decoderhandle/handle_test.go`:

```go
package decoderhandle_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
)

// fakeDecoder is an instrumented decoder.Decoder for Pool tests. Counts
// Decode and Close calls; never touches libjpeg-turbo.
type fakeDecoder struct {
	mu       sync.Mutex
	decoded  int
	closed   bool
	closeErr error
}

func (d *fakeDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("fakeDecoder: closed")
	}
	d.decoded++
	return &decoder.Image{
		Width: 1, Height: 1,
		Format: decoder.PixelFormatRGB,
		Stride: 3,
		Pix:    []byte{0, 0, 0},
	}, nil
}

func (d *fakeDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	return d.closeErr
}

// fakeFactory is an instrumented decoder.Factory. Counts New() calls;
// returns a fresh fakeDecoder each time.
type fakeFactory struct {
	news    atomic.Int32
	makeNil bool
}

func (f *fakeFactory) New() decoder.Decoder {
	f.news.Add(1)
	if f.makeNil {
		return nil
	}
	return &fakeDecoder{}
}

func (f *fakeFactory) CompressionTags() []uint16 { return []uint16{0xFFFF} }

func TestPoolSequentialReuse(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	defer p.Close()
	for i := 0; i < 10; i++ {
		d, err := p.Borrow()
		if err != nil {
			t.Fatalf("Borrow #%d: %v", i, err)
		}
		if _, err := d.Decode(nil, decoder.DecodeOptions{}); err != nil {
			t.Fatalf("Decode #%d: %v", i, err)
		}
		p.Return(d)
	}
	if got := fac.news.Load(); got != 1 {
		t.Fatalf("factory.New() called %d times; want 1 (single member reused)", got)
	}
}

func TestPoolConcurrentBounded(t *testing.T) {
	fac := &fakeFactory{}
	const cap = 4
	p := decoderhandle.New(fac, cap)
	defer p.Close()

	const N = 32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := p.Borrow()
			if err != nil {
				t.Errorf("Borrow: %v", err)
				return
			}
			defer p.Return(d)
			if _, err := d.Decode(nil, decoder.DecodeOptions{}); err != nil {
				t.Errorf("Decode: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := fac.news.Load(); got > cap {
		t.Fatalf("factory.New() called %d times; want <= %d (capacity)", got, cap)
	}
}

func TestPoolLazyCreation(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 8)
	defer p.Close()
	// Borrow 3 distinct concurrent members; never reaches capacity=8.
	var bs []decoder.Decoder
	for i := 0; i < 3; i++ {
		d, err := p.Borrow()
		if err != nil {
			t.Fatal(err)
		}
		bs = append(bs, d)
	}
	for _, d := range bs {
		p.Return(d)
	}
	if got := fac.news.Load(); got != 3 {
		t.Fatalf("factory.New() called %d times; want 3 (lazy)", got)
	}
}

func TestPoolBorrowAfterClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := p.Borrow()
	if !errors.Is(err, decoderhandle.ErrClosed) {
		t.Fatalf("got %v; want ErrClosed", err)
	}
}

func TestPoolReturnAfterClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	d, err := p.Borrow()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p.Return(d) // closed-pool branch: closes Decoder directly
	fd := d.(*fakeDecoder)
	if !fd.closed {
		t.Fatal("Decoder not Closed after Return-on-closed-pool")
	}
}

func TestPoolCloseRacesWithBorrow(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 1)
	d, err := p.Borrow()
	if err != nil {
		t.Fatal(err)
	}
	// One goroutine sits in Borrow waiting for capacity.
	got := make(chan error, 1)
	go func() {
		_, err := p.Borrow()
		got <- err
	}()
	// Give the goroutine time to block.
	time.Sleep(20 * time.Millisecond)
	// Close while a Borrow is in flight.
	p.Close()
	select {
	case err := <-got:
		if !errors.Is(err, decoderhandle.ErrClosed) {
			t.Fatalf("waiting Borrow got %v; want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Borrow did not return after Close")
	}
	p.Return(d) // closes Decoder directly
}

func TestPoolDoubleClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	if err := p.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestPoolFactoryReturnsNil(t *testing.T) {
	fac := &fakeFactory{makeNil: true}
	p := decoderhandle.New(fac, 2)
	defer p.Close()
	_, err := p.Borrow()
	if err == nil {
		t.Fatal("Borrow with nil-returning factory returned nil error")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/decoderhandle/ -v`
Expected: FAIL — `no Go files in internal/decoderhandle` or `undefined: decoderhandle.Pool`.

- [ ] **Step 4: Implement the Pool type**

Create `internal/decoderhandle/handle.go`:

```go
// Package decoderhandle provides a small fixed-size pool of long-lived
// decoder.Decoder instances. Replaces the fac.New() / dec.Close()
// per-tile pattern with Borrow/Return, eliminating per-tile tjInit +
// tjDestroy churn (~290 µs/call dominated by tjDestroy).
//
// Concurrency: Borrow blocks when all pool members are in use; Return
// is non-blocking. Pool members are not concurrent-safe (libjpeg-turbo
// tjhandle is single-threaded), so Borrow grants exclusive access for
// the lifetime of the borrow.
//
// Lifetime: members are lazy-initialised on first Borrow. Close drains
// the pool and tears down every member. After Close, Borrow returns
// ErrClosed; Return on a closed pool tears the member down directly.
package decoderhandle

import (
	"errors"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// ErrClosed is returned by Borrow if Close has been called.
var ErrClosed = errors.New("decoderhandle: pool closed")

// ErrFactoryReturnedNil is returned by Borrow if factory.New() returned
// nil. Surfaces what would otherwise be a nil-decoder panic in Decode.
var ErrFactoryReturnedNil = errors.New("decoderhandle: factory.New() returned nil")

// Pool is a fixed-size pool of decoder.Decoder instances.
type Pool struct {
	factory  decoder.Factory
	capacity int

	initMu      sync.Mutex // guards lazy-create + outstanding + closed
	outstanding int        // count of members issued and not yet returned
	items       chan decoder.Decoder
	closed      bool
}

// New constructs a pool of capacity members for the given factory.
// capacity must be > 0; smaller values are clamped to 1. Members are
// NOT created up-front; the first Borrow that needs to grow the pool
// invokes factory.New().
func New(fac decoder.Factory, capacity int) *Pool {
	if capacity < 1 {
		capacity = 1
	}
	return &Pool{
		factory:  fac,
		capacity: capacity,
		items:    make(chan decoder.Decoder, capacity),
	}
}

// Borrow acquires a Decoder. Blocks if all members are in use AND
// the pool is at capacity. Returns ErrClosed if Close has been called.
// Caller must call Return when done.
func (p *Pool) Borrow() (decoder.Decoder, error) {
	// Fast path: try to grab an existing returned member without
	// holding initMu.
	select {
	case d, ok := <-p.items:
		if !ok {
			return nil, ErrClosed
		}
		return d, nil
	default:
	}

	// Slow path: try to lazy-create under initMu.
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		return nil, ErrClosed
	}
	if p.outstanding < p.capacity {
		d := p.factory.New()
		if d == nil {
			p.initMu.Unlock()
			return nil, ErrFactoryReturnedNil
		}
		p.outstanding++
		p.initMu.Unlock()
		return d, nil
	}
	p.initMu.Unlock()

	// At capacity, all members busy — block waiting for Return.
	// We must NOT hold initMu while blocking; another goroutine's
	// Return needs to be able to write to p.items, and Close needs
	// to be able to close it.
	d, ok := <-p.items
	if !ok {
		return nil, ErrClosed
	}
	return d, nil
}

// Return puts the Decoder back into the pool. Safe after Close
// (closes the Decoder directly in that case). Safe with d == nil.
func (p *Pool) Return(d decoder.Decoder) {
	if d == nil {
		return
	}
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		_ = d.Close()
		return
	}
	p.initMu.Unlock()

	select {
	case p.items <- d:
		// Returned to pool. outstanding stays incremented; the member
		// is now "available" but still counted against capacity. The
		// next Borrow that picks it up doesn't increment outstanding
		// (it's a hit on the channel, not a new factory.New()).
	default:
		// Pool channel full — should not happen if Borrow/Return are
		// balanced and capacity is respected. Close defensively.
		_ = d.Close()
		p.initMu.Lock()
		p.outstanding--
		p.initMu.Unlock()
	}
}

// Close drains and closes every member. Safe to call multiple times.
// In-flight Borrows blocked on the channel see channel-closed and
// return ErrClosed. Returns the first Decoder.Close error encountered
// during drain.
func (p *Pool) Close() error {
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		return nil
	}
	p.closed = true
	close(p.items)
	p.initMu.Unlock()

	var firstErr error
	for d := range p.items {
		if err := d.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race -count=3 ./internal/decoderhandle/ -v`
Expected: all 8 tests PASS across 3 iterations under `-race`, no data race detected.

If `TestPoolCloseRacesWithBorrow` flakes (timing-dependent): bump the `time.Sleep` from 20ms to 50ms. Document the bump with a comment.

- [ ] **Step 6: Commit**

```bash
git add internal/decoderhandle/
git commit -m "feat(internal/decoderhandle): bounded Pool of long-lived Decoders"
```

**Checkpoint:** Phase 1 complete. Pool primitive is committed and tested in isolation. No other code touched yet.

---

# Phase 2 — Migrate NDPI to use the shared primitive

## Task 2.1: Retype `strippedImage.decHandle` and update fast-path call sites

**Files:**
- Modify: `formats/ndpi/stripped.go`

- [ ] **Step 1: Inspect the v0.27 strippedImage field + fast-path calls**

Run:
```bash
grep -n "decHandle\|ensureDecHandle\|decoderHandle" formats/ndpi/stripped.go
```
Confirm: field type is `*decoderHandle`; `ensureDecHandle` calls `newDecoderHandle`; `DecodedTile` / `decodedTileViaCrop` / pixelCache's load callback all call `l.decHandle.Decode(...)`.

- [ ] **Step 2: Update field type and ensureDecHandle**

Edit `formats/ndpi/stripped.go` imports — add `"github.com/wsilabs/opentile-go/internal/decoderhandle"`.

Find the `strippedImage` struct (around line 75-79 inside the type block) and change:

```go
	decHandle     *decoderHandle
	decHandleOnce sync.Once
```

to:

```go
	decHandle     *decoderhandle.Pool
	decHandleOnce sync.Once
```

Find `ensureDecHandle` and replace its body:

```go
func (l *strippedImage) ensureDecHandle() {
	l.decHandleOnce.Do(func() {
		tag := opentile.CompressionToTIFFTag(l.compression)
		fac, ok := decoder.GetByCompressionTag(tag)
		if !ok {
			return
		}
		capacity := runtime.NumCPU()
		if capacity > 8 {
			capacity = 8
		}
		l.decHandle = decoderhandle.New(fac, capacity)
	})
}
```

- [ ] **Step 3: Update fast-path call sites to use Borrow/Return**

Find the `pixelCache.getOrLoad` load callback inside `DecodedTile` (~the `func() (*decoder.Image, error) { ... }` block). Currently:

```go
pixFrame, err := l.pixelCache.getOrLoad(key, func() (*decoder.Image, error) {
	jpegFrame, err := l.getFrame(framePos, frameSize)
	if err != nil {
		return nil, err
	}
	l.ensureDecHandle()
	return l.decHandle.Decode(jpegFrame, decoder.DecodeOptions{
		Format: decoder.PixelFormatRGB,
	})
})
```

Replace with:

```go
pixFrame, err := l.pixelCache.getOrLoad(key, func() (*decoder.Image, error) {
	jpegFrame, err := l.getFrame(framePos, frameSize)
	if err != nil {
		return nil, err
	}
	l.ensureDecHandle()
	if l.decHandle == nil {
		return nil, fmt.Errorf("ndpi: no decoder registered for %s", l.compression)
	}
	dec, err := l.decHandle.Borrow()
	if err != nil {
		return nil, err
	}
	defer l.decHandle.Return(dec)
	return dec.Decode(jpegFrame, decoder.DecodeOptions{
		Format: decoder.PixelFormatRGB,
	})
})
```

Find `decodedTileViaCrop`. Currently:

```go
func (l *strippedImage) decodedTileViaCrop(tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	jpegBytes, err := l.Tile(tx, ty)
	if err != nil {
		return nil, err
	}
	l.ensureDecHandle()
	out, err := l.decHandle.Decode(jpegBytes, opts)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}
	return out, nil
}
```

Replace with:

```go
func (l *strippedImage) decodedTileViaCrop(tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	jpegBytes, err := l.Tile(tx, ty)
	if err != nil {
		return nil, err
	}
	l.ensureDecHandle()
	if l.decHandle == nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty,
			Err: fmt.Errorf("ndpi: no decoder registered for %s", l.compression)}
	}
	dec, err := l.decHandle.Borrow()
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}
	defer l.decHandle.Return(dec)
	out, err := dec.Decode(jpegBytes, opts)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}
	return out, nil
}
```

Find `closeResources`. It still calls `l.decHandle.Close()`, which now refers to `*decoderhandle.Pool.Close()` — same method name, same semantics. Verify the body is still:

```go
func (l *strippedImage) closeResources() error {
	if l.decHandle == nil {
		return nil
	}
	return l.decHandle.Close()
}
```

No edit needed; the existing code compiles against the new type.

- [ ] **Step 4: Verify the file still compiles**

Run: `go build ./formats/ndpi/...`
Expected: error — `undefined: decoderHandle`, `undefined: newDecoderHandle`, or similar, because the old file still exists and is now stale. We'll delete it in the next step.

If build succeeds: that means stale type/function references in the rest of the package; investigate before deleting.

- [ ] **Step 5: Commit (partial — file deletion follows in Task 2.2)**

```bash
git add formats/ndpi/stripped.go
git commit -m "feat(ndpi): retype strippedImage.decHandle to internal/decoderhandle.Pool"
```

Note: the working tree will NOT build at this commit (the v0.27 decoder_handle.go still defines the old type). Task 2.2 removes it; the build is restored at the end of Task 2.2.

## Task 2.2: Delete the v0.27 NDPI decoder_handle files

**Files:**
- Delete: `formats/ndpi/decoder_handle.go`
- Delete: `formats/ndpi/decoder_handle_test.go`

- [ ] **Step 1: Delete the v0.27 files**

Run:
```bash
git rm formats/ndpi/decoder_handle.go formats/ndpi/decoder_handle_test.go
```

- [ ] **Step 2: Build and verify no other references**

Run: `go build ./...`
Expected: clean build. If any reference to `decoderHandle` (lowercase d, the old type) remains, surface them with:
```bash
grep -rn "decoderHandle\b" formats/ndpi/
```
Expected: zero hits (only the new type `*decoderhandle.Pool` appears).

- [ ] **Step 3: Run all NDPI tests under -race**

Run:
```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=1 ./formats/ndpi/...
```
Expected: all tests PASS. The v0.27 test suite (`stripped_pixel_parity_smoke_test.go`, `stripped_decodedtile_test.go`, `pixel_cache_test.go`) is the regression witness; its pass confirms the migration preserves semantics.

If `TestNDPIFastPathConcurrent` (the v0.27 32-goroutine fanout test) regresses: investigate the lock-order discipline in `DecodedTile` — `pixelCache.mu` should release before `Borrow` runs (it does, via `getOrLoad`'s promise pattern).

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(ndpi): delete v0.27 decoder_handle, superseded by internal/decoderhandle"
```

**Checkpoint:** Phase 2 complete. NDPI uses the shared primitive; the v0.27 fast-path tests pass. The single `*Pool` instance per `strippedImage` now supports concurrent Borrows up to `min(NumCPU, 8)` — multi-thread `ScaledStrips` callers will see the parallelism win at this point even before the Slide-level cache lands.

---

# Phase 3 — Wire Slide-level pool cache for the slow path

## Task 3.1: Add fields to Slide + new `slide_decoder_cache.go`

**Files:**
- Modify: `slide.go` (add struct fields)
- Modify: `opentile.go` (extend Close to drain pools)
- Create: `slide_decoder_cache.go`

- [ ] **Step 1: Inspect Slide struct + existing Close**

Run:
```bash
grep -n "type Slide struct\|func (s \*Slide) Close\|s\.r\.Close" slide.go opentile.go
```
Note the existing `Slide.r slideReader` field and where `Slide.Close` lives.

- [ ] **Step 2: Add fields to Slide struct**

Edit `slide.go`. Find the `type Slide struct {` block (around line 49). Replace:

```go
type Slide struct {
	r slideReader
}
```

with:

```go
type Slide struct {
	r slideReader

	// v0.28: per-codec decoder pool cache. Lazy: nil until first
	// decoderFor() call. Drained by Close. Keyed by TIFF compression
	// tag (the same tag space CompressionToTIFFTag emits).
	handlesMu sync.Mutex
	handles   map[uint16]*decoderhandle.Pool
}
```

Add imports to `slide.go`:

```go
import (
	"sync"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
	// ... existing imports ...
)
```

- [ ] **Step 3: Add Slide.Close drain**

Find `func (s *Slide) Close` (likely in `opentile.go` or `slide.go`). The v0.27 form delegates to `s.r.Close()`. Replace with:

```go
// Close releases the Slide's resources: drains every cached decoder
// pool, then delegates to the underlying reader (which closes the
// mmap or file handle and tears down format-specific state).
//
// v0.27 contract: Close must not race with in-flight tile reads. v0.28
// preserves that. A racing Borrow gets ErrClosed; a racing Decode
// completes against an already-borrowed Decoder, then Return closes
// it directly via the pool's closed-pool branch.
func (s *Slide) Close() error {
	s.handlesMu.Lock()
	handles := s.handles
	s.handles = nil
	s.handlesMu.Unlock()

	var firstErr error
	for _, pool := range handles {
		if err := pool.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.r.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
```

If the v0.27 Close is in `opentile.go`, edit there; otherwise edit in `slide.go`.

- [ ] **Step 4: Create the decoderFor accessor**

Create `slide_decoder_cache.go`:

```go
package opentile

import (
	"fmt"
	"runtime"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
)

// decoderPoolCapacity is the per-(Slide, codec) pool size:
// min(NumCPU, 8). The 8 cap bounds memory at ~16 MB/codec/Slide
// (8 × libjpeg-turbo work buffer ≈ 2 MB) while still giving
// good multi-core throughput; cgo decode is intrinsically
// blocking, so concurrency benefit plateaus around 4-8 workers.
func decoderPoolCapacity() int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	return n
}

// decoderFor returns (and lazily creates) the decoder pool for the
// given TIFF compression tag. Pools are cached on the Slide; each is
// torn down by Slide.Close.
//
// Returns ErrCodecNotRegistered (wrapped with the tag) if no factory
// is registered for the compression.
//
// Added in v0.28.
func (s *Slide) decoderFor(tag uint16) (*decoderhandle.Pool, error) {
	s.handlesMu.Lock()
	defer s.handlesMu.Unlock()
	if pool, ok := s.handles[tag]; ok {
		return pool, nil
	}
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("%w: tag %d (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, tag)
	}
	pool := decoderhandle.New(fac, decoderPoolCapacity())
	if s.handles == nil {
		s.handles = make(map[uint16]*decoderhandle.Pool)
	}
	s.handles[tag] = pool
	return pool, nil
}
```

- [ ] **Step 5: Build + run existing tests (no behavior change yet)**

Run:
```bash
go build ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test -race -count=1 -short ./...
```
Expected: clean build, all existing tests still pass. The new `decoderFor` is unused at this commit; it's wired up in Task 3.2.

- [ ] **Step 6: Commit**

```bash
git add slide.go opentile.go slide_decoder_cache.go
git commit -m "feat(slide): add per-codec decoder pool cache + drain in Close"
```

## Task 3.2: Update Slide.ImageDecodedTile slow path to use Borrow/Return

**Files:**
- Modify: `slide_decoded_tile.go`

- [ ] **Step 1: Re-read the v0.27 slow paths**

Run:
```bash
sed -n '36,135p' slide_decoded_tile.go
```
Note: `ImageDecodedTile` and `ImageDecodedTileInto` each have a slow-path branch that does:
```go
dec := fac.New()
defer dec.Close()
return dec.Decode(...)
```
Both fall through here if the v0.27 `s.r.(decodedTiler)` fast-path returned `fastpath.ErrUnsupported`. Both need updating.

- [ ] **Step 2: Update the ImageDecodedTile slow path**

Find:

```go
	tag := CompressionToTIFFTag(lvl.Compression)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("%w: %s (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, lvl.Compression)
	}
	dec := fac.New()
	defer dec.Close()
	return dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
	})
}
```

Replace with:

```go
	tag := CompressionToTIFFTag(lvl.Compression)
	pool, err := s.decoderFor(tag)
	if err != nil {
		return nil, err
	}
	dec, err := pool.Borrow()
	if err != nil {
		return nil, err
	}
	defer pool.Return(dec)
	return dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
	})
}
```

- [ ] **Step 3: Update the ImageDecodedTileInto slow path**

Find the parallel block in `ImageDecodedTileInto` (the `_, err = dec.Decode(...)` block). Replace `fac.New() / defer dec.Close()` with the Borrow/Return pattern:

```go
	tag := CompressionToTIFFTag(lvl.Compression)
	pool, err := s.decoderFor(tag)
	if err != nil {
		return err
	}
	dec, err := pool.Borrow()
	if err != nil {
		return err
	}
	defer pool.Return(dec)
	_, err = dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
		Dst:    dst,
	})
	return err
}
```

- [ ] **Step 4: Build + run end-to-end NDPI tests**

Run:
```bash
go build ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=1 -run 'TestNDPI' ./formats/ndpi/ ./tests/
```
Expected: all NDPI tests pass — both fast-path (v0.27, untouched) and slow-path (now Borrow/Return-based, NDPI's oneframe-level + edge tiles).

- [ ] **Step 5: Commit**

```bash
git add slide_decoded_tile.go
git commit -m "feat(slide): replace per-tile fac.New()/Close() with pool.Borrow()/Return()"
```

## Task 3.3: Slide-level integration tests

**Files:**
- Create: `slide_handle_test.go`

- [ ] **Step 1: Write the tests**

Create `slide_handle_test.go`:

```go
package opentile

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// testFakeDecoder + testFakeFactory: instrumented decoder.Decoder/Factory
// for Slide-level pool tests. Counts New / Decode / Close. Registers
// under a sentinel TIFF tag (0xFFEE) so it doesn't collide with real
// codecs. Tests construct a synthetic Slide via the test-only reader
// pattern.

const testCodecTag uint16 = 0xFFEE

type testFakeDecoder struct {
	mu       sync.Mutex
	decoded  int
	closed   bool
}

func (d *testFakeDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("testFakeDecoder: closed")
	}
	d.decoded++
	w, h := 1, 1
	return &decoder.Image{
		Width: w, Height: h, Format: decoder.PixelFormatRGB,
		Stride: w * 3, Pix: make([]byte, w*h*3),
	}, nil
}

func (d *testFakeDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

type testFakeFactory struct {
	news    atomic.Int32
	last    atomic.Pointer[testFakeDecoder]
	decoded atomic.Int32
}

func (f *testFakeFactory) New() decoder.Decoder {
	f.news.Add(1)
	d := &testFakeDecoder{}
	f.last.Store(d)
	return d
}

func (f *testFakeFactory) CompressionTags() []uint16 { return []uint16{testCodecTag} }

// minimalReader implements slideReader with just enough to drive
// ImageDecodedTile's slow path: Level + ImageRawTile.
type minimalReader struct {
	levelSize    Size
	tileSize     Size
	compression  Compression
	rawCallCount atomic.Int32
}

func (r *minimalReader) Format() Format    { return Format("test") }
func (r *minimalReader) Images() []Image   { return []Image{{Levels: []Level{{Index: 0, Size: r.levelSize, TileSize: r.tileSize, Compression: r.compression}}}} }
func (r *minimalReader) Level(image, level int) (Level, error) {
	return Level{Index: 0, Size: r.levelSize, TileSize: r.tileSize, Compression: r.compression}, nil
}
func (r *minimalReader) Associated() []AssociatedImage { return nil }
func (r *minimalReader) Metadata() Metadata            { return Metadata{} }
func (r *minimalReader) ICCProfile() []byte            { return nil }
func (r *minimalReader) WarmLevel(image, level int) error { return nil }
func (r *minimalReader) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	r.rawCallCount.Add(1)
	return []byte{0xFF}, nil // fake decoder ignores content
}
func (r *minimalReader) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	r.rawCallCount.Add(1)
	if len(dst) < 1 {
		return 0, errors.New("dst too small")
	}
	dst[0] = 0xFF
	return 1, nil
}
func (r *minimalReader) ImageTileMaxSize(image, level int) int    { return 1 }
func (r *minimalReader) ImageTilePrefix(image, level int) []byte  { return nil }
func (r *minimalReader) ImageTileBodyMaxSize(image, level int) int { return 1 }
func (r *minimalReader) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return r.ImageRawTileInto(image, level, tx, ty, dst)
}
func (r *minimalReader) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return nil, errors.New("not implemented for tests")
}
func (r *minimalReader) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[TilePos, TileResult] {
	return func(yield func(TilePos, TileResult) bool) {}
}
func (r *minimalReader) Close() error { return nil }

func setupTestCodec(t *testing.T, fac *testFakeFactory, comp Compression) {
	t.Helper()
	decoder.RegisterFactory(testCodecTag, fac)
	// Ensure CompressionToTIFFTag(comp) returns testCodecTag.
	// Real codec registration uses init(); we rely on the test
	// compression value mapping to testCodecTag via an explicit
	// registration. If the registry doesn't support deregistration
	// in tests, the registered factory persists across tests but
	// each test uses its own fac instance pointer — the registry
	// returns the most recent one.
}

// To make CompressionToTIFFTag(testCompression) == testCodecTag,
// we need a synthetic Compression value. Use Compression(0xFFEE) as
// the literal opentile.Compression to bypass the existing registry.

const testCompression Compression = Compression(testCodecTag)

func TestSlideDecoderHandleReuse(t *testing.T) {
	fac := &testFakeFactory{}
	setupTestCodec(t, fac, testCompression)
	s := &Slide{r: &minimalReader{
		levelSize:   Size{W: 100, H: 100},
		tileSize:    Size{W: 1, H: 1},
		compression: testCompression,
	}}
	defer s.Close()
	for i := 0; i < 100; i++ {
		_, err := s.DecodedTile(0, 0, 0)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := fac.news.Load(); got != 1 {
		t.Fatalf("factory.New() called %d times; want 1 (single member reused across 100 sequential calls)", got)
	}
}

func TestSlideCloseReleasesHandles(t *testing.T) {
	fac := &testFakeFactory{}
	setupTestCodec(t, fac, testCompression)
	s := &Slide{r: &minimalReader{
		levelSize:   Size{W: 100, H: 100},
		tileSize:    Size{W: 1, H: 1},
		compression: testCompression,
	}}
	_, err := s.DecodedTile(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d := fac.last.Load(); d == nil {
		t.Fatal("no decoder created")
	} else if !d.closed {
		t.Fatal("decoder not closed after Slide.Close")
	}
}

func TestSlideHandleConcurrent(t *testing.T) {
	fac := &testFakeFactory{}
	setupTestCodec(t, fac, testCompression)
	s := &Slide{r: &minimalReader{
		levelSize:   Size{W: 100, H: 100},
		tileSize:    Size{W: 1, H: 1},
		compression: testCompression,
	}}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, err := s.DecodedTile(0, 0, 0)
				if err != nil {
					t.Errorf("decode: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	cap := decoderPoolCapacity()
	if got := fac.news.Load(); got > int32(cap) {
		t.Fatalf("factory.New() called %d times; want <= %d (pool capacity)", got, cap)
	}
}
```

> **Note on `setupTestCodec`:** the `decoder` package's registry API may not expose `RegisterFactory` as a public symbol. If it doesn't, do one of:
>   - (a) Use blank-import of an existing codec (e.g. `decoder/jpeg`) and a real JPEG byte fixture as `compressed`. The real decoder counts can be measured via instrumentation in the test.
>   - (b) Add a test-only `RegisterFactoryForTest` helper in `decoder/decoder_test_export.go` (export_test.go pattern).
>   - (c) Construct the Slide via the test-only path used by `slide_best_level_test.go:38` (search for that file to see the pattern).
>
> Pick whichever path the existing test infrastructure supports. If (a), the test instrumentation switches from "fake decoder counts" to "wall-time / cpuprofile shape comparison" — slightly weaker but still meaningful (pre-v0.28 shows tjDestroy in hot spots; post-v0.28 does not). The plan executor should choose during implementation; report which option was used.

- [ ] **Step 2: Adjust setup helper to match the decoder registry's actual surface**

Run:
```bash
grep -n "RegisterFactory\|GetByCompressionTag\|func.*Register" decoder/*.go
```
Confirm the registration surface. Adjust `setupTestCodec` accordingly. If the registry only supports init-time registration (no test helper), add an `export_test.go` in package decoder that re-exports the registry's internal append function for test use, or use a real codec + JPEG fixture path per the note above.

- [ ] **Step 3: Run tests**

Run:
```bash
go test -race -count=2 -run 'TestSlideDecoderHandleReuse|TestSlideCloseReleasesHandles|TestSlideHandleConcurrent' .
```
Expected: all 3 tests pass.

- [ ] **Step 4: Commit**

```bash
git add slide_handle_test.go decoder/export_test.go  # if (b) used
git commit -m "test(slide): pool reuse, Close-releases, and concurrent fanout integration tests"
```

**Checkpoint:** Phase 3 complete. Slide-level cache lands. The cross-format win is now wired. Halt for controller review before Phase 4.

---

# Phase 4 — Bench infrastructure

## Task 4.1: Extend cmd/bench/ndpi with `-goroutines` flag

**Files:**
- Modify: `cmd/bench/ndpi/main.go`

- [ ] **Step 1: Re-read existing bench main**

Run: `cat cmd/bench/ndpi/main.go`
Note the existing flags (`-in`, `-cpuprofile`, `-memprofile`, `-maxtiles`) and the single-loop tile iteration around line 50-75.

- [ ] **Step 2: Add the -goroutines flag and parallel iteration**

Edit `cmd/bench/ndpi/main.go`. Add to the flag block:

```go
goroutines := flag.Int("goroutines", 1, "number of goroutines fanning out tile reads (1 = sequential, current v0.27 behavior)")
```

Refactor the tile loop to use `*goroutines` workers when > 1:

```go
if *goroutines <= 1 {
	// Existing single-thread loop (preserve v0.27 semantics).
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			// ... existing body ...
		}
	}
} else {
	type tilePos struct{ tx, ty, w, h int }
	jobs := make(chan tilePos, *goroutines*4)
	var wg sync.WaitGroup
	var pixAtomic int64
	var nAtomic int64
	for i := 0; i < *goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				img, err := slide.ReadRegion(0, j.tx, j.ty, j.w, j.h)
				if err != nil {
					panic(err)
				}
				_ = img
				atomic.AddInt64(&pixAtomic, int64(j.w*j.h))
				atomic.AddInt64(&nAtomic, 1)
			}
		}()
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			x := c * TS
			y := r * TS
			tw := TS
			if x+tw > w { tw = w - x }
			th := TS
			if y+th > h { th = h - y }
			jobs <- tilePos{x, y, tw, th}
			if *maxTiles > 0 && nAtomic+int64(len(jobs)) >= int64(*maxTiles) { goto submitDone }
		}
	}
submitDone:
	close(jobs)
	wg.Wait()
	pix = pixAtomic
	nTiles = int(nAtomic)
}
```

Add imports: `sync`, `sync/atomic`.

> **Note:** The single-thread branch must remain bit-identical to v0.27's loop so `make bench-ndpi` continues to produce comparable numbers. Only the multi-thread branch is new.

- [ ] **Step 3: Test single-thread (regression of v0.27 behavior)**

Run:
```bash
go build -o /tmp/bench-opentile ./cmd/bench/ndpi/
/tmp/bench-opentile -in sample_files/ndpi/CMU-1.ndpi
```
Expected: ~8s, ~240 Mpix/s. Same shape as v0.27.

- [ ] **Step 4: Test multi-thread**

Run:
```bash
/tmp/bench-opentile -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu 2>/dev/null || nproc)
```
Expected: significantly faster wall time (target ~1.3-2.5s, target throughput ~1500-2000 Mpix/s). If wall time is unchanged from single-thread, the pool isn't unlocking parallelism — investigate before proceeding.

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/ndpi/main.go
git commit -m "feat(cmd/bench/ndpi): add -goroutines flag for multi-thread bench variant"
```

## Task 4.2: Create cmd/bench/svs

**Files:**
- Create: `cmd/bench/svs/main.go`
- Create: `cmd/bench/svs/README.md`

- [ ] **Step 1: Create the SVS bench main**

Create `cmd/bench/svs/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
	path := flag.String("in", "", "slide path")
	cpuProf := flag.String("cpuprofile", "", "write cpu profile to file")
	maxTiles := flag.Int("maxtiles", 0, "stop after N tiles (0 = all)")
	goroutines := flag.Int("goroutines", 1, "number of goroutines fanning out tile reads")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing -in")
		os.Exit(2)
	}
	slide, err := opentile.OpenFile(*path)
	if err != nil {
		panic(err)
	}
	defer slide.Close()
	l0 := slide.Levels()[0]
	w, h := l0.Size.W, l0.Size.H
	fmt.Printf("source L0: %dx%d  TileSize=%v Grid=%v\n", w, h, l0.TileSize, l0.Grid)

	if *cpuProf != "" {
		f, err := os.Create(*cpuProf)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}

	start := time.Now()
	var pixTotal atomic.Int64
	var nTotal atomic.Int64

	if *goroutines <= 1 {
		for ty := 0; ty < l0.Grid.H; ty++ {
			for tx := 0; tx < l0.Grid.W; tx++ {
				img, err := slide.DecodedTile(0, tx, ty)
				if err != nil {
					panic(err)
				}
				pixTotal.Add(int64(img.Width * img.Height))
				if n := nTotal.Add(1); *maxTiles > 0 && int(n) >= *maxTiles {
					goto done
				}
			}
		}
	} else {
		type tp struct{ tx, ty int }
		jobs := make(chan tp, *goroutines*4)
		var wg sync.WaitGroup
		for i := 0; i < *goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					img, err := slide.DecodedTile(0, j.tx, j.ty)
					if err != nil {
						panic(err)
					}
					pixTotal.Add(int64(img.Width * img.Height))
					nTotal.Add(1)
				}
			}()
		}
		for ty := 0; ty < l0.Grid.H; ty++ {
			for tx := 0; tx < l0.Grid.W; tx++ {
				jobs <- tp{tx, ty}
				if *maxTiles > 0 && int(nTotal.Load())+len(jobs) >= *maxTiles {
					close(jobs)
					goto waitDone
				}
			}
		}
		close(jobs)
	waitDone:
		wg.Wait()
	}

done:
	el := time.Since(start).Seconds()
	pix := pixTotal.Load()
	n := nTotal.Load()
	fmt.Printf("%d tiles, %d MiB pixels in %.2f s (%.1f Mpix/s, %.1f MiB/s)\n",
		n, pix*3>>20, el, float64(pix)/el/1e6, float64(pix)*3/el/1024/1024)
}
```

- [ ] **Step 2: Build and run on CMU-1.svs**

Run:
```bash
go build -o /tmp/bench-svs ./cmd/bench/svs/
/tmp/bench-svs -in sample_files/svs/CMU-1.svs
```
Expected: throughput report with non-zero numbers. Record the value — it becomes the gate baseline.

- [ ] **Step 3: Run multi-thread**

Run:
```bash
/tmp/bench-svs -in sample_files/svs/CMU-1.svs -goroutines $(sysctl -n hw.ncpu 2>/dev/null || nproc)
```
Expected: higher aggregate throughput than single-thread (should be 3-8× on multi-core).

- [ ] **Step 4: Create the README**

Create `cmd/bench/svs/README.md`:

```markdown
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

# Multi-thread (measurement)
/tmp/bench-svs -in sample_files/svs/CMU-1.svs -goroutines $(nproc)

# With CPU profile
/tmp/bench-svs -in sample_files/svs/CMU-1.svs -cpuprofile /tmp/cpu.prof
go tool pprof -top /tmp/cpu.prof
```

## Expected numbers

Apple Silicon (13 cores), CMU-1.svs (~1.9 MB, small fixture):

| Build              | Mode           | Throughput (approx) |
|---|---|---|
| v0.27 baseline     | single-thread  | TBD post-Task 4.4   |
| v0.28              | single-thread  | ~15% above v0.27    |
| v0.28              | multi-thread   | ~6-8× single-thread |

The Makefile `bench-svs` target enforces a single-thread floor
(MIN_SVS_MPIXS, set in Task 4.4). bench-svs-mt is measurement only.
```

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/svs/
git commit -m "feat(cmd/bench/svs): SVS tile-decode bench (single + multi-thread)"
```

## Task 4.3: Update Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Bump MIN_NDPI_MPIXS to 220**

Edit `Makefile`. Find:

```make
MIN_NDPI_MPIXS ?= 130
```

Change to:

```make
MIN_NDPI_MPIXS ?= 220
```

- [ ] **Step 2: Add bench-ndpi-mt target**

Append to `Makefile`:

```make
# Multi-thread NDPI bench. Measurement only — no gate. Pre-v0.28
# capped at ~single-thread (mutex bottleneck); post-v0.28 scales to
# ~6-8× single-thread via the decoder-handle pool.
bench-ndpi-mt:
	@if [ ! -f "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
	@/tmp/bench-opentile-ndpi -in "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" -goroutines $$(sysctl -n hw.ncpu 2>/dev/null || nproc)

.PHONY: bench-ndpi-mt
```

- [ ] **Step 3: Add bench-svs target (gate value placeholder)**

Append to `Makefile`:

```make
# SVS single-thread tile-decode bench. v0.28 hard gate for the
# cross-format pool's measured deliverable. MIN_SVS_MPIXS gate value
# is set after baseline measurement (Task 4.4).
MIN_SVS_MPIXS ?= 0

bench-svs:
	@if [ ! -f "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/svs/CMU-1.svs"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
	@result=$$(/tmp/bench-opentile-svs -in "$(OPENTILE_TESTDIR)/svs/CMU-1.svs"); \
	echo "$$result"; \
	if [ "$(MIN_SVS_MPIXS)" = "0" ]; then \
		echo "(no gate yet — set MIN_SVS_MPIXS once baseline is measured)"; \
		exit 0; \
	fi; \
	mpps=$$(echo "$$result" | tail -1 | sed -E 's/.* \(([0-9.]+) Mpix\/s.*/\1/'); \
	awk -v got="$$mpps" -v min="$(MIN_SVS_MPIXS)" 'BEGIN { \
		if (got+0 < min+0) { \
			printf "FAIL: %.1f Mpix/s < %.1f Mpix/s threshold\n", got, min; \
			exit 1; \
		} else { \
			printf "PASS: %.1f Mpix/s >= %.1f Mpix/s threshold\n", got, min; \
		} \
	}'

.PHONY: bench-svs

# Multi-thread SVS bench. Measurement only — no gate.
bench-svs-mt:
	@if [ ! -f "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/svs/CMU-1.svs"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
	@/tmp/bench-opentile-svs -in "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" -goroutines $$(sysctl -n hw.ncpu 2>/dev/null || nproc)

.PHONY: bench-svs-mt
```

- [ ] **Step 4: Verify each target runs**

Run:
```bash
make bench-ndpi
make bench-ndpi-mt
make bench-svs
make bench-svs-mt
```
Expected:
- `bench-ndpi`: PASS at ≥220 Mpix/s
- `bench-ndpi-mt`: prints multi-thread number (no gate)
- `bench-svs`: prints throughput + "no gate yet" message (gate set in Task 4.4)
- `bench-svs-mt`: prints multi-thread number

If `bench-ndpi` fails: the gate-tighten exposed a regression. Investigate via profile before continuing.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "feat(make): tighten bench-ndpi to 220 Mpix/s; add bench-ndpi-mt + bench-svs + bench-svs-mt"
```

## Task 4.4: Measure SVS baseline and set MIN_SVS_MPIXS

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Run bench-svs three times, take the lowest**

Run:
```bash
for i in 1 2 3; do make bench-svs; done 2>&1 | grep Mpix
```
Record the lowest single-thread throughput. Call it `M_svs`.

- [ ] **Step 2: Set MIN_SVS_MPIXS to floor(M_svs * 0.95)**

Edit `Makefile`. Find:

```make
MIN_SVS_MPIXS ?= 0
```

Change to (substituting the integer floor of M_svs × 0.95):

```make
MIN_SVS_MPIXS ?= <computed>
```

E.g. if M_svs = 280 Mpix/s, set to 266. Use an integer.

- [ ] **Step 3: Verify gate passes**

Run: `make bench-svs`
Expected: `PASS: <M_svs> Mpix/s >= <MIN_SVS_MPIXS> Mpix/s threshold`.

- [ ] **Step 4: Record numbers for CHANGELOG (Task 5.1)**

Note down:
- bench-ndpi single-thread Mpix/s
- bench-ndpi-mt multi-thread Mpix/s
- bench-svs single-thread Mpix/s
- bench-svs-mt multi-thread Mpix/s

These go into Task 5.1's CHANGELOG entry.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "feat(make): set MIN_SVS_MPIXS gate from measured v0.28 baseline"
```

**Checkpoint:** Phase 4 complete. All benches green; all gates set. Halt for controller review of v0.28's measured numbers before Phase 5.

---

# Phase 5 — Documentation

## Task 5.1: CHANGELOG v0.28.0 entry

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add the v0.28.0 entry**

Edit `CHANGELOG.md`. Find:

```markdown
## [Unreleased]

## [0.27.0] — 2026-05-28
```

Replace with (substituting actual measured numbers from Task 4.4):

```markdown
## [Unreleased]

## [0.28.0] — 2026-05-29

Cross-format decoder-handle pool. Eliminates per-tile
`decoder.Factory.New() / dec.Close()` churn across every format
routing through `Slide.ImageDecodedTile`. Introduces a fixed-size
pool of long-lived `decoder.Decoder` instances per `(Slide, codec)`,
sized `min(NumCPU, 8)`. NDPI's v0.27 per-`strippedImage` handle
migrates to the same shared primitive, gaining multi-core parallelism
on `Slide.DecodedTile` calls.

### Measured throughput (Apple Silicon, single-thread unless noted)

| Bench               | v0.27        | v0.28          |
|---|---|---|
| bench-ndpi          | 243.1 Mpix/s | <fill in>     |
| bench-ndpi-mt       | ~245 (capped) | <fill in>     |
| bench-svs           | <pre-v0.28>  | <post-v0.28>  |
| bench-svs-mt        | <pre-v0.28>  | <post-v0.28>  |

CPU profile: `tjDestroy` (240 µs/call in v0.27) and `tjInit` (~50
µs/call) are eliminated from the per-tile loop. The dominant
post-v0.28 hot spots remain `tjDecompress2` (the actual decode) and
pixel memmove (the v0.27 blit).

### Added (internal only)

- `internal/decoderhandle.Pool` — fixed-size pool of long-lived
  decoder.Decoder instances. Lazy member creation; mutex-guarded
  outstanding counter; channel-based borrow/return semantics.
  Replaces v0.27's `formats/ndpi.decoderHandle`.
- `(*Slide).decoderFor(tag uint16)` (unexported) — Slide-level pool
  cache accessor; lazy per-codec creation under `Slide.handlesMu`.
- `cmd/bench/svs/` — single-thread + multi-thread SVS tile-decode
  benchmark, plus README.

### Changed

- `(*Slide).ImageDecodedTile` and `(*Slide).ImageDecodedTileInto`
  slow paths replaced `fac.New() / dec.Close()` with
  `pool.Borrow() / pool.Return()`. NDPI fast-path dispatch
  unchanged.
- `(*Slide).Close` drains every cached decoder pool before
  delegating to `s.r.Close`. Drains are first-error semantics;
  idempotent.
- `formats/ndpi/strippedImage.decHandle` retypes from
  `*decoderHandle` to `*decoderhandle.Pool`. NDPI fast-path code
  paths (`DecodedTile`, `decodedTileViaCrop`, pixelCache load
  callback) switch from direct `dec.Decode` to `pool.Borrow() /
  pool.Return()`.
- `formats/ndpi/decoder_handle.go` and `decoder_handle_test.go`
  **deleted** — superseded by the shared package.
- `cmd/bench/ndpi/main.go` gained `-goroutines N` flag. Default 1
  preserves v0.27 single-thread behavior.
- `Makefile`: `MIN_NDPI_MPIXS` tightened from 130 to 220 Mpix/s
  (v0.27 lands at ~243; 130 was set as ≥2× of openslide before v0.27's
  real numbers were known and silently allowed up to 40% regressions).
  New targets: `bench-ndpi-mt`, `bench-svs`, `bench-svs-mt`.

### Public API

- **No additions.** No new exported types, functions, or methods.
- **No breaking changes.** RawTile, ScaledStrips, ReadRegion,
  DecodedTile, and every format reader continue to behave
  identically.

### Tests

- `internal/decoderhandle/handle_test.go` — pool unit tests:
  sequential reuse, concurrent borrow bound, lazy creation,
  Borrow-after-Close, Return-after-Close, Close-races-with-Borrow,
  double-Close, factory-returns-nil. Run under `-race -count=3`.
- `slide_handle_test.go` — Slide-level integration: handle reuse
  across 100 sequential calls, Close releases handles, 32-goroutine
  fanout safety.

### Out of scope (deferred forward)

- **NDPI handle instance consolidation** — sharing one Slide-level
  pool across all NDPI levels (instead of per-strippedImage) would
  marginally reduce memory but requires `formats/ndpi` knowing about
  `Slide`. Deferred indefinitely.
- **sync.Pool migration** — fixed-channel pool gives deterministic
  teardown; sync.Pool's auto-shrink under GC is appealing for
  long-running multi-Slide servers but complicates Close
  correctness testing. Revisit if memory pressure surfaces.
- **NDPI oneframe fast path** — confirmed during v0.28 brainstorm
  that oneframe fires only on tiny levels (<1 MB RGB in real
  fixtures); not worth a perf milestone.
- **`tests/oracle/`** build break (v0.24 Level API drift) — still
  pre-existing, still out of scope.

## [0.27.0] — 2026-05-28
```

Fill in the four `<fill in>` cells with actual numbers from Task 4.4 Step 4.

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.28.0 — cross-format decoder-handle pool"
```

## Task 5.2: CLAUDE.md milestone promote

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add v0.28 block at the top, demote v0.27**

Edit `CLAUDE.md`. Find:

```markdown
## Current milestone — v0.27 (in progress 2026-05-28)
```

Replace with:

```markdown
## Current milestone — v0.28 (in progress 2026-05-29)

- **Scope:** Cross-format decoder-handle pool. New
  `internal/decoderhandle.Pool` primitive — a fixed-size pool of
  long-lived `decoder.Decoder` instances per `(Slide, codec)`, sized
  `min(NumCPU, 8)`. NDPI's v0.27 per-`strippedImage` `decoderHandle`
  migrates to the same shared primitive (instance ownership stays
  per-level). `Slide.ImageDecodedTile` slow path replaces
  per-call `fac.New() / dec.Close()` with `pool.Borrow() /
  pool.Return()`. `Slide.Close` drains every cached pool. v0.27 NDPI
  fast path gains multi-core parallelism (was capped at single-thread
  by mutex). Cross-format slow paths (SVS, OME-TIFF tiled, BIF,
  Leica SCN, IFE, SZI, COG-WSI, generictiff, Philips) get ~15% bulk
  throughput improvement from eliminated `tjInit + tjDestroy`
  churn (~290 µs/call avoided, dominated by `tjDestroy`).
- **API additions:** none public. Internal: `decoderhandle.Pool`,
  `Slide.decoderFor`, `Slide.handlesMu`, `Slide.handles`.
- **API breaks:** none. RawTile bit-for-bit unchanged.
- **Active limitations:** NDPI handle instance scope stays
  per-strippedImage (no Slide-level consolidation). Pool capacity is
  hardcoded at `min(NumCPU, 8)` — no public knob.
- **Correctness bar:** `make test` green; new pool unit tests
  (`internal/decoderhandle/handle_test.go`, 8 tests) and Slide
  integration tests (`slide_handle_test.go`, 3 tests) pass under
  `-race -count=3`. TestSlideParity 40 fixtures green. v0.27 NDPI
  fast-path tests (`TestNDPIFastPathPixelParity`,
  `TestNDPIFastPathConcurrent`, `TestNDPIDecodedTilePathParity`)
  pass unchanged. `make bench-ndpi` ≥220 Mpix/s (tightened from 130).
  `make bench-svs` passes its measured gate.
- **Sealed Q-decisions** (per spec): see design doc §3 — 10 sealed
  Qs covering scope, primitive migration, concurrency shape, lazy
  vs eager init, API surface, instance scope, bench coverage, gate
  level, multi-thread bench validation, Close lifecycle.
- **Deferred forward:** NDPI Slide-level handle consolidation;
  sync.Pool migration; NDPI oneframe fast path (confirmed
  unprofitable during v0.28 brainstorm); JPEG-frame cache bounding;
  `WithScale != 1` integration. `tests/oracle/` build break stays
  pre-existing.
- **Bench reality:** v0.28 unlocks multi-core throughput on
  `Slide.DecodedTile` (~6-8× single-thread on multi-core boxes) and
  delivers a measurable bulk-decode improvement on every JPEG-tiled
  non-NDPI format.
- **Design:** docs/superpowers/specs/2026-05-29-opentile-go-v28-cross-format-decoder-pool-design.md
- **Plan:** docs/superpowers/plans/2026-05-29-opentile-go-v28-cross-format-decoder-pool.md
- **Work branch:** feat/v0.28

## Previous milestone — v0.27 (shipped 2026-05-28)
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude-md): promote v0.28 to current milestone"
```

**Checkpoint:** Phase 5 complete. Documentation reflects v0.28 state.

---

# Phase 6 — Final gate

## Task 6.1: Run all gates + merge prep

**Files:**
- None modified.

- [ ] **Step 1: Run the full gate suite**

Run, stopping on first failure:

```bash
make vet
make test
make cover
make bench-ndpi
make bench-ndpi-mt
make bench-svs
make bench-svs-mt
```

Expected:
- `make vet`: zero warnings.
- `make test`: all tests pass under `-race -count=1`.
- `make cover`: ≥80% on `internal/decoderhandle`, ≥80% on `formats/ndpi`, unchanged elsewhere.
- `make bench-ndpi`: PASS at ≥220 Mpix/s.
- `make bench-ndpi-mt`: prints multi-thread number; should be ≥3× single-thread.
- `make bench-svs`: PASS at ≥`MIN_SVS_MPIXS`.
- `make bench-svs-mt`: prints multi-thread number; should be ≥3× single-thread.

- [ ] **Step 2: Confirm branch is merge-ready**

Run:
```bash
git log --oneline main..feat/v0.28
git status
```

Expected: every commit titled per the convention (`feat(scope):`, `test(scope):`, `docs:`, `refactor(scope):`); working tree clean.

The branch ends ready for merge. The merge step is user-driven:

```bash
git checkout main
git merge --no-ff feat/v0.28 -m "Merge feat/v0.28 into main (v0.28.0)"
git tag -a v0.28.0 -m "v0.28.0 — cross-format decoder-handle pool"
git push origin main v0.28.0
git branch -d feat/v0.28
```

Do not execute the merge as part of the plan; surface the commands for the user to run.

---

## Self-review checklist (ran after writing the plan)

- **Spec coverage** — every spec section maps to a task:
  - §1.1 `Pool` type → Task 1.1
  - §1.2 NDPI primitive migration → Task 2.1 (retype) + Task 2.2 (delete v0.27 files)
  - §1.3 Slide handle cache → Task 3.1
  - §1.4 Slide.ImageDecodedTile slow path → Task 3.2
  - §1.5 NDPI multi-core fast path → Task 2.1 (implicit; the type swap enables it)
  - §1.6 Bench coverage → Tasks 4.1 + 4.2 + 4.3 + 4.4
  - §1.7 NDPI gate tighten → Task 4.3 Step 1
  - §4.2 component list → all New/Modified/Deleted files map to tasks above
  - §4.3 lock order → encoded in Task 1.1's Borrow/Return implementation
  - §4.4 concurrency invariants → Task 1.1's tests (Borrow/Return/Close races)
  - §5.1 pool unit tests → Task 1.1 Step 2
  - §5.2 Slide integration tests → Task 3.3
  - §5.3 NDPI regression → Task 2.2 Step 3 (existing tests as witness)
  - §5.5 performance gates → Task 4.3 + Task 4.4
- **Placeholder scan** — no "TBD" / "TODO" / "fill in" left as gaps. The `<fill in>` in Task 5.1's CHANGELOG and the `<computed>` in Task 4.4's `MIN_SVS_MPIXS` are intentional plan-step outputs (measurement → substitute), explicit about what to substitute and where the value comes from.
- **Type consistency** —
  - `decoderhandle.Pool` (Task 1.1, 2.1, 3.1, 3.2) — same identifier throughout
  - `decoderhandle.New` (Task 1.1, 2.1, 3.1) — same factory function
  - `Pool.Borrow` / `Pool.Return` / `Pool.Close` (Task 1.1, 2.1, 3.1, 3.2) — consistent method names
  - `Slide.decoderFor(tag uint16)` (Task 3.1, 3.2) — same signature
  - `Slide.handles map[uint16]*decoderhandle.Pool` (Task 3.1) — single canonical type
  - `ErrClosed`, `ErrFactoryReturnedNil` (Task 1.1) — sentinel names match
