# opentile-go v0.22 — Decoder + Resample Lift Design

**Version:** v0.22.0
**Status:** Draft
**Date:** 2026-05-23

## 1. Goal & scope

Lift the decoder layer and pixel-resample primitives from `wsitools` into
`opentile-go`, where the in-progress v1.0 `*Slide` API
(`DecodedTile`, `ReadRegion`, `Thumbnail`, `ScaledStrips`) needs them as
internal infrastructure. After this work, opentile-go owns the read-side
codec layer; wsitools is purely the writer + CLI.

Ships as **opentile-go v0.22.0** — pure addition. No public API change,
no behavior change for current consumers. The new packages are fully
usable by third-party consumers immediately.

wsitools follows in a separate later release (**wsitools v0.9.0**) that
deletes its own `internal/decoder` + `internal/resample` and re-imports
from opentile-go.

### 1.1 In scope (opentile-go v0.22)

- New `opentile-go/decoder/` package — public Decoder interface,
  DecodeOptions, Factory, Registry. Plus the `Image` and `PixelFormat`
  types (small primitives, lifted forward from the v1.0 design so the
  decoder interface is image-aware from day one).
- New `opentile-go/decoder/<codec>/` subpackages — one per codec, each
  carrying `init()`-time registration:
  - Pure-Go decoders (always built): `none`, `lzw`, `deflate`.
  - cgo decoders: `jpeg`, `jpeg2000`, `jpegxl`, `avif`, `webp`, `htj2k`.
- New `opentile-go/decoder/all/` — blanket-imports every codec.
- New `opentile-go/resample/` — pure-Go area + Lanczos resamplers,
  lifted from `wsitools/internal/resample/`.
- Build-tag pattern: per-codec `no<codec>` + master `nocgo`, with stubs
  that register-but-error at decode time with a precise diagnostic.
- Tests: ported from wsitools' `internal/decoder/*_test.go` and
  `internal/resample/*_test.go`. Sample fixtures continue to live in
  `sample_files/` (gitignored, symlinked).

### 1.2 Out of scope for v0.22

- **Encoder lift** — encoders stay in `wsitools/internal/codec/`.
  opentile-go is read-only.
- **The `*Slide` API redesign** — that's v1.0, separate spec.
- **Strip iterator (`ScaledStrips`)** — also v1.0.
- **Performance work beyond exposing the JPEG IDCT scale-factor
  parameter** — the IDCT-scale parameter is already in the v0.22
  decoder interface; using it from a strip iterator is v1.0.
- **Quality-estimate metadata** (e.g., JPEG Q-factor in `Level`) —
  separate future spec.
- **Go-assembly-accelerated resample kernels** — separate future
  spec when profiling justifies it.

### 1.3 Out of scope for the wsitools follow-on (v0.9.0)

- Any behavior change to transcode / downsample / convert. They get
  new import paths and a re-routed decoder dependency; output stays
  byte-identical (verified by golden-master hashes).
- Any change to wsitools' encoder layout.

## 2. Package layout

### 2.1 Inside `opentile-go` (new additions)

```
decoder/                  -- public surface: Decoder, DecodeOptions, Factory, Registry,
                             Image + PixelFormat types
decoder/all/              -- side-effect imports every codec below

# Pure-Go decoders (always built, no build tags):
decoder/none/             -- TIFF Compression=1 (uncompressed)
decoder/lzw/              -- TIFF Compression=5 (uses internal/tifflzw)
decoder/deflate/          -- TIFF Compression=8 (stdlib compress/zlib)

# cgo decoders (per-codec opt-out + master nocgo, with register-but-error stubs):
decoder/jpeg/             -- libjpeg-turbo (no per-codec opt-out; foundational. honors nocgo.)
decoder/jpeg2000/         -- openjp2       (no per-codec opt-out; standard WSI codec. honors nocgo.)
decoder/jpegxl/           -- libjxl        (`-tags nojxl` opt-out or master nocgo)
decoder/avif/             -- libavif       (`-tags noavif` opt-out or master nocgo)
decoder/webp/             -- libwebp       (`-tags nowebp` opt-out or master nocgo)
decoder/htj2k/            -- openjphjs     (`-tags nohtj2k` opt-out or master nocgo)

resample/                 -- pure-Go Lanczos + Box (area) resamplers
```

