// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
//
// Note: tiler_test.go is in package cogwsi_test (external), so New() must be
// exported from the cogwsi package. It lives here (same package, white-box)
// so it's compiled only during testing.
package cogwsi

import (
	opentile "github.com/wsilabs/opentile-go"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in tests.
func New() *Factory { return &Factory{} }

// Format returns FormatCOGWSI.
func (f *Factory) Format() opentile.Format { return opentile.FormatCOGWSI }
