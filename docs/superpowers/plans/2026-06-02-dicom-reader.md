# DICOM WSI Reader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read DICOM VL Whole Slide Microscopy series (a directory of `.dcm` instances) as opentile-go Slides — the first multi-file format — passing all three fixture scanners (Leica GT450 TILED_SPARSE, 3DHISTECH + Grundium TILED_FULL).

**Architecture:** `internal/dicom` wraps `suyashkumar/dicom` for cold metadata parsing only (no library type escapes it). `formats/dicom` owns series assembly, the frame→tile map (FULL raster + SPARSE positions), and an mmap fragment-offset-walk hot path that returns zero-copy raw JPEG frames. Because all VOLUME levels are JPEG, `DecodedTile`/`ReadRegion`/`ScaledStrips` come free via the existing slow-path codec dispatch — `formats/dicom` contains no decode code. The uncompressed Leica label is served as a raw-bytes associated image. Multi-file Open is a path-aware branch in core `open.go`; the `Tiler` owns its N mmaps and closes them all.

**Tech Stack:** Go 1.23+, `github.com/suyashkumar/dicom` v1.1.0 (pure Go, MIT, behind `internal/dicom`), `golang.org/x/exp/mmap` (already a dep).

**Reference:** `docs/superpowers/specs/2026-06-02-dicom-reader-design.md`, `docs/formats/dicom.md`. Interface contracts cited inline come from the codebase as of merge `b0dd6c4`.

**Working notes for the executor:**
- Tests that need real `.dcm` files guard on `OPENTILE_TESTDIR` and `t.Skip` when absent, like `TestSlideParity`. Fixtures: `sample_files/dicom/Leica-4/` (extracted), and `sample_files/dicom/{3DHISTECH-1,scan_621_grundium_dicom}.zip` (the executor extracts these to sibling dirs once; see Task 11).
- Pure-function tasks (fragment-walk, frame→tile maps, assembly) are unit-tested with **synthetic** in-memory data — no real file needed.
- Run the full gate with `make test` (it sets `OPENTILE_TESTDIR` and runs `-race`).

---

## Task 1: `internal/dicom` — metadata parser (cold path)

**Files:**
- Modify: `go.mod` (add dependency)
- Create: `internal/dicom/instance.go`
- Create: `internal/dicom/instance_test.go`

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/suyashkumar/dicom@v1.1.0
```
Expected: `go.mod` gains `github.com/suyashkumar/dicom v1.1.0` and `golang.org/x/text` (x/exp is already present).

- [ ] **Step 2: Write the failing test** (`internal/dicom/instance_test.go`)

This test guards on the fixture dir (DICOM instances are large; no tiny synthetic instance is committed). It parses one known Leica-4 VOLUME instance and asserts the attributes.

```go
package dicom

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureDir(t *testing.T) string {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "Leica-4")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("Leica-4 fixture not present: %v", err)
	}
	return dir
}

// largestVolume returns the path of the biggest .dcm by file size (the L0 VOLUME).
func largestVolume(t *testing.T, dir string) string {
	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var best string
	var bestSize int64
	for _, p := range entries {
		fi, _ := os.Stat(p)
		if fi.Size() > bestSize {
			bestSize, best = fi.Size(), p
		}
	}
	return best
}

