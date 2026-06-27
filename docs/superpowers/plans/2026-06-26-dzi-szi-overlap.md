# DZI / SZI Overlap > 0 Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read tiles correctly from DZI/SZI pyramids whose manifest declares `Overlap > 0`, cropping each tile's redundant border so composited output is overlap-free; keep `Overlap = 0` byte-identical.

**Architecture:** A new `OverlapMode` enum classifies levels (None/Bordered/Stitched); DZI/SZI `Overlap>0` levels are `OverlapBordered` and implement the existing `regionLayout`/`subtileLayout` capability (same source tile, cropped to its content rect), so `ReadRegion`/`ScaledStrips`/`StitchedTile` reuse the BIF compositor unchanged. The per-level `StitchedSize(level) ok=false` gate keeps `Overlap=0` on the existing fast path. `DecodedTile` returns the raw padded tile; a pure-field `Level.TileContentRect` exposes the crop.

**Tech Stack:** Go 1.23, `internal/dzi` (pyramid math + manifest), `formats/dzi` (filesystem), `formats/szi` (ZIP), the root `opentile` package (public `Level` API + region compositor). Tests use `image/png` for lossless synthetic fixtures.

**Spec:** `docs/superpowers/specs/2026-06-26-dzi-szi-overlap-design.md`

---

## File Structure

- `image.go` — `OverlapMode` enum + consts; `Level.OverlapMode` field; reworded `Overlapping`/`Grid`/`TileOverlap` docs.
- `level_reads.go` — `Level.TileContentRect(col,row) (Region, bool)` pure-field method.
- `internal/dzi/content.go` (new) — `ContentRect` helper (shared by both readers).
- `formats/dzi/level.go` — `overlap` field; `regionLayout`/`subtileLayout` engine methods.
- `formats/dzi/tiler.go` — drop the `Overlap>0` guard; carry overlap; populate `Level` fields; tiler-level `regionLayout`/`subtileLayout` methods delegating to the engine.
- `formats/szi/level.go`, `formats/szi/tiler.go` — same as DZI (ZIP tile source).
- Tests: per-package `_test.go` files; a local-only CMU-1 parity test; an in-test synthetic PNG-DZI generator for CI.

---

## Task 1: `OverlapMode` enum

**Files:**
- Modify: `image.go` (add after the `Compression` type block; near the `Level` struct)
- Test: `image_overlapmode_test.go` (new, package `opentile`)

- [ ] **Step 1: Write the failing test**

Create `image_overlapmode_test.go`:

```go
package opentile

import "testing"

func TestOverlapModeString(t *testing.T) {
	cases := map[OverlapMode]string{
		OverlapNone:     "none",
		OverlapBordered: "bordered",
		OverlapStitched: "stitched",
		OverlapMode(99): "OverlapMode(99)",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("OverlapMode(%d).String() = %q, want %q", int(m), got, want)
		}
	}
}

func TestOverlapNoneIsZeroValue(t *testing.T) {
	var m OverlapMode
	if m != OverlapNone {
		t.Errorf("zero value = %v, want OverlapNone", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ -run TestOverlapMode -v`
Expected: FAIL — `undefined: OverlapMode`.

- [ ] **Step 3: Write minimal implementation**

In `image.go`, add (place it just above the `Level` struct definition):

```go
// OverlapMode classifies how a level's stored/decoded tiles relate to its
// content grid.
type OverlapMode int

const (
	// OverlapNone: tiles are a clean partition of Size. Grid tiles Size;
	// per-tile reads are verbatim content cells; verbatim tile-copy is safe.
	OverlapNone OverlapMode = iota

	// OverlapBordered: stored/decoded tiles carry a redundant overlap border
	// (DZI/SZI Overlap>0). Grid STILL tiles Size (content cells partition it);
	// crop each decoded tile to TileContentRect, or use the region API.
	OverlapBordered

	// OverlapStitched: the stitch layout compacted the grid (BIF). Grid does
	// NOT tile Size (Grid.W×TileSize.W > Size.W); per-tile reads are raw
	// overlapping frames at stored positions; use the region API.
	OverlapStitched
)

// String returns a lowercase label ("none" / "bordered" / "stitched").
func (m OverlapMode) String() string {
	switch m {
	case OverlapNone:
		return "none"
	case OverlapBordered:
		return "bordered"
	case OverlapStitched:
		return "stitched"
	default:
		return "OverlapMode(" + strconv.Itoa(int(m)) + ")"
	}
}
```

Ensure `image.go` imports `"strconv"` (add to its import block if absent).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ -run TestOverlapMode -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add image.go image_overlapmode_test.go
git commit -m "feat(api): add OverlapMode enum (None/Bordered/Stitched)"
```

---

## Task 2: `Level.OverlapMode` field + reworded docs

**Files:**
- Modify: `image.go` (the `Level` struct: add field; reword `Grid`, `Overlapping`, `TileOverlap` docs)
- Test: `image_overlapmode_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `image_overlapmode_test.go`:

```go
func TestLevelOverlapModeField(t *testing.T) {
	l := Level{OverlapMode: OverlapBordered, TileOverlap: Point{X: 1, Y: 1}}
	if l.OverlapMode != OverlapBordered {
		t.Fatalf("OverlapMode = %v, want OverlapBordered", l.OverlapMode)
	}
	// Overlapping is the derived convenience: true iff OverlapMode != None.
	if !l.Overlapping {
		// NOTE: readers set Overlapping explicitly; this test documents the
		// intended invariant that callers can rely on (see Task 5/7 population).
		t.Skip("Overlapping is reader-populated; invariant checked in format tests")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ -run TestLevelOverlapModeField -v`
Expected: FAIL — `unknown field 'OverlapMode' in struct literal`.

- [ ] **Step 3: Write minimal implementation**

In `image.go`, add the field to the `Level` struct, immediately above the existing `Overlapping bool` field:

