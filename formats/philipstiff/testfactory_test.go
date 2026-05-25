// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
package philipstiff

import (
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatPhilipsTIFF.
func (f *Factory) Format() opentile.Format { return opentile.FormatPhilipsTIFF }

// Supports reports whether file looks like a Philips TIFF.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	sw, ok := pages[0].Software()
	if !ok || !strings.HasPrefix(sw, philipsSoftwarePrefix) {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(desc), philipsDescriptionSuffix)
}
