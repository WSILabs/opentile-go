//go:build cgo && !nocgo && !nojp2k

package jpeg2000

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/internal/j2kheader"
)

// Verify the GH#53 fix preserves Aperio 33003: its raw J2K tiles are YCbCr
// WITHOUT MCT and WITHOUT a colorspace box, so decodeIsYCbCr must return true
// (unchanged from the historical behavior). Probes a real extracted tile if one
// is dropped at testdata/aperio_33003_tile.j2k; otherwise skips.
func TestAperio33003StaysYCbCr(t *testing.T) {
	p := filepath.Join("testdata", "aperio_33003_tile.j2k")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("no aperio tile fixture: %v", err)
	}
	h, err := j2kheader.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("aperio tile: MCT=%v EnumColorspace=%d Components=%d", h.MCT, h.EnumColorspace, h.Components)
	if h.MCT {
		t.Fatalf("unexpected: Aperio 33003 tile uses MCT — fix would wrongly skip YCbCr")
	}
	if !decodeIsYCbCr(src) {
		t.Fatalf("decodeIsYCbCr=false for Aperio 33003 tile — regression (should stay YCbCr)")
	}
}
