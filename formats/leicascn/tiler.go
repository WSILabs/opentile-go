package leicascn

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
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
	// (one element with kind="overview" in the AssociatedImage list,
	// per sealed Q8, updated in v0.15). Order matches AssociatedImage iteration order.
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

// MetadataOf returns the SCN-specific metadata if v is (or wraps) a Leica SCN
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
func MetadataOf(v any) (*Metadata, bool) {
	const maxUnwrapHops = 16
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if st, ok := v.(*tiler); ok {
			return &st.md, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}

// scnDirSpec captures the physical page pointer and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
// SCN levels are composed from multiple region IFDs; the canonical page is
// region 0 / channel 0 for each level. All other region/channel IFDs at a
// level become DirOther.
type scnDirSpec struct {
	page  *tiff.Page
	kind  opentile.DirectoryKind
	level int    // valid when kind==DirLevel
	assoc string // valid when kind==DirAssociated; matches AssociatedImage.Type()
}

// tiler is the Leica SCN implementation of format.Reader.
type tiler struct {
	md         Metadata
	levelImpls []*compositeLevel // parallel to images[0].Levels; tile-read logic
	images     []opentile.Image  // value-type image/level metadata
	associated []opentile.AssociatedImage
	icc        []byte
	sizeC      int
	channels   []ChannelInfo

	// dirSpecs captures the page→role mapping for every IFD, recorded
	// at Open time so TIFFDirectories() can build the public view lazily.
	dirSpecs []scnDirSpec
}

func (t *tiler) Format() opentile.Format    { return opentile.FormatLeicaSCN }
func (t *tiler) Images() []opentile.Image   { return t.images }
func (t *tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error                           { return nil }

// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider.
//
// SCN is multi-region: each composite level is backed by N regions × M
// channels IFDs. The canonical IFD for each level is region 0 / channel 0
// (the same page newTiledRegion uses as its "canonical" source for tile
// size / grid). All other region/channel IFDs at a level are emitted as
// DirOther. Associated images use the lowest-resolution auxiliary IFD
// (the page selected by newAssociatedImage). SCN exposes a single main
// image (Image: 0).
func (t *tiler) TIFFDirectories() []opentile.TIFFDirectory {
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.page == nil {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Kind:       ds.kind,
			Image:      0, // SCN exposes a single main image
			Level:      ds.level,
			Associated: ds.assoc,
			Tags:       opentile.TIFFTagsFromPage(ds.page),
		})
	}
	return out
}

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].warm()
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	return t.levelImpls[level].TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return func(yield func(opentile.TilePos, opentile.TileResult) bool) {}
	}
	return t.levelImpls[level].Tiles(ctx)
}

// SizeC returns the number of fluorescence channels for this slide
// (1 for brightfield files). Exposed for internal tests; consumers
// should use MetadataOf to inspect channel metadata.
func (t *tiler) SizeC() int { return t.sizeC }

// ChannelName returns the name of channel c from the SCN XML.
// Returns "" for out-of-range indices or brightfield files.
func (t *tiler) ChannelName(c int) string {
	if c < 0 || c >= len(t.channels) {
		return ""
	}
	return t.channels[c].Name
}

// buildMetadata constructs the SCN-specific Metadata from the parsed
// Collection plus the auxiliary/main-scan partitioning, plus the raw
// SCN-XML text (surfaced verbatim as cross.ImageDescription). Cross-
// format fields are populated from the SCN-XML's primary main-scan
// <image> element:
//
//   - ScannerManufacturer = "Leica" (hardcoded; SCN is Leica's format)
//   - ScannerModel from <device model="..."> first ;-separated token
//   - ScannerSoftware from <device version="...">
//   - AcquisitionDateTime from <creationDate> (ISO 8601)
//   - Magnification from <objective>
//   - MicronsPerPixelX/Y derived from the primary <view sizeX/sizeY>
//     (slide-physical extent in nm) divided by <pixels sizeX/sizeY>
//     (level-0 pixel extent), converted nm → µm. SetMPPSymmetric
//     collapses to the symmetric slot when X == Y (typical for SCN).
//   - ImageDescription = raw SCN-XML text (verbatim, mirrors svs/bif
//     pattern)
//   - Properties["leica.collection.uuid"] = collection UUID
//   - Properties["leica.collection.name"] = collection name (when set)
//   - Properties["leica.barcode"] = base64-encoded barcode (when set)
//   - Properties["leica.illumination_source"] = primary main's
//     illumination ("brightfield" or "fluorescence")
//   - Properties["leica.region_count"] = number of main-scan regions
//     (>1 only on multi-region files like Leica-2.scn)
//
// Multi-region note: SCN files can carry multiple main-scan <image>
// elements (Leica-2.scn has 4); the cross-format Metadata reflects
// region 0's pixel scale + objective. Per-region pixel scale is
// available via Metadata.Regions (slide-physical extent + offset)
// for consumers needing the full layout.
func buildMetadata(c *Collection, auxs, mains []Image, desc string) Metadata {
	md := Metadata{
		CollectionUUID: c.UUID,
		Barcode:        c.Barcode,
	}

	// ImageDescription: surface the full raw SCN-XML verbatim. Mirrors
	// the svs / bif pattern of preserving the source description so
	// callers can re-parse it without going through MetadataOf.
	md.ImageDescription = desc

	// Properties: collection-level non-canonical surface. Use the
	// "leica." prefix for SCN-specific keys per v0.17 convention.
	if c.UUID != "" {
		md.SetProperty("leica.collection.uuid", c.UUID)
	}
	if c.Name != "" {
		md.SetProperty("leica.collection.name", c.Name)
	}
	if c.Barcode != "" {
		md.SetProperty("leica.barcode", c.Barcode)
	}
	md.SetProperty("leica.region_count", strconv.Itoa(len(mains)))

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
			md.Writer = primaryImg.DeviceVersion // v0.20
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

		// Per-axis MPP from <view sizeX/sizeY> (slide-physical nm) ÷
		// <pixels sizeX/sizeY> (level-0 pixels) ÷ 1000 (nm → µm).
		// SCN files in our slate produce symmetric values (X == Y);
		// SetMPPSymmetric collapses to the symmetric slot.
		if primaryImg.PixelsSizeX > 0 && primaryImg.ViewSizeXNm > 0 {
			md.MicronsPerPixelX = float64(primaryImg.ViewSizeXNm) / float64(primaryImg.PixelsSizeX) / 1000.0
		}
		if primaryImg.PixelsSizeY > 0 && primaryImg.ViewSizeYNm > 0 {
			md.MicronsPerPixelY = float64(primaryImg.ViewSizeYNm) / float64(primaryImg.PixelsSizeY) / 1000.0
		}
		md.SetMPPSymmetric()

		if primaryImg.IlluminationSource != "" {
			md.SetProperty("leica.illumination_source", primaryImg.IlluminationSource)
		}
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

// suppress unused-import warnings during iterative development.
var _ = fmt.Sprintf
