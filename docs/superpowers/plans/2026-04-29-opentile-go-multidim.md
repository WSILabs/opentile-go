# opentile-go Multi-Dimensional WSI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Per project execution policy, sequential in-thread execution is also supported (recent v0.7 Batches C-F took that path).

**Goal:** Land cross-format multi-dimensional WSI abstractions
(TileCoord + TileAt + SizeZ/C/T + ChannelName + ZPlaneFocus), then
implement BIF Z-stack support against them. v0.7 closeout — these
items roll up under v0.7's existing scope-grant rather than splitting
to v0.8. L21 (Volumetric Z-stacks) becomes the primary work item;
L20 (DP 600 unverified) remains trigger-driven; L19
(openslide pixel-equivalence on BIF) remains v0.8+ infrastructure-only.

**Architecture:** Additive interface evolution (`Level.TileAt` +
`Image.SizeZ/C/T/ChannelName/ZPlaneFocus`) with backward-compatible
2D semantics via embedded `SingleImage` defaults. BIF gains
multi-Z reading via `IMAGE_DEPTH` tag + tile-array stride math
(`offsets[z*M*N + serpentine_idx]`). OME forward-compatibility
verification — `<Pixels SizeZ/C/T>` honestly surfaced; per-IFD
`TileAt(z != 0)` returns `ErrDimensionUnavailable` until OME multi-Z
reader lands as a separate milestone. SVS / NDPI / Philips / IFE
(when it lands) inherit 2D-only defaults.

**Tech Stack:** Go 1.23+, libjpeg-turbo 2.1+ (existing). No new
external dependencies.

**Spec:** [`docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md`](../specs/2026-04-29-opentile-go-multidim-design.md).

