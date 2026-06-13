# v1.0 API Breaking Pass — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the coordinated breaking pass of the v1.0 API cleanup — rename the
container type, move tile/region reads onto receiver methods, move associated-image
accessors onto the interface, unify geometry/units, and type the stringly values —
in one consumer-coordinated sweep.

**Architecture:** Six non-structural renames first (each keeps the build green),
then accessors-onto-interface, then the receiver-method restructure (the large
item: `*Level`/`*Pyramid` gain read methods, `*Slide` loses them). The deferred
multi-dimensional surface (`decoder.Image` `Bands`/`Sample`, `TileAt`/`…At`,
`Pyramid.SizeZ/C/T`) is OUT OF SCOPE here — it is fixture-gated (see the design
doc §5/§9).

**Tech Stack:** Go 1.23+, the existing `internal/*` reader chain, `make test`
(`go test ./... -race`), `make bench-ndpi` / `make bench-svs` / `make bench-ndpi-mem`
gates, fixture parity suite under `tests/` (needs `OPENTILE_TESTDIR`).

**Design:** `docs/superpowers/specs/2026-06-12-v1-api-vocabulary-and-multidim-design.md`
(§9 records the five resolved decisions this plan implements).

**Consumers:** `wsitools` and `openscope` import this package directly. Task 0
produces the migration note; the work branch must not merge until both have
signed off.

---

## File Structure

Root package (`github.com/wsilabs/opentile-go`), key files:
- `image.go` — `Level` struct, `AssociatedImage` interface + `AssociatedType` consts, `Image` struct (→ `Pyramid`), `TilePos`, `TileResult`.
- `slide.go` — `*Slide`, `Images()`/`Levels()`/`Level(i)`, `RawTile`/`RawTileInto`/`TileReader`/`TileMaxSize`/`TilePrefix`/`TileBodyInto`/`WarmLevel`, all `Image*` twins, `Associated()`, `AssociatedIFDOffset`, `slideReader` interface.
- `decoded_tile.go` — `DecodedTile`/`DecodedTileInto`/`ImageDecodedTile*`.
- `region.go`, `region_scaled.go` — `ReadRegion*` family.
- `strips.go` — `ScaledStrips`/`StripIterator`/`StripOption`.
- `associated_encoding.go` — `AssociatedEncoding` + `Slide.AssociatedEncoding(a)`.
- `tifftags.go` — `TIFFTags`/`TIFFDirectory`/`TIFFDirectoriesOf`/`LevelTIFFTags`/`AssociatedTIFFTags`.
- `metadata.go` — `Metadata` (MPP fields), `geometry.go` — `Point`/`Size`/`Region`/`SizeMm`.
- `image_valuetype_test.go` — asserts `Image`/`Level` value semantics (CHANGES in Task 8).

Format packages (`formats/<f>/`, 11 of them: svs, ndpi, philipstiff, ometiff, bif, ife, szi, leicascn, generictiff, cogwsi, dicom) — each has an `associated.go` (or `mappage.go`) implementing the `AssociatedImage` methods, and a tiler implementing `slideReader`.

`decoder/` — `decoder.Image` (untouched here; band/depth is deferred).

`bench/`, `tests/`, `tests/parity/` — call sites that must be updated when Slide read methods move.

Out-of-tree but in-scope for the migration note: `../wsitools`, `../openscope`.

---

## Phase 1 — Non-structural renames (build stays green at each task)

### Task 0: Migration note (first deliverable)

**Files:**
- Create: `docs/migrations/2026-06-12-v1-api-breaking-pass.md`

- [ ] **Step 1: Write the migration note**

A consumer-facing before→after table covering every breaking change in this plan,
so wsitools/openscope can map their call sites. Include exactly these rows (fill
the "after" forms as each later task finalizes them; this doc is updated as the
plan lands):

