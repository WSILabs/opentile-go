# True-Content-Extent Pyramid (DP-BIF + IFE) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Level.Size`/`Downsample` the true content extent at every pyramid level for **DP-generation BIF** and **IFE**, so inter-level scale is exactly ~2×. BIF-DP derives reduced levels from the L0 stitched hull (floor-halving); IFE derives from the per-layer `scale` + `x_extent` anchor. Legacy-BIF is untouched (deferred to #80). Closes #78 for DP-BIF + IFE.

**Architecture:** For BIF, one field (`levelImpl.size`) already drives `Level.Size`, `Downsample`, and (after a one-line alignment) `StitchedSize`; we override it for DP `i≥1`. For IFE, the per-level geometry is extracted into a pure helper and re-derived from layer scales. `Grid` (the stored tile grid) is unchanged in both; pixels still come from stored tiles; reads clip to the corrected `Size`.

**Tech Stack:** Go 1.23+, `formats/bif`, `formats/ife`, `internal/dzi`-style pure helpers for testability. No cgo changes. Geometry-only — no tile bytes change.

**Reference (read first):**
- Design spec: `docs/superpowers/specs/2026-06-24-true-content-extent-pyramid-design.md` (esp. the **Consistency invariant**).
- `formats/bif/bif.go:80-113` (level-build loop), `formats/bif/level.go:218-228` (`l.size` assignment), `:304-310` (`StitchedSize`), `formats/bif/classify.go:15-28` (`GenerationSpecCompliant` = DP).
- `formats/ife/tiler.go:65-100` (level-build loop), `formats/ife/reader.go:69-78` (`LayerExtent{XTiles,YTiles,Scale}`), `:53-64` (`TileTable.XExtent/YExtent`).

---

## File Structure

| File | Change |
|---|---|
| `formats/bif/bif.go` | Hoist `gen`; capture L0 hull; for DP `i≥1`, override `l.size` via `floorHalveSize`. |
| `formats/bif/level.go` | `StitchedSize()` returns `l.size` (align to its `== Size()` doc); add `floorHalveSize` helper. |
| `formats/bif/geometry_test.go` | Create: `floorHalveSize` unit test (fixture-free, `package bif`). |
| `formats/ife/tiler.go` | Replace inline `Size`/`Downsample` with `ifeGeometry(...)` results. |
| `formats/ife/geometry.go` | Create: pure `ifeGeometry` + `validPixelExtent`. |
| `formats/ife/geometry_test.go` | Create: `ifeGeometry` unit tests (fixture-free, `package ife`). |
| `pyramid_content_extent_test.go` | Create (root dir, `package opentile_test`): fixture-gated Ventana-1/OS-1/cervix geometry via `opentile.OpenFile` (public `StitchedTile`/`StitchedGrid` reachable, no internal-opener guessing). |
| Parity/geometry snapshots | Update DP-BIF + IFE per-level `Size`/`Downsample` expectations (tile SHAs unchanged). |
| `CHANGELOG.md` | `[Unreleased]` entry + consumer note. |

---

## Task 1: BIF — DP reduced-level content extent

**Files:**
- Modify: `formats/bif/bif.go`, `formats/bif/level.go`
- Test: `formats/bif/geometry_test.go` (create; the `floorHalveSize` unit test only in this task)

- [ ] **Step 1: Write the failing helper test**

Create `formats/bif/geometry_test.go`:

```go
package bif

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestFloorHalveSize(t *testing.T) {
	// Ventana-1 DP hull → bio-formats sizeX/sizeY chain.
	hull := opentile.Size{W: 23432, H: 21504}
	wantW := []int{23432, 11716, 5858, 2929, 1464, 732, 366, 183}
	wantH := []int{21504, 10752, 5376, 2688, 1344, 672, 336, 168}
	for i := range wantW {
		got := floorHalveSize(hull, i)
		if got.W != wantW[i] || got.H != wantH[i] {
			t.Errorf("floorHalveSize(hull,%d) = %dx%d, want %dx%d", i, got.W, got.H, wantW[i], wantH[i])
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./formats/bif/ -run TestFloorHalveSize -count=1`
Expected: FAIL — `undefined: floorHalveSize`.

