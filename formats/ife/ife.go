package ife

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/internal/format"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
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

