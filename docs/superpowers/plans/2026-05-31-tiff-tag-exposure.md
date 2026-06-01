# Raw TIFF Tag Exposure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose raw, typed TIFF tags to consumers — anchored to semantic levels/associated images — starting with the substrate plus the SVS reader (a complete, shippable vertical slice).

**Architecture:** A lazy type-assertion provider (the existing `MetadataOf`/`decodedTiler` pattern). `internal/tiff.Page` gains a generic `RawTags()` enumerator; public typed `TIFFTag`/`TIFFTags`/`TIFFDirectory` live in the `opentile` root; TIFF readers implement an exported `TIFFDirectories()` method; `Slide` accessors walk the `UnwrapReader` chain to reach it. Non-TIFF formats simply don't implement it → `ok=false`.

**Tech Stack:** Go 1.23+, `internal/tiff` (Entry/Page/ifd), the `opentile` root package, `formats/svs`.

**Spec:** `docs/superpowers/specs/2026-05-31-tiff-tag-exposure-design.md`

**Scope of THIS plan:** substrate + **SVS** reader. The other 7 TIFF formats (NDPI, Philips, OME, BIF, generic-TIFF, Leica-SCN, COG-WSI) follow the SVS pattern in a follow-up plan. IFE/SZI are non-TIFF (out of scope, `ok=false`).

**Verified facts for the implementer:**
- `internal/tiff.Entry` fields: `Tag uint16`, `Type DataType`, `Count uint64`, `valueBytes [8]byte` (inline cell), `valueOrOffset uint64`, `inlineCap int`. Methods: `Values(b)`, `Values64(b) ([]uint64,error)`, private `decodeASCII(b,cell)`, private `decodeRational(b) ([][2]uint32,error)`, `fitsInline()`. `DataType` has `.Size() int`; constants `DTByte=1, DTASCII=2, DTShort=3, DTLong=4, DTRational=5, DTUndefined=7, DTSShort=8, DTSLong=9, DTSRational=10` (`internal/tiff/tag.go`).
- `internal/tiff.Page{ ifd *ifd; br *byteReader }`; `ifd.entries map[uint16]Entry`; `ifd.get(tag)`. `byteReader` has `order binary.ByteOrder` and `bytes(off int64, n int) ([]byte,error)`.
- `internal/tiff.File.Pages() []*tiff.Page`, `File.ReaderAt() io.ReaderAt`.
- `Slide.UnwrapReader() any` returns `s.r`; `fileCloser`/`mmapCloser` also expose `UnwrapReader() any` (`opentile.go`, `slide.go`).
- `opentile.AssociatedImage` has `Type() string` (e.g. "label", "macro", "thumbnail", "overview").
- SVS reader: `formats/svs/svs.go` `openFromTIFFFile(file *tiff.File, cfg)` builds `pages := file.Pages()`, classifies them, constructs levels via `newTiledImage(levelIdx, pages[pageIdx], ...)` and associated via `newAssociatedImage(kind, pages[spec.pageIdx], ...)`. The SVS `tiler` is the `format.Reader`.
- The `ld: warning: ignoring duplicate libraries` linker line is benign throughout this repo.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/tiff/tag.go` (MODIFY) | Add `Entry.RawBytes(b)`; add exported `RawTag` struct. |
| `internal/tiff/page.go` (MODIFY) | Add `Page.RawTags() []RawTag` (deterministic, tag-sorted). |
| `tiff_tags.go` (pkg `opentile`, NEW) | Public types (`TIFFType`+consts, `Rational`, `TIFFTag`+getters, `TIFFTags`+lookup, `TIFFDirectory`, `DirectoryKind`); `tiffTagsFrom` translator; name dictionary; pixel-pointer denylist; `tiffTagProvider` interface; `Slide` accessors; `TIFFDirectoriesOf`. |
| `formats/svs/svs.go` (MODIFY) | SVS `tiler` retains `file` + a page→role mapping; implements `TIFFDirectories()`. |

---

## Task 1: internal/tiff — raw tag enumeration

**Files:**
- Modify: `internal/tiff/tag.go`
- Modify: `internal/tiff/page.go`
- Test: `internal/tiff/rawtags_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/tiff/rawtags_test.go`:

```go
package tiff

import "testing"

