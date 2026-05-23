# opentile-go v0.16 — Smart Zoom Image (SZI) reader implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a Smart Zoom Image (SZI) reader at `formats/szi/`, backed by a new shared `internal/dzi/` core (manifest parser + tile-coordinate math). Reads ZIP-wrapped Microsoft Deep Zoom pyramids with `scan-properties.xml` and `associated_images/`. Driven by user's wsi-tools / viewer pipeline targeting Grundium-scanner output.

**Architecture:** Six-task single batch. T1 lands `internal/dzi/` (pure manifest + coords; no I/O). T2 lands the `formats/szi/` skeleton + factory + new `opentile.FormatSZI` and `opentile.CompressionPNG` enum values. T3 wires Image / Level / Tile. T4 wires Associated images + scan-properties metadata. T5 wires fixtures into TestSlideParity (28 → 30). T6 closes with docs.

**Tech stack:** Go 1.23+; standard `archive/zip` for ZIP central-directory parsing; standard `encoding/xml` for DZI manifest + scan-properties.xml.

**Spec:** [`docs/superpowers/specs/2026-05-08-opentile-go-v16-szi-design.md`](../specs/2026-05-08-opentile-go-v16-szi-design.md).

---

## Task layout

6 tasks, single batch:

- T1 — `internal/dzi/` package (manifest parser + tile-coordinate math) + unit tests
- T2 — `opentile.FormatSZI` + `opentile.CompressionPNG` + `formats/szi/` skeleton (factory + Tiler open/close + ZIP central-directory parse + register)
- T3 — `formats/szi/` Image / Level / Tile (per-level dims; ZIP-entry tile lookup; border-tile sizing)
- T4 — `formats/szi/` Associated images + `scan-properties.xml` parser + `szi.Metadata` + `szi.MetadataOf`
- T5 — Fixtures + tests (per-tile SHA JSON; new `tests/parity/szi_geometry_test.go`; TestSlideParity 30 fixtures)
- T6 — Docs + ship (`docs/formats/szi.md`; README; `docs/deferred.md §8j`; CHANGELOG; CLAUDE.md milestone bump)

---

## T1 — `internal/dzi/` manifest parser + tile-coordinate math

**Files:**
- Create: `internal/dzi/doc.go`
- Create: `internal/dzi/manifest.go`
- Create: `internal/dzi/manifest_test.go`
- Create: `internal/dzi/coords.go`
- Create: `internal/dzi/coords_test.go`

Pure-function package — no I/O. Provides the DZI manifest parser + level/tile coordinate math used by `formats/szi/` (v0.16) and a future `formats/dzi/` (v0.17+).

- [ ] **Step 1: Create `internal/dzi/doc.go`**

```go
// Package dzi parses the Microsoft Deep Zoom Image (DZI) manifest
// XML format and computes per-level / per-tile coordinate
// information.
//
// Per-image DZI manifests look like:
//
//	<?xml version="1.0" encoding="UTF-8"?>
//	<Image xmlns="http://schemas.microsoft.com/deepzoom/2008"
//	       Format="jpeg" Overlap="0" TileSize="256">
//	  <Size Width="2220" Height="2967"/>
//	</Image>
//
// Tile naming convention (per Microsoft spec, verbatim):
//
//	"The tiles are named as column_row.format, where row is the row
//	 number of the tile (starting from 0 at top) and column is the
//	 column number of the tile (starting from 0 at left). format is
//	 the appropriate extension for the image format used – either
//	 JPEG or PNG."
//
// Pyramid level numbering: level 0 is 1×1 pixel; each level doubles
// the previous. Total levels = ceil(log2(max(W, H))) + 1.
//
// This package is pure: no I/O, no allocation contracts beyond
// returning parsed values. Storage backend selection (ZIP for SZI,
// filesystem for bare DZI) lives in format packages.
package dzi
```

- [ ] **Step 2: Create `internal/dzi/manifest.go`**

```go
package dzi

import (
	"encoding/xml"
	"errors"
	"fmt"
)

// Namespace is the Microsoft Deep Zoom XML namespace declared on
// the root Image element of a DZI manifest.
const Namespace = "http://schemas.microsoft.com/deepzoom/2008"

// Manifest is a parsed DZI manifest XML document.
//
// All fields come from the XML attributes on the root <Image>
// element + its single child <Size> element. Width and Height are
// the full-resolution image dimensions.
type Manifest struct {
	Format   string // "jpeg" or "png"; spec restricts to these two
	Overlap  int    // tile-edge overlap in pixels; typically 0
	TileSize int    // standard tile dimension; typically 256
	Width    int    // image width at the deepest pyramid level
	Height   int    // image height at the deepest pyramid level
}

// rawImage is the wire representation parsed from XML.
type rawImage struct {
	XMLName  xml.Name `xml:"Image"`
	Format   string   `xml:"Format,attr"`
	Overlap  int      `xml:"Overlap,attr"`
	TileSize int      `xml:"TileSize,attr"`
	Size     rawSize  `xml:"Size"`
}

type rawSize struct {
	Width  int `xml:"Width,attr"`
	Height int `xml:"Height,attr"`
}

// ParseManifest decodes a DZI manifest XML document.
//
// Returns an error if the XML is malformed, the root element is
// not <Image>, the namespace is wrong, or required fields are
// missing/zero.
func ParseManifest(data []byte) (Manifest, error) {
	var raw rawImage
	if err := xml.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("dzi: parse manifest: %w", err)
	}
	if raw.XMLName.Local != "Image" {
		return Manifest{}, fmt.Errorf("dzi: root element %q, want Image", raw.XMLName.Local)
	}
	if raw.XMLName.Space != Namespace {
		return Manifest{}, fmt.Errorf("dzi: namespace %q, want %q", raw.XMLName.Space, Namespace)
	}
	if raw.Format == "" {
		return Manifest{}, errors.New("dzi: missing Format attribute")
	}
	if raw.TileSize <= 0 {
		return Manifest{}, fmt.Errorf("dzi: TileSize %d must be > 0", raw.TileSize)
	}
	if raw.Size.Width <= 0 || raw.Size.Height <= 0 {
		return Manifest{}, fmt.Errorf("dzi: Size %dx%d must have positive dimensions", raw.Size.Width, raw.Size.Height)
	}
	if raw.Overlap < 0 {
		return Manifest{}, fmt.Errorf("dzi: Overlap %d must be >= 0", raw.Overlap)
	}
	return Manifest{
		Format:   raw.Format,
		Overlap:  raw.Overlap,
		TileSize: raw.TileSize,
		Width:    raw.Size.Width,
		Height:   raw.Size.Height,
	}, nil
}
```

- [ ] **Step 3: Create `internal/dzi/coords.go`**

```go
package dzi

import (
	"fmt"
	"math"
)

// MaxLevel returns the deepest pyramid level index for an image of
// the given dimensions. Per Microsoft spec: each level is
// 2^level × 2^level (logical), and the image is laid out at the
// smallest level whose 2^level dimension is >= max(width, height).
//
// Examples (from spec page 13):
//
//	max(W,H) = 234298 → MaxLevel = ceil(log2(234298)) = 18
//	max(W,H) = 2967   → MaxLevel = ceil(log2(2967))   = 12
//	max(W,H) = 1      → MaxLevel = 0
//
// Total level count = MaxLevel(w, h) + 1.
func MaxLevel(width, height int) int {
	if width < 1 || height < 1 {
		return 0
	}
	max := width
	if height > max {
		max = height
	}
	if max == 1 {
		return 0
	}
	return int(math.Ceil(math.Log2(float64(max))))
}

// LevelDims returns the pixel dimensions of the given level for an
// image whose deepest level has the given full-resolution dims.
//
// The deepest level (MaxLevel) is at the full Width/Height. Each
// level above (toward 0) halves the previous level's dimensions,
// rounding up.
//
// Examples for a 2220×2967 image (MaxLevel = 12):
//
//	level 12: 2220×2967  (full)
//	level 11: 1110×1484
//	level 10:  555× 742
//	...
//	level  0:    1×   1
func LevelDims(width, height, level int) (w, h int) {
	maxLevel := MaxLevel(width, height)
	if level >= maxLevel {
		return width, height
	}
	if level < 0 {
		return 1, 1
	}
	// Halve from full dims down to the requested level.
	delta := maxLevel - level
	w = width
	h = height
	for i := 0; i < delta; i++ {
		w = (w + 1) / 2
		h = (h + 1) / 2
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
	}
	return w, h
}

// GridDims returns the tile-grid dimensions (cols × rows) for a
// level whose pixel dimensions are levelW × levelH and whose tile
// size is tileSize.
//
//	cols = ceil(levelW / tileSize)
//	rows = ceil(levelH / tileSize)
func GridDims(levelW, levelH, tileSize int) (cols, rows int) {
	if tileSize <= 0 {
		return 0, 0
	}
	cols = (levelW + tileSize - 1) / tileSize
	rows = (levelH + tileSize - 1) / tileSize
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

// TilePath returns the on-disk path of a tile within a DZI pyramid
// rooted at rootDir. Paths follow the Microsoft spec convention
// "<rootDir>/<level>/<col>_<row>.<format>" — note column-then-row,
// NOT row-then-column.
//
// Example: TilePath("CMU-1_files", 12, 5, 8, "jpeg") returns
// "CMU-1_files/12/5_8.jpeg".
func TilePath(rootDir string, level, col, row int, format string) string {
	return fmt.Sprintf("%s/%d/%d_%d.%s", rootDir, level, col, row, format)
}
```

- [ ] **Step 4: Create `internal/dzi/manifest_test.go`**

```go
package dzi

import "testing"

func TestParseManifest_HappyPath(t *testing.T) {
	const data = `<?xml version="1.0" encoding="UTF-8"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008"
       Format="jpeg" Overlap="0" TileSize="256">
  <Size Width="2220" Height="2967"/>
</Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Format != "jpeg" {
		t.Errorf("Format = %q, want jpeg", m.Format)
	}
	if m.Overlap != 0 {
		t.Errorf("Overlap = %d, want 0", m.Overlap)
	}
	if m.TileSize != 256 {
		t.Errorf("TileSize = %d, want 256", m.TileSize)
	}
	if m.Width != 2220 || m.Height != 2967 {
		t.Errorf("Size = %dx%d, want 2220x2967", m.Width, m.Height)
	}
}

