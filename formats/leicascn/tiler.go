package leicascn

import (
	"fmt"
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
)

// Metadata is the Leica SCN-specific format metadata. Embeds
// opentile.Metadata so the cross-format fields (ScannerManufacturer,
// ScannerModel, etc.) populate via the embedded struct; the v0.11
// SCN-only fields live on the outer struct.
//
// Read via [MetadataOf]:
//
//	if md, ok := leicascn.MetadataOf(t); ok {
//	    fmt.Println(md.Barcode, md.Channels[0].ExcitationFilter)
//	}
type Metadata struct {
	opentile.Metadata

	// CollectionUUID is the <collection uuid="..."> attribute. May be
	// empty on malformed XML; non-empty on every shipped fixture.
	CollectionUUID string

	// Barcode is the slide barcode (base64-encoded) from the
	// <barcode> element under <collection>. May be empty.
	Barcode string

	// Auxiliaries carries one entry per auxiliary <image>
	// (one element with kind="macro" in the AssociatedImage list,
	// per sealed Q8). Order matches AssociatedImage iteration order.
	Auxiliaries []AuxiliaryInfo

	// Regions carries one entry per main scan <image> (each main
	// becomes a region in the composite Image's Levels). Order
	// matches the multi-region dispatch order in the composite.
	Regions []RegionInfo

	// Channels carries per-channel fluorescence metadata. Populated
	// only when SizeC > 1 (e.g., Leica-Fluorescence-1 has 3 channels);
	// nil for brightfield SCN files. Order matches the channel index
	// (Channels[i].Index == i).
	Channels []ChannelInfo
}

// AuxiliaryInfo carries the SCN XML metadata for one auxiliary
// <image> element. Surfaced via Metadata.Auxiliaries so consumers
// can disambiguate brightfield-macro vs fluorescence-macro on
// multi-auxiliary files (Leica-Fluorescence-1 has 2 auxiliaries).
type AuxiliaryInfo struct {
	Name               string
	IlluminationSource string  // "brightfield" or "fluorescence"
	Objective          float64 // microscope objective magnification
}

// RegionInfo carries the SCN XML metadata for one main scan
// <image> element. Surfaced via Metadata.Regions for consumers
// that want the per-region slide-physical layout (e.g., to render
// scale bars or position annotations).
type RegionInfo struct {
	Name               string
	OffsetXNm          uint64 // slide-physical X offset of this region
	OffsetYNm          uint64
	SizeXNm            uint64 // slide-physical extent of this region
	SizeYNm            uint64
	Objective          float64
	IlluminationSource string
}

// ChannelInfo carries the SCN XML <channel> metadata for one
// fluorescence channel. Populated only on multi-channel main scans
// (SizeC > 1).
type ChannelInfo struct {
	Index              int
	Name               string // e.g. "405|Empty"
	RGB                string // e.g. "#0000ff"
	ExcitationFilter   string // e.g. "BP 405/60"
	DichromaticMirror  string // e.g. "455"
	SuppressionFilter  string // e.g. "470/50"
	ExposureTimeMicros int64
	CCDGain            int
}

// tilerUnwrapper is the unexported wrapper interface implemented by
// *fileCloser (returned by opentile.OpenFile). Mirrors the pattern
// used in svs/bif/ome/philips/ndpi/generictiff.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

// maxTilerUnwrapHops caps the unwrap walk in [MetadataOf]. The
// realistic chain length is 1 (just *fileCloser); 16 is ample
// headroom while still preventing infinite loops on a wrapper that
// cycles.
const maxTilerUnwrapHops = 16

// MetadataOf returns the SCN-specific metadata if t is a Leica SCN
// Tiler, otherwise (nil, false). Walks any number of wrappers
// (e.g., the *fileCloser returned by opentile.OpenFile) before
// asserting on the concrete type.
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if st, ok := t.(*tiler); ok {
			return &st.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}

// tiler is the Leica SCN implementation of opentile.Tiler.
type tiler struct {
	md         Metadata
	levels     []opentile.Level
	associated []opentile.AssociatedImage
	icc        []byte
	sizeC      int
	channels   []ChannelInfo
}

