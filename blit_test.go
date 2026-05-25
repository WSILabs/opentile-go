package opentile

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestBlitIntoRGBSameSize(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	dst := decoder.NewImage(4, 4)
	blitInto(src, 0, 0, 4, 4, dst, 0, 0)
	if !bytes.Equal(src.Pix, dst.Pix) {
		t.Errorf("identity blit changed pixels")
	}
}

func TestBlitIntoSubregion(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			off := y*src.Stride + x*3
			src.Pix[off] = byte(x)
			src.Pix[off+1] = byte(y)
			src.Pix[off+2] = 0
		}
	}
	dst := decoder.NewImage(2, 2)
	blitInto(src, 1, 1, 2, 2, dst, 0, 0)
	if dst.Pix[0] != 1 || dst.Pix[1] != 1 || dst.Pix[2] != 0 {
		t.Errorf("dst[0,0]: got %v, want [1, 1, 0]", dst.Pix[0:3])
	}
	dstOff := 1*dst.Stride + 1*3
	if dst.Pix[dstOff] != 2 || dst.Pix[dstOff+1] != 2 || dst.Pix[dstOff+2] != 0 {
		t.Errorf("dst[1,1]: got %v, want [2, 2, 0]", dst.Pix[dstOff:dstOff+3])
	}
}

func TestBlitIntoRGBAPreservesAlpha(t *testing.T) {
	src := decoder.NewImageFormat(2, 2, decoder.PixelFormatRGBA)
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i] = 10
		src.Pix[i+1] = 20
		src.Pix[i+2] = 30
		src.Pix[i+3] = 255
	}
	dst := decoder.NewImageFormat(2, 2, decoder.PixelFormatRGBA)
	blitInto(src, 0, 0, 2, 2, dst, 0, 0)
	for i := 3; i < len(dst.Pix); i += 4 {
		if dst.Pix[i] != 255 {
			t.Errorf("alpha lost: dst.Pix[%d]=%d", i, dst.Pix[i])
		}
	}
}
