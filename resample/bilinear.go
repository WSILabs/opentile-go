package resample

import "github.com/wsilabs/opentile-go/decoder"

func bilinearInto(src, dst *decoder.Image) error {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	sx := float64(src.Width) / float64(dst.Width)
	sy := float64(src.Height) / float64(dst.Height)
	for dy := 0; dy < dst.Height; dy++ {
		fy := (float64(dy)+0.5)*sy - 0.5
		y0 := int(fy)
		if y0 < 0 {
			y0 = 0
		}
		y1 := y0 + 1
		if y1 >= src.Height {
			y1 = src.Height - 1
		}
		wy := fy - float64(y0)
		for dx := 0; dx < dst.Width; dx++ {
			fx := (float64(dx)+0.5)*sx - 0.5
			x0 := int(fx)
			if x0 < 0 {
				x0 = 0
			}
			x1 := x0 + 1
			if x1 >= src.Width {
				x1 = src.Width - 1
			}
			wx := fx - float64(x0)

			for c := 0; c < bpp; c++ {
				p00 := float64(src.Pix[y0*src.Stride+x0*bpp+c])
				p10 := float64(src.Pix[y0*src.Stride+x1*bpp+c])
				p01 := float64(src.Pix[y1*src.Stride+x0*bpp+c])
				p11 := float64(src.Pix[y1*src.Stride+x1*bpp+c])
				v := (1-wy)*((1-wx)*p00+wx*p10) + wy*((1-wx)*p01+wx*p11)
				dst.Pix[dy*dst.Stride+dx*bpp+c] = byte(v + 0.5)
			}
		}
	}
	return nil
}