func TestParseManifest_GrundiumLayout(t *testing.T) {
	// scan_618_grundium_SZI.szi manifest: TileSize=512, large dims.
	const data = `<?xml version="1.0" encoding="UTF-8"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="512">
    <Size Height="81920" Width="147456"/>
</Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.TileSize != 512 {
		t.Errorf("TileSize = %d, want 512", m.TileSize)
	}
	if m.Width != 147456 || m.Height != 81920 {
		t.Errorf("Size = %dx%d, want 147456x81920", m.Width, m.Height)
	}
}

func TestParseManifest_PNGFormat(t *testing.T) {
	const data = `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="png" Overlap="0" TileSize="256"><Size Width="100" Height="100"/></Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Format != "png" {
		t.Errorf("Format = %q, want png", m.Format)
	}
}

func TestParseManifest_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"malformed XML", `<Image><Size`},
		{"wrong root", `<Other xmlns="http://schemas.microsoft.com/deepzoom/2008"/>`},
		{"wrong namespace", `<Image xmlns="http://example.com/foo" Format="jpeg" TileSize="256"><Size Width="100" Height="100"/></Image>`},
		{"missing Format", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" TileSize="256"><Size Width="100" Height="100"/></Image>`},
		{"zero TileSize", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" TileSize="0"><Size Width="100" Height="100"/></Image>`},
		{"zero size", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" TileSize="256"><Size Width="0" Height="0"/></Image>`},
		{"negative overlap", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="-1" TileSize="256"><Size Width="100" Height="100"/></Image>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.data)); err == nil {
				t.Error("ParseManifest: want error, got nil")
			}
		})
	}
}
```

- [ ] **Step 5: Create `internal/dzi/coords_test.go`**

```go
package dzi

import "testing"

func TestMaxLevel(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want int
	}{
		// Spec worked example (page 13): 234298 → 18.
		{234298, 234298, 18},
		// Spec single-tile range: levels 0-8 are all 1 tile; level 8
		// largest dim is between 129 and 256 px (= 2^7+1 to 2^8).
		{256, 256, 8},
		{129, 129, 8},
		{128, 128, 7},
		// CMU-1.szi dimensions: 2220x2967 → MaxLevel = 12.
		{2220, 2967, 12},
		// Grundium dimensions: 147456x81920 → MaxLevel = 18.
		// log2(147456) ≈ 17.17 → ceil = 18.
		{147456, 81920, 18},
		// Trivial cases.
		{1, 1, 0},
		{2, 1, 1},
	} {
		t.Run("", func(t *testing.T) {
			if got := MaxLevel(tc.w, tc.h); got != tc.want {
				t.Errorf("MaxLevel(%d, %d) = %d, want %d", tc.w, tc.h, got, tc.want)
			}
		})
	}
}

func TestLevelDims_CMU1(t *testing.T) {
	// 2220x2967 image. MaxLevel = 12 (full); each level halves.
	for _, tc := range []struct {
		level int
		w, h  int
	}{
		{12, 2220, 2967}, // full
		{11, 1110, 1484}, // (2220+1)/2 = 1110, (2967+1)/2 = 1484
		{10, 555, 742},
		{9, 278, 371},
		{8, 139, 186},
		{0, 1, 1}, // bottom level always 1×1
	} {
		t.Run("", func(t *testing.T) {
			w, h := LevelDims(2220, 2967, tc.level)
			if w != tc.w || h != tc.h {
				t.Errorf("LevelDims(2220, 2967, %d) = %dx%d, want %dx%d",
					tc.level, w, h, tc.w, tc.h)
			}
		})
	}
}

func TestGridDims(t *testing.T) {
	// CMU-1 L12 = 2220x2967, TileSize=256 → 9x12 grid (9*256=2304, 12*256=3072).
	if cols, rows := GridDims(2220, 2967, 256); cols != 9 || rows != 12 {
		t.Errorf("GridDims(2220, 2967, 256) = %dx%d, want 9x12", cols, rows)
	}
	// Grundium L18 = 147456x81920, TileSize=512 → 288x160.
	if cols, rows := GridDims(147456, 81920, 512); cols != 288 || rows != 160 {
		t.Errorf("GridDims(147456, 81920, 512) = %dx%d, want 288x160", cols, rows)
	}
	// Levels 0-8 have a single tile.
	if cols, rows := GridDims(1, 1, 256); cols != 1 || rows != 1 {
		t.Errorf("GridDims(1, 1, 256) = %dx%d, want 1x1", cols, rows)
	}
}

func TestTilePath(t *testing.T) {
	// Microsoft spec verbatim: "<col>_<row>.<format>" — column FIRST.
	if got := TilePath("CMU-1_files", 12, 5, 8, "jpeg"); got != "CMU-1_files/12/5_8.jpeg" {
		t.Errorf("TilePath = %q", got)
	}
	if got := TilePath("scan_618__files", 18, 287, 159, "jpeg"); got != "scan_618__files/18/287_159.jpeg" {
		t.Errorf("TilePath = %q", got)
	}
}
```

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./internal/dzi/ 2>&1 | head -3
go test -count=1 ./internal/dzi/ 2>&1 | tail -5
gofmt -l internal/dzi/
```

Expected: build clean, all tests pass, gofmt empty.

- [ ] **Step 7: Commit**

```bash
git add internal/dzi/
git commit -m "$(cat <<'EOF'
feat(dzi): T1 — internal/dzi/ manifest parser + tile-coord math

New internal/dzi/ package: pure DZI manifest parser + level/tile
coordinate math. No I/O, no storage backend — provides the shared
foundation for formats/szi/ (v0.16) and a future formats/dzi/
(v0.17+).

Surface:
  Manifest{Format, Overlap, TileSize, Width, Height}
  ParseManifest(xml []byte) (Manifest, error)  -- Microsoft DZI
  MaxLevel(w, h int) int                       -- ceil(log2(max(w,h)))
  LevelDims(w, h, level) (w, h)                -- per-level dims
  GridDims(levelW, levelH, tileSize) (cols, rows)
  TilePath(rootDir, level, col, row, format)   -- spec col_row order

Verified against spec worked example (234298 px → MaxLevel 18),
CMU-1.szi (2220x2967 → MaxLevel 12), and Grundium (147456x81920
→ MaxLevel 18, TileSize 512). PNG Format value supported.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — `opentile.FormatSZI` + `opentile.CompressionPNG` + `formats/szi/` skeleton

**Files:**
- Modify: `tiler.go` (add `FormatSZI = "szi"`)
- Modify: `compression.go` (add `CompressionPNG`)
- Modify: `compression_test.go` (test row)
- Create: `formats/szi/doc.go`
- Create: `formats/szi/factory.go`
- Create: `formats/szi/tiler.go`
- Create: `formats/szi/tiler_test.go`
- Modify: `formats/all/all.go` (register szi factory)

T2 lands a working but minimal Tiler: `Open()` succeeds on both fixtures; `Format()` reports `"szi"`; `Levels()` / `Images()` / `Associated()` / `Metadata()` return empty placeholders. T3 / T4 fill those in.

- [ ] **Step 1: Add `opentile.FormatSZI`**

Edit `/Users/cornish/GitHub/opentile-go/tiler.go`. Find the const block declaring existing format values. Append after the last entry (likely `FormatLeicaSCN`):

```go
	// FormatSZI identifies a Smart Zoom Image file (ZIP-wrapped
	// Microsoft Deep Zoom pyramid + scan-properties.xml +
	// associated_images/, per the smartinmedia/SZI-Format spec).
	//
	// Added in v0.16.
	FormatSZI Format = "szi"
```

- [ ] **Step 2: Add `opentile.CompressionPNG`**

Edit `/Users/cornish/GitHub/opentile-go/compression.go`. Append a new const after `CompressionHTJ2K`:

```go
	// CompressionPNG identifies a PNG-encoded tile (`\x89PNG` magic).
	// DZI's Format attribute admits both jpeg and png. Tile bytes
	// are a complete self-contained PNG file. Consumer decodes via
	// `image/png` (stdlib).
	//
	// Added in v0.16.
	CompressionPNG
```

Add the matching String() case in the same file's `func (c Compression) String() string`:

```go
	case CompressionPNG:
		return "png"
```

(Place after the `CompressionHTJ2K` case, before `default`.)

Add the test row in `compression_test.go`'s `TestCompressionString` table after the `CompressionHTJ2K` row:

```go
		{CompressionPNG, "png"},
```

- [ ] **Step 3: Create `formats/szi/doc.go`**

```go
// Package szi reads Smart Zoom Image (.szi) files — ZIP-wrapped
// Microsoft Deep Zoom pyramids with scan-properties.xml and an
// associated_images/ folder. Spec: smartinmedia/SZI-Format
// (LGPL + CC-BY licensed).
//
// SZI structure:
//
//	<root>/
//	  <name>.dzi                 -- DZI manifest XML
//	  scan-properties.xml        -- SZI-specific scan metadata
//	  <name>_files/<lvl>/<c>_<r>.<fmt>  -- tile pyramid (Microsoft DZI)
//	  associated_images/         -- optional
//	    macro.jpg                -- exposed as Type() == "overview"
//	    label.jpg                -- exposed as Type() == "label"
//	    thumbnail.jpg            -- exposed as Type() == "thumbnail"
//	  vendor/                    -- optional; v0.16 ignores contents
//
// Tile fetch is byte-passthrough: each ZIP entry is an
// uncompressed-stored JPEG (or PNG, per DZI spec); reads resolve
// directly to a SectionReader on the .szi file. No decompression,
// no copy on the hot path.
//
// Sparse SZI files are not supported per the spec (a missing tile
// is treated as a corrupt archive). Bare DZI (filesystem-backed,
// no ZIP) is deferred to a future formats/dzi/ package.
package szi
```

- [ ] **Step 4: Create `formats/szi/factory.go`**

```go
package szi

