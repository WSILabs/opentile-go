//go:build cgo && !nocgo && !nowebp

package webp

import (
	"errors"
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// sampleTile is a 240x240 RGB WebP tile (transcoded from CMU-1-Small-Region).
func sampleTile(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/sample_tile.webp")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mean returns the average byte across pix (a cheap "same image" signal:
// codec-domain downscale preserves the mean within a few LSB).
func mean(pix []byte) float64 {
	var sum float64
	for _, b := range pix {
		sum += float64(b)
	}
	return sum / float64(len(pix))
}

// TestWebPScaleDims: libwebp's rescaler yields ceil(src/Scale) dims for the
// {1,2,4,8} contract, the raster stays non-constant, and the mean is preserved
// (proving it downscales the actual content, not garbage) (GH #11).
func TestWebPScaleDims(t *testing.T) {
	src := sampleTile(t)
	full, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: 1, Format: decoder.PixelFormatRGB})
	if err != nil {
		t.Fatalf("scale 1: %v", err)
	}
	fullMean := mean(full.Pix)

	for _, tc := range []struct{ scale, dim int }{
		{1, 240}, {2, 120}, {4, 60}, {8, 30},
	} {
		img, err := (&factory{}).New().Decode(src, decoder.DecodeOptions{Scale: tc.scale, Format: decoder.PixelFormatRGB})
		if err != nil {
			t.Fatalf("scale %d: %v", tc.scale, err)
		}
		if img.Width != tc.dim || img.Height != tc.dim {
			t.Errorf("scale %d: dims %dx%d, want %dx%d", tc.scale, img.Width, img.Height, tc.dim, tc.dim)
		}
		mn, mx := byte(255), byte(0)
		for _, b := range img.Pix {
			if b < mn {
				mn = b
			}
			if b > mx {
				mx = b
			}
		}
		if mn == mx {
			t.Errorf("scale %d: decoded to a constant image", tc.scale)
		}
		if m := mean(img.Pix); m < fullMean-6 || m > fullMean+6 {
			t.Errorf("scale %d: mean %.1f far from full-res mean %.1f — not a downscale of the same image", tc.scale, m, fullMean)
		}
	}
}

// TestWebPScaleRGBA: scaling works for the RGBA output format too.
func TestWebPScaleRGBA(t *testing.T) {
	img, err := (&factory{}).New().Decode(sampleTile(t), decoder.DecodeOptions{Scale: 4, Format: decoder.PixelFormatRGBA})
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 60 || img.Height != 60 {
		t.Fatalf("RGBA scale 4: dims %dx%d, want 60x60", img.Width, img.Height)
	}
}

// TestWebPScaleUnsupported: non-{1,2,4,8} factors return ErrUnsupportedScale so
// the consumer falls back to full-decode + spatial reduction.
func TestWebPScaleUnsupported(t *testing.T) {
	_, err := (&factory{}).New().Decode(sampleTile(t), decoder.DecodeOptions{Scale: 3})
	if !errors.Is(err, decoder.ErrUnsupportedScale) {
		t.Fatalf("scale 3: err = %v, want ErrUnsupportedScale", err)
	}
}
