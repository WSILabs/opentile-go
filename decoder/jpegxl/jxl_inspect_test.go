//go:build cgo && !nocgo && !nojxl

package jpegxl_test

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestJPEGXLInspect(t *testing.T) {
	f, ok := decoder.Get("jpegxl")
	if !ok {
		t.Skip("jpegxl decoder not registered")
	}
	p, ok := f.(decoder.CodestreamInspector)
	if !ok {
		t.Fatal("jpegxl factory does not implement decoder.CodestreamInspector")
	}

	// A raw 3-channel 8-bit JXL tile (transcoded from CMU-1-Small-Region).
	b, err := os.ReadFile("testdata/sample_tile.jxl")
	if err != nil {
		t.Fatal(err)
	}
	ci, err := p.Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	// libjxl exposes no header-only lossless flag → LosslessUnknown is expected.
	if ci.Components != 3 || ci.BitDepth != 8 || ci.Lossless != decoder.LosslessUnknown ||
		ci.ColorEncoding != decoder.ColorRGB || ci.ChromaSubsampling != decoder.SubsamplingUnknown || ci.Boxed {
		t.Errorf("jxl inspect = %+v, want comps=3 depth=8 lossless=unknown RGB raw", ci)
	}

	if _, err := p.Inspect([]byte{0x00, 0x01, 0x02}); err == nil {
		t.Error("expected error probing non-JXL bytes")
	}
}