**Branch:** `feat/v0.7` (extends in place — v0.7 hasn't merged yet).

**Sample slides:** No new fixtures. v0.7 BIF fixtures
(`Ventana-1.bif`, `OS-1.bif`) both have `IMAGE_DEPTH == 1` (effectively
2D); the multi-Z BIF path is exercised exclusively by synthetic
in-test fixtures (per Q5 sign-off, embedded in test source via the
existing `formats/bif/detection_test.go::buildBIFLikeBigTIFF` pattern).

**Python venv:** unchanged from v0.7. No new oracle work this
milestone.

---

## Universal task contract: "confirm upstream first"

Every task starts with `Step 0: Confirm upstream` — names the
upstream rule that governs the behaviour, states it, includes a
verification command. No task body proceeds until that command has
been run.

For multi-dim, "upstream" sources are layered:

1. **`docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md`** — the
   project-internal design spec, sealed 2026-04-29. **Cite this first**;
   §11 sign-off log records the six locked-in decisions.
2. **The Roche BIF whitepaper** (`sample_files/ventana-bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf`)
   §"Whole slide imaging process" + Appendix A (IMAGE_DEPTH tag
   32997). Convert once via `pdftotext` if `/tmp/bif-whitepaper.txt`
   doesn't exist.
3. **OME-XML schema** for `<Pixels SizeZ/SizeC/SizeT>` — already
   parsed in `formats/ome/metadata.go`; verify the fields are
   captured.
4. **The v0.6 `Image` interface** in `image.go` — the multi-dim
   additions extend it without breaking v0.6's contract.

---

## File structure

New files this plan creates:

| Path | Responsibility |
|---|---|
| `formats/bif/multiz_test.go` | Multi-Z BIF synthetic-fixture tests |

Files modified:

| Path | What changes |
|---|---|
| `geometry.go` | New `TileCoord` struct |
| `errors.go` | New `ErrDimensionUnavailable` error |
| `image.go` | `Level.TileAt` method; `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus` methods; `SingleImage` default impls |
| `image_test.go` | `fakeLevel` gains `TileAt` impl |
| `formats/svs/tiled.go` | `tiledImage.TileAt` impl (delegates to Tile, validates Z=C=T=0) |
| `formats/ndpi/striped.go` | `stripedImage.TileAt` impl |
| `formats/philips/tiled.go` | `tiledImage.TileAt` impl |
| `formats/ome/tiled.go` | `tiledImage.TileAt` impl (2D delegate; multi-Z deferred) |
| `formats/ome/oneframe.go` (or `internal/oneframe/`) | OneFrame `TileAt` impl |
| `internal/oneframe/oneframe.go` | If carries Level impl: `TileAt` impl |
| `formats/bif/level.go` | `levelImpl` gains `imageDepth`, `zPlaneFocus []float64`; `TileAt` does Z-stride math |
| `formats/bif/bif.go` | Wire `imageDepth` from per-IFD `Page.ImageDepth()`; build `zPlaneFocus` table from `<iScan>/@Z-spacing` |
| `formats/bif/metadata.go` | `bif.Metadata` gains `ZPlaneFoci []float64` |
| `formats/ome/ome.go` | `pyramidImage` gains SizeZ/C/T from parsed `<Pixels>` |
| `formats/ome/metadata.go` | OME-XML parser exposes per-Image SizeZ/C/T (already extracted; just expose) |
| `tests/parity/bif_geometry_test.go` | Add multi-dim assertions (SizeZ/C/T = 1 on existing fixtures) |
| `docs/deferred.md` | Register multi-dim API addition as v0.7 deviation; close L21; add multi-dim milestone retirement note |
| `docs/formats/bif.md` | Multi-Z surfacing in "What's supported" table; loses L21 from limitations |
| `docs/formats/ometiff.md` | Note honest SizeZ/C/T reporting + deferred multi-Z TileAt |
| `README.md` | Deviations table gets a multi-dim API row |
| `CHANGELOG.md` | `[0.7.0]` Added section gains multi-dim API + BIF Z-stack rows |
| `CLAUDE.md` | Active-limitations list loses L21 |

---

# Batch A — JIT verification gates

Three short gate tasks. Confirm spec assumptions before sinking
implementation work. Each task is probe + record under
`docs/deferred.md §9 → ### v0.7 multi-dim gates` (parallel structure
to v0.7 Batch A).

## Task 1: BIF IMAGE_DEPTH gate — confirm tag value + accessor returns expected counts

**Goal:** Verify `internal/tiff.Page.ImageDepth()` (added in v0.7 T6)
returns the right values across all 13 sample fixtures. Expected:
every BigTIFF page returns `(1, false)` since none of the local
fixtures are volumetric. The probe also pins our 32997 tag-value
choice.

**Files:**
- Modify: `docs/deferred.md` (§9 v0.7 multi-dim gates).

- [ ] **Step 0: Confirm upstream**

```sh
grep -n 'IMAGE_DEPTH\|32997\|0x80E5' /tmp/bif-whitepaper.txt 2>/dev/null \
  || pdftotext -layout sample_files/ventana-bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf /tmp/bif-whitepaper.txt
grep -n 'TagImageDepth\|ImageDepth' internal/tiff/page.go
```

- [ ] **Step 1: Probe across the local fixture set.**

```sh
cat > /tmp/imagedepth_probe.go <<'GO'
package main
import (
    "bytes"
    "fmt"
    "io"
    "io/ioutil"
    "os"
    "path/filepath"
    "github.com/wsilabs/opentile-go/internal/tiff"
    _ "github.com/wsilabs/opentile-go/formats/all"
)
func main() {
    var paths []string
    filepath.Walk("sample_files", func(p string, info os.FileInfo, _ error) error {
        if info != nil && !info.IsDir() {
            ext := filepath.Ext(p)
            if ext == ".svs" || ext == ".ndpi" || ext == ".tiff" || ext == ".bif" {
                paths = append(paths, p)
            }
        }
        return nil
    })
    for _, p := range paths {
        data, err := ioutil.ReadFile(p)
        if err != nil {
            fmt.Printf("ERR %s: %v\n", p, err)
            continue
        }
        f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
        if err != nil {
            fmt.Printf("ERR open %s: %v\n", p, err); continue
        }
        depths := []string{}
        for i, page := range f.Pages() {
            d, ok := page.ImageDepth()
            depths = append(depths, fmt.Sprintf("IFD%d=(%d,%v)", i, d, ok))
        }
        fmt.Printf("%s: %v\n", p, depths)
    }
    _ = io.Discard
}
GO
go run /tmp/imagedepth_probe.go && rm /tmp/imagedepth_probe.go
```

- [ ] **Step 2: Record outcome.** Add an entry under `docs/deferred.md §9 → ### v0.7 multi-dim gates → #### Task 1 — IMAGE_DEPTH gate` with the per-fixture table. Expected: every page on every fixture returns `(1, false)` (tag absent). Verifies (a) accessor doesn't fault on real data, (b) we have no volumetric fixtures locally → multi-Z BIF must be tested via synthetic in-code fixtures (per Q5 sign-off).

## Task 2: OME `<Pixels>` SizeZ/C/T extraction gate

**Goal:** Verify the existing OME-XML parser already extracts
`<Pixels SizeZ/SizeC/SizeT>` (since at minimum it must to size the
2D image correctly). Pin which struct field carries the values so
Batch D can wire them through.

**Files:**
- Modify: `docs/deferred.md` (§9 v0.7 multi-dim gates).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'SizeZ\|SizeC\|SizeT\|<Pixels' formats/ome/metadata.go formats/ome/series.go 2>/dev/null | head -20
```

- [ ] **Step 1: Probe Leica-1 / Leica-2.**

```sh
/tmp/probe-venv/bin/python <<'PY'
import os
os.chdir("/Users/cornish/GitHub/opentile-go")
import tifffile
import re
for p in ["sample_files/ome-tiff/Leica-1.ome.tiff",
          "sample_files/ome-tiff/Leica-2.ome.tiff"]:
    print(f"\n{p}:")
    with tifffile.TiffFile(p) as tf:
        desc = tf.pages[0].tags.get('ImageDescription').value
        for m in re.finditer(r'<Pixels[^>]*>', desc):
            attrs = m.group(0)
            sz = re.findall(r'SizeZ="([^"]*)"', attrs)
            sc = re.findall(r'SizeC="([^"]*)"', attrs)
            st = re.findall(r'SizeT="([^"]*)"', attrs)
            print(f"  Pixels: SizeZ={sz} SizeC={sc} SizeT={st}")
PY
```

- [ ] **Step 2: Record outcome.** Expected: every Pixels element has
`SizeZ=1 SizeC=3 SizeT=1` (the Leica fixtures are 2D RGB at one
time point, with C=3 representing the three RGB channels stored
contiguously per the OME convention). **Important interpretation:**
SizeC=3 here doesn't mean "3 fluorescence channels" — it's the
count of color samples per pixel, and our 2D RGB pipeline already
handles this through composite-pixel JPEGs. For the new
`Image.SizeC()` accessor, the right semantic is **separately-stored
channels**, which is 1 for these fixtures (one composite RGB JPEG
per tile). Document this carefully — getting it wrong would
double-count brightfield slides as multichannel.

## Task 3: BIF Z-spacing parsing gate

**Goal:** Confirm `<iScan>/@Z-spacing` is captured by `internal/bifxml`
(it should be, since `IScan` already has `ZLayers int`). Pin where
to wire it through to `levelImpl.zPlaneFocus`.

**Files:**
- Modify: `docs/deferred.md` (§9 v0.7 multi-dim gates).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'Z-spacing|ZSpacing|Z-layers' internal/bifxml/bifxml.go internal/bifxml/bifxml_test.go
grep -n 'Z-spacing' /tmp/bif-whitepaper.txt
```

- [ ] **Step 1: Probe both BIF fixtures.**

```sh
/tmp/probe-venv/bin/python <<'PY'
import os, re
os.chdir("/Users/cornish/GitHub/opentile-go")
import tifffile
for p in ["sample_files/ventana-bif/Ventana-1.bif",
          "sample_files/ventana-bif/OS-1.bif"]:
    with tifffile.TiffFile(p) as tf:
        xmp = tf.pages[0].tags['XMP'].value
        if isinstance(xmp, bytes): xmp = xmp.decode('utf-8','replace')
        zl = re.search(r'Z-layers="([^"]*)"', xmp)
        zs = re.search(r'Z-spacing="([^"]*)"', xmp)
        print(f"{p}: Z-layers={zl.group(1) if zl else '(missing)'}, Z-spacing={zs.group(1) if zs else '(missing)'}")
PY
```

- [ ] **Step 2: Record outcome.** Expected: both fixtures have
`Z-layers=1` (single-plane scans) and `Z-spacing=1` (default-ish
value; meaningless for 1-layer scans but present per spec). The
`bifxml.IScan` parser must extract both. If `ZSpacing` isn't a
field today, **add it** as part of Batch C — but flag here so the
follow-up work is clear.

---

# Batch B — Interface evolution (Phase α)

Pure interface evolution. Every existing format inherits 2D-only
semantics via embedded defaults. Codebase compiles + every test
passes after this batch with no behavioural change.

## Task 4: New `TileCoord` struct + `ErrDimensionUnavailable`

**Goal:** Land the multi-dim coordinate type and the
new error sentinel. No interface changes yet.

**Files:**
- Modify: `geometry.go`, `errors.go`.
- New: `geometry_test.go` gains `TileCoord` round-trip tests (already exists).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'type Tile(Coord|Pos)|^func.*TileCoord' geometry.go geometry_test.go
grep -nE 'ErrTileOutOfBounds|ErrDimensionUnavailable' errors.go
```

- [ ] **Step 1: Add `TileCoord` to `geometry.go`.**

```go
// TileCoord identifies a tile by its position in the multi-
// dimensional WSI space. X and Y are the existing 2D grid
// position; Z, C, and T select among focal planes, fluorescence
// channels, and time points respectively.
//
// Z, C, T default to zero — a TileCoord literal {X: x, Y: y}
// addresses the same tile that Level.Tile(x, y) returns. Zero is
// the "nominal" / "first" / "T=0" plane in every dimension.
//
// Valid range per axis:
//   0 <= X < Level.Grid().W
//   0 <= Y < Level.Grid().H
//   0 <= Z < Image.SizeZ()
//   0 <= C < Image.SizeC()
//   0 <= T < Image.SizeT()
//
// Out-of-range values yield *opentile.TileError wrapping
// ErrDimensionUnavailable (axis is unsupported on the underlying
// format) or ErrTileOutOfBounds (axis is supported but the index
// is past the size).
type TileCoord struct {
    X, Y int
    Z    int
    C    int
    T    int
}
```

- [ ] **Step 2: Add `ErrDimensionUnavailable` to `errors.go`.**

```go
ErrDimensionUnavailable = errors.New("opentile: dimension not supported on this format/image")
```

Update existing `errors_test.go` if it tests the error list shape.

- [ ] **Step 3: Verify.** `go build ./...`; `go vet ./...`;
`go test ./... -count=1 -race`.

- [ ] **Step 4: Commit.** `feat(opentile): add TileCoord +
ErrDimensionUnavailable for multi-dim addressing`.

## Task 5: `Level.TileAt` interface method + concrete impls on existing formats

**Goal:** Add `TileAt(coord TileCoord) ([]byte, error)` to the
`Level` interface. Add 2D-delegating impls to every concrete level
type so the codebase compiles.

**Files:**
- Modify: `image.go` (interface), `formats/svs/tiled.go`,
  `formats/ndpi/striped.go`, `formats/philips/tiled.go`,
  `formats/ome/tiled.go`, `internal/oneframe/oneframe.go`,
  `formats/bif/level.go`, `image_test.go` (`fakeLevel`).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'type Level interface' image.go
grep -nE 'func \(l \*\w+\) TileSize\(\)' formats/*/*.go internal/oneframe/oneframe.go
```

- [ ] **Step 1: Add to interface in `image.go`.**

```go
type Level interface {
    // ... existing methods unchanged
    Tile(x, y int) ([]byte, error)
    // TileAt returns the raw compressed tile bytes at the given
    // multi-dimensional coordinate. Tile(x, y) is shorthand for
    // TileAt(TileCoord{X: x, Y: y}). For 2D-only Levels (SizeZ/C/T
    // on the parent Image == 1), any non-zero Z, C, or T value
    // returns *TileError wrapping ErrDimensionUnavailable.
    TileAt(coord TileCoord) ([]byte, error)
    // ... existing methods continue
    TileReader(x, y int) (io.ReadCloser, error)
    Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult]
}
```

- [ ] **Step 2: Add delegating impls on each existing concrete
level type.** Pattern:

```go
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
    if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
        return nil, &opentile.TileError{
            Level: l.index, X: coord.X, Y: coord.Y,
            Err: opentile.ErrDimensionUnavailable,
        }
    }
    return l.Tile(coord.X, coord.Y)
}
```

Apply to:
- `formats/svs/tiled.go::*tiledImage`
- `formats/ndpi/striped.go::*stripedImage`
- `internal/oneframe/oneframe.go::*Image`
- `formats/philips/tiled.go::*tiledImage`
- `formats/ome/tiled.go::*tiledImage`
- `formats/bif/level.go::*levelImpl` (will be replaced in Batch C with multi-Z-aware version)
- `image_test.go::*fakeLevel`

- [ ] **Step 3: Verify.** `go build ./...`; `go vet ./...`;
`go test ./... -count=1 -race` (every test stays green; behaviour
unchanged).

- [ ] **Step 4: Commit.** `feat(level): add TileAt(TileCoord) to
Level interface; 2D delegates`.

## Task 6: `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus` interface methods + `SingleImage` defaults

**Goal:** Add the dimension-accessor methods to `Image` interface.
`SingleImage` (the helper used by SVS/NDPI/Philips/BIF/IFE) gets
2D-default implementations.

**Files:**
- Modify: `image.go`, `image_test.go` (`fakeImage` if present).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'type Image interface\|type SingleImage struct' image.go
grep -n 'fakeImage\|fakeLevel' image_test.go
```

- [ ] **Step 1: Add interface methods.**

```go
type Image interface {
    // ... existing methods
    Index() int
    Name() string
    Levels() []Level
    Level(i int) (Level, error)
    MPP() SizeMm
    // NEW: dimension counts. Default 1 for 2D formats; > 1 for
    // multi-dim formats like volumetric BIF or multi-Z OME.
    SizeZ() int
    SizeC() int
    SizeT() int
    // NEW: per-axis metadata accessors. Defaults are empty/zero.
    ChannelName(c int) string
    ZPlaneFocus(z int) float64
}
```

- [ ] **Step 2: Add defaults to `SingleImage`.**

```go
func (s *SingleImage) SizeZ() int                  { return 1 }
func (s *SingleImage) SizeC() int                  { return 1 }
func (s *SingleImage) SizeT() int                  { return 1 }
func (s *SingleImage) ChannelName(c int) string    { return "" }
func (s *SingleImage) ZPlaneFocus(z int) float64   { return 0 }
```

- [ ] **Step 3: Update test mocks.** Any `fakeImage` in
`image_test.go` gains the five new methods; tests stay green.

- [ ] **Step 4: Verify.** `go build ./...`; `go test ./... -count=1
-race`.

- [ ] **Step 5: Commit.** `feat(image): add SizeZ/SizeC/SizeT/
ChannelName/ZPlaneFocus to Image interface; SingleImage defaults`.

---

# Batch C — BIF Z-stack support (Phase β)

The BIF format gains real multi-Z reading. Per Q5: synthetic-only
fixture coverage (no real volumetric BIF in `sample_files/`).

## Task 7: Surface `Z-spacing` from `internal/bifxml`

**Goal:** Add `ZSpacing float64` to `bifxml.IScan` (or extend the
existing field set). Used by Batch C downstream to compute
`ZPlaneFocus(z)`.

**Files:**
- Modify: `internal/bifxml/bifxml.go`, `internal/bifxml/bifxml_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'ZLayers|Z-spacing|ZSpacing' internal/bifxml/bifxml.go
```

- [ ] **Step 1: Add field + parser.** Mirrors the existing
`ZLayers int` field; `Z-spacing` is a float (microns per plane).

```go
type IScan struct {
    // ... existing fields
    ZLayers  int
    ZSpacing float64 // NEW: microns per Z-plane step (per <iScan>/@Z-spacing)
    // ... rest
}
```

Add parsing in `parseIScanAttrs` (lenient — missing/empty defaults
to 0, matching `ZLayers` convention).

- [ ] **Step 2: Test.** Add a synthetic-XMP test case that asserts
`ZSpacing` extracted correctly from `<iScan Z-spacing="1.5">`.

- [ ] **Step 3: Verify.** `go test ./internal/bifxml/... -count=1 -race`.

- [ ] **Step 4: Commit.** `feat(bifxml): expose Z-spacing
attribute as IScan.ZSpacing`.

## Task 8: BIF `levelImpl` gains `imageDepth` + multi-Z `TileAt`

**Goal:** `levelImpl` reads each pyramid IFD's `IMAGE_DEPTH` tag at
construction time and stores `imageDepth int`. `TileAt(coord)`
applies the serpentine remap to (X, Y) and reads
`offsets[coord.Z * (gridW * gridH) + serpIdx]` with bounds checking
on `coord.Z`.

**Files:**
- Modify: `formats/bif/level.go` (the existing `levelImpl`),
  `formats/bif/bif.go` (Tiler construction passes per-level depth).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'IMAGE_DEPTH|TagImageDepth|ImageDepth' /tmp/bif-whitepaper.txt internal/tiff/page.go formats/bif/level.go
```

- [ ] **Step 1: Add `imageDepth int` field to `levelImpl`.**

In `formats/bif/level.go`:

```go
type levelImpl struct {
    // ... existing fields
    offsets        []uint64
    counts         []uint64
    jpegTables     []byte
    scanWhitePoint uint8
    // NEW:
    imageDepth     int     // tile-array stride per Z-plane; 1 for 2D BIFs
    zPlaneFocus    []float64 // ZPlaneFocus(z) lookup table; len == imageDepth
    // ... rest
    reader         io.ReaderAt
}
```

- [ ] **Step 2: Wire `imageDepth` in `newLevelImpl`.**

```go
imageDepth, _ := c.Page.ImageDepth()  // returns (1, false) for absent tag
if imageDepth < 1 {
    imageDepth = 1
}
expected := imageDepth * cols * rows
if len(offsets) != len(counts) || len(offsets) != expected {
    return nil, fmt.Errorf("bif level=%d: tile table length mismatch: offsets=%d counts=%d expected=%d (= depth %d × grid %dx%d)",
        c.Level, len(offsets), len(counts), expected, imageDepth, cols, rows)
}
```

Sanity-check: spec says BIF stores the entire Z-stack across one
TileOffsets array. Length must equal `imageDepth * cols * rows`.

- [ ] **Step 3: Add `TileAt(coord)` method.** Replaces the
delegating impl from Batch B Task 5.

```go
func (l *levelImpl) TileAt(coord opentile.TileCoord) ([]byte, error) {
    if coord.C != 0 || coord.T != 0 {
        return nil, &opentile.TileError{
            Level: l.index, X: coord.X, Y: coord.Y,
            Err: opentile.ErrDimensionUnavailable,
        }
    }
    if coord.Z < 0 || coord.Z >= l.imageDepth {
        return nil, &opentile.TileError{
            Level: l.index, X: coord.X, Y: coord.Y,
            Err: opentile.ErrTileOutOfBounds,
        }
    }
    if coord.X < 0 || coord.Y < 0 || coord.X >= l.grid.W || coord.Y >= l.grid.H {
        return nil, &opentile.TileError{
            Level: l.index, X: coord.X, Y: coord.Y,
            Err: opentile.ErrTileOutOfBounds,
        }
    }
    serpIdx := imageToSerpentine(coord.X, coord.Y, l.grid.W, l.grid.H)
    if serpIdx < 0 {
        return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrTileOutOfBounds}
    }
    // Z-plane stride: each Z-plane occupies (gridW * gridH) tile entries.
    idx := coord.Z*(l.grid.W*l.grid.H) + serpIdx
    if l.isEmpty(idx) {
        return blankTile(l.tileSize.W, l.tileSize.H, l.scanWhitePoint)
    }
    return l.readTileAtIdx(idx, coord.X, coord.Y)
}
```

`readTileAtIdx` is a new internal helper extracting the existing
`Tile()` logic so both paths share it (cleanup refactor from
existing Tile()).

- [ ] **Step 4: Existing `Tile(x, y)` becomes a shorthand.**

```go
func (l *levelImpl) Tile(x, y int) ([]byte, error) {
    return l.TileAt(opentile.TileCoord{X: x, Y: y})
}
```

- [ ] **Step 5: Build `zPlaneFocus` table at Open time.** In
`formats/bif/bif.go`'s `Open()`:

```go
zSpacing := 0.0
if iscan != nil {
    zSpacing = iscan.ZSpacing
}
// Compute per-Z focal offset per BIF spec:
//   Z=0 nominal (offset = 0)
//   Z=1..n_near map to -1*spacing, -2*spacing, ..., -n_near*spacing
//   Z=n_near+1..n_near+n_far map to +1*spacing, +2*spacing, ..., +n_far*spacing
// (Per whitepaper §"Whole slide imaging process" — near layers first, then far.)
n := /* level-0's IMAGE_DEPTH */
focus := make([]float64, n)
nNear := (n - 1) / 2
nFar := n - 1 - nNear
focus[0] = 0
for i := 1; i <= nNear; i++ {
    focus[i] = -float64(i) * zSpacing
}
for i := 1; i <= nFar; i++ {
    focus[nNear+i] = float64(i) * zSpacing
}
```

Pass `focus` into each `levelImpl` (or store on Tiler and have
`zPlaneFocus(z)` look it up).

- [ ] **Step 6: Verify.** `go test ./formats/bif/... -count=1 -race`
(all existing tests stay green — `imageDepth=1` path is unchanged
behaviour).

- [ ] **Step 7: Commit.** `feat(bif): multi-Z TileAt with
IMAGE_DEPTH × tile-array stride`.

## Task 9: BIF Image gains `SizeZ/ZPlaneFocus` accessors

**Goal:** `bif.Tiler`'s `Image` (currently `*opentile.SingleImage`)
gets a wrapper that overrides `SizeZ()` and `ZPlaneFocus(z)` based
on the level-0 IFD's `imageDepth`.

**Files:**
- Modify: `formats/bif/bif.go` (introduce `bifImage` type that
  embeds `*opentile.SingleImage`).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'SingleImage\|Tiler.image' formats/bif/bif.go
```

- [ ] **Step 1: Define `bifImage`.**

```go
// bifImage wraps SingleImage with BIF-specific multi-Z accessors.
type bifImage struct {
    *opentile.SingleImage
    sizeZ        int
    zPlaneFocus  []float64
}

func (i *bifImage) SizeZ() int                  { return i.sizeZ }
func (i *bifImage) ZPlaneFocus(z int) float64 {
    if z < 0 || z >= len(i.zPlaneFocus) {
        return 0 // out of range; matches Image.SizeZ() bounds
    }
    return i.zPlaneFocus[z]
}
```

- [ ] **Step 2: Wire in `Open()`.** Replace the
`opentile.NewSingleImage(levels)` call with a `bifImage`
constructor that captures `imageDepth` from the level-0 IFD's
`Page.ImageDepth()` and `zPlaneFocus` from the precomputed table.

- [ ] **Step 3: Test.** Add to `formats/bif/multiz_test.go`:
- A synthetic 1-Z BIF reports `SizeZ() == 1`, `ZPlaneFocus(0) == 0`.
- A synthetic 3-Z BIF (1 near, nominal, 1 far, with Z-spacing=1.5)
  reports `SizeZ() == 3`, ZPlaneFocus values in the expected order
  and signs.
- A synthetic 5-Z BIF (2 near, nominal, 2 far) similar.

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race
-run TestMultiZ`.

- [ ] **Step 5: Commit.** `feat(bif): expose SizeZ + ZPlaneFocus on
the Image wrapper`.

## Task 10: Synthetic multi-Z BIF fixture builder + integration tests

**Goal:** Extend `formats/bif/detection_test.go::iFDSpec` and
`buildBIFLikeBigTIFF` to support multi-Z layouts. Per Q5, the
fixture lives in test code only.

**Files:**
- Modify: `formats/bif/detection_test.go`.
- New: `formats/bif/multiz_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'iFDSpec struct\|buildBIFLikeBigTIFF' formats/bif/detection_test.go
```

- [ ] **Step 1: Extend `iFDSpec` with `imageDepth int` field.**

When `imageDepth > 0`:
- Builder writes `IMAGE_DEPTH` tag (32997, type SHORT, count 1, value imageDepth).
- TileOffsets / TileByteCounts arrays have length `imageDepth × gridW × gridH`.
- Each Z-plane gets `gridW × gridH` consecutive entries with
  per-tile content distinguishable across Z (e.g., `tileFill = byte(zPlaneIdx)`).

- [ ] **Step 2: Fixture builder logic for multi-Z.** Add a small
helper:

```go
// For multi-Z fixtures, tileBytes is laid out as:
//   [Z=0 tiles in serpentine order]
//   [Z=1 tiles in serpentine order]
//   ...
// Each Z-plane's tile fill differs so tests can verify the right
// plane is read.
```

- [ ] **Step 3: Tests in `multiz_test.go`.**

- `TestMultiZBIFOpens`: construct a 3-Z BIF with `imageDepth=3` and
  Z-spacing=1.5; verify `tiler.Images()[0].SizeZ() == 3`,
  `ZPlaneFocus(0) == 0`, `ZPlaneFocus(1) == -1.5`,
  `ZPlaneFocus(2) == +1.5`.
- `TestMultiZTileAtReadsCorrectPlane`: `TileAt({X: 0, Y: 0, Z: 0})`
  returns the Z=0 fill bytes; `TileAt({Z: 1})` returns the Z=1
  fill bytes; etc.
- `TestMultiZTileBoundsCheck`: `TileAt({Z: 3})` (out of range for
  3-Z slide) returns `*TileError` wrapping `ErrTileOutOfBounds`.
- `TestMultiZTileChannelTimeBoundsCheck`: `TileAt({C: 1})`
  returns `*TileError` wrapping `ErrDimensionUnavailable`.
- `TestSingleZBIFCompatibility`: a `imageDepth=0` (omit IMAGE_DEPTH
  tag) fixture reports `SizeZ() == 1` and tile reads work
  identically to before.

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race
-run TestMultiZ`.

- [ ] **Step 5: Commit.** `feat(bif): synthetic multi-Z fixture +
TileAt unit tests`.

## Task 11: BIF metadata gains `ZPlaneFoci`

**Goal:** `bif.Metadata` exposes the per-Z focal offsets for
consumers using `bif.MetadataOf(tiler)`.

**Files:**
- Modify: `formats/bif/metadata.go`, `formats/bif/metadata_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'type Metadata struct|ZLayers|ZSpacing|ZPlaneFoc' formats/bif/metadata.go
```

- [ ] **Step 1: Add fields.**

```go
type Metadata struct {
    // ... existing fields
    ZLayers  int
    // NEW:
    ZSpacing  float64    // microns per Z-plane step from <iScan>/@Z-spacing
    ZPlaneFoci []float64 // per-Z focal offset (microns); ZPlaneFoci[z] = ZPlaneFocus(z)
}
```

Build `ZPlaneFoci` once at metadata construction; same data the
levelImpl uses.

- [ ] **Step 2: Test.** Synthetic fixture with `Z-spacing=2.0`,
`Z-layers=5` (2 near, nominal, 2 far) reports
`ZPlaneFoci == [0, -2.0, -4.0, +2.0, +4.0]`.

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race
-run TestMetadata`.

- [ ] **Step 4: Commit.** `feat(bif): expose ZSpacing + ZPlaneFoci
on bif.Metadata`.

---

# Batch D — OME forward-compatibility (Phase γ)

Honest reporting of OME's `<Pixels SizeZ/C/T>` even though full
OME multi-Z `TileAt` reading isn't implemented.

## Task 12: OME-XML parser surfaces SizeZ/SizeC/SizeT per Image

**Goal:** Wherever `formats/ome/metadata.go` already extracts
`<Pixels>` attributes (per Batch A Task 2), expose the count fields
on the per-Image metadata struct.

**Files:**
- Modify: `formats/ome/metadata.go`, `formats/ome/series.go` (or
  wherever per-Image metadata is built), `formats/ome/ome.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'SizeZ|SizeC|SizeT|<Pixels' formats/ome/*.go
```

- [ ] **Step 1: Surface fields per Image.** The existing `Pixels`
struct (or equivalent) already extracts them; ensure they're stored
on the per-Image metadata that backs `pyramidImage`.

- [ ] **Step 2: Override `pyramidImage` accessors.**

```go
type pyramidImage struct {
    // ... existing
    sizeZ, sizeC, sizeT int
    channelNames []string  // optional; default-empty entries
}

func (i *pyramidImage) SizeZ() int                  { return i.sizeZ }
func (i *pyramidImage) SizeC() int                  { return i.sizeC }
func (i *pyramidImage) SizeT() int                  { return i.sizeT }
func (i *pyramidImage) ChannelName(c int) string {
    if c < 0 || c >= len(i.channelNames) {
        return ""
    }
    return i.channelNames[c]
}
func (i *pyramidImage) ZPlaneFocus(z int) float64 {
    return 0  // OME-XML may have <Plane PositionZ> but v0.7 doesn't parse it
}
```

**Important interpretation per Batch A Task 2 outcome:** for
brightfield OME files (Leica fixtures), `<Pixels SizeC=3>` describes
RGB sample count, NOT separately-stored channels. The reader's
job is to figure out which is which. For v0.7, the simplest correct
behaviour is:

- If `Channels` count == 1 (the typical RGB-stored-as-one-element
  case), report `SizeC() = 1` regardless of `<Pixels SizeC>`.
- If `Channels` count > 1 (each channel has its own `<Channel>`
  element), report `SizeC()` = that count.

OME-XML's `<Channel>` element count is the better discriminator
than `<Pixels SizeC>` for the "separately-stored channels" semantic.

- [ ] **Step 3: TileAt impl in `formats/ome/tiled.go`.** When
`SizeZ/C/T > 1` and the caller asks for a non-zero plane:

```go
func (l *tiledImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
    if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
        return nil, &opentile.TileError{
            Level: l.index, X: coord.X, Y: coord.Y,
            Err: opentile.ErrDimensionUnavailable, // multi-Z OME reading deferred
        }
    }
    return l.Tile(coord.X, coord.Y)
}
```

Even if `pyramidImage.SizeZ() > 1`, `TileAt(z != 0)` errors loudly.
Half-supported per Q6.

- [ ] **Step 4: Test.** Both Leica fixtures report
`SizeZ() = SizeC() = SizeT() = 1` (no multi-Z, no fluorescence,
no time series). Assert via `formats/ome/integration_test.go`.

- [ ] **Step 5: Verify.** `go test ./formats/ome/... -count=1 -race`
+ `OPENTILE_TESTDIR=$PWD/sample_files go test ./formats/ome/...
-count=1`.

- [ ] **Step 6: Commit.** `feat(ome): honestly surface
Image.SizeZ/SizeC/SizeT from <Pixels>`.

## Task 13: Document OME multi-Z TileAt implementation strategy (deferred)

**Goal:** Write the deferral plan into `docs/formats/ometiff.md` so the
future implementer has it.

**Files:**
- Modify: `docs/formats/ometiff.md` ("Active limitations" section).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'Active limitations|Multi-Z|SizeZ' docs/formats/ometiff.md
```

- [ ] **Step 1: Add a paragraph** describing the per-IFD
addressing strategy a future implementer would need (compute IFD
index from (Z, C, T) using `<Pixels DimensionOrder>`; resolve via
SubIFD chain when applicable).

- [ ] **Step 2: Commit.** `docs(ome): document deferred multi-Z
TileAt strategy`.

---

# Batch E — Tests + docs

Final integration. Every existing 2D fixture round-trips through
the new API; v0.7 docs reflect the multi-dim closeout.

## Task 14: Round-trip compatibility tests

**Goal:** Pin that every existing 2D fixture reports
`SizeZ/C/T = 1` and that `Tile(x, y) ≡ TileAt({X: x, Y: y})`
byte-identically.

**Files:**
- Modify: `tests/parity/bif_geometry_test.go` (extend existing
  per-fixture pinning) OR new `tests/parity/multidim_test.go`
  (cross-format). Going with the new file.
- New: `tests/parity/multidim_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
ls tests/parity/
grep -n 'TestBIFGeometry' tests/parity/bif_geometry_test.go
```

- [ ] **Step 1: Implement.** Iterates every fixture in
`slideCandidates` (from `tests/integration_test.go`). For each:
- Open, read `Images()[0]`.
- Assert `SizeZ() == 1`, `SizeC() == 1`, `SizeT() == 1`.
  (Skip BIF fixtures that DO have multi-Z if any added in
  the future — none in v0.7 sample_files.)
- For each Level: Tile(0, 0) and TileAt(TileCoord{X: 0, Y: 0})
  return byte-identical results.

```go
func TestMultiDimCompat2D(t *testing.T) {
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set")
    }
    for _, name := range slideCandidates {
        t.Run(name, func(t *testing.T) {
            slide, ok := resolveSlide(dir, name)
            if !ok { t.Skipf("%s not present", name) }
            tiler, err := opentile.OpenFile(slide)
            if err != nil { t.Fatalf("OpenFile: %v", err) }
            defer tiler.Close()
            img := tiler.Images()[0]
            if got := img.SizeZ(); got != 1 {
                t.Errorf("SizeZ: got %d, want 1 (2D fixture)", got)
            }
            // ... SizeC, SizeT
            for li, lvl := range img.Levels() {
                a, _ := lvl.Tile(0, 0)
                b, _ := lvl.TileAt(opentile.TileCoord{X: 0, Y: 0})
                if !bytes.Equal(a, b) {
                    t.Errorf("L%d: Tile(0,0) vs TileAt({0,0}) byte mismatch", li)
                }
            }
        })
    }
}
```

- [ ] **Step 2: Define `slideCandidates` + `resolveSlide` for the
new package** OR move them to a shared test helper. Going with
exposing `tests.SlideCandidates` and `tests.ResolveSlide` so both
`tests_test` and `parity_test` can share.

- [ ] **Step 3: Verify.**

```sh
OPENTILE_TESTDIR=$PWD/sample_files go test ./tests/parity/... -count=1 -race
```

All 16 fixtures (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF) pass.

- [ ] **Step 4: Commit.** `test(parity): cross-format multi-dim
compatibility round-trip`.

## Task 15: docs/deferred.md — close L21, register multi-dim deviations

**Goal:** Mark L21 as resolved. Add a multi-dim deviations entry to
§1a. Add a "Multi-dim" subsection to §8a "Retired in v0.7."

**Files:**
- Modify: `docs/deferred.md`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'L21\|^## 8a\|multi-dim\|Multi-dim' docs/deferred.md
```

