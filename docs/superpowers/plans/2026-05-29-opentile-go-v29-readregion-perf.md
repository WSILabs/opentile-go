# opentile-go v0.29 ReadRegion allocation-elimination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate ~82% of per-call heap allocation in `Slide.ReadRegion` across all formats via three independent, layered optimizations: skip `fillWhite` when fully in-bounds, reuse per-tile decode output via a module-level `sync.Pool`, and reuse NDPI pixel-frame buffers within `pixelFrameCache`'s eviction cycle.

**Architecture:** Three layers landed in three phases (each independently revertable):
- **Layer 1** modifies `slide_region.go` to move clip computation ahead of `fillWhite` and gate the fill on whether OOB pixels exist.
- **Layer 2** introduces a module-level `tileScratchPool sync.Map[scratchKey]*sync.Pool` in a new `decoded_tile_scratch.go`; `ReadRegion` borrows one scratch per call and uses `ImageDecodedTileInto`. Hard prereq: `strippedImage.DecodedTile` honors `opts.Dst`.
- **Layer 3** adds a `sync.Pool` per `pixelFrameCache` instance; evicted frames recycle into it and on-miss decodes pull from it via `opts.Dst`. Refactors `getOrLoad` → `getOrLoadInto`.

**Tech Stack:** Go 1.26+, stdlib `sync.Pool` + `sync.Map`. No new external dependencies. Pure-Go primitive over existing `decoder.Decoder` interface (which already supports `opts.Dst`).

**Reference docs (read before starting):**
- Spec (READ FIRST): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-29-opentile-go-v29-readregion-perf-design.md`
- v0.28 spec (decoder pool, the foundation): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-29-opentile-go-v28-cross-format-decoder-pool-design.md`
- v0.27 spec (NDPI fast path, what Layer 2 prereq modifies): `~/GitHub/opentile-go/docs/superpowers/specs/2026-05-28-opentile-go-v27-ndpi-pixel-cache-design.md`
- v0.28 plan execution pattern (the model for this plan): `~/GitHub/opentile-go/docs/superpowers/plans/2026-05-29-opentile-go-v28-cross-format-decoder-pool.md`
- Current ReadRegion: `~/GitHub/opentile-go/slide_region.go`
- Current NDPI strippedImage.DecodedTile: `~/GitHub/opentile-go/formats/ndpi/stripped.go`
- Current pixelFrameCache: `~/GitHub/opentile-go/formats/ndpi/pixel_cache.go`
- Current Slide.ImageDecodedTileInto fast-path dispatch: `~/GitHub/opentile-go/slide_decoded_tile.go`

**Branch:** `feat/v0.29` on opentile-go. Create from `main` at `2d09d49` (the v0.29 spec commit).

**CLAUDE.md invariants worth re-reading:**
- "Public API stable from v0.3." v0.29 adds zero exported symbols.
- "Lock-free hot path for metadata." Layer 2's `sync.Map` lookup + `sync.Pool` Get are micro-cost vs decode (~tens of ns vs ~tens of µs).
- "No cutting corners; no active users yet." If a layer's projected improvement doesn't materialize, halt and surface for JIT decision per spec §1.5 — do NOT auto-revert.

**Gate-failure policy (per spec §1.5):** Each layer ends with a measurement step. If the layer's projected improvement doesn't materialize, the plan halts for a human JIT decision: accept-and-document, investigate-and-retry, defer-to-v0.30, or reframe. The plan does NOT mechanically revert. Each layer's commits are self-contained so any layer can be selectively reverted post-decision.

---

## File Structure

**New files in opentile-go:**

```
decoded_tile_scratch.go              Module-level tile-Image sync.Pool keyed by
                                      (W, H, PixelFormat). borrowTileScratch and
                                      returnTileScratch helpers. Used by
                                      imageReadRegionImpl.

decoded_tile_scratch_test.go         Pool unit tests: reuse on Borrow-after-
                                      Return; size-keyed separation; 32-way
                                      concurrent under -race.
```

**Modified files in opentile-go:**

```
slide_region.go                      Layer 1: move clip computation ahead of
                                      fillWhite; gate fillWhite on
                                      fullyInBounds + non-edge-tile check.
                                      Layer 2: borrow scratch on entry,
                                      call ImageDecodedTileInto with scratch
                                      across tile loop, return on defer.

slide_region_test.go                 Layer 1 tests: TestReadRegionFullyInBounds
                                      PathSkipsFillWhite +
                                      TestReadRegionEdgeRegionForceFillWhite.
                                      Adds synthetic knownPixelReader stub.

slide_decoded_tile.go                Layer 2: ImageDecodedTileInto fast-path
                                      dispatch passes Dst into the decodedTiler
                                      call; skips copyImageInto when fast path
                                      wrote directly into dst.

formats/ndpi/stripped.go             Layer 2 prereq: strippedImage.DecodedTile
                                      honors opts.Dst when dimensions+format
                                      match.
                                      Layer 3 wiring: pixelCache callback
                                      passes scratch through to decoder via
                                      opts.Dst.

formats/ndpi/stripped_decodedtile_test.go
                                      Layer 2 prereq tests:
                                      TestNDPIFastPathHonorsDst +
                                      TestNDPIFastPathDstWrongSizeFallsBackToAlloc.

formats/ndpi/pixel_cache.go          Layer 3: scratchPool sync.Pool field on
                                      pixelFrameCache; evictIfOverLocked
                                      returns evicted entries; new
                                      getOrLoadInto(key, wantW, wantH, load)
                                      method routing scratch into load.

formats/ndpi/pixel_cache_test.go     Layer 3 tests:
                                      TestPixelCacheRecyclesEvictedFrames +
                                      TestPixelCacheConcurrentScratchSafe.

Makefile                             Bump MIN_NDPI_MPIXS 220 → 235; bump
                                      MIN_SVS_MPIXS 475 → measured value
                                      (Task 5.2 sets after Layer 2 bench).

CHANGELOG.md                         v0.29.0 entry with measured per-layer
                                      numbers.

CLAUDE.md                            Promote v0.29 to current milestone;
                                      demote v0.28 to previous.
```

No deletions. Pure-additive optimization.

---

# Phase 1 — Layer 1 (fillWhite skip)

## Task 1.1: Cut work branch + Layer 1 implementation

**Files:**
- Modify: `slide_region.go` (lines 69-79, the `imageReadRegionImpl` head)

- [ ] **Step 1: Cut the v0.29 branch**

Run:
```bash
cd ~/GitHub/opentile-go && git checkout main && git pull && git checkout -b feat/v0.29
```
Expected: branch created at `2d09d49` (v0.29 spec commit). Working tree clean.

- [ ] **Step 2: Re-read the current imageReadRegionImpl**

Run:
```bash
sed -n '66,110p' slide_region.go
```
Note the existing order: `lvl := s.r.Level(...)`, `w, h := dst.Width, dst.Height`, `fillWhite(dst)`, then the clip computation, then the tile loop. v0.29 moves clip computation ahead of fillWhite.

- [ ] **Step 3: Replace the imageReadRegionImpl head**

Edit `slide_region.go`. Replace:

```go
func (s *Slide) imageReadRegionImpl(image, level, x, y int, dst *decoder.Image, opts []DecodeOption) error {
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}

	w, h := dst.Width, dst.Height

	// Pre-fill the output with white. Out-of-bounds pixels stay white;
	// in-bounds pixels get overwritten by blitInto below.
	fillWhite(dst)

	// Clip the requested rectangle to the level's bounds.
	x0 := x
	y0 := y
	x1 := x + w
	y1 := y + h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > lvl.Size.W {
		x1 = lvl.Size.W
	}
	if y1 > lvl.Size.H {
		y1 = lvl.Size.H
	}
	if x0 >= x1 || y0 >= y1 {
		return ErrRegionEmpty
	}
```

with:

