package jpeg_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/jpeg"
)

// makeMinimalJPEG returns a minimal JPEG header with one component using
// the given sampling-factor byte. Returns SOI + SOF0 + minimal SOS + EOI.
func makeMinimalJPEG(samplingFactor byte) []byte {
	out := []byte{0xFF, 0xD8}
	// SOF0: marker FF C0, length 0x000B (11), precision 8, height 0x0008,
	// width 0x0008, 1 component, ID 1, sampling factor, table 0
	out = append(out,
		0xFF, 0xC0,
		0x00, 0x0B,
		0x08,
		0x00, 0x08, 0x00, 0x08,
		0x01,
		0x01, samplingFactor, 0x00,
	)
	// SOS: marker FF DA, length 0x0008, ncomp 1, ID 1, tables 0, Ss 0, Se 63, Ah/Al 0
	out = append(out,
		0xFF, 0xDA,
		0x00, 0x08,
		0x01,
		0x01, 0x00,
		0x00, 0x3F, 0x00,
	)
	out = append(out, 0xFF, 0xD9)
	return out
}

func TestMCUSizeOf420(t *testing.T) {
	src := makeMinimalJPEG(0x22) // h=2, v=2 -> 4:2:0
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 16 || h != 16 {
		t.Errorf("MCU size: got %dx%d, want 16x16", w, h)
	}
}

func TestMCUSizeOf422(t *testing.T) {
	src := makeMinimalJPEG(0x21) // h=2, v=1
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 16 || h != 8 {
		t.Errorf("MCU size: got %dx%d, want 16x8", w, h)
	}
}

func TestMCUSizeOf444(t *testing.T) {
	src := makeMinimalJPEG(0x11) // h=1, v=1
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 8 || h != 8 {
		t.Errorf("MCU size: got %dx%d, want 8x8", w, h)
	}
}

func TestMCUSizeOfMissingSOF(t *testing.T) {
	src := []byte{0xFF, 0xD8, 0xFF, 0xD9} // SOI + EOI only
	_, _, err := jpeg.MCUSizeOf(src)
	if err == nil {
		t.Fatal("expected error for missing SOF, got nil")
	}
}