**Notes on package shape:**

- The `decoder/` parent package exports the public Decoder interface,
  DecodeOptions, Factory, the Image / PixelFormat types, and a small
  Register/Get registry. No cgo here.
- Each `decoder/<codec>/` subpackage contains a single cgo wrapper
  (where applicable) plus an `init()` that registers the codec factory
  against the parent's registry. Build tags on the real impl; stubs
  for disabled builds.
- `decoder/all/` is the ergonomic "import everything" shortcut: a
  single blank-import line in a consumer's main.go covers every codec.
- `resample/` is pure Go, no cgo, no build tags. Standalone primitives
  operating on the public Image type from `decoder/`.

### 2.2 Inside `wsitools` (v0.9.0 follow-on)

```
internal/decoder/         -- DELETE (entire package)
internal/resample/        -- DELETE (entire package)

cmd/wsitools/transcode.go   -- swap internal/decoder imports → opentile-go/decoder
cmd/wsitools/downsample.go  -- same
cmd/wsitools/main.go        -- add blank-import: _ "opentile-go/decoder/all"

internal/codec/           -- UNCHANGED (encoders stay)
```

- The blanket `_ "github.com/wsilabs/opentile-go/decoder/all"` in the
  wsitools binary ensures every command has every decoder registered.
  The `internal/codec/all` import pattern already in place for encoders
  is the model.
- transcode + downsample need decoder calls; their call-site shape
  changes from `decoder.NewJPEG().DecodeTile(...)` to a registry lookup
  `decoder.Get("jpeg").New().Decode(...)`. Functionally identical,
  slightly cleaner.

### 2.3 Inside `opentile-go` (existing, unchanged)

- `internal/jpegturbo/` — narrow cgo for `tjTransform` (NDPI restart-
  marker reshuffle + Philips strip-to-tile splice). NOT replaced or
  duplicated by the new `decoder/jpeg` package. The two exist for
  different purposes: `jpegturbo` for lossless JPEG bitstream surgery,
  `decoder/jpeg` for full RGB decode. They share libjpeg-turbo at link
  time; that's it.
- `internal/tifflzw/` — pure-Go LZW (TIFF flavor) decoder. The new
  `decoder/lzw/` package wraps this as a registered Decoder.
- `internal/jpeg/` — pure-Go JPEG byte-manipulation helpers (DQT/DHT/
  SOF parsing). Not touched.

## 3. Decoder interface contract

The decoder layer's public surface is small. Three value types
(`Image`, `PixelFormat`, `DecodeOptions`), two interfaces (`Decoder`,
`Factory`), and a registry.

### 3.1 Image + PixelFormat

```go
package decoder

type PixelFormat int

const (
    PixelFormatRGB  PixelFormat = iota  // 3 bytes/pixel, no alpha (default)
    PixelFormatRGBA                      // 4 bytes/pixel, alpha = 0xFF
)

type Image struct {
    Width, Height int
    Stride        int          // bytes per row; can over-allocate for SIMD alignment
    Format        PixelFormat
    Pix           []byte       // len(Pix) == Stride * Height
}

func NewImage(w, h int) *Image                          // RGB
func NewImageFormat(w, h int, fmt PixelFormat) *Image
```

`Image` and `PixelFormat` live in the `decoder` package (not `opentile`
directly) because they're a decoder-layer concept. v1.0's `*Slide` will
return them from its methods; the slide layer doesn't need to redefine
the types.

### 3.2 Decode contract

```go
type DecodeOptions struct {
    // Scale is the IDCT-time scale factor (JPEG decoders only). Valid
    // values: 1, 2, 4, 8. Other values return ErrUnsupportedScale.
    // Non-JPEG decoders return ErrUnsupportedScale if Scale != 1 (they
    // can't IDCT-scale; caller is responsible for upstream/downstream
    // resample).
    Scale int

    // Format is the requested output pixel format. Decoders return
    // ErrUnsupportedFormat if they can't produce the requested format.
    // Today: PixelFormatRGB is universal; PixelFormatRGBA is also
    // universal since RGBA is just RGB with appended alpha=0xFF.
    Format PixelFormat

    // Dst is an optional caller-supplied destination. If nil, the
    // decoder allocates a fresh Image. If non-nil and its dimensions/
    // format match the decoded size, the decoder writes into Dst.Pix
    // and returns Dst. Mismatched dimensions return ErrDestinationSize.
    Dst *Image
}

type Decoder interface {
    // Decode compressed bytes into an Image at the requested options.
    // Always returns a fresh *Image when opts.Dst is nil; otherwise
    // returns opts.Dst after writing into its Pix.
    Decode(compressed []byte, opts DecodeOptions) (*Image, error)

    // Close releases any decoder state. Safe to call multiple times.
    // Decoders may pool their internal state (e.g., libjpeg-turbo
    // instances) and release on Close.
    Close() error
}
```

