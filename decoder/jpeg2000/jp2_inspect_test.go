//go:build cgo && !nocgo && !nojp2k

package jpeg2000_test

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

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
