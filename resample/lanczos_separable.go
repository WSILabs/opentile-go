package resample

import (
	"math"

	"github.com/wsilabs/opentile-go/decoder"
)

// weightRow is a precomputed 1-D resampling kernel for one output position:
// the source index where the window starts and the normalized weights.
type weightRow struct {
	start int
	w     []float64
}

// buildLanczosWeights precomputes, for every output position along one axis,
// the clamped source window and its Lanczos weights normalized to sum 1.
// Normalizing per axis is exactly equivalent to the naive 2-D convolution's
// single wSum = (Σwx)(Σwy) normalization, so output is preserved. All sin()
// calls happen here, once per output position — never in the resample loops.
func buildLanczosWeights(dstLen, srcLen int, scale, support float64) []weightRow {
	rows := make([]weightRow, dstLen)
	for d := 0; d < dstLen; d++ {
		center := (float64(d)+0.5)*scale - 0.5
		i0 := int(math.Floor(center - support))
		i1 := int(math.Ceil(center + support))
		if i0 < 0 {
			i0 = 0
		}
		if i1 >= srcLen {
			i1 = srcLen - 1
		}
		if i1 < i0 {
			i1 = i0 // degenerate safety; srcLen >= 1
		}
		n := i1 - i0 + 1
		w := make([]float64, n)
		var sum float64
		for k := 0; k < n; k++ {
			wk := lanczosWeight((float64(i0+k) - center) / scale)
			w[k] = wk
			sum += wk
		}
		if sum == 0 {
			sum = 1 // matches the naive impl's wSum==0 guard
		}
		for k := range w {
			w[k] /= sum
		}
		rows[d] = weightRow{start: i0, w: w}
	}
	return rows
}

// lanczosSeparableInto resamples src into dst with separable Lanczos (a=3):
// a horizontal 1-D pass into a float intermediate, then a vertical 1-D pass.
// This is O(out·scale) with no transcendental in the hot loops (the naive
// 2-D form is O(out·scale²) with two sin() per source pixel). Output matches
// the naive convolution to within 1 LSB per channel (see
// TestLanczosSeparableEquivalence). Rounding happens only once, at the end of
// the vertical pass, to avoid double-rounding.
func lanczosSeparableInto(src, dst *decoder.Image) error {
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

	xw := buildLanczosWeights(dst.Width, src.Width, scaleX, supportX)
	yw := buildLanczosWeights(dst.Height, src.Height, scaleY, supportY)

	// Horizontal pass: src (W×H) → tmp (dst.Width × src.Height), kept in
	// float to avoid an intermediate round. tmp is row-major over source
	// rows, each row holding dst.Width packed pixels of bpp channels.
	tmp := make([]float64, src.Height*dst.Width*bpp)
	for sy := 0; sy < src.Height; sy++ {
		srcRow := sy * src.Stride
		tmpRow := sy * dst.Width * bpp
		for dx := 0; dx < dst.Width; dx++ {
			wr := xw[dx]
			var acc [4]float64
			for k, wk := range wr.w {
				so := srcRow + (wr.start+k)*bpp
				for c := 0; c < bpp; c++ {
					acc[c] += wk * float64(src.Pix[so+c])
				}
			}
			to := tmpRow + dx*bpp
			for c := 0; c < bpp; c++ {
				tmp[to+c] = acc[c]
			}
		}
	}

	// Vertical pass: tmp (dst.Width × src.Height) → dst, rounding/clamping
	// once at the end.
	rowStride := dst.Width * bpp
	for dy := 0; dy < dst.Height; dy++ {
		wr := yw[dy]
		dstRow := dy * dst.Stride
		for dx := 0; dx < dst.Width; dx++ {
			col := dx * bpp
			var acc [4]float64
			for k, wk := range wr.w {
				to := (wr.start+k)*rowStride + col
				for c := 0; c < bpp; c++ {
					acc[c] += wk * tmp[to+c]
				}
			}
			do := dstRow + col
			for c := 0; c < bpp; c++ {
				v := acc[c] + 0.5
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				dst.Pix[do+c] = byte(v)
			}
		}
	}
	return nil
}
