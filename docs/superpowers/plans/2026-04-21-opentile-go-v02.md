# opentile-go v0.2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship v0.2 of opentile-go — Hamamatsu NDPI format support, BigTIFF parsing, and associated-image support (label, overview, thumbnail) for both SVS and NDPI. Introduces cgo over libjpeg-turbo for the one operation that's genuinely painful in pure Go (MCU-aligned lossless JPEG crop).

**Architecture:** Additive on v0.1. Two new internal packages: `internal/jpeg/` (pure-Go marker-level JPEG bitstream manipulation) and `internal/jpegturbo/` (single-function cgo wrapper over `tjTransform`). BigTIFF lands as a parallel parse path in `internal/tiff` — format packages are agnostic. `formats/ndpi/` is new; `formats/svs/` gains associated-image support. Parity oracle (Python opentile in a subprocess) lands under `//go:build parity`.

**Tech Stack:** Go 1.23+, libjpeg-turbo 2.1+ (linked via `pkg-config: libturbojpeg` under cgo builds). Python 3.10+ with opentile + PyTurboJPEG pinned for the parity oracle. All tests stdlib where possible; `image/jpeg` is acceptable inside `_test.go` files for verification but never in library code.

**Scope boundary:** v0.2 covers NDPI (<4GB or whatever BigTIFF reaches), associated images for both SVS and NDPI, BigTIFF parsing. Deferred: NDPI's proprietary 64-bit offset extension for >4GB files (reopen if the pending 6.5 GB sample requires it), SVS corrupt-edge reconstruct (v1.0), Philips/Histech/OME (v0.4+).

---

## File Structure Overview

### Created in v0.2

```
opentile-go/
├── internal/tiff/              # EXTENDED
│   ├── bigheader.go            # NEW — BigTIFF header parse
│   ├── bigifd.go               # NEW — BigTIFF IFD walker (20-byte entries)
│   ├── tag.go                  # EXTENDED — LONG8, IFD, IFD8 types
│   ├── page.go                 # EXTENDED — uint64-aware internally
│   └── header.go               # EXTENDED — dispatch on magic 43
│
├── internal/jpeg/              # NEW, pure Go
│   ├── marker.go               # Marker constants + Segment type
│   ├── scan.go                 # Iterator-first Scan over segments
│   ├── segment.go              # byte-stuffed ReadScan helper
│   ├── sof.go                  # ParseSOF / BuildSOF / ReplaceSOFDimensions
│   ├── tables.go               # SplitJPEGTables (TIFF JPEGTables tag)
│   └── concat.go               # ConcatenateScans
│
├── internal/jpegturbo/         # NEW, cgo
│   ├── turbo.go                # always-compiled: Region, ErrCGORequired
│   ├── turbo_cgo.go            # //go:build cgo && !nocgo
│   └── turbo_nocgo.go          # //go:build !cgo || nocgo
│
├── formats/svs/
│   └── associated.go           # NEW — striped label/overview/thumbnail
│
├── formats/ndpi/               # NEW
│   ├── ndpi.go                 # Factory, Supports, Open
│   ├── metadata.go             # ndpi.Metadata, MetadataOf
│   ├── tilesize.go             # AdjustTileSize
│   ├── striped.go              # NdpiStripedImage
│   ├── oneframe.go             # NdpiOneFrameImage
│   └── associated.go           # NdpiLabel, NdpiOverview
│
└── tests/oracle/               # NEW, //go:build parity
    ├── oracle.go
    ├── oracle_runner.py
    ├── parity_test.go
    └── requirements.txt
```

### Modified in v0.2

- `errors.go` — 4 new sentinels
- `formats/svs/svs.go` — `Tiler.Associated()` returns non-nil; page classification routes striped associated pages
- `formats/all/all.go` — register ndpi.New()
- `README.md` — adds NDPI/BigTIFF usage, cgo/nocgo build notes, parity oracle section
- `CLAUDE.md` — reflects v0.2 scope
- `docs/deferred.md` — retires R1 (NDPI), R2 (internal/jpeg), R3 (associated images); adds any new items surfaced during v0.2
- `tests/integration_test.go` — renames `TestSVSParity` → `TestSlideParity`; iterates any `.svs` or `.ndpi` with a committed fixture

### Deferred (do NOT create in v0.2)

- NDPI >4GB Hamamatsu 64-bit extension (unless pending sample requires it)
- SVS corrupt-edge fix path
- Philips / 3DHistech / OME format packages

---

## Batch 1 — Error sentinels and BigTIFF parsing

Extends `internal/tiff/` so every downstream format picks up BigTIFF automatically. No format code changes in this batch.

---

