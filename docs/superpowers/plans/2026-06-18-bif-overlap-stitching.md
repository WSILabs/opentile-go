# BIF overlap-aware stitching (GH #60) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **GIT DISCIPLINE (mandatory for every subagent):** Do all work on the current branch `fix/bif-l0-grid-width-60`. NEVER run `git checkout <sha>`, `git switch --detach`, or anything that detaches HEAD. NEVER `git reset --hard`, `git rebase`, `git push --force`, or delete branches. Commit on the current branch only. If you think you need to change branches or rewrite history, STOP and report instead. (A detached-HEAD incident orphaned two task commits during the v0.45.0 build — this guard exists because of it.)

**Goal:** Make BIF *stitched-pixel* output (`Level.Size`, `ReadRegion`, `ReadRegionScaled`, `ScaledStrips`) pixel-exact for the DP generation by computing an overlap-aware tile layout from the Roche whitepaper, while leaving the raw/decoded per-tile API byte-identical and the 10 non-BIF formats' compositing path bit-identical. Legacy (OS-1) ships as documented best-effort (exact overlap deferred per #60-legacy).

**Architecture:** A pure, file-free stitch engine in `formats/bif` turns parsed `EncodeInfo` + grid geometry into a `Layout` (per-tile stitched origin + stitched dimensions). `newLevelImpl` computes the layout once at Open and exposes `TileOrigin`/`TilesIntersecting`/`StitchedSize`. The opentile core gains an optional `regionLayout` capability interface, discovered through the existing `UnwrapReader` chain (the `MetadataOf` pattern); `imageReadRegionImpl` takes a layout-aware branch when present and the unchanged naive-grid path otherwise. The BIF `Tiler` implements `regionLayout` by delegating to its `levelImpls`.

**Tech Stack:** Go 1.23+, existing `internal/bifxml` parser, `region.go` compositing, `decoder.Image`. Tests: fixture-free engine units (CI-safe), oracle-pinned DP dimension constants, bio-formats black-box pixel oracle (fixture-gated, build tag), existing tifffile/bio-formats placement oracles.

**Licensing (hard constraint, repeated from the spec):** The ONLY source for stitch algorithm/layout semantics is `sample_files/bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf`. `bio-formats` `VentanaReader.java` (GPL v2) and `openslide` (LGPL 2.1) are used ONLY as black-box pixel/dimension oracles — never read for expression to translate. Every algorithm comment cites the whitepaper section, never another reader's source.

**Design doc:** `docs/superpowers/specs/2026-06-18-bif-overlap-stitching-design.md`

---

## File Structure

- `internal/bifxml/bifxml.go` — MODIFY: add `TileJoint.Confidence int` (additive parse).
- `internal/bifxml/bifxml_test.go` — MODIFY: assert Confidence parse.
- `formats/bif/stitch.go` — CREATE: the pure stitch engine (`StitchInput`, `TilePlacement`, `Layout`, `BuildLayout`, queries). Unexported package-internal file (Q-A1 resolved: lives in `formats/bif`, not a subpackage).
- `formats/bif/stitch_test.go` — CREATE: fixture-free engine unit tests.
- `formats/bif/testdata/ventana1_encodeinfo.xml` — CREATE: golden EncodeInfo XMP captured from Ventana-1 (geometry only, no PHI) for the CI-safe DP exact-dim test.
- `formats/bif/stitch_golden_test.go` — CREATE: DP exact-dimension assertion + legacy honesty lock.
- `formats/bif/level.go` — MODIFY: `levelImpl` gains a cached `*Layout`; `newLevelImpl` builds it; add `TileOrigin`/`TilesIntersecting`/`StitchedSize`; `size` for L0 becomes stitched.
- `formats/bif/bif.go` — MODIFY: `Tiler` implements `regionLayout`; fix `Downsample` to use stitched L0 width.
- `region.go` — MODIFY: add `regionLayout` interface + `regionLayoutOf` (UnwrapReader walk) + layout-aware branch in `imageReadRegionImpl`.
- `region_layout_test.go` — CREATE: non-BIF path unchanged (table), BIF layout-aware path correctness.
- `tests/oracle/bif_stitch_oracle_test.go` — CREATE: bio-formats tolerance pixel oracle (build-tagged, fixture-gated).
- `docs/formats/bif.md` — MODIFY: stitching section.
- `docs/migrations/2026-06-18-bif-level-size-stitched.md` — CREATE: `Level.Size` value-change note.
- `CHANGELOG.md` — MODIFY: `[Unreleased]` entry.

---

## Task 1: Add `TileJoint.Confidence` to bifxml

**Files:**
- Modify: `internal/bifxml/bifxml.go` (struct `TileJoint` ~line 93; `parseTileJoint` ~line 429)
- Test: `internal/bifxml/bifxml_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bifxml/bifxml_test.go`:

```go
func TestParseEncodeInfoTileJointConfidence(t *testing.T) {
	xmp := []byte(`<EncodeInfo Ver="2"><SlideStitchInfo>` +
		`<ImageInfo AOIScanned="1" AOIIndex="0" NumRows="1" NumCols="2">` +
		`<TileJointInfo FlagJoined="1" Direction="RIGHT" Tile1="0" Tile2="1" OverlapX="120" OverlapY="0" Confidence="100"/>` +
		`</ImageInfo></SlideStitchInfo></EncodeInfo>`)
	ei, err := ParseEncodeInfo(xmp)
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	if len(ei.ImageInfos) != 1 || len(ei.ImageInfos[0].Joints) != 1 {
		t.Fatalf("want 1 ImageInfo with 1 joint, got %+v", ei.ImageInfos)
	}
	if got := ei.ImageInfos[0].Joints[0].Confidence; got != 100 {
		t.Errorf("Confidence = %d, want 100", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bifxml/ -run TestParseEncodeInfoTileJointConfidence -v`
Expected: FAIL — `Confidence` is not a field of `TileJoint` (compile error), or value 0.

- [ ] **Step 3: Add the field and parse it**

In `internal/bifxml/bifxml.go`, add to `TileJoint` (after `OverlapX, OverlapY int`):

```go
	OverlapX, OverlapY int
	Confidence         int // 0..100; whitepaper §"Image Stitching": only Confidence==100 joins are trusted
```

In `parseTileJoint`, add a case:

```go
		case "OverlapY":
			tj.OverlapY = parseInt(a.Value)
		case "Confidence":
			tj.Confidence = parseInt(a.Value)
		}
```

