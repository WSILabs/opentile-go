package leicascn

import (
	"errors"
	"strings"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Factory is the FormatFactory implementation for Leica SCN.
// Registered BEFORE generictiff in formats/all so vendor detection
// wins on any TIFF that smells like SCN.
type Factory struct{ opentile.RawUnsupported }

// New returns a Leica SCN factory. Safe to call once and register
// globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier.
func (f *Factory) Format() opentile.Format { return opentile.FormatLeicaSCN }

// Supports reports whether file looks like a Leica SCN BigTIFF.
// Discriminator (sealed Q1): IFD 0's ImageDescription contains the
// SCN schema URN. Cheap substring search; full XML parse happens at
// Open time.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return strings.Contains(desc, SchemaURN)
}

// Open constructs a Leica SCN Tiler. T4 placeholder returns
// errSCNTilerUnimplemented; T6 wires the real Tiler.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	return nil, errSCNTilerUnimplemented
}

// errSCNTilerUnimplemented is the placeholder returned by Open in
// T4-T5 before the Tiler scaffolding lands in T6. Removed once the
// Tiler is wired up.
var errSCNTilerUnimplemented = errors.New("formats/leicascn: Tiler not yet implemented (T6+)")
