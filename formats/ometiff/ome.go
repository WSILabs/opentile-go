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
// new opentile.Tiler.Images() API. Upstream's base Tiler loop
// silently overwrites _level_series_index on each match and surfaces
// only the last main pyramid; we treat that as an upstream oversight
// rather than intentional behaviour.
package ometiff

import (
	"errors"
	"fmt"
	"strings"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// omeDescriptionSuffix is the substring `is_ome` looks for at the end
// of the first page's ImageDescription. tifffile.py:10125-10129:
//
//	if self.index != 0 or not self.description:
//	    return False
//	return self.description[-10:].strip().endswith('OME>')
const omeDescriptionSuffix = "OME>"

// Factory is the FormatFactory implementation for OME TIFF.
type Factory struct{ opentile.RawUnsupported }

// New returns an OME factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatOMETIFF }

// Supports reports whether file looks like an OME TIFF: its first
// page's ImageDescription's last 10 characters, after stripping
// trailing whitespace, end with `OME>` (i.e. the closing tag of the
// `<OME>` root element). Direct port of tifffile's `is_ome`
// predicate.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok || desc == "" {
		return false
	}
	tail := desc
	if len(tail) > 10 {
		tail = tail[len(tail)-10:]
	}
	return strings.HasSuffix(strings.TrimSpace(tail), omeDescriptionSuffix)
}

// Open constructs an OME Tiler from a parsed TIFF file. Parses
// OME-XML metadata from page-0's ImageDescription, classifies Images
// into main pyramids vs. associated, and walks each main pyramid's
// SubIFD chain to build per-level Tiles.
//
// Multi-image OME files (Leica-2 has 4 main pyramids) expose all
// pyramids via Tiler.Images(); upstream's last-wins behaviour is the
// v0.6 deviation documented in docs/deferred.md "Deviations from
// upstream Python opentile".
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, errors.New("ome: file has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return nil, errors.New("ome: page 0 missing ImageDescription")
	}
	md, err := parseOMEMetadata(desc)
	if err != nil {
		return nil, err
	}
	cls, err := classifyImages(md.Images)
	if err != nil {
		return nil, err
	}

	// Default OneFrame tile size: caller-supplied WithTileSize wins;
	// otherwise fall back to the first main pyramid's base page tile
	// dims (mirrors upstream's tile_size = Size(base_page.tilewidth,
	// base_page.tilelength)).
	oneFrameTileSize, err := defaultOneFrameTileSize(pages, cls.LevelImages, cfg)
	if err != nil {
		return nil, err
	}

	images := make([]opentile.Image, 0, len(cls.LevelImages))
	for k, omeIdx := range cls.LevelImages {
		// Top-level pages align 1:1 with OME Image indices in document
		// order (verified empirically against both Leica fixtures —
		// series[i].pages[0].offset == tf.pages[i].offset).
		if omeIdx >= len(pages) {
			return nil, fmt.Errorf("ome: OME Image %d has no corresponding TIFF page (only %d top-level pages)", omeIdx, len(pages))
		}
		basePage := pages[omeIdx]
		baseSize, err := pageDims(basePage)
		if err != nil {
			return nil, fmt.Errorf("ome: image %d base page: %w", omeIdx, err)
		}
		baseMPP := opentile.SizeMm{
			W: md.Images[omeIdx].PhysicalSizeX,
			H: md.Images[omeIdx].PhysicalSizeY,
		}
		levels, err := buildLevels(file, basePage, baseSize, baseMPP, oneFrameTileSize)
		if err != nil {
			return nil, fmt.Errorf("ome: image %d: %w", omeIdx, err)
		}
		omeImg := md.Images[omeIdx]
		// SizeC discriminator (per T2 gate outcome): use the count of
		// <Channel> elements, NOT <Pixels SizeC>. <Pixels SizeC>
		// describes per-pixel sample count (3 on RGB brightfield);
		// <Channel> count describes separately-stored channels (1 on
		// brightfield; > 1 only on fluorescence).
		sizeC := omeImg.Channels
		if sizeC < 1 {
			sizeC = 1
		}
		images = append(images, &pyramidImage{
			index:        k,
			name:         omeImg.Name,
			levels:       levels,
			mpp:          baseMPP,
			sizeZ:        omeImg.SizeZ,
			sizeC:        sizeC,
			sizeT:        omeImg.SizeT,
			channelNames: append([]string(nil), omeImg.ChannelNames...),
		})
	}

	// Associated images. Order matches our convention (thumbnail,
	// label, overview) for parity with SVS/NDPI/Philips.
	var associated []opentile.AssociatedImage
	for _, spec := range []struct {
		kind   string
		omeIdx int
	}{
		{"thumbnail", cls.Thumbnail},
		{"label", cls.Label},
		{"overview", cls.Macro}, // OME XML calls it "macro"; we expose as "overview"
	} {
		if spec.omeIdx < 0 {
			continue
		}
		if spec.omeIdx >= len(pages) {
			return nil, fmt.Errorf("ome: associated %s OME Image %d has no corresponding TIFF page", spec.kind, spec.omeIdx)
		}
		a, err := newAssociatedImage(spec.kind, pages[spec.omeIdx], file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("ome: associated %s: %w", spec.kind, err)
		}
		associated = append(associated, a)
	}

	icc, _ := pages[0].ICCProfile()
	cross := crossMetadata(md, cls)
	return &tiler{
		md:         md,
		cross:      cross,
		images:     images,
		associated: associated,
		icc:        icc,
	}, nil
}