Also update the package doc comment on `TileJoint` if it enumerates fields (it doesn't currently — skip).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bifxml/ -run TestParseEncodeInfoTileJointConfidence -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bifxml/bifxml.go internal/bifxml/bifxml_test.go
git commit -m "feat(bifxml): parse TileJointInfo Confidence attribute (#60)"
```

---

## Task 2: Stitch engine — types and naive layout

**Files:**
- Create: `formats/bif/stitch.go`
- Test: `formats/bif/stitch_test.go`

The naive layout is the fallback (no joints, `Ver<2`, non-DP, or pyramid levels ≥1): every tile at its regular grid position, dimensions = grid × tile.

- [ ] **Step 1: Write the failing test**

Create `formats/bif/stitch_test.go`:

```go
package bif

import "testing"

func TestBuildLayoutNaiveNoEncodeInfo(t *testing.T) {
	in := StitchInput{Cols: 3, Rows: 2, TileW: 1024, TileH: 1024, EncodeInfo: nil, Generation: GenerationLegacyIScan}
	l := BuildLayout(in)
	if l.Width != 3*1024 || l.Height != 2*1024 {
		t.Fatalf("dims = %dx%d, want 3072x2048", l.Width, l.Height)
	}
	x, y, ok := l.TileOrigin(2, 1)
	if !ok || x != 2*1024 || y != 1*1024 {
		t.Errorf("TileOrigin(2,1) = (%d,%d,%v), want (2048,1024,true)", x, y, ok)
	}
	if _, _, ok := l.TileOrigin(3, 0); ok {
		t.Errorf("TileOrigin(3,0) should be out of grid")
	}
}

func TestLayoutTilesIntersecting(t *testing.T) {
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 100, TileH: 100, Generation: GenerationLegacyIScan})
	got := l.TilesIntersecting(50, 50, 100, 100) // spans tiles (0,0)(1,0)(0,1)(1,1)
	if len(got) != 4 {
		t.Fatalf("intersecting = %d tiles, want 4: %+v", len(got), got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/bif/ -run 'TestBuildLayoutNaive|TestLayoutTilesIntersecting' -v`
Expected: FAIL — `StitchInput`/`BuildLayout`/`Layout` undefined.

- [ ] **Step 3: Write the engine types + naive `BuildLayout` + queries**

Create `formats/bif/stitch.go`:

```go
package bif

import "github.com/wsilabs/opentile-go/internal/bifxml"

// StitchInput is the pure, file-free description the stitch engine needs to
// compute a layout. EncodeInfo may be nil (legacy slides without it) → naive.
type StitchInput struct {
	Cols, Rows   int
	TileW, TileH int
	EncodeInfo   *bifxml.EncodeInfo
	Generation   Generation
}

// TilePlacement is where one image-grid tile lands in stitched output space.
type TilePlacement struct {
	Col, Row int
	X, Y     int
}

// Layout is the stitch engine's result: per-tile placement + stitched extent.
// Built once at Open and cached on the level; immutable thereafter.
type Layout struct {
	Width, Height int
	cols, rows    int
	tileW, tileH  int
	origin        map[[2]int]TilePlacement // (col,row) → placement
}

// TileOrigin returns the stitched-space top-left of image-grid tile (col,row).
func (l *Layout) TileOrigin(col, row int) (x, y int, ok bool) {
	p, ok := l.origin[[2]int{col, row}]
	if !ok {
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// Placements returns every tile placement (stitch order is row-major here;
// callers needing overwrite order use stitchOrder()).
func (l *Layout) Placements() []TilePlacement {
	out := make([]TilePlacement, 0, len(l.origin))
	for row := 0; row < l.rows; row++ {
		for col := 0; col < l.cols; col++ {
			if p, ok := l.origin[[2]int{col, row}]; ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// TilesIntersecting returns image-grid tiles whose stitched extent (tileW×tileH
// at their placement) overlaps the output rectangle [x,y,x+w,y+h).
func (l *Layout) TilesIntersecting(x, y, w, h int) []TilePlacement {
	x1, y1 := x+w, y+h
	var out []TilePlacement
	for _, p := range l.Placements() {
		px1, py1 := p.X+l.tileW, p.Y+l.tileH
		if p.X < x1 && px1 > x && p.Y < y1 && py1 > y {
			out = append(out, p)
		}
	}
	return out
}

// BuildLayout computes the tile layout for a level. Dispatches to the
// whitepaper-exact DP path when the inputs support it (Task 4); otherwise
// returns the naive regular-grid layout used by legacy fallback and pyramid
// levels ≥1 (per Roche whitepaper §"Image Pyramid": only level 0 overlaps).
func BuildLayout(in StitchInput) *Layout {
	if dp := buildDPLayout(in); dp != nil {
		return dp
	}
	return buildNaiveLayout(in)
}

func buildNaiveLayout(in StitchInput) *Layout {
	l := newLayout(in.Cols, in.Rows, in.TileW, in.TileH)
	for row := 0; row < in.Rows; row++ {
		for col := 0; col < in.Cols; col++ {
			l.origin[[2]int{col, row}] = TilePlacement{Col: col, Row: row, X: col * in.TileW, Y: row * in.TileH}
		}
	}
	l.Width = in.Cols * in.TileW
	l.Height = in.Rows * in.TileH
	return l
}

func newLayout(cols, rows, tileW, tileH int) *Layout {
	return &Layout{cols: cols, rows: rows, tileW: tileW, tileH: tileH, origin: make(map[[2]int]TilePlacement, cols*rows)}
}

// buildDPLayout is filled in Task 4; until then it always declines (nil).
func buildDPLayout(in StitchInput) *Layout { return nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./formats/bif/ -run 'TestBuildLayoutNaive|TestLayoutTilesIntersecting' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch.go formats/bif/stitch_test.go
git commit -m "feat(bif): stitch engine types + naive layout + queries (#60)"
```

---

## Task 3: Stitch engine — horizontal overlap compaction (single AOI)

**Files:**
- Modify: `formats/bif/stitch.go` (`buildDPLayout`)
- Test: `formats/bif/stitch_test.go`

**Step 0 — Confirm upstream (MANDATORY before editing):** Open `sample_files/bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf` and re-read §"Image Stitching"/"AOI Positions". Confirm: (a) `<Frame XY="C,R">` gives storage→image-grid position (row-major); (b) a `<TileJointInfo Direction="RIGHT">` between Tile1 and Tile2 means Tile2 sits `OverlapX` pixels to the left of `Tile1.X + tileW` (i.e. `Tile2.X = Tile1.X + tileW - OverlapX`); (c) DOWN joints do the same in Y with `OverlapY`; (d) DP 200 has `OverlapY == 0`. If any of these differs from the whitepaper, STOP and report — do not guess. Cite the section in code comments.

This task handles a **single AOI** (one `ImageInfo`): place tile (0,0) at origin (0,0), then propagate placements along the joint graph using the confident RIGHT/DOWN joints.

- [ ] **Step 1: Write the failing test**

Add to `formats/bif/stitch_test.go`. Helper builds a single-AOI EncodeInfo with a uniform horizontal overlap so the expected math is hand-checkable:

```go
// singleAOIEncodeInfo builds a Ver=2 EncodeInfo for one AOI: a cols×rows grid
// with row-major Frames, uniform RIGHT overlap=ox between horizontally adjacent
// tiles and uniform DOWN overlap=oy between vertically adjacent tiles. Tile
// indices are row-major (matching the Frame storage order) for test simplicity.
func singleAOIEncodeInfo(cols, rows, ox, oy int) *bifxml.EncodeInfo {
	ii := bifxml.ImageInfo{AOIScanned: true, AOIIndex: 0, NumCols: cols, NumRows: rows}
	idx := func(c, r int) int { return r*cols + c }
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			ii.Frames = append(ii.Frames, bifxml.Frame{Col: c, Row: r})
			if c+1 < cols {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "RIGHT", Tile1: idx(c, r), Tile2: idx(c+1, r), OverlapX: ox, Confidence: 100})
			}
			if r+1 < rows {
				ii.Joints = append(ii.Joints, bifxml.TileJoint{FlagJoined: true, Direction: "DOWN", Tile1: idx(c, r), Tile2: idx(c, r+1), OverlapY: oy, Confidence: 100})
			}
		}
	}
	return &bifxml.EncodeInfo{Ver: 2, ImageInfos: []bifxml.ImageInfo{ii}}
}

func TestBuildDPLayoutHorizontalOverlap(t *testing.T) {
	// 3 cols × 2 rows, tile 1024, RIGHT overlap 120, no vertical overlap.
	ei := singleAOIEncodeInfo(3, 2, 120, 0)
	l := BuildLayout(StitchInput{Cols: 3, Rows: 2, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// Column origins: 0, 1024-120=904, 904+904=1808. Width = 1808+1024 = 2832,
	// rounded up to a tile multiple (3*1024=3072) by top/right white padding.
	wantCols := []int{0, 904, 1808}
	for c, wantX := range wantCols {
		x, _, ok := l.TileOrigin(c, 0)
		if !ok || x != wantX {
			t.Errorf("TileOrigin(%d,0).X = (%d,%v), want %d", c, x, ok, wantX)
		}
	}
	if l.Width != 3072 {
		t.Errorf("Width = %d, want 3072 (content 2832 padded up to tile multiple)", l.Width)
	}
	if l.Height != 2048 {
		t.Errorf("Height = %d, want 2048", l.Height)
	}
}
```

NOTE: the white-pad-to-tile-multiple expectation is finalized in Task 4; if Task 4's padding rule changes the expected `Width`, update this test there. Confirm the padding direction (top+right) against the whitepaper in Task 4 Step 0.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/bif/ -run TestBuildDPLayoutHorizontalOverlap -v`
Expected: FAIL — `buildDPLayout` returns nil → naive layout → column 1 at 1024, not 904.

- [ ] **Step 3: Implement single-AOI propagation in `buildDPLayout`**

Replace the stub `buildDPLayout` in `formats/bif/stitch.go`:

```go
// buildDPLayout computes the whitepaper-exact layout (Roche BIF whitepaper
// §"Image Stitching"). Declines (nil) — falling back to naive — unless the
// inputs are a spec-compliant DP slide with a usable EncodeInfo (Ver≥2, at
// least one confident joint). Pyramid levels ≥1 carry no joints → nil → naive.
func buildDPLayout(in StitchInput) *Layout {
	ei := in.EncodeInfo
	if in.Generation != GenerationSpecCompliant || ei == nil || ei.Ver < 2 {
		return nil
	}
	if !hasConfidentJoint(ei) {
		return nil
	}
	// Map storage index → image-grid (col,row) from the Frames (row-major).
	// Each ImageInfo's Frames are local to that AOI; Tile1/Tile2 in its Joints
	// index into the same per-AOI frame ordering.
	l := newLayout(in.Cols, in.Rows, in.TileW, in.TileH)
	type key = [2]int
	placed := map[key]bool{}
	// Single-AOI for this task; multi-AOI offsets added in Task 4.
	for _, ii := range ei.ImageInfos {
		framePos := make([]key, len(ii.Frames)) // storage idx → (col,row)
		for i, f := range ii.Frames {
			framePos[i] = key{f.Col, f.Row}
		}
		// Anchor: place every frame at its nominal grid position first, then
		// relax along confident joints. Iterate to a fixed point (the joint
		// graph is a DAG over a grid, so |cols|+|rows| passes converge).
		pos := make(map[key][2]int, len(framePos))
		for _, p := range framePos {
			pos[p] = [2]int{p[0] * in.TileW, p[1] * in.TileH}
		}
		for pass := 0; pass < in.Cols+in.Rows; pass++ {
			for _, j := range ii.Joints {
				if !j.FlagJoined || j.Confidence != 100 {
					continue // whitepaper: trust only confident, joined pairs
				}
				if j.Tile1 < 0 || j.Tile1 >= len(framePos) || j.Tile2 < 0 || j.Tile2 >= len(framePos) {
					continue
				}
				a, b := framePos[j.Tile1], framePos[j.Tile2]
				switch j.Direction {
				case "RIGHT":
					// RIGHT joint pulls Tile2's X to Tile1.X + tileW - overlap;
					// Y unchanged. Take the smallest consistent X (compaction).
					nx := pos[a][0] + in.TileW - j.OverlapX
					if pass == 0 || nx < pos[b][0] {
						pos[b] = [2]int{nx, pos[b][1]}
					}
				case "DOWN":
					// DOWN joint pulls Tile2's Y; X unchanged.
					ny := pos[a][1] + in.TileH - j.OverlapY
					if pass == 0 || ny < pos[b][1] {
						pos[b] = [2]int{pos[b][0], ny}
					}
				}
			}
		}
		for _, p := range framePos {
			l.origin[p] = TilePlacement{Col: p[0], Row: p[1], X: pos[p][0], Y: pos[p][1]}
		}
	}
	finalizeExtent(l, in) // hull + white-pad; defined in Task 4
	return l
}

func hasConfidentJoint(ei *bifxml.EncodeInfo) bool {
	for _, ii := range ei.ImageInfos {
		for _, j := range ii.Joints {
			if j.FlagJoined && j.Confidence == 100 {
				return true
			}
		}
	}
	return false
}

// finalizeExtent is completed in Task 4 (hull + normalize + white-pad). For now
// it sets the extent to the bounding box of placements with no padding, so the
// single-AOI test can assert column origins; Task 4 adds the tile-multiple pad.
func finalizeExtent(l *Layout, in StitchInput) {
	maxX, maxY := 0, 0
	for _, p := range l.origin {
		if p.X+l.tileW > maxX {
			maxX = p.X + l.tileW
		}
		if p.Y+l.tileH > maxY {
			maxY = p.Y + l.tileH
		}
	}
	// Pad up to a tile multiple (top/right per whitepaper; finalized in Task 4).
	l.Width = roundUpToMultiple(maxX, in.TileW)
	l.Height = roundUpToMultiple(maxY, in.TileH)
}

func roundUpToMultiple(v, m int) int {
	if m <= 0 || v%m == 0 {
		return v
	}
	return ((v / m) + 1) * m
}
```

NOTE for the executor: the relaxation above is intentionally simple (one-directional "pull left/up to the smallest consistent offset"). Confirm against the whitepaper that confident RIGHT/DOWN joints form a consistent monotone offset field; if the real Ventana-1 joints require a proper topological propagation (anchor at frame (0,0) and BFS), implement that instead and document why. The Task 5 golden-dimension test is the ground truth that decides correctness.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./formats/bif/ -run 'TestBuildDPLayout|TestBuildLayoutNaive|TestLayoutTilesIntersecting' -v`
Expected: PASS (all engine tests).

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch.go formats/bif/stitch_test.go
git commit -m "feat(bif): DP horizontal/vertical overlap compaction, single AOI (#60)"
```

---

## Task 4: Stitch engine — AOI origins, hull, white-pad, gating

**Files:**
- Modify: `formats/bif/stitch.go`
- Test: `formats/bif/stitch_test.go`

**Step 0 — Confirm upstream (MANDATORY):** Re-read whitepaper §"AOI Positions". Confirm: (a) each AOI's tiles are shifted by `<AoiOrigin OriginX/OriginY>` (multiples of tile size); (b) the stitched image is the bounding box (convex hull) of all AOIs, normalized so the min corner is (0,0); (c) white padding extends to a tile multiple on **top and right** (verify which edges — this determines whether normalization shifts content). Cite the section. If the whitepaper specifies a different pad edge, follow it and update Task 3's test expectation.

- [ ] **Step 1: Write the failing test**

Add to `formats/bif/stitch_test.go`:

```go
func TestBuildDPLayoutTwoAOIsWithOrigins(t *testing.T) {
	// Two single-tile AOIs (1024 tiles, no internal overlap). AOI0 at origin
	// (0,0), AOI1 at OriginX=1024 → side by side, total 2048×1024.
	mk := func(aoiIdx int) bifxml.ImageInfo {
		return bifxml.ImageInfo{AOIScanned: true, AOIIndex: aoiIdx, NumCols: 1, NumRows: 1,
			Frames: []bifxml.Frame{{Col: 0, Row: 0}}}
	}
	ei := &bifxml.EncodeInfo{Ver: 2,
		ImageInfos: []bifxml.ImageInfo{mk(0), mk(1)},
		AoiOrigins: []bifxml.AoiOrigin{{Index: 0, OriginX: 0, OriginY: 0}, {Index: 1, OriginX: 1024, OriginY: 0}},
	}
	// NOTE: grid Cols/Rows here is the *image* grid the level reports. With two
	// 1-tile AOIs side by side the image grid is 2×1.
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	if l.Width != 2048 || l.Height != 1024 {
		t.Fatalf("dims = %dx%d, want 2048x1024", l.Width, l.Height)
	}
}

func TestBuildDPLayoutGatingDeclinesToNaive(t *testing.T) {
	// Ver<2 → naive; legacy generation → naive even with joints.
	verLow := singleAOIEncodeInfo(2, 1, 120, 0)
	verLow.Ver = 1
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: verLow, Generation: GenerationSpecCompliant})
	if x, _, _ := l.TileOrigin(1, 0); x != 1024 {
		t.Errorf("Ver<2 must fall back to naive (x=1024), got %d", x)
	}
	legacy := singleAOIEncodeInfo(2, 1, 120, 0)
	l2 := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: legacy, Generation: GenerationLegacyIScan})
	if x, _, _ := l2.TileOrigin(1, 0); x != 1024 {
		t.Errorf("legacy generation must fall back to naive (x=1024), got %d", x)
	}
}

func TestBuildDPLayoutDropsLowConfidenceJoint(t *testing.T) {
	ei := singleAOIEncodeInfo(2, 1, 120, 0)
	ei.ImageInfos[0].Joints[0].Confidence = 50 // not trusted
	l := BuildLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024, EncodeInfo: ei, Generation: GenerationSpecCompliant})
	// Only low-confidence joints → no confident joint → declines to naive.
	if x, _, _ := l.TileOrigin(1, 0); x != 1024 {
		t.Errorf("low-confidence joint must not compact (x=1024), got %d", x)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./formats/bif/ -run TestBuildDPLayout -v`
Expected: `TwoAOIsWithOrigins` fails (AOI origins not applied); gating tests likely already pass (Ver/gen gates exist from Task 3) — confirm. `DropsLowConfidence` passes via `hasConfidentJoint`. Any failing assertion proves the new behavior is needed.

- [ ] **Step 3: Apply AOI origins + hull normalization in `buildDPLayout`/`finalizeExtent`**

Update `buildDPLayout` so that, after computing per-AOI local placements, it adds the AOI's `OriginX/OriginY` to every tile of that AOI before recording into `l.origin`. Build an `aoiOriginByIndex map[int][2]int` from `ei.AoiOrigins`. Then update `finalizeExtent` to (1) compute min corner across placements, (2) normalize all placements so min = (0,0), (3) set Width/Height to the normalized max corner rounded up to a tile multiple per the whitepaper pad rule confirmed in Step 0.

```go
	aoiOrigin := map[int][2]int{}
	for _, o := range ei.AoiOrigins {
		aoiOrigin[o.Index] = [2]int{o.OriginX, o.OriginY}
	}
	// ... inside the per-ImageInfo loop, when recording placements:
	off := aoiOrigin[ii.AOIIndex] // zero value {0,0} if absent
	for _, p := range framePos {
		l.origin[p] = TilePlacement{Col: p[0], Row: p[1], X: pos[p][0] + off[0], Y: pos[p][1] + off[1]}
	}
```

```go
func finalizeExtent(l *Layout, in StitchInput) {
	if len(l.origin) == 0 {
		l.Width, l.Height = 0, 0
		return
	}
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	for _, p := range l.origin {
		if p.X < minX {
			minX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
	}
	maxX, maxY := 0, 0
	for k, p := range l.origin {
		p.X -= minX
		p.Y -= minY
		l.origin[k] = p
		if p.X+l.tileW > maxX {
			maxX = p.X + l.tileW
		}
		if p.Y+l.tileH > maxY {
			maxY = p.Y + l.tileH
		}
	}
	l.Width = roundUpToMultiple(maxX, in.TileW)
	l.Height = roundUpToMultiple(maxY, in.TileH)
}
```

- [ ] **Step 4: Run all engine tests**

Run: `go test ./formats/bif/ -run 'TestBuildDPLayout|TestBuildLayoutNaive|TestLayoutTilesIntersecting' -v`
Expected: PASS. If Task 3's `TestBuildDPLayoutHorizontalOverlap` Width expectation changed due to the finalized pad rule, update it now and note why in the commit.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/stitch.go formats/bif/stitch_test.go
git commit -m "feat(bif): AOI origins, hull normalize, white-pad, confident-join gating (#60)"
```

---

## Task 5: DP exact-dimension golden test (CI-safe)

**Files:**
- Create: `formats/bif/testdata/ventana1_encodeinfo.xml`
- Create: `formats/bif/stitch_golden_test.go`

**Step 0 — Capture inputs and ground truth (MANDATORY, run once):**
1. Extract the EncodeInfo XMP from Ventana-1's level-0 IFD and save it to `formats/bif/testdata/ventana1_encodeinfo.xml`. It is geometry-only (no PHI). Capture command (local, fixtures present):
   ```bash
   OPENTILE_TESTDIR="$PWD/sample_files" go run ./cmd/... # or a one-off:
   ```
   Use a throwaway Go snippet that opens `sample_files/bif/Ventana-1.bif`, reads `levelIFDs[0].Page.XMP()`, and writes the bytes. Delete the snippet after. Verify the file is < ~200 KB and contains only `<EncodeInfo>` geometry.
2. Record bio-formats' reported Ventana-1 L0 dimensions as the ground-truth constant. From `TestBIFTilePlacementSpatial`'s oracle (already in `formats/bif/spatial_oracle_test.go`) or by running bio-formats `showinf` once; pin the exact integers. The whitepaper-derived expectation is content `23432 × 21504` — confirm against the oracle before pinning.

If capturing the golden XML cleanly proves impractical, gate this test behind fixture presence (skip when `OPENTILE_TESTDIR` BIF is absent) instead — but prefer the committed golden so CI exercises the math.

- [ ] **Step 1: Write the failing test**

Create `formats/bif/stitch_golden_test.go`:

```go
package bif

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// Ground truth: bio-formats' reported stitched dimensions for Ventana-1 L0.
// Pinned from the oracle (see Step 0). DP exactness bar — no tolerance.
const (
	ventana1StitchedW = 23432
	ventana1StitchedH = 21504
	ventana1Cols      = 24 // image grid incl. phantom pad column
	ventana1Rows      = 22
	ventana1TileW     = 1024
	ventana1TileH     = 1024
)

func TestVentana1DPExactDimensions(t *testing.T) {
	xmp, err := os.ReadFile(filepath.Join("testdata", "ventana1_encodeinfo.xml"))
	if err != nil {
		t.Fatalf("read golden EncodeInfo: %v", err)
	}
	ei, err := bifxml.ParseEncodeInfo(xmp)
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	l := BuildLayout(StitchInput{
		Cols: ventana1Cols, Rows: ventana1Rows,
		TileW: ventana1TileW, TileH: ventana1TileH,
		EncodeInfo: ei, Generation: GenerationSpecCompliant,
	})
	if l.Width != ventana1StitchedW || l.Height != ventana1StitchedH {
		t.Fatalf("stitched dims = %dx%d, want %dx%d (bio-formats ground truth)",
			l.Width, l.Height, ventana1StitchedW, ventana1StitchedH)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or surfaces the gap)**

Run: `go test ./formats/bif/ -run TestVentana1DPExactDimensions -v`
Expected: Either FAIL with a dimension mismatch (engine math needs adjustment — iterate on Task 3/4 propagation until exact) or PASS. This is the milestone's correctness gate: do not proceed past it with a mismatch. If the pad rule rounds to a tile multiple but bio-formats reports the unpadded content size, reconcile here (the reported `Size` may be content size while internal extent is padded — decide and document per whitepaper).

- [ ] **Step 3: Iterate the engine until exact**

If mismatched, debug the propagation against the whitepaper (Step 0 of Tasks 3/4). Common reconciliations: padded-vs-content reported dimension; per-row vs global overlap; the phantom 24th column being pad (not a content frame). Adjust `buildDPLayout`/`finalizeExtent` accordingly. Re-run until exact.

- [ ] **Step 4: Verify pass**

Run: `go test ./formats/bif/ -run 'TestVentana1DPExactDimensions' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add formats/bif/testdata/ventana1_encodeinfo.xml formats/bif/stitch_golden_test.go
git commit -m "test(bif): DP exact-dimension golden gate from Ventana-1 EncodeInfo (#60)"
```

---

## Task 6: Wire layout into `levelImpl`; stitched `Level.Size`

**Files:**
- Modify: `formats/bif/level.go`
- Modify: `formats/bif/bif.go` (`Downsample` computation)
- Test: `formats/bif/level_test.go`

- [ ] **Step 1: Write the failing test**

Add to `formats/bif/level_test.go` (a fixture-free constructor test using a synthetic classified IFD is heavy; instead assert the layout accessor surface directly via a small helper). Add:

```go
func TestLevelImplLayoutAccessors(t *testing.T) {
	// Construct a minimal levelImpl with an injected layout to exercise the
	// accessor wiring (the real layout is built in newLevelImpl).
	l := &levelImpl{
		index: 0, grid: opentile.Size{W: 2, H: 1},
		tileSize: opentile.Size{W: 1024, H: 1024},
		layout:   buildNaiveLayout(StitchInput{Cols: 2, Rows: 1, TileW: 1024, TileH: 1024}),
	}
	x, y, ok := l.TileOrigin(1, 0)
	if !ok || x != 1024 || y != 0 {
		t.Errorf("TileOrigin(1,0) = (%d,%d,%v), want (1024,0,true)", x, y, ok)
	}
	w, h, ok := l.StitchedSize()
	if !ok || w != 2048 || h != 1024 {
		t.Errorf("StitchedSize = (%d,%d,%v), want (2048,1024,true)", w, h, ok)
	}
	if got := l.TilesIntersecting(0, 0, 2048, 1024); len(got) != 2 {
		t.Errorf("TilesIntersecting = %d, want 2", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/bif/ -run TestLevelImplLayoutAccessors -v`
Expected: FAIL — `levelImpl.layout` field and accessor methods don't exist.

- [ ] **Step 3: Add `layout` field, build it in `newLevelImpl`, add accessors, set stitched `size`**

In `formats/bif/level.go`, add to `levelImpl`:

```go
	// layout is the stitch layout for this level: per-tile stitched origin and
	// the stitched extent. For pyramid levels ≥1 and non-DP/legacy slides it is
	// the naive regular grid. Built once in newLevelImpl. See stitch.go and #60.
	layout *Layout
```

In `newLevelImpl`, after `cols, rows` are known and before the return, build the layout. The DP path only applies to level 0 (whitepaper: levels ≥1 are pre-stitched). Pass `encodeInfo` only for level 0:

```go
	var ei *bifxml.EncodeInfo
	gen := classifyGeneration(nil) // see note below
	if c.Level == 0 {
		ei = encodeInfo
	}
	layout := BuildLayout(StitchInput{
		Cols: cols, Rows: rows, TileW: int(tw), TileH: int(tl),
		EncodeInfo: ei, Generation: gen,
	})
```

NOTE on `gen`: `newLevelImpl` does not currently receive the slide's `Generation`. Add a `gen Generation` parameter to `newLevelImpl` and pass `t.gen`/`classifyGeneration(iscan)` from the `Open` site in `bif.go` (the caller has `iscan`). Update the one call site at `bif.go:86`:

```go
	l, err := newLevelImpl(i, c, iscan.ScanRes, scanWhite, classifyGeneration(iscan), encodeInfo, file.ReaderAt())
```

and the signature:

```go
func newLevelImpl(
	index int,
	c classifiedIFD,
	baseMPP float64,
	scanWhitePoint uint8,
	gen Generation,
	encodeInfo *bifxml.EncodeInfo,
	reader io.ReaderAt,
) (*levelImpl, error) {
```

Set the stitched size. Replace the `size:` field assignment so it uses the layout extent:

```go
	size:           opentile.Size{W: layout.Width, H: layout.Height},
```

Store `layout: layout` in the returned struct literal. Keep `grid`, `offsets`, `counts`, `frameIndex`, `tileSize` exactly as-is — per-tile addressing is unchanged.

Add accessor methods near the other `levelImpl` methods:

```go
// TileOrigin returns the stitched-space top-left of image-grid tile (col,row).
func (l *levelImpl) TileOrigin(col, row int) (x, y int, ok bool) {
	if l.layout == nil {
		return col * l.tileSize.W, row * l.tileSize.H, col >= 0 && col < l.grid.W && row >= 0 && row < l.grid.H
	}
	return l.layout.TileOrigin(col, row)
}

// StitchedSize returns this level's stitched dimensions (== Size()).
func (l *levelImpl) StitchedSize() (w, h int, ok bool) {
	if l.layout == nil {
		return l.size.W, l.size.H, true
	}
	return l.layout.Width, l.layout.Height, true
}

// TilesIntersecting returns the image-grid tiles whose stitched extent touches
// the output rectangle [x,y,x+w,y+h).
func (l *levelImpl) TilesIntersecting(x, y, w, h int) []TilePlacement {
	if l.layout == nil {
		return nil
	}
	return l.layout.TilesIntersecting(x, y, w, h)
}
```

In `bif.go`, fix `Downsample` to use stitched L0 width (it already reads `l.size.W` via `l0Width = l.size.W` at line 92 — now stitched, which is correct). Confirm `l0Width` is captured from the stitched `l.size.W`; no change needed beyond verifying. Lower levels' `Downsample = l0Width / l.size.W` now relates stitched-L0 to (naive) lower-level extents; document that lower levels report their own IFD extent (they are pre-stitched) so the ratio stays the true pyramid downsample.

- [ ] **Step 4: Run tests**

Run: `go test ./formats/bif/ -run 'TestLevelImplLayoutAccessors|TestBuildLayout|TestBuildDPLayout|TestVentana1' -v`
Expected: PASS. Then `go build ./...` to confirm the signature change compiled everywhere.

- [ ] **Step 5: Commit**

```bash
git add formats/bif/level.go formats/bif/bif.go formats/bif/level_test.go
git commit -m "feat(bif): build stitch layout in newLevelImpl; Level.Size = stitched extent (#60)"
```

---

## Task 7: `regionLayout` capability interface + layout-aware compositing

**Files:**
- Modify: `region.go`
- Create: `region_layout_test.go`

- [ ] **Step 1: Write the failing test**

Create `region_layout_test.go` in package `opentile` (use a fake reader implementing `regionLayout` + the minimal `format.Reader` surface needed, or assert via a real BIF slide gated on fixtures). Prefer a focused unit test of `regionLayoutOf` discovery + a fake:

```go
package opentile

import "testing"

type fakeLayoutReader struct {
	format.Reader // embed to satisfy the interface; methods below override
	originX       int
}

func (f *fakeLayoutReader) TileOrigin(level, col, row int) (int, int, bool) {
	return f.originX + col*100, row*100, true
}
func (f *fakeLayoutReader) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	return []struct{ Col, Row int }{{0, 0}}
}
func (f *fakeLayoutReader) StitchedSize(level int) (int, int, bool) { return 200, 100, true }

func TestRegionLayoutOfDiscovery(t *testing.T) {
	r := &fakeLayoutReader{originX: 7}
	rl, ok := regionLayoutOf(r)
	if !ok {
		t.Fatal("regionLayoutOf should find the interface on a direct reader")
	}
	x, _, _ := rl.TileOrigin(0, 1, 0)
	if x != 107 {
		t.Errorf("TileOrigin pass-through x = %d, want 107", x)
	}
	// Non-implementer → ok=false.
	if _, ok := regionLayoutOf(struct{ format.Reader }{}); ok {
		t.Error("regionLayoutOf should return false for non-implementer")
	}
}
```

(If embedding `format.Reader` is awkward, define the fake to satisfy only what `regionLayoutOf` needs — `regionLayoutOf` takes `any` and walks `UnwrapReader`, so a bare struct with the three methods suffices.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRegionLayoutOfDiscovery -v`
Expected: FAIL — `regionLayout`/`regionLayoutOf` undefined.

- [ ] **Step 3: Add the interface, discovery, and the layout-aware branch**

In `region.go`, add the interface + discovery (mirrors `bif.MetadataOf`'s `UnwrapReader` walk, `maxUnwrapHops = 16`):

```go
// regionLayout is the optional capability a reader implements when its tile
// grid is not a regular spatial partition of the level (BIF stitching, #60).
// Discovered via the UnwrapReader chain in imageReadRegionImpl; absent → the
// regular-grid compositing path runs unchanged. Coordinates are image-grid
// (col,row) and level-resolution stitched-output pixels.
type regionLayout interface {
	TileOrigin(level, col, row int) (x, y int, ok bool)
	TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int }
	StitchedSize(level int) (w, h int, ok bool)
}

const regionLayoutMaxHops = 16

// regionLayoutOf walks the UnwrapReader chain looking for a regionLayout.
func regionLayoutOf(v any) (regionLayout, bool) {
	for i := 0; v != nil && i <= regionLayoutMaxHops; i++ {
		if rl, ok := v.(regionLayout); ok {
			return rl, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}
```

In `imageReadRegionImpl`, after `lvl, err := s.r.Level(...)` and the clip computation, branch. Keep the existing naive body as the `else`. The layout-aware body iterates `rl.TilesIntersecting`, decodes each via `imageDecodedTileInto`, and blits at `rl.TileOrigin` with the same intersection/clipping math, painting in row-major order (later tiles overwrite earlier in overlap bands — the whitepaper's Tile2-over-Tile1, Q-B2):

```go
	if rl, ok := regionLayoutOf(s.r); ok {
		if sw, sh, ok := rl.StitchedSize(level); ok {
			// Re-clip to stitched bounds (lvl.Size is already stitched for BIF,
			// but be explicit so this path is self-contained).
			if x1 > sw {
				x1 = sw
			}
			if y1 > sh {
				y1 = sh
			}
		}
		if x0 >= x1 || y0 >= y1 {
			return ErrRegionEmpty
		}
		fillWhite(dst) // stitched output always white-initialized (overlaps/gaps)
		scratch := borrowTileScratch(lvl.TileSize.W, lvl.TileSize.H, dst.Format)
		defer returnTileScratch(scratch)
		for _, tp := range rl.TilesIntersecting(level, x0, y0, x1-x0, y1-y0) {
			tileX, tileY, ok := rl.TileOrigin(level, tp.Col, tp.Row)
			if !ok {
				continue
			}
			if err := s.imageDecodedTileInto(image, level, tp.Col, tp.Row, scratch, opts...); err != nil {
				return fmt.Errorf("opentile: decode tile (%d,%d) at level %d: %w", tp.Col, tp.Row, level, err)
			}
			tileW, tileH := lvl.TileSize.W, lvl.TileSize.H
			ix0 := maxInt(tileX, x0)
			iy0 := maxInt(tileY, y0)
			ix1 := minInt(tileX+tileW, x1)
			iy1 := minInt(tileY+tileH, y1)
			if ix0 >= ix1 || iy0 >= iy1 {
				continue
			}
			blitInto(scratch, ix0-tileX, iy0-tileY, ix1-ix0, iy1-iy0, dst, ix0-x, iy0-y)
		}
		return nil
	}
```

NOTE: `TilesIntersecting` must return tiles in row-major (col-then-row) order so the overwrite order matches the whitepaper. `Layout.Placements()` already iterates row-major; confirm the BIF `Tiler.TilesIntersecting` (Task 8) preserves that order.

- [ ] **Step 4: Run tests**

Run: `go test . -run TestRegionLayoutOfDiscovery -v && go build ./...`
Expected: PASS + clean build. Then run the full region suite to confirm the non-BIF naive path is untouched: `go test . -run 'Region' -v`.

- [ ] **Step 5: Commit**

```bash
git add region.go region_layout_test.go
git commit -m "feat: regionLayout capability + layout-aware ReadRegion compositing (#60)"
```

---

## Task 8: BIF `Tiler` implements `regionLayout`

**Files:**
- Modify: `formats/bif/bif.go`
- Test: `formats/bif/bif_region_layout_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `formats/bif/bif_region_layout_test.go`:

```go
package bif

import "testing"

func TestTilerImplementsRegionLayout(t *testing.T) {
	tl := &Tiler{levelImpls: []*levelImpl{{
		index: 0, grid: opentile.Size{W: 2, H: 1}, tileSize: opentile.Size{W: 100, H: 100},
		layout: buildNaiveLayout(StitchInput{Cols: 2, Rows: 1, TileW: 100, TileH: 100}),
	}}}
	x, y, ok := tl.TileOrigin(0, 1, 0)
	if !ok || x != 100 || y != 0 {
		t.Errorf("TileOrigin(0,1,0) = (%d,%d,%v), want (100,0,true)", x, y, ok)
	}
	w, h, ok := tl.StitchedSize(0)
	if !ok || w != 200 || h != 100 {
		t.Errorf("StitchedSize(0) = (%d,%d,%v), want (200,100,true)", w, h, ok)
	}
	got := tl.TilesIntersecting(0, 0, 0, 200, 100)
	if len(got) != 2 {
		t.Errorf("TilesIntersecting = %d, want 2", len(got))
	}
	// Out-of-range level → ok=false, empty.
	if _, _, ok := tl.TileOrigin(9, 0, 0); ok {
		t.Error("TileOrigin on bad level should be ok=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./formats/bif/ -run TestTilerImplementsRegionLayout -v`
Expected: FAIL — `Tiler` lacks the three methods.

- [ ] **Step 3: Implement the three methods on `Tiler`**

Add to `formats/bif/bif.go` (near the other `Tiler` methods). Return shape must match the opentile interface exactly (`[]struct{ Col, Row int }`):

```go
// TileOrigin reports the stitched-space top-left of image-grid tile (col,row)
// at the given level. Implements opentile's regionLayout (#60).
func (t *Tiler) TileOrigin(level, col, row int) (x, y int, ok bool) {
	if level < 0 || level >= len(t.levelImpls) {
		return 0, 0, false
	}
	return t.levelImpls[level].TileOrigin(col, row)
}

// TilesIntersecting reports image-grid tiles whose stitched extent touches
// [x,y,x+w,y+h) at the given level, in row-major order. Implements regionLayout.
func (t *Tiler) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	if level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	tps := t.levelImpls[level].TilesIntersecting(x, y, w, h)
	out := make([]struct{ Col, Row int }, len(tps))
	for i, p := range tps {
		out[i] = struct{ Col, Row int }{p.Col, p.Row}
	}
	return out
}

// StitchedSize reports the level's stitched dimensions. Implements regionLayout.
func (t *Tiler) StitchedSize(level int) (w, h int, ok bool) {
	if level < 0 || level >= len(t.levelImpls) {
		return 0, 0, false
	}
	return t.levelImpls[level].StitchedSize()
}
```

- [ ] **Step 4: Run tests + build**

Run: `go test ./formats/bif/ -run TestTilerImplementsRegionLayout -v && go build ./...`
Expected: PASS + clean build. Confirm `regionLayoutOf` finds the `Tiler` through `fileCloser`/`mmapCloser` (both already implement `UnwrapReader`).

- [ ] **Step 5: Commit**

```bash
git add formats/bif/bif.go formats/bif/bif_region_layout_test.go
git commit -m "feat(bif): Tiler implements regionLayout (delegates to levelImpls) (#60)"
```

---

## Task 9: ScaledStrips / ReadRegionScaled geometry audit

**Files:**
- Read/Modify: `region_scaled.go`, `scaled_strips*.go` (whichever holds `ScaledStrips`)
- Test: existing scaled tests + a BIF-gated assertion

- [ ] **Step 1: Audit for grid×TileSize geometry derivation**

Read `region_scaled.go` and the `ScaledStrips` implementation. Confirm they derive source geometry from `lvl.Size` (now stitched) and not from `Grid × TileSize`. Grep:

```bash
grep -rn "Grid\|TileSize.W\s*\*\|TileSize.H\s*\*" region_scaled.go scaled_strips*.go
```

Any `grid.W * TileSize.W` style computation that assumes a regular partition must switch to `lvl.Size.W`. Since scaled paths call `imageReadRegionImpl`/`imageDecodedTileInto` internally, the layout-aware branch from Task 7 already handles compositing; this task only fixes any *geometry* (output dimension / strip count) that still assumes naive extent.

- [ ] **Step 2: Write/extend a test**

Add a BIF-gated test (skips without fixtures) asserting `ReadRegionScaled` and `ScaledStrips` total output dimensions equal the stitched `Pyramid.Level(0).Size` scaled by the requested factor — proving they use stitched geometry. Use the existing scaled-test helpers; place near the other scaled tests. If a pure-unit assertion is feasible (no fixture), prefer it.

- [ ] **Step 3: Apply any geometry fix found in Step 1**

If Step 1 found a naive-extent assumption, replace it with `lvl.Size`. If none found, document in the commit that the audit confirmed scaled paths are geometry-correct (no code change) and the test is the regression guard.

- [ ] **Step 4: Run tests**

Run: `go test . -run 'Scaled|Strip' -v` (+ fixture-gated BIF run locally: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run BIF -v`)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix(bif): scaled-strip/region geometry uses stitched Level.Size (#60)"
```

---

## Task 10: bio-formats tolerance pixel oracle

**Files:**
- Create: `tests/oracle/bif_stitch_oracle_test.go`

This is the end-to-end correctness proof: stitched `ReadRegion` pixels vs bio-formats' crop, with per-channel tolerance for decoder rounding + a structural mean-abs-diff guard so misplacement can't pass.

- [ ] **Step 1: Confirm the oracle harness**

Read `formats/bif/spatial_oracle_test.go` (the existing `TestBIFTilePlacementSpatial` bio-formats oracle) to reuse its bio-formats invocation pattern (build tag, fixture gate, how it shells out / parses pixels). Mirror its black-box approach — bio-formats as an external process/data source, never source-translated (GPL).

- [ ] **Step 2: Write the oracle test**

Create `tests/oracle/bif_stitch_oracle_test.go` (match the existing oracle build tag, e.g. `//go:build parity` or the bio-formats-specific tag the placement oracle uses). Steps:
1. Open `Ventana-1.bif` via opentile-go; `ReadRegion` a tissue-bearing rectangle at L0 (pick a region with structure, e.g. mid-slide, away from white padding).
2. Get bio-formats' equivalent crop of the stitched image for the same rectangle.
3. Compare per-pixel with `tolerance = 3` per channel; assert the fraction of pixels exceeding tolerance is below a small threshold (e.g. < 0.5%) AND mean-abs-diff < ~2.0. A misplacement produces a structurally different crop → mean-abs-diff explodes → fail.

```go
// thresholds: decoder rounding (JVM JPEG vs libjpeg-turbo) is ±1–2/channel;
// a placement error shifts whole tiles → mean-abs-diff ≫ 2. The structural
// guard (meanAbsDiff) is what actually catches misplacement; per-pixel
// tolerance just absorbs codec noise.
const (
	perChannelTol   = 3
	maxOverTolFrac  = 0.005
	maxMeanAbsDiff  = 2.0
)
```

- [ ] **Step 3: Run locally (fixture-gated)**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/oracle/ -tags <oracle-tag> -run BIFStitch -v`
Expected: PASS within tolerance. If it fails on placement (high mean-abs-diff), the engine (Tasks 3–5) is wrong — return to systematic-debugging on `buildDPLayout`. If it fails only on a thin seam, revisit the Q-B2 overwrite order.

- [ ] **Step 4: Confirm CI behavior**

Confirm the test skips cleanly when fixtures/bio-formats are absent (CI without the slide). Document in the test header that it is local/fixture-gated, like `TestBIFTilePlacementSpatial`.

- [ ] **Step 5: Commit**

```bash
git add tests/oracle/bif_stitch_oracle_test.go
git commit -m "test(bif): bio-formats tolerance pixel oracle for stitched ReadRegion (#60)"
```

---

## Task 11: Legacy honesty lock + docs + migration + CHANGELOG

**Files:**
- Modify/Create: `formats/bif/stitch_golden_test.go` (legacy lock)
- Modify: `docs/formats/bif.md`
- Create: `docs/migrations/2026-06-18-bif-level-size-stitched.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Capture OS-1 best-effort dims + write the lock test**

**Step 0 — capture (local, OS-1 present):** Open `OS-1.bif`, read its L0 stitched `Size` from opentile-go after Tasks 3–8, and record the best-effort integers (spec expects ≈ `114468 × 98094`). Also record bio-formats' OS-1 dims (≈ `105817 × 93978`) for the doc comment. These are facts, not expression.

Add a legacy lock to `formats/bif/stitch_golden_test.go` (fixture-gated — OS-1 is local-only PHI):

```go
// TestOS1LegacyBestEffortDims locks the CURRENT best-effort legacy stitched
// dimensions so a future legacy-overlap fix (#60-legacy) is a deliberate,
// reviewed change — not silent drift. We do NOT match bio-formats here:
// bio-formats reaches 105817×93978 via a GPL columnXAdjust heuristic we will
// not port; the whitepaper disclaims legacy reconstruction. See design §E.
func TestOS1LegacyBestEffortDims(t *testing.T) {
	dir := tests.TestdataDir()
	slide := filepath.Join(dir, "bif", "OS-1.bif")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	if _, err := os.Stat(slide); err != nil {
		t.Skip("OS-1.bif not present (local-only PHI fixture)")
	}
	s, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer s.Close()
	lvl, _ := s.Level(0)
	const wantW, wantH = 114468, 98094 // best-effort; NOT bio-formats' 105817×93978
	if lvl.Size.W != wantW || lvl.Size.H != wantH {
		t.Errorf("OS-1 L0 stitched = %dx%d, want best-effort %dx%d (legacy exactness deferred: #60-legacy)",
			lvl.Size.W, lvl.Size.H, wantW, wantH)
	}
}
```

- [ ] **Step 2: Run the lock test locally**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/bif/ -run TestOS1LegacyBestEffort -v`
Expected: PASS (pin the constants to whatever the engine actually produces — this records reality, then we improve it later). If the produced dims differ from 114468×98094, set the constants to the actual produced values and note the gap-to-bio-formats in the comment.

- [ ] **Step 3: Docs — `docs/formats/bif.md` stitching section**

Add a "Tile stitching & dimensions" section documenting: per-tile API returns raw camera frames addressed row-major (unchanged); `Level.Size` and the region/scaled APIs report/produce the **stitched** image; DP generation is pixel-exact (whitepaper-derived); legacy is best-effort with a documented gap to bio-formats and a deferred exactness item (#60-legacy); the clean-room note (whitepaper-sourced, bio-formats/openslide are oracles only). Cite the whitepaper sections.

- [ ] **Step 4: Migration note + CHANGELOG**

Create `docs/migrations/2026-06-18-bif-level-size-stitched.md`: explain that `Level.Size` on a BIF L0 changed from the raw-frame extent (`ImageWidth×ImageLength`, e.g. Ventana-1 `24576×22528`) to the stitched extent (`23432×21504`); per-tile bytes and `Grid` are unchanged; consumers that derived slide pixel dimensions from BIF `Level.Size` now get the correct stitched size; `ReadRegion`/`ScaledStrips` are now stitched (were seam-artifacted). Note wsitools/openscope should re-validate any cached BIF dimensions.

Add a `## [Unreleased]` CHANGELOG entry under the right headings (Added: overlap-aware stitching, `regionLayout`; Changed: BIF `Level.Size` = stitched extent; Fixed: BIF `ReadRegion`/`ScaledStrips` seam artifacts; cite #60 and the deferred #60-legacy).

- [ ] **Step 5: Full gate + commit**

Run: `make test` (green under `-race`), `make vet`, and locally `OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -run BIF` + the oracle.
Expected: green; existing `TestTifffileParityBIF` and `TestBIFTilePlacementSpatial` still pass (per-tile addressing untouched).

```bash
git add formats/bif/stitch_golden_test.go docs/formats/bif.md docs/migrations/2026-06-18-bif-level-size-stitched.md CHANGELOG.md
git commit -m "docs+test(bif): legacy honesty lock, stitching docs, Level.Size migration note (#60)"
```

---

## Final verification

- [ ] `make test` green under `-race` (all 39+ packages).
- [ ] `make vet` clean.
- [ ] `make cover` — new `formats/bif` stitch code ≥80%.
- [ ] Locally with fixtures: `TestVentana1DPExactDimensions` (exact), `TestBIFTilePlacementSpatial` + `TestTifffileParityBIF` (unchanged), bio-formats stitch oracle (within tolerance), `TestOS1LegacyBestEffortDims` (locked).
- [ ] Non-BIF formats: `go test . -run Region` confirms the naive-grid path is byte-unchanged (the 10 other formats never enter the layout-aware branch).
- [ ] Dispatch a final whole-implementation code review (subagent-driven-development final reviewer) before finishing the branch.
- [ ] Open a follow-up issue `#60-legacy` capturing the deferred legacy-overlap exactness work (hypothesis: file carries more usable info than the engine currently extracts; investigate OS-1 `<TileJointInfo>`/`<AoiOrigin>`/`<iScan>` fields we don't yet read).

---

## Self-review notes (writing-plans skill)

- **Spec coverage:** §A engine → Tasks 2–5; §B compositing → Tasks 7–8; §C per-level/dims → Task 6 + Task 9; §D testing → Tasks 5, 10, 11 + unchanged oracles; §E legacy deferral → Task 11 lock + follow-up issue; §2 licensing → Step-0 gates on Tasks 3/4 + oracle tasks cite black-box-only; bifxml Confidence (§A.6) → Task 1.
- **Type consistency:** `Layout`/`TilePlacement`/`StitchInput`/`BuildLayout` defined in Task 2, used consistently in 3–8. `regionLayout` interface return type `[]struct{ Col, Row int }` matches between `region.go` (Task 7) and the BIF `Tiler` (Task 8). `newLevelImpl` signature change (added `gen Generation`) is applied at its sole call site in Task 6.
- **Known soft spots flagged for the executor (not placeholders):** the exact propagation algorithm in `buildDPLayout` (Task 3) and the pad/reported-dimension reconciliation (Task 5) are gated by the whitepaper Step-0 reads and the Ventana-1 golden-dimension test — the plan says iterate against that ground truth rather than guess, per the project's "read upstream first" invariant.
