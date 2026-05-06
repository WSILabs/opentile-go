# opentile-go v0.11 — Leica SCN reader + generictiff relaxations

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land `formats/leicascn` — a Leica SCN reader covering the 3 openslide-testdata fixtures (Leica-1, Leica-2, Leica-Fluorescence-1) — plus two `formats/generictiff` validator relaxations (single-level + mixed-ratio pyramid support, both fixture-driven from Grundium files).

**Architecture:** Single new `formats/leicascn` package mirroring the structure of `formats/svs/` and `formats/bif/`. SCN's "discontinuous scanning" (multiple disjoint main-scan rectangles per slide) is exposed as a single composite Image whose Levels are sparse: tile dispatch routes to the per-region IFD via SCN XML coordinates; tiles outside any region return synthesized white JPEGs (consumer never sees the discontinuity). Multi-channel fluorescence reuses the v0.7 `Image.SizeC()` API. Generictiff relaxations are validator-cap loosenings only (no API surface change).

**Tech stack:** Go 1.23+, BigTIFF reader (`internal/tiff`), TIFF-LZW (`internal/tifflzw`), in-place JPEG splice (`internal/jpeg`), bio-formats CLI for parity oracle (`/opt/bftools/showinf`).

**Spec:** [`docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md`](../specs/2026-05-06-opentile-go-v11-leica-scn-design.md).

---

## Step 0: Confirm upstream

Before any production-code edit, run a quick read of the relevant upstream source. Prevents the v0.2 "guess and debug five times" pattern (CLAUDE.md invariant: "Don't guess format behavior — read upstream").

For SCN: openslide is the upstream of record. Local copy at `/tmp/openslide-vendor-leica.c` (fetched 2026-05-06 from `openslide/openslide:src/openslide-vendor-leica.c`). Bio-formats's `LeicaSCNReader` (`components/formats-bsd/src/loci/formats/in/LeicaSCNReader.java`) is a secondary reference if openslide is ambiguous.

For generictiff: existing `internal/tiff/classify_pyramid.go` is the upstream — relaxations are cap loosenings; mirror the existing test cadence.

---

## Batch A — Pure helpers (3 tasks)

**Goal:** XML parser + classifier as value-in/value-out pure functions, fixture-anchored, before any factory or Tiler code.

### T1 — `scnxml.go` parser + golden tests

**Files:**
- Create: `formats/leicascn/scnxml.go`
- Create: `formats/leicascn/scnxml_test.go`
- Create: `formats/leicascn/scnxml_fixtures_test.go` (committed XML strings from probed fixtures)

**Goal:** parse the SCN XML schema (spec §3) into hand-rolled Go structs. Mirrors `formats/bif/internal/bifxml/` walker pattern.

- [ ] **Step 1: Define the parser types**

```go
package leicascn

// Collection is the top-level <scn>/<collection> element. Carries the
// slide's physical extent (in nm) and the list of <image> children.
type Collection struct {
    UUID       string  // collection's uuid attribute
    Name       string  // collection's name attribute
    Barcode    string  // <barcode> child text (base64-encoded; may be empty)
    SizeXNm    uint64  // <collection sizeX>; slide physical extent
    SizeYNm    uint64
    Images     []Image
}

// Image is one <image> element under <collection>. Each Image has its
// own pyramid (one IFD per resolution × channel) and slide-physical
// view rectangle.
type Image struct {
    Name              string
    UUID              string
    CreationDate      string  // ISO-8601 string; verbatim
    DeviceModel       string
    DeviceVersion     string
    PixelsSizeX       uint32  // <pixels sizeX>; same as level-0 width
    PixelsSizeY       uint32
    Dimensions        []Dimension
    ViewSizeXNm       uint64  // <view sizeX>; slide-physical extent of this scan
    ViewSizeYNm       uint64
    ViewOffsetXNm     uint64
    ViewOffsetYNm     uint64
    SpacingZNm        uint64  // 0 for single-Z (our 3 fixtures)
    Objective         float64
    NumericalAperture float64
    IlluminationSource string  // "brightfield" or "fluorescence"
    Channels          []Channel  // populated when SizeC > 1
}

// Dimension is one <dimension> entry under <pixels>. Maps a (level r,
// channel c) coordinate to a TIFF IFD index.
type Dimension struct {
    R     int     // resolution / level (0 = highest)
    C     int     // channel; 0 if absent (single-channel)
    SizeX uint32  // pixel width at this level
    SizeY uint32  // pixel height at this level
    IFD   int     // TIFF IFD index containing this level/channel's tiles
}

// Channel is one <channelSettings>/<channel> element. Populated only
// for multi-channel fluorescence main scans.
type Channel struct {
    Index             int
    Name              string  // e.g. "405|Empty"
    RGB               string  // e.g. "#0000ff"
    ExcitationFilter  string  // e.g. "BP 405/60"
    DichromaticMirror string  // e.g. "455"
    SuppressionFilter string  // e.g. "470/50"
    ExposureTimeMicros int64
    CCDGain           int
}
```

- [ ] **Step 2: Write the parser entry point**

```go
// ParseDescription parses an SCN XML document (typically the value of
// IFD 0's ImageDescription tag) into a Collection. Returns an error
// if the schema URN doesn't match or required attributes are missing.
//
// The expected schema URN is sealed in v0.10 spec Q1:
//   http://www.leica-microsystems.com/scn/2010/10/01
func ParseDescription(xmlText string) (*Collection, error) {
    // ... handcrafted parser using encoding/xml.Decoder; walk-based to
    // tolerate whitespace + nested namespaced elements without struct
    // tag gymnastics. Mirrors internal/bifxml's DecodeIScan walker.
}

// SchemaURN is the SCN XML schema URN used as the detection
// discriminator. Sealed at v0.10 design Q1.
const SchemaURN = "http://www.leica-microsystems.com/scn/2010/10/01"
```

The handcrafted-walker style (vs. encoding/xml struct tags) matches `formats/bif/internal/bifxml/` and tolerates the SCN XML's verbose namespaces without tag gymnastics. Read that package first for the convention.

- [ ] **Step 3: Write golden-XML test fixtures**

In `formats/leicascn/scnxml_fixtures_test.go`, commit verbatim XML strings extracted from each of our 3 fixtures (use `tifffile` to dump IFD 0 ImageDescription and copy in). Example:

```go
const xmlLeica1 = `<?xml version="1.0"?>
<scn xmlns="http://www.leica-microsystems.com/scn/2010/10/01" ...>
  ...
</scn>`
```

This avoids requiring `OPENTILE_TESTDIR` to test the parser; the parser is exercisable in CI without sample files.

- [ ] **Step 4: Write parser unit tests**

```go
func TestParseDescription_Leica1(t *testing.T) {
    c, err := ParseDescription(xmlLeica1)
    if err != nil { t.Fatal(err) }
    if got := len(c.Images); got != 2 {
        t.Errorf("Images = %d, want 2", got)
    }
    if got := c.SizeXNm; got != 26564529 {
        t.Errorf("SizeXNm = %d, want 26564529", got)
    }
    aux := c.Images[0]
    if got := aux.Objective; got != 0.60833 {
        t.Errorf("Aux objective = %v, want 0.60833", got)
    }
    main := c.Images[1]
    if got := len(main.Dimensions); got != 5 {
        t.Errorf("Main dimensions = %d, want 5", got)
    }
    if got := main.Dimensions[0].IFD; got != 3 {
        t.Errorf("Main L0 IFD = %d, want 3", got)
    }
}

func TestParseDescription_Fluorescence_Channels(t *testing.T) {
    c, err := ParseDescription(xmlFluor1)
    if err != nil { t.Fatal(err) }
    main := c.Images[2] // 3rd <image> = the fluor main
    if got := len(main.Channels); got != 3 {
        t.Errorf("Channels = %d, want 3", got)
    }
    if got := main.Channels[0].Name; got != "405|Empty" {
        t.Errorf("Channels[0].Name = %q, want %q", got, "405|Empty")
    }
    if got := len(main.Dimensions); got != 12 { // 4 levels × 3 channels
        t.Errorf("Fluor main dimensions = %d, want 12", got)
    }
}

func TestParseDescription_RejectsBadURN(t *testing.T) {
    _, err := ParseDescription(`<?xml version="1.0"?><other/>`)
    if err == nil { t.Error("expected URN-mismatch error, got nil") }
}
```

- [ ] **Step 5: Run + commit**

```bash
go test ./formats/leicascn/ -run TestParseDescription -v
git add formats/leicascn/scnxml.go formats/leicascn/scnxml_test.go formats/leicascn/scnxml_fixtures_test.go
git commit -m "feat(leicascn): T1 — SCN XML parser + golden fixture tests"
```

---

### T2 — `classify.go` auxiliary/main classifier + multi-main composer

**Files:**
- Create: `formats/leicascn/classify.go`
- Create: `formats/leicascn/classify_test.go`

**Goal:** classify each `<image>` as auxiliary or main; compose multi-main pyramids into one virtual canvas with sealed invariants (Q5).

- [ ] **Step 1: Write `IsAuxiliary` (sealed Q2)**

```go
// IsAuxiliary reports whether img's view covers the entire
// collection (offset 0,0 + dims match). Sealed Q2: matches openslide's
// is_macro check in src/openslide-vendor-leica.c:469. Magnification
// is metadata only; the role decision is geometric.
func IsAuxiliary(img Image, c *Collection) bool {
    return img.ViewOffsetXNm == 0 &&
        img.ViewOffsetYNm == 0 &&
        img.ViewSizeXNm == c.SizeXNm &&
        img.ViewSizeYNm == c.SizeYNm
}
```

Test: confirm Leica-1 image[0] is auxiliary, image[1] is main; Leica-Fluorescence-1 image[0..1] auxiliary, image[2] main; Leica-2 image[0] auxiliary, image[1..4] main.

- [ ] **Step 2: Write `ComposePyramid` (sealed Q5)**

```go
// CompositeLevel is one level in the multi-region composite pyramid.
// Each Region carries the per-main-scan level data (offset within
// the union pixel space, IFD per channel). Tile dispatch at this
// level is region-local; tiles outside any Region return blank fill.
type CompositeLevel struct {
    Index           int           // 0 = baseline
    PixelSizeX      int           // union extent at this level
    PixelSizeY      int
    NMPerPixelX     float64
    NMPerPixelY     float64
    TileWidth       int           // uniform across regions
    TileHeight      int
    Regions         []RegionLevel
    SizeC           int           // 1 for brightfield, >1 for fluorescence
}

// RegionLevel is one main scan's level slot, positioned within the
// composite level's pixel coordinate space.
type RegionLevel struct {
    OffsetX         int           // pixel offset within composite level
    OffsetY         int
    PixelSizeX      int           // this region's pixel extent at this level
    PixelSizeY      int
    IFDPerChannel   []int         // length = composite SizeC; one IFD per channel
}

// ComposePyramid validates and composes the main-scan list into a
// single multi-region pyramid. Mirrors openslide's compositing logic
// (openslide-vendor-leica.c:560+). Sealed Q5 invariants:
//
//   - All mains share pyramid depth (number of levels).
//   - All mains share illumination source.
//   - All mains share objective magnification.
//   - Per-level resolution similarity ≤ 2% across mains.
//
// Returns ErrUnsupportedSCN with a descriptive message on violation.
func ComposePyramid(mains []Image, collection *Collection) ([]CompositeLevel, error)
```

The arithmetic: convert each main's `ViewSizeXNm + offsetX` to baseline-pixel coords using the main's L0 nm-per-pixel (= ViewSizeXNm / PixelsSizeX); take the bounding rectangle of all mains as the composite L0 extent; compute downsampled extents for higher levels using the shared per-level resolution.

- [ ] **Step 3: Write classifier unit tests**

```go
func TestIsAuxiliary_Leica1(t *testing.T) {
    c, _ := ParseDescription(xmlLeica1)
    if !IsAuxiliary(c.Images[0], c) {
        t.Error("Leica-1 image[0] should be auxiliary")
    }
    if IsAuxiliary(c.Images[1], c) {
        t.Error("Leica-1 image[1] should NOT be auxiliary")
    }
}

func TestComposePyramid_Leica2_FourRegions(t *testing.T) {
    c, _ := ParseDescription(xmlLeica2)
    var mains []Image
    for _, img := range c.Images {
        if !IsAuxiliary(img, c) { mains = append(mains, img) }
    }
    if got := len(mains); got != 4 {
        t.Fatalf("mains = %d, want 4", got)
    }
    levels, err := ComposePyramid(mains, c)
    if err != nil { t.Fatal(err) }
    if got := len(levels); got != 6 {
        t.Errorf("levels = %d, want 6", got)
    }
    l0 := levels[0]
    if got := len(l0.Regions); got != 4 {
        t.Errorf("L0 regions = %d, want 4", got)
    }
    // Union extent: bounding-box across all 4 mains' offsets+sizes
    if l0.PixelSizeX < 39000 || l0.PixelSizeY < 100000 {
        t.Errorf("L0 union extent unexpectedly small: %d×%d",
            l0.PixelSizeX, l0.PixelSizeY)
    }
}

func TestComposePyramid_RejectsMixedDepth(t *testing.T) {
    // Two synthetic mains with different pyramid depths.
    // Should return ErrUnsupportedSCN with a clear message.
    ...
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./formats/leicascn/ -run TestIsAuxiliary -run TestComposePyramid -v
git add formats/leicascn/classify.go formats/leicascn/classify_test.go
git commit -m "feat(leicascn): T2 — auxiliary/main classifier + multi-main composer"
```

