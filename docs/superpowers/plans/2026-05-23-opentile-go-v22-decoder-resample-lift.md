# opentile-go v0.22 — Decoder + Resample Lift Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lift the decoder layer and pixel-resample primitives from `wsitools/internal/decoder` + `wsitools/internal/codec/*` + `wsitools/internal/resample` into opentile-go as new public subpackages, with a registry-based decoder dispatch and image-aware decoder interface. Ship as opentile-go v0.22.0 (pure addition, no behavior change for current consumers); wsitools follows in v0.9.0 by deleting its internal copies and re-importing.

**Architecture:** New `decoder/` parent package exporting `Decoder`/`Factory`/`Image`/`PixelFormat`/`DecodeOptions` + registry. 9 codec subpackages registering via `init()` against the registry: 3 pure-Go (`none`, `lzw`, `deflate`); 6 cgo (`jpeg`, `jpeg2000`, `jpegxl`, `avif`, `webp`, `htj2k`) with per-codec opt-out tags + master `nocgo` + register-but-error stubs. New `resample/` pure-Go subpackage with `Box`/`Lanczos`/`Bilinear`/`Nearest` kernels. wsitools then deletes its `internal/decoder` + `internal/resample` and re-imports.

**Tech Stack:** Go 1.22+, cgo via pkg-config for libjpeg-turbo / openjp2 / libjxl / libavif / libwebp / openjphjs. Reuses existing opentile-go internal infrastructure (`internal/jpegturbo`, `internal/tifflzw`, `internal/jpeg`).

**Reference docs (read before starting):**
- Spec: `docs/superpowers/specs/2026-05-23-opentile-go-v22-decoder-resample-lift-design.md` (this repo)
- wsitools' decoder source: `~/GitHub/wsitools/internal/decoder/{decoder.go,jpeg.go,jpeg2000.go}`
- wsitools' resample source: `~/GitHub/wsitools/internal/resample/{area.go,lanczos.go}`
- wsitools' encoder bindings (cgo binding templates for the 4 new decoders): `~/GitHub/wsitools/internal/codec/{jpegxl,avif,webp,htj2k}/*.go`
- opentile-go's existing narrow cgo pattern: `internal/jpegturbo/turbo_{cgo,nocgo}.go`
- opentile-go's pure-Go LZW: `internal/tifflzw/{reader,writer}.go`

---

## File Structure

**New files in opentile-go:**

```
decoder/
    doc.go                  -- package documentation
    image.go                -- Image, PixelFormat, NewImage, NewImageFormat
    decoder.go              -- Decoder interface, DecodeOptions
    factory.go              -- Factory interface, registry (Register, Get, GetByCompressionTag, Registered)
    errors.go               -- ErrCodecUnavailable, ErrUnsupportedScale, ErrUnsupportedFormat, ErrDestinationSize, ErrCorruptInput
    decoder_test.go         -- Registry round-trip with synthetic factory
    image_test.go           -- Image construction / format invariants
    errors_test.go          -- errors.Is sentinel checks

decoder/none/
    none.go                 -- uncompressed (TIFF Compression=1) decoder; pure Go
    none_test.go

decoder/lzw/
    lzw.go                  -- LZW decoder wrapping internal/tifflzw (TIFF Compression=5)
    lzw_test.go

decoder/deflate/
    deflate.go              -- deflate/zip decoder via compress/zlib (TIFF Compression=8)
    deflate_test.go

decoder/jpeg/
    jpeg_cgo.go             -- libjpeg-turbo decoder (real impl, build cgo && !nocgo)
    jpeg_nocgo.go           -- stub with ErrCodecUnavailable (build !cgo || nocgo)
    jpeg_test.go            -- real-decoder tests (build cgo && !nocgo)
    jpeg_nocgo_test.go      -- stub diagnostic check (build !cgo || nocgo)

decoder/jpeg2000/
    jp2_cgo.go              -- openjp2 decoder (cgo && !nocgo)
    jp2_nocgo.go            -- stub
    jp2_test.go
    jp2_nocgo_test.go

decoder/jpegxl/
    jxl_cgo.go              -- libjxl decoder (cgo && !nocgo && !nojxl)
    jxl_nocgo.go            -- stub
    jxl_test.go
    jxl_nocgo_test.go

decoder/avif/
    avif_cgo.go             -- libavif decoder (cgo && !nocgo && !noavif)
    avif_nocgo.go
    avif_test.go
    avif_nocgo_test.go

decoder/webp/
    webp_cgo.go             -- libwebp decoder (cgo && !nocgo && !nowebp)
    webp_nocgo.go
    webp_test.go
    webp_nocgo_test.go

decoder/htj2k/
    htj2k_cgo.go            -- openjphjs decoder (cgo && !nocgo && !nohtj2k)
    htj2k_nocgo.go
    htj2k_test.go
    htj2k_nocgo_test.go
    shim.cpp                -- if openjphjs uses a C++ shim like wsitools' encoder does

decoder/all/
    all.go                  -- blanket side-effect imports

resample/
    doc.go
    resample.go             -- Kernel, Image, ImageInto
    area.go                 -- Box (area-averaging) kernel
    lanczos.go              -- Lanczos kernel
    nearest.go              -- Nearest-neighbor kernel
    bilinear.go             -- Bilinear kernel
    area_test.go            -- ported from wsitools
    lanczos_test.go         -- new
    nearest_test.go         -- new
    bilinear_test.go        -- new
```

**Modified files in opentile-go:**
- `CHANGELOG.md` — v0.22.0 entry
- `README.md` — note the new decoder + resample subpackages (optional but recommended)

**Modified + deleted files in wsitools (Phase 2):**
- Modify: `go.mod` (bump opentile-go to v0.22.0)
- Modify: `cmd/wsitools/main.go` (add blanket import)
- Modify: `cmd/wsitools/transcode.go` (decoder call-site update)
- Modify: `cmd/wsitools/downsample.go` (decoder call-site update)
- Modify: `CHANGELOG.md`, `cmd/wsitools/version.go`
- Delete: `internal/decoder/` (entire directory)
- Delete: `internal/resample/` (entire directory)

---

# PHASE 1 — opentile-go v0.22.0

All Phase 1 tasks happen in `/Users/cornish/GitHub/opentile-go` on branch `main`.

## Task 1.1: Scaffold `decoder/` parent package with doc + smoke test

**Files:**
- Create: `decoder/doc.go`
- Create: `decoder/decoder_test.go`

- [ ] **Step 1: Write a smoke test**

Create `decoder/decoder_test.go`:

```go
package decoder_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestPackageCompiles(t *testing.T) {
	var _ decoder.PixelFormat
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/...`

Expected: `package github.com/wsilabs/opentile-go/decoder: no Go files`

- [ ] **Step 3: Create doc.go**

Create `decoder/doc.go`:

```go
// Package decoder defines the public Decoder interface and registry for
// opentile-go's pluggable codec layer. Codec-specific subpackages
// (decoder/jpeg, decoder/jpeg2000, decoder/lzw, etc.) register
// themselves into this package's registry at init() time.
//
// Most consumers wanting "all codecs available" should blank-import
// the decoder/all subpackage:
//
//	import _ "github.com/wsilabs/opentile-go/decoder/all"
//
// Smaller-footprint consumers can blank-import only the codec
// subpackages they need.
//
// The decoder layer is designed to back v1.0's Slide.DecodedTile /
// ReadRegion / ScaledStrips methods. It is also usable standalone for
// third-party Go pathology code that wants decoded tile bytes from
// opentile-go-readable WSI files.
//
// Design spec: docs/superpowers/specs/2026-05-23-opentile-go-v22-decoder-resample-lift-design.md.
package decoder
```

- [ ] **Step 4: Create stub PixelFormat to make the smoke test compile**

Append to `decoder/doc.go` (or create a temp `decoder/image.go`):

```go
// PixelFormat selects the in-memory pixel layout of a decoded Image.
// Defined fully in image.go (Task 1.2). Declared here so this file
// compiles before image.go lands.
type PixelFormat int
```

Actually, scratch that — clean approach: create `decoder/image.go` with the full PixelFormat now, since the smoke test needs it.

Create `decoder/image.go`:

```go
package decoder

// PixelFormat selects the in-memory pixel layout of a decoded Image.
type PixelFormat int

const (
	// PixelFormatRGB is 3 bytes per pixel, no alpha channel.
	// The default — WSI imagery is opaque so alpha is wasted memory.
	PixelFormatRGB PixelFormat = iota

	// PixelFormatRGBA is 4 bytes per pixel with alpha = 0xFF.
	// Use when interop with Go stdlib image.NRGBA matters.
	PixelFormatRGBA
)

// Image is a decoded raster bitmap.
type Image struct {
	Width, Height int
	Stride        int         // bytes per row; may over-allocate for SIMD alignment
	Format        PixelFormat
	Pix           []byte      // len(Pix) == Stride * Height
}
```

- [ ] **Step 5: Run test to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/... -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/cornish/GitHub/opentile-go
git add decoder/doc.go decoder/image.go decoder/decoder_test.go
git commit -m "feat(decoder): scaffold decoder package with PixelFormat + Image type"
```

---

## Task 1.2: Image type construction helpers

**Files:**
- Modify: `decoder/image.go`
- Create: `decoder/image_test.go`

- [ ] **Step 1: Write failing tests**

Create `decoder/image_test.go`:

```go
package decoder

import "testing"

func TestNewImageRGB(t *testing.T) {
	im := NewImage(100, 50)
	if im.Width != 100 || im.Height != 50 {
		t.Errorf("dimensions: got %dx%d want 100x50", im.Width, im.Height)
	}
	if im.Format != PixelFormatRGB {
		t.Errorf("default format: got %d want PixelFormatRGB", im.Format)
	}
	if im.Stride != 100*3 {
		t.Errorf("RGB stride: got %d want %d", im.Stride, 100*3)
	}
	if len(im.Pix) != im.Stride*im.Height {
		t.Errorf("Pix size: got %d want %d", len(im.Pix), im.Stride*im.Height)
	}
}

func TestNewImageFormatRGBA(t *testing.T) {
	im := NewImageFormat(100, 50, PixelFormatRGBA)
	if im.Format != PixelFormatRGBA {
		t.Errorf("format: got %d want PixelFormatRGBA", im.Format)
	}
	if im.Stride != 100*4 {
		t.Errorf("RGBA stride: got %d want %d", im.Stride, 100*4)
	}
	if len(im.Pix) != im.Stride*im.Height {
		t.Errorf("Pix size: got %d want %d", len(im.Pix), im.Stride*im.Height)
	}
}