```markdown
# Migration: v1.0 API breaking pass

| Area | Before | After |
|------|--------|-------|
| Container type | `opentile.Image` | `opentile.Pyramid` |
| Pyramid list | `slide.Images()` / `slide.Image(i)` | `slide.Pyramids()` / `slide.Pyramid(i)` |
| Associated list | `slide.Associated()` | `slide.AssociatedImages()` |
| Associated encoding | `slide.AssociatedEncoding(a)` | `a.Encoding()` |
| Associated TIFF tags | `slide.AssociatedTIFFTags(a)` | `a.TIFFTags()` |
| Associated IFD offset | `slide.AssociatedIFDOffset(a)` | `a.IFDOffset()` |
| Type() result | `string` | `AssociatedType` (string-underlying; comparisons unchanged, conversions needed for `string` storage) |
| MPP | `level.MPP SizeMm` / `md.MicronsPerPixel` | `level.MPP MPP` / `md.MPP MPP` |
| Raw tile | `slide.RawTile(level, tx, ty)` | `slide.Level(level).Tile(tx, ty)` |
| Decoded tile | `slide.DecodedTile(level, tx, ty, opts)` | `slide.Level(level).DecodedTile(tx, ty, opts)` |
| Region | `slide.ReadRegion(level, x, y, w, h, opts)` | `slide.Level(level).ReadRegion(Region{Origin:Point{x,y}, Size:Size{w,h}}, opts)` |
| Scaled region | `slide.ReadRegionScaled(...)` | `slide.Pyramid(0).ReadRegionScaled(srcRegion, outSize, opts)` |
| Multi-image reads | `slide.ImageRawTile(img, level, tx, ty)` etc. | `slide.Pyramid(img).Level(level).Tile(tx, ty)` etc. |
| Scaled strips | `slide.ScaledStrips(...)` | `slide.Pyramid(0).ScaledStrips(...)` |
| TIFF dirs | `opentile.TIFFDirectoriesOf(s)` | `s.TIFFDirectories()` |
| Tile position | `TilePos{X,Y}` (from RangeTiles) | `Point{X,Y}` |
```

- [ ] **Step 2: Commit**

```bash
git add docs/migrations/2026-06-12-v1-api-breaking-pass.md
git commit -m "docs(migration): v1.0 API breaking-pass migration note (consumer-facing)"
```

---

### Task 1: `opentile.Image` → `Pyramid`

**Files:**
- Modify: `image.go` (`type Image struct` → `type Pyramid struct`; doc), `slide.go` (`Images()`→`Pyramids()`, `Image(i)`→`Pyramid(i)`, the `[]Image` field/returns), every internal `opentile.Image` ref across `formats/*` and root.
- Test: `image_test.go`, `image_valuetype_test.go` (type name only at this stage).

Note: the `Image*`-prefixed read methods (`ImageRawTile`, …) keep their names in
this task — they are removed wholesale in Task 10. Renaming only the *type* + the
two *navigation* methods now keeps the diff reviewable.

- [ ] **Step 1: Adjust the value-type / image test names to the new type**

In `image_test.go` and `image_valuetype_test.go`, replace `opentile.Image` →
`opentile.Pyramid` and `Images()` → `Pyramids()`, `Image(i)` → `Pyramid(i)`.
Run to confirm they FAIL to compile (type not yet renamed):

Run: `go vet ./... 2>&1 | head`
Expected: compile errors referencing `Pyramid` undefined.

- [ ] **Step 2: Rename the type + navigation methods**

In `image.go`:
```go
// Pyramid identifies one multi-resolution image within a slide. Single-image
// formats carry a single Pyramid. OME-TIFF can carry multiple.
type Pyramid struct {
	Name   string
	Index  int
	Levels []Level
}
```
In `slide.go`, rename `func (s *Slide) Images() []Image` → `Pyramids() []Pyramid`,
`func (s *Slide) Image(i int)` (if present) → `Pyramid(i int)`, and the internal
`[]Image` storage type. Keep `Levels()`/`Level(i)` as-is.

- [ ] **Step 3: Sweep remaining `opentile.Image` references**

