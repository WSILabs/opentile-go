package boxhalve

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestHalveOnceRGB(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			off := y*src.Stride + x*3
			v := byte(10 + (y/2)*40 + (x/2)*20) // each 2x2 block uniform
			src.Pix[off], src.Pix[off+1], src.Pix[off+2] = v, v, v
		}
	}
	got := Halve(src, 1)
	if got.Width != 2 || got.Height != 2 {
		t.Fatalf("dims = %dx%d, want 2x2", got.Width, got.Height)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			want := byte(10 + y*40 + x*20)
			off := y*got.Stride + x*3
			if got.Pix[off] != want {
				t.Errorf("(%d,%d)=%d want %d", x, y, got.Pix[off], want)
			}
		}
	}
}

func TestHalveTimesZeroReturnsInput(t *testing.T) {
	src := decoder.NewImage(3, 3)
	if got := Halve(src, 0); got != src {
		t.Errorf("times=0 should return src unchanged")
	}
}

func TestToRGBA(t *testing.T) {
	src := decoder.NewImageFormat(8, 8, decoder.PixelFormatRGBA)
	got := To(src, 2, 2) // 8->2 is two halvings
	if got.Width != 2 || got.Height != 2 || got.Format != decoder.PixelFormatRGBA {
		t.Fatalf("got %dx%d fmt %v", got.Width, got.Height, got.Format)
	}
}

func TestToOddDimensionCeil(t *testing.T) {
	src := decoder.NewImage(5, 5)
	got := To(src, 3, 3) // 5 -> ceil(5/2)=3
	if got.Width != 3 || got.Height != 3 {
		t.Fatalf("got %dx%d, want 3x3", got.Width, got.Height)
	}
}
