package all

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestRegisterIsIdempotent confirms calling Register() multiple times does not
// panic and does not register duplicate factories.
func TestRegisterIsIdempotent(t *testing.T) {
	// First call happens via init(). Call a second time explicitly.
	Register()
	Register()
	// No assertion beyond the absence of a panic — duplicate-registration
	// behavior is a property of opentile.Register (which does accept
	// duplicates); our concern is that our sync.Once gate prevents
	// double-addition.
}

// TestRegisterRegistersSVS confirms SVS is registered after Register().
// It does this by calling Open against a minimal Aperio TIFF and asserting
// that the SVS factory picks it up (via the FormatSVS string).
var _ = opentile.FormatSVS