func (t *tiler) Format() opentile.Format { return opentile.FormatLeicaSCN }
func (t *tiler) Images() []opentile.Image {
	return []opentile.Image{newSCNImage(t.levels, t.sizeC, t.channels)}
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

// scnImage wraps SingleImage and overrides SizeC + ChannelName for
// fluorescence files. Mirrors formats/bif/bif.go's bifImage shape.
type scnImage struct {
	*opentile.SingleImage
	sizeC    int
	channels []ChannelInfo
}

func newSCNImage(levels []opentile.Level, sizeC int, channels []ChannelInfo) *scnImage {
	return &scnImage{
		SingleImage: opentile.NewSingleImage(levels),
		sizeC:       sizeC,
		channels:    channels,
	}
}

func (i *scnImage) SizeC() int { return i.sizeC }

func (i *scnImage) ChannelName(c int) string {
	if c < 0 || c >= len(i.channels) {
		// SizeC > len(channels) only on parser/composer drift; fall
		// back to "" rather than panic. Per Image interface contract
		// callers use indices in [0, SizeC()).
		return ""
	}
	return i.channels[c].Name
}

// buildMetadata constructs the SCN-specific Metadata from the parsed
// Collection plus the auxiliary/main-scan partitioning. Cross-format
// fields (ScannerManufacturer, ScannerModel, AcquisitionDateTime)
// are populated from the SCN XML's <device model="..."> attribute and
// <creationDate> element.
func buildMetadata(c *Collection, auxs, mains []Image) Metadata {
	md := Metadata{
		CollectionUUID: c.UUID,
		Barcode:        c.Barcode,
	}

	// Cross-format identity fields. Use the first main-scan's device
	// info if available, else the first auxiliary's.
	var primaryImg *Image
	switch {
	case len(mains) > 0:
		primaryImg = &mains[0]
	case len(auxs) > 0:
		primaryImg = &auxs[0]
	}
	if primaryImg != nil {
		md.ScannerManufacturer = "Leica"
		md.ScannerModel = scannerModel(primaryImg.DeviceModel)
		if primaryImg.DeviceVersion != "" {
			md.ScannerSoftware = []string{primaryImg.DeviceVersion}
		}
		if primaryImg.CreationDate != "" {
			if ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(primaryImg.CreationDate)); err == nil {
				md.AcquisitionDateTime = ts
			} else if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(primaryImg.CreationDate)); err == nil {
				md.AcquisitionDateTime = ts
			}
		}
		// Magnification from primary image's objective. Honest mapping —
		// SCN's <objective> is the microscope objective magnification,
		// directly comparable to SVS's AppMag.
		md.Magnification = primaryImg.Objective
	}

	for _, aux := range auxs {
		md.Auxiliaries = append(md.Auxiliaries, AuxiliaryInfo{
			Name:               aux.Name,
			IlluminationSource: aux.IlluminationSource,
			Objective:          aux.Objective,
		})
	}
	for _, m := range mains {
		md.Regions = append(md.Regions, RegionInfo{
			Name:               m.Name,
			OffsetXNm:          m.ViewOffsetXNm,
			OffsetYNm:          m.ViewOffsetYNm,
			SizeXNm:            m.ViewSizeXNm,
			SizeYNm:            m.ViewSizeYNm,
			Objective:          m.Objective,
			IlluminationSource: m.IlluminationSource,
		})
	}

	// Channel metadata: copy from the first main scan if present.
	// Mains pass the Q5 invariant of matching channel layout, so any
	// main's <channelSettings> serves as canonical.
	if len(mains) > 0 && len(mains[0].Channels) > 0 {
		md.Channels = make([]ChannelInfo, len(mains[0].Channels))
		for i, ch := range mains[0].Channels {
			md.Channels[i] = ChannelInfo{
				Index:              ch.Index,
				Name:               ch.Name,
				RGB:                ch.RGB,
				ExcitationFilter:   ch.ExcitationFilter,
				DichromaticMirror:  ch.DichromaticMirror,
				SuppressionFilter:  ch.SuppressionFilter,
				ExposureTimeMicros: ch.ExposureTimeMicros,
				CCDGain:            ch.CCDGain,
			}
		}
	}

	return md
}

// scannerModel extracts the Leica scanner model from the SCN XML's
// <device model="..."> attribute. Real values seen in fixtures:
//
//	"Leica SCN400;Leica SCN"   (Leica-1, Leica-2)
//	"Leica SCN400F;Leica SCN"  (Leica-Fluorescence-1)
//
// We surface the first ;-separated component as the model
// (e.g. "Leica SCN400" or "Leica SCN400F"). Returns the verbatim
// string when no separator is present.
func scannerModel(deviceModel string) string {
	if deviceModel == "" {
		return ""
	}
	if i := strings.Index(deviceModel, ";"); i >= 0 {
		return strings.TrimSpace(deviceModel[:i])
	}
	return strings.TrimSpace(deviceModel)
}

// silence the "unused" warning for fmt while the Tiler is partially
// built out — multi-region dispatch (T8) introduces fmt.Errorf calls.
var _ = fmt.Sprintf