- [ ] **Step 1: Update L21.** Move from "Active limitations" §2 to
"Retired in v0.7" §8a. Note that the multi-dim API landed; full BIF
multi-Z support landed; OME multi-Z stays deferred.

- [ ] **Step 2: Add §1a deviation entry.**

```markdown
### Multi-dimensional WSI API addition (since v0.7)

- **Upstream:** Python opentile is 2D-only. Each format's pyramid
  is exposed as a flat list of levels with no Z/C/T axis support.
- **opentile-go:** adds cross-format multi-dim addressing via
  `Level.TileAt(TileCoord)`, `Image.SizeZ/SizeC/SizeT`,
  `ChannelName`, `ZPlaneFocus`. 2D-only formats (SVS / NDPI /
  Philips) inherit defaults from `SingleImage`; BIF surfaces
  `IMAGE_DEPTH`-driven multi-Z; OME honestly reports `<Pixels>`
  dimension counts.
- **Reason:** modern WSI consumers — fluorescence imaging, focal
  plane viewers, time series — need explicit multi-dim addressing.
  Designed cross-format-extensibly so OME multi-Z, future
  fluorescence, and time-series support land additively without
  re-shaping the API.
- **Tracking:** see [`docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md`](superpowers/specs/2026-04-29-opentile-go-multidim-design.md).
```