```go
	// OverlapMode classifies this level's tile/grid relationship:
	// OverlapNone (clean partition), OverlapBordered (DZI/SZI overlap>0 —
	// tiles padded with a croppable border; Grid still tiles Size), or
	// OverlapStitched (BIF — compacted hull; Grid does NOT tile Size).
	// Overlapping == (OverlapMode != OverlapNone).
	OverlapMode OverlapMode
```

Reword the existing `Overlapping` field doc to:

```go
	// Overlapping is a convenience equal to (OverlapMode != OverlapNone): the
	// level's stored/decoded tiles overlap (bordered or stitched) and are not a
	// clean verbatim partition, so gate any verbatim per-tile copy on
	// !Overlapping. For the precise flavour — and specifically whether Grid
	// tiles Size — read OverlapMode (only OverlapStitched has Grid != Size).
	// False for every clean-grid level. The per-tile accessors still return the
	// raw (padded/overlapping) tiles; use the region API or, for bordered
	// levels, TileContentRect, to obtain correctly-placed pixels.
	Overlapping bool
```

Reword the `Grid` field doc's "IMPORTANT" paragraph to scope it to stitched:

```go
	// IMPORTANT: when OverlapMode == OverlapStitched (a stitched BIF level),
	// Grid is the RAW stored tile grid of OVERLAPPING tiles and does NOT tile
	// Size — Grid.W × TileSize.W > Size.W. Per-tile reads address those raw
	// overlapping tiles at their stored positions, NOT a clean partition of the
	// stitched image; use the region API to reassemble pixels. For
	// OverlapBordered (DZI/SZI overlap>0) and OverlapNone, Grid DOES tile Size.
	// See Overlapping / OverlapMode.
```

Reword the `TileOverlap` field doc to:

```go
	// TileOverlap is the per-tile overlap magnitude. For OverlapBordered it is
	// {ov, ov} (the DZI Overlap attribute), always non-zero. For OverlapStitched
	// it is the BIF L0 magnitude where one is meaningful, but {0,0} on BIF
	// reduced levels (per-frame placement is authoritative there). Zero for
	// OverlapNone. NOT a reliable overlap test — gate on Overlapping/OverlapMode.
	TileOverlap Point
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ -run TestLevelOverlapModeField -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add image.go image_overlapmode_test.go
git commit -m "feat(api): Level.OverlapMode field; reword Overlapping/Grid/TileOverlap docs"
```

---

## Task 3: `Level.TileContentRect` pure-field method

**Files:**
- Modify: `level_reads.go` (add method near `StitchedGridFor`, ~line 149)
- Test: `level_contentrect_test.go` (new, package `opentile`)

- [ ] **Step 1: Write the failing test**

Create `level_contentrect_test.go`:

```go
package opentile

import "testing"

func lvl(mode OverlapMode, w, h, t, ov int) Level {
	cols := (w + t - 1) / t
	rows := (h + t - 1) / t
	tov := Point{}
	if mode == OverlapBordered {
		tov = Point{X: ov, Y: ov}
	}
	return Level{
		Size: Size{W: w, H: h}, TileSize: Size{W: t, H: t},
		Grid: Size{W: cols, H: rows}, OverlapMode: mode, TileOverlap: tov,
	}
}

func TestTileContentRectBordered(t *testing.T) {
	// CMU-1 level-16 geometry: 46000x32914, T=256, ov=1, grid 180x129.
	l := lvl(OverlapBordered, 46000, 32914, 256, 1)
	cases := []struct {
		c, r, ox, oy, w, h int
	}{
		{0, 0, 0, 0, 256, 256},     // corner: no left/top overlap
		{1, 1, 1, 1, 256, 256},     // interior: +1 left/top
		{179, 0, 1, 0, 176, 256},   // right edge: clipped width 176
		{0, 128, 0, 1, 256, 146},   // bottom edge: clipped height 146
		{179, 128, 1, 1, 176, 146}, // far corner
	}
	for _, c := range cases {
		got, ok := l.TileContentRect(c.c, c.r)
		if !ok {
			t.Errorf("(%d,%d): ok=false, want true", c.c, c.r)
			continue
		}
		want := Region{Origin: Point{X: c.ox, Y: c.oy}, Size: Size{W: c.w, H: c.h}}
		if got != want {
			t.Errorf("(%d,%d) = %+v, want %+v", c.c, c.r, got, want)
		}
	}
}

func TestTileContentRectNoneIsFullCell(t *testing.T) {
	l := lvl(OverlapNone, 46000, 32914, 256, 0)
	got, ok := l.TileContentRect(1, 1)
	if !ok || got != (Region{Origin: Point{}, Size: Size{W: 256, H: 256}}) {
		t.Errorf("None interior = %+v ok=%v, want full cell at origin", got, ok)
	}
	// last column clips, origin stays (0,0)
	got, _ = l.TileContentRect(179, 0)
	if got.Origin != (Point{}) || got.Size.W != 176 {
		t.Errorf("None right-edge = %+v, want origin(0,0) width 176", got)
	}
}

func TestTileContentRectStitchedAndOOB(t *testing.T) {
	if _, ok := lvl(OverlapStitched, 1000, 1000, 256, 0).TileContentRect(0, 0); ok {
		t.Error("stitched: ok=true, want false (use region API)")
	}
	if _, ok := lvl(OverlapBordered, 1000, 1000, 256, 1).TileContentRect(99, 0); ok {
		t.Error("out-of-grid: ok=true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ -run TestTileContentRect -v`
Expected: FAIL — `l.TileContentRect undefined`.

- [ ] **Step 3: Write minimal implementation**

In `level_reads.go`, add:

