//go:build cgo && !nocgo && !nojxl

package jpegxl

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatalf("jpegxl decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 50002 {
		t.Errorf("TIFFCompressionTags: got %v want [50002]", got)
	}
}

// TestDecodeSampleTile is the end-to-end decode gate: decode a real 240×240
// JPEG-XL tile and confirm actual image data comes back. Guards the bug where
// the decoder subscribed to JXL_DEC_NEED_IMAGE_OUT_BUFFER (a return-only status,
// not a subscribable flag), which made libjxl reject the event subscription and
// every decode fail with "corrupt input data".
func TestDecodeSampleTile(t *testing.T) {
	b, err := os.ReadFile("testdata/sample_tile.jxl")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatal("jpegxl decoder not registered")
	}
	d := f.New()
	defer d.Close()

	img, err := d.Decode(b, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode(RGB): %v", err)
	}
	if img.Width != 240 || img.Height != 240 {
		t.Errorf("dims = %dx%d, want 240x240", img.Width, img.Height)
	}
	if img.Format != decoder.PixelFormatRGB {
		t.Errorf("format = %v, want RGB", img.Format)
	}
	if !hasVariation(img) {
		t.Error("decoded pixels are uniform — decode did not produce real image data")
	}

	// RGBA path.
	rgba, err := d.Decode(b, decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
	if err != nil {
		t.Fatalf("Decode(RGBA): %v", err)
	}
	if rgba.Width != 240 || rgba.Height != 240 || rgba.Format != decoder.PixelFormatRGBA {
		t.Errorf("RGBA decode = %dx%d fmt=%v, want 240x240 RGBA", rgba.Width, rgba.Height, rgba.Format)
	}

	// Decode-into a caller-provided destination of the right size.
	dst := decoder.NewImageFormat(240, 240, decoder.PixelFormatRGB)
	if _, err := d.Decode(b, decoder.DecodeOptions{Dst: dst}); err != nil {
		t.Fatalf("Decode(Dst): %v", err)
	}
}

// hasVariation reports whether any pixel differs from the top-left pixel — a
// cheap proof that a real (non-flat) image was decoded. Stride-aware.
func hasVariation(img *decoder.Image) bool {
	bpp := 3
	if img.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	r0, g0, b0 := img.Pix[0], img.Pix[1], img.Pix[2]
	for y := 0; y < img.Height; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < img.Width; x++ {
			i := x * bpp
			if row[i] != r0 || row[i+1] != g0 || row[i+2] != b0 {
				return true
			}
		}
	}
	return false
}