// tilerUnwrapper is the same coordination interface SVS / NDPI / Philips
// use to peel off opentile's *fileCloser wrapper before MetadataOf can
// type-assert on the concrete *tiler.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

const maxTilerUnwrapHops = 16

// MetadataOf returns the OME-specific metadata if t is an OME Tiler,
// otherwise (nil, false). Walks any number of wrappers before
// asserting on the concrete type.
//
//	if md, ok := ometiff.MetadataOf(tiler); ok {
//	    fmt.Println("OME images:", len(md.Images))
//	}
func MetadataOf(t opentile.Tiler) (*OMEMetadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if ot, ok := t.(*tiler); ok {
			return &ot.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
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
func defaultOneFrameTileSize(pages []*tiff.Page, levelImageIndices []int, _ *opentile.Config) (opentile.Size, error) {
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
// the level list (top-level page L0 + each SubIFD as L1..Ln).
// Dispatches per-page on TileWidth: tiled pages → tiledImage,
// non-tiled pages → oneframe.Image.
func buildLevels(
	file *tiff.File,
	basePage *tiff.Page,
	baseSize opentile.Size,
	baseMPP opentile.SizeMm,
	oneFrameTileSize opentile.Size,
) ([]opentile.Level, error) {
	pages := []*tiff.Page{basePage}
	if subOffsets, ok := basePage.SubIFDOffsets(); ok {
		for _, off := range subOffsets {
			sub, err := file.PageAtOffset(off)
			if err != nil {
				return nil, fmt.Errorf("read SubIFD at %d: %w", off, err)
			}
			pages = append(pages, sub)
		}
	}
	out := make([]opentile.Level, 0, len(pages))
	for li, p := range pages {
		tw, _ := p.TileWidth()
		var lvl opentile.Level
		if tw > 0 {
			t, err := newTiledImage(li, p, baseSize, baseMPP, file.ReaderAt())
			if err != nil {
				return nil, fmt.Errorf("level %d (tiled): %w", li, err)
			}
			lvl = t
		} else {
			of, err := newOneFrameImage(li, p, oneFrameTileSize, baseSize, baseMPP, file.ReaderAt())
			if err != nil {
				return nil, fmt.Errorf("level %d (oneframe): %w", li, err)
			}
			lvl = of
		}
		out = append(out, lvl)
	}
	return out, nil
}

// pyramidImage is the OME-specific opentile.Image implementation.
// Multi-image files expose multiple instances via Tiler.Images().
//
// Multi-dim accessors (added v0.7): every Leica fixture in our suite
// has SizeZ=SizeC=SizeT=1 (verified via the T2 OME-XML probe). For
// future multi-Z OME slides, these accessors get populated from
// <Pixels>/<Channel> in T12; the actual TileAt(z != 0) read path
// remains deferred (OME multi-Z reading is its own milestone).
type pyramidImage struct {
	index  int
	name   string
	levels []opentile.Level
	mpp    opentile.SizeMm

	// Multi-dim dimensions (v0.7). Populated by T12 from
	// OME-XML <Pixels SizeZ/SizeC/SizeT> + <Channel> element count.
	// All default to 1 for now (SingleImage-equivalent semantics);
	// T12 wires real values.
	sizeZ, sizeC, sizeT int
	channelNames        []string
}

func (i *pyramidImage) Index() int           { return i.index }
func (i *pyramidImage) Name() string         { return i.name }
func (i *pyramidImage) MPP() opentile.SizeMm { return i.mpp }
func (i *pyramidImage) Levels() []opentile.Level {
	out := make([]opentile.Level, len(i.levels))
	copy(out, i.levels)
	return out
}
func (i *pyramidImage) Level(k int) (opentile.Level, error) {
	if k < 0 || k >= len(i.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return i.levels[k], nil
}
func (i *pyramidImage) SizeZ() int { return max1(i.sizeZ) }
func (i *pyramidImage) SizeC() int { return max1(i.sizeC) }
func (i *pyramidImage) SizeT() int { return max1(i.sizeT) }
func (i *pyramidImage) ChannelName(c int) string {
	if c < 0 || c >= len(i.channelNames) {
		return ""
	}
	return i.channelNames[c]
}
func (i *pyramidImage) ZPlaneFocus(z int) float64 {
	// OME-XML <Plane PositionZ> exists but isn't parsed in v0.7;
	// even a multi-Z OME would report 0 for every plane until
	// the future multi-Z reader lands.
	return 0
}

// max1 normalises a possibly-zero count (parser default before
// T12) to 1 so legacy 2D OME callers get the SingleImage-equivalent
// answer. Once T12 wires Size{Z,C,T} from the parser, the values
// will be ≥ 1 by construction and this helper becomes a passthrough.
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// tiler is the OME implementation of opentile.Tiler.
type tiler struct {
	md         OMEMetadata
	cross      opentile.Metadata // v0.17 cross-format view; populated from md at Open time
	images     []opentile.Image
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format { return opentile.FormatOMETIFF }
func (t *tiler) Images() []opentile.Image {
	out := make([]opentile.Image, len(t.images))
	copy(out, t.images)
	return out
}
func (t *tiler) Levels() []opentile.Level {
	if len(t.images) == 0 {
		return nil
	}
	return t.images[0].Levels()
}
func (t *tiler) Level(i int) (opentile.Level, error) {
	if len(t.images) == 0 {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.images[0].Level(i)
}
func (t *tiler) WarmLevel(i int) error {
	lvl, err := t.Level(i)
	if err != nil {
		return err
	}
	if w, ok := lvl.(interface{ warm() error }); ok {
		return w.warm()
	}
	return nil
}
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.cross }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }
