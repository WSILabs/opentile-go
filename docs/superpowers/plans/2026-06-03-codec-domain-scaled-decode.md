# Codec-domain scaled decode (JP2K + HTJ2K) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `decoder/jpeg2000` and `decoder/htj2k` honor `DecodeOptions.Scale ∈ {1,2,4,8}` via DWT resolution-level decode (1/2^r by skipping high-frequency subbands), matching the `jpeg` decoder's contract — faster, anti-aliased, seam-free downscaling.

**Architecture:** Both codecs are wavelet-based and expose a resolution-skip control: OpenJPEG `opj_set_decoded_resolution_factor` / `cp_reduce`; openjph `restrict_input_resolution(r, r)` (call after `read_headers`, before `create`). Map `Scale → r = log2(Scale)`. When the codestream has fewer decomposition levels than `r`, the lib clamps; we **box-finish** the residual factor to land on exactly `ceil(srcDim/Scale)`, keeping the dimension contract identical to the `jpeg` path. A shared `internal/boxhalve` helper does the residual 2× reductions (the decoder packages cannot import `resample` — it imports `decoder`).

**Tech Stack:** Go 1.23+, cgo (OpenJPEG `libopenjp2` 2.5, openjph 0.27 via `shim.cpp`), the existing decoder registry.

**Issues:** #11 (tracking umbrella), #10 (jpeg2000), #12 (htj2k). Decisions sealed with the owner: do #10+#12 together; **box-finish to exact dims** (not honor-actual).

**Upstream confirmed (headers read 2026-06-03):**
- OpenJPEG `openjpeg.h`: `opj_set_decoded_resolution_factor(codec, res_factor)` → "original dimension divided by 2^(reduce)", "limited by the smallest total number of decomposition levels among tiles". Also `opj_dparameters_t.cp_reduce` (what `opj_decompress -r` sets) does the same at `opj_setup_decoder` time.
- openjph `ojph_codestream.h`: `restrict_input_resolution(skipped_res_for_data, skipped_res_for_recon)` — "call after read_headers() but before create()"; `skipped_res_for_recon` shrinks the image, `skipped_res_for_data` skips reading/decoding those fine resolutions (the speed win).

---

## File Structure