func TestNewImageZeroDimensions(t *testing.T) {
	im := NewImage(0, 0)
	if im.Width != 0 || im.Height != 0 || len(im.Pix) != 0 {
		t.Errorf("zero dimensions: got %+v", im)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -run TestNewImage`

Expected: compile error (`undefined: NewImage`).

- [ ] **Step 3: Add constructors to image.go**

Append to `decoder/image.go`:

```go
// NewImage returns a freshly-allocated Image with PixelFormatRGB and
// Stride = w * 3. The Pix slice is zero-filled.
func NewImage(w, h int) *Image {
	return NewImageFormat(w, h, PixelFormatRGB)
}

// NewImageFormat returns a freshly-allocated Image with the requested
// format. Stride is set to the format's bytes-per-pixel times w.
func NewImageFormat(w, h int, fmt PixelFormat) *Image {
	bpp := bytesPerPixel(fmt)
	stride := w * bpp
	return &Image{
		Width:  w,
		Height: h,
		Stride: stride,
		Format: fmt,
		Pix:    make([]byte, stride*h),
	}
}

func bytesPerPixel(fmt PixelFormat) int {
	switch fmt {
	case PixelFormatRGBA:
		return 4
	default:
		return 3 // PixelFormatRGB
	}
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -v`

Expected: PASS for all tests.

- [ ] **Step 5: Commit**

```bash
git add decoder/image.go decoder/image_test.go
git commit -m "feat(decoder): NewImage + NewImageFormat with stride math"
```

---

## Task 1.3: Decoder interface + DecodeOptions

**Files:**
- Create: `decoder/decoder.go`

- [ ] **Step 1: Create decoder.go**

Create `decoder/decoder.go`:

```go
package decoder

// DecodeOptions configures a single Decode call. The zero value is
// valid (Scale=1, Format=PixelFormatRGB, Dst=nil → allocate fresh RGB).
type DecodeOptions struct {
	// Scale is the IDCT-time scale factor (JPEG decoders only).
	// Valid values: 1, 2, 4, 8. Other values return ErrUnsupportedScale.
	// Non-JPEG decoders return ErrUnsupportedScale if Scale != 1.
	// The zero value (0) is treated as 1 (no scaling).
	Scale int

	// Format is the requested output pixel format. Decoders return
	// ErrUnsupportedFormat if they can't produce the requested format.
	// Today: PixelFormatRGB and PixelFormatRGBA are universal.
	Format PixelFormat

	// Dst is an optional caller-supplied destination Image. If nil, the
	// decoder allocates. If non-nil and its dimensions match the
	// decoded size, the decoder writes into Dst.Pix and returns Dst.
	// Mismatched dimensions return ErrDestinationSize.
	Dst *Image
}

// Decoder turns compressed tile bytes into a decoded Image. Decoders
// are NOT safe for concurrent use; callers running concurrent decodes
// on the same slide should construct one Decoder per goroutine via
// Factory.New().
type Decoder interface {
	// Decode the compressed bytes per opts. If opts.Dst is non-nil and
	// matches the decoded dimensions, writes into Dst and returns it;
	// otherwise allocates a fresh Image.
	Decode(compressed []byte, opts DecodeOptions) (*Image, error)

	// Close releases the decoder's internal state. Safe to call
	// multiple times. After Close, further Decode calls return an
	// error.
	Close() error
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `cd /Users/cornish/GitHub/opentile-go && go build ./decoder/...`

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add decoder/decoder.go
git commit -m "feat(decoder): Decoder interface + DecodeOptions"
```

---

## Task 1.4: Factory interface + Registry

**Files:**
- Create: `decoder/factory.go`
- Modify: `decoder/decoder_test.go` (extend)

- [ ] **Step 1: Append failing tests**

Replace `decoder/decoder_test.go` with:

```go
package decoder_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestPackageCompiles(t *testing.T) {
	var _ decoder.PixelFormat
}

// fakeFactory is a registry test double.
type fakeFactory struct {
	name string
	tags []uint16
}

func (f *fakeFactory) Name() string                  { return f.name }
func (f *fakeFactory) TIFFCompressionTags() []uint16 { return f.tags }
func (f *fakeFactory) New() decoder.Decoder          { return nil } // unused in registry tests

func TestRegisterAndGet(t *testing.T) {
	f := &fakeFactory{name: "fake-codec-1", tags: []uint16{9001}}
	decoder.Register(f)

	got, ok := decoder.Get("fake-codec-1")
	if !ok {
		t.Fatalf("Get(fake-codec-1): not registered")
	}
	if got.Name() != "fake-codec-1" {
		t.Errorf("Get returned %q want fake-codec-1", got.Name())
	}
}

func TestGetByCompressionTag(t *testing.T) {
	f := &fakeFactory{name: "fake-codec-2", tags: []uint16{9002, 9003}}
	decoder.Register(f)

	for _, tag := range []uint16{9002, 9003} {
		got, ok := decoder.GetByCompressionTag(tag)
		if !ok {
			t.Errorf("GetByCompressionTag(%d): not registered", tag)
			continue
		}
		if got.Name() != "fake-codec-2" {
			t.Errorf("tag %d: got %q want fake-codec-2", tag, got.Name())
		}
	}
}

func TestGetMissing(t *testing.T) {
	if _, ok := decoder.Get("does-not-exist"); ok {
		t.Errorf("Get(does-not-exist): expected (nil, false)")
	}
	if _, ok := decoder.GetByCompressionTag(0xFFFF); ok {
		t.Errorf("GetByCompressionTag(0xFFFF): expected (nil, false)")
	}
}

func TestRegistered(t *testing.T) {
	decoder.Register(&fakeFactory{name: "fake-codec-3"})
	names := decoder.Registered()
	found := false
	for _, n := range names {
		if n == "fake-codec-3" {
			found = true
		}
	}
	if !found {
		t.Errorf("Registered(): fake-codec-3 not in %v", names)
	}
}

func TestRegisterOverwrites(t *testing.T) {
	f1 := &fakeFactory{name: "shadow"}
	f2 := &fakeFactory{name: "shadow"}
	decoder.Register(f1)
	decoder.Register(f2) // last-in-wins
	got, _ := decoder.Get("shadow")
	if got != f2 {
		t.Errorf("last-in-wins broken: got %p want %p", got, f2)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -run TestRegister -v`

Expected: compile error (`undefined: decoder.Register`, etc).

- [ ] **Step 3: Implement factory.go**

Create `decoder/factory.go`:

```go
package decoder

import "sync"

// Factory constructs decoders for a specific codec. Codec subpackages
// register a Factory in their init() function.
type Factory interface {
	// Name is the canonical codec identifier (e.g., "jpeg",
	// "jpeg2000", "lzw"). Lowercase.
	Name() string

	// TIFFCompressionTags lists the TIFF Compression tag values this
	// factory's decoder handles. Multiple tags allowed (e.g., JPEG
	// 2000 is both 33003 (Aperio) and 34712 (libtiff)). Empty for
	// non-TIFF-associated codecs.
	TIFFCompressionTags() []uint16

	// New returns a fresh Decoder instance. Each call returns a new
	// instance with its own state. Decoders are NOT safe for
	// concurrent use across goroutines.
	New() Decoder
}

var (
	regMu      sync.RWMutex
	byName     = map[string]Factory{}
	byTIFFTag  = map[uint16]Factory{}
)

// Register adds a factory to the global decoder registry. Called from
// each codec subpackage's init(). Last-in-wins on name or tag
// collision (intentional — lets consumers shadow a default decoder
// with a custom impl).
func Register(f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	byName[f.Name()] = f
	for _, tag := range f.TIFFCompressionTags() {
		byTIFFTag[tag] = f
	}
}

// Get returns the factory registered for the given codec name, or
// (nil, false) if none is registered.
func Get(name string) (Factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := byName[name]
	return f, ok
}

// GetByCompressionTag returns the factory registered for the given
// TIFF Compression tag value, or (nil, false) if none is registered.
func GetByCompressionTag(tag uint16) (Factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := byTIFFTag[tag]
	return f, ok
}

// Registered returns the canonical names of every registered decoder.
// Order is unspecified.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(byName))
	for n := range byName {
		out = append(out, n)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -v`

Expected: PASS for all tests.

- [ ] **Step 5: Commit**

```bash
git add decoder/factory.go decoder/decoder_test.go
git commit -m "feat(decoder): Factory interface + Register/Get/GetByCompressionTag/Registered"
```

---

## Task 1.5: Sentinel errors

**Files:**
- Create: `decoder/errors.go`
- Create: `decoder/errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `decoder/errors_test.go`:

```go
package decoder

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrorsExist(t *testing.T) {
	for _, e := range []error{
		ErrCodecUnavailable,
		ErrUnsupportedScale,
		ErrUnsupportedFormat,
		ErrDestinationSize,
		ErrCorruptInput,
	} {
		if e == nil {
			t.Errorf("nil sentinel error")
		}
		if e.Error() == "" {
			t.Errorf("empty error message on %T", e)
		}
	}
}

func TestErrorsIsWraps(t *testing.T) {
	wrapped := fmt.Errorf("decoder: jpegxl unavailable: %w", ErrCodecUnavailable)
	if !errors.Is(wrapped, ErrCodecUnavailable) {
		t.Errorf("errors.Is failed to detect ErrCodecUnavailable in wrapped error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -run TestSentinel`

Expected: compile error.

- [ ] **Step 3: Implement errors.go**

Create `decoder/errors.go`:

```go
package decoder

import "errors"

// Sentinel errors used by Decode implementations. Wrap with
// fmt.Errorf("...: %w", ErrXxx) for codec-specific context; callers
// detect via errors.Is.
var (
	// ErrCodecUnavailable is returned by the stub Decoder of a codec
	// subpackage that was excluded from this build (via -tags
	// no<codec> or -tags nocgo). The wrapping error message names the
	// codec and the build tag to remove.
	ErrCodecUnavailable = errors.New("decoder: codec not available in this build")

	// ErrUnsupportedScale is returned by Decode when DecodeOptions.Scale
	// is not a value the decoder supports. JPEG decoders accept 1, 2,
	// 4, 8; other decoders accept only 1.
	ErrUnsupportedScale = errors.New("decoder: scale factor not supported by this codec")

	// ErrUnsupportedFormat is returned by Decode when DecodeOptions.Format
	// is not producible by the decoder.
	ErrUnsupportedFormat = errors.New("decoder: pixel format not supported by this codec")

	// ErrDestinationSize is returned by Decode when DecodeOptions.Dst
	// is non-nil but its dimensions don't match the decoded size.
	ErrDestinationSize = errors.New("decoder: dst Image dimensions don't match decoded size")

	// ErrCorruptInput is returned by Decode when the compressed bytes
	// can't be parsed.
	ErrCorruptInput = errors.New("decoder: corrupt input data")
)
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/errors.go decoder/errors_test.go
git commit -m "feat(decoder): sentinel errors (ErrCodecUnavailable, ErrUnsupportedScale, etc.)"
```

---

## Task 1.6: `decoder/none` — uncompressed (TIFF Compression=1)

**Files:**
- Create: `decoder/none/none.go`
- Create: `decoder/none/none_test.go`

- [ ] **Step 1: Write failing tests**

Create `decoder/none/none_test.go`:

```go
package none

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("none")
	if !ok {
		t.Fatalf("none decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 1 {
		t.Errorf("TIFFCompressionTags: got %v want [1]", got)
	}
}

func TestDecodeRGBPassthrough(t *testing.T) {
	// Uncompressed: bytes ARE the pixels. 2x2 RGB.
	src := []byte{
		1, 2, 3, 4, 5, 6,        // row 0: two pixels
		7, 8, 9, 10, 11, 12,     // row 1: two pixels
	}
	f, _ := decoder.Get("none")
	d := f.New()
	defer d.Close()

	// Need width/height somehow — for "none" the caller must size via Dst.
	// Allocate a destination of known size and the decoder fills it.
	dst := decoder.NewImage(2, 2)
	got, err := d.Decode(src, decoder.DecodeOptions{Dst: dst})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != dst {
		t.Errorf("Decode should return the supplied Dst")
	}
	if !bytes.Equal(dst.Pix, src) {
		t.Errorf("Pix: got %v want %v", dst.Pix, src)
	}
}

func TestDecodeRequiresDst(t *testing.T) {
	// Without Dst, the decoder has no way to know image dimensions for raw bytes.
	f, _ := decoder.Get("none")
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{1, 2, 3}, decoder.DecodeOptions{})
	if err == nil {
		t.Errorf("Decode without Dst should error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/none/...`

Expected: compile error (package doesn't exist).

- [ ] **Step 3: Implement none.go**

Create `decoder/none/none.go`:

```go
// Package none implements the trivial "no-compression" decoder for
// TIFF Compression=1 tiles, where the on-disk bytes ARE the decoded
// pixels.
//
// Because uncompressed tile bytes carry no dimensions or format, the
// caller MUST supply DecodeOptions.Dst pre-sized to the expected
// tile dimensions; the decoder memcpys src into Dst.Pix.
package none

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "none" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{1} }
func (f *factory) New() decoder.Decoder          { return &noneDecoder{} }

type noneDecoder struct{}

func (d *noneDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/none: Dst is required (uncompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	expect := opts.Dst.Stride * opts.Dst.Height
	if len(compressed) != expect {
		return nil, fmt.Errorf("decoder/none: src length %d != Dst.Stride*Height %d: %w", len(compressed), expect, decoder.ErrDestinationSize)
	}
	copy(opts.Dst.Pix, compressed)
	return opts.Dst, nil
}

func (d *noneDecoder) Close() error { return nil }
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/none/ -v`

Expected: PASS for all 3 tests.

- [ ] **Step 5: Commit**

```bash
git add decoder/none/
git commit -m "feat(decoder): none — TIFF Compression=1 uncompressed passthrough"
```

---

## Task 1.7: `decoder/deflate` — TIFF Compression=8 (zlib)

**Files:**
- Create: `decoder/deflate/deflate.go`
- Create: `decoder/deflate/deflate_test.go`

- [ ] **Step 1: Write failing tests**

Create `decoder/deflate/deflate_test.go`:

```go
package deflate

import (
	"bytes"
	"compress/zlib"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("deflate")
	if !ok {
		t.Fatalf("deflate decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 8 {
		t.Errorf("TIFFCompressionTags: got %v want [8]", got)
	}
}

func TestRoundTrip(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6,
		7, 8, 9, 10, 11, 12,
	}
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(pixels)
	_ = w.Close()

	f, _ := decoder.Get("deflate")
	d := f.New()
	defer d.Close()
	dst := decoder.NewImage(2, 2)
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{Dst: dst})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Pix, pixels) {
		t.Errorf("Pix: got %v want %v", got.Pix, pixels)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/deflate/...`

Expected: compile error.

- [ ] **Step 3: Implement deflate.go**

Create `decoder/deflate/deflate.go`:

```go
// Package deflate implements the decoder for TIFF Compression=8
// (Deflate/Zip). Uses stdlib compress/zlib.
package deflate

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "deflate" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{8} }
func (f *factory) New() decoder.Decoder          { return &deflateDecoder{} }

type deflateDecoder struct{}

func (d *deflateDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/deflate: Dst is required (decompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("decoder/deflate: zlib header: %w (%w)", err, decoder.ErrCorruptInput)
	}
	defer zr.Close()
	n, err := io.ReadFull(zr, opts.Dst.Pix)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("decoder/deflate: read: %w (%w)", err, decoder.ErrCorruptInput)
	}
	if n != len(opts.Dst.Pix) {
		return nil, fmt.Errorf("decoder/deflate: decoded %d bytes, expected %d: %w", n, len(opts.Dst.Pix), decoder.ErrDestinationSize)
	}
	return opts.Dst, nil
}

func (d *deflateDecoder) Close() error { return nil }
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/deflate/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/deflate/
git commit -m "feat(decoder): deflate — TIFF Compression=8 via stdlib zlib"
```

---

## Task 1.8: `decoder/lzw` — TIFF Compression=5 (wraps internal/tifflzw)

**Files:**
- Create: `decoder/lzw/lzw.go`
- Create: `decoder/lzw/lzw_test.go`

- [ ] **Step 1: Write failing tests**

Create `decoder/lzw/lzw_test.go`:

```go
package lzw

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("lzw")
	if !ok {
		t.Fatalf("lzw decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 5 {
		t.Errorf("TIFFCompressionTags: got %v want [5]", got)
	}
}

func TestRoundTrip(t *testing.T) {
	pixels := []byte{
		10, 20, 30, 40, 50, 60,
		70, 80, 90, 100, 110, 120,
	}
	var buf bytes.Buffer
	w := tifflzw.NewWriter(&buf, tifflzw.MSB, 8)
	_, _ = w.Write(pixels)
	_ = w.Close()

	f, _ := decoder.Get("lzw")
	d := f.New()
	defer d.Close()
	dst := decoder.NewImage(2, 2)
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{Dst: dst})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Pix, pixels) {
		t.Errorf("Pix: got %v want %v", got.Pix, pixels)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/lzw/...`

Expected: compile error.

- [ ] **Step 3: Implement lzw.go**

Create `decoder/lzw/lzw.go`:

```go
// Package lzw implements the decoder for TIFF Compression=5 (LZW).
// Wraps the existing internal/tifflzw package which carries the
// TIFF "off-by-one" code-width transition incompatible with stdlib
// compress/lzw.
package lzw

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "lzw" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{5} }
func (f *factory) New() decoder.Decoder          { return &lzwDecoder{} }

type lzwDecoder struct{}

func (d *lzwDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/lzw: Dst is required (decompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	r := tifflzw.NewReader(bytes.NewReader(compressed), tifflzw.MSB, 8)
	defer r.Close()
	n, err := io.ReadFull(r, opts.Dst.Pix)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("decoder/lzw: read: %w (%w)", err, decoder.ErrCorruptInput)
	}
	if n != len(opts.Dst.Pix) {
		return nil, fmt.Errorf("decoder/lzw: decoded %d bytes, expected %d: %w", n, len(opts.Dst.Pix), decoder.ErrDestinationSize)
	}
	return opts.Dst, nil
}

func (d *lzwDecoder) Close() error { return nil }
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/lzw/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/lzw/
git commit -m "feat(decoder): lzw — TIFF Compression=5 wrapping internal/tifflzw"
```

---

## Task 1.9: `decoder/jpeg` — libjpeg-turbo wrapper

**Files:**
- Create: `decoder/jpeg/jpeg_cgo.go`
- Create: `decoder/jpeg/jpeg_nocgo.go`
- Create: `decoder/jpeg/jpeg_test.go`
- Create: `decoder/jpeg/jpeg_nocgo_test.go`

**Background:** Port the existing wsitools JPEG decoder. Read `~/GitHub/wsitools/internal/decoder/jpeg.go` as the reference implementation. The cgo binding shape is the same; the wrapper logic adapts to the new `decoder.Decoder` interface (returns `*Image` instead of `[]byte`; consumes `DecodeOptions.Scale` for the IDCT-time scale-factor parameter).

- [ ] **Step 1: Write failing tests**

Create `decoder/jpeg/jpeg_test.go`:

```go
//go:build cgo && !nocgo

package jpeg

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpeg")
	if !ok {
		t.Fatalf("jpeg decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 7 {
		t.Errorf("TIFFCompressionTags: got %v want [7]", got)
	}
}

func TestDecodeBasic(t *testing.T) {
	// Encode a 16x16 RGB image as JPEG using the stdlib encoder.
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.Pix[(y*16+x)*4+0] = byte(x * 16)
			src.Pix[(y*16+x)*4+1] = byte(y * 16)
			src.Pix[(y*16+x)*4+2] = 128
			src.Pix[(y*16+x)*4+3] = 0xFF
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90})

	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Width != 16 || got.Height != 16 {
		t.Errorf("dimensions: got %dx%d want 16x16", got.Width, got.Height)
	}
	if got.Format != decoder.PixelFormatRGB {
		t.Errorf("format: got %d want RGB", got.Format)
	}
	if got.Stride != 16*3 {
		t.Errorf("stride: got %d want %d", got.Stride, 16*3)
	}
}

func TestDecodeIDCTScale(t *testing.T) {
	// Encode 32x32, decode at scale 2 → 16x16.
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90})

	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{Scale: 2})
	if err != nil {
		t.Fatalf("Decode scale=2: %v", err)
	}
	if got.Width != 16 || got.Height != 16 {
		t.Errorf("scale=2 dimensions: got %dx%d want 16x16", got.Width, got.Height)
	}
}

func TestDecodeUnsupportedScale(t *testing.T) {
	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0xD8, 0xFF, 0xD9}, decoder.DecodeOptions{Scale: 3})
	if err == nil {
		t.Errorf("scale=3: expected error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpeg/...`

Expected: compile error.

- [ ] **Step 3: Port the cgo decoder**

Open `~/GitHub/wsitools/internal/decoder/jpeg.go` and read it through. It's a libjpeg-turbo wrapper using `tjDecompressHeader3` + `tjDecompress2`. Port to `decoder/jpeg/jpeg_cgo.go`:

Create `decoder/jpeg/jpeg_cgo.go`:

```go
//go:build cgo && !nocgo

// Package jpeg implements the JPEG decoder via libjpeg-turbo.
// TIFF Compression=7. Supports IDCT-time scale factors 1, 2, 4, 8.
package jpeg

/*
#cgo pkg-config: libturbojpeg
#include <stdlib.h>
#include <turbojpeg.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "jpeg" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{7} }
func (f *factory) New() decoder.Decoder          { return newCGODecoder() }

type cgoDecoder struct {
	mu     sync.Mutex
	handle C.tjhandle
	closed bool
}

func newCGODecoder() *cgoDecoder {
	return &cgoDecoder{handle: C.tjInitDecompress()}
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, fmt.Errorf("decoder/jpeg: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/jpeg: empty src: %w", decoder.ErrCorruptInput)
	}

	scale := opts.Scale
	if scale == 0 {
		scale = 1
	}
	if scale != 1 && scale != 2 && scale != 4 && scale != 8 {
		return nil, fmt.Errorf("decoder/jpeg: scale=%d (want 1,2,4,8): %w", scale, decoder.ErrUnsupportedScale)
	}

	// Read JPEG header to get full-resolution dimensions.
	var srcW, srcH, subsamp, colorspace C.int
	if rc := C.tjDecompressHeader3(d.handle,
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		&srcW, &srcH, &subsamp, &colorspace); rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg: tjDecompressHeader3: %s: %w", C.GoString(C.tjGetErrorStr2(d.handle)), decoder.ErrCorruptInput)
	}

	// Compute scaled output dimensions per libjpeg-turbo's tjScalingFactor table.
	// For scale N (1/N), output dim is ceil(srcDim / N).
	outW := int((int(srcW) + scale - 1) / scale)
	outH := int((int(srcH) + scale - 1) / scale)

	var pixelFormat C.int
	bpp := 3
	switch opts.Format {
	case decoder.PixelFormatRGBA:
		pixelFormat = C.TJPF_RGBA
		bpp = 4
	default:
		pixelFormat = C.TJPF_RGB
		bpp = 3
	}

	dst := opts.Dst
	if dst == nil {
		dst = decoder.NewImageFormat(outW, outH, opts.Format)
	} else if dst.Width != outW || dst.Height != outH {
		return nil, fmt.Errorf("decoder/jpeg: dst %dx%d != decoded %dx%d: %w",
			dst.Width, dst.Height, outW, outH, decoder.ErrDestinationSize)
	}

	stride := outW * bpp
	if rc := C.tjDecompress2(d.handle,
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst.Pix[0])),
		C.int(outW),
		C.int(stride),
		C.int(outH),
		pixelFormat,
		0); rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg: tjDecompress2: %s: %w", C.GoString(C.tjGetErrorStr2(d.handle)), decoder.ErrCorruptInput)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	C.tjDestroy(d.handle)
	d.closed = true
	return nil
}
```

**Note:** the exact libjpeg-turbo function signatures may differ slightly between versions. Cross-reference with `~/GitHub/wsitools/internal/decoder/jpeg.go` and adjust if the wsitools impl uses different signatures.

Create `decoder/jpeg/jpeg_nocgo.go`:

```go
//go:build !cgo || nocgo

package jpeg

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "jpeg" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{7} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/jpeg: requires cgo + libjpeg-turbo (rebuild with cgo enabled / without -tags nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/jpeg/jpeg_nocgo_test.go`:

```go
//go:build !cgo || nocgo

package jpeg

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpeg")
	if !ok {
		t.Fatalf("jpeg stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0xD8}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 4: Run tests to verify pass (default cgo build)**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpeg/ -v`

Expected: PASS for TestRegistered, TestDecodeBasic, TestDecodeIDCTScale, TestDecodeUnsupportedScale.

If you see build errors related to libjpeg-turbo function signatures or constant names (e.g. `tjGetErrorStr2` vs `tjGetErrorStr`), cross-reference `~/GitHub/wsitools/internal/decoder/jpeg.go` to match the exact API the existing wsitools wrapper uses.

- [ ] **Step 5: Run nocgo build to verify stub path compiles + tests pass**

Run: `cd /Users/cornish/GitHub/opentile-go && CGO_ENABLED=0 go test ./decoder/jpeg/ -v`

Expected: PASS for TestStubReturnsUnavailable.

- [ ] **Step 6: Commit**

```bash
git add decoder/jpeg/
git commit -m "feat(decoder): jpeg — libjpeg-turbo decoder with IDCT scale + nocgo stub"
```

---

## Task 1.10: `decoder/jpeg2000` — openjp2 wrapper

**Files:**
- Create: `decoder/jpeg2000/jp2_cgo.go`
- Create: `decoder/jpeg2000/jp2_nocgo.go`
- Create: `decoder/jpeg2000/jp2_test.go`
- Create: `decoder/jpeg2000/jp2_nocgo_test.go`

**Background:** Port wsitools' existing JPEG 2000 decoder. Read `~/GitHub/wsitools/internal/decoder/jpeg2000.go` first.

- [ ] **Step 1: Write failing tests**

Create `decoder/jpeg2000/jp2_test.go`:

```go
//go:build cgo && !nocgo

package jpeg2000

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Fatalf("jpeg2000 decoder not registered")
	}
	tags := f.TIFFCompressionTags()
	if len(tags) < 2 {
		t.Errorf("expected at least 2 tags (Aperio 33003 + libtiff 34712), got %v", tags)
	}
	wantTags := map[uint16]bool{33003: false, 34712: false}
	for _, tag := range tags {
		if _, want := wantTags[tag]; want {
			wantTags[tag] = true
		}
	}
	for tag, ok := range wantTags {
		if !ok {
			t.Errorf("missing TIFF tag %d", tag)
		}
	}
}

// Decode tests against real JP2K fixtures live in
// sample_files/svs/JP2K-33003-1.svs. Defer end-to-end JP2K decode
// validation to wsitools' golden-master pass at the v0.9.0 port.
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpeg2000/...`

Expected: compile error.

- [ ] **Step 3: Port the cgo decoder**

Open `~/GitHub/wsitools/internal/decoder/jpeg2000.go` and port to `decoder/jpeg2000/jp2_cgo.go`. The wsitools impl uses openjp2's stream-callback API; port the cgo binding verbatim, then rewrap to satisfy the `decoder.Decoder` interface (return `*Image` instead of `[]byte`).

Skeleton (substitute the actual openjp2 calls from wsitools verbatim):

```go
//go:build cgo && !nocgo

// Package jpeg2000 implements the JPEG 2000 decoder via openjp2.
// TIFF Compression=33003 (Aperio convention) and 34712 (libtiff
// convention). Does not support IDCT-time scaling; Decode rejects
// DecodeOptions.Scale != 1.
package jpeg2000

/*
#cgo pkg-config: libopenjp2
#include <openjpeg.h>
*/
import "C"

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "jpeg2000" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{33003, 34712} }
func (f *factory) New() decoder.Decoder          { return newCGODecoder() }

type cgoDecoder struct{
    // openjp2 codec/stream state; see wsitools/internal/decoder/jpeg2000.go
}

func newCGODecoder() *cgoDecoder { /* port from wsitools */ }

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
    if opts.Scale != 0 && opts.Scale != 1 {
        return nil, fmt.Errorf("decoder/jpeg2000: scale not supported: %w", decoder.ErrUnsupportedScale)
    }
    // ... port openjp2 decode logic from wsitools/internal/decoder/jpeg2000.go ...
    // Build *decoder.Image from the decoded RGB pixels.
    return nil, nil // placeholder
}

func (d *cgoDecoder) Close() error { /* destroy codec/stream */ return nil }
```

Cross-reference the wsitools impl for the precise openjp2 calls. Match the existing logic; just adapt the return shape.

Create `decoder/jpeg2000/jp2_nocgo.go` (same pattern as jpeg's nocgo stub):

```go
//go:build !cgo || nocgo

package jpeg2000

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "jpeg2000" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{33003, 34712} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/jpeg2000: requires cgo + libopenjp2 (rebuild with cgo enabled / without -tags nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/jpeg2000/jp2_nocgo_test.go`:

```go
//go:build !cgo || nocgo

package jpeg2000

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Fatalf("jpeg2000 stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x4F}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpeg2000/ -v`

Expected: PASS for TestRegistered. (Decode tests are deferred to wsitools' v0.9.0 golden-master pass with a real JP2K fixture.)

Run: `cd /Users/cornish/GitHub/opentile-go && CGO_ENABLED=0 go test ./decoder/jpeg2000/ -v`

Expected: PASS for TestStubReturnsUnavailable.

- [ ] **Step 5: Commit**

```bash
git add decoder/jpeg2000/
git commit -m "feat(decoder): jpeg2000 — openjp2 decoder + nocgo stub"
```

---

## Task 1.11: `decoder/jpegxl` — libjxl wrapper (new cgo decoder)

**Files:**
- Create: `decoder/jpegxl/jxl_cgo.go`
- Create: `decoder/jpegxl/jxl_nocgo.go`
- Create: `decoder/jpegxl/jxl_test.go`
- Create: `decoder/jpegxl/jxl_nocgo_test.go`

**Background:** Greenfield cgo decoder — wsitools has an encoder at `internal/codec/jpegxl/jpegxl.go` but no decoder. Read the encoder's cgo block to see the libjxl pkg-config + include pattern, then implement the decode side using `JxlDecoderCreate` / `JxlDecoderProcessInput` / `JxlDecoderSetImageOutBuffer`. The TIFF Compression tag is `50002` (wsitools convention).

- [ ] **Step 1: Write failing tests**

Create `decoder/jpegxl/jxl_test.go`:

```go
//go:build cgo && !nocgo && !nojxl

package jpegxl

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatalf("jpegxl decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 50002 {
		t.Errorf("TIFFCompressionTags: got %v want [50002]", got)
	}
}

// End-to-end Decode validation: encode a known image via the wsitools
// encoder, decode via this package, assert pixel-level closeness.
// Detailed test added when encoder + decoder are both available; defer
// to wsitools v0.9.0 cross-checks if fixtures aren't readily available
// in this repo.
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpegxl/...`

Expected: compile error.

- [ ] **Step 3: Implement jxl_cgo.go**

Open `~/GitHub/wsitools/internal/codec/jpegxl/jpegxl.go` and study the cgo block (pkg-config + headers). Then implement the decoder using libjxl's decoder API:

Create `decoder/jpegxl/jxl_cgo.go`:

```go
//go:build cgo && !nocgo && !nojxl

// Package jpegxl implements the JPEG-XL decoder via libjxl.
// TIFF Compression=50002 (wsi-tools convention; not registered with Adobe).
package jpegxl

/*
#cgo pkg-config: libjxl libjxl_threads
#include <stdlib.h>
#include <jxl/decode.h>
#include <jxl/thread_parallel_runner.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "jpegxl" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{50002} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/jpegxl: scale not supported: %w", decoder.ErrUnsupportedScale)
	}
	if d.closed {
		return nil, fmt.Errorf("decoder/jpegxl: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/jpegxl: empty src: %w", decoder.ErrCorruptInput)
	}

	dec := C.JxlDecoderCreate(nil)
	if dec == nil {
		return nil, fmt.Errorf("decoder/jpegxl: JxlDecoderCreate failed")
	}
	defer C.JxlDecoderDestroy(dec)

	if rc := C.JxlDecoderSubscribeEvents(dec,
		C.JXL_DEC_BASIC_INFO|C.JXL_DEC_FULL_IMAGE); rc != C.JXL_DEC_SUCCESS {
		return nil, fmt.Errorf("decoder/jpegxl: SubscribeEvents rc=%d", rc)
	}
	if rc := C.JxlDecoderSetInput(dec,
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src))); rc != C.JXL_DEC_SUCCESS {
		return nil, fmt.Errorf("decoder/jpegxl: SetInput rc=%d", rc)
	}
	C.JxlDecoderCloseInput(dec)

	// Pump events: basic-info → image.
	var width, height C.uint32_t
	var dst *decoder.Image
	bpp := 3
	pf := C.JxlPixelFormat{num_channels: 3, data_type: C.JXL_TYPE_UINT8, endianness: C.JXL_NATIVE_ENDIAN, align: 0}
	if opts.Format == decoder.PixelFormatRGBA {
		pf.num_channels = 4
		bpp = 4
	}

	for {
		status := C.JxlDecoderProcessInput(dec)
		switch status {
		case C.JXL_DEC_BASIC_INFO:
			var info C.JxlBasicInfo
			if rc := C.JxlDecoderGetBasicInfo(dec, &info); rc != C.JXL_DEC_SUCCESS {
				return nil, fmt.Errorf("decoder/jpegxl: GetBasicInfo rc=%d", rc)
			}
			width = info.xsize
			height = info.ysize
			if opts.Dst != nil {
				if opts.Dst.Width != int(width) || opts.Dst.Height != int(height) {
					return nil, fmt.Errorf("decoder/jpegxl: dst %dx%d != decoded %dx%d: %w",
						opts.Dst.Width, opts.Dst.Height, int(width), int(height), decoder.ErrDestinationSize)
				}
				dst = opts.Dst
			} else {
				dst = decoder.NewImageFormat(int(width), int(height), opts.Format)
			}
		case C.JXL_DEC_NEED_IMAGE_OUT_BUFFER:
			needed := C.size_t(width) * C.size_t(height) * C.size_t(bpp)
			if rc := C.JxlDecoderSetImageOutBuffer(dec, &pf, unsafe.Pointer(&dst.Pix[0]), needed); rc != C.JXL_DEC_SUCCESS {
				return nil, fmt.Errorf("decoder/jpegxl: SetImageOutBuffer rc=%d", rc)
			}
		case C.JXL_DEC_FULL_IMAGE:
			return dst, nil
		case C.JXL_DEC_ERROR:
			return nil, fmt.Errorf("decoder/jpegxl: decode error: %w", decoder.ErrCorruptInput)
		default:
			return nil, fmt.Errorf("decoder/jpegxl: unexpected status %d", status)
		}
	}
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
```

Create `decoder/jpegxl/jxl_nocgo.go`:

```go
//go:build !cgo || nocgo || nojxl

package jpegxl

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "jpegxl" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{50002} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/jpegxl: requires cgo + libjxl (rebuild without -tags nojxl or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/jpegxl/jxl_nocgo_test.go`:

```go
//go:build !cgo || nocgo || nojxl

package jpegxl

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatalf("jpegxl stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x0A}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/jpegxl/ -v`

Expected: PASS for TestRegistered.

Run with nojxl tag: `go test -tags nojxl ./decoder/jpegxl/ -v`

Expected: PASS for TestStubReturnsUnavailable.

If libjxl headers / functions differ from the sketch (libjxl evolves), adjust to match the actual library API. Cross-reference https://github.com/libjxl/libjxl decode example code.

- [ ] **Step 5: Commit**

```bash
git add decoder/jpegxl/
git commit -m "feat(decoder): jpegxl — libjxl decoder + nocgo/nojxl stub"
```

---

## Task 1.12: `decoder/avif` — libavif wrapper (new cgo decoder)

**Files:**
- Create: `decoder/avif/avif_cgo.go`
- Create: `decoder/avif/avif_nocgo.go`
- Create: `decoder/avif/avif_test.go`
- Create: `decoder/avif/avif_nocgo_test.go`

**Background:** Greenfield. Reference: `~/GitHub/wsitools/internal/codec/avif/avif.go` for the cgo pkg-config setup. Implement decode via `avifDecoderCreate` / `avifDecoderSetIOMemory` / `avifDecoderParse` / `avifDecoderNextImage` + `avifRGBImageSetDefaults` + `avifImageYUVToRGB`. TIFF Compression tag: `60001` (wsi-tools private/experimental).

- [ ] **Step 1: Write failing tests**

Create `decoder/avif/avif_test.go`:

```go
//go:build cgo && !nocgo && !noavif

package avif

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("avif")
	if !ok {
		t.Fatalf("avif decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 60001 {
		t.Errorf("TIFFCompressionTags: got %v want [60001]", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/avif/...`

Expected: compile error.

- [ ] **Step 3: Implement avif_cgo.go**

Open `~/GitHub/wsitools/internal/codec/avif/avif.go` and copy the cgo block layout. Implement decoder using libavif's decoder API.

Create `decoder/avif/avif_cgo.go`:

```go
//go:build cgo && !nocgo && !noavif

// Package avif implements the AVIF decoder via libavif.
// TIFF Compression=60001 (wsi-tools private/experimental range).
package avif

/*
#cgo pkg-config: libavif
#include <avif/avif.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "avif" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{60001} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/avif: scale not supported: %w", decoder.ErrUnsupportedScale)
	}
	if d.closed {
		return nil, fmt.Errorf("decoder/avif: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/avif: empty src: %w", decoder.ErrCorruptInput)
	}

	dec := C.avifDecoderCreate()
	if dec == nil {
		return nil, fmt.Errorf("decoder/avif: avifDecoderCreate failed")
	}
	defer C.avifDecoderDestroy(dec)

	if rc := C.avifDecoderSetIOMemory(dec,
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src))); rc != C.AVIF_RESULT_OK {
		return nil, fmt.Errorf("decoder/avif: SetIOMemory rc=%d", rc)
	}
	if rc := C.avifDecoderParse(dec); rc != C.AVIF_RESULT_OK {
		return nil, fmt.Errorf("decoder/avif: Parse rc=%d: %w", rc, decoder.ErrCorruptInput)
	}
	if rc := C.avifDecoderNextImage(dec); rc != C.AVIF_RESULT_OK {
		return nil, fmt.Errorf("decoder/avif: NextImage rc=%d: %w", rc, decoder.ErrCorruptInput)
	}

	w := int(dec.image.width)
	h := int(dec.image.height)

	var dst *decoder.Image
	if opts.Dst != nil {
		if opts.Dst.Width != w || opts.Dst.Height != h {
			return nil, fmt.Errorf("decoder/avif: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	} else {
		dst = decoder.NewImageFormat(w, h, opts.Format)
	}

	var rgb C.avifRGBImage
	C.avifRGBImageSetDefaults(&rgb, dec.image)
	if opts.Format == decoder.PixelFormatRGBA {
		rgb.format = C.AVIF_RGB_FORMAT_RGBA
	} else {
		rgb.format = C.AVIF_RGB_FORMAT_RGB
	}
	rgb.depth = 8
	rgb.pixels = (*C.uint8_t)(unsafe.Pointer(&dst.Pix[0]))
	rgb.rowBytes = C.uint32_t(dst.Stride)

	if rc := C.avifImageYUVToRGB(dec.image, &rgb); rc != C.AVIF_RESULT_OK {
		return nil, fmt.Errorf("decoder/avif: YUVToRGB rc=%d", rc)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
```

Create `decoder/avif/avif_nocgo.go`:

```go
//go:build !cgo || nocgo || noavif

package avif

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "avif" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{60001} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/avif: requires cgo + libavif (rebuild without -tags noavif or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/avif/avif_nocgo_test.go`:

```go
//go:build !cgo || nocgo || noavif

package avif

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("avif")
	if !ok {
		t.Fatalf("avif stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0x00, 0x00, 0x00, 0x18}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/avif/ -v`

Run with tag: `go test -tags noavif ./decoder/avif/ -v`

Expected: both PASS.

If libavif function signatures differ, cross-reference https://github.com/AOMediaCodec/libavif decode example.

- [ ] **Step 5: Commit**

```bash
git add decoder/avif/
git commit -m "feat(decoder): avif — libavif decoder + nocgo/noavif stub"
```

---

## Task 1.13: `decoder/webp` — libwebp wrapper (new cgo decoder)

**Files:**
- Create: `decoder/webp/webp_cgo.go`
- Create: `decoder/webp/webp_nocgo.go`
- Create: `decoder/webp/webp_test.go`
- Create: `decoder/webp/webp_nocgo_test.go`

**Background:** Greenfield. Reference: `~/GitHub/wsitools/internal/codec/webp/webp.go` for the cgo setup. libwebp's decoder API is simple: `WebPDecodeRGBInto` / `WebPDecodeRGBAInto` write directly into a caller-supplied buffer. TIFF Compression tag: `50001` (libtiff convention).

- [ ] **Step 1: Write failing tests**

Create `decoder/webp/webp_test.go`:

```go
//go:build cgo && !nocgo && !nowebp

package webp

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("webp")
	if !ok {
		t.Fatalf("webp decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 50001 {
		t.Errorf("TIFFCompressionTags: got %v want [50001]", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/webp/...`

Expected: compile error.

- [ ] **Step 3: Implement webp_cgo.go**

Create `decoder/webp/webp_cgo.go`:

```go
//go:build cgo && !nocgo && !nowebp

// Package webp implements the WebP decoder via libwebp.
// TIFF Compression=50001 (libtiff convention).
package webp

/*
#cgo pkg-config: libwebp
#include <webp/decode.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "webp" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{50001} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/webp: scale not supported: %w", decoder.ErrUnsupportedScale)
	}
	if d.closed {
		return nil, fmt.Errorf("decoder/webp: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/webp: empty src: %w", decoder.ErrCorruptInput)
	}

	var w, h C.int
	if C.WebPGetInfo(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		&w, &h) == 0 {
		return nil, fmt.Errorf("decoder/webp: WebPGetInfo (corrupt input): %w", decoder.ErrCorruptInput)
	}

	var dst *decoder.Image
	if opts.Dst != nil {
		if opts.Dst.Width != int(w) || opts.Dst.Height != int(h) {
			return nil, fmt.Errorf("decoder/webp: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, int(w), int(h), decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	} else {
		dst = decoder.NewImageFormat(int(w), int(h), opts.Format)
	}

	var ok *C.uint8_t
	if opts.Format == decoder.PixelFormatRGBA {
		ok = C.WebPDecodeRGBAInto(
			(*C.uint8_t)(unsafe.Pointer(&src[0])),
			C.size_t(len(src)),
			(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
			C.size_t(len(dst.Pix)),
			C.int(dst.Stride))
	} else {
		ok = C.WebPDecodeRGBInto(
			(*C.uint8_t)(unsafe.Pointer(&src[0])),
			C.size_t(len(src)),
			(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
			C.size_t(len(dst.Pix)),
			C.int(dst.Stride))
	}
	if ok == nil {
		return nil, fmt.Errorf("decoder/webp: decode failed: %w", decoder.ErrCorruptInput)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
```

Create `decoder/webp/webp_nocgo.go`:

```go
//go:build !cgo || nocgo || nowebp

package webp

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "webp" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{50001} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/webp: requires cgo + libwebp (rebuild without -tags nowebp or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/webp/webp_nocgo_test.go`:

```go
//go:build !cgo || nocgo || nowebp

package webp

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("webp")
	if !ok {
		t.Fatalf("webp stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte("RIFF"), decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/webp/ -v`

Run with tag: `go test -tags nowebp ./decoder/webp/ -v`

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/webp/
git commit -m "feat(decoder): webp — libwebp decoder + nocgo/nowebp stub"
```

---

## Task 1.14: `decoder/htj2k` — openjphjs wrapper (new cgo decoder)

**Files:**
- Create: `decoder/htj2k/htj2k_cgo.go`
- Create: `decoder/htj2k/htj2k_nocgo.go`
- Create: `decoder/htj2k/htj2k_test.go`
- Create: `decoder/htj2k/htj2k_nocgo_test.go`
- Optional: `decoder/htj2k/shim.cpp` if openjphjs requires a C++ shim like wsitools' encoder uses

**Background:** Greenfield. Reference: `~/GitHub/wsitools/internal/codec/htj2k/htj2k.go` and `shim.cpp`. openjphjs has a C++ API; wsitools uses a `.cpp` shim that exposes a C-callable surface. Mirror that pattern for decode. TIFF Compression tag: `60003` (wsi-tools private/experimental).

- [ ] **Step 1: Read the wsitools encoder + shim to understand the openjphjs binding pattern**

```bash
cat ~/GitHub/wsitools/internal/codec/htj2k/htj2k.go
cat ~/GitHub/wsitools/internal/codec/htj2k/shim.cpp
```

Identify: the C++ classes used (`ojph::codestream`, `ojph::param_*`), the C-callable shim function signatures, the pkg-config / compiler flags.

- [ ] **Step 2: Write failing tests**

Create `decoder/htj2k/htj2k_test.go`:

```go
//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatalf("htj2k decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 60003 {
		t.Errorf("TIFFCompressionTags: got %v want [60003]", got)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/htj2k/...`

Expected: compile error.

- [ ] **Step 4: Implement htj2k_cgo.go + shim.cpp**

Port the encoder's cgo binding shape. The decoder shim function in C++ would be roughly:

```cpp
// decoder/htj2k/shim.cpp
#include <openjph/ojph_codestream.h>
#include <openjph/ojph_mem.h>
extern "C" {

// decode_htj2k decodes a HTJ2K codestream into an RGB raster.
// Returns 0 on success, non-zero on error. Width/height returned via out params.
int decode_htj2k(const unsigned char *src, size_t src_len,
                 unsigned char *dst_rgb, size_t dst_stride,
                 int *out_w, int *out_h) {
    try {
        ojph::mem_infile in;
        in.open((char*)src, src_len);
        ojph::codestream cs;
        cs.read_headers(&in);
        ojph::param_siz siz = cs.access_siz();
        *out_w = siz.get_image_extent().x - siz.get_image_offset().x;
        *out_h = siz.get_image_extent().y - siz.get_image_offset().y;
        cs.create();
        // Read pixels into dst_rgb at dst_stride bytes per row...
        // (Implementation details depend on openjph API. Cross-reference
        // wsitools/internal/codec/htj2k/shim.cpp for the encoder side
        // and adapt.)
        return 0;
    } catch (...) {
        return 1;
    }
}

} // extern "C"
```

Create `decoder/htj2k/htj2k_cgo.go`:

```go
//go:build cgo && !nocgo && !nohtj2k

// Package htj2k implements the HTJ2K (High-Throughput JPEG 2000)
// decoder via openjph (https://github.com/aous72/OpenJPH).
// TIFF Compression=60003 (wsi-tools private/experimental).
package htj2k

/*
#cgo pkg-config: openjph
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "htj2k" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{60003} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/htj2k: scale not supported: %w", decoder.ErrUnsupportedScale)
	}
	if d.closed {
		return nil, fmt.Errorf("decoder/htj2k: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/htj2k: empty src: %w", decoder.ErrCorruptInput)
	}
	if opts.Format == decoder.PixelFormatRGBA {
		// openjph decode shim only emits RGB; the slide layer can pad to RGBA if needed.
		return nil, fmt.Errorf("decoder/htj2k: RGBA output not supported: %w", decoder.ErrUnsupportedFormat)
	}

	// Probe dimensions via a single-call shim that returns size + decoded pixels.
	// Two-pass would be cleaner; using single-pass for now.
	tmp := decoder.NewImage(1, 1) // placeholder; the shim writes after returning dims
	_ = tmp
	// Real impl: first call probes header to get dims, second call decodes into Dst.
	// Or: shim takes a Dst with pre-allocated capacity and returns final dims.
	return nil, fmt.Errorf("decoder/htj2k: shim impl TBD — port from wsitools/internal/codec/htj2k")
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
```

**This task is the highest-complexity one in the plan.** openjph's C++ API requires careful shim design. Allocate ~1 day for the cgo + shim implementation. Look at openjphjs decoder examples upstream for the decode-side header probe + raster output pattern.

Create `decoder/htj2k/htj2k_nocgo.go`:

```go
//go:build !cgo || nocgo || nohtj2k

package htj2k

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "htj2k" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{60003} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/htj2k: requires cgo + openjph (rebuild without -tags nohtj2k or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
```

Create `decoder/htj2k/htj2k_nocgo_test.go`:

```go
//go:build !cgo || nocgo || nohtj2k

package htj2k

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatalf("htj2k stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x4F, 0xFF, 0x51}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/htj2k/ -v`

Run with tag: `go test -tags nohtj2k ./decoder/htj2k/ -v`

Expected: TestRegistered + TestStubReturnsUnavailable both pass. Decode tests deferred to wsitools' golden-master pass.

If you can't complete the cgo decode shim in the allotted time, report DONE_WITH_CONCERNS noting the shim needs more work; the stub-on-default-build still ships and unblocks downstream tasks.

- [ ] **Step 6: Commit**

```bash
git add decoder/htj2k/
git commit -m "feat(decoder): htj2k — openjph decoder + shim + nocgo/nohtj2k stub"
```

---

## Task 1.15: `decoder/all` — blanket import

**Files:**
- Create: `decoder/all/all.go`

- [ ] **Step 1: Create the blanket import**

Create `decoder/all/all.go`:

```go
// Package all blank-imports every decoder subpackage so all codecs
// register at init() time. Most consumers wanting "every codec
// available" should blank-import this package:
//
//	import _ "github.com/wsilabs/opentile-go/decoder/all"
//
// Consumers wanting a smaller cgo footprint can blank-import only the
// codec subpackages they need.
package all

import (
	// Pure-Go decoders — always built.
	_ "github.com/wsilabs/opentile-go/decoder/deflate"
	_ "github.com/wsilabs/opentile-go/decoder/lzw"
	_ "github.com/wsilabs/opentile-go/decoder/none"

	// cgo decoders — register stubs when not built.
	_ "github.com/wsilabs/opentile-go/decoder/avif"
	_ "github.com/wsilabs/opentile-go/decoder/htj2k"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg2000"
	_ "github.com/wsilabs/opentile-go/decoder/jpegxl"
	_ "github.com/wsilabs/opentile-go/decoder/webp"
)
```

- [ ] **Step 2: Build to confirm**

Run: `cd /Users/cornish/GitHub/opentile-go && go build ./decoder/all/...`

Expected: success.

- [ ] **Step 3: Sanity-check that every decoder is registered**

Add an ad-hoc test in `decoder/all/all_test.go`:

```go
package all_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

func TestAllRegistered(t *testing.T) {
	want := []string{"none", "lzw", "deflate", "jpeg", "jpeg2000", "jpegxl", "avif", "webp", "htj2k"}
	registered := decoder.Registered()
	for _, name := range want {
		found := false
		for _, r := range registered {
			if r == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing decoder: %q (registered: %v)", name, registered)
		}
	}
}
```

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./decoder/all/ -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add decoder/all/
git commit -m "feat(decoder): all — blanket side-effect import of every codec"
```

---

## Task 1.16: `resample/` package — Kernel + Image + ImageInto

**Files:**
- Create: `resample/doc.go`
- Create: `resample/resample.go`
- Create: `resample/area.go`
- Create: `resample/lanczos.go`
- Create: `resample/nearest.go`
- Create: `resample/bilinear.go`
- Create: `resample/area_test.go`
- Create: `resample/lanczos_test.go`
- Create: `resample/nearest_test.go`
- Create: `resample/bilinear_test.go`

**Background:** Port `~/GitHub/wsitools/internal/resample/area.go` and `lanczos.go`. Add Nearest + Bilinear (new). All pure Go, no cgo.

- [ ] **Step 1: Create scaffold**

Create `resample/doc.go`:

```go
// Package resample provides pure-Go pixel resamplers operating on
// decoder.Image. Used by the v1.0 Slide.ScaledStrips iterator (when it
// lands) and exposed as standalone primitives for ad-hoc callers.
//
// Kernels: Nearest (cheap, ugly for downsampling), Bilinear, Lanczos
// (best for arbitrary ratios), Box (area-averaging, best for integer
// downsampling).
//
// Future Go-assembly acceleration is possible but out of scope for
// v0.22; the public API stays stable.
package resample
```

Create `resample/resample.go`:

```go
package resample

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

type Kernel int

const (
	Nearest Kernel = iota
	Bilinear
	Lanczos
	Box
)

// Image returns a freshly-allocated Image at the requested output
// dimensions, resampled from src using kernel k. The output format
// matches src.Format.
func Image(src *decoder.Image, outW, outH int, k Kernel) *decoder.Image {
	dst := decoder.NewImageFormat(outW, outH, src.Format)
	_ = ImageInto(src, dst, k) // can't fail for matched formats
	return dst
}

// ImageInto writes the resampled output into dst (dimensions
// determined by dst). dst.Format must match src.Format.
func ImageInto(src, dst *decoder.Image, k Kernel) error {
	if src.Format != dst.Format {
		return fmt.Errorf("resample: format mismatch: src=%d dst=%d", src.Format, dst.Format)
	}
	switch k {
	case Nearest:
		return nearestInto(src, dst)
	case Bilinear:
		return bilinearInto(src, dst)
	case Lanczos:
		return lanczosInto(src, dst)
	case Box:
		return boxInto(src, dst)
	default:
		return fmt.Errorf("resample: unknown kernel %d", k)
	}
}
```

- [ ] **Step 2: Port the Box (area-averaging) kernel from wsitools**

Read `~/GitHub/wsitools/internal/resample/area.go`. Adapt to operate on `*decoder.Image` (Width/Height/Stride/Pix) instead of whatever shape it uses today.

Create `resample/area.go`:

```go
package resample

import "github.com/wsilabs/opentile-go/decoder"

// boxInto resamples src into dst using area-averaging (box filter).
// Best-quality fast downsample for integer ratios (2x, 4x, 8x);
// acceptable for arbitrary ratios. For upscaling, falls through to
// nearest-neighbor behavior.
func boxInto(src, dst *decoder.Image) error {
	// Port verbatim from wsitools/internal/resample/area.go. Adapt
	// pixel-access to use src.Stride / dst.Stride explicitly and to
	// handle PixelFormatRGB (3 bytes/pixel) vs PixelFormatRGBA (4
	// bytes/pixel). Pixel bytes per pixel:
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	_ = bpp

	// Inner loop (sketch — port from wsitools):
	for dy := 0; dy < dst.Height; dy++ {
		// compute the y-range of src rows that contribute to this dst row
		// for each dst x, accumulate src pixels in that area and average
		// write into dst.Pix[dy*dst.Stride + dx*bpp + 0..bpp-1]
	}
	return nil // placeholder until ported
}
```

Then port the actual algorithm from wsitools' `area.go`. The structure stays the same; only the buffer-access plumbing differs.

- [ ] **Step 3: Port the Lanczos kernel from wsitools**

Read `~/GitHub/wsitools/internal/resample/lanczos.go`. Same adaptation pattern.

Create `resample/lanczos.go`:

```go
package resample

import "github.com/wsilabs/opentile-go/decoder"

// lanczosInto resamples src into dst using Lanczos resampling with
// a=3. Best quality for arbitrary downsampling ratios; more expensive
// than Box.
func lanczosInto(src, dst *decoder.Image) error {
	// Port from wsitools/internal/resample/lanczos.go.
	return nil
}
```

Port the algorithm.

- [ ] **Step 4: Write Nearest + Bilinear kernels (new — not in wsitools)**

Create `resample/nearest.go`:

```go
package resample

import "github.com/wsilabs/opentile-go/decoder"

func nearestInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	for dy := 0; dy < dst.Height; dy++ {
		sy := dy * src.Height / dst.Height
		if sy >= src.Height {
			sy = src.Height - 1
		}
		for dx := 0; dx < dst.Width; dx++ {
			sx := dx * src.Width / dst.Width
			if sx >= src.Width {
				sx = src.Width - 1
			}
			srcOff := sy*src.Stride + sx*bpp
			dstOff := dy*dst.Stride + dx*bpp
			copy(dst.Pix[dstOff:dstOff+bpp], src.Pix[srcOff:srcOff+bpp])
		}
	}
	return nil
}
```

Create `resample/bilinear.go`:

```go
package resample

import "github.com/wsilabs/opentile-go/decoder"

func bilinearInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	sx := float64(src.Width) / float64(dst.Width)
	sy := float64(src.Height) / float64(dst.Height)
	for dy := 0; dy < dst.Height; dy++ {
		fy := (float64(dy) + 0.5) * sy - 0.5
		y0 := int(fy)
		if y0 < 0 {
			y0 = 0
		}
		y1 := y0 + 1
		if y1 >= src.Height {
			y1 = src.Height - 1
		}
		wy := fy - float64(y0)
		for dx := 0; dx < dst.Width; dx++ {
			fx := (float64(dx) + 0.5) * sx - 0.5
			x0 := int(fx)
			if x0 < 0 {
				x0 = 0
			}
			x1 := x0 + 1
			if x1 >= src.Width {
				x1 = src.Width - 1
			}
			wx := fx - float64(x0)

			for c := 0; c < bpp; c++ {
				p00 := float64(src.Pix[y0*src.Stride+x0*bpp+c])
				p10 := float64(src.Pix[y0*src.Stride+x1*bpp+c])
				p01 := float64(src.Pix[y1*src.Stride+x0*bpp+c])
				p11 := float64(src.Pix[y1*src.Stride+x1*bpp+c])
				v := (1-wy)*((1-wx)*p00+wx*p10) + wy*((1-wx)*p01+wx*p11)
				dst.Pix[dy*dst.Stride+dx*bpp+c] = byte(v + 0.5)
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Port wsitools area_test.go**

Read `~/GitHub/wsitools/internal/resample/area_test.go` and create `resample/area_test.go` with the same test cases, adapted for the new `*decoder.Image` shape.

- [ ] **Step 6: Write a basic identity-resample test for every kernel**

Create `resample/nearest_test.go`, `resample/bilinear_test.go`, `resample/lanczos_test.go` each with a simple identity test:

```go
package resample

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestNearestIdentity(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	dst := decoder.NewImage(4, 4) // identity = same size
	if err := ImageInto(src, dst, Nearest); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src.Pix, dst.Pix) {
		t.Errorf("identity resample changed pixels")
	}
}
```

Equivalent tests for Bilinear, Lanczos, Box in their respective `_test.go` files.

- [ ] **Step 7: Run all resample tests**

Run: `cd /Users/cornish/GitHub/opentile-go && go test ./resample/ -v`

Expected: all tests PASS, including the wsitools-ported area tests.

- [ ] **Step 8: Commit**

```bash
git add resample/
git commit -m "feat(resample): pure-Go Nearest/Bilinear/Lanczos/Box kernels on decoder.Image"
```

---

## Task 1.17: CHANGELOG, README, version tag

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (optional, but recommended)

- [ ] **Step 1: Add v0.22.0 CHANGELOG entry**

Read the current `CHANGELOG.md` head. Add an entry above the existing latest version block:

```markdown
## [0.22.0] — 2026-05-23

Decoder + resample lift from wsitools. Adds the read-side codec layer
that opentile-go v1.0's `*Slide` decoded-pixel methods will consume.
Pure addition — no public API change, no behavior change for existing
consumers.

### Added

- New `decoder/` package: public `Decoder` interface, `DecodeOptions`,
  `Factory` interface, registry (`Register`, `Get`, `GetByCompressionTag`,
  `Registered`), and `Image` + `PixelFormat` value types.
- 9 codec subpackages registering against the registry at `init()`:
  - Pure-Go: `decoder/none`, `decoder/lzw`, `decoder/deflate`.
  - cgo: `decoder/jpeg` (libjpeg-turbo, with IDCT-time scale factor),
    `decoder/jpeg2000` (openjp2), `decoder/jpegxl` (libjxl),
    `decoder/avif` (libavif), `decoder/webp` (libwebp), `decoder/htj2k`
    (openjph).
- `decoder/all` — blanket side-effect import for "every codec
  available."
- `resample/` package: pure-Go Nearest, Bilinear, Lanczos, and Box
  (area-averaging) resamplers operating on `decoder.Image`.
- Per-codec build-tag opt-outs (`nojxl`, `noavif`, `nowebp`, `nohtj2k`)
  + master `nocgo`. Disabled codecs register a stub that returns
  `decoder.ErrCodecUnavailable` with a precise rebuild diagnostic.

### Unchanged

- All format readers (`formats/svs/`, `formats/philipstiff/`, etc.) and
  the public `Tiler` interface.
- `internal/jpegturbo/`, `internal/tifflzw/`, `internal/jpeg/` —
  untouched.
```

- [ ] **Step 2: Add link references to the link block at the bottom of CHANGELOG.md**

Find the link block (e.g., `[0.21.0]: https://...`) and add:

```markdown
[0.22.0]: https://github.com/WSILabs/opentile-go/releases/tag/v0.22.0
```

Update the `[Unreleased]` line if present to compare against v0.22.0.

- [ ] **Step 3: (Optional) Update README.md to mention the new subpackages**

Search README.md for an "Imports" or "Usage" section and add a note about the new decoder subpackages with a code example:

```go
import (
    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
    _ "github.com/wsilabs/opentile-go/decoder/all"  // NEW: enables decoded-tile access in v1.0+
)
```

If the README doesn't have a natural spot, skip this step.

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/cornish/GitHub/opentile-go && go test -race -count=1 ./...`

Expected: every package PASSes. If any test fails, halt and investigate before tagging.

- [ ] **Step 5: Commit + tag + push**

```bash
cd /Users/cornish/GitHub/opentile-go
git add CHANGELOG.md README.md
git commit -m "docs: CHANGELOG v0.22.0 — decoder + resample lift"
git tag v0.22.0
git push origin main v0.22.0
```

- [ ] **Step 6: Create GH release**

```bash
gh release create v0.22.0 --title "v0.22.0 — decoder + resample lift" --notes "$(cat <<'EOF'
Adds the read-side codec layer (decoder + resample subpackages) that opentile-go v1.0's *Slide decoded-pixel methods will consume.

## Added

- **`decoder/`** package: public Decoder interface, DecodeOptions, Factory, registry, Image + PixelFormat value types.
- **9 codec subpackages** (`decoder/<codec>/`) registering via init():
  - Pure-Go: none, lzw, deflate.
  - cgo: jpeg, jpeg2000, jpegxl, avif, webp, htj2k.
- **`decoder/all/`** — blanket side-effect import.
- **`resample/`** package: Nearest / Bilinear / Lanczos / Box (area-averaging) on decoder.Image.
- **Per-codec build-tag opt-outs** + master nocgo with register-but-error stubs.

## Unchanged

Pure addition — no public API change, no behavior change for existing consumers. All format readers and the Tiler interface are untouched.

## Install

\`\`\`sh
go get github.com/wsilabs/opentile-go@v0.22.0
\`\`\`
EOF
)"
```

**End of Phase 1.** opentile-go v0.22.0 is now published. The wsitools port (Phase 2) is independent of Phase 1's release timing — wsitools can adopt v0.22.0 whenever ready.

---

# PHASE 2 — wsitools v0.9.0 (consumer port)

All Phase 2 tasks happen in `/Users/cornish/GitHub/wsitools` on branch `main`.

## Task 2.1: Capture pre-port golden hashes

**Files:**
- Modify: `docs/superpowers/golden-masters-v0.6.0-transcode.txt` (append new section)

- [ ] **Step 1: Confirm wsitools currently at v0.8.x with pre-port internal/decoder**

```bash
cd /Users/cornish/GitHub/wsitools
git status   # clean main
grep "Version" cmd/wsitools/version.go
```

Expected: `Version = "0.9.0-dev"` (or similar dev marker).

- [ ] **Step 2: Build the current binary**

```bash
cd /Users/cornish/GitHub/wsitools && make build
```

- [ ] **Step 3: Capture pre-port hashes**

```bash
cd /Users/cornish/GitHub/wsitools
GOLDEN=docs/superpowers/golden-masters-v0.6.0-transcode.txt

# Append a new section, don't overwrite.
cat >> "$GOLDEN" <<'EOF'

# === Pre-v0.9.0-port capture (decoder + resample lift) ===
# These hashes capture transcode + downsample output using the
# pre-port wsitools/internal/decoder + internal/resample packages.
# Post-port v0.9.0 output MUST match byte-for-byte.
EOF

SAMPLES=~/GitHub/opentile-go/sample_files
for f in "$SAMPLES/svs/CMU-1-Small-Region.svs" "$SAMPLES/svs/CMU-1.svs"; do
  tmp=$(mktemp -t svs.XXXXXX).svs
  ./bin/wsitools transcode --codec jpeg --container svs -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  transcode-svs   jpeg  $(basename "$f")  sha256:$hash" >> "$GOLDEN"
  rm "$tmp"
done

for f in "$SAMPLES/svs/CMU-1-Small-Region.svs" "$SAMPLES/svs/CMU-1.svs"; do
  tmp=$(mktemp -t tiff.XXXXXX).tiff
  ./bin/wsitools transcode --codec jpeg --container tiff -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  transcode-tiff  jpeg  $(basename "$f")  sha256:$hash" >> "$GOLDEN"
  rm "$tmp"
done

for f in "$SAMPLES/svs/CMU-1-Small-Region.svs"; do
  tmp=$(mktemp -t ds.XXXXXX).svs
  ./bin/wsitools downsample --factor 2 -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  downsample-2x   $(basename "$f")  sha256:$hash" >> "$GOLDEN"
  rm "$tmp"
done

cat "$GOLDEN" | tail -20
```

Expected: 5 new lines appended showing pre-port sha256 hashes.

- [ ] **Step 4: Commit golden capture**

```bash
git add docs/superpowers/golden-masters-v0.6.0-transcode.txt
git commit -m "test: capture pre-v0.9.0-port golden hashes (decoder + resample lift)"
```

---

## Task 2.2: Update wsitools to depend on opentile-go v0.22.0

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Bump the dependency**

```bash
cd /Users/cornish/GitHub/wsitools
go get github.com/wsilabs/opentile-go@v0.22.0
go mod tidy
```

- [ ] **Step 2: Verify go.mod line**

```bash
grep opentile-go go.mod
```

Expected: `github.com/wsilabs/opentile-go v0.22.0`.

- [ ] **Step 3: Build to confirm nothing breaks at the new version**

Run: `cd /Users/cornish/GitHub/wsitools && go build ./...`

Expected: success. The new opentile-go is purely additive at the public-API level; nothing in wsitools should break.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: bump opentile-go to v0.22.0 (adds decoder + resample subpackages)"
```

---

## Task 2.3: Add decoder/all blanket import in main.go

**Files:**
- Modify: `cmd/wsitools/main.go`

- [ ] **Step 1: Read the current main.go**

```bash
head -30 /Users/cornish/GitHub/wsitools/cmd/wsitools/main.go
```

Find the import block.

- [ ] **Step 2: Add the blanket decoder import**

Add `_ "github.com/wsilabs/opentile-go/decoder/all"` to the import block, alongside the existing `_ "github.com/wsilabs/wsitools/internal/codec/all"`. Suggested location: immediately after that line.

- [ ] **Step 3: Build to confirm**

Run: `cd /Users/cornish/GitHub/wsitools && go build ./cmd/wsitools/...`

Expected: success. Every decoder is now registered when wsitools starts.

- [ ] **Step 4: Commit**

```bash
git add cmd/wsitools/main.go
git commit -m "feat(wsitools): blanket-import opentile-go/decoder/all for all decoders"
```

---

## Task 2.4: Port transcode.go decoder call sites

**Files:**
- Modify: `cmd/wsitools/transcode.go`

- [ ] **Step 1: Survey current decoder usage**

```bash
cd /Users/cornish/GitHub/wsitools
grep -n "internal/decoder\|decoder\.NewJPEG\|decoder\.NewJPEG2000" cmd/wsitools/transcode.go
```

Find every call site. Typically:
- Import line: `"github.com/wsilabs/wsitools/internal/decoder"`
- Usage: `decoder.NewJPEG()`, `decoder.NewJPEG2000()`, or `pickDecoder(comp)` that returns one of them.
- Calls: `.DecodeTile(compressed, dst, scaleNum, scaleDen)` which returns `([]byte, error)`.

- [ ] **Step 2: Replace import**

In `cmd/wsitools/transcode.go`:

Change `"github.com/wsilabs/wsitools/internal/decoder"` to `"github.com/wsilabs/opentile-go/decoder"`.

- [ ] **Step 3: Replace `decoder.NewJPEG()` and `decoder.NewJPEG2000()` call patterns**

Find code like:

```go
dec := decoder.NewJPEG()
rgb, err := dec.DecodeTile(compressed, buf, scaleNum, scaleDen)
```

Replace with:

```go
factory, ok := decoder.Get("jpeg")  // or "jpeg2000"
if !ok {
    return fmt.Errorf("no jpeg decoder registered")
}
dec := factory.New()
defer dec.Close()
img, err := dec.Decode(compressed, decoder.DecodeOptions{
    Scale:  scaleNum,  // 1 if scaleNum/scaleDen is 1/1
    Format: decoder.PixelFormatRGB,
    Dst:    nil,       // or supply a pre-allocated *decoder.Image for buffer reuse
})
if err != nil {
    return err
}
rgb := img.Pix  // for PixelFormatRGB, Pix is the RGB byte slice
```

For sites that use `pickDecoder(compression Compression)` style:

```go
func pickDecoder(comp source.Compression) decoder.Decoder {
    switch comp {
    case source.CompressionJPEG:
        if f, ok := decoder.Get("jpeg"); ok {
            return f.New()
        }
    case source.CompressionJPEG2000:
        if f, ok := decoder.Get("jpeg2000"); ok {
            return f.New()
        }
    }
    return nil
}
```

Important: scale factor mapping. Old `scaleNum/scaleDen` represented IDCT scale as a ratio (1/1, 1/2, 1/4, 1/8). New API uses a single int (1, 2, 4, 8). When `scaleNum == 1 && scaleDen == N` map to `Scale: N`.

- [ ] **Step 4: Build to confirm**

Run: `cd /Users/cornish/GitHub/wsitools && go build ./cmd/wsitools/...`

Expected: success. If you see compile errors about missing types or methods, the call site changes are incomplete.

- [ ] **Step 5: Run unit tests on cmd/wsitools**

Run: `cd /Users/cornish/GitHub/wsitools && go test ./cmd/wsitools/ -run 'TestConvert|TestParseImageDescription|TestBuildSVS|TestSVS' -v -count=1`

Expected: PASS. (Integration tests against sample files come in Task 2.7.)

- [ ] **Step 6: Commit**

```bash
git add cmd/wsitools/transcode.go
git commit -m "refactor(transcode): port decoder call sites to opentile-go/decoder API"
```

---

## Task 2.5: Port downsample.go decoder call sites

**Files:**
- Modify: `cmd/wsitools/downsample.go`

Same pattern as Task 2.4 but on `downsample.go`.

- [ ] **Step 1: Survey current usage**

```bash
grep -n "internal/decoder\|decoder\.New" /Users/cornish/GitHub/wsitools/cmd/wsitools/downsample.go
```

- [ ] **Step 2: Replace import + call sites**

Apply the same translation rules as Task 2.4 Step 3 to every decoder call site in downsample.go.

- [ ] **Step 3: Build + unit tests**

Run: `cd /Users/cornish/GitHub/wsitools && go build ./... && go test ./cmd/wsitools/ -v -run 'TestMutate|TestParse|TestBuild' -count=1`

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add cmd/wsitools/downsample.go
git commit -m "refactor(downsample): port decoder call sites to opentile-go/decoder API"
```

---

## Task 2.6: Delete internal/decoder + internal/resample

**Files:**
- Delete: `internal/decoder/` (entire directory)
- Delete: `internal/resample/` (entire directory)

- [ ] **Step 1: Verify no remaining references**

```bash
cd /Users/cornish/GitHub/wsitools
grep -rn "internal/decoder\|internal/resample" --include="*.go" .
```

Expected: zero matches (or only matches inside `internal/decoder/` / `internal/resample/` themselves, which are about to be deleted).

If any match remains in files outside those directories, fix them BEFORE deleting (likely a missed call site in Task 2.4 or 2.5).

- [ ] **Step 2: Delete the directories**

```bash
cd /Users/cornish/GitHub/wsitools
find internal/decoder -mindepth 1 -delete
rmdir internal/decoder
find internal/resample -mindepth 1 -delete
rmdir internal/resample
```

- [ ] **Step 3: Build + test**

Run: `cd /Users/cornish/GitHub/wsitools && go build ./... && go test -count=1 ./internal/... ./cmd/wsitools/`

Expected: success. Anything other than the convert integration tests should pass.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: delete internal/decoder + internal/resample (now sourced from opentile-go v0.22)"
```

---

## Task 2.7: Run golden-master verification + extended integration tests

**Files:** none modified; verification only.

- [ ] **Step 1: Rebuild wsitools post-port**

Run: `cd /Users/cornish/GitHub/wsitools && make build`

- [ ] **Step 2: Recapture golden hashes**

```bash
cd /Users/cornish/GitHub/wsitools
SAMPLES=~/GitHub/opentile-go/sample_files
POST=/tmp/post-port-hashes.txt
> "$POST"

for f in "$SAMPLES/svs/CMU-1-Small-Region.svs" "$SAMPLES/svs/CMU-1.svs"; do
  tmp=$(mktemp -t svs.XXXXXX).svs
  ./bin/wsitools transcode --codec jpeg --container svs -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  transcode-svs   jpeg  $(basename "$f")  sha256:$hash" >> "$POST"
  rm "$tmp"
done
for f in "$SAMPLES/svs/CMU-1-Small-Region.svs" "$SAMPLES/svs/CMU-1.svs"; do
  tmp=$(mktemp -t tiff.XXXXXX).tiff
  ./bin/wsitools transcode --codec jpeg --container tiff -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  transcode-tiff  jpeg  $(basename "$f")  sha256:$hash" >> "$POST"
  rm "$tmp"
done
for f in "$SAMPLES/svs/CMU-1-Small-Region.svs"; do
  tmp=$(mktemp -t ds.XXXXXX).svs
  ./bin/wsitools downsample --factor 2 -o "$tmp" "$f" >/dev/null
  hash=$(shasum -a 256 "$tmp" | awk '{print $1}')
  echo "v0.9.0-pre  downsample-2x   $(basename "$f")  sha256:$hash" >> "$POST"
  rm "$tmp"
done

cat "$POST"
```

- [ ] **Step 3: Diff against the pre-port hashes**

```bash
cd /Users/cornish/GitHub/wsitools
GOLDEN=docs/superpowers/golden-masters-v0.6.0-transcode.txt
diff <(grep "v0.9.0-pre" "$GOLDEN") /tmp/post-port-hashes.txt
```

Expected: **no output** (byte-identical pre and post-port).

If any diff appears, halt the port. Bisect by reverting individual codec imports until the mismatch goes away to identify the offending decoder.

- [ ] **Step 4: Run convert integration tests (bit-exact tile copy)**

Run: `cd /Users/cornish/GitHub/wsitools && WSI_TOOLS_TESTDIR=$HOME/GitHub/opentile-go/sample_files go test -count=1 -timeout 600s ./cmd/wsitools/ -run TestConvert`

Expected: PASS. Convert doesn't actually use decoders (it's bit-exact tile copy), so this is mostly a sanity check that the convert path isn't broken.

- [ ] **Step 5: Update CHANGELOG with v0.9.0 acceptance note**

Append a comment to `docs/superpowers/golden-masters-v0.6.0-transcode.txt`:

```
# === Post-v0.9.0-port verification PASSED on YYYY-MM-DD ===
# All transcode + downsample output byte-identical to pre-port.
```

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/golden-masters-v0.6.0-transcode.txt
git commit -m "test: v0.9.0 golden-master verification passed (byte-identical post-port)"
```

---

## Task 2.8: CHANGELOG + version bump + tag wsitools v0.9.0

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `cmd/wsitools/version.go`

- [ ] **Step 1: Bump Version constant**

Edit `cmd/wsitools/version.go`:

```go
const Version = "0.9.0"
```

- [ ] **Step 2: Add v0.9.0 CHANGELOG entry**

Add to `CHANGELOG.md` above the v0.8.1 section:

```markdown
## [0.9.0] — 2026-05-23

Consumes opentile-go v0.22.0's new `decoder/` and `resample/`
subpackages. Deletes wsitools' own `internal/decoder` +
`internal/resample`; transcode + downsample now source decoders from
opentile-go. No behavior change — transcode + downsample output is
byte-identical to v0.8.1 (verified by golden-master hashes).

### Dependencies

- Bumped `github.com/wsilabs/opentile-go` to v0.22.0.

### Changed (internal)

- Deleted `internal/decoder/` (JPEG + JPEG 2000 decoders moved to
  `github.com/wsilabs/opentile-go/decoder/{jpeg,jpeg2000}`).
- Deleted `internal/resample/` (Lanczos + Box resamplers moved to
  `github.com/wsilabs/opentile-go/resample`).
- transcode.go + downsample.go updated to use the new decoder API
  (registry-based factory lookup; `*decoder.Image` return type instead
  of `[]byte`).
- `cmd/wsitools/main.go` now blank-imports
  `github.com/wsilabs/opentile-go/decoder/all` to register every codec.

### Unchanged

- All command-line surfaces (transcode, downsample, convert, info,
  dump-ifds, extract, hash, doctor, version).
- All output formats; output bytes byte-identical to v0.8.1.
- `internal/codec/` (encoders) unchanged.
```

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/cornish/GitHub/wsitools && go test -race -count=1 ./...`

Expected: all packages PASS. Some convert integration tests may ENOSPC on large samples; that's environmental, not a regression.

- [ ] **Step 4: Commit + tag + push**

```bash
cd /Users/cornish/GitHub/wsitools
git add CHANGELOG.md cmd/wsitools/version.go
git commit -m "release: bump Version to 0.9.0"
git tag v0.9.0
git push origin main v0.9.0
```

- [ ] **Step 5: Post-release bump to 0.10.0-dev**

Edit `cmd/wsitools/version.go`:

```go
const Version = "0.10.0-dev"
```

```bash
git add cmd/wsitools/version.go
git commit -m "post-release: bump Version to 0.10.0-dev"
git push origin main
```

- [ ] **Step 6: Create GH release**

```bash
gh release create v0.9.0 --title "v0.9.0 — adopt opentile-go v0.22 decoder + resample" --notes "$(cat <<'EOF'
Switches wsitools to consume opentile-go v0.22.0's new decoder + resample subpackages. Deletes wsitools' own internal/decoder + internal/resample. No behavior change — transcode + downsample output is byte-identical to v0.8.1 (verified by golden-master hashes).

## Dependencies

- Bumped \`github.com/wsilabs/opentile-go\` to v0.22.0.

## Internal

- Deleted \`internal/decoder/\` (sources now in \`opentile-go/decoder/{jpeg,jpeg2000}\`).
- Deleted \`internal/resample/\` (sources now in \`opentile-go/resample\`).
- transcode.go + downsample.go use registry-based decoder lookup with the new image-aware Decode signature.
- main.go blank-imports \`opentile-go/decoder/all\`.

## Install

\`\`\`sh
go install github.com/wsilabs/wsitools/cmd/wsitools@v0.9.0
\`\`\`
EOF
)"
```

**End of Phase 2.** wsitools v0.9.0 now depends on opentile-go v0.22.0 for decoder + resample. The refactor is complete; transcode/downsample/convert all behave identically to v0.8.1.

---

# Spec Coverage Self-Review

Mapping spec sections → implementing task(s):

| Spec section | Requirement | Task(s) |
|---|---|---|
| §2.1 / §3.1 | `decoder/` package + Image/PixelFormat types | 1.1, 1.2 |
| §3.2 | Decoder interface + DecodeOptions | 1.3 |
| §3.3 | Factory + Registry (Register/Get/GetByCompressionTag/Registered) | 1.4 |
| §3.4 | Sentinel errors | 1.5 |
| §2.1 (pure-Go decoders) | none / lzw / deflate | 1.6, 1.7, 1.8 |
| §2.1 (cgo decoders) | jpeg / jpeg2000 / jpegxl / avif / webp / htj2k | 1.9, 1.10, 1.11, 1.12, 1.13, 1.14 |
| §2.1 (blanket import) | decoder/all/ | 1.15 |
| §4 | resample/ subpackage | 1.16 |
| §5 | Build tags + cgo fallback | 1.6–1.14 (per-codec stub files) |
| §6.1 (Layer 1: unit tests) | Ported + new test files | 1.1–1.16 each task includes tests |
| §6.2 (Layer 2: golden masters) | Pre/post-port hash verification | 2.1, 2.7 |
| §7 (release sequencing) | opentile-go v0.22 then wsitools v0.9.0 | 1.17 (Phase 1 tag), 2.8 (Phase 2 tag) |
| §2.2 (wsitools side) | delete + re-import | 2.2, 2.3, 2.4, 2.5, 2.6 |
| §8.1 (CHANGELOG entries) | opentile-go + wsitools changelogs | 1.17, 2.8 |

No gaps identified.
