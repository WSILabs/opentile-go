package resample

import "github.com/wsilabs/opentile-go/decoder"

func nearestInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	for dy := 0; dy < dst.Height; dy++ {
		sy := dy * src.Height / dst.Height
		if sy >= src.Height {
			sy = src.Height - 1
		}
		for dx := 0; dx < dst.Width; dx++ {
			sx := dx * src.Width / dst.Width
			if sx >= src.Width {
				sx = src.Width - 1
			}
			srcOff := sy*src.Stride + sx*bpp
			dstOff := dy*dst.Stride + dx*bpp
			copy(dst.Pix[dstOff:dstOff+bpp], src.Pix[srcOff:srcOff+bpp])
		}
	}
	return nil
}