- `internal/boxhalve/boxhalve.go` — NEW. `Halve(img *decoder.Image, times int) *decoder.Image` and `To(img, w, h)`: residual box reduction by power-of-2. One responsibility: finish a partial codec reduction to exact dims.
- `internal/boxhalve/boxhalve_test.go` — NEW.
- `decoder/jpeg2000/jp2_cgo.go` — MODIFY. Scale validation (mirror jpeg), thread `res_factor` through `opj_jpeg2000_dimensions` + `opj_jpeg2000_decode`, box-finish.
- `decoder/jpeg2000/scale_test.go` — NEW. Dims + quality-sanity (reuses `testdata/subsampled_422_256.j2k` from #7).
- `decoder/jpeg2000/scale_bench_test.go` — NEW.
- `decoder/htj2k/shim.h` + `shim.cpp` — MODIFY. `resolution_factor` arg; `restrict_input_resolution`.
- `decoder/htj2k/htj2k_cgo.go` — MODIFY. Scale validation, plumb factor, box-finish.
- `decoder/htj2k/scale_test.go` + `scale_bench_test.go` — NEW.
- `CHANGELOG.md`, `decoder/decoder.go` doc comment, `decoder/jpeg2000/jp2_nocgo.go` + `decoder/htj2k/htj2k_nocgo.go` (confirm stubs unaffected).

---

## Task 0: Confirm upstream behavior (mandatory spike — do not skip)

Per CLAUDE.md's "Step 0: confirm upstream" rule and the issues' "don't guess". The one thing the headers don't settle: with OpenJPEG, does the reduced dimension show up at `opj_read_header` time or only after `opj_decode` (in `image->comps[c].w/h`)? And does `cp_reduce` (set before `opj_setup_decoder`) behave the same as `opj_set_decoded_resolution_factor` (after)?

- [ ] **Step 1: Write a throwaway C probe** (in a scratch dir, not committed) that decodes `decoder/jpeg2000/testdata/subsampled_422_256.j2k` with `params.cp_reduce = 1`, prints `image->x1-x0`, `image->y1-y0`, and `image->comps[0].w/h` after `opj_read_header` AND after `opj_decode`.

- [ ] **Step 2: Run it.** Record which call reports the reduced 128×128 and whether comps reflect it. Expected (to verify, not assume): `cp_reduce` set before setup → `opj_read_header` reports reduced canvas, `comps[].w/h` reduced after decode.

- [ ] **Step 3: Decide the dims source** for `opj_jpeg2000_dimensions`. If read_header reports reduced dims with `cp_reduce`, use `cp_reduce` (simplest, no ordering ambiguity); otherwise fall back to `opj_set_decoded_resolution_factor` after read_header. **Record the finding as a comment** in `jp2_cgo.go` and proceed with whichever the probe confirms. The rest of the plan assumes the `cp_reduce` path; adjust the two C edits in Tasks 2–3 if the probe says otherwise.

- [ ] **Step 4: Confirm openjph dims** with a quick check: the existing htj2k roundtrip test encoder uses `set_num_decomposition(1)`; confirm `restrict_input_resolution(1,1)` after `read_headers` yields half-size output (note: testing r>1 needs an encoder with ≥3 decomposition levels — Task 6/8 handles that).

---

## Task 1: Shared box-halve helper

**Files:**
- Create: `internal/boxhalve/boxhalve.go`
- Test: `internal/boxhalve/boxhalve_test.go`

- [ ] **Step 1: Write the failing test.**

```go
package boxhalve

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestHalveOnceRGB(t *testing.T) {
	src := decoder.NewImage(4, 4)
	// Fill 2x2 blocks with known averages: top-left block all 100, etc.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			off := y*src.Stride + x*3
			v := byte(10 + (y/2)*40 + (x/2)*20) // each 2x2 block uniform
			src.Pix[off], src.Pix[off+1], src.Pix[off+2] = v, v, v
		}
	}
	got := Halve(src, 1)
	if got.Width != 2 || got.Height != 2 {
		t.Fatalf("dims = %dx%d, want 2x2", got.Width, got.Height)
	}
	// Each output pixel is the average of a uniform 2x2 block → equals v.
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			want := byte(10 + y*40 + x*20)
			off := y*got.Stride + x*3
			if got.Pix[off] != want {
				t.Errorf("(%d,%d)=%d want %d", x, y, got.Pix[off], want)
			}
		}
	}
}

func TestHalveTimesZeroReturnsInput(t *testing.T) {
	src := decoder.NewImage(3, 3)
	if got := Halve(src, 0); got != src {
		t.Errorf("times=0 should return src unchanged")
	}
}

func TestToRGBA(t *testing.T) {
	src := decoder.NewImageFormat(8, 8, decoder.PixelFormatRGBA)
	got := To(src, 2, 2) // 8->2 is two halvings
	if got.Width != 2 || got.Height != 2 || got.Format != decoder.PixelFormatRGBA {
		t.Fatalf("got %dx%d fmt %v", got.Width, got.Height, got.Format)
	}
}
```

- [ ] **Step 2: Run, verify it fails.**

Run: `go test ./internal/boxhalve/ -v`
Expected: FAIL (package/functions undefined).

- [ ] **Step 3: Implement.**

```go
// Package boxhalve finishes a partial codec resolution reduction by box-
// averaging the residual power-of-two factor, so a wavelet decoder that
// could only reduce part of the requested Scale still lands on exact dims.
package boxhalve

import "github.com/wsilabs/opentile-go/decoder"

func bpp(f decoder.PixelFormat) int {
	if f == decoder.PixelFormatRGBA {
		return 4
	}
	return 3
}

// Halve box-reduces img by 2^times in each dimension (ceil), averaging
// 2x2 source blocks (edge blocks average the available 1- or 2-wide cells).
// times <= 0 returns img unchanged.
func Halve(img *decoder.Image, times int) *decoder.Image {
	cur := img
	for t := 0; t < times; t++ {
		cur = halveOnce(cur)
	}
	return cur
}

// To halves img repeatedly until it reaches (w, h). Requires that w,h are
// img dims reduced by a power of two (the codec-reduction residual always
// is); it halves ceil-wise until width <= w.
func To(img *decoder.Image, w, h int) *decoder.Image {
	cur := img
	for cur.Width > w || cur.Height > h {
		cur = halveOnce(cur)
	}
	return cur
}

func halveOnce(src *decoder.Image) *decoder.Image {
	b := bpp(src.Format)
	dw := (src.Width + 1) / 2
	dh := (src.Height + 1) / 2
	dst := decoder.NewImageFormat(dw, dh, src.Format)
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			x0, y0 := dx*2, dy*2
			x1, y1 := x0+1, y0+1
			if x1 >= src.Width {
				x1 = x0
			}
			if y1 >= src.Height {
				y1 = y0
			}
			do := dy*dst.Stride + dx*b
			for c := 0; c < b; c++ {
				s := int(src.Pix[y0*src.Stride+x0*b+c]) +
					int(src.Pix[y0*src.Stride+x1*b+c]) +
					int(src.Pix[y1*src.Stride+x0*b+c]) +
					int(src.Pix[y1*src.Stride+x1*b+c])
				dst.Pix[do+c] = byte((s + 2) / 4)
			}
		}
	}
	return dst
}
```

- [ ] **Step 4: Run, verify pass.** Run: `go test ./internal/boxhalve/ -race -v` → PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/boxhalve/
git commit -m "feat(internal/boxhalve): residual box-halve helper for scaled decode"
```

---

## Task 2: JP2K — Scale validation + scaled dimensions

**Files:**
- Modify: `decoder/jpeg2000/jp2_cgo.go` (the `scale` check at ~279, the `opj_jpeg2000_dimensions` C func, and the Go dims handling)
- Test: `decoder/jpeg2000/scale_test.go`

- [ ] **Step 1: Write the failing test.**

```go
//go:build cgo && !nocgo

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func readJP2KFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/subsampled_422_256.j2k")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return b
}

