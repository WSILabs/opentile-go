package generic

import (
	"errors"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Factory is the FormatFactory implementation for generic tiled
// pyramidal TIFF — the catch-all reader registered LAST in the
// dispatch order so vendor format detectors get first crack at any
// TIFF.
type Factory struct{ opentile.RawUnsupported }

// New returns a generic-TIFF factory. Safe to call once and register
// globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier.
func (f *Factory) Format() opentile.Format { return opentile.FormatGeneric }

// Supports reports whether file looks like a generic pyramidal TIFF
// per the v0.10 spec §4 algorithm: ≥3 tiled IFDs forming a coherent
// pyramid, each carrying valid uint8 RGB/YCbCr/grayscale photometric
// + whitelisted compression. Multi-pyramid TIFFs (more than 2
// leftover tiled IFDs, or any leftover larger than 1% of baseline
// area) are rejected — those are OME's job.
//
// Detection is conservative: when in doubt, return false. The
// dispatch loop falls through to ErrUnsupportedFormat rather than
// silently misclassifying a vendor-shaped TIFF.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	_, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	return err == nil
}

// Open constructs a generic-TIFF Tiler. T7+ wires the real Tiler
// + Level + AssociatedImage; until then this stub returns a
// not-implemented error so the factory wiring round-trips cleanly
// without silently mis-handling a Supports() match.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	return nil, errGenericTilerUnimplemented
}

// errGenericTilerUnimplemented is the placeholder Open failure used
// in T6 before the real Tiler lands in T7+. Removed once the Tiler
// is wired up.
var errGenericTilerUnimplemented = errors.New("formats/generic: Tiler not yet implemented (T7+)")
