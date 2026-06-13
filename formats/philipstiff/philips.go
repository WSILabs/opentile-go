package philipstiff

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
	format.Register("philipstiff", matchPhilips, openPhilips)
}

// philipsSoftwarePrefix is the literal prefix on the Software tag (305)
// that identifies a Philips IntelliSite Pathology Solution scan.
// Upstream tifffile keys detection off this:
//
//	software[:10] == 'Philips DP' AND description[-16:].strip().endswith('</DataObject>')
const philipsSoftwarePrefix = "Philips DP"

// philipsDescriptionSuffix is the closing tag of the DICOM-XML blob
// Philips writes into the ImageDescription tag (270). Upstream pins
// detection on the suffix to avoid false positives from generic
// </DataObject>-bearing XML in non-Philips TIFFs.
const philipsDescriptionSuffix = "</DataObject>"

// matchPhilips returns nil iff r is a Philips TIFF file.
func matchPhilips(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("philips: not a TIFF: %w", err)
	}
	pages := file.Pages()
	if len(pages) == 0 {
		return errors.New("philips: TIFF has no pages")
	}
	sw, ok := pages[0].Software()
	if !ok || !strings.HasPrefix(sw, philipsSoftwarePrefix) {
		return errors.New("philips: Software tag does not start with Philips DP")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return errors.New("philips: ImageDescription missing")
	}
	if !strings.HasSuffix(strings.TrimSpace(desc), philipsDescriptionSuffix) {
		return errors.New("philips: ImageDescription does not end with </DataObject>")
	}
	return nil
}

// openPhilips constructs a format.Reader from a raw reader. It re-parses
// the TIFF (matchPhilips already parsed it; header-only reads are cheap).
func openPhilips(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("philips: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both openPhilips
// and Factory.Open.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("philips: file has no pages")
	}

	desc, ok := pages[0].ImageDescription()
	if !ok {
		return nil, fmt.Errorf("philips: base page missing ImageDescription")
	}

	md, err := parseMetadata(desc)
	if err != nil {
		return nil, err
	}

	// Raw on-disk page-0 dims drive computeCorrectedSizes. The corrected
	// sizes apply to tiled pages in document order.
	baseRawW, ok := pages[0].ImageWidth()
	if !ok {
		return nil, fmt.Errorf("philips: base page missing ImageWidth")
	}
	baseRawH, ok := pages[0].ImageLength()
	if !ok {
		return nil, fmt.Errorf("philips: base page missing ImageLength")
	}
	correctedSizes, err := computeCorrectedSizes(desc, int(baseRawW), int(baseRawH))
	if err != nil {
		return nil, err
	}

	// Classify pages into Levels / Macro / Label / Thumbnail.
	metas := make([]philipsPageMeta, len(pages))
	for i, p := range pages {
		_, tiled := p.TileWidth()
		d, _ := p.ImageDescription()
		metas[i] = philipsPageMeta{Tiled: tiled, Description: d}
	}
	class, err := classifyPages(metas)
	if err != nil {
		return nil, err
	}

	// baseSize and baseMPP for the pyramid: corrected page-0 dims and
	// the DICOM_PIXEL_SPACING-derived microns/pixel.
	if len(correctedSizes) == 0 {
		return nil, fmt.Errorf("philips: no corrected pyramid sizes (need ≥2 DICOM_PIXEL_SPACING entries)")
	}
	baseSize := opentile.Size{
		W: correctedSizes[0][0],
		H: correctedSizes[0][1],
	}
	baseMPP := opentile.SizeMm{
		W: md.PixelSpacing[0] * 1000.0, // mm → microns
		H: md.PixelSpacing[1] * 1000.0,
	}

	tiledLevels := make([]*tiledImage, 0, len(class.Levels))
	valueLevels := make([]opentile.Level, 0, len(class.Levels))
	var dirSpecs []philipsDirSpec
	seenPages := make(map[int]bool)
	for k, pageIdx := range class.Levels {
		var levelSize opentile.Size
		if k < len(correctedSizes) {
			levelSize = opentile.Size{W: correctedSizes[k][0], H: correctedSizes[k][1]}
		} else {
			// More tiled pages than PS entries → fall back to on-disk dims.
			iw, _ := pages[pageIdx].ImageWidth()
			il, _ := pages[pageIdx].ImageLength()
			levelSize = opentile.Size{W: int(iw), H: int(il)}
		}
		lvl, err := newTiledImage(k, pages[pageIdx], levelSize, baseSize, baseMPP, file.ReaderAt(), cfg)
		if err != nil {
			return nil, fmt.Errorf("philips: page %d (level %d): %w", pageIdx, k, err)
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
		dirSpecs = append(dirSpecs, philipsDirSpec{pageIdx: pageIdx, typ: opentile.DirLevel, level: k})
		seenPages[pageIdx] = true
	}

	// Associated images: emit in upstream's accessor order — thumbnail,
	// label, overview (Philips's "Macro"). Any of the three may be
	// absent; absent types are simply not emitted.
	var associated []opentile.AssociatedImage
	for _, spec := range []struct {
		imageType opentile.AssociatedType
		pageIdx   int
	}{
		{opentile.AssociatedThumbnail, class.Thumbnail},
		{opentile.AssociatedLabel, class.Label},
		{opentile.AssociatedOverview, class.Macro},
	} {
		if spec.pageIdx < 0 {
			continue
		}
		a, err := newAssociatedImage(spec.imageType, pages[spec.pageIdx], file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("philips: associated %s (page %d): %w", spec.imageType, spec.pageIdx, err)
		}
		associated = append(associated, a)
		dirSpecs = append(dirSpecs, philipsDirSpec{pageIdx: spec.pageIdx, typ: opentile.DirAssociated, assoc: spec.imageType})
		seenPages[spec.pageIdx] = true
	}
	// Capture orphan pages (IFDs not surfaced as a level or associated image).
	for i := range pages {
		if !seenPages[i] {
			dirSpecs = append(dirSpecs, philipsDirSpec{pageIdx: i, typ: opentile.DirOther})
		}
	}

	icc, _ := pages[0].ICCProfile()
	images := []opentile.Pyramid{{
		Name:   "",
		Index:  0,
		Levels: valueLevels,
	}}
	return &tiler{
		md:          md,
		tiledLevels: tiledLevels,
		images:      images,
		associated:  associated,
		icc:         icc,
		baseSize:    baseSize,
		baseMPP:     baseMPP,
		file:        file,
		dirSpecs:    dirSpecs,
	}, nil
}

// philipsDirSpec captures the physical page index and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
type philipsDirSpec struct {
	pageIdx int
	typ     opentile.DirectoryType
	level   int    // valid when typ==DirLevel
	assoc   opentile.AssociatedType // valid when typ==DirAssociated; matches AssociatedImage.Type()
}

// tiler is the Philips implementation of format.Reader.
type tiler struct {
	md          Metadata
	tiledLevels []*tiledImage
	images      []opentile.Pyramid
	associated  []opentile.AssociatedImage
	icc         []byte
	baseSize    opentile.Size
	baseMPP     opentile.SizeMm
	file        *tiff.File       // retained for lazy TIFF-tag exposure
	dirSpecs    []philipsDirSpec // page→role mapping captured at Open
}

func (t *tiler) Format() opentile.Format                  { return opentile.FormatPhilipsTIFF }
func (t *tiler) Pyramids() []opentile.Pyramid           { return t.images }
func (t *tiler) AssociatedImages() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }

// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider.
func (t *tiler) TIFFDirectories() []opentile.TIFFDirectory {
	if t.file == nil {
		return nil
	}
	pages := t.file.Pages()
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.pageIdx < 0 || ds.pageIdx >= len(pages) {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Type:           ds.typ,
			Image:          0, // Philips TIFF is single-image
			Level:          ds.level,
			AssociatedType: ds.assoc,
			Tags:           opentile.TIFFTagsFromPage(pages[ds.pageIdx]),
		})
	}
	return out
}

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return opentile.ErrLevelOutOfRange
	}
	return t.tiledLevels[level].warm()
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.tiledLevels[level].Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.tiledLevels[level].TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.tiledLevels) {
		return 0
	}
	return t.tiledLevels[level].TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.tiledLevels) {
		return nil
	}
	return t.tiledLevels[level].TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.tiledLevels) {
		return 0
	}
	return t.tiledLevels[level].TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.tiledLevels[level].TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.tiledLevels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.tiledLevels[level].TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.tiledLevels) {
		return func(yield func(opentile.TilePos, opentile.TileResult) bool) {}
	}
	return t.tiledLevels[level].Tiles(ctx)
}

const maxUnwrapHops = 16

// MetadataOf returns the Philips-specific metadata if v is (or wraps) a Philips
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
//
//	if md, ok := philipstiff.MetadataOf(slide); ok {
//	    fmt.Println(md.PixelSpacing, md.BitsAllocated)
//	}
func MetadataOf(v any) (*Metadata, bool) {
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if pt, ok := v.(*tiler); ok {
			return &pt.md, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}
