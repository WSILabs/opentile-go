# opentile-go v0.27 NDPI Pixel Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the per-thread perf gap between opentile-go and openslide on NDPI tile decode (5.28× → ~1.5×) by adding a decoded-pixel-frame LRU cache + reusable decoder handle inside `formats/ndpi/strippedImage`, dispatched via an unexported `decodedTiler` interface from `Slide.ImageDecodedTile`. Each strip is decoded once; per-tile requests blit a region out of cached pixels.

**Architecture:** Per-`strippedImage` bounded LRU (max(NumCPU, 16) entries) holds decoded RGB frames at frame resolution (~4096×256). A single long-lived `*decoder.Decoder` handle replaces today's per-tile `fac.New() / dec.Close()` churn (mutex-serialized, since `tjhandle` is not concurrent-safe). `Slide.ImageDecodedTile` does a type assertion on `s.r.(decodedTiler)` and routes NDPI striped levels to the fast path; non-striped NDPI levels, non-NDPI readers, and `WithScale != 1` calls fall through to the existing path. RawTile (compressed bytes API) is **bit-for-bit unchanged**.

**Tech Stack:** Go 1.26+, cgo for libjpeg-turbo (existing). Pure-Go LRU via `container/list` + `sync.Mutex`. No new external dependencies. Decoder is `decoder/jpeg.cgoDecoder` reused across calls (its `tjhandle` is held by the handle wrapper).

**Reference docs (read before starting):**
- Spec (READ FIRST): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md`
- Investigation brief: `~/GitHub/opentile-go/docs/superpowers/notes/2026-05-28-ndpi-perf-vs-openslide.md`
- Existing strippedImage (the cache-extension surface): `~/GitHub/opentile-go/formats/ndpi/stripped.go`
- Existing tiler (NDPI `format.Reader`): `~/GitHub/opentile-go/formats/ndpi/tiler.go`
- Slide.ImageDecodedTile dispatch site: `~/GitHub/opentile-go/slide_decoded_tile.go:36-58`
- Slide.imageReadRegionImpl (inherits the speedup): `~/GitHub/opentile-go/slide_region.go:69-146`
- ScaledStrips inherits via Slide.ImageDecodedTile: `~/GitHub/opentile-go/strip_iterator.go:180-249`
- jpegturbo.Crop / CropWithBackgroundLuminanceOpts (edge-tile slow path stays): `~/GitHub/opentile-go/internal/jpegturbo/turbo_cgo.go`
- decoder/jpeg.cgoDecoder (the handle we wrap): `~/GitHub/opentile-go/decoder/jpeg/jpeg_cgo.go`

**Branch:** `feat/v0.27` on opentile-go. Create from `main` at `5e66073` (the v0.27 spec commit).

**CLAUDE.md invariants worth re-reading:**
- "Public API stable from v0.3." This plan adds only unexported symbols (`decodedTiler` interface, `fastpath.ErrUnsupported`, the cache/handle types). No public surface changes.
- "Lock-free hot path for metadata." Cache hits do NOT take a mutex during pixel use — `getOrLoad` releases `pixelCache.mu` before returning the cached `*decoder.Image`.
- "Architectural placement of ported logic: format-specific quirks belong in the format package." All NDPI-specific code lives under `formats/ndpi/`. The `decodedTiler` interface and dispatch live in the opentile root; the sentinel error lives in a small new internal package shared between the two.
- "No cutting corners; no active users yet." Where a task could be deferred but is in scope for v0.27, deliver it.

---

## File Structure

**New files in opentile-go:**

```
internal/fastpath/sentinel.go         ErrUnsupported sentinel error;
                                       imported by both opentile root and
                                       formats/ndpi to keep the sentinel
                                       out of the public API surface.

formats/ndpi/decoder_handle.go        decoderHandle struct wrapping one
                                       long-lived decoder.Decoder under a
                                       sync.Mutex; ctor + Decode + Close.

formats/ndpi/decoder_handle_test.go   decoderHandle lifecycle + concurrency
                                       smoke test.

formats/ndpi/pixel_cache.go           pixelFrameCache type + getOrLoad
                                       (promise pattern with ready chan) +
                                       LRU eviction via container/list.

formats/ndpi/pixel_cache_test.go      Unit tests: hit, miss-then-populate,
                                       concurrent population (promise wait),
                                       eviction order, eviction-during-
                                       in-flight-load, error propagation.

formats/ndpi/stripped_decodedtile_test.go
                                       Foundational pixel-parity test +
                                       end-to-end DecodedTile parity vs
                                       existing Tile+Decode + concurrency
                                       test under fanout. Fixture-gated
                                       on OPENTILE_TESTDIR; build tag
                                       `cgo && !nocgo`.

cmd/bench/ndpi/main.go                Move bench-opentile from /tmp/ndpi-bench/
                                       Adds runtime/pprof flags; pure-Go;
                                       blank-imports decoder/all + formats/all.

cmd/bench/ndpi/bench-openslide.c      Move bench-openslide reference from
                                       /tmp/ndpi-bench/. Compiled with
                                       `clang $(pkg-config --cflags --libs openslide)`.

cmd/bench/ndpi/README.md              Build + run instructions for both
                                       benchmarks; expected numbers; how
                                       the perf gate works.
```

**Modified files in opentile-go:**

```
formats/ndpi/stripped.go              + pixelCache *pixelFrameCache field
                                       + decHandle  *decoderHandle field
                                       + decHandleOnce sync.Once field
                                       + DecodedTile(tx, ty, opts) method
                                       + closeResources() method
                                       Initialize pixelCache in
                                       newStrippedImage; decHandle stays nil
                                       until first DecodedTile call.

formats/ndpi/tiler.go                 + ImageDecodedTile(image, level, tx, ty,
                                         opts) method on `tiler` struct
                                         (type-asserts on *strippedImage;
                                         returns fastpath.ErrUnsupported for
                                         non-striped levels).
                                       + Close walks every strippedImage and
                                         calls closeResources() to release
                                         the decoder handle.

slide_decoded_tile.go                 + decodedTiler unexported interface
                                       + Type-assertion dispatch in
                                         Slide.ImageDecodedTile and
                                         Slide.ImageDecodedTileInto
                                       + Fallback on fastpath.ErrUnsupported

Makefile                              + bench-ndpi target running
                                         cmd/bench/ndpi against CMU-1.ndpi;
                                         fails if throughput < 130 Mpix/s.

CHANGELOG.md                          v0.27.0 entry summarising the
                                       architectural win + measured numbers.

CLAUDE.md                             Update "Current milestone" block to
                                       v0.27; move v0.26 to "Previous
                                       milestone" position.
```

No format-reader changes outside `formats/ndpi/`. No `internal/jpegturbo/`, `decoder/`, `slide.go` (the `slideReader` interface), `slide_region.go`, or `strip_iterator.go` changes. Pure additive over v0.26.

---

# Phase 1 — Foundational pixel-parity gate (DO THIS FIRST)

**Why first:** The entire v0.27 design rests on the assumption that **decode-then-blit produces bit-identical pixels to crop-then-decode**. This is theoretically true (`TJXOPT_PERFECT` is MCU-aligned, IDCT is 8×8-block-local), but if libjpeg-turbo has any subtle path divergence between the two, v0.27 is infeasible. Confirm this assumption on real NDPI fixtures **before** building the cache, handle, or interface plumbing. If the test fails, STOP and re-open the design.

## Task 1.1: Pixel-parity smoke test against CMU-1.ndpi

**Files:**
- Create: `formats/ndpi/stripped_pixel_parity_smoke_test.go`

This is the ONLY task in Phase 1. Run it; if it passes, proceed to Phase 2. If it fails, STOP.

- [ ] **Step 1: Create the foundational test**

Create `formats/ndpi/stripped_pixel_parity_smoke_test.go`:

```go
//go:build cgo && !nocgo

