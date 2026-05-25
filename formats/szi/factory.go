package szi

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *Tiler satisfies format.Reader.
var _ format.Reader = (*Tiler)(nil)

func init() {
	// TODO(v0.23): remove old opentile.Register once tiler.go deletion lands.
	opentile.Register(&Factory{})
	format.Register("szi", matchSZI, openSZIFormat)
}

// matchSZI returns nil iff r looks like an SZI file (ZIP local-file-
// header magic PK\x03\x04 at offset 0). Any other ZIP that fails
// OpenRaw is rejected then; the match step stays cheap.
func matchSZI(r io.ReaderAt, size int64) error {
	if size < 4 {
		return fmt.Errorf("szi: file too small (%d bytes)", size)
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return fmt.Errorf("szi: read magic: %w", err)
	}
	const zipMagic uint32 = 0x04034B50
	got := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
	if got != zipMagic {
		return fmt.Errorf("szi: magic mismatch (not a ZIP/SZI file)")
	}
	return nil
}

// openSZIFormat constructs a format.Reader from a raw reader.
func openSZIFormat(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	return openSZIWithFormatConfig(r, size, cfg)
}

// opentileConfigToFormatConfig translates the opaque opentile.Config wrapper
// into a format.Config. Called from Factory.OpenRaw during the dual-registration
// transition; the new openSZIFormat path receives *format.Config directly.
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
	return matchSZI(r, size) == nil
}

// OpenRaw parses an SZI file and returns a Tiler.
func (f *Factory) OpenRaw(r io.ReaderAt, size int64, cfg *opentile.Config) (opentile.Tiler, error) {
	fcfg := opentileConfigToFormatConfig(cfg)
	return openSZIWithFormatConfig(r, size, fcfg)
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
