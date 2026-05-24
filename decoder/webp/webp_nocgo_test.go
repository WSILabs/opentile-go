//go:build !cgo || nocgo || nowebp

package webp

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestStubReturnsUnavailable(t *testing.T) {
	f, ok := decoder.Get("webp")
	if !ok {
		t.Fatalf("webp stub not registered")
	}
	d := f.New()
	defer d.Close()
	_, err := d.Decode([]byte("RIFF"), decoder.DecodeOptions{})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Errorf("stub error: got %v want wrap of ErrCodecUnavailable", err)
	}
}