// buildSyntheticPage constructs a Page with a couple of inline entries
// for RawTags testing, bypassing file I/O.
func buildSyntheticPage() *Page {
	// ImageWidth (256) = SHORT 1, value 512; a fake ASCII tag.
	entries := map[uint16]Entry{
		256: {Tag: 256, Type: DTShort, Count: 1, valueOrOffset: 512, inlineCap: 4},
		270: asciiInlineEntry(270, "hi"),
	}
	br := &byteReader{order: leOrder()}
	return &Page{ifd: &ifd{entries: entries}, br: br}
}

func TestRawTagsEnumeratesSorted(t *testing.T) {
	p := buildSyntheticPage()
	raw := p.RawTags()
	if len(raw) != 2 {
		t.Fatalf("RawTags len = %d, want 2", len(raw))
	}
	// Deterministic ascending tag order.
	if raw[0].Number != 256 || raw[1].Number != 270 {
		t.Fatalf("not tag-sorted: %d, %d", raw[0].Number, raw[1].Number)
	}
	if raw[0].Type != DTShort || len(raw[0].Uints) != 1 || raw[0].Uints[0] != 512 {
		t.Fatalf("tag 256 decode wrong: %+v", raw[0])
	}
	if raw[1].ASCII != "hi" {
		t.Fatalf("tag 270 ASCII = %q, want hi", raw[1].ASCII)
	}
}
```

NOTE: you must supply the two test helpers `asciiInlineEntry(tag, s)` and `leOrder()` in the test file — implement them using the package internals: `leOrder()` returns `binary.LittleEndian`; `asciiInlineEntry` builds an `Entry{Tag: tag, Type: DTASCII, Count: uint64(len(s)+1), inlineCap: 4}` with `valueBytes` set to the NUL-terminated string bytes (fits inline when len(s)+1 <= 4; use a short string like "hi"). Add `import "encoding/binary"` to the test.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tiff/ -run TestRawTags -count=1`
Expected: FAIL — `RawTag` / `RawTags` / `RawBytes` undefined.

- [ ] **Step 3: Add `RawTag` + `Entry.RawBytes` to `internal/tiff/tag.go`**

Append to `internal/tiff/tag.go`:

```go
// RawTag is a decoded TIFF tag exposed to the opentile root package so it
// can build the public opentile.TIFFTag. Internal package only.
type RawTag struct {
	Number    uint16
	Type      DataType
	Count     int
	Raw       []byte      // verbatim payload bytes, file byte order
	ASCII     string      // set when Type==DTASCII
	Uints     []uint64    // BYTE/SHORT/LONG/LONG8 (best-effort)
	Rationals [][2]uint32 // num/denom pairs when Type==DTRational
}

// RawBytes returns the verbatim value payload for the entry (the bytes a
// re-encoder would re-emit), in file byte order.
func (e Entry) RawBytes(b *byteReader) ([]byte, error) {
	need := int64(e.Count) * int64(e.Type.Size())
	if need <= 0 {
		return nil, nil
	}
	if e.fitsInline() {
		cap := e.inlineCap
		if cap == 0 {
			cap = 4
		}
		if need > int64(cap) {
			need = int64(cap)
		}
		return append([]byte(nil), e.valueBytes[:need]...), nil
	}
	if need > int64(^uint(0)>>1) {
		return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, need)
	}
	return b.bytes(int64(e.valueOrOffset), int(need))
}

// decode populates a RawTag from the entry using the page's byteReader.
// Best-effort: a field that doesn't apply to the type is left zero.
func (e Entry) decode(b *byteReader) RawTag {
	rt := RawTag{Number: e.Tag, Type: e.Type, Count: int(e.Count)}
	rt.Raw, _ = e.RawBytes(b)
	switch e.Type {
	case DTASCII:
		rt.ASCII, _ = e.decodeASCII(b, e.valueBytes[:])
	case DTRational, DTSRational:
		rt.Rationals, _ = e.decodeRational(b)
	default:
		rt.Uints, _ = e.Values64(b)
	}
	return rt
}
```

- [ ] **Step 4: Add `Page.RawTags()` to `internal/tiff/page.go`**

Append to `internal/tiff/page.go` (add `"sort"` to its imports):

```go
// RawTags enumerates every tag in this page's IFD, decoded, in ascending
// tag-number order (deterministic). Used by the opentile root package to
// build the public TIFF-tag API.
func (p *Page) RawTags() []RawTag {
	nums := make([]uint16, 0, len(p.ifd.entries))
	for n := range p.ifd.entries {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	out := make([]RawTag, 0, len(nums))
	for _, n := range nums {
		out = append(out, p.ifd.entries[n].decode(p.br))
	}
	return out
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/tiff/ -run TestRawTags -race -count=1`
Expected: PASS.

