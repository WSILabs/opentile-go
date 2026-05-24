//go:build !cgo || nocgo || nohtj2k

package htj2k

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatalf("htj2k stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte{0xFF, 0x4F, 0xFF, 0x51}, decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
