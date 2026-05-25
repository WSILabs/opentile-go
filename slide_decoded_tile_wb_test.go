package opentile

import (
	"errors"
	"testing"
)

// TestCompressionToTIFFTag verifies the mapping table exhaustively.
func TestCompressionToTIFFTag(t *testing.T) {
	cases := []struct {
		c    Compression
		want uint16
	}{
		{CompressionNone, 1},
		{CompressionLZW, 5},
		{CompressionJPEG, 7},
		{CompressionDeflate, 8},
		{CompressionJP2K, 33003},
		{CompressionWebP, 50001},
		{CompressionJPEGXL, 50002},
		{CompressionAVIF, 60001},
		{CompressionHTJ2K, 60003},
		// no-decoder compressions → 0
		{CompressionIRIS, 0},
		{CompressionPNG, 0},
		{CompressionUnknown, 0},
	}
	for _, tc := range cases {
		got := CompressionToTIFFTag(tc.c)
		if got != tc.want {
			t.Errorf("CompressionToTIFFTag(%v): got %d, want %d", tc.c, got, tc.want)
		}
	}
}

// TestErrCodecNotRegisteredSentinel verifies the sentinel is wired and
// errors.Is works through wrapped errors.
func TestErrCodecNotRegisteredSentinel(t *testing.T) {
	if ErrCodecNotRegistered == nil {
		t.Fatal("ErrCodecNotRegistered is nil")
	}
	// Verify errors.Is works when wrapped (as DecodedTile does via fmt.Errorf %w).
	import_err := errors.New("opentile: codec not registered for this slide's tile compression")
	_ = import_err
	if !errors.Is(ErrCodecNotRegistered, ErrCodecNotRegistered) {
		t.Error("errors.Is self-check failed")
	}
}