- [ ] **Step 3: Add §8a entry** under v0.7 retirement audit
listing the multi-dim work as part of v0.7 closeout.

- [ ] **Step 4: Commit.** `docs(v0.7): retire L21; register
multi-dim deviation`.

## Task 16: docs/formats/bif.md — multi-Z surfacing

**Goal:** Update bif.md "What's supported" to include multi-Z;
remove L21 from "What's not supported."

**Files:**
- Modify: `docs/formats/bif.md`.

- [ ] **Step 1: Update the "What's supported" table** with a row
for multi-Z reading via `IMAGE_DEPTH` + `ZPlaneFocus`.

- [ ] **Step 2: Remove or downgrade the L21 entry** in "What's not
supported." Note that we still don't have a real volumetric
fixture; coverage is synthetic-only.

- [ ] **Step 3: Add a paragraph** to "Implementation references"
listing the multi-dim spec doc.

- [ ] **Step 4: Commit.** `docs(bif): document multi-Z support`.

## Task 17: docs/formats/ometiff.md — honest dimension reporting

**Goal:** Document the half-supported state — SizeZ/C/T honestly
surfaced, but `TileAt(z != 0)` errors with `ErrDimensionUnavailable`
until OME multi-Z reader lands.

**Files:**
- Modify: `docs/formats/ometiff.md`.

