package ometiff

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
)

// OMEImage is one entry in the OME-XML <Image> list. Carries the
// fields opentile-go needs from each Image's <Pixels> child:
// classification anchor (Name), physical pixel size (microns/pixel),
// pixel-array dimensions, and pixel type.
//
// Fields absent in the XML stay at their zero values — the parser is
// tolerant so callers can branch on (X == 0) for "unknown".
type OMEImage struct {
	Name string

	// Description is the <Description> child element's text content.
	// Empty when absent. Surfaced verbatim into the cross-format
	// opentile.Metadata.ImageDescription for the primary main-pyramid
	// image (v0.17).
	Description string

	// AcquisitionDate is the <AcquisitionDate> child element's text
	// content (ISO 8601 — e.g., "2011-05-31T09:43:06.873"). Empty
	// when absent. Parsed into a time.Time at the cross-format
	// Metadata layer (v0.17).
	AcquisitionDate string

	// ObjectiveSettingsID is the <ObjectiveSettings ID> attribute's
	// value, identifying which <Objective> from the file's
	// <Instrument> applies to this Image. Empty when absent.
	// Resolved against OMEMetadata.Objectives (by ID) at the
	// cross-format Metadata layer to populate Magnification (v0.17).
	ObjectiveSettingsID string

	PhysicalSizeX     float64
	PhysicalSizeY     float64
	PhysicalSizeXUnit string
	PhysicalSizeYUnit string

	SizeX int
	SizeY int

	// SizeZ / SizeC / SizeT from <Pixels>. Surfaced verbatim from
	// the XML for forward-compat with multi-Z / fluorescence /
	// time-series OME files. v0.7 NOTE: <Pixels SizeC> describes
	// per-pixel sample count (e.g., 3 for RGB), NOT the count of
	// separately-stored channels — `Channels` (below) is the right
	// discriminator for Image.SizeC(). Both Leica fixtures report
	// SizeZ=1, SizeC=3 (RGB sample count), SizeT=1.
	SizeZ int
	SizeC int
	SizeT int

	// Channels is the count of <Channel> elements within this
	// <Image>. The right discriminator for Image.SizeC() — 1 on
	// brightfield slides (one composite RGB channel per pixel; the
	// underlying tile bytes are a single composite JPEG), > 1 on
	// fluorescence imaging (each <Channel> is a separately-stored
	// grayscale plane). v0.7 surfaces Channels via the public
	// Image.SizeC() accessor; <Pixels SizeC> is captured for
	// completeness but not exposed publicly.
	Channels int

	// ChannelNames mirrors each <Channel Name> attribute. Used by
	// Image.ChannelName(c). Length == Channels; entries default to
	// "" when the attribute is absent (which is the case on every
	// Leica fixture). Future fluorescence support populates real
	// names like "DAPI", "FITC", "TRITC".
	ChannelNames []string

	Type string
}

// OMEObjective mirrors a <Objective> element under <Instrument>.
// The cross-format Metadata layer resolves an Image's
// ObjectiveSettingsID against this list (by ID) to populate
// Magnification (v0.17).
type OMEObjective struct {
	ID                      string
	NominalMagnification    float64
	CalibratedMagnification float64
	LensNA                  float64
}

// OMEMetadata is the top-level parsed view of an OME-XML document.
// Holds the Image list in document order; further interpretation
// (classification of macro / label / thumbnail vs main pyramid) is
// done in formats/ometiff/series.go.
type OMEMetadata struct {
	// Creator mirrors the <OME Creator> root attribute. Identifies
	// the writer that produced the file (e.g., "OME Bio-Formats
	// 6.0.0-rc1"). Surfaced as ome.creator in cross-format
	// Properties (v0.17).
	Creator string

	// UUID mirrors the <OME UUID> root attribute. Surfaced as
	// ome.uuid in cross-format Properties (v0.17).
	UUID string

	// Objectives is every <Objective> under every <Instrument>,
	// flattened in document order. Used by the Magnification
	// resolver (Image.ObjectiveSettingsID → Objective.ID) at the
	// cross-format Metadata layer (v0.17).
	Objectives []OMEObjective

	Images []OMEImage
}

