package assocdecode

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

func makePNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestViaCodecPNG covers the GH #74 path: CompressionPNG decodes via the
// standard library (no cgo decoder registered). Fixture-free, CI-safe.
func TestViaCodecPNG(t *testing.T) {
	data := makePNG(t, 8, 6, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	t.Run("RGB", func(t *testing.T) {
		img, err := ViaCodec(opentile.CompressionPNG, data, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 8 || img.Height != 6 {
			t.Fatalf("dims %dx%d, want 8x6", img.Width, img.Height)
		}
		if img.Format != decoder.PixelFormatRGB {
			t.Errorf("format = %v, want RGB", img.Format)
		}
		if img.Pix[0] != 10 || img.Pix[1] != 20 || img.Pix[2] != 30 {
			t.Errorf("px0 = %d,%d,%d, want 10,20,30", img.Pix[0], img.Pix[1], img.Pix[2])
		}
	})

	t.Run("RGBA keeps alpha", func(t *testing.T) {
		img, err := ViaCodec(opentile.CompressionPNG, data, decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
		if err != nil {
			t.Fatal(err)
		}
		if img.Format != decoder.PixelFormatRGBA || img.Pix[3] != 255 {
			t.Errorf("format=%v alpha=%d, want RGBA / 255", img.Format, img.Pix[3])
		}
	})

	t.Run("scale 2 downsamples", func(t *testing.T) {
		img, err := ViaCodec(opentile.CompressionPNG, makePNG(t, 8, 8, color.RGBA{R: 200, A: 255}), decoder.DecodeOptions{Scale: 2})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 4 || img.Height != 4 {
			t.Errorf("scaled dims %dx%d, want 4x4", img.Width, img.Height)
		}
	})

	t.Run("unsupported scale", func(t *testing.T) {
		if _, err := ViaCodec(opentile.CompressionPNG, data, decoder.DecodeOptions{Scale: 3}); !errors.Is(err, decoder.ErrUnsupportedScale) {
			t.Errorf("scale=3 err = %v, want ErrUnsupportedScale", err)
		}
	})

	t.Run("garbage bytes error", func(t *testing.T) {
		if _, err := ViaCodec(opentile.CompressionPNG, []byte("not a png"), decoder.DecodeOptions{}); err == nil {
			t.Error("want error decoding non-PNG bytes")
		}
	})
}
