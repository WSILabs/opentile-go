//go:build cgo && !nocgo

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// TestDecodeRGBAFormat verifies the decoder honors opts.Format ==
// PixelFormatRGBA (GH #8 bug 1). Before the fix jpeg2000 always returned
// RGB regardless of the requested format — the lone codec that ignored
// opts.Format. The RGBA result must carry opaque alpha and RGB channels
// identical to the RGB decode.
func TestDecodeRGBAFormat(t *testing.T) {
	src, err := os.ReadFile("testdata/subsampled_422_256.j2k")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dec := (&factory{}).New()
	rgb, err := dec.Decode(src, decoder.DecodeOptions{}) // default RGB
	if err != nil {
		t.Fatalf("rgb decode: %v", err)
	}
	rgba, err := dec.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
	if err != nil {
		t.Fatalf("rgba decode: %v", err)
	}
	if rgba.Format != decoder.PixelFormatRGBA {
		t.Fatalf("Format = %v, want PixelFormatRGBA", rgba.Format)
	}
	if rgba.Stride != 4*rgba.Width {
		t.Fatalf("Stride = %d, want %d", rgba.Stride, 4*rgba.Width)
	}
	if len(rgba.Pix) != 4*rgba.Width*rgba.Height {
		t.Fatalf("len(Pix) = %d, want %d", len(rgba.Pix), 4*rgba.Width*rgba.Height)
	}
	for y := 0; y < rgba.Height; y++ {
		for x := 0; x < rgba.Width; x++ {
			ro := y*rgb.Stride + x*3
			ao := y*rgba.Stride + x*4
			for c := 0; c < 3; c++ {
				if rgba.Pix[ao+c] != rgb.Pix[ro+c] {
					t.Fatalf("pixel (%d,%d) ch %d: rgba=%d rgb=%d", x, y, c, rgba.Pix[ao+c], rgb.Pix[ro+c])
				}
			}
			if rgba.Pix[ao+3] != 0xFF {
				t.Fatalf("pixel (%d,%d) alpha = %d, want 255", x, y, rgba.Pix[ao+3])
			}
		}
	}
}
