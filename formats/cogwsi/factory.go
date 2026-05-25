package cogwsi

import (
	"errors"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *Tiler satisfies format.Reader.
var _ format.Reader = (*Tiler)(nil)

func init() {
	format.Register("cogwsi", matchCOGWSI, openCOGWSIFormat)
}

// matchCOGWSI returns nil iff r is a COG-WSI file (TIFF with a ghost
// area containing the COG_WSI_VERSION key).
func matchCOGWSI(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("cogwsi: not a TIFF: %w", err)
	}
	ghost, err := readGhostArea(file)
	if err != nil {
		return fmt.Errorf("cogwsi: ghost area: %w", err)
	}
	if ghost.COGWSIVersion == "" {
		return fmt.Errorf("cogwsi: no COG_WSI_VERSION in ghost area")
	}
	return nil
}

// openCOGWSIFormat constructs a format.Reader from a raw reader.
func openCOGWSIFormat(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("cogwsi: %w", err)
	}
	return openCOGWSIFromFile(file, cfg)
}

// GhostAreaMaxBytes caps the number of bytes read from the
// position immediately after the TIFF header when probing for a
// GDAL ghost area. Real ghost areas are typically <200 bytes; this
// generous upper bound protects against malformed files declaring
// implausible sizes.
const GhostAreaMaxBytes = 16384

// readGhostArea reads the ghost-area bytes from the file. The
// ghost area starts immediately after the TIFF header: offset 8
// for classic TIFF (2-byte byte-order + 2-byte version + 4-byte
// first-IFD pointer) or offset 16 for BigTIFF (2 + 2 + 2 + 2 +
// 8-byte first-IFD pointer).
//
// Reads up to GhostAreaMaxBytes; truncates against EOF without
// erroring (smaller files are valid). The ghost area parser
// validates the declared SIZE header itself.
func readGhostArea(tf *tiff.File) (cog.GhostArea, error) {
	var headerLen int64 = 8
	if tf.BigTIFF() {
		headerLen = 16
	}
	size := tf.Size()
	if size <= headerLen {
		return cog.GhostArea{}, fmt.Errorf("cogwsi: file too small for ghost area (size=%d)", size)
	}
	want := int64(GhostAreaMaxBytes)
	if avail := size - headerLen; avail < want {
		want = avail
	}
	buf := make([]byte, want)
	n, err := tf.ReaderAt().ReadAt(buf, headerLen)
	if err != nil && !errors.Is(err, io.EOF) {
		return cog.GhostArea{}, fmt.Errorf("cogwsi: read ghost area: %w", err)
	}
	return cog.ParseGhostArea(buf[:n])
}