- [ ] **Step 1: Add a "Multi-dimensional reading" subsection** under
"What's supported" / "What's not supported." Note the `<Pixels>`
extraction strategy; the deferral of per-IFD `TileAt`; the OME-XML
`<Channel>`-count vs `<Pixels SizeC>` distinction (per Batch A Task 2
outcome).

- [ ] **Step 2: Commit.** `docs(ome): note honest SizeZ/C/T +
deferred multi-Z TileAt`.

## Task 18: README.md + CHANGELOG.md + CLAUDE.md updates

**Goal:** Final v0.7 documentation closeout.

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `CLAUDE.md`.

- [ ] **Step 1: README.md deviations table** gains a multi-dim API
row.

- [ ] **Step 2: CHANGELOG.md `[0.7.0]` Added section** gains:
- "Multi-dimensional addressing — `Level.TileAt(TileCoord)` +
  `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus`. Additive; 2D
  formats unchanged."
- "BIF multi-Z reading via `IMAGE_DEPTH` (32997) tag. Synthetic
  fixture coverage; no real volumetric BIF in `sample_files/`."
- "OME-TIFF honest `<Pixels SizeZ/SizeC/SizeT>` reporting. Multi-Z
  `TileAt` returns `ErrDimensionUnavailable` until the per-IFD
  reader lands as a separate format-package milestone."