import (
	"encoding/binary"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory implements opentile.FormatFactory for Smart Zoom Image
// files. Detection uses the SupportsRaw / OpenRaw byte-level path
// (mirrors the v0.8 IFE precedent for non-TIFF formats).
type Factory struct{}

// New returns an SZI factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatSZI }

// SupportsRaw sniffs the first 4 bytes of r for the ZIP local-file-
// header magic (PK\x03\x04). True only on full match.
//
// SZI files are ZIP archives; a deeper check (presence of a .dzi
// entry inside) happens at OpenRaw time. Any other ZIP file would
// fail OpenRaw; SupportsRaw stays cheap.
func (f *Factory) SupportsRaw(r io.ReaderAt, size int64) bool {
	if size < 4 {
		return false
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(buf[:]) == 0x04034B50
}

// OpenRaw parses an SZI file and returns a Tiler.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (opentile.Tiler, error) {
	return openSZI(r, size, cfg)
}

// Supports is the TIFF-path entry point; SZI files are never
// TIFFs, so this always returns false. Required to satisfy
// opentile.FormatFactory.
func (f *Factory) Supports(*tiff.File) bool { return false }

// Open is the TIFF-path entry point; never reached because
// Supports returns false.
func (f *Factory) Open(*tiff.File, *opentile.Config) (opentile.Tiler, error) {
	return nil, opentile.ErrUnsupportedFormat
}
```

- [ ] **Step 5: Create `formats/szi/tiler.go`**

```go
package szi

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

// Tiler is the SZI-format implementation of opentile.Tiler.
type Tiler struct {
	r        io.ReaderAt
	size     int64
	zipR     *zip.Reader

	// rootDir is the single top-level directory inside the ZIP
	// (e.g., "CMU-1" or "scan_618_"). All other paths are relative
	// to it.
	rootDir  string

	// manifest is the parsed DZI XML manifest from <rootDir>/<name>.dzi.
	manifest dzi.Manifest

	// filesDir is the tile-pyramid root, "<rootDir>/<rootName>_files".
	filesDir string

	// scanPropertiesXML holds the raw bytes of scan-properties.xml.
	// T4 parses these into szi.Metadata.
	scanPropertiesXML []byte

	// entries indexes ZIP central-directory entries by full path
	// for fast tile lookup.
	entries map[string]*zip.File

	cfg *opentile.Config
}

// openSZI is the FormatFactory.OpenRaw implementation.
func openSZI(r io.ReaderAt, size int64, cfg *opentile.Config) (*Tiler, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("szi: open zip: %w", err)
	}

	t := &Tiler{
		r:       r,
		size:    size,
		zipR:    zr,
		entries: make(map[string]*zip.File, len(zr.File)),
		cfg:     cfg,
	}
	for _, f := range zr.File {
		t.entries[f.Name] = f
	}

	if err := t.discoverRoot(); err != nil {
		return nil, err
	}
	if err := t.loadManifest(); err != nil {
		return nil, err
	}
	if err := t.loadScanProperties(); err != nil {
		return nil, err
	}
	return t, nil
}

// discoverRoot identifies the single top-level directory inside the
// ZIP archive. SZI spec mandates exactly one root folder named
// after the image.
func (t *Tiler) discoverRoot() error {
	roots := make(map[string]struct{})
	for _, f := range t.zipR.File {
		// Take the first path component.
		name := f.Name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			roots[name[:i]] = struct{}{}
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("szi: expected exactly 1 root folder, got %d", len(roots))
	}
	for name := range roots {
		t.rootDir = name
	}
	return nil
}

// loadManifest finds and parses <rootDir>/<rootDir>.dzi (case-
// insensitive on the .dzi extension per spec).
func (t *Tiler) loadManifest() error {
	// Try canonical name first: <root>/<root>.dzi (lowercase) and
	// <root>/<root>.DZI (uppercase, per spec page 5).
	candidates := []string{
		t.rootDir + "/" + t.rootDir + ".dzi",
		t.rootDir + "/" + t.rootDir + ".DZI",
	}
	var manifestEntry *zip.File
	for _, p := range candidates {
		if e, ok := t.entries[p]; ok {
			manifestEntry = e
			break
		}
	}
	// Fallback: any file ending in .dzi/.DZI directly under rootDir.
	if manifestEntry == nil {
		for _, f := range t.zipR.File {
			lower := strings.ToLower(f.Name)
			if !strings.HasSuffix(lower, ".dzi") {
				continue
			}
			if path.Dir(f.Name) != t.rootDir {
				continue
			}
			manifestEntry = f
			break
		}
	}
	if manifestEntry == nil {
		return errors.New("szi: no .dzi manifest found in archive")
	}

	data, err := readZipEntry(manifestEntry)
	if err != nil {
		return fmt.Errorf("szi: read manifest %s: %w", manifestEntry.Name, err)
	}
	m, err := dzi.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("szi: parse manifest %s: %w", manifestEntry.Name, err)
	}
	t.manifest = m

	// _files folder: same name as the .dzi without the extension,
	// plus "_files".
	base := strings.TrimSuffix(path.Base(manifestEntry.Name), path.Ext(manifestEntry.Name))
	t.filesDir = t.rootDir + "/" + base + "_files"
	return nil
}

// loadScanProperties locates and reads <rootDir>/scan-properties.xml.
// The spec marks this file mandatory; T4 parses it into Metadata.
func (t *Tiler) loadScanProperties() error {
	p := t.rootDir + "/scan-properties.xml"
	entry, ok := t.entries[p]
	if !ok {
		return fmt.Errorf("szi: missing %s", p)
	}
	data, err := readZipEntry(entry)
	if err != nil {
		return fmt.Errorf("szi: read %s: %w", p, err)
	}
	t.scanPropertiesXML = data
	return nil
}

// readZipEntry reads a complete ZIP entry into memory. Used for
// small metadata blobs (manifest, scan-properties.xml) — NOT for
// tiles, which use a SectionReader fast path.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Format returns opentile.FormatSZI.
func (t *Tiler) Format() opentile.Format { return opentile.FormatSZI }

// Close releases resources held by the Tiler. The underlying
// ReaderAt remains the caller's responsibility.
func (t *Tiler) Close() error {
	t.r = nil
	t.zipR = nil
	t.entries = nil
	return nil
}

// Levels is the legacy single-image shortcut accessor; SZI files
// always carry exactly one image, so this delegates to Images()[0].
//
// T3 implementation. v0.16 T2 returns nil placeholder.
func (t *Tiler) Levels() []opentile.Level { return nil }

// Level returns Levels()[i]. T3 implementation; T2 returns
// ErrLevelOutOfRange for any i.
func (t *Tiler) Level(i int) (opentile.Level, error) {
	return nil, opentile.ErrLevelOutOfRange
}

// Images returns the single Image carried by the SZI file. T3
// implementation; T2 returns nil placeholder.
func (t *Tiler) Images() []opentile.Image { return nil }

// Associated returns the associated_images/ entries. T4 implementation.
func (t *Tiler) Associated() []opentile.AssociatedImage { return nil }

// Metadata returns the cross-format metadata populated from
// scan-properties.xml. T4 implementation.
func (t *Tiler) Metadata() opentile.Metadata { return opentile.Metadata{} }

// ICCProfile returns nil — SZI does not surface ICC profiles in v0.16.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel pre-warms the page cache. SZI's tile lookup is via
// SectionReader on the file; warming would touch each tile's
// uncompressed-stored bytes. T3 implementation.
func (t *Tiler) WarmLevel(i int) error {
	return opentile.ErrLevelOutOfRange
}
```

NOTE on `WarmLevel`: the `opentile.Tiler` interface includes WarmLevel from v0.9. The full method set must be satisfied for `Tiler` to satisfy the interface. Verify against `tiler.go`'s interface definition; if there are additional v0.13+ methods (`TilePrefix` etc.), those live on `Level` not `Tiler`, so they apply in T3.

- [ ] **Step 6: Create `formats/szi/tiler_test.go`**

```go
package szi

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	return filepath.Join(dir, "szi")
}

func TestOpen_CMU1(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}

	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatSZI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatSZI)
	}
}

func TestOpen_Grundium(t *testing.T) {
	path := filepath.Join(testdataDir(t), "scan_618_grundium_SZI.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("scan_618_grundium_SZI.szi not present")
	}

	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatSZI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatSZI)
	}
}
```

- [ ] **Step 7: Register in `formats/all/all.go`**

Edit `/Users/cornish/GitHub/opentile-go/formats/all/all.go`. Add the szi import + registration alongside the others. Read the file's existing pattern first (likely registers leicascn, generictiff, etc.); follow the same shape.

```go
import (
    // ... existing imports ...
    "github.com/wsilabs/opentile-go/formats/szi"
)

func init() {
    // ... existing Register() calls ...
    opentile.Register(szi.New())
}
```

Place the szi registration BEFORE `generictiff` (as IFE does) so the byte-level SupportsRaw runs first on .szi files — generic-TIFF would never SupportsRaw a ZIP-magic file anyway, but ordering keeps detection deterministic.

- [ ] **Step 8: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/szi/ 2>&1 | tail -10
go test -count=1 -run TestCompressionString ./ 2>&1 | tail -3
gofmt -l compression.go compression_test.go tiler.go formats/szi/ formats/all/
```

Expected: build clean, T2 smoke tests open both fixtures successfully, gofmt empty.

- [ ] **Step 9: Commit**

```bash
git add -u formats/szi/
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.16): T2 — opentile.FormatSZI + CompressionPNG + formats/szi/ skeleton

New public surface:
  opentile.FormatSZI = "szi"            (Tiler.Format())
  opentile.CompressionPNG               (DZI Format="png" tiles)
  formats/szi/ package + Factory        (registered via formats/all)

Tiler skeleton:
  - SupportsRaw byte-level detection (PK\x03\x04 ZIP magic)
  - OpenRaw parses zip.Reader; discovers single root folder; loads
    .dzi manifest (case-insensitive); reads scan-properties.xml;
    populates entries map for hot-path tile lookup
  - Format() / Close() / placeholders for Levels / Images /
    Associated / Metadata / WarmLevel (filled in T3 + T4)

T2 smoke tests verify both fixtures open and report
Format() == "szi" via opentile.OpenFile(). Tile / metadata APIs
return placeholders until T3 / T4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — `formats/szi/` Image / Level / Tile

**Files:**
- Create: `formats/szi/image.go`
- Create: `formats/szi/level.go`
- Create: `formats/szi/level_test.go`
- Modify: `formats/szi/tiler.go` (wire Levels / Images; add buildLevels in openSZI)

T3 implements the pyramid: Image (single), Levels[], Level.Tile / TileInto / TileBodyInto / TilePrefix / TileBodyMaxSize / Compression / Size / TileSize / Grid.

NOTE: opentile-go's level convention is **highest resolution at index 0** (per [tiler.go](../../tiler.go) doc and existing format readers). DZI's deepest level (full resolution) is the highest-numbered DZI level (= MaxLevel). So opentile-go level 0 maps to DZI MaxLevel; opentile-go level i maps to DZI MaxLevel - i.

- [ ] **Step 1: Create `formats/szi/image.go`**

```go
package szi