### 3.3 Factory + Registry

```go
type Factory interface {
    // Name is the canonical codec identifier (e.g., "jpeg", "jpeg2000",
    // "jpegxl", "webp", "avif", "htj2k", "lzw", "deflate", "none").
    // Used for registry lookup.
    Name() string

    // TIFFCompressionTags lists the TIFF Compression tag values this
    // factory's decoder handles. Multiple tags allowed (e.g., JPEG 2000
    // is both 33003 (Aperio) and 34712 (libtiff)). Empty for non-TIFF-
    // associated codecs (none of which exist today, but the API leaves
    // room).
    TIFFCompressionTags() []uint16

    // New returns a fresh Decoder instance. Each call returns a new
    // instance with its own internal state. Decoders are not safe for
    // concurrent use; callers concurrent on the same slide should
    // construct one Decoder per goroutine.
    New() Decoder
}

// Register adds a factory to the decoder registry. Called from each
// decoder subpackage's init(). Last-in-wins on name collision
// (intentional — lets consumers shadow a default decoder with a custom
// impl).
func Register(f Factory)

// Get returns the factory registered for the given codec name, or
// (nil, false) if none is registered.
func Get(name string) (Factory, bool)

// GetByCompressionTag returns the factory registered for the given
// TIFF Compression tag value, or (nil, false) if none is registered.
// Used by Slide.DecodedTile and AssociatedImage.Decoded (in v1.0).
func GetByCompressionTag(tag uint16) (Factory, bool)

// Registered returns the canonical names of every registered decoder.
// Useful for diagnostic output ("doctor"-style commands).
func Registered() []string
```

### 3.4 Sentinel errors

```go
var (
    ErrCodecUnavailable  = errors.New("decoder: codec not available in this build")
    ErrUnsupportedScale  = errors.New("decoder: scale factor not supported by this codec")
    ErrUnsupportedFormat = errors.New("decoder: pixel format not supported by this codec")
    ErrDestinationSize   = errors.New("decoder: dst Image dimensions don't match decoded size")
    ErrCorruptInput      = errors.New("decoder: corrupt input data")
)
```

Specific decoders may wrap these with additional context. Callers use
`errors.Is` to check sentinels.

## 4. Resample subpackage

Single flat package, no subpackages. Pure Go, no cgo, no build tags.

```go
package resample

import "github.com/wsilabs/opentile-go/decoder"

type Kernel int

const (
    Nearest  Kernel = iota   // pixel-replicating; useful for label preview etc.
    Bilinear                 // linear interpolation; OK for moderate ratios
    Lanczos                  // a=3 by default; best quality for arbitrary ratios
    Box                      // area-averaging; fastest for integer downsampling
)

// Image returns a freshly-allocated Image at the requested output
// dimensions, resampled from src using kernel k. The output format
// matches src.Format. Use Box for power-of-2 downsampling in pyramid
// lifts (fastest); Lanczos for arbitrary-ratio downsampling where
// quality matters.
func Image(src *decoder.Image, outW, outH int, k Kernel) *decoder.Image

// ImageInto writes the resampled output into dst (whose dimensions
// determine output size). dst.Format must match src.Format. For buffer
// reuse across many resamples (strip iterator's inner loop), allocate
// once + reuse.
func ImageInto(src, dst *decoder.Image, k Kernel) error
```

### 4.1 Why these four kernels

- **Nearest** — cheap; right for upscaling label-like content that
  should stay crisp.
- **Bilinear** — middle ground; matches stdlib `image/draw`
  interpolators.
- **Lanczos** — what wsitools' `internal/resample/lanczos.go` provides;
  the quality default for arbitrary-ratio downsampling.
