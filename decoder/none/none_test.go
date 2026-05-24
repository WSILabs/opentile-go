package none

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("none")
	if !ok {
		t.Fatalf("none decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 1 {
		t.Errorf("TIFFCompressionTags: got %v want [1]", got)
	}
}

func TestDecodeRGBPassthrough(t *testing.T) {
	// Uncompressed: bytes ARE the pixels. 2x2 RGB.
	src := []byte{
		1, 2, 3, 4, 5, 6,        // row 0: two pixels
		7, 8, 9, 10, 11, 12,     // row 1: two pixels
	}
	f, _ := decoder.Get("none")
	d := f.New()
	defer d.Close()

	// Need width/height somehow — for "none" the caller must size via Dst.
	// Allocate a destination of known size and the decoder fills it.
	dst := decoder.NewImage(2, 2)
	got, err := d.Decode(src, decoder.DecodeOptions{Dst: dst})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != dst {
		t.Errorf("Decode should return the supplied Dst")
	}
	if !bytes.Equal(dst.Pix, src) {
		t.Errorf("Pix: got %v want %v", dst.Pix, src)
	}
}

func TestDecodeRequiresDst(t *testing.T) {
	// Without Dst, the decoder has no way to know image dimensions for raw bytes.
	f, _ := decoder.Get("none")
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{1, 2, 3}, decoder.DecodeOptions{})
	if err == nil {
		t.Errorf("Decode without Dst should error")
	}
}
