package all_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

// TestProberNotImplementedForRawCodecs pins the negative half of the #41
// surface: codecs without a meaningful codestream header must NOT implement
// decoder.Prober, so a consumer's type assertion reports ok == false (the
// idiomatic "this codec can't be probed"). The positive half — that jpeg /
// jpeg2000 / htj2k / jpegxl DO implement Prober — is asserted in each codec's
// own probe test under its build tags.
func TestProberNotImplementedForRawCodecs(t *testing.T) {
	for _, name := range []string{"none", "lzw", "deflate", "webp", "avif"} {
		f, ok := decoder.Get(name)
		if !ok {
			continue
		}
		if _, isProber := f.(decoder.Prober); isProber {
			t.Errorf("%s unexpectedly implements decoder.Prober", name)
		}
	}
}
