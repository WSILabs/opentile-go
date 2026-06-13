package generictiff

import (
	"context"
	"io"
	"iter"
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Metadata is the generic-TIFF format-specific metadata. The shape is
// purely the embedded cross-format opentile.Metadata: as of v0.17 the
// previously-outer fields (MicronsPerPixel, ImageDescription) moved to
// the cross-format struct and are accessed via field promotion.
//
// Read via [MetadataOf]:
//
//	if md, ok := generic.MetadataOf(tiler); ok {
//	    fmt.Println(md.MicronsPerPixel, md.MicronsPerPixelX, md.ImageDescription)
//	}
//
// Magnification is always 0 unless the wsi-tools ImageDescription
// extension supplies one: generic TIFFs don't carry magnification in
// any standard TIFF tag and we don't synthesise one. Derive from
// MicronsPerPixel if needed (e.g., 0.25 µm/px ≈ 40× on a typical
// pathology scanner — but that's caller policy, not slide truth).
//
// MicronsPerPixel is set when level-0 XResolution + ResolutionUnit are
// both present and ResolutionUnit ∈ {2 (inch), 3 (cm)}; isotropy is
// inferred from a separate YResolution read (when present and equal,
// MicronsPerPixel == X == Y per [opentile.Metadata.SetMPPSymmetric]).
// Callers reading the per-axis fields directly can detect anisotropy.
type Metadata struct {
	opentile.Metadata
}

// MetadataOf returns the generic-TIFF format-specific metadata if v is (or
// wraps) a generic-TIFF reader, otherwise (nil, false). Accepts *opentile.Slide,
// format.Reader implementations, and any type implementing UnwrapReader() any.
func MetadataOf(v any) (*Metadata, bool) {
	const maxUnwrapHops = 16
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if gt, ok := v.(*tiler); ok {
			return &gt.md, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}

// genericDirSpec captures the physical page index and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
type genericDirSpec struct {
	pageIdx int
	typ     opentile.DirectoryType
	level   int    // valid when typ==DirLevel
	assoc   opentile.AssociatedType // valid when typ==DirAssociated; matches AssociatedImage.Type()
}

// tiler is the generic-TIFF implementation of format.Reader.
type tiler struct {
	md          Metadata
	tiledLevels []*tiledImage
	images      []opentile.Pyramid
	associated  []opentile.AssociatedImage
	icc         []byte
	file        *tiff.File       // retained for lazy TIFF-tag exposure
	dirSpecs    []genericDirSpec // page→role mapping captured at Open
}

func (t *tiler) Format() opentile.Format                  { return opentile.FormatGenericTIFF }
func (t *tiler) Pyramids() []opentile.Pyramid             { return t.images }
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
			Image:          0, // generic-TIFF is single-image
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
	if t.file == nil {
		return 0, false
	}
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

// buildMetadata reads the cross-format + generic-specific metadata
// from the level-0 IFD's standard TIFF tags. Per spec §7:
//
//	Make (271)         → ScannerManufacturer
//	Model (272)        → ScannerModel
//	Software (305)     → ScannerSoftware (semicolon/newline-split)
//	DateTime (306)     → AcquisitionDateTime (TIFF "YYYY:MM:DD HH:MM:SS")
//	XResolution (282)  → MicronsPerPixelX (via ResolutionUnit)
//	YResolution (283)  → MicronsPerPixelY (via ResolutionUnit)
//	ResolutionUnit (296)
//	ImageDescription (270) → cross.ImageDescription verbatim
//
// Magnification has no standard TIFF tag → always 0 unless overridden
// below.
//
// v0.14 addition: when ImageDescription begins with `wsi-tools/`, the
// wsi-tools parser populates Magnification / ScannerManufacturer /
// AcquisitionDateTime / MicronsPerPixelX/Y from the parsed fields,
// overriding any standard-TIFF-tag-derived values. wsi-tools fixtures
// also surface source/codec/version under Properties under the
// "wsi-tools." namespace.
//
// v0.17: per-axis MPP is now populated; SetMPPSymmetric() then
// populates the scalar MicronsPerPixel slot only when X == Y.
func buildMetadata(p *tiff.Page) Metadata {
	var md Metadata
	if v, ok := p.ASCII(tagMake); ok {
		md.ScannerManufacturer = strings.TrimSpace(v)
	}
	if v, ok := p.ASCII(tagModel); ok {
		md.ScannerModel = strings.TrimSpace(v)
	}
	if v, ok := p.Software(); ok {
		md.ScannerSoftware = splitSoftware(v)
		md.Writer = v // v0.20: raw Software string (may be overridden by wsi-tools path below)
	}
	if v, ok := p.ASCII(tiff.TagDateTime); ok {
		if ts, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(v)); err == nil {
			md.AcquisitionDateTime = ts
		}
	}
	if v, ok := p.ImageDescription(); ok {
		md.ImageDescription = strings.TrimSpace(v)
	}
	mppX, mppY := perAxisMicronsPerPixel(p)
	md.MicronsPerPixelX = mppX
	md.MicronsPerPixelY = mppY

	// v0.14: wsi-tools ImageDescription override.
	if md.ImageDescription != "" {
		if wt, ok := parseWSIToolsDescription(md.ImageDescription); ok {
			if wt.hasMag {
				md.Magnification = wt.magnification
			}
			if wt.hasScanner {
				md.ScannerManufacturer = wt.scannerManufacturer
			}
			if wt.hasDate {
				md.AcquisitionDateTime = wt.acquisitionDate
			}
			if wt.hasMPP {
				// wsi-tools mpp is a scalar; treat as isotropic.
				md.MicronsPerPixelX = wt.micronsPerPixel
				md.MicronsPerPixelY = wt.micronsPerPixel
			}
			// v0.17: wsi-tools-only provenance fields surface under the
			// "wsi-tools." Properties namespace so consumers can detect
			// transcoded fixtures + recover source/codec/version without
			// reparsing the raw ImageDescription.
			populateWSIToolsProperties(&md, md.ImageDescription)
			// v0.20: wsi-tools is the file producer; override the Software-derived Writer.
			if wt.Version != "" {
				md.Writer = "wsitools/" + wt.Version
			}
		}
	}
	md.SetMPPSymmetric()
	return md
}

// tagMake / tagModel are the standard TIFF tags 271 / 272.
// internal/tiff doesn't currently export accessors for these;
// declared here so generic.go can read them via Page.ASCII.
const (
	tagMake  uint16 = 271
	tagModel uint16 = 272
)

// splitSoftware splits the Software tag value on common delimiters
// (semicolon, newline). Trims whitespace; drops empty fragments.
// A simple "Aperio ImageScope v12" stays as a single-element slice.
func splitSoftware(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, ";", "\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// perAxisMicronsPerPixel computes (X, Y) µm/px from the page's
// XResolution + YResolution + ResolutionUnit. Returns 0 on either axis
// when its rational is malformed or ResolutionUnit is missing /
// ResolutionUnit=1 (no unit). When YResolution is missing but
// XResolution is present, Y mirrors X (most generic-TIFF fixtures are
// isotropic and only emit one of the two tags). Spec §7 conversion
// factors:
//
//	ResolutionUnit=2 (inch) → 25400 µm/inch / pixels-per-unit
//	ResolutionUnit=3 (cm)   → 10000 µm/cm   / pixels-per-unit
func perAxisMicronsPerPixel(p *tiff.Page) (x, y float64) {
	unit, ok := p.ResolutionUnit()
	if !ok {
		return 0, 0
	}
	convert := func(num, den uint32) float64 {
		if num == 0 || den == 0 {
			return 0
		}
		pixelsPerUnit := float64(num) / float64(den)
		if pixelsPerUnit == 0 {
			return 0
		}
		switch unit {
		case 2: // inch
			return 25400.0 / pixelsPerUnit
		case 3: // centimeter
			return 10000.0 / pixelsPerUnit
		default:
			return 0
		}
	}
	if num, den, ok := p.XResolution(); ok {
		x = convert(num, den)
	}
	if num, den, ok := p.YResolution(); ok {
		y = convert(num, den)
	} else {
		// No explicit YResolution → assume isotropic (mirror X).
		y = x
	}
	return x, y
}