---

### T3 — Verification gate against real fixtures

**Files:**
- Create: `formats/leicascn/internal/gates/scn_test.go` (build tag `gates`)

**Goal:** end-to-end probe that opens each real SCN fixture, parses its XML, classifies auxiliary/main, and composes the pyramid — confirming the parser+classifier work on bytes-from-disk before any production-code Open path is wired.

- [ ] **Step 1: Write the gate test**

```go
//go:build gates

package gates

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/cornish/opentile-go/formats/leicascn"
    "github.com/cornish/opentile-go/internal/tiff"
)

func TestSCNFixtureGate(t *testing.T) {
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" { t.Skip("OPENTILE_TESTDIR unset") }

    for _, tc := range []struct {
        file               string
        expectImages       int
        expectAuxiliaries  int
        expectMains        int
        expectMaxC         int
    }{
        {"Leica-1.scn", 2, 1, 1, 1},
        {"Leica-2.scn", 5, 1, 4, 1},
        {"Leica-Fluorescence-1.scn", 3, 2, 1, 3},
    } {
        t.Run(tc.file, func(t *testing.T) {
            path := filepath.Join(dir, "scn", tc.file)
            f, err := os.Open(path); if err != nil { t.Skipf("missing: %v", err) }
            defer f.Close()
            st, _ := f.Stat()
            tf, err := tiff.Open(f, st.Size())
            if err != nil { t.Fatal(err) }

            xmlText, _ := tf.Pages()[0].ImageDescription()
            c, err := leicascn.ParseDescription(xmlText)
            if err != nil { t.Fatal(err) }

            if got := len(c.Images); got != tc.expectImages {
                t.Errorf("Images = %d, want %d", got, tc.expectImages)
            }
            var auxs, mains []leicascn.Image
            maxC := 1
            for _, img := range c.Images {
                if leicascn.IsAuxiliary(img, c) { auxs = append(auxs, img) } else { mains = append(mains, img) }
                for _, d := range img.Dimensions {
                    if d.C+1 > maxC { maxC = d.C + 1 }
                }
            }
            if len(auxs) != tc.expectAuxiliaries { t.Errorf("auxs = %d, want %d", len(auxs), tc.expectAuxiliaries) }
            if len(mains) != tc.expectMains { t.Errorf("mains = %d, want %d", len(mains), tc.expectMains) }
            if maxC != tc.expectMaxC { t.Errorf("max channel = %d, want %d", maxC, tc.expectMaxC) }
        })
    }
}
```

- [ ] **Step 2: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -tags gates -count=1 -v -run TestSCNFixtureGate ./formats/leicascn/internal/gates/
git add formats/leicascn/internal/gates/scn_test.go
git commit -m "test(leicascn): T3 — fixture gate (parser+classifier vs real SCN bytes)"
```

End-of-batch checkpoint: T1+T2 unit-tested in isolation; T3 gate confirms behavior against the 3 real fixtures. No production code outside `formats/leicascn/` yet (no Factory, no Open, no opentile.FormatLeicaSCN).

---

## Batch B — Factory + Open + scaffolding (3 tasks)

**Goal:** wire detection + Tiler scaffolding so `OpenFile()` routes SCN files to a real (but Levels-empty) Tiler. Lets us validate the dispatch order before we wire Levels.

### T4 — `FormatLeicaSCN` constant + Factory + Detection

**Files:**
- Modify: `tiler.go` (add `FormatLeicaSCN` constant)
- Create: `formats/leicascn/leicascn.go`
- Create: `formats/leicascn/leicascn_test.go`
- Modify: `formats/all/all.go` (register Factory before generictiff)

- [ ] **Step 1: Add the Format constant**

```go
// In tiler.go alongside FormatGenericTIFF:
//
// FormatLeicaSCN is the Leica SCN reader (added in v0.11). SCN is a
// BigTIFF dialect produced by Leica SCN400/SCN400F scanners; production
// stopped ~2015. Reports as "leica-scn" to differentiate from other
// Leica-related formats (LIF, LMS) that aren't SCN.
FormatLeicaSCN Format = "leica-scn"
```

- [ ] **Step 2: Write `Factory`, `Supports`, `Open` placeholder**

```go
// formats/leicascn/leicascn.go

package leicascn

import (
    "errors"
    "strings"

    opentile "github.com/cornish/opentile-go"
    "github.com/cornish/opentile-go/internal/tiff"
)

// Factory is the FormatFactory for Leica SCN. Registered BEFORE
// generictiff in formats/all so vendor detection wins on any TIFF
// that smells like SCN.
type Factory struct{ opentile.RawUnsupported }

func New() *Factory { return &Factory{} }

func (f *Factory) Format() opentile.Format { return opentile.FormatLeicaSCN }

// Supports reports whether file is a Leica SCN BigTIFF. Discriminator
// (sealed Q1): IFD 0's ImageDescription contains the SCN schema URN.
func (f *Factory) Supports(file *tiff.File) bool {
    pages := file.Pages()
    if len(pages) == 0 { return false }
    desc, ok := pages[0].ImageDescription()
    if !ok { return false }
    return strings.Contains(desc, SchemaURN)
}

// Open constructs a SCN Tiler. T4 placeholder returns
// errSCNTilerUnimplemented; T6 wires the real Tiler.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    return nil, errSCNTilerUnimplemented
}

var errSCNTilerUnimplemented = errors.New("formats/leicascn: Tiler not yet implemented (T6+)")

// ErrUnsupportedSCN is returned when an SCN file fails the v0.11
// composition invariants (Q5: same depth/illumination/objective; ±2%
// per-level resolution similarity). Wraps the specific violation.
var ErrUnsupportedSCN = errors.New("leicascn: unsupported SCN layout")
```

- [ ] **Step 3: Register before generictiff**

```go
// formats/all/all.go — update the registration order:
opentile.Register(svs.New())
opentile.Register(ndpi.New())
opentile.Register(philips.New())
opentile.Register(ome.New())
opentile.Register(bif.New())
opentile.Register(ife.New())
opentile.Register(leicascn.New())   // v0.11 — vendor format, before catch-all
opentile.Register(generictiff.New()) // catch-all, last
```

- [ ] **Step 4: Write detection tests**

```go
// formats/leicascn/leicascn_test.go

