//go:build cgo && !nocgo && !nojxl

package jpegxl

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Fatalf("jpegxl decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 50002 {
		t.Errorf("TIFFCompressionTags: got %v want [50002]", got)
	}
}

// End-to-end Decode validation: encode a known image via the wsitools
// encoder, decode via this package, assert pixel-level closeness.
// Detailed test added when encoder + decoder are both available; defer
// to wsitools v0.9.0 cross-checks if fixtures aren't readily available
// in this repo.
