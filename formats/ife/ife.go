package ife

import (
	"encoding/binary"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory is the FormatFactory implementation for Iris IFE — the
// first non-TIFF format in opentile-go. It overrides SupportsRaw +
// OpenRaw rather than the TIFF-path Supports + Open. The TIFF-path
// methods are present (returning false / ErrUnsupportedFormat) so
// the factory satisfies opentile.FormatFactory.
type Factory struct{}

// New returns an IFE factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatIFE }

// SupportsRaw sniffs the first 4 bytes of r for the IFE magic
// (0x49726973 LE — "Iris"). True only on full match. Files smaller
// than 4 bytes never match.
func (f *Factory) SupportsRaw(r io.ReaderAt, size int64) bool {
	if size < 4 {
		return false
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(buf[:]) == MagicBytes
}

// OpenRaw parses an IFE v1.0 file and returns a Tiler.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (opentile.Tiler, error) {
	return openIFE(r, size, cfg)
}

// Supports is the TIFF-path entry point; IFE files are never TIFFs,
// so this always returns false. Required to satisfy
// opentile.FormatFactory.
func (f *Factory) Supports(*tiff.File) bool { return false }

// Open is the TIFF-path entry point; never reached because Supports
// returns false. Required to satisfy opentile.FormatFactory.
func (f *Factory) Open(*tiff.File, *opentile.Config) (opentile.Tiler, error) {
	return nil, opentile.ErrUnsupportedFormat
}