func TestFactory_Format(t *testing.T) {
    if got := New().Format(); got != opentile.FormatLeicaSCN {
        t.Errorf("Format = %v, want %v", got, opentile.FormatLeicaSCN)
    }
}

func TestFactory_Supports_RealFixtures(t *testing.T) {
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" { t.Skip("OPENTILE_TESTDIR unset") }
    for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
        t.Run(name, func(t *testing.T) {
            // Open via tiff.Open + assert Supports() returns true.
        })
    }
}

func TestFactory_Supports_RejectsNonSCN(t *testing.T) {
    // Open CMU-1.svs and CMU-1.tiff; Supports() must return false.
}

func TestFactory_Open_Placeholder(t *testing.T) {
    // Open a real fixture; expect errSCNTilerUnimplemented (T4 stub).
}
```

- [ ] **Step 5: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -v ./formats/leicascn/ ./formats/all/...
git add tiler.go formats/leicascn/leicascn.go formats/leicascn/leicascn_test.go formats/all/all.go
git commit -m "feat(leicascn): T4 — Factory + Detection + dispatch order"
```

---

### T5 — `associated.go` AssociatedImage impl

**Files:**
- Create: `formats/leicascn/associated.go`
- Create: `formats/leicascn/associated_test.go`

**Goal:** one AssociatedImage per auxiliary `<image>`, reading the highest-res IFD. Pyramid sub-levels of the auxiliary are dropped (sealed Q8).

- [ ] **Step 1: Write the AssociatedImage**

```go
// associatedImage reads the highest-resolution IFD of an auxiliary
// SCN <image> element. Bytes are eagerly read at construction time
// (typical < 5 MB; same convention as formats/generictiff). Kind() is
// always "macro" per Q8; format-specific metadata (illumination,
// objective) is exposed via leicascn.MetadataOf.
type associatedImage struct {
    size        opentile.Size
    compression opentile.Compression
    bytes       []byte
}

func (a *associatedImage) Kind() string                      { return "macro" }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
    out := make([]byte, len(a.bytes)); copy(out, a.bytes); return out, nil
}

// newAssociatedImage builds an associatedImage from an auxiliary
// <image>'s level-0 (highest-resolution) Dimension entry. The IFD's
// tile offsets/lengths are read; tiles are concatenated into a single
// JPEG (libtiff RST-marker layout reproduces a valid JPEG via simple
// concat — same pattern formats/generictiff uses for stripped JPEG).
//
// For non-tiled auxiliaries (which we expect on macro IFDs):
// strip-based path; mirrors generictiff's associated reader.
func newAssociatedImage(img Image, file *tiff.File, r io.ReaderAt) (*associatedImage, error)
```

The auxiliary IFD might be tiled OR stripped. Both Leica-1 and Leica-Fluorescence-1's auxiliary IFDs are tiled (TileWidth/TileLength tags present per the bio-formats probe — "Tile size = 512 x 512"). For the tiled case: walk all tiles, concatenate JPEG tile bytes via the libtiff-RST-layout pattern (works because the bytes share JPEGTables). For the (unlikely) stripped case: reuse the generictiff `assembleAssociated` helper if practical.

- [ ] **Step 2: Tests**

```go
func TestAssociatedImage_Leica1(t *testing.T) {
    // Read Leica-1.scn; pull IFD 0 (the auxiliary's level 0); confirm:
    //   - Kind() == "macro"
    //   - Size().W == 1616, .H == 4668
    //   - Compression() == JPEG
    //   - Bytes() starts with FF D8 (JPEG SOI), ends with FF D9 (EOI)
}
```

- [ ] **Step 3: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -v -run TestAssociatedImage ./formats/leicascn/
git add formats/leicascn/associated.go formats/leicascn/associated_test.go
git commit -m "feat(leicascn): T5 — AssociatedImage impl (one per auxiliary)"
```

---

### T6 — `tiler.go` Tiler + Metadata + MetadataOf

**Files:**
- Create: `formats/leicascn/tiler.go`
- Create: `formats/leicascn/tiler_test.go`
- Modify: `formats/leicascn/leicascn.go` (wire real Open returning Tiler)

**Goal:** end-to-end Open path returning a Tiler with populated AssociatedImages and Metadata. Levels still empty (T7+ wires them).

- [ ] **Step 1: Define `Metadata`, `MetadataOf`, `tiler`**

```go
// Metadata is the Leica SCN-specific format metadata. Embeds opentile.Metadata.
//
// Read via MetadataOf:
//
//   if md, ok := leicascn.MetadataOf(t); ok {
//       fmt.Println(md.Barcode, md.Regions[0].Objective)
//   }
type Metadata struct {
    opentile.Metadata
    CollectionUUID string
    Barcode        string  // base64-encoded; may be empty
    Auxiliaries    []AuxiliaryInfo
    Regions        []RegionInfo
    Channels       []ChannelInfo  // nil when SizeC == 1
}

// AuxiliaryInfo / RegionInfo / ChannelInfo: see spec §5.1 for the
// exact field set. All fields populated from the SCN XML's <image>
// and <channelSettings> elements.

type tiler struct {
    md         Metadata
    levels     []opentile.Level         // empty in T6; T7-T9 populate
    associated []opentile.AssociatedImage
    icc        []byte
    sizeC      int
}