- [ ] **Step 3: Add the helper to level.go**

In `formats/bif/level.go`, add (near the other package helpers, e.g. after `weightedAverageOverlap`):

```go
// floorHalveSize halves sz by 2, n times, with integer (floor) division —
// reproducing bio-formats' per-resolution sizeX/sizeY chain for DP pyramids
// (e.g. 23432 → 11716 → 5858 → 2929 → 1464 → …). n == 0 returns sz.
func floorHalveSize(sz opentile.Size, n int) opentile.Size {
	w, h := sz.W, sz.H
	for k := 0; k < n; k++ {
		w /= 2
		h /= 2
	}
	return opentile.Size{W: w, H: h}
}
```

- [ ] **Step 4: Run the helper test to verify it passes**

Run: `go test ./formats/bif/ -run TestFloorHalveSize -count=1`
Expected: PASS.

- [ ] **Step 5: Align `StitchedSize()` to `l.size`**

In `formats/bif/level.go`, replace the `StitchedSize` method (currently `:304-310`):

```go
// StitchedSize returns this level's stitched content extent (== Size()). It is
// the single source of truth for the compositor's clip bounds; for DP reduced
// levels this is the hull-derived content extent (#78). The stored-grid extent
// (cols*tileW from the naive layout) is recoverable as Grid*TileSize.
func (l *levelImpl) StitchedSize() (w, h int, ok bool) {
	return l.size.W, l.size.H, true
}
```

(This is behaviour-preserving today — `l.size` already equals `l.layout.Width/Height` at every level for real data — and makes `l.size` the one value `Level.Size` *and* `StitchedSize` both read, so the Task-Step-6 override fixes both at once. `TileOrigin`/`TilesIntersecting` keep using `l.layout` for placement, unchanged.)

- [ ] **Step 6: Override `l.size` for DP reduced levels in the build loop**

In `formats/bif/bif.go`, in `openFromTIFFFile`, hoist the generation and capture the L0 hull, then override DP `i≥1` sizes. Replace the loop header + the `if i == 0` block (`:84-93`):

Current:
```go
	var l0Width int
	for i, c := range levelIFDs {
		l, err := newLevelImpl(i, c, iscan.ScanRes, scanWhite, classifyGeneration(iscan), encodeInfo, file.ReaderAt())
		if err != nil {
			return nil, err
		}
		if i == 0 {
			levelZeroDepth = l.imageDepth
			l0Width = l.size.W
		}
```

New:
```go
	var l0Width int
	var l0Hull opentile.Size
	gen := classifyGeneration(iscan)
	for i, c := range levelIFDs {
		l, err := newLevelImpl(i, c, iscan.ScanRes, scanWhite, gen, encodeInfo, file.ReaderAt())
		if err != nil {
			return nil, err
		}
		if i == 0 {
			levelZeroDepth = l.imageDepth
			l0Width = l.size.W
			l0Hull = l.size
		} else if gen == GenerationSpecCompliant {
			// #78: DP (spec-compliant / DP 200) reduced levels derive their
			// content extent from the L0 stitched hull by floor-halving, not the
			// padded IFD ImageWidth (which keeps the un-compacted frame-grid
			// width). This drives Level.Size, Downsample, AND StitchedSize (all
			// read l.size), so the pyramid's inter-level scale is exactly 2× and
			// the compositor clips display tiles to true content. Legacy iScan is
			// deliberately untouched — its reduced levels still carry frame
			// overlap (GH #80) and need per-level stitching, not a Size change.
			l.size = floorHalveSize(l0Hull, i)
		}
```

Also update the later `gen:` field assignment (`:159`) to reuse the hoisted local: change `gen: classifyGeneration(iscan),` to `gen: gen,`.

- [ ] **Step 7: Verify build + helper test + no other-format breakage at unit level**

Run: `go build ./... && go vet ./formats/bif/`
Expected: clean.

