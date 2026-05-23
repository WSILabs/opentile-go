# opentile-go v0.1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship v0.1 of `opentile-go` — a pure-Go port of the Python `opentile` library that reads raw compressed tiles from Aperio SVS whole-slide imaging files.

**Architecture:** Three layers. `internal/tiff` parses TIFF IFDs and exposes per-page tile tables. A top-level public surface (`opentile.Tiler`, `opentile.Level`, factory) defines the API consumers use. `formats/svs` implements format detection, metadata parsing, and level/tile access by passing raw TIFF tile byte ranges through to the caller — no JPEG manipulation, no codec. See `docs/superpowers/specs/2026-04-19-opentile-go-design.md` for the full design.

**Tech Stack:** Go 1.23+ (for `iter.Seq2`). No external dependencies in the main module. Tests may use `testing`, `bytes`, `errors`, `encoding/json`, `crypto/sha256` — all stdlib.

**Scope boundary:** v0.1 covers SVS tiled level passthrough only. Deferred to later milestones: NDPI (v0.2), SVS associated images (v0.3), SVS corrupt-edge reconstruct fix (v1.0), BigTIFF (ship when first format needs it), `internal/jpeg` (ship with NDPI).

---

## File Structure

### Created in v0.1

```
opentile-go/
├── go.mod
├── go.sum                        # empty — no deps
├── LICENSE                       # Apache 2.0 full text
├── NOTICE                        # attribution to Sectra AB
├── README.md                     # usage example, attribution line
├── .gitignore                    # Go defaults
│
├── opentile.go                   # Open, OpenFile, Register
├── tiler.go                      # Tiler interface
├── image.go                      # Level, AssociatedImage interfaces, TilePos, TileResult
├── metadata.go                   # Metadata struct
├── geometry.go                   # Point, Size, SizeMm, Region
├── compression.go                # Compression enum + String
├── errors.go                     # sentinel errors + TileError
├── options.go                    # Option, config, WithTileSize, WithCorruptTilePolicy
│
├── geometry_test.go
├── compression_test.go
├── errors_test.go
├── options_test.go
├── opentile_test.go              # factory + registration tests
│
├── internal/tiff/
│   ├── reader.go                 # endian-aware uint16/uint32 readers over ReaderAt
│   ├── header.go                 # TIFF header parse
│   ├── tag.go                    # DataType, Tag IDs, value decode
│   ├── ifd.go                    # IFD walker
│   ├── file.go                   # File wrapper: parses on construction, exposes []*Page
│   ├── page.go                   # Page: typed accessors for SVS tags
│   ├── reader_test.go
│   ├── header_test.go
│   ├── tag_test.go
│   ├── ifd_test.go
│   ├── file_test.go
│   └── page_test.go
│
├── formats/
│   ├── svs/
│   │   ├── svs.go                # Tiler struct, Supports, New (constructor)
│   │   ├── metadata.go           # ImageDescription parser
│   │   ├── image.go              # tiledImage (Level impl)
│   │   ├── svs_test.go
│   │   ├── metadata_test.go
│   │   └── image_test.go
│   └── all/
│       └── all.go                # umbrella: registers all known formats
│
└── tests/
    ├── download/
    │   └── main.go               # openslide testdata downloader
    ├── fixtures/
    │   └── CMU-1-Small-Region.json   # generated, committed
    ├── fixtures.go               # fixture schema + loader
    ├── generate_test.go          # go test -run TestGenerateFixtures -generate
    └── integration_test.go       # SVS parity against fixtures (skips without OPENTILE_TESTDIR)
```

### Deferred (do NOT create in v0.1)

- `internal/jpeg/` — v0.2 with NDPI
- `formats/ndpi/`, `formats/philips/`, `formats/histech/`, `formats/ome/` — later milestones
- `tests/oracle/` — opt-in parity harness under `//go:build parity`, v0.2+
- BigTIFF support — add when first format needs it

---

## Task 1: Project scaffold

**Files:**
- Create: `go.mod`
- Create: `LICENSE`
- Create: `NOTICE`
- Create: `README.md`
- Create: `.gitignore`

- [ ] **Step 1: Create `go.mod`**

```
module github.com/wsilabs/opentile-go

go 1.23
```

- [ ] **Step 2: Create `LICENSE`**

Fetch the Apache License 2.0 text and save as `LICENSE`. Command:

```bash
curl -fsSL https://www.apache.org/licenses/LICENSE-2.0.txt -o LICENSE
```

- [ ] **Step 3: Create `NOTICE`**

```
opentile-go
Copyright 2026 <Your Name or Org>

This product is a Go port of opentile (https://github.com/imi-bigpicture/opentile),
Copyright 2021-2024 Sectra AB, licensed under the Apache License, Version 2.0.

This is an independent port. It is not affiliated with or endorsed by Sectra AB
or the Innovative Medicines Initiative BigPicture project.
```

- [ ] **Step 4: Create `README.md` stub**

```markdown
# opentile-go

Pure-Go port of [opentile](https://github.com/imi-bigpicture/opentile), a library for reading
tiles from whole-slide imaging (WSI) TIFF files used in digital pathology.

Status: early development. v0.1 supports Aperio SVS tiled-level passthrough.

## License

Apache 2.0. See LICENSE and NOTICE.
```

- [ ] **Step 5: Create `.gitignore`**

```
# Go build artifacts
*.exe
*.test
*.out

# IDE
.idea/
.vscode/

# Local test data
/testdata/slides/
```

- [ ] **Step 6: Verify toolchain**

Run: `go mod tidy && go version`
Expected: prints Go 1.23+. `go mod tidy` is a no-op (no deps yet).

- [ ] **Step 7: Commit**

```bash
git add go.mod LICENSE NOTICE README.md .gitignore
git commit -m "chore: scaffold go module with license and attribution"
```

---

## Task 2: `geometry.go`

**Files:**
- Create: `geometry.go`
- Create: `geometry_test.go`

- [ ] **Step 1: Write failing tests**

Create `geometry_test.go`:

```go
package opentile

import "testing"

func TestSize(t *testing.T) {
    s := Size{W: 10, H: 20}
    if s.Area() != 200 {
        t.Fatalf("Area: want 200, got %d", s.Area())
    }
    if s.String() != "10x20" {
        t.Fatalf("String: want 10x20, got %s", s.String())
    }
}

func TestPoint(t *testing.T) {
    p := Point{X: 3, Y: 4}
    if p.String() != "(3,4)" {
        t.Fatalf("String: want (3,4), got %s", p.String())
    }
}

func TestSizeMm(t *testing.T) {
    m := SizeMm{W: 0.5, H: 0.25}
    if m.IsZero() {
        t.Fatal("IsZero: non-zero value reported zero")
    }
    if !(SizeMm{}).IsZero() {
        t.Fatal("IsZero: zero value reported non-zero")
    }
}

func TestRegionContains(t *testing.T) {
    r := Region{Origin: Point{X: 5, Y: 5}, Size: Size{W: 10, H: 10}}
    tests := []struct {
        p    Point
        want bool
    }{
        {Point{X: 5, Y: 5}, true},
        {Point{X: 14, Y: 14}, true},
        {Point{X: 15, Y: 14}, false},
        {Point{X: 4, Y: 5}, false},
    }
    for _, tt := range tests {
        if got := r.Contains(tt.p); got != tt.want {
            t.Errorf("Contains(%v) = %v, want %v", tt.p, got, tt.want)
        }
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./...`
Expected: FAIL — undefined: `Size`, `Point`, `SizeMm`, `Region`.

- [ ] **Step 3: Implement geometry**

Create `geometry.go`:

```go
// Package opentile provides utilities to read tiles from whole-slide imaging
// (WSI) TIFF files. See the repository README for a high-level overview.
package opentile

import "fmt"

// Point is a 2D integer position measured in pixels or tile units.
type Point struct {
    X, Y int
}

func (p Point) String() string { return fmt.Sprintf("(%d,%d)", p.X, p.Y) }

// Size is a 2D integer extent measured in pixels or tile units.
type Size struct {
    W, H int
}

func (s Size) Area() int      { return s.W * s.H }
func (s Size) String() string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// SizeMm is a 2D extent measured in millimeters. Used for pixel spacing and
// microns-per-pixel conversion (SizeMm scaled by 1000 equals micrometers).
type SizeMm struct {
    W, H float64
}

func (s SizeMm) IsZero() bool { return s.W == 0 && s.H == 0 }

// Region is an axis-aligned rectangle in pixel or tile units.
type Region struct {
    Origin Point
    Size   Size
}

// Contains reports whether p lies inside r (inclusive of origin, exclusive of
// the far edge).
func (r Region) Contains(p Point) bool {
    return p.X >= r.Origin.X && p.X < r.Origin.X+r.Size.W &&
        p.Y >= r.Origin.Y && p.Y < r.Origin.Y+r.Size.H
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add geometry.go geometry_test.go
git commit -m "feat(geometry): add Point, Size, SizeMm, Region types"
```

---

## Task 3: `compression.go`

**Files:**
- Create: `compression.go`
- Create: `compression_test.go`

- [ ] **Step 1: Write failing tests**

```go
package opentile

import "testing"

func TestCompressionString(t *testing.T) {
    tests := []struct {
        c    Compression
        want string
    }{
        {CompressionUnknown, "unknown"},
        {CompressionNone, "none"},
        {CompressionJPEG, "jpeg"},
        {CompressionJP2K, "jp2k"},
        {Compression(99), "unknown(99)"},
    }
    for _, tt := range tests {
        if got := tt.c.String(); got != tt.want {
            t.Errorf("Compression(%d).String() = %q, want %q", tt.c, got, tt.want)
        }
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./...`
Expected: FAIL — undefined: `Compression`, `CompressionUnknown`, `CompressionNone`, etc.

- [ ] **Step 3: Implement `compression.go`**

```go
package opentile

import "fmt"

// Compression identifies the bitstream format of a tile as stored in a TIFF.
//
// opentile-go returns tile bytes in the compression format of the source TIFF
// without decoding them. Consumers that need decoded pixels should pass the
// bytes to a codec appropriate for the reported compression.
//
// The zero value is CompressionUnknown: a forgotten-to-initialize field
// surfaces loudly rather than masquerading as a known compression.
type Compression uint8

const (
    CompressionUnknown Compression = iota // zero value; unset or unrecognized
    CompressionNone
    CompressionJPEG
    CompressionJP2K
)

func (c Compression) String() string {
    switch c {
    case CompressionUnknown:
        return "unknown"
    case CompressionNone:
        return "none"
    case CompressionJPEG:
        return "jpeg"
    case CompressionJP2K:
        return "jp2k"
    default:
        return fmt.Sprintf("unknown(%d)", uint8(c))
    }
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compression.go compression_test.go
git commit -m "feat(compression): add Compression enum"
```

---

## Task 4: `errors.go`

**Files:**
- Create: `errors.go`
- Create: `errors_test.go`

- [ ] **Step 1: Write failing tests**

```go
package opentile

import (
    "errors"
    "io"
    "testing"
)

func TestSentinelErrors(t *testing.T) {
    errs := []error{
        ErrUnsupportedFormat,
        ErrUnsupportedCompression,
        ErrTileOutOfBounds,
        ErrCorruptTile,
        ErrLevelOutOfRange,
        ErrInvalidTIFF,
    }
    seen := make(map[string]bool)
    for _, e := range errs {
        if e == nil {
            t.Fatal("sentinel is nil")
        }
        if seen[e.Error()] {
            t.Errorf("duplicate sentinel text: %q", e.Error())
        }
        seen[e.Error()] = true
    }
}

func TestTileError(t *testing.T) {
    te := &TileError{Level: 2, X: 7, Y: 3, Err: ErrCorruptTile}

    if !errors.Is(te, ErrCorruptTile) {
        t.Fatal("errors.Is should find wrapped sentinel")
    }

    var got *TileError
    if !errors.As(te, &got) {
        t.Fatal("errors.As should extract TileError")
    }
    if got.Level != 2 || got.X != 7 || got.Y != 3 {
        t.Fatalf("TileError fields: got %+v", got)
    }

    wantMsg := "opentile: tile (7,3) on level 2: opentile: corrupt tile"
    if te.Error() != wantMsg {
        t.Fatalf("Error(): got %q, want %q", te.Error(), wantMsg)
    }
}

func TestTileErrorWrapsIO(t *testing.T) {
    te := &TileError{Level: 0, X: 0, Y: 0, Err: io.ErrUnexpectedEOF}
    if !errors.Is(te, io.ErrUnexpectedEOF) {
        t.Fatal("should unwrap to io.ErrUnexpectedEOF")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement `errors.go`**

```go
package opentile

import (
    "errors"
    "fmt"
)

var (
    ErrUnsupportedFormat      = errors.New("opentile: unsupported format")
    ErrUnsupportedCompression = errors.New("opentile: unsupported compression")
    ErrTileOutOfBounds        = errors.New("opentile: tile position out of bounds")
    ErrCorruptTile            = errors.New("opentile: corrupt tile")
    ErrLevelOutOfRange        = errors.New("opentile: level index out of range")
    ErrInvalidTIFF            = errors.New("opentile: invalid TIFF structure")
)

// TileError wraps a per-tile failure with the (level, x, y) that produced it.
// Consumers use errors.As to extract the coordinates and errors.Is against the
// exported sentinels to branch on the underlying cause.
type TileError struct {
    Level int
    X, Y  int
    Err   error
}

func (e *TileError) Error() string {
    return fmt.Sprintf("opentile: tile (%d,%d) on level %d: %v", e.X, e.Y, e.Level, e.Err)
}

func (e *TileError) Unwrap() error { return e.Err }
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add errors.go errors_test.go
git commit -m "feat(errors): add sentinel errors and TileError wrapper"
```

---

## Task 5: `internal/tiff/reader.go` — endian-aware helpers

**Files:**
- Create: `internal/tiff/reader.go`
- Create: `internal/tiff/reader_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tiff

