package all_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

// TestRawCodecsAreNotInspectors pins the negative half of the #41
// surface: codecs without a meaningful codestream header must NOT implement
// decoder.CodestreamInspector, so a consumer's type assertion reports ok == false (the
// idiomatic "this codec can't be inspected"). The positive half — that jpeg /
// jpeg2000 / htj2k / jpegxl DO implement CodestreamInspector — is asserted in each codec's
// own inspect test under its build tags.
func TestRawCodecsAreNotInspectors(t *testing.T) {
	for _, name := range []string{"none", "lzw", "deflate", "webp", "avif"} {
		f, ok := decoder.Get(name)
		if !ok {
			continue
		}
		if _, isInspector := f.(decoder.CodestreamInspector); isInspector {
			t.Errorf("%s unexpectedly implements decoder.CodestreamInspector", name)
		}
	}
}