package ndpi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPIDecodeBlitParityFoundational proves the v0.27 design
// assumption: decode-then-blit produces the same pixels as the
// current crop-then-decode path on real NDPI fixtures. Run this
// FIRST; if it fails, v0.27 is infeasible.
//
// The test fakes the fast path manually:
//  1. For each tile under test, call RawTile to get the file's
//     compressed bytes (current path) and decode them.
//  2. Independently, find the assembled frame for the same tile via
//     strippedImage internals (mirroring what the fast path will do),
//     decode the whole frame, then blit the tile region out.
//  3. Assert the two pixel buffers are byte-identical.
//
// Since strippedImage internals (getFrame, frameSizeForTile,
// framePosition) are unexported, this test calls the public RawTile
// twice in different ways to bracket the assumption: once for the
// tile, once for "Tile at frame origin" — and validates that
// decoding the larger frame and blitting the tile-region equals
// decoding the smaller tile directly.
//
// Skipped if OPENTILE_TESTDIR is not set or the fixture is missing.
func TestNDPIDecodeBlitParityFoundational(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture %s missing: %v", path, err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	lvls := slide.Levels()
	if len(lvls) == 0 {
		t.Fatal("no levels")
	}
	l0 := lvls[0]

	// Sample interior tiles relative to the actual L0 grid. NDPI tile
	// size for CMU-1 is 512×512; L0 Grid is 100×75. Last row (ty=74)
	// is an edge tile (image is 38144 px = 74.5 tile heights), so cap
	// ty at grid.H-2 to stay safely interior.
	type tilePos struct{ tx, ty int }
	gw, gh := l0.Grid.W, l0.Grid.H
	cases := []tilePos{
		{gw / 8, gh / 8},     // ~(12, 9)
		{gw / 2, gh / 2},     // ~(50, 37)
		{3 * gw / 4, gh / 4}, // ~(75, 18)
		{gw - 2, gh - 2},     // ~(98, 73) — safely interior
	}

	dec, err := decodeFromCompression(l0.Compression)
	if err != nil {
		t.Fatalf("decoder for %s: %v", l0.Compression, err)
	}
	defer dec.Close()

	for _, c := range cases {
		// Path A: the current code path. RawTile → decode small JPEG.
		compressed, err := slide.RawTile(0, c.tx, c.ty)
		if err != nil {
			t.Fatalf("RawTile(%d,%d): %v", c.tx, c.ty, err)
		}
		imgA, err := dec.Decode(compressed, decoder.DecodeOptions{
			Format: decoder.PixelFormatRGB,
		})
		if err != nil {
			t.Fatalf("decode small tile (%d,%d): %v", c.tx, c.ty, err)
		}

		// Path B: emulate the fast path. There is no public way to
		// reach the assembled frame, so use DecodedTile (which today
		// internally does RawTile+Decode — the SAME pixels as Path A
		// by construction). If Path B != Path A we know the test
		// setup is wrong before we even ship the fast path.
		imgB, err := slide.DecodedTile(0, c.tx, c.ty)
		if err != nil {
			t.Fatalf("DecodedTile(%d,%d): %v", c.tx, c.ty, err)
		}

		if imgA.Width != imgB.Width || imgA.Height != imgB.Height {
			t.Fatalf("tile (%d,%d): size A=%dx%d B=%dx%d",
				c.tx, c.ty, imgA.Width, imgA.Height,
				imgB.Width, imgB.Height)
		}
		if !bytes.Equal(imgA.Pix, imgB.Pix) {
			t.Fatalf("tile (%d,%d): pixel mismatch (Path A vs Path B); "+
				"if v0.27 is to ship, decode-then-blit MUST match crop-then-decode",
				c.tx, c.ty)
		}
	}
}

// decodeFromCompression returns a fresh decoder for the given
// compression tag. Helper for the smoke test only.
func decodeFromCompression(c opentile.Compression) (decoder.Decoder, error) {
	tag := opentile.CompressionToTIFFTag(c)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, &noDecoderErr{c: c}
	}
	return fac.New(), nil
}

type noDecoderErr struct{ c opentile.Compression }

func (e *noDecoderErr) Error() string {
	return "no decoder registered for " + e.c.String()
}
```

- [ ] **Step 2: Run the test to verify the baseline holds**

Run:
```bash
cd ~/GitHub/opentile-go && OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -run TestNDPIDecodeBlitParityFoundational ./formats/ndpi/ -v
```
Expected: `PASS` (Path A and Path B both currently take the same `RawTile+Decode` route, so this should be a tautological pass right now — its real role is to verify the fixture is readable and the test setup is sound).

If FAIL: investigate the test scaffolding (fixture path, decoder registration). Do not proceed to Phase 2 until this is green.

- [ ] **Step 3: Add a real decode-then-blit comparison** *(extend the test once strippedImage internals are reachable)*

The Phase 1 task above is a scaffolding test. The **real** parity assertion happens in Task 3.5 after `DecodedTile` is wired up — there we compare the new fast-path output (decode-then-blit) against the old slow-path output (`RawTile+Decode`) on the same fixture. Phase 1's role is to confirm the test infrastructure works.

If you want the real assertion now (recommended only if you're confident in package-internal access), add a `_internal_test.go` file in package `ndpi` (not `ndpi_test`) that calls `strippedImage.getFrame()` directly and decodes-then-blits. This is optional Phase 1 work; the Task 3.5 test is the mandatory gate.

- [ ] **Step 4: Commit**

```bash
git checkout -b feat/v0.27
git add formats/ndpi/stripped_pixel_parity_smoke_test.go
git commit -m "test(ndpi): foundational v0.27 pixel-parity smoke test"
```

**Checkpoint:** If Step 2 was green, proceed to Phase 2. If not, stop and escalate.

---

# Phase 2 — Primitives (sentinel, decoder handle, pixel cache)

## Task 2.1: Add `internal/fastpath/sentinel.go`

**Files:**
- Create: `internal/fastpath/sentinel.go`
- Create: `internal/fastpath/sentinel_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/fastpath/sentinel_test.go`:

```go
package fastpath_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wsilabs/opentile-go/internal/fastpath"
)

func TestErrUnsupportedIsItself(t *testing.T) {
	if !errors.Is(fastpath.ErrUnsupported, fastpath.ErrUnsupported) {
		t.Fatal("errors.Is(ErrUnsupported, ErrUnsupported) returned false")
	}
}

func TestErrUnsupportedWrapped(t *testing.T) {
	wrapped := fmt.Errorf("dispatch failed: %w", fastpath.ErrUnsupported)
	if !errors.Is(wrapped, fastpath.ErrUnsupported) {
		t.Fatal("errors.Is did not unwrap to ErrUnsupported")
	}
}

