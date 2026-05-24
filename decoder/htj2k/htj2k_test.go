//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatalf("htj2k decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 60003 {
		t.Errorf("TIFFCompressionTags: got %v want [60003]", got)
	}
}

// TestRoundTrip encodes a synthetic 8x4 RGB image via encodeTestLossless
// (lossless HTJ2K) and decodes it back; checks pixel-exact match.
func TestRoundTrip(t *testing.T) {
	const w, h = 8, 4
	orig := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			orig[i+0] = byte(x * 8)
			orig[i+1] = byte(y * 32)
			orig[i+2] = 128
		}
	}

	cs, err := encodeTestLossless(orig, w, h)
	if err != nil {
		t.Fatalf("encodeTestLossless: %v", err)
	}
	if len(cs) == 0 {
		t.Fatal("empty codestream")
	}

	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatal("htj2k decoder not registered")
	}
	dec := f.New()
	defer dec.Close()

	img, err := dec.Decode(cs, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Width != w || img.Height != h {
		t.Fatalf("dims: got %dx%d want %dx%d", img.Width, img.Height, w, h)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			oi := (y*w + x) * 3
			di := y*img.Width*3 + x*3
			if img.Pix[di] != orig[oi] || img.Pix[di+1] != orig[oi+1] || img.Pix[di+2] != orig[oi+2] {
				t.Errorf("pixel (%d,%d): got (%d,%d,%d) want (%d,%d,%d)",
					x, y,
					img.Pix[di], img.Pix[di+1], img.Pix[di+2],
					orig[oi], orig[oi+1], orig[oi+2])
			}
		}
	}
}