Run: `go test ./formats/bif/ -run TestFloorHalveSize -count=1`
Expected: PASS.

Run: `go test ./formats/bif/ -count=1`
Expected: PASS (existing non-fixture BIF unit tests; fixture-gated ones skip without `OPENTILE_TESTDIR`).

- [ ] **Step 8: Commit**

```bash
git add formats/bif/bif.go formats/bif/level.go formats/bif/geometry_test.go
git commit -m "feat(bif): DP reduced levels report true content extent (Size/Downsample/StitchedSize from L0 hull) (#78)"
```

---

## Task 2: BIF — fixture-gated geometry + consistency tests (root package)

**Files:**
- Create: `pyramid_content_extent_test.go` (root dir, **`package opentile_test`**)

All fixture-gated geometry tests go through the public `opentile.OpenFile` in an external test package — the same pattern `bif_stitched_tile_test.go` uses — so there's no internal-opener guessing and `StitchedTile`/`StitchedGrid` (public `*Level` methods) are reachable. Ventana-1 + OS-1 are in the public wsi-fixtures BIF tar → these run in CI's integration job; they skip without `OPENTILE_TESTDIR`.

- [ ] **Step 1: Create the BIF geometry test file**

Create `pyramid_content_extent_test.go`:

```go
package opentile_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// openFixture opens dir/<sub>/<name> via the public API, skipping when the
// fixture corpus or the file is absent.
func openFixture(t *testing.T, sub, name string) *opentile.Slide {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(dir, sub, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	s, err := opentile.OpenFile(p)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return s
}

func ceilDivT(a, b int) int { return (a + b - 1) / b }

func TestBIFVentanaContentExtent(t *testing.T) {
	s := openFixture(t, "bif", "Ventana-1.bif")
	defer s.Close()
	wantW := []int{23432, 11716, 5858, 2929, 1464, 732, 366, 183}
	wantH := []int{21504, 10752, 5376, 2688, 1344, 672, 336, 168}
	levels := s.Levels()
	if len(levels) != len(wantW) {
		t.Fatalf("level count %d, want %d", len(levels), len(wantW))
	}
	for i, l := range levels {
		if l.Size.W != wantW[i] || l.Size.H != wantH[i] {
			t.Errorf("L%d Size = %dx%d, want %dx%d", i, l.Size.W, l.Size.H, wantW[i], wantH[i])
		}
		// Downsample is exactly 2^i (no longer 1.907 at L1).
		want := float64(int(1) << uint(i))
		if l.Downsample != want {
			t.Errorf("L%d Downsample = %v, want %v", i, l.Downsample, want)
		}
		// StitchedGrid is the clean ceil partition of the (corrected) Size.
		g := l.StitchedGrid()
		if g.W != ceilDivT(l.Size.W, l.TileSize.W) || g.H != ceilDivT(l.Size.H, l.TileSize.H) {
			t.Errorf("L%d StitchedGrid %v != ceil(Size %v / Tile %v)", i, g, l.Size, l.TileSize)
		}
	}
}

// Proves StitchedSize == Size functionally: at DP L1 the last display column
// must clip overscan to white (only StitchedTile clips to StitchedSize, so if
// StitchedSize still equalled the padded 12288 this column would show stored
// overscan instead of white).
func TestBIFVentanaStitchedTileClipsOverscan(t *testing.T) {
	s := openFixture(t, "bif", "Ventana-1.bif")
	defer s.Close()
	l1, err := s.Level(1)
	if err != nil {
		t.Fatal(err)
	}
	g := l1.StitchedGrid() // expect W = ceil(11716/1024) = 12
	img, err := l1.StitchedTile(g.W-1, 0)
	if errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Skip("decode unavailable (nocgo)")
	}
	if err != nil {
		t.Fatal(err)
	}
	// Last column origin = (g.W-1)*TileW = 11*1024 = 11264; content ends at
	// Size.W = 11716, so local columns >= 11716-11264 = 452 are overscan → white.
	contentCols := l1.Size.W - (g.W-1)*l1.TileSize.W // 452
	bpp := 3
	for _, y := range []int{0, img.Height / 2, img.Height - 1} {
		for _, x := range []int{contentCols + 8, img.Width - 1} {
			o := y*img.Stride + x*bpp
			if img.Pix[o] != 0xFF || img.Pix[o+1] != 0xFF || img.Pix[o+2] != 0xFF {
				t.Fatalf("overscan pixel (%d,%d) = %d,%d,%d, want white 255 (StitchedSize must == Size, not padded)",
					x, y, img.Pix[o], img.Pix[o+1], img.Pix[o+2])
			}
		}
	}
}

func TestBIFLegacySizeUnchanged(t *testing.T) {
	s := openFixture(t, "bif", "OS-1.bif")
	defer s.Close()
	// Legacy iScan is deferred to #80 — its reduced-level Size must stay the raw
	// (un-compacted) grid extent. Pin L0/L1 so this change can't silently alter
	// legacy. (105818 hull at L0; 59392 = 58*1024 raw grid at L1.)
	levels := s.Levels()
	if levels[0].Size.W != 105818 {
		t.Errorf("OS-1 L0 Size.W = %d, want 105818 (unchanged hull)", levels[0].Size.W)
	}
	if levels[1].Size.W != 59392 {
		t.Errorf("OS-1 L1 Size.W = %d, want 59392 (raw grid, legacy untouched — see #80)", levels[1].Size.W)
	}
}
```

