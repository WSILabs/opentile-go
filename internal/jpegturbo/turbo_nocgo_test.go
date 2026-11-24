//go:build !cgo || nocgo

package jpegturbo

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestCropReturnsErrCGORequired(t *testing.T) {
	_, err := Crop([]byte{0xFF, 0xD8, 0xFF, 0xD9}, Region{X: 0, Y: 0, Width: 8, Height: 8})
	if !errors.Is(err, ErrCGORequired) {
		t.Fatalf("expected ErrCGORequired, got %v", err)
	}
}

// TestErrCGORequiredIsCodecUnavailable pins #104: the internal sentinel must
// match the public decoder.ErrCodecUnavailable, so library users can detect a
// nocgo build on the NDPI/Philips raw-tile paths that crop through here.
func TestErrCGORequiredIsCodecUnavailable(t *testing.T) {
	_, err := Crop([]byte{0xFF, 0xD8, 0xFF, 0xD9}, Region{X: 0, Y: 0, Width: 8, Height: 8})
	if !errors.Is(err, decoder.ErrCodecUnavailable) {
		t.Fatalf("Crop error %v does not wrap decoder.ErrCodecUnavailable", err)
	}
}
