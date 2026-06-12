package dicom

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// rgbRaster builds an interleaved RGB raster with a distinct, non-gray triple
// per pixel so a grayscale collapse (the GH #21 bug) is detectable.
func rgbRaster(w, h int) []byte {
	raw := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		raw[i*3+0] = byte(i*7 + 1)
		raw[i*3+1] = byte(i*13 + 50)
		raw[i*3+2] = byte(i*29 + 100)
	}
	return raw
}

// TestNativeAssociatedRGB_PadByte is the GH #21 regression: a native
// (uncompressed) 8-bit RGB associated image whose w*h*3 is ODD carries one
// even-length pad byte (PS3.5 §7.1.1). It must decode to the correct RGB —
// not collapse to grayscale — both with the authoritative SamplesPerPixel and
// with length inference when it's absent.
func TestNativeAssociatedRGB_PadByte(t *testing.T) {
	const w, h = 3, 5 // w*h*3 = 45 (odd) → padded to 46
	raw := rgbRaster(w, h)
	padded := append(append([]byte(nil), raw...), 0x00) // even-length pad
	if len(padded) != w*h*3+1 {
		t.Fatalf("setup: padded len %d", len(padded))
	}

	for _, tc := range []struct {
		name    string
		samples int // 0 = absent → exercise inference
	}{
		{"authoritative SamplesPerPixel=3", 3},
		{"inferred from padded length", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &associatedImage{
				typ:         "label",
				size:        opentile.Size{W: w, H: h},
				compression: opentile.CompressionNone,
				data:        padded,
				samples:     tc.samples,
				photometric: "RGB",
			}
			img, err := a.Decode(decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if img.Width != w || img.Height != h {
				t.Fatalf("size %dx%d, want %dx%d", img.Width, img.Height, w, h)
			}
			for i := 0; i < w*h; i++ {
				y, x := i/w, i%w
				do := y*img.Stride + x*3
				for c := 0; c < 3; c++ {
					if img.Pix[do+c] != raw[i*3+c] {
						t.Fatalf("pixel %d chan %d: got %d, want %d (grayscale collapse?)",
							i, c, img.Pix[do+c], raw[i*3+c])
					}
				}
			}
		})
	}
}

// TestNativeAssociatedMonochrome confirms a native MONOCHROME2 1-sample
// associated image still decodes correctly (gray replicated to R=G=B).
func TestNativeAssociatedMonochrome(t *testing.T) {
	const w, h = 4, 3
	raw := make([]byte, w*h)
	for i := range raw {
		raw[i] = byte(i*11 + 3)
	}
	a := &associatedImage{
		typ:         "label",
		size:        opentile.Size{W: w, H: h},
		compression: opentile.CompressionNone,
		data:        raw,
		samples:     1,
		photometric: "MONOCHROME2",
	}
	img, err := a.Decode(decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for i := 0; i < w*h; i++ {
		v := raw[i]
		do := (i/w)*img.Stride + (i%w)*3
		if img.Pix[do] != v || img.Pix[do+1] != v || img.Pix[do+2] != v {
			t.Fatalf("pixel %d: got %d,%d,%d want %d", i, img.Pix[do], img.Pix[do+1], img.Pix[do+2], v)
		}
	}
}
