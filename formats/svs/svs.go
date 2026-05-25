// Package svs implements opentile-go format support for Aperio SVS files.
//
// SVS is a TIFF variant produced by Leica Aperio scanners used in digital
// pathology. This package detects SVS files, parses the Aperio metadata
// carried in the ImageDescription tag, and exposes the pyramid levels as
// opentile.Level values with raw compressed tile byte passthrough.
package svs

import (
	"errors"
	"fmt"
	"io"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
	format.Register("svs", matchSVS, openSVS)
}

// aperioPrefix is the literal prefix on the ImageDescription tag of Aperio SVS
// files. Upstream opentile and openslide both key their detection off this.
const aperioPrefix = "Aperio"

// matchSVS returns nil iff r is an SVS file (a TIFF whose first page's
// ImageDescription starts with "Aperio"). Returns an error if it is not.
func matchSVS(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("svs: not a TIFF: %w", err)
	}
	pages := file.Pages()
	if len(pages) == 0 {
		return errors.New("svs: TIFF has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok || !strings.HasPrefix(desc, aperioPrefix) {
		return errors.New("svs: ImageDescription does not start with Aperio")
	}
	return nil
}

// openSVS constructs a format.Reader from a raw reader. It re-parses the TIFF
// (matchSVS already parsed it once; the cost is negligible for header-only reads).
func openSVS(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("svs: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both openSVS and
// Factory.Open. cfg carries the format-level configuration.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
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
	// returned Reader are contiguous (0..N-1) in pyramid order and do not
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

// tiler is the SVS implementation of format.Reader.
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

// readerUnwrapper is implemented by wrapper types that hold an inner reader.
type readerUnwrapper interface {
	UnwrapReader() any
}

// maxUnwrapHops caps the number of UnwrapReader calls MetadataOf will make.
const maxUnwrapHops = 16

// MetadataOf returns the SVS-specific metadata if v is (or wraps) an SVS
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
//
//	if md, ok := svs.MetadataOf(slide); ok {
//	    fmt.Println(md.MPP, md.SoftwareLine)
//	}
func MetadataOf(v any) (*Metadata, bool) {
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if svsT, ok := v.(*tiler); ok {
			return &svsT.md, true
		}
		u, ok := v.(readerUnwrapper)
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}
