//go:build !cgo || nocgo

package jpeg2000

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Fatalf("jpeg2000 stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x4F}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