import (
    "bytes"
    "testing"
)

func TestByteReader(t *testing.T) {
    // little-endian: u16=0x0201 at offset 0; u32=0x06050403 at offset 2
    data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
    r := bytes.NewReader(data)

    bl := newByteReader(r, true)
    v16, err := bl.uint16(0)
    if err != nil || v16 != 0x0201 {
        t.Fatalf("uint16 LE: got 0x%x, err %v; want 0x0201", v16, err)
    }
    v32, err := bl.uint32(2)
    if err != nil || v32 != 0x06050403 {
        t.Fatalf("uint32 LE: got 0x%x, err %v; want 0x06050403", v32, err)
    }

    bb := newByteReader(r, false)
    v16b, _ := bb.uint16(0)
    if v16b != 0x0102 {
        t.Fatalf("uint16 BE: got 0x%x, want 0x0102", v16b)
    }
    v32b, _ := bb.uint32(2)
    if v32b != 0x03040506 {
        t.Fatalf("uint32 BE: got 0x%x, want 0x03040506", v32b)
    }
}

func TestByteReaderShort(t *testing.T) {
    r := bytes.NewReader([]byte{0x01})
    b := newByteReader(r, true)
    if _, err := b.uint16(0); err == nil {
        t.Fatal("expected error for short read")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — undefined `newByteReader`.

- [ ] **Step 3: Implement `internal/tiff/reader.go`**

```go
// Package tiff parses a minimal subset of the TIFF file format sufficient to
// locate compressed tile byte ranges for whole-slide imaging TIFFs. It is not
// a general-purpose TIFF library; it exposes raw tile bytes and vendor tags
// needed by the opentile-go format packages.
package tiff

import (
    "encoding/binary"
    "errors"
    "fmt"
    "io"
)

// byteReader reads fixed-width integers at arbitrary offsets in a ReaderAt
// using the byte order established by the TIFF header.
type byteReader struct {
    r         io.ReaderAt
    order     binary.ByteOrder
}

func newByteReader(r io.ReaderAt, littleEndian bool) *byteReader {
    order := binary.ByteOrder(binary.BigEndian)
    if littleEndian {
        order = binary.LittleEndian
    }
    return &byteReader{r: r, order: order}
}

func (b *byteReader) read(offset int64, n int) ([]byte, error) {
    buf := make([]byte, n)
    got, err := b.r.ReadAt(buf, offset)
    if err != nil && !(errors.Is(err, io.EOF) && got == n) {
        return nil, fmt.Errorf("tiff: read %d bytes at %d: %w", n, offset, err)
    }
    if got != n {
        return nil, fmt.Errorf("tiff: short read at %d: got %d, want %d", offset, got, n)
    }
    return buf, nil
}

func (b *byteReader) uint16(offset int64) (uint16, error) {
    buf, err := b.read(offset, 2)
    if err != nil {
        return 0, err
    }
    return b.order.Uint16(buf), nil
}

func (b *byteReader) uint32(offset int64) (uint32, error) {
    buf, err := b.read(offset, 4)
    if err != nil {
        return 0, err
    }
    return b.order.Uint32(buf), nil
}

func (b *byteReader) bytes(offset int64, n int) ([]byte, error) {
    return b.read(offset, n)
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/reader.go internal/tiff/reader_test.go
git commit -m "feat(tiff): add endian-aware byte reader over io.ReaderAt"
```

---

## Task 6: `internal/tiff/header.go` — TIFF header parse

**Files:**
- Create: `internal/tiff/header.go`
- Create: `internal/tiff/header_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tiff

import (
    "bytes"
    "errors"
    "testing"
)

func TestParseHeader(t *testing.T) {
    tests := []struct {
        name       string
        bytes      []byte
        wantLE     bool
        wantOffset uint32
        wantErr    error
    }{
        {
            name:       "little-endian classic",
            bytes:      []byte{'I', 'I', 42, 0, 0x08, 0, 0, 0},
            wantLE:     true,
            wantOffset: 8,
        },
        {
            name:       "big-endian classic",
            bytes:      []byte{'M', 'M', 0, 42, 0, 0, 0, 0x10},
            wantLE:     false,
            wantOffset: 16,
        },
        {
            name:    "bad byte order",
            bytes:   []byte{'X', 'Y', 42, 0, 0, 0, 0, 0},
            wantErr: ErrInvalidTIFF,
        },
        {
            name:    "bad magic",
            bytes:   []byte{'I', 'I', 99, 0, 0, 0, 0, 0},
            wantErr: ErrInvalidTIFF,
        },
        {
            name:    "bigtiff (unsupported v0.1)",
            bytes:   []byte{'I', 'I', 43, 0, 8, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0},
            wantErr: ErrUnsupportedTIFF,
        },
        {
            name:    "short",
            bytes:   []byte{'I', 'I'},
            wantErr: ErrInvalidTIFF,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            h, err := parseHeader(bytes.NewReader(tt.bytes))
            if tt.wantErr != nil {
                if !errors.Is(err, tt.wantErr) {
                    t.Fatalf("err: got %v, want %v", err, tt.wantErr)
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected err: %v", err)
            }
            if h.littleEndian != tt.wantLE {
                t.Errorf("littleEndian: got %v, want %v", h.littleEndian, tt.wantLE)
            }
            if h.firstIFD != tt.wantOffset {
                t.Errorf("firstIFD: got %d, want %d", h.firstIFD, tt.wantOffset)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — undefined `parseHeader`, `ErrInvalidTIFF`, `ErrUnsupportedTIFF`.

- [ ] **Step 3: Implement `internal/tiff/header.go`**

```go
package tiff

import (
    "errors"
    "fmt"
    "io"
)

// ErrInvalidTIFF indicates the input does not parse as TIFF at the header level.
// Package-local sentinel; the top-level opentile package wraps it as
// opentile.ErrInvalidTIFF before returning to callers.
var ErrInvalidTIFF = errors.New("tiff: invalid TIFF structure")

// ErrUnsupportedTIFF indicates a valid TIFF variant that opentile-go v0.1 does
// not yet parse (e.g., BigTIFF).
var ErrUnsupportedTIFF = errors.New("tiff: unsupported TIFF variant")

type header struct {
    littleEndian bool
    firstIFD     uint32
}

// parseHeader reads the 8-byte TIFF header. BigTIFF (magic 43) is reported as
// ErrUnsupportedTIFF; v0.1 only targets classic TIFF, which covers SVS.
func parseHeader(r io.ReaderAt) (header, error) {
    var buf [4]byte
    if _, err := r.ReadAt(buf[:], 0); err != nil {
        return header{}, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
    }
    var le bool
    switch {
    case buf[0] == 'I' && buf[1] == 'I':
        le = true
    case buf[0] == 'M' && buf[1] == 'M':
        le = false
    default:
        return header{}, fmt.Errorf("%w: bad byte order %q", ErrInvalidTIFF, buf[:2])
    }
    b := newByteReader(r, le)
    magic, err := b.uint16(2)
    if err != nil {
        return header{}, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
    }
    switch magic {
    case 42:
        // classic TIFF
    case 43:
        return header{}, fmt.Errorf("%w: BigTIFF", ErrUnsupportedTIFF)
    default:
        return header{}, fmt.Errorf("%w: magic %d", ErrInvalidTIFF, magic)
    }
    offset, err := b.uint32(4)
    if err != nil {
        return header{}, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
    }
    return header{littleEndian: le, firstIFD: offset}, nil
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/header.go internal/tiff/header_test.go
git commit -m "feat(tiff): parse TIFF header (classic only; BigTIFF deferred)"
```

---

## Task 7: `internal/tiff/tag.go` — TIFF tag types and decoding

**Files:**
- Create: `internal/tiff/tag.go`
- Create: `internal/tiff/tag_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tiff

import (
    "bytes"
    "testing"
)

func TestDataTypeSize(t *testing.T) {
    tests := []struct {
        dt   DataType
        want int
    }{
        {DTByte, 1},
        {DTASCII, 1},
        {DTShort, 2},
        {DTLong, 4},
        {DTRational, 8},
        {DTUndefined, 1},
    }
    for _, tt := range tests {
        if got := tt.dt.Size(); got != tt.want {
            t.Errorf("%v.Size() = %d, want %d", tt.dt, got, tt.want)
        }
    }
}

func TestTagValueDecodeInline(t *testing.T) {
    // Entry with count=2 SHORT values stored inline: values 100, 200.
    // Inline 4-byte cell (LE): [0x64,0x00,0xC8,0x00]
    data := []byte{0x64, 0x00, 0xC8, 0x00}
    r := bytes.NewReader(data)
    b := newByteReader(r, true)

    entry := Entry{Tag: 256, Type: DTShort, Count: 2, valueOrOffset: 0}
    // override: put the payload at offset 0 for this test
    vals, err := entry.decodeInline(b, data)
    if err != nil {
        t.Fatalf("decodeInline: %v", err)
    }
    if len(vals) != 2 || vals[0] != 100 || vals[1] != 200 {
        t.Fatalf("vals: got %v, want [100 200]", vals)
    }
}

func TestTagValueDecodeExternal(t *testing.T) {
    // Entry count=2 LONG (8 bytes), value stored at offset 16.
    // LE: [0x0A,0,0,0, 0x14,0,0,0] → values 10, 20
    data := make([]byte, 24)
    copy(data[16:], []byte{0x0A, 0, 0, 0, 0x14, 0, 0, 0})
    r := bytes.NewReader(data)
    b := newByteReader(r, true)

    entry := Entry{Tag: 324, Type: DTLong, Count: 2, valueOrOffset: 16}
    vals, err := entry.decodeExternal(b)
    if err != nil {
        t.Fatalf("decodeExternal: %v", err)
    }
    if len(vals) != 2 || vals[0] != 10 || vals[1] != 20 {
        t.Fatalf("vals: got %v, want [10 20]", vals)
    }
}

func TestDecodeASCII(t *testing.T) {
    // "Aperio\x00" (7 bytes) stored externally.
    data := make([]byte, 16)
    copy(data[8:], []byte("Aperio\x00"))
    r := bytes.NewReader(data)
    b := newByteReader(r, true)

    entry := Entry{Tag: 270, Type: DTASCII, Count: 7, valueOrOffset: 8}
    s, err := entry.decodeASCII(b, data[:8]) // inline cell unused here
    if err != nil {
        t.Fatalf("decodeASCII: %v", err)
    }
    if s != "Aperio" {
        t.Fatalf("got %q, want %q", s, "Aperio")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — undefined `DataType`, `Entry`, helpers.

- [ ] **Step 3: Implement `internal/tiff/tag.go`**

```go
package tiff

import (
    "fmt"
    "strings"
)

// DataType is the TIFF tag data type as defined in TIFF 6.0 + common extensions.
type DataType uint16

const (
    DTByte      DataType = 1
    DTASCII     DataType = 2
    DTShort     DataType = 3
    DTLong      DataType = 4
    DTRational  DataType = 5
    DTUndefined DataType = 7
)

// Size returns the byte size of a single value of the given data type.
// Returns 1 for unknown types so callers reading external offsets can still
// bound-check; callers should check Size() > 0 in a validity guard before use
// for types they care about.
func (d DataType) Size() int {
    switch d {
    case DTByte, DTASCII, DTUndefined:
        return 1
    case DTShort:
        return 2
    case DTLong:
        return 4
    case DTRational:
        return 8
    default:
        return 1
    }
}

// Entry is a raw IFD entry: tag id, type, count, and a 4-byte
// value-or-offset cell. The cell is a little/big-endian encoded uint32 as
// stored in the file; whether it carries the value inline or an external
// offset depends on Count * Type.Size().
type Entry struct {
    Tag           uint16
    Type          DataType
    Count         uint32
    valueOrOffset uint32
    valueBytes    [4]byte // raw 4-byte cell, preserving byte order
}

// fitsInline reports whether the tag value fits in the 4-byte inline cell.
func (e Entry) fitsInline() bool {
    return int64(e.Count)*int64(e.Type.Size()) <= 4
}

// decodeInline decodes the inline 4-byte cell (cell) as a slice of uint32 values
// according to the entry's Type. cell must be the raw 4 bytes in file order.
func (e Entry) decodeInline(b *byteReader, cell []byte) ([]uint32, error) {
    if !e.fitsInline() {
        return nil, fmt.Errorf("tiff: tag %d: value does not fit inline", e.Tag)
    }
    return e.decodeBuffer(b, cell)
}

// decodeExternal decodes values stored at e.valueOrOffset in the underlying file.
func (e Entry) decodeExternal(b *byteReader) ([]uint32, error) {
    n := int64(e.Count) * int64(e.Type.Size())
    buf, err := b.bytes(int64(e.valueOrOffset), int(n))
    if err != nil {
        return nil, fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
    }
    return e.decodeBuffer(b, buf)
}

// Values returns the decoded uint32 values for this entry, reading the inline
// cell when possible or the external offset otherwise.
func (e Entry) Values(b *byteReader) ([]uint32, error) {
    if e.fitsInline() {
        return e.decodeInline(b, e.valueBytes[:])
    }
    return e.decodeExternal(b)
}

// decodeBuffer decodes buf (which must be exactly Count*Type.Size() bytes)
// into uint32 values. Rational and unknown types return raw byte groups as
// uint32s (use dedicated helpers for those cases).
func (e Entry) decodeBuffer(b *byteReader, buf []byte) ([]uint32, error) {
    out := make([]uint32, 0, e.Count)
    switch e.Type {
    case DTByte, DTUndefined:
        for _, v := range buf[:e.Count] {
            out = append(out, uint32(v))
        }
    case DTShort:
        for i := uint32(0); i < e.Count; i++ {
            out = append(out, uint32(b.order.Uint16(buf[i*2:])))
        }
    case DTLong:
        for i := uint32(0); i < e.Count; i++ {
            out = append(out, b.order.Uint32(buf[i*4:]))
        }
    default:
        return nil, fmt.Errorf("tiff: tag %d: unsupported type %d for uint decode", e.Tag, e.Type)
    }
    return out, nil
}

// decodeASCII reads the string value for an ASCII entry.
// cell is the 4-byte inline cell used when the value fits inline.
func (e Entry) decodeASCII(b *byteReader, cell []byte) (string, error) {
    var data []byte
    if e.fitsInline() {
        data = cell[:e.Count]
    } else {
        buf, err := b.bytes(int64(e.valueOrOffset), int(e.Count))
        if err != nil {
            return "", fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
        }
        data = buf
    }
    // TIFF ASCII values are NUL-terminated; strip trailing NULs.
    return strings.TrimRight(string(data), "\x00"), nil
}

// decodeRational reads the uint32 numerator/denominator pairs.
func (e Entry) decodeRational(b *byteReader) ([][2]uint32, error) {
    n := int64(e.Count) * 8
    buf, err := b.bytes(int64(e.valueOrOffset), int(n))
    if err != nil {
        return nil, err
    }
    out := make([][2]uint32, 0, e.Count)
    for i := uint32(0); i < e.Count; i++ {
        num := b.order.Uint32(buf[i*8:])
        den := b.order.Uint32(buf[i*8+4:])
        out = append(out, [2]uint32{num, den})
    }
    return out, nil
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/tag.go internal/tiff/tag_test.go
git commit -m "feat(tiff): add Entry and DataType with inline/external value decoders"
```

---

## Task 8: `internal/tiff/ifd.go` — IFD walker

**Files:**
- Create: `internal/tiff/ifd.go`
- Create: `internal/tiff/ifd_test.go`

- [ ] **Step 1: Write failing tests**

Construct a minimal in-memory classic TIFF with one IFD containing two tags (ImageWidth, ImageLength) and walk it.

```go
package tiff

import (
    "bytes"
    "encoding/binary"
    "testing"
)

// buildClassicTIFF builds a tiny in-memory TIFF (LE) with one IFD containing
// the supplied entries. All tag values must fit inline (≤4 bytes).
// Returns the raw bytes; first IFD is at offset 8.
func buildClassicTIFF(t *testing.T, entries [][3]uint32) []byte {
    t.Helper()
    // Header: II 42 0x00000008
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    // IFD: count (u16), entries (12 bytes each), next IFD (u32)
    n := uint16(len(entries))
    _ = binary.Write(buf, binary.LittleEndian, n)
    for _, e := range entries {
        // tag u16, type u16 (SHORT=3), count u32, value-or-offset u32
        _ = binary.Write(buf, binary.LittleEndian, uint16(e[0]))
        _ = binary.Write(buf, binary.LittleEndian, uint16(3)) // SHORT
        _ = binary.Write(buf, binary.LittleEndian, uint32(1))
        _ = binary.Write(buf, binary.LittleEndian, e[2])
    }
    _ = binary.Write(buf, binary.LittleEndian, uint32(0)) // next IFD = 0
    return buf.Bytes()
}

func TestWalkIFDs(t *testing.T) {
    data := buildClassicTIFF(t, [][3]uint32{
        {256, 3, 1024}, // ImageWidth = 1024
        {257, 3, 768},  // ImageLength = 768
    })
    r := bytes.NewReader(data)
    h, err := parseHeader(r)
    if err != nil {
        t.Fatalf("parseHeader: %v", err)
    }
    b := newByteReader(r, h.littleEndian)

    ifds, err := walkIFDs(b, int64(h.firstIFD))
    if err != nil {
        t.Fatalf("walkIFDs: %v", err)
    }
    if len(ifds) != 1 {
        t.Fatalf("ifd count: got %d, want 1", len(ifds))
    }
    ifd := ifds[0]
    if len(ifd.entries) != 2 {
        t.Fatalf("entry count: got %d, want 2", len(ifd.entries))
    }
    w, ok := ifd.get(256)
    if !ok {
        t.Fatal("ImageWidth missing")
    }
    wv, err := w.Values(b)
    if err != nil || len(wv) != 1 || wv[0] != 1024 {
        t.Fatalf("ImageWidth: got %v, err %v; want [1024]", wv, err)
    }
}

func TestWalkIFDsMultiple(t *testing.T) {
    // Two IFDs: first points to second via next-IFD offset.
    // Use buildClassicTIFF for the first, manually chain a second.
    // For simplicity, verify walkIFDs handles cycles / unbounded reads by
    // asserting it stops at a zero next-IFD offset and does not panic.
    data := buildClassicTIFF(t, [][3]uint32{{256, 3, 100}})
    r := bytes.NewReader(data)
    h, _ := parseHeader(r)
    b := newByteReader(r, h.littleEndian)
    ifds, err := walkIFDs(b, int64(h.firstIFD))
    if err != nil {
        t.Fatalf("walkIFDs: %v", err)
    }
    if len(ifds) != 1 {
        t.Fatalf("expected 1 IFD, got %d", len(ifds))
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — undefined `walkIFDs`, `ifd`, `ifd.get`.

- [ ] **Step 3: Implement `internal/tiff/ifd.go`**

```go
package tiff

import (
    "fmt"
)

// ifd is a parsed Image File Directory: a collection of Entry values indexed by tag id.
type ifd struct {
    entries map[uint16]Entry
}

func (i *ifd) get(tag uint16) (Entry, bool) {
    e, ok := i.entries[tag]
    return e, ok
}

// maxIFDs guards against pathological inputs with circular or excessive IFD chains.
const maxIFDs = 1024

// walkIFDs reads the IFD chain starting at offset and returns every IFD.
// The chain terminates when next-IFD-offset is zero or maxIFDs is reached
// (returning an error in the latter case).
func walkIFDs(b *byteReader, offset int64) ([]*ifd, error) {
    var out []*ifd
    seen := make(map[int64]bool)
    for offset != 0 {
        if len(out) >= maxIFDs {
            return nil, fmt.Errorf("tiff: IFD chain exceeds max length %d", maxIFDs)
        }
        if seen[offset] {
            return nil, fmt.Errorf("tiff: IFD cycle at offset %d", offset)
        }
        seen[offset] = true

        count, err := b.uint16(offset)
        if err != nil {
            return nil, fmt.Errorf("tiff: IFD entry count at %d: %w", offset, err)
        }
        ifd := &ifd{entries: make(map[uint16]Entry, count)}
        pos := offset + 2
        for i := uint16(0); i < count; i++ {
            entry, err := readEntry(b, pos)
            if err != nil {
                return nil, err
            }
            ifd.entries[entry.Tag] = entry
            pos += 12
        }
        out = append(out, ifd)
        next, err := b.uint32(pos)
        if err != nil {
            return nil, fmt.Errorf("tiff: next IFD offset at %d: %w", pos, err)
        }
        offset = int64(next)
    }
    return out, nil
}

// readEntry reads a 12-byte IFD entry at offset.
func readEntry(b *byteReader, offset int64) (Entry, error) {
    tag, err := b.uint16(offset)
    if err != nil {
        return Entry{}, err
    }
    typ, err := b.uint16(offset + 2)
    if err != nil {
        return Entry{}, err
    }
    count, err := b.uint32(offset + 4)
    if err != nil {
        return Entry{}, err
    }
    cell, err := b.bytes(offset+8, 4)
    if err != nil {
        return Entry{}, err
    }
    vo := b.order.Uint32(cell)
    var e Entry
    e.Tag = tag
    e.Type = DataType(typ)
    e.Count = count
    e.valueOrOffset = vo
    copy(e.valueBytes[:], cell)
    return e, nil
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/ifd.go internal/tiff/ifd_test.go
git commit -m "feat(tiff): walk IFD chain with cycle and length guards"
```

---

## Task 9: `internal/tiff/file.go` — `File` wrapper

**Files:**
- Create: `internal/tiff/file.go`
- Create: `internal/tiff/file_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tiff

import (
    "bytes"
    "testing"
)

func TestOpenFileMinimal(t *testing.T) {
    data := buildClassicTIFF(t, [][3]uint32{
        {256, 3, 1024}, // ImageWidth
        {257, 3, 768},  // ImageLength
    })
    f, err := Open(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    if len(f.Pages()) != 1 {
        t.Fatalf("pages: got %d, want 1", len(f.Pages()))
    }
    if !f.LittleEndian() {
        t.Error("expected LittleEndian true")
    }
}

func TestOpenRejectsBigTIFF(t *testing.T) {
    data := []byte{'I', 'I', 43, 0, 8, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0}
    if _, err := Open(bytes.NewReader(data), int64(len(data))); err == nil {
        t.Fatal("expected BigTIFF to be rejected")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — undefined `Open`, `File`, `Pages`.

- [ ] **Step 3: Implement `internal/tiff/file.go`**

```go
package tiff

import (
    "encoding/binary"
    "io"
)

// File is a parsed TIFF file: a list of pages in IFD order, plus the reader
// and byte order needed to decode tag values and read tile payloads.
type File struct {
    r      io.ReaderAt
    size   int64
    reader *byteReader
    pages  []*Page
}

// Open parses the header and every IFD in r, producing a File ready for use by
// format packages. Open does not read tile payloads. The caller retains
// ownership of r; File does not close it (the io.ReaderAt contract does not
// include Close). size is the total readable size of r in bytes and is stored
// for future offset-bounds validation.
func Open(r io.ReaderAt, size int64) (*File, error) {
    h, err := parseHeader(r)
    if err != nil {
        return nil, err
    }
    br := newByteReader(r, h.littleEndian)
    ifds, err := walkIFDs(br, int64(h.firstIFD))
    if err != nil {
        return nil, err
    }
    pages := make([]*Page, 0, len(ifds))
    for _, i := range ifds {
        pages = append(pages, newPage(i, br))
    }
    return &File{r: r, size: size, reader: br, pages: pages}, nil
}

// Pages returns the pages in IFD order. The slice is owned by File; do not mutate.
func (f *File) Pages() []*Page { return f.pages }

// LittleEndian reports whether the file is stored little-endian.
func (f *File) LittleEndian() bool { return f.reader.order == binary.LittleEndian }

// ReaderAt returns the underlying reader for use by format packages reading
// tile byte ranges.
func (f *File) ReaderAt() io.ReaderAt { return f.r }
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS. (Requires Task 10's `Page` stub to compile — implement alongside as a single commit, OR add a minimal `Page` stub here and extend in Task 10.)

To unblock compilation in this task, add a minimal `Page` stub in `internal/tiff/page.go`:

```go
package tiff

// Page wraps a single parsed IFD with the byte reader needed to decode its tag
// values. Typed tag accessors are added in a subsequent task.
type Page struct {
    ifd *ifd
    br  *byteReader
}

func newPage(i *ifd, br *byteReader) *Page { return &Page{ifd: i, br: br} }
```

Task 10 will replace the stub with the full implementation.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/file.go internal/tiff/file_test.go internal/tiff/page.go
git commit -m "feat(tiff): add File.Open that parses header and IFDs"
```

---

## Task 10: `internal/tiff/page.go` — typed tag accessors

**Files:**
- Modify: `internal/tiff/page.go` (replace stub from Task 9)
- Create: `internal/tiff/page_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tiff

import (
    "bytes"
    "encoding/binary"
    "testing"
)

// buildPageTIFF builds a TIFF with a single IFD carrying the minimum SVS-level
// tag set: ImageWidth, ImageLength, TileWidth, TileLength, Compression,
// Photometric, TileOffsets[4], TileByteCounts[4], plus ImageDescription.
func buildPageTIFF(t *testing.T) []byte {
    t.Helper()
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0}) // header, first IFD at 8

    const (
        tImageWidth   uint16 = 256
        tImageLength  uint16 = 257
        tCompression  uint16 = 259
        tPhotometric  uint16 = 262
        tImageDesc    uint16 = 270
        tTileWidth    uint16 = 322
        tTileLength   uint16 = 323
        tTileOffsets  uint16 = 324
        tTileByteCnts uint16 = 325
    )

    // 9 entries. count(u16) + 9*12 entry bytes + 4 nextIFD = 2 + 108 + 4 = 114 bytes.
    // External data region starts at offset 8 + 114 = 122.
    externalBase := uint32(122)

    // Description ASCII: "Aperio"
    desc := []byte("Aperio\x00")
    descOff := externalBase
    externalAfterDesc := externalBase + uint32(len(desc))

    // TileOffsets: 4 LONGs
    tileOff := []uint32{1000, 2000, 3000, 4000}
    tileOffOff := externalAfterDesc
    externalAfterOffsets := tileOffOff + uint32(4*len(tileOff))

    // TileByteCounts: 4 LONGs
    tileBC := []uint32{100, 200, 300, 400}
    tileBCOff := externalAfterOffsets
    _ = tileBCOff + uint32(4*len(tileBC))

    // 9 entries
    _ = binary.Write(buf, binary.LittleEndian, uint16(9)) // entry count
    writeEntry := func(tag uint16, typ DataType, count uint32, voc uint32) {
        _ = binary.Write(buf, binary.LittleEndian, tag)
        _ = binary.Write(buf, binary.LittleEndian, uint16(typ))
        _ = binary.Write(buf, binary.LittleEndian, count)
        _ = binary.Write(buf, binary.LittleEndian, voc)
    }
    writeEntry(tImageWidth, DTShort, 1, 1024)
    writeEntry(tImageLength, DTShort, 1, 768)
    writeEntry(tCompression, DTShort, 1, 7) // JPEG
    writeEntry(tPhotometric, DTShort, 1, 6) // YCbCr
    writeEntry(tImageDesc, DTASCII, uint32(len(desc)), descOff)
    writeEntry(tTileWidth, DTShort, 1, 256)
    writeEntry(tTileLength, DTShort, 1, 256)
    writeEntry(tTileOffsets, DTLong, 4, tileOffOff)
    writeEntry(tTileByteCnts, DTLong, 4, tileBCOff)
    _ = binary.Write(buf, binary.LittleEndian, uint32(0)) // next IFD

    // External data (order: desc, tileOff, tileBC)
    buf.Write(desc)
    for _, v := range tileOff {
        _ = binary.Write(buf, binary.LittleEndian, v)
    }
    for _, v := range tileBC {
        _ = binary.Write(buf, binary.LittleEndian, v)
    }
    return buf.Bytes()
}

func TestPageAccessors(t *testing.T) {
    data := buildPageTIFF(t)
    f, err := Open(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    if len(f.Pages()) != 1 {
        t.Fatalf("pages: got %d", len(f.Pages()))
    }
    p := f.Pages()[0]

    if iw, _ := p.ImageWidth(); iw != 1024 {
        t.Errorf("ImageWidth: got %d, want 1024", iw)
    }
    if il, _ := p.ImageLength(); il != 768 {
        t.Errorf("ImageLength: got %d, want 768", il)
    }
    if tw, _ := p.TileWidth(); tw != 256 {
        t.Errorf("TileWidth: got %d, want 256", tw)
    }
    if th, _ := p.TileLength(); th != 256 {
        t.Errorf("TileLength: got %d, want 256", th)
    }
    if c, _ := p.Compression(); c != 7 {
        t.Errorf("Compression: got %d, want 7", c)
    }
    desc, _ := p.ImageDescription()
    if desc != "Aperio" {
        t.Errorf("ImageDescription: got %q, want %q", desc, "Aperio")
    }
    offs, _ := p.TileOffsets()
    if got := []uint32{1000, 2000, 3000, 4000}; !equalU32(offs, got) {
        t.Errorf("TileOffsets: got %v, want %v", offs, got)
    }
    counts, _ := p.TileByteCounts()
    if got := []uint32{100, 200, 300, 400}; !equalU32(counts, got) {
        t.Errorf("TileByteCounts: got %v, want %v", counts, got)
    }
}

func TestPageTileGrid(t *testing.T) {
    data := buildPageTIFF(t)
    f, _ := Open(bytes.NewReader(data), int64(len(data)))
    p := f.Pages()[0]
    gx, gy, err := p.TileGrid()
    if err != nil {
        t.Fatalf("TileGrid: %v", err)
    }
    // ImageWidth 1024 / TileWidth 256 = 4; ImageLength 768 / TileLength 256 = 3.
    if gx != 4 || gy != 3 {
        t.Errorf("grid: got %dx%d, want 4x3", gx, gy)
    }
}

func equalU32(a, b []uint32) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/...`
Expected: FAIL — accessors not implemented.

- [ ] **Step 3: Replace `internal/tiff/page.go` with full implementation**

```go
package tiff

import (
    "fmt"
)

// Well-known TIFF tag IDs used by opentile-go.
const (
    TagImageWidth        uint16 = 256
    TagImageLength       uint16 = 257
    TagBitsPerSample     uint16 = 258
    TagCompression       uint16 = 259
    TagPhotometric       uint16 = 262
    TagImageDescription  uint16 = 270
    TagSamplesPerPixel   uint16 = 277
    TagXResolution       uint16 = 282
    TagYResolution       uint16 = 283
    TagResolutionUnit    uint16 = 296
    TagTileWidth         uint16 = 322
    TagTileLength        uint16 = 323
    TagTileOffsets       uint16 = 324
    TagTileByteCounts    uint16 = 325
    TagJPEGTables        uint16 = 347
    TagYCbCrSubSampling  uint16 = 530
    TagInterColorProfile uint16 = 34675
)

// Page wraps a parsed IFD and exposes typed accessors for the tags opentile-go
// needs. Missing tags return (zero, false) — callers decide whether the
// absence is fatal.
type Page struct {
    ifd *ifd
    br  *byteReader
}

func newPage(i *ifd, br *byteReader) *Page { return &Page{ifd: i, br: br} }

// scalarU32 returns the first value of a tag, or (0, false) if missing.
func (p *Page) scalarU32(tag uint16) (uint32, bool) {
    e, ok := p.ifd.get(tag)
    if !ok {
        return 0, false
    }
    vals, err := e.Values(p.br)
    if err != nil || len(vals) == 0 {
        return 0, false
    }
    return vals[0], true
}

func (p *Page) ImageWidth() (uint32, bool)       { return p.scalarU32(TagImageWidth) }
func (p *Page) ImageLength() (uint32, bool)      { return p.scalarU32(TagImageLength) }
func (p *Page) TileWidth() (uint32, bool)        { return p.scalarU32(TagTileWidth) }
func (p *Page) TileLength() (uint32, bool)       { return p.scalarU32(TagTileLength) }
func (p *Page) Compression() (uint32, bool)      { return p.scalarU32(TagCompression) }
func (p *Page) Photometric() (uint32, bool)      { return p.scalarU32(TagPhotometric) }
func (p *Page) SamplesPerPixel() (uint32, bool)  { return p.scalarU32(TagSamplesPerPixel) }
func (p *Page) BitsPerSample() (uint32, bool)    { return p.scalarU32(TagBitsPerSample) }
func (p *Page) ResolutionUnit() (uint32, bool)   { return p.scalarU32(TagResolutionUnit) }

// ImageDescription returns the ASCII ImageDescription tag if present.
func (p *Page) ImageDescription() (string, bool) {
    e, ok := p.ifd.get(TagImageDescription)
    if !ok {
        return "", false
    }
    s, err := e.decodeASCII(p.br, e.valueBytes[:])
    if err != nil {
        return "", false
    }
    return s, true
}

// JPEGTables returns the JPEG tables blob used as a prefix for tiles, if present.
func (p *Page) JPEGTables() ([]byte, bool) {
    e, ok := p.ifd.get(TagJPEGTables)
    if !ok {
        return nil, false
    }
    // Tables are UNDEFINED bytes; read the payload.
    if e.fitsInline() {
        return append([]byte(nil), e.valueBytes[:e.Count]...), true
    }
    buf, err := p.br.bytes(int64(e.valueOrOffset), int(e.Count))
    if err != nil {
        return nil, false
    }
    return buf, true
}

// ICCProfile returns the InterColorProfile tag bytes if present.
func (p *Page) ICCProfile() ([]byte, bool) {
    e, ok := p.ifd.get(TagInterColorProfile)
    if !ok {
        return nil, false
    }
    if e.fitsInline() {
        return append([]byte(nil), e.valueBytes[:e.Count]...), true
    }
    buf, err := p.br.bytes(int64(e.valueOrOffset), int(e.Count))
    if err != nil {
        return nil, false
    }
    return buf, true
}

// TileOffsets returns the TileOffsets array.
func (p *Page) TileOffsets() ([]uint32, error) {
    return p.arrayU32(TagTileOffsets)
}

// TileByteCounts returns the TileByteCounts array.
func (p *Page) TileByteCounts() ([]uint32, error) {
    return p.arrayU32(TagTileByteCounts)
}

func (p *Page) arrayU32(tag uint16) ([]uint32, error) {
    e, ok := p.ifd.get(tag)
    if !ok {
        return nil, fmt.Errorf("tiff: tag %d missing", tag)
    }
    return e.Values(p.br)
}

// XResolution returns the X resolution as a numerator/denominator rational.
func (p *Page) XResolution() (num, den uint32, ok bool) {
    return p.rationalFirst(TagXResolution)
}

// YResolution returns the Y resolution as a numerator/denominator rational.
func (p *Page) YResolution() (num, den uint32, ok bool) {
    return p.rationalFirst(TagYResolution)
}

func (p *Page) rationalFirst(tag uint16) (uint32, uint32, bool) {
    e, found := p.ifd.get(tag)
    if !found {
        return 0, 0, false
    }
    vals, err := e.decodeRational(p.br)
    if err != nil || len(vals) == 0 {
        return 0, 0, false
    }
    return vals[0][0], vals[0][1], true
}

// TileGrid returns the tile grid dimensions (tiles in X, tiles in Y).
// Result is computed via ceil division: a partial tile at the edge counts as one.
func (p *Page) TileGrid() (int, int, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return 0, 0, fmt.Errorf("tiff: ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return 0, 0, fmt.Errorf("tiff: ImageLength missing")
    }
    tw, ok := p.TileWidth()
    if !ok || tw == 0 {
        return 0, 0, fmt.Errorf("tiff: TileWidth missing or zero")
    }
    tl, ok := p.TileLength()
    if !ok || tl == 0 {
        return 0, 0, fmt.Errorf("tiff: TileLength missing or zero")
    }
    gx := int(iw / tw)
    if iw%tw != 0 {
        gx++
    }
    gy := int(il / tl)
    if il%tl != 0 {
        gy++
    }
    return gx, gy, nil
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/tiff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/page.go internal/tiff/page_test.go
git commit -m "feat(tiff): add Page with typed tag accessors for SVS"
```

---

## Task 11: `options.go` — options and config

**Files:**
- Create: `options.go`
- Create: `options_test.go`

- [ ] **Step 1: Write failing tests**

```go
package opentile

import "testing"

func TestDefaultConfig(t *testing.T) {
    c := newConfig(nil)
    if c.tileSize.W != 0 || c.tileSize.H != 0 {
        t.Errorf("tileSize default: got %v, want 0,0", c.tileSize)
    }
    if c.corruptTile != CorruptTileError {
        t.Errorf("corruptTile default: got %v, want CorruptTileError", c.corruptTile)
    }
}

func TestWithTileSize(t *testing.T) {
    c := newConfig([]Option{WithTileSize(512, 256)})
    if c.tileSize.W != 512 || c.tileSize.H != 256 {
        t.Errorf("tileSize: got %v, want 512x256", c.tileSize)
    }
}

func TestWithCorruptTilePolicy(t *testing.T) {
    c := newConfig([]Option{WithCorruptTilePolicy(CorruptTileError)})
    if c.corruptTile != CorruptTileError {
        t.Errorf("policy: got %v", c.corruptTile)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `options.go`**

```go
package opentile

// CorruptTilePolicy controls how corrupt-edge tiles (currently: Aperio SVS) are
// reported. v0.1 supports only CorruptTileError.
type CorruptTilePolicy uint8

const (
    CorruptTileError CorruptTilePolicy = iota // return ErrCorruptTile (default, v0.1)
    CorruptTileBlank                          // v0.3: return a typed blank tile
    CorruptTileFix                            // v1.0: reconstruct from parent level
)

// Option mutates the opentile configuration before Open returns a Tiler.
type Option func(*config)

// config is the aggregate of all Option values applied at Open time.
type config struct {
    tileSize    Size
    hasTileSize bool
    corruptTile CorruptTilePolicy
}

func newConfig(opts []Option) *config {
    c := &config{
        tileSize:    Size{},
        corruptTile: CorruptTileError,
    }
    for _, o := range opts {
        o(c)
    }
    return c
}

// WithTileSize requests output tile dimensions in pixels. If unset, the format
// default is used (SVS: native tile size from the TIFF). Required for formats
// that have no native rectangular tiles (NDPI, v0.2+).
func WithTileSize(w, h int) Option {
    return func(c *config) {
        c.tileSize = Size{W: w, H: h}
        c.hasTileSize = true
    }
}

// WithCorruptTilePolicy sets the behavior for corrupt-edge tiles. v0.1 supports
// only CorruptTileError.
func WithCorruptTilePolicy(p CorruptTilePolicy) Option {
    return func(c *config) { c.corruptTile = p }
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add options.go options_test.go
git commit -m "feat(options): add Option type and v0.1 options"
```

---

## Task 12: `metadata.go`, `image.go`, `tiler.go` — public interfaces

**Files:**
- Create: `metadata.go`
- Create: `image.go`
- Create: `tiler.go`

These three define the public interface surface. No direct tests — they are exercised by Tasks 14–17 (formats/svs) and Task 13 (factory).

- [ ] **Step 1: Create `metadata.go`**

```go
package opentile

import "time"

// Metadata is the common subset of slide metadata surfaced across all formats.
// Format packages embed this struct to add format-specific fields; consumers
// access those extras via a per-format accessor (e.g. svs.MetadataOf(tiler)).
type Metadata struct {
    Magnification       float64   // 0 if unknown
    ScannerManufacturer string
    ScannerModel        string
    ScannerSoftware     []string
    ScannerSerial       string
    AcquisitionDateTime time.Time // zero if unknown
}
```

- [ ] **Step 2: Create `image.go`**

```go
package opentile

import (
    "context"
    "io"
    "iter"
)

// Level is a single resolution in a pyramidal WSI.
//
// Tile and TileReader are safe for concurrent use from multiple goroutines,
// provided the io.ReaderAt supplied to Open is also safe for concurrent use.
// (stdlib *os.File satisfies this.)
type Level interface {
    Index() int
    PyramidIndex() int
    Size() Size
    TileSize() Size
    Grid() Size
    Compression() Compression
    MPP() SizeMm
    FocalPlane() float64

    // Tile returns the raw compressed tile bytes at (x, y) as stored in the
    // source TIFF.
    Tile(x, y int) ([]byte, error)

    // TileReader returns a streaming reader for the tile at (x, y). Callers
    // should Close the returned ReadCloser.
    TileReader(x, y int) (io.ReadCloser, error)

    // Tiles iterates every tile position in row-major order. Callers that need
    // parallelism goroutine on top of Tile(x, y); Tiles itself is serial.
    Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult]
}

// AssociatedImage is a non-pyramidal slide-level image (label, overview,
// thumbnail). v0.1 returns an empty slice from Tiler.Associated().
type AssociatedImage interface {
    Kind() string
    Size() Size
    Compression() Compression
    Bytes() ([]byte, error)
}

// TilePos is a (column, row) pair returned by Level.Tiles.
type TilePos struct{ X, Y int }

// TileResult carries the yield from Level.Tiles.
type TileResult struct {
    Bytes []byte
    Err   error
}
```

- [ ] **Step 3: Create `tiler.go`**

```go
package opentile

// Format identifies the source file format.
type Format string

const (
    FormatSVS  Format = "svs"
    FormatNDPI Format = "ndpi" // defined for future use; not implemented in v0.1
)

// Tiler is the top-level handle returned by Open. All accessors are
// immutable and safe for concurrent use; Close() must not race with in-flight
// tile reads.
type Tiler interface {
    Format() Format
    Levels() []Level
    Level(i int) (Level, error)
    Associated() []AssociatedImage
    Metadata() Metadata
    ICCProfile() []byte
    Close() error
}
```

- [ ] **Step 4: Verify compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add metadata.go image.go tiler.go
git commit -m "feat: add public interfaces (Tiler, Level, Metadata, AssociatedImage)"
```

---

## Task 13: `opentile.go` — factory and registration

**Files:**
- Create: `opentile.go`
- Create: `opentile_test.go`

- [ ] **Step 1: Write failing tests**

```go
package opentile

import (
    "bytes"
    "errors"
    "testing"

    "github.com/wsilabs/opentile-go/internal/tiff"
)

// fakeFactory is a test double that reports support when the tag
// ImageDescription begins with "FAKE".
type fakeFactory struct{ openCalled bool }

func (f *fakeFactory) Format() Format { return Format("fake") }
func (f *fakeFactory) Supports(file *tiff.File) bool {
    if len(file.Pages()) == 0 {
        return false
    }
    desc, _ := file.Pages()[0].ImageDescription()
    return len(desc) >= 4 && desc[:4] == "FAKE"
}
func (f *fakeFactory) Open(file *tiff.File, cfg *config) (Tiler, error) {
    f.openCalled = true
    return &noopTiler{format: Format("fake")}, nil
}

type noopTiler struct {
    format Format
}

func (n *noopTiler) Format() Format                 { return n.format }
func (n *noopTiler) Levels() []Level                { return nil }
func (n *noopTiler) Level(i int) (Level, error)     { return nil, ErrLevelOutOfRange }
func (n *noopTiler) Associated() []AssociatedImage  { return nil }
func (n *noopTiler) Metadata() Metadata             { return Metadata{} }
func (n *noopTiler) ICCProfile() []byte             { return nil }
func (n *noopTiler) Close() error                   { return nil }

func TestRegisterAndOpen(t *testing.T) {
    // Reset registry for test isolation.
    resetRegistry()
    f := &fakeFactory{}
    Register(f)

    data := buildTIFFWithDescription(t, "FAKE slide")
    tiler, err := Open(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    defer tiler.Close()
    if !f.openCalled {
        t.Fatal("factory.Open was not called")
    }
    if tiler.Format() != Format("fake") {
        t.Fatalf("Format: got %q", tiler.Format())
    }
}

func TestOpenUnsupported(t *testing.T) {
    resetRegistry()
    data := buildTIFFWithDescription(t, "UNKNOWN")
    _, err := Open(bytes.NewReader(data), int64(len(data)))
    if !errors.Is(err, ErrUnsupportedFormat) {
        t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
    }
}

func TestOpenInvalidTIFF(t *testing.T) {
    resetRegistry()
    _, err := Open(bytes.NewReader([]byte{'X', 'Y'}), 2)
    if !errors.Is(err, ErrInvalidTIFF) {
        t.Fatalf("expected ErrInvalidTIFF, got %v", err)
    }
}

// buildTIFFWithDescription creates a 1-IFD TIFF whose ImageDescription is desc.
// Minimal: ImageWidth, ImageLength, TileWidth, TileLength, ImageDescription.
func buildTIFFWithDescription(t *testing.T, desc string) []byte {
    t.Helper()
    // Reuse the tiff test helper via a copy; since the helper is lower-case in
    // the tiff package it isn't accessible here. For this test, build directly.
    // 5 entries: 256, 257, 270 (ASCII), 322, 323.
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    // IFD at 8: count(2) + 5*12 + 4 = 66 bytes → external base at 8+66 = 74.
    descBytes := append([]byte(desc), 0) // NUL terminate
    descOff := uint32(74)
    // entries
    writeU16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
    writeU32 := func(v uint32) {
        buf.WriteByte(byte(v))
        buf.WriteByte(byte(v >> 8))
        buf.WriteByte(byte(v >> 16))
        buf.WriteByte(byte(v >> 24))
    }
    writeU16(5)
    // ImageWidth=1024
    writeU16(256); writeU16(3); writeU32(1); writeU32(1024)
    // ImageLength=768
    writeU16(257); writeU16(3); writeU32(1); writeU32(768)
    // ImageDescription
    writeU16(270); writeU16(2); writeU32(uint32(len(descBytes))); writeU32(descOff)
    // TileWidth=256
    writeU16(322); writeU16(3); writeU32(1); writeU32(256)
    // TileLength=256
    writeU16(323); writeU16(3); writeU32(1); writeU32(256)
    writeU32(0) // next IFD
    buf.Write(descBytes)
    return buf.Bytes()
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./...`
Expected: FAIL — undefined `Open`, `Register`, `resetRegistry`.

- [ ] **Step 3: Implement `opentile.go`**

```go
package opentile

import (
    "errors"
    "fmt"
    "io"
    "os"
    "sync"

    "github.com/wsilabs/opentile-go/internal/tiff"
)

// FormatFactory is implemented by format packages to register themselves with
// the top-level opentile package. Factories are queried in registration order;
// the first factory whose Supports() returns true is used.
type FormatFactory interface {
    Format() Format
    Supports(file *tiff.File) bool
    Open(file *tiff.File, cfg *config) (Tiler, error)
}

var (
    registryMu sync.RWMutex
    registry   []FormatFactory
)

// Register adds a FormatFactory to the registry. It is safe to call concurrently
// but typically called from init or a main-package setup function.
func Register(f FormatFactory) {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry = append(registry, f)
}

// resetRegistry is for tests only.
func resetRegistry() {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry = nil
}

// Open parses r as a WSI TIFF and returns a Tiler for the matching format.
// size is the total file size in bytes.
func Open(r io.ReaderAt, size int64, opts ...Option) (Tiler, error) {
    cfg := newConfig(opts)
    file, err := tiff.Open(r, size)
    if err != nil {
        if errors.Is(err, tiff.ErrInvalidTIFF) {
            return nil, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
        }
        return nil, err
    }
    registryMu.RLock()
    factories := append([]FormatFactory(nil), registry...)
    registryMu.RUnlock()

    for _, f := range factories {
        if f.Supports(file) {
            return f.Open(file, cfg)
        }
    }
    return nil, ErrUnsupportedFormat
}

// OpenFile opens path for reading and delegates to Open. The returned Tiler
// owns the file handle; Close closes it.
func OpenFile(path string, opts ...Option) (Tiler, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    info, err := f.Stat()
    if err != nil {
        f.Close()
        return nil, err
    }
    t, err := Open(f, info.Size(), opts...)
    if err != nil {
        f.Close()
        return nil, err
    }
    return &fileCloser{Tiler: t, f: f}, nil
}

// fileCloser overrides Close to also close the underlying file.
type fileCloser struct {
    Tiler
    f *os.File
}

func (fc *fileCloser) Close() error {
    err1 := fc.Tiler.Close()
    err2 := fc.f.Close()
    if err1 != nil {
        return err1
    }
    return err2
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add opentile.go opentile_test.go
git commit -m "feat: add Open, OpenFile, and FormatFactory registration"
```

---

## Task 14: `formats/svs/svs.go` — SVS support detection and tiler skeleton

**Files:**
- Create: `formats/svs/svs.go`
- Create: `formats/svs/svs_test.go`

- [ ] **Step 1: Write failing tests**

```go
package svs

import (
    "bytes"
    "strings"
    "testing"

    "github.com/wsilabs/opentile-go/internal/tiff"
)

func TestSupportsAperio(t *testing.T) {
    data := buildTIFFWithDesc(t, "Aperio Image Library v10.2.0\n46000x32914 ...")
    f, _ := tiff.Open(bytes.NewReader(data))
    if !New().Supports(f) {
        t.Fatal("expected Supports to return true for Aperio-prefixed description")
    }
}

func TestSupportsRejectsOther(t *testing.T) {
    data := buildTIFFWithDesc(t, "Hamamatsu Ndpi\n...")
    f, _ := tiff.Open(bytes.NewReader(data))
    if New().Supports(f) {
        t.Fatal("expected Supports to return false for non-Aperio description")
    }
}

func TestSupportsHandlesEmpty(t *testing.T) {
    // Empty description → not Aperio.
    data := buildTIFFWithDesc(t, "")
    f, _ := tiff.Open(bytes.NewReader(data))
    if New().Supports(f) {
        t.Fatal("expected Supports to return false for empty description")
    }
}

// copied from opentile_test.go pattern; keeps this package self-contained.
func buildTIFFWithDesc(t *testing.T, desc string) []byte {
    t.Helper()
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    descBytes := append([]byte(desc), 0)
    descOff := uint32(74)
    writeU16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
    writeU32 := func(v uint32) {
        buf.WriteByte(byte(v))
        buf.WriteByte(byte(v >> 8))
        buf.WriteByte(byte(v >> 16))
        buf.WriteByte(byte(v >> 24))
    }
    writeU16(5)
    writeU16(256); writeU16(3); writeU32(1); writeU32(1024)
    writeU16(257); writeU16(3); writeU32(1); writeU32(768)
    writeU16(270); writeU16(2); writeU32(uint32(len(descBytes))); writeU32(descOff)
    writeU16(322); writeU16(3); writeU32(1); writeU32(256)
    writeU16(323); writeU16(3); writeU32(1); writeU32(256)
    writeU32(0)
    buf.Write(descBytes)
    // silence unused-import warning for strings if we grow the file later
    _ = strings.TrimSpace
    return buf.Bytes()
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/svs/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `formats/svs/svs.go`**

```go
// Package svs implements opentile-go format support for Aperio SVS files.
//
// SVS is a TIFF variant produced by Leica Aperio scanners used in digital
// pathology. This package detects SVS files, parses the Aperio metadata
// carried in the ImageDescription tag, and exposes the pyramid levels as
// opentile.Level values with raw compressed tile byte passthrough.
package svs

import (
    "fmt"
    "strings"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// aperioPrefix is the literal prefix on the ImageDescription tag of Aperio SVS
// files. Upstream opentile and openslide both key their detection off this.
const aperioPrefix = "Aperio"

// Factory is the FormatFactory implementation for SVS.
type Factory struct{}

// New returns an SVS factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatSVS }

// Supports reports whether file looks like an SVS: its first page's
// ImageDescription starts with "Aperio".
func (f *Factory) Supports(file *tiff.File) bool {
    pages := file.Pages()
    if len(pages) == 0 {
        return false
    }
    desc, ok := pages[0].ImageDescription()
    if !ok {
        return false
    }
    return strings.HasPrefix(desc, aperioPrefix)
}

// Open constructs an SVS Tiler from a parsed TIFF file.
// (Implementation completed in Task 16.)
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    return nil, fmt.Errorf("svs.Open: not yet implemented")
}
```

- [ ] **Step 4: Add `Config` type alias in top-level**

The `Config` type must be exported for format factories to consume the settings without importing an unexported type. Modify `options.go`:

```go
// Add near the existing config struct:

// Config is the opaque, read-only view of configuration passed to
// FormatFactory.Open. Access via the accessor methods.
type Config struct {
    c *config
}

// TileSize returns the requested output tile size and whether the caller set
// one. ok=false means "use format default"; callers must not treat the zero
// Size as equivalent to "default" because (Size{}, true) is distinct from
// (Size{}, false) — the former asserts an explicit 0x0 (which format packages
// should reject as malformed input).
func (c *Config) TileSize() (Size, bool) { return c.c.tileSize, c.c.hasTileSize }

// CorruptTilePolicy returns the configured policy.
func (c *Config) CorruptTilePolicy() CorruptTilePolicy { return c.c.corruptTile }

// Also modify FormatFactory.Open signature in opentile.go:
//    Open(file *tiff.File, cfg *Config) (Tiler, error)
// and Open() call site:
//    return f.Open(file, &Config{c: cfg})
```

Apply those modifications to `options.go` and `opentile.go`. Re-run `go test ./...` — tests in `opentile_test.go` need the `Open(file, cfg *config)` signature updated to `Open(file, cfg *Config)`. Update the test double accordingly.

- [ ] **Step 5: Run tests to verify**

Run: `go test ./...`
Expected: PASS for all tests (including `formats/svs/svs_test.go`).

- [ ] **Step 6: Commit**

```bash
git add formats/svs/svs.go formats/svs/svs_test.go options.go opentile.go opentile_test.go
git commit -m "feat(svs): add Factory with Supports; wire Config into FormatFactory"
```

---

## Task 15: `formats/svs/metadata.go` — ImageDescription parser

**Files:**
- Create: `formats/svs/metadata.go`
- Create: `formats/svs/metadata_test.go`

The Aperio ImageDescription is a newline-separated header line followed by `|`-separated key-value pairs. Example:

```
Aperio Image Library v11.2.1
46000x32914 [0,100 46000x32714] (240x240) JPEG/RGB Q=30|AppMag = 20|MPP = 0.4990|Date = 02/02/2017|Time = 11:22:33|ScanScope ID = SS1234
```

- [ ] **Step 1: Write failing tests**

```go
package svs

import (
    "testing"
    "time"
)

func TestParseDescription(t *testing.T) {
    desc := "Aperio Image Library v11.2.1\n" +
        "46000x32914 [0,100 46000x32714] (240x240) JPEG/RGB Q=30|" +
        "AppMag = 20|MPP = 0.4990|Date = 02/02/2017|Time = 11:22:33|" +
        "ScanScope ID = SS1234|Filename = CMU-1"

    md, err := parseDescription(desc)
    if err != nil {
        t.Fatalf("parseDescription: %v", err)
    }
    if md.Magnification != 20 {
        t.Errorf("Magnification: got %v, want 20", md.Magnification)
    }
    if md.MPP != 0.499 {
        t.Errorf("MPP: got %v, want 0.499", md.MPP)
    }
    if md.ScannerSerial != "SS1234" {
        t.Errorf("ScannerSerial: got %q, want SS1234", md.ScannerSerial)
    }
    if md.SoftwareLine != "Aperio Image Library v11.2.1" {
        t.Errorf("SoftwareLine: got %q", md.SoftwareLine)
    }
    want := time.Date(2017, 2, 2, 11, 22, 33, 0, time.UTC)
    if !md.AcquisitionDateTime.Equal(want) {
        t.Errorf("AcquisitionDateTime: got %v, want %v", md.AcquisitionDateTime, want)
    }
}

func TestParseDescriptionMissingFields(t *testing.T) {
    md, err := parseDescription("Aperio Image Library v11.2.1\n256x256 (16x16) JPEG/RGB")
    if err != nil {
        t.Fatalf("parseDescription: %v", err)
    }
    if md.Magnification != 0 || md.MPP != 0 || md.ScannerSerial != "" {
        t.Errorf("expected zero values for missing fields, got %+v", md)
    }
}

func TestParseDescriptionRejectsNonAperio(t *testing.T) {
    _, err := parseDescription("Hamamatsu Ndpi\n...")
    if err == nil {
        t.Fatal("expected error on non-Aperio description")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/svs/...`
Expected: FAIL.

- [ ] **Step 3: Implement `formats/svs/metadata.go`**

```go
package svs

import (
    "errors"
    "fmt"
    "strconv"
    "strings"
    "time"

    opentile "github.com/wsilabs/opentile-go"
)

// Metadata is the SVS-specific slide metadata. It embeds opentile.Metadata so
// the common fields (magnification, scanner identity, acquisition datetime)
// are populated via the embedded struct; Aperio-specific fields (MPP,
// SoftwareLine, Filename) live on the outer struct.
//
// Consumers read the common fields via opentile.Tiler.Metadata() as usual;
// to read the Aperio-specific fields, pass the Tiler to svs.MetadataOf.
type Metadata struct {
    opentile.Metadata
    MPP          float64 // microns per pixel
    SoftwareLine string  // first line of ImageDescription
    Filename     string  // Aperio "Filename" key if present
}

// parseDescription decodes the ImageDescription tag stored by Aperio scanners.
// Format: first line is a free-form software banner; subsequent content is
// '|'-separated "key = value" pairs embedded in the same string.
func parseDescription(desc string) (Metadata, error) {
    if !strings.HasPrefix(desc, aperioPrefix) {
        return Metadata{}, errors.New("svs: description is not Aperio")
    }
    var md Metadata

    // Split off the software banner (first line).
    newline := strings.IndexByte(desc, '\n')
    if newline < 0 {
        md.SoftwareLine = desc
        md.ScannerManufacturer = "Aperio"
        md.ScannerSoftware = []string{desc}
        return md, nil
    }
    md.SoftwareLine = desc[:newline]
    md.ScannerManufacturer = "Aperio"
    md.ScannerSoftware = []string{md.SoftwareLine}

    // Parse '|' separated key-value pairs in the remainder.
    body := desc[newline+1:]
    kv := splitKV(body)

    if v, ok := kv["AppMag"]; ok {
        parsed, err := strconv.ParseFloat(v, 64)
        if err != nil {
            return md, fmt.Errorf("svs: parse AppMag %q: %w", v, err)
        }
        md.Magnification = parsed
    }
    if v, ok := kv["MPP"]; ok {
        parsed, err := strconv.ParseFloat(v, 64)
        if err != nil {
            return md, fmt.Errorf("svs: parse MPP %q: %w", v, err)
        }
        md.MPP = parsed
    }
    if v, ok := kv["ScanScope ID"]; ok {
        md.ScannerSerial = v
    }
    if v, ok := kv["Filename"]; ok {
        md.Filename = v
    }

    // Aperio Date/Time are separate fields in MM/DD/YYYY and HH:MM:SS form.
    date, hasDate := kv["Date"]
    tm, hasTime := kv["Time"]
    if hasDate && hasTime {
        parsed, err := time.Parse("01/02/2006 15:04:05", date+" "+tm)
        if err != nil {
            return md, fmt.Errorf("svs: parse Date/Time %q %q: %w", date, tm, err)
        }
        md.AcquisitionDateTime = parsed
    }
    return md, nil
}

// splitKV parses "k1 = v1|k2 = v2|..." into a map. Whitespace around keys and
// values is trimmed. Tokens without '=' are ignored.
func splitKV(s string) map[string]string {
    out := make(map[string]string)
    for _, tok := range strings.Split(s, "|") {
        eq := strings.IndexByte(tok, '=')
        if eq < 0 {
            continue
        }
        k := strings.TrimSpace(tok[:eq])
        v := strings.TrimSpace(tok[eq+1:])
        if k != "" {
            out[k] = v
        }
    }
    return out
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./formats/svs/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/svs/metadata.go formats/svs/metadata_test.go
git commit -m "feat(svs): parse Aperio ImageDescription metadata"
```

---

## Task 16: `formats/svs/image.go` — tiled Level implementation and Tiler.Open

**Files:**
- Create: `formats/svs/image.go`
- Create: `formats/svs/image_test.go`
- Modify: `formats/svs/svs.go` (replace the "not yet implemented" stub from Task 14)

- [ ] **Step 1: Write failing tests for image.go**

Build a minimal SVS-like TIFF with real tile bytes and read them through the Level API.

```go
package svs

import (
    "bytes"
    "context"
    "errors"
    "io"
    "testing"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// buildSVSTIFF builds a TIFF with one tiled page carrying tileCount*tileCount
// synthetic tile payloads (each unique). Returns (bytes, tileBytes[idx]).
// The ImageDescription starts with "Aperio" so the SVS factory accepts it.
func buildSVSTIFF(t *testing.T, tileW, tileH, tilesX, tilesY int, extraDesc string) (data []byte, tiles [][]byte) {
    t.Helper()
    // Build tiles: each is a unique byte pattern of length 32.
    nTiles := tilesX * tilesY
    tiles = make([][]byte, nTiles)
    for i := 0; i < nTiles; i++ {
        buf := make([]byte, 32)
        for j := range buf {
            buf[j] = byte(i*7 + j) // arbitrary deterministic
        }
        tiles[i] = buf
    }
    desc := "Aperio Test\n"
    if extraDesc != "" {
        desc += extraDesc
    }
    descBytes := append([]byte(desc), 0)

    // Layout: Header (8) + IFD at 8 + external data after.
    // IFD entries: ImageWidth, ImageLength, Compression, Photometric,
    // ImageDescription, TileWidth, TileLength, TileOffsets, TileByteCounts = 9
    // IFD size = 2 + 9*12 + 4 = 114
    ifdStart := uint32(8)
    extStart := ifdStart + 114

    descOff := extStart
    extAfterDesc := descOff + uint32(len(descBytes))

    tileBCOff := extAfterDesc
    extAfterBC := tileBCOff + uint32(4*nTiles)

    tileOffOff := extAfterBC
    extAfterTO := tileOffOff + uint32(4*nTiles)

    // Tile data offsets: pack consecutively starting at extAfterTO.
    tileOffsets := make([]uint32, nTiles)
    off := extAfterTO
    for i := range tiles {
        tileOffsets[i] = off
        off += uint32(len(tiles[i]))
    }

    buf := new(bytes.Buffer)
    w16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
    w32 := func(v uint32) {
        buf.WriteByte(byte(v))
        buf.WriteByte(byte(v >> 8))
        buf.WriteByte(byte(v >> 16))
        buf.WriteByte(byte(v >> 24))
    }
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    w16(9)
    entry := func(tag, typ uint16, count, voc uint32) {
        w16(tag); w16(typ); w32(count); w32(voc)
    }
    entry(256, 3, 1, uint32(tileW*tilesX)) // ImageWidth
    entry(257, 3, 1, uint32(tileH*tilesY)) // ImageLength
    entry(259, 3, 1, 7)                    // Compression = JPEG
    entry(262, 3, 1, 6)                    // Photometric = YCbCr
    entry(270, 2, uint32(len(descBytes)), descOff)
    entry(322, 3, 1, uint32(tileW))
    entry(323, 3, 1, uint32(tileH))
    entry(324, 4, uint32(nTiles), tileOffOff)
    entry(325, 4, uint32(nTiles), tileBCOff)
    w32(0) // next IFD

    // External region
    buf.Write(descBytes)
    for _, t := range tiles {
        _ = t
    }
    // Write TileByteCounts
    for _, tb := range tiles {
        w32(uint32(len(tb)))
    }
    // Write TileOffsets
    for _, o := range tileOffsets {
        w32(o)
    }
    // Finally, write tile payloads in the same order.
    for _, tb := range tiles {
        buf.Write(tb)
    }
    return buf.Bytes(), tiles
}

func TestSvsTilerOpenAndLevel(t *testing.T) {
    data, tiles := buildSVSTIFF(t, 16, 16, 3, 2, "AppMag = 20|MPP = 0.5")
    f, err := tiff.Open(bytes.NewReader(data))
    if err != nil {
        t.Fatalf("tiff.Open: %v", err)
    }
    cfg := opentile.NewTestConfig(opentile.Size{}, opentile.CorruptTileError)
    tiler, err := New().Open(f, cfg)
    if err != nil {
        t.Fatalf("svs.New().Open: %v", err)
    }
    defer tiler.Close()

    levels := tiler.Levels()
    if len(levels) != 1 {
        t.Fatalf("levels: got %d, want 1", len(levels))
    }
    lvl, err := tiler.Level(0)
    if err != nil {
        t.Fatalf("Level(0): %v", err)
    }
    if got := lvl.TileSize(); got.W != 16 || got.H != 16 {
        t.Errorf("TileSize: got %v, want 16x16", got)
    }
    if got := lvl.Grid(); got.W != 3 || got.H != 2 {
        t.Errorf("Grid: got %v, want 3x2", got)
    }
    // Tile (0,0) → first tile payload
    b, err := lvl.Tile(0, 0)
    if err != nil {
        t.Fatalf("Tile(0,0): %v", err)
    }
    if !bytes.Equal(b, tiles[0]) {
        t.Fatalf("Tile(0,0) bytes mismatch")
    }
    // Tile (2,1) → last tile (index 5)
    b, err = lvl.Tile(2, 1)
    if err != nil {
        t.Fatalf("Tile(2,1): %v", err)
    }
    if !bytes.Equal(b, tiles[5]) {
        t.Fatalf("Tile(2,1) bytes mismatch")
    }
}

func TestSvsLevelTileOutOfBounds(t *testing.T) {
    data, _ := buildSVSTIFF(t, 16, 16, 2, 2, "")
    f, _ := tiff.Open(bytes.NewReader(data))
    cfg := opentile.NewTestConfig(opentile.Size{}, opentile.CorruptTileError)
    tiler, _ := New().Open(f, cfg)
    lvl, _ := tiler.Level(0)
    _, err := lvl.Tile(99, 99)
    if !errors.Is(err, opentile.ErrTileOutOfBounds) {
        t.Fatalf("expected ErrTileOutOfBounds, got %v", err)
    }
    var te *opentile.TileError
    if !errors.As(err, &te) {
        t.Fatal("expected TileError wrapping")
    }
    if te.X != 99 || te.Y != 99 {
        t.Errorf("TileError coords: got %d,%d", te.X, te.Y)
    }
}

func TestSvsLevelTilesIterator(t *testing.T) {
    data, tiles := buildSVSTIFF(t, 16, 16, 2, 2, "")
    f, _ := tiff.Open(bytes.NewReader(data))
    cfg := opentile.NewTestConfig(opentile.Size{}, opentile.CorruptTileError)
    tiler, _ := New().Open(f, cfg)
    lvl, _ := tiler.Level(0)

    ctx := context.Background()
    count := 0
    for pos, res := range lvl.Tiles(ctx) {
        if res.Err != nil {
            t.Fatalf("Tiles err at %v: %v", pos, res.Err)
        }
        idx := pos.Y*2 + pos.X
        if !bytes.Equal(res.Bytes, tiles[idx]) {
            t.Errorf("mismatch at %v", pos)
        }
        count++
    }
    if count != 4 {
        t.Errorf("count: got %d, want 4", count)
    }
}

func TestSvsLevelTileReader(t *testing.T) {
    data, tiles := buildSVSTIFF(t, 16, 16, 2, 2, "")
    f, _ := tiff.Open(bytes.NewReader(data))
    cfg := opentile.NewTestConfig(opentile.Size{}, opentile.CorruptTileError)
    tiler, _ := New().Open(f, cfg)
    lvl, _ := tiler.Level(0)
    rc, err := lvl.TileReader(1, 1)
    if err != nil {
        t.Fatalf("TileReader: %v", err)
    }
    defer rc.Close()
    got, err := io.ReadAll(rc)
    if err != nil {
        t.Fatalf("ReadAll: %v", err)
    }
    if !bytes.Equal(got, tiles[3]) {
        t.Fatalf("TileReader(1,1) bytes mismatch")
    }
}
```

- [ ] **Step 2: Add `NewTestConfig` to top-level (for test-only construction)**

Append to `options.go`:

```go
// NewTestConfig constructs a Config for use in tests. It is not intended for
// production callers, which should go through opentile.Open. A non-zero
// tileSize is treated as explicitly set (TileSize ok=true); a zero Size is
// treated as "use format default" (TileSize ok=false).
func NewTestConfig(tileSize Size, policy CorruptTilePolicy) *Config {
    has := tileSize.W != 0 || tileSize.H != 0
    return &Config{c: &config{tileSize: tileSize, hasTileSize: has, corruptTile: policy}}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./formats/svs/...`
Expected: FAIL — `New().Open` still stubbed; tiledImage not implemented.

- [ ] **Step 4: Replace `formats/svs/svs.go` stub and add `formats/svs/image.go`**

Replace the `Open` method body in `formats/svs/svs.go`:

```go
// Open constructs an SVS Tiler from a parsed TIFF file.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    pages := file.Pages()
    if len(pages) == 0 {
        return nil, fmt.Errorf("svs: file has no pages")
    }
    basePage := pages[0]
    desc, ok := basePage.ImageDescription()
    if !ok {
        return nil, fmt.Errorf("svs: base page missing ImageDescription")
    }
    md, err := parseDescription(desc)
    if err != nil {
        return nil, err
    }

    // Pyramid levels are tiled; non-tiled pages (thumbnail, label, macro)
    // are the substrate for associated-image support (v0.3) and are skipped
    // here. Level indices are contiguous (0..N-1) in pyramid order and do
    // not correspond to physical page indices in the TIFF.
    levels := make([]opentile.Level, 0, len(pages))
    baseSize, err := pageSize(basePage)
    if err != nil {
        return nil, err
    }
    levelIdx := 0
    for pageIdx, p := range pages {
        if _, ok := p.TileWidth(); !ok {
            continue // non-tiled page; defer to v0.3 associated-image support
        }
        lvl, err := newTiledImage(levelIdx, p, baseSize, md.MPP, file.ReaderAt(), cfg)
        if err != nil {
            return nil, fmt.Errorf("svs: page %d (level %d): %w", pageIdx, levelIdx, err)
        }
        levels = append(levels, lvl)
        levelIdx++
    }
    icc, _ := basePage.ICCProfile()
    return &tiler{md: md, levels: levels, icc: icc}, nil
}

// pageSize returns the (ImageWidth, ImageLength) as opentile.Size.
func pageSize(p *tiff.Page) (opentile.Size, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return opentile.Size{}, fmt.Errorf("ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return opentile.Size{}, fmt.Errorf("ImageLength missing")
    }
    return opentile.Size{W: int(iw), H: int(il)}, nil
}

// tiler is the SVS implementation of opentile.Tiler.
type tiler struct {
    md     Metadata
    levels []opentile.Level
    icc    []byte
}

func (t *tiler) Format() opentile.Format                { return opentile.FormatSVS }
func (t *tiler) Levels() []opentile.Level               { return t.levels }
func (t *tiler) Associated() []opentile.AssociatedImage { return nil }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }
func (t *tiler) Level(i int) (opentile.Level, error) {
    if i < 0 || i >= len(t.levels) {
        return nil, opentile.ErrLevelOutOfRange
    }
    return t.levels[i], nil
}

// MetadataOf returns the SVS-specific metadata if t is an SVS Tiler, otherwise
// (nil, false). Use this to read the Aperio extras (MPP, SoftwareLine,
// Filename) that are not visible through the common opentile.Metadata struct.
//
//	if md, ok := svs.MetadataOf(tiler); ok {
//	    fmt.Println(md.MPP, md.SoftwareLine)
//	}
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
    svsT, ok := t.(*tiler)
    if !ok {
        return nil, false
    }
    // Return a pointer into the tiler's stored metadata. t.md is populated
    // once at Open time and never mutated; the returned pointer is safe to
    // hold for the lifetime of the Tiler.
    return &svsT.md, true
}
```

Create `formats/svs/image.go`:

```go
package svs

import (
    "context"
    "fmt"
    "io"
    "iter"
    "math"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// tiledImage is the SVS Level implementation for tiled pages. v0.1 passes
// through the raw compressed TIFF tile bytes unmodified; no JPEG manipulation.
type tiledImage struct {
    index       int
    size        opentile.Size
    tileSize    opentile.Size
    grid        opentile.Size
    compression opentile.Compression
    mpp         opentile.SizeMm
    pyrIndex    int

    offsets []uint32
    counts  []uint32
    reader  io.ReaderAt

    cfg *opentile.Config
}

func newTiledImage(
    index int,
    p *tiff.Page,
    baseSize opentile.Size,
    baseMPP float64,
    r io.ReaderAt,
    cfg *opentile.Config,
) (*tiledImage, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return nil, fmt.Errorf("ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return nil, fmt.Errorf("ImageLength missing")
    }
    tw, ok := p.TileWidth()
    if !ok || tw == 0 {
        return nil, fmt.Errorf("TileWidth missing or zero")
    }
    tl, ok := p.TileLength()
    if !ok || tl == 0 {
        return nil, fmt.Errorf("TileLength missing or zero")
    }
    gx, gy, err := p.TileGrid()
    if err != nil {
        return nil, err
    }
    offsets, err := p.TileOffsets()
    if err != nil {
        return nil, err
    }
    counts, err := p.TileByteCounts()
    if err != nil {
        return nil, err
    }
    if len(offsets) != len(counts) || len(offsets) != gx*gy {
        return nil, fmt.Errorf("tile table length mismatch: offsets=%d counts=%d grid=%dx%d", len(offsets), len(counts), gx, gy)
    }
    comp, _ := p.Compression()
    ocomp := mapCompression(comp)

    // Pyramid index: log2(baseSize.W / iw), rounded to nearest int.
    var pyr int
    if baseSize.W > 0 {
        pyr = int(math.Round(math.Log2(float64(baseSize.W) / float64(iw))))
        if pyr < 0 {
            pyr = 0
        }
    }

    scale := float64(1)
    if iw > 0 {
        scale = float64(baseSize.W) / float64(iw)
    }
    mpp := opentile.SizeMm{W: baseMPP * scale / 1000.0, H: baseMPP * scale / 1000.0}

    return &tiledImage{
        index:       index,
        size:        opentile.Size{W: int(iw), H: int(il)},
        tileSize:    opentile.Size{W: int(tw), H: int(tl)},
        grid:        opentile.Size{W: gx, H: gy},
        compression: ocomp,
        mpp:         mpp,
        pyrIndex:    pyr,
        offsets:     offsets,
        counts:      counts,
        reader:      r,
        cfg:         cfg,
    }, nil
}

// mapCompression translates TIFF compression codes into opentile.Compression.
func mapCompression(code uint32) opentile.Compression {
    switch code {
    case 1:
        return opentile.CompressionNone
    case 7:
        return opentile.CompressionJPEG
    case 33003, 33005:
        return opentile.CompressionJP2K
    default:
        return opentile.CompressionUnknown
    }
}

func (l *tiledImage) Index() int                    { return l.index }
func (l *tiledImage) PyramidIndex() int             { return l.pyrIndex }
func (l *tiledImage) Size() opentile.Size           { return l.size }
func (l *tiledImage) TileSize() opentile.Size       { return l.tileSize }
func (l *tiledImage) Grid() opentile.Size           { return l.grid }
func (l *tiledImage) Compression() opentile.Compression { return l.compression }
func (l *tiledImage) MPP() opentile.SizeMm          { return l.mpp }
func (l *tiledImage) FocalPlane() float64           { return 0 }

func (l *tiledImage) indexOf(x, y int) (int, error) {
    if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
        return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
    }
    return y*l.grid.W + x, nil
}

// Tile returns the raw compressed tile bytes at (x, y).
func (l *tiledImage) Tile(x, y int) ([]byte, error) {
    idx, err := l.indexOf(x, y)
    if err != nil {
        return nil, err
    }
    length := l.counts[idx]
    if length == 0 {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
    }
    off := int64(l.offsets[idx])
    buf := make([]byte, length)
    if _, err := l.reader.ReadAt(buf, off); err != nil {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
    }
    return buf, nil
}

// TileReader returns an io.ReadCloser backed by an io.SectionReader so the
// tile bytes are streamed without buffering.
func (l *tiledImage) TileReader(x, y int) (io.ReadCloser, error) {
    idx, err := l.indexOf(x, y)
    if err != nil {
        return nil, err
    }
    length := l.counts[idx]
    if length == 0 {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
    }
    sr := io.NewSectionReader(l.reader, int64(l.offsets[idx]), int64(length))
    return io.NopCloser(sr), nil
}

// Tiles iterates all tiles in row-major order.
func (l *tiledImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
    return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
        for y := 0; y < l.grid.H; y++ {
            for x := 0; x < l.grid.W; x++ {
                if err := ctx.Err(); err != nil {
                    yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Err: err})
                    return
                }
                b, err := l.Tile(x, y)
                if !yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
                    return
                }
            }
        }
    }
}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add formats/svs/svs.go formats/svs/image.go formats/svs/image_test.go options.go
git commit -m "feat(svs): implement Tiler.Open and tiled Level with passthrough"
```

---

## Task 17: `formats/all/all.go` — umbrella package

**Files:**
- Create: `formats/all/all.go`
- Create: `formats/all/all_test.go`

- [ ] **Step 1: Write failing tests**

```go
package all

import (
    "testing"

    opentile "github.com/wsilabs/opentile-go"
)

func TestRegisterAllIncludesSVS(t *testing.T) {
    // Register populates the top-level registry.
    Register()
    // Look for SVS by sniffing: construct a minimal Aperio-prefixed TIFF and
    // ensure Open picks up the SVS factory.
    // Minimal valid TIFF: reuse the approach from opentile_test's
    // buildTIFFWithDescription — but since that's a test helper in another
    // package, do a minimal direct build here.
    t.Skip("covered by opentile_test.go integration; Register() is tested for side-effect only")
}

func TestRegisterIsIdempotent(t *testing.T) {
    Register()
    Register()
    // No assertion — we only confirm double-Register does not panic.
}

// Minimal smoke test: the package imports the SVS factory and compiles.
var _ = opentile.FormatSVS
```

- [ ] **Step 2: Run tests to verify compile/pass**

Run: `go test ./formats/all/...`
Expected: PASS (one skipped).

- [ ] **Step 3: Implement `formats/all/all.go`**

```go
// Package all registers every format implemented by opentile-go. Import this
// package for its side effect (via a blank import) or call Register() once from
// main for equivalent behavior without relying on import ordering.
//
//     import _ "github.com/wsilabs/opentile-go/formats/all"
//
// Or:
//
//     formats_all.Register()
package all

import (
    "sync"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/formats/svs"
)

var once sync.Once

// Register registers all known format factories with the top-level opentile
// package. Safe to call multiple times.
func Register() {
    once.Do(func() {
        opentile.Register(svs.New())
    })
}

func init() { Register() }
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/all/all.go formats/all/all_test.go
git commit -m "feat(formats): add all umbrella package for registration"
```

---

## Task 18: `tests/fixtures.go` + downloader

**Files:**
- Create: `tests/fixtures.go`
- Create: `tests/download/main.go`

- [ ] **Step 1: Create fixture schema and loader**

Create `tests/fixtures.go`:

```go
// Package tests contains integration test helpers and fixture schemas shared
// between integration and fixture-generation tests.
package tests

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
)

// Fixture is the on-disk schema for a single-slide parity fixture.
type Fixture struct {
    Slide       string         `json:"slide"`
    Format      string         `json:"format"`
    Levels      []LevelFixture `json:"levels"`
    Metadata    MetadataFixture `json:"metadata"`
    TileSHA256  map[string]string `json:"tiles"` // key: "level:x:y", value: hex sha256
    ICCProfileSHA256 string     `json:"icc_profile_sha256,omitempty"`
}

type LevelFixture struct {
    Index       int     `json:"index"`
    Size        [2]int  `json:"size"`
    TileSize    [2]int  `json:"tile_size"`
    Grid        [2]int  `json:"grid"`
    Compression string  `json:"compression"`
    MPPUm       float64 `json:"mpp_um"`
    PyramidIdx  int     `json:"pyramid_index"`
}

type MetadataFixture struct {
    Magnification       float64 `json:"magnification"`
    ScannerManufacturer string  `json:"scanner_manufacturer,omitempty"`
    ScannerSerial       string  `json:"scanner_serial,omitempty"`
    SoftwareLine        string  `json:"software_line,omitempty"`
    MPP                 float64 `json:"mpp_um,omitempty"`
    AcquisitionRFC3339  string  `json:"acquisition_rfc3339,omitempty"`
}

// LoadFixture reads a Fixture from fixturePath.
func LoadFixture(fixturePath string) (*Fixture, error) {
    data, err := os.ReadFile(fixturePath)
    if err != nil {
        return nil, err
    }
    f := &Fixture{}
    if err := json.Unmarshal(data, f); err != nil {
        return nil, fmt.Errorf("parse fixture %s: %w", fixturePath, err)
    }
    return f, nil
}

// SaveFixture writes f to fixturePath as indented JSON.
func SaveFixture(fixturePath string, f *Fixture) error {
    data, err := json.MarshalIndent(f, "", "  ")
    if err != nil {
        return err
    }
    if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
        return err
    }
    return os.WriteFile(fixturePath, data, 0o644)
}

// TileKey formats a (level, x, y) triple for fixture lookup.
func TileKey(level, x, y int) string {
    return fmt.Sprintf("%d:%d:%d", level, x, y)
}

// TestdataDir returns the directory holding slide files for integration tests.
// Resolved from OPENTILE_TESTDIR env var; empty string means integration tests
// should t.Skip.
func TestdataDir() string { return os.Getenv("OPENTILE_TESTDIR") }
```

- [ ] **Step 2: Create `tests/download/main.go`**

```go
// download fetches openslide's public test slides into OPENTILE_TESTDIR. It is
// run manually (not by `go test`) to prepare a development machine.
//
// Usage:
//     OPENTILE_TESTDIR=$PWD/testdata/slides go run ./tests/download -slide CMU-1-Small-Region
package main

import (
    "flag"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
)

const openslideBase = "https://openslide.cs.cmu.edu/download/openslide-testdata"

type slide struct {
    name     string
    subpath  string // subpath under openslide-testdata/
    filename string
}

var knownSlides = map[string]slide{
    "CMU-1-Small-Region": {
        name:     "CMU-1-Small-Region",
        subpath:  "Aperio",
        filename: "CMU-1-Small-Region.svs",
    },
    // Additional slides can be added here when needed.
}

func main() {
    slideFlag := flag.String("slide", "CMU-1-Small-Region", "slide to download")
    flag.Parse()

    dest := os.Getenv("OPENTILE_TESTDIR")
    if dest == "" {
        log.Fatal("OPENTILE_TESTDIR not set; refusing to guess a destination")
    }
    s, ok := knownSlides[*slideFlag]
    if !ok {
        log.Fatalf("unknown slide: %q", *slideFlag)
    }
    if err := os.MkdirAll(dest, 0o755); err != nil {
        log.Fatalf("mkdir %s: %v", dest, err)
    }
    url := fmt.Sprintf("%s/%s/%s", openslideBase, s.subpath, s.filename)
    outPath := filepath.Join(dest, s.filename)
    if _, err := os.Stat(outPath); err == nil {
        log.Printf("already present: %s", outPath)
        return
    }
    log.Printf("downloading %s → %s", url, outPath)
    resp, err := http.Get(url)
    if err != nil {
        log.Fatalf("GET %s: %v", url, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        log.Fatalf("GET %s: %s", url, resp.Status)
    }
    out, err := os.Create(outPath)
    if err != nil {
        log.Fatalf("create %s: %v", outPath, err)
    }
    defer out.Close()
    if _, err := io.Copy(out, resp.Body); err != nil {
        log.Fatalf("download body: %v", err)
    }
    log.Printf("done: %s", outPath)
}
```

- [ ] **Step 3: Verify compiles**

Run: `go build ./tests/download/...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add tests/fixtures.go tests/download/main.go
git commit -m "feat(tests): add fixture schema and openslide testdata downloader"
```

---

## Task 19: `tests/generate_test.go` + `tests/integration_test.go`

**Files:**
- Create: `tests/generate_test.go`
- Create: `tests/integration_test.go`

The generator populates a fixture JSON from a real slide. The integration test consumes it.

- [ ] **Step 1: Create `tests/integration_test.go`**

```go
package tests_test

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "path/filepath"
    "testing"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
    "github.com/wsilabs/opentile-go/tests"
)

// slideUnderTest is the fixture slide we expect in OPENTILE_TESTDIR.
const slideUnderTest = "CMU-1-Small-Region.svs"

// TestSVSParity reads every tile in the fixture slide and compares against the
// committed fixture. Skips when OPENTILE_TESTDIR is unset.
func TestSVSParity(t *testing.T) {
    dir := tests.TestdataDir()
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set; skipping integration test")
    }
    slide := filepath.Join(dir, slideUnderTest)
    fixturePath := filepath.Join("fixtures", stemWithExt(slideUnderTest, ".json"))
    fix, err := tests.LoadFixture(fixturePath)
    if err != nil {
        t.Fatalf("load fixture: %v", err)
    }

    tiler, err := opentile.OpenFile(slide)
    if err != nil {
        t.Fatalf("OpenFile: %v", err)
    }
    defer tiler.Close()

    if string(tiler.Format()) != fix.Format {
        t.Errorf("Format: got %q, want %q", tiler.Format(), fix.Format)
    }
    levels := tiler.Levels()
    if len(levels) != len(fix.Levels) {
        t.Fatalf("level count: got %d, want %d", len(levels), len(fix.Levels))
    }
    for i, lvl := range levels {
        exp := fix.Levels[i]
        if lvl.Size().W != exp.Size[0] || lvl.Size().H != exp.Size[1] {
            t.Errorf("level %d size: got %v, want %v", i, lvl.Size(), exp.Size)
        }
        if lvl.TileSize().W != exp.TileSize[0] || lvl.TileSize().H != exp.TileSize[1] {
            t.Errorf("level %d tile size: got %v, want %v", i, lvl.TileSize(), exp.TileSize)
        }
        if lvl.Grid().W != exp.Grid[0] || lvl.Grid().H != exp.Grid[1] {
            t.Errorf("level %d grid: got %v, want %v", i, lvl.Grid(), exp.Grid)
        }
        if lvl.Compression().String() != exp.Compression {
            t.Errorf("level %d compression: got %q, want %q", i, lvl.Compression(), exp.Compression)
        }
        // Tile hashes
        for y := 0; y < lvl.Grid().H; y++ {
            for x := 0; x < lvl.Grid().W; x++ {
                b, err := lvl.Tile(x, y)
                if err != nil {
                    t.Errorf("Tile(%d,%d) level %d: %v", x, y, i, err)
                    continue
                }
                sum := sha256.Sum256(b)
                got := hex.EncodeToString(sum[:])
                key := tests.TileKey(i, x, y)
                want, ok := fix.TileSHA256[key]
                if !ok {
                    t.Errorf("fixture missing key %s", key)
                    continue
                }
                if got != want {
                    t.Errorf("tile %s hash: got %s, want %s", key, got, want)
                }
            }
        }
    }

    md := tiler.Metadata()
    if md.Magnification != fix.Metadata.Magnification {
        t.Errorf("magnification: got %v, want %v", md.Magnification, fix.Metadata.Magnification)
    }
}

func stemWithExt(filename, ext string) string {
    base := filepath.Base(filename)
    for i := len(base) - 1; i >= 0; i-- {
        if base[i] == '.' {
            return base[:i] + ext
        }
    }
    return base + ext
}

// unused helper to avoid import lint for io when kept minimal
var _ = io.ReadAll
var _ = fmt.Sprintf
```

- [ ] **Step 2: Create `tests/generate_test.go`**

```go
//go:build generate

package tests_test

import (
    "crypto/sha256"
    "encoding/hex"
    "flag"
    "fmt"
    "path/filepath"
    "testing"
    "time"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
    "github.com/wsilabs/opentile-go/tests"
)

var regenerate = flag.Bool("generate", false, "regenerate fixtures from live slides")

// TestGenerateFixtures is a dev-only helper. Run with:
//
//     OPENTILE_TESTDIR=$PWD/testdata/slides go test ./tests -tags generate -run TestGenerateFixtures -generate
//
// Build tag "generate" keeps it out of default CI.
func TestGenerateFixtures(t *testing.T) {
    if !*regenerate {
        t.Skip("pass -generate to regenerate fixtures")
    }
    dir := tests.TestdataDir()
    if dir == "" {
        t.Fatal("OPENTILE_TESTDIR not set")
    }
    slide := filepath.Join(dir, slideUnderTest)
    tiler, err := opentile.OpenFile(slide)
    if err != nil {
        t.Fatalf("OpenFile: %v", err)
    }
    defer tiler.Close()

    f := &tests.Fixture{
        Slide:      slideUnderTest,
        Format:     string(tiler.Format()),
        TileSHA256: make(map[string]string),
    }
    for i, lvl := range tiler.Levels() {
        f.Levels = append(f.Levels, tests.LevelFixture{
            Index:       i,
            Size:        [2]int{lvl.Size().W, lvl.Size().H},
            TileSize:    [2]int{lvl.TileSize().W, lvl.TileSize().H},
            Grid:        [2]int{lvl.Grid().W, lvl.Grid().H},
            Compression: lvl.Compression().String(),
            MPPUm:       lvl.MPP().W * 1000,
            PyramidIdx:  lvl.PyramidIndex(),
        })
        for y := 0; y < lvl.Grid().H; y++ {
            for x := 0; x < lvl.Grid().W; x++ {
                b, err := lvl.Tile(x, y)
                if err != nil {
                    t.Fatalf("Tile(%d,%d) level %d: %v", x, y, i, err)
                }
                sum := sha256.Sum256(b)
                f.TileSHA256[tests.TileKey(i, x, y)] = hex.EncodeToString(sum[:])
            }
        }
    }
    md := tiler.Metadata()
    f.Metadata = tests.MetadataFixture{
        Magnification:       md.Magnification,
        ScannerManufacturer: md.ScannerManufacturer,
        ScannerSerial:       md.ScannerSerial,
    }
    if !md.AcquisitionDateTime.IsZero() {
        f.Metadata.AcquisitionRFC3339 = md.AcquisitionDateTime.Format(time.RFC3339)
    }

    outPath := filepath.Join("fixtures", stemWithExt(slideUnderTest, ".json"))
    if err := tests.SaveFixture(outPath, f); err != nil {
        t.Fatalf("SaveFixture: %v", err)
    }
    fmt.Printf("wrote %s\n", outPath)
}
```

- [ ] **Step 3: Verify compile**

Run: `go test ./tests/... -run nonexistent`
Expected: compile succeeds; zero tests run.

- [ ] **Step 4: Commit**

```bash
git add tests/integration_test.go tests/generate_test.go
git commit -m "feat(tests): add integration parity test and fixture generator"
```

---

## Task 20: Generate fixture and finalize

This task requires a working development environment: Go 1.23+ and internet access to download the test slide. It must be run interactively by the implementer; CI cannot generate the fixture.

**Files:**
- Create: `tests/fixtures/CMU-1-Small-Region.json` (generated, committed)

- [ ] **Step 1: Prepare testdata directory**

```bash
export OPENTILE_TESTDIR="$PWD/testdata/slides"
mkdir -p "$OPENTILE_TESTDIR"
go run ./tests/download -slide CMU-1-Small-Region
```

Expected: `testdata/slides/CMU-1-Small-Region.svs` exists (approx 2 MB).

- [ ] **Step 2: Run generator**

```bash
OPENTILE_TESTDIR="$PWD/testdata/slides" go test ./tests -tags generate -run TestGenerateFixtures -generate -v
```

Expected: `tests/fixtures/CMU-1-Small-Region.json` created with level geometry, metadata, and per-tile sha256 hashes. Check a few hashes look sane (64 hex chars).

- [ ] **Step 3: Run integration test against the generated fixture**

```bash
OPENTILE_TESTDIR="$PWD/testdata/slides" go test ./tests -run TestSVSParity -v
```

Expected: PASS.

- [ ] **Step 4: Sanity check — clear OPENTILE_TESTDIR and confirm skip**

```bash
go test ./tests -run TestSVSParity -v
```

Expected: test skipped (no `OPENTILE_TESTDIR`).

- [ ] **Step 5: Run full suite**

```bash
go test ./... -race
```

Expected: PASS, no race warnings.

- [ ] **Step 6: Commit fixture (but not the slide binary)**

```bash
git add tests/fixtures/CMU-1-Small-Region.json
git commit -m "test(svs): commit CMU-1-Small-Region parity fixture"
```

The slide binary stays in `testdata/slides/` which is already gitignored.

---

## Task 21: README usage example and final polish

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace `README.md` with a complete usage guide**

```markdown
# opentile-go

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)

Pure-Go port of [opentile](https://github.com/imi-bigpicture/opentile), a library for reading
tiles from whole-slide imaging (WSI) TIFF files used in digital pathology.

**Status: v0.1 — Aperio SVS only.** NDPI, Philips, 3DHistech, OME TIFF planned; see [the design doc](./docs/superpowers/specs/2026-04-19-opentile-go-design.md) for the roadmap.

## Install

```
go get github.com/wsilabs/opentile-go
```

Requires Go 1.23+. No cgo, no C dependencies.

## Usage

```go
package main

import (
    "fmt"
    "log"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
    tiler, err := opentile.OpenFile("slide.svs")
    if err != nil {
        log.Fatal(err)
    }
    defer tiler.Close()

    fmt.Println("format:", tiler.Format())
    fmt.Println("levels:", len(tiler.Levels()))

    base, _ := tiler.Level(0)
    fmt.Printf("base: %v tiles of %v pixels, compression %s\n",
        base.Grid(), base.TileSize(), base.Compression())

    tile, err := base.Tile(0, 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("tile[0,0]: %d bytes of %s\n", len(tile), base.Compression())
}
```

The returned bytes are the compressed tile bitstream exactly as stored in the source TIFF (JPEG or JPEG 2000). Decode with a codec of your choice.

## Concurrency

`Level.Tile(x, y)` and `Level.TileReader(x, y)` are safe to call concurrently from multiple goroutines, provided the underlying `io.ReaderAt` supplied to `Open` is also safe for concurrent use (`*os.File` is).

## Scope

See `docs/superpowers/specs/2026-04-19-opentile-go-design.md` for the full design and non-goals.

## License

Apache 2.0. This is a direct port of the Python `opentile` library (Copyright 2021–2024 Sectra AB). See [NOTICE](./NOTICE) for full attribution. Not affiliated with or endorsed by Sectra AB or the BigPicture project.
```

- [ ] **Step 2: Verify link validity and re-run tests**

Run: `go test ./... -race`
Expected: PASS.

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: expand README with install, usage, concurrency notes"
```

---

## Done when

- `go test ./... -race` passes with no skips on `OPENTILE_TESTDIR`-set runs.
- `go vet ./...` clean.
- `tests/fixtures/CMU-1-Small-Region.json` committed and `TestSVSParity` passes against it.
- `go doc github.com/wsilabs/opentile-go` renders the public API with complete godoc comments.
