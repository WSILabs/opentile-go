package cogwsi

import (
	"errors"
	"fmt"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/cog"
	"github.com/cornish/opentile-go/internal/tiff"
)

// ErrNotConformantCOGWSI is returned by Open when the file claims
// to be COG-WSI (via the COG_WSI_VERSION ghost-area key) but
// violates the spec.
var ErrNotConformantCOGWSI = errors.New("cogwsi: file is not spec-conformant")

// Tiler is the COG-WSI format implementation of opentile.Tiler.
//
// T5 ships a skeleton: Format() / Close() are real, but Levels /
// Images / Associated / Metadata / ICCProfile / WarmLevel return
// placeholders. T6 fills in real behavior backed by the WSI
// private tags and the generic-TIFF tile-fetch infrastructure.
type Tiler struct {
	tf    *tiff.File
	ghost cog.GhostArea
	cfg   *opentile.Config
	// T6: pyramid + associated + metadata
}

// openCOGWSI parses the ghost area, validates the COG-WSI version,
// and constructs a Tiler. Detailed spec validation lands in T6.
func openCOGWSI(tf *tiff.File, cfg *opentile.Config) (*Tiler, error) {
	ghost, err := readGhostArea(tf)
	if err != nil {
		return nil, fmt.Errorf("cogwsi: ghost-area read: %w", err)
	}
	if ghost.COGWSIVersion == "" {
		// Shouldn't reach here — Supports() returned true. Defensive.
		return nil, fmt.Errorf("%w: ghost area lacks COG_WSI_VERSION", ErrNotConformantCOGWSI)
	}

	major, _, err := cog.ParseCOGWSIVersion(ghost.COGWSIVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConformantCOGWSI, err)
	}
	if major > 0 {
		return nil, fmt.Errorf("%w: unsupported major version %d (reader supports 0.x)",
			ErrNotConformantCOGWSI, major)
	}

	t := &Tiler{
		tf:    tf,
		ghost: ghost,
		cfg:   cfg,
	}
	// T6: full spec validation + pyramid + associated + metadata wiring.
	return t, nil
}

// Format reports the format identifier.
func (t *Tiler) Format() opentile.Format { return opentile.FormatCOGWSI }

// Close releases resources associated with the Tiler. The
// underlying io.ReaderAt is owned by the caller per the opentile
// contract; Close drops the reference but does not close it.
func (t *Tiler) Close() error { t.tf = nil; return nil }

// T6 will replace these placeholders with real implementations.

// Images returns the main pyramids. T5 placeholder — T6 fills in.
func (t *Tiler) Images() []opentile.Image { return nil }

// Levels returns the main pyramid levels. T5 placeholder.
func (t *Tiler) Levels() []opentile.Level { return nil }

// Level returns the level at index i. T5 placeholder — always
// reports ErrLevelOutOfRange until T6 populates pyramids.
func (t *Tiler) Level(i int) (opentile.Level, error) { return nil, opentile.ErrLevelOutOfRange }

// Associated returns associated images. T5 placeholder.
func (t *Tiler) Associated() []opentile.AssociatedImage { return nil }

// Metadata returns the cross-format metadata. T5 placeholder.
func (t *Tiler) Metadata() opentile.Metadata { return opentile.Metadata{} }

// ICCProfile returns the ICC profile bytes, if any. T5 placeholder.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel pre-warms the OS page cache for level i. T5 placeholder
// — always reports ErrLevelOutOfRange until T6 populates pyramids.
func (t *Tiler) WarmLevel(i int) error { return opentile.ErrLevelOutOfRange }
