package resample

import (
	"math"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// lanczosNaiveRef is a verbatim copy of the original non-separable 2-D
// Lanczos convolution. It is the correctness oracle for the separable
// rewrite: lanczosSeparableInto must match it to within 1 LSB per channel.
func lanczosNaiveRef(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	scaleX := float64(src.Width) / float64(dst.Width)
	scaleY := float64(src.Height) / float64(dst.Height)
	supportX := lanczosA
	if scaleX > 1 {
		supportX = lanczosA * scaleX
	}
	supportY := lanczosA
	if scaleY > 1 {
		supportY = lanczosA * scaleY
	}
	for dy := 0; dy < dst.Height; dy++ {
		centerY := (float64(dy)+0.5)*scaleY - 0.5
		y0 := int(math.Floor(centerY - supportY))
		y1 := int(math.Ceil(centerY + supportY))
		if y0 < 0 {
			y0 = 0
		}
		if y1 >= src.Height {
			y1 = src.Height - 1
		}
		for dx := 0; dx < dst.Width; dx++ {
			centerX := (float64(dx)+0.5)*scaleX - 0.5
			x0 := int(math.Floor(centerX - supportX))
			x1 := int(math.Ceil(centerX + supportX))
			if x0 < 0 {
				x0 = 0
			}
			if x1 >= src.Width {
				x1 = src.Width - 1
			}
			var acc [4]float64
			var wSum float64
			for sy := y0; sy <= y1; sy++ {
				wy := lanczosWeight((float64(sy) - centerY) / scaleY)
				for sx := x0; sx <= x1; sx++ {
					wx := lanczosWeight((float64(sx) - centerX) / scaleX)
					w := wx * wy
					off := sy*src.Stride + sx*bpp
					for c := 0; c < bpp; c++ {
						acc[c] += float64(src.Pix[off+c]) * w
					}
					wSum += w
				}
			}
			dstOff := dy*dst.Stride + dx*bpp
			if wSum == 0 {
				wSum = 1
			}
			for c := 0; c < bpp; c++ {
				v := acc[c]/wSum + 0.5
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				dst.Pix[dstOff+c] = byte(v)
			}
		}
	}
	return nil
}

// fillLanczosPattern writes a deterministic, spatially-varying pattern so
// that resampling weights and edge clamping actually exercise the kernel.
func fillLanczosPattern(img *decoder.Image) {
	bpp := 3
	if img.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			off := y*img.Stride + x*bpp
			img.Pix[off+0] = byte((x*7 + y*3) & 0xFF)            // diagonal ramp
			img.Pix[off+1] = byte((x * x) & 0xFF)                // horizontal curve
			img.Pix[off+2] = byte(int(40*math.Sin(float64(x+y)/3.0)) + 128) // high-freq
			if bpp == 4 {
				img.Pix[off+3] = byte((y*5 + 17) & 0xFF)
			}
		}
	}
}

func maxChannelDiff(a, b *decoder.Image) int {
	max := 0
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
		}
	}
	return max
}

// TestLanczosSeparableEquivalence is the safety net for the separable
// rewrite (GH #9): the new separable+weight-cached implementation must
// produce output within 1 LSB per channel of the original naive 2-D
// convolution, across RGB/RGBA, integer and non-integer ratios, edge
// clamping, and upsampling.
func TestLanczosSeparableEquivalence(t *testing.T) {
	cases := []struct {
		name                   string
		sw, sh, dw, dh int
		format                 decoder.PixelFormat
	}{
		{"rgb_down_2x", 64, 64, 32, 32, decoder.PixelFormatRGB},
		{"rgb_down_4x", 64, 64, 16, 16, decoder.PixelFormatRGB},
		{"rgb_down_noninteger", 70, 70, 20, 20, decoder.PixelFormatRGB},
		{"rgba_down_2x", 48, 48, 24, 24, decoder.PixelFormatRGBA},
		{"rgb_edge_clamped_small", 5, 5, 3, 3, decoder.PixelFormatRGB},
		{"rgb_upsample_2x", 16, 16, 32, 32, decoder.PixelFormatRGB},
		{"rgb_asymmetric", 80, 50, 33, 27, decoder.PixelFormatRGB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := decoder.NewImageFormat(c.sw, c.sh, c.format)
			fillLanczosPattern(src)

			ref := decoder.NewImageFormat(c.dw, c.dh, c.format)
			if err := lanczosNaiveRef(src, ref); err != nil {
				t.Fatalf("naive ref: %v", err)
			}
			got := decoder.NewImageFormat(c.dw, c.dh, c.format)
			if err := lanczosSeparableInto(src, got); err != nil {
				t.Fatalf("separable: %v", err)
			}
			if d := maxChannelDiff(ref, got); d > 1 {
				t.Errorf("max channel diff = %d, want <= 1", d)
			}
		})
	}
}