- **Box (area averaging)** — what wsitools' `internal/resample/area.go`
  provides; near-optimal quality for integer downsampling (2×, 4×, 8×)
  at much lower CPU than Lanczos.

The four cover the practical quality/speed tradeoffs without
proliferating choices. More kernels (Hamming, Catmull-Rom, etc.) added
later if a real consumer needs them.

### 4.2 On the `decoder.Image` import

The resample package operates on `decoder.Image` (the type defined in
the decoder package). That's a small downstream dependency from
resample → decoder. The alternative — defining `Image` at the top-level
`opentile` package — bloats the top-level package with what's really a
decoder concern and forces a circular layout if Slide also needs Image
methods. Keeping Image in `decoder/` and having resample reference it is
the cleanest split.

### 4.3 Future Go-assembly path

Pure Go is fine for v0.22. If profiling later shows the Lanczos kernel
is a bottleneck in dzsave/extract pipelines, a Go-assembly-accelerated
impl (per-arch: amd64 SSE4/AVX2/AVX-512, arm64 NEON) can be added
under per-arch assembly files (`lanczos_amd64.s`, `lanczos_arm64.s`)
with a pure-Go fallback for other arches. The public API stays the
same.

**Avoid binding libvips' resampler:** libvips is LGPL-2.1+, opentile-go
+ wsitools are Apache 2.0, and static-linking an LGPL library into an
Apache-licensed binary creates compliance friction. Go-assembly avoids
the licensing question entirely.

Out of scope for v0.22.

## 5. Build tags + cgo fallback details

The pattern is consistent across cgo decoders. JPEG (always-built among
the cgo group, foundational for WSI):

```go
// decoder/jpeg/jpeg_cgo.go
//go:build cgo && !nocgo

package jpeg

import "github.com/wsilabs/opentile-go/decoder"

// ... real cgo impl ...

func init() {
    decoder.Register(&factory{})
}

type factory struct{}
func (f *factory) Name() string                  { return "jpeg" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{7} }
func (f *factory) New() decoder.Decoder          { return newCGODecoder() }
```

```go
// decoder/jpeg/jpeg_nocgo.go
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
    return nil, fmt.Errorf("decoder: jpeg requires cgo + libjpeg-turbo (rebuild without CGO_ENABLED=0 / -tags nocgo): %w",
        decoder.ErrCodecUnavailable)
}
func (d *stubDecoder) Close() error { return nil }
```

For per-codec opt-out codecs (jpegxl, avif, webp, htj2k), the build
tags add the opt-out:

```go
// decoder/jpegxl/jpegxl_cgo.go
//go:build cgo && !nocgo && !nojxl

// decoder/jpegxl/jpegxl_nocgo.go
//go:build !cgo || nocgo || nojxl
```

The stub error message includes both opt-out paths:

```
"decoder: jpegxl requires cgo + libjxl (rebuild without -tags nojxl)"
```

For the pure-Go decoders (none, lzw, deflate), no build tags — single
file, always built, always registered.

**Cross-cutting:**

- Every codec subpackage exports nothing publicly except via `init()`.
  The Factory type is unexported. Consumers use the parent registry;
  they never call codec-package symbols directly.
- The blanket `decoder/all/` package is a one-file side-effect import:

```go
// decoder/all/all.go
package all

import (
    _ "github.com/wsilabs/opentile-go/decoder/none"
    _ "github.com/wsilabs/opentile-go/decoder/lzw"
    _ "github.com/wsilabs/opentile-go/decoder/deflate"
    _ "github.com/wsilabs/opentile-go/decoder/jpeg"
    _ "github.com/wsilabs/opentile-go/decoder/jpeg2000"
    _ "github.com/wsilabs/opentile-go/decoder/jpegxl"
    _ "github.com/wsilabs/opentile-go/decoder/webp"
    _ "github.com/wsilabs/opentile-go/decoder/avif"
    _ "github.com/wsilabs/opentile-go/decoder/htj2k"
)
```

## 6. Verification strategy

Two layers, building on the existing wsitools golden-master discipline
from v0.7.

### 6.1 Layer 1: ported unit tests pass at the new location

For each lifted package, port the wsitools test file:

