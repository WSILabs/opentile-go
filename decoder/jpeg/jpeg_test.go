//go:build cgo && !nocgo

package jpeg

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpeg")
	if !ok {
		t.Fatalf("jpeg decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 7 {
		t.Errorf("TIFFCompressionTags: got %v want [7]", got)
	}
}

func TestDecodeBasic(t *testing.T) {
	// Encode a 16x16 RGB image as JPEG using the stdlib encoder.
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.Pix[(y*16+x)*4+0] = byte(x * 16)
			src.Pix[(y*16+x)*4+1] = byte(y * 16)
			src.Pix[(y*16+x)*4+2] = 128
			src.Pix[(y*16+x)*4+3] = 0xFF
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90})

	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Width != 16 || got.Height != 16 {
		t.Errorf("dimensions: got %dx%d want 16x16", got.Width, got.Height)
	}
	if got.Format != decoder.PixelFormatRGB {
		t.Errorf("format: got %d want RGB", got.Format)
	}
	if got.Stride != 16*3 {
		t.Errorf("stride: got %d want %d", got.Stride, 16*3)
	}
}

func TestDecodeIDCTScale(t *testing.T) {
	// Encode 32x32, decode at scale 2 -> 16x16.
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90})

	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{Scale: 2})
	if err != nil {
		t.Fatalf("Decode scale=2: %v", err)
	}
	if got.Width != 16 || got.Height != 16 {
		t.Errorf("scale=2 dimensions: got %dx%d want 16x16", got.Width, got.Height)
	}
}

func TestDecodeUnsupportedScale(t *testing.T) {
	f, _ := decoder.Get("jpeg")
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0xD8, 0xFF, 0xD9}, decoder.DecodeOptions{Scale: 3})
	if err == nil {
		t.Errorf("scale=3: expected error")
	}
}