- [ ] **Step 2: Run the fixture-gated tests (locally, with fixtures)**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run 'TestBIFVentana|TestBIFLegacy' -count=1 -v`
Expected: PASS (Ventana-1 chain + Downsample + StitchedGrid + StitchedTile overscan-white; OS-1 legacy unchanged). If `sample_files/bif/*` absent, SKIP — report which.

Also confirm it builds under nocgo (the file is in the root package, which the nocgo `test` job compiles): `go vet -tags nocgo .` — clean.

- [ ] **Step 3: Commit**

```bash
git add pyramid_content_extent_test.go
git commit -m "test(bif): Ventana-1 content-extent chain + Downsample + StitchedGrid/StitchedTile overscan consistency; OS-1 legacy pinned unchanged"
```

---

## Task 3: IFE — scale-derived content extent

**Files:**
- Create: `formats/ife/geometry.go`, `formats/ife/geometry_test.go`
- Modify: `formats/ife/tiler.go`

- [ ] **Step 1: Write the failing helper test**

Create `formats/ife/geometry_test.go`:

```go
package ife

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestIFEGeometryScaleRatios(t *testing.T) {
	// 3 layers, native-first: scales 4,2,1 → downsamples 1,2,4.
	api := []LayerExtent{
		{XTiles: 8, YTiles: 6, Scale: 4},
		{XTiles: 4, YTiles: 3, Scale: 2},
		{XTiles: 2, YTiles: 2, Scale: 1},
	}
	// x_extent valid pixels (not a multiple of 256): in ((8-1)*256, 8*256] = (1792,2048].
	tt := TileTable{XExtent: 1900, YExtent: 1400}
	sizes, downs := ifeGeometry(api, tt)
	if sizes[0] != (opentile.Size{W: 1900, H: 1400}) {
		t.Fatalf("L0 size = %v, want 1900x1400 (x_extent anchor)", sizes[0])
	}
	wantDown := []float64{1, 2, 4}
	for i := range downs {
		if downs[i] != wantDown[i] {
			t.Errorf("L%d downsample = %v, want %v", i, downs[i], wantDown[i])
		}
	}
	// Exact 2x ratios (round of 1900/2=950, /4=475).
	if sizes[1] != (opentile.Size{W: 950, H: 700}) || sizes[2] != (opentile.Size{W: 475, H: 350}) {
		t.Fatalf("scaled sizes = %v %v, want 950x700 475x350", sizes[1], sizes[2])
	}
}

func TestIFEGeometryInvalidExtentFallsBack(t *testing.T) {
	api := []LayerExtent{
		{XTiles: 8, YTiles: 6, Scale: 2},
		{XTiles: 4, YTiles: 3, Scale: 1},
	}
	// x_extent carries TILE COUNTS (cervix non-conformance): 8 is not in (1792,2048].
	tt := TileTable{XExtent: 8, YExtent: 6}
	sizes, downs := ifeGeometry(api, tt)
	// Falls back to padded L0 (8*256 x 6*256); ratios still exact.
	if sizes[0] != (opentile.Size{W: 2048, H: 1536}) {
		t.Fatalf("L0 size = %v, want padded 2048x1536 fallback", sizes[0])
	}
	if downs[1] != 2 || sizes[1] != (opentile.Size{W: 1024, H: 768}) {
		t.Fatalf("L1 down=%v size=%v, want 2 / 1024x768", downs[1], sizes[1])
	}
}

func TestValidPixelExtent(t *testing.T) {
	if !validPixelExtent(1900, 8) { // (1792,2048]
		t.Error("1900 with 8 tiles should be valid pixels")
	}
	if validPixelExtent(8, 8) { // tile-count, not pixels
		t.Error("8 with 8 tiles should be invalid (tile count)")
	}
	if validPixelExtent(2048, 8) == false { // exact multiple is valid
		t.Error("2048 with 8 tiles should be valid")
	}
	if validPixelExtent(1792, 8) { // == (tiles-1)*256, boundary excluded
		t.Error("1792 with 8 tiles should be invalid")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./formats/ife/ -run 'TestIFEGeometry|TestValidPixelExtent' -count=1`
Expected: FAIL — `undefined: ifeGeometry` / `validPixelExtent`.

- [ ] **Step 3: Create the geometry helper**

Create `formats/ife/geometry.go`:

```go
package ife

import (
	"math"

	opentile "github.com/wsilabs/opentile-go"
)

// ifeGeometry computes per-API-level content Size and Downsample from the layer
// scales and the L0 content extent. api is native-first (api[0] = finest =
// max scale). The spec's downsample is max_scale/scale; the content extent at
// each level is L0_content / downsample, rounded. L0 content comes from
// TileTable.x_extent/y_extent when those are valid pixel dimensions, else the
// padded XTiles*256 grid (the cervix fixture stores tile counts there).
func ifeGeometry(api []LayerExtent, tt TileTable) (sizes []opentile.Size, downs []float64) {
	n := len(api)
	sizes = make([]opentile.Size, n)
	downs = make([]float64, n)
	if n == 0 {
		return sizes, downs
	}
	maxScale := float64(api[0].Scale)

	size0 := opentile.Size{
		W: int(api[0].XTiles) * TileSidePixels,
		H: int(api[0].YTiles) * TileSidePixels,
	}
	if validPixelExtent(int(tt.XExtent), int(api[0].XTiles)) &&
		validPixelExtent(int(tt.YExtent), int(api[0].YTiles)) {
		size0 = opentile.Size{W: int(tt.XExtent), H: int(tt.YExtent)}
	}

	for i := range api {
		ds := maxScale / float64(api[i].Scale)
		downs[i] = ds
		sizes[i] = opentile.Size{
			W: int(math.Round(float64(size0.W) / ds)),
			H: int(math.Round(float64(size0.H) / ds)),
		}
	}
	return sizes, downs
}

// validPixelExtent reports whether ext is a plausible pixel dimension for a
// tile grid of `tiles` columns/rows: it must lie in ((tiles-1)*256, tiles*256].
// Tile COUNTS (e.g. cervix's x_extent) fail this test, so the caller falls back
// to the padded grid base.
func validPixelExtent(ext, tiles int) bool {
	if tiles <= 0 {
		return false
	}
	return ext > (tiles-1)*TileSidePixels && ext <= tiles*TileSidePixels
}
```

- [ ] **Step 4: Run the helper tests to verify they pass**

Run: `go test ./formats/ife/ -run 'TestIFEGeometry|TestValidPixelExtent' -count=1`
Expected: PASS.

- [ ] **Step 5: Rewire `tiler.go` to use the helper**

In `formats/ife/tiler.go`, compute geometry once before the level loop and use it. Replace `l0Width` (`:69-70`) and the per-level `Size`/`Downsample` (`:92,96`):

After `apiOrder` is built (and `tt` is in scope), add before the loop:
```go
	sizes, downs := ifeGeometry(apiOrder, tt)
```
Remove the now-unused `l0FileIdx`/`l0Width` lines if they become unused. Inside the loop, keep `ext := fileOrder[fi]` (still used for `Grid` and the tile-count walk), and set the level fields from the helper:
```go
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         sizes[i],
			TileSize:     opentile.Size{W: TileSidePixels, H: TileSidePixels},
			Grid:         opentile.Size{W: int(ext.XTiles), H: int(ext.YTiles)},
			Compression:  compression,
			Downsample:   downs[i],
		}
```
Delete the now-unused `levelW := int(ext.XTiles) * TileSidePixels` line (Grid uses `ext.XTiles` directly). If `l0Width`/`l0FileIdx` are now unused, remove them (the compiler will flag).

- [ ] **Step 6: Verify build + unit tests + IFE package suite**

Run: `go build ./... && go vet ./formats/ife/`
Expected: clean (no unused-variable errors — remove `l0Width`/`l0FileIdx`/`levelW` if flagged).

Run: `go test ./formats/ife/ -count=1`
Expected: PASS (helper tests + existing IFE unit tests; fixture-gated ones skip).

- [ ] **Step 7: Commit**

```bash
git add formats/ife/geometry.go formats/ife/geometry_test.go formats/ife/tiler.go
git commit -m "feat(ife): Level.Size/Downsample from per-layer scale + x_extent anchor (true content extent) (#78)"
```

---

## Task 4: IFE fixture test, snapshots, docs, verification

**Files:**
- Modify: `pyramid_content_extent_test.go` (append the cervix ratio test — root package, public API), parity/geometry snapshots, `CHANGELOG.md`

- [ ] **Step 1: Add the cervix ratio test (local-gated, root package)**

Append to `pyramid_content_extent_test.go` (the `package opentile_test` file from Task 2; reuses its `openFixture` helper). cervix is local-only (not in CI fixtures) so it skips in CI:

```go
func TestIFECervixRatiosConsistent(t *testing.T) {
	s := openFixture(t, "ife", "cervix_2x_jpeg.iris")
	defer s.Close()
	levels := s.Levels()
	for i := 1; i < len(levels); i++ {
		rw := float64(levels[i-1].Size.W) / float64(levels[i].Size.W)
		rh := float64(levels[i-1].Size.H) / float64(levels[i].Size.H)
		// Drift is gone: every adjacent ratio is ~2 (the bug had 1.5–1.99 at
		// coarse levels). Tolerate <=1px rounding => ratio within [1.95, 2.05].
		if rw < 1.95 || rw > 2.05 || rh < 1.95 || rh > 2.05 {
			t.Errorf("L%d->L%d ratio = %.4f/%.4f, want ~2.0", i-1, i, rw, rh)
		}
		// Inter-level downsample is exact-2 (the spec's max_scale/scale).
		if d := levels[i].Downsample / levels[i-1].Downsample; d < 1.95 || d > 2.05 {
			t.Errorf("L%d/L%d Downsample ratio = %.4f, want ~2.0", i, i-1, d)
		}
	}
}
```

- [ ] **Step 2: Update parity / geometry snapshots**

DP-BIF (Ventana-1) and IFE per-level `Size`/`Downsample` values change (tile SHAs do **not**). Find and update any committed expectations:

```bash
grep -rIl "12288\|Downsample" tests/ formats/bif formats/ife 2>/dev/null | grep -iE "geometry|parity|golden|snapshot|expect" || true
```

Run the parity/geometry suites and update the BIF-DP + IFE numbers (and only those) where they assert per-level dims; confirm tile-byte SHAs are unchanged:

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/... -run 'Geometry|Parity' -count=1` (and `./tests/parity/...` if present).
Expected: BIF-DP + IFE dim expectations need updating to the content-extent values; **legacy-BIF and the other 9 formats unchanged**. Update only the changed expectations; if a snapshot regen command exists (e.g. a `-generate` flag like `tests` uses), prefer it but diff the output to confirm only BIF-DP/IFE dims moved.

- [ ] **Step 3: CHANGELOG entry**

In `CHANGELOG.md`, insert above the top release section:

```markdown
## [Unreleased]

### Fixed

- **Pyramid `Level.Size`/`Downsample` now report true content extent for DP-BIF
  and IFE** (#78). Previously these two formats reported a tile-grid-padded (BIF
  reduced levels) or overlap-compacted-only-at-L0 extent, so the inter-level
  scale wasn't exactly 2× and a consumer building a pyramid from `Level.Size`
  (e.g. `downsample = Size[0]/Size[i]`) mis-registered content across the L0/L1
  boundary (BIF) or at coarse levels (IFE). DP-BIF reduced levels now derive from
  the L0 stitched hull (floor-halving, matching bio-formats); IFE derives from the
  per-layer `scale` anchored to `TILE_TABLE.x_extent/y_extent`. `Grid` (the stored
  tile grid) is unchanged; pixels are unchanged (tile bytes byte-identical); the
  padded raster extent remains recoverable as `Grid × TileSize`. **Legacy iScan
  BIF is unchanged** (its reduced levels carry frame overlap — tracked in #80);
  the IFE Magnification/MPP issue is separate (#81).

  ⚠️ **Consumer note:** DP-BIF reduced-level and IFE `Level.Size`/`Downsample`
  values change to the corrected ones. Consumers that built pyramids from the old
  padded values (e.g. wsitools DZI output dims) will see shifted — now correct —
  output.
```

- [ ] **Step 4: Full verification**

macOS linker warnings (`ld: warning: ignoring duplicate libraries`) are pre-existing noise.
- `go vet ./...` — clean.
- `go build ./... && go build -tags nocgo ./...` — both clean.
- `go test ./... -race -count=1` — PASS (note: synthetic helper tests are fixture-free; new fixture-gated tests skip without `OPENTILE_TESTDIR`).
- `OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/bif/ ./formats/ife/ ./tests/... -count=1` — PASS: Ventana-1 content chain (root `pyramid_content_extent_test.go`), OS-1 legacy unchanged, IFE cervix ratios ~2×, parity (other formats byte-identical, BIF-DP+IFE dims updated).

- [ ] **Step 5: Commit**

```bash
git add pyramid_content_extent_test.go CHANGELOG.md tests/
git commit -m "test(ife): cervix ratio consistency; update DP-BIF+IFE geometry snapshots; CHANGELOG (#78)"
```

---

## Notes for the executor

- **Geometry-only:** no tile bytes change for any format. If a *tile-byte* SHA snapshot moves, something is wrong — stop and investigate.
- **Legacy BIF is untouched by design** — the `gen == GenerationSpecCompliant` gate is the load-bearing line. `TestBIFLegacySizeUnchanged` pins it; if it fails, the gate is wrong (do not "fix" the test).
- **`StitchedSize == Size` is the consistency invariant** (the spec's load-bearing detail). Both must read `l.size` for BIF; the Task-1 Step-5 change is what guarantees it.
- **Fixture availability:** Ventana-1 + OS-1 are in the public wsi-fixtures BIF tar → BIF geometry tests run in CI. cervix IFE is **local-only** → the cervix test skips in CI; the `ifeGeometry` unit tests are the CI-safe IFE coverage.
- **Consumer coordination:** flag the wsitools/openscope `Size` change (CHANGELOG ⚠️) — not in our CI.
- **Version:** ships as the next minor (additive-in-spirit, but a behavior change to two formats' `Size`); per cadence, **v0.53.0** after CI green. (It's a fix, not a new API; MINOR is appropriate given the visible `Size` change.)
- **Out of scope:** legacy-BIF reduced levels (#80), IFE MPP (#81), the other 10 formats.
```
