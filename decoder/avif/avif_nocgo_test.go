//go:build !cgo || nocgo || noavif

package avif

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("avif")
	if !ok {
		t.Fatalf("avif stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0x00, 0x00}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