```go
func (s *Slide) imageReadRegionImpl(image, level, x, y int, dst *decoder.Image, opts []DecodeOption) error {
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}

	w, h := dst.Width, dst.Height

	// Clip the requested rectangle to the level's bounds.
	x0 := x
	y0 := y
	x1 := x + w
	y1 := y + h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > lvl.Size.W {
		x1 = lvl.Size.W
	}
	if y1 > lvl.Size.H {
		y1 = lvl.Size.H
	}
	if x0 >= x1 || y0 >= y1 {
		return ErrRegionEmpty
	}

	// v0.29 Layer 1: skip fillWhite when the requested region is fully
	// in-bounds AND no edge tile contributes (edge tiles return less
	// than nominal TileSize, and the blit only writes the actual
	// decoded extent — pre-existing dst contents would leak in
	// without a fillWhite prelude).
	fullyInBounds := x0 == x && y0 == y && x1 == x+w && y1 == y+h
	edgeTileX := x1 == lvl.Size.W && lvl.Size.W%lvl.TileSize.W != 0
	edgeTileY := y1 == lvl.Size.H && lvl.Size.H%lvl.TileSize.H != 0
	if !fullyInBounds || edgeTileX || edgeTileY {
		fillWhite(dst)
	}
```

- [ ] **Step 4: Build + run existing slide_region tests**

Run:
```bash
go build ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test -race -count=1 -run 'TestReadRegion|TestImageReadRegion' . 2>&1 | tail -10
```
Expected: existing ReadRegion tests pass. If any fail, the order-of-operations change has affected semantics — investigate.

- [ ] **Step 5: Commit**

```bash
git add slide_region.go
git commit -m "feat(slide): Layer 1 — skip fillWhite when region fully in-bounds (no edge tile)"
```

## Task 1.2: Layer 1 tests (synthetic minimalReader)

**Files:**
- Modify: `slide_region_test.go` (append tests + the test stub)

- [ ] **Step 1: Inspect existing slide_region_test.go for the stub pattern**

Run:
```bash
grep -n "type.*Reader struct\|minimalReader\|slideReader" slide_region_test.go slide_handle_test.go slide_best_level_test.go 2>&1 | head -10
```
Confirm: `slide_best_level_test.go` and `slide_handle_test.go` (v0.28) already have `slideReader` stub patterns. For Layer 1 we need a stub that returns deterministic pixel content per tile so we can detect contamination.

- [ ] **Step 2: Append the synthetic reader + Layer 1 tests**

Append to `slide_region_test.go`:

```go
// knownPixelReader is a synthetic slideReader returning every tile as
// uniform fill bytes. Used by Layer 1 tests to detect (a) whether
// fillWhite ran when it shouldn't have, and (b) whether the blit
// covered every in-bounds pixel.
type knownPixelReader struct {
	levelSize Size
	tileSize  Size
	fill      byte
}

func (r *knownPixelReader) Format() Format    { return Format("test") }
func (r *knownPixelReader) Images() []Image {
	return []Image{{Levels: []Level{{
		Index: 0, Size: r.levelSize, TileSize: r.tileSize,
		Compression: CompressionJPEG,
	}}}}
}
func (r *knownPixelReader) Level(image, level int) (Level, error) {
	if image != 0 || level != 0 {
		return Level{}, ErrLevelOutOfRange
	}
	return Level{
		Index: 0, Size: r.levelSize, TileSize: r.tileSize,
		Compression: CompressionJPEG,
	}, nil
}
func (r *knownPixelReader) Associated() []AssociatedImage { return nil }
func (r *knownPixelReader) Metadata() Metadata            { return Metadata{} }
func (r *knownPixelReader) ICCProfile() []byte            { return nil }
func (r *knownPixelReader) WarmLevel(image, level int) error { return nil }
func (r *knownPixelReader) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	return nil, errors.New("knownPixelReader: ImageRawTile unused — fast path consumed via DecodedTile interface")
}
func (r *knownPixelReader) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("knownPixelReader: ImageRawTileInto unused")
}
func (r *knownPixelReader) ImageTileMaxSize(image, level int) int    { return 1 }
func (r *knownPixelReader) ImageTilePrefix(image, level int) []byte  { return nil }
func (r *knownPixelReader) ImageTileBodyMaxSize(image, level int) int { return 1 }
func (r *knownPixelReader) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("knownPixelReader: ImageTileBodyInto unused")
}
func (r *knownPixelReader) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return nil, errors.New("knownPixelReader: ImageTileReader unused")
}
func (r *knownPixelReader) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[TilePos, TileResult] {
	return func(yield func(TilePos, TileResult) bool) {}
}
func (r *knownPixelReader) Close() error { return nil }

// ImageDecodedTile satisfies decodedTiler. Returns a tile-sized Image
// filled with r.fill (or writes into opts.Dst if provided, per
// v0.29 Layer 2 contract).
func (r *knownPixelReader) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	format := opts.Format
	if format == 0 {
		format = decoder.PixelFormatRGB
	}
	var out *decoder.Image
	if opts.Dst != nil &&
		opts.Dst.Width == r.tileSize.W &&
		opts.Dst.Height == r.tileSize.H &&
		opts.Dst.Format == format {
		out = opts.Dst
	} else {
		out = decoder.NewImageFormat(r.tileSize.W, r.tileSize.H, format)
	}
	for i := range out.Pix {
		out.Pix[i] = r.fill
	}
	return out, nil
}

func TestReadRegionFullyInBoundsPathSkipsFillWhite(t *testing.T) {
	s := &Slide{r: &knownPixelReader{
		levelSize: Size{W: 1024, H: 1024},
		tileSize:  Size{W: 256, H: 256},
		fill:      0x42,
	}}
	defer s.Close()

	dst := decoder.NewImageFormat(512, 512, decoder.PixelFormatRGB)
	// Pre-fill dst with a sentinel value that is NOT 0xFF (fillWhite)
	// and NOT 0x42 (the synthetic reader's tile fill). Survives only
	// if neither fillWhite nor blit touched the pixel.
	for i := range dst.Pix {
		dst.Pix[i] = 0xAA
	}

	// Fully-in-bounds region (origin 128,128; 512×512 inside 1024×1024
	// level with 256×256 tiles → no edge tile contribution).
	if err := s.ImageReadRegionInto(0, 0, 128, 128, dst); err != nil {
		t.Fatal(err)
	}

	for i, b := range dst.Pix {
		if b != 0x42 {
			t.Fatalf("dst[%d]=0x%02x; expected 0x42 (fillWhite was unnecessarily called OR blit missed a pixel)", i, b)
		}
	}
}

func TestReadRegionEdgeRegionForceFillWhite(t *testing.T) {
	s := &Slide{r: &knownPixelReader{
		levelSize: Size{W: 1024, H: 1024},
		tileSize:  Size{W: 256, H: 256},
		fill:      0x42,
	}}
	defer s.Close()

	dst := decoder.NewImageFormat(512, 512, decoder.PixelFormatRGB)
	for i := range dst.Pix {
		dst.Pix[i] = 0xAA
	}

	// Region crossing the right edge: x=768, w=512 → right half is OOB.
	if err := s.ImageReadRegionInto(0, 0, 768, 128, dst); err != nil {
		t.Fatal(err)
	}

	stride := dst.Stride
	bpp := 3
	for row := 0; row < 512; row++ {
		for col := 0; col < 256; col++ {
			off := row*stride + col*bpp
			if dst.Pix[off] != 0x42 {
				t.Fatalf("dst[r=%d,c=%d]=0x%02x; expected 0x42 (in-bounds region)", row, col, dst.Pix[off])
			}
		}
		for col := 256; col < 512; col++ {
			off := row*stride + col*bpp
			if dst.Pix[off] != 0xFF {
				t.Fatalf("dst[r=%d,c=%d]=0x%02x; expected 0xFF (OOB needs fillWhite)", row, col, dst.Pix[off])
			}
		}
	}
}
```

Required imports at the top of `slide_region_test.go` (add if not already present):

```go
import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)
```

- [ ] **Step 3: Run the two new tests**

Run:
```bash
go test -race -count=2 -run 'TestReadRegionFullyInBoundsPathSkipsFillWhite|TestReadRegionEdgeRegionForceFillWhite' .
```
Expected: PASS twice.

If `TestReadRegionFullyInBoundsPathSkipsFillWhite` fails because the synthetic reader's path isn't reached: the `Slide.r.(decodedTiler)` type assertion may fail because `knownPixelReader` doesn't implement the wrapper-delegation path. The fix is to ensure `Slide.r` is the bare `knownPixelReader` (not wrapped by `fileCloser`/`mmapCloser`); the test already does this via `&Slide{r: ...}` direct construction.