func (t *tiler) Format() opentile.Format             { return opentile.FormatLeicaSCN }
func (t *tiler) Images() []opentile.Image            { return []opentile.Image{newSCNImage(t.levels, t.sizeC, t.md.Channels)} }
func (t *tiler) Levels() []opentile.Level            { /* return copy */ }
func (t *tiler) Level(i int) (opentile.Level, error) { /* bounds-check */ }
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata         { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                  { return t.icc }
func (t *tiler) Close() error                        { return nil }
func (t *tiler) WarmLevel(i int) error               { /* delegate to level.warm() */ }

// scnImage implements opentile.Image with SizeC > 1 support for
// fluorescence files. Reuses opentile.NewSingleImage's pattern but
// overrides SizeC + ChannelName.
type scnImage struct {
    levels   []opentile.Level
    sizeC    int
    channels []ChannelInfo
}
// ... Index, Name, Levels, Level, MPP, SizeZ/SizeC/SizeT, ChannelName, ZPlaneFocus
```

Look at how `formats/bif/image.go` exposes `SizeZ + ZPlaneFocus` for the pattern; SCN's `SizeC + ChannelName` is the analogous addition on the C axis.

- [ ] **Step 2: Wire Open**

In `leicascn.go`'s `Open`, replace the placeholder error with:

```go
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    pages := file.Pages()
    if len(pages) == 0 { return nil, errors.New("leicascn: no pages") }
    desc, ok := pages[0].ImageDescription()
    if !ok { return nil, errors.New("leicascn: missing IFD 0 ImageDescription") }
    c, err := ParseDescription(desc)
    if err != nil { return nil, fmt.Errorf("leicascn: %w", err) }

    // Split into auxiliaries and mains.
    var auxs, mains []Image
    for _, img := range c.Images {
        if IsAuxiliary(img, c) { auxs = append(auxs, img) } else { mains = append(mains, img) }
    }

    // Build AssociatedImages from auxiliaries.
    r := file.ReaderAt()
    var associated []opentile.AssociatedImage
    for _, aux := range auxs {
        a, err := newAssociatedImage(aux, file, r)
        if err != nil { return nil, fmt.Errorf("leicascn: aux %s: %w", aux.Name, err) }
        associated = append(associated, a)
    }

    // Compose pyramid from mains. T7+ populates Levels from the result;
    // T6 leaves the slice empty so end-to-end Open returns successfully.
    composite, err := ComposePyramid(mains, c)
    if err != nil { return nil, fmt.Errorf("leicascn: %w", err) }
    _ = composite // T7 wires this into Levels

    // Determine SizeC.
    sizeC := 1
    if len(mains) > 0 {
        for _, d := range mains[0].Dimensions { if d.C+1 > sizeC { sizeC = d.C + 1 } }
    }

    icc, _ := pages[0].ICCProfile()

    return &tiler{
        md:         buildMetadata(c, auxs, mains),
        levels:     nil, // T7-T9 populate
        associated: associated,
        icc:        icc,
        sizeC:      sizeC,
    }, nil
}
```

- [ ] **Step 3: Tests**

```go
func TestOpen_Leica1(t *testing.T) {
    // Open Leica-1.scn end-to-end; expect:
    //   - Format() == FormatLeicaSCN
    //   - len(Levels()) == 0 (T6 placeholder)
    //   - len(Associated()) == 1
    //   - Associated[0].Kind() == "macro", Size 1616×4668
    //   - SizeC == 1
}

func TestOpen_Fluorescence_SizeC(t *testing.T) {
    // Open Leica-Fluorescence-1.scn; expect:
    //   - SizeC == 3
    //   - 2 AssociatedImages (both kind="macro")
    //   - Metadata.Channels has 3 entries with Name "405|Empty" / "L5|Empty" / "TX2|Empty"
}

func TestMetadataOf(t *testing.T) {
    // Open Leica-1; MetadataOf returns ok=true; check Barcode field.
}
```

- [ ] **Step 4: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -v ./formats/leicascn/
git add formats/leicascn/tiler.go formats/leicascn/tiler_test.go formats/leicascn/leicascn.go
git commit -m "feat(leicascn): T6 — Tiler + Metadata + MetadataOf (Levels empty pending T7+)"
```

End-of-batch checkpoint: `OpenFile("Leica-1.scn")` returns a usable Tiler with AssociatedImages + Metadata populated. Tiles aren't readable yet.

---

## Batch C — Level impl (4 tasks)

**Goal:** real per-tile reads, including blank-fill for inter-region gaps and per-channel access for fluorescence.

### T7 — Per-region tiledImage Level (single-region path)

**Files:**
- Create: `formats/leicascn/tiled_region.go`
- Create: `formats/leicascn/tiled_region_test.go`

**Goal:** reusable Level shape representing ONE main scan's pyramid level. Used by Leica-1 (1 region) directly and by T8's composite for multi-region cases.

- [ ] **Step 1: Define `tiledRegion`**

Mirror `formats/generictiff/tiled.go`'s shape exactly — generictiff's `tiledImage` is the right reference. Per-channel: `tiledRegion` carries an `[]int` of IFD indices (one per channel). Tile reads dispatch by channel:

```go
type tiledRegion struct {
    levelIdx       int
    tileSize       opentile.Size
    grid           opentile.Size
    pixelSize      opentile.Size  // this region's level pixel extent
    offsetInLevel  opentile.Size  // pixel offset within composite level
    compression    opentile.Compression

    // Per-channel TIFF-IFD-derived data. Index 0 == default for
    // Tile() (single-channel == 2D entry point).
    perChannel    []channelData
    reader        io.ReaderAt
}

type channelData struct {
    offsets       []uint64
    counts        []uint64
    jpegTables    []byte
    splicePrefix  []byte
    maxTileSize   int
}

func (r *tiledRegion) Tile(c, x, y int) ([]byte, error) {
    // Bounds-check; lookup IFD via perChannel[c]; ReadAt + splice if JPEG.
}

func (r *tiledRegion) TileInto(c, x, y int, dst []byte) (int, error) { ... }
func (r *tiledRegion) warm() error { ... }
```

- [ ] **Step 2: Tests against single-region Leica-1**

```go
func TestTiledRegion_Leica1_L0_RGBTile(t *testing.T) {
    // Build a tiledRegion for Leica-1's main scan L0 (IFD 3).
    // Tile(0, 0, 0) returns valid JPEG bytes (FF D8 ... FF D9).
    // Out-of-bounds returns ErrTileOutOfBounds.
}

func TestTiledRegion_TileEqualsTileInto(t *testing.T) {
    // Tile() and TileInto() return byte-identical output (sample 4 corners).
}
```

- [ ] **Step 3: Run + commit**

```bash
git add formats/leicascn/tiled_region.go formats/leicascn/tiled_region_test.go
git commit -m "feat(leicascn): T7 — per-region tiledRegion Level"
```

---

### T8 — Multi-region composite Level

**Files:**
- Create: `formats/leicascn/tiled.go`
- Create: `formats/leicascn/tiled_test.go`

**Goal:** the public `opentile.Level` impl that wraps N `tiledRegion`s + a blank-tile fallback. Tile dispatch selects the region containing (x, y) or returns the blank tile.

- [ ] **Step 1: Define `compositeLevel`**

