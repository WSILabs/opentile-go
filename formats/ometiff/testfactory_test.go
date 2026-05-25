// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
package ometiff

import (
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatOMETIFF.
func (f *Factory) Format() opentile.Format { return opentile.FormatOMETIFF }

// Supports reports whether file's first page description ends with "OME>"
// (mirrors tifffile's is_ome check: description[-10:].strip().endswith('OME>')).
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok || desc == "" {
		return false
	}
	tail := desc
	if len(tail) > 10 {
		tail = tail[len(tail)-10:]
	}
	return strings.HasSuffix(strings.TrimSpace(tail), omeDescriptionSuffix)
}
