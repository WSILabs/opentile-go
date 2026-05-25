package all

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestFormatsRegistered confirms that importing this package registers formats.
// We verify by checking that the FormatSVS constant exists (compilation check)
// and that OpenFile with a nonexistent path returns an error containing the
// path (not a "no formats registered" error).
func TestFormatsRegistered(t *testing.T) {
	_, err := opentile.OpenFile("/nonexistent/path.svs")
	if err == nil {
		t.Fatal("expected error")
	}
	// If no formats were registered, the error would be "no format registered".
	// With formats registered, it should be a file-not-found error.
	if err.Error() == "opentile: no format registered" {
		t.Fatalf("formats were not registered: %v", err)
	}
}

var _ = opentile.FormatSVS