```go
type compositeLevel struct {
    index       int
    pyrIndex    int
    size        opentile.Size  // composite/union pixel extent
    tileSize    opentile.Size  // uniform across regions
    grid        opentile.Size  // composite grid
    compression opentile.Compression
    sizeC       int

    regions     []*tiledRegion // one per main scan
    blank       []byte         // synthesized white JPEG, lazily computed
}

func (l *compositeLevel) Tile(x, y int) ([]byte, error) { return l.TileAt(opentile.TileCoord{X: x, Y: y}) }
func (l *compositeLevel) TileInto(x, y int, dst []byte) (int, error) { ... }
func (l *compositeLevel) TileAt(coord opentile.TileCoord) ([]byte, error) {
    if coord.Z != 0 || coord.T != 0 {
        return nil, &opentile.TileError{...ErrDimensionUnavailable...}
    }
    if coord.C < 0 || coord.C >= l.sizeC {
        return nil, &opentile.TileError{...ErrDimensionUnavailable...}
    }
    if coord.X < 0 || coord.Y < 0 || coord.X >= l.grid.W || coord.Y >= l.grid.H {
        return nil, &opentile.TileError{...ErrTileOutOfBounds...}
    }
    // Find region containing tile origin. Uses per-region OffsetInLevel
    // + PixelSize bounds in composite-pixel-space.
    px := coord.X * l.tileSize.W
    py := coord.Y * l.tileSize.H
    for _, r := range l.regions {
        if pointInRegion(px, py, r) {
            // Translate to region-local tile coords and dispatch.
            rx := (px - r.offsetInLevel.W) / l.tileSize.W
            ry := (py - r.offsetInLevel.H) / l.tileSize.H
            return r.Tile(coord.C, rx, ry)
        }
    }
    return l.blankTile(), nil
}
```

- [ ] **Step 2: Tests against Leica-2 (4 regions) and Leica-1 (1 region)**

```go
func TestCompositeLevel_Leica2_DispatchesByRegion(t *testing.T) {
    // Open Leica-2.scn; sample tiles at coords known to be in each
    // region (from XML offsets) + at a coord in the inter-region gap.
    // Inside-region tiles return JPEG bytes; gap tile equals the
    // synthesized blank.
}

func TestCompositeLevel_Leica1_SingleRegion_DegenerateCase(t *testing.T) {
    // Leica-1 has 1 main; the composite collapses to a 1-region level.
    // Verify it behaves like a plain Level (corner tiles work, OOB errors).
}
```

- [ ] **Step 3: Run + commit**

```bash
git add formats/leicascn/tiled.go formats/leicascn/tiled_test.go
git commit -m "feat(leicascn): T8 — multi-region compositeLevel with per-region dispatch"
```

---

### T9 — Blank tile generator

**Files:**
- Create: `formats/leicascn/blanktile.go`
- Create: `formats/leicascn/blanktile_test.go`

**Goal:** synthesize a white JPEG of given (W, H) tile size, cached per level. Reuses (or mirrors) the existing Philips blank-tile pattern.

- [ ] **Step 1: Read existing Philips blank-tile impl + decide reuse vs reimplement**

Read `formats/philips/blank_tile.go` (or wherever the existing pattern lives). If it's reusable as-is: add a thin wrapper. If not: reimplement using `internal/jpegturbo` to encode a constant-color RGB block.

- [ ] **Step 2: Implement + cache**

```go
func (l *compositeLevel) blankTile() []byte {
    l.blankOnce.Do(func() {
        l.blank = generateBlankJPEG(l.tileSize.W, l.tileSize.H)
    })
    return l.blank
}

func generateBlankJPEG(w, h int) []byte {
    // RGB white pixels, JPEG-encode via internal/jpegturbo or stdlib jpeg.
    // Prefer stdlib (image/jpeg) for the blank case — no special tuning,
    // minimal cgo footprint. Encoded once per level (lazy) and shared.
}
```

- [ ] **Step 3: Tests**

```go
func TestBlankTile_ValidJPEG(t *testing.T) {
    b := generateBlankJPEG(512, 512)
    if !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) { t.Error("missing SOI") }
    if !bytes.HasSuffix(b, []byte{0xFF, 0xD9}) { t.Error("missing EOI") }
    // Optionally: decode and confirm white.
}
```

- [ ] **Step 4: Run + commit**

```bash
git add formats/leicascn/blanktile.go formats/leicascn/blanktile_test.go
git commit -m "feat(leicascn): T9 — synthesized white-JPEG blank tile for inter-region gaps"
```

---

### T10 — Wire Levels + multi-channel TileAt into Open

**Files:**
- Modify: `formats/leicascn/leicascn.go` (wire `composite` into Tiler.levels)
- Modify: `formats/leicascn/tiler.go` (scnImage.SizeC + ChannelName)

**Goal:** `OpenFile` now returns a fully-functional Tiler. End-to-end smoke test reads tiles from all 3 fixtures.

- [ ] **Step 1: Convert `[]CompositeLevel` → `[]opentile.Level`**

In `leicascn.go`'s `Open`, after `ComposePyramid`:

```go
levels := make([]opentile.Level, len(composite))
for i, cl := range composite {
    levels[i] = newCompositeLevel(i, cl, mains, file.ReaderAt())
}
```

- [ ] **Step 2: Smoke test all 3 fixtures**

```go
func TestOpen_AllFixtures_ReadL0Corner(t *testing.T) {
    for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
        t.Run(name, func(t *testing.T) {
            // OpenFile; read L0 (0,0); confirm valid JPEG.
            // For fluorescence: also read TileAt({C:1,X:0,Y:0}) and confirm
            // it's a different IFD than C:0.
        })
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -v ./formats/leicascn/
git add formats/leicascn/leicascn.go formats/leicascn/tiler.go formats/leicascn/leicascn_test.go
git commit -m "feat(leicascn): T10 — wire Levels into Tiler; multi-channel SizeC support"
```

End-of-batch checkpoint: `make test` green; SCN fixtures readable end-to-end. `formats/leicascn/` complete except for ICC handling on auxiliary IFDs (TBD if a fixture surfaces an ICC profile worth surfacing).

---

## Batch D — Generictiff relaxations (2 tasks)

**Goal:** Grundium fixtures load via the generictiff catch-all reader.

### T11 — Validator threshold relaxations

**Files:**
- Modify: `internal/tiff/classify_pyramid.go` (config defaults)
- Modify: `internal/tiff/classify_pyramid_test.go` (update tests that pinned old caps)

**Goal:** `MinLevels: 3 → 1` and `LeftoverTiledMaxAreaRatio: 0.01 → 0.05`. Both relaxations are cap-loosenings; existing passing fixtures remain unchanged.

