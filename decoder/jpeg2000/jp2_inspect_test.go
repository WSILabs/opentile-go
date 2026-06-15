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
		file       string
		lossless   decoder.Lossless
		color      decoder.ColorEncoding
		components int
	}{
		// Reversible 5/3 + MCT → YBR_RCT, lossless.
		{"testdata/lowres_2levels.j2k", decoder.LosslessYes, decoder.ColorYBRRCT, 3},
		// Irreversible 9/7, no MCT.
		{"testdata/subsampled_422_256.j2k", decoder.LosslessNo, decoder.ColorRGB, 3},
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
			ci.ColorEncoding != tc.color || ci.Boxed {
			t.Errorf("%s inspect = %+v, want comps=%d depth=8 lossless=%s color=%s raw",
				tc.file, ci, tc.components, tc.lossless, tc.color)
		}
	}

	if _, err := p.Inspect([]byte{0xFF, 0xD8}); err == nil { // JPEG SOI, not J2K
		t.Error("expected error probing non-J2K bytes")
	}
}
