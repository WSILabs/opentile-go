package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/j2kheader"
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

// decodedColorSpaceFromHeader must cover every rule branch — including the
// enumerated-box cases (sRGB / greyscale / sYCC) and the grayscale-component
// case that no real .j2k fixture in testdata exercises. Constructed headers pin
// them deterministically. EnumColorspace -1 means "no colr box" (j2kheader's
// no-signal sentinel).
func TestDecodedColorSpaceFromHeaderBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    j2kheader.Info
		want decoder.ColorEncoding
	}{
		{"grayscale-1comp", j2kheader.Info{Components: 1, EnumColorspace: -1}, decoder.ColorGrayscale},
		{"mct-3comp", j2kheader.Info{Components: 3, MCT: true, EnumColorspace: -1}, decoder.ColorRGB},
		{"srgb-box", j2kheader.Info{Components: 3, EnumColorspace: 16}, decoder.ColorRGB},
		{"greyscale-box", j2kheader.Info{Components: 3, EnumColorspace: 17}, decoder.ColorGrayscale},
		{"sycc-box", j2kheader.Info{Components: 3, EnumColorspace: 18}, decoder.ColorYCbCr},
		{"no-signal", j2kheader.Info{Components: 3, EnumColorspace: -1}, decoder.ColorYCbCr},
		{"mct-beats-box", j2kheader.Info{Components: 3, MCT: true, EnumColorspace: 18}, decoder.ColorRGB},
	} {
		if got := decodedColorSpaceFromHeader(tc.h); got != tc.want {
			t.Errorf("%s: decodedColorSpaceFromHeader = %s, want %s", tc.name, got, tc.want)
		}
	}
}