## Task 1: Add v0.2 error sentinels

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`

- [ ] **Step 1: Extend `errors_test.go`**

Append to the existing `errors_test.go`:

```go
func TestV02Sentinels(t *testing.T) {
    sentinels := []error{
        ErrBadJPEGBitstream,
        ErrMCUAlignment,
        ErrCGORequired,
        ErrTileSizeRequired,
    }
    seen := make(map[string]bool)
    for _, e := range sentinels {
        if e == nil {
            t.Fatal("sentinel is nil")
        }
        if seen[e.Error()] {
            t.Errorf("duplicate sentinel text: %q", e.Error())
        }
        seen[e.Error()] = true
    }

    // Confirm the new sentinels wrap cleanly through TileError.
    te := &TileError{Level: 0, X: 1, Y: 2, Err: ErrBadJPEGBitstream}
    if !errors.Is(te, ErrBadJPEGBitstream) {
        t.Fatal("TileError should unwrap to ErrBadJPEGBitstream")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./... -run TestV02Sentinels`
Expected: FAIL — `undefined: ErrBadJPEGBitstream`, `ErrMCUAlignment`, `ErrCGORequired`, `ErrTileSizeRequired`.

- [ ] **Step 3: Extend `errors.go`**

Add these sentinels inside the existing `var ( ... )` block in `errors.go`:

```go
    // Returned (wrapped in TileError) when internal/jpeg cannot parse a JPEG
    // bitstream or assemble a valid one from TIFF fragments.
    ErrBadJPEGBitstream = errors.New("opentile: invalid JPEG bitstream")

    // Returned when an operation requires an MCU-aligned region and the
    // computed or requested region is not. Primarily an internal invariant
    // guard; consumers encounter it only on malformed slides.
    ErrMCUAlignment = errors.New("opentile: operation requires MCU alignment")

    // Returned from NDPI one-frame levels and NDPI label on builds compiled
    // without cgo (CGO_ENABLED=0 or -tags nocgo).
    ErrCGORequired = errors.New("opentile: operation requires cgo build with libjpeg-turbo")

    // Reserved for future use; currently unfired because v0.2 defaults the
    // NDPI tile size to 512 rather than erroring. Predefined so exporting
    // it later is not a breaking change.
    ErrTileSizeRequired = errors.New("opentile: tile size not representable for this format")
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add errors.go errors_test.go
git commit -m "feat(errors): add v0.2 sentinels (ErrBadJPEGBitstream, ErrMCUAlignment, ErrCGORequired, ErrTileSizeRequired)"
```

---

## Task 2: BigTIFF header detection

**Files:**
- Create: `internal/tiff/bigheader.go`
- Create: `internal/tiff/bigheader_test.go`
- Modify: `internal/tiff/header.go`

- [ ] **Step 1: Write failing tests**

Create `internal/tiff/bigheader_test.go`:

```go
package tiff

import (
    "bytes"
    "errors"
    "testing"
)

func TestParseBigTIFFHeader(t *testing.T) {
    // BigTIFF LE: II(4949) 43(2B00) offsetSize(0800) constant(0000) firstIFD(uint64)
    // Place firstIFD at 0x10.
    data := []byte{
        'I', 'I',
        0x2B, 0x00, // magic 43
        0x08, 0x00, // offset size = 8
        0x00, 0x00, // constant
        0x10, 0, 0, 0, 0, 0, 0, 0, // firstIFD = 0x10 (uint64 LE)
    }
    h, err := parseHeader(bytes.NewReader(data))
    if err != nil {
        t.Fatalf("parseHeader: %v", err)
    }
    if !h.littleEndian || !h.bigTIFF || h.firstIFD != 0x10 {
        t.Fatalf("header: got %+v", h)
    }
}

func TestParseBigTIFFRejectsBadOffsetSize(t *testing.T) {
    data := []byte{
        'I', 'I',
        0x2B, 0x00,
        0x04, 0x00, // bad offset size (should be 8)
        0x00, 0x00,
        0, 0, 0, 0, 0, 0, 0, 0,
    }
    _, err := parseHeader(bytes.NewReader(data))
    if !errors.Is(err, ErrInvalidTIFF) {
        t.Fatalf("expected ErrInvalidTIFF, got %v", err)
    }
}

func TestParseBigTIFFRejectsBadConstant(t *testing.T) {
    data := []byte{
        'I', 'I',
        0x2B, 0x00,
        0x08, 0x00,
        0xFF, 0xFF, // bad constant
        0, 0, 0, 0, 0, 0, 0, 0,
    }
    _, err := parseHeader(bytes.NewReader(data))
    if !errors.Is(err, ErrInvalidTIFF) {
        t.Fatalf("expected ErrInvalidTIFF, got %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/... -run TestParseBigTIFF`
Expected: FAIL — existing `parseHeader` returns `ErrUnsupportedTIFF: BigTIFF`.

- [ ] **Step 3: Extend the `header` struct and add BigTIFF parsing**

Modify `internal/tiff/header.go`. Add a `bigTIFF bool` field to the `header` struct:

```go
type header struct {
    littleEndian bool
    bigTIFF      bool  // NEW: true when magic 43 (BigTIFF); false for magic 42 (classic)
    firstIFD     uint64 // CHANGED from uint32 — carries BigTIFF's 64-bit offset; classic widens safely
}
```

Replace the `case 43` branch in `parseHeader` — instead of returning `ErrUnsupportedTIFF`, dispatch to the new BigTIFF parser. Update the body to look like:

```go
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
        offset, err := b.uint32(4)
        if err != nil {
            return header{}, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
        }
        return header{littleEndian: le, bigTIFF: false, firstIFD: uint64(offset)}, nil
    case 43:
        return parseBigTIFFHeader(b, le)
    default:
        return header{}, fmt.Errorf("%w: magic %d", ErrInvalidTIFF, magic)
    }
}
```

Create `internal/tiff/bigheader.go`:

```go
package tiff

import "fmt"

// parseBigTIFFHeader reads the 16-byte BigTIFF header starting at offset 2
// (just past the byte-order mark and magic). The byteReader b already has
// the correct endianness set.
//
// BigTIFF layout (TIFF 6.0 Appendix, BigTIFF extension):
//   bytes 0..1   byte order (II/MM) — already consumed
//   bytes 2..3   magic 43          — already consumed
//   bytes 4..5   offset size (must be 8)
//   bytes 6..7   constant (must be 0)
//   bytes 8..15  first IFD offset (uint64)
func parseBigTIFFHeader(b *byteReader, littleEndian bool) (header, error) {
    offsetSize, err := b.uint16(4)
    if err != nil {
        return header{}, fmt.Errorf("%w: BigTIFF offset size: %v", ErrInvalidTIFF, err)
    }
    if offsetSize != 8 {
        return header{}, fmt.Errorf("%w: BigTIFF offset size %d (expected 8)", ErrInvalidTIFF, offsetSize)
    }
    constant, err := b.uint16(6)
    if err != nil {
        return header{}, fmt.Errorf("%w: BigTIFF constant: %v", ErrInvalidTIFF, err)
    }
    if constant != 0 {
        return header{}, fmt.Errorf("%w: BigTIFF constant %d (expected 0)", ErrInvalidTIFF, constant)
    }
    firstIFD, err := b.uint64(8)
    if err != nil {
        return header{}, fmt.Errorf("%w: BigTIFF first IFD: %v", ErrInvalidTIFF, err)
    }
    return header{littleEndian: littleEndian, bigTIFF: true, firstIFD: firstIFD}, nil
}
```

- [ ] **Step 4: Add `uint64` to byteReader**

The `byteReader` in `internal/tiff/reader.go` does not yet have a `uint64` method. Add it:

```go
func (b *byteReader) uint64(offset int64) (uint64, error) {
    buf, err := b.read(offset, 8)
    if err != nil {
        return 0, err
    }
    return b.order.Uint64(buf), nil
}
```

Also add a regression test for it in `internal/tiff/reader_test.go`:

```go
func TestByteReaderUint64(t *testing.T) {
    data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
    r := bytes.NewReader(data)
    bl := newByteReader(r, true)
    v, err := bl.uint64(0)
    if err != nil || v != 0x0807060504030201 {
        t.Fatalf("uint64 LE: got %x, err %v; want 0x0807060504030201", v, err)
    }
    bb := newByteReader(r, false)
    v, err = bb.uint64(0)
    if err != nil || v != 0x0102030405060708 {
        t.Fatalf("uint64 BE: got %x, err %v", v, err)
    }
}
```

- [ ] **Step 5: Fix existing classic-TIFF tests**

`internal/tiff/header_test.go` currently asserts `h.firstIFD != tt.wantOffset` where `wantOffset uint32`. Update the struct tags:

```go
    tests := []struct {
        name       string
        bytes      []byte
        wantLE     bool
        wantBig    bool    // NEW
        wantOffset uint64  // CHANGED from uint32
        wantErr    error
    }{
        {
            name:       "little-endian classic",
            bytes:      []byte{'I', 'I', 42, 0, 0x08, 0, 0, 0},
            wantLE:     true,
            wantBig:    false,
            wantOffset: 8,
        },
        {
            name:       "big-endian classic",
            bytes:      []byte{'M', 'M', 0, 42, 0, 0, 0, 0x10},
            wantLE:     false,
            wantBig:    false,
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
            name:       "bigtiff",
            bytes:      []byte{'I', 'I', 43, 0, 8, 0, 0, 0, 0x20, 0, 0, 0, 0, 0, 0, 0},
            wantLE:     true,
            wantBig:    true,
            wantOffset: 0x20,
        },
        {
            name:    "short",
            bytes:   []byte{'I', 'I'},
            wantErr: ErrInvalidTIFF,
        },
    }
```

And update the success-path assertion block:

```go
            if h.littleEndian != tt.wantLE {
                t.Errorf("littleEndian: got %v, want %v", h.littleEndian, tt.wantLE)
            }
            if h.bigTIFF != tt.wantBig {
                t.Errorf("bigTIFF: got %v, want %v", h.bigTIFF, tt.wantBig)
            }
            if h.firstIFD != tt.wantOffset {
                t.Errorf("firstIFD: got %d, want %d", h.firstIFD, tt.wantOffset)
            }
```

The previous "bigtiff (unsupported v0.1)" case is removed because BigTIFF is now supported.

- [ ] **Step 6: Also fix File.Open call sites that depend on firstIFD width**

In `internal/tiff/file.go`, the line `ifds, err := walkIFDs(br, int64(h.firstIFD))` now takes a uint64. Since `walkIFDs` (Task 4) will be updated to accept int64 or uint64, for this task keep `int64(h.firstIFD)` which is safe for classic (offset <= 4 GB). BigTIFF support in walkIFDs will land in Task 4; until then, this commit may have BigTIFF files parse their header successfully but fail at `walkIFDs` because entry count is still uint16. That's expected — Task 4 closes the loop.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tiff/...`
Expected: all TestParseBigTIFF* pass, all existing `TestParseHeader`/`TestByteReader*`/`TestWalkIFDs*`/`TestPageAccessors`/etc. still pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tiff/header.go internal/tiff/header_test.go internal/tiff/bigheader.go internal/tiff/bigheader_test.go internal/tiff/reader.go internal/tiff/reader_test.go
git commit -m "feat(tiff): detect BigTIFF header (magic 43) and widen firstIFD to uint64"
```

---

## Task 3: BigTIFF data types (LONG8, IFD, IFD8)

**Files:**
- Modify: `internal/tiff/tag.go`
- Modify: `internal/tiff/tag_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/tiff/tag_test.go`:

```go
func TestDataTypeSizeV02(t *testing.T) {
    tests := []struct {
        dt   DataType
        want int
    }{
        {DTLong8, 8},
        {DTIFD, 4},
        {DTIFD8, 8},
    }
    for _, tt := range tests {
        if got := tt.dt.Size(); got != tt.want {
            t.Errorf("%v.Size() = %d, want %d", tt.dt, got, tt.want)
        }
    }
}

func TestDecodeLong8(t *testing.T) {
    // Entry count=2 LONG8 (16 bytes), stored at offset 16.
    data := make([]byte, 32)
    copy(data[16:], []byte{
        0x01, 0, 0, 0, 0, 0, 0, 0,
        0x02, 0, 0, 0, 0, 0, 0, 0,
    })
    r := bytes.NewReader(data)
    b := newByteReader(r, true)
    entry := Entry{Tag: 324, Type: DTLong8, Count: 2, valueOrOffset: 16}
    vals, err := entry.Values64(b)
    if err != nil {
        t.Fatalf("Values64: %v", err)
    }
    if len(vals) != 2 || vals[0] != 1 || vals[1] != 2 {
        t.Fatalf("vals: got %v, want [1 2]", vals)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/... -run "TestDataTypeSizeV02|TestDecodeLong8"`
Expected: FAIL — `undefined: DTLong8, DTIFD, DTIFD8, Values64`.

- [ ] **Step 3: Extend `tag.go`**

Add the new data type constants and their sizes:

```go
const (
    DTByte      DataType = 1
    DTASCII     DataType = 2
    DTShort     DataType = 3
    DTLong      DataType = 4
    DTRational  DataType = 5
    DTUndefined DataType = 7
    DTIFD       DataType = 13 // NEW — uint32 offset to sub-IFD
    DTLong8     DataType = 16 // NEW — uint64 (BigTIFF)
    DTIFD8      DataType = 18 // NEW — uint64 offset to sub-IFD (BigTIFF)
)
```

Extend `Size()`:

```go
func (d DataType) Size() int {
    switch d {
    case DTByte, DTASCII, DTUndefined:
        return 1
    case DTShort:
        return 2
    case DTLong, DTIFD:
        return 4
    case DTRational, DTLong8, DTIFD8:
        return 8
    default:
        return 1
    }
}
```

Add a uint64-returning Values accessor. The existing `Values(b) ([]uint32, error)` stays for backward compatibility on Long/Short arrays; `Values64` handles any width (including LONG8):

```go
// Values64 returns decoded values as []uint64, accepting Short, Long, Long8,
// IFD, and IFD8 entry types. Prefer this over Values for entries that might
// carry BigTIFF LONG8 data (tile offsets, for instance).
func (e Entry) Values64(b *byteReader) ([]uint64, error) {
    need := int64(e.Count) * int64(e.Type.Size())
    var buf []byte
    if e.fitsInline() {
        // valueBytes is the raw inline cell; may be 4 or 8 bytes depending on TIFF variant
        buf = append([]byte(nil), e.valueBytes[:need]...)
    } else {
        if need > int64(^uint(0)>>1) {
            return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, need)
        }
        b, err := b.bytes(int64(e.valueOrOffset), int(need))
        if err != nil {
            return nil, fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
        }
        buf = b
    }
    out := make([]uint64, 0, e.Count)
    size := int(e.Type.Size())
    switch size {
    case 2:
        for i := uint32(0); i < e.Count; i++ {
            out = append(out, uint64(b.order.Uint16(buf[int(i)*2:])))
        }
    case 4:
        for i := uint32(0); i < e.Count; i++ {
            out = append(out, uint64(b.order.Uint32(buf[int(i)*4:])))
        }
    case 8:
        for i := uint32(0); i < e.Count; i++ {
            out = append(out, b.order.Uint64(buf[int(i)*8:]))
        }
    default:
        return nil, fmt.Errorf("tiff: tag %d: unsupported type %d for uint64 decode", e.Tag, e.Type)
    }
    return out, nil
}
```

Note: the parameter name `b` is reused for the inner `bytes` slice in the conditional block — rename to `payload` to avoid shadowing:

```go
    } else {
        if need > int64(^uint(0)>>1) {
            return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, need)
        }
        payload, err := b.bytes(int64(e.valueOrOffset), int(need))
        if err != nil {
            return nil, fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
        }
        buf = payload
    }
```

- [ ] **Step 4: Widen `valueBytes` to 8 bytes**

BigTIFF inline cells are 8 bytes wide (vs. 4 in classic). Widen `Entry.valueBytes`:

```go
type Entry struct {
    Tag           uint16
    Type          DataType
    Count         uint64     // CHANGED from uint32 — BigTIFF entry count
    valueOrOffset uint64     // CHANGED from uint32 — BigTIFF cell carries up to 8 bytes inline or an 8-byte offset
    valueBytes    [8]byte    // CHANGED from [4]byte
}
```

`fitsInline` now compares against 8 instead of 4 for BigTIFF. Since `Size()` math uses `int64(Count) * int64(Type.Size())`, the inline capacity must come from somewhere. Add a flag:

Actually simplification: BigTIFF inline is always 8 bytes; classic inline is always 4. The reader who populated the Entry knows which variant. Encode that as a field:

```go
type Entry struct {
    Tag           uint16
    Type          DataType
    Count         uint64
    valueOrOffset uint64
    valueBytes    [8]byte
    inlineCap     int // 4 for classic, 8 for BigTIFF; set by the IFD walker
}

func (e Entry) fitsInline() bool {
    cap := e.inlineCap
    if cap == 0 {
        cap = 4 // defensive: treat uninitialized as classic
    }
    return int64(e.Count)*int64(e.Type.Size()) <= int64(cap)
}
```

Existing Values/decodeBuffer paths need Count type updated (uint32 → uint64). Walk the file and swap:

```go
func (e Entry) decodeBuffer(b *byteReader, buf []byte) ([]uint32, error) {
    need := int64(e.Count) * int64(e.Type.Size())
    if int64(len(buf)) < need {
        return nil, fmt.Errorf("tiff: tag %d: buffer %d < needed %d bytes", e.Tag, len(buf), need)
    }
    out := make([]uint32, 0, e.Count)
    switch e.Type {
    case DTByte, DTUndefined:
        for _, v := range buf[:e.Count] {
            out = append(out, uint32(v))
        }
    case DTShort:
        for i := uint64(0); i < e.Count; i++ {
            out = append(out, uint32(b.order.Uint16(buf[i*2:])))
        }
    case DTLong, DTIFD:
        for i := uint64(0); i < e.Count; i++ {
            out = append(out, b.order.Uint32(buf[i*4:]))
        }
    default:
        return nil, fmt.Errorf("tiff: tag %d: unsupported type %d for uint decode", e.Tag, e.Type)
    }
    return out, nil
}
```

Existing tests (`TestTagValueDecodeInline`, `TestTagValueDecodeExternal`, `TestDecodeASCII`, `TestDecodeBufferShortBuffer`, `TestDecodeInlineRejectsOversize`) construct `Entry` literals. Some set `valueOrOffset` explicitly and don't set `inlineCap`; those will default to 0 which `fitsInline` treats as 4. Good — classic behavior preserved by default.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tiff/...`
Expected: all pass including new LONG8 tests.

- [ ] **Step 6: Commit**

```bash
git add internal/tiff/tag.go internal/tiff/tag_test.go
git commit -m "feat(tiff): add LONG8/IFD/IFD8 data types and Values64 accessor

Widens Entry.Count to uint64 and valueBytes to [8]byte to accommodate
BigTIFF entries, with an inlineCap field that preserves the 4-byte
classic-TIFF inline semantics when the IFD walker leaves it unset."
```

---

## Task 4: BigTIFF IFD walker

**Files:**
- Create: `internal/tiff/bigifd.go`
- Create: `internal/tiff/bigifd_test.go`
- Modify: `internal/tiff/ifd.go`

- [ ] **Step 1: Write failing tests**

Create `internal/tiff/bigifd_test.go`:

```go
package tiff

import (
    "bytes"
    "encoding/binary"
    "testing"
)

// buildBigTIFF constructs a tiny BigTIFF with a single IFD containing one
// SHORT entry (tag=256, value=1024). All tag values fit inline in the 8-byte
// cell.
func buildBigTIFF(t *testing.T, entries [][3]uint64) []byte {
    t.Helper()
    buf := new(bytes.Buffer)
    // BigTIFF header: II 43 offsetSize=8 constant=0 firstIFD=0x10
    buf.Write([]byte{'I', 'I', 0x2B, 0x00, 0x08, 0x00, 0x00, 0x00})
    _ = binary.Write(buf, binary.LittleEndian, uint64(0x10))
    // IFD at offset 0x10: count(u64), entries (20 bytes each), next IFD (u64)
    _ = binary.Write(buf, binary.LittleEndian, uint64(len(entries)))
    for _, e := range entries {
        _ = binary.Write(buf, binary.LittleEndian, uint16(e[0]))
        _ = binary.Write(buf, binary.LittleEndian, uint16(3)) // SHORT
        _ = binary.Write(buf, binary.LittleEndian, uint64(1))
        _ = binary.Write(buf, binary.LittleEndian, e[2])      // value-or-offset (8 bytes)
    }
    _ = binary.Write(buf, binary.LittleEndian, uint64(0)) // next IFD = 0
    return buf.Bytes()
}

func TestWalkBigIFDs(t *testing.T) {
    data := buildBigTIFF(t, [][3]uint64{
        {256, 3, 1024}, // ImageWidth = 1024
        {257, 3, 768},  // ImageLength = 768
    })
    r := bytes.NewReader(data)
    h, err := parseHeader(r)
    if err != nil {
        t.Fatalf("parseHeader: %v", err)
    }
    if !h.bigTIFF {
        t.Fatal("expected bigTIFF=true")
    }
    b := newByteReader(r, h.littleEndian)
    ifds, err := walkIFDs(b, int64(h.firstIFD), h.bigTIFF)
    if err != nil {
        t.Fatalf("walkIFDs: %v", err)
    }
    if len(ifds) != 1 {
        t.Fatalf("ifd count: got %d, want 1", len(ifds))
    }
    e, ok := ifds[0].get(256)
    if !ok {
        t.Fatal("ImageWidth missing")
    }
    if e.Count != 1 {
        t.Errorf("count: got %d, want 1", e.Count)
    }
    if e.inlineCap != 8 {
        t.Errorf("inlineCap: got %d, want 8", e.inlineCap)
    }
    vals, err := e.Values(b)
    if err != nil || len(vals) != 1 || vals[0] != 1024 {
        t.Fatalf("ImageWidth: got %v, err %v", vals, err)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tiff/... -run TestWalkBigIFDs`
Expected: FAIL — `walkIFDs` signature is `(b *byteReader, offset int64) ([]*ifd, error)`, not taking a bigTIFF flag.

- [ ] **Step 3: Update `walkIFDs` signature and create BigTIFF variant**

Modify `internal/tiff/ifd.go` — add a `bigTIFF bool` parameter and dispatch:

```go
func walkIFDs(b *byteReader, offset int64, bigTIFF bool) ([]*ifd, error) {
    if bigTIFF {
        return walkBigIFDs(b, offset)
    }
    return walkClassicIFDs(b, offset)
}
```

Rename the existing `walkIFDs` body to `walkClassicIFDs`. Its signature becomes `(b *byteReader, offset int64) ([]*ifd, error)`. Inside, after `readEntry`, set `entry.inlineCap = 4` before storing.

Existing `readEntry` currently produces a 4-byte cell copied into the 8-byte `valueBytes`; the remaining 4 bytes stay zero. That's fine for classic — they're never read because `fitsInline()` caps at 4.

Create `internal/tiff/bigifd.go`:

```go
package tiff

import "fmt"

// walkBigIFDs is the BigTIFF variant of walkIFDs. BigTIFF IFDs use uint64
// entry counts, 20-byte entries (tag u16, type u16, count u64, value u64),
// and uint64 next-IFD offsets.
func walkBigIFDs(b *byteReader, offset int64) ([]*ifd, error) {
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

        count, err := b.uint64(offset)
        if err != nil {
            return nil, fmt.Errorf("tiff: BigTIFF IFD entry count at %d: %w", offset, err)
        }
        ifd := &ifd{entries: make(map[uint16]Entry, count)}
        pos := offset + 8
        for i := uint64(0); i < count; i++ {
            entry, err := readBigEntry(b, pos)
            if err != nil {
                return nil, err
            }
            ifd.entries[entry.Tag] = entry
            pos += 20
        }
        out = append(out, ifd)
        next, err := b.uint64(pos)
        if err != nil {
            return nil, fmt.Errorf("tiff: BigTIFF next IFD offset at %d: %w", pos, err)
        }
        offset = int64(next)
    }
    return out, nil
}

// readBigEntry reads a 20-byte BigTIFF IFD entry at offset.
func readBigEntry(b *byteReader, offset int64) (Entry, error) {
    tag, err := b.uint16(offset)
    if err != nil {
        return Entry{}, err
    }
    typ, err := b.uint16(offset + 2)
    if err != nil {
        return Entry{}, err
    }
    count, err := b.uint64(offset + 4)
    if err != nil {
        return Entry{}, err
    }
    cell, err := b.bytes(offset+12, 8)
    if err != nil {
        return Entry{}, err
    }
    vo := b.order.Uint64(cell)
    var e Entry
    e.Tag = tag
    e.Type = DataType(typ)
    e.Count = count
    e.valueOrOffset = vo
    copy(e.valueBytes[:], cell)
    e.inlineCap = 8
    return e, nil
}
```

- [ ] **Step 4: Fix classic walk to set inlineCap = 4**

In `internal/tiff/ifd.go` (now `walkClassicIFDs`), modify the `readEntry` path. Since `readEntry` returns an `Entry` with `inlineCap = 0`, either update it to set `inlineCap = 4`, or set it at the call site:

```go
            entry, err := readEntry(b, pos)
            if err != nil {
                return nil, err
            }
            entry.inlineCap = 4
            ifd.entries[entry.Tag] = entry
            pos += 12
```

Also extend the classic `readEntry`:
- `Count` is now `uint64` on the Entry; the classic entry reads it as `uint32` then widens. Existing code: `count, err := b.uint32(offset + 4)`; then `e.Count = count`. Because Entry.Count is now uint64, this becomes `e.Count = uint64(count)`. Update.
- `valueOrOffset` is now `uint64`; existing classic reads 4 bytes. Update `vo := uint64(b.order.Uint32(cell))`.

- [ ] **Step 5: Update File.Open to thread bigTIFF through**

In `internal/tiff/file.go`, update:

```go
    ifds, err := walkIFDs(br, int64(h.firstIFD), h.bigTIFF)
```

And add a `BigTIFF()` accessor on `File` alongside `LittleEndian()`:

```go
// BigTIFF reports whether the file uses BigTIFF (8-byte offsets, magic 43).
func (f *File) BigTIFF() bool { return f.bigTIFF }
```

Add a `bigTIFF bool` field to `File`:

```go
type File struct {
    r       io.ReaderAt
    size    int64
    reader  *byteReader
    pages   []*Page
    bigTIFF bool // NEW
}
```

Populate it in `Open`:

```go
    return &File{r: r, size: size, reader: br, pages: pages, bigTIFF: h.bigTIFF}, nil
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tiff/...`
Expected: all classic + BigTIFF tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/tiff/ifd.go internal/tiff/bigifd.go internal/tiff/bigifd_test.go internal/tiff/file.go
git commit -m "feat(tiff): walk BigTIFF IFDs with 20-byte entries and uint64 offsets"
```

---

## Task 5: `Page.TileOffsets64` / `TileByteCounts64` for BigTIFF

**Files:**
- Modify: `internal/tiff/page.go`
- Modify: `internal/tiff/page_test.go`

A BigTIFF SVS or large NDPI may store `TileOffsets` as `LONG8` rather than `LONG`. Existing `Page.TileOffsets() ([]uint32, error)` would truncate. Add uint64 accessors; keep the uint32 versions for callers that don't need 64-bit range.

- [ ] **Step 1: Write failing tests**

Append to `internal/tiff/page_test.go`:

```go
func TestTileOffsets64Compatibility(t *testing.T) {
    // Existing LONG TileOffsets via buildPageTIFF still round-trip through
    // the uint64 accessor.
    f, _ := Open(bytes.NewReader(buildPageTIFF(t)), 0)
    p := f.Pages()[0]
    offs, err := p.TileOffsets64()
    if err != nil {
        t.Fatalf("TileOffsets64: %v", err)
    }
    want := []uint64{1000, 2000, 3000, 4000}
    if !equalU64(offs, want) {
        t.Errorf("got %v, want %v", offs, want)
    }
}

func equalU64(a, b []uint64) bool {
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

Run: `go test ./internal/tiff/... -run TestTileOffsets64Compatibility`
Expected: FAIL — `undefined: TileOffsets64`.

- [ ] **Step 3: Add `TileOffsets64` and `TileByteCounts64`**

In `internal/tiff/page.go`:

```go
// TileOffsets64 returns the TileOffsets array as uint64 values; supports both
// LONG (classic TIFF) and LONG8 (BigTIFF) encodings.
func (p *Page) TileOffsets64() ([]uint64, error) {
    return p.arrayU64(TagTileOffsets)
}

// TileByteCounts64 returns the TileByteCounts array as uint64 values.
func (p *Page) TileByteCounts64() ([]uint64, error) {
    return p.arrayU64(TagTileByteCounts)
}

func (p *Page) arrayU64(tag uint16) ([]uint64, error) {
    e, ok := p.ifd.get(tag)
    if !ok {
        return nil, fmt.Errorf("tiff: tag %d missing", tag)
    }
    return e.Values64(p.br)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tiff/...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/page.go internal/tiff/page_test.go
git commit -m "feat(tiff): add Page.TileOffsets64/TileByteCounts64 for BigTIFF ranges"
```

---

## Batch 2 — `internal/jpeg` (pure Go, marker-level)

Creates the JPEG bitstream manipulation package that assembles valid JPEGs from TIFF-embedded scan fragments. No codec; no pixel data. Consumed by NDPI striped levels and SVS striped associated images.

---

## Task 6: Marker constants and segment iterator

**Files:**
- Create: `internal/jpeg/marker.go`
- Create: `internal/jpeg/scan.go`
- Create: `internal/jpeg/marker_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/jpeg/marker_test.go`:

```go
package jpeg

import (
    "bytes"
    "errors"
    "testing"
)

func TestScanSegmentsMinimal(t *testing.T) {
    // Minimal JPEG: SOI (FFD8), COM (FFFE) with 2-byte length+1-byte payload,
    // EOI (FFD9).
    data := []byte{
        0xFF, 0xD8, // SOI
        0xFF, 0xFE, 0x00, 0x03, 'X', // COM length=3 (includes length bytes), payload 'X'
        0xFF, 0xD9, // EOI
    }
    var got []Marker
    for seg, err := range Scan(bytes.NewReader(data)) {
        if err != nil {
            t.Fatalf("scan err: %v", err)
        }
        got = append(got, seg.Marker)
    }
    want := []Marker{SOI, COM, EOI}
    if len(got) != len(want) {
        t.Fatalf("got markers %v, want %v", got, want)
    }
    for i := range got {
        if got[i] != want[i] {
            t.Errorf("marker[%d]: got 0x%X, want 0x%X", i, got[i], want[i])
        }
    }
}

func TestScanStopsAtEOI(t *testing.T) {
    // Trailing bytes after EOI must not be consumed.
    data := []byte{0xFF, 0xD8, 0xFF, 0xD9, 'J', 'U', 'N', 'K'}
    count := 0
    for _, err := range Scan(bytes.NewReader(data)) {
        if err != nil {
            t.Fatalf("err: %v", err)
        }
        count++
    }
    if count != 2 {
        t.Fatalf("got %d markers, want 2 (SOI+EOI)", count)
    }
}

func TestScanSOSReturnsScanSegment(t *testing.T) {
    // After SOS, the iterator should yield a Segment with Marker=SOS and
    // Payload holding the SOS parameters (length minus 2 bytes). It does
    // NOT walk the entropy-coded scan data — caller uses ReadScan for that.
    data := []byte{
        0xFF, 0xD8,                   // SOI
        0xFF, 0xDA, 0x00, 0x08, 1, 2, 3, 4, 5, 6, // SOS len=8, payload 6 bytes
        // entropy-coded scan (byte-stuffed): the iterator should NOT read this
        0x11, 0x22, 0x33, 0xFF, 0x00, 0x44,
        0xFF, 0xD9,                   // EOI
    }
    var sosPayload []byte
    for seg, err := range Scan(bytes.NewReader(data)) {
        if err != nil {
            t.Fatalf("err: %v", err)
        }
        if seg.Marker == SOS {
            sosPayload = seg.Payload
            break // iterator lets us stop here; scan data reading is caller's job
        }
    }
    want := []byte{1, 2, 3, 4, 5, 6}
    if !bytes.Equal(sosPayload, want) {
        t.Fatalf("SOS payload: got %v, want %v", sosPayload, want)
    }
}

func TestScanRejectsBadMagic(t *testing.T) {
    data := []byte{0xFF, 0xFF, 0xFF, 0xFF} // padding-like; no valid marker
    var gotErr error
    for _, err := range Scan(bytes.NewReader(data)) {
        if err != nil {
            gotErr = err
            break
        }
    }
    if !errors.Is(gotErr, ErrBadJPEG) {
        t.Fatalf("expected ErrBadJPEG, got %v", gotErr)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `internal/jpeg/marker.go`**

```go
// Package jpeg provides marker-level JPEG bitstream manipulation sufficient
// to assemble valid JPEGs from TIFF-embedded scan fragments. It does not
// decode or encode pixel data; callers wanting decoded pixels should pass
// this package's output to a JPEG codec of their choice.
//
// The package is deliberately narrow: it understands the 2-byte marker /
// length segment framing of a JPEG bitstream (JFIF JPEG / baseline DCT), can
// round-trip segment sequences, can rewrite the SOF0 dimensions in-place,
// and can concatenate multiple scan fragments (as stored in TIFF tile/stripe
// payloads) into a single valid JPEG by prepending an appropriate header
// derived from the TIFF JPEGTables tag. It does not interpret entropy-coded
// scan data beyond preserving byte stuffing.
package jpeg

import "errors"

// ErrBadJPEG is surfaced when a JPEG bitstream cannot be parsed.
// The top-level opentile package re-exports this as ErrBadJPEGBitstream for
// consumers who import by sentinel rather than by package.
var ErrBadJPEG = errors.New("jpeg: invalid bitstream")

// Marker is the one-byte marker code that follows a 0xFF prefix byte.
type Marker byte

const (
    SOI  Marker = 0xD8 // Start Of Image
    EOI  Marker = 0xD9 // End Of Image
    SOS  Marker = 0xDA // Start Of Scan
    DQT  Marker = 0xDB // Define Quantization Table
    DHT  Marker = 0xC4 // Define Huffman Table
    SOF0 Marker = 0xC0 // Baseline DCT Start Of Frame
    DRI  Marker = 0xDD // Define Restart Interval
    COM  Marker = 0xFE // Comment
    APP0 Marker = 0xE0 // APPn range start
    APP14 Marker = 0xEE
    RST0 Marker = 0xD0 // RST0..RST7 occupy 0xD0..0xD7
)

// Segment is a parsed marker + its payload. Payload excludes the 2-byte
// length prefix when present; it is nil for stand-alone markers (SOI, EOI,
// RSTn). For SOS, Payload holds the scan header parameters; the entropy-
// coded scan that follows is not read by the Scan iterator.
type Segment struct {
    Marker  Marker
    Payload []byte
}

// isStandalone reports whether m is a stand-alone marker (no length / payload).
func (m Marker) isStandalone() bool {
    switch m {
    case SOI, EOI, 0x01, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7:
        return true
    }
    return false
}
```

- [ ] **Step 4: Implement `internal/jpeg/scan.go`**

```go
package jpeg

import (
    "bufio"
    "encoding/binary"
    "fmt"
    "io"
    "iter"
)

// Scan returns a Go 1.23 iterator over JPEG segments. The iterator stops
// after yielding the EOI marker. It does not follow the entropy-coded scan
// data after SOS — callers that need the scan bytes use ReadScan on the
// reader after the SOS Segment is yielded.
//
// Malformed inputs yield a Segment with a zero-valued Marker and a non-nil
// error. Callers must honor the error yield and stop iterating.
func Scan(r io.Reader) iter.Seq2[Segment, error] {
    return func(yield func(Segment, error) bool) {
        br := bufio.NewReader(r)
        for {
            // Every marker begins with 0xFF, possibly preceded by any number
            // of 0xFF fill bytes.
            b, err := br.ReadByte()
            if err != nil {
                yield(Segment{}, fmt.Errorf("%w: read marker prefix: %v", ErrBadJPEG, err))
                return
            }
            if b != 0xFF {
                yield(Segment{}, fmt.Errorf("%w: expected 0xFF, got 0x%02X", ErrBadJPEG, b))
                return
            }
            // Skip fill bytes: consecutive 0xFF until a non-0xFF code.
            var code byte
            for {
                code, err = br.ReadByte()
                if err != nil {
                    yield(Segment{}, fmt.Errorf("%w: read marker code: %v", ErrBadJPEG, err))
                    return
                }
                if code != 0xFF {
                    break
                }
            }
            if code == 0x00 {
                yield(Segment{}, fmt.Errorf("%w: 0xFF00 stuffed byte outside scan data", ErrBadJPEG))
                return
            }
            m := Marker(code)
            if m.isStandalone() {
                if !yield(Segment{Marker: m}, nil) {
                    return
                }
                if m == EOI {
                    return
                }
                continue
            }
            // Marker-segment with 2-byte length.
            var lenBuf [2]byte
            if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
                yield(Segment{}, fmt.Errorf("%w: read length: %v", ErrBadJPEG, err))
                return
            }
            length := binary.BigEndian.Uint16(lenBuf[:])
            if length < 2 {
                yield(Segment{}, fmt.Errorf("%w: segment length %d < 2", ErrBadJPEG, length))
                return
            }
            payload := make([]byte, length-2)
            if _, err := io.ReadFull(br, payload); err != nil {
                yield(Segment{}, fmt.Errorf("%w: read payload: %v", ErrBadJPEG, err))
                return
            }
            if !yield(Segment{Marker: m, Payload: payload}, nil) {
                return
            }
        }
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/jpeg/marker.go internal/jpeg/scan.go internal/jpeg/marker_test.go
git commit -m "feat(jpeg): Marker constants, Segment type, iterator-first Scan"
```

---

## Task 7: `ReadScan` — byte-stuffed entropy-data reader

**Files:**
- Create: `internal/jpeg/segment.go`
- Create: `internal/jpeg/segment_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/jpeg/segment_test.go`:

```go
package jpeg

import (
    "bytes"
    "testing"
)

func TestReadScanStripsNothing(t *testing.T) {
    // Entropy data with a stuffed byte (FF 00) and an RST1 in the middle.
    // ReadScan should return the raw bytes including stuffed 00, stopping
    // just before the next non-RST marker.
    scan := []byte{0x11, 0x22, 0xFF, 0x00, 0x33, 0xFF, 0xD1 /*RST1*/, 0x44, 0xFF, 0xD9 /*EOI*/}
    r := bytes.NewReader(scan)
    got, next, err := ReadScan(r)
    if err != nil {
        t.Fatalf("ReadScan: %v", err)
    }
    want := []byte{0x11, 0x22, 0xFF, 0x00, 0x33, 0xFF, 0xD1, 0x44}
    if !bytes.Equal(got, want) {
        t.Fatalf("got %v, want %v", got, want)
    }
    if next != EOI {
        t.Errorf("next marker: got 0x%X, want 0x%X", next, EOI)
    }
}

func TestReadScanStopsAtSOF(t *testing.T) {
    scan := []byte{0x11, 0xFF, 0xC0, 0x00, 0x08, 1, 2, 3, 4, 5, 6}
    r := bytes.NewReader(scan)
    got, next, err := ReadScan(r)
    if err != nil {
        t.Fatalf("ReadScan: %v", err)
    }
    if !bytes.Equal(got, []byte{0x11}) {
        t.Fatalf("scan data: got %v, want [0x11]", got)
    }
    if next != SOF0 {
        t.Errorf("next marker: got 0x%X, want 0x%X", next, SOF0)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/... -run TestReadScan`
Expected: FAIL — `undefined: ReadScan`.

- [ ] **Step 3: Implement `internal/jpeg/segment.go`**

```go
package jpeg

import (
    "bufio"
    "fmt"
    "io"
)

// ReadScan reads entropy-coded JPEG scan data from r up to (but not
// including) the next non-RST marker. Byte stuffing (0xFF 0x00) is
// preserved in the returned slice because the caller may want to
// concatenate this scan into a new bitstream without decoding.
//
// Returns the scan bytes, the marker code that terminated the scan (with
// the 0xFF prefix stripped), and any read error. If the caller needs the
// stream position to continue past the returned marker, they must then
// consume the marker's length+payload (for length-bearing markers) before
// the next operation. For RST markers (stand-alone) there is nothing more
// to consume.
//
// If r is a *bufio.Reader, ReadScan uses it directly; otherwise it wraps r.
func ReadScan(r io.Reader) (data []byte, end Marker, err error) {
    br, ok := r.(*bufio.Reader)
    if !ok {
        br = bufio.NewReader(r)
    }
    var out []byte
    for {
        b, err := br.ReadByte()
        if err != nil {
            return nil, 0, fmt.Errorf("%w: read scan byte: %v", ErrBadJPEG, err)
        }
        if b != 0xFF {
            out = append(out, b)
            continue
        }
        // Peek the next byte to disambiguate.
        next, err := br.ReadByte()
        if err != nil {
            return nil, 0, fmt.Errorf("%w: read marker after 0xFF: %v", ErrBadJPEG, err)
        }
        switch {
        case next == 0x00:
            // Byte stuffing: represents a literal 0xFF within scan data.
            out = append(out, 0xFF, 0x00)
        case next >= 0xD0 && next <= 0xD7:
            // RSTn — part of the scan, keep going.
            out = append(out, 0xFF, next)
        default:
            // Non-scan marker: end of scan.
            return out, Marker(next), nil
        }
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/segment.go internal/jpeg/segment_test.go
git commit -m "feat(jpeg): add ReadScan for byte-stuffing-aware entropy-data extraction"
```

---

## Task 8: `SplitJPEGTables` — extract DQT/DHT from TIFF JPEGTables

**Files:**
- Create: `internal/jpeg/tables.go`
- Create: `internal/jpeg/tables_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/jpeg/tables_test.go`:

```go
package jpeg

import (
    "bytes"
    "testing"
)

func TestSplitJPEGTables(t *testing.T) {
    // Synthetic JPEGTables blob: SOI + DQT + DHT + DHT + EOI.
    // DQT: FFDB 0003 00 → one table class/id=0, payload is 1 byte
    // DHT: FFC4 0003 10 → one Huffman DC table id=0, payload 1 byte (malformed
    //      but fine for splitting — we're not decoding)
    tables := []byte{
        0xFF, 0xD8, // SOI
        0xFF, 0xDB, 0x00, 0x03, 0x00,          // DQT class=0 id=0 (1 byte payload)
        0xFF, 0xC4, 0x00, 0x03, 0x10,          // DHT class=1 id=0
        0xFF, 0xC4, 0x00, 0x03, 0x00,          // DHT class=0 id=0
        0xFF, 0xD9,                            // EOI
    }
    dqts, dhts, err := SplitJPEGTables(tables)
    if err != nil {
        t.Fatalf("SplitJPEGTables: %v", err)
    }
    if len(dqts) != 1 {
        t.Fatalf("dqts: got %d, want 1", len(dqts))
    }
    if len(dhts) != 2 {
        t.Fatalf("dhts: got %d, want 2", len(dhts))
    }
    // Each returned segment is the full bytes INCLUDING the marker and
    // length, suitable for concatenation into a new bitstream.
    wantDQT := []byte{0xFF, 0xDB, 0x00, 0x03, 0x00}
    if !bytes.Equal(dqts[0], wantDQT) {
        t.Errorf("dqt[0]: got %v, want %v", dqts[0], wantDQT)
    }
}

func TestSplitJPEGTablesRejectsNoSOI(t *testing.T) {
    _, _, err := SplitJPEGTables([]byte{0xFF, 0xDB, 0, 3, 0})
    if err == nil {
        t.Fatal("expected error on missing SOI")
    }
}

func TestSplitJPEGTablesIgnoresUnknownSegments(t *testing.T) {
    // A COM segment in the middle should be tolerated but not returned.
    tables := []byte{
        0xFF, 0xD8,
        0xFF, 0xFE, 0x00, 0x04, 'x', 'y',      // COM
        0xFF, 0xDB, 0x00, 0x03, 0x00,          // DQT
        0xFF, 0xD9,
    }
    dqts, dhts, err := SplitJPEGTables(tables)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if len(dqts) != 1 || len(dhts) != 0 {
        t.Errorf("got dqts=%d dhts=%d, want 1/0", len(dqts), len(dhts))
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/... -run TestSplitJPEGTables`
Expected: FAIL — `undefined: SplitJPEGTables`.

- [ ] **Step 3: Implement `internal/jpeg/tables.go`**

```go
package jpeg

import (
    "bytes"
    "encoding/binary"
    "fmt"
)

// SplitJPEGTables parses the value of a TIFF JPEGTables tag and returns each
// DQT and DHT segment separately. Each returned slice element is the full
// segment bytes including the 0xFF marker prefix and 2-byte length, ready
// to concatenate into a new bitstream.
//
// JPEGTables is a mini-JPEG containing SOI, one or more DQT, one or more
// DHT, and EOI. Other segments (COM, APPn) are tolerated but ignored.
func SplitJPEGTables(tables []byte) (dqts [][]byte, dhts [][]byte, err error) {
    if len(tables) < 4 || tables[0] != 0xFF || Marker(tables[1]) != SOI {
        return nil, nil, fmt.Errorf("%w: JPEGTables does not start with SOI", ErrBadJPEG)
    }
    // Walk segments starting after SOI.
    pos := 2
    for pos < len(tables) {
        if tables[pos] != 0xFF {
            return nil, nil, fmt.Errorf("%w: expected 0xFF at pos %d", ErrBadJPEG, pos)
        }
        // Skip fill bytes.
        for pos < len(tables) && tables[pos] == 0xFF {
            pos++
        }
        if pos >= len(tables) {
            return nil, nil, fmt.Errorf("%w: truncated", ErrBadJPEG)
        }
        code := Marker(tables[pos])
        pos++
        if code == EOI {
            return dqts, dhts, nil
        }
        if code.isStandalone() {
            continue
        }
        if pos+2 > len(tables) {
            return nil, nil, fmt.Errorf("%w: truncated length at pos %d", ErrBadJPEG, pos)
        }
        length := int(binary.BigEndian.Uint16(tables[pos : pos+2]))
        if length < 2 {
            return nil, nil, fmt.Errorf("%w: segment length %d < 2", ErrBadJPEG, length)
        }
        // Segment bytes to return: the 0xFF prefix, the marker code, the
        // length bytes, and the payload. Since fill bytes may have been
        // consumed, we reconstruct a single-prefix segment from the data
        // we saw.
        segStart := pos - 2 // back up to the one retained 0xFF (we consumed extras as fill)
        // Locate the actual 0xFF + code we kept:
        actualStart := bytes.LastIndexByte(tables[:segStart+1], 0xFF)
        if actualStart < 0 {
            actualStart = segStart
        }
        end := pos + length
        if end > len(tables) {
            return nil, nil, fmt.Errorf("%w: segment extends past buffer (end=%d, len=%d)", ErrBadJPEG, end, len(tables))
        }
        seg := make([]byte, 0, 2+length)
        seg = append(seg, 0xFF, byte(code))
        seg = append(seg, tables[pos:end]...)
        pos = end
        switch code {
        case DQT:
            dqts = append(dqts, seg)
        case DHT:
            dhts = append(dhts, seg)
        }
    }
    return dqts, dhts, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/tables.go internal/jpeg/tables_test.go
git commit -m "feat(jpeg): SplitJPEGTables extracts DQT/DHT segments from TIFF JPEGTables tag"
```

---

## Task 9: SOF0 parse and build

**Files:**
- Create: `internal/jpeg/sof.go`
- Create: `internal/jpeg/sof_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/jpeg/sof_test.go`:

```go
package jpeg

import (
    "bytes"
    "testing"
)

func TestParseSOFYCbCr420(t *testing.T) {
    // SOF0 payload: precision=8, height=0x0200 (512), width=0x0300 (768),
    // 3 components, each: id, sampling (H<<4|V), quant-id.
    // Y: H=2 V=2, Cb: 1/1, Cr: 1/1 → 4:2:0 subsampling.
    payload := []byte{
        0x08,             // precision
        0x02, 0x00,       // height 512
        0x03, 0x00,       // width 768
        0x03,             // 3 components
        0x01, 0x22, 0x00, // Y id=1 H=2 V=2 qt=0
        0x02, 0x11, 0x01, // Cb id=2 H=1 V=1 qt=1
        0x03, 0x11, 0x01, // Cr id=3 H=1 V=1 qt=1
    }
    sof, err := ParseSOF(payload)
    if err != nil {
        t.Fatalf("ParseSOF: %v", err)
    }
    if sof.Width != 768 || sof.Height != 512 {
        t.Errorf("dims: got %dx%d, want 768x512", sof.Width, sof.Height)
    }
    if len(sof.Components) != 3 {
        t.Fatalf("components: got %d, want 3", len(sof.Components))
    }
    if sof.Components[0].SamplingH != 2 || sof.Components[0].SamplingV != 2 {
        t.Errorf("Y sampling: got %d/%d, want 2/2",
            sof.Components[0].SamplingH, sof.Components[0].SamplingV)
    }
    mcuW, mcuH := sof.MCUSize()
    if mcuW != 16 || mcuH != 16 {
        t.Errorf("MCU: got %dx%d, want 16x16 (4:2:0)", mcuW, mcuH)
    }
}

func TestBuildSOFRoundTrip(t *testing.T) {
    want := &SOF{
        Precision: 8, Width: 512, Height: 256,
        Components: []SOFComponent{
            {ID: 1, SamplingH: 1, SamplingV: 1, QuantTableID: 0},
            {ID: 2, SamplingH: 1, SamplingV: 1, QuantTableID: 0},
            {ID: 3, SamplingH: 1, SamplingV: 1, QuantTableID: 0},
        },
    }
    seg := BuildSOF(want)
    // Verify seg begins with FF C0 and the length field is consistent.
    if seg[0] != 0xFF || Marker(seg[1]) != SOF0 {
        t.Fatalf("marker: got %x %x, want FF C0", seg[0], seg[1])
    }
    length := int(seg[2])<<8 | int(seg[3])
    wantLen := 2 /*length bytes*/ + 6 /*fixed*/ + 3*len(want.Components)
    if length != wantLen {
        t.Errorf("length: got %d, want %d", length, wantLen)
    }
    // Strip marker+length, parse back, compare structurally.
    got, err := ParseSOF(seg[4:])
    if err != nil {
        t.Fatalf("ParseSOF round-trip: %v", err)
    }
    if got.Width != want.Width || got.Height != want.Height {
        t.Errorf("dims drift: got %dx%d", got.Width, got.Height)
    }
    if !bytes.Equal(seg, BuildSOF(got)) {
        t.Error("BuildSOF not deterministic on round-trip")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/... -run "TestParseSOF|TestBuildSOF"`
Expected: FAIL — `undefined: SOF, ParseSOF, BuildSOF`.

- [ ] **Step 3: Implement `internal/jpeg/sof.go`**

```go
package jpeg

import (
    "encoding/binary"
    "fmt"
)

// SOF describes a Start-Of-Frame-0 (baseline DCT) segment's parameters.
type SOF struct {
    Precision     uint8 // typically 8 for baseline DCT
    Height, Width uint16
    Components    []SOFComponent
}

// SOFComponent describes one component within an SOF segment.
type SOFComponent struct {
    ID            uint8 // 1=Y, 2=Cb, 3=Cr for YCbCr; 1=R, 2=G, 3=B for RGB
    SamplingH     uint8
    SamplingV     uint8
    QuantTableID  uint8
}

// MCUSize returns the minimum coded unit size in pixels, derived from the
// maximum horizontal and vertical sampling factors across components.
// For YCbCr 4:2:0 (Y=2,2 others 1,1) → 16x16; 4:4:4 → 8x8; 4:2:2 → 16x8.
func (s *SOF) MCUSize() (w, h int) {
    var maxH, maxV uint8 = 1, 1
    for _, c := range s.Components {
        if c.SamplingH > maxH {
            maxH = c.SamplingH
        }
        if c.SamplingV > maxV {
            maxV = c.SamplingV
        }
    }
    return int(maxH) * 8, int(maxV) * 8
}

// ParseSOF decodes a SOF0 segment payload (the bytes AFTER the 2-byte length).
func ParseSOF(payload []byte) (*SOF, error) {
    if len(payload) < 6 {
        return nil, fmt.Errorf("%w: SOF payload %d < 6", ErrBadJPEG, len(payload))
    }
    s := &SOF{
        Precision: payload[0],
        Height:    binary.BigEndian.Uint16(payload[1:3]),
        Width:     binary.BigEndian.Uint16(payload[3:5]),
    }
    n := int(payload[5])
    expected := 6 + 3*n
    if len(payload) < expected {
        return nil, fmt.Errorf("%w: SOF payload %d < needed %d", ErrBadJPEG, len(payload), expected)
    }
    s.Components = make([]SOFComponent, n)
    for i := 0; i < n; i++ {
        off := 6 + 3*i
        samp := payload[off+1]
        s.Components[i] = SOFComponent{
            ID:           payload[off],
            SamplingH:    samp >> 4,
            SamplingV:    samp & 0x0F,
            QuantTableID: payload[off+2],
        }
    }
    return s, nil
}

// BuildSOF encodes an SOF struct as a complete marker segment (prefix
// 0xFF 0xC0, 2-byte length, payload). The returned slice is ready to
// concatenate into a new bitstream.
func BuildSOF(s *SOF) []byte {
    n := len(s.Components)
    length := 2 + 6 + 3*n
    out := make([]byte, 2+length)
    out[0] = 0xFF
    out[1] = byte(SOF0)
    binary.BigEndian.PutUint16(out[2:4], uint16(length))
    out[4] = s.Precision
    binary.BigEndian.PutUint16(out[5:7], s.Height)
    binary.BigEndian.PutUint16(out[7:9], s.Width)
    out[9] = byte(n)
    for i, c := range s.Components {
        o := 10 + 3*i
        out[o] = c.ID
        out[o+1] = (c.SamplingH << 4) | (c.SamplingV & 0x0F)
        out[o+2] = c.QuantTableID
    }
    return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/sof.go internal/jpeg/sof_test.go
git commit -m "feat(jpeg): parse/build SOF0 segments with MCU-size derivation"
```

---

## Task 10: `ReplaceSOFDimensions` — rewrite SOF width/height in-place

**Files:**
- Modify: `internal/jpeg/sof.go`
- Modify: `internal/jpeg/sof_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/jpeg/sof_test.go`:

```go
func TestReplaceSOFDimensions(t *testing.T) {
    // Full minimal JPEG: SOI + SOF0(512x256) + SOS(empty) + EOI.
    sof := BuildSOF(&SOF{
        Precision: 8, Width: 512, Height: 256,
        Components: []SOFComponent{
            {ID: 1, SamplingH: 1, SamplingV: 1, QuantTableID: 0},
        },
    })
    jpg := append([]byte{0xFF, 0xD8}, sof...)
    jpg = append(jpg, 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0, 0, 0x3F, 0x00) // SOS
    jpg = append(jpg, 0xFF, 0xD9)

    got, err := ReplaceSOFDimensions(jpg, 1024, 768)
    if err != nil {
        t.Fatalf("ReplaceSOFDimensions: %v", err)
    }
    // Find the new SOF, parse it, confirm dims.
    var newSOF *SOF
    for seg, err := range Scan(bytes.NewReader(got)) {
        if err != nil {
            t.Fatalf("scan: %v", err)
        }
        if seg.Marker == SOF0 {
            newSOF, _ = ParseSOF(seg.Payload)
            break
        }
    }
    if newSOF == nil {
        t.Fatal("SOF not found in rewritten JPEG")
    }
    if newSOF.Width != 1024 || newSOF.Height != 768 {
        t.Errorf("dims: got %dx%d, want 1024x768", newSOF.Width, newSOF.Height)
    }
}

func TestReplaceSOFDimensionsRejectsMissingSOF(t *testing.T) {
    jpg := []byte{0xFF, 0xD8, 0xFF, 0xD9} // SOI + EOI, no SOF
    _, err := ReplaceSOFDimensions(jpg, 1, 1)
    if err == nil {
        t.Fatal("expected error when no SOF present")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/... -run TestReplaceSOFDimensions`
Expected: FAIL — `undefined: ReplaceSOFDimensions`.

- [ ] **Step 3: Implement `ReplaceSOFDimensions`**

Append to `internal/jpeg/sof.go`:

```go
// ReplaceSOFDimensions returns a copy of jpg with the SOF0 segment's width
// and height fields rewritten. Other bytes are unchanged; the encoded scan
// coefficients are not interpreted. This is the operation needed to "pad"
// a JPEG to MCU-aligned dimensions before a lossless crop: the header
// advertises the MCU-rounded size even though the scan data is the same.
func ReplaceSOFDimensions(jpg []byte, width, height uint16) ([]byte, error) {
    // Locate the SOF0 marker.
    sofStart := -1
    for i := 0; i < len(jpg)-1; i++ {
        if jpg[i] == 0xFF && Marker(jpg[i+1]) == SOF0 {
            sofStart = i
            break
        }
    }
    if sofStart < 0 {
        return nil, fmt.Errorf("%w: no SOF0 in bitstream", ErrBadJPEG)
    }
    // SOF payload begins at sofStart+4 (2 marker bytes + 2 length bytes).
    payloadStart := sofStart + 4
    if payloadStart+5 > len(jpg) {
        return nil, fmt.Errorf("%w: SOF truncated", ErrBadJPEG)
    }
    out := make([]byte, len(jpg))
    copy(out, jpg)
    // Height at payloadStart+1, width at payloadStart+3 (big-endian).
    binary.BigEndian.PutUint16(out[payloadStart+1:payloadStart+3], height)
    binary.BigEndian.PutUint16(out[payloadStart+3:payloadStart+5], width)
    return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/sof.go internal/jpeg/sof_test.go
git commit -m "feat(jpeg): ReplaceSOFDimensions rewrites SOF0 width/height in place"
```

---

## Task 11: `ConcatenateScans` — assemble JPEGs from TIFF fragments

**Files:**
- Create: `internal/jpeg/concat.go`
- Create: `internal/jpeg/concat_test.go`

This is the hot path used by NDPI striped levels and SVS striped associated images. It builds a complete JPEG from multiple TIFF-embedded scan fragments plus the JPEGTables tables.

- [ ] **Step 1: Write failing tests**

Create `internal/jpeg/concat_test.go`:

```go
package jpeg

import (
    "bytes"
    "testing"
)

// fakeScan constructs a "fragment" that looks like a single-SOS JPEG scan:
// SOI + DQT + DHT + SOF + SOS + scan_data + EOI. ConcatenateScans will
// extract the entropy-coded part (the bytes between SOS's payload end and
// the next non-RST marker) from each fragment.
func fakeScan(t *testing.T, width, height uint16, scanData []byte) []byte {
    t.Helper()
    var buf bytes.Buffer
    buf.Write([]byte{0xFF, 0xD8})
    // DQT: marker + len=3 + class/id=0 + 1 byte quant value
    buf.Write([]byte{0xFF, 0xDB, 0x00, 0x03, 0x00})
    // DHT: marker + len=3 + class/id=0x10 + 1 byte symbol length count
    buf.Write([]byte{0xFF, 0xC4, 0x00, 0x03, 0x10})
    // SOF
    sof := BuildSOF(&SOF{
        Precision: 8, Width: width, Height: height,
        Components: []SOFComponent{
            {ID: 1, SamplingH: 1, SamplingV: 1, QuantTableID: 0},
        },
    })
    buf.Write(sof)
    // SOS: marker + len=8 + 1 component + id=1 + 0x00 + Ss=0 + Se=63 + Ah/Al=0
    buf.Write([]byte{0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00})
    // Scan data (byte-stuffed)
    buf.Write(scanData)
    // EOI
    buf.Write([]byte{0xFF, 0xD9})
    return buf.Bytes()
}

func TestConcatenateScansTwoFragments(t *testing.T) {
    frag1 := fakeScan(t, 16, 8, []byte{0x11, 0x22})
    frag2 := fakeScan(t, 16, 8, []byte{0x33, 0x44})

    jpegtables := []byte{
        0xFF, 0xD8,                    // SOI
        0xFF, 0xDB, 0x00, 0x03, 0x55,  // DQT with different quant value
        0xFF, 0xC4, 0x00, 0x03, 0x20,  // DHT
        0xFF, 0xD9,                    // EOI
    }
    out, err := ConcatenateScans(
        [][]byte{frag1, frag2},
        ConcatOpts{Width: 16, Height: 16, JPEGTables: jpegtables, RestartInterval: 1},
    )
    if err != nil {
        t.Fatalf("ConcatenateScans: %v", err)
    }
    // Verify the output is well-formed by walking segments.
    var markers []Marker
    for seg, err := range Scan(bytes.NewReader(out)) {
        if err != nil {
            t.Fatalf("scan: %v", err)
        }
        markers = append(markers, seg.Marker)
        if seg.Marker == SOS {
            break
        }
    }
    // Expected order: SOI, DQT (from tables), DHT (from tables), SOF, DRI, SOS
    want := []Marker{SOI, DQT, DHT, SOF0, DRI, SOS}
    if len(markers) != len(want) {
        t.Fatalf("segment order: got %v, want %v", markers, want)
    }
    for i := range markers {
        if markers[i] != want[i] {
            t.Errorf("segment %d: got 0x%X, want 0x%X", i, markers[i], want[i])
        }
    }
    // The tail should be ...scan1 + RST0 + scan2 + EOI.
    // Find the last marker (EOI) position.
    if out[len(out)-2] != 0xFF || Marker(out[len(out)-1]) != EOI {
        t.Errorf("final bytes: got 0x%X 0x%X, want FF D9", out[len(out)-2], out[len(out)-1])
    }
}

func TestConcatenateScansRejectsEmptyFragments(t *testing.T) {
    _, err := ConcatenateScans(nil, ConcatOpts{Width: 1, Height: 1})
    if err == nil {
        t.Fatal("expected error on empty fragments")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/jpeg/... -run TestConcatenateScans`
Expected: FAIL — `undefined: ConcatenateScans, ConcatOpts`.

- [ ] **Step 3: Implement `internal/jpeg/concat.go`**

```go
package jpeg

import (
    "bufio"
    "bytes"
    "encoding/binary"
    "fmt"
)

// ConcatOpts controls how ConcatenateScans assembles the output JPEG.
type ConcatOpts struct {
    Width, Height   uint16 // output SOF dimensions
    JPEGTables      []byte // raw TIFF JPEGTables value; DQT/DHT extracted via SplitJPEGTables
    ColorspaceFix   bool   // if true, emit an APP14 "Adobe" segment signalling RGB (for SVS non-standard RGB JPEGs)
    RestartInterval int    // 0 = no DRI; otherwise emit DRI and insert RST markers between fragment scans
}

// ConcatenateScans builds a single valid JPEG from one or more TIFF-embedded
// JPEG fragments. Each fragment is a mini-JPEG (SOI ... SOS + scan + EOI);
// the assembled output uses the tables from opts.JPEGTables and its own SOF
// derived from opts.Width/Height, with the scan data of each fragment
// concatenated in order and optionally separated by restart markers.
func ConcatenateScans(fragments [][]byte, opts ConcatOpts) ([]byte, error) {
    if len(fragments) == 0 {
        return nil, fmt.Errorf("%w: no fragments", ErrBadJPEG)
    }

    dqts, dhts, err := SplitJPEGTables(opts.JPEGTables)
    if err != nil {
        return nil, fmt.Errorf("split tables: %w", err)
    }

    // Determine the SOF from the first fragment (for component sampling info)
    // and override width/height from opts.
    var firstSOF *SOF
    var firstSOS []byte
    for seg, err := range Scan(bytes.NewReader(fragments[0])) {
        if err != nil {
            return nil, fmt.Errorf("scan first fragment: %w", err)
        }
        switch seg.Marker {
        case SOF0:
            s, err := ParseSOF(seg.Payload)
            if err != nil {
                return nil, err
            }
            firstSOF = s
        case SOS:
            firstSOS = make([]byte, 0, 4+len(seg.Payload))
            firstSOS = append(firstSOS, 0xFF, byte(SOS))
            length := 2 + len(seg.Payload)
            lb := make([]byte, 2)
            binary.BigEndian.PutUint16(lb, uint16(length))
            firstSOS = append(firstSOS, lb...)
            firstSOS = append(firstSOS, seg.Payload...)
        }
        if firstSOF != nil && firstSOS != nil {
            break
        }
    }
    if firstSOF == nil {
        return nil, fmt.Errorf("%w: first fragment missing SOF", ErrBadJPEG)
    }
    if firstSOS == nil {
        return nil, fmt.Errorf("%w: first fragment missing SOS", ErrBadJPEG)
    }
    sof := &SOF{
        Precision:  firstSOF.Precision,
        Width:      opts.Width,
        Height:     opts.Height,
        Components: firstSOF.Components,
    }

    var out bytes.Buffer
    out.Write([]byte{0xFF, 0xD8}) // SOI
    if opts.ColorspaceFix {
        // APP14 "Adobe\0" segment identifying RGB-encoded JPEG.
        // Payload: "Adobe" (5 bytes) + 0x00 + version (2 bytes) + flags0 (2) + flags1 (2) + transform (1).
        app14 := []byte{
            0xFF, 0xEE, 0x00, 0x0E,
            'A', 'd', 'o', 'b', 'e', 0x00,
            0x64, 0x00, // version
            0x00, 0x00, // flags0
            0x00, 0x00, // flags1
            0x00,       // transform = 0 (RGB)
        }
        out.Write(app14)
    }
    for _, seg := range dqts {
        out.Write(seg)
    }
    for _, seg := range dhts {
        out.Write(seg)
    }
    out.Write(BuildSOF(sof))
    if opts.RestartInterval > 0 {
        // DRI: marker + length=4 + interval (u16)
        dri := []byte{0xFF, 0xDD, 0x00, 0x04, 0, 0}
        binary.BigEndian.PutUint16(dri[4:], uint16(opts.RestartInterval))
        out.Write(dri)
    }
    out.Write(firstSOS)

    // Concatenate entropy data from each fragment, inserting restart markers
    // between fragments when RestartInterval > 0.
    for i, frag := range fragments {
        scanData, err := extractScanData(frag)
        if err != nil {
            return nil, fmt.Errorf("fragment %d: %w", i, err)
        }
        out.Write(scanData)
        if opts.RestartInterval > 0 && i < len(fragments)-1 {
            rstCode := byte(0xD0 + (i % 8))
            out.Write([]byte{0xFF, rstCode})
        }
    }
    out.Write([]byte{0xFF, 0xD9}) // EOI
    return out.Bytes(), nil
}

// extractScanData walks frag, finds the SOS, and returns the entropy-coded
// bytes up to (but not including) the trailing EOI. Byte stuffing is
// preserved.
func extractScanData(frag []byte) ([]byte, error) {
    br := bufio.NewReader(bytes.NewReader(frag))
    // Find SOS.
    for seg, err := range Scan(br) {
        if err != nil {
            return nil, err
        }
        if seg.Marker == SOS {
            // Entropy-coded scan follows immediately.
            data, end, err := ReadScan(br)
            if err != nil {
                return nil, err
            }
            if end != EOI {
                return nil, fmt.Errorf("%w: scan ended with 0x%X, want EOI", ErrBadJPEG, end)
            }
            return data, nil
        }
    }
    return nil, fmt.Errorf("%w: no SOS found", ErrBadJPEG)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jpeg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/concat.go internal/jpeg/concat_test.go
git commit -m "feat(jpeg): ConcatenateScans assembles JPEGs from TIFF fragments

The hot operation for NDPI striped tiles and SVS striped associated
images: given N JPEG fragments (mini-JPEGs as stored in TIFF) plus the
JPEGTables value, emit a single well-formed JPEG whose SOF carries the
caller-specified dimensions and whose scan data is the fragments'
entropy-coded streams concatenated with optional restart markers."
```

---

## Batch 3 — `internal/jpegturbo` (cgo)

Single-function wrapper over `tjTransform + TJXOPT_CROP + TJXOPT_PERFECT`. Build-tag-gated: real implementation under cgo, stub under `!cgo || nocgo`.

---

## Task 12: `internal/jpegturbo` — scaffolding + `nocgo` stub

**Files:**
- Create: `internal/jpegturbo/turbo.go`
- Create: `internal/jpegturbo/turbo_nocgo.go`
- Create: `internal/jpegturbo/turbo_nocgo_test.go`

- [ ] **Step 1: Write failing test (nocgo path)**

Create `internal/jpegturbo/turbo_nocgo_test.go`:

```go
//go:build !cgo || nocgo

package jpegturbo

import (
    "errors"
    "testing"
)

func TestCropReturnsErrCGORequired(t *testing.T) {
    _, err := Crop([]byte{0xFF, 0xD8, 0xFF, 0xD9}, Region{X: 0, Y: 0, Width: 8, Height: 8})
    if !errors.Is(err, ErrCGORequired) {
        t.Fatalf("expected ErrCGORequired, got %v", err)
    }
}
```

- [ ] **Step 2: Run with nocgo to verify failure**

Run: `CGO_ENABLED=0 go test ./internal/jpegturbo/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Create `turbo.go` (always compiled)**

```go
// Package jpegturbo provides a minimal cgo wrapper over libjpeg-turbo's
// tjTransform operation, scoped to the lossless MCU-aligned JPEG crop that
// opentile-go needs for one-frame NDPI pyramid levels and NDPI label
// cropping. It is deliberately the only cgo package in the module.
//
// Default builds link libjpeg-turbo 2.1+ via pkg-config. The `nocgo` build
// tag (or CGO_ENABLED=0) swaps in a stub Crop that returns ErrCGORequired,
// letting the rest of the library build and run for SVS-only consumers who
// cannot link C dependencies.
package jpegturbo

import "errors"

// ErrCGORequired is returned from Crop when the package was compiled without
// cgo support. Callers propagate this wrapped in opentile.TileError.
var ErrCGORequired = errors.New("jpegturbo: this operation requires cgo + libjpeg-turbo (build without -tags nocgo)")

// Region is an MCU-aligned pixel rectangle within a JPEG. libjpeg-turbo with
// TJXOPT_PERFECT rejects non-aligned regions rather than silently producing
// a partial MCU output.
type Region struct {
    X, Y, Width, Height int
}
```

- [ ] **Step 4: Create `turbo_nocgo.go`**

```go
//go:build !cgo || nocgo

package jpegturbo

// Crop returns ErrCGORequired in nocgo builds. See turbo_cgo.go for the real
// implementation.
func Crop(src []byte, r Region) ([]byte, error) {
    return nil, ErrCGORequired
}
```

- [ ] **Step 5: Run nocgo test**

Run: `CGO_ENABLED=0 go test ./internal/jpegturbo/...`
Expected: PASS.

Also confirm `go vet` is happy:

Run: `CGO_ENABLED=0 go vet ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/jpegturbo/turbo.go internal/jpegturbo/turbo_nocgo.go internal/jpegturbo/turbo_nocgo_test.go
git commit -m "feat(jpegturbo): scaffold package + nocgo stub returning ErrCGORequired"
```

---

## Task 13: `internal/jpegturbo` — cgo implementation of `Crop`

**Files:**
- Create: `internal/jpegturbo/turbo_cgo.go`
- Create: `internal/jpegturbo/turbo_cgo_test.go`

- [ ] **Step 1: Verify prerequisites on the dev machine**

Run: `pkg-config --modversion libturbojpeg`
Expected: prints `2.1.x` or higher. If not, install libjpeg-turbo-dev before proceeding. On macOS: `brew install jpeg-turbo`. On Debian/Ubuntu: `apt-get install libturbojpeg0-dev`.

- [ ] **Step 2: Write failing tests (cgo path)**

Create `internal/jpegturbo/turbo_cgo_test.go`:

```go
//go:build cgo && !nocgo

package jpegturbo

import (
    "bytes"
    "image"
    "image/jpeg"
    "testing"
)

// encodeTestJPEG creates a plain solid-color JPEG of the given dimensions
// via stdlib — test-only, never imported by library code.
func encodeTestJPEG(t *testing.T, w, h int) []byte {
    t.Helper()
    img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
    // Fill Y plane with a constant so MCU alignment is easy to reason about.
    for i := range img.Y {
        img.Y[i] = 128
    }
    var buf bytes.Buffer
    if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
        t.Fatalf("encode: %v", err)
    }
    return buf.Bytes()
}

func TestCropMCUAligned(t *testing.T) {
    // 32x32 4:2:0 JPEG → MCU 16x16; crop the top-left 16x16.
    src := encodeTestJPEG(t, 32, 32)
    got, err := Crop(src, Region{X: 0, Y: 0, Width: 16, Height: 16})
    if err != nil {
        t.Fatalf("Crop: %v", err)
    }
    // Decode the crop and confirm dimensions.
    img, err := jpeg.Decode(bytes.NewReader(got))
    if err != nil {
        t.Fatalf("decode cropped: %v", err)
    }
    if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
        t.Errorf("dims: got %v, want 16x16", img.Bounds())
    }
}

func TestCropNonAlignedRejected(t *testing.T) {
    src := encodeTestJPEG(t, 32, 32)
    _, err := Crop(src, Region{X: 1, Y: 0, Width: 16, Height: 16})
    if err == nil {
        t.Fatal("expected error on non-MCU-aligned crop")
    }
}

func TestCropBadInput(t *testing.T) {
    _, err := Crop([]byte("not a jpeg"), Region{X: 0, Y: 0, Width: 8, Height: 8})
    if err == nil {
        t.Fatal("expected error on garbage input")
    }
}
```

- [ ] **Step 3: Run test to verify failure**

Run: `go test ./internal/jpegturbo/...`
Expected: FAIL — `undefined: Crop` for the cgo build (the nocgo stub is not compiled when cgo is active).

- [ ] **Step 4: Implement `internal/jpegturbo/turbo_cgo.go`**

```go
//go:build cgo && !nocgo

package jpegturbo

/*
#cgo pkg-config: libturbojpeg
#include <turbojpeg.h>
#include <stdlib.h>

// Small C helper keeps the Go code free of tjtransform struct literal.
static int go_tj_transform_crop(
    const unsigned char *src, unsigned long src_size,
    int x, int y, int w, int h,
    unsigned char **dst, unsigned long *dst_size
) {
    tjhandle h_ = tjInitTransform();
    if (h_ == NULL) {
        return -1;
    }
    tjtransform t;
    // Zero-init the struct; only the fields we touch matter.
    t.r.x = x;
    t.r.y = y;
    t.r.w = w;
    t.r.h = h;
    t.op = TJXOP_NONE;
    t.options = TJXOPT_CROP | TJXOPT_PERFECT;
    t.data = NULL;
    t.customFilter = NULL;

    int rc = tjTransform(h_, src, src_size, 1, dst, dst_size, &t, 0);
    tjDestroy(h_);
    return rc;
}
*/
import "C"
import (
    "fmt"
    "unsafe"
)

// Crop performs an MCU-aligned lossless crop of src using libjpeg-turbo's
// tjTransform with TJXOPT_CROP and TJXOPT_PERFECT. A region that is not
// MCU-aligned, or that extends past the source dimensions, is rejected by
// libjpeg-turbo with a non-zero return code and Crop returns an error.
func Crop(src []byte, r Region) ([]byte, error) {
    if len(src) == 0 {
        return nil, fmt.Errorf("jpegturbo: empty source")
    }
    var dst *C.uchar
    var dstSize C.ulong
    rc := C.go_tj_transform_crop(
        (*C.uchar)(unsafe.Pointer(&src[0])),
        C.ulong(len(src)),
        C.int(r.X), C.int(r.Y), C.int(r.Width), C.int(r.Height),
        &dst, &dstSize,
    )
    if rc != 0 {
        return nil, fmt.Errorf("jpegturbo: tjTransform returned rc=%d (non-MCU crop? malformed input?)", rc)
    }
    defer C.tjFree(unsafe.Pointer(dst))
    out := C.GoBytes(unsafe.Pointer(dst), C.int(dstSize))
    return out, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/jpegturbo/...`
Expected: PASS for all three tests. If the "not a jpeg" or non-aligned case panics rather than errors, the C helper's contract with `tjTransform` may have changed between libjpeg-turbo versions — verify the installed version is 2.1+ and debug from there.

Run: `go test ./... -race`
Expected: entire suite still passes.

- [ ] **Step 6: Commit**

```bash
git add internal/jpegturbo/turbo_cgo.go internal/jpegturbo/turbo_cgo_test.go
git commit -m "feat(jpegturbo): cgo Crop via tjTransform with TJXOPT_CROP|TJXOPT_PERFECT"
```

---

## Batch 4 — `formats/ndpi/` (the NDPI format)

Six tasks. Mirrors the v0.1 SVS layout but with the stripe-to-tile reshaping logic at the center.

---

## Task 14: `formats/ndpi/ndpi.go` — Factory, Supports, Open skeleton

**Files:**
- Create: `formats/ndpi/ndpi.go`
- Create: `formats/ndpi/ndpi_test.go`

Mirrors the v0.1 SVS Factory; `Open` is stubbed and replaced in Task 20 after all helpers exist.

- [ ] **Step 1: Write failing tests**

Create `formats/ndpi/ndpi_test.go`:

```go
package ndpi

import (
    "bytes"
    "testing"

    "github.com/wsilabs/opentile-go/internal/tiff"
)

// buildNDPIStub returns a tiny classic-TIFF with a SourceLens tag (65420).
func buildNDPIStub(t *testing.T) []byte {
    t.Helper()
    // 5 entries: ImageWidth, ImageLength, TileWidth, TileLength, SourceLens (65420).
    // IFD size = 2 + 5*12 + 4 = 66. ext base at 8+66 = 74.
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    w16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
    w32 := func(v uint32) {
        buf.WriteByte(byte(v))
        buf.WriteByte(byte(v >> 8))
        buf.WriteByte(byte(v >> 16))
        buf.WriteByte(byte(v >> 24))
    }
    w16(5)
    w16(256); w16(3); w32(1); w32(1024)  // ImageWidth
    w16(257); w16(3); w32(1); w32(768)   // ImageLength
    w16(322); w16(3); w32(1); w32(640)   // TileWidth
    w16(323); w16(3); w32(1); w32(8)     // TileLength (stripes are 8 tall)
    w16(65420); w16(3); w32(1); w32(20)  // SourceLens = 20
    w32(0)
    return buf.Bytes()
}

func TestSupportsDetectsNDPI(t *testing.T) {
    data := buildNDPIStub(t)
    f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        t.Fatalf("tiff.Open: %v", err)
    }
    if !New().Supports(f) {
        t.Fatal("Supports: expected true for SourceLens-bearing TIFF")
    }
}

func TestSupportsRejectsNonNDPI(t *testing.T) {
    // Same builder minus the SourceLens entry → no 65420 tag, should reject.
    buf := new(bytes.Buffer)
    buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
    w16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
    w32 := func(v uint32) {
        buf.WriteByte(byte(v))
        buf.WriteByte(byte(v >> 8))
        buf.WriteByte(byte(v >> 16))
        buf.WriteByte(byte(v >> 24))
    }
    w16(4)
    w16(256); w16(3); w32(1); w32(1024)
    w16(257); w16(3); w32(1); w32(768)
    w16(322); w16(3); w32(1); w32(640)
    w16(323); w16(3); w32(1); w32(8)
    w32(0)
    data := buf.Bytes()
    f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
    if New().Supports(f) {
        t.Fatal("Supports: expected false for non-NDPI TIFF")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/ndpi/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `formats/ndpi/ndpi.go`**

```go
// Package ndpi implements opentile-go format support for Hamamatsu NDPI
// files. NDPI is a TIFF variant with vendor-private tags (SourceLens,
// ZOffsetFromSlideCenter, etc.) and pyramid levels stored as horizontal
// stripes — typically 8 pixels tall — that must be reshaped into square
// output tiles at the JPEG marker level.
//
// This package detects NDPI files via the SourceLens (65420) vendor tag,
// parses NDPI-specific metadata, and exposes pyramid levels as opentile.Level
// values. Striped levels use pure-Go marker concatenation (internal/jpeg);
// one-frame levels and the label image require cgo (internal/jpegturbo).
package ndpi

import (
    "fmt"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// ndpiSourceLensTag is the Hamamatsu vendor-private tag used for NDPI
// detection and objective-magnification extraction.
const ndpiSourceLensTag uint16 = 65420

// Factory is the FormatFactory implementation for NDPI.
type Factory struct{}

// New returns an NDPI factory. Safe to register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatNDPI }

// Supports reports whether file looks like an NDPI by checking the first
// page for the SourceLens (65420) vendor-private tag.
func (f *Factory) Supports(file *tiff.File) bool {
    pages := file.Pages()
    if len(pages) == 0 {
        return false
    }
    _, ok := pages[0].ScalarU32(ndpiSourceLensTag)
    return ok
}

// Open is replaced in Task 20 with the real NDPI opener.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    return nil, fmt.Errorf("ndpi.Open: not yet implemented")
}
```

Note: this uses `page.ScalarU32(tag)` — a generic scalar accessor that `page.go` exposes as a public method. Add it if missing:

In `internal/tiff/page.go`:

```go
// ScalarU32 returns the first value of an arbitrary tag as uint32, or
// (0, false) if the tag is absent. Exposed so format packages can read
// vendor-private tags without the internal helpers gaining an accessor.
func (p *Page) ScalarU32(tag uint16) (uint32, bool) { return p.scalarU32(tag) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS for the new NDPI tests plus existing ones.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/ndpi.go formats/ndpi/ndpi_test.go internal/tiff/page.go
git commit -m "feat(ndpi): add Factory with SourceLens-based Supports detection"
```

---

## Task 15: `formats/ndpi/metadata.go` — NDPI metadata parsing and `MetadataOf`

**Files:**
- Create: `formats/ndpi/metadata.go`
- Create: `formats/ndpi/metadata_test.go`

- [ ] **Step 1: Write failing tests**

Create `formats/ndpi/metadata_test.go`:

```go
package ndpi

import (
    "testing"
    "time"

    "github.com/wsilabs/opentile-go/internal/tiff"
)

// These tests exercise the parse helpers directly; end-to-end parsing is
// covered by ndpi_test.go via buildNDPIStub when metadata tags are populated.

func TestParseMetadataFromTags(t *testing.T) {
    // parseMetadata takes a page-like shape we pass manually.
    got := parseFromFields(metadataFields{
        SourceLens:              20,
        Model:                   "NanoZoomer 2.0-HT",
        DateTime:                "2014:01:07 11:22:33",
        XResolution:             [2]uint32{100000, 1}, // 100000 dpi → conversion applies below
        YResolution:             [2]uint32{100000, 1},
        ResolutionUnit:          3, // centimeters
        ZOffsetFromSlideCenter:  2500, // nm
        Reference:               "SN-1234",
    })
    if got.Magnification != 20 {
        t.Errorf("Magnification: got %v, want 20", got.Magnification)
    }
    if got.ScannerModel != "NanoZoomer 2.0-HT" {
        t.Errorf("Model: got %q", got.ScannerModel)
    }
    want := time.Date(2014, 1, 7, 11, 22, 33, 0, time.UTC)
    if !got.AcquisitionDateTime.Equal(want) {
        t.Errorf("Acq: got %v, want %v", got.AcquisitionDateTime, want)
    }
    if got.SourceLens != 20 {
        t.Errorf("SourceLens: got %v, want 20", got.SourceLens)
    }
    if got.FocalOffset != 2.5 {
        t.Errorf("FocalOffset: got %v mm, want 2.5", got.FocalOffset)
    }
    if got.Reference != "SN-1234" {
        t.Errorf("Reference: got %q, want SN-1234", got.Reference)
    }
}

func TestMetadataOfRejectsNonNDPITiler(t *testing.T) {
    _, ok := MetadataOf(&fakeTiler{})
    if ok {
        t.Fatal("MetadataOf: expected ok=false for non-NDPI Tiler")
    }
}

// fakeTiler is a non-NDPI Tiler used by TestMetadataOfRejectsNonNDPITiler.
type fakeTiler struct{}

func (f *fakeTiler) Format() interface{}                     { return nil }

// The following satisfies just enough of opentile.Tiler for the test.
// Concrete implementations live in opentile; for the purpose of
// MetadataOf we only exercise the type assertion, so we import opentile
// and build a minimal embedded doppelganger.

// The real test uses a Tiler built via ndpi.Factory.Open once that's
// implemented; this placeholder stays as a compile-time sanity check.
var _ = tiff.File{}
```

Note: the "fakeTiler" above is a partial stub and the test will be fleshed out once `ndpi.Open` returns a real Tiler (Task 20). For this task, focus the test on `parseFromFields`, which tests metadata parsing in isolation.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/ndpi/...`
Expected: FAIL — `undefined: parseFromFields, metadataFields, Metadata`.

- [ ] **Step 3: Implement `formats/ndpi/metadata.go`**

```go
package ndpi

import (
    "fmt"
    "time"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// Metadata is NDPI-specific slide metadata. Embeds opentile.Metadata for the
// cross-format fields (Magnification, scanner info, AcquisitionDateTime).
type Metadata struct {
    opentile.Metadata
    SourceLens  float64 // objective magnification from Hamamatsu SourceLens tag (65420)
    FocalDepth  float64 // mm, from FocalDepth tag (if present)
    FocalOffset float64 // mm, from ZOffsetFromSlideCenter tag (nanometers → mm)
    Reference   string  // scanner reference/serial
}

// NDPI vendor-private tag IDs.
const (
    tagSourceLens             uint16 = 65420
    tagZOffsetFromSlideCenter uint16 = 65427
    tagFocalDepth             uint16 = 65432
    tagReference              uint16 = 65442
)

// metadataFields is the deliberately un-marshaled shape consumed by
// parseFromFields. Tests construct it directly; the production path
// populates it from *tiff.Page via parseMetadata.
type metadataFields struct {
    SourceLens             uint32
    Model                  string
    DateTime               string
    XResolution            [2]uint32
    YResolution            [2]uint32
    ResolutionUnit         uint32
    ZOffsetFromSlideCenter uint32
    FocalDepth             uint32
    Reference              string
}

func parseMetadata(p *tiff.Page) (Metadata, error) {
    var f metadataFields
    if v, ok := p.ScalarU32(tagSourceLens); ok {
        f.SourceLens = v
    }
    // Model and DateTime are standard TIFF ASCII tags.
    f.Model, _ = p.ASCII(tagModel)
    f.DateTime, _ = p.ASCII(tagDateTime)
    if numer, denom, ok := p.XResolution(); ok {
        f.XResolution = [2]uint32{numer, denom}
    }
    if numer, denom, ok := p.YResolution(); ok {
        f.YResolution = [2]uint32{numer, denom}
    }
    if v, ok := p.ResolutionUnit(); ok {
        f.ResolutionUnit = v
    }
    if v, ok := p.ScalarU32(tagZOffsetFromSlideCenter); ok {
        f.ZOffsetFromSlideCenter = v
    }
    if v, ok := p.ScalarU32(tagFocalDepth); ok {
        f.FocalDepth = v
    }
    f.Reference, _ = p.ASCII(tagReference)
    return parseFromFields(f), nil
}

func parseFromFields(f metadataFields) Metadata {
    md := Metadata{
        SourceLens:  float64(f.SourceLens),
        FocalOffset: float64(f.ZOffsetFromSlideCenter) / 1_000_000.0, // nm → mm
        FocalDepth:  float64(f.FocalDepth) / 1_000_000.0,
        Reference:   f.Reference,
    }
    md.Magnification = float64(f.SourceLens)
    md.ScannerManufacturer = "Hamamatsu"
    md.ScannerModel = f.Model
    if f.Model != "" {
        md.ScannerSoftware = []string{f.Model}
    }
    if t, err := time.Parse("2006:01:02 15:04:05", f.DateTime); err == nil {
        md.AcquisitionDateTime = t
    }
    return md
}

// Standard TIFF tag IDs (values are in the TIFF 6.0 spec).
const (
    tagModel    uint16 = 272
    tagDateTime uint16 = 306
)

// MetadataOf returns the NDPI-specific metadata if t is an NDPI Tiler.
// Walks Tiler wrappers (mirrors svs.MetadataOf).
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
    const maxHops = 16
    for i := 0; t != nil && i <= maxHops; i++ {
        if nt, ok := t.(*tiler); ok {
            return &nt.md, true
        }
        u, ok := t.(interface{ UnwrapTiler() opentile.Tiler })
        if !ok {
            return nil, false
        }
        t = u.UnwrapTiler()
    }
    return nil, false
}

// tiler is the NDPI implementation of opentile.Tiler; fleshed out in Task 20.
// Declared here so MetadataOf compiles.
type tiler struct {
    md         Metadata
    levels     []opentile.Level
    associated []opentile.AssociatedImage
    icc        []byte
}

// Ensure unused import warnings don't fire if parseMetadata is called
// nowhere in this file once Task 20 wires it up.
var _ = fmt.Errorf
```

- [ ] **Step 4: Add ASCII accessor to `internal/tiff/page.go`**

NDPI parser needs `p.ASCII(tag) (string, bool)`. The existing `ImageDescription()` uses this pattern internally; generalize:

```go
// ASCII returns an ASCII-typed tag's string value (NUL-stripped), or
// ("", false) if missing.
func (p *Page) ASCII(tag uint16) (string, bool) {
    e, ok := p.ifd.get(tag)
    if !ok {
        return "", false
    }
    s, err := e.decodeASCII(p.br, e.valueBytes[:])
    if err != nil {
        return "", false
    }
    return s, true
}
```

Update `ImageDescription` to use `ASCII(TagImageDescription)` to avoid duplication.

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: PASS including the metadata parse test. `TestMetadataOfRejectsNonNDPITiler` compiles but the placeholder `fakeTiler` doesn't implement enough of the Tiler interface for the test to run — leave it as a compile-time sanity check or remove the test stub in this task.

- [ ] **Step 6: Commit**

```bash
git add formats/ndpi/metadata.go formats/ndpi/metadata_test.go internal/tiff/page.go
git commit -m "feat(ndpi): parse Hamamatsu metadata (SourceLens, DateTime, focal offsets); MetadataOf accessor"
```

---

## Task 16: `formats/ndpi/tilesize.go` — `AdjustTileSize`

**Files:**
- Create: `formats/ndpi/tilesize.go`
- Create: `formats/ndpi/tilesize_test.go`

- [ ] **Step 1: Write failing tests**

Create `formats/ndpi/tilesize_test.go`:

```go
package ndpi

import "testing"

func TestAdjustTileSize(t *testing.T) {
    tests := []struct {
        name        string
        requested   int
        stripe      int
        wantW       int
    }{
        {"equal_to_stripe", 640, 640, 640},
        {"smaller_than_stripe_ratio_close_to_1", 500, 640, 640},   // factor 640/500 = 1.28, log2≈0.36, round→0, factor_2=1
        {"smaller_than_stripe_needs_doubling", 256, 640, 1280},    // factor 2.5, round→1, factor_2=2 → 2*640
        {"larger_than_stripe_ratio_3", 2048, 640, 2560},           // factor 3.2, round→2, factor_2=4 → 4*640 = 2560
        {"larger_than_stripe_ratio_2", 1280, 640, 1280},           // factor 2, log2=1, factor_2=2
        {"no_stripe_pages", 1024, 0, 1024},                        // no striped pages → passthrough
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := AdjustTileSize(tc.requested, tc.stripe)
            if got.W != tc.wantW || got.H != tc.wantW {
                t.Errorf("AdjustTileSize(%d, %d): got %v, want {%d,%d}",
                    tc.requested, tc.stripe, got, tc.wantW, tc.wantW)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/ndpi/... -run TestAdjustTileSize`
Expected: FAIL — `undefined: AdjustTileSize`.

- [ ] **Step 3: Implement `formats/ndpi/tilesize.go`**

```go
package ndpi

import (
    "math"

    opentile "github.com/wsilabs/opentile-go"
)

// AdjustTileSize returns the output tile size to use for an NDPI tiler given
// the user's requested size and the smallest native stripe width in the file.
//
// Upstream opentile's algorithm: the adjusted size is a power-of-2 multiple
// of the smallest stripe width, where the exponent is
// round(log2(ratio(requested, stripe))). If there are no striped pages
// (stripeWidth == 0), the request passes through unchanged. The result is
// always square.
//
// Concretely this guarantees every output tile is an integer number of
// native stripes wide, so the stripe-concat code never needs to crop
// horizontally within a stripe — it just concatenates whole stripes.
func AdjustTileSize(requested, stripeWidth int) opentile.Size {
    if stripeWidth == 0 || requested == stripeWidth {
        return opentile.Size{W: requested, H: requested}
    }
    var factor float64
    if requested > stripeWidth {
        factor = float64(requested) / float64(stripeWidth)
    } else {
        factor = float64(stripeWidth) / float64(requested)
    }
    factor2 := math.Pow(2, math.Round(math.Log2(factor)))
    adjusted := int(factor2) * stripeWidth
    return opentile.Size{W: adjusted, H: adjusted}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./formats/ndpi/... -run TestAdjustTileSize`
Expected: PASS all six cases.

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/tilesize.go formats/ndpi/tilesize_test.go
git commit -m "feat(ndpi): AdjustTileSize snaps requested tile to power-of-2 × stripe width"
```

---

## Task 17: `formats/ndpi/striped.go` — `NdpiStripedImage`

**Files:**
- Create: `formats/ndpi/striped.go`
- Create: `formats/ndpi/striped_test.go`

The NDPI pyramid-level hot path: output tile = concatenation of (nx × ny) native stripes, assembled via `internal/jpeg.ConcatenateScans`.

- [ ] **Step 1: Create a smoke test that skips until Task 24 lands**

Because a synthetic in-memory JPEG-fragment-per-stripe fixture large enough to exercise `ConcatenateScans` end-to-end is itself nontrivial, this task ships a structural smoke test that t.Skip's until the real-slide fixture harness (Task 24) takes over byte-level acceptance. Create `formats/ndpi/striped_test.go`:

```go
package ndpi

import "testing"

func TestNdpiStripedSmoke(t *testing.T) {
    t.Skip("striped image acceptance comes from Task 24 fixture round-trip")
}
```

- [ ] **Step 2: Implement `formats/ndpi/striped.go`**

```go
package ndpi

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "iter"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/jpeg"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// stripedImage is an NDPI Level backed by a page of 8-pixel-tall horizontal
// stripes. Each output Tile is assembled from multiple native stripes via
// pure-Go JPEG marker concatenation.
type stripedImage struct {
    index       int
    size        opentile.Size // level pixel dims
    tileSize    opentile.Size // adjusted output tile size (square)
    grid        opentile.Size // output tile grid count

    // native stripe geometry
    stripeW, stripeH int

    // per-native-stripe arrays (ordered row-major over the stripe grid)
    nativeGrid    opentile.Size
    stripeOffsets []uint64
    stripeCounts  []uint64

    // output-tile → native-stripe mapping helpers
    nx, ny int // native stripes per output tile (horizontal, vertical)

    jpegTables []byte
    reader     io.ReaderAt

    compression opentile.Compression
    mpp         opentile.SizeMm
    pyrIndex    int
}

func newStripedImage(
    index int,
    p *tiff.Page,
    tileSize opentile.Size,
    r io.ReaderAt,
) (*stripedImage, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return nil, fmt.Errorf("ndpi: ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return nil, fmt.Errorf("ndpi: ImageLength missing")
    }
    stripeW, ok := p.TileWidth()
    if !ok {
        return nil, fmt.Errorf("ndpi: TileWidth missing (expected striped page)")
    }
    stripeH, ok := p.TileLength()
    if !ok {
        return nil, fmt.Errorf("ndpi: TileLength missing")
    }
    nativeGx, nativeGy, err := p.TileGrid()
    if err != nil {
        return nil, err
    }
    offsets, err := p.TileOffsets64()
    if err != nil {
        return nil, err
    }
    counts, err := p.TileByteCounts64()
    if err != nil {
        return nil, err
    }
    if tileSize.W%int(stripeW) != 0 || tileSize.H%int(stripeH) != 0 {
        return nil, fmt.Errorf("ndpi: adjusted tile size %v not aligned to stripe %dx%d", tileSize, stripeW, stripeH)
    }
    nx := tileSize.W / int(stripeW)
    ny := tileSize.H / int(stripeH)
    gridW := (int(iw) + tileSize.W - 1) / tileSize.W
    gridH := (int(il) + tileSize.H - 1) / tileSize.H
    tables, _ := p.JPEGTables()
    return &stripedImage{
        index:         index,
        size:          opentile.Size{W: int(iw), H: int(il)},
        tileSize:      tileSize,
        grid:          opentile.Size{W: gridW, H: gridH},
        stripeW:       int(stripeW),
        stripeH:       int(stripeH),
        nativeGrid:    opentile.Size{W: nativeGx, H: nativeGy},
        stripeOffsets: offsets,
        stripeCounts:  counts,
        nx:            nx,
        ny:            ny,
        jpegTables:    tables,
        reader:        r,
        compression:   opentile.CompressionJPEG, // NDPI is always JPEG per upstream
    }, nil
}

// --- opentile.Level interface ---

func (l *stripedImage) Index() int                           { return l.index }
func (l *stripedImage) PyramidIndex() int                    { return l.pyrIndex }
func (l *stripedImage) Size() opentile.Size                  { return l.size }
func (l *stripedImage) TileSize() opentile.Size              { return l.tileSize }
func (l *stripedImage) Grid() opentile.Size                  { return l.grid }
func (l *stripedImage) Compression() opentile.Compression    { return l.compression }
func (l *stripedImage) MPP() opentile.SizeMm                 { return l.mpp }
func (l *stripedImage) FocalPlane() float64                  { return 0 }

func (l *stripedImage) Tile(x, y int) ([]byte, error) {
    if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
    }
    fragments, err := l.readStripeFragments(x, y)
    if err != nil {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
    }
    out, err := jpeg.ConcatenateScans(fragments, jpeg.ConcatOpts{
        Width:           uint16(l.tileSize.W),
        Height:          uint16(l.tileSize.H),
        JPEGTables:      l.jpegTables,
        RestartInterval: l.restartIntervalPerStripe(),
    })
    if err != nil {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: fmt.Errorf("%w: %v", opentile.ErrBadJPEGBitstream, err)}
    }
    return out, nil
}

func (l *stripedImage) TileReader(x, y int) (io.ReadCloser, error) {
    b, err := l.Tile(x, y)
    if err != nil {
        return nil, err
    }
    return io.NopCloser(bytes.NewReader(b)), nil
}

func (l *stripedImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
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

// --- internal helpers ---

// readStripeFragments reads the native-stripe JPEG fragments that compose
// the output tile at (x, y). Order: top-to-bottom, left-to-right, matching
// the scan order ConcatenateScans expects.
func (l *stripedImage) readStripeFragments(x, y int) ([][]byte, error) {
    fragments := make([][]byte, 0, l.nx*l.ny)
    for dy := 0; dy < l.ny; dy++ {
        sy := y*l.ny + dy
        for dx := 0; dx < l.nx; dx++ {
            sx := x*l.nx + dx
            if sx >= l.nativeGrid.W || sy >= l.nativeGrid.H {
                continue // edge tile may have fewer native stripes
            }
            idx := sy*l.nativeGrid.W + sx
            off := int64(l.stripeOffsets[idx])
            length := int(l.stripeCounts[idx])
            buf := make([]byte, length)
            if _, err := l.reader.ReadAt(buf, off); err != nil {
                return nil, fmt.Errorf("read stripe (%d,%d) [idx=%d]: %w", sx, sy, idx, err)
            }
            fragments = append(fragments, buf)
        }
    }
    return fragments, nil
}

// restartIntervalPerStripe computes the MCU count in one native stripe,
// given we assume the typical NDPI YCbCr 4:2:0 subsampling (MCU=16x16) —
// in practice each fragment we read IS already one stripe's worth of scan
// data, so the restart interval equals the MCU count per stripe.
func (l *stripedImage) restartIntervalPerStripe() int {
    // NDPI uses 4:2:0 subsampling in practice; MCU = 16x16. Native stripes
    // are typically 8 pixels tall, meaning 0.5 MCU rows vertically — upstream
    // handles this via DRI = stripeW/mcuW. Use that formula (assuming 16x16
    // MCUs; refined once we observe real-slide metadata).
    const mcuW = 16
    return l.stripeW / mcuW
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./formats/ndpi/...`
Expected: PASS (TestAdjustTileSize + the skipped TestNdpiStripedTileBasic). No other tests fire until Task 20 wires Open.

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/striped.go formats/ndpi/striped_test.go
git commit -m "feat(ndpi): NdpiStripedImage assembles tiles from native stripes via internal/jpeg"
```

---

## Task 18: `formats/ndpi/oneframe.go` — `NdpiOneFrameImage` (cgo crop path)

**Files:**
- Create: `formats/ndpi/oneframe.go`
- Create: `formats/ndpi/oneframe_test.go`

- [ ] **Step 1: Write failing tests**

Create `formats/ndpi/oneframe_test.go`:

```go
package ndpi

import "testing"

func TestNdpiOneFrameImageSmoke(t *testing.T) {
    t.Skip("oneframe image smoke test relies on real-slide fixtures from Task 24")
}
```

- [ ] **Step 2: Implement `formats/ndpi/oneframe.go`**

```go
package ndpi

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "iter"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/jpeg"
    "github.com/wsilabs/opentile-go/internal/jpegturbo"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// oneFrameImage is an NDPI Level backed by a single JPEG per page (typical
// for lower pyramid levels that fit in one JPEG). Output tiles are produced
// by lossless MCU-aligned crop via libjpeg-turbo.
type oneFrameImage struct {
    index       int
    size        opentile.Size
    tileSize    opentile.Size
    grid        opentile.Size
    compression opentile.Compression
    mpp         opentile.SizeMm
    pyrIndex    int

    // cached, MCU-padded JPEG bytes for the entire level (built lazily on
    // first call; safe because level state is otherwise immutable and the
    // one-time build is idempotent).
    paddedJPEGOnce bool
    paddedJPEG     []byte
    mcuW, mcuH     int

    reader io.ReaderAt
    page   *tiff.Page
}

func newOneFrameImage(
    index int,
    p *tiff.Page,
    tileSize opentile.Size,
    r io.ReaderAt,
) (*oneFrameImage, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return nil, fmt.Errorf("ndpi: ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return nil, fmt.Errorf("ndpi: ImageLength missing")
    }
    gridW := (int(iw) + tileSize.W - 1) / tileSize.W
    gridH := (int(il) + tileSize.H - 1) / tileSize.H
    return &oneFrameImage{
        index:       index,
        size:        opentile.Size{W: int(iw), H: int(il)},
        tileSize:    tileSize,
        grid:        opentile.Size{W: gridW, H: gridH},
        compression: opentile.CompressionJPEG,
        reader:      r,
        page:        p,
    }, nil
}

// --- opentile.Level interface ---

func (l *oneFrameImage) Index() int                        { return l.index }
func (l *oneFrameImage) PyramidIndex() int                 { return l.pyrIndex }
func (l *oneFrameImage) Size() opentile.Size               { return l.size }
func (l *oneFrameImage) TileSize() opentile.Size           { return l.tileSize }
func (l *oneFrameImage) Grid() opentile.Size               { return l.grid }
func (l *oneFrameImage) Compression() opentile.Compression { return l.compression }
func (l *oneFrameImage) MPP() opentile.SizeMm              { return l.mpp }
func (l *oneFrameImage) FocalPlane() float64               { return 0 }

func (l *oneFrameImage) Tile(x, y int) ([]byte, error) {
    if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
    }
    padded, err := l.getPaddedJPEG()
    if err != nil {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
    }
    region := jpegturbo.Region{X: x * l.tileSize.W, Y: y * l.tileSize.H, Width: l.tileSize.W, Height: l.tileSize.H}
    out, err := jpegturbo.Crop(padded, region)
    if err != nil {
        return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
    }
    return out, nil
}

func (l *oneFrameImage) TileReader(x, y int) (io.ReadCloser, error) {
    b, err := l.Tile(x, y)
    if err != nil {
        return nil, err
    }
    return io.NopCloser(bytes.NewReader(b)), nil
}

func (l *oneFrameImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
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

// getPaddedJPEG reads the entire page's JPEG payload once and returns a
// slice where the SOF dimensions are rounded up to MCU boundaries (safe for
// tjTransform's TJXOPT_PERFECT). Called on first Tile; result is cached for
// the level's lifetime.
func (l *oneFrameImage) getPaddedJPEG() ([]byte, error) {
    if l.paddedJPEGOnce {
        return l.paddedJPEG, nil
    }
    offsets, err := l.page.TileOffsets64()
    if err != nil {
        // Single-frame pages might use StripOffsets instead of TileOffsets
        // depending on NDPI producer conventions; here we assume TileOffsets
        // since upstream opentile treats these the same way. If the page
        // lacks both, fail with a typed error surfaced upward.
        return nil, fmt.Errorf("one-frame page missing TileOffsets: %w", err)
    }
    counts, err := l.page.TileByteCounts64()
    if err != nil {
        return nil, fmt.Errorf("one-frame page missing TileByteCounts: %w", err)
    }
    if len(offsets) != 1 || len(counts) != 1 {
        return nil, fmt.Errorf("one-frame page expected 1 offset/count, got %d/%d", len(offsets), len(counts))
    }
    buf := make([]byte, counts[0])
    if _, err := l.reader.ReadAt(buf, int64(offsets[0])); err != nil {
        return nil, fmt.Errorf("read one-frame JPEG: %w", err)
    }
    // Determine MCU size from SOF inside buf.
    var sof *jpeg.SOF
    for seg, err := range jpeg.Scan(bytes.NewReader(buf)) {
        if err != nil {
            return nil, fmt.Errorf("%w: %v", opentile.ErrBadJPEGBitstream, err)
        }
        if seg.Marker == jpeg.SOF0 {
            sof, err = jpeg.ParseSOF(seg.Payload)
            if err != nil {
                return nil, fmt.Errorf("%w: %v", opentile.ErrBadJPEGBitstream, err)
            }
            break
        }
    }
    if sof == nil {
        return nil, fmt.Errorf("%w: SOF not found in one-frame page", opentile.ErrBadJPEGBitstream)
    }
    mcuW, mcuH := sof.MCUSize()
    l.mcuW, l.mcuH = mcuW, mcuH
    paddedW := roundUp(l.size.W, mcuW)
    paddedH := roundUp(l.size.H, mcuH)
    if paddedW == l.size.W && paddedH == l.size.H {
        // Already MCU-aligned — no rewrite needed.
        l.paddedJPEG = buf
    } else {
        rewrote, err := jpeg.ReplaceSOFDimensions(buf, uint16(paddedW), uint16(paddedH))
        if err != nil {
            return nil, fmt.Errorf("pad SOF: %w", err)
        }
        l.paddedJPEG = rewrote
    }
    l.paddedJPEGOnce = true
    return l.paddedJPEG, nil
}

func roundUp(n, to int) int {
    if n%to == 0 {
        return n
    }
    return n + (to - n%to)
}
```

Note: `paddedJPEGOnce` is a simple non-atomic bool — safe because we populate it on first call and the caller is expected to serialize the first tile read (or tolerate a small double-work race on concurrent first-calls). For v0.2 the contract explicitly permits this benign race; it does not violate immutability of the caller-visible result.

- [ ] **Step 3: Run tests**

Run: `go test ./formats/ndpi/...`
Expected: PASS (the oneframe test is skipped).

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/oneframe.go formats/ndpi/oneframe_test.go
git commit -m "feat(ndpi): NdpiOneFrameImage crops tiles via internal/jpegturbo.Crop"
```

---

## Task 19: `formats/ndpi/associated.go` — `NdpiLabel`, `NdpiOverview`

**Files:**
- Create: `formats/ndpi/associated.go`
- Create: `formats/ndpi/associated_test.go`

- [ ] **Step 1: Write failing tests (smoke-only)**

Create `formats/ndpi/associated_test.go`:

```go
package ndpi

import "testing"

func TestAssociatedImagesSmoke(t *testing.T) {
    t.Skip("associated-image smoke tests rely on real-slide fixtures from Task 24")
}
```

- [ ] **Step 2: Implement `formats/ndpi/associated.go`**

```go
package ndpi

import (
    "fmt"
    "io"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/jpegturbo"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// overviewImage is an NDPI "Macro" page exposed as an AssociatedImage
// with Kind() == "overview". Its Bytes() passes through the raw JPEG
// payload without modification (no cgo).
type overviewImage struct {
    size        opentile.Size
    compression opentile.Compression
    offset      uint64
    length      uint64
    reader      io.ReaderAt
}

func newOverviewImage(p *tiff.Page, r io.ReaderAt) (*overviewImage, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return nil, fmt.Errorf("ndpi: overview ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return nil, fmt.Errorf("ndpi: overview ImageLength missing")
    }
    offsets, err := p.TileOffsets64()
    if err != nil {
        return nil, fmt.Errorf("ndpi: overview offsets: %w", err)
    }
    counts, err := p.TileByteCounts64()
    if err != nil {
        return nil, fmt.Errorf("ndpi: overview counts: %w", err)
    }
    if len(offsets) != 1 || len(counts) != 1 {
        return nil, fmt.Errorf("ndpi: overview expected 1 tile, got %d", len(offsets))
    }
    return &overviewImage{
        size:        opentile.Size{W: int(iw), H: int(il)},
        compression: opentile.CompressionJPEG,
        offset:      offsets[0],
        length:      counts[0],
        reader:      r,
    }, nil
}

func (o *overviewImage) Kind() string                      { return "overview" }
func (o *overviewImage) Size() opentile.Size               { return o.size }
func (o *overviewImage) Compression() opentile.Compression { return o.compression }

func (o *overviewImage) Bytes() ([]byte, error) {
    buf := make([]byte, o.length)
    if _, err := o.reader.ReadAt(buf, int64(o.offset)); err != nil {
        return nil, fmt.Errorf("ndpi: read overview: %w", err)
    }
    return buf, nil
}

// labelImage is the cropped left portion of the macro image, exposed
// with Kind() == "label". Upstream default crop is 0.0 → 0.3 of macro width.
type labelImage struct {
    overview  *overviewImage
    cropFrom  int // left pixel offset in source (MCU-aligned)
    cropTo    int // right pixel offset in source (exclusive)
    cropH     int
}

func newLabelImage(overview *overviewImage, crop float64, mcuW, mcuH int) *labelImage {
    // Snap crop boundaries down to the nearest MCU.
    pixelTo := int(float64(overview.size.W) * crop)
    pixelTo = (pixelTo / mcuW) * mcuW
    if pixelTo <= 0 {
        pixelTo = mcuW
    }
    return &labelImage{
        overview: overview,
        cropFrom: 0,
        cropTo:   pixelTo,
        cropH:    (overview.size.H / mcuH) * mcuH,
    }
}

func (l *labelImage) Kind() string                      { return "label" }
func (l *labelImage) Size() opentile.Size               { return opentile.Size{W: l.cropTo - l.cropFrom, H: l.cropH} }
func (l *labelImage) Compression() opentile.Compression { return l.overview.compression }

func (l *labelImage) Bytes() ([]byte, error) {
    src, err := l.overview.Bytes()
    if err != nil {
        return nil, err
    }
    return jpegturbo.Crop(src, jpegturbo.Region{
        X: l.cropFrom, Y: 0, Width: l.cropTo - l.cropFrom, Height: l.cropH,
    })
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./formats/ndpi/...`
Expected: PASS (smoke test is skipped; new files compile).

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/associated.go formats/ndpi/associated_test.go
git commit -m "feat(ndpi): overview (passthrough) and label (cgo crop) associated images"
```

---

## Task 20: `formats/ndpi/ndpi.go` — wire up `Open`

**Files:**
- Modify: `formats/ndpi/ndpi.go`
- Create: `formats/ndpi/tiler.go`
- Modify: `formats/ndpi/ndpi_test.go`

Replaces the stub `Open` with the real constructor that classifies pages into level + associated, builds the stripe/one-frame Levels, and returns the tiler.

- [ ] **Step 1: Write failing test**

Append to `formats/ndpi/ndpi_test.go`:

```go
func TestNdpiOpenClassifiesPages(t *testing.T) {
    // buildNDPIStub has a single tiled page; Open should surface 1 level,
    // no associated images, and a Metadata struct with SourceLens = 20.
    data := buildNDPIStub(t)
    f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        t.Fatalf("tiff.Open: %v", err)
    }
    cfg := opentile.NewTestConfig(opentile.Size{W: 640, H: 640}, opentile.CorruptTileError)
    tiler, err := New().Open(f, cfg)
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    defer tiler.Close()
    if tiler.Format() != opentile.FormatNDPI {
        t.Errorf("Format: got %q, want %q", tiler.Format(), opentile.FormatNDPI)
    }
    if got := len(tiler.Levels()); got != 1 {
        t.Errorf("levels: got %d, want 1", got)
    }
    if got := len(tiler.Associated()); got != 0 {
        t.Errorf("associated: got %d, want 0", got)
    }
    md, ok := MetadataOf(tiler)
    if !ok {
        t.Fatal("MetadataOf returned ok=false")
    }
    if md.SourceLens != 20 {
        t.Errorf("SourceLens: got %v, want 20", md.SourceLens)
    }
}
```

The import list must be extended:

```go
import (
    "bytes"
    "testing"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./formats/ndpi/... -run TestNdpiOpenClassifiesPages`
Expected: FAIL — `"ndpi.Open: not yet implemented"`.

- [ ] **Step 3: Implement the tiler in `formats/ndpi/tiler.go`**

```go
package ndpi

import (
    opentile "github.com/wsilabs/opentile-go"
)

// tiler satisfies opentile.Tiler. Declared in metadata.go; the method set
// lives here to keep the file focused.

func (t *tiler) Format() opentile.Format                { return opentile.FormatNDPI }
func (t *tiler) Levels() []opentile.Level               { out := make([]opentile.Level, len(t.levels)); copy(out, t.levels); return out }
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }
func (t *tiler) Level(i int) (opentile.Level, error) {
    if i < 0 || i >= len(t.levels) {
        return nil, opentile.ErrLevelOutOfRange
    }
    return t.levels[i], nil
}
```

- [ ] **Step 4: Replace the `Open` stub in `formats/ndpi/ndpi.go`**

```go
// Open constructs an NDPI Tiler from a parsed TIFF file.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
    pages := file.Pages()
    if len(pages) == 0 {
        return nil, fmt.Errorf("ndpi: file has no pages")
    }
    md, err := parseMetadata(pages[0])
    if err != nil {
        return nil, err
    }

    // Determine the requested tile size and snap to stripe width.
    reqSize := opentile.Size{W: 512, H: 512}
    if sz, set := cfg.TileSize(); set {
        if sz.W != sz.H {
            return nil, fmt.Errorf("ndpi: tile size must be square, got %v", sz)
        }
        reqSize = sz
    }
    smallestStripe := smallestStripeWidth(pages)
    adjusted := AdjustTileSize(reqSize.W, smallestStripe)

    var levels []opentile.Level
    var associated []opentile.AssociatedImage
    var overview *overviewImage
    levelIdx := 0
    for _, p := range pages {
        kind := classifyPage(p)
        switch kind {
        case pageStripedLevel:
            lvl, err := newStripedImage(levelIdx, p, adjusted, file.ReaderAt())
            if err != nil {
                return nil, fmt.Errorf("ndpi: level %d: %w", levelIdx, err)
            }
            levels = append(levels, lvl)
            levelIdx++
        case pageOneFrameLevel:
            lvl, err := newOneFrameImage(levelIdx, p, adjusted, file.ReaderAt())
            if err != nil {
                return nil, fmt.Errorf("ndpi: level %d: %w", levelIdx, err)
            }
            levels = append(levels, lvl)
            levelIdx++
        case pageMacro:
            ov, err := newOverviewImage(p, file.ReaderAt())
            if err != nil {
                return nil, fmt.Errorf("ndpi: overview: %w", err)
            }
            overview = ov
            associated = append(associated, ov)
        }
    }
    if overview != nil {
        // Default label crop is 0 → 30% of macro width. MCU sizes default to
        // 16x16; refined when the label is actually read.
        associated = append(associated, newLabelImage(overview, 0.3, 16, 16))
    }
    return &tiler{md: md, levels: levels, associated: associated, icc: nil}, nil
}

// pageKind classifies an NDPI TIFF page.
type pageKind int

const (
    pageSkip pageKind = iota
    pageStripedLevel
    pageOneFrameLevel
    pageMacro
)

func classifyPage(p *tiff.Page) pageKind {
    // Macro pages carry a "Macro" ImageDescription (upstream convention).
    if desc, ok := p.ImageDescription(); ok && desc == "Macro" {
        return pageMacro
    }
    // Tiled pages → striped level.
    if _, ok := p.TileWidth(); ok {
        return pageStripedLevel
    }
    // Otherwise treat as one-frame level.
    return pageOneFrameLevel
}

// smallestStripeWidth walks all tiled pages and returns the smallest
// TileWidth, or 0 if no pages are tiled.
func smallestStripeWidth(pages []*tiff.Page) int {
    smallest := 0
    for _, p := range pages {
        tw, ok := p.TileWidth()
        if !ok {
            continue
        }
        if smallest == 0 || int(tw) < smallest {
            smallest = int(tw)
        }
    }
    return smallest
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: PASS for `TestNdpiOpenClassifiesPages`; all previous tests still green.

- [ ] **Step 6: Commit**

```bash
git add formats/ndpi/ndpi.go formats/ndpi/tiler.go formats/ndpi/ndpi_test.go
git commit -m "feat(ndpi): wire Open to classify pages, build Levels, expose MetadataOf"
```

---

## Batch 5 — SVS associated images

---

## Task 21: `formats/svs/associated.go` — striped label / overview / thumbnail

**Files:**
- Create: `formats/svs/associated.go`
- Create: `formats/svs/associated_test.go`
- Modify: `formats/svs/svs.go`

Page classification in `formats/svs/svs.go` currently skips non-tiled pages (the Task 16 v0.1 fix). v0.2 splits the skip into "associated-image striped page" (label/overview/thumbnail) and everything-else (still skip).

- [ ] **Step 1: Write failing test**

Create `formats/svs/associated_test.go`:

```go
package svs

import "testing"

func TestSvsAssociatedSmoke(t *testing.T) {
    t.Skip("associated-image parity comes from Task 24 fixture regeneration")
}
```

(Parity with real SVS slides is the ultimate acceptance bar; the regenerated fixtures in Task 24 catch regressions.)

- [ ] **Step 2: Implement `formats/svs/associated.go`**

```go
package svs

import (
    "fmt"
    "io"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/jpeg"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// stripedAssociated is an SVS AssociatedImage for thumbnail / label / overview
// pages, which Aperio stores as JPEG-striped pages alongside the tiled pyramid.
// Bytes() assembles a valid JPEG from the strips via internal/jpeg.
type stripedAssociated struct {
    kind        string
    size        opentile.Size
    compression opentile.Compression

    stripOffsets   []uint64
    stripCounts    []uint64
    jpegTables     []byte
    colorspaceFix  bool

    reader io.ReaderAt
}

func newStripedAssociated(kind string, p *tiff.Page, r io.ReaderAt) (*stripedAssociated, error) {
    iw, ok := p.ImageWidth()
    if !ok {
        return nil, fmt.Errorf("svs: associated ImageWidth missing")
    }
    il, ok := p.ImageLength()
    if !ok {
        return nil, fmt.Errorf("svs: associated ImageLength missing")
    }
    // Associated images use StripOffsets/StripByteCounts (TIFF strip model).
    strips, err := p.ScalarArrayU64(tiff.TagStripOffsets)
    if err != nil {
        return nil, fmt.Errorf("svs: associated strip offsets: %w", err)
    }
    counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
    if err != nil {
        return nil, fmt.Errorf("svs: associated strip counts: %w", err)
    }
    tables, _ := p.JPEGTables()
    return &stripedAssociated{
        kind:          kind,
        size:          opentile.Size{W: int(iw), H: int(il)},
        compression:   opentile.CompressionJPEG, // always JPEG for SVS associated
        stripOffsets:  strips,
        stripCounts:   counts,
        jpegTables:    tables,
        colorspaceFix: true, // Aperio non-standard RGB JPEG; APP14 needed for downstream decoders
        reader:        r,
    }, nil
}

func (a *stripedAssociated) Kind() string                      { return a.kind }
func (a *stripedAssociated) Size() opentile.Size               { return a.size }
func (a *stripedAssociated) Compression() opentile.Compression { return a.compression }

func (a *stripedAssociated) Bytes() ([]byte, error) {
    fragments := make([][]byte, len(a.stripOffsets))
    for i := range a.stripOffsets {
        buf := make([]byte, a.stripCounts[i])
        if _, err := a.reader.ReadAt(buf, int64(a.stripOffsets[i])); err != nil {
            return nil, fmt.Errorf("svs: read associated strip %d: %w", i, err)
        }
        fragments[i] = buf
    }
    return jpeg.ConcatenateScans(fragments, jpeg.ConcatOpts{
        Width:         uint16(a.size.W),
        Height:        uint16(a.size.H),
        JPEGTables:    a.jpegTables,
        ColorspaceFix: a.colorspaceFix,
    })
}
```

- [ ] **Step 3: Add `StripOffsets`/`StripByteCounts` tag constants and `ScalarArrayU64` to `internal/tiff/page.go`**

```go
// Additional tag IDs used by SVS associated images (TIFF strip model).
const (
    TagStripOffsets    uint16 = 273
    TagStripByteCounts uint16 = 279
    TagRowsPerStrip    uint16 = 278
)

// ScalarArrayU64 returns the value array for an arbitrary tag as uint64s.
// Generalizes TileOffsets64/TileByteCounts64 for callers that need other
// array-valued tags (e.g., SVS StripOffsets).
func (p *Page) ScalarArrayU64(tag uint16) ([]uint64, error) {
    return p.arrayU64(tag)
}
```

- [ ] **Step 4: Wire SvsTiler.Open to attach associated images**

In `formats/svs/svs.go`, modify the `Open` method's non-tiled-page skip to classify and append. Locate this block:

```go
    levelIdx := 0
    for pageIdx, p := range pages {
        if _, ok := p.TileWidth(); !ok {
            continue // non-tiled page; defer to v0.3 associated-image support
        }
        lvl, err := newTiledImage(levelIdx, p, baseSize, md.MPP, file.ReaderAt(), cfg)
        ...
```

Replace with:

```go
    levelIdx := 0
    var associated []opentile.AssociatedImage
    for pageIdx, p := range pages {
        if _, ok := p.TileWidth(); !ok {
            // Non-tiled page — classify as associated (label/overview/thumbnail)
            // if the ImageDescription identifies it; otherwise skip.
            desc, _ := p.ImageDescription()
            kind := classifyAssociated(desc)
            if kind == "" {
                continue
            }
            a, err := newStripedAssociated(kind, p, file.ReaderAt())
            if err != nil {
                return nil, fmt.Errorf("svs: associated %s: %w", kind, err)
            }
            associated = append(associated, a)
            continue
        }
        lvl, err := newTiledImage(levelIdx, p, baseSize, md.MPP, file.ReaderAt(), cfg)
        if err != nil {
            return nil, fmt.Errorf("svs: page %d (level %d): %w", pageIdx, levelIdx, err)
        }
        levels = append(levels, lvl)
        levelIdx++
    }
```

Update the `tiler` construction at the bottom to attach `associated`:

```go
    icc, _ := basePage.ICCProfile()
    return &tiler{md: md, levels: levels, associated: associated, icc: icc}, nil
```

And add the field to `tiler`:

```go
type tiler struct {
    md         Metadata
    levels     []opentile.Level
    associated []opentile.AssociatedImage  // NEW
    icc        []byte
}

func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }  // CHANGED: returns t.associated not nil
```

Add the `classifyAssociated` helper near the top of `svs.go`:

```go
// classifyAssociated returns the AssociatedImage kind for an SVS non-tiled
// page based on its ImageDescription. Aperio embeds "label", "macro", or
// leaves the description as the main "Aperio Image Library..." banner for
// the thumbnail. Unknown → empty string (page is skipped).
func classifyAssociated(desc string) string {
    switch {
    case desc == "label":
        return "label"
    case desc == "macro":
        return "overview"
    case strings.HasPrefix(desc, aperioPrefix):
        // The thumbnail page carries the same ImageDescription as page 0.
        // Treat it as "thumbnail".
        return "thumbnail"
    }
    return ""
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: PASS; existing SVS integration tests continue to pass because the `Associated()` fields, while populated now, match existing fixtures if no associated-image hashes were previously recorded. (Task 24 regenerates fixtures to include them.)

- [ ] **Step 6: Commit**

```bash
git add formats/svs/associated.go formats/svs/associated_test.go formats/svs/svs.go internal/tiff/page.go
git commit -m "feat(svs): expose striped label/overview/thumbnail as AssociatedImage"
```

---

## Batch 6 — umbrella registration + fixture regeneration

---

## Task 22: Register NDPI in `formats/all`

**Files:**
- Modify: `formats/all/all.go`

- [ ] **Step 1: Edit `formats/all/all.go`**

Current body:
```go
func Register() {
    once.Do(func() {
        opentile.Register(svs.New())
    })
}
```

Add an NDPI registration line:
```go
func Register() {
    once.Do(func() {
        opentile.Register(svs.New())
        opentile.Register(ndpi.New())
    })
}
```

Add the import:
```go
import (
    "sync"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/formats/ndpi"
    "github.com/wsilabs/opentile-go/formats/svs"
)
```

- [ ] **Step 2: Run tests**

Run: `go test ./...`
Expected: all pass. The `formats/all` package's idempotent-Register test still passes (both factories register exactly once).

- [ ] **Step 3: Commit**

```bash
git add formats/all/all.go
git commit -m "feat(formats): register ndpi in the all umbrella"
```

---

## Task 23: Regenerate SVS fixtures (add associated-image hashes)

**Files:**
- Modify: `tests/fixtures.go`
- Modify: `tests/generate_test.go`
- Modify: `tests/integration_test.go`
- Regenerate: `tests/fixtures/CMU-1-Small-Region.json`, `CMU-1.json`, `JP2K-33003-1.json`

- [ ] **Step 1: Extend `tests/fixtures.go` to include associated-image fields**

Add `AssociatedImages` to the fixture schema:

```go
// AssociatedFixture is the per-associated-image portion of a fixture.
type AssociatedFixture struct {
    Kind        string `json:"kind"`
    Size        [2]int `json:"size"`
    Compression string `json:"compression"`
    SHA256      string `json:"sha256"`
}
```

And add a field to `Fixture`:

```go
type Fixture struct {
    Slide            string              `json:"slide"`
    Format           string              `json:"format"`
    Levels           []LevelFixture      `json:"levels"`
    Metadata         MetadataFixture     `json:"metadata"`
    TileSHA256       map[string]string   `json:"tiles"`
    ICCProfileSHA256 string              `json:"icc_profile_sha256,omitempty"`
    AssociatedImages []AssociatedFixture `json:"associated,omitempty"`  // NEW
}
```

- [ ] **Step 2: Extend `tests/generate_test.go` to populate `AssociatedImages`**

After the per-level loop, add:

```go
    for _, a := range tiler.Associated() {
        b, err := a.Bytes()
        if err != nil {
            return fmt.Errorf("Associated(%s).Bytes: %w", a.Kind(), err)
        }
        sum := sha256.Sum256(b)
        f.AssociatedImages = append(f.AssociatedImages, tests.AssociatedFixture{
            Kind:        a.Kind(),
            Size:        [2]int{a.Size().W, a.Size().H},
            Compression: a.Compression().String(),
            SHA256:      hex.EncodeToString(sum[:]),
        })
    }
```

- [ ] **Step 3: Extend `tests/integration_test.go` to verify associated hashes**

Inside the slide loop, after the level+tile checks:

```go
    if len(tiler.Associated()) != len(fix.AssociatedImages) {
        t.Errorf("associated count: got %d, want %d", len(tiler.Associated()), len(fix.AssociatedImages))
    } else {
        for i, a := range tiler.Associated() {
            exp := fix.AssociatedImages[i]
            if a.Kind() != exp.Kind {
                t.Errorf("associated[%d] kind: got %q, want %q", i, a.Kind(), exp.Kind)
            }
            b, err := a.Bytes()
            if err != nil {
                t.Errorf("associated[%d] Bytes: %v", i, err)
                continue
            }
            sum := sha256.Sum256(b)
            if got := hex.EncodeToString(sum[:]); got != exp.SHA256 {
                t.Errorf("associated[%d] sha256: got %s, want %s", i, got, exp.SHA256)
            }
        }
    }
```

Also rename `TestSVSParity` to `TestSlideParity` for honesty — it covers SVS and NDPI now. Adjust the `slideCandidates` list to include NDPI files:

```go
var slideCandidates = []string{
    "CMU-1-Small-Region.svs",
    "CMU-1.svs",
    "JP2K-33003-1.svs",
    "CMU-1.ndpi",
    "OS-2.ndpi",
}
```

Note: the test iterates every candidate; slides without an on-disk file or a committed fixture are t.Skip'd per the v0.1 design.

- [ ] **Step 4: Regenerate the three SVS fixtures**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR="$PWD/sample_files/svs" \
    go test ./tests -tags generate -run TestGenerateFixtures -generate -v -timeout 15m
```

Expected: three PASS sub-tests; each emits `wrote fixtures/<slide>.json`. The three files change only in adding an `associated` top-level key.

- [ ] **Step 5: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files/svs" go test ./tests -run TestSlideParity -v -timeout 15m
```

Expected: three PASS sub-tests. Associated hashes match the just-regenerated fixtures.

- [ ] **Step 6: Commit**

```bash
git add tests/fixtures.go tests/generate_test.go tests/integration_test.go tests/fixtures/CMU-1-Small-Region.json tests/fixtures/CMU-1.json tests/fixtures/JP2K-33003-1.json
git commit -m "test(svs): add associated-image fixture hashes

Regenerates all three SVS fixtures to include label/overview/thumbnail
hashes now that Tiler.Associated() is populated. Renames
TestSVSParity to TestSlideParity ahead of adding NDPI slides to the
candidate list."
```

---

## Task 24: Generate NDPI fixtures

**Files:**
- Create: `tests/fixtures/CMU-1.ndpi.json`
- Create: `tests/fixtures/OS-2.ndpi.json` (committed if under ~5 MB; otherwise skipped)

- [ ] **Step 1: Generate**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR="$PWD/sample_files" \
    go test ./tests -tags generate -run TestGenerateFixtures -generate -v -timeout 30m
```

Expected: the generator iterates `slideCandidates` and emits fixtures for every present slide. If CMU-1.ndpi and OS-2.ndpi are in `sample_files/ndpi/`, the harness produces fixtures for them.

The integration path points `OPENTILE_TESTDIR` at `sample_files` (parent) so both `svs/` and `ndpi/` subdirectories are walked.

Update `tests/integration_test.go` slide-resolution to handle the subdirectory layout:

```go
func resolveSlide(dir, name string) (string, bool) {
    // Look for the slide in dir itself, then in dir/svs, dir/ndpi.
    for _, sub := range []string{"", "svs", "ndpi"} {
        p := filepath.Join(dir, sub, name)
        if _, err := os.Stat(p); err == nil {
            return p, true
        }
    }
    return "", false
}
```

Use it in the test loop instead of `filepath.Join(dir, name)`.

- [ ] **Step 2: Inspect fixture sizes**

```bash
du -h tests/fixtures/*.ndpi.json
```

If any fixture exceeds ~5 MB, note it in `docs/deferred.md` under "Known v0.1 limitations" and optionally move it out of the committed tree (keep the slide generatable; CI re-hydrates on demand).

- [ ] **Step 3: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 30m
```

Expected: SVS sub-tests + NDPI sub-tests all PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/fixtures/CMU-1.ndpi.json tests/fixtures/OS-2.ndpi.json tests/integration_test.go
git commit -m "test(ndpi): commit CMU-1.ndpi and OS-2.ndpi parity fixtures

Integration test now iterates both svs/ and ndpi/ subdirectories of
OPENTILE_TESTDIR to locate candidate slides. Each fixture records
level geometry, NDPI metadata, per-tile sha256, and associated-image
hashes (overview + label when present)."
```

(If the pending 6.5 GB NDPI download arrived during development, also add its fixture in this commit or a follow-up.)

---

## Batch 7 — Python parity oracle

---

## Task 25: `tests/oracle/` scaffolding

**Files:**
- Create: `tests/oracle/oracle.go`
- Create: `tests/oracle/oracle_runner.py`
- Create: `tests/oracle/requirements.txt`

- [ ] **Step 1: Create `tests/oracle/requirements.txt`**

```
opentile==0.22.0
PyTurboJPEG>=1.7.5
imagecodecs>=2025.3.30
tifffile>=2025.3.13
defusedxml>=0.7.1
```

(Pin to the latest stable opentile at the time of implementation. The pin exists so parity comparisons are reproducible across machines.)

- [ ] **Step 2: Create `tests/oracle/oracle_runner.py`**

```python
#!/usr/bin/env python3
"""Emit a single tile from a slide using Python opentile, writing its bytes
to stdout. Used by the Go parity harness; never imported as a library.

Usage:
    oracle_runner.py <slide_path> <level> <x> <y>

Notes:
- Loads opentile with default configuration (no turbo path override).
- Writes raw tile bytes to stdout (binary). Callers should set PYTHONIOENCODING
  and read from stdout as bytes.
- Returns exit code 0 on success; nonzero on any error with the traceback on
  stderr.
"""
import sys

def main() -> int:
    if len(sys.argv) != 5:
        print("usage: oracle_runner.py <slide> <level> <x> <y>", file=sys.stderr)
        return 2
    slide, level_str, x_str, y_str = sys.argv[1:]
    try:
        level = int(level_str)
        x = int(x_str)
        y = int(y_str)
    except ValueError as e:
        print(f"bad arg: {e}", file=sys.stderr)
        return 2
    from opentile import OpenTile
    # Default tile_size=1024 for NDPI, ignored for SVS (passthrough). For
    # parity tests to work, the Go side must call Open with the same tile
    # size. The Go harness passes this via -tile-size.
    tile_size = int(sys.environ.get("OPENTILE_TILE_SIZE", "1024"))
    with OpenTile.open(slide, (tile_size, tile_size)) as tiler:
        lvl = tiler.get_level(level)
        data = lvl.get_tile((x, y))
    sys.stdout.buffer.write(data)
    return 0

if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 3: Create `tests/oracle/oracle.go`**

```go
// Package oracle provides a Go harness around the Python opentile CLI runner
// for byte-for-byte parity testing. Compile only under the `parity` build
// tag so default builds remain free of the Python dependency.
package oracle

import (
    "bytes"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
)

// RunnerScript returns the path to oracle_runner.py, resolving relative to
// this package's source file. If unset, assumes tests/oracle/oracle_runner.py
// rooted at the module's tests directory.
func RunnerScript() string {
    if p := os.Getenv("OPENTILE_ORACLE_RUNNER"); p != "" {
        return p
    }
    // Default: co-located with this source file.
    return filepath.Join("tests", "oracle", "oracle_runner.py")
}

// Tile invokes the Python oracle for one tile and returns the bytes.
func Tile(slide string, level, x, y, tileSize int) ([]byte, error) {
    cmd := exec.Command("python3", RunnerScript(), slide, fmt.Sprint(level), fmt.Sprint(x), fmt.Sprint(y))
    cmd.Env = append(os.Environ(), fmt.Sprintf("OPENTILE_TILE_SIZE=%d", tileSize))
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("python oracle failed: %w\nstderr: %s", err, stderr.String())
    }
    return stdout.Bytes(), nil
}
```

- [ ] **Step 4: Verify compile under parity tag**

Run:
```bash
go build -tags parity ./tests/oracle/...
```

Expected: build succeeds. Without the tag, the directory is still present but `oracle.go` should be guarded. Add at the top of oracle.go:

```go
//go:build parity
```

Re-run the build with and without tag:
```bash
go build ./...         # default — oracle.go not compiled, no error
go build -tags parity ./tests/oracle/...  # oracle.go compiled
```

- [ ] **Step 5: Commit**

```bash
git add tests/oracle/oracle.go tests/oracle/oracle_runner.py tests/oracle/requirements.txt
git commit -m "feat(oracle): scaffold Python opentile subprocess harness under -tags parity"
```

---

## Task 26: `tests/oracle/parity_test.go`

**Files:**
- Create: `tests/oracle/parity_test.go`

- [ ] **Step 1: Implement the parity test**

```go
//go:build parity

package oracle_test

import (
    "bytes"
    "flag"
    "os"
    "path/filepath"
    "testing"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
    "github.com/wsilabs/opentile-go/tests"
    "github.com/wsilabs/opentile-go/tests/oracle"
)

var fullParity = flag.Bool("parity-full", false, "walk every tile (slow) instead of sampling")

var slideCandidates = []string{
    "CMU-1-Small-Region.svs",
    "CMU-1.svs",
    "JP2K-33003-1.svs",
    "CMU-1.ndpi",
    "OS-2.ndpi",
}

const tileSize = 1024

// TestParityAgainstPython walks each candidate slide, invokes the Python
// oracle for a sample of tiles (or all tiles with -parity-full), and
// byte-compares against our Level.Tile output.
func TestParityAgainstPython(t *testing.T) {
    dir := tests.TestdataDir()
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set; skipping parity test")
    }
    for _, name := range slideCandidates {
        t.Run(name, func(t *testing.T) {
            slide, ok := resolveSlide(dir, name)
            if !ok {
                t.Skipf("slide %s not present", name)
            }
            runParityOnSlide(t, slide)
        })
    }
}

func runParityOnSlide(t *testing.T, slide string) {
    tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(tileSize, tileSize))
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    defer tiler.Close()
    for li, lvl := range tiler.Levels() {
        positions := samplePositions(lvl.Grid(), *fullParity)
        for _, pos := range positions {
            our, err := lvl.Tile(pos.X, pos.Y)
            if err != nil {
                t.Errorf("Tile(%d, %d) level %d: %v", pos.X, pos.Y, li, err)
                continue
            }
            theirs, err := oracle.Tile(slide, li, pos.X, pos.Y, tileSize)
            if err != nil {
                t.Errorf("oracle.Tile: %v", err)
                continue
            }
            if !bytes.Equal(our, theirs) {
                t.Errorf("slide %s level %d tile (%d,%d): byte-level divergence (%d vs %d bytes)",
                    slide, li, pos.X, pos.Y, len(our), len(theirs))
            }
        }
    }
}

// samplePositions returns either every (x, y) in the grid (full) or a
// sample of up to 10 positions spread across corners and the interior.
func samplePositions(grid opentile.Size, full bool) []opentile.TilePos {
    if full {
        out := make([]opentile.TilePos, 0, grid.W*grid.H)
        for y := 0; y < grid.H; y++ {
            for x := 0; x < grid.W; x++ {
                out = append(out, opentile.TilePos{X: x, Y: y})
            }
        }
        return out
    }
    // Sample corners + interior quartiles; clamp to grid bounds.
    cand := []opentile.TilePos{
        {X: 0, Y: 0},
        {X: grid.W - 1, Y: 0},
        {X: 0, Y: grid.H - 1},
        {X: grid.W - 1, Y: grid.H - 1},
        {X: grid.W / 4, Y: grid.H / 4},
        {X: grid.W / 2, Y: grid.H / 2},
        {X: 3 * grid.W / 4, Y: 3 * grid.H / 4},
        {X: 1, Y: grid.H / 2},
        {X: grid.W / 2, Y: 1},
        {X: grid.W - 2, Y: grid.H - 2},
    }
    seen := make(map[opentile.TilePos]bool)
    out := cand[:0]
    for _, p := range cand {
        if p.X < 0 || p.Y < 0 || p.X >= grid.W || p.Y >= grid.H {
            continue
        }
        if seen[p] {
            continue
        }
        seen[p] = true
        out = append(out, p)
    }
    return out
}

func resolveSlide(dir, name string) (string, bool) {
    for _, sub := range []string{"", "svs", "ndpi"} {
        p := filepath.Join(dir, sub, name)
        if _, err := os.Stat(p); err == nil {
            return p, true
        }
    }
    return "", false
}
```

- [ ] **Step 2: Install Python opentile locally and run**

```bash
pip install -r tests/oracle/requirements.txt
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/oracle/... -tags parity -timeout 60m -v
```

Expected: per-slide sub-tests PASS with byte equality across the sampled tiles. If byte equality fails, the Go implementation (internal/jpeg or internal/jpegturbo) diverges from Python opentile — file a BLOCKED and investigate.

- [ ] **Step 3: Commit**

```bash
git add tests/oracle/parity_test.go
git commit -m "test(oracle): Python-vs-Go byte parity test under -tags parity"
```

---

## Batch 8 — Final polish

---

## Task 27: README, CLAUDE.md, and deferred-list cleanup

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/deferred.md`

- [ ] **Step 1: README updates**

Extend the README usage section with NDPI and the cgo build notes. Append a new "Prerequisites" section:

```markdown
## Prerequisites

- Go 1.23+
- libjpeg-turbo 2.1+ (for NDPI pyramid levels and NDPI label). Installed via:
  - macOS: `brew install jpeg-turbo`
  - Debian/Ubuntu: `apt-get install libturbojpeg0-dev`
- `pkg-config` to resolve the library at build time.

Building without cgo is supported via `-tags nocgo`; that build continues to
support SVS (all features) and NDPI striped pyramid levels + overview, but
NDPI one-frame levels and NDPI label return `ErrCGORequired`.
```

Update the Status line from v0.1:

```markdown
**Status — v0.2**: Aperio SVS (JPEG and JPEG 2000) and Hamamatsu NDPI. Associated
images (label, overview, thumbnail) supported for both. BigTIFF supported
transparently. See `docs/deferred.md` for roadmap items and known limitations.
```

Add an NDPI example block:

```markdown
### NDPI-specific metadata

```go
import ndpi "github.com/wsilabs/opentile-go/formats/ndpi"

if nm, ok := ndpi.MetadataOf(tiler); ok {
    fmt.Println("SourceLens:", nm.SourceLens)
    fmt.Println("Focal offset:", nm.FocalOffset, "mm")
}
```
```

Add a parity-oracle blurb:

```markdown
### Parity testing

An opt-in `//go:build parity` test harness byte-compares tile output against
the Python `opentile` reference implementation:

```bash
pip install -r tests/oracle/requirements.txt
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/oracle/... -tags parity
```
```

- [ ] **Step 2: CLAUDE.md updates**

Bump the milestone line:

```markdown
## Current milestone — v0.2

- **Scope:** Aperio SVS + Hamamatsu NDPI with associated images. BigTIFF supported.
- **Deferred:** SVS corrupt-edge reconstruct fix (v1.0), Philips/Histech/OME (v0.4+), NDPI 64-bit offset extension for >4GB files (unless a test file forces earlier).
- **Design:** `docs/superpowers/specs/2026-04-21-opentile-go-v02-design.md`
- **Plan:** `docs/superpowers/plans/2026-04-21-opentile-go-v02.md`
- **Work branch:** `feat/v0.2`
```

And the invariants section gets a cgo clarification:

```markdown
- **cgo narrowly scoped.** `internal/jpegturbo/` is the only package that
  links libjpeg-turbo. The `nocgo` build tag (or `CGO_ENABLED=0`) swaps in
  a stub that returns `ErrCGORequired` for the one-frame/label crop paths;
  every other capability remains available.
```

- [ ] **Step 3: Retire deferred items**

In `docs/deferred.md`, delete the entries for:
- R1 (NDPI format support)
- R2 (internal/jpeg marker package)
- R3 (SVS associated images)
- R8 partial retirement (BigTIFF now supported)
- R11 (parity oracle)

Add any new deferred items surfaced during v0.2 (CRLF in SoftwareLine, etc., if still unfixed; a v0.2-specific note on the 64-bit NDPI extension if the 6.5 GB sample requires it and we punted).

- [ ] **Step 4: Run final checks**

```bash
go test ./... -race
go vet ./...
CGO_ENABLED=0 go test ./... -tags nocgo
```

Expected: full suite clean in both builds; vet clean.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md docs/deferred.md
git commit -m "docs: update README/CLAUDE.md for v0.2; retire NDPI and associated-image backlog items"
```

---

## Task 28: Final vet / race / coverage sweep

**Files:**
- None (this task validates, it does not modify)

- [ ] **Step 1: Full race + vet**

```bash
go test ./... -race -count=1
go vet ./...
```

Expected: all tests pass; no vet warnings.

- [ ] **Step 2: nocgo build verification**

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./... -tags nocgo -count=1
```

Expected: build succeeds; tests that exercise the jpegturbo crop path are skipped or return `ErrCGORequired` gracefully.

- [ ] **Step 3: Coverage**

```bash
go test ./... -coverpkg=./... -coverprofile=v02.coverprofile
go tool cover -func=v02.coverprofile | tail -20
```

Expected: top-line coverage ≥ the v0.1 baseline (≥75% overall, ≥80% formats/*, ≥90% internal/*). If any package regressed materially, add targeted unit tests to close the gap before declaring the batch done.

- [ ] **Step 4: Final doc cross-check**

```bash
go doc github.com/wsilabs/opentile-go | head -60
go doc github.com/wsilabs/opentile-go/formats/ndpi | head -40
```

Expected: godoc renders the public surface (Tiler, Level, Metadata, Open, OpenFile, Compression, error sentinels, ndpi.Factory, ndpi.New, ndpi.MetadataOf) with usable comments.

- [ ] **Step 5: Branch log snapshot**

```bash
git log --oneline main..feat/v0.2 | head -40
```

Expected: the full series of feature/fix/test/docs commits for v0.2 is visible.

---

## Done when

- `go test ./... -race` passes on the cgo build.
- `CGO_ENABLED=0 go test ./... -tags nocgo` passes.
- `go vet ./...` is clean.
- Five parity fixtures are committed: three SVS (regenerated) + two NDPI.
- The parity oracle passes on at least one slide per format when Python opentile is installed (may be skipped in default CI).
- `README.md`, `CLAUDE.md`, and `docs/deferred.md` reflect v0.2 scope.
- The plan file's checkboxes all carry a ✓ (manually marked or tracked separately).

