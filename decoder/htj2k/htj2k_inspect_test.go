//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestHTJ2KInspect(t *testing.T) {
	// Encode a small reversible (lossless) HTJ2K codestream and inspect its header.
	const w, h = 8, 4
	rgb := make([]byte, w*h*3)
	for i := range rgb {
		rgb[i] = byte(i * 7)
	}
	cs, err := encodeTestLossless(rgb, w, h, 1)
	if err != nil {
		t.Fatalf("encodeTestLossless: %v", err)
	}

	var p decoder.CodestreamInspector = &factory{}
	ci, err := p.Inspect(cs)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessYes ||
		ci.ChromaSubsampling != decoder.Subsampling444 {
		t.Errorf("htj2k inspect = %+v, want comps=3 depth=8 lossless 4:4:4", ci)
	}

	if _, err := p.Inspect([]byte{0xFF, 0xD8}); err == nil { // JPEG SOI, not J2K
		t.Error("expected error probing non-J2K bytes")
	}
}
