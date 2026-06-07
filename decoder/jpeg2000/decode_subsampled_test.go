//go:build cgo && !nocgo && !nojp2k

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// golden422 holds RGB samples produced by opj_decompress (OpenJPEG 2.5.4
// CLI) decoding testdata/subsampled_422_256.j2k — an independent reference
// for the same 4:2:2-subsampled tile. Coordinates span y<128 (where the
// buggy single-index packing reads wrong-but-mapped chroma) and y>=128
// (where it reads past the end of the 128x256 chroma planes).
var golden422 = []struct{ x, y, r, g, b int }{
	{0, 0, 255, 253, 255},
	{2, 0, 254, 253, 253},
	{64, 10, 248, 239, 251},
	{130, 5, 235, 234, 250},
	{255, 100, 238, 242, 214},
	{10, 128, 249, 251, 254},
	{200, 200, 246, 248, 246},
	{255, 255, 251, 253, 247},
}

// TestDecodeSubsampled422 is the regression guard for the chroma-subsampled
// over-read (GH #7). Before the fix the packing loop indexes every component
// with i in [0, w*h), over-reading the half-width chroma planes — yielding
// scrambled/garbage chroma (and intermittently SIGBUS). After the fix each
// component is indexed by its own geometry, so the decode matches the
// independent opj_decompress reference within rounding tolerance.
func TestDecodeSubsampled422(t *testing.T) {
	src, err := os.ReadFile("testdata/subsampled_422_256.j2k")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	img, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Fatalf("dims = %dx%d, want 256x256", img.Width, img.Height)
	}
	const tol = 6 // opj_decompress sYCC->RGB vs our conversion rounding
	for _, g := range golden422 {
		o := g.y*img.Stride + g.x*3
		r, gg, b := int(img.Pix[o]), int(img.Pix[o+1]), int(img.Pix[o+2])
		if iabs(r-g.r) > tol || iabs(gg-g.g) > tol || iabs(b-g.b) > tol {
			t.Errorf("pixel (%d,%d) = (%d,%d,%d), want ~(%d,%d,%d)",
				g.x, g.y, r, gg, b, g.r, g.g, g.b)
		}
	}
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