- [ ] **Step 6: Confirm the whole tiff package still builds + tests**

Run: `go test ./internal/tiff/ -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL)"`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/tiff/tag.go internal/tiff/page.go internal/tiff/rawtags_test.go
git commit -m "tiff: add RawTag + Entry.RawBytes + Page.RawTags enumerator"
```

End every commit in this plan with:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

---

## Task 2: opentile public tag types + getters + lookup

**Files:**
- Create: `tiff_tags.go`
- Test: `tiff_tags_test.go`

- [ ] **Step 1: Write the failing test**

Create `tiff_tags_test.go`:

```go
package opentile

import "testing"

func TestTIFFTagGetters(t *testing.T) {
	ascii := TIFFTag{Number: 270, Type: TIFFASCII, Count: 3, ascii: "abc"}
	if s, ok := ascii.ASCII(); !ok || s != "abc" {
		t.Fatalf("ASCII() = %q,%v", s, ok)
	}
	if _, ok := ascii.Uints(); ok {
		t.Fatalf("Uints() should be false for ASCII tag")
	}
	u := TIFFTag{Number: 256, Type: TIFFShort, Count: 1, uints: []uint64{512}}
	if v, ok := u.Uints(); !ok || len(v) != 1 || v[0] != 512 {
		t.Fatalf("Uints() = %v,%v", v, ok)
	}
	r := TIFFTag{Number: 282, Type: TIFFRational, Count: 1, rationals: []Rational{{Num: 40, Denom: 1}}}
	if v, ok := r.Rationals(); !ok || v[0].Num != 40 {
		t.Fatalf("Rationals() = %v,%v", v, ok)
	}
}

