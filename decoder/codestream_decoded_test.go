package decoder

import "testing"

// The zero value of the new field must be ColorUnknown (iota 0), and it must be
// assignable from the decoded-pixel subset of ColorEncoding.
func TestDecodedColorSpaceField(t *testing.T) {
	var ci CodestreamInfo
	if ci.DecodedColorSpace != ColorUnknown {
		t.Errorf("zero-value DecodedColorSpace = %s, want unknown", ci.DecodedColorSpace)
	}
	ci.DecodedColorSpace = ColorRGB
	if ci.DecodedColorSpace != ColorRGB {
		t.Errorf("DecodedColorSpace = %s, want RGB", ci.DecodedColorSpace)
	}
}
