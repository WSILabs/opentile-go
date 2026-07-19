# Decoded-colorspace on CodestreamInfo — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose a `DecodedColorSpace ColorEncoding` field on `decoder.CodestreamInfo`, populated by all four codestream-inspecting codecs (jpeg, jpeg2000, htj2k, jpegxl), reporting the colorspace of the planes the decode library hands back before opentile's reader-side YCbCr→RGB normalization.

**Architecture:** The jpeg2000 decode-policy rule (`decodeIsYCbCr`) is promoted into a shared, build-tag-free `decoder/jpeg2000/color.go` returning a `decoder.ColorEncoding`, and reused for both decode and inspect so they can't drift. htj2k (no reader-side conversion → RGB/Grayscale by component count), jpeg (libjpeg converts → RGB/Grayscale), and jpegxl (libjxl converts → RGB/Grayscale) set the field directly in their `Inspect`. Purely additive; no `Decode` output bytes change.

**Tech Stack:** Go 1.23+, cgo codec decoders (libjpeg-turbo / OpenJPEG / openjph / libjxl), pure-Go `internal/j2kheader`. Build tags: `cgo`, `nocgo`, `nojp2k`, `nojxl`, `nohtj2k`.

**Spec:** `docs/superpowers/specs/2026-07-12-decoded-colorspace-design.md`

---

### Task 1: Add the `DecodedColorSpace` field to `decoder.CodestreamInfo`

**Files:**
- Modify: `decoder/codestream.go` (add field after the `ColorEncoding` field, ~line 47)
- Test: `decoder/codestream_decoded_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `decoder/codestream_decoded_test.go`:

```go
package decoder

import "testing"