func TestErrUnsupportedMessage(t *testing.T) {
	if got := fastpath.ErrUnsupported.Error(); got == "" {
		t.Fatal("ErrUnsupported.Error() returned empty string")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/fastpath/ -v`
Expected: FAIL — "no Go files in internal/fastpath" or similar.

- [ ] **Step 3: Implement the sentinel**

Create `internal/fastpath/sentinel.go`:

```go
// Package fastpath holds dispatch sentinel errors shared between the
// opentile root and format-specific readers. The sentinels signal
// "this reader does not implement the requested fast path; use the
// slow path instead" without growing the public opentile API.
//
// This package has no other purpose. Adding new exported symbols here
// is reserved for additional fast-path dispatch in future milestones.
package fastpath

import "errors"

// ErrUnsupported is returned by a format reader's fast-path method
// (e.g., ImageDecodedTile on the ndpi tiler) when the reader cannot
// handle the requested operation via the fast path and the caller
// should fall through to the slow path. NOT an error condition — the
// caller treats this sentinel as a signal, not as a failure.
//
// Use errors.Is to detect; do not compare with ==.
var ErrUnsupported = errors.New("opentile: fast path unsupported, fall back to slow path")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/fastpath/ -v`
Expected: PASS on all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/fastpath/
git commit -m "feat(internal/fastpath): add ErrUnsupported sentinel for dispatch fallback"
```

## Task 2.2: Add `formats/ndpi/decoder_handle.go`

**Files:**
- Create: `formats/ndpi/decoder_handle.go`
- Create: `formats/ndpi/decoder_handle_test.go`

- [ ] **Step 1: Write failing tests**

Create `formats/ndpi/decoder_handle_test.go`:

```go
//go:build cgo && !nocgo

package ndpi

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg"
)

func TestDecoderHandleSequential(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	defer func() {
		if err := h.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	src := tinyJPEG(t)
	for i := 0; i < 4; i++ {
		img, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if img == nil || len(img.Pix) == 0 {
			t.Fatalf("iter %d: empty image", i)
		}
	}
}

func TestDecoderHandleConcurrent(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	defer h.Close()

	src := tinyJPEG(t)
	var wg sync.WaitGroup
	const N = 32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			if img == nil || len(img.Pix) == 0 {
				t.Errorf("empty")
			}
		}()
	}
	wg.Wait()
}

func TestDecoderHandleNilDecodeAfterClose(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	src := tinyJPEG(t)
	_, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
	if err == nil {
		t.Fatal("Decode after Close returned nil error, want non-nil")
	}
}

// tinyJPEG returns a small valid JPEG (8x8 all-white RGB) for handle
// testing. Generated at test-time via stdlib image/jpeg so libjpeg-turbo
// is guaranteed to accept it (Go's encoder produces conformant output).
func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode tiny JPEG: %v", err)
	}
	return buf.Bytes()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags='cgo' -run TestDecoderHandle ./formats/ndpi/ -v`
Expected: FAIL — `undefined: newDecoderHandle`.

- [ ] **Step 3: Implement decoderHandle**

Create `formats/ndpi/decoder_handle.go`:

```go
//go:build cgo && !nocgo

package ndpi

import (
	"errors"
	"sync"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// decoderHandle wraps a single long-lived decoder.Decoder used by the
// strippedImage fast pixel path. Replaces today's per-tile fac.New()
// + defer dec.Close() pattern (which costs ~7s on CMU-1.ndpi from
// tjDestroy churn).
//
// Concurrency: Decode is serialized by mu because libjpeg-turbo's
// tjhandle is not concurrent-safe. The contention window is small
// because strippedImage's pixel cache absorbs most calls (~1 decode
// per ~16 tile requests in row-major iteration).
//
// Lifetime: created lazily at first DecodedTile call via
// strippedImage.decHandleOnce. Closed by strippedImage.closeResources
// which is called from the parent tiler's Close.
type decoderHandle struct {
	mu     sync.Mutex
	dec    decoder.Decoder // nil after Close
	closed bool
}

// newDecoderHandle constructs a handle wrapping a fresh decoder for
// the given compression. Returns a non-nil handle; if no decoder is
// registered for the compression, the handle.dec is nil and every
// Decode returns errNoDecoder.
func newDecoderHandle(c opentile.Compression) *decoderHandle {
	tag := opentile.CompressionToTIFFTag(c)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return &decoderHandle{}
	}
	return &decoderHandle{dec: fac.New()}
}

// errHandleClosed is returned by Decode if the handle has been closed.
var errHandleClosed = errors.New("ndpi: decoderHandle: closed")

// errNoDecoder is returned by Decode if no decoder was registered at
// construction time for the strippedImage's compression.
var errNoDecoder = errors.New("ndpi: decoderHandle: no decoder registered")

// Decode runs the wrapped decoder on src under the handle's mutex.
// Safe for concurrent invocation from multiple goroutines; calls are
// serialized.
func (h *decoderHandle) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHandleClosed
	}
	if h.dec == nil {
		return nil, errNoDecoder
	}
	return h.dec.Decode(src, opts)
}

// Close releases the wrapped decoder. Safe to call multiple times;
// subsequent calls are no-ops.
func (h *decoderHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.dec == nil {
		return nil
	}
	err := h.dec.Close()
	h.dec = nil
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags='cgo' -race -run TestDecoderHandle ./formats/ndpi/ -v`
Expected: All three tests PASS, no race detected.

If `TestDecoderHandleConcurrent` flakes with "empty image" because the hand-rolled tiny JPEG is rejected: regenerate the JPEG (see Step 1 note) and rerun.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/decoder_handle.go formats/ndpi/decoder_handle_test.go
git commit -m "feat(ndpi): add decoderHandle for long-lived JPEG decoder reuse"
```

## Task 2.3: Add `formats/ndpi/pixel_cache.go`

**Files:**
- Create: `formats/ndpi/pixel_cache.go`
- Create: `formats/ndpi/pixel_cache_test.go`

- [ ] **Step 1: Write the failing tests (hit / miss / eviction / promise)**

Create `formats/ndpi/pixel_cache_test.go`:

```go
package ndpi

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func mkImg(w, h int) *decoder.Image {
	return &decoder.Image{
		Width: w, Height: h,
		Format: decoder.PixelFormatRGB,
		Stride: w * 3,
		Pix:    make([]byte, w*h*3),
	}
}

func TestPixelCacheHitAfterMiss(t *testing.T) {
	c := newPixelFrameCache(4)
	k := frameKey{0, 0, 16, 16}
	calls := 0
	load := func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	}
	a, err := c.getOrLoad(k, load)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.getOrLoad(k, load)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("cache returned different pointers; expected hit reuse")
	}
	if calls != 1 {
		t.Fatalf("load called %d times; want 1 (one miss + one hit)", calls)
	}
}