func TestJP2KScaleDims(t *testing.T) {
	src := readJP2KFixture(t) // 256x256
	for _, tc := range []struct{ scale, w, h int }{
		{1, 256, 256}, {2, 128, 128}, {4, 64, 64}, {8, 32, 32},
	} {
		img, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: tc.scale})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.w || img.Height != tc.h {
			t.Errorf("scale %d: dims %dx%d, want %dx%d", tc.scale, img.Width, img.Height, tc.w, tc.h)
		}
	}
}

func TestJP2KScaleUnsupported(t *testing.T) {
	src := readJP2KFixture(t)
	for _, s := range []int{3, 5, 6, 16} {
		_, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: s})
		if err == nil {
			t.Errorf("scale %d: expected ErrUnsupportedScale", s)
		}
	}
}
```

- [ ] **Step 2: Run, verify it fails.**

Run: `OPENTILE_TESTDIR unused` `go test ./decoder/jpeg2000/ -run TestJP2KScale -v`
Expected: FAIL — current code rejects `Scale != 1` (`TestJP2KScaleDims` errors at scale 2).

- [ ] **Step 3: Implement Scale validation + reduced dims.** Replace the scale guard in the Go `Decode` (currently `if scale != 0 && scale != 1 { return ErrUnsupportedScale }`) with the jpeg-matching form, and pass a `resolution_factor` into the dimensions + decode C calls. Mirror `decoder/jpeg/jpeg_cgo.go`:

```go
	scale := opts.Scale
	if scale == 0 {
		scale = 1
	}
	resFactor := 0
	switch scale {
	case 1:
		resFactor = 0
	case 2:
		resFactor = 1
	case 4:
		resFactor = 2
	case 8:
		resFactor = 3
	default:
		return nil, fmt.Errorf("decoder/jpeg2000: scale=%d (want 1,2,4,8): %w", scale, decoder.ErrUnsupportedScale)
	}
