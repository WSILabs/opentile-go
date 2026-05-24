package all_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

func TestAllRegistered(t *testing.T) {
	want := []string{"none", "lzw", "deflate", "jpeg", "jpeg2000", "jpegxl", "avif", "webp", "htj2k"}
	registered := decoder.Registered()
	for _, name := range want {
		found := false
		for _, r := range registered {
			if r == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing decoder: %q (registered: %v)", name, registered)
		}
	}
}
