package szi

import (
	"encoding/binary"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Factory implements opentile.FormatFactory for Smart Zoom Image
// files. Detection uses the SupportsRaw / OpenRaw byte-level path
// (mirrors the v0.8 IFE precedent for non-TIFF formats).
type Factory struct{}

// New returns an SZI factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatSZI }

// SupportsRaw sniffs the first 4 bytes of r for the ZIP local-file-
// header magic (PK\x03\x04). True only on full match.
//
// SZI files are ZIP archives; a deeper check (presence of a .dzi
// entry inside) happens at OpenRaw time. Any other ZIP file would
// fail OpenRaw; SupportsRaw stays cheap.
func (f *Factory) SupportsRaw(r io.ReaderAt, size int64) bool {
	if size < 4 {
		return false
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(buf[:]) == 0x04034B50
}

// OpenRaw parses an SZI file and returns a Tiler.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (opentile.Tiler, error) {
	return openSZI(r, size, cfg)
}

// Supports is the TIFF-path entry point; SZI files are never
// TIFFs, so this always returns false. Required to satisfy
// opentile.FormatFactory.
func (f *Factory) Supports(*tiff.File) bool { return false }

// Open is the TIFF-path entry point; never reached because
// Supports returns false.
func (f *Factory) Open(*tiff.File, *opentile.Config) (opentile.Tiler, error) {
	return nil, opentile.ErrUnsupportedFormat
}