```bash
grep -rln "opentile\.Image\b\|\.Images()\b" --include="*.go" . | grep -v "decoder/"
# update each (formats/*, internal bridges, root) Image -> Pyramid, Images() -> Pyramids()
# CAUTION: do NOT touch decoder.Image (different type) or the Image*-prefixed
# read method names (ImageRawTile etc.) — those are handled in Task 10.
```
Audit: `grep -rn "opentile\.Image\b" --include="*.go" . | grep -v decoder` → empty.

- [ ] **Step 4: Build + test**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor(api): rename opentile.Image -> Pyramid; Images()/Image(i) -> Pyramids()/Pyramid(i)"
```

---

### Task 2: `Slide.Associated()` → `AssociatedImages()`

**Files:**
- Modify: `slide.go` (method), all root + `formats/*` + `bench/` + `tests/` call sites.

- [ ] **Step 1: Rename the method**

In `slide.go`: `func (s *Slide) Associated() []AssociatedImage` → `AssociatedImages()`.

- [ ] **Step 2: Sweep call sites**

```bash
grep -rln "\.Associated()" --include="*.go" .   # update each to .AssociatedImages()
```
Audit: `grep -rn "\.Associated()" --include="*.go" .` → empty (only `AssociatedImages()` remains).

- [ ] **Step 3: Build + test + commit**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1`
```bash
git add -A && git commit -m "refactor(api): Slide.Associated() -> AssociatedImages()"
```

---

### Task 3: Typed `AssociatedType` return

**Files:**
- Modify: `image.go` (constants → typed; interface method return type), every format `Type()` impl, every `Type()` consumer comparing to `string`.
- Test: `associated_decode_test.go`, `tests/parity/*` (compare against typed consts).

- [ ] **Step 1: Make the constants typed**

In `image.go`:
```go
type AssociatedType string

const (
	AssociatedLabel       AssociatedType = "label"
	AssociatedOverview    AssociatedType = "overview"
	AssociatedThumbnail   AssociatedType = "thumbnail"
	AssociatedMap         AssociatedType = "map"
	AssociatedProbability AssociatedType = "probability"
	AssociatedMacro       AssociatedType = "macro"
	AssociatedGeneric     AssociatedType = "associated"
)
```
And change the interface: `Type() AssociatedType`.

- [ ] **Step 2: Update every format `Type()` impl**

Each `func (a *…) Type() string { return "label" }` → `Type() AssociatedType { return opentile.AssociatedLabel }` (or the matching const). The string literals already match the const values, so `return AssociatedType("label")` is also valid where a package can't import root; format packages already import `opentile`, so use the consts.

```bash
grep -rln "func.*Type() string" --include="*.go" formats/   # 11 packages
```

- [ ] **Step 3: Fix consumers that stored `Type()` as `string`**

```bash
go build ./... 2>&1 | grep "cannot use"   # find string-typed assignment sites
# At each: convert with string(a.Type()) where a plain string is genuinely needed
# (e.g. map keys, fmt into %s is fine without conversion).
```

- [ ] **Step 4: Build + full test + commit**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -count=1` (skip -race here for speed; -race in the final gate)
```bash
git add -A && git commit -m "refactor(api): AssociatedImage.Type() returns typed AssociatedType"
```

---

### Task 4: MPP-type unification

**Files:**
- Modify: `geometry.go` (new `MPP` type), `image.go` (`Level.MPP`), `metadata.go` (`Metadata` MPP fields + `SetMPPSymmetric`), every reader populating MPP, `SizeMm` usages.

- [ ] **Step 1: Add the `MPP` type**

In `geometry.go`:
```go
// MPP is microns-per-pixel, per axis. Zero value means "unknown".
type MPP struct{ X, Y float64 }

func (m MPP) IsZero() bool { return m.X == 0 && m.Y == 0 }
// Symmetric reports the single value when X==Y (and non-zero), else 0.
func (m MPP) Symmetric() float64 { if m.X == m.Y { return m.X }; return 0 }
```

- [ ] **Step 2: Switch `Level.MPP` and `Metadata`**

`image.go`: `MPP SizeMm` → `MPP MPP` (microns, not mm). `metadata.go`: replace
`MicronsPerPixel`/`MicronsPerPixelX`/`MicronsPerPixelY float64` with `MPP MPP`;
update `SetMPPSymmetric` accordingly (or drop it for `MPP.Symmetric()`).

- [ ] **Step 3: Update every reader's MPP population**

```bash
grep -rln "MicronsPerPixel\|\.MPP\b\|SizeMm{" --include="*.go" formats/ .
# Each reader currently sets SizeMm{W,H} (mm) on Level.MPP and the
# MicronsPerPixel* fields on Metadata. Convert: Level.MPP = MPP{X: µmX, Y: µmY}
# (note the mm->µm unit change: old SizeMm held mm, new MPP holds microns —
# verify each reader's source units against the format docs before scaling).
```

- [ ] **Step 4: Build + test + commit**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1`
Verify `TestCrossFormatMetadata` (in `metadata_test.go`) and per-format MPP assertions still pass with the new type/units.
```bash
git add -A && git commit -m "refactor(api): unify MPP into a microns MPP type (Level.MPP, Metadata.MPP)"
```

---

### Task 5: Geometry unification

**Files:**
- Modify: `image.go` (delete `TilePos`), `slide.go`/`strips.go` (`RangeTiles` yields `Point`; `ScaledStrips`/`TileOverlap` use our types), `region.go`/`region_scaled.go` (`ReadRegion` takes `Region`), call sites.

- [ ] **Step 1: Delete `TilePos`, yield `Point` from RangeTiles**

Remove `type TilePos struct{ X, Y int }` from `image.go`. Change
`RangeTiles`/`ImageRangeTiles` return type `iter.Seq2[TilePos, TileResult]` →
`iter.Seq2[Point, TileResult]`. Update call sites (`grep -rn "TilePos"`).

- [ ] **Step 2: `Level.TileOverlap` and `ScaledStrips` drop stdlib geometry**

`Level.TileOverlap image.Point` → `Point`. `ScaledStrips(l0Rect image.Rectangle,
outSize image.Point, …)` → `(l0Rect Region, outSize Size, …)`. Update the
`StripIterator` internals that consumed `image.Rectangle`/`image.Point`.

- [ ] **Step 3: `ReadRegion` family takes `Region`**

`ReadRegion(level, x, y, w, h int, …)` → `ReadRegion(level int, r Region, …)`;
`ReadRegionInto(level, x, y int, dst, …)` → `(level int, origin Point, dst, …)`.
(These methods move to `*Level` in Task 8 — keeping the `level int` param now
is fine; Task 8 drops it.) Update `region_scaled.go` similarly with `Region`/`Size`.

- [ ] **Step 4: Build + test + commit**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1`
```bash
git add -A && git commit -m "refactor(api): unify geometry (delete TilePos, Region in region API, drop stdlib image.* from signatures)"
```

---

### Task 6: `TIFFDirectoriesOf` → `Slide.TIFFDirectories()`

**Files:**
- Modify: `tifftags.go` (function → method), call sites in `tests/`, `bench/`, docs.

- [ ] **Step 1: Convert to a method**

`func TIFFDirectoriesOf(s *Slide) ([]TIFFDirectory, bool)` →
`func (s *Slide) TIFFDirectories() ([]TIFFDirectory, bool)` (same body, `s` is the receiver).

- [ ] **Step 2: Sweep + build + test + commit**

```bash
grep -rln "TIFFDirectoriesOf" --include="*.go" .   # -> s.TIFFDirectories()
go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1
git add -A && git commit -m "refactor(api): TIFFDirectoriesOf(s) -> s.TIFFDirectories()"
```

---

## Phase 2 — Accessors onto the `AssociatedImage` interface

### Task 7: `Encoding()` / `TIFFTags()` / `IFDOffset()` on the interface

**Files:**
- Modify: `image.go` (interface), `associated_encoding.go` / `tifftags.go` / `associated_ifd_offset*.go` (remove the `*Slide` methods; add per-impl), every format associated type, `associated_encoding_test.go`, `associated_ifd_offset_test.go`, `tifftags_test.go`.

- [ ] **Step 1: Write the failing interface-conformance test**

In `image_test.go` add:
```go
func TestAssociatedImageHasEncodingAccessors(t *testing.T) {
	var _ interface {
		Encoding() (AssociatedEncoding, bool)
		TIFFTags() (TIFFTags, bool)
		IFDOffset() (int64, bool)
	} = AssociatedImage(nil)
}
```
Run: `go test . -run TestAssociatedImageHasEncodingAccessors`
Expected: FAIL (interface lacks the methods).

- [ ] **Step 2: Add the three methods to the interface**

In `image.go` `AssociatedImage`:
```go
	Encoding() (AssociatedEncoding, bool)
	TIFFTags() (TIFFTags, bool)
	IFDOffset() (int64, bool)
```

- [ ] **Step 3: Implement on every format associated type**

For each `formats/<f>/associated.go` (and `ndpi/mappage.go`):
- TIFF-backed types: `Encoding()` returns what the package's existing
  `AssociatedEncoding()` method returns; `TIFFTags()` returns the IFD's
  `TIFFTagsFromPage(page)`; `IFDOffset()` returns the source IFD byte offset (for
  the SVS/generic-TIFF providers that have it) else `(0, false)`.
- Non-TIFF / synthesized (DICOM frames, SZI, IFE, NDPI synthesized label,
  Leica/NDPI self-contained overviews): return `(_, false)` for the ones that
  have no faithful form. (Many already have `AssociatedEncoding()` — rename it to
  `Encoding()` and add the two stubs.)

The dispatch that lived in `Slide.AssociatedEncoding/AssociatedTIFFTags/AssociatedIFDOffset`
(the `UnwrapReader`-chain type assertions) moves INTO each impl, which already
holds its reader — so the per-impl methods read directly.

- [ ] **Step 4: Remove the three `*Slide` methods**

Delete `func (s *Slide) AssociatedEncoding(a AssociatedImage)`,
`AssociatedTIFFTags(a)`, `AssociatedIFDOffset(a)` and the now-unused
`associatedEncoder` interface in `associated_encoding.go`.

- [ ] **Step 5: Update tests + call sites**

`associated_encoding_test.go`: `s.AssociatedEncoding(a)` → `a.Encoding()`.
`associated_ifd_offset_test.go`: `s.AssociatedIFDOffset(a)` → `a.IFDOffset()`.
`tifftags_test.go`: `s.AssociatedTIFFTags(a)` → `a.TIFFTags()`. (`LevelTIFFTags`
stays on Slide — it's keyed by level index, not an associated image.)

- [ ] **Step 6: Build + full test + commit**

Run: `go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -count=1`
```bash
git add -A && git commit -m "refactor(api): move Encoding()/TIFFTags()/IFDOffset() onto the AssociatedImage interface"
```

---

## Phase 3 — Receiver-method restructure

### Task 8: `*Level` gains a back-reference + per-level read methods

**Files:**
- Modify: `image.go` (`Level` gains unexported back-ref + pyramid/level index; navigation returns `*Level`), new `level_reads.go` (Level read methods delegating to existing internal paths), `slide.go` (`Levels() []*Level`, `Level(i) (*Level, error)`), `image_valuetype_test.go` (Level is no longer a bare value — update/retire the value-semantics assertions for `Level`).
- Test: new `level_reads_test.go`.

- [ ] **Step 1: Write the failing test for `Level.Tile`/`DecodedTile`**

`level_reads_test.go`:
```go
func TestLevelReadMethods(t *testing.T) {
	s := openTestSlide(t) // helper: OpenFile(CMU-1-Small-Region.svs)
	defer s.Close()
	l, err := s.Level(0)
	if err != nil { t.Fatal(err) }
	raw, err := l.Tile(0, 0)
	if err != nil { t.Fatal(err) }
	old, _ := s.legacyRawTileForTest(0, 0, 0) // temporary bridge via export_test.go
	if !bytes.Equal(raw, old) { t.Fatal("Level.Tile != legacy RawTile") }
}
```
Run: expected FAIL (`*Level` has no `Tile`).

- [ ] **Step 2: Give `Level` a back-ref and make navigation return `*Level`**

In `image.go`:
```go
type Level struct {
	Index        int
	PyramidIndex int
	Size         Size
	TileSize     Size
	Grid         Size
	TileOverlap  Point
	Compression  Compression
	MPP          MPP
	FocalPlane   float64
	Downsample   float64

	slide *Slide // back-ref, set at Open; immutable thereafter
}
```
In `slide.go`, the internal pyramid build sets `lvl.slide = s` for every level.
`Levels() []*Level` and `Level(i) (*Level, error)` return pointers into the
stored slice.

- [ ] **Step 3: Add the per-level read methods (delegate to existing internals)**

New `level_reads.go` — each method routes into the SAME internal read path the
old Slide methods used (so the v0.27–v0.30 fast paths are preserved):
```go
func (l *Level) Tile(tx, ty int) ([]byte, error) {
	return l.slide.imageRawTile(l.PyramidIndex, l.Index, tx, ty)
}
func (l *Level) TileInto(tx, ty int, dst []byte) (int, error) { … }
func (l *Level) TileReader(tx, ty int) (io.ReadCloser, error) { … }
func (l *Level) TileMaxSize() int { … }
func (l *Level) TilePrefix() []byte { … }
func (l *Level) TileBodyInto(tx, ty int, dst []byte) (int, error) { … }
func (l *Level) DecodedTile(tx, ty int, opts ...DecodeOption) (*decoder.Image, error) { … }
func (l *Level) DecodedTileInto(tx, ty int, dst *decoder.Image, opts ...DecodeOption) error { … }
func (l *Level) ReadRegion(r Region, opts ...DecodeOption) (*decoder.Image, error) { … }
func (l *Level) ReadRegionInto(origin Point, dst *decoder.Image, opts ...DecodeOption) error { … }
func (l *Level) Tiles(ctx context.Context) iter.Seq2[Point, TileResult] { … }
func (l *Level) Warm() error { … }
```
Where `imageRawTile`/`imageDecodedTile`/… are the existing Slide method bodies
renamed to unexported helpers taking `(pyramidIdx, levelIdx, …)`. (Keep the
helpers; Task 10 deletes only the EXPORTED Slide read methods.)

- [ ] **Step 4: Update the value-type test**

`image_valuetype_test.go`: the `Level` value-copy assertions no longer hold
(it has an unexported pointer). Replace with an assertion that `*Level` from
`s.Level(i)` is stable (`s.Level(0) == s.Level(0)` by pointer) and that the
metadata fields are still read-only. Keep any `Pyramid`/`Size`/`Point` value
assertions that remain valid.

- [ ] **Step 5: Run the test, then the suite**

Run: `go test . -run TestLevelReadMethods` → PASS, then
`go build ./... && OPENTILE_TESTDIR="$PWD/sample_files" go test . ./formats/... -count=1`.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(api): *Level back-ref + per-level read methods (Tile/DecodedTile/ReadRegion/Tiles/…)"
```

---

### Task 9: `*Pyramid` gains cross-level read methods

**Files:**
- Modify: `image.go`/new `pyramid_reads.go` (`Pyramid` gains back-ref + methods), `slide.go` (`Pyramid(i) *Pyramid`, `Pyramids() []*Pyramid`), `region_scaled.go`/`strips.go` internals.
- Test: `pyramid_reads_test.go`.

- [ ] **Step 1: Failing test for `Pyramid.ReadRegionScaled` / `Level(i)`**

```go
func TestPyramidReadMethods(t *testing.T) {
	s := openTestSlide(t); defer s.Close()
	p := s.Pyramid(0)
	if p.Level(0) != s.Level0ForTest() { t.Fatal("Pyramid.Level(0) identity") }
	img, err := p.ReadRegionScaled(Region{Size: Size{W: 64, H: 64}}, Size{W: 32, H: 32})
	if err != nil { t.Fatal(err) }
	if img.Width != 32 { t.Fatalf("got %d", img.Width) }
}
```
Run: expected FAIL.

- [ ] **Step 2: `Pyramid` back-ref + methods**

`Pyramid` gains an unexported `slide *Slide` (set at Open). New `pyramid_reads.go`:
```go
func (p *Pyramid) Levels() []*Level { … }                 // pointers into p.Levels
func (p *Pyramid) Level(i int) *Level { … }
func (p *Pyramid) BestLevelForDownsample(d float64) *Level { … } // returns *Level (was int)
func (p *Pyramid) ReadRegionScaled(src Region, out Size, opts ...DecodeOption) (*decoder.Image, error) { … }
func (p *Pyramid) ReadRegionScaledInto(src Region, dst *decoder.Image, opts ...DecodeOption) error { … }
func (p *Pyramid) ScaledStrips(src Region, out Size, stripHeight int, opts ...StripOption) *StripIterator { … }
```
Each delegates to the existing internal `imageReadRegionScaled` / `imageScaledStrips`
helpers (the bodies of the old `ImageReadRegionScaled*`/`ImageScaledStrips`).

Note the `Pyramid.Levels []Level` field vs `Levels() []*Level` method: keep the
value-slice field for inspection; the method returns pointers (`&p.Levels[i]`,
back-ref-populated). Document the distinction.

- [ ] **Step 3: Run test, then suite, then commit**

Run: `go test . -run TestPyramidReadMethods` → PASS; full `go test . ./formats/...`.
```bash
git add -A && git commit -m "feat(api): *Pyramid cross-level reads (ReadRegionScaled/ScaledStrips/BestLevelForDownsample/Level)"
```

---

### Task 10: Remove the exported `*Slide` read methods + `Image*` twins

**Files:**
- Modify: `slide.go`, `decoded_tile.go`, `region.go`, `region_scaled.go`, `strips.go` (delete exported read methods + `Image*` twins; keep the unexported helpers from Tasks 8–9 and the navigation `Pyramid(s)`/`Level(s)`/`Metadata`/`ICCProfile`/`Format`/`Close`/`TIFFDirectories`/`LevelTIFFTags`).
- Modify: `bench/`, `tests/`, `tests/parity/`, any root `*_test.go` still calling the removed methods.

- [ ] **Step 1: Delete the exported Slide read surface**

Remove: `RawTile`, `RawTileInto`, `TileReader`, `TileMaxSize`, `TilePrefix`,
`TileBodyInto`, `TileBodyMaxSize`, `WarmLevel`, `DecodedTile`, `DecodedTileInto`,
`ReadRegion`, `ReadRegionInto`, `ReadRegionScaled`, `ReadRegionScaledInto`,
`ScaledStrips`, `RangeTiles`, `BestLevelForDownsample`, and EVERY `Image*` twin
(`ImageRawTile`, `ImageDecodedTile`, `ImageReadRegion*`, `ImageScaledStrips`,
`ImageRangeTiles`, `ImageTile*`, `ImageWarmLevel`, `ImageBestLevelForDownsample`,
`ImageLevelTIFFTags`→keep? no: `LevelTIFFTags` stays on Slide; `ImageLevelTIFFTags`
becomes `pyramid.Level(l)`-anchored — move it to `*Level` as `Level.TIFFTags()`
mirroring the associated accessor). Keep the unexported helpers they delegated to.

- [ ] **Step 2: Move `ImageLevelTIFFTags`/`LevelTIFFTags` to `*Level`**

`Level.TIFFTags() (TIFFTags, bool)` (image-0 `s.LevelTIFFTags(l)` becomes
`s.Level(l).TIFFTags()`). Remove the Slide forms.

- [ ] **Step 3: Update every internal call site**

```bash
grep -rln "\.RawTile(\|\.DecodedTile(\|\.ReadRegion(\|\.ScaledStrips(\|\.ImageRawTile(\|\.ImageReadRegion\|\.RangeTiles(\|\.BestLevelForDownsample(\|\.LevelTIFFTags(" --include="*.go" bench tests . | sort -u
# rewrite each: s.RawTile(l,x,y) -> s.Level(l).Tile(x,y);
#   s.ImageRawTile(i,l,x,y) -> s.Pyramid(i).Level(l).Tile(x,y);
#   s.ReadRegionScaled(...) -> s.Pyramid(0).ReadRegionScaled(Region{...}, Size{...}); etc.
```

- [ ] **Step 4: Build + full `-race` test + bench gates**

Run:
```bash
go build ./...
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
make bench-ndpi   # >=270
make bench-svs    # >=475
make bench-ndpi-mem
```
Expected: all green; bench numbers within gate (perf-neutral per design §2.1). If a
bench regresses, an accidental escape or broken fast-path dispatch was introduced —
fix before proceeding (do NOT re-pin the gate).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor(api)!: remove Slide read methods + Image* twins (reads now on *Level/*Pyramid)"
```

---

### Task 11: Docs, CHANGELOG, finalize migration note

**Files:**
- Modify: `README.md` (all read examples → receiver form), `CHANGELOG.md`, `docs/migrations/2026-06-12-v1-api-breaking-pass.md` (fill final signatures), CLAUDE.md milestone header.

- [ ] **Step 1: Rewrite README read examples** to `s.Level(0).Tile(...)` /
  `s.Pyramid(0).ReadRegionScaled(...)` / `a.Encoding()` forms; update the
  associated-images section and the § "three faces" wording.

- [ ] **Step 2: CHANGELOG `[Unreleased]`** — a single "Changed (BREAKING)" block
  enumerating every rename/move with the migration-note link.

- [ ] **Step 3: Verify the migration note** matches the shipped signatures exactly
  (diff against `go doc .`).

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: v1.0 breaking-pass README/CHANGELOG/migration finalization"
```

---

## Completion

After all tasks: announce and use **superpowers:finishing-a-development-branch**.
Tests must be green under `-race` and all three bench gates within range. Do NOT
merge to `main` until wsitools and openscope have acknowledged the migration note
(per the consumer-compat invariant). The version bump is a **minor** (pre-1.0
breaking) — propose `v0.41.0` at finish.

Deferred (NOT in this plan, fixture-gated): `decoder.Image` `Bands`/`Sample`,
`TileAt`/`DecodedTileAt`/`DecodedTileAtInto`, `Pyramid.SizeZ/C/T`/`Channel`/
`FocalPlane`, `ReadRegion…At(r, Plane)` — each lands per-axis (C → Z → T) with its
first real fixture.

---

## Self-Review

- **Spec coverage:** §9 decisions 1–5 → Tasks 8–10 (receiver, decision 1),
  Task 1 (`Pyramid`, decision 2), Task 7 keeps `AssociatedEncoding` raw-int fields
  untouched (decision 3 — no change needed), decisions 4 (`Bands`/`Sample`) and 5
  (`ReadRegion…At`) are explicitly deferred. §8 coordinated-pass rows → Tasks 1–10.
  Free-now items already shipped (v0.40.0) — not repeated.
- **Placeholder scan:** every step has an exact file, signature, or command. The
  mechanical-rename steps specify the `grep` audit + build/test gate rather than
  listing every call site (the sweep is deterministic).
- **Type consistency:** `Pyramid` (Task 1) used consistently in Tasks 8–10;
  `MPP` (Task 4) used in `Level`/`Metadata`; `AssociatedType` (Task 3) used in the
  interface + Task 7; `*Level`/`*Pyramid` return types consistent Tasks 8–10.
- **Ordering:** renames (build-green) → interface accessors → receiver restructure
  (the only steps that delete exported surface are Tasks 7 and 10, late). Bench
  gates only meaningful after Task 10, where they run.