// parseOMEMetadata parses an OME-XML document — the page-0
// ImageDescription of an OME TIFF file. Returns the per-Image
// inventory needed for series classification + per-level MPP.
//
// Direct port of upstream's `ome_types.from_xml(metadata)` for the
// subset of OME-XML attributes opentile-go cares about (Image Name +
// Pixels PhysicalSize / Size / Type). We deliberately ignore the
// other ~30 OME-XML elements; surfacing them is out of scope for
// v0.6 (matches upstream's narrow `Metadata()` return).
//
// Namespace-agnostic: encoding/xml struct tags don't qualify by
// namespace, so OME schemas at any version
// (2015-01, 2016-06, etc.) parse uniformly.
func parseOMEMetadata(xmlStr string) (OMEMetadata, error) {
	var doc omeDoc
	if err := xml.NewDecoder(strings.NewReader(xmlStr)).Decode(&doc); err != nil {
		return OMEMetadata{}, fmt.Errorf("ome: parse OME-XML: %w", err)
	}
	if len(doc.Images) == 0 {
		return OMEMetadata{}, fmt.Errorf("ome: OME document carries zero <Image> elements")
	}
	out := OMEMetadata{
		Creator: doc.Creator,
		UUID:    doc.UUID,
		Images:  make([]OMEImage, 0, len(doc.Images)),
	}
	for _, inst := range doc.Instruments {
		for _, obj := range inst.Objectives {
			out.Objectives = append(out.Objectives, OMEObjective{
				ID:                      obj.ID,
				NominalMagnification:    obj.NominalMagnification,
				CalibratedMagnification: obj.CalibratedMagnification,
				LensNA:                  obj.LensNA,
			})
		}
	}
	for _, im := range doc.Images {
		channelNames := make([]string, len(im.Pixels.Channels))
		for i, ch := range im.Pixels.Channels {
			channelNames[i] = ch.Name
		}
		out.Images = append(out.Images, OMEImage{
			Name:                im.Name,
			Description:         strings.TrimSpace(im.Description),
			AcquisitionDate:     strings.TrimSpace(im.AcquisitionDate),
			ObjectiveSettingsID: im.ObjectiveSettings.ID,
			PhysicalSizeX:       im.Pixels.PhysicalSizeX,
			PhysicalSizeY:       im.Pixels.PhysicalSizeY,
			PhysicalSizeXUnit:   im.Pixels.PhysicalSizeXUnit,
			PhysicalSizeYUnit:   im.Pixels.PhysicalSizeYUnit,
			SizeX:               im.Pixels.SizeX,
			SizeY:               im.Pixels.SizeY,
			SizeZ:               im.Pixels.SizeZ,
			SizeC:               im.Pixels.SizeC,
			SizeT:               im.Pixels.SizeT,
			Channels:            len(im.Pixels.Channels),
			ChannelNames:        channelNames,
			Type:                im.Pixels.Type,
		})
	}
	return out, nil
}

// omeDoc / omeImage / omePixels are private XML-decoding shapes. The
// public structs (OMEMetadata / OMEImage) carry the merged view
// callers consume.
type omeDoc struct {
	XMLName     xml.Name        `xml:"OME"`
	Creator     string          `xml:"Creator,attr"`
	UUID        string          `xml:"UUID,attr"`
	Instruments []omeInstrument `xml:"Instrument"`
	Images      []omeImage      `xml:"Image"`
}

type omeInstrument struct {
	ID         string         `xml:"ID,attr"`
	Objectives []omeObjective `xml:"Objective"`
}

type omeObjective struct {
	ID                      string  `xml:"ID,attr"`
	NominalMagnification    float64 `xml:"NominalMagnification,attr"`
	CalibratedMagnification float64 `xml:"CalibratedMagnification,attr"`
	LensNA                  float64 `xml:"LensNA,attr"`
}