func TestTIFFTagsLookup(t *testing.T) {
	ts := TIFFTags{
		{Number: 256, Name: "ImageWidth", Type: TIFFShort},
		{Number: 270, Name: "ImageDescription", Type: TIFFASCII},
	}
	if tag, ok := ts.Tag(270); !ok || tag.Name != "ImageDescription" {
		t.Fatalf("Tag(270) = %+v,%v", tag, ok)
	}
	if _, ok := ts.Tag(999); ok {
		t.Fatalf("Tag(999) should be false")
	}
	if tag, ok := ts.ByName("ImageWidth"); !ok || tag.Number != 256 {
		t.Fatalf("ByName = %+v,%v", tag, ok)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test . -run 'TestTIFFTag' -count=1`
Expected: FAIL — `TIFFTag` / `TIFFTags` / `Rational` / `TIFFASCII` undefined.

- [ ] **Step 3: Create `tiff_tags.go` with the public types**

Create `tiff_tags.go`:

```go
package opentile

// TIFFType mirrors the TIFF field type. Named so consumers can interpret
// TIFFTag.Type without a magic-number table.
type TIFFType uint16

const (
	TIFFByte      TIFFType = 1
	TIFFASCII     TIFFType = 2
	TIFFShort     TIFFType = 3
	TIFFLong      TIFFType = 4
	TIFFRational  TIFFType = 5
	TIFFUndefined TIFFType = 7
	TIFFSShort    TIFFType = 8
	TIFFSLong     TIFFType = 9
	TIFFSRational TIFFType = 10
	TIFFLong8     TIFFType = 16
)

// Rational is an unsigned TIFF RATIONAL value.
type Rational struct{ Num, Denom uint32 }

// TIFFTag is one parsed TIFF tag, typed. Number is always set (the key for
// vendor/private tags); Name is "" when not in the known-tag dictionary.
// Raw is the verbatim payload (file byte order) for faithful re-encode.
type TIFFTag struct {
	Number uint16
	Name   string
	Type   TIFFType
	Count  int
	Raw    []byte

	// decoded forms (populated by the translator; exposed via getters)
	ascii     string
	uints     []uint64
	rationals []Rational
}

// ASCII returns the string value, ok=false unless Type==TIFFASCII.
func (t TIFFTag) ASCII() (string, bool) {
	if t.Type != TIFFASCII {
		return "", false
	}
	return t.ascii, true
}

// Uints returns unsigned integer values, ok=false unless the type is an
// unsigned integer type.
func (t TIFFTag) Uints() ([]uint64, bool) {
	switch t.Type {
	case TIFFByte, TIFFShort, TIFFLong, TIFFLong8:
		return t.uints, true
	}
	return nil, false
}

// Rationals returns rational values, ok=false unless Type==TIFFRational.
func (t TIFFTag) Rationals() ([]Rational, bool) {
	if t.Type != TIFFRational {
		return nil, false
	}
	return t.rationals, true
}

// TIFFTags is the set of tags on one IFD with lookup helpers.
type TIFFTags []TIFFTag

// Tag returns the tag with the given number, ok=false if absent.
func (ts TIFFTags) Tag(number uint16) (TIFFTag, bool) {
	for _, t := range ts {
		if t.Number == number {
			return t, true
		}
	}
	return TIFFTag{}, false
}

// ByName returns the tag with the given dictionary name, ok=false if absent.
func (ts TIFFTags) ByName(name string) (TIFFTag, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return TIFFTag{}, false
}

// DirectoryKind classifies a TIFF IFD's semantic role.
type DirectoryKind uint8

const (
	DirOther      DirectoryKind = iota // hidden / Map / SubIFD not surfaced elsewhere
	DirLevel                           // a pyramid level
	DirAssociated                      // an associated image
)

// TIFFDirectory is one IFD with structured identity. Image/Level are valid
// when Kind==DirLevel; Associated is the associated image Type() when
// Kind==DirAssociated.
type TIFFDirectory struct {
	Kind       DirectoryKind
	Image      int
	Level      int
	Associated string
	Tags       TIFFTags
}

// Tag is a convenience for d.Tags.Tag(number).
func (d TIFFDirectory) Tag(number uint16) (TIFFTag, bool) { return d.Tags.Tag(number) }
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test . -run 'TestTIFFTag' -count=1`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add tiff_tags.go tiff_tags_test.go
git commit -m "feat: public TIFFTag/TIFFTags/TIFFDirectory types + getters"
```

---

## Task 3: translator + name dictionary + pixel-pointer denylist

**Files:**
- Modify: `tiff_tags.go`
- Test: `tiff_tags_test.go`

- [ ] **Step 1: Write the failing test**

Add to `tiff_tags_test.go`:

```go
import "github.com/wsilabs/opentile-go/internal/tiff"

func TestTiffTagsFromTranslatesAndFilters(t *testing.T) {
	raw := []tiff.RawTag{
		{Number: 256, Type: tiff.DTShort, Count: 1, Uints: []uint64{512}},
		{Number: 270, Type: tiff.DTASCII, Count: 3, ASCII: "abc"},
		{Number: 273, Type: tiff.DTLong, Count: 9, Uints: []uint64{1, 2, 3}}, // StripOffsets — must be dropped
		{Number: 324, Type: tiff.DTLong, Count: 9, Uints: []uint64{4, 5}},    // TileOffsets — must be dropped
		{Number: 65420, Type: tiff.DTLong, Count: 1, Uints: []uint64{7}},     // vendor/private — kept, no name
	}
	ts := tiffTagsFrom(raw)
	if _, ok := ts.Tag(273); ok {
		t.Fatalf("StripOffsets (273) should be filtered")
	}
	if _, ok := ts.Tag(324); ok {
		t.Fatalf("TileOffsets (324) should be filtered")
	}
	w, ok := ts.Tag(256)
	if !ok || w.Name != "ImageWidth" || w.Type != TIFFShort {
		t.Fatalf("ImageWidth not translated: %+v %v", w, ok)
	}
	v, ok := ts.Tag(65420)
	if !ok || v.Name != "" {
		t.Fatalf("vendor tag 65420 should be kept with empty name: %+v %v", v, ok)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test . -run TestTiffTagsFrom -count=1`
Expected: FAIL — `tiffTagsFrom` undefined.

- [ ] **Step 3: Implement the translator in `tiff_tags.go`**

Add to `tiff_tags.go` (add the import `"github.com/wsilabs/opentile-go/internal/tiff"`):

```go
// pixelPointerTags are the bulk pixel-data-pointer tags excluded from the
// public API (regenerated on re-encode; not metadata): StripOffsets,
// StripByteCounts, TileOffsets, TileByteCounts.
var pixelPointerTags = map[uint16]bool{273: true, 279: true, 324: true, 325: true}

// tiffTagNames is a best-effort dictionary of well-known TIFF tag names.
// Number is always authoritative; unknown tags get Name == "".
var tiffTagNames = map[uint16]string{
	256: "ImageWidth", 257: "ImageLength", 258: "BitsPerSample",
	259: "Compression", 262: "PhotometricInterpretation", 270: "ImageDescription",
	271: "Make", 272: "Model", 274: "Orientation", 277: "SamplesPerPixel",
	282: "XResolution", 283: "YResolution", 284: "PlanarConfiguration",
	296: "ResolutionUnit", 305: "Software", 306: "DateTime",
	322: "TileWidth", 323: "TileLength", 339: "SampleFormat",
	34665: "ExifIFD", 34675: "ICCProfile",
}

// tiffTagsFrom translates internal raw tags to the public TIFFTags: maps
// types, applies the name dictionary, and drops the pixel-pointer denylist.
func tiffTagsFrom(raw []tiff.RawTag) TIFFTags {
	out := make(TIFFTags, 0, len(raw))
	for _, r := range raw {
		if pixelPointerTags[r.Number] {
			continue
		}
		t := TIFFTag{
			Number: r.Number,
			Name:   tiffTagNames[r.Number],
			Type:   TIFFType(r.Type),
			Count:  r.Count,
			Raw:    r.Raw,
			ascii:  r.ASCII,
			uints:  r.Uints,
		}
		for _, rr := range r.Rationals {
			t.rationals = append(t.rationals, Rational{Num: rr[0], Denom: rr[1]})
		}
		out = append(out, t)
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test . -run 'TestTiffTagsFrom|TestTIFFTag' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tiff_tags.go tiff_tags_test.go
git commit -m "feat: tiffTagsFrom translator + name dictionary + pixel-pointer filter"
```

---

## Task 4: provider interface + Slide accessors

**Files:**
- Modify: `tiff_tags.go`
- Test: `tiff_tags_test.go`

- [ ] **Step 1: Write the failing test (with a fake provider Slide)**

Add to `tiff_tags_test.go`:

```go
// fakeReader is a slideReader stub that also implements tiffTagProvider.
type fakeTagReader struct {
	slideReader
	dirs []TIFFDirectory
}

func (f fakeTagReader) TIFFDirectories() []TIFFDirectory { return f.dirs }

func TestSlideTIFFAccessors(t *testing.T) {
	s := &Slide{r: fakeTagReader{dirs: []TIFFDirectory{
		{Kind: DirLevel, Image: 0, Level: 0, Tags: TIFFTags{{Number: 270, Name: "ImageDescription", Type: TIFFASCII}}},
		{Kind: DirLevel, Image: 0, Level: 1, Tags: TIFFTags{{Number: 256, Name: "ImageWidth", Type: TIFFShort}}},
		{Kind: DirAssociated, Associated: "label", Tags: TIFFTags{{Number: 305, Name: "Software", Type: TIFFASCII}}},
		{Kind: DirOther, Tags: TIFFTags{{Number: 65500, Type: TIFFLong}}},
	}}}

	if tags, ok := s.LevelTIFFTags(0); !ok {
		t.Fatal("LevelTIFFTags(0) ok=false")
	} else if _, ok := tags.Tag(270); !ok {
		t.Fatal("level 0 missing tag 270")
	}
	if tags, ok := s.LevelTIFFTags(1); !ok || func() bool { _, ok := tags.Tag(256); return !ok }() {
		t.Fatal("level 1 missing tag 256")
	}
	if _, ok := s.LevelTIFFTags(9); ok {
		t.Fatal("out-of-range level should be ok=false")
	}
	all, ok := TIFFDirectoriesOf(s)
	if !ok || len(all) != 4 {
		t.Fatalf("TIFFDirectoriesOf = %d dirs, ok=%v", len(all), ok)
	}
}

func TestSlideTIFFAccessorsNonTIFF(t *testing.T) {
	s := &Slide{r: stubReader{}} // stubReader does NOT implement tiffTagProvider
	if _, ok := s.LevelTIFFTags(0); ok {
		t.Fatal("non-TIFF should return ok=false")
	}
	if _, ok := TIFFDirectoriesOf(s); ok {
		t.Fatal("non-TIFF TIFFDirectoriesOf should be ok=false")
	}
}
```

NOTE: `slideReader` is the unexported interface in `slide.go`. `fakeTagReader` embeds it (nil is fine — the test never calls its other methods). You must also provide a minimal `stubReader` that satisfies `slideReader` without implementing `TIFFDirectories` — if one already exists in the package's test files (search `stubReader`/`fakeReader` in `*_test.go`), reuse it; otherwise define a minimal stub embedding `slideReader`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test . -run TestSlideTIFFAccessors -count=1`
Expected: FAIL — `LevelTIFFTags` / `TIFFDirectoriesOf` / `tiffTagProvider` undefined.

- [ ] **Step 3: Implement the provider + accessors in `tiff_tags.go`**

Add to `tiff_tags.go`:

```go
// tiffTagProvider is implemented by TIFF-based format readers. The method
// is exported because readers live in other packages. Returns every IFD
// with structured identity; the Slide accessors derive views from it.
type tiffTagProvider interface {
	TIFFDirectories() []TIFFDirectory
}

// tiffProviderOf walks the UnwrapReader chain (like the MetadataOf helpers)
// looking for a reader that implements tiffTagProvider.
func tiffProviderOf(s *Slide) (tiffTagProvider, bool) {
	var cur any = s.r
	for cur != nil {
		if p, ok := cur.(tiffTagProvider); ok {
			return p, true
		}
		u, ok := cur.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		cur = u.UnwrapReader()
	}
	return nil, false
}

// TIFFDirectoriesOf enumerates every TIFF IFD (including orphan IFDs not
// surfaced as a level or associated image). ok=false for non-TIFF formats
// (IFE, SZI). The escape hatch for "dump all"; prefer LevelTIFFTags /
// AssociatedTIFFTags for everyday access.
func TIFFDirectoriesOf(s *Slide) ([]TIFFDirectory, bool) {
	p, ok := tiffProviderOf(s)
	if !ok {
		return nil, false
	}
	return p.TIFFDirectories(), true
}

// ImageLevelTIFFTags returns the TIFF tags of image's level's backing IFD,
// keyed exactly like ImageRawTile(image, level, ...). ok=false for non-TIFF
// formats or an out-of-range (image, level).
func (s *Slide) ImageLevelTIFFTags(image, level int) (TIFFTags, bool) {
	dirs, ok := TIFFDirectoriesOf(s)
	if !ok {
		return nil, false
	}
	for _, d := range dirs {
		if d.Kind == DirLevel && d.Image == image && d.Level == level {
			return d.Tags, true
		}
	}
	return nil, false
}

// LevelTIFFTags is the image-0 shortcut for ImageLevelTIFFTags.
func (s *Slide) LevelTIFFTags(level int) (TIFFTags, bool) {
	return s.ImageLevelTIFFTags(0, level)
}

// AssociatedTIFFTags returns the TIFF tags of an associated image's IFD,
// matched on a.Type(). ok=false for non-TIFF or if not found.
func (s *Slide) AssociatedTIFFTags(a AssociatedImage) (TIFFTags, bool) {
	dirs, ok := TIFFDirectoriesOf(s)
	if !ok {
		return nil, false
	}
	for _, d := range dirs {
		if d.Kind == DirAssociated && d.Associated == a.Type() {
			return d.Tags, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test . -run TestSlideTIFFAccessors -race -count=1`
Expected: PASS (both). If `stubReader` doesn't exist, the build error will say so — add the minimal stub per Step 1's note and re-run.

- [ ] **Step 5: Commit**

```bash
git add tiff_tags.go tiff_tags_test.go
git commit -m "feat: tiffTagProvider + Slide.LevelTIFFTags/AssociatedTIFFTags/TIFFDirectoriesOf"
```

---

## Task 5: SVS reader implements TIFFDirectories()

**Files:**
- Modify: `formats/svs/svs.go`
- Test: `formats/svs/tifftags_test.go` (create)

- [ ] **Step 1: Write the failing integration test**

Create `formats/svs/tifftags_test.go`:

```go
package svs_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func svsFixture(t *testing.T) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "sample_files")
	}
	p := filepath.Join(dir, "svs", "CMU-1.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

func TestSVSLevelTIFFTags(t *testing.T) {
	s, err := opentile.OpenFile(svsFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("LevelTIFFTags(0) ok=false on SVS")
	}
	// SVS level 0 carries ImageDescription (270) with the Aperio header.
	desc, ok := tags.Tag(270)
	if !ok {
		t.Fatal("level 0 missing ImageDescription (270)")
	}
	if v, ok := desc.ASCII(); !ok || len(v) == 0 {
		t.Fatalf("ImageDescription ASCII empty: %q %v", v, ok)
	}
	// Pixel-pointer tags must be filtered.
	if _, ok := tags.Tag(324); ok {
		t.Fatal("TileOffsets (324) should be filtered out")
	}
	// Full enumeration includes more than one directory.
	dirs, ok := opentile.TIFFDirectoriesOf(s)
	if !ok || len(dirs) == 0 {
		t.Fatalf("TIFFDirectoriesOf empty: %d %v", len(dirs), ok)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/svs/ -run TestSVSLevelTIFFTags -count=1`
Expected: FAIL — `LevelTIFFTags` returns ok=false because the SVS reader doesn't implement `TIFFDirectories()` yet.

- [ ] **Step 3: Read `openFromTIFFFile` and capture the page→role mapping**

Read `formats/svs/svs.go` `openFromTIFFFile`. It builds `pages := file.Pages()`, then a level-construction loop (`newTiledImage(levelIdx, pages[pageIdx], ...)`) and an associated-construction loop (`newAssociatedImage(spec.kind, pages[spec.pageIdx], ...)`). Add a field to the SVS `tiler` struct to retain what's needed for lazy tag exposure:

```go
// (add to the tiler struct)
file    *tiff.File       // retained for lazy TIFF-tag exposure
dirSpecs []svsDirSpec    // page->role mapping captured at Open
```

Add a small spec type near the tiler:

```go
type svsDirSpec struct {
	pageIdx int
	kind    opentile.DirectoryKind
	level   int    // valid when kind==DirLevel
	assoc   string // valid when kind==DirAssociated
}
```

In `openFromTIFFFile`, set `t.file = file` on the returned tiler, and populate `t.dirSpecs`:
- In the level loop, for each constructed level at `levelIdx` from `pages[pageIdx]`, append `svsDirSpec{pageIdx: pageIdx, kind: opentile.DirLevel, level: levelIdx}`.
- In the associated loop, for each `spec` from `pages[spec.pageIdx]` of kind `spec.kind`, append `svsDirSpec{pageIdx: spec.pageIdx, kind: opentile.DirAssociated, assoc: string(spec.kind)}` (use the same string the AssociatedImage's `Type()` returns — verify it matches, e.g. "label"/"macro"/"thumbnail").
- Any page index not covered by a level or associated spec → append `svsDirSpec{pageIdx: i, kind: opentile.DirOther}` so orphan IFDs are still enumerated.

(The exact variable names — `levelIdx`, `pageIdx`, `spec`, the tiler receiver — come from the existing code; adapt to them. Import `opentile` and `internal/tiff` if not already imported.)

- [ ] **Step 4: Implement `TIFFDirectories()` on the SVS tiler**

Add to `formats/svs/svs.go`:

```go
// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider.
func (t *tiler) TIFFDirectories() []opentile.TIFFDirectory {
	pages := t.file.Pages()
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.pageIdx < 0 || ds.pageIdx >= len(pages) {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Kind:       ds.kind,
			Image:      0, // SVS is single-image
			Level:      ds.level,
			Associated: ds.assoc,
			Tags:       opentile.TIFFTagsFromPage(pages[ds.pageIdx]),
		})
	}
	return out
}
```

This calls a NEW exported bridge `opentile.TIFFTagsFromPage(*tiff.Page) TIFFTags`. Add it to `tiff_tags.go` (it wraps the internal `RawTags()` + `tiffTagsFrom`, so format packages don't need to translate themselves):

```go
// TIFFTagsFromPage builds the public TIFFTags for a parsed TIFF page. For
// use by format readers implementing the TIFF-tag provider; not part of
// the consumer-facing surface.
func TIFFTagsFromPage(p *tiff.Page) TIFFTags {
	return tiffTagsFrom(p.RawTags())
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/svs/ -run TestSVSLevelTIFFTags -race -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL)|--- FAIL"`
Expected: `ok` (the SVS ImageDescription tag is exposed; pixel-pointer tags filtered; directories enumerated).

- [ ] **Step 6: Confirm SVS package + opentile package still green**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/svs/ . -race -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL)"`
Expected: both `ok`.

- [ ] **Step 7: Commit**

```bash
git add formats/svs/svs.go formats/svs/tifftags_test.go tiff_tags.go
git commit -m "feat(svs): expose raw TIFF tags via TIFFDirectories provider"
```

---

## Task 6: non-TIFF safety + full suite + docs

**Files:**
- Test: `tiff_tags_nontiff_test.go` (create)
- Modify: `README.md`

- [ ] **Step 1: Write a non-TIFF safety test**

Create `tiff_tags_nontiff_test.go`:

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

func TestNonTIFFReturnsFalse(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	szi := filepath.Join(dir, "szi", "CMU-1.szi")
	if _, err := os.Stat(szi); err != nil {
		t.Skipf("fixture missing: %s", szi)
	}
	s, err := opentile.OpenFile(szi)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.LevelTIFFTags(0); ok {
		t.Fatal("SZI (non-TIFF) LevelTIFFTags should be ok=false")
	}
	if _, ok := opentile.TIFFDirectoriesOf(s); ok {
		t.Fatal("SZI (non-TIFF) TIFFDirectoriesOf should be ok=false")
	}
}
```

- [ ] **Step 2: Run it**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run TestNonTIFFReturnsFalse -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL)|SKIP"`
Expected: `ok` (SZI returns ok=false, as it doesn't implement the provider).

- [ ] **Step 3: Full suite under race**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" make test 2>&1 | grep -vE "duplicate libraries" | grep -E "^FAIL" | head`
Expected: no FAIL lines (the whole module stays green; the additions are additive + lazy).

- [ ] **Step 4: Document in README**

Add a short subsection to `README.md` under "Format-specific metadata" (after the `MetadataOf` block) documenting the new API:

```markdown
### Raw TIFF tags

For TIFF-based formats, raw tags (including vendor/private tags not surfaced
as typed `Metadata` fields) are available per IFD, anchored to the level or
associated image:

```go
tags, ok := slide.LevelTIFFTags(0)          // image-0 level-0 IFD
if ok {
    if t, ok := tags.Tag(65420); ok {        // a vendor/private tag by number
        s, _ := t.ASCII()
        _ = s
    }
}
slide.AssociatedTIFFTags(a)                   // an associated image's tags
opentile.TIFFDirectoriesOf(slide)             // every IFD incl. orphans (Map/hidden)
```

`TIFFTag` carries `Number`, best-effort `Name`, `Type`, `Count`, verbatim
`Raw` bytes, and typed getters (`ASCII`/`Uints`/`Rationals`). Non-TIFF
formats (IFE, SZI) return `ok=false`. Pixel-pointer tags
(`StripOffsets`/`TileOffsets`/...) are excluded. Currently implemented for
SVS; other TIFF formats follow.
```

- [ ] **Step 5: Commit**

```bash
git add tiff_tags_nontiff_test.go README.md
git commit -m "test/docs: non-TIFF TIFF-tag safety + README section"
```

---

## Self-Review Notes

**Spec coverage:**
- D1 lazy type-assertion provider → Task 4 (`tiffProviderOf`, lazy). ✓
- D2 entity-anchored access → Task 4 (`LevelTIFFTags`/`ImageLevelTIFFTags`/`AssociatedTIFFTags`). ✓
- D3 completeness view w/ structured identity → Task 2 (`TIFFDirectory`) + Task 4 (`TIFFDirectoriesOf`); orphan IFDs captured as `DirOther` (Task 5 Step 3). ✓
- D4 raw + typed → Task 1 (`RawBytes`, decode) + Task 2 (getters). ✓
- D5 pixel-pointer exclusion → Task 3 (`pixelPointerTags`). ✓
- D6 best-effort names → Task 3 (`tiffTagNames`). ✓
- D7 lazy → translation only inside accessor calls (Tasks 4/5). ✓
- D8 additive → all new exported names; no breaking changes. ✓
- §3 API surface → Tasks 2/4. ✓ §4 plumbing → Tasks 1/4/5. ✓ §7 testing → each task's tests + Task 6. ✓

**Scope note (flagged):** This plan implements the substrate + **SVS only** (spec §9's "high-use first", trimmed to one reference reader to keep every task concrete). NDPI, Philips, OME, BIF, generic-TIFF, Leica-SCN, COG-WSI each need their own `TIFFDirectories()` following the Task 5 pattern (capture page→role mapping at Open + the provider method) — a follow-up plan, one task per reader. The substrate + SVS is independently shippable and demonstrable.

**Type consistency:** `tiff.RawTag` (Task 1) fields used verbatim in `tiffTagsFrom` (Task 3). `TIFFDirectory`/`DirectoryKind`/`TIFFTags` (Task 2) used in Tasks 4/5. `TIFFTagsFromPage` (Task 5) bridges `RawTags()`→`tiffTagsFrom`. The provider method name `TIFFDirectories()` is consistent across the interface (Task 4) and the SVS impl (Task 5).

**Placeholder scan:** Task 5 Step 3 intentionally references existing-code variable names (`levelIdx`/`pageIdx`/`spec`) the implementer adapts to — this is integration into existing code, not a placeholder; the exact additions (struct fields, `svsDirSpec`, the capture points, the provider method) are fully specified.