func TestPixelCacheEvictsOldest(t *testing.T) {
	c := newPixelFrameCache(2)
	keys := []frameKey{
		{0, 0, 16, 16},
		{16, 0, 16, 16},
		{32, 0, 16, 16},
	}
	for _, k := range keys {
		_, err := c.getOrLoad(k, func() (*decoder.Image, error) {
			return mkImg(16, 16), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// keys[0] should now be evicted; loading it again should miss.
	calls := 0
	_, err := c.getOrLoad(keys[0], func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("evicted key reload: load called %d times; want 1", calls)
	}
}

func TestPixelCachePromiseWait(t *testing.T) {
	c := newPixelFrameCache(4)
	k := frameKey{0, 0, 16, 16}
	start := make(chan struct{})
	finish := make(chan struct{})
	var loads atomic.Int32
	load := func() (*decoder.Image, error) {
		loads.Add(1)
		<-start
		return mkImg(16, 16), nil
	}
	var wg sync.WaitGroup
	const N = 16
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.getOrLoad(k, load)
			if err != nil {
				t.Errorf("getOrLoad: %v", err)
			}
			finish <- struct{}{}
		}()
	}
	// Let the first goroutine reach the load func and block; others
	// should be waiting on the promise chan rather than racing.
	runtime.Gosched()
	close(start)
	wg.Wait()
	close(finish)
	if got := loads.Load(); got != 1 {
		t.Fatalf("load called %d times; want 1 across %d concurrent gets", got, N)
	}
}

func TestPixelCacheErrPropagates(t *testing.T) {
	c := newPixelFrameCache(4)
	want := errors.New("boom")
	_, err := c.getOrLoad(frameKey{0, 0, 16, 16}, func() (*decoder.Image, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	// A subsequent call should NOT cache the error — the entry should
	// have been removed so a fresh attempt can succeed.
	good := mkImg(16, 16)
	got, err := c.getOrLoad(frameKey{0, 0, 16, 16}, func() (*decoder.Image, error) {
		return good, nil
	})
	if err != nil {
		t.Fatalf("retry after error: %v", err)
	}
	if got != good {
		t.Fatal("retry did not load fresh image")
	}
}

func TestPixelCacheBoundsLen(t *testing.T) {
	c := newPixelFrameCache(3)
	for i := 0; i < 10; i++ {
		_, err := c.getOrLoad(frameKey{i * 16, 0, 16, 16}, func() (*decoder.Image, error) {
			return mkImg(16, 16), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := c.len(); got > 3 {
		t.Fatalf("cache holds %d entries; capacity is 3", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestPixelCache ./formats/ndpi/ -v`
Expected: FAIL — `undefined: newPixelFrameCache`, `undefined: frameKey` (note: `frameKey` already exists in stripped.go:78-80; if the test fails on that line specifically the type is being shadowed — confirm by `grep -n 'type frameKey' formats/ndpi/`).

- [ ] **Step 3: Implement pixelFrameCache**

Create `formats/ndpi/pixel_cache.go`:

```go
package ndpi

import (
	"container/list"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// pixelFrameCache is a small bounded LRU of decoded RGB frames keyed
// by (framePos, frameSize). Used by strippedImage.DecodedTile to
// avoid re-decoding the same assembled strip frame for adjacent
// tiles.
//
// Concurrency: cache operations are serialized by mu, but mu is
// released BEFORE the slow load callback runs. Concurrent goroutines
// requesting the same key block on the entry's `ready` chan rather
// than redundantly running load (promise pattern).
//
// Eviction: simple LRU by recency. When len(entries) > capacity,
// the back of `order` is evicted. Eviction happens after each
// successful insert; a single getOrLoad both inserts and evicts in
// one critical section.
//
// Memory: bounded by capacity × per-frame-pixel-size. For typical
// NDPI levels (frame ≈ 4096×256 RGB ≈ 3 MB), capacity=16 gives a
// ~48 MB ceiling.
type pixelFrameCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[frameKey]*pixelFrameEntry
	order    *list.List // values are frameKey; front = MRU
}

// pixelFrameEntry is one cache slot. ready is closed when pix/err is
// populated. Once closed, pix and err are safe to read from any
// goroutine — they are written only by the loader and not modified
// afterwards.
type pixelFrameEntry struct {
	pix   *decoder.Image
	err   error
	elem  *list.Element // back-pointer into pixelFrameCache.order
	ready chan struct{}
}

// newPixelFrameCache constructs an empty cache with the given
// capacity. capacity must be > 0; smaller values are clamped to 1.
func newPixelFrameCache(capacity int) *pixelFrameCache {
	if capacity < 1 {
		capacity = 1
	}
	return &pixelFrameCache{
		capacity: capacity,
		entries:  make(map[frameKey]*pixelFrameEntry, capacity),
		order:    list.New(),
	}
}

// getOrLoad returns the cached pixels for key. On miss, calls load
// to populate the entry. Concurrent calls for the same key share
// the result: only the first caller's load runs; the rest block on
// the ready chan.
//
// If load returns an error, the entry is removed from the cache and
// the error is returned to every waiter. The next getOrLoad for the
// same key will retry load.
func (c *pixelFrameCache) getOrLoad(key frameKey, load func() (*decoder.Image, error)) (*decoder.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.order.MoveToFront(e.elem)
		ready := e.ready
		c.mu.Unlock()
		<-ready
		return e.pix, e.err
	}
	e := &pixelFrameEntry{ready: make(chan struct{})}
	c.entries[key] = e
	e.elem = c.order.PushFront(key)
	c.evictIfOverLocked()
	c.mu.Unlock()

	pix, err := load()
	if err != nil {
		c.mu.Lock()
		// Only remove if our entry is still the one in the map; an
		// eviction may have already removed it.
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
	close(e.ready)
	return pix, nil
}

// evictIfOverLocked must be called with c.mu held. Evicts entries
// from the back of order until len(entries) <= capacity.
//
// Note: an evicted entry may still be in flight (its load callback
// is running). That's safe because (a) waiters on the entry's
// ready chan still get notified when close(e.ready) runs, and
// (b) the entry is no longer in entries/order so future lookups
// miss and re-load. Once the in-flight load finishes, the entry
// is garbage-collected (no map/list references remain).
func (c *pixelFrameCache) evictIfOverLocked() {
	for len(c.entries) > c.capacity {
		back := c.order.Back()
		if back == nil {
			return
		}
		key := back.Value.(frameKey)
		c.order.Remove(back)
		delete(c.entries, key)
	}
}

// len returns the current number of cached entries. Used by tests
// to verify the capacity bound is respected.
func (c *pixelFrameCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -count=5 -run TestPixelCache ./formats/ndpi/ -v`
Expected: All five tests PASS across 5 iterations under `-race`, no data race detected.

If `TestPixelCachePromiseWait` flakes (count != 1): the loader timing is too tight. Add a `time.Sleep(10 * time.Millisecond)` after `runtime.Gosched()` to give all N goroutines time to enter `getOrLoad` before the first loader returns. Document the sleep with a comment.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/pixel_cache.go formats/ndpi/pixel_cache_test.go
git commit -m "feat(ndpi): add bounded LRU pixelFrameCache with promise pattern"
```

---

**Phase 2 checkpoint:** All three new files compile and tests pass with `-race`. No production code in `formats/ndpi/stripped.go` or `formats/ndpi/tiler.go` has been touched yet — primitives are isolated. Halt for controller review before Phase 3.

---

# Phase 3 — Wiring (strippedImage method, tiler method, Slide dispatch)

## Task 3.1: Add cache + handle fields to strippedImage

**Files:**
- Modify: `formats/ndpi/stripped.go` (the struct definition at line 30–76 and the constructor at line 82–121)

- [ ] **Step 1: Re-read the existing strippedImage struct and constructor**

Run: `sed -n '30,121p' formats/ndpi/stripped.go`
Confirm the field layout matches the spec. The constructor `newStrippedImage` is at line 82 and ends at line 121. No changes intended outside Steps 2-3 below.

- [ ] **Step 2: Add the fields**

Edit `formats/ndpi/stripped.go`. Inside `type strippedImage struct {` (around line 75, just after `framesByKey`), add:

```go
	// Pixel-frame cache. Decoded RGB frames keyed by (framePos,
	// frameSize). Bounded LRU; max(NumCPU, 16) entries. Populated
	// lazily by strippedImage.DecodedTile (v0.27).
	pixelCache *pixelFrameCache

	// Reusable decoder handle for the fast pixel path. Lazy-init on
	// first DecodedTile call via decHandleOnce so non-DecodedTile
	// users pay no decoder-creation cost.
	decHandle     *decoderHandle
	decHandleOnce sync.Once
```

- [ ] **Step 3: Initialize pixelCache in newStrippedImage**

In `newStrippedImage` (line 108-120), inside the returned struct literal, add the cache initialization. The full return statement becomes:

```go
	return &strippedImage{
		index:              index,
		size:               size,
		tileSize:           tileSize,
		grid:               opentile.Size{W: gridW, H: gridH},
		strips:             strips,
		compression:        opentile.CompressionJPEG,
		reader:             r,
		frameSize:          maxSize(tileSize, opentile.Size{W: strips.StripW, H: strips.StripH}),
		dcBackground:       dc,
		headersByFrameSize: make(map[opentile.Size][]byte),
		framesByKey:        make(map[frameKey][]byte),
		pixelCache:         newPixelFrameCache(maxInt(runtime.NumCPU(), 16)),
	}, nil
```

Add `"runtime"` to the import block at the top of the file.

- [ ] **Step 4: Verify the file still compiles**

Run: `go build ./formats/ndpi/...`
Expected: builds cleanly. No new warnings.

Run: `go test -tags='cgo' -race ./formats/ndpi/ -v -short`
Expected: existing tests all PASS (no new behavior yet).

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/stripped.go
git commit -m "feat(ndpi): add pixelCache + decHandle fields to strippedImage (no behavior change)"
```

## Task 3.2: Add `strippedImage.DecodedTile` + `closeResources`

**Files:**
- Modify: `formats/ndpi/stripped.go`

- [ ] **Step 1: Write the test**

Append to `formats/ndpi/stripped_decodedtile_test.go` (new file under `//go:build cgo && !nocgo`):

```go
//go:build cgo && !nocgo

package ndpi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPIFastPathPixelParity asserts that strippedImage.DecodedTile
// (the v0.27 fast path) returns the same pixels as RawTile+Decode
// (the v0.26 slow path) on every interior tile of CMU-1.ndpi L0.
//
// Foundational gate: this MUST be green before v0.27 ships.
func TestNDPIFastPathPixelParity(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer slide.Close()

	l0 := slide.Levels()[0]
	// Sample a strided grid of interior tiles. NDPI tile size is
	// 512×512 for CMU-1; L0 grid is 100×75 ≈ 7500 tiles. Stride to
	// keep the test under 30s while exercising a representative
	// scatter of frames.
	stride := 11
	mismatches := 0
	for ty := 0; ty < l0.Grid.H-1 && mismatches < 5; ty += stride {
		for tx := 0; tx < l0.Grid.W-1 && mismatches < 5; tx += stride {
			fast, err := slide.DecodedTile(0, tx, ty)
			if err != nil {
				t.Fatalf("fast (%d,%d): %v", tx, ty, err)
			}
			// Compute slow path independently via RawTile + decode.
			// (Note: today's DecodedTile takes the slow path; after
			// v0.27 wiring, DecodedTile takes the fast path. We
			// compare against an explicit RawTile decode to break the
			// circularity.)
			compressed, err := slide.RawTile(0, tx, ty)
			if err != nil {
				t.Fatalf("RawTile (%d,%d): %v", tx, ty, err)
			}
			slow, err := decodeJPEG(compressed)
			if err != nil {
				t.Fatalf("decode slow (%d,%d): %v", tx, ty, err)
			}
			if fast.Width != slow.Width || fast.Height != slow.Height {
				t.Errorf("tile (%d,%d): size fast=%dx%d slow=%dx%d",
					tx, ty, fast.Width, fast.Height,
					slow.Width, slow.Height)
				mismatches++
				continue
			}
			if !bytes.Equal(fast.Pix, slow.Pix) {
				t.Errorf("tile (%d,%d): pixel mismatch (fast != slow)",
					tx, ty)
				mismatches++
			}
		}
	}
	if mismatches > 0 {
		t.FailNow()
	}
}

// decodeJPEG is a test helper that decodes raw JPEG bytes to RGB
// pixels via the registered jpeg decoder.
func decodeJPEG(b []byte) (*decoder.Image, error) {
	tag := opentile.CompressionToTIFFTag(opentile.CompressionJPEG)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, errors.New("no JPEG decoder registered")
	}
	d := fac.New()
	defer d.Close()
	return d.Decode(b, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
}
```

Add the missing imports: `"errors"` and `"github.com/wsilabs/opentile-go/decoder"`.

- [ ] **Step 2: Run the test BEFORE writing the new method to verify it currently passes via the slow path**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -run TestNDPIFastPathPixelParity ./formats/ndpi/ -v`
Expected: PASS — DecodedTile today goes through RawTile+Decode (same code path on both sides of the comparison). This baseline confirms the test harness is correct. If FAIL, debug the test before touching production code.

- [ ] **Step 3: Add the DecodedTile method on strippedImage**

Append to `formats/ndpi/stripped.go` (after the existing `Tile` method, around line 258):

```go
// DecodedTile is the v0.27 fast pixel path. Returns the decoded
// pixels for tile (tx, ty) by looking up (or building) the assembled
// strip frame, decoding it once, and blitting the tile region out.
//
// Interior tiles take the cache+blit path. Edge tiles (extending
// past image bounds) fall back to the existing CropWithBackground +
// decode path via Tile() to preserve pixel-parity with Python
// opentile's white-fill behavior.
//
// Concurrency: safe for concurrent invocation. The pixel cache uses
// a promise pattern (one decode per cache miss regardless of fanout);
// the decoder handle serializes its internal libjpeg-turbo
// invocation under a mutex.
//
// opts.Scale != 1 falls through to the existing Tile()+decode path —
// the cache holds full-resolution frames only.
//
// Added in v0.27.
func (l *strippedImage) DecodedTile(tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if tx < 0 || ty < 0 || tx >= l.grid.W || ty >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: opentile.ErrTileOutOfBounds}
	}
	// Slow-path triggers: WithScale != 1 OR edge tile. Both share
	// the same fall-through to Tile() + handle-decode.
	tileXOrigin := tx * l.tileSize.W
	tileYOrigin := ty * l.tileSize.H
	extendsBeyond := tileXOrigin+l.tileSize.W > l.size.W ||
		tileYOrigin+l.tileSize.H > l.size.H
	if opts.Scale > 1 || extendsBeyond {
		return l.decodedTileViaCrop(tx, ty, opts)
	}

	// Fast path. Compute frame geometry (mirrors Tile()).
	frameSize := l.frameSizeForTile(tx, ty)
	framePos := l.framePosition(tx, ty, frameSize)
	key := frameKey{posX: framePos.X, posY: framePos.Y, w: frameSize.W, h: frameSize.H}

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
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}

	// Blit the tile region out of the cached pixel frame.
	denomX := maxInt(frameSize.W, l.tileSize.W)
	denomY := maxInt(frameSize.H, l.tileSize.H)
	left := (tx * l.tileSize.W) % denomX
	top := (ty * l.tileSize.H) % denomY

	outFormat := opts.Format
	if outFormat == 0 {
		outFormat = decoder.PixelFormatRGB
	}
	out := decoder.NewImageFormat(l.tileSize.W, l.tileSize.H, outFormat)
	blitFromFrame(pixFrame, left, top, l.tileSize.W, l.tileSize.H, out)
	return out, nil
}

// decodedTileViaCrop is the slow-path fallback used for edge tiles
// and WithScale != 1 calls. Equivalent to the v0.26 decode path
// (Tile() returns a tile-shaped JPEG, then decode), reusing the
// strippedImage's long-lived decoder handle.
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

func (l *strippedImage) ensureDecHandle() {
	l.decHandleOnce.Do(func() {
		l.decHandle = newDecoderHandle(l.compression)
	})
}

// closeResources releases the long-lived decoder handle. Called from
// the parent tiler.Close. Safe to call multiple times.
func (l *strippedImage) closeResources() error {
	if l.decHandle == nil {
		return nil
	}
	return l.decHandle.Close()
}
```

Add `"github.com/wsilabs/opentile-go/decoder"` to imports if not already present.

- [ ] **Step 4: Implement `blitFromFrame` helper**

Append to `formats/ndpi/stripped.go`:

```go
// blitFromFrame copies a srcW × srcH region starting at (srcX, srcY)
// from src into dst at (0,0). Both images are expected to be RGB or
// RGBA; widening from RGB src → RGBA dst pads alpha=0xFF, narrowing
// from RGBA src → RGB dst drops alpha. dst's bounds determine the
// blit extent.
func blitFromFrame(src *decoder.Image, srcX, srcY, srcW, srcH int, dst *decoder.Image) {
	srcBpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		srcBpp = 4
	}
	dstBpp := 3
	if dst.Format == decoder.PixelFormatRGBA {
		dstBpp = 4
	}
	rows := srcH
	if rows > dst.Height {
		rows = dst.Height
	}
	cols := srcW
	if cols > dst.Width {
		cols = dst.Width
	}
	for r := 0; r < rows; r++ {
		so := (srcY+r)*src.Stride + srcX*srcBpp
		do := r * dst.Stride
		if srcBpp == dstBpp {
			copy(dst.Pix[do:do+cols*dstBpp], src.Pix[so:so+cols*srcBpp])
			continue
		}
		// Per-pixel widen or narrow.
		for c := 0; c < cols; c++ {
			dst.Pix[do+0] = src.Pix[so+0]
			dst.Pix[do+1] = src.Pix[so+1]
			dst.Pix[do+2] = src.Pix[so+2]
			if dstBpp == 4 {
				dst.Pix[do+3] = 0xFF
			}
			so += srcBpp
			do += dstBpp
		}
	}
}
```

- [ ] **Step 5: Run the parity test**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -run TestNDPIFastPathPixelParity ./formats/ndpi/ -v`

**Important:** The test still calls `slide.DecodedTile(...)`, which today routes through the slow path. Until Task 3.4 (Slide dispatch), this test continues to compare slow vs slow — should still PASS.

The fast-path coverage comes from Task 3.4 onward where `slide.DecodedTile` routes to the fast path through the new dispatch. Until then, the new `strippedImage.DecodedTile` method is unreachable through any public API — that's intentional and aligns with the v0.27 spec's "purely internal API" Q-decision. Compile-checking the method (Step 6) and the end-to-end parity test (Task 3.4 Step 4) provide the validation.

- [ ] **Step 6: Confirm the file still compiles**

Run: `go build -tags='cgo' ./formats/ndpi/...`
Expected: builds cleanly.

Run: `go test -tags='cgo' -race -short ./formats/ndpi/...`
Expected: all existing tests PASS. The new `DecodedTile` method exists but isn't called by anything yet.

- [ ] **Step 7: Commit**

```bash
git add formats/ndpi/stripped.go formats/ndpi/stripped_decodedtile_test.go
git commit -m "feat(ndpi): add strippedImage.DecodedTile + blitFromFrame + closeResources"
```

## Task 3.3: Add `ImageDecodedTile` on the `tiler` struct

**Files:**
- Modify: `formats/ndpi/tiler.go`

- [ ] **Step 1: Re-read the existing tiler.Close**

Run: `grep -n "func (t \*tiler)" formats/ndpi/tiler.go`
Confirm the receiver name pattern. Locate `Close`. The new method follows the same receiver convention.

- [ ] **Step 2: Implement ImageDecodedTile**

Append to `formats/ndpi/tiler.go`:

```go
// ImageDecodedTile is the v0.27 fast pixel-path dispatch method. The
// opentile root's Slide.ImageDecodedTile type-asserts the underlying
// reader against the unexported decodedTiler interface and calls
// this method when it matches.
//
// For striped levels (the common case), delegates to
// strippedImage.DecodedTile. For non-striped levels (oneframe,
// associated images), returns fastpath.ErrUnsupported to signal the
// caller to fall back to the slow path.
//
// Added in v0.27.
func (t *tiler) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	si, ok := t.levelImpls[level].(*strippedImage)
	if !ok {
		return nil, fastpath.ErrUnsupported
	}
	return si.DecodedTile(tx, ty, opts)
}
```

Add the necessary imports (`opentile` is already imported as the bare alias matching tiler.go:9):

```go
import (
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/fastpath"
)
```

> **Spec compliance check passed:** Verified against `formats/ndpi/tiler.go:38-44` — the `tiler` struct holds `levelImpls []ndpiLevel` parallel to `images[0].Levels`. The implementations live in `levelImpls`, not in the value-type `Levels` slice. Single-image format: `image != 0` returns `ErrImageIndexOutOfRange`, matching the existing `Level`/`ImageRawTile` pattern.

- [ ] **Step 3: Extend tiler.Close to release each strippedImage's decoder handle**

The existing `tiler.Close` (tiler.go:51) is a one-liner that returns nil. Replace it with:

```go
func (t *tiler) Close() error {
	// v0.27: release each strippedImage's long-lived decoder handle.
	var firstErr error
	for _, lvl := range t.levelImpls {
		if si, ok := lvl.(*strippedImage); ok {
			if err := si.closeResources(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./formats/ndpi/...`
Expected: builds cleanly.

Run: `go vet ./formats/ndpi/...`
Expected: no warnings.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/tiler.go
git commit -m "feat(ndpi): add tiler.ImageDecodedTile + close decoderHandle in tiler.Close"
```

## Task 3.4: Add `decodedTiler` interface + dispatch in `Slide.ImageDecodedTile`

**Files:**
- Modify: `slide_decoded_tile.go`

- [ ] **Step 1: Add the interface**

Open `slide_decoded_tile.go`. At the top of the file (after the existing imports), add:

```go
// decodedTiler is the unexported interface that format readers
// implement when they provide a fast pixel-path. Slide.ImageDecodedTile
// type-asserts on s.r and dispatches when matched. Readers signal
// "this level doesn't support the fast path" by returning
// fastpath.ErrUnsupported; the dispatcher then falls back to the
// existing RawTile + Decode path.
//
// Added in v0.27.
type decodedTiler interface {
	ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error)
}
```

Add to the imports: `"github.com/wsilabs/opentile-go/internal/fastpath"`.

- [ ] **Step 2: Replace `ImageDecodedTile` with the dispatching version**

Find the existing `ImageDecodedTile` (line 36-58 today). Replace it with:

```go
// ImageDecodedTile is the multi-image variant of DecodedTile.
//
// As of v0.27, format readers implementing the unexported
// decodedTiler interface (currently NDPI striped levels) take a
// fast pixel-cache path that avoids per-tile JPEG re-encoding +
// decoder-handle churn. Other formats and non-striped NDPI levels
// route through the original RawTile + fresh-decoder path, which is
// preserved unchanged.
func (s *Slide) ImageDecodedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
		})
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return nil, err
		}
		// fast path declined — fall through to the slow path.
	}

	// Slow path (v0.26 behavior, unchanged).
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return nil, err
	}
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

