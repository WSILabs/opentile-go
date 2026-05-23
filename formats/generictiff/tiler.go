package generictiff

import (
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

// tilerUnwrapper is the unexported wrapper interface implemented by
// *fileCloser (returned by opentile.OpenFile). Mirrors the pattern
// used in svs/bif/ome/philips/ndpi.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

// maxTilerUnwrapHops caps the unwrap walk in [MetadataOf]. The
// realistic chain length is 1 (just *fileCloser); 16 is ample
// headroom while still preventing infinite loops on a wrapper that
// cycles.
const maxTilerUnwrapHops = 16

// MetadataOf returns the generic-TIFF format-specific metadata if t
// is a generic Tiler, otherwise (nil, false). Walks any number of
// wrappers (e.g., the *fileCloser returned by opentile.OpenFile)
// before asserting on the concrete type.
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if gt, ok := t.(*tiler); ok {
			return &gt.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}

// tiler is the generic-TIFF implementation of opentile.Tiler.
type tiler struct {
	md         Metadata
	levels     []opentile.Level
	associated []opentile.AssociatedImage
	icc        []byte
}

func (t *tiler) Format() opentile.Format { return opentile.FormatGenericTIFF }
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