| Source test | New location |
|---|---|
| `wsitools/internal/decoder/jpeg_test.go` | `decoder/jpeg/jpeg_test.go` |
| `wsitools/internal/decoder/jpeg2000_test.go` | `decoder/jpeg2000/jpeg2000_test.go` |
| `wsitools/internal/resample/area_test.go` | `resample/area_test.go` |
| (no existing test) | `resample/lanczos_test.go` (new — fills the gap) |

Plus new tests for the parent `decoder/` package:
- `decoder_test.go` — Register/Get/GetByCompressionTag round-trip with
  fake factories.
- `image_test.go` — NewImage/NewImageFormat dimensions, stride, format.
- `errors_test.go` — sentinel error wrapping (`errors.Is` cases for
  stub-decoder errors).

For each cgo-decoder package, both build paths get tests:
- `<codec>_test.go` runs against the real decoder under default
  `cgo && !no<codec>`.
- `<codec>_nocgo_test.go` (build tag `!cgo || nocgo || no<codec>`)
  verifies the stub returns `ErrCodecUnavailable` with the expected
  diagnostic message.

### 6.2 Layer 2: end-to-end golden-master verification at the wsitools port

Reuses the file at `wsitools/docs/superpowers/golden-masters-v0.6.0-transcode.txt`,
which lists transcode + downsample SHA-256 hashes for the CMU fixture.
The wsitools v0.9.0 port (which switches decoder imports to opentile-go)
MUST produce byte-identical output to v0.8.1.

Verification flow at wsitools v0.9.0 land time:

1. **Capture pre-port hashes.** Run the existing post-v0.8.1 wsitools
   binary against the standard fixtures; record hashes (these should
   already match the golden file).
2. **Make the port.** Switch wsitools' transcode + downsample to import
   from `opentile-go/decoder/*`. Delete `internal/decoder` +
   `internal/resample`.
3. **Recapture hashes.** Run the same fixtures through the post-port
   binary. Hashes MUST match the pre-port set byte-for-byte.
4. **If mismatch:** halt the port. The decoder lift introduced a
   regression. Bisect by reverting individual codec imports.

The golden master file gets a new comment block noting the v0.22/v0.9
verification pass.

### 6.3 On the interface shape change

The decoder interface is changing shape (was: `DecodeTile(compressed,
dst []byte, ...) ([]byte, error)`; now: `Decode(compressed,
DecodeOptions) (*Image, error)`). The wsitools transcode + downsample
call sites need updating to match. The output of those call sites — RGB
bytes flowing into the encoder — is what we hash-check.

At the call sites, the old code received `[]byte` from
`decoder.DecodeTile`. The new code receives an `*Image` from
`decoder.Decode`. The Image's `Pix []byte` slice IS the RGB bytes
(PixelFormatRGB has `len(Pix) == w*h*3` and stride = `w*3`). So the
call-site change is a tiny wrapping difference, not a semantic one.
Buffer reuse via `DecodeOptions.Dst` covers the `sync.Pool` patterns
wsitools uses today.