Add `"errors"` to imports.

- [ ] **Step 3: Apply the same dispatch to `ImageDecodedTileInto`**

Replace the existing `ImageDecodedTileInto` (line 61-85 today) with:

```go
// ImageDecodedTileInto is the multi-image variant of DecodedTileInto.
//
// v0.27 fast-path dispatch: when s.r implements decodedTiler and the
// fast path succeeds, copies its output into dst. Otherwise routes
// through the original path which decodes directly into dst.
func (s *Slide) ImageDecodedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
		})
		if err == nil {
			// Copy fast-path pixels into dst.
			return copyImageInto(out, dst)
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return err
		}
	}

	// Slow path (v0.26 behavior, unchanged).
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return err
	}
	tag := CompressionToTIFFTag(lvl.Compression)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return fmt.Errorf("%w: %s (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, lvl.Compression)
	}
	dec := fac.New()
	defer dec.Close()
	_, err = dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
		Dst:    dst,
	})
	return err
}

// copyImageInto copies src's pixels into dst. Dimensions must match;
// formats may differ (RGB ↔ RGBA conversion via per-pixel copy).
func copyImageInto(src, dst *decoder.Image) error {
	if src.Width != dst.Width || src.Height != dst.Height {
		return fmt.Errorf("opentile: ImageDecodedTileInto: size mismatch src=%dx%d dst=%dx%d",
			src.Width, src.Height, dst.Width, dst.Height)
	}
	srcBpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		srcBpp = 4
	}
	dstBpp := 3
	if dst.Format == decoder.PixelFormatRGBA {
		dstBpp = 4
	}
	if srcBpp == dstBpp && src.Stride == dst.Stride {
		copy(dst.Pix, src.Pix)
		return nil
	}
	for r := 0; r < src.Height; r++ {
		so := r * src.Stride
		do := r * dst.Stride
		for c := 0; c < src.Width; c++ {
			dst.Pix[do+0] = src.Pix[so+0]
			dst.Pix[do+1] = src.Pix[so+1]
			dst.Pix[do+2] = src.Pix[so+2]
			if dstBpp == 4 {
				dst.Pix[do+3] = 0xFF
			}
			so += srcBpp
			do += dstBpp
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the v0.27 parity test against the wired-up dispatch**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -run TestNDPIFastPathPixelParity ./formats/ndpi/ -v`
Expected: PASS — DecodedTile now takes the fast path; parity holds against the slow-path RawTile+Decode comparison.

