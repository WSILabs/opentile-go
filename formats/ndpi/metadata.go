package ndpi

import (
	"strconv"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Metadata is NDPI-specific slide metadata. Embeds opentile.Metadata for the
// cross-format fields (Magnification, scanner info, AcquisitionDateTime).
type Metadata struct {
	opentile.Metadata
	SourceLens  float64 // objective magnification from tag 65421 (equivalent to Magnification)
	FocalOffset float64 // mm, from ZOffsetFromSlideCenter tag 65424 (nanometers → mm)
	Reference   string  // scanner serial, tag 65442
}

// NDPI vendor-private tag IDs (verified against cgohlke/tifffile NDPI_TAGS registry).
const (
	tagFileFormat             uint16 = 65420 // present iff NDPI; version marker
	tagMagnification          uint16 = 65421 // FLOAT; -1 = Macro, -2 = Map, >0 = source lens
	tagXOffsetFromSlideCenter uint16 = 65422 // SLONG
	tagYOffsetFromSlideCenter uint16 = 65423 // SLONG
	tagZOffsetFromSlideCenter uint16 = 65424 // SLONG (nanometers; focal plane)
	tagTissueIndex            uint16 = 65425
	tagSlideLabel             uint16 = 65427 // ASCII
	tagCaptureMode            uint16 = 65441
	tagReference              uint16 = 65442 // ScannerSerialNumber (ASCII)
)

// Standard TIFF tag IDs used by the NDPI metadata parser.
const (
	tagModel            uint16 = 272
	tagDateTime         uint16 = 306
	tagImageDescription uint16 = 270
)

// metadataFields is the un-marshaled shape consumed by parseFromFields.
// Production paths populate it from *tiff.Page via parseMetadata; tests
// construct it directly.
type metadataFields struct {
	Magnification          float32 // from tag 65421; may be negative for Macro/Map
	Model                  string
	DateTime               string
	ImageDescription       string
	XResolution            [2]uint32
	YResolution            [2]uint32
	ResolutionUnit         uint32
	ZOffsetFromSlideCenter int32 // SLONG, nanometers (note: signed)
	Reference              string
}

// parseMetadata reads NDPI metadata from the first TIFF page.
func parseMetadata(p *tiff.Page) (Metadata, error) {
	var f metadataFields
	if v, ok := p.Float32(tagMagnification); ok {
		f.Magnification = v
	}
	f.Model, _ = p.ASCII(tagModel)
	f.DateTime, _ = p.ASCII(tagDateTime)
	f.ImageDescription, _ = p.ASCII(tagImageDescription)
	if numer, denom, ok := p.XResolution(); ok {
		f.XResolution = [2]uint32{numer, denom}
	}
	if numer, denom, ok := p.YResolution(); ok {
		f.YResolution = [2]uint32{numer, denom}
	}
	if v, ok := p.ResolutionUnit(); ok {
		f.ResolutionUnit = v
	}
	// ZOffsetFromSlideCenter is SLONG (signed). Use the raw uint32 value
	// reinterpreted as int32.
	if v, ok := p.ScalarU32(tagZOffsetFromSlideCenter); ok {
		f.ZOffsetFromSlideCenter = int32(v)
	}
	f.Reference, _ = p.ASCII(tagReference)
	return parseFromFields(f), nil
}

// parseFromFields builds a Metadata from its un-marshaled tag values. Kept
// separate from parseMetadata so unit tests can construct metadata without
// needing a *tiff.Page.
func parseFromFields(f metadataFields) Metadata {
	md := Metadata{
		FocalOffset: float64(f.ZOffsetFromSlideCenter) / 1_000_000.0, // nm → mm
		Reference:   f.Reference,
	}
	// NDPI magnification may be negative for associated-image pages (-1 Macro,
	// -2 Map). For the pyramid-level metadata path we clamp to >=0, so a
	// negative value means "not a real magnification" and Magnification stays 0.
	if f.Magnification > 0 {
		md.Magnification = float64(f.Magnification)
		md.SourceLens = float64(f.Magnification)
	}
	md.ScannerManufacturer = "Hamamatsu"
	md.ScannerModel = f.Model
	if f.Model != "" {
		md.ScannerSoftware = []string{f.Model}
		md.Writer = f.Model // v0.20: NDPI's Model is the writer identifier
	}
	// Reference (tag 65442) carries the scanner serial number on real
	// Hamamatsu fixtures (e.g., OS-2.ndpi reports "477130"). Mirror it
	// into the cross-format ScannerSerial slot so consumers don't have
	// to type-assert into Metadata.
	if f.Reference != "" {
		md.ScannerSerial = f.Reference
	}
	if t, err := time.Parse("2006:01:02 15:04:05", f.DateTime); err == nil {
		md.AcquisitionDateTime = t
	}

	// Cross-format ImageDescription. NDPI fixtures usually leave this
	// empty (CMU-1, OS-2 both report ""), but the slot is always
	// populated from the TIFF tag if present.
	md.ImageDescription = f.ImageDescription

	// Cross-format MicronsPerPixel. NDPI carries pixel size in the
	// standard TIFF Resolution tags, with ResolutionUnit=3 (centimeters)
	// in every real fixture observed. MPP_um = 10000 / pixels_per_cm.
	if f.ResolutionUnit == 3 {
		if mpp, ok := mppFromRational(f.XResolution); ok {
			md.MicronsPerPixelX = mpp
		}
		if mpp, ok := mppFromRational(f.YResolution); ok {
			md.MicronsPerPixelY = mpp
		}
		md.SetMPPSymmetric()
	}

	// Vendor passthrough. Surface already-parsed Hamamatsu vendor data
	// under "hamamatsu." so consumers can read format-specific fields
	// without type-asserting back to ndpi.Metadata. We deliberately
	// surface only fields we already parse; richer NDPI vendor tag
	// coverage is deferred (not in scope for v0.17).
	if f.Reference != "" {
		md.SetProperty("hamamatsu.Reference", f.Reference)
	}
	if f.Magnification > 0 {
		md.SetProperty("hamamatsu.SourceLens",
			strconv.FormatFloat(float64(f.Magnification), 'f', -1, 64))
	}
	if f.ZOffsetFromSlideCenter != 0 {
		md.SetProperty("hamamatsu.FocalOffsetMM",
			strconv.FormatFloat(md.FocalOffset, 'f', -1, 64))
	}
	return md
}

// mppFromRational converts a TIFF RATIONAL resolution (numer/denom pixels
// per centimeter) into microns per pixel. Returns ok=false if the
// rational is malformed (zero denominator or zero numerator).
func mppFromRational(r [2]uint32) (float64, bool) {
	if r[0] == 0 || r[1] == 0 {
		return 0, false
	}
	pixelsPerCm := float64(r[0]) / float64(r[1])
	if pixelsPerCm == 0 {
		return 0, false
	}
	// 10000 µm/cm → MPP = 10000 / (pixels/cm)
	return 10000.0 / pixelsPerCm, true
}

// MetadataOf returns the NDPI-specific metadata if t is an NDPI Tiler.
// Walks Tiler wrappers (mirrors svs.MetadataOf) to accommodate the
// fileCloser wrapper that opentile.OpenFile returns.
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	const maxHops = 16
	for i := 0; t != nil && i <= maxHops; i++ {
		if nt, ok := t.(*tiler); ok {
			return &nt.md, true
		}
		u, ok := t.(interface{ UnwrapTiler() opentile.Tiler })
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}
