package cogwsi

import (
	"errors"
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/formats/generictiff"
	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// ErrNotConformantCOGWSI is returned by Open when the file claims
// to be COG-WSI (via the COG_WSI_VERSION ghost-area key) but
// violates the spec. Callers can match with
// errors.Is(err, cogwsi.ErrNotConformantCOGWSI).
var ErrNotConformantCOGWSI = errors.New("cogwsi: file is not spec-conformant")

// Tiler is the COG-WSI format implementation of opentile.Tiler.
//
// Per v0.19 plan T6, cogwsi delegates the tile-byte hot path
// (pyramid + associated level construction, tile reads, splice
// prefix, mmap aliasing) to formats/generictiff via the
// WSI-tag-aware path landed in T4. cogwsi's distinctions are:
//
//   - Spec validation at open (ErrNotConformantCOGWSI) — done
//     before the inner Open runs, so non-conformant files fail
//     fast with a precise message.
//   - Format() returns FormatCOGWSI (not FormatGenericTIFF), so
//     downstream consumers can distinguish.
//   - Metadata() augments the cross-format struct with the
//     WSIMPP*/WSIMagnification tags + COG-WSI Properties.
type Tiler struct {
	inner opentile.Tiler
	md    opentile.Metadata
}

// openCOGWSIFromFile is the shared construction path used by both
// openCOGWSIFormat (new format.Register path) and Factory.Open
// (legacy FormatFactory path).
func openCOGWSIFromFile(tf *tiff.File, cfg *format.Config) (*Tiler, error) {
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

	// Spec §3 — ghost area required-value invariants.
	if err := validateGhost(ghost); err != nil {
		return nil, err
	}

	// Spec §5.2 + §6 — per-IFD WSI tag conformance + ordering.
	pages := tf.Pages()
	if err := validateIFDs(pages); err != nil {
		return nil, err
	}

	// Build the inner tile-byte machinery via generic-TIFF. T4
	// added WSI-tag awareness so the existing Open path handles
	// COG-WSI pyramids + associated classification correctly.
	inner, err := generictiff.New().OpenFromTIFF(tf, cfg)
	if err != nil {
		return nil, fmt.Errorf("cogwsi: generic-tiff open: %w", err)
	}

	md := buildMetadata(pages[0], ghost)

	return &Tiler{inner: inner, md: md}, nil
}

// Format reports the format identifier.
func (t *Tiler) Format() opentile.Format { return opentile.FormatCOGWSI }

// Close releases resources held by the inner Tiler.
func (t *Tiler) Close() error {
	if t.inner == nil {
		return nil
	}
	err := t.inner.Close()
	t.inner = nil
	return err
}

// Images returns the main pyramids. Delegates to the inner generic
// Tiler; COG-WSI files always carry a single pyramid (spec §5.2).
func (t *Tiler) Images() []opentile.Image { return t.inner.Images() }

// Levels returns the main pyramid levels (shortcut for Images()[0].
// Levels()).
func (t *Tiler) Levels() []opentile.Level { return t.inner.Levels() }

// Level returns the pyramid level at index i.
func (t *Tiler) Level(i int) (opentile.Level, error) { return t.inner.Level(i) }

// Associated returns associated images (label / overview / thumbnail
// per v0.15 canonical naming; the WSIImageType=macro tag maps to
// Type() == "overview" per the v0.15 SCN+generictiff alignment).
func (t *Tiler) Associated() []opentile.AssociatedImage { return t.inner.Associated() }

// Metadata returns the COG-WSI-specific cross-format Metadata —
// MPP / Magnification populated from the WSI private tags; scanner
// attribution from standard TIFF tags (preserved by the writer per
// spec §5.2); Properties[cog-wsi.*] for source-format / wsitools-
// version / spec-version.
func (t *Tiler) Metadata() opentile.Metadata { return t.md }

// ICCProfile returns the ICC profile bytes from L0, if present.
func (t *Tiler) ICCProfile() []byte { return t.inner.ICCProfile() }

// WarmLevel pre-warms the OS page cache for level i. Delegates to
// the inner Tiler (which implements the v0.9 mmap-aware warm path).
func (t *Tiler) WarmLevel(i int) error { return t.inner.WarmLevel(i) }

// UnwrapTiler exposes the inner generic-TIFF Tiler so callers that
// hold a *cogwsi.Tiler can reach the generic-TIFF-format-specific
// helpers (e.g., generictiff.MetadataOf). Mirrors the unwrap
// pattern used by *fileCloser and other format wrappers.
func (t *Tiler) UnwrapTiler() opentile.Tiler { return t.inner }