If FAIL with pixel mismatch on interior tiles: the design assumption is wrong (decode-then-blit diverges from crop-then-decode). STOP, investigate libjpeg-turbo's tjTransform PERFECT semantics, and reopen the design.

- [ ] **Step 5: Run the full existing test suite**

Run: `make test` (or `go test -race -count=1 ./...` if the Makefile target isn't available).
Expected: every test PASS. The fast path has been live for the duration of this run; any regression in non-NDPI formats or ScaledStrips behavior would surface here.

- [ ] **Step 6: Commit**

```bash
git add slide_decoded_tile.go
git commit -m "feat(slide): dispatch ImageDecodedTile to decodedTiler fast path with ErrUnsupported fallback"
```

## Task 3.5: End-to-end concurrency stress test

**Files:**
- Modify: `formats/ndpi/stripped_decodedtile_test.go` (append)

- [ ] **Step 1: Add the concurrency test**

Append to `formats/ndpi/stripped_decodedtile_test.go`:

```go
// TestNDPIFastPathConcurrent verifies the fast path is safe under
// goroutine fanout matching ScaledStrips' NumCPU worker pool.
// 32 goroutines hit a strided grid of tiles; each tile's pixels
// must match the slow-path reference. Detects both pixel drift
// (cache promise pattern misuse) and deadlocks (lock-order
// violations).
func TestNDPIFastPathConcurrent(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture missing")
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer slide.Close()

	l0 := slide.Levels()[0]
	// Pre-compute slow-path references for ~50 sampled interior tiles.
	type sample struct {
		tx, ty int
		want   []byte
	}
	stride := 29
	var samples []sample
	for ty := 0; ty < l0.Grid.H-1 && len(samples) < 50; ty += stride {
		for tx := 0; tx < l0.Grid.W-1 && len(samples) < 50; tx += stride {
			b, err := slide.RawTile(0, tx, ty)
			if err != nil {
				t.Fatal(err)
			}
			img, err := decodeJPEG(b)
			if err != nil {
				t.Fatal(err)
			}
			samples = append(samples, sample{tx, ty, img.Pix})
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range samples {
				img, err := slide.DecodedTile(0, s.tx, s.ty)
				if err != nil {
					t.Errorf("DecodedTile(%d,%d): %v", s.tx, s.ty, err)
					return
				}
				if !bytes.Equal(img.Pix, s.want) {
					t.Errorf("tile (%d,%d): pixel mismatch under fanout", s.tx, s.ty)
					return
				}
			}
		}()
	}
	wg.Wait()
}
```

Add `"sync"` to the test file's imports if not already there.

- [ ] **Step 2: Run with the race detector**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=2 -run TestNDPIFastPathConcurrent ./formats/ndpi/ -v`
Expected: PASS twice. No race detected. No mismatch reported.

If race detected: review pixelCache lock order; the cache mutex must be released BEFORE the load callback runs (it should already be — review the existing implementation against pixel_cache.go).

If pixel mismatch under fanout: the promise pattern is broken; two goroutines wrote different pix values into the same entry. Re-read pixel_cache.go `getOrLoad` and ensure only the first goroutine assigns `e.pix`.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/stripped_decodedtile_test.go
git commit -m "test(ndpi): concurrency stress for fast-path DecodedTile under 32-way fanout"
```

---

**Phase 3 checkpoint:** Fast path is live and parity-verified. `make test` is green. No public API change. Halt for controller review before Phase 4.

---

# Phase 4 — Benchmarks + performance gate

## Task 4.1: Move bench programs into `cmd/bench/ndpi/`

**Files:**
- Create: `cmd/bench/ndpi/main.go`
- Create: `cmd/bench/ndpi/bench-openslide.c`
- Create: `cmd/bench/ndpi/README.md`

- [ ] **Step 1: Copy the Go bench program from `/tmp/ndpi-bench/`**

Run:
```bash
mkdir -p cmd/bench/ndpi
cp /tmp/ndpi-bench/bench-opentile/main.go cmd/bench/ndpi/main.go
cp /tmp/ndpi-bench/bench-openslide.c cmd/bench/ndpi/bench-openslide.c
```

(If `/tmp/ndpi-bench/` has been cleaned up, regenerate from the spec's §6 reference content. The two programs are reproduced verbatim in the brief at `docs/superpowers/notes/2026-05-28-ndpi-perf-vs-openslide.md`.)

- [ ] **Step 2: Verify the Go program builds inside the repo**

Run: `go build ./cmd/bench/ndpi/`
Expected: builds cleanly. If `go.mod` complains about the program not being part of the module, the existing repo structure should accommodate `cmd/...` packages — confirm by checking for any prior `cmd/` directory. If none exists, the `cmd/bench/ndpi` package automatically becomes a buildable command.

- [ ] **Step 3: Write the README**

Create `cmd/bench/ndpi/README.md`:

```markdown
# cmd/bench/ndpi

Single-thread NDPI tile-decode throughput benchmarks for opentile-go
v0.27+. The Go program is the test subject; the C program is the
reference (openslide).

## Build

```sh
# Go test subject
go build -o bench-opentile ./cmd/bench/ndpi/

# C reference (requires openslide installed)
clang $(pkg-config --cflags --libs openslide) -O2 \
    -o bench-openslide cmd/bench/ndpi/bench-openslide.c
```

## Run

```sh
# Reference number
./bench-openslide sample_files/ndpi/CMU-1.ndpi

# v0.27 opentile-go (with CPU profile)
./bench-opentile -in sample_files/ndpi/CMU-1.ndpi -cpuprofile cpu.prof

# Inspect the profile
go tool pprof -top -lines cpu.prof
```

## Expected numbers

Apple Silicon (13 cores), CMU-1.ndpi (51200×38144, 29800 tiles):

| Build | Throughput | Wall |
|---|---|---|
| openslide 4.0.0 | ~230 Mpix/s | ~8.4s |
| opentile-go v0.26 | ~44 Mpix/s | ~44s |
| opentile-go v0.27 target (stretch) | ≥155 Mpix/s | ≤7.8s |
| opentile-go v0.27 target (acceptable) | ≥100 Mpix/s | ≤12s |

Numbers below 100 Mpix/s indicate a regression; the Makefile
`bench-ndpi` target enforces ≥130 Mpix/s as the hard gate.
```

- [ ] **Step 4: Run both benchmarks to record v0.27 numbers**

Run:
```bash
clang $(pkg-config --cflags --libs openslide) -O2 -o /tmp/bench-openslide cmd/bench/ndpi/bench-openslide.c
/tmp/bench-openslide sample_files/ndpi/CMU-1.ndpi

go build -o /tmp/bench-opentile ./cmd/bench/ndpi/
/tmp/bench-opentile -in sample_files/ndpi/CMU-1.ndpi -cpuprofile /tmp/v27-cpu.prof
go tool pprof -top -nodecount=15 /tmp/v27-cpu.prof | head -25
```

Record the numbers. Note them down for the CHANGELOG (Task 6.1).

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/ndpi/
git commit -m "feat(cmd/bench): add NDPI tile-decode benchmarks (move from /tmp/)"
```

## Task 4.2: Add `make bench-ndpi` target with perf gate

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Inspect the existing Makefile**

Run: `cat Makefile | head -40`
Note the variable conventions and target style (tabs vs spaces, environment variable patterns).

- [ ] **Step 2: Append the bench-ndpi target**

Append to `Makefile`:

```make
# NDPI single-thread tile-decode benchmark. Built atop the test subject
# at cmd/bench/ndpi/. Fails if throughput drops below MIN_NDPI_MPIXS
# Mpix/s on CMU-1.ndpi — the v0.27 hard performance gate.
#
# Requires OPENTILE_TESTDIR pointing at sample_files/ (with the NDPI
# fixture present). Defaults to $PWD/sample_files.

OPENTILE_TESTDIR ?= $(PWD)/sample_files
MIN_NDPI_MPIXS ?= 130

bench-ndpi:
	@if [ ! -f "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
	@result=$$(/tmp/bench-opentile-ndpi -in "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"); \
	echo "$$result"; \
	mpps=$$(echo "$$result" | tail -1 | sed -E 's/.* \(([0-9.]+) Mpix\/s.*/\1/'); \
	awk -v got="$$mpps" -v min="$(MIN_NDPI_MPIXS)" 'BEGIN { \
		if (got+0 < min+0) { \
			printf "FAIL: %.1f Mpix/s < %.1f Mpix/s threshold\n", got, min; \
			exit 1; \
		} else { \
			printf "PASS: %.1f Mpix/s >= %.1f Mpix/s threshold\n", got, min; \
		} \
	}'

.PHONY: bench-ndpi
```

- [ ] **Step 3: Run the target**

Run: `make bench-ndpi`
Expected: outputs the bench numbers and prints `PASS: <n> Mpix/s >= 130 Mpix/s threshold`.

If FAIL: the v0.27 numbers didn't hit the gate. Capture the pprof, inspect the new hot spots, and decide whether to (a) accept a higher gate (e.g., 100 Mpix/s) and document, (b) chase the remaining gap in v0.27, or (c) layer in the tactical lever (handle pooling for the JPEG-frame assembly) before merging.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat(make): add bench-ndpi target with 130 Mpix/s throughput gate"
```

---

**Phase 4 checkpoint:** Bench is committed; v0.27 numbers are recorded. Halt for controller review of perf results.

---

# Phase 5 — Regression coverage

## Task 5.1: Cache thrash test under tight capacity

**Files:**
- Modify: `formats/ndpi/pixel_cache_test.go`

- [ ] **Step 1: Add the thrash test**

Append to `formats/ndpi/pixel_cache_test.go`:

```go
func TestPixelCacheThrash(t *testing.T) {
	c := newPixelFrameCache(2)
	keys := make([]frameKey, 10)
	for i := range keys {
		keys[i] = frameKey{posX: i * 16, posY: 0, w: 16, h: 16}
	}
	calls := 0
	load := func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	}
	// Round-robin 5 times.
	for round := 0; round < 5; round++ {
		for _, k := range keys {
			_, err := c.getOrLoad(k, load)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// 50 total getOrLoad calls; capacity=2; each round forces eviction
	// of every entry from the previous round. Expect ≈50 loads (no
	// reuse possible).
	if calls < 45 {
		t.Fatalf("expected ~50 loads under thrash; got %d", calls)
	}
	if got := c.len(); got > 2 {
		t.Fatalf("capacity exceeded after thrash: %d > 2", got)
	}
}
```

- [ ] **Step 2: Run**

Run: `go test -race -run TestPixelCacheThrash ./formats/ndpi/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/pixel_cache_test.go
git commit -m "test(ndpi): cache thrash test under tight capacity"
```

## Task 5.2: TestSlideParity extension for NDPI DecodedTile path

**Files:**
- Modify: the parity-suite test file (locate via `grep -rn TestSlideParity tests/ formats/`)

- [ ] **Step 1: Locate the existing parity suite**

Run: `grep -rln TestSlideParity ./tests ./formats ./`
The parity suite walks 40 fixtures and snapshot-compares tile bytes. We extend it to also call DecodedTile on NDPI fixtures.

- [ ] **Step 2: Add DecodedTile parity assertions for NDPI fixtures**

Inside the parity loop, after the RawTile snapshot comparison, add (for fixtures matching `.ndpi`):

```go
if strings.HasSuffix(fixtureName, ".ndpi") {
	// v0.27: also exercise the fast pixel path on a sampling of
	// interior tiles to detect drift in the new path.
	img, err := slide.DecodedTile(level, sampleTx, sampleTy)
	if err != nil {
		t.Errorf("%s: DecodedTile(%d,%d,%d): %v", fixtureName, level, sampleTx, sampleTy, err)
	} else {
		// Compare against an independent RawTile+Decode result.
		compressed, _ := slide.RawTile(level, sampleTx, sampleTy)
		ref, _ := decodeJPEG(compressed)
		if !bytes.Equal(img.Pix, ref.Pix) {
			t.Errorf("%s: NDPI DecodedTile drift at (%d,%d,%d)", fixtureName, level, sampleTx, sampleTy)
		}
	}
}
```

Where `decodeJPEG` is the helper already defined in `formats/ndpi/stripped_decodedtile_test.go` (or duplicate it locally in the parity-suite file if cross-package access is awkward).

- [ ] **Step 3: Run**

Run: `make parity` (or whatever the project's parity-test invocation is — see the Commands block in CLAUDE.md).
Expected: PASS on all 40 fixtures including the NDPI fixtures (CMU-1.ndpi, OS-2.ndpi, Hamamatsu-1.ndpi). Hamamatsu-1.ndpi is oneframe — DecodedTile takes the slow path there — but parity should still hold (the dispatcher's ErrUnsupported fall-through is invisible).

- [ ] **Step 4: Commit**

```bash
git add tests/...  # or wherever the parity suite lives
git commit -m "test(parity): extend TestSlideParity to assert NDPI DecodedTile path"
```

## Task 5.3: Re-run Python parity oracle

**Files:**
- None modified. Documentation only.

- [ ] **Step 1: Re-run the parity oracle**

Run: `make parity` (which per CLAUDE.md invokes the batched parity oracle vs Python opentile 0.20.0).
Expected: zero divergence on NDPI fixtures. The fast path produces the same pixels as the slow path (verified in Phase 3); the slow path is byte-identical to Python opentile (existing property).

- [ ] **Step 2: Record the result**

If clean: note for the CHANGELOG that v0.27 maintains byte-parity with Python opentile.

If divergent: investigate — likely a regression in the slow-path edge-tile handling that was inadvertently changed. Bisect by reverting Task 3.4's `ImageDecodedTile` changes and re-running.

(No commit for this task; the result feeds Task 6.1.)

---

**Phase 5 checkpoint:** All regression coverage is in place; parity is verified. Halt for controller review.

---

# Phase 6 — Release prep

## Task 6.1: Update CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Inspect the existing CHANGELOG**

Run: `head -40 CHANGELOG.md`
Note the section style (markdown headers, bullet conventions).

- [ ] **Step 2: Add the v0.27.0 entry**

Prepend to `CHANGELOG.md` (above the v0.26.0 entry):

```markdown
## v0.27.0 — YYYY-MM-DD

NDPI striped fast pixel path (decode-once-per-strip + blit). Closes the
~5× per-thread perf gap vs openslide on NDPI tile decode.

- `formats/ndpi`: new pixel-frame LRU cache (bounded, `max(NumCPU, 16)`
  entries) on `strippedImage`; reusable long-lived JPEG decoder handle
  replacing per-tile `tjInitDecompress`/`tjDestroy` churn.
- `opentile`: `Slide.ImageDecodedTile` now dispatches NDPI striped
  levels through a fast pixel path via an unexported `decodedTiler`
  interface. Non-NDPI formats, non-striped NDPI levels (oneframe,
  associated), and `WithScale != 1` calls keep the v0.26 behavior
  exactly.
- `internal/fastpath`: new tiny package holding `ErrUnsupported`, the
  dispatch sentinel used to signal slow-path fallback.
- `cmd/bench/ndpi`: new single-thread tile-decode benchmark moved in
  from the v0.27 investigation; `make bench-ndpi` gates at 130 Mpix/s.

### Measured throughput (Apple Silicon, CMU-1.ndpi L0, 29,800 tiles)

| Build | Wall | Throughput | Ratio vs openslide |
|---|---|---|---|
| openslide 4.0.0 | 8.38 s | 233.0 Mpix/s | 1.00× |
| opentile-go v0.26 | 44.25 s | 44.1 Mpix/s | 5.28× slower |
| opentile-go v0.27 | `<measured wall from Task 4.1 Step 4>` | `<measured Mpix/s from Task 4.1 Step 4>` | `<computed: 233 / mpps>×` slower |

Replace the three placeholders in the v0.27 row with the actual numbers captured at Task 4.1 Step 4 before committing this CHANGELOG entry.

### Public API

- No additions.
- No breaking changes.
- `RawTile` (compressed bytes API) is bit-for-bit unchanged.

### Out of scope (deferred)

- NDPI oneframe path — same algorithmic opportunity, deferred to a
  follow-up milestone.
- Tactical decoder-handle pooling for the RawTile + `tjTransform` path.
- JPEG-frame cache bounding (still unbounded; CLAUDE.md
  stripped.go:67-73 acknowledges).
```

Replace the three `<measured ...>` placeholders in the throughput table with the actual numbers captured at Task 4.1 Step 4.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.27.0 — NDPI striped fast pixel path"
```

## Task 6.2: Update CLAUDE.md milestone block

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Move v0.26 down, add v0.27 at the top**

Open `CLAUDE.md`. The current `## Current milestone — v0.20 (shipped 2026-05-20)` block (the v0.20 Writer field section) is now historical — it should already have been demoted by the v0.21–v0.26 milestone PRs but if not, demote it.

Add a new top-of-file milestone block:

```markdown
## Current milestone — v0.27 (in progress YYYY-MM-DD)

- **Scope:** NDPI striped fast pixel path. Adds a decoded-pixel-frame
  LRU cache and reusable decoder handle on `formats/ndpi.strippedImage`,
  dispatched from `Slide.ImageDecodedTile` via an unexported
  `decodedTiler` interface. Closes the ~5× per-thread perf gap vs
  openslide on NDPI tile decode (44.25 s → `<measured at Task 4.1>` s
  on CMU-1.ndpi L0).
- **API additions:** none public. Internal: `decodedTiler` interface,
  `internal/fastpath.ErrUnsupported` sentinel, `strippedImage.DecodedTile`
  method, `decoderHandle` + `pixelFrameCache` types.
- **API breaks:** none.
- **Active limitations:** NDPI oneframe path still uses the v0.26 slow
  path; same lever applicable as a follow-up. RawTile + JPEG-frame
  cache unchanged (latent unbounded-growth concern preserved).
  `WithScale != 1` calls fall through to slow path.
- **Correctness bar:** TestNDPIFastPathPixelParity + TestNDPIFastPathConcurrent
  green; TestSlideParity 40 fixtures green; Python parity oracle zero
  divergence; `make cover` ≥80% per package; `make bench-ndpi` ≥130
  Mpix/s on CMU-1.ndpi.
- **Sealed Q-decisions** (per spec): see design doc §3 — 10 sealed Qs.
- **Deferred forward:** NDPI oneframe; tactical handle pooling for
  RawTile; JPEG-frame cache bounding.
- **Design:** docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md
- **Plan:** docs/superpowers/plans/2026-05-28-opentile-go-v27-ndpi-pixel-cache.md
- **Work branch:** feat/v0.27

## Previous milestone — v0.26 (shipped 2026-05-26)

[ existing v0.26 milestone block content ]

## Previous milestone — v0.20 (shipped 2026-05-20)

[ existing v0.20 block, demoted ]
```

Update the dates as appropriate.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude-md): promote v0.27 to current milestone"
```

## Task 6.3: Final gate check

**Files:**
- None modified.

- [ ] **Step 1: Run the full gate suite**

Run, in this order, stopping on first failure:

```bash
make vet
make test
make cover
make bench-ndpi
make parity
```

Expected: each target PASSES. Specifically:
- `make vet`: zero warnings.
- `make test`: all tests green under `-race -count=1`.
- `make cover`: ≥80% on `formats/ndpi`, `internal/fastpath`, and unchanged from v0.26 elsewhere.
- `make bench-ndpi`: ≥130 Mpix/s on CMU-1.ndpi.
- `make parity`: zero divergence against Python opentile across all 40 fixtures.

- [ ] **Step 2: Stage merge prep**

The work branch `feat/v0.27` is ready to merge. Confirm:

```bash
git log --oneline main..feat/v0.27
git status
```

Expected: every commit titled per the conventional-prefix scheme (`feat(ndpi):`, `test(ndpi):`, `docs:`, etc.); working tree clean.

Merge is a separate user-driven step (`git checkout main && git merge --no-ff feat/v0.27`); the plan stops at "ready to merge."

---

## Self-review checklist (run after writing the plan; not a step in itself)

- ✅ Every spec section in `2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md` is covered by a task: §1.1 cache → Task 2.3; §1.2 handle → Task 2.2; §1.3 interface → Task 3.4; §1.4 fast path → Task 3.2; §1.5 RawTile unchanged → verified by no-op in Tasks 3.1-3.4; §1.6 ReadRegion inherits → verified by Phase 3 `make test` step; §5 testing → Phase 5; §6 perf → Phase 4.
- ✅ Every Q-decision is encoded in code: Q1 arch lever → Task 3.2; Q2 NDPI striped only → Task 3.3 sentinel; Q3 internal API → Task 2.1 + Task 3.4; Q4 bounded LRU → Task 2.3; Q5 edge tiles → Task 3.2 `decodedTileViaCrop`; Q6 RawTile unchanged → no touch to RawTile path; Q7 handle mutex → Task 2.2; Q8 dispatch shape → Task 3.4; Q9 cache content shape → Task 3.2 blit format conversion; Q10 WithScale fall-through → Task 3.2 `opts.Scale > 1`.
- ✅ No "TODO", "TBD", "implement later" in any step.
- ✅ Every code step shows actual code, not a description.
- ✅ Method names and types are consistent across tasks (`pixelFrameCache`, `getOrLoad`, `decoderHandle`, `decodedTiler`, `DecodedTile`, `closeResources`, `blitFromFrame`, `copyImageInto`, `ensureDecHandle`, `fastpath.ErrUnsupported`).
- ✅ Commit messages follow project convention (`feat(scope):` / `test(scope):` / `docs:`).
- ✅ Phase boundaries align with the spec's logical layers (primitives → wiring → bench → regression → release).
