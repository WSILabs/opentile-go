package resample

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// benchLanczos runs a resample function repeatedly at a fixed geometry.
func benchLanczos(b *testing.B, fn func(src, dst *decoder.Image) error, sw, sh, dw, dh int, format decoder.PixelFormat) {
	src := decoder.NewImageFormat(sw, sh, format)
	fillLanczosPattern(src)
	dst := decoder.NewImageFormat(dw, dh, format)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fn(src, dst); err != nil {
			b.Fatal(err)
		}
	}
}

// Separable (production) vs Naive (pre-#9 reference) at representative WSI
// downsample ratios. Expect ~an order of magnitude at 2x, more at 4x.
func BenchmarkLanczosSeparable2xRGB(b *testing.B) {
	benchLanczos(b, lanczosSeparableInto, 512, 512, 256, 256, decoder.PixelFormatRGB)
}
func BenchmarkLanczosNaive2xRGB(b *testing.B) {
	benchLanczos(b, lanczosNaiveRef, 512, 512, 256, 256, decoder.PixelFormatRGB)
}
func BenchmarkLanczosSeparable4xRGB(b *testing.B) {
	benchLanczos(b, lanczosSeparableInto, 512, 512, 128, 128, decoder.PixelFormatRGB)
}
func BenchmarkLanczosNaive4xRGB(b *testing.B) {
	benchLanczos(b, lanczosNaiveRef, 512, 512, 128, 128, decoder.PixelFormatRGB)
}
func BenchmarkLanczosSeparable2xRGBA(b *testing.B) {
	benchLanczos(b, lanczosSeparableInto, 512, 512, 256, 256, decoder.PixelFormatRGBA)
}