```go
// TileContentRect returns the content sub-rectangle within the DECODED tile
// (col,row) — its in-tile origin and size. For OverlapBordered levels the
// origin is the overlap border to skip ((col>0?ov:0, row>0?ov:0)) and the size
// is the content cell clipped at the level's right/bottom edge; a consumer
// crops a decoded tile to this rect to drop the redundant overlap. For
// OverlapNone it is the full clipped cell at origin (0,0) — a universal "where
// is the real content" answer. ok is false for OverlapStitched (Grid does not
// tile Size — use the region API) and for an out-of-grid (col,row).
func (l *Level) TileContentRect(col, row int) (Region, bool) {
	if l.OverlapMode == OverlapStitched {
		return Region{}, false
	}
	if col < 0 || row < 0 || col >= l.Grid.W || row >= l.Grid.H {
		return Region{}, false
	}
	var ox, oy int
	if l.OverlapMode == OverlapBordered {
		if col > 0 {
			ox = l.TileOverlap.X
		}
		if row > 0 {
			oy = l.TileOverlap.Y
		}
	}
	w := l.TileSize.W
	if rem := l.Size.W - col*l.TileSize.W; rem < w {
		w = rem
	}
	h := l.TileSize.H
	if rem := l.Size.H - row*l.TileSize.H; rem < h {
		h = rem
	}
	return Region{Origin: Point{X: ox, Y: oy}, Size: Size{W: w, H: h}}, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ -run TestTileContentRect -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add level_reads.go level_contentrect_test.go
git commit -m "feat(api): Level.TileContentRect(col,row) pure-field crop accessor"
```

---

## Task 4: `internal/dzi.ContentRect` helper

**Files:**
- Create: `internal/dzi/content.go`
- Test: `internal/dzi/content_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/dzi/content_test.go`:

```go
package dzi

import "testing"

func TestContentRect(t *testing.T) {
	// Level 46000x32914, T=256, ov=1 (CMU-1 level 16).
	cases := []struct {
		c, r, ox, oy, w, h int
	}{
		{0, 0, 0, 0, 256, 256},
		{1, 1, 1, 1, 256, 256},
		{179, 0, 1, 0, 176, 256},
		{0, 128, 0, 1, 256, 146},
		{179, 128, 1, 1, 176, 146},
	}
	for _, c := range cases {
		ox, oy, w, h := ContentRect(c.c, c.r, 46000, 32914, 256, 1)
		if ox != c.ox || oy != c.oy || w != c.w || h != c.h {
			t.Errorf("ContentRect(%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.c, c.r, ox, oy, w, h, c.ox, c.oy, c.w, c.h)
		}
	}
}

func TestContentRectZeroOverlap(t *testing.T) {
	ox, oy, w, h := ContentRect(1, 1, 1000, 1000, 256, 0)
	if ox != 0 || oy != 0 || w != 256 || h != 256 {
		t.Errorf("overlap=0 interior = (%d,%d,%d,%d), want (0,0,256,256)", ox, oy, w, h)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dzi/ -run TestContentRect -v`
Expected: FAIL — `undefined: ContentRect`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/dzi/content.go`:

```go
package dzi

