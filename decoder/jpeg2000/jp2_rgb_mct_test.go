//go:build cgo && !nocgo && !nojp2k

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/j2kheader"
)

// TestDecodeRGBMCTNotMisreadAsYCbCr: a standard RGB raw J2K codestream encoded
// with the multiple-component transform (MCT) — the form wsitools' JP2K encoder
// emits — must decode to its true RGB. OpenJPEG already inverts the MCT during
// decode, so the components are RGB; the decoder must NOT apply a further
// YCbCr->RGB conversion. The codestream carries no JP2 colorspace box (raw J2K,
// FF4F), so the old blanket "3-component non-sRGB => YCbCr" heuristic misread
// it. (GH #53)
func TestDecodeRGBMCTNotMisreadAsYCbCr(t *testing.T) {
	src, err := os.ReadFile("testdata/rgb_mct_solid.j2k")
	if err != nil {
		t.Fatal(err)
	}

	// Sanity: the fixture genuinely uses MCT — otherwise the test wouldn't
	// exercise the MCT branch of the fix.
	h, err := j2kheader.Parse(src)
	if err != nil {
		t.Fatalf("parse fixture header: %v", err)
	}
	if !h.MCT {
		t.Fatalf("fixture must use MCT to exercise GH #53; got MCT=%v", h.MCT)
	}

	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Fatal("jpeg2000 decoder not registered")
	}
	d := f.New()
	defer d.Close()

	img, err := d.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(img.Pix) < 3 {
		t.Fatalf("short pixel buffer: %d bytes", len(img.Pix))
	}

	// The fixture is a solid RGB(200,50,100) encoded losslessly (reversible
	// 5/3 + reversible MCT), so the decode must be byte-exact.
	got := [3]byte{img.Pix[0], img.Pix[1], img.Pix[2]}
	want := [3]byte{200, 50, 100}
	if got != want {
		t.Fatalf("first pixel = %v, want %v — RGB-MCT codestream misread as YCbCr", got, want)
	}
}
