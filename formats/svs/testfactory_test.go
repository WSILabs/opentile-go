// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.  Production code no longer
// has a Factory; these helpers live here so they only exist during testing.
package svs

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim for tests written against the pre-v0.23 API.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatSVS.
func (f *Factory) Format() opentile.Format { return opentile.FormatSVS }

// Supports reports whether the tiff.File looks like an SVS file.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return len(desc) >= len(aperioPrefix) && desc[:len(aperioPrefix)] == aperioPrefix
}

// Open constructs a format.Reader from an already-parsed tiff.File.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (format.Reader, error) {
	return openFromTIFFFile(file, opentileConfigToFormatConfig(cfg))
}

func opentileConfigToFormatConfig(cfg *opentile.Config) *format.Config {
	if cfg == nil {
		return &format.Config{}
	}
	ts, hasTS := cfg.TileSize()
	return &format.Config{
		TileSize:             ts,
		HasTileSize:          hasTS,
		CorruptTilePolicy:    cfg.CorruptTilePolicy(),
		NDPISynthesizedLabel: cfg.NDPISynthesizedLabel(),
		Backing:              cfg.Backing(),
	}
}
