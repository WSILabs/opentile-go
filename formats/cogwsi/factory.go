package cogwsi

import (
	"errors"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// GhostAreaMaxBytes caps the number of bytes read from the
// position immediately after the TIFF header when probing for a
// GDAL ghost area. Real ghost areas are typically <200 bytes; this
// generous upper bound protects against malformed files declaring
// implausible sizes.
const GhostAreaMaxBytes = 16384

// Factory implements opentile.FormatFactory for COG-WSI files.
type Factory struct{ opentile.RawUnsupported }

// New constructs a COG-WSI Factory ready for registration.
func New() *Factory { return &Factory{} }

// Format reports the format identifier for COG-WSI files.
func (f *Factory) Format() opentile.Format { return opentile.FormatCOGWSI }

// Supports is the TIFF-path entry point. Reads the ghost area
// (after the TIFF header) and returns true iff the COG_WSI_VERSION
// key is present.
func (f *Factory) Supports(tf *tiff.File) bool {
	ghost, err := readGhostArea(tf)
	if err != nil {
		return false
	}
	return ghost.COGWSIVersion != ""
}

// Open parses a COG-WSI file. Validates spec conformance and
// returns ErrNotConformantCOGWSI on violations.
func (f *Factory) Open(tf *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	return openCOGWSI(tf, cfg)
}

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
