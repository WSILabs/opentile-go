package resample

import "github.com/wsilabs/opentile-go/decoder"

// boxInto resamples src into dst using area-averaging (box filter).
// Best-quality fast downsample for integer ratios (2x, 4x, 8x);
// acceptable for arbitrary ratios. For upscaling, falls through to
// nearest-neighbor behavior.
//
// Algorithm ported from wsitools/internal/resample/area.go.
// The wsitools implementation performs a fixed 2×2 average (half
// dimensions, RGB-only). This version generalises to arbitrary output
// dimensions and handles both PixelFormatRGB (3 bytes/pixel) and
// PixelFormatRGBA (4 bytes/pixel) using src.Stride / dst.Stride.
func boxInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}

	scaleX := float64(src.Width) / float64(dst.Width)
	scaleY := float64(src.Height) / float64(dst.Height)

	for dy := 0; dy < dst.Height; dy++ {
		// Determine the range of source rows that map to dst row dy.
		srcY0f := float64(dy) * scaleY
		srcY1f := float64(dy+1) * scaleY
		srcY0 := int(srcY0f)
		srcY1 := int(srcY1f)
		if srcY1 > src.Height {
			srcY1 = src.Height
		}
		if srcY1 == srcY0 {
			srcY1 = srcY0 + 1
		}
		if srcY1 > src.Height {
			srcY1 = src.Height
		}

		for dx := 0; dx < dst.Width; dx++ {
			// Determine the range of source columns that map to dst col dx.
			srcX0f := float64(dx) * scaleX
			srcX1f := float64(dx+1) * scaleX
			srcX0 := int(srcX0f)
			srcX1 := int(srcX1f)
			if srcX1 > src.Width {
				srcX1 = src.Width
			}
			if srcX1 == srcX0 {
				srcX1 = srcX0 + 1
			}
			if srcX1 > src.Width {
				srcX1 = src.Width
			}

			// Accumulate pixel values across the source box.
			var acc [4]uint
			n := uint((srcY1 - srcY0) * (srcX1 - srcX0))
			for sy := srcY0; sy < srcY1; sy++ {
				for sx := srcX0; sx < srcX1; sx++ {
					off := sy*src.Stride + sx*bpp
					for c := 0; c < bpp; c++ {
						acc[c] += uint(src.Pix[off+c])
					}
				}
			}

			// Write rounded average into dst. Rounding: add n/2 before dividing
			// (same round-to-nearest as wsitools Area2x2 which uses +2 for n=4).
			dstOff := dy*dst.Stride + dx*bpp
			half := n / 2
			for c := 0; c < bpp; c++ {
				dst.Pix[dstOff+c] = byte((acc[c] + half) / n)
			}
		}
	}
	return nil
}
