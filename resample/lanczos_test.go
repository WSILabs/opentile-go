package resample

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestLanczosIdentity(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	dst := decoder.NewImage(4, 4) // identity = same size
	if err := ImageInto(src, dst, Lanczos); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src.Pix, dst.Pix) {
		t.Errorf("identity resample changed pixels")
	}
}
