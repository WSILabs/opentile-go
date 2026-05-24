package philipstiff

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
	// TODO(v0.23): remove old opentile.Register once tiler.go deletion lands.
	opentile.Register(&Factory{})
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

	levels := make([]opentile.Level, 0, len(class.Levels))
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
		levels = append(levels, lvl)
	}

	// Associated images: emit in upstream's accessor order — thumbnail,
	// label, overview (Philips's "Macro"). Any of the three may be
	// absent; absent kinds are simply not emitted.
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
			return nil, fmt.Errorf("philips: associated %s (page %d): %w", spec.kind, spec.pageIdx, err)
		}
		associated = append(associated, a)
	}

	icc, _ := pages[0].ICCProfile()
	return &tiler{
		md:         md,
		levels:     levels,
		associated: associated,
		icc:        icc,
		baseSize:   baseSize,
		baseMPP:    baseMPP,
	}, nil
}

// Factory is the FormatFactory implementation for Philips TIFF.
type Factory struct{ opentile.RawUnsupported }

// New returns a Philips factory. Safe to call once and register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatPhilipsTIFF }

// Supports reports whether file looks like a Philips TIFF: its first
// page's Software tag starts with "Philips DP" AND its ImageDescription
// tag ends in </DataObject> (after stripping trailing whitespace).
//
// Direct port of tifffile's `is_philips` predicate
// (tifffile.py:10267-10271). Both signals are required — either alone
// has too high a false-positive rate.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	sw, ok := pages[0].Software()
	if !ok || !strings.HasPrefix(sw, philipsSoftwarePrefix) {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(desc), philipsDescriptionSuffix)
}

// Open constructs a Philips Tiler from a parsed TIFF file.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	fcfg := opentileConfigToFormatConfig(cfg)
	return openFromTIFFFile(file, fcfg)
}

// opentileConfigToFormatConfig translates the opaque opentile.Config wrapper
// into a format.Config. Called from Factory.Open during the dual-registration
// transition; the new openPhilips path receives *format.Config directly.
func opentileConfigToFormatConfig(cfg *opentile.Config) *format.Config {
	if cfg == nil {
		return &format.Config{}
	}
	ts, hasTS := cfg.TileSize()
	return &format.Config{
		TileSize:             ts,
		HasTileSize:          hasTS,
		CorruptTilePolicy:    cfg.CorruptTilePolicy(),
		NDPISynthesizedLabel: cfg.NDPISynthesizedLabel(),
		Backing:              cfg.Backing(),
	}
}

// tiler is the Philips implementation of opentile.Tiler.
type tiler struct {
	md         Metadata
	levels     []opentile.Level
	associated []opentile.AssociatedImage
	icc        []byte
	baseSize   opentile.Size
	baseMPP    opentile.SizeMm
}

func (t *tiler) Format() opentile.Format { return opentile.FormatPhilipsTIFF }
func (t *tiler) Images() []opentile.Image {
	return []opentile.Image{opentile.NewSingleImage(t.levels)}
}
func (t *tiler) Levels() []opentile.Level {
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

// tilerUnwrapper is the same coordination interface SVS / NDPI use to
// peel off opentile's *fileCloser wrapper before MetadataOf can type-
// assert on the concrete philipstiff.tiler.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

const maxTilerUnwrapHops = 16

// MetadataOf returns the Philips-specific metadata if t is a Philips
// Tiler, otherwise (nil, false). Walks any number of wrappers before
// asserting on the concrete type.
//
//	if md, ok := philipstiff.MetadataOf(tiler); ok {
//	    fmt.Println(md.PixelSpacing, md.BitsAllocated)
//	}
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if pt, ok := t.(*tiler); ok {
			return &pt.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}
