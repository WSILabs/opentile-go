//go:build cgo && !nocgo

package jpeg_test

import (
	"bytes"
	"image"
	"image/color"
	gojpeg "image/jpeg"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func encodeJPEG(t *testing.T, im image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gojpeg.Encode(&b, im, &gojpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestJPEGProbe(t *testing.T) {
	f, ok := decoder.Get("jpeg")
	if !ok {
		t.Skip("jpeg decoder not registered")
	}
	p, ok := f.(decoder.Prober)
	if !ok {
		t.Fatal("jpeg factory does not implement decoder.Prober")
	}

	// Color JPEG (image/jpeg encodes YCbCr).
	rgb := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			rgb.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 16), 128, 255})
		}
	}
	ci, err := p.Probe(encodeJPEG(t, rgb))
	if err != nil {
		t.Fatal(err)
	}
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessNo ||
		ci.ColorEncoding != decoder.ColorYCbCr || ci.Boxed {
		t.Errorf("color JPEG probe = %+v, want comps=3 depth=8 lossy YCbCr raw", ci)
	}

	// Grayscale JPEG.
	gray := image.NewGray(image.Rect(0, 0, 16, 16))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i)
	}
	ci, err = p.Probe(encodeJPEG(t, gray))
	if err != nil {
		t.Fatal(err)
	}
	if ci.Components != 1 || ci.ColorEncoding != decoder.ColorGrayscale {
		t.Errorf("gray JPEG probe = %+v, want comps=1 grayscale", ci)
	}

	// Corrupt input → error.
	if _, err := p.Probe([]byte{0x00, 0x01, 0x02}); err == nil {
		t.Error("expected error probing non-JPEG bytes")
	}
}
