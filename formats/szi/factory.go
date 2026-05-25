package szi

import (
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/internal/format"
)

// Compile-time assertion: *Tiler satisfies format.Reader.
var _ format.Reader = (*Tiler)(nil)

func init() {
	format.Register("szi", matchSZI, openSZIFormat)
}

// matchSZI returns nil iff r looks like an SZI file (ZIP local-file-
// header magic PK\x03\x04 at offset 0). Any other ZIP that fails
// openSZIFormat is rejected then; the match step stays cheap.
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
