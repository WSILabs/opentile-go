//go:build !cgo || nocgo || nojxl

package jpegxl

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatalf("jpegxl stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x0A}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