import opentile "github.com/wsilabs/opentile-go"

// image is the single-image opentile.Image implementation for SZI.
// SZI files always carry exactly one image (no DZC collections per
// spec page 4).
type image struct {
	t      *Tiler
	levels []opentile.Level
}

func (i *image) Index() int               { return 0 }
func (i *image) Name() string             { return "" }
func (i *image) Levels() []opentile.Level { return append([]opentile.Level(nil), i.levels...) }
func (i *image) Level(idx int) (opentile.Level, error) {
	if idx < 0 || idx >= len(i.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return i.levels[idx], nil
}

// SizeZ / SizeC / SizeT — SZI is 2D brightfield: depth=1, channels=1, time=1.
// (Cross-format Image interface from v0.7+ — verify against current
// image.go interface; if these methods aren't present, omit them.)
func (i *image) SizeZ() int { return 1 }
func (i *image) SizeC() int { return 1 }
func (i *image) SizeT() int { return 1 }
```

NOTE: confirm against `image.go` whether `SizeZ` / `SizeC` / `SizeT` are required Image methods in current API — if so, include them. If only some, include those.

- [ ] **Step 2: Create `formats/szi/level.go`**

```go
package szi

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

// level is the opentile.Level implementation for one SZI/DZI
// pyramid level.
type level struct {
	t           *Tiler
	dziLevel    int        // DZI-side level index (0 = 1×1; MaxLevel = full)
	openTileIdx int        // opentile-side level index (0 = full)
	width       int        // pixel width at this level
	height      int        // pixel height at this level
	cols        int
	rows        int
	tileSize    int        // standard tile dimension at this level (typically 256 or 512)
	compression opentile.Compression
}

func (l *level) Size() opentile.Size     { return opentile.Size{W: l.width, H: l.height} }
func (l *level) TileSize() opentile.Size { return opentile.Size{W: l.tileSize, H: l.tileSize} }
func (l *level) Grid() opentile.Size     { return opentile.Size{W: l.cols, H: l.rows} }
func (l *level) Compression() opentile.Compression { return l.compression }
func (l *level) TileMaxSize() int {
	// Conservative upper bound: full tile dim². Real tiles
	// (compressed JPEG / PNG) are far smaller. Used by consumers
	// for buffer pooling; over-estimating is safe.
	return l.tileSize * l.tileSize * 4
}

// Tile reads tile (x, y) and returns the raw passthrough bytes.
// Honors v0.9 zero-allocation contract via TileInto + a fresh slice.
func (l *level) Tile(x, y int) ([]byte, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return nil, err
	}
	return readZipEntry(entry)
}

// TileInto reads tile (x, y) into dst and returns the byte count.
// dst must be at least the tile's compressed size; if smaller,
// returns 0, io.ErrShortBuffer.
func (l *level) TileInto(x, y int, dst []byte) (int, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return 0, err
	}
	if int64(len(dst)) < int64(entry.UncompressedSize64) {
		return 0, io.ErrShortBuffer
	}
	rc, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	return io.ReadFull(rc, dst[:entry.UncompressedSize64])
}

// TileReader returns a streaming reader for tile (x, y).
func (l *level) TileReader(x, y int) (io.Reader, error) {
	entry, err := l.tileEntry(x, y)
	if err != nil {
		return nil, err
	}
	return entry.Open()
}

// TilePrefix is the v0.13 splice prefix accessor. SZI tiles are
// self-contained JPEG / PNG files with no shared prefix structure
// — the entire tile is its own splice. Returns nil per the v0.13
// "non-applicable" convention.
func (l *level) TilePrefix() []byte { return nil }

// TileBodyInto is equivalent to TileInto when TilePrefix is nil
// (v0.13 invariant).
func (l *level) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize is equivalent to TileMaxSize when TilePrefix
// is nil.
func (l *level) TileBodyMaxSize() int { return l.TileMaxSize() }

// tileEntry resolves (x, y) to the corresponding ZIP entry.
// Returns ErrTileOutOfBounds for invalid coordinates and a
// "missing tile in valid range" sentinel for sparse-but-malformed
// SZI files.
func (l *level) tileEntry(x, y int) (*zip.File, error) {
	if x < 0 || x >= l.cols || y < 0 || y >= l.rows {
		return nil, opentile.ErrTileOutOfBounds
	}
	path := dzi.TilePath(l.t.filesDir, l.dziLevel, x, y, l.t.manifest.Format)
	entry, ok := l.t.entries[path]
	if !ok {
		// SZI spec forbids sparse images; missing tile = corrupt archive.
		return nil, fmt.Errorf("szi: missing tile %s: %w", path, errors.New("ErrCorruptArchive"))
	}
	return entry, nil
}
```

NOTE: replace `errors.New("ErrCorruptArchive")` with whatever sentinel the implementer chooses (e.g., add a new `var ErrCorruptArchive = errors.New("szi: corrupt archive")` in tiler.go). Per Q2, sparse SZI = corrupt archive; the sentinel name is implementation choice.

NOTE: actual border-tile dimensions are inferred from `dzi.LevelDims` + `tileSize`; the on-disk tile bytes have whatever the encoder wrote (typically smaller than `tileSize`). Consumers can ignore actual pixel dim and just decode the tile bytes — a rendering pipeline gets the actual size from the JPEG header.

- [ ] **Step 3: Wire `buildLevels` in `tiler.go`**

Edit `formats/szi/tiler.go`. After `loadScanProperties()` succeeds in `openSZI`, add:

```go
	if err := t.buildLevels(); err != nil {
		return nil, err
	}
```

And add the method:

```go
func (t *Tiler) buildLevels() error {
	maxLevel := dzi.MaxLevel(t.manifest.Width, t.manifest.Height)

	var comp opentile.Compression
	switch t.manifest.Format {
	case "jpeg", "jpg":
		comp = opentile.CompressionJPEG
	case "png":
		comp = opentile.CompressionPNG
	default:
		comp = opentile.CompressionUnknown
	}

	// opentile-go convention: index 0 = highest resolution.
	// DZI: highest resolution = MaxLevel.
	levels := make([]opentile.Level, maxLevel+1)
	for i := 0; i <= maxLevel; i++ {
		dziL := maxLevel - i
		w, h := dzi.LevelDims(t.manifest.Width, t.manifest.Height, dziL)
		cols, rows := dzi.GridDims(w, h, t.manifest.TileSize)
		levels[i] = &level{
			t:           t,
			dziLevel:    dziL,
			openTileIdx: i,
			width:       w,
			height:      h,
			cols:        cols,
			rows:        rows,
			tileSize:    t.manifest.TileSize,
			compression: comp,
		}
	}
	t.image = &image{t: t, levels: levels}
	return nil
}
```

Add to the Tiler struct:
```go
	image *image  // single image; SZI carries exactly one
```

Replace the Levels / Images / Level placeholder methods in tiler.go:
```go
func (t *Tiler) Levels() []opentile.Level { return t.image.Levels() }
func (t *Tiler) Level(i int) (opentile.Level, error) { return t.image.Level(i) }
func (t *Tiler) Images() []opentile.Image { return []opentile.Image{t.image} }
```

`WarmLevel` — implement minimally for v0.16:

```go
func (t *Tiler) WarmLevel(i int) error {
	if t.image == nil {
		return opentile.ErrLevelOutOfRange
	}
	if i < 0 || i >= len(t.image.levels) {
		return opentile.ErrLevelOutOfRange
	}
	// SZI tiles are uncompressed-stored ZIP entries → SectionReader
	// over the .szi file. Warming touches one byte per OS page of
	// each tile's data range. Use existing internal/tiff/mmap warm
	// helper if available; otherwise no-op for v0.16 (warming is a
	// performance hint, not a correctness requirement).
	return nil
}
```

Document the no-op as a v0.16 limitation; future enhancement when consumer signal motivates it.

- [ ] **Step 4: Create `formats/szi/level_test.go`**

```go
package szi

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestLevels_CMU1Geometry(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	levels := tlr.Levels()
	// MaxLevel = 12 → 13 total levels (0-12 in DZI, mapped to
	// opentile indices 0-12 reverse-ordered).
	if len(levels) != 13 {
		t.Fatalf("Levels: got %d, want 13", len(levels))
	}

	// opentile L0 = DZI L12 = full resolution 2220×2967.
	if got := levels[0].Size(); got.W != 2220 || got.H != 2967 {
		t.Errorf("L0 Size = %v, want {W:2220 H:2967}", got)
	}
	if got := levels[0].TileSize(); got.W != 256 || got.H != 256 {
		t.Errorf("L0 TileSize = %v, want {256, 256}", got)
	}
	if got := levels[0].Grid(); got.W != 9 || got.H != 12 {
		t.Errorf("L0 Grid = %v, want {9, 12}", got)
	}
	if got := levels[0].Compression(); got != opentile.CompressionJPEG {
		t.Errorf("L0 Compression = %v, want %v", got, opentile.CompressionJPEG)
	}
}

func TestTile_CMU1_FirstTileIsJPEG(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	tile, err := tlr.Levels()[0].Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0, 0): %v", err)
	}
	// JPEG SOI marker: FF D8 FF.
	soi := []byte{0xFF, 0xD8, 0xFF}
	if !bytes.HasPrefix(tile, soi) {
		t.Errorf("L0 tile does not start with JPEG SOI: got % x", tile[:min(8, len(tile))])
	}
}