```

In the C `opj_jpeg2000_dimensions` and `opj_jpeg2000_decode`, add an `int resolution_factor` parameter and set `params.cp_reduce = (OPJ_UINT32)resolution_factor;` immediately after `opj_set_default_decoder_parameters(&params);` (the path Task 0 confirmed). Update both Go call sites to pass `C.int(resFactor)`. The dims returned by `opj_jpeg2000_dimensions` now reflect the reduced canvas.

(Show the full edited C signatures + the `cp_reduce` line in the implementation; keep the #7 chroma-subsampling packing loop unchanged — it reads `comps[c].w/h` which are now the reduced per-component dims.)

- [ ] **Step 4: Run, verify pass** for `TestJP2KScaleDims` (scales whose level exists) and `TestJP2KScaleUnsupported`. Run: `go test ./decoder/jpeg2000/ -run 'TestJP2KScale|TestDecodeSubsampled422|TestDecodeRGBAFormat' -race -v` → PASS (the #7 + #8 tests must stay green).

Note: if the fixture has fewer than 3 decomposition levels, scale 4/8 will come back larger than target until Task 3's box-finish lands — `TestJP2KScaleDims` for 4/8 may still fail here. That is expected; Task 3 closes it. If so, temporarily restrict this step's assertion to scales {1,2} and re-enable 4/8 in Task 3.

- [ ] **Step 5: Commit.**

```bash
git add decoder/jpeg2000/jp2_cgo.go decoder/jpeg2000/scale_test.go
git commit -m "feat(decoder/jpeg2000): honor DecodeOptions.Scale via cp_reduce resolution decode"
```

---

## Task 3: JP2K — box-finish to exact dims

**Files:**
- Modify: `decoder/jpeg2000/jp2_cgo.go` (after the C decode returns)

- [ ] **Step 1: Extend the dims test** to assert exactness at all of {1,2,4,8} even when the codestream clamps (re-enable 4/8 from Task 2 Step 4). Add a uniform-color quality check that survives box-finish:

```go
func TestJP2KScaleBoxFinishExact(t *testing.T) {
	src := readJP2KFixture(t)
	for _, s := range []int{1, 2, 4, 8} {
		img, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: s})
		if err != nil {
			t.Fatalf("scale %d: %v", s, err)
		}
		wantW := (256 + s - 1) / s
		if img.Width != wantW || img.Height != wantW {
			t.Errorf("scale %d: %dx%d, want %dx%d", s, img.Width, img.Height, wantW, wantW)
		}
	}
}
```

- [ ] **Step 2: Run, verify it fails** for the scales the codestream can't fully reduce (dims too large). Run: `go test ./decoder/jpeg2000/ -run TestJP2KScaleBoxFinishExact -v`.

- [ ] **Step 3: Implement box-finish.** After the C decode produces an image of the codec-reduced dims `(cw, ch)`, if `(cw, ch)` is larger than the target `(ceil(srcW/scale), ceil(srcH/scale))`, finish with `boxhalve.To`. Concretely: have the C dimensions/decode return the *actual* reduced dims; compute the target from the full source dims (read once at scale 1, or `target = ceil(actualFull/scale)`); if different, `dst = boxhalve.To(dst, targetW, targetH)`. Import `github.com/wsilabs/opentile-go/internal/boxhalve`. Record the policy in a comment ("OpenJPEG clamps res_factor to numresolutions-1; box-finish the residual").

(Show the exact Go: read full dims via a scale-1 header read or by multiplying back; compute target; conditional `boxhalve.To`. Honor `opts.Dst`: if a Dst is supplied its dims must equal the *target*; validate against target, not the codec-reduced size.)

- [ ] **Step 4: Run, verify pass.** Run: `go test ./decoder/jpeg2000/ -run 'TestJP2KScale|TestDecodeSubsampled422|TestDecodeRGBAFormat' -race -v` → all PASS.

- [ ] **Step 5: Commit.**

```bash
git add decoder/jpeg2000/jp2_cgo.go decoder/jpeg2000/scale_test.go
git commit -m "feat(decoder/jpeg2000): box-finish scaled decode to exact ceil(src/scale) dims"
```

---

## Task 4: JP2K — quality sanity + benchmark

**Files:**
- Modify: `decoder/jpeg2000/scale_test.go`
- Create: `decoder/jpeg2000/scale_bench_test.go`

- [ ] **Step 1: Write the quality-sanity test.** Resolution-decode at scale 2 vs full-decode-then-box-halve; assert *close*, not equal (wavelet low-pass ≠ box). Use mean-abs-diff tolerance:

```go
func TestJP2KScaleQualityClose(t *testing.T) {
	src := readJP2KFixture(t)
	full, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	ref := boxhalve.Halve(full, 1) // 128x128 box reference
	got, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != ref.Width || got.Height != ref.Height {
		t.Fatalf("dims %dx%d vs %dx%d", got.Width, got.Height, ref.Width, ref.Height)
	}
	var sum, n int
	for i := range got.Pix {
		d := int(got.Pix[i]) - int(ref.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
		n++
	}
	mean := float64(sum) / float64(n)
	if mean > 12 { // wavelet vs box differ but should be in the same ballpark
		t.Errorf("mean abs diff %.2f too large (resolution decode vs box)", mean)
	}
}
```

(`import "github.com/wsilabs/opentile-go/internal/boxhalve"` in the test.)

- [ ] **Step 2: Run, verify pass.** Run: `go test ./decoder/jpeg2000/ -run TestJP2KScaleQualityClose -v`. If the mean exceeds 12, investigate (wrong res mapping) rather than loosening blindly; document the chosen tolerance.

- [ ] **Step 3: Write the benchmark.**

```go
//go:build cgo && !nocgo

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/boxhalve"
)

