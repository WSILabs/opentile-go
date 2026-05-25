// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
package szi

import (
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatSZI.
func (f *Factory) Format() opentile.Format { return opentile.FormatSZI }

// Supports always returns false for SZI (ZIP/non-TIFF format).
func (f *Factory) Supports(_ *tiff.File) bool { return false }

// SupportsRaw reports whether r looks like an SZI (ZIP) file.
func (f *Factory) SupportsRaw(r io.ReaderAt, size int64) bool {
	return matchSZI(r, size) == nil
}

// Open returns ErrUnsupportedFormat — SZI is not a TIFF-based format.
func (f *Factory) Open(_ *tiff.File, _ *opentile.Config) (format.Reader, error) {
	return nil, opentile.ErrUnsupportedFormat
}

// OpenRaw constructs a format.Reader from a raw ZIP reader.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (format.Reader, error) {
	return openSZIFormat(r, size, opentileConfigToFormatConfig(cfg))
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