- [ ] **Step 4: Run the full opentile package test suite for regression**

Run:
```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test -race -count=1 -short . 2>&1 | tail -3
```
Expected: PASS. Confirms Layer 1 doesn't regress existing tests.

- [ ] **Step 5: Commit**

```bash
git add slide_region_test.go
git commit -m "test(slide): Layer 1 — fullyInBounds skips fillWhite, edge regions force it"
```

## Task 1.3: Layer 1 measurement + gate decision

**Files:**
- None modified.

- [ ] **Step 1: Run bench-ndpi-mt for Layer 1 baseline**

Run:
```bash
/tmp/bench-opentile-ndpi -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu) 2>&1 | tail -1
```
Record the multi-thread number. v0.28 baseline: ~539 Mpix/s. Layer 1 projection: ~570 Mpix/s (5% improvement).

If the bench binary is stale, rebuild first:
```bash
go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
```

- [ ] **Step 2: Run bench-svs-mt for Layer 1 baseline**

Run:
```bash
go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
/tmp/bench-opentile-svs -in sample_files/svs/CMU-1.svs -goroutines $(sysctl -n hw.ncpu) 2>&1 | tail -1
```
Record. v0.28 baseline: ~2121 Mpix/s. Layer 1 projection: ~2200 Mpix/s.

- [ ] **Step 3: Run single-thread benches**

