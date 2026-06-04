//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"math"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/boxhalve"
)

// makeTestRGB builds a deterministic, spatially-varying RGB image so that
// resolution decode and box reduction are actually exercised.
func makeTestRGB(w, h int) []byte {
	b := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			// Smooth, non-wrapping gradients + low-frequency sine. Real WSI
			// content is not near-Nyquist aliased, where the wavelet low-pass
			// and a box average diverge most.
			b[i+0] = byte(x % 256)
			b[i+1] = byte(y % 256)
			b[i+2] = byte(int(40*math.Sin(float64(x+y)/16.0)) + 128)
		}
	}
	return b
}

// TestHTJ2KScaleDims: a 3-level (numDecomp=3) codestream reduces fully in
// the codec for all of {1,2,4,8} → exact ceil(src/Scale) dims.
func TestHTJ2KScaleDims(t *testing.T) {
	enc, err := encodeTestLossless(makeTestRGB(256, 256), 256, 256, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ scale, w int }{{1, 256}, {2, 128}, {4, 64}, {8, 32}} {
		img, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: tc.scale})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.w || img.Height != tc.w {
			t.Errorf("scale %d: %dx%d want %dx%d", tc.scale, img.Width, img.Height, tc.w, tc.w)
		}
	}
}

// TestHTJ2KScaleBoxFinish: a 1-level codestream can reduce at most 1 level
// in the codec; Scale 4/8 must box-finish the residual to exact dims.
func TestHTJ2KScaleBoxFinish(t *testing.T) {
	enc, err := encodeTestLossless(makeTestRGB(256, 256), 256, 256, 1) // 1 decomposition only
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ scale, w int }{{1, 256}, {2, 128}, {4, 64}, {8, 32}} {
		img, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: tc.scale})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.w || img.Height != tc.w {
			t.Errorf("scale %d: %dx%d want %dx%d", tc.scale, img.Width, img.Height, tc.w, tc.w)
		}
	}
}

// TestHTJ2KScaleUnsupported: non-power-of-2 / >8 reject.
func TestHTJ2KScaleUnsupported(t *testing.T) {
	enc, _ := encodeTestLossless(makeTestRGB(64, 64), 64, 64, 1)
	for _, s := range []int{3, 5, 7} {
		if _, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{Scale: s}); err == nil {
			t.Errorf("scale %d: want ErrUnsupportedScale", s)
		}
	}
}

// TestHTJ2KScaleQualityClose: Scale 2 resolution decode should be in the
// same ballpark as full-decode + box (not bit-equal — wavelet vs box).
func TestHTJ2KScaleQualityClose(t *testing.T) {
	enc, err := encodeTestLossless(makeTestRGB(256, 256), 256, 256, 3)
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
	if got.Width != ref.Width || got.Height != ref.Height {
		t.Fatalf("dims %dx%d vs %dx%d", got.Width, got.Height, ref.Width, ref.Height)
	}
	var sum int
	for i := range got.Pix {
		d := int(got.Pix[i]) - int(ref.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	if mean := float64(sum) / float64(len(got.Pix)); mean > 12 {
		t.Errorf("mean abs diff %.2f too large (resolution decode vs box)", mean)
	}
}

// TestEncodeWithLevels proves the parameterized test encoder produces a
// valid codestream at multiple decomposition levels (needed to exercise
// Scale 4/8 resolution decode in later tests).
func TestEncodeWithLevels(t *testing.T) {
	enc, err := encodeTestLossless(makeTestRGB(256, 256), 256, 256, 3)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("empty codestream")
	}
	// Sanity: it decodes back at scale 1 to the original dims.
	img, err := (&factory{}).New().Decode(enc, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Fatalf("dims %dx%d, want 256x256", img.Width, img.Height)
	}
}
