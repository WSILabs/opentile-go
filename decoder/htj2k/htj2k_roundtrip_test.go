//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// TestRoundTripRGBA encodes a synthetic 8x4 RGB image via encodeTestLossless
// (lossless HTJ2K) and decodes it requesting PixelFormatRGBA output.
// It checks: correct format, correct dimensions, R/G/B match original (lossless),
// and alpha == 0xFF for every pixel.
func TestRoundTripRGBA(t *testing.T) {
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

	cs, err := encodeTestLossless(orig, w, h, 1)
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

	img, err := dec.Decode(cs, decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
	if err != nil {
		t.Fatalf("Decode RGBA: %v", err)
	}
	if img.Format != decoder.PixelFormatRGBA {
		t.Errorf("Format: got %v want PixelFormatRGBA", img.Format)
	}
	if img.Width != w || img.Height != h {
		t.Fatalf("dims: got %dx%d want %dx%d", img.Width, img.Height, w, h)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			oi := (y*w + x) * 3
			di := y*img.Stride + x*4
			gotR := img.Pix[di+0]
			gotG := img.Pix[di+1]
			gotB := img.Pix[di+2]
			gotA := img.Pix[di+3]
			wantR := orig[oi+0]
			wantG := orig[oi+1]
			wantB := orig[oi+2]
			if gotR != wantR || gotG != wantG || gotB != wantB {
				t.Errorf("pixel (%d,%d) RGB: got (%d,%d,%d) want (%d,%d,%d)",
					x, y, gotR, gotG, gotB, wantR, wantG, wantB)
			}
			if gotA != 0xFF {
				t.Errorf("pixel (%d,%d) alpha: got %d want 255", x, y, gotA)
			}
		}
	}
}

// TestRoundTripRGBAWithDst is the same test but uses a caller-provided opts.Dst
// allocated via decoder.NewImageFormat(w, h, PixelFormatRGBA).
func TestRoundTripRGBAWithDst(t *testing.T) {
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

	cs, err := encodeTestLossless(orig, w, h, 1)
	if err != nil {
		t.Fatalf("encodeTestLossless: %v", err)
	}

	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatal("htj2k decoder not registered")
	}
	dec := f.New()
	defer dec.Close()

	dst := decoder.NewImageFormat(w, h, decoder.PixelFormatRGBA)
	got, err := dec.Decode(cs, decoder.DecodeOptions{
		Format: decoder.PixelFormatRGBA,
		Dst:    dst,
	})
	if err != nil {
		t.Fatalf("Decode RGBA with Dst: %v", err)
	}
	if got != dst {
		t.Errorf("returned image pointer != supplied Dst")
	}
	if got.Format != decoder.PixelFormatRGBA {
		t.Errorf("Format: got %v want PixelFormatRGBA", got.Format)
	}
	if got.Width != w || got.Height != h {
		t.Fatalf("dims: got %dx%d want %dx%d", got.Width, got.Height, w, h)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			oi := (y*w + x) * 3
			di := y*got.Stride + x*4
			gotR := got.Pix[di+0]
			gotG := got.Pix[di+1]
			gotB := got.Pix[di+2]
			gotA := got.Pix[di+3]
			wantR := orig[oi+0]
			wantG := orig[oi+1]
			wantB := orig[oi+2]
			if gotR != wantR || gotG != wantG || gotB != wantB {
				t.Errorf("pixel (%d,%d) RGB: got (%d,%d,%d) want (%d,%d,%d)",
					x, y, gotR, gotG, gotB, wantR, wantG, wantB)
			}
			if gotA != 0xFF {
				t.Errorf("pixel (%d,%d) alpha: got %d want 255", x, y, gotA)
			}
		}
	}
}
