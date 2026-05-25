// testfactory_test.go provides a thin Factory shim for white-box tests that
// were written against the pre-v0.23 Factory API.
package generictiff

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is a thin test shim.
type Factory struct{}

// New returns a Factory shim. Only used in white-box tests.
func New() *Factory { return &Factory{} }

// Format returns FormatGenericTIFF.
func (f *Factory) Format() opentile.Format { return opentile.FormatGenericTIFF }

// Supports reports whether the tiff.File looks like a generic pyramidal TIFF.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	_, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	return err == nil
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