// The zero value of the new field must be ColorUnknown (iota 0), and it must be
// assignable from the decoded-pixel subset of ColorEncoding.
func TestDecodedColorSpaceField(t *testing.T) {
	var ci CodestreamInfo
	if ci.DecodedColorSpace != ColorUnknown {
		t.Errorf("zero-value DecodedColorSpace = %s, want unknown", ci.DecodedColorSpace)
	}
	ci.DecodedColorSpace = ColorRGB
	if ci.DecodedColorSpace != ColorRGB {
		t.Errorf("DecodedColorSpace = %s, want RGB", ci.DecodedColorSpace)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/ -run TestDecodedColorSpaceField -v`
Expected: FAIL — compile error `ci.DecodedColorSpace undefined (type CodestreamInfo has no field or method DecodedColorSpace)`.

- [ ] **Step 3: Add the field**

In `decoder/codestream.go`, insert the field immediately after the `ColorEncoding ColorEncoding` field (which ends at the line `ColorEncoding ColorEncoding`, right before the `// ChromaSubsampling ...` doc comment):

```go
	// DecodedColorSpace is the colorspace of the component planes the codec's
	// decode library hands back — before any reader-side YCbCr→RGB conversion
	// opentile itself performs. Unlike ColorEncoding (the stored codestream
	// colorspace, what a frame-copy preserves), this is what the pixels are in
	// once decoded: JPEG 2000 MCT codestreams report RGB (the library inverts
	// the MCT); an sYCC or unsignalled Aperio-33003 raw codestream reports
	// YCbCr (opentile converts it); JPEG and JPEG XL colour tiles, and htj2k,
	// report RGB (their libraries convert / opentile applies no chroma
	// conversion); single-component tiles report Grayscale.
	//
	// It only ever takes the decoded-pixel subset — ColorGrayscale, ColorRGB,
	// ColorYCbCr, ColorUnknown — never the ColorYBRICT / ColorYBRRCT stored
	// transforms.
	DecodedColorSpace ColorEncoding
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./decoder/ -run TestDecodedColorSpaceField -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/codestream.go decoder/codestream_decoded_test.go
git commit -m "feat(decoder): add DecodedColorSpace field to CodestreamInfo (#112)"
```

---

### Task 2: Extract the jpeg2000 decode-policy rule into build-tag-free `color.go`

Moves `decodeIsYCbCr` out of the cgo-tagged `jp2_cgo.go` into a new build-tag-free file, adds the `ColorEncoding`-valued rule, and unifies both onto one source of truth. No decode output changes.

**Files:**
- Create: `decoder/jpeg2000/color.go`
- Create: `decoder/jpeg2000/color_test.go`
- Modify: `decoder/jpeg2000/jp2_cgo.go` (delete old `decodeIsYCbCr` + the `jp2EnumSRGB`/`jp2EnumSYCC` const block at ~lines 302-336)

- [ ] **Step 1: Write the failing test**

Create `decoder/jpeg2000/color_test.go` (build-tag-free — no `//go:build` line):

```go
package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// decodedColorSpace maps each fixture to the colorspace OpenJPEG hands back
// (before the decoder's YCbCr→RGB normalization).
func TestDecodedColorSpace(t *testing.T) {
	for _, tc := range []struct {
		file string
		want decoder.ColorEncoding
	}{
		{"testdata/lowres_2levels.j2k", decoder.ColorRGB},       // MCT → RGB (library inverts)
		{"testdata/rgb_mct_solid.j2k", decoder.ColorRGB},        // MCT → RGB
		{"testdata/aperio_33003_tile.j2k", decoder.ColorYCbCr},  // raw, no MCT/box → Aperio YCbCr
		{"testdata/subsampled_422_256.j2k", decoder.ColorYCbCr}, // no MCT/box → YCbCr
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if got := decodedColorSpace(b); got != tc.want {
			t.Errorf("%s: decodedColorSpace = %s, want %s", tc.file, got, tc.want)
		}
	}
}

// decodeIsYCbCr must return the historical truth value for every fixture, pinning
// the refactor as behaviour-preserving (the decode path depends on it, GH #53).
func TestDecodeIsYCbCrParity(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"testdata/lowres_2levels.j2k", false},    // MCT
		{"testdata/rgb_mct_solid.j2k", false},     // MCT
		{"testdata/aperio_33003_tile.j2k", true},  // Aperio raw
		{"testdata/subsampled_422_256.j2k", true}, // no MCT/box
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeIsYCbCr(b); got != tc.want {
			t.Errorf("%s: decodeIsYCbCr = %v, want %v", tc.file, got, tc.want)
		}
	}
	// Unparseable header falls back to the Aperio default (YCbCr → true).
	if !decodeIsYCbCr([]byte{0xFF, 0xD8}) {
		t.Error("decodeIsYCbCr on garbage = false, want true (Aperio fallback)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/jpeg2000/ -run 'TestDecodedColorSpace|TestDecodeIsYCbCrParity' -v`
Expected: FAIL — compile error `undefined: decodedColorSpace` (and `decodeIsYCbCr` is still defined in `jp2_cgo.go` under cgo, but `decodedColorSpace` does not exist yet).

- [ ] **Step 3: Create `color.go`**

Create `decoder/jpeg2000/color.go` (NO build-tag line — must compile in every configuration):

```go
package jpeg2000

import (
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/j2kheader"
)

// JP2 'colr' box enumerated colorspace values.
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

- [ ] **Step 4: Delete the old `decodeIsYCbCr` + const block from `jp2_cgo.go`**

In `decoder/jpeg2000/jp2_cgo.go`, delete this entire block (the `// JP2 'colr' box enumerated colorspace values.` const declaration through the closing brace of `func decodeIsYCbCr`, currently ~lines 302-336):

```go
// JP2 'colr' box enumerated colorspace values.
const (
	jp2EnumSRGB = 16
	jp2EnumSYCC = 18
)

// decodeIsYCbCr reports whether the decoded 3-component planes need a
// YCbCr->RGB conversion, decided from the codestream rather than a blanket
// assumption (GH #53). OpenJPEG applies the inverse multiple-component
// transform (MCT) during decode, so an MCT codestream's components are already
// RGB. The decoder therefore treats 3-component data as YCbCr only on a
// positive signal: an sYCC JP2 colorspace box, or — the Aperio 33003 convention
// — a raw codestream with no MCT and no (decisive) colorspace box. An MCT
// codestream or an explicit sRGB box is RGB and needs no conversion.
func decodeIsYCbCr(src []byte) bool {
	h, err := j2kheader.Parse(src)
	if err != nil {
		// Unparseable header: fall back to the historical default (treat
		// 3-component data as YCbCr), preserving Aperio 33003 behavior.
		return true
	}
	if h.MCT {
		return false // OpenJPEG already inverted the MCT -> RGB
	}
	switch h.EnumColorspace {
	case jp2EnumSRGB:
		return false // explicit RGB
	case jp2EnumSYCC:
		return true // explicit YCbCr
	}
	// No MCT and no decisive colorspace box: ambiguous. Default to YCbCr to
	// preserve the Aperio 33003 convention (raw J2K, YCbCr, no MCT, no box).
	// A standard RGB encoder uses MCT or carries an sRGB box, both handled above.
	return true
}
```

Leave `detectCodecFormat` (just above it) and everything below (`cgoDecoder`, `Decode`) untouched. `Decode`'s existing call to `decodeIsYCbCr(src)` now resolves to the `color.go` definition in the same package.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./decoder/jpeg2000/ -run 'TestDecodedColorSpace|TestDecodeIsYCbCrParity' -v`
Expected: PASS (both tests).

Also verify it builds and the parity test passes without cgo (color.go must be build-tag-free):
Run: `go test -tags nocgo ./decoder/jpeg2000/ -run 'TestDecodedColorSpace|TestDecodeIsYCbCrParity' -v`
Expected: PASS.

- [ ] **Step 6: Run the full jpeg2000 package tests to prove the decode path is byte-neutral**

Run: `go test ./decoder/jpeg2000/ -race`
Expected: PASS — existing decode / RGBA / MCT / subsampled tests unchanged and green.

- [ ] **Step 7: Commit**

```bash
git add decoder/jpeg2000/color.go decoder/jpeg2000/color_test.go decoder/jpeg2000/jp2_cgo.go
git commit -m "refactor(jpeg2000): unify decodeIsYCbCr + decoded-colorspace rule in color.go (#112)"
```

---

### Task 3: Populate `DecodedColorSpace` in the jpeg2000 `Inspect`

**Files:**
- Modify: `decoder/jpeg2000/jp2_cgo.go` (`Inspect`, ~lines 282-288)
- Test: `decoder/jpeg2000/jp2_inspect_test.go` (extend `TestJPEG2000Inspect`)

- [ ] **Step 1: Write the failing test**

Replace the body of `TestJPEG2000Inspect` in `decoder/jpeg2000/jp2_inspect_test.go` with this version (adds a `decodedColor` column asserted on the two fully-known fixtures, plus a focused decoded-only loop for the two extra fixtures):

```go
func TestJPEG2000Inspect(t *testing.T) {
	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Skip("jpeg2000 decoder not registered")
	}
	p, ok := f.(decoder.CodestreamInspector)
	if !ok {
		t.Fatal("jpeg2000 factory does not implement decoder.CodestreamInspector")
	}

	for _, tc := range []struct {
		file         string
		lossless     decoder.Lossless
		color        decoder.ColorEncoding
		decodedColor decoder.ColorEncoding
		chroma       decoder.ChromaSubsampling
		components   int
	}{
		// Reversible 5/3 + MCT → stored YBR_RCT, decoded RGB (OpenJPEG inverts MCT).
		{"testdata/lowres_2levels.j2k", decoder.LosslessYes, decoder.ColorYBRRCT, decoder.ColorRGB, decoder.Subsampling444, 3},
		// Irreversible 9/7, no MCT, no box → stored RGB, decoded YCbCr (Aperio default).
		{"testdata/subsampled_422_256.j2k", decoder.LosslessNo, decoder.ColorRGB, decoder.ColorYCbCr, decoder.Subsampling422, 3},
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		ci, err := p.Inspect(b)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if ci.Components != tc.components || ci.BitDepth != 8 || ci.Lossless != tc.lossless ||
			ci.ColorEncoding != tc.color || ci.DecodedColorSpace != tc.decodedColor ||
			ci.ChromaSubsampling != tc.chroma || ci.Boxed {
			t.Errorf("%s inspect = %+v, want comps=%d depth=8 lossless=%s color=%s decoded=%s chroma=%s raw",
				tc.file, ci, tc.components, tc.lossless, tc.color, tc.decodedColor, tc.chroma)
		}
	}

	// Decoded-colorspace on the remaining fixtures (fields other than
	// DecodedColorSpace not asserted here — covered by color_test.go's rule test).
	for _, tc := range []struct {
		file    string
		decoded decoder.ColorEncoding
	}{
		{"testdata/aperio_33003_tile.j2k", decoder.ColorYCbCr}, // raw, no MCT/box
		{"testdata/rgb_mct_solid.j2k", decoder.ColorRGB},       // MCT → RGB
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		ci, err := p.Inspect(b)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if ci.DecodedColorSpace != tc.decoded {
			t.Errorf("%s: DecodedColorSpace = %s, want %s", tc.file, ci.DecodedColorSpace, tc.decoded)
		}
	}

	if _, err := p.Inspect([]byte{0xFF, 0xD8}); err == nil { // JPEG SOI, not J2K
		t.Error("expected error probing non-J2K bytes")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/jpeg2000/ -run TestJPEG2000Inspect -v`
Expected: FAIL — `DecodedColorSpace` is the zero value `unknown` from `Inspect` (field not yet populated), so the first loop's assertion fails on `decoded=RGB`/`decoded=YCbCr`.

- [ ] **Step 3: Populate the field in `Inspect`**

In `decoder/jpeg2000/jp2_cgo.go`, replace the `Inspect` function body's return so it sets the field from the already-parsed header:

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./decoder/jpeg2000/ -run TestJPEG2000Inspect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/jpeg2000/jp2_cgo.go decoder/jpeg2000/jp2_inspect_test.go
git commit -m "feat(jpeg2000): report DecodedColorSpace from Inspect (#112)"
```

---

### Task 4: Populate `DecodedColorSpace` in the htj2k `Inspect`

htj2k applies no reader-side chroma conversion (openjph reconstructs the stored planes, opentile packs RGB), so decoded output is RGB for colour input and Grayscale for one component — regardless of MCT/box signalling. Set by component count.

**Files:**
- Modify: `decoder/htj2k/htj2k_cgo.go` (`Inspect`, ~lines 45-51)
- Test: `decoder/htj2k/htj2k_inspect_test.go` (extend `TestHTJ2KInspect`)

- [ ] **Step 1: Write the failing test**

In `decoder/htj2k/htj2k_inspect_test.go`, add a `DecodedColorSpace` assertion to `TestHTJ2KInspect`. Change the existing field-check `if` (the one asserting `ci.Components != 3 || ...`) to also require the decoded colorspace, and add an explanatory comment. Replace:

```go
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessYes ||
		ci.ChromaSubsampling != decoder.Subsampling444 {
		t.Errorf("htj2k inspect = %+v, want comps=3 depth=8 lossless 4:4:4", ci)
	}
```

with:

```go
	// The test encoder uses set_color_transform(false): a no-MCT raw HTJ2K
	// codestream. opentile's htj2k decode applies no chroma conversion, so the
	// decoded output is RGB regardless — this is the case that proves htj2k must
	// differ from the jpeg2000 decode-policy rule (which would say YCbCr here).
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessYes ||
		ci.ChromaSubsampling != decoder.Subsampling444 || ci.DecodedColorSpace != decoder.ColorRGB {
		t.Errorf("htj2k inspect = %+v, want comps=3 depth=8 lossless 4:4:4 decoded=RGB", ci)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/htj2k/ -run TestHTJ2KInspect -v`
Expected: FAIL — `DecodedColorSpace` is zero (`unknown`), want `RGB`.

- [ ] **Step 3: Populate the field in `Inspect`**

In `decoder/htj2k/htj2k_cgo.go`, replace the `return h.CodestreamInfo(), nil` line of `Inspect` with:

```go
	ci := h.CodestreamInfo()
	if ci.Components == 1 {
		ci.DecodedColorSpace = decoder.ColorGrayscale
	} else {
		ci.DecodedColorSpace = decoder.ColorRGB
	}
	return ci, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./decoder/htj2k/ -run TestHTJ2KInspect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/htj2k/htj2k_cgo.go decoder/htj2k/htj2k_inspect_test.go
git commit -m "feat(htj2k): report DecodedColorSpace (RGB/Grayscale) from Inspect (#112)"
```

---

### Task 5: Populate `DecodedColorSpace` in the jpeg `Inspect`

libjpeg-turbo's `Decode` emits RGB for any colour input and grayscale for `TJCS_GRAY`. So a JFIF tile whose stored `ColorEncoding` is `YCbCr` reports decoded `RGB` — the headline divergence.

**Files:**
- Modify: `decoder/jpeg/jpeg_cgo.go` (`Inspect` colorspace switch, ~lines 59-70)
- Test: `decoder/jpeg/jpeg_inspect_test.go` (extend `TestJPEGInspect`)

- [ ] **Step 1: Write the failing test**

In `decoder/jpeg/jpeg_inspect_test.go`, extend the two existing assertions in `TestJPEGInspect`.

Replace the color-JPEG check:

```go
	// image/jpeg encodes YCbCr with 4:2:0 chroma subsampling.
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessNo ||
		ci.ColorEncoding != decoder.ColorYCbCr || ci.ChromaSubsampling != decoder.Subsampling420 || ci.Boxed {
		t.Errorf("color JPEG inspect = %+v, want comps=3 depth=8 lossy YCbCr 4:2:0 raw", ci)
	}
```

with (adds `DecodedColorSpace == ColorRGB` — stored YCbCr but decoded RGB):

```go
	// image/jpeg encodes YCbCr with 4:2:0 chroma subsampling. libjpeg-turbo
	// decodes to RGB, so DecodedColorSpace diverges from the stored ColorEncoding.
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessNo ||
		ci.ColorEncoding != decoder.ColorYCbCr || ci.DecodedColorSpace != decoder.ColorRGB ||
		ci.ChromaSubsampling != decoder.Subsampling420 || ci.Boxed {
		t.Errorf("color JPEG inspect = %+v, want comps=3 depth=8 lossy YCbCr decoded=RGB 4:2:0 raw", ci)
	}
```

Replace the grayscale-JPEG check:

```go
	if ci.Components != 1 || ci.ColorEncoding != decoder.ColorGrayscale || ci.ChromaSubsampling != decoder.SubsamplingNone {
		t.Errorf("gray JPEG inspect = %+v, want comps=1 grayscale none", ci)
	}
```

with:

```go
	if ci.Components != 1 || ci.ColorEncoding != decoder.ColorGrayscale ||
		ci.DecodedColorSpace != decoder.ColorGrayscale || ci.ChromaSubsampling != decoder.SubsamplingNone {
		t.Errorf("gray JPEG inspect = %+v, want comps=1 grayscale decoded=grayscale none", ci)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/jpeg/ -run TestJPEGInspect -v`
Expected: FAIL — `DecodedColorSpace` is zero (`unknown`), want `RGB` / `grayscale`.

- [ ] **Step 3: Populate the field in `Inspect`**

In `decoder/jpeg/jpeg_cgo.go`, replace the `switch colorspace { ... }` block (the one that sets `ci.Components, ci.ColorEncoding`) with:

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
		ci.DecodedColorSpace = decoder.ColorRGB // libjpeg-turbo converts YCbCr→RGB
	case C.TJCS_CMYK, C.TJCS_YCCK:
		ci.Components, ci.ColorEncoding = 4, decoder.ColorUnknown
		ci.DecodedColorSpace = decoder.ColorUnknown
	default:
		ci.Components, ci.ColorEncoding = 3, decoder.ColorUnknown
		ci.DecodedColorSpace = decoder.ColorUnknown
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./decoder/jpeg/ -run TestJPEGInspect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/jpeg/jpeg_cgo.go decoder/jpeg/jpeg_inspect_test.go
git commit -m "feat(jpeg): report DecodedColorSpace (RGB/Grayscale) from Inspect (#112)"
```

---

### Task 6: Populate `DecodedColorSpace` in the jpegxl `Inspect`

libjxl's `Decode` outputs RGB(A); mirror the existing colour-channel switch.

**Files:**
- Modify: `decoder/jpegxl/jxl_cgo.go` (`Inspect` channel switch, ~lines 180-187)
- Test: `decoder/jpegxl/jxl_inspect_test.go` (extend `TestJPEGXLInspect`)

- [ ] **Step 1: Write the failing test**

In `decoder/jpegxl/jxl_inspect_test.go`, extend the field check. Replace:

```go
	// libjxl exposes no header-only lossless flag → LosslessUnknown is expected.
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessUnknown ||
		ci.ColorEncoding != decoder.ColorRGB || ci.ChromaSubsampling != decoder.SubsamplingUnknown || ci.Boxed {
		t.Errorf("jxl inspect = %+v, want comps=3 depth=8 lossless=unknown RGB raw", ci)
	}
```

with:

```go
	// libjxl exposes no header-only lossless flag → LosslessUnknown is expected.
	// It decodes to RGB, so DecodedColorSpace == RGB for this 3-channel tile.
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessUnknown ||
		ci.ColorEncoding != decoder.ColorRGB || ci.DecodedColorSpace != decoder.ColorRGB ||
		ci.ChromaSubsampling != decoder.SubsamplingUnknown || ci.Boxed {
		t.Errorf("jxl inspect = %+v, want comps=3 depth=8 lossless=unknown RGB decoded=RGB raw", ci)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./decoder/jpegxl/ -run TestJPEGXLInspect -v`
Expected: FAIL — `DecodedColorSpace` is zero (`unknown`), want `RGB`.

- [ ] **Step 3: Populate the field in `Inspect`**

In `decoder/jpegxl/jxl_cgo.go`, replace the `switch ch { ... }` block (the one setting `ci.ColorEncoding`) with:

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./decoder/jpegxl/ -run TestJPEGXLInspect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add decoder/jpegxl/jxl_cgo.go decoder/jpegxl/jxl_inspect_test.go
git commit -m "feat(jpegxl): report DecodedColorSpace (RGB/Grayscale) from Inspect (#112)"
```

---

### Task 7: CHANGELOG + full-suite verification

**Files:**
- Modify: `CHANGELOG.md` (add a `## [0.62.0]` section at the top of the version list)

- [ ] **Step 1: Add the CHANGELOG entry**

Open `CHANGELOG.md`, find the most recent version heading (`## [0.61.0] — 2026-07-12`), and insert a new section immediately above it. Use the date the work is completed (check with `date +%F` and substitute below):

```markdown
## [0.62.0] — 2026-07-19

### Added
- `decoder.CodestreamInfo.DecodedColorSpace` (`ColorEncoding`) — the colorspace
  of the component planes the decode library hands back, *before* opentile's
  reader-side YCbCr→RGB normalization; the decoded-pixel counterpart to the
  stored `ColorEncoding`. Populated by all four codestream-inspecting codecs
  (jpeg, jpeg2000, htj2k, jpegxl). JPEG 2000 MCT / JPEG / JPEG XL / htj2k colour
  tiles report `RGB`; an sYCC or unsignalled Aperio-33003 raw JP2K codestream
  reports `YCbCr`; single-component tiles report `Grayscale`. Additive; no
  behaviour change to any existing field or `Decode` output. (#112)

### Changed
- Internal: the jpeg2000 decode-time `decodeIsYCbCr` decision and the new
  inspect-time `DecodedColorSpace` now share one build-tag-free rule in
  `decoder/jpeg2000/color.go`, so they cannot drift. No decode output changes.
```

If a `## [Unreleased]` section exists at the top, place the `## [0.62.0]` heading directly under it (above `## [0.61.0]`).

- [ ] **Step 2: Commit the CHANGELOG**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): v0.62.0 — DecodedColorSpace on CodestreamInfo (#112)"
```

- [ ] **Step 3: Full test suite under race**

Run: `make test`
Expected: PASS across all packages under `-race`.

- [ ] **Step 4: nocgo build + test (proves color.go / color_test.go are build-tag-clean)**

Run: `go build -tags nocgo ./... && go test -tags nocgo ./decoder/...`
Expected: builds clean; jpeg2000 `color_test.go` rule + parity tests PASS; cgo-gated inspect tests skip.

- [ ] **Step 5: Vet**

Run: `go vet ./...`
Expected: clean (no output).

---

## Notes for the implementer

- **Do not** modify `internal/j2kheader` — the decode-policy rule is
  jpeg2000-decoder-specific and lives in `decoder/jpeg2000/color.go`. The htj2k
  decoded value is a component-count rule, deliberately different.
- **Do not** touch `ColorEncoding` (stored) semantics anywhere — only add the
  new field alongside it.
- The `decoder.ColorEncoding.String()` method already renders
  `grayscale`/`RGB`/`YCbCr`/`unknown`, so `%s`/`%+v` in the test error messages
  Just Work.
- After Task 2, `jp2_cgo.go` no longer declares `jp2EnumSRGB`/`jp2EnumSYCC`; if
  the compiler reports them still referenced anywhere in `jp2_cgo.go`, that is a
  sign the deletion removed too much — only the const block and `decodeIsYCbCr`
  move; `detectCodecFormat` and its `OPJ_CODEC_*` returns stay.
