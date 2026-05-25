// Package ometiff implements opentile-go format support for OME TIFF
// files — a TIFF dialect carrying OME-XML metadata in the first
// page's ImageDescription, with reduced-resolution pyramid levels
// stored as TIFF SubIFDs of the base page.
//
// Direct port of Python opentile 0.20.0's formats/ome/ subtree
// (Apache 2.0, Sectra AB) — note: the upstream Python package directory
// is still called "ome"; opentile-go renamed to "ometiff" in v0.12
// (see docs/deferred.md §8f). One deliberate deviation: multi-image
// OME files (where several main pyramids share a single TIFF
// container — Leica-2.ome.tiff is one) expose every pyramid via the
// new format.Reader.Images() API. Upstream's base Tiler loop
// silently overwrites _level_series_index on each match and surfaces
// only the last main pyramid; we treat that as an upstream oversight
// rather than intentional behaviour.
package ometiff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// omeLevel is an internal interface for tile-read dispatch. Both
// *tiledImage and *oneframe.Image satisfy it. Stored in tiler.levels
// as [image][level] for (image, level, tx, ty) dispatch.
type omeLevel interface {
	Tile(x, y int) ([]byte, error)
	TileInto(x, y int, dst []byte) (int, error)
	TileMaxSize() int
	TilePrefix() []byte
	TileBodyMaxSize() int
	TileBodyInto(x, y int, dst []byte) (int, error)
	TileReader(x, y int) (io.ReadCloser, error)
	Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult]
}

// omeWarmer is optionally implemented by omeLevel types that support page
// pre-warming. warm() is unexported, so only same-package types (*tiledImage)
// can satisfy this; oneframe.Image (external package) is silently skipped.
type omeWarmer interface {
	warm() error
}

// omeDescriptionSuffix is the substring `is_ome` looks for at the end
// of the first page's ImageDescription. tifffile.py:10125-10129:
//
//	if self.index != 0 or not self.description:
//	    return False
//	return self.description[-10:].strip().endswith('OME>')
const omeDescriptionSuffix = "OME>"

const maxUnwrapHops = 16

