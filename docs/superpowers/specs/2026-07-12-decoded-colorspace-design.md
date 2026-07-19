# Decoded-colorspace on `CodestreamInfo` — design (GH #112)

**Status:** approved design → this spec.
**Target release:** v0.62.0 (MINOR, additive — no breaking changes).

## 1. Problem

`decoder.CodestreamInfo` (from GH #41's header-only inspection) reports the
colorspace of the samples **as stored in the codestream** via
`ColorEncoding`: for JPEG 2000 that is `YBR_ICT` / `YBR_RCT` (the MCT), or
`RGB` when there is no decorrelating transform; for baseline JPEG it is
`YCbCr` (the JFIF storage convention).

Stored colorspace is what a *frame-copy* preserves. But a consumer that
instead **decodes** a tile through opentile needs a different fact: what
colorspace are the sample planes in when the decode library hands them back —
*before* opentile's own reader-side YCbCr→RGB normalization? That value is not
derivable from `ColorEncoding`, and today it lives only inside the jpeg2000
decoder's private `decodeIsYCbCr` (GH #53). GH #112 asks to surface it.

The two facts genuinely diverge — for example a raw Aperio-33003 JPEG 2000
tile: `ColorEncoding == RGB` (no MCT, no colorspace box → the stored-default
rule picks RGB), but the samples OpenJPEG returns are YCbCr and opentile must
convert them. A consumer choosing a DICOM `PhotometricInterpretation` for a
*re-decoded-then-re-encoded* tile, or reasoning about the pixel model it will
receive from `Decode`, needs the decoded value, not the stored one.

## 2. Scope

- Add one field, `DecodedColorSpace ColorEncoding`, to
  `decoder.CodestreamInfo`.
- Populate it in all four codecs that implement `CodestreamInspector`:
  **jpeg, jpeg2000, htj2k, jpegxl**. ("All-codecs" = exactly these four; the
  codecs without a codestream header — none/lzw/deflate/webp/avif — do not
  implement `CodestreamInspector` and are unaffected.)
- Unify the jpeg2000 decode-time YCbCr decision (`decodeIsYCbCr`) and the new
  inspect-time value onto **one** rule, so they cannot drift.

Out of scope:
- The `nocgo` / `nojp2k` jpeg2000 stub does not implement `Inspect`; not adding
  it here.
- No change to any `Decode` output bytes. `decodeIsYCbCr` is refactored to
  delegate to the shared rule but must remain behaviourally identical.
- No change to `ColorEncoding` (stored) semantics or any existing field.

## 3. Semantics of `DecodedColorSpace`

`DecodedColorSpace` is the colorspace of the component planes as they cross the
boundary from the decode library into opentile's decoder wrapper — i.e. the
colorspace opentile's `Decode` path treats the planes as, *before* any
reader-side YCbCr→RGB conversion opentile itself performs.

It only ever takes the **decoded-pixel subset** of `ColorEncoding`:
`ColorGrayscale`, `ColorRGB`, `ColorYCbCr`, or `ColorUnknown`. It never takes
`ColorYBRICT` / `ColorYBRRCT` — those are stored-codestream transforms that no
longer describe the samples once the codec has (or has not) inverted the MCT.

Per-codec derivation:

| Codec | 1 component | 3 components (color) | 4 components |
|-------|-------------|----------------------|--------------|
| **jpeg** | `Grayscale` | `RGB` (libjpeg-turbo converts YCbCr→RGB inside `Decode`) | `Unknown` (CMYK/YCCK) |
| **jpegxl** | `Grayscale` | `RGB` (libjxl outputs RGB) | `Unknown` |
| **jpeg2000** | `Grayscale` | jpeg2000 decode-policy rule (below) | (n/a in fixtures; falls out of the rule) |
| **htj2k** | `Grayscale` | `RGB` (opentile applies no chroma conversion; see caveat) | — |

**jpeg2000 decode-policy rule** (the existing `decodeIsYCbCr` truth table,
promoted to a `ColorEncoding` and extended with the grayscale case, keyed off
the parsed `internal/j2kheader.Info`):

1. `Components == 1` → `ColorGrayscale`.
2. MCT present (`h.MCT`) → `ColorRGB` — OpenJPEG applies the inverse MCT during
   decode, so the planes are already RGB.
3. Enumerated colorspace box `sRGB` → `ColorRGB`.
4. Enumerated colorspace box `sYCC` → `ColorYCbCr` — opentile's jpeg2000 decoder
   applies the YCbCr→RGB conversion (the `apply_ycbcr` path).
5. No MCT and no decisive box → `ColorYCbCr` — the Aperio-33003 convention (a
   raw J2K codestream carrying YCbCr with no signalling); opentile converts it.

This rule is *jpeg2000-decoder-specific policy*, not a generic codestream fact:
it encodes exactly which planes opentile's jpeg2000 `Decode` treats as YCbCr and
converts. It therefore lives in the `decoder/jpeg2000` package (§4.3), not in the
shared `internal/j2kheader`.

**Why htj2k is not the same rule (measured, not assumed):** opentile's htj2k
decoder performs **no** reader-side chroma conversion. openjph reconstructs the
stored component planes — inverting the MCT when the codestream carries one, or
passing components through when it does not (the htj2k test encoder sets
`set_color_transform(false)`) — and opentile packs whatever it gets as RGB. So
htj2k's decoded output is always RGB for colour input and Grayscale for a single
component, regardless of MCT/box signalling. A shared JP2K rule would mislabel
the common no-MCT htj2k codestream as YCbCr even though opentile emits correct
RGB. htj2k therefore reports `Grayscale`/`RGB` by component count only. (A
pathological no-MCT *sYCC* htj2k would be mis-decoded by opentile today — a
pre-existing gap unrelated to this field.)

## 4. Architecture

The jpeg2000 decode-policy rule is a single source of truth in the new
build-tag-free `decoder/jpeg2000/color.go`, shared between the decode-time
`decodeIsYCbCr` and the inspect-time `DecodedColorSpace` so the two cannot
drift. htj2k, jpeg, and jpegxl set the field from their own decode behaviour
directly in their `Inspect`. `internal/j2kheader` is unchanged.

### 4.1 `decoder/codestream.go`

Add the field to `CodestreamInfo`, documented per §3:

```go
	// DecodedColorSpace is the colorspace of the component planes the codec's
	// decode library hands back — before any reader-side YCbCr→RGB conversion
	// opentile itself performs. Unlike ColorEncoding (the stored codestream
	// colorspace, what a frame-copy preserves), this is what the pixels are in
	// once decoded: JPEG 2000 MCT codestreams report RGB (the library inverts
	// the MCT); an sYCC or unsignalled Aperio-33003 raw codestream reports
	// YCbCr (opentile converts it); JPEG and JPEG XL colour tiles report RGB
	// (their libraries convert); single-component tiles report Grayscale.
	//
	// It only ever takes the decoded-pixel subset — ColorGrayscale, ColorRGB,
	// ColorYCbCr, ColorUnknown — never the ColorYBRICT / ColorYBRRCT stored
	// transforms.
	DecodedColorSpace ColorEncoding
```

### 4.2 `decoder/jpeg2000/color.go` (new, build-tag-free)

Move `decodeIsYCbCr` out of the cgo-tagged `jp2_cgo.go` into a new
build-tag-free file so it is unit-testable under `nocgo`, and add the shared
`ColorEncoding`-valued rule. A header-based form avoids re-parsing when the
caller already has the parsed `Info`:

```go
package jpeg2000

import (
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/j2kheader"
)

// JP2 'colr' box enumerated colorspace values (mirrors the decoder's
// jp2EnumSRGB / jp2EnumSYCC).
const (
	jp2EnumSRGB = 16
	jp2EnumGray = 17
	jp2EnumSYCC = 18
)

// decodedColorSpaceFromHeader reports the colorspace of the planes OpenJPEG hands
// opentile's jpeg2000 decoder, before its YCbCr→RGB normalization. It is the
// decode-policy counterpart to the stored ColorEncoding: an MCT codestream
// decodes to RGB (OpenJPEG inverts the MCT); an sYCC box or an unsignalled raw
// codestream (the Aperio-33003 convention) is treated as YCbCr and converted; a
// single component is grayscale.
func decodedColorSpaceFromHeader(h j2kheader.Info) decoder.ColorEncoding {
	switch {
	case h.Components == 1:
		return decoder.ColorGrayscale
	case h.MCT:
		return decoder.ColorRGB // OpenJPEG already inverted the MCT
	}
	switch h.EnumColorspace {
	case jp2EnumSRGB:
		return decoder.ColorRGB
	case jp2EnumGray:
		return decoder.ColorGrayscale
	case jp2EnumSYCC:
		return decoder.ColorYCbCr
	}
	// No MCT, no decisive box: Aperio-33003 convention → YCbCr.
	return decoder.ColorYCbCr
}

// decodedColorSpace is the src-level form; on an unparseable header it falls back
// to the historical Aperio-33003 default (YCbCr), matching Decode.
func decodedColorSpace(src []byte) decoder.ColorEncoding {
	h, err := j2kheader.Parse(src)
	if err != nil {
		return decoder.ColorYCbCr
	}
	return decodedColorSpaceFromHeader(h)
}

// decodeIsYCbCr reports whether the decoded 3-component planes need a YCbCr→RGB
// conversion (GH #53). Single source of truth with DecodedColorSpace.
func decodeIsYCbCr(src []byte) bool {
	return decodedColorSpace(src) == decoder.ColorYCbCr
}
```

Delete the old `decodeIsYCbCr` and its `jp2EnumSRGB`/`jp2EnumSYCC` const block
from `jp2_cgo.go` (they move to `color.go`; `detectCodecFormat` and the C bridge
stay). The old truth table was `h.MCT→false`, `sRGB→false`, `sYCC→true`,
default→true, parse-error→true. The new path yields the same boolean for every
3-component input **that any real codestream produces** — with two deliberate,
harmless extensions the old table lacked:

- **1-component** input returns `false` (`Grayscale != YCbCr`), where the old
  table fell through to `true`. The C side ignores it: conversion is gated on
  `numcomps == 3`.
- A **3-component input carrying an enumerated *greyscale* colr box**
  (`EnumColorspace == 17`) now returns `false` (`Grayscale`), where the old
  table fell through to `true`. This is a self-contradictory header (3 planes
  labelled greyscale) that no real WSI fixture produces; the new value is
  actually *more* consistent — the stored-colorspace classifier
  (`j2kheader.CodestreamInfo`) already maps the same input to `ColorGrayscale`.

Neither extension changes the decode output of any real codestream, so the
refactor is byte-neutral in practice (pinned by `TestDecodeIsYCbCrParity` over
the fixtures and the existing decode/RGBA/MCT pixel tests).

### 4.3 `decoder/jpeg2000/jp2_cgo.go` — `Inspect`

Parse once, reuse the header for both stored and decoded colorspace:

```go
func (f *factory) Inspect(src []byte) (decoder.CodestreamInfo, error) {
	h, err := j2kheader.Parse(src)
	if err != nil {
		return decoder.CodestreamInfo{}, fmt.Errorf("decoder/jpeg2000: inspect: %w", decoder.ErrCorruptInput)
	}
	ci := h.CodestreamInfo()
	ci.DecodedColorSpace = decodedColorSpaceFromHeader(h)
	return ci, nil
}
```

### 4.4 `decoder/htj2k/htj2k_cgo.go` — `Inspect`

htj2k applies no reader-side chroma conversion, so its decoded output is RGB for
colour input and Grayscale for one component (§3). Set the field by component
count after `h.CodestreamInfo()`:

```go
	ci := h.CodestreamInfo()
	if ci.Components == 1 {
		ci.DecodedColorSpace = decoder.ColorGrayscale
	} else {
		ci.DecodedColorSpace = decoder.ColorRGB
	}
	return ci, nil
```

### 4.5 `decoder/jpeg/jpeg_cgo.go` — `Inspect`

Set `DecodedColorSpace` from the detected colorspace (libjpeg-turbo's `Decode`
emits RGB for any colour input, grayscale for `TJCS_GRAY`):

```go
	switch colorspace {
	case C.TJCS_GRAY:
		ci.Components, ci.ColorEncoding = 1, decoder.ColorGrayscale
		ci.DecodedColorSpace = decoder.ColorGrayscale
	case C.TJCS_RGB:
		ci.Components, ci.ColorEncoding = 3, decoder.ColorRGB
		ci.DecodedColorSpace = decoder.ColorRGB
	case C.TJCS_YCbCr:
		ci.Components, ci.ColorEncoding = 3, decoder.ColorYCbCr
		ci.DecodedColorSpace = decoder.ColorRGB // libjpeg converts YCbCr→RGB
	case C.TJCS_CMYK, C.TJCS_YCCK:
		ci.Components, ci.ColorEncoding = 4, decoder.ColorUnknown
		ci.DecodedColorSpace = decoder.ColorUnknown
	default:
		ci.Components, ci.ColorEncoding = 3, decoder.ColorUnknown
		ci.DecodedColorSpace = decoder.ColorUnknown
	}
```

### 4.6 `decoder/jpegxl/jxl_cgo.go` — `Inspect`

libjxl's `Decode` outputs RGB(A); mirror the existing colour-channel switch:

```go
	switch ch {
	case 1:
		ci.ColorEncoding = decoder.ColorGrayscale
		ci.DecodedColorSpace = decoder.ColorGrayscale
	case 3:
		ci.ColorEncoding = decoder.ColorRGB
		ci.DecodedColorSpace = decoder.ColorRGB
	default:
		ci.ColorEncoding = decoder.ColorUnknown
		ci.DecodedColorSpace = decoder.ColorUnknown
	}
```

## 5. Testing

All new assertions are additive to existing `*_inspect_test.go` files, plus one
new build-tag-free colour test.

1. **jpeg2000** (`jp2_inspect_test.go`, extend the table): add a
   `decodedColor` column and assert per fixture —
   - `lowres_2levels.j2k` (MCT) → `DecodedColorSpace == ColorRGB`
     (diverges from stored `ColorYBRRCT`).
   - `subsampled_422_256.j2k` (no MCT, no box) → `ColorYCbCr`
     (diverges from stored `ColorRGB`; the Aperio default).
   - Add `aperio_33003_tile.j2k` (raw, no MCT/box) → `ColorYCbCr`.
   - Add `rgb_mct_solid.j2k` (MCT) → `ColorRGB`.

2. **htj2k** (`htj2k_inspect_test.go`): the existing synthetic RGB fixture is a
   no-MCT raw codestream (encoder uses `set_color_transform(false)`); assert
   `DecodedColorSpace == ColorRGB` (component-count rule, not the JP2K
   decode-policy rule — this is the case that proves htj2k must differ from
   jpeg2000). A single-component fixture, if added, → `ColorGrayscale`.

3. **jpeg** (`jpeg_inspect_test.go`): a JFIF colour tile → stored
   `ColorEncoding == ColorYCbCr` **and** `DecodedColorSpace == ColorRGB` (the
   headline divergence); grayscale JPEG → both `ColorGrayscale`.

4. **jpegxl** (`jxl_inspect_test.go`): colour tile → `DecodedColorSpace ==
   ColorRGB`; grayscale → `ColorGrayscale`.

5. **jpeg2000 decode-policy rule** (new `color_test.go`, build-tag-free, in
   package `jpeg2000`): unit-test `decodedColorSpaceFromHeader` /
   `decodedColorSpace` across the fixtures covering the rule branches — MCT
   fixtures (`lowres_2levels.j2k`, `rgb_mct_solid.j2k`) → `ColorRGB`; no-signal
   raw (`aperio_33003_tile.j2k`, `subsampled_422_256.j2k`) → `ColorYCbCr`; plus
   a synthetic sRGB/sYCC/grayscale header case if constructible from
   `j2kheader.Parse` on the fixtures. Also assert `decodeIsYCbCr` returns the
   historical value for each fixture (MCT→false, aperio_33003→true,
   subsampled_422→true), pinning the refactor as behaviour-preserving. This test
   runs under `nocgo` because `color.go` no longer needs cgo.

6. Existing jpeg2000 decode / RGBA / MCT pixel tests are unchanged and continue
   to guard the decoded output — the safety net proving the `decodeIsYCbCr`
   refactor is byte-neutral.

## 6. Correctness bar

- `make test` green under `-race`.
- `go test -tags nocgo ./...` builds and passes (new `color.go` /
  `color_test.go` are build-tag-free and must compile without cgo).
- Existing jpeg2000 decode, RGBA, and MCT pixel tests unchanged and green —
  proves the `decodeIsYCbCr` refactor is byte-neutral.
- `go vet ./...` clean.

## 7. Release

- Additive field, no breaking change → **v0.62.0** (MINOR).
- CHANGELOG entry under a new `## [0.62.0]` heading.
- Closes #112.
