//go:build !cgo || nocgo

package jpeg

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("jpeg")
	if !ok {
		t.Fatalf("jpeg stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0xD8}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