If the wrapping introduces any byte drift (it shouldn't), the golden
masters catch it immediately.

### 6.4 Pure-Go decoders verification

For none/lzw/deflate (NEW additions; LZW was internal-only in
opentile-go; the others didn't exist), there's no pre-existing wsitools
output to compare against. Unit tests cover correctness directly. The
pure-Go LZW decoder shares its impl with `internal/tifflzw`, which
already has tests — the new `decoder/lzw` package just adds the
Factory + Decoder wrapper.

## 7. Release sequencing

Two-phase release.

### 7.1 Phase 1 — opentile-go v0.22.0

opentile-go publishes the new decoder + resample packages. No public
API breakage; no existing-consumer behavior change. The new packages
are fully usable by third-party consumers immediately.

What ships in v0.22.0:
- `decoder/` (parent, with interface + registry + types).
- `decoder/<codec>/` (9 subpackages: none, lzw, deflate, jpeg,
  jpeg2000, jpegxl, avif, webp, htj2k).
- `decoder/all/`.
- `resample/`.
- `internal/jpegturbo/` — untouched (still narrow tjTransform wrapper).
- `internal/tifflzw/` — untouched (thin wrapper at `decoder/lzw/`
  references it).
- `internal/jpeg/` — untouched.
- All format readers — untouched.
- `Tiler` interface — untouched.

Tag: **v0.22.0**.

### 7.2 Phase 2 — wsitools v0.9.0

After opentile-go v0.22.0 is tagged, wsitools port:
- `go get github.com/wsilabs/opentile-go@v0.22.0`.
- Update `cmd/wsitools/main.go` to add
  `_ "github.com/wsilabs/opentile-go/decoder/all"`.
- Update transcode.go + downsample.go to use `decoder.Get("jpeg").New().Decode(...)`
  instead of `internal/decoder.NewJPEG().DecodeTile(...)`. Call-site
  shape changes from byte-bag to image-aware (`*Image`).
- Run golden-master verification (§6.2).
- Delete `internal/decoder/` + `internal/resample/`.
- Bump version to 0.9.0 (post-release bump back to 0.10.0-dev).

Tag: **wsitools v0.9.0**.

### 7.3 Decoupling consequences

- opentile-go v0.22.0 can release as soon as it's ready. Doesn't wait
  on wsitools.
- Third-party Go pathology code can adopt the decoders immediately at
  v0.22.0.
- The wsitools port can happen days, weeks, or months later.
- If the port surfaces a bug in opentile-go's decoder layer, opentile-
  go ships v0.22.1 as a patch; wsitools' port resumes against the
  corrected version.

No release notes coordination required. Each repo's CHANGELOG
describes its own changes.

## 8. Risk register + open questions

### 8.1 Risks

1. **Subtle cgo signature drift during the lift.** The decoder
   interface is changing shape (byte-bag → Image-aware). Most likely
   place for a regression: the buffer-reuse path. Mitigation: per-codec
   unit tests on both real and stub impls; golden masters at wsitools
   port.

2. **Build-tag complexity confuses users.** A user with
   `-tags 'nojxl nowebp'` building wsitools should still get jpeg +
   jpeg2000 + avif + htj2k decoders. The tag-composition behavior
   needs documenting in opentile-go's README + a `doctor`-style command
   on the wsitools side that reports which codecs are linked. The
   existing `wsitools doctor` already lists encoder availability;
   extend it to list decoder availability post-port.

3. **`decoder/lzw` and `internal/tifflzw` divergence.** The new public
   LZW decoder wraps the internal package. If the SVS reader updates
   its LZW usage in a future opentile-go release, `decoder/lzw` must
   follow or there's a public surface lag. Mitigation: `decoder/lzw`
   is a thin wrapper, so it stays in sync by construction.

4. **Cross-arch cgo regressions.** opentile-go currently only links
   libjpeg-turbo via cgo. The new decoders add 5 more system libraries
   to the linkage matrix. CI on darwin/arm64 + darwin/amd64 +
   linux/amd64 + linux/arm64 should cover them. Library packaging
   (Homebrew formulae for the cgo deps) needs to install cleanly on
   all four. Mitigation: a `make doctor` Make target that checks every
   system lib is present.

5. **wsitools v0.9.0 golden-master mismatch.** Post-port output should
   be byte-identical. If it isn't, the port has a bug. Mitigation:
   halt the port and bisect codec by codec.

### 8.2 Open questions deferred (not blockers for v0.22)

- **Quality-estimate metadata** (raised during brainstorm). Future
  spec.
- **The Go-assembly resample acceleration.** Future spec when
  profiling justifies it.
- **Per-decoder cache layer** for sync.Pool-style instance reuse —
  relevant for the v1.0 strip iterator, not for v0.22's port.
- **`decoder/lzw` as a public package vs. keeping it internal.**
  Current design promotes it to public. If a real reason emerges to
  keep it internal, the registry can still register it via
  `opentile/internal/lzw` with a thin public-Factory shim. Defer until
  v1.0 lands and we see usage patterns.

## 9. References

- v1.0 `*Slide` API surface (consumer of the decoder lift):
  `wsilabs/wsitools/docs/strategic-direction.md` §1.
- Existing wsitools codec/decoder/resample to be lifted:
  `wsilabs/wsitools/internal/codec/`, `internal/decoder/`,
  `internal/resample/`.
- opentile-go's existing narrow cgo precedent:
  `wsilabs/opentile-go/internal/jpegturbo/`.
- opentile-go's existing pure-Go LZW: `internal/tifflzw/`.
- v0.7 TIFF core extraction (model for refactor + golden-master
  verification): `wsilabs/wsitools/docs/superpowers/specs/2026-05-21-tiff-core-extraction-design.md`.
