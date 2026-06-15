package all_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

// garbageInputs are malformed byte sequences every decoder must reject with
// an error rather than crash. Includes empty input, random bytes, and
// plausible-but-truncated codec headers (J2K SOC, JPEG SOI, RIFF/WEBP, etc.).
func garbageInputs() [][]byte {
	rand := make([]byte, 256)
	for i := range rand {
		rand[i] = byte(i*7 + 13)
	}
	return [][]byte{
		{},                                   // empty
		{0x00},                               // one byte
		rand,                                 // pseudo-random
		{0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x2F}, // J2K SOC+SIZ then truncated
		{0xFF, 0xD8, 0xFF, 0xE0, 0x00},       // JPEG SOI + APP0 truncated
		{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, // WEBP container, no data
		{0x00, 0x00, 0x00, 0x0C, 'j', 'P', 0x20, 0x20},       // JP2 box signature truncated
	}
}

// TestDecoderNoPanicOnGarbage is the class-level guard against decoder crashes
// on malformed input (the bug class behind the #7 OOB / suyashkumar SIGSEGV):
// EVERY registered decoder, fed garbage, must return an error — never panic or
// SIGSEGV. A cgo over-read would surface here (crash) under -race and -asan.
func TestDecoderNoPanicOnGarbage(t *testing.T) {
	names := decoder.Registered()
	if len(names) == 0 {
		t.Fatal("no decoders registered")
	}
	for _, name := range names {
		fac, ok := decoder.Get(name)
		if !ok {
			t.Fatalf("Get(%q) failed", name)
		}
		for i, in := range garbageInputs() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("decoder %q panicked on garbage input #%d: %v", name, i, r)
					}
				}()
				d := fac.New()
				defer d.Close()
				img, err := d.Decode(in, decoder.DecodeOptions{})
				if err == nil && img == nil {
					t.Errorf("decoder %q returned (nil, nil) on garbage input #%d", name, i)
				}
			}()

			// Same class-level guard for the header-only Inspect path (#41):
			// a CodestreamInspector fed garbage must return an error, never panic or
			// over-read (a cgo header parse would crash here under -asan).
			if pr, isInspector := fac.(decoder.CodestreamInspector); isInspector {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("inspector %q panicked on garbage input #%d: %v", name, i, r)
						}
					}()
					if _, err := pr.Inspect(in); err == nil {
						t.Errorf("inspector %q returned nil error on garbage input #%d", name, i)
					}
				}()
			}
		}
	}
}
