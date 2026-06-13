// Package svs implements opentile-go format support for Aperio SVS files.
//
// SVS is a TIFF variant produced by Leica Aperio scanners used in digital
// pathology. This package detects SVS files, parses the Aperio metadata
// carried in the ImageDescription tag, and exposes the pyramid levels as
// opentile.Level values with raw compressed tile byte passthrough.
package svs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
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
	tiledLevels := make([]*tiledImage, 0, len(class.Levels))
	valueLevels := make([]opentile.Level, 0, len(class.Levels))
	var dirSpecs []svsDirSpec
	seenPages := make(map[int]bool)
	for levelIdx, pageIdx := range class.Levels {
		lvl, err := newTiledImage(levelIdx, pages[pageIdx], baseSize, md.MPP, file.ReaderAt(), cfg)
		if err != nil {
			return nil, fmt.Errorf("svs: page %d (level %d): %w", pageIdx, levelIdx, err)
		}
		tiledLevels = append(tiledLevels, lvl)
		valueLevels = append(valueLevels, opentile.Level{
			Index:        lvl.index,
			PyramidIndex: lvl.pyrIndex,
			Size:         lvl.size,
			TileSize:     lvl.tileSize,
			Grid:         lvl.grid,
			Compression:  lvl.compression,
			MPP:          lvl.mpp,
			FocalPlane:   0,
			Downsample:   float64(baseSize.W) / float64(lvl.size.W),
		})
		dirSpecs = append(dirSpecs, svsDirSpec{pageIdx: pageIdx, typ: opentile.DirLevel, level: levelIdx})
		seenPages[pageIdx] = true
	}
	images := []opentile.Pyramid{{
		Name:   "",
		Index:  0,
		Levels: valueLevels,
	}}
	var associated []opentile.AssociatedImage
	for _, spec := range []struct {
		imageType string
		pageIdx   int
	}{
		{"thumbnail", class.Thumbnail},
		{"label", class.Label},
		{"overview", class.Macro},
	} {
		if spec.pageIdx < 0 {
			continue
		}
		a, err := newAssociatedImage(spec.imageType, pages[spec.pageIdx], file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("svs: associated %s (page %d): %w", spec.imageType, spec.pageIdx, err)
		}
		associated = append(associated, a)
		dirSpecs = append(dirSpecs, svsDirSpec{pageIdx: spec.pageIdx, typ: opentile.DirAssociated, assoc: spec.imageType})
		seenPages[spec.pageIdx] = true
	}
	// Capture orphan pages (IFDs not surfaced as a level or associated image).
	for i := range pages {
		if !seenPages[i] {
			dirSpecs = append(dirSpecs, svsDirSpec{pageIdx: i, typ: opentile.DirOther})
		}
	}
	icc, _ := basePage.ICCProfile()
	return &tiler{md: md, levels: tiledLevels, images: images, associated: associated, icc: icc, file: file, dirSpecs: dirSpecs}, nil
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

// svsDirSpec captures the physical page index and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
type svsDirSpec struct {
	pageIdx int
	typ     opentile.DirectoryType
	level   int    // valid when typ==DirLevel
	assoc   string // valid when typ==DirAssociated; matches AssociatedImage.Type()
}

// tiler is the SVS implementation of format.Reader.
type tiler struct {
	md         Metadata
	levels     []*tiledImage
	images     []opentile.Pyramid
	associated []opentile.AssociatedImage
	icc        []byte
	file       *tiff.File   // retained for lazy TIFF-tag exposure
	dirSpecs   []svsDirSpec // page→role mapping captured at Open
}

func (t *tiler) Format() opentile.Format                  { return opentile.FormatSVS }
func (t *tiler) Pyramids() []opentile.Pyramid             { return t.images }
func (t *tiler) AssociatedImages() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }

// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider.
func (t *tiler) TIFFDirectories() []opentile.TIFFDirectory {
	pages := t.file.Pages()
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.pageIdx < 0 || ds.pageIdx >= len(pages) {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Type:           ds.typ,
			Image:          0, // SVS is single-image
			Level:          ds.level,
			AssociatedType: ds.assoc,
			Tags:           opentile.TIFFTagsFromPage(pages[ds.pageIdx]),
		})
	}
	return out
}

// AssociatedIFDOffset maps associated image a (matched on a.Type()) to its
// source IFD byte offset. Implements the opentile associated-IFD-offset
// provider. ok=false if a is not one of this slide's associated images.
func (t *tiler) AssociatedIFDOffset(a opentile.AssociatedImage) (int64, bool) {
	pages := t.file.Pages()
	for _, ds := range t.dirSpecs {
		if ds.typ != opentile.DirAssociated || ds.assoc != a.Type() {
			continue
		}
		if ds.pageIdx < 0 || ds.pageIdx >= len(pages) {
			return 0, false
		}
		return pages[ds.pageIdx].IFDOffset(), true
	}
	return 0, false
}

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return opentile.ErrLevelOutOfRange
	}
	return t.levels[level].warm()
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levels[level].Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levels[level].TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levels) {
		return 0
	}
	return t.levels[level].TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.levels) {
		return nil
	}
	return t.levels[level].TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levels) {
		return 0
	}
	return t.levels[level].TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levels[level].TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levels[level].TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.levels) {
		return func(yield func(opentile.TilePos, opentile.TileResult) bool) {}
	}
	return t.levels[level].Tiles(ctx)
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