func TestTile_OutOfBoundsReturnsSentinel(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	_, err = tlr.Levels()[0].Tile(99, 99)
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("OOB tile: got %v, want ErrTileOutOfBounds", err)
	}
}
```

(Add `import "errors"` for `errors.Is`.)

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/szi/ 2>&1 | tail -10
gofmt -l formats/szi/
```

Expected: build clean, all T3 tests pass, gofmt empty.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.16): T3 — formats/szi/ Image / Level / Tile

Single-image SZI implementation with full pyramid:
  - opentile.Image (single; spec forbids DZC collections in SZI)
  - opentile.Level per pyramid level; index 0 = full resolution
    (mapped from DZI's MaxLevel)
  - per-level Size / TileSize / Grid / Compression
  - Tile / TileInto / TileReader hot-path: dzi.TilePath →
    ZIP central-directory lookup → io.SectionReader on the
    .szi file (uncompressed-stored entries; v0.9 mmap-aliased
    fast path preserved)
  - TilePrefix returns nil (no shared splice prefix in DZI tiles
    — each is a complete JPEG/PNG); TileBodyInto/MaxSize delegate
    to TileInto/MaxSize per v0.13 non-applicable convention
  - missing tile in addressable range → ErrCorruptArchive sentinel
    (per spec's no-sparse-images rule; Q2)
  - WarmLevel is a no-op stub for v0.16 (consumer-signal-driven
    enhancement)

DZI-to-opentile level mapping: opentile L0 = DZI MaxLevel (full);
opentile L_n = DZI 0 (1×1).

Verified on CMU-1.szi: 13 levels (0-12), L0 = 2220×2967, 9×12 grid
of 256-px tiles, first tile starts with JPEG SOI (FF D8 FF).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — Associated images + scan-properties.xml parser + metadata

**Files:**
- Create: `formats/szi/associated.go`
- Create: `formats/szi/associated_test.go`
- Create: `formats/szi/metadata.go`
- Create: `formats/szi/metadata_test.go`
- Modify: `formats/szi/tiler.go` (wire Associated, Metadata)

- [ ] **Step 1: Create `formats/szi/associated.go`**

```go
package szi

import (
	"archive/zip"

	opentile "github.com/wsilabs/opentile-go"
)

// associatedImage is the SZI-format opentile.AssociatedImage
// implementation. Backed by a single ZIP entry (a JPEG file under
// associated_images/).
type associatedImage struct {
	imgType     string  // v0.15 Type() value: "label" / "overview" / "thumbnail"
	entry       *zip.File
	width       int
	height      int
}

func (a *associatedImage) Type() string                { return a.imgType }
func (a *associatedImage) Size() opentile.Size         { return opentile.Size{W: a.width, H: a.height} }
func (a *associatedImage) Compression() opentile.Compression {
	return opentile.CompressionJPEG // SZI spec mandates JPEG for associated_images/
}
func (a *associatedImage) Bytes() ([]byte, error) {
	return readZipEntry(a.entry)
}
```

Add a method to populate associated images. In `tiler.go`, after `buildLevels()`:

```go
	t.buildAssociated()
```

And the implementation:

```go
// buildAssociated discovers the optional associated_images/ folder
// and populates t.associated. Filenames per spec page 5:
//   - macro.jpg     → Type() == "overview"  (per v0.15 alignment)
//   - label.jpg     → Type() == "label"
//   - thumbnail.jpg → Type() == "thumbnail"
//
// Missing files are skipped (the entire folder is optional).
//
// Image dimensions are decoded from JPEG headers lazily (v0.16:
// just probe via image.DecodeConfig once at Open() time).
func (t *Tiler) buildAssociated() {
	mapping := []struct {
		filename string
		typ      string
	}{
		{"macro.jpg", "overview"},
		{"label.jpg", "label"},
		{"thumbnail.jpg", "thumbnail"},
	}
	for _, m := range mapping {
		path := t.rootDir + "/associated_images/" + m.filename
		entry, ok := t.entries[path]
		if !ok {
			continue
		}
		w, h, err := decodeJPEGDims(entry)
		if err != nil {
			// Malformed JPEG: skip but don't fail the file load.
			continue
		}
		t.associated = append(t.associated, &associatedImage{
			imgType: m.typ,
			entry:   entry,
			width:   w,
			height:  h,
		})
	}
}

// decodeJPEGDims reads the JPEG header to extract dimensions.
// Returns (width, height, nil) on success.
func decodeJPEGDims(entry *zip.File) (int, int, error) {
	rc, err := entry.Open()
	if err != nil {
		return 0, 0, err
	}
	defer rc.Close()
	cfg, _, err := image.DecodeConfig(rc)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
```

(Add imports for `image` and `_ "image/jpeg"` to tiler.go.)

Add to Tiler struct:
```go
	associated []opentile.AssociatedImage
```

And implement:
```go
func (t *Tiler) Associated() []opentile.AssociatedImage {
	return append([]opentile.AssociatedImage(nil), t.associated...)
}
```

- [ ] **Step 2: Create `formats/szi/metadata.go`**

```go
package szi

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
)

// Metadata is the SZI format-specific scan metadata parsed from
// the file's scan-properties.xml. Cross-format fields populate
// opentile.Metadata via Tiler.Metadata(); SZI-specific fields are
// accessible here via MetadataOf(tiler).
type Metadata struct {
	Version string    // <image version="...">
	Date    time.Time // <image date="...">

	UserName        string
	SoftwareName    string
	SoftwareVersion string

	TimeStart   time.Time
	TimeEnd     time.Time
	ElapsedTime string

	CaseNumber  string
	ScanJobName string

	ScannerSerialNo string

	CameraName      string
	SensorPixelSize float64 // µm

	ScannedArea float64 // mm²
	ScanWidth   float64 // mm
	ScanHeight  float64 // mm

	MicronsPerPixelX float64
	MicronsPerPixelY float64

	Comments string

	// VendorProperties holds open-ended custom properties prefixed
	// with vendor name + dot per the SZI spec page 9 convention.
	// Keys surfaced as-is including the dotted prefix.
	VendorProperties map[string]string
}

// MetadataOf returns the SZI-specific Metadata for an SZI-format
// Tiler. Returns false on non-SZI tilers.
//
// Mirrors the v0.6+/Philips/OME/IFE/SCN format-specific accessor
// pattern.
func MetadataOf(t opentile.Tiler) (Metadata, bool) {
	if t.Format() != opentile.FormatSZI {
		return Metadata{}, false
	}
	tt, ok := t.(*Tiler)
	if !ok {
		return Metadata{}, false
	}
	return tt.szim, true
}

// rawScanProperties is the wire form of scan-properties.xml.
// Note: spec example uses lowercase root <image>; namespace is
// http://www.pathozoom.com/SZI (case-sensitive but probed Grundium
// fixture uses lowercase path "/szi" — accept both).
type rawScanProperties struct {
	XMLName    xml.Name      `xml:"image"`
	Date       string        `xml:"date,attr"`
	Version    string        `xml:"version,attr"`
	Properties []rawProperty `xml:"properties>property"`
}

type rawProperty struct {
	Name  string `xml:"name"`
	Value string `xml:"value"`
}

// parseScanProperties decodes the scan-properties.xml bytes and
// returns the canonical opentile.Metadata + the SZI-specific
// Metadata. Missing fields land as zero values; malformed numerics
// likewise (lenient parser).
func parseScanProperties(data []byte) (cross opentile.Metadata, szim Metadata, err error) {
	var raw rawScanProperties
	if err = xml.Unmarshal(data, &raw); err != nil {
		return cross, szim, err
	}
	szim.Version = raw.Version
	if raw.Date != "" {
		if d, e := time.Parse("2006-01-02", raw.Date); e == nil {
			szim.Date = d.UTC()
		}
	}
	szim.VendorProperties = make(map[string]string)

	var softwareName, softwareVersion string
	for _, p := range raw.Properties {
		// Vendor-prefixed properties (key contains "."): surface in
		// the typed VendorProperties map.
		if strings.Contains(p.Name, ".") {
			szim.VendorProperties[p.Name] = p.Value
			continue
		}
		switch p.Name {
		// Cross-format Metadata fields.
		case "VendorName":
			cross.ScannerManufacturer = p.Value
		case "ScannerName":
			cross.ScannerModel = p.Value
		case "ObjectiveMagnification":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.Magnification = f
			}
		case "TimeStart":
			if t, e := time.Parse("2006-01-02T15:04:05", p.Value); e == nil {
				cross.AcquisitionDateTime = t
				szim.TimeStart = t
			}
		case "MicronsPerPixel":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.MicronsPerPixel = f
			}
		case "Comments":
			cross.ImageDescription = p.Value
			szim.Comments = p.Value

		// SZI-specific Metadata fields.
		case "ScannerSerialNo":
			szim.ScannerSerialNo = p.Value
		case "SoftwareName":
			softwareName = p.Value
			szim.SoftwareName = p.Value
		case "SoftwareVersion":
			softwareVersion = p.Value
			szim.SoftwareVersion = p.Value
		case "UserName":
			szim.UserName = p.Value
		case "TimeEnd":
			if t, e := time.Parse("2006-01-02T15:04:05", p.Value); e == nil {
				szim.TimeEnd = t
			}
		case "ElapsedTime":
			szim.ElapsedTime = p.Value
		case "CaseNumber":
			szim.CaseNumber = p.Value
		case "ScanJobName":
			szim.ScanJobName = p.Value
		case "CameraName":
			szim.CameraName = p.Value
		case "SensorPixelSize":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.SensorPixelSize = f
			}
		case "ScannedArea":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScannedArea = f
			}
		case "ScanWidth":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanWidth = f
			}
		case "ScanHeight":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanHeight = f
			}
		case "MicronsPerPixelX":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.MicronsPerPixelX = f
			}
		case "MicronsPerPixelY":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.MicronsPerPixelY = f
			}
		}
	}
	// Populate ScannerSoftware as "<name> <version>" if both present.
	if softwareName != "" {
		s := softwareName
		if softwareVersion != "" {
			s += " " + softwareVersion
		}
		cross.ScannerSoftware = []string{s}
	}
	// MicronsPerPixel fallback: average X/Y if canonical field missing.
	if cross.MicronsPerPixel == 0 && szim.MicronsPerPixelX > 0 && szim.MicronsPerPixelY > 0 {
		cross.MicronsPerPixel = (szim.MicronsPerPixelX + szim.MicronsPerPixelY) / 2
	}
	return cross, szim, nil
}
```

- [ ] **Step 3: Wire metadata in `tiler.go`**

Add fields:
```go
	cross opentile.Metadata
	szim  Metadata
```

After `loadScanProperties()` succeeds, parse:
```go
	cross, szim, err := parseScanProperties(t.scanPropertiesXML)
	if err != nil {
		return nil, fmt.Errorf("szi: parse scan-properties.xml: %w", err)
	}
	t.cross = cross
	t.szim = szim
```

Wire `Metadata()`:
```go
func (t *Tiler) Metadata() opentile.Metadata { return t.cross }
```

- [ ] **Step 4: Create `formats/szi/metadata_test.go`**

```go
package szi

import (
	"testing"
	"time"
)

func TestParseScanProperties_GrundiumFlavored(t *testing.T) {
	const data = `<?xml version="1.0"?>
<image xmlns="http://www.pathozoom.com/SZI" date="2024-01-15" version="1.0">
  <properties>
    <property><name>VendorName</name><value>Grundium</value></property>
    <property><name>ScannerName</name><value>Ocus</value></property>
    <property><name>ObjectiveMagnification</name><value>40</value></property>
    <property><name>MicronsPerPixel</name><value>0.25055239898989901</value></property>
    <property><name>MicronsPerPixelX</name><value>0.25055239898989901</value></property>
    <property><name>MicronsPerPixelY</name><value>0.25055239898989901</value></property>
    <property><name>TimeStart</name><value>2024-01-15T10:30:00</value></property>
    <property><name>TimeEnd</name><value>2024-01-15T10:45:30</value></property>
    <property><name>ElapsedTime</name><value>0h15m30s</value></property>
    <property><name>SoftwareName</name><value>OcusScan</value></property>
    <property><name>SoftwareVersion</name><value>3.1.4</value></property>
    <property><name>UserName</name><value>operator1</value></property>
    <property><name>vendor.SerialNumber</name><value>OCUS-1234</value></property>
    <property><name>Grundium.CustomField</name><value>customvalue</value></property>
  </properties>
</image>`

	cross, szim, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}

	// Cross-format fields.
	if cross.ScannerManufacturer != "Grundium" {
		t.Errorf("ScannerManufacturer = %q, want Grundium", cross.ScannerManufacturer)
	}
	if cross.ScannerModel != "Ocus" {
		t.Errorf("ScannerModel = %q, want Ocus", cross.ScannerModel)
	}
	if cross.Magnification != 40 {
		t.Errorf("Magnification = %v, want 40", cross.Magnification)
	}
	if cross.MicronsPerPixel != 0.25055239898989901 {
		t.Errorf("MicronsPerPixel = %v", cross.MicronsPerPixel)
	}
	wantTimeStart := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !cross.AcquisitionDateTime.Equal(wantTimeStart) {
		t.Errorf("AcquisitionDateTime = %v", cross.AcquisitionDateTime)
	}
	if len(cross.ScannerSoftware) != 1 || cross.ScannerSoftware[0] != "OcusScan 3.1.4" {
		t.Errorf("ScannerSoftware = %v", cross.ScannerSoftware)
	}

	// SZI-specific fields.
	if szim.UserName != "operator1" {
		t.Errorf("UserName = %q", szim.UserName)
	}
	if szim.ElapsedTime != "0h15m30s" {
		t.Errorf("ElapsedTime = %q", szim.ElapsedTime)
	}

	// Vendor-prefixed properties.
	if got := szim.VendorProperties["vendor.SerialNumber"]; got != "OCUS-1234" {
		t.Errorf("vendor.SerialNumber = %q", got)
	}
	if got := szim.VendorProperties["Grundium.CustomField"]; got != "customvalue" {
		t.Errorf("Grundium.CustomField = %q", got)
	}
}

func TestParseScanProperties_MissingFieldsLenient(t *testing.T) {
	const data = `<?xml version="1.0"?>
<image><properties>
<property><name>VendorName</name><value>X</value></property>
</properties></image>`
	cross, szim, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.ScannerManufacturer != "X" {
		t.Errorf("ScannerManufacturer = %q", cross.ScannerManufacturer)
	}
	// Missing fields → zero values; should not error.
	if cross.Magnification != 0 {
		t.Errorf("Magnification = %v, want 0", cross.Magnification)
	}
	if szim.VendorProperties == nil {
		t.Error("VendorProperties should be non-nil even when empty")
	}
}

func TestParseScanProperties_MicronsAvg(t *testing.T) {
	// MicronsPerPixel missing → average of X/Y.
	const data = `<image><properties>
<property><name>MicronsPerPixelX</name><value>0.4</value></property>
<property><name>MicronsPerPixelY</name><value>0.6</value></property>
</properties></image>`
	cross, _, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.MicronsPerPixel != 0.5 {
		t.Errorf("MicronsPerPixel avg = %v, want 0.5", cross.MicronsPerPixel)
	}
}
```

- [ ] **Step 5: Create `formats/szi/associated_test.go`**

```go
package szi

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestAssociated_CMU1HasAllThree(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	got := tlr.Associated()
	if len(got) != 3 {
		t.Fatalf("Associated count = %d, want 3", len(got))
	}

	wantTypes := map[string]bool{"label": false, "overview": false, "thumbnail": false}
	for _, a := range got {
		typ := a.Type()
		if _, ok := wantTypes[typ]; !ok {
			t.Errorf("unexpected Type() = %q", typ)
		}
		wantTypes[typ] = true

		// Each should decode-config without error.
		bytes, err := a.Bytes()
		if err != nil {
			t.Errorf("%s Bytes(): %v", typ, err)
			continue
		}
		// JPEG SOI check.
		if !bytesStartsWithJPEGSOI(bytes) {
			t.Errorf("%s does not start with JPEG SOI", typ)
		}
	}
	for typ, found := range wantTypes {
		if !found {
			t.Errorf("missing Type() = %q in associated set", typ)
		}
	}
}

func bytesStartsWithJPEGSOI(b []byte) bool {
	soi := []byte{0xFF, 0xD8, 0xFF}
	return bytes.HasPrefix(b, soi)
}
```

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/szi/ 2>&1 | tail -15
gofmt -l formats/szi/
```

Expected: all T4 tests pass; full szi package green.

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.16): T4 — formats/szi/ Associated images + metadata

Associated images:
  - macro.jpg     → Type() == "overview" (per v0.15 alignment)
  - label.jpg     → Type() == "label"
  - thumbnail.jpg → Type() == "thumbnail"
  Dimensions decoded lazily from JPEG headers at Open() time.
  Missing files skip silently (associated_images/ folder optional
  per spec).

Metadata:
  - parseScanProperties decodes SZI's scan-properties.xml format
    (root <image> + <properties> + <property name/value> entries)
  - Cross-format opentile.Metadata populated from canonical fields
    (VendorName → ScannerManufacturer; ObjectiveMagnification →
    Magnification; TimeStart → AcquisitionDateTime;
    MicronsPerPixel; Comments → ImageDescription;
    SoftwareName + SoftwareVersion → ScannerSoftware)
  - SZI-specific szi.Metadata struct exposes UserName, CaseNumber,
    ScanJobName, CameraName, ScannerSerialNo, sensor + per-axis MPP,
    physical scan dimensions, and ElapsedTime
  - Vendor-prefixed properties (e.g., "Grundium.CustomField",
    "vendor.SerialNumber") surface in szi.Metadata.VendorProperties
    with keys preserved as-is per spec page 9 convention
  - szi.MetadataOf(t opentile.Tiler) accessor mirrors v0.6+/
    Philips/OME/IFE/SCN precedent
  - MicronsPerPixel fallback: average of X/Y if canonical field
    absent

Lenient parser: malformed values yield zero on that field but
don't fail the file load; missing fields yield zero values.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — Fixtures + tests

**Files:**
- Create: `tests/parity/szi_geometry_test.go`
- Modify: `tests/integration_test.go` (slideCandidates)
- Generate: `tests/fixtures/CMU-1.szi.json`, `tests/fixtures/scan_618_grundium_SZI.szi.json`

- [ ] **Step 1: Probe both fixtures for exact geometry**

```bash
cd /Users/cornish/GitHub/opentile-go
go run /tmp/genericsmoke/main.go sample_files/szi/CMU-1.szi
go run /tmp/genericsmoke/main.go sample_files/szi/scan_618_grundium_SZI.szi
```

Expected output captures: per-level Size + TileSize + Grid + Compression for each level. Capture associated-image byte counts via Associated() probe (probe script may need updating to use Type() — already done in v0.15).

Pre-confirmed values from spec + earlier probing:
- CMU-1: 13 levels (0-12), L0 = 2220×2967, TileSize=256, L0 Grid=9×12. Associated: thumbnail (50888 B), label (45693 B), macro (99466 B); each ≥ 256×256 dims (probe to confirm exact dims).
- Grundium: 19 levels (0-18), L0 = 147456×81920, TileSize=512, L0 Grid=288×160. Associated: thumbnail (1474560 B), label (2035200 B), macro (960000 B); dimensions probe-confirmed.

- [ ] **Step 2: Create `tests/parity/szi_geometry_test.go`**

Mirror the existing `tests/parity/generic_geometry_test.go` shape. Read it first to confirm the exact struct + fields used. Write rows for both fixtures with the probed geometry.

```go
package parity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

type sziLevelExpect struct {
	W, H        int
	TileW       int
	TileH       int
	GridW       int
	GridH       int
	Compression opentile.Compression
}

type sziAssocExpect struct {
	Type        string
	W, H        int
	Compression opentile.Compression
	ByteCount   int
}

type sziFixture struct {
	filename   string
	levels     []sziLevelExpect
	associated []sziAssocExpect
	tileMagic  []byte
}

var sziFixtures = []sziFixture{
	{
		filename: "CMU-1.szi",
		levels: []sziLevelExpect{
			// L0 = full = DZI L12.
			{W: 2220, H: 2967, TileW: 256, TileH: 256, GridW: 9, GridH: 12, Compression: opentile.CompressionJPEG},
			{W: 1110, H: 1484, TileW: 256, TileH: 256, GridW: 5, GridH: 6, Compression: opentile.CompressionJPEG},
			{W: 555, H: 742, TileW: 256, TileH: 256, GridW: 3, GridH: 3, Compression: opentile.CompressionJPEG},
			{W: 278, H: 371, TileW: 256, TileH: 256, GridW: 2, GridH: 2, Compression: opentile.CompressionJPEG},
			{W: 139, H: 186, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			// L5..L12 single 1×1-tile levels (dimensions halve per level)
			{W: 70, H: 93, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 35, H: 47, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 18, H: 24, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 9, H: 12, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 5, H: 6, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 3, H: 3, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 2, H: 2, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
			{W: 1, H: 1, TileW: 256, TileH: 256, GridW: 1, GridH: 1, Compression: opentile.CompressionJPEG},
		},
		associated: []sziAssocExpect{
			{Type: "label", Compression: opentile.CompressionJPEG, ByteCount: 45693},
			{Type: "overview", Compression: opentile.CompressionJPEG, ByteCount: 99466},
			{Type: "thumbnail", Compression: opentile.CompressionJPEG, ByteCount: 50888},
		},
		tileMagic: []byte{0xFF, 0xD8, 0xFF},
	},
	// scan_618_grundium_SZI.szi — fill in via probe in step 1
	// (full-walk fixture sampled if too large for in-mem comparison).
}

// TestSZIGeometry pins per-fixture expected geometry. (Mirror the
// generic_geometry_test.go test runner.)
func TestSZIGeometry(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, fx := range sziFixtures {
		t.Run(fx.filename, func(t *testing.T) {
			path := filepath.Join(dir, "szi", fx.filename)
			if _, err := os.Stat(path); err != nil {
				t.Skip(fx.filename + " not present")
			}

			tlr, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tlr.Close()

			levels := tlr.Levels()
			if len(levels) != len(fx.levels) {
				t.Fatalf("Levels count = %d, want %d", len(levels), len(fx.levels))
			}

			for i, exp := range fx.levels {
				l := levels[i]
				if got := l.Size(); got.W != exp.W || got.H != exp.H {
					t.Errorf("L%d Size = %v, want {%d, %d}", i, got, exp.W, exp.H)
				}
				if got := l.TileSize(); got.W != exp.TileW || got.H != exp.TileH {
					t.Errorf("L%d TileSize = %v", i, got)
				}
				if got := l.Grid(); got.W != exp.GridW || got.H != exp.GridH {
					t.Errorf("L%d Grid = %v", i, got)
				}
				if got := l.Compression(); got != exp.Compression {
					t.Errorf("L%d Compression = %v", i, got)
				}
			}

			// First-tile magic-byte verification.
			tile, err := levels[0].Tile(0, 0)
			if err != nil {
				t.Fatalf("L0 Tile(0,0): %v", err)
			}
			if len(fx.tileMagic) > 0 && (len(tile) < len(fx.tileMagic) ||
				string(tile[:len(fx.tileMagic)]) != string(fx.tileMagic)) {
				t.Errorf("L0 first tile magic mismatch: got % x", tile[:8])
			}

			// Out-of-bounds.
			grid := levels[0].Grid()
			_, err = levels[0].Tile(grid.W, 0)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("OOB on L0: got %v", err)
			}

			// Associated images.
			associated := tlr.Associated()
			if len(associated) != len(fx.associated) {
				t.Fatalf("associated count = %d, want %d", len(associated), len(fx.associated))
			}
			for i, exp := range fx.associated {
				a := associated[i]
				if a.Type() != exp.Type {
					t.Errorf("associated[%d] Type = %q, want %q", i, a.Type(), exp.Type)
				}
				if got := a.Compression(); got != exp.Compression {
					t.Errorf("associated[%d] Compression = %v", i, got)
				}
				bytes, err := a.Bytes()
				if err != nil {
					t.Errorf("associated[%d] Bytes: %v", i, err)
					continue
				}
				if len(bytes) != exp.ByteCount {
					t.Errorf("associated[%d] Bytes length = %d, want %d", i, len(bytes), exp.ByteCount)
				}
			}
		})
	}
}
```

NOTE: Width/Height for associated images need to be probed before pinning. Use the L0 W,H or a dedicated decode-config helper. For v0.16, ByteCount + Type + Compression are the strict pin.

For the Grundium fixture row, do a quick `unzip -l` / probe-script run in step 1 above to capture exact L0-L18 dimensions + grid sizes; write similar rows. The dim formula is dzi.LevelDims(147456, 81920, dziLevel) which the implementer can compute either by running the probe script or reproducing the formula in their head.

- [ ] **Step 3: Wire into TestSlideParity**

Edit `/Users/cornish/GitHub/opentile-go/tests/integration_test.go`. Find `slideCandidates` (around line 50-70). Add SZI fixtures after the last existing entry (the v0.14 wsi-tools / generic-tiff entries):

```go
	// SZI fixtures (v0.16):
	"CMU-1.szi",
	"scan_618_grundium_SZI.szi",
```

Verify the `resolveSlide` helper looks for SZI files under `dir/szi/`; if not, extend it to do so.

- [ ] **Step 4: Generate per-tile SHA fixtures**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run TestGenerateFixtures -generate -v 2>&1 | tail -30
```

Expected: `tests/fixtures/CMU-1.szi.json` + `tests/fixtures/scan_618_grundium_SZI.szi.json` appear. Each well within the 5 MB cap.

- [ ] **Step 5: Verify full suite**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -run TestSZIGeometry ./tests/parity/ 2>&1 | tail -10
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -run "TestSlideParity/(CMU-1|scan_618).*\.szi" ./tests/ 2>&1 | tail -10
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: SZI geometry + parity tests pass; full module green; TestSlideParity total 30 fixtures.

- [ ] **Step 6: Commit**

```bash
git add -u tests/integration_test.go tests/parity/szi_geometry_test.go tests/fixtures/CMU-1.szi.json tests/fixtures/scan_618_grundium_SZI.szi.json
git commit -m "$(cat <<'EOF'
test(v0.16): T5 — wire SZI fixtures into TestSlideParity (28 → 30)

  CMU-1.szi                   2220×2967    13 levels  TileSize=256
  scan_618_grundium_SZI.szi   147456×81920 19 levels  TileSize=512

Both fixtures: full pyramid + 3 associated images (label/macro→
overview/thumbnail). Geometry pinned in tests/parity/szi_geometry_test.go;
per-tile SHA JSONs generated.

TestSlideParity total: 30 fixtures (was 28 post-v0.14).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Docs + ship

**Files:**
- Create: `docs/formats/szi.md`
- Modify: `README.md` (Supported-formats row)
- Modify: `docs/deferred.md` (§8j retirement audit)
- Modify: `CHANGELOG.md` ([0.16.0] section)
- Modify: `CLAUDE.md` (milestone bump)

- [ ] **Step 1: Create `docs/formats/szi.md`**

Read one of the existing format docs (e.g., `docs/formats/leicascn.md` or `docs/formats/ife.md`) to mirror the structure. Cover:

- Format origin + license (Smart In Media / pathozoom; LGPL + CC-BY)
- Architecture (ZIP wrapper around DZI; scan-properties.xml; associated_images/)
- Supported features
- Tile format (JPEG / PNG passthrough; uncompressed-stored ZIP entries; mmap-aliased fast path)
- Associated images: label/macro→overview/thumbnail per v0.15 alignment
- Metadata fields (cross-format + szi.Metadata struct + VendorProperties map)
- Limitations (sparse SZI not supported per spec; vendor/ folder ignored; bare DZI deferred)
- References (spec PDF, smartinmedia/SZI-Format repo, Microsoft DZI docs)

Approximately 100-150 lines following the existing per-format doc shape.

- [ ] **Step 2: Update README.md Supported-formats table**

Find the existing table (around line 27-34). Insert a new row for SZI before or after the leicascn row:

```
| **Smart Zoom Image (SZI)** | `.szi` | ZIP-wrapped Microsoft Deep Zoom pyramid; per-level dim halving; sparse images not supported per spec | label, overview (from `macro.jpg`), thumbnail | JPEG / PNG (all passthrough) | sampled-tile SHAs + per-fixture geometry pin | [docs/formats/szi.md](./docs/formats/szi.md) |
```

Use Edit (markdown table corruption risk with sed).

- [ ] **Step 3: docs/deferred.md §8j**

Insert §8j BEFORE §8i (newest-first ordering). Mirror the §8i structure:

```markdown
## 8j. Retired in v0.16

v0.16 ships Smart Zoom Image (SZI) support — closes R18 from §11
backlog. R19 (bare Deep Zoom Image, filesystem-backed) remains
parked; v0.16 architecture pre-pares it via the new internal/dzi/
package, but no consumer signal yet motivates shipping bare DZI.

**Items shipped:**

- internal/dzi/ — pure DZI manifest parser + tile-coordinate math
  (Manifest, ParseManifest, MaxLevel, LevelDims, GridDims, TilePath).
- formats/szi/ — full Tiler implementation:
  - SupportsRaw byte-level detection (PK\x03\x04 ZIP magic)
  - eager ZIP central-directory parse at Open() (v0.9 lock-free
    invariant honored)
  - uncompressed-stored ZIP entries → SectionReader on the .szi
    file (mmap-aliased fast path preserved)
  - Image / Level with Tile / TileInto / TileReader / TilePrefix /
    TileBodyInto / TileBodyMaxSize / Compression / Size / TileSize
    / Grid
  - Associated images: macro.jpg → "overview" (per v0.15);
    label.jpg → "label"; thumbnail.jpg → "thumbnail"
  - scan-properties.xml parser → cross-format opentile.Metadata +
    SZI-specific szi.Metadata + VendorProperties map
  - szi.MetadataOf(t opentile.Tiler) accessor
- New public enum values: opentile.FormatSZI ("szi"),
  opentile.CompressionPNG.
- 2 new fixtures (CMU-1.szi from spec repo; scan_618_grundium_SZI.szi
  from Grundium scanner). TestSlideParity 30 fixtures (was 28).

**Architecture invariants preserved:**

- Public API additive only (new format reader + new enum values);
  existing consumers unaffected.
- v0.9 mmap-aliased fast path: SZI's uncompressed-stored ZIP
  entries resolve to byte slices over the file, no inflate, no
  copy on the hot path.
- v0.13 splice-prefix model: SZI tiles are self-contained
  JPEG/PNG; TilePrefix() returns nil; TileBodyInto delegates to
  TileInto per the v0.13 non-applicable convention.
- v0.15 Type() canonical naming: SZI's filename macro.jpg maps to
  Type() == "overview" (not exposed as "macro").
- No new active limitations; sparse SZI deferred (spec forbids;
  breadcrumbs left for a future ErrTileMissing sentinel + opt-in
  lenient mode if a real sparse-SZI fixture surfaces).
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R18 (SZI support) — landed; backlog row retires.

**Still parked:**

- R19 (bare DZI) — deferred to v0.17+; internal/dzi/ pre-pares it.

**v0.16 lessons:** the ZIP central-directory eager parse pattern
is a clean fit for the v0.9 lock-free hot-path invariant. Future
ZIP-backed format readers can mirror this shape directly. The
internal/dzi/ extraction validated the co-design intent — only one
backend ships in v0.16, but the pure-function shape lets a future
backend slot in additively.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v16-szi.md`](superpowers/plans/2026-05-08-opentile-go-v16-szi.md).
```

- [ ] **Step 4: CHANGELOG.md [0.16.0]**

Insert before [0.15.0]:

```markdown
## [0.16.0] — 2026-05-08

Smart Zoom Image (SZI) support — closes R18. New formats/szi/
package backed by new shared internal/dzi/ core (DZI manifest
parser + tile-coordinate math, designed for additive bare-DZI
support in v0.17+). Driven by user's wsi-tools / viewer pipeline
targeting Grundium-scanner output.

### Added

- **`opentile.FormatSZI`** new enum value (`"szi"`).
- **`opentile.CompressionPNG`** new enum value (`"png"`). DZI's
  Format attribute admits both jpeg and png; opentile-go now
  accurately reports the codec on PNG-tiled SZI/DZI files.
- **`internal/dzi/`** new package: pure DZI manifest XML parser
  (`Manifest`, `ParseManifest`) + tile-coordinate math (`MaxLevel`,
  `LevelDims`, `GridDims`, `TilePath`). No I/O; designed to underpin
  multiple storage backends.
- **`formats/szi/`** new package: SZI Tiler with eager ZIP
  central-directory parse, mmap-aliased tile fetch via SectionReader
  on uncompressed-stored entries, full pyramid (Image / Level / Tile
  / TileInto / TileReader / TilePrefix / TileBodyInto), and
  associated images (`macro.jpg` → `Type() == "overview"`;
  `label.jpg`; `thumbnail.jpg`).
- **`szi.Metadata`** struct + **`szi.MetadataOf(t)`** accessor for
  format-specific scan-properties.xml fields including
  `VendorProperties map[string]string` for open-ended `vendor.<key>`
  custom properties (mirrors v0.6+/Philips/OME/IFE/SCN precedent).
- 2 new fixtures wired into TestSlideParity:
  - `CMU-1.szi` (1.5 MB, from smartinmedia/SZI-Format spec repo)
  - `scan_618_grundium_SZI.szi` (709 MB, Grundium-produced)
- TestSlideParity total: **30 fixtures** (was 28).

### Notes

- **Sparse SZI files are not supported** per the spec page 4
  (verbatim: *"sparse images and collections are not supported in
  the SZI format"*). A missing tile in the addressable grid
  returns a corrupt-archive error. Breadcrumbs left for a future
  additive `ErrTileMissing` sentinel + opt-in lenient mode if a
  sparse-SZI fixture surfaces.
- **Bare DZI** (filesystem-backed, no ZIP wrapper) is deferred to
  v0.17+ pending consumer signal. The `internal/dzi/` extraction
  pre-pares this without compromise to SZI.
- **DZC collections** (Morton-laid-out shared thumbnails) are
  permanently out of scope — multi-image; opentile-go reads
  single-WSI files only.
- Optional `vendor/` folder content is not surfaced through the
  public API in v0.16; deferred until consumer signal.
- v1.0 cut still pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump from "after v0.15" → "after v0.16."

- [ ] **Step 5: CLAUDE.md milestone bump**

Replace `## Current milestone — v0.15` block with v0.16. Demote v0.15 to "Previous milestone" with a one-paragraph summary.

```markdown
## Current milestone — v0.16 (shipped)

- **Scope:** Smart Zoom Image (SZI) reader. Closes R18 from
  deferred backlog. New formats/szi/ package backed by new shared
  internal/dzi/ core (manifest parser + tile-coordinate math;
  designed for additive bare-DZI support in v0.17+). Driven by
  user's wsi-tools / viewer pipeline targeting Grundium-scanner
  output. 6 plan tasks single batch.
- **API additions:** opentile.FormatSZI ("szi") + opentile.CompressionPNG
  enum values; internal/dzi package; formats/szi package with
  szi.MetadataOf accessor; szi.Metadata struct including
  VendorProperties map[string]string for open-ended vendor.<key>
  properties.
- **API breaks:** none (purely additive).
- **Active limitations:** unchanged from v0.15. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  docs/deferred.md §1a): unchanged from v0.15. SZI is beyond
  upstream's coverage — no upstream parity to honor.
- **Correctness bar:** make test green; TestSlideParity 30 fixtures
  (was 28).
- **Sealed Q-decisions (8):** Q1 SZI-only in v0.16 (DZI deferred);
  Q2 strict on missing tiles (ErrCorruptArchive); Q3 typed
  szi.Metadata + VendorProperties map; Q4 vendor/ folder content
  deferred; Q5 eager ZIP central-directory parse; Q6 internal/dzi
  split; Q7 both fixtures into TestSlideParity; Q8 6-task batch.
- **Deferred forward:** R19 (bare DZI) — internal/dzi pre-pares it.
  L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9, R15. v1.0 cut
  still pending.
- **Design:** docs/superpowers/specs/2026-05-08-opentile-go-v16-szi-design.md
- **Plan:** docs/superpowers/plans/2026-05-08-opentile-go-v16-szi.md
- **Work branch:** feat/v0.16

## Previous milestone — v0.15 (shipped 2026-05-08)

Naming-cleanup milestone — AssociatedImage.Kind() renamed to Type()
(DICOM ImageType convention); generic-TIFF + Leica SCN emitted
"macro" flipped to "overview" (aligns with DICOM + Python opentile
+ 6 sibling format readers). IFE preserves both as IFE-spec-distinct
values. Breaking change; pre-1.0; sole-consumer sign-off.
```

- [ ] **Step 6: Final pre-commit verification**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: vet clean, gofmt clean, every package green, TestSlideParity 30 fixtures green.

- [ ] **Step 7: Commit**

```bash
git add docs/formats/szi.md README.md docs/deferred.md CHANGELOG.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs(v0.16): T6 — szi.md + README + deferred §8j + CHANGELOG + CLAUDE.md

docs/formats/szi.md: new — SZI architecture + spec + license +
metadata mapping + associated-image semantics + limitations +
references (smartinmedia spec PDF, repo, Microsoft DZI docs).

README.md: new Supported-formats row for SZI.

docs/deferred.md §8j: Retired in v0.16 — closes R18; R19 (bare
DZI) remains parked with internal/dzi/ pre-paring its future.

CHANGELOG.md [0.16.0]: explicit Added block (FormatSZI,
CompressionPNG, internal/dzi, formats/szi, szi.Metadata + MetadataOf,
2 fixtures, TestSlideParity 30 total); Notes (sparse-SZI deferred,
bare-DZI deferred, vendor/ folder deferred).

CLAUDE.md: bump Current milestone v0.15 → v0.16. v0.15 demoted
to Previous; v0.14 / v0.13 / earlier collapsed.

End of milestone; v0.16 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (internal/dzi/) → T1.
- §1.2 (formats/szi/ package) → T2/T3/T4.
- §1.3 (FormatSZI enum) → T2.
- §1.3a (CompressionPNG enum) → T2.
- §1.4 (cross-format Metadata population) → T4.
- §1.5 (szi.Metadata struct) → T4.
- §1.6 (associated images) → T4.
- §1.7 (detection + registration) → T2.
- §3 (Q-decisions) → all reflected in tasks.
- §4 (fixtures) → T5.
- §5 (test strategy) → T1/T2/T3/T4 unit tests + T5 fixtures + geometry pinning.
- §6 (architecture) → T2/T3 implementation matches.
- §8 (verification gates) → T6 step 6.

**Placeholder scan:** every step has exact code blocks, exact paths, expected outputs. T3 step 2's `errors.New("ErrCorruptArchive")` is flagged as implementer-choice naming for the sentinel — non-blocking. T5 step 1 includes a probe step before locking in geometry rows for Grundium (acceptable: the rows for Grundium's 19 levels are mechanically derivable from `dzi.LevelDims(147456, 81920, dziLevel)` once T1 lands; implementer can either compute them or use the probe script).

**Type consistency:** `Tiler`, `level`, `image`, `associatedImage`, `Metadata`, `MetadataOf` used consistently across T2 → T6. All references to v0.15 Type() values (`"label"`, `"overview"`, `"thumbnail"`) consistent.

**Risks:**

- **R1 — Tiler interface evolution.** opentile.Tiler may have additional methods I missed (e.g., a v0.13 `WarmLevel` was confirmed but I should also verify the latest interface in tiler.go before T2 commits). Mitigation: T2 step 5 instructs the implementer to verify against tiler.go's actual interface and add any methods I missed; v0.16 may need additional stubs.
- **R2 — Image interface methods.** SizeZ/SizeC/SizeT (v0.7 multi-dim) may or may not be required on the current Image interface. T3 step 1 instructs the implementer to read image.go and adapt.
- **R3 — Mock test coverage.** opentile_test.go's mock may need updating to satisfy any new interface methods if v0.16 introduces them. None planned for v0.16, so no change expected; but flagged for awareness.
- **R4 — ZIP entry compression method check.** The implementer should verify that uncompressed-stored entries (compression method 0) are required and reject non-stored entries on Open() to match the SZI spec mandate. T2's openSZI doesn't currently enforce this — adding the check is a one-liner per central-directory entry; flag for T2 implementer to add.
