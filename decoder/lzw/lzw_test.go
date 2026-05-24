package lzw

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("lzw")
	if !ok {
		t.Fatalf("lzw decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 5 {
		t.Errorf("TIFFCompressionTags: got %v want [5]", got)
	}
}

func TestRoundTrip(t *testing.T) {
	pixels := []byte{
		10, 20, 30, 40, 50, 60,
		70, 80, 90, 100, 110, 120,
	}
	var buf bytes.Buffer
	w := tifflzw.NewWriter(&buf, tifflzw.MSB, 8)
	_, _ = w.Write(pixels)
	_ = w.Close()

	f, _ := decoder.Get("lzw")
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