type omeImage struct {
	Name              string               `xml:"Name,attr"`
	AcquisitionDate   string               `xml:"AcquisitionDate"`
	Description       string               `xml:"Description"`
	ObjectiveSettings omeObjectiveSettings `xml:"ObjectiveSettings"`
	Pixels            omePixels            `xml:"Pixels"`
}

type omeObjectiveSettings struct {
	ID string `xml:"ID,attr"`
}

type omePixels struct {
	PhysicalSizeX     float64      `xml:"PhysicalSizeX,attr"`
	PhysicalSizeY     float64      `xml:"PhysicalSizeY,attr"`
	PhysicalSizeXUnit string       `xml:"PhysicalSizeXUnit,attr"`
	PhysicalSizeYUnit string       `xml:"PhysicalSizeYUnit,attr"`
	SizeX             int          `xml:"SizeX,attr"`
	SizeY             int          `xml:"SizeY,attr"`
	SizeZ             int          `xml:"SizeZ,attr"`
	SizeC             int          `xml:"SizeC,attr"`
	SizeT             int          `xml:"SizeT,attr"`
	Channels          []omeChannel `xml:"Channel"`
	Type              string       `xml:"Type,attr"`
}

// omeChannel captures the bits of <Channel> opentile-go uses today —
// just Name (for Image.ChannelName(c)). Future fluorescence work
// can extend with Color / ExcitationWavelength / EmissionWavelength
// / Fluor without breaking the parser.
type omeChannel struct {
	Name string `xml:"Name,attr"`
}

// crossMetadata builds a cross-format opentile.Metadata view of the
// parsed OME-XML using the primary main-pyramid image identified by
// classify (LevelImages[0]). When the OME document carries multiple
// main pyramids (Leica-2.ome.tiff has 4), only the first contributes
// to cross.MicronsPerPixelX/Y / ImageDescription / AcquisitionDateTime
// / Magnification — consumers needing per-image metadata read the
// raw OMEMetadata.Images slice via ometiff.MetadataOf.
//
// Fields populated:
//   - MicronsPerPixelX/Y from <Pixels PhysicalSizeX/Y> + Unit (with
//     unit conversion via convertToMicrons); SetMPPSymmetric collapses
//     to the symmetric MicronsPerPixel slot when X == Y.
//   - ImageDescription from <Image><Description> (verbatim).
//   - AcquisitionDateTime from <Image><AcquisitionDate> (ISO 8601 —
//     RFC3339 with optional sub-seconds).
//   - Magnification from the resolved <ObjectiveSettings>→<Objective>
//     NominalMagnification (preferred) or CalibratedMagnification
//     (fallback when NominalMagnification is zero / absent).
//   - Properties[ome.creator] from <OME Creator> root attribute.
//   - Properties[ome.uuid] from <OME UUID> root attribute.
//
// Fields NOT populated (the OME fixtures we have don't carry them
// in the parsed form, and adding them would require parsing
// StructuredAnnotations OriginalMetadata which is a separate effort):
//   - ScannerManufacturer / ScannerModel / ScannerSerial — Leica
//     fixtures stash these in StructuredAnnotations OriginalMetadata
//     keys like "macro device.model for image" rather than the
//     typed <Microscope> element.
//   - ScannerSoftware — same StructuredAnnotations source.
//   - PropertyUserName — neither Leica fixture carries an
//     <Experimenter> element. Code path is in place but inactive
//     until a fixture surfaces one.
func crossMetadata(om OMEMetadata, cls omeClassification) opentile.Metadata {
	var md opentile.Metadata

	// OME root-level passthrough (always present in well-formed
	// OME-XML files written by Bio-Formats / OME tools).
	if om.Creator != "" {
		md.SetProperty("ome.creator", om.Creator)
		md.Writer = om.Creator // v0.20: promote OME Creator to typed Writer field
	}
	if om.UUID != "" {
		md.SetProperty("ome.uuid", om.UUID)
	}

	if len(cls.LevelImages) == 0 {
		// Unreachable in the live Open() path (classifyImages errors
		// out earlier with errNoLevelImages) but defensive against
		// direct callers building Metadata from a bare classification.
		return md
	}
	primary := om.Images[cls.LevelImages[0]]

	// Per-axis MPP from <Pixels PhysicalSizeX/Y>. OME-TIFF convention
	// is microns when the Unit attribute is absent or "µm" / "um";
	// convertToMicrons handles "nm" / "mm" too.
	if primary.PhysicalSizeX > 0 {
		md.MicronsPerPixelX = convertToMicrons(primary.PhysicalSizeX, primary.PhysicalSizeXUnit)
	}
	if primary.PhysicalSizeY > 0 {
		md.MicronsPerPixelY = convertToMicrons(primary.PhysicalSizeY, primary.PhysicalSizeYUnit)
	}
	md.SetMPPSymmetric()

	// Structured description.
	if primary.Description != "" {
		md.ImageDescription = primary.Description
	}

	// AcquisitionDate: OME stores ISO 8601, e.g.
	// "2011-05-31T09:43:06.873" (no timezone) or
	// "2014-11-26T14:09:07.390Z" (with Z). time.Parse with RFC3339Nano
	// handles the Z suffix; for the no-timezone form we fall back to
	// a trailing-Z layout.
	if primary.AcquisitionDate != "" {
		if t, ok := parseOMETime(primary.AcquisitionDate); ok {
			md.AcquisitionDateTime = t
		}
	}

	// Magnification: resolve <ObjectiveSettings ID> against the
	// flattened Objectives list. Prefer NominalMagnification (the
	// rated objective magnification), fall back to
	// CalibratedMagnification (the actual measured value, useful for
	// e.g. macro objectives like 0.60833× that aren't a standard
	// nominal class).
	if primary.ObjectiveSettingsID != "" {
		for _, o := range om.Objectives {
			if o.ID != primary.ObjectiveSettingsID {
				continue
			}
			switch {
			case o.NominalMagnification > 0:
				md.Magnification = o.NominalMagnification
			case o.CalibratedMagnification > 0:
				md.Magnification = o.CalibratedMagnification
			}
			break
		}
	}

	return md
}

