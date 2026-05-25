// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
package bif

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatBIF.
func (f *Factory) Format() opentile.Format { return opentile.FormatBIF }

// Supports reports whether the tiff.File looks like a BIF file.
func (f *Factory) Supports(file *tiff.File) bool {
	return Detect(file)
}

// Open constructs a format.Reader from an already-parsed tiff.File.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (*Tiler, error) {
	r, err := openFromTIFFFile(file, opentileConfigToFormatConfig(cfg))
	if err != nil {
		return nil, err
	}
	return r.(*Tiler), nil
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
