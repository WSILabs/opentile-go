package ife

import (
	"encoding/binary"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
	// TODO(v0.23): remove old opentile.Register once tiler.go deletion lands.
	opentile.Register(&Factory{})
	format.Register("ife", matchIFE, openIFEFormat)
}

// matchIFE probes the raw bytes for the IFE magic number
// (0x49726973 LE — "Iris"). IFE is not a TIFF; detection is purely
// byte-based without any TIFF parsing.
func matchIFE(r io.ReaderAt, size int64) error {
	if size < 4 {
		return fmt.Errorf("ife: file too small (%d bytes)", size)
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return fmt.Errorf("ife: read magic: %w", err)
	}
	if binary.LittleEndian.Uint32(buf[:]) != MagicBytes {
		return fmt.Errorf("ife: magic mismatch (not an IFE file)")
	}
	return nil
}

// openIFEFormat constructs a format.Reader from a raw reader.
func openIFEFormat(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	return openIFE(r, size, cfg)
}

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
	return matchIFE(r, size) == nil
}

// OpenRaw parses an IFE v1.0 file and returns a Tiler.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (opentile.Tiler, error) {
	fcfg := opentileConfigToFormatConfig(cfg)
	return openIFE(r, size, fcfg)
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

// opentileConfigToFormatConfig translates the opaque opentile.Config wrapper
// into a format.Config. Called from Factory.OpenRaw during the dual-registration
// transition; the new openIFEFormat path receives *format.Config directly.
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
