package generictiff

import (
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Metadata is the generic-TIFF format-specific metadata. Embeds
// opentile.Metadata so the cross-format fields (ScannerManufacturer,
// ScannerModel, ScannerSoftware, AcquisitionDateTime) populate via
// the embedded struct; the v0.10 spec §7 generic-only fields
// (MicronsPerPixel, ImageDescription) live on the outer struct.
//
// Read via [MetadataOf]:
//
//	if md, ok := generic.MetadataOf(tiler); ok {
//	    fmt.Println(md.MicronsPerPixel, md.ImageDescription)
//	}
//
// Magnification is always 0: generic TIFFs don't carry magnification
// in any standard TIFF tag, and we don't synthesise one. Derive from
// MicronsPerPixel if needed (e.g., 0.25 µm/px ≈ 40× on a typical
// pathology scanner — but that's caller policy, not slide truth).
type Metadata struct {
	opentile.Metadata

	// MicronsPerPixel is the level-0 X-axis pixel spacing derived
	// from XResolution (282) + ResolutionUnit (296). Set to 0 when
	// either tag is missing or ResolutionUnit is 1 (no unit).
	// ResolutionUnit values: 1=none → 0; 2=inch → 25400/res µm;
	// 3=cm → 10000/res µm.
	//
	// Generic TIFFs are typically isotropic so we surface a single
	// scalar rather than X/Y. Callers who care about anisotropy can
	// still read XResolution / YResolution off the page directly.
	MicronsPerPixel float64

	// ImageDescription is the level-0 IFD's ImageDescription tag
	// (270) verbatim. Generic-TIFF encoders may stash arbitrary
	// free-form text here; the reader doesn't try to parse it.
	ImageDescription string
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
//	XResolution (282)  → MicronsPerPixel (via ResolutionUnit)
//	ResolutionUnit (296)
//	ImageDescription (270) → ImageDescription verbatim
//
// Magnification has no standard TIFF tag → always 0.
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
	}
	if v, ok := p.ASCII(tiff.TagDateTime); ok {
		if ts, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(v)); err == nil {
			md.AcquisitionDateTime = ts
		}
	}
	if v, ok := p.ImageDescription(); ok {
		md.ImageDescription = strings.TrimSpace(v)
	}
	md.MicronsPerPixel = micronsPerPixel(p)
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

// micronsPerPixel computes µm/px from the page's XResolution +
// ResolutionUnit. Returns 0 on missing tags or ResolutionUnit=1
// (no unit). Spec §7 conversion factors:
//
//	ResolutionUnit=2 (inch) → 25400 µm/inch / pixels-per-inch
//	ResolutionUnit=3 (cm)   → 10000 µm/cm   / pixels-per-cm
func micronsPerPixel(p *tiff.Page) float64 {
	num, den, ok := p.XResolution()
	if !ok || num == 0 || den == 0 {
		return 0
	}
	unit, ok := p.ResolutionUnit()
	if !ok {
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
