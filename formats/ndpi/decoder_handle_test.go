//go:build cgo && !nocgo

package ndpi

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg"
)

func TestDecoderHandleSequential(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	defer func() {
		if err := h.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	src := tinyJPEG(t)
	for i := 0; i < 4; i++ {
		img, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if img == nil || len(img.Pix) == 0 {
			t.Fatalf("iter %d: empty image", i)
		}
	}
}

func TestDecoderHandleConcurrent(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	defer h.Close()

	src := tinyJPEG(t)
	var wg sync.WaitGroup
	const N = 32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			if img == nil || len(img.Pix) == 0 {
				t.Errorf("empty")
			}
		}()
	}
	wg.Wait()
}

func TestDecoderHandleDecodeAfterClose(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	src := tinyJPEG(t)
	_, err := h.Decode(src, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
	if err == nil {
		t.Fatal("Decode after Close returned nil error, want non-nil")
	}
}

func TestDecoderHandleDoubleClose(t *testing.T) {
	h := newDecoderHandle(opentile.CompressionJPEG)
	if err := h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// tinyJPEG returns a small valid JPEG (8x8 all-white RGB) for handle
// testing. Generated at test-time via stdlib image/jpeg so libjpeg-turbo
// is guaranteed to accept it.
func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode tiny JPEG: %v", err)
	}
	return buf.Bytes()
}
