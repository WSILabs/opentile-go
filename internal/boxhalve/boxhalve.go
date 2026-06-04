// Package boxhalve finishes a partial codec resolution reduction by box-
// averaging the residual power-of-two factor, so a wavelet decoder that
// could only reduce part of the requested Scale still lands on exact dims.
package boxhalve

import "github.com/wsilabs/opentile-go/decoder"

func bpp(f decoder.PixelFormat) int {
	if f == decoder.PixelFormatRGBA {
		return 4
	}
	return 3
}

// Halve box-reduces img by 2^times in each dimension (ceil), averaging
// 2x2 source blocks (edge blocks average the available 1- or 2-wide cells).
// times <= 0 returns img unchanged.
func Halve(img *decoder.Image, times int) *decoder.Image {
	cur := img
	for t := 0; t < times; t++ {
		cur = halveOnce(cur)
	}
	return cur
}

// To halves img repeatedly until it reaches (w, h). Requires that w,h are
// img dims reduced by a power of two (the codec-reduction residual always
// is); it halves ceil-wise until both dimensions are <= the targets.
func To(img *decoder.Image, w, h int) *decoder.Image {
	cur := img
	for cur.Width > w || cur.Height > h {
		cur = halveOnce(cur)
	}
	return cur
}

func halveOnce(src *decoder.Image) *decoder.Image {
	b := bpp(src.Format)
	dw := (src.Width + 1) / 2
	dh := (src.Height + 1) / 2
	dst := decoder.NewImageFormat(dw, dh, src.Format)
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			x0, y0 := dx*2, dy*2
			x1, y1 := x0+1, y0+1
			if x1 >= src.Width {
				x1 = x0
			}
			if y1 >= src.Height {
				y1 = y0
			}
			do := dy*dst.Stride + dx*b
			for c := 0; c < b; c++ {
				s := int(src.Pix[y0*src.Stride+x0*b+c]) +
					int(src.Pix[y0*src.Stride+x1*b+c]) +
					int(src.Pix[y1*src.Stride+x0*b+c]) +
					int(src.Pix[y1*src.Stride+x1*b+c])
				dst.Pix[do+c] = byte((s + 2) / 4)
			}
		}
	}
	return dst
}
