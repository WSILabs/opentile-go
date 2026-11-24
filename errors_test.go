package opentile

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/jpegturbo"
)

func TestSentinelErrors(t *testing.T) {
	errs := []error{
		ErrUnsupportedFormat,
		ErrUnsupportedCompression,
		ErrTileOutOfBounds,
		ErrCorruptTile,
		ErrLevelOutOfRange,
		ErrInvalidTIFF,
	}
	seen := make(map[string]bool)
	for _, e := range errs {
		if e == nil {
			t.Fatal("sentinel is nil")
		}
		if seen[e.Error()] {
			t.Errorf("duplicate sentinel text: %q", e.Error())
		}
		seen[e.Error()] = true
	}
}

func TestTileError(t *testing.T) {
	te := &TileError{Level: 2, X: 7, Y: 3, Err: ErrCorruptTile}

	if !errors.Is(te, ErrCorruptTile) {
		t.Fatal("errors.Is should find wrapped sentinel")
	}

	var got *TileError
	if !errors.As(te, &got) {
		t.Fatal("errors.As should extract TileError")
	}
	if got.Level != 2 || got.X != 7 || got.Y != 3 {
		t.Fatalf("TileError fields: got %+v", got)
	}

	wantMsg := "opentile: tile (7,3) on level 2: opentile: corrupt tile"
	if te.Error() != wantMsg {
		t.Fatalf("Error(): got %q, want %q", te.Error(), wantMsg)
	}
}

func TestTileErrorWrapsIO(t *testing.T) {
	te := &TileError{Level: 0, X: 0, Y: 0, Err: io.ErrUnexpectedEOF}
	if !errors.Is(te, io.ErrUnexpectedEOF) {
		t.Fatal("should unwrap to io.ErrUnexpectedEOF")
	}
}

func TestV02Sentinels(t *testing.T) {
	sentinels := []error{
		ErrBadJPEGBitstream,
		ErrMCUAlignment,
		ErrCGORequired,
		ErrTileSizeRequired,
	}
	seen := make(map[string]bool)
	for _, e := range sentinels {
		if e == nil {
			t.Fatal("sentinel is nil")
		}
		if seen[e.Error()] {
			t.Errorf("duplicate sentinel text: %q", e.Error())
		}
		seen[e.Error()] = true
	}

	// Confirm the new sentinels wrap cleanly through TileError.
	te := &TileError{Level: 0, X: 1, Y: 2, Err: ErrBadJPEGBitstream}
	if !errors.Is(te, ErrBadJPEGBitstream) {
		t.Fatal("TileError should unwrap to ErrBadJPEGBitstream")
	}
}

// TestErrCGORequiredMatchesBuildErrors pins the ErrCGORequired doc promise:
// it must match what the library really returns when a codec is missing.
// Before #104's follow-up it was a separate value nothing ever returned.
func TestErrCGORequiredMatchesBuildErrors(t *testing.T) {
	cases := map[string]error{
		// NDPI / Philips raw tiles: TileError wrapping the jpegturbo stub error.
		"jpegturbo crop": &TileError{Level: 0, X: 1, Y: 2,
			Err: fmt.Errorf("oneframe: extend frame: %w", jpegturbo.ErrCGORequired)},
		// Decoded paths: a codec stub's error.
		"codec stub": fmt.Errorf("decoder/jpeg: requires cgo: %w", decoder.ErrCodecUnavailable),
	}
	for name, err := range cases {
		if !errors.Is(err, ErrCGORequired) {
			t.Errorf("%s: %v does not match ErrCGORequired", name, err)
		}
		if !errors.Is(err, decoder.ErrCodecUnavailable) {
			t.Errorf("%s: %v does not match decoder.ErrCodecUnavailable", name, err)
		}
	}
}
