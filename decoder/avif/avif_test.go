//go:build cgo && !nocgo && !noavif

package avif

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("avif")
	if !ok {
		t.Fatalf("avif decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 60001 {
		t.Errorf("TIFFCompressionTags: got %v want [60001]", got)
	}
}

// End-to-end Decode validation: encode a known image via the wsitools
// encoder, decode via this package, assert pixel-level closeness.
// Detailed test deferred to wsitools v0.9.0 cross-checks when fixtures
// are available in this repo.
