package resample

import (
	"math"

	"github.com/wsilabs/opentile-go/decoder"
)

const lanczosA = 3.0 // lobe count

// lanczos2D returns the 2-D Lanczos kernel weight for position (dx, dy)
// from the sample centre.
func lanczosWeight(x float64) float64 {
	if x == 0 {
		return 1
	}
	if x < -lanczosA || x > lanczosA {
		return 0
	}
	px := math.Pi * x
	return (lanczosA * math.Sin(px) * math.Sin(px/lanczosA)) / (px * px)
}

// lanczosInto resamples src into dst using Lanczos resampling with a=3.
// Best quality for arbitrary downsampling ratios; more expensive than Box.
//
// The wsitools/internal/resample/lanczos.go equivalent was a v0.1 stub
// (ErrNotImplemented). This is the real implementation, ported to operate
// on *decoder.Image using src.Stride / dst.Stride and handling both
// PixelFormatRGB (3 bytes/pixel) and PixelFormatRGBA (4 bytes/pixel).
func lanczosInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}

	scaleX := float64(src.Width) / float64(dst.Width)
	scaleY := float64(src.Height) / float64(dst.Height)

	// Support radius: when downsampling, the kernel widens proportionally.
	supportX := lanczosA
	if scaleX > 1 {
		supportX = lanczosA * scaleX
	}
	supportY := lanczosA
	if scaleY > 1 {
		supportY = lanczosA * scaleY
	}

	for dy := 0; dy < dst.Height; dy++ {
		// Centre of this dst row in src coordinates.
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
