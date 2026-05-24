package resample

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestNearestIdentity(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	dst := decoder.NewImage(4, 4) // identity = same size
	if err := ImageInto(src, dst, Nearest); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src.Pix, dst.Pix) {
		t.Errorf("identity resample changed pixels")
	}
}

func TestNearestFormatMismatch(t *testing.T) {
	src := decoder.NewImage(4, 4)
	dst := decoder.NewImageFormat(4, 4, decoder.PixelFormatRGBA)
	if err := ImageInto(src, dst, Nearest); err == nil {
		t.Error("expected format mismatch error")
	}
}