Run:
```bash
make bench-ndpi 2>&1 | tail -2
make bench-svs 2>&1 | tail -2
```
Record. v0.28 baselines: ndpi ~251, svs ~596. Layer 1 projection: minor improvement (the bench's single-tile-per-call pattern barely exercises Layer 1).

- [ ] **Step 4: JIT decision checkpoint**

Layer 1's projected improvement is the smallest of the three layers (3-5% on multi-thread). If the measured numbers show:

- **≥ +3% on multi-thread**: layer worked as expected; record numbers (used in CHANGELOG Task 6.1) and proceed to Phase 2.
- **+0 to +3% on multi-thread**: layer was marginal but not regressive. Record honestly; proceed to Phase 2; CHANGELOG documents the smaller win.
- **Regression**: STOP. Surface the regression with measured numbers + JIT decision options (accept, investigate, defer, reframe). Do not auto-revert.

No commit for this task; the measurement results feed Task 6.1's CHANGELOG.

---

**Phase 1 checkpoint:** Layer 1 is live and tested. Halt for controller review of measured numbers before Phase 2.

---

# Phase 2 — Layer 2 prereq (NDPI fast-path Dst plumbing)

Layer 2's scratch pool only delivers value if `strippedImage.DecodedTile` writes into the borrowed scratch via `opts.Dst`. Phase 2 wires that prereq before Layer 2's own pool work.

## Task 2.1: NDPI fast path honors opts.Dst

**Files:**
- Modify: `formats/ndpi/stripped.go` (lines 292-338, `strippedImage.DecodedTile`)

- [ ] **Step 1: Re-read the current DecodedTile body**

Run:
```bash
sed -n '292,340p' formats/ndpi/stripped.go
```
Note the line `out := decoder.NewImageFormat(l.tileSize.W, l.tileSize.H, outFormat)` — this is what v0.29 changes to honor `opts.Dst`.

- [ ] **Step 2: Replace the output allocation block**

Edit `formats/ndpi/stripped.go`. Find:

```go
	outFormat := opts.Format
	if outFormat == 0 {
		outFormat = decoder.PixelFormatRGB
	}
	out := decoder.NewImageFormat(l.tileSize.W, l.tileSize.H, outFormat)
	blitFromFrame(pixFrame, left, top, l.tileSize.W, l.tileSize.H, out)
	return out, nil
}
```

Replace with:

```go
	outFormat := opts.Format
	if outFormat == 0 {
		outFormat = decoder.PixelFormatRGB
	}
	// v0.29 Layer 2 prereq: honor opts.Dst when caller provides a
	// buffer of matching dimensions+format. Falls back to allocation
	// otherwise (defensive — callers passing arbitrary Dst don't
	// panic).
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
}
```

- [ ] **Step 3: Build + run existing NDPI tests**

Run:
```bash
go build ./formats/ndpi/...
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=1 ./formats/ndpi/... 2>&1 | tail -3
```
Expected: all NDPI tests PASS. The new Dst-honoring branch is dormant (no caller passes Dst yet); existing tests prove no regression.

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/stripped.go
git commit -m "feat(ndpi): Layer 2 prereq — strippedImage.DecodedTile honors opts.Dst"
```

## Task 2.2: NDPI fast-path Dst tests

**Files:**
- Modify: `formats/ndpi/stripped_decodedtile_test.go` (append)

- [ ] **Step 1: Append the two Dst tests**

Append to `formats/ndpi/stripped_decodedtile_test.go`:

```go
// TestNDPIFastPathHonorsDst confirms that a pre-allocated dst is
// returned (not a fresh allocation) when its dimensions+format
// match the level's tile shape. Critical prereq for Layer 2.
func TestNDPIFastPathHonorsDst(t *testing.T) {
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
	dst := decoder.NewImageFormat(l0.TileSize.W, l0.TileSize.H, decoder.PixelFormatRGB)

	if err := slide.DecodedTileInto(0, 0, 0, dst); err != nil {
		t.Fatalf("DecodedTileInto: %v", err)
	}

	// At least one pixel should be non-zero (the fixture has real
	// content; an unwritten dst would still be all-zeros from the
	// fresh NewImageFormat allocation).
	allZero := true
	for _, b := range dst.Pix {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("dst pixels are all zero; fast path may not have written into dst")
	}
}

// TestNDPIFastPathDstWrongSizeFallsBackToAlloc confirms defensive
// behavior: a mismatched-size dst is silently ignored, the fast
// path allocates fresh, and no panic occurs.
func TestNDPIFastPathDstWrongSizeFallsBackToAlloc(t *testing.T) {
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
	// Wrong size: 100×100 instead of TileSize.W × TileSize.H.
	wrongDst := decoder.NewImageFormat(100, 100, decoder.PixelFormatRGB)
	for i := range wrongDst.Pix {
		wrongDst.Pix[i] = 0x55 // sentinel
	}

	// DecodedTileInto with wrong-size dst: returns
	// decoder.ErrDestinationSize from the underlying decoder OR (if
	// the fast path took the allocation fallback) succeeds. Either
	// is acceptable — neither path panics or corrupts wrongDst.
	err = slide.DecodedTileInto(0, 0, 0, wrongDst)
	if err == nil {
		// Allocation-fallback path: wrongDst was never written;
		// sentinel survives.
		for i, b := range wrongDst.Pix {
			if b != 0x55 {
				t.Fatalf("wrongDst[%d]=0x%02x; expected sentinel 0x55 to survive (fast path should have allocated separately)", i, b)
			}
		}
	}
	// If err != nil, decoder.ErrDestinationSize is the expected reason;
	// no further assertion needed — the test's purpose is "no panic".
}
```

Required imports at the top of `formats/ndpi/stripped_decodedtile_test.go` (add if not already present): `"github.com/wsilabs/opentile-go/decoder"`.

- [ ] **Step 2: Run the two new tests**

Run:
```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=2 -run 'TestNDPIFastPathHonorsDst|TestNDPIFastPathDstWrongSizeFallsBackToAlloc' ./formats/ndpi/
```
Expected: PASS twice.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/stripped_decodedtile_test.go
git commit -m "test(ndpi): Layer 2 prereq — fast-path Dst honor + wrong-size fallback"
```

---

**Phase 2 checkpoint:** NDPI fast path now honors opts.Dst on the v0.27 hot path. Phase 3 wires it.

---

# Phase 3 — Layer 2 (per-tile output sync.Pool)

## Task 3.1: Create decoded_tile_scratch.go + tests

**Files:**
- Create: `decoded_tile_scratch.go`
- Create: `decoded_tile_scratch_test.go`

- [ ] **Step 1: Write the failing tests**

Create `decoded_tile_scratch_test.go`:

```go
package opentile

import (
	"sync"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestTileScratchPoolReuse(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	returnTileScratch(a)
	b := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	if a != b {
		t.Fatal("expected sync.Pool to reuse the returned scratch on next Borrow")
	}
	returnTileScratch(b)
}

func TestTileScratchPoolSizeKeyed(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	c := borrowTileScratch(512, 512, decoder.PixelFormatRGB)
	if a == c {
		t.Fatal("different-sized scratches should not share buffers")
	}
	returnTileScratch(a)
	returnTileScratch(c)
}

func TestTileScratchPoolFormatKeyed(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	c := borrowTileScratch(256, 256, decoder.PixelFormatRGBA)
	if a == c {
		t.Fatal("different-format scratches should not share buffers")
	}
	returnTileScratch(a)
	returnTileScratch(c)
}

func TestTileScratchPoolConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
				returnTileScratch(s)
			}
		}()
	}
	wg.Wait()
}

func TestTileScratchPoolReturnNilSafe(t *testing.T) {
	// Must not panic.
	returnTileScratch(nil)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestTileScratchPool' .`
Expected: FAIL — `undefined: borrowTileScratch`, `undefined: returnTileScratch`.

- [ ] **Step 3: Implement the scratch pool**

Create `decoded_tile_scratch.go`:

```go
package opentile

import (
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// scratchKey identifies a buffer size class for the tile-Image
// sync.Pool. Two formats (RGB, RGBA) × N tile sizes per format mean
// we key on (W, H, Format) — but in practice every level of a single
// Slide uses the same TileSize, so the per-Slide active key set is
// small (typically 1 or 2).
type scratchKey struct {
	w, h   int
	format decoder.PixelFormat
}

// tileScratchPool is the package-level pool of *decoder.Image
// instances reused as per-tile decode-Into scratch buffers in
// imageReadRegionImpl. Module-level (not per-Slide) so multiple
// Slides sharing a layout share buffers transparently.
//
// Members are stateless after each blit; sync.Pool auto-shrinks
// under GC pressure. No Slide.Close drain required.
var tileScratchPool sync.Map // scratchKey -> *sync.Pool of *decoder.Image

// borrowTileScratch returns a *decoder.Image of (w, h, format) from
// the pool, or allocates a fresh one if the pool is empty. The
// returned Image's Pix is NOT zeroed — caller must fully overwrite
// before reading.
//
// Added in v0.29.
func borrowTileScratch(w, h int, format decoder.PixelFormat) *decoder.Image {
	key := scratchKey{w, h, format}
	pi, _ := tileScratchPool.LoadOrStore(key, &sync.Pool{
		New: func() any {
			return decoder.NewImageFormat(w, h, format)
		},
	})
	return pi.(*sync.Pool).Get().(*decoder.Image)
}

// returnTileScratch returns a scratch Image to the pool. Safe with
// nil. Caller MUST stop reading from the Image after Return.
//
// Added in v0.29.
func returnTileScratch(img *decoder.Image) {
	if img == nil {
		return
	}
	key := scratchKey{img.Width, img.Height, img.Format}
	pi, ok := tileScratchPool.Load(key)
	if !ok {
		return // unknown key; let GC reclaim
	}
	pi.(*sync.Pool).Put(img)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -count=3 -run 'TestTileScratchPool' .`
Expected: all 5 tests PASS across 3 iterations under -race.

- [ ] **Step 5: Commit**

```bash
git add decoded_tile_scratch.go decoded_tile_scratch_test.go
git commit -m "feat(slide): Layer 2 — module-level tile-Image sync.Pool keyed by (W,H,Format)"
```

## Task 3.2: Wire imageReadRegionImpl to use the scratch pool

**Files:**
- Modify: `slide_region.go` (the tile loop in `imageReadRegionImpl`)

- [ ] **Step 1: Re-read the current tile loop**

Run:
```bash
sed -n '100,146p' slide_region.go
```
Note the existing loop uses `s.ImageDecodedTile(...)` per tile and trusts `tileImg.Width / .Height` as the actual decoded extent.

- [ ] **Step 2: Replace the tile loop**

Edit `slide_region.go`. Find:

```go
	// Tile grid covering the clipped rectangle.
	txMin := x0 / lvl.TileSize.W
	tyMin := y0 / lvl.TileSize.H
	txMax := (x1 - 1) / lvl.TileSize.W
	tyMax := (y1 - 1) / lvl.TileSize.H

	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			tileImg, err := s.ImageDecodedTile(image, level, tx, ty, opts...)
			if err != nil {
				return fmt.Errorf("opentile: decode tile (%d,%d) at level %d: %w", tx, ty, level, err)
			}
			tileX := tx * lvl.TileSize.W
			tileY := ty * lvl.TileSize.H
			tileW := lvl.TileSize.W
			tileH := lvl.TileSize.H
			// Edge tiles may have smaller actual decoded extents than
			// the nominal TileSize when the level's dimensions aren't
			// a multiple of TileSize. Trust the decoded image's own
			// reported size.
			if tileImg.Width < tileW {
				tileW = tileImg.Width
			}
			if tileImg.Height < tileH {
				tileH = tileImg.Height
			}
			// Intersect tile bounds with the clipped output region.
			ix0 := maxInt(tileX, x0)
			iy0 := maxInt(tileY, y0)
			ix1 := minInt(tileX+tileW, x1)
			iy1 := minInt(tileY+tileH, y1)
			if ix0 >= ix1 || iy0 >= iy1 {
				continue
			}
			srcX := ix0 - tileX
			srcY := iy0 - tileY
			srcW := ix1 - ix0
			srcH := iy1 - iy0
			dstX := ix0 - x
			dstY := iy0 - y
			blitInto(tileImg, srcX, srcY, srcW, srcH, dst, dstX, dstY)
		}
	}
	return nil
}
```

Replace with:

```go
	// Tile grid covering the clipped rectangle.
	txMin := x0 / lvl.TileSize.W
	tyMin := y0 / lvl.TileSize.H
	txMax := (x1 - 1) / lvl.TileSize.W
	tyMax := (y1 - 1) / lvl.TileSize.H

	// v0.29 Layer 2: borrow a scratch *decoder.Image once per call,
	// reuse across every tile in the loop. Returned on defer.
	// Format follows dst's so the decoder writes into the right
	// pixel layout.
	scratch := borrowTileScratch(lvl.TileSize.W, lvl.TileSize.H, dst.Format)
	defer returnTileScratch(scratch)

	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			if err := s.ImageDecodedTileInto(image, level, tx, ty, scratch, opts...); err != nil {
				return fmt.Errorf("opentile: decode tile (%d,%d) at level %d: %w", tx, ty, level, err)
			}
			tileX := tx * lvl.TileSize.W
			tileY := ty * lvl.TileSize.H
			tileW := lvl.TileSize.W
			tileH := lvl.TileSize.H
			// Edge tiles may decode to less than nominal TileSize. The
			// decoder writes only the actual extent into scratch; the
			// scratch's Width/Height are the nominal pool size, which
			// stays constant across reuse. Use the level geometry to
			// derive the actual decoded extent.
			actualW := lvl.Size.W - tileX
			if actualW > lvl.TileSize.W {
				actualW = lvl.TileSize.W
			}
			actualH := lvl.Size.H - tileY
			if actualH > lvl.TileSize.H {
				actualH = lvl.TileSize.H
			}
			if actualW < tileW {
				tileW = actualW
			}
			if actualH < tileH {
				tileH = actualH
			}
			// Intersect tile bounds with the clipped output region.
			ix0 := maxInt(tileX, x0)
			iy0 := maxInt(tileY, y0)
			ix1 := minInt(tileX+tileW, x1)
			iy1 := minInt(tileY+tileH, y1)
			if ix0 >= ix1 || iy0 >= iy1 {
				continue
			}
			srcX := ix0 - tileX
			srcY := iy0 - tileY
			srcW := ix1 - ix0
			srcH := iy1 - iy0
			dstX := ix0 - x
			dstY := iy0 - y
			blitInto(scratch, srcX, srcY, srcW, srcH, dst, dstX, dstY)
		}
	}
	return nil
}
```

The key behavioral change: instead of trusting `tileImg.Width / .Height` (which previously reflected the actual decoded extent), the code now computes the actual extent from the level geometry (`lvl.Size - tileOrigin`, capped at `lvl.TileSize`). This is necessary because the reused scratch has constant Width/Height across loop iterations — its dimensions no longer track the per-tile decoded extent.

- [ ] **Step 3: Build + run all opentile tests**

Run:
```bash
go build ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test -race -count=1 -short ./... 2>&1 | tail -10
```
Expected: PASS across all packages. The new scratch-pool path is now live; existing tests prove no behavioral regression.

If `tests/integration_test.go` fails on any fixture (parity oracle), investigate the extent-computation change — it should match the pre-v0.29 behavior bit-for-bit.

- [ ] **Step 4: Commit**

```bash
git add slide_region.go
git commit -m "feat(slide): Layer 2 — imageReadRegionImpl uses scratch pool via ImageDecodedTileInto"
```

## Task 3.3: Wire Slide.ImageDecodedTileInto fast-path dispatch

**Files:**
- Modify: `slide_decoded_tile.go` (the `ImageDecodedTileInto` body)

- [ ] **Step 1: Re-read the current ImageDecodedTileInto**

Run:
```bash
sed -n '95,140p' slide_decoded_tile.go
```
Note: v0.28's dispatch passes `Format` + `Scale` into the fast path but NOT `Dst`; it then `copyImageInto(out, dst)` when the fast path succeeds. v0.29 passes `Dst: dst`, and if the fast path returns `dst` (wrote in-place), skips the copy.

- [ ] **Step 2: Replace the fast-path dispatch block**

Edit `slide_decoded_tile.go`. Find:

```go
func (s *Slide) ImageDecodedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
		})
		if err == nil {
			return copyImageInto(out, dst)
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return err
		}
	}
```

Replace with:

```go
func (s *Slide) ImageDecodedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		// v0.29 Layer 2: pass dst as opts.Dst so the fast path can
		// write directly into it (eliminating the v0.28 copy step).
		// Fast-path impls that ignore Dst still return a fresh Image;
		// the out != dst branch below covers that defensively.
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
			Dst:    dst,
		})
		if err == nil {
			if out == dst {
				return nil
			}
			return copyImageInto(out, dst)
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return err
		}
	}
```

- [ ] **Step 3: Run NDPI fast-path tests + the full opentile suite**

Run:
```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=1 -run 'TestNDPIFastPath|TestSlide' ./formats/ndpi/ . 2>&1 | tail -10
```
Expected: every test PASS. The fast path now writes through dst when caller-provided, which is the v0.29 Layer 2 hot path under bench-ndpi-mt.

- [ ] **Step 4: Commit**

```bash
git add slide_decoded_tile.go
git commit -m "feat(slide): Layer 2 — ImageDecodedTileInto passes dst into fast path, skips copy"
```

## Task 3.4: Layer 2 measurement + JIT checkpoint

**Files:**
- None modified.

- [ ] **Step 1: Run multi-thread benches**

Run:
```bash
go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
/tmp/bench-opentile-ndpi -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu) 2>&1 | tail -1
/tmp/bench-opentile-svs -in sample_files/svs/CMU-1.svs -goroutines $(sysctl -n hw.ncpu) 2>&1 | tail -1
```
Record both. Projections:
- bench-ndpi-mt: 539 → ~700 Mpix/s (per-tile alloc gone)
- bench-svs-mt: 2121 → ~2400 Mpix/s

- [ ] **Step 2: Run single-thread benches**

Run:
```bash
make bench-ndpi 2>&1 | tail -2
make bench-svs 2>&1 | tail -2
```
Projections: bench-ndpi 251 → ~260, bench-svs 596 → ~620.

- [ ] **Step 3: Capture allocation profile for verification**

Run:
```bash
/tmp/bench-opentile-ndpi -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu) -memprofile /tmp/v29-layer2.memprof 2>&1 | tail -1
go tool pprof -top -nodecount=12 -sample_index=alloc_space /tmp/v29-layer2.memprof 2>&1 | head -25
```
Expected: `decoder.NewImageFormat` total drops from ~38.7 GB (v0.28) to ~17 GB (Layer 2 eliminates the 22 GB per-tile output portion).

If `NewImageFormat` is still near 38 GB: the scratch pool isn't being hit. Investigate via:
- Is `Slide.ImageDecodedTileInto` actually getting called from ReadRegion? (yes if Layer 2 wiring is correct)
- Is `strippedImage.DecodedTile` honoring `opts.Dst`? (Task 2.1 was the prereq; if measurement here shows no improvement, the prereq isn't wired)

- [ ] **Step 4: JIT decision checkpoint**

Per spec §1.5, evaluate:

- **bench-ndpi-mt ≥ 700 Mpix/s and bench-svs-mt ≥ 2400 Mpix/s**: Layer 2 delivered. Proceed to Phase 4.
- **bench-ndpi-mt ≥ 600 Mpix/s OR bench-svs-mt ≥ 2300 Mpix/s** (partial win): Layer 2 delivered something. Record honestly; CHANGELOG documents the actual numbers. Proceed to Phase 4.
- **No improvement or regression**: STOP. Surface measurements + alloc profile for JIT decision.

No commit for this task; numbers feed Task 6.1.

---

**Phase 3 checkpoint:** Layer 2 live. Halt for controller review of measured numbers before Phase 4.

---

# Phase 4 — Layer 3 (pixelCache frame sync.Pool)

## Task 4.1: Refactor pixelFrameCache.evictIfOverLocked to return evicted entries

**Files:**
- Modify: `formats/ndpi/pixel_cache.go` (lines 102-118, `evictIfOverLocked`)

- [ ] **Step 1: Re-read the current evictIfOverLocked**

Run:
```bash
sed -n '99,120p' formats/ndpi/pixel_cache.go
```
Note: current signature is `func (c *pixelFrameCache) evictIfOverLocked()` (returns nothing). Layer 3 changes it to return `[]*pixelFrameEntry` so callers can route survivors to the scratch pool.

- [ ] **Step 2: Update evictIfOverLocked to return survivors**

Edit `formats/ndpi/pixel_cache.go`. Replace:

```go
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
```

with:

```go
// evictIfOverLocked must be called with c.mu held. Evicts entries
// from the back of order until len(entries) <= capacity. Returns the
// evicted entries so callers can route their populated *pix into a
// scratch pool (v0.29 Layer 3).
func (c *pixelFrameCache) evictIfOverLocked() []*pixelFrameEntry {
	var evicted []*pixelFrameEntry
	for len(c.entries) > c.capacity {
		back := c.order.Back()
		if back == nil {
			return evicted
		}
		key := back.Value.(frameKey)
		if e, ok := c.entries[key]; ok {
			evicted = append(evicted, e)
		}
		c.order.Remove(back)
		delete(c.entries, key)
	}
	return evicted
}
```

- [ ] **Step 3: Update the sole caller (getOrLoad)**

Find the call in `getOrLoad`:

```go
	c.evictIfOverLocked()
	c.mu.Unlock()
```

Change to:

```go
	evicted := c.evictIfOverLocked()
	c.mu.Unlock()
	_ = evicted // routed to scratch pool in Task 4.2
```

- [ ] **Step 4: Build + run existing pixelCache tests**

Run:
```bash
go build ./formats/ndpi/...
go test -race -count=2 -run 'TestPixelCache' ./formats/ndpi/
```
Expected: PASS. The signature change is internal; behavior unchanged.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/pixel_cache.go
git commit -m "refactor(ndpi): Layer 3 prep — evictIfOverLocked returns evicted entries"
```

## Task 4.2: Add scratchPool + getOrLoadInto

**Files:**
- Modify: `formats/ndpi/pixel_cache.go`

- [ ] **Step 1: Add scratchPool field to pixelFrameCache**

Edit `formats/ndpi/pixel_cache.go`. Find:

```go
type pixelFrameCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[frameKey]*pixelFrameEntry
	order    *list.List // values are frameKey; front = MRU
}
```

Replace with:

```go
type pixelFrameCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[frameKey]*pixelFrameEntry
	order    *list.List // values are frameKey; front = MRU

	// v0.29 Layer 3: pool of evicted decoded frames. Reused by the
	// next getOrLoadInto call that needs a same-sized buffer.
	// sync.Pool auto-shrinks under GC pressure. Per-cache (per-
	// strippedImage) instance so we never get cross-level size
	// mismatches.
	scratchPool sync.Pool
}
```

- [ ] **Step 2: Add getOrLoadInto method**

Append to `formats/ndpi/pixel_cache.go` (after the existing `getOrLoad`):

```go
// getOrLoadInto is the v0.29 Layer 3 variant of getOrLoad. The load
// callback receives a scratch *decoder.Image (or nil if the pool is
// empty); decoders that honor opts.Dst can write into it,
// eliminating the per-miss allocation. Evicted entries route into
// the scratch pool best-effort (matching size only).
//
// wantW / wantH describe the expected frame dimensions; the pool
// only serves matching-size buffers. Mismatches are GC'd.
//
// Added in v0.29.
func (c *pixelFrameCache) getOrLoadInto(
	key frameKey,
	wantW, wantH int,
	load func(scratch *decoder.Image) (*decoder.Image, error),
) (*decoder.Image, error) {
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
	evicted := c.evictIfOverLocked()
	c.mu.Unlock()

	// Route evicted entries into the scratch pool (size-matched only).
	for _, ev := range evicted {
		if ev.pix != nil &&
			ev.pix.Width == wantW &&
			ev.pix.Height == wantH {
			c.scratchPool.Put(ev.pix)
		}
	}

	// Try to borrow a same-sized scratch from the pool.
	var scratch *decoder.Image
	if v := c.scratchPool.Get(); v != nil {
		s := v.(*decoder.Image)
		if s.Width == wantW && s.Height == wantH {
			scratch = s
		}
		// Mismatched-size pool drop: GC reclaims.
	}

	pix, err := load(scratch)
	if err != nil {
		c.mu.Lock()
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
```

- [ ] **Step 3: Build + run existing pixelCache tests**

Run:
```bash
go build ./formats/ndpi/...
go test -race -count=2 -run 'TestPixelCache' ./formats/ndpi/
```
Expected: PASS. The new `getOrLoadInto` is dormant until Task 4.3 wires it; existing `getOrLoad` semantics unchanged.

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/pixel_cache.go
git commit -m "feat(ndpi): Layer 3 — pixelFrameCache.getOrLoadInto + scratchPool"
```

## Task 4.3: Switch strippedImage.DecodedTile to use getOrLoadInto

**Files:**
- Modify: `formats/ndpi/stripped.go` (the DecodedTile pixelCache callback)

- [ ] **Step 1: Locate the current pixelCache call site**

Run:
```bash
grep -n "pixelCache.getOrLoad\|pixelCache\." formats/ndpi/stripped.go | head -5
```
Expected: one hit, the callback inside `DecodedTile`.

- [ ] **Step 2: Switch from getOrLoad to getOrLoadInto**

Edit `formats/ndpi/stripped.go`. Find:

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

Replace with:

```go
	pixFrame, err := l.pixelCache.getOrLoadInto(key, frameSize.W, frameSize.H,
		func(scratch *decoder.Image) (*decoder.Image, error) {
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
			// v0.29 Layer 3: pass scratch through to decoder via opts.Dst.
			// nil scratch (pool empty) is handled by the decoder's own
			// allocation path.
			return dec.Decode(jpegFrame, decoder.DecodeOptions{
				Format: decoder.PixelFormatRGB,
				Dst:    scratch,
			})
		})
```

- [ ] **Step 3: Build + run NDPI tests**

Run:
```bash
go build ./formats/ndpi/...
OPENTILE_TESTDIR="$PWD/sample_files" go test -tags='cgo' -race -count=2 ./formats/ndpi/... 2>&1 | tail -5
```
Expected: every NDPI test PASS twice under -race. The frame-scratch reuse is now live on the v0.27 hot path.

If `TestNDPIFastPathPixelParity` or `TestNDPIFastPathConcurrent` regresses: the scratch's pre-existing content is leaking into the decode output. Investigate the decoder's Dst-honoring behavior — `cgoDecoder.Decode` is expected to fully overwrite the Dst buffer; if it doesn't, the pool can't safely reuse it.

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/stripped.go
git commit -m "feat(ndpi): Layer 3 — strippedImage.DecodedTile wires scratch via getOrLoadInto"
```

## Task 4.4: Layer 3 tests

**Files:**
- Modify: `formats/ndpi/pixel_cache_test.go` (append)

- [ ] **Step 1: Append the recycling test**

Append to `formats/ndpi/pixel_cache_test.go`:

```go
// TestPixelCacheRecyclesEvictedFrames verifies the v0.29 Layer 3
// promise: evicted frames whose size matches the next request are
// pulled from the scratch pool, not freshly allocated.
//
// The test uses an instrumented load callback that detects whether
// the provided scratch is non-nil after the first eviction cycle.
func TestPixelCacheRecyclesEvictedFrames(t *testing.T) {
	c := newPixelFrameCache(2)
	keys := []frameKey{
		{posX: 0, posY: 0, w: 16, h: 16},
		{posX: 16, posY: 0, w: 16, h: 16},
		{posX: 32, posY: 0, w: 16, h: 16}, // triggers eviction of keys[0]
	}

	scratchReceived := 0
	load := func(scratch *decoder.Image) (*decoder.Image, error) {
		if scratch != nil {
			scratchReceived++
			// Reuse the scratch buffer (decoder would normally write
			// into it).
			return scratch, nil
		}
		return mkImg(16, 16), nil
	}

	// First two calls fill the cache; no scratch should be available yet.
	for i := 0; i < 2; i++ {
		_, err := c.getOrLoadInto(keys[i], 16, 16, load)
		if err != nil {
			t.Fatal(err)
		}
	}
	if scratchReceived != 0 {
		t.Fatalf("scratchReceived=%d after first 2 calls; expected 0", scratchReceived)
	}

	// Third call evicts keys[0]; keys[0]'s frame should route to pool.
	_, err := c.getOrLoadInto(keys[2], 16, 16, load)
	if err != nil {
		t.Fatal(err)
	}

	// Fourth call: trigger another eviction, then ask for a frame —
	// the pool should now serve a buffer.
	_, err = c.getOrLoadInto(frameKey{posX: 48, posY: 0, w: 16, h: 16}, 16, 16, load)
	if err != nil {
		t.Fatal(err)
	}
	if scratchReceived == 0 {
		t.Fatal("scratchReceived=0 after eviction cycle; pool not delivering recycled buffer")
	}
}

// TestPixelCacheConcurrentScratchSafe verifies no race / no
// pixel-corruption under fanout with scratch recycling.
func TestPixelCacheConcurrentScratchSafe(t *testing.T) {
	c := newPixelFrameCache(4)
	load := func(scratch *decoder.Image) (*decoder.Image, error) {
		if scratch != nil {
			return scratch, nil
		}
		return mkImg(16, 16), nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				k := frameKey{posX: ((seed*j)%32)*16, posY: 0, w: 16, h: 16}
				_, err := c.getOrLoadInto(k, 16, 16, load)
				if err != nil {
					t.Errorf("getOrLoadInto: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run the two new tests**

Run:
```bash
go test -race -count=3 -run 'TestPixelCacheRecyclesEvictedFrames|TestPixelCacheConcurrentScratchSafe' ./formats/ndpi/
```
Expected: PASS across 3 iterations under -race.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/pixel_cache_test.go
git commit -m "test(ndpi): Layer 3 — scratch recycling + concurrent fanout safety"
```

## Task 4.5: Layer 3 measurement + JIT checkpoint

**Files:**
- None modified.

- [ ] **Step 1: Run multi-thread benches**

Run:
```bash
go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
/tmp/bench-opentile-ndpi -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu) 2>&1 | tail -1
```
Projection: bench-ndpi-mt post-Layer-3 ≥ 800-900 Mpix/s (Layer 2 baseline + Layer 3's frame-alloc elimination).

- [ ] **Step 2: Capture allocation profile**

Run:
```bash
/tmp/bench-opentile-ndpi -in sample_files/ndpi/CMU-1.ndpi -goroutines $(sysctl -n hw.ncpu) -memprofile /tmp/v29-layer3.memprof 2>&1 | tail -1
go tool pprof -top -nodecount=12 -sample_index=alloc_space /tmp/v29-layer3.memprof 2>&1 | head -25
```
Expected: total allocation drops from ~17 GB (Layer 2 baseline) to ~7 GB (Layer 3 eliminates the 11.4 GB pixelCache frame portion).

- [ ] **Step 3: Single-thread bench-ndpi**

Run: `make bench-ndpi 2>&1 | tail -2`
Should still pass at ≥220 (the v0.28 gate); the new ≥235 v0.29 gate is set in Task 5.

- [ ] **Step 4: JIT decision checkpoint**

Per spec §1.5:

- **bench-ndpi-mt ≥ 800 Mpix/s + alloc profile shows frame-alloc dropped**: Layer 3 delivered. Proceed.
- **Partial win**: Record honestly. CHANGELOG documents actual numbers.
- **No improvement**: STOP. Investigate via pool-hit-rate analysis (the load callback's `scratch != nil` count should be high; if it's low, the pool isn't being populated correctly).

No commit for this task.

---

**Phase 4 checkpoint:** Layer 3 live. Halt for controller review before Phase 5.

---

# Phase 5 — Gate tightening

## Task 5.1: Tighten bench-ndpi gate

**Files:**
- Modify: `Makefile` (the `MIN_NDPI_MPIXS` variable)

- [ ] **Step 1: Run bench-ndpi 3 times for variance baseline**

Run:
```bash
for i in 1 2 3; do make bench-ndpi 2>&1 | grep Mpix; done
```
Record the lowest single-thread throughput observed. Call it `M_ndpi`.

- [ ] **Step 2: Bump MIN_NDPI_MPIXS**

Edit `Makefile`. Find:

```make
MIN_NDPI_MPIXS ?= 220
```

Change to (substituting `M_ndpi × 0.95` rounded down to integer; e.g., if M_ndpi = 252, use 240):

```make
MIN_NDPI_MPIXS ?= <computed>
```

If the measured single-thread is roughly unchanged from v0.28 (~251), keep the v0.29 spec's `235` value:

```make
MIN_NDPI_MPIXS ?= 235
```

- [ ] **Step 3: Verify gate passes**

Run: `make bench-ndpi 2>&1 | tail -2`
Expected: PASS at the new threshold.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat(make): tighten MIN_NDPI_MPIXS to v0.29 baseline floor"
```

## Task 5.2: Tighten bench-svs gate

**Files:**
- Modify: `Makefile` (the `MIN_SVS_MPIXS` variable)

- [ ] **Step 1: Run bench-svs 3 times**

Run:
```bash
for i in 1 2 3; do make bench-svs 2>&1 | grep Mpix; done
```
Record the lowest. Call it `M_svs`. v0.28 baseline was ~596; Layer 2 projects ~620.

- [ ] **Step 2: Bump MIN_SVS_MPIXS**

Edit `Makefile`. Find:

```make
MIN_SVS_MPIXS ?= 475
```

Change to `floor(M_svs × 0.95)` (e.g., M_svs = 615 → 584):

```make
MIN_SVS_MPIXS ?= <computed>
```

- [ ] **Step 3: Verify**

Run: `make bench-svs 2>&1 | tail -2`
Expected: PASS at the new threshold.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat(make): tighten MIN_SVS_MPIXS to v0.29 baseline floor"
```

---

# Phase 6 — Documentation

## Task 6.1: CHANGELOG v0.29.0 entry

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Compile measured numbers from Phases 1, 3, 4**

Pull from:
- Task 1.3 records (Layer 1)
- Task 3.4 records (Layer 2)
- Task 4.5 records (Layer 3)
- Task 5.1 and 5.2 (final gates)

- [ ] **Step 2: Add v0.29.0 entry**

Edit `CHANGELOG.md`. Find:

```markdown
## [Unreleased]

## [0.28.0] — 2026-05-29
```

Replace with (substituting measured numbers for the `<measured ...>` placeholders):

```markdown
## [Unreleased]

## [0.29.0] — 2026-05-29

ReadRegion allocation-elimination perf milestone. Three independent
layered optimizations targeting the 38% multi-thread CPU spent in
`runtime.pthread_cond_signal` (GC sweep under bench-ndpi-mt's
39 GB/run allocation rate):

- **Layer 1**: skip `fillWhite(dst)` when ReadRegion's requested
  region is fully in-bounds and doesn't touch an edge tile.
- **Layer 2**: module-level `sync.Pool` of per-tile decode-output
  `*decoder.Image` buffers, keyed by `(W, H, PixelFormat)`. Reused
  by ReadRegion's tile loop via `ImageDecodedTileInto`. Required
  `strippedImage.DecodedTile` to honor `opts.Dst` (the v0.27 fast
  path previously always allocated).
- **Layer 3**: NDPI-specific `sync.Pool` of evicted decoded-RGB
  frames inside `pixelFrameCache`. New `getOrLoadInto` variant
  passes a borrowed scratch into the load callback, which routes it
  to the decoder via `opts.Dst`.

### Measured throughput (Apple Silicon, 13 cores)

| Bench               | v0.28          | v0.29           | Delta |
|---|---|---|---|
| bench-ndpi (single) | 251 Mpix/s     | <measured single ndpi> | <delta> |
| bench-ndpi-mt       | 539 Mpix/s     | <measured mt ndpi>     | <delta> |
| bench-svs (single)  | 596 Mpix/s     | <measured single svs>  | <delta> |
| bench-svs-mt        | 2121 Mpix/s    | <measured mt svs>      | <delta> |

### Measured allocation reduction (bench-ndpi-mt, alloc_space)

| Source                              | v0.28    | v0.29 |
|---|---|---|
| Per-tile output (Layer 2 target)    | 22 GB    | <measured> |
| pixelCache frame (Layer 3 target)   | 11.4 GB  | <measured> |
| Total `decoder.NewImageFormat`      | 38.7 GB  | <measured> |

### Added (internal only)

- `borrowTileScratch(w, h int, format decoder.PixelFormat) *decoder.Image` —
  module-level `sync.Pool` accessor; lazy per-(W, H, Format).
- `returnTileScratch(img *decoder.Image)` — paired return; nil-safe.
- `pixelFrameCache.getOrLoadInto(key, wantW, wantH, load func(scratch))` —
  Layer 3's scratch-aware variant; routes evicted frames into the
  per-cache scratchPool.
- `pixelFrameCache.scratchPool sync.Pool` field (new field; per-
  instance). No public API.

### Changed

- `Slide.imageReadRegionImpl`: clip-to-bounds computation moved
  ahead of `fillWhite`; fillWhite gated on `!fullyInBounds ||
  edgeTileX || edgeTileY`. Borrows a scratch tile-Image once per
  call and uses `ImageDecodedTileInto` across the tile loop.
- `Slide.ImageDecodedTileInto`: fast-path dispatch passes `Dst: dst`
  into the decoded-tile call. When the fast path writes into `dst`
  directly (returns `dst`), skips the post-call `copyImageInto`.
- `formats/ndpi.strippedImage.DecodedTile`: honors `opts.Dst` when
  caller-provided dimensions+format match the level's tile shape.
  Falls back to allocation on mismatch (defensive).
- `formats/ndpi.pixelFrameCache.evictIfOverLocked`: now returns
  `[]*pixelFrameEntry` so callers can route survivors to the
  scratch pool. Previously returned nothing.
- `Makefile`: `MIN_NDPI_MPIXS` raised from 220 to `<final ndpi gate>`;
  `MIN_SVS_MPIXS` raised from 475 to `<final svs gate>`.

### Public API

- **No additions.** No new exported types, functions, or methods.
- **No breaking changes.** RawTile, DecodedTile, ReadRegion,
  ScaledStrips, and every format reader behave bit-identically.

### Tests

- `decoded_tile_scratch_test.go`: 5 pool unit tests (reuse, size-
  keyed, format-keyed, concurrent, return-nil-safe) under
  `-race -count=3`.
- `slide_region_test.go`: 2 new Layer 1 tests
  (`TestReadRegionFullyInBoundsPathSkipsFillWhite`,
  `TestReadRegionEdgeRegionForceFillWhite`) using a new
  `knownPixelReader` test double.
- `formats/ndpi/stripped_decodedtile_test.go`: 2 new prereq tests
  (`TestNDPIFastPathHonorsDst`,
  `TestNDPIFastPathDstWrongSizeFallsBackToAlloc`).
- `formats/ndpi/pixel_cache_test.go`: 2 new Layer 3 tests
  (`TestPixelCacheRecyclesEvictedFrames`,
  `TestPixelCacheConcurrentScratchSafe`).

### Out of scope (deferred forward)

- **`Slide.DecodedTile` (no `*Into` variant) allocation reduction.**
  Direct DecodedTile callers receive a user-owned `*decoder.Image`;
  pooling there would require API changes.
- **Allocation reduction in `Slide.ImageRawTile`'s callers.** RawTile
  consumers (wsi-tools splice path) manage their own buffers.
- **`ScaledStrips` allocation profile.** Has its own internal tile
  cache + worker pool tuned for the libvips-speed use case;
  ScaledStrips inherits Layer 2 via `Slide.ImageDecodedTile` but
  its own allocator wasn't profiled in v0.29.
- **pixelCache LRU capacity tuning.** Layer 3 reduces allocation
  per miss but doesn't change miss rate. Capacity stays
  `max(NumCPU, 16)` from v0.27.

### Pre-existing issues out of scope

- `tests/oracle/` build break (v0.24 Level API drift). Same as
  v0.27 / v0.28; not v0.29-introduced.

## [0.28.0] — 2026-05-29
```

Fill in every `<measured ...>` and `<delta>` with the actual numbers from Phase 1/3/4/5 records.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.29.0 — ReadRegion allocation-elimination perf milestone"
```

## Task 6.2: CLAUDE.md milestone update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Promote v0.29; demote v0.28**

Edit `CLAUDE.md`. Find:

```markdown
## Current milestone — v0.28 (in progress 2026-05-29)
```

Replace with:

```markdown
## Current milestone — v0.29 (in progress 2026-05-29)

- **Scope:** ReadRegion allocation-elimination perf milestone. Three
  independent layered optimizations targeting the 38% multi-thread
  CPU spent in `runtime.pthread_cond_signal` (GC sweep under
  bench-ndpi-mt's 39 GB/run allocation rate). Layer 1: skip
  `fillWhite(dst)` when ReadRegion's region is fully in-bounds
  (no edge tile). Layer 2: module-level `sync.Pool` of per-tile
  decode-output `*decoder.Image` buffers keyed by (W, H, Format);
  ReadRegion borrows once per call via `ImageDecodedTileInto`.
  Required `strippedImage.DecodedTile` to honor `opts.Dst`. Layer 3:
  NDPI-specific `sync.Pool` of evicted decoded RGB frames inside
  `pixelFrameCache`; new `getOrLoadInto` variant routes scratch via
  `opts.Dst`. Projected ~82% allocation reduction on bench-ndpi-mt
  (39 GB → ~7 GB).
- **API additions:** none public. Internal: `borrowTileScratch` /
  `returnTileScratch`; `pixelFrameCache.getOrLoadInto`;
  `pixelFrameCache.scratchPool` field. New file
  `decoded_tile_scratch.go`.
- **API breaks:** none. RawTile / DecodedTile / ReadRegion /
  ScaledStrips behave bit-identically.
- **Active limitations:** Direct `Slide.DecodedTile` callers
  (no `*Into` variant) still pay per-call allocation; pooling
  would require API changes. `ScaledStrips`' internal tile-cache
  allocation profile not addressed in v0.29.
- **Correctness bar:** `make test` green; new tests under
  `-race -count=3`: 5 scratch-pool tests, 2 fillWhite-skip tests,
  2 NDPI fast-path Dst tests, 2 pixelCache recycling tests. v0.27 +
  v0.28 regression suite green. `make bench-ndpi` ≥<v0.29 gate>;
  `make bench-svs` ≥<v0.29 gate>.
- **Sealed Q-decisions** (per spec): 10 sealed Qs covering scope,
  pool primitive (sync.Pool), pool scope (module-level cross-Slide),
  NDPI Dst plumbing, getOrLoad refactor scope, eviction-to-pool
  semantics, layer-by-layer gate tightening, gate-failure policy
  (JIT decision, not auto-revert), test fixture strategy, commit
  boundaries.
- **Deferred forward:** Direct DecodedTile allocation pooling
  (requires API); ScaledStrips internal cache profile; pixelCache
  LRU capacity tuning. `tests/oracle/` build break stays
  pre-existing.
- **Bench reality:** v0.29 numbers post-Phase 5:
  - bench-ndpi: <measured> Mpix/s single-thread
  - bench-ndpi-mt: <measured> Mpix/s multi-thread
  - bench-svs: <measured> Mpix/s single-thread
  - bench-svs-mt: <measured> Mpix/s multi-thread
- **Design:** docs/superpowers/specs/2026-05-29-opentile-go-v29-readregion-perf-design.md
- **Plan:** docs/superpowers/plans/2026-05-29-opentile-go-v29-readregion-perf.md
- **Work branch:** feat/v0.29

## Previous milestone — v0.28 (shipped 2026-05-29)
```

Fill in `<measured>` and `<v0.29 gate>` with the values from Phase 5.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude-md): promote v0.29 to current milestone"
```

---

# Phase 7 — Final gate

## Task 7.1: Run all gates + merge prep

**Files:**
- None modified.

- [ ] **Step 1: Run the full gate suite**

```bash
make vet
OPENTILE_TESTDIR="$PWD/sample_files" go test -race -count=1 ./... 2>&1 | grep -E "ok|FAIL" | tail -30
make bench-ndpi
make bench-ndpi-mt
make bench-svs
make bench-svs-mt
```

Expected:
- `make vet`: zero warnings.
- Full test suite: all packages PASS under `-race -count=1`.
- `make bench-ndpi`: PASS at new threshold.
- `make bench-svs`: PASS at new threshold.
- `make bench-ndpi-mt` + `make bench-svs-mt`: prints multi-thread numbers (no gates; pure measurement).

- [ ] **Step 2: Confirm branch is merge-ready**

```bash
git log --oneline main..feat/v0.29
git status
```

Expected: every commit titled per the convention; working tree clean. ~12-15 commits total.

The branch is ready for merge. Per the v0.28 pattern:

```bash
git checkout main
git merge --no-ff feat/v0.29 -m "Merge feat/v0.29 into main (v0.29.0)"
git tag -a v0.29.0 -m "v0.29.0 — ReadRegion allocation-elimination perf milestone"
git push origin main v0.29.0
git branch -d feat/v0.29
```

Do not execute the merge as part of the plan; surface the commands for the user to run.

---

## Self-review checklist

- **Spec coverage** — every spec section maps to a task:
  - §1.1 Layer 1 → Task 1.1 + 1.2 + 1.3
  - §1.2 Layer 2 (scratch pool + Dst plumbing) → Task 2.1 + 2.2 + 3.1 + 3.2 + 3.3 + 3.4
  - §1.3 Layer 3 (pixelCache pool) → Task 4.1 + 4.2 + 4.3 + 4.4 + 4.5
  - §1.4 bench/gate updates → Task 5.1 + 5.2
  - §1.5 gate-failure policy → encoded in Task 1.3 Step 4, Task 3.4 Step 4, Task 4.5 Step 4
  - §4.2 component list → all New/Modified files map to tasks
  - §5 testing matrix → tests in Task 1.2, 2.2, 3.1, 4.4
- **Placeholder scan** — `<measured ...>` / `<v0.29 gate>` cells in CHANGELOG and CLAUDE.md are explicit measurement-step outputs from Phase 5 with stated value sources (Task 5.1, 5.2 Makefile values; Task 1.3 / 3.4 / 4.5 bench measurements). Same pattern as the v0.28 plan. The `<computed>` in Task 5.1 / 5.2 has explicit formula: `floor(M_x × 0.95)`.
- **Type consistency** —
  - `borrowTileScratch` / `returnTileScratch` (Task 3.1, 3.2) — same names throughout.
  - `scratchKey` / `tileScratchPool` (Task 3.1) — consistent.
  - `pixelFrameCache.getOrLoadInto(key, wantW, wantH, load func(scratch))` — same signature in Task 4.2 (definition) and Task 4.3 (call site).
  - `pixelFrameCache.scratchPool sync.Pool` — same field name throughout.
  - `evictIfOverLocked() []*pixelFrameEntry` — same return shape in Task 4.1.
  - `strippedImage.DecodedTile`'s Dst-honoring block (Task 2.1) — `opts.Dst.Width == l.tileSize.W` etc.
  - `Slide.ImageDecodedTileInto`'s `Dst: dst` pass + `if out == dst` shortcut (Task 3.3).
