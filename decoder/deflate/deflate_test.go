package deflate

import (
	"bytes"
	"compress/zlib"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("deflate")
	if !ok {
		t.Fatalf("deflate decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 8 {
		t.Errorf("TIFFCompressionTags: got %v want [8]", got)
	}
}

func TestRoundTrip(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6,
		7, 8, 9, 10, 11, 12,
	}
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(pixels)
	_ = w.Close()

	f, _ := decoder.Get("deflate")
	d := f.New()
	defer d.Close()
	dst := decoder.NewImage(2, 2)
	got, err := d.Decode(buf.Bytes(), decoder.DecodeOptions{Dst: dst})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Pix, pixels) {
		t.Errorf("Pix: got %v want %v", got.Pix, pixels)
	}
}