- [ ] **Step 3: CLAUDE.md** active-limitations list loses L21
(replaced by v0.8+ items if any new ones surface during multi-dim
implementation).

- [ ] **Step 4: Commit.** `docs(v0.7): multi-dim API + BIF Z-stack
in CHANGELOG / README / CLAUDE.md`.

## Task 19: Final validation sweep

**Goal:** All gates green; v0.7 ready to merge.

- [ ] **Step 0: Sweep.**

```sh
make vet
make test
OPENTILE_TESTDIR=$PWD/sample_files go test ./... -count=1 -race
OPENTILE_ORACLE_PYTHON=/tmp/probe-venv/bin/python OPENTILE_TESTDIR=$PWD/sample_files \
  go test ./tests/oracle/... -tags parity -count=1 -timeout 5m
make cover  # ≥80% per package, including new multi-dim paths
```

- [ ] **Step 1: Coverage.** `make cover` reports ≥80% on every
package, including `formats/bif/` (multi-Z paths) and the new
`tests/parity/multidim_test.go`.

- [ ] **Step 2: Branch state check.** `git log --oneline main..HEAD`
shows the v0.7 multi-dim commits stacked on the v0.7 BIF commits;
`git status` clean.

- [ ] **Step 3: PR title** suggestion: `v0.7: Ventana BIF support +
cross-format multi-dim addressing`. PR body cross-links the v0.7
design spec, the multi-dim design spec, the deferred.md retirement
audit, and the CHANGELOG entry.