// MetadataOf returns the OME-specific metadata if v is (or wraps) an OME
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
//
//	if md, ok := ometiff.MetadataOf(slide); ok {
//	    fmt.Println("OME images:", len(md.Images))
//	}
func MetadataOf(v any) (*OMEMetadata, bool) {
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if ot, ok := v.(*tiler); ok {
			return &ot.md, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}

// defaultOneFrameTileSize picks the tile size used for non-tiled
// (OneFrame) levels. Always uses the first main pyramid's base page
// TileWidth/TileLength — for byte parity with Python opentile, which
// hardcodes Size(self._base_page.tilewidth, self._base_page.tilelength)
// in OmeTiffTiler.get_level (ome_tiff_tiler.py:128) regardless of
// the user's tile_size argument. We deliberately ignore cfg.TileSize
// for OME; it's a no-op on this format.
func defaultOneFrameTileSize(pages []*tiff.Page, levelImageIndices []int) (opentile.Size, error) {
	if len(levelImageIndices) == 0 {
		return opentile.Size{}, errors.New("ome: cannot derive tile size — no main pyramids")
	}
	first := pages[levelImageIndices[0]]
	tw, ok := first.TileWidth()
	if !ok || tw == 0 {
		return opentile.Size{}, errors.New("ome: first main pyramid base page has no TileWidth — cannot default OneFrame tile size")
	}
	tl, ok := first.TileLength()
	if !ok || tl == 0 {
		return opentile.Size{}, errors.New("ome: first main pyramid base page has no TileLength")
	}
	return opentile.Size{W: int(tw), H: int(tl)}, nil
}

// pageDims returns a page's ImageWidth/ImageLength as opentile.Size.
func pageDims(p *tiff.Page) (opentile.Size, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return opentile.Size{}, errors.New("ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return opentile.Size{}, errors.New("ImageLength missing")
	}
	return opentile.Size{W: int(iw), H: int(il)}, nil
}

// buildLevels walks an OME main pyramid's SubIFD chain and returns
// both the value-type level metadata slice and the internal tile-read
// engine slice (top-level page L0 + each SubIFD as L1..Ln).
// Dispatches per-page on TileWidth: tiled pages → tiledImage,
// non-tiled pages → oneframe.Image.
func buildLevels(
	file *tiff.File,
	basePage *tiff.Page,
	baseSize opentile.Size,
	baseMPP opentile.SizeMm,
	oneFrameTileSize opentile.Size,
) ([]opentile.Level, []omeLevel, error) {
	pages := []*tiff.Page{basePage}
	if subOffsets, ok := basePage.SubIFDOffsets(); ok {
		for _, off := range subOffsets {
			sub, err := file.PageAtOffset(off)
			if err != nil {
				return nil, nil, fmt.Errorf("read SubIFD at %d: %w", off, err)
			}
			pages = append(pages, sub)
		}
	}
	valueLevels := make([]opentile.Level, 0, len(pages))
	engines := make([]omeLevel, 0, len(pages))
	for li, p := range pages {
		tw, _ := p.TileWidth()
		if tw > 0 {
			ti, err := newTiledImage(li, p, baseSize, baseMPP, file.ReaderAt())
			if err != nil {
				return nil, nil, fmt.Errorf("level %d (tiled): %w", li, err)
			}
			valueLevels = append(valueLevels, opentile.Level{
				Index:        ti.index,
				PyramidIndex: ti.pyrIndex,
				Size:         ti.size,
				TileSize:     ti.tileSize,
				Grid:         ti.grid,
				Compression:  ti.compression,
				MPP:          ti.mpp,
				Downsample:   float64(baseSize.W) / float64(ti.size.W),
			})
			engines = append(engines, ti)
		} else {
			of, err := newOneFrameImage(li, p, oneFrameTileSize, baseSize, baseMPP, file.ReaderAt())
			if err != nil {
				return nil, nil, fmt.Errorf("level %d (oneframe): %w", li, err)
			}
			sz := of.Size()
			valueLevels = append(valueLevels, opentile.Level{
				Index:        of.Index(),
				PyramidIndex: of.PyramidIndex(),
				Size:         sz,
				TileSize:     of.TileSize(),
				Grid:         of.Grid(),
				Compression:  of.Compression(),
				MPP:          of.MPP(),
				Downsample:   float64(baseSize.W) / float64(sz.W),
			})
			engines = append(engines, of)
		}
	}
	return valueLevels, engines, nil
}

// tiler is the OME implementation of format.Reader.
//
// images holds the value-type opentile.Image slice (public surface).
// levels holds the per-(image, level) tile-read engines (private
// dispatch table): levels[imageIdx][levelIdx] → omeLevel.
type tiler struct {
	md         OMEMetadata
	cross      opentile.Metadata // v0.17 cross-format view; populated from md at Open time
	images     []opentile.Image
	levels     [][]omeLevel // [imageIdx][levelIdx]
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format    { return opentile.FormatOMETIFF }
func (t *tiler) Images() []opentile.Image   { return t.images }
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.cross }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image < 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	lvls := t.images[image].Levels
	if level < 0 || level >= len(lvls) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return lvls[level], nil
}

func (t *tiler) WarmLevel(image, level int) error {
	eng, err := t.engine(image, level)
	if err != nil {
		return err
	}
	if w, ok := eng.(omeWarmer); ok {
		return w.warm()
	}
	return nil
}

// engine returns the tile-read engine for (image, level), validating
// bounds and returning ErrImageIndexOutOfRange / ErrLevelOutOfRange.
func (t *tiler) engine(image, level int) (omeLevel, error) {
	if image < 0 || image >= len(t.levels) {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levels[image]) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levels[image][level], nil
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0, err
	}
	return eng.TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0
	}
	return eng.TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil
	}
	return eng.TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0
	}
	return eng.TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0, err
	}
	return eng.TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	eng, err := t.engine(image, level)
	if err != nil {
		return func(yield func(opentile.TilePos, opentile.TileResult) bool) {}
	}
	return eng.Tiles(ctx)
}