- [ ] **Step 1: Edit `DefaultClassifyPyramidConfig`**

```go
func DefaultClassifyPyramidConfig() ClassifyPyramidConfig {
    return ClassifyPyramidConfig{
        MinLevels:                 1,    // v0.10: 3, v0.11 relaxed for single-level tiled TIFFs
        InterAxisTolerance:        0.02,
        InterLevelTolerance:       0.05,
        MaxLeftoverTiled:          2,
        LeftoverTiledMaxAreaRatio: 0.05, // v0.10: 0.01, v0.11 relaxed for mixed-ratio chains
    }
}
```

- [ ] **Step 2: Update tests pinning the old caps**

Find existing tests that asserted `MinLevels=3` rejection or `LeftoverTiledMaxAreaRatio=0.01` rejection. Adjust assertions or add the legacy cap as an explicit override in those tests.

- [ ] **Step 3: Verify v0.10 fixtures unaffected**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/generictiff/ ./tests/parity/
```

Expected: green. CMU-1.tiff and CMU-1.stripped.tiff continue to classify identically.

- [ ] **Step 4: Commit**

```bash
git add internal/tiff/classify_pyramid.go internal/tiff/classify_pyramid_test.go
git commit -m "refactor(tiff): v0.11 R1+R2 — relax MinLevels=1 + LeftoverTiledMaxAreaRatio=0.05"
```

---

### T12 — Grundium fixture wiring + geometry pinning

**Files:**
- Modify: `tests/integration_test.go` (add scan_619 + scan_620 to slideCandidates)
- Modify: `tests/parity/generic_geometry_test.go` (add 2 fixture rows)
- Generate: `tests/fixtures/scan_619_grundium_pyramid_TIFF.tif.json`, `tests/fixtures/scan_620_grundium_TIFF.tif.json`

- [ ] **Step 1: Probe + pin geometry**

```bash
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/scan_619_grundium_pyramid_TIFF.tif
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/scan_620_grundium_TIFF.tif
```

Capture the Levels/AssociatedImages output and add fixture rows to `genericFixtures` in `tests/parity/generic_geometry_test.go`.

- [ ] **Step 2: Generate SHA fixtures**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run "TestGenerateFixtures/scan_619|TestGenerateFixtures/scan_620" -generate -v
```

- [ ] **Step 3: Add to slideCandidates**

```go
// tests/integration_test.go
var slideCandidates = []string{
    ...
    "CMU-1.tiff",
    "CMU-1.stripped.tiff",
    "scan_619_grundium_pyramid_TIFF.tif",  // v0.11
    "scan_620_grundium_TIFF.tif",          // v0.11
}
```

- [ ] **Step 4: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./tests/ ./tests/parity/ -v -run "TestSlideParity|TestGenericGeometry"
git add tests/integration_test.go tests/parity/generic_geometry_test.go tests/fixtures/scan_619_grundium_pyramid_TIFF.tif.json tests/fixtures/scan_620_grundium_TIFF.tif.json
git commit -m "test(generictiff): T12 — wire Grundium fixtures (single-level + mixed-ratio)"
```

End-of-batch checkpoint: `make test` green; Grundium fixtures load via the generictiff factory. `TestSlideParity` total grows to 21 (was 19 post-v0.10 + 2 generic).

---

## Batch E — Integration + parity + docs (3 tasks)

### T13 — `tests/integration_test.go` SCN wiring + per-fixture geometry pinning

**Files:**
- Modify: `tests/integration_test.go` (add SCN to slideCandidates + resolveSlide subdir)
- Create: `tests/parity/leicascn_geometry_test.go`
- Generate: `tests/fixtures/Leica-1.scn.json`, `tests/fixtures/Leica-2.scn.json`, `tests/fixtures/Leica-Fluorescence-1.scn.json`

- [ ] **Step 1: Wire SCN in `resolveSlide` + `slideCandidates`**

```go
// resolveSlide subdirectory list:
for _, sub := range []string{"", "svs", "ndpi", "philips-tiff", "ome-tiff", "bif", "ife", "generic-tiff", "scn"} {
    ...
}

// fixtureJSONFor: .scn → <stem>.scn.json
case ".scn": return stem + ".scn.json"

// slideCandidates additions:
"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn",
```

- [ ] **Step 2: Decide sampledByDefault**

Per existing policy (>100 MB → sampled by default), Leica-1 (278 MB) + Leica-2 (2.1 GB) get sampled mode; Leica-Fluorescence-1 (21 MB) is full-walk.

```go
func sampledByDefault(slide string) bool {
    ...
    case "Leica-1.scn", "Leica-2.scn":
        return true
    ...
}
```

- [ ] **Step 3: Generate SHA fixtures**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run "TestGenerateFixtures/Leica" -generate -v
```

- [ ] **Step 4: Write `leicascn_geometry_test.go`**

Mirror `bif_geometry_test.go` exactly: per-fixture level Size/TileSize/Grid/Compression, AssociatedImage kinds + sizes, SizeC + ChannelName, L0 (0,0) JPEG SOI check, OOB rejection, cross-backing parity (`TestSCNOpenFileBackingsByteIdentical`).

- [ ] **Step 5: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./tests/ ./tests/parity/ -v
git add tests/integration_test.go tests/parity/leicascn_geometry_test.go tests/fixtures/Leica-*.scn.json
git commit -m "test(leicascn): T13 — TestSlideParity wiring + per-fixture geometry pinning"
```

---

### T14 — Bio-formats parity oracle

**Files:**
- Create: `tests/oracle/leicascn_bf_test.go` (build tag `bfparity`)
- Create: `tests/oracle/bf_runner.go` (helper invoking `/opt/bftools/showinf`)

**Goal:** structural-equivalence parity oracle vs bio-formats CLI. Sealed Q9: dimensions + channels match; tile byte-equality is NOT feasible (decode+re-encode divergence).

- [ ] **Step 1: Implement `bf_runner.go`**

```go
// runShowinf invokes `bfconfig`+`showinf -nopix -no-upgrade <file>`,
// parses the output for Series count + per-series Width/Height/SizeC,
// and returns a structured summary.
func runShowinf(t *testing.T, file string) *ShowinfSummary { ... }

type ShowinfSummary struct {
    SeriesCount int
    Series      []ShowinfSeriesEntry
}
type ShowinfSeriesEntry struct {
    Width, Height int
    SizeC         int
    Thumbnail     bool
}
```

- [ ] **Step 2: Write the parity test**

```go
//go:build bfparity

package oracle

