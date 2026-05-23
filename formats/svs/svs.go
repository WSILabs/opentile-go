// Package svs implements opentile-go format support for Aperio SVS files.
//
// SVS is a TIFF variant produced by Leica Aperio scanners used in digital
// pathology. This package detects SVS files, parses the Aperio metadata
// carried in the ImageDescription tag, and exposes the pyramid levels as
// opentile.Level values with raw compressed tile byte passthrough.
package svs

import (
	"fmt"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// aperioPrefix is the literal prefix on the ImageDescription tag of Aperio SVS
// files. Upstream opentile and openslide both key their detection off this.
const aperioPrefix = "Aperio"

// Factory is the FormatFactory implementation for SVS.
type Factory struct{ opentile.RawUnsupported }

// New returns an SVS factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatSVS }

// Supports reports whether file looks like an SVS: its first page's
// ImageDescription starts with "Aperio".
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return strings.HasPrefix(desc, aperioPrefix)
}

// Open constructs an SVS Tiler from a parsed TIFF file.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("svs: file has no pages")
	}
	basePage := pages[0]
	desc, ok := basePage.ImageDescription()
	if !ok {
		return nil, fmt.Errorf("svs: base page missing ImageDescription")
	}
	md, err := parseDescription(desc)
	if err != nil {
		return nil, err
	}

	// Classify pages into Baseline / Thumbnail / Label / Macro series via
	// the tifffile-style algorithm in classifyPages. Level indices in the
	// returned Tiler are contiguous (0..N-1) in pyramid order and do not
	// correspond to physical page indices in the TIFF. Associated images are
	// emitted in tifffile's series order: Thumbnail, Label, Macro (any of
	// which may be absent).
	baseSize, err := pageSize(basePage)
	if err != nil {
		return nil, err
	}
	metas := make([]pageMeta, len(pages))
	for i, p := range pages {
		_, tiled := p.TileWidth()
		sub, _ := p.ScalarU32(tiff.TagNewSubfileType)
		metas[i] = pageMeta{
			Tiled:       tiled,
			Reduced:     sub&0x1 != 0,
			SubfileType: sub,
		}
	}
	class, err := classifyPages(metas)
	if err != nil {
		return nil, err
	}
	levels := make([]opentile.Level, 0, len(class.Levels))
	for levelIdx, pageIdx := range class.Levels {
		lvl, err := newTiledImage(levelIdx, pages[pageIdx], baseSize, md.MPP, file.ReaderAt(), cfg)
		if err != nil {
			return nil, fmt.Errorf("svs: page %d (level %d): %w", pageIdx, levelIdx, err)
		}
		levels = append(levels, lvl)
	}
	var associated []opentile.AssociatedImage
	for _, spec := range []struct {
		kind    string
		pageIdx int
	}{
		{"thumbnail", class.Thumbnail},
		{"label", class.Label},
		{"overview", class.Macro},
	} {
		if spec.pageIdx < 0 {
			continue
		}
		a, err := newAssociatedImage(spec.kind, pages[spec.pageIdx], file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("svs: associated %s (page %d): %w", spec.kind, spec.pageIdx, err)
		}
		associated = append(associated, a)
	}
	icc, _ := basePage.ICCProfile()
	return &tiler{md: md, levels: levels, associated: associated, icc: icc}, nil
}

// pageSize returns the (ImageWidth, ImageLength) as opentile.Size.
func pageSize(p *tiff.Page) (opentile.Size, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return opentile.Size{}, fmt.Errorf("ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return opentile.Size{}, fmt.Errorf("ImageLength missing")
	}
	return opentile.Size{W: int(iw), H: int(il)}, nil
}

// tiler is the SVS implementation of opentile.Tiler.
type tiler struct {
	md         Metadata
	levels     []opentile.Level
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format { return opentile.FormatSVS }
func (t *tiler) Images() []opentile.Image {
	return []opentile.Image{opentile.NewSingleImage(t.levels)}
}
func (t *tiler) Levels() []opentile.Level {
	// Return a fresh slice so callers cannot mutate the immutable internal
	// state. The underlying Level pointers are shared; only the slice header
	// is copied.
	out := make([]opentile.Level, len(t.levels))
	copy(out, t.levels)
	return out
}
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }
func (t *tiler) Level(i int) (opentile.Level, error) {
	if i < 0 || i >= len(t.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levels[i], nil
}
func (t *tiler) WarmLevel(i int) error {
	if i < 0 || i >= len(t.levels) {
		return opentile.ErrLevelOutOfRange
	}
	if w, ok := t.levels[i].(interface{ warm() error }); ok {
		return w.warm()
	}
	return nil
}

// tilerUnwrapper is implemented by opentile wrapper types (e.g., *fileCloser
// returned by OpenFile) that hold an inner Tiler. Kept unexported because it
// is a coordination interface between opentile and its format packages.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

// maxTilerUnwrapHops caps the number of UnwrapTiler calls MetadataOf will make.
// The realistic chain length is 1 (just *fileCloser); 16 is ample headroom
// while still preventing infinite loops on a wrapper that cycles.
const maxTilerUnwrapHops = 16

// MetadataOf returns the SVS-specific metadata if t is an SVS Tiler, otherwise
// (nil, false). It walks any number of wrappers (e.g., the *fileCloser
// returned by opentile.OpenFile) before asserting on the concrete type.
//
//	if md, ok := svs.MetadataOf(tiler); ok {
//	    fmt.Println(md.MPP, md.SoftwareLine)
//	}
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if svsT, ok := t.(*tiler); ok {
			return &svsT.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}