func benchTile(b *testing.B) []byte {
	x, err := os.ReadFile("testdata/subsampled_422_256.j2k")
	if err != nil {
		b.Fatal(err)
	}
	return x
}

func BenchmarkJP2KResolutionDecode2x(b *testing.B) {
	src := benchTile(b)
	dec := (&factory{}).New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dec.Decode(src, decoder.DecodeOptions{Scale: 2}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJP2KFullDecodePlusBox2x(b *testing.B) {
	src := benchTile(b)
	dec := (&factory{}).New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		full, err := dec.Decode(src, decoder.DecodeOptions{Scale: 1})
		if err != nil {
			b.Fatal(err)
		}
		_ = boxhalve.Halve(full, 1)
	}
}
```

- [ ] **Step 4: Run the benchmark; record the speedup.** Run: `go test ./decoder/jpeg2000/ -run '^$' -bench BenchmarkJP2K -benchmem`. Expect resolution-decode meaningfully faster than full+box.

- [ ] **Step 5: Commit.**

```bash
git add decoder/jpeg2000/scale_test.go decoder/jpeg2000/scale_bench_test.go
git commit -m "test(decoder/jpeg2000): scaled-decode quality sanity + benchmark"
```

---

## Task 5: HTJ2K — test encoder with multiple decomposition levels

The existing `wsi_htj2k_encode_test` uses `set_num_decomposition(1)` — only enough for scale 2. Resolution decode at scale 4/8 needs ≥2/≥3 levels. Add a levels parameter so tests can produce codestreams that exercise scale 4/8.

**Files:**
- Modify: `decoder/htj2k/shim.h`, `decoder/htj2k/shim.cpp` (`wsi_htj2k_encode_test`), `decoder/htj2k/htj2k_roundtrip_test.go` helper

- [ ] **Step 1: Write a failing test** asserting a 256×256 image encoded with 3 decomposition levels decodes (scale 1) round-trip correctly (proves the new levels param path works before relying on it for scale tests).

```go
//go:build cgo && !nocgo && !nohtj2k

package htj2k

import "testing"

func TestEncodeWithLevels(t *testing.T) {
	src := makeTestRGB(256, 256) // existing helper in htj2k_roundtrip_test.go
	enc, err := encodeForTest(src, 256, 256, 3) // NEW signature: + numDecomp
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("empty codestream")
	}
}
```

- [ ] **Step 2: Run, verify fail** (`encodeForTest` arity / `numDecomp` not wired). Run: `go test ./decoder/htj2k/ -run TestEncodeWithLevels -v`.

- [ ] **Step 3: Implement.** Add `int num_decomp` to `wsi_htj2k_encode_test` (shim.h + shim.cpp), replacing the hardcoded `cod.set_num_decomposition(1)` with `cod.set_num_decomposition((ui32)num_decomp)`. Thread a `numDecomp` arg through the Go test helper `encodeForTest`. (Show the exact shim signature change and the Go wrapper.)

- [ ] **Step 4: Run, verify pass.** Run: `go test ./decoder/htj2k/ -run 'TestEncodeWithLevels|TestRoundTrip' -race -v` → PASS (existing roundtrip unchanged when callers pass the old level count).

- [ ] **Step 5: Commit.**

```bash
git add decoder/htj2k/shim.h decoder/htj2k/shim.cpp decoder/htj2k/htj2k_roundtrip_test.go
git commit -m "test(decoder/htj2k): parameterize test encoder decomposition levels"
```

---

## Task 6: HTJ2K — Scale validation + resolution-skip decode

**Files:**
- Modify: `decoder/htj2k/shim.h`, `decoder/htj2k/shim.cpp` (`wsi_htj2k_dimensions` + `wsi_htj2k_decode`), `decoder/htj2k/htj2k_cgo.go`
- Test: `decoder/htj2k/scale_test.go`

- [ ] **Step 1: Write the failing dims test.** Encode a 256×256 image with 3 levels, decode at scales 1/2/4/8, assert dims (box-finish lands in Task 7, so assert {1,2} exact here and full set after Task 7 — note inline).

```go
//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestHTJ2KScaleDims(t *testing.T) {
	src := makeTestRGB(256, 256)
	enc, err := encodeForTest(src, 256, 256, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ scale, w int }{{1, 256}, {2, 128}} {
		img, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: tc.scale})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.w || img.Height != tc.w {
			t.Errorf("scale %d: %dx%d want %dx%d", tc.scale, img.Width, img.Height, tc.w, tc.w)
		}
	}
}