func TestBioFormatsParity_SCN(t *testing.T) {
    if _, err := exec.LookPath("/opt/bftools/showinf"); err != nil {
        t.Skip("bio-formats CLI not installed")
    }
    ...
    for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
        t.Run(name, func(t *testing.T) {
            // Run showinf; parse summary.
            // Open via opentile.OpenFile; build our own summary
            // (Levels + AssociatedImages); compare.
        })
    }
}
```

Comparison rule: bio-formats counts the lowest-resolution IFD of each pyramid as a "Thumbnail series". We collapse those into our Levels (all main pyramid levels) + the highest-res of auxiliaries (single-image AssociatedImage). The comparator filters bio-formats's Thumbnail series and asserts the dimensions match our Level/AssociatedImage list.

- [ ] **Step 3: Run + commit**

```bash
OPENTILE_TESTDIR=$PWD/sample_files go test -tags bfparity -count=1 -v ./tests/oracle/...
git add tests/oracle/leicascn_bf_test.go tests/oracle/bf_runner.go
git commit -m "test(leicascn): T14 — bio-formats CLI parity oracle (structural equivalence)"
```

---

### T15 — Docs + ship

**Files:**
- Create: `docs/formats/leicascn.md`
- Modify: `docs/deferred.md` (§1a + §2 L30-L34 + §8e v0.11 retirement audit + §11 backlog rows for L30-L34)
- Modify: `README.md` (Supported-formats table + Detection paragraph + Format() example values)
- Modify: `CHANGELOG.md` (`[0.11.0]` section + [Unreleased] reset)
- Modify: `CLAUDE.md` (Current milestone bump v0.10 → v0.11)

- [ ] **Step 1: `docs/formats/leicascn.md`**

Mirror the bif.md / generictiff.md template. Sections: Format basics, Fixture inventory (with the 3-fixture coverage limitation called out prominently per owner directive 2026-05-06), What's supported, What's not supported (L30-L34), Parity (sample SHAs + bio-formats CLI parity + the byte-equality limitation), Deviations from upstream Python opentile, Implementation references, Known issues + history.

- [ ] **Step 2: `docs/deferred.md` updates**

Mirror the v0.10 T13 commit shape: §1a deviation entry for "Leica SCN reader (since v0.11)"; §2 L30-L34 entries; §8e (new) v0.11 retirement audit subsection mirroring §8d shape; §11 backlog rows for L30-L34.

- [ ] **Step 3: README updates**

Add Leica SCN row to Supported-formats table; update Detection paragraph (mention SCN registers between IFE and generictiff); update `Format()` example string; add deviation row.

- [ ] **Step 4: `CHANGELOG.md` `[0.11.0]`**

Mirror the v0.10 entry shape: Headline (generic-TIFF milestone → SCN milestone); Added (formats/leicascn package, FormatLeicaSCN constant, leicascn.Metadata + MetadataOf, multi-region compositeLevel, multi-channel TileAt support, blank-tile fill); Changed (validator relaxations on generictiff: MinLevels=1, LeftoverTiledMaxAreaRatio=0.05); Deviations (1 new §1a entry); Test coverage (3 SCN fixtures + 2 Grundium fixtures; bio-formats CLI parity oracle); Active limitations (L30-L34); Notes.

Also reset `[Unreleased]` to point at post-v0.11 active limitations.

- [ ] **Step 5: `CLAUDE.md` milestone bump**

Demote v0.10 to "Previous milestone (shipped 2026-05-06)" with one-paragraph summary; promote "Current milestone — v0.11 (shipped YYYY-MM-DD)" with the SCN headline.

- [ ] **Step 6: Final verification + commit + tag**

```bash
go vet ./...
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./...
git add docs/formats/leicascn.md docs/deferred.md README.md CHANGELOG.md CLAUDE.md
git commit -m "docs(v0.11): T15 — formats/leicascn.md + README + CHANGELOG + CLAUDE.md milestone bump"
```

End-of-milestone: `make test` green; merge to main; `git tag v0.11.0`; push when owner approves.

---

## Test count delta

`TestSlideParity` total post-v0.11: 5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE + 4 generic (CMU-1, stripped, scan_619, scan_620) + 3 SCN = **24 fixtures**.

## Risks

- **R1 — Fluorescence fixture is the only multi-channel exercise we'll ever have.** `Image.SizeC()>1` correctness depends entirely on Leica-Fluorescence-1.scn behaving as expected. Mitigation: bio-formats CLI parity (T14) catches divergences at structural level; consumer-side multi-channel divergences would need a future fixture.
- **R2 — Multi-region resolution mismatch in real-world SCN files.** Our 3 fixtures have well-aligned mains. A real-world SCN with mismatched per-region pyramid depths would trigger `ErrUnsupportedSCN`; consumers report the violation. Mitigation: error message is descriptive; debug-from-scratch flow accepted (L34 permanent limitation).
- **R3 — Generictiff relaxation regresses existing fixture detection.** Mitigation: T11 step 3 explicitly verifies v0.10 fixtures unaffected; full test suite at end-of-batch checkpoints.
- **R4 — Bio-formats CLI behavior changes.** The parity oracle depends on `showinf` output format being stable. If bio-formats is upgraded and the output format drifts, the parity test breaks. Mitigation: pin a specific bio-formats version in the oracle test; document the version in `docs/formats/leicascn.md`.

## Plan-spec coverage check

Spec sections covered by tasks:

- §1 Scope (SCN headline + Grundium folded in) → all batches.
- §2 Fixtures + detection discriminator → T1 (XML schema URN) + T4 (Supports check).
- §3 SCN file structure → T1 (parser) + T2 (classifier) + T3 (gate).
- §4.1 Single Image, multi-region levels → T8.
- §4.2 Composition invariants → T2 ComposePyramid validation.
- §4.3 Per-tile dispatch → T8.
- §4.4 Inter-region blank fill → T9.
- §4.5 Multi-channel exposure → T10 + scnImage.SizeC.
- §4.6 Auxiliary AssociatedImages → T5.
- §5 Public API surface → T4 (FormatLeicaSCN) + T5 (AssociatedImage) + T6 (Metadata + MetadataOf).
- §6 Generictiff relaxations → T11 + T12.
- §7 Bio-formats parity oracle → T14.
- §8 Test fixtures → T13 + T12.
- §9 Detection-order sanity → T4 (formats/all/all.go ordering).
- §10 Sealed Q-decisions → all references in the relevant tasks.
- §11 Active limitations L30-L34 → T15 deferred.md update.
- §12 Plan outline → THIS DOCUMENT.

No spec section uncovered.