func TestParseInstanceLeicaL0(t *testing.T) {
	in, err := ParseInstance(largestVolume(t, fixtureDir(t)))
	if err != nil {
		t.Fatalf("ParseInstance: %v", err)
	}
	if in.SOPClassUID != WSMStorageUID {
		t.Errorf("SOPClassUID = %q, want WSM", in.SOPClassUID)
	}
	if got, want := roleOf(in.ImageType), "VOLUME"; got != want {
		t.Errorf("role = %q, want %q", got, want)
	}
	if in.TotalCols != 23374 || in.TotalRows != 22079 {
		t.Errorf("TotalPixelMatrix = %dx%d, want 23374x22079", in.TotalCols, in.TotalRows)
	}
	if in.TileCols != 256 || in.TileRows != 256 {
		t.Errorf("tile = %dx%d, want 256x256", in.TileCols, in.TileRows)
	}
	if in.NumFrames != 8004 {
		t.Errorf("NumFrames = %d, want 8004", in.NumFrames)
	}
	if in.DimOrg != "TILED_SPARSE" {
		t.Errorf("DimOrg = %q, want TILED_SPARSE", in.DimOrg)
	}
	if len(in.FramePositions) != in.NumFrames {
		t.Errorf("FramePositions = %d, want %d", len(in.FramePositions), in.NumFrames)
	}
	// First Leica frame observed at col=1,row=1281 (1-based pixel coords).
	if in.FramePositions[0].Col != 1 {
		t.Errorf("FramePositions[0].Col = %d, want 1", in.FramePositions[0].Col)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./internal/dicom/ -run TestParseInstanceLeicaL0 -v`
Expected: FAIL — `ParseInstance` / `Instance` undefined.

- [ ] **Step 4: Implement** (`internal/dicom/instance.go`)

`roleOf` and `WSMStorageUID` are defined here too (used by tests and Task 5). The parse mirrors the validated prototype.

```go
// Package dicom parses the metadata of DICOM VL Whole Slide Microscopy
// (WSM) SOP instances for the formats/dicom reader. It is the only place
// in opentile-go that imports github.com/suyashkumar/dicom, and no
// suyashkumar type is exported from here. It parses metadata only — it
// does not read pixel data and does not import the root opentile package.
package dicom

import (
	"strconv"
	"strings"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

// WSMStorageUID is the SOP Class UID for VL Whole Slide Microscopy Image Storage.
const WSMStorageUID = "1.2.840.10008.5.1.4.1.1.77.1.6"

// FramePos is the 1-based top-left pixel position of a frame in the total
// pixel matrix (TILED_SPARSE only).
type FramePos struct{ Col, Row int }

// Instance is the parsed metadata of one WSM SOP instance.
type Instance struct {
	Path           string
	SOPClassUID    string
	SeriesUID      string
	ImageType      []string
	TransferSyntax string
	TotalCols      int
	TotalRows      int
	TileCols       int
	TileRows       int
	NumFrames      int
	DimOrg         string // TILED_FULL | TILED_SPARSE
	Photometric    string
	PixelSpacingX  float64
	PixelSpacingY  float64
	ObjectivePower float64
	Manufacturer   string
	Model          string
	Software       string
	Writer         string
	ICCProfile     []byte
	FramePositions []FramePos // len == NumFrames for SPARSE; nil for FULL
}

var (
	tTransferSyntax = tag.Tag{Group: 0x0002, Element: 0x0010}
	tWriter         = tag.Tag{Group: 0x0002, Element: 0x0013} // ImplementationVersionName
	tSourceAE       = tag.Tag{Group: 0x0002, Element: 0x0016}
	tSOPClass       = tag.Tag{Group: 0x0008, Element: 0x0016}
	tImageType      = tag.Tag{Group: 0x0008, Element: 0x0008}
	tManufacturer   = tag.Tag{Group: 0x0008, Element: 0x0070}
	tModel          = tag.Tag{Group: 0x0008, Element: 0x1090}
	tSoftware       = tag.Tag{Group: 0x0018, Element: 0x1020}
	tSeries         = tag.Tag{Group: 0x0020, Element: 0x000E}
	tTotalCols      = tag.Tag{Group: 0x0048, Element: 0x0006}
	tTotalRows      = tag.Tag{Group: 0x0048, Element: 0x0007}
	tRows           = tag.Tag{Group: 0x0028, Element: 0x0010}
	tCols           = tag.Tag{Group: 0x0028, Element: 0x0011}
	tNumFrames      = tag.Tag{Group: 0x0028, Element: 0x0008}
	tPhotometric    = tag.Tag{Group: 0x0028, Element: 0x0004}
	tDimOrg         = tag.Tag{Group: 0x0020, Element: 0x9311}
	tObjective      = tag.Tag{Group: 0x0048, Element: 0x0112}
	tPixelSpacing   = tag.Tag{Group: 0x0028, Element: 0x0030}
	tPerFrameFG     = tag.Tag{Group: 0x5200, Element: 0x9230}
	tPlanePosSlide  = tag.Tag{Group: 0x0048, Element: 0x021A}
	tColPos         = tag.Tag{Group: 0x0048, Element: 0x021E}
	tRowPos         = tag.Tag{Group: 0x0048, Element: 0x021F}
)

// ParseInstance parses one instance's metadata (pixel data skipped).
func ParseInstance(path string) (Instance, error) {
	ds, err := dicom.ParseFile(path, nil,
		dicom.SkipPixelData(),
		dicom.AllowMismatchPixelDataLength(),
	)
	if err != nil {
		return Instance{}, err
	}
	in := Instance{
		Path:           path,
		SOPClassUID:    firstStr(ds, tSOPClass),
		SeriesUID:      firstStr(ds, tSeries),
		ImageType:      allStr(ds, tImageType),
		TransferSyntax: firstStr(ds, tTransferSyntax),
		TotalCols:      firstInt(ds, tTotalCols),
		TotalRows:      firstInt(ds, tTotalRows),
		TileCols:       firstInt(ds, tCols),
		TileRows:       firstInt(ds, tRows),
		DimOrg:         firstStr(ds, tDimOrg),
		Photometric:    firstStr(ds, tPhotometric),
		ObjectivePower: nestedFloat(ds, tObjective),
		Manufacturer:   firstStr(ds, tManufacturer),
		Model:          firstStr(ds, tModel),
		Software:       firstStr(ds, tSoftware),
	}
	in.NumFrames = atoiSafe(firstStr(ds, tNumFrames))
	in.Writer = firstStr(ds, tWriter)
	if in.Writer == "" {
		in.Writer = firstStr(ds, tSourceAE)
	}
	if sx, sy, ok := nestedPixelSpacing(ds); ok {
		in.PixelSpacingX, in.PixelSpacingY = sx, sy
	}
	if in.DimOrg == "TILED_SPARSE" {
		in.FramePositions = parseFramePositions(ds)
	}
	return in, nil
}

// roleOf returns the WSM role token from ImageType, or "" if none present.
func roleOf(imageType []string) string {
	for _, v := range imageType {
		switch v {
		case "VOLUME", "LABEL", "OVERVIEW", "THUMBNAIL":
			return v
		}
	}
	return ""
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func firstStr(ds dicom.Dataset, t tag.Tag) string {
	if v := allStr(ds, t); len(v) > 0 {
		return v[0]
	}
	return ""
}

func allStr(ds dicom.Dataset, t tag.Tag) []string {
	e, err := ds.FindElementByTag(t)
	if err != nil {
		return nil
	}
	if v, ok := e.Value.GetValue().([]string); ok {
		return v
	}
	return nil
}

func firstInt(ds dicom.Dataset, t tag.Tag) int {
	e, err := ds.FindElementByTag(t)
	if err != nil {
		return 0
	}
	if v, ok := e.Value.GetValue().([]int); ok && len(v) > 0 {
		return v[0]
	}
	return 0
}

// nestedFloat finds the first DS-valued element with tag t anywhere
// (including inside sequences) and parses it as a float.
func nestedFloat(ds dicom.Dataset, t tag.Tag) float64 {
	e, err := ds.FindElementByTagNested(t)
	if err != nil {
		return 0
	}
	if v, ok := e.Value.GetValue().([]string); ok && len(v) > 0 {
		f, _ := strconv.ParseFloat(strings.TrimSpace(v[0]), 64)
		return f
	}
	return 0
}

// nestedPixelSpacing reads PixelSpacing (row\col spacing in mm) from the
// Shared Functional Groups → Pixel Measures sequence.
func nestedPixelSpacing(ds dicom.Dataset) (x, y float64, ok bool) {
	e, err := ds.FindElementByTagNested(tPixelSpacing)
	if err != nil {
		return 0, 0, false
	}
	v, vok := e.Value.GetValue().([]string)
	if !vok || len(v) < 2 {
		return 0, 0, false
	}
	// PixelSpacing is [rowSpacing, colSpacing]; Y = row, X = col.
	y, _ = strconv.ParseFloat(strings.TrimSpace(v[0]), 64)
	x, _ = strconv.ParseFloat(strings.TrimSpace(v[1]), 64)
	return x, y, true
}

// parseFramePositions walks PerFrameFunctionalGroupsSequence →
// PlanePositionSlideSequence and returns one FramePos per frame.
func parseFramePositions(ds dicom.Dataset) []FramePos {
	pf, err := ds.FindElementByTag(tPerFrameFG)
	if err != nil {
		return nil
	}
	items, ok := pf.Value.GetValue().([]*dicom.SequenceItemValue)
	if !ok {
		return nil
	}
	out := make([]FramePos, 0, len(items))
	for _, it := range items {
		els, _ := it.GetValue().([]*dicom.Element)
		pps := findIn(els, tPlanePosSlide)
		if pps == nil {
			out = append(out, FramePos{})
			continue
		}
		ppsItems, _ := pps.Value.GetValue().([]*dicom.SequenceItemValue)
		if len(ppsItems) == 0 {
			out = append(out, FramePos{})
			continue
		}
		inner, _ := ppsItems[0].GetValue().([]*dicom.Element)
		out = append(out, FramePos{Col: intOf(inner, tColPos), Row: intOf(inner, tRowPos)})
	}
	return out
}

func findIn(els []*dicom.Element, t tag.Tag) *dicom.Element {
	for _, e := range els {
		if e.Tag == t {
			return e
		}
	}
	return nil
}

func intOf(els []*dicom.Element, t tag.Tag) int {
	if e := findIn(els, t); e != nil {
		if v, ok := e.Value.GetValue().([]int); ok && len(v) > 0 {
			return v[0]
		}
	}
	return 0
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./internal/dicom/ -v`
Expected: PASS (or SKIP if the fixture is absent — acceptable in CI, which has no `sample_files`).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/dicom/
git commit -m "feat(dicom): internal/dicom metadata parser wrapping suyashkumar/dicom"
```

---

## Task 2: `formats/dicom` — fragment-offset-walk (hot path core)

A pure function over the mmap'd instance bytes. TDD with synthetic encapsulated PixelData — no real file needed.

**Files:**
- Create: `formats/dicom/frames.go`
- Create: `formats/dicom/frames_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/frames_test.go`)

```go
package dicom

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildEncapsulated synthesizes an encapsulated PixelData (OB, undefined
// length): the 12-byte header, an empty Basic Offset Table item, then one
// item per frame, then the sequence delimiter.
func buildEncapsulated(frames [][]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 0x4F, 0x42, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF})
	item := func(data []byte) {
		b.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(data)))
		b.Write(l[:])
		b.Write(data)
	}
	item(nil) // empty BOT
	for _, f := range frames {
		item(f)
	}
	b.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0x00, 0x00, 0x00, 0x00}) // seq delimiter
	return b.Bytes()
}

func TestWalkEncapsulatedFrames(t *testing.T) {
	frames := [][]byte{{0xAA, 0xBB}, {0x01, 0x02, 0x03}, {0xFF}}
	// prefix some bytes so offsets are not trivially small
	blob := append([]byte("PREAMBLE-AND-DATASET"), buildEncapsulated(frames)...)
	spans, err := walkEncapsulatedFrames(blob)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	for i, want := range frames {
		got := blob[spans[i].off : spans[i].off+spans[i].length]
		if !bytes.Equal(got, want) {
			t.Errorf("frame %d = % x, want % x", i, got, want)
		}
	}
}

func TestWalkEncapsulatedFramesMissingSignature(t *testing.T) {
	if _, err := walkEncapsulatedFrames([]byte("no pixel data here")); err == nil {
		t.Fatal("expected error when signature absent")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestWalkEncapsulated -v`
Expected: FAIL — `walkEncapsulatedFrames`/`span` undefined.

- [ ] **Step 3: Implement** (`formats/dicom/frames.go`) — lifted verbatim from the validated prototype.

```go
package dicom

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// span is the byte range of one frame's compressed data in the instance.
type span struct {
	off    int
	length int
}

// walkEncapsulatedFrames locates the encapsulated PixelData (VR OB,
// undefined length) and walks its fragment items, returning one span per
// frame. Assumes one fragment per frame (true for all v1 fixtures; a
// future multi-fragment case is unsupported). The Basic Offset Table item
// (first item) is skipped; opentile-go always derives offsets from this
// walk rather than the BOT (empty across all observed scanners).
func walkEncapsulatedFrames(b []byte) ([]span, error) {
	sig := []byte{0xE0, 0x7F, 0x10, 0x00, 0x4F, 0x42, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF}
	pos := bytes.Index(b, sig)
	if pos < 0 {
		return nil, fmt.Errorf("dicom: encapsulated PixelData (OB) not found")
	}
	p := pos + len(sig)
	itemTag := []byte{0xFE, 0xFF, 0x00, 0xE0}
	seqDelim := []byte{0xFE, 0xFF, 0xDD, 0xE0}
	var frames []span
	first := true
	for p+8 <= len(b) {
		t := b[p : p+4]
		if bytes.Equal(t, seqDelim) {
			break
		}
		if !bytes.Equal(t, itemTag) {
			return nil, fmt.Errorf("dicom: unexpected item tag at %d: % x", p, t)
		}
		length := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		p += 8
		if p+length > len(b) {
			return nil, fmt.Errorf("dicom: fragment at %d overruns file", p)
		}
		if first {
			first = false // skip BOT
			p += length
			continue
		}
		frames = append(frames, span{off: p, length: length})
		p += length
	}
	return frames, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./formats/dicom/ -run TestWalkEncapsulated -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add formats/dicom/frames.go formats/dicom/frames_test.go
git commit -m "feat(dicom): encapsulated PixelData fragment-offset walk"
```

---

## Task 3: `formats/dicom` — frame→tile maps (FULL + SPARSE)

Pure functions building `(tx,ty) → frameIndex`. TDD with synthetic inputs.

**Files:**
- Create: `formats/dicom/tilemap.go`
- Create: `formats/dicom/tilemap_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/tilemap_test.go`)

```go
package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestTileMapFull(t *testing.T) {
	// 3x2 grid (cols x rows), raster order, 6 frames.
	m := buildTileMap("TILED_FULL", 3, 2, 256, nil, 6)
	// frame index = ty*cols + tx
	for ty := 0; ty < 2; ty++ {
		for tx := 0; tx < 3; tx++ {
			if got := m[tileKey{tx, ty}]; got != ty*3+tx {
				t.Errorf("(%d,%d) -> %d, want %d", tx, ty, got, ty*3+tx)
			}
		}
	}
}

func TestTileMapSparse(t *testing.T) {
	// two frames: tile (0,5) and (1,5), 256px tiles, 1-based positions.
	pos := []idicom.FramePos{{Col: 1, Row: 1281}, {Col: 257, Row: 1281}}
	m := buildTileMap("TILED_SPARSE", 6, 6, 256, pos, 2)
	if m[tileKey{0, 5}] != 0 {
		t.Errorf("(0,5) -> %d, want 0", m[tileKey{0, 5}])
	}
	if m[tileKey{1, 5}] != 1 {
		t.Errorf("(1,5) -> %d, want 1", m[tileKey{1, 5}])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestTileMap -v`
Expected: FAIL — `buildTileMap`/`tileKey` undefined.

- [ ] **Step 3: Implement** (`formats/dicom/tilemap.go`)

```go
package dicom

import idicom "github.com/wsilabs/opentile-go/internal/dicom"

type tileKey struct{ tx, ty int }

// buildTileMap returns a (tx,ty) -> frame-index map. For TILED_FULL the
// order is implicit raster (ty*tilesAcross + tx). For TILED_SPARSE the
// 1-based pixel positions are converted to tile indices. Absent cells are
// simply missing from the map (callers blank-fill).
func buildTileMap(dimOrg string, tilesAcross, tilesDown, tileSize int, pos []idicom.FramePos, numFrames int) map[tileKey]int {
	m := make(map[tileKey]int, numFrames)
	if dimOrg == "TILED_SPARSE" {
		for i, p := range pos {
			if p.Col == 0 && p.Row == 0 {
				continue // unpositioned frame; skip
			}
			tx := (p.Col - 1) / tileSize
			ty := (p.Row - 1) / tileSize
			m[tileKey{tx, ty}] = i
		}
		return m
	}
	// TILED_FULL: raster fill up to numFrames.
	idx := 0
	for ty := 0; ty < tilesDown && idx < numFrames; ty++ {
		for tx := 0; tx < tilesAcross && idx < numFrames; tx++ {
			m[tileKey{tx, ty}] = idx
			idx++
		}
	}
	return m
}
```

Note: `buildTileMap` takes a single `tileSize` for square tiles (all v1 fixtures are square). The SPARSE branch uses it for both axes.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./formats/dicom/ -run TestTileMap -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/dicom/tilemap.go formats/dicom/tilemap_test.go
git commit -m "feat(dicom): frame->tile map for TILED_FULL and TILED_SPARSE"
```

---

## Task 4: `formats/dicom` — series assembly

Group instances into one slide: VOLUME levels sorted desc, associated images classified. Pure function over `[]idicom.Instance`. TDD with synthetic instances.

**Files:**
- Create: `formats/dicom/assemble.go`
- Create: `formats/dicom/assemble_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/assemble_test.go`)

```go
package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func inst(role string, cols, rows int) idicom.Instance {
	return idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", role, "NONE"},
		TotalCols: cols, TotalRows: rows, TileCols: 256, TileRows: 256,
		DimOrg: "TILED_FULL",
	}
}

func TestAssembleSeries(t *testing.T) {
	in := []idicom.Instance{
		inst("VOLUME", 1460, 1379),
		inst("VOLUME", 23374, 22079),
		inst("VOLUME", 5843, 5519),
		inst("LABEL", 608, 547),
		inst("OVERVIEW", 1491, 605),
		{SOPClassUID: "1.2.99", TotalCols: 100}, // non-WSM, must be dropped
		inst("THUMBNAIL", 1920, 1813),
	}
	s, err := assembleSeries(in)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(s.levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(s.levels))
	}
	// Sorted largest-first.
	if s.levels[0].inst.TotalCols != 23374 || s.levels[2].inst.TotalCols != 1460 {
		t.Errorf("levels not sorted desc: %d..%d", s.levels[0].inst.TotalCols, s.levels[2].inst.TotalCols)
	}
	// Downsample derived from L0.
	if s.levels[1].downsample != float64(23374)/float64(5843) {
		t.Errorf("L1 downsample = %v", s.levels[1].downsample)
	}
	roles := map[string]bool{}
	for _, a := range s.associated {
		roles[a.role] = true
	}
	for _, want := range []string{"LABEL", "OVERVIEW", "THUMBNAIL"} {
		if !roles[want] {
			t.Errorf("missing associated %s", want)
		}
	}
}

func TestAssembleNoVolume(t *testing.T) {
	if _, err := assembleSeries([]idicom.Instance{inst("LABEL", 1, 1)}); err == nil {
		t.Fatal("expected error when no VOLUME level present")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestAssemble -v`
Expected: FAIL — `assembleSeries` etc. undefined.

- [ ] **Step 3: Implement** (`formats/dicom/assemble.go`)

```go
package dicom

import (
	"fmt"
	"sort"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

type levelInfo struct {
	inst       idicom.Instance
	downsample float64
}

type assocInfo struct {
	inst idicom.Instance
	role string
}

type series struct {
	levels     []levelInfo
	associated []assocInfo
}

// assembleSeries filters to WSM instances with a positive total matrix,
// sorts VOLUME instances into levels (largest first), and classifies
// LABEL/OVERVIEW/THUMBNAIL as associated images.
func assembleSeries(insts []idicom.Instance) (series, error) {
	var s series
	var vols []idicom.Instance
	for _, in := range insts {
		if in.SOPClassUID != idicom.WSMStorageUID || in.TotalCols <= 0 || in.TotalRows <= 0 {
			continue
		}
		switch roleOfInstance(in) {
		case "VOLUME":
			vols = append(vols, in)
		case "LABEL", "OVERVIEW", "THUMBNAIL":
			s.associated = append(s.associated, assocInfo{inst: in, role: roleOfInstance(in)})
		}
	}
	if len(vols) == 0 {
		return series{}, fmt.Errorf("dicom: no VOLUME level in series")
	}
	sort.SliceStable(vols, func(i, j int) bool { return vols[i].TotalCols > vols[j].TotalCols })
	l0 := float64(vols[0].TotalCols)
	for _, v := range vols {
		s.levels = append(s.levels, levelInfo{inst: v, downsample: l0 / float64(v.TotalCols)})
	}
	return s, nil
}

// roleOfInstance mirrors internal/dicom role classification at the format layer.
func roleOfInstance(in idicom.Instance) string {
	for _, v := range in.ImageType {
		switch v {
		case "VOLUME", "LABEL", "OVERVIEW", "THUMBNAIL":
			return v
		}
	}
	return ""
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./formats/dicom/ -run TestAssemble -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/dicom/assemble.go formats/dicom/assemble_test.go
git commit -m "feat(dicom): series assembly (levels + associated classification)"
```

---

## Task 5: `formats/dicom` — the Tiler (format.Reader implementation)

Wires assembly + offset-walk + tile-map into a reader. Per-instance mmap; `ImageRawTile` is a zero-copy subslice. `Compression = CompressionJPEG` on every level so the existing slow path decodes for free.

**Files:**
- Create: `formats/dicom/tiler.go`
- Create: `formats/dicom/tiler_test.go`

This task depends on the open/series constructor (Task 6) for an end-to-end test, so its unit test exercises the reader methods on a hand-built `Tiler`. Define the `Tiler` and an internal `openSeriesFromInstances(parsed []idicom.Instance, openers map[string]instanceBytes) (*Tiler, error)` seam, where `instanceBytes` abstracts "give me this instance's bytes" so tests can inject synthetic encapsulated blobs without real files.

- [ ] **Step 1: Write the failing test** (`formats/dicom/tiler_test.go`)

```go
package dicom

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestTilerRawTile(t *testing.T) {
	// One VOLUME level, 2x1 grid, TILED_FULL, two synthetic JPEG-ish frames.
	frameA := []byte{0xFF, 0xD8, 0xAA}
	frameB := []byte{0xFF, 0xD8, 0xBB}
	blob := append([]byte("HDR"), buildEncapsulated([][]byte{frameA, frameB})...)

	vol := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", "VOLUME", "NONE"},
		TotalCols: 512, TotalRows: 256, TileCols: 256, TileRows: 256,
		NumFrames: 2, DimOrg: "TILED_FULL",
	}
	tiler, err := openSeriesFromInstances([]idicom.Instance{vol},
		func(path string) ([]byte, func() error, error) { return blob, func() error { return nil }, nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tiler.Close()

	if tiler.Format() != opentile.FormatDICOM {
		t.Errorf("Format = %v", tiler.Format())
	}
	lvl, _ := tiler.Level(0, 0)
	if lvl.Compression != opentile.CompressionJPEG {
		t.Errorf("Compression = %v, want JPEG", lvl.Compression)
	}
	if lvl.Grid != (opentile.Size{W: 2, H: 1}) {
		t.Errorf("Grid = %+v, want 2x1", lvl.Grid)
	}
	got, err := tiler.ImageRawTile(0, 0, 1, 0)
	if err != nil {
		t.Fatalf("RawTile: %v", err)
	}
	if !bytes.Equal(got, frameB) {
		t.Errorf("tile(1,0) = % x, want % x", got, frameB)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestTilerRawTile -v`
Expected: FAIL — `openSeriesFromInstances`, `Tiler`, `opentile.FormatDICOM` undefined. (`FormatDICOM` is added in Task 7; for now add a temporary const at top of `tiler.go` if needed, then remove in Task 7 — OR do Task 7 Step "add FormatDICOM" first. To avoid churn, **add the `FormatDICOM` constant to `format.go` as the first action of this task.**)

- [ ] **Step 3: Add the format constant** (`format.go`)

Add to the const block:
```go
	FormatDICOM       Format = "dicom"
```

- [ ] **Step 4: Implement** (`formats/dicom/tiler.go`)

```go
package dicom

import (
	"context"
	"fmt"
	"image"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

var _ formatReader = (*Tiler)(nil) // formatReader alias documented below

// instanceBytes returns the (mmapped) bytes of an instance file plus a
// closer. Production passes an mmap-backed opener; tests inject blobs.
type instanceBytes func(path string) (data []byte, closeFn func() error, err error)

type levelEngine struct {
	info    levelInfo
	data    []byte
	spans   []span
	tileMap map[tileKey]int
	grid    opentile.Size
	closeFn func() error
}

// Tiler is the formats/dicom reader. It owns one mmap per instance and
// closes them all in Close().
type Tiler struct {
	img        opentile.Image
	levels     []*levelEngine
	associated []opentile.AssociatedImage
	meta       opentile.Metadata
	dmeta      Metadata // format-specific (Task 8)
	icc        []byte
	closers    []func() error
}

func openSeriesFromInstances(insts []idicom.Instance, open instanceBytes) (*Tiler, error) {
	s, err := assembleSeries(insts)
	if err != nil {
		return nil, err
	}
	t := &Tiler{}
	l0 := s.levels[0].inst
	for i, li := range s.levels {
		data, closeFn, err := open(li.inst.Path)
		if err != nil {
			t.Close()
			return nil, err
		}
		t.closers = append(t.closers, closeFn)
		spans, err := walkEncapsulatedFrames(data)
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("dicom: level %d: %w", i, err)
		}
		across := ceilDiv(li.inst.TotalCols, li.inst.TileCols)
		down := ceilDiv(li.inst.TotalRows, li.inst.TileRows)
		eng := &levelEngine{
			info:    li,
			data:    data,
			spans:   spans,
			tileMap: buildTileMap(li.inst.DimOrg, across, down, li.inst.TileCols, li.inst.FramePositions, li.inst.NumFrames),
			grid:    opentile.Size{W: across, H: down},
			closeFn: closeFn,
		}
		t.levels = append(t.levels, eng)
	}
	t.img = opentile.Image{Name: "", Index: 0, Levels: t.buildLevels(l0)}
	t.meta, t.dmeta = buildMetadata(l0, s) // Task 8
	t.associated = buildAssociated(s, open) // Task 7
	return t, nil
}

func (t *Tiler) buildLevels(l0 idicom.Instance) []opentile.Level {
	out := make([]opentile.Level, len(t.levels))
	for i, e := range t.levels {
		out[i] = opentile.Level{
			Index:        i,
			PyramidIndex: 0,
			Size:         opentile.Size{W: e.info.inst.TotalCols, H: e.info.inst.TotalRows},
			TileSize:     opentile.Size{W: e.info.inst.TileCols, H: e.info.inst.TileRows},
			Grid:         e.grid,
			TileOverlap:  image.Point{},
			Compression:  opentile.CompressionJPEG,
			Downsample:   e.info.downsample,
		}
	}
	return out
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

// --- format.Reader methods ---

func (t *Tiler) Format() opentile.Format          { return opentile.FormatDICOM }
func (t *Tiler) Images() []opentile.Image         { return []opentile.Image{t.img} }
func (t *Tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *Tiler) Metadata() opentile.Metadata      { return t.meta }
func (t *Tiler) ICCProfile() []byte               { return t.icc }

func (t *Tiler) Level(imageIdx, level int) (opentile.Level, error) {
	if imageIdx != 0 || level < 0 || level >= len(t.levels) {
		return opentile.Level{}, fmt.Errorf("dicom: level (%d,%d) out of range", imageIdx, level)
	}
	return t.img.Levels[level], nil
}

func (t *Tiler) ImageRawTile(imageIdx, level, tx, ty int) ([]byte, error) {
	e, err := t.engine(imageIdx, level)
	if err != nil {
		return nil, err
	}
	idx, ok := e.tileMap[tileKey{tx, ty}]
	if !ok {
		return nil, &opentile.TileError{Op: "RawTile", Err: fmt.Errorf("dicom: tile (%d,%d) absent", tx, ty)}
	}
	if idx < 0 || idx >= len(e.spans) {
		return nil, &opentile.TileError{Op: "RawTile", Err: fmt.Errorf("dicom: frame %d out of range", idx)}
	}
	sp := e.spans[idx]
	out := make([]byte, sp.length)
	copy(out, e.data[sp.off:sp.off+sp.length])
	return out, nil
}

func (t *Tiler) ImageRawTileInto(imageIdx, level, tx, ty int, dst []byte) (int, error) {
	b, err := t.ImageRawTile(imageIdx, level, tx, ty)
	if err != nil {
		return 0, err
	}
	if len(dst) < len(b) {
		return 0, fmt.Errorf("dicom: dst too small (%d < %d)", len(dst), len(b))
	}
	return copy(dst, b), nil
}

func (t *Tiler) engine(imageIdx, level int) (*levelEngine, error) {
	if imageIdx != 0 || level < 0 || level >= len(t.levels) {
		return nil, fmt.Errorf("dicom: level (%d,%d) out of range", imageIdx, level)
	}
	return t.levels[level], nil
}

// ImageTileMaxSize returns the largest frame length for the level (bound for buffers).
func (t *Tiler) ImageTileMaxSize(imageIdx, level int) int {
	e, err := t.engine(imageIdx, level)
	if err != nil {
		return 0
	}
	max := 0
	for _, sp := range e.spans {
		if sp.length > max {
			max = sp.length
		}
	}
	return max
}

// DICOM frames are self-contained JPEGs: no shared prefix.
func (t *Tiler) ImageTilePrefix(imageIdx, level int) []byte { return nil }
func (t *Tiler) ImageTileBodyMaxSize(imageIdx, level int) int { return t.ImageTileMaxSize(imageIdx, level) }
func (t *Tiler) ImageTileBodyInto(imageIdx, level, tx, ty int, dst []byte) (int, error) {
	return t.ImageRawTileInto(imageIdx, level, tx, ty, dst)
}

func (t *Tiler) ImageTileReader(imageIdx, level, tx, ty int) (io.ReadCloser, error) {
	b, err := t.ImageRawTile(imageIdx, level, tx, ty)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(byteReader(b)), nil
}

func (t *Tiler) WarmLevel(imageIdx, level int) error {
	_, err := t.engine(imageIdx, level)
	return err // bytes already mapped; warming is a no-op
}

func (t *Tiler) ImageRangeTiles(ctx context.Context, imageIdx, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		e, err := t.engine(imageIdx, level)
		if err != nil {
			return
		}
		for ty := 0; ty < e.grid.H; ty++ {
			for tx := 0; tx < e.grid.W; tx++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				b, err := t.ImageRawTile(imageIdx, level, tx, ty)
				if !yield(opentile.TilePos{X: tx, Y: ty}, opentile.TileResult{Data: b, Err: err}) {
					return
				}
			}
		}
	}
}

func (t *Tiler) Close() error {
	var firstErr error
	for _, c := range t.closers {
		if c != nil {
			if err := c(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
```

The executor must confirm the exact field names of `opentile.TilePos` / `opentile.TileResult` and the `TileError` struct from `image.go` / `errors.go` and adjust the literals to match (the Explore reference lists `TileError{Op, Err}`; verify `TilePos`/`TileResult` fields and the `byteReader` helper — use `bytes.NewReader` instead if simpler). Also confirm `formatReader` — alias it at the top of the file as `type formatReader = format.Reader` via an import of `internal/format`, mirroring `formats/szi/factory.go:10`'s `var _ format.Reader = (*Tiler)(nil)`.

**Compile-ordering note:** `buildMetadata` (Task 8) and `buildAssociated` (Task 7) are referenced here but implemented later. So that Task 5 compiles and its test passes, add **temporary stubs** in this task — `func buildMetadata(l0 idicom.Instance, s series) (opentile.Metadata, Metadata) { return opentile.Metadata{}, Metadata{} }` and `func buildAssociated(s series, open instanceBytes) []opentile.AssociatedImage { return nil }` — in `tiler.go`, then **delete them** when Tasks 7/8 add the real versions in their own files. (Likewise add a no-op `selectDominantSeries` only if Task 6 has not yet landed.)

**Plan decision — absent sparse cells:** `ImageRawTile` returns a `TileError` for a `(tx,ty)` not in the tile map (no blank-fill). All three v1 fixtures have dense grids, so this never triggers; blank-fill (a cached white tile à la BIF) is deferred until a genuinely gapped sparse slide appears. Returning an error beats silently fabricating pixels.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./formats/dicom/ -run TestTilerRawTile -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add format.go formats/dicom/tiler.go formats/dicom/tiler_test.go
git commit -m "feat(dicom): Tiler implementing format.Reader (raw frame passthrough)"
```

---

## Task 6: Multi-file Open — `internal/dicom` series gather + `formats/dicom` constructor + core `open.go` branch

This is the invasive task: core `open.go` gains a path-aware DICOM branch (registry openers can't see the path). The `Tiler` owns its per-instance mmaps.

**Files:**
- Create: `formats/dicom/open.go`
- Modify: `open.go` (root) — add the DICOM detection branch
- Create: `formats/dicom/open_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/open_test.go`)

Fixture-guarded end-to-end open of the real Leica-4 directory.

```go
package dicom_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func leica4(t *testing.T) string {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "Leica-4")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return dir
}

func TestOpenLeica4Directory(t *testing.T) {
	s, err := opentile.OpenFile(leica4(t))
	if err != nil {
		t.Fatalf("OpenFile(dir): %v", err)
	}
	defer s.Close()
	if s.Format() != opentile.FormatDICOM {
		t.Fatalf("Format = %v", s.Format())
	}
	levels := s.Levels()
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	if levels[0].Size != (opentile.Size{W: 23374, H: 22079}) {
		t.Errorf("L0 size = %+v", levels[0].Size)
	}
	// A center tile decodes (slow-path JPEG) to the tile size.
	img, err := s.DecodedTile(2, 3, 3)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Errorf("decoded tile = %dx%d, want 256x256", img.Width, img.Height)
	}
}

func TestOpenSingleDcmExpandsToSeries(t *testing.T) {
	dir := leica4(t)
	one, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	s, err := opentile.OpenFile(one[0]) // any one instance
	if err != nil {
		t.Fatalf("OpenFile(.dcm): %v", err)
	}
	defer s.Close()
	if len(s.Levels()) != 3 {
		t.Errorf("sibling-scan levels = %d, want 3", len(s.Levels()))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -run TestOpen -v`
Expected: FAIL — `OpenFile` doesn't recognize the directory / DICOM.

- [ ] **Step 3: Implement the series gather + constructor** (`formats/dicom/open.go`)

```go
package dicom

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/exp/mmap"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// IsDICOM reports whether path is openable as a DICOM WSM series: either a
// directory containing WSM instances, or a single .dcm whose magic + SOP
// class is WSM.
func IsDICOM(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		entries, _ := filepath.Glob(filepath.Join(path, "*.dcm"))
		for _, p := range entries {
			if hasDICMMagic(p) {
				return true
			}
		}
		return false
	}
	return hasDICMMagic(path)
}

func hasDICMMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [132]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return false
	}
	return string(buf[128:132]) == "DICM"
}

// OpenSeries opens a WSM series given a directory or a single instance path,
// honoring the openslide-style sibling-scan contract: same directory only,
// same SeriesUID only, WSM-filtered.
func OpenSeries(path string) (*Tiler, error) {
	dir := path
	var anchorSeries string
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		dir = filepath.Dir(path)
		in, err := idicom.ParseInstance(path)
		if err != nil {
			return nil, err
		}
		if in.SOPClassUID != idicom.WSMStorageUID {
			return nil, fmt.Errorf("dicom: %s is not a WSM instance", path)
		}
		anchorSeries = in.SeriesUID
	}

	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var parsed []idicom.Instance
	for _, p := range entries {
		in, err := idicom.ParseInstance(p)
		if err != nil || in.SOPClassUID != idicom.WSMStorageUID {
			continue // skip unreadable / non-WSM
		}
		if anchorSeries != "" && in.SeriesUID != anchorSeries {
			continue // bound to the anchor's series
		}
		parsed = append(parsed, in)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("dicom: no WSM instances under %s", dir)
	}
	// If multiple series remain (no anchor), pick the one with most VOLUME levels.
	parsed = selectDominantSeries(parsed)

	return openSeriesFromInstances(parsed, mmapOpener)
}

// mmapOpener maps an instance file and returns its bytes + a closer.
func mmapOpener(path string) ([]byte, func() error, error) {
	r, err := mmap.Open(path)
	if err != nil {
		return nil, nil, err
	}
	b := make([]byte, r.Len())
	if _, err := r.ReadAt(b, 0); err != nil {
		r.Close()
		return nil, nil, err
	}
	// NOTE: golang.org/x/exp/mmap exposes only ReaderAt; we copy into a
	// []byte for the fragment-walk + zero-copy-ish slicing. The copy is the
	// instance's full size once per open. A future optimization can use
	// syscall.Mmap to get a true []byte view (see prototype). For v1 this
	// keeps the dependency surface to x/exp/mmap (already used elsewhere).
	return b, r.Close, nil
}
```

Add `selectDominantSeries(parsed []idicom.Instance) []idicom.Instance` to `assemble.go`: group by `SeriesUID`, return the group with the most VOLUME instances (ties → first by UID sort). Add a unit test `TestSelectDominantSeries` in `assemble_test.go`.

> **Plan decision (documented):** v1 copies each instance into a heap `[]byte` via `x/exp/mmap.ReaderAt` rather than a true `syscall.Mmap` []byte view. Rationale: keeps the dependency to the already-used `x/exp/mmap`, and instances are opened lazily per level. The prototype proved a true-mmap zero-copy path; converting `mmapOpener` to `syscall.Mmap` is a drop-in future optimization that doesn't change any other code. This trades some memory (one level's compressed bytes resident) for dependency simplicity; revisit if the memory-budget gate flags it.

- [ ] **Step 4: Wire the core `open.go` branch** (root `open.go`)

In `OpenFile`, before the existing single-file mmap/pread dispatch, add:

```go
	if dicom.IsDICOM(path) {
		tiler, err := dicom.OpenSeries(path)
		if err != nil {
			return nil, err
		}
		return newSlide(tiler, cfg), nil // construct *Slide around the reader
	}
```

The executor must:
- Add `"github.com/wsilabs/opentile-go/formats/dicom"` import to `open.go`. **Check for an import cycle**: `formats/dicom` imports the root `opentile` package (for `Level`/`Image`/etc.), and `open.go` is in the root package — so the root importing `formats/dicom` creates a cycle. **Resolution:** do NOT import `formats/dicom` from the root. Instead, register a path-opener hook the same way `openAnyHook` is registered from `internal/format` (see `open.go:43-56` + `internal/format/opentile_bridge.go`). Add a `dicomPathOpenHook func(path string, cfg *config) (slideReader, error)` package var in the root, set by `formats/dicom`'s `init()` via a bridge function `opentile.SetDICOMPathOpenHook(...)`. `OpenFile` calls the hook when non-nil and `IsDICOM`-positive. This mirrors the existing `SetOpenAnyHook` indirection and avoids the cycle.
- Confirm how `*Slide` is constructed from a `slideReader` (the existing dispatch builds it; reuse that helper, e.g. wrap in `mmapCloser`/`fileCloser` is NOT applicable here since the Tiler owns its own mmaps — construct `&Slide{r: tiler, cfg: cfg}` directly, matching whatever `dispatchOpen` does minus the file wrapper).

> **Plan decision (documented — the spec's Contract 1/2):** DICOM is the first format reachable via `OpenFile(path)` but not via `Open(io.ReaderAt, size)`. The hook is path-based. `Open(reader,size)` continues to return `ErrUnsupportedFormat` for DICOM (it cannot express a multi-file series). The single-`.dcm` path triggers a bounded sibling-scan (same dir, same SeriesUID, WSM-only). Both are documented in `Open`/`OpenFile` godoc and `docs/formats/dicom.md`.

- [ ] **Step 5: Run to verify it passes**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -run TestOpen -v`
Expected: PASS (or SKIP without the fixture).

- [ ] **Step 6: Commit**

```bash
git add open.go formats/dicom/open.go formats/dicom/assemble.go formats/dicom/assemble_test.go
git commit -m "feat(dicom): multi-file OpenSeries + path-aware open hook (Contracts 1 & 2)"
```

---

## Task 7: Associated images + `formats/all` registration

**Files:**
- Create: `formats/dicom/associated.go`
- Create: `formats/dicom/factory.go` (the `init()` bridge registration)
- Modify: `formats/all/all.go`
- Create: `formats/dicom/associated_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/associated_test.go`)

```go
package dicom

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestAssociatedImage(t *testing.T) {
	frame := []byte{0xFF, 0xD8, 0x42}
	blob := append([]byte("X"), buildEncapsulated([][]byte{frame})...)
	label := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"ORIGINAL", "PRIMARY", "LABEL", "NONE"},
		TotalCols: 608, TotalRows: 547, TileCols: 608, TileRows: 547,
		NumFrames: 1, DimOrg: "TILED_FULL", TransferSyntax: "1.2.840.10008.1.2.4.50",
	}
	vol := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", "VOLUME", "NONE"},
		TotalCols: 512, TotalRows: 256, TileCols: 256, TileRows: 256, NumFrames: 1, DimOrg: "TILED_FULL",
	}
	openers := map[string][]byte{"label": blob, "vol": append([]byte("Y"), buildEncapsulated([][]byte{{0xFF, 0xD8}})...)}
	label.Path, vol.Path = "label", "vol"
	tiler, err := openSeriesFromInstances([]idicom.Instance{vol, label},
		func(p string) ([]byte, func() error, error) { return openers[p], func() error { return nil }, nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tiler.Close()
	as := tiler.Associated()
	if len(as) != 1 || as[0].Type() != "label" {
		t.Fatalf("associated = %+v", as)
	}
	if as[0].Compression() != opentile.CompressionJPEG {
		t.Errorf("label compression = %v", as[0].Compression())
	}
	b, err := as[0].Bytes()
	if err != nil || !bytes.Equal(b, frame) {
		t.Errorf("label bytes = % x (err %v), want % x", b, err, frame)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestAssociatedImage -v`
Expected: FAIL — `buildAssociated` undefined.

- [ ] **Step 3: Implement** (`formats/dicom/associated.go`)

```go
package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// associatedImage serves a single-frame WSM instance (label/overview/
// thumbnail) as raw compressed bytes.
type associatedImage struct {
	typ         string
	size        opentile.Size
	compression opentile.Compression
	data        []byte // the single frame's bytes (already extracted)
}

func (a *associatedImage) Type() string                  { return a.typ }
func (a *associatedImage) Size() opentile.Size           { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.data))
	copy(out, a.data)
	return out, nil
}

// dicomTypeToOpentile maps a WSM ImageType token to an opentile Type().
func dicomTypeToOpentile(role string) string {
	switch role {
	case "LABEL":
		return "label"
	case "OVERVIEW":
		return "overview"
	case "THUMBNAIL":
		return "thumbnail"
	}
	return "associated"
}

func buildAssociated(s series, open instanceBytes) []opentile.AssociatedImage {
	var out []opentile.AssociatedImage
	for _, a := range s.associated {
		data, closeFn, err := open(a.inst.Path)
		if err != nil {
			continue
		}
		spans, err := walkEncapsulatedFrames(data)
		if err != nil || len(spans) == 0 {
			closeFn()
			continue
		}
		sp := spans[0]
		frame := make([]byte, sp.length)
		copy(frame, data[sp.off:sp.off+sp.length])
		closeFn() // associated bytes are copied out; no need to keep the mmap
		out = append(out, &associatedImage{
			typ:         dicomTypeToOpentile(a.role),
			size:        opentile.Size{W: a.inst.TotalCols, H: a.inst.TotalRows},
			compression: compressionForSyntax(a.inst.TransferSyntax),
			data:        frame,
		})
	}
	return out
}

// compressionForSyntax maps a Transfer Syntax UID to opentile.Compression.
func compressionForSyntax(ts string) opentile.Compression {
	switch ts {
	case "1.2.840.10008.1.2.4.50": // JPEG Baseline
		return opentile.CompressionJPEG
	case "1.2.840.10008.1.2.1", "1.2.840.10008.1.2": // Explicit/Implicit LE, uncompressed
		return opentile.CompressionNone
	default:
		return opentile.CompressionJPEG // best-effort; all v1 fixture associated images are JPEG
	}
}
```

Wire `buildAssociated` into `openSeriesFromInstances` (already referenced in Task 5's `t.associated = buildAssociated(s, open)`).

- [ ] **Step 4: Register** (`formats/dicom/factory.go`)

DICOM does not register through the normal `format.Register` opener path (it's multi-file). Instead its `init()` installs the path-open hook on the root package (the bridge from Task 6 Step 4):

```go
package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
)

func init() {
	opentile.SetDICOMPathOpenHook(func(path string) (any, error) {
		return OpenSeries(path)
	})
}
```

(The exact hook signature must match what Task 6 added to the root. Adjust the `any`/typed return to the root's hook type.)

Then add the blank import to `formats/all/all.go` (with the others):
```go
	_ "github.com/wsilabs/opentile-go/formats/dicom"
```

- [ ] **Step 5: Run tests**

Run: `go test ./formats/dicom/ -v` and `go build ./...`
Expected: PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add formats/dicom/associated.go formats/dicom/factory.go formats/all/all.go
git commit -m "feat(dicom): associated images + formats/all registration"
```

---

## Task 8: Metadata mapping + `MetadataOf` accessor

**Files:**
- Create: `formats/dicom/metadata.go`
- Create: `formats/dicom/metadata_test.go`

- [ ] **Step 1: Write the failing test** (`formats/dicom/metadata_test.go`)

```go
package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestBuildMetadata(t *testing.T) {
	l0 := idicom.Instance{
		Manufacturer: "Leica Biosystems", Model: "GT450", Software: "1.0.1",
		Writer: "Leica ScnUtility", ObjectivePower: 40,
		PixelSpacingX: 0.00105105, PixelSpacingY: 0.00105105,
	}
	md, _ := buildMetadata(l0, series{})
	if md.ScannerManufacturer != "Leica Biosystems" || md.ScannerModel != "GT450" {
		t.Errorf("scanner = %q/%q", md.ScannerManufacturer, md.ScannerModel)
	}
	if md.Magnification != 40 {
		t.Errorf("magnification = %v", md.Magnification)
	}
	// 0.00105105 mm = 1.05105 µm
	if got := md.MicronsPerPixelX; got < 1.05 || got > 1.06 {
		t.Errorf("MPP X = %v, want ~1.051", got)
	}
	if md.Writer != "Leica ScnUtility" {
		t.Errorf("writer = %q", md.Writer)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./formats/dicom/ -run TestBuildMetadata -v`
Expected: FAIL — `buildMetadata`/`Metadata` undefined.

- [ ] **Step 3: Implement** (`formats/dicom/metadata.go`)

```go
package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// Metadata is the format-specific accessor payload (embeds the cross-format
// opentile.Metadata).
type Metadata struct {
	opentile.Metadata
	SeriesUID      string
	TransferSyntax string
	DimOrg         string
}

func buildMetadata(l0 idicom.Instance, s series) (opentile.Metadata, Metadata) {
	md := opentile.Metadata{
		Magnification:       l0.ObjectivePower,
		ScannerManufacturer: l0.Manufacturer,
		ScannerModel:        l0.Model,
		Writer:              l0.Writer,
		Properties:          map[string]string{},
	}
	if l0.Software != "" {
		md.ScannerSoftware = []string{l0.Software}
	}
	// PixelSpacing is in mm; opentile MPP is µm.
	md.MicronsPerPixelX = l0.PixelSpacingX * 1000
	md.MicronsPerPixelY = l0.PixelSpacingY * 1000
	if md.MicronsPerPixelX == md.MicronsPerPixelY {
		md.MicronsPerPixel = md.MicronsPerPixelX
	}
	dm := Metadata{Metadata: md, SeriesUID: l0.SeriesUID, TransferSyntax: l0.TransferSyntax, DimOrg: l0.DimOrg}
	return md, dm
}

// MetadataOf returns the DICOM-specific Metadata for a Slide-or-reader that
// wraps a *Tiler, walking the UnwrapReader chain (mirrors szi.MetadataOf).
func MetadataOf(r any) (*Metadata, bool) {
	for i := 0; i < 16 && r != nil; i++ {
		if t, ok := r.(*Tiler); ok {
			return &t.dmeta, true
		}
		u, ok := r.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		r = u.UnwrapReader()
	}
	return nil, false
}
```

The executor must confirm `szi.MetadataOf`'s exact unwrap pattern (`formats/szi/metadata.go:92-105`) and match it (parameter type may be `opentile.Tiler` or `any`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./formats/dicom/ -run TestBuildMetadata -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/dicom/metadata.go formats/dicom/metadata_test.go
git commit -m "feat(dicom): metadata mapping + MetadataOf accessor"
```

---

## Task 9: Fixtures + parity integration

**Files:**
- Modify: `tests/integration_test.go` (slide candidates, resolveSlide, fixtureJSONFor)
- Modify: `tests/generate_test.go` (`sampledByDefault`)
- Create: `tests/fixtures/Leica-4.dicom.json`, `tests/fixtures/3DHISTECH-1.dicom.json`, `tests/fixtures/scan_621_grundium_dicom.dicom.json` (generated)
- Create: a helper to extract the two zips once (or document manual extraction)

- [ ] **Step 1: Extract the zipped fixtures**

```bash
mkdir -p sample_files/dicom/3DHISTECH-1 sample_files/dicom/scan_621_grundium_dicom
unzip -q -o sample_files/dicom/3DHISTECH-1.zip -d sample_files/dicom/3DHISTECH-1
unzip -q -o sample_files/dicom/scan_621_grundium_dicom.zip -d sample_files/dicom/scan_621_grundium_dicom
```
(`sample_files/` is gitignored; these stay local.)

- [ ] **Step 2: Register the three fixtures**

In `tests/integration_test.go`:
- Add to `slideCandidates` (mirroring existing entries) three DICOM series, each pointing at its directory under the `dicom` subdir: `Leica-4`, `3DHISTECH-1`, `scan_621_grundium_dicom`. Because DICOM slides are directories, the candidate "slide path" is the directory; confirm `resolveSlide` handles a directory path (it should `os.Stat` and accept dirs — add a dir case if needed).
- Add a `case` to `fixtureJSONFor` mapping a DICOM series directory to `tests/fixtures/<stem>.dicom.json`.
- Add the three series to `sampledByDefault()` in `generate_test.go` (all are >100 MB).

(Exact slice literals must match the existing `slideCandidates` struct shape — the executor copies an existing entry and adapts the subdir + filename.)

- [ ] **Step 3: Generate the fixtures**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run TestGenerateFixtures -generate -v
```
Expected: three new `tests/fixtures/*.dicom.json` written with sampled tile hashes + level geometry + associated-image hashes.

- [ ] **Step 4: Verify parity passes**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v
```
Expected: PASS for all three DICOM fixtures (plus the existing formats).

- [ ] **Step 5: Commit**

```bash
git add tests/integration_test.go tests/generate_test.go tests/fixtures/*.dicom.json
git commit -m "test(dicom): wire 3 scanner fixtures into TestSlideParity (sampled)"
```

---

## Task 10: Byte-identity regression + cross-backing parity

**Files:**
- Create: `formats/dicom/parity_test.go`

- [ ] **Step 1: Write the test** (fixture-guarded)

```go
package dicom_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestDICOMBackingParity(t *testing.T) {
	dir := leica4(t) // from open_test.go (same package _test)
	probe := func(backing opentile.Backing) string {
		s, err := opentile.OpenFile(dir, opentile.WithBacking(backing))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()
		b, err := s.RawTile(2, 3, 3)
		if err != nil {
			t.Fatalf("rawtile: %v", err)
		}
		h := sha256.Sum256(b)
		return string(h[:])
	}
	if probe(opentile.BackingMmap) != probe(opentile.BackingPread) {
		t.Error("mmap vs pread raw tile differ")
	}
	_ = filepath.Join
	_ = os.Stat
}
```

If `WithBacking` is not the exact option name, the executor confirms it from `options.go` (Explore reference: `BackingMmap`/`BackingPread` + a `WithBacking` option). Note the v1 `mmapOpener` always copies into a `[]byte`, so the backing choice does not currently change DICOM behavior — this test documents that invariant and guards a future true-mmap path.

- [ ] **Step 2: Run**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -run TestDICOMBackingParity -v
```
Expected: PASS (or SKIP).

- [ ] **Step 3: Full gate**

```bash
make test
```
Expected: all packages green under `-race` (DICOM tests SKIP in CI where `sample_files` is absent; they run locally).

- [ ] **Step 4: Commit**

```bash
git add formats/dicom/parity_test.go
git commit -m "test(dicom): cross-backing raw-tile parity"
```

---

## Task 11: Docs + CHANGELOG + README

**Files:**
- Modify: `docs/formats/dicom.md` (flip the pre-implementation banner to a shipped-reader doc; keep the field study + cross-scanner sections)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (add DICOM to the format list)
- Modify: `CLAUDE.md` (format count 10 → 11; new milestone block)

- [ ] **Step 1: Update `docs/formats/dicom.md`**

Replace the "pre-implementation field study" banner with a shipped-status header. Add a "What's supported" table (levels/associated/metadata/RawTile/DecodedTile; FULL+SPARSE; JPEG + uncompressed-label), a "What's not supported" table (deferred list from the spec), the two documented Open contracts, and an "Implementation references" section pointing at `formats/dicom/` + `internal/dicom/`. Keep the field study and cross-scanner sections as the empirical record.

- [ ] **Step 2: Update `CHANGELOG.md`** under `[Unreleased]`:

```markdown
### Added — DICOM WSI reader (the 11th format)

Reads DICOM VL Whole Slide Microscopy series — opentile-go's first
multi-file format. `OpenFile` accepts a series directory or any one
`.dcm` (bounded sibling-scan: same directory, same SeriesUID, WSM-only).
Handles both TILED_FULL and TILED_SPARSE; JPEG-baseline levels +
uncompressed associated images. `suyashkumar/dicom` parses metadata
behind `internal/dicom`; the raw-frame hot path is an own mmap
fragment-offset walk. Verified on Leica GT450, 3DHISTECH, and Grundium
fixtures. DICOM is reachable via `OpenFile(path)` only, not
`Open(io.ReaderAt, size)` (multi-file). `decoder/none`-class transfer
syntaxes (JPEG-LS, RLE) and JP2K/HTJ2K are deferred.
```

- [ ] **Step 3: Update `README.md`** — add "Hamamatsu/Leica/3DHISTECH/Grundium DICOM WSI" to the intro format list and bump any "10 formats" phrasing to 11.

- [ ] **Step 4: Update `CLAUDE.md`** — new milestone block describing the DICOM reader; bump "10 WSI formats" → "11".

- [ ] **Step 5: Commit**

```bash
git add docs/formats/dicom.md CHANGELOG.md README.md CLAUDE.md
git commit -m "docs(dicom): mark reader shipped; CHANGELOG + README + CLAUDE.md"
```

---

## Final verification

- [ ] **Step 1:** `make test` green under `-race` (DICOM tests run locally with `OPENTILE_TESTDIR`, SKIP in CI).
- [ ] **Step 2:** `go vet ./...` clean.
- [ ] **Step 3:** Confirm the one-cgo-dep invariant is intact — `suyashkumar/dicom` is pure Go; `go list -deps ./formats/dicom | grep -i cgo` finds nothing new. The `nocgo` build still works: `go build -tags nocgo ./...`.
- [ ] **Step 4:** Confirm `Open(reader, size)` on a DICOM byte stream returns `ErrUnsupportedFormat` (it cannot reach the path hook) — add/keep a small test asserting this documented behavior.
- [ ] **Step 5:** Dispatch a final code-review subagent over the whole `formats/dicom` + `internal/dicom` + `open.go` diff, then `superpowers:finishing-a-development-branch`.