func TestHTJ2KScaleUnsupported(t *testing.T) {
	enc, _ := encodeForTest(makeTestRGB(64, 64), 64, 64, 1)
	for _, s := range []int{3, 5, 7} {
		if _, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: s}); err == nil {
			t.Errorf("scale %d: want ErrUnsupportedScale", s)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail.** Run: `go test ./decoder/htj2k/ -run TestHTJ2KScale -v` (current code rejects `Scale != 1`).

- [ ] **Step 3: Implement.** In `htj2k_cgo.go`, replace the `Scale != 1` guard with the `{1,2,4,8}→resFactor` switch (identical to Task 2 Step 3, `decoder/htj2k:` message). Add `int resolution_factor` to `wsi_htj2k_dimensions` and `wsi_htj2k_decode`; in `shim.cpp`, insert `cs.restrict_input_resolution((ui32)resolution_factor, (ui32)resolution_factor);` **between `cs.read_headers(&in)` and `cs.create()`** (the header-confirmed ordering). Compute reduced `w,h` after the restriction: `w = ceil(full_w / 2^r)` (or read from the post-restrict siz/line geometry — verify which against the openjph header during impl). Update the Go call sites to pass `C.int(resFactor)`.

(Show the exact shim insertions for both functions + the Go switch. The existing `line->size`-bounded packing from the htj2k subsampling fix stays — it already adapts to the reduced line width.)

- [ ] **Step 4: Run, verify pass** for scales {1,2}. Run: `go test ./decoder/htj2k/ -run 'TestHTJ2KScale|TestRoundTrip' -race -v` → PASS.

- [ ] **Step 5: Commit.**

```bash
git add decoder/htj2k/shim.h decoder/htj2k/shim.cpp decoder/htj2k/htj2k_cgo.go decoder/htj2k/scale_test.go
git commit -m "feat(decoder/htj2k): honor DecodeOptions.Scale via restrict_input_resolution"
```

---

## Task 7: HTJ2K — box-finish to exact dims

**Files:**
- Modify: `decoder/htj2k/htj2k_cgo.go`

- [ ] **Step 1: Extend the dims test** to {1,2,4,8} exact (mirror Task 3):

```go
func TestHTJ2KScaleBoxFinishExact(t *testing.T) {
	enc, err := encodeForTest(makeTestRGB(256, 256), 256, 256, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []int{1, 2, 4, 8} {
		img, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: s})
		if err != nil {
			t.Fatalf("scale %d: %v", s, err)
		}
		want := (256 + s - 1) / s
		if img.Width != want || img.Height != want {
			t.Errorf("scale %d: %dx%d want %dx%d", s, img.Width, img.Height, want, want)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail** for scales the 3-level codestream can't fully reduce.

- [ ] **Step 3: Implement box-finish** in `htj2k_cgo.go` identical in shape to Task 3: after the C decode returns the codec-reduced image, if larger than `target = ceil(full/scale)`, `dst = boxhalve.To(dst, targetW, targetH)`. Honor `opts.Dst` against the *target* dims. Import `internal/boxhalve`. Handle both the RGB and RGBA output paths (the htj2k decoder has both).

- [ ] **Step 4: Run, verify pass.** Run: `go test ./decoder/htj2k/ -run 'TestHTJ2KScale|TestRoundTrip|TestEncodeWithLevels' -race -v` → PASS.

- [ ] **Step 5: Commit.**

```bash
git add decoder/htj2k/htj2k_cgo.go decoder/htj2k/scale_test.go
git commit -m "feat(decoder/htj2k): box-finish scaled decode to exact dims"
```

---

## Task 8: HTJ2K — quality sanity + benchmark

**Files:**
- Modify: `decoder/htj2k/scale_test.go`
- Create: `decoder/htj2k/scale_bench_test.go`

- [ ] **Step 1: Quality-sanity test** (mirror Task 4): scale-2 resolution decode vs full-decode + `boxhalve.Halve(.,1)`, mean-abs-diff ≤ 12.

```go
func TestHTJ2KScaleQualityClose(t *testing.T) {
	enc, err := encodeForTest(makeTestRGB(256, 256), 256, 256, 3)
	if err != nil {
		t.Fatal(err)
	}
	dec := (&factory{}).New()
	full, err := dec.Decode(enc, decoder.DecodeOptions{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	ref := boxhalve.Halve(full, 1)
	got, err := dec.Decode(enc, decoder.DecodeOptions{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	var sum, n int
	for i := range got.Pix {
		d := int(got.Pix[i]) - int(ref.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
		n++
	}
	if mean := float64(sum) / float64(n); mean > 12 {
		t.Errorf("mean abs diff %.2f too large", mean)
	}
}
```

- [ ] **Step 2: Run, verify pass.** Run: `go test ./decoder/htj2k/ -run TestHTJ2KScaleQualityClose -v`.

- [ ] **Step 3: Benchmark** resolution-decode vs full+box at scale 2 (mirror Task 4 Step 3, htj2k types).

- [ ] **Step 4: Run, record speedup.** Run: `go test ./decoder/htj2k/ -run '^$' -bench BenchmarkHTJ2K -benchmem`.

- [ ] **Step 5: Commit.**

```bash
git add decoder/htj2k/scale_test.go decoder/htj2k/scale_bench_test.go
git commit -m "test(decoder/htj2k): scaled-decode quality sanity + benchmark"
```

---

## Task 9: nocgo/nohtj2k stubs, docs, tracking

**Files:**
- Verify: `decoder/jpeg2000/jp2_nocgo.go`, `decoder/htj2k/htj2k_nocgo.go`
- Modify: `decoder/decoder.go` (Scale doc comment), `CHANGELOG.md`

- [ ] **Step 1: Confirm the stub builds.** The `nocgo`/`nohtj2k` stubs return `ErrCGORequired` before any Scale handling — no change needed, but confirm they still compile and that a `CGO_ENABLED=0` build is clean.

```bash
CGO_ENABLED=0 go build ./...
go build -tags nohtj2k ./...
```
Expected: both OK.

- [ ] **Step 2: Update the `DecodeOptions.Scale` doc** in `decoder/decoder.go`: change "(JPEG decoders only)" / "Non-JPEG decoders return ErrUnsupportedScale if Scale != 1" to note that `jpeg`, `jpeg2000`, and `htj2k` honor `{1,2,4,8}` (jpeg via IDCT, jp2k/htj2k via DWT resolution decode); others still reject `Scale != 1`.

- [ ] **Step 3: CHANGELOG** `[Unreleased]` entry: "decoder/jpeg2000 + decoder/htj2k honor DecodeOptions.Scale ∈ {1,2,4,8} via DWT resolution-level decode (#10, #12, #11) — faster, anti-aliased, seam-free downscaling; box-finish to exact ceil(src/scale) dims." Include the measured speedups from Tasks 4 & 8.

- [ ] **Step 4: Full gate.**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
go vet ./...
```
Expected: green.

- [ ] **Step 5: Commit + close tracking.**

```bash
git add decoder/decoder.go CHANGELOG.md
git commit -m "docs: jp2k/htj2k Scale support; CHANGELOG; closes #10 #12"
```

Update #11's checklist (jpeg2000 + htj2k boxes) and note #10/#12 closed. The umbrella #11 stays open for the remaining `webp` / `jpegxl` items.

---

## Self-Review

- **Spec coverage:** #10 → Tasks 2,3,4 (Scale validation, cp_reduce resolution decode, box-finish, quality, bench). #12 → Tasks 5,6,7,8 (test-encoder levels, restrict_input_resolution, box-finish, quality, bench). #11 contract (`{1,2,4,8}`, `ErrUnsupportedScale`, `ceil` dims) → Tasks 2/6 mirror the jpeg decoder exactly. Box-finish clamp policy (sealed decision) → Tasks 3,7. "Read upstream first" → Task 0 spike + header confirmations in the header. #7 composition → asserted by keeping `TestDecodeSubsampled422`/`line->size` packing and re-running them. nocgo/nohtj2k + docs → Task 9.
- **Placeholders:** Task 0 is a genuine confirm-upstream spike (the issues mandate it), not a TODO. The two spots that say "show the full C edit in the implementation" (Tasks 2/3/6) reference confirmed APIs (`cp_reduce`, `restrict_input_resolution`) with exact insertion points and parameter shapes — the executor writes the C using the named call at the named location; this is direction with the mechanism specified, not a vague placeholder.
- **Type consistency:** `boxhalve.Halve(img, times)` / `boxhalve.To(img, w, h)` used consistently (Tasks 1,3,4,7,8). `encodeForTest(src, w, h, numDecomp)` consistent (Tasks 5–8). `resFactor`/`resolution_factor` naming consistent across Go/C. Scale switch identical in Tasks 2 and 6. Output-dims formula `(d+s-1)/s` matches the jpeg decoder.