- [ ] **Step 4: After merge, tag.**

```sh
git checkout main && git pull
git tag -a v0.7.0 -m "v0.7.0: Ventana BIF + multi-dim WSI" && git push --tags
```

---

## Plan-level notes

- **Backward compatibility is load-bearing.** Every existing
  consumer calling `Tile(x, y)` keeps working. Every existing test
  fixture stays green without regeneration. Any failure in the
  cross-format `TestMultiDimCompat2D` test (Task 14) is a real
  regression to investigate, not a fixture-update opportunity.
- **Multi-Z BIF coverage is synthetic-only.** Both real BIF
  fixtures in `sample_files/ventana-bif/` have `IMAGE_DEPTH = 1`
  (effectively 2D). The Z-stack code path is exercised exclusively
  by tests in `formats/bif/multiz_test.go` using in-code-built
  fixtures. If a real volumetric BIF surfaces, add it under L20's
  trigger-driven workflow and verify against this milestone's
  output.
- **OME multi-Z reading is deferred.** Surfacing `SizeZ/C/T` is
  half-support — consumers can detect multi-Z OMEs and graceful-
  fail-forward via `ErrDimensionUnavailable`. Reading actual
  multi-Z tiles requires per-IFD addressing (compute IFD from
  `(Z, C, T)` per `<Pixels DimensionOrder>` and resolve via SubIFD
  chain when applicable). Documented in §10 / Task 13.
- **Step-0 contract continues to be load-bearing.** Multi-dim is
  not a "direct port from upstream Python opentile" milestone —
  there's no upstream to port from; the cross-format API is
  opentile-go's own design. The Step 0 confirmations are against
  the v0.7-multidim spec (sealed 2026-04-29) + the BIF whitepaper +
  OME-XML schema.
- **No new external deps.** Stdlib + the existing libjpeg-turbo
  cgo pin. AVIF decoder integration deferred to whenever IFE
  multi-channel (or OME fluorescence) genuinely lands.
