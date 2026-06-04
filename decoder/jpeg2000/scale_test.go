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

// TestJP2KScaleDims: resolution-level decode yields ceil(src/Scale) dims.
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

// TestJP2KScaleBoxFinish: a codestream with only 2 resolution levels can
// reduce at most 1 level in the codec; Scale 4/8 must box-finish the
// residual to reach exactly ceil(src/Scale). (cp_reduce beyond available
// levels makes opj_read_header fail, so the decoder retries down then
// box-finishes — see jp2_cgo.go.)
func TestJP2KScaleBoxFinish(t *testing.T) {
	b, err := os.ReadFile("testdata/lowres_2levels.j2k")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	for _, tc := range []struct{ scale, w int }{
		{1, 256}, {2, 128}, {4, 64}, {8, 32},
	} {
		img, err := (&factory{}).New().Decode(b, decoder.DecodeOptions{Scale: tc.scale})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.w || img.Height != tc.w {
			t.Errorf("scale %d: dims %dx%d, want %dx%d", tc.scale, img.Width, img.Height, tc.w, tc.w)
		}
	}
}

// TestJP2KScaleUnsupported: non-power-of-2 / >8 reject like the jpeg decoder.
func TestJP2KScaleUnsupported(t *testing.T) {
	src := readJP2KFixture(t)
	for _, s := range []int{3, 5, 6, 16} {
		_, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: s})
		if err == nil {
			t.Errorf("scale %d: expected ErrUnsupportedScale", s)
		}
	}
}