// convertToMicrons normalises a PhysicalSize value + Unit attribute to
// microns/pixel. OME-TIFF convention treats a missing Unit attribute
// as "µm", so absent / blank / unrecognised units pass through
// unchanged (which is the right default for the vast majority of
// OME-TIFF files).
//
// The "µ" character below is U+00B5 (MICRO SIGN); some writers use
// U+03BC (GREEK SMALL LETTER MU) instead. We handle both, plus the
// ASCII "u" alias and the long-form spellings, since OME-XML files in
// the wild are inconsistent here.
func convertToMicrons(v float64, unit string) float64 {
	if v == 0 {
		return 0
	}
	switch strings.TrimSpace(unit) {
	case "", "µm", "μm", "um", "micrometer", "micrometre", "microns":
		return v
	case "nm", "nanometer", "nanometre":
		return v / 1000.0
	case "mm", "millimeter", "millimetre":
		return v * 1000.0
	case "cm", "centimeter", "centimetre":
		return v * 10000.0
	case "m", "meter", "metre":
		return v * 1_000_000.0
	default:
		// Unknown unit — fall back to OME default (µm) rather than
		// returning zero; the alternative drops valid metadata when
		// an exotic unit slips through.
		return v
	}
}

// omeTimeLayouts lists the timestamp formats we accept for
// <AcquisitionDate>. OME-XML schema specifies xsd:dateTime, but
// real-world writers omit the timezone (Bio-Formats does this for
// the Leica fixtures in our suite — "2011-05-31T09:43:06.873" with
// no Z and no offset). RFC3339 covers the spec-compliant form;
// the bare-local layout covers the common Bio-Formats output.
var omeTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999",
	"2006-01-02T15:04:05",
}

// parseOMETime tries each known OME timestamp layout and returns the
// first successful parse. Mirrors the lenient parsing strategy used
// elsewhere in opentile-go (Philips's philipsTimeLayout is single-
// shot because Philips writes a single canonical form; OME's writer
// fleet is more diverse).
func parseOMETime(s string) (time.Time, bool) {
	for _, layout := range omeTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