// ContentRect returns the content sub-rectangle (offX, offY, w, h) within the
// stored/decoded tile (col,row) of a level sized levelW×levelH with the given
// tileSize and overlap. offX/offY are the in-tile content offset (the overlap
// border present on the left/top edge when the tile has a neighbour there); w/h
// are the content cell size, clipped at the level's right/bottom edge. The
// right/bottom overlap (present when not on the last column/row) does not affect
// the content origin or size, so it needs no explicit term here.
func ContentRect(col, row, levelW, levelH, tileSize, overlap int) (offX, offY, w, h int) {
	if col > 0 {
		offX = overlap
	}
	if row > 0 {
		offY = overlap
	}
	w = tileSize
	if rem := levelW - col*tileSize; rem < w {
		w = rem
	}
	h = tileSize
	if rem := levelH - row*tileSize; rem < h {
		h = rem
	}
	return offX, offY, w, h
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dzi/ -run TestContentRect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dzi/content.go internal/dzi/content_test.go
git commit -m "feat(internal/dzi): ContentRect helper for overlap tile cropping"
```

---

## Task 5: DZI reader — accept Overlap>0 + regionLayout/subtileLayout

**Files:**
- Modify: `formats/dzi/tiler.go` (drop guard; carry overlap; populate `Level` fields; add tiler `regionLayout`/`subtileLayout` methods)
- Modify: `formats/dzi/level.go` (add `overlap` field + engine methods)
- Test: `formats/dzi/overlap_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `formats/dzi/overlap_test.go`:

```go
package dzi

import (
	"testing"

	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// buildOverlapTiler constructs a Tiler directly from a synthetic manifest, no
// filesystem needed — exercises the layout math only.
func buildOverlapTiler(t *testing.T, w, h, tile, overlap int) *Tiler {
	t.Helper()
	tl := &Tiler{}
	tl.manifest = idzi.Manifest{Width: w, Height: h, TileSize: tile, Overlap: overlap, Format: "jpeg"}
	tl.filesDir = "/nonexistent"
	tl.buildLevels()
	return tl
}

func TestDZIOverlapLevelFields(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	lv, err := tl.Level(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lv.OverlapMode.String() != "bordered" || !lv.Overlapping {
		t.Errorf("mode=%v overlapping=%v, want bordered/true", lv.OverlapMode, lv.Overlapping)
	}
	if lv.TileOverlap.X != 1 || lv.TileOverlap.Y != 1 {
		t.Errorf("TileOverlap=%v, want {1,1}", lv.TileOverlap)
	}
}

func TestDZIZeroOverlapCleanGrid(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 0)
	lv, _ := tl.Level(0, 0)
	if lv.OverlapMode != 0 /*OverlapNone*/ || lv.Overlapping {
		t.Errorf("overlap=0: mode=%v overlapping=%v, want none/false", lv.OverlapMode, lv.Overlapping)
	}
	// StitchedSize gates the composite path off for overlap=0.
	if _, _, ok := tl.StitchedSize(0); ok {
		t.Error("overlap=0 StitchedSize ok=true, want false (fast path)")
	}
}

func TestDZISubtileSourceCropsBorder(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	// L0 is full res. Interior unit (1,1) sources tile (1,1) cropped at (1,1).
	sc, sr, cx, cy := tl.SubtileSource(0, 1, 1)
	if sc != 1 || sr != 1 || cx != 1 || cy != 1 {
		t.Errorf("SubtileSource(0,1,1) = (%d,%d,%d,%d), want (1,1,1,1)", sc, sr, cx, cy)
	}
	// Corner (0,0) has no left/top overlap.
	if _, _, cx, cy := tl.SubtileSource(0, 0, 0); cx != 0 || cy != 0 {
		t.Errorf("SubtileSource(0,0,0) crop = (%d,%d), want (0,0)", cx, cy)
	}
	x, y, ok := tl.TileOrigin(0, 1, 1)
	if !ok || x != 256 || y != 256 {
		t.Errorf("TileOrigin(0,1,1) = (%d,%d,%v), want (256,256,true)", x, y, ok)
	}
	uw, uh := tl.UnitSize(0)
	if uw != 256 || uh != 256 {
		t.Errorf("UnitSize(0) = (%d,%d), want (256,256)", uw, uh)
	}
	sw, sh, ok := tl.StitchedSize(0)
	if !ok || sw != 46000 || sh != 32914 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (46000,32914,true)", sw, sh, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/dzi/ -run TestDZI -v`
Expected: FAIL — `manifestFor undefined` / `tl.SubtileSource undefined`.

- [ ] **Step 3a: Add a manifest helper + drop the guard (tiler.go)**

In `formats/dzi/tiler.go`, replace the `Overlap>0` rejection in `openBareDZI`:

```go
	t := &Tiler{filesDir: filesDir, manifest: m}
	t.buildLevels()
	return t, nil
```

(i.e. delete the `if m.Overlap > 0 { return nil, ... }` block.) The synthetic
manifest used by the test is built inline in `buildOverlapTiler`
(`idzi.Manifest{Width, Height, TileSize, Overlap, Format}` — confirmed field
names in `internal/dzi/manifest.go`); no production helper is added.

- [ ] **Step 3b: Carry overlap onto the engine + populate Level fields (tiler.go)**

In `buildLevels`, set the engine's overlap and the `Level` fields. Inside the
`for i := 0; i <= maxLevel; i++` loop, add `overlap: t.manifest.Overlap,` to the
`engines[i] = &level{...}` literal, and add the overlap fields to the
`valueLevels[i] = opentile.Level{...}` literal:

```go
		mode := opentile.OverlapNone
		tov := opentile.Point{}
		if t.manifest.Overlap > 0 {
			mode = opentile.OverlapBordered
			tov = opentile.Point{X: t.manifest.Overlap, Y: t.manifest.Overlap}
		}
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         opentile.Size{W: w, H: h},
			TileSize:     opentile.Size{W: t.manifest.TileSize, H: t.manifest.TileSize},
			Grid:         opentile.Size{W: cols, H: rows},
			Compression:  comp,
			Downsample:   float64(l0W) / float64(w),
			OverlapMode:  mode,
			Overlapping:  t.manifest.Overlap > 0,
			TileOverlap:  tov,
		}
```

- [ ] **Step 3c: Engine layout methods (level.go)**

In `formats/dzi/level.go`, add `overlap int` to the `level` struct, then add:

```go
import idzi "github.com/wsilabs/opentile-go/internal/dzi" // if not already imported

// tileOrigin returns the content cell's top-left in level pixels.
func (l *level) tileOrigin(col, row int) (x, y int, ok bool) {
	if !l.inBounds(col, row) {
		return 0, 0, false
	}
	return col * l.tileSize, row * l.tileSize, true
}

// tilesIntersecting returns the content cells overlapping [x,y,x+w,y+h).
func (l *level) tilesIntersecting(x, y, w, h int) []struct{ Col, Row int } {
	if w <= 0 || h <= 0 {
		return nil
	}
	c0, r0 := x/l.tileSize, y/l.tileSize
	c1, r1 := (x+w-1)/l.tileSize, (y+h-1)/l.tileSize
	if c0 < 0 {
		c0 = 0
	}
	if r0 < 0 {
		r0 = 0
	}
	if c1 >= l.cols {
		c1 = l.cols - 1
	}
	if r1 >= l.rows {
		r1 = l.rows - 1
	}
	var out []struct{ Col, Row int }
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			out = append(out, struct{ Col, Row int }{c, r})
		}
	}
	return out
}

// stitchedSize gates the composite path: ok only when this level has overlap.
func (l *level) stitchedSize() (w, h int, ok bool) {
	return l.width, l.height, l.overlap > 0
}

// unitSize is the content cell size (TileSize). Edge clipping is handled by the
// compositor's region clamp.
func (l *level) unitSize() (w, h int) { return l.tileSize, l.tileSize }

// subtileSource maps a content cell to its (same) stored tile + the overlap
// crop origin within the decoded tile.
func (l *level) subtileSource(col, row int) (srcCol, srcRow, cropX, cropY int) {
	ox, oy, _, _ := idzi.ContentRect(col, row, l.width, l.height, l.tileSize, l.overlap)
	return col, row, ox, oy
}
```

- [ ] **Step 3d: Tiler regionLayout/subtileLayout methods (tiler.go)**

In `formats/dzi/tiler.go`, add (mirroring BIF's tiler delegation):

```go
// --- regionLayout / subtileLayout (only effective when Overlap>0; StitchedSize
// gates the composite path per level) ---

func (t *Tiler) TileOrigin(level, col, row int) (x, y int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.tileOrigin(col, row)
}

func (t *Tiler) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	eng, err := t.engine(0, level)
	if err != nil {
		return nil
	}
	return eng.tilesIntersecting(x, y, w, h)
}

func (t *Tiler) StitchedSize(level int) (w, h int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.stitchedSize()
}

func (t *Tiler) UnitSize(level int) (w, h int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0
	}
	return eng.unitSize()
}

func (t *Tiler) SubtileSource(level, col, row int) (srcCol, srcRow, cropX, cropY int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return col, row, 0, 0
	}
	return eng.subtileSource(col, row)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./formats/dzi/ -run TestDZI -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add formats/dzi/tiler.go formats/dzi/level.go formats/dzi/overlap_test.go
git commit -m "feat(dzi): accept Overlap>0; implement regionLayout/subtileLayout crop"
```

---

## Task 6: DZI end-to-end — synthetic PNG-DZI generator + composite parity

**Files:**
- Test: `formats/dzi/overlap_e2e_test.go` (new) — builds a lossless overlap=1 PNG DZI on disk, reads it back, asserts overlap-free composite == source. CI-safe (no committed binary; no cgo — PNG decode is pure-Go via the registered decoder).

- [ ] **Step 1: Write the failing test**

Create `formats/dzi/overlap_e2e_test.go`:

```go
package dzi_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"strconv"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// writeOverlapDZI generates a single-level overlap=ov DZI from src under dir,
// tiling exactly as libvips/OpenSeadragon do, with lossless PNG tiles. Returns
// the .dzi path. Only the deepest (full-res) level's tiles are written — the
// test reads only that level.
func writeOverlapDZI(t *testing.T, dir string, src *image.NRGBA, tileSize, ov int) string {
	t.Helper()
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	maxLevel := idzi.MaxLevel(w, h)
	filesDir := filepath.Join(dir, "img_files", itoa(maxLevel))
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cols, rows := idzi.GridDims(w, h, tileSize)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			ox, oy, cw, ch := idzi.ContentRect(c, r, w, h, tileSize, ov)
			// Stored tile spans content plus right/bottom overlap when present.
			rx := 0
			if c < cols-1 {
				rx = ov
			}
			by := 0
			if r < rows-1 {
				by = ov
			}
			sx, sy := c*tileSize-ox, r*tileSize-oy
			tw, th := ox+cw+rx, oy+ch+by
			tile := image.NewNRGBA(image.Rect(0, 0, tw, th))
			for ty := 0; ty < th; ty++ {
				for tx := 0; tx < tw; tx++ {
					tile.Set(tx, ty, src.At(sx+tx, sy+ty))
				}
			}
			f, err := os.Create(filepath.Join(filesDir, itoa(c)+"_"+itoa(r)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tile); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
	manifest := `<?xml version="1.0"?>` +
		`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="png" ` +
		`Overlap="` + itoa(ov) + `" TileSize="` + itoa(tileSize) + `">` +
		`<Size Width="` + itoa(w) + `" Height="` + itoa(h) + `"/></Image>`
	dziPath := filepath.Join(dir, "img.dzi")
	if err := os.WriteFile(dziPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dziPath
}

func itoa(n int) string { return strconv.Itoa(n) }

func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	return img
}

func TestDZIOverlapCompositeMatchesSource(t *testing.T) {
	dir := t.TempDir()
	src := gradient(70, 50) // 70x50 with T=16 => grid 5x4, interior+edge tiles
	dziPath := writeOverlapDZI(t, dir, src, 16, 1)

	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	l0, _ := s.Level(0) // deepest level == full res == src dims
	if l0.Size.W != 70 || l0.Size.H != 50 || l0.OverlapMode != opentile.OverlapBordered {
		t.Fatalf("level0 = %+v (mode %v), want 70x50 bordered", l0.Size, l0.OverlapMode)
	}
	got, err := l0.ReadRegion(opentile.Region{Origin: opentile.Point{}, Size: opentile.Size{W: 70, H: 50}})
	if err != nil {
		t.Fatal(err)
	}
	// Lossless PNG round-trip → exact match to the source pixels.
	if d := maxAbsDiff(got, src); d != 0 {
		t.Errorf("composite != source: maxAbsDiff=%d (overlap not cropped correctly)", d)
	}
	// StitchedTile of an interior cell is a clean 16x16 content tile.
	st, err := l0.StitchedTile(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.Width != 16 || st.Height != 16 {
		t.Errorf("StitchedTile(1,1) = %dx%d, want 16x16", st.Width, st.Height)
	}
	// DecodedTile returns the PADDED tile (interior 18x18 for ov=1).
	dt, err := l0.DecodedTile(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if dt.Width != 18 || dt.Height != 18 {
		t.Errorf("DecodedTile(1,1) = %dx%d, want 18x18 (padded)", dt.Width, dt.Height)
	}
}

func maxAbsDiff(img *decoder.Image, src *image.NRGBA) int {
	bpp := 3
	if img.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	max := 0
	for y := 0; y < src.Bounds().Dy() && y < img.Height; y++ {
		for x := 0; x < src.Bounds().Dx() && x < img.Width; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			want := []int{int(r >> 8), int(g >> 8), int(b >> 8)}
			for c := 0; c < 3; c++ {
				d := int(img.Pix[y*img.Stride+x*bpp+c]) - want[c]
				if d < 0 {
					d = -d
				}
				if d > max {
					max = d
				}
			}
		}
	}
	return max
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./formats/dzi/ -run TestDZIOverlapCompositeMatchesSource -v`
Expected: PASS if Task 5 is correct. If it FAILS with a non-zero `maxAbsDiff`, the
crop/placement is wrong — debug `subtileSource`/`tileOrigin` before proceeding.

- [ ] **Step 3: (only if failing) fix Task-5 layout** — no new code expected; this task validates Task 5 end-to-end.

- [ ] **Step 4: Confirm pass + no regression**

Run: `go test ./formats/dzi/ -count=1`
Expected: PASS (including the existing overlap=0 dzi tests, unchanged).

- [ ] **Step 5: Commit**

```bash
git add formats/dzi/overlap_e2e_test.go
git commit -m "test(dzi): synthetic overlap=1 PNG-DZI composite == source (CI-safe)"
```

---

## Task 7: SZI reader — accept Overlap>0 + regionLayout/subtileLayout

SZI is `formats/szi`; it imports `internal/dzi` as `dzi` (not `idzi`). Its
`level` struct is `{t *Tiler, dziLevel, openTileIdx, pyrIndex, width, height,
cols, rows, tileSize int, compression opentile.Compression}` — no `overlap`
field and no `inBounds` helper, so the engine methods inline their bounds check.

**Files:**
- Modify: `formats/szi/tiler.go`, `formats/szi/level.go`
- Test: `formats/szi/overlap_test.go` (new, package `szi`)

- [ ] **Step 1: Write the failing test**

Create `formats/szi/overlap_test.go`:

```go
package szi

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

func buildOverlapTiler(t *testing.T, w, h, tile, overlap int) *Tiler {
	t.Helper()
	tl := &Tiler{}
	tl.manifest = dzi.Manifest{Width: w, Height: h, TileSize: tile, Overlap: overlap, Format: "jpeg"}
	if err := tl.buildLevels(); err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestSZIOverlapFieldsAndLayout(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 1)
	lv, err := tl.Level(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lv.OverlapMode != opentile.OverlapBordered || !lv.Overlapping || lv.TileOverlap.X != 1 {
		t.Errorf("got mode=%v overlapping=%v tov=%v, want bordered/true/{1,1}", lv.OverlapMode, lv.Overlapping, lv.TileOverlap)
	}
	if _, _, ok := tl.StitchedSize(0); !ok {
		t.Error("StitchedSize ok=false, want true for overlap=1")
	}
	if _, _, cx, cy := tl.SubtileSource(0, 1, 1); cx != 1 || cy != 1 {
		t.Errorf("SubtileSource crop=(%d,%d), want (1,1)", cx, cy)
	}
	if sw, sh, ok := tl.StitchedSize(0); !ok || sw != 46000 || sh != 32914 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (46000,32914,true)", sw, sh, ok)
	}
}

func TestSZIZeroOverlapCleanGrid(t *testing.T) {
	tl := buildOverlapTiler(t, 46000, 32914, 256, 0)
	lv, _ := tl.Level(0, 0)
	if lv.OverlapMode != opentile.OverlapNone || lv.Overlapping {
		t.Errorf("overlap=0: mode=%v overlapping=%v, want none/false", lv.OverlapMode, lv.Overlapping)
	}
	if _, _, ok := tl.StitchedSize(0); ok {
		t.Error("overlap=0 StitchedSize ok=true, want false (fast path)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/szi/ -run TestSZI.*Overlap -v`
Expected: FAIL — `tl.buildLevels`/`tl.StitchedSize`/`tl.SubtileSource` undefined
or the guard rejects.

- [ ] **Step 3a: Drop the guard (tiler.go)**

In `formats/szi/tiler.go`, delete the rejection block (~line 246):

```go
	if m.Overlap > 0 {
		return fmt.Errorf("szi: %s: Overlap=%d: %w", manifestEntry.Name, m.Overlap, dzi.ErrOverlapNotSupported)
	}
```

- [ ] **Step 3b: Add `overlap` to the level struct + populate fields (level.go, tiler.go)**

Add `overlap int` to the `level` struct in `formats/szi/level.go` (e.g. after
`tileSize int`).

In `formats/szi/tiler.go` `buildLevels`, set `overlap: t.manifest.Overlap` on the
`eng := &level{...}` literal, and add the overlap fields to the
`valueLevels[i] = opentile.Level{...}` literal:

```go
		mode := opentile.OverlapNone
		tov := opentile.Point{}
		if t.manifest.Overlap > 0 {
			mode = opentile.OverlapBordered
			tov = opentile.Point{X: t.manifest.Overlap, Y: t.manifest.Overlap}
		}
		// ... in the opentile.Level{...} literal, add:
		//   OverlapMode: mode,
		//   Overlapping: t.manifest.Overlap > 0,
		//   TileOverlap: tov,
```

- [ ] **Step 3c: Engine layout methods (level.go)**

Add to `formats/szi/level.go` (import `dzi "github.com/wsilabs/opentile-go/internal/dzi"`
if not already imported):

```go
func (l *level) tileOrigin(col, row int) (x, y int, ok bool) {
	if col < 0 || row < 0 || col >= l.cols || row >= l.rows {
		return 0, 0, false
	}
	return col * l.tileSize, row * l.tileSize, true
}

func (l *level) tilesIntersecting(x, y, w, h int) []struct{ Col, Row int } {
	if w <= 0 || h <= 0 {
		return nil
	}
	c0, r0 := x/l.tileSize, y/l.tileSize
	c1, r1 := (x+w-1)/l.tileSize, (y+h-1)/l.tileSize
	if c0 < 0 {
		c0 = 0
	}
	if r0 < 0 {
		r0 = 0
	}
	if c1 >= l.cols {
		c1 = l.cols - 1
	}
	if r1 >= l.rows {
		r1 = l.rows - 1
	}
	var out []struct{ Col, Row int }
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			out = append(out, struct{ Col, Row int }{c, r})
		}
	}
	return out
}

func (l *level) stitchedSize() (w, h int, ok bool) { return l.width, l.height, l.overlap > 0 }

func (l *level) unitSize() (w, h int) { return l.tileSize, l.tileSize }

func (l *level) subtileSource(col, row int) (srcCol, srcRow, cropX, cropY int) {
	ox, oy, _, _ := dzi.ContentRect(col, row, l.width, l.height, l.tileSize, l.overlap)
	return col, row, ox, oy
}
```

- [ ] **Step 3d: Tiler regionLayout/subtileLayout methods (tiler.go)**

Add to `formats/szi/tiler.go` (delegating to `t.engine(0, level)`, exactly like
DZI Task 5 Step 3d — the SZI `engine(image, level)` helper exists at
`formats/szi/tiler.go:372`):

```go
func (t *Tiler) TileOrigin(level, col, row int) (x, y int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.tileOrigin(col, row)
}

func (t *Tiler) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	eng, err := t.engine(0, level)
	if err != nil {
		return nil
	}
	return eng.tilesIntersecting(x, y, w, h)
}

func (t *Tiler) StitchedSize(level int) (w, h int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.stitchedSize()
}

func (t *Tiler) UnitSize(level int) (w, h int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0
	}
	return eng.unitSize()
}

func (t *Tiler) SubtileSource(level, col, row int) (srcCol, srcRow, cropX, cropY int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return col, row, 0, 0
	}
	return eng.subtileSource(col, row)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./formats/szi/ -run TestSZI.*Overlap -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add formats/szi/tiler.go formats/szi/level.go formats/szi/overlap_test.go
git commit -m "feat(szi): accept Overlap>0; implement regionLayout/subtileLayout crop"
```

---

## Task 8: SZI end-to-end — synthetic overlap=1 SZI composite parity

**Files:**
- Test: `formats/szi/overlap_e2e_test.go` (new) — build a tiny overlap=1 SZI (a ZIP wrapping a PNG DZI) in `t.TempDir()`, read it back, assert composite == source.

- [ ] **Step 1: Write the failing test**

Create `formats/szi/overlap_e2e_test.go`. Reuse DZI's tiling logic but pack the
result into a ZIP per the SZI layout (a `.szi` is a ZIP containing the `.dzi`
manifest + `<name>_files/...`). Inspect `formats/szi/tiler.go` for the exact
in-ZIP entry names SZI expects (manifest entry + tile path convention), then:

```go
package szi_test

// 1. Generate the same gradient source + PNG tiles as the DZI e2e test
//    (factor the generator into an internal testhelper or copy it).
// 2. Write them into a zip.Writer with the SZI entry layout (manifest +
//    <base>_files/<level>/<col>_<row>.png), STORE (uncompressed) to match SZI's
//    mmap-aliased read path.
// 3. opentile.OpenFile(sziPath); read L0 full region; assert maxAbsDiff==0 vs
//    source; assert DecodedTile(1,1) padded 18x18 and StitchedTile(1,1) 16x16.
```

(Full code: mirror `TestDZIOverlapCompositeMatchesSource`, replacing the on-disk
`_files` tree with `archive/zip` entries. Keep tiles `STORE`d, not `Deflate`d, so
the SZI mmap path is exercised.)

- [ ] **Step 2: Run test to verify it fails/passes**

Run: `go test ./formats/szi/ -run TestSZIOverlapComposite -v`
Expected: PASS if Task 7 is correct.

- [ ] **Step 3: (only if failing) fix Task-7 SZI layout.**

- [ ] **Step 4: Confirm no regression**

Run: `go test ./formats/szi/ -count=1`
Expected: PASS (existing overlap=0 SZI tests unchanged).

- [ ] **Step 5: Commit**

```bash
git add formats/szi/overlap_e2e_test.go
git commit -m "test(szi): synthetic overlap=1 SZI composite == source (CI-safe)"
```

---

## Task 9: Local-only CMU-1 cross-overlap parity gate

**Files:**
- Test: `formats/dzi/overlap_parity_test.go` (new) — uses the two libvips fixtures in `sample_files/dzi/`; skips when absent (local-only, like other large fixtures).

- [ ] **Step 1: Write the failing test**

Create `formats/dzi/overlap_parity_test.go`:

```go
package dzi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestDZIOverlapParityVsZero asserts that reading the overlap=1 libvips DZI of
// CMU-1 produces the same pixels as the overlap=0 DZI of the SAME slide (both
// encode the identical image). Tiles are independent JPEGs, so the bar is low
// MAD (JPEG re-encode noise), not bit-exact. Local-only: skips when the
// fixtures are absent. A wrong crop/placement shifts content and spikes MAD.
func TestDZIOverlapParityVsZero(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "/Volumes/Ext/GitHub/opentile-go/sample_files"
	}
	base := filepath.Join(dir, "dzi")
	p0 := filepath.Join(base, "CMU-1_dzi_libvips_overlap_0.dzi")
	p1 := filepath.Join(base, "CMU-1_dzi_libvips_overlap_1.dzi")
	for _, p := range []string{p0, p1} {
		if _, err := os.Stat(p); err != nil {
			t.Skip("CMU-1 dzi fixtures absent")
		}
	}
	s0, err := opentile.OpenFile(p0)
	if err != nil {
		t.Fatal(err)
	}
	defer s0.Close()
	s1, err := opentile.OpenFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	// Regions chosen to stress the crop: tile-aligned, offset, seam-crossing,
	// and a right/bottom-edge region. (CMU-1 L0 = 46000x32914.)
	regions := []opentile.Region{
		{Origin: opentile.Point{X: 5120, Y: 5120}, Size: opentile.Size{W: 600, H: 600}},   // interior, multi-tile
		{Origin: opentile.Point{X: 5133, Y: 5251}, Size: opentile.Size{W: 517, H: 503}},    // offset, seam-crossing
		{Origin: opentile.Point{X: 45600, Y: 32500}, Size: opentile.Size{W: 400, H: 414}},  // bottom-right edge
	}
	for li := 0; li < 3; li++ { // a few levels
		l0, e0 := s0.Level(li)
		l1, e1 := s1.Level(li)
		if e0 != nil || e1 != nil {
			continue
		}
		if l1.OverlapMode != opentile.OverlapBordered {
			t.Fatalf("L%d overlap=1 fixture OverlapMode=%v, want bordered", li, l1.OverlapMode)
		}
		for _, r := range regions {
			rr := scaleRegion(r, li)
			if rr.Origin.X+rr.Size.W > l0.Size.W || rr.Origin.Y+rr.Size.H > l0.Size.H {
				continue
			}
			a, err := l0.ReadRegion(rr)
			if err != nil {
				t.Fatal(err)
			}
			b, err := l1.ReadRegion(rr)
			if err != nil {
				t.Fatal(err)
			}
			if mad := regionMAD(a, b); mad > 4.0 {
				t.Errorf("L%d region %+v: overlap0-vs-overlap1 MAD=%.2f, want <=4 (crop/placement wrong)", li, rr, mad)
			}
		}
	}
}

func scaleRegion(r opentile.Region, level int) opentile.Region {
	s := 1 << level
	return opentile.Region{
		Origin: opentile.Point{X: r.Origin.X / s, Y: r.Origin.Y / s},
		Size:   r.Size,
	}
}

func regionMAD(a, b *decoder.Image) float64 {
	bppA, bppB := 3, 3
	if a.Format == decoder.PixelFormatRGBA {
		bppA = 4
	}
	if b.Format == decoder.PixelFormatRGBA {
		bppB = 4
	}
	w, h := a.Width, a.Height
	if b.Width < w {
		w = b.Width
	}
	if b.Height < h {
		h = b.Height
	}
	var sum, n float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				d := int(a.Pix[y*a.Stride+x*bppA+c]) - int(b.Pix[y*b.Stride+x*bppB+c])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}
```

- [ ] **Step 2: Run with fixtures**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dzi/ -run TestDZIOverlapParityVsZero -v`
Expected: PASS with low MAD logged. If MAD is high (content shift), the crop is
wrong — debug before continuing.

- [ ] **Step 3: Confirm CI-skip behavior**

Run: `go test ./formats/dzi/ -run TestDZIOverlapParityVsZero -v` (no `OPENTILE_TESTDIR`)
Expected: SKIP ("CMU-1 dzi fixtures absent") — confirms it won't break CI. (On
the dev machine the hardcoded fallback path resolves; that's fine, it still
passes locally.)

- [ ] **Step 4: Add a ScaledStrips smoke assertion**

`ReadRegionScaled` and `ScaledStrips` (DZI is exposed via `*Pyramid`) consume the
**same** `subtileLayout` (`strip_iterator.go` / `strip_workers.go` were made
subtile-aware by the BIF work), so the crop is applied identically — `ReadRegion`
parity above is the primary proxy. Add one smoke check that `ScaledStrips` runs
and yields non-empty strips on the overlap=1 fixture (confirming the strip path
engages the layout without error). Append to `TestDZIOverlapParityVsZero` or a
sibling test:

```go
// Smoke: the scaled-strip path engages the subtile layout without error.
pyr := s1.Pyramids()[0]
it, err := pyr.ScaledStrips(opentile.Size{W: 1024, H: 1024}) // Scale to ~1024 wide; adjust to actual signature
if err != nil {
	t.Fatalf("ScaledStrips: %v", err)
}
defer it.Close()
got := 0
for strip := range it.Strips(context.Background()) {
	if strip.Err != nil {
		t.Fatalf("strip: %v", strip.Err)
	}
	got++
	if got >= 2 {
		break
	}
}
if got == 0 {
	t.Error("ScaledStrips yielded no strips on overlap=1 fixture")
}
```

> Confirm the exact `ScaledStrips` signature and iterator shape against
> `level_reads.go` / the `*Pyramid` methods before finalizing this snippet; the
> intent is a no-error smoke, not a pixel gate (pixel correctness is covered by
> the shared-path argument + the `ReadRegion` parity gate).

- [ ] **Step 5: Commit**

```bash
git add formats/dzi/overlap_parity_test.go
git commit -m "test(dzi): local-only CMU-1 overlap0-vs-overlap1 region parity + ScaledStrips smoke"
```

---

## Task 10: Docs + changelog

**Files:**
- Modify: `docs/formats/dzi.md`, `docs/formats/szi.md` (if present — else skip the missing one) — overlap support section.
- Modify: `docs/deferred.md` — mark the R19 "Overlap>0 deferred" note resolved.
- Modify: `CHANGELOG.md` — new `## [Unreleased]` (or next-version) entry.

- [ ] **Step 1: Update format docs**

In `docs/formats/dzi.md` (and `szi.md`), add a section documenting: `Overlap>0`
is read; `OverlapMode == OverlapBordered`; `TileOverlap = {ov,ov}`; per-tile
`DecodedTile` returns the padded tile + `TileContentRect` gives the crop;
`StitchedTile`/`ReadRegion`/`ScaledStrips` return clean composited pixels;
`Overlap=0` is unchanged/byte-identical.

- [ ] **Step 2: Update deferred.md**

Find the R19 / "Overlap>0 crop/placement deferred" note and mark it RESOLVED
(date 2026-06-26), pointing at this spec/plan and the `OverlapMode`/
`TileContentRect` API.

- [ ] **Step 3: CHANGELOG entry**

Add to `CHANGELOG.md` under a new top version section:

```markdown
### Added

- **DZI/SZI `Overlap > 0` support.** Bare DZI and SZI pyramids whose manifest
  declares a tile overlap now read correctly: `ReadRegion` / `ReadRegionScaled` /
  `ScaledStrips` / `StitchedTile` return clean, overlap-free composited pixels,
  while raw `Tile()` / `DecodedTile()` return the on-disk padded tile. New
  `opentile.OverlapMode` enum (`OverlapNone` / `OverlapBordered` /
  `OverlapStitched`) on `Level`; `Level.Overlapping` is retained as the derived
  convenience `OverlapMode != OverlapNone` (value unchanged for every previously
  readable slide). New `Level.TileContentRect(col,row) (Region, bool)` returns
  the per-tile content crop within a decoded tile. `Overlap = 0` reads are
  byte-identical. Validated against libvips overlap_0/overlap_1 DZIs of the same
  slide.

### Changed

- `Level.Overlapping` / `Level.Grid` / `Level.TileOverlap` field docs reworded to
  distinguish "padded tiles" (`OverlapBordered`) from "compacted grid"
  (`OverlapStitched`); the "Grid does not tile Size" property now belongs to
  `OverlapMode == OverlapStitched`.
```

- [ ] **Step 4: Verify full suite + gates**

Run:
```bash
go vet ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -count=1
CGO_ENABLED=0 go build -tags nocgo ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dzi/ ./formats/szi/ -race -count=1
```
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add docs/formats/dzi.md docs/formats/szi.md docs/deferred.md CHANGELOG.md
git commit -m "docs: DZI/SZI Overlap>0 support; retire R19 overlap deferral"
```

---

## Final review (after all tasks)

- Dispatch a final code-review over the whole branch diff.
- Confirm: `Overlap=0` byte-identical (existing dzi/szi tests unchanged); new
  enum value unchanged for all readable slides; `nocgo` builds; `-race` clean.
- Then use `superpowers:finishing-a-development-branch` to open the PR.
