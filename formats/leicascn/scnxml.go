// Package leicascn implements opentile-go format support for Leica SCN —
// a BigTIFF dialect produced by Leica SCN400 / SCN400F scanners
// (production discontinued ~2015). Detection: BigTIFF + IFD 0
// ImageDescription contains the SCN schema URN. The XML in IFD 0
// maps every TIFF IFD to a logical (image, level, channel) tuple;
// without it the file is a pile of unlabeled IFDs.
//
// SCN files express "discontinuous scanning" of a single slide:
// multiple <image> elements, each with its own pyramid, sharing one
// slide-level coordinate system via <view> offsets. Inter-region
// slide area carries no pixel data; the reader fills those gaps with
// synthesized blank tiles so consumers see one continuous slide.
//
// Spec: docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md.
package leicascn

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// SchemaURN is the SCN XML schema URN used as the detection
// discriminator. Sealed at v0.11 design Q1.
const SchemaURN = "http://www.leica-microsystems.com/scn/2010/10/01"

// Collection is the top-level <scn>/<collection> element. Carries the
// slide's physical extent (in nm) and the list of <image> children.
type Collection struct {
	UUID    string  // collection's uuid attribute
	Name    string  // collection's name attribute
	Barcode string  // <barcode> child text (base64-encoded; may be empty)
	SizeXNm uint64  // collection's sizeX attribute; slide physical extent in nm
	SizeYNm uint64  // collection's sizeY attribute
	Images  []Image // each <image> child
}

// Image is one <image> element under <collection>. Each Image has its
// own pyramid (one IFD per resolution × channel) and slide-physical
// view rectangle.
type Image struct {
	Name              string
	UUID              string
	CreationDate      string // ISO-8601 string; verbatim
	DeviceModel       string
	DeviceVersion     string
	PixelsSizeX       uint32 // <pixels sizeX>; same as level-0 width
	PixelsSizeY       uint32
	Dimensions        []Dimension
	ViewSizeXNm       uint64 // <view sizeX>; slide-physical extent of this scan
	ViewSizeYNm       uint64
	ViewOffsetXNm     uint64
	ViewOffsetYNm     uint64
	SpacingZNm        uint64 // <view spacingZ>; 0 for single-Z (our 3 fixtures)
	Objective         float64
	NumericalAperture float64
	IlluminationSource string  // "brightfield" or "fluorescence"
	Channels          []Channel // populated when SizeC > 1
}

// Dimension is one <dimension> entry under <pixels>. Maps a
// (resolution r, channel c) coordinate to a TIFF IFD index.
type Dimension struct {
	R     int    // resolution / level (0 = highest)
	C     int    // channel index; 0 if absent (single-channel)
	SizeX uint32 // pixel width at this level
	SizeY uint32 // pixel height at this level
	IFD   int    // TIFF IFD index containing this level/channel's tiles
}

// Channel is one <channelSettings>/<channel> element. Populated only
// for multi-channel fluorescence main scans.
type Channel struct {
	Index              int
	Name               string // e.g. "405|Empty"
	RGB                string // e.g. "#0000ff"
	ExcitationFilter   string // e.g. "BP 405/60"
	DichromaticMirror  string // e.g. "455"
	SuppressionFilter  string // e.g. "470/50"
	ExposureTimeMicros int64
	CCDGain            int
}

// ParseDescription parses an SCN XML document (typically the value
// of IFD 0's ImageDescription tag) into a Collection. Returns an
// error if the schema URN doesn't match.
//
// Lenient: missing optional attributes produce zero values rather
// than errors. Mirrors internal/bifxml's lenient walker style.
func ParseDescription(xmlText string) (*Collection, error) {
	if !strings.Contains(xmlText, SchemaURN) {
		return nil, fmt.Errorf("leicascn: ImageDescription does not contain SCN schema URN %q", SchemaURN)
	}
	dec := xml.NewDecoder(strings.NewReader(xmlText))
	dec.Strict = false

	c := &Collection{}
	var (
		curImage          *Image
		curChannel        *Channel
		captureBarcode    bool
		captureCreate     bool
		captureFcCube     bool
		captureFcFilter   bool
		curCubeShortName  string
		curFilterValue    string
		captureExp        bool
		captureGain       bool
		captureChNameText string // unused; channel name lives in attr
		_                 = captureChNameText
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			break // EOF or parse error; return what we have
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "collection":
				parseCollectionAttrs(t.Attr, c)
			case "barcode":
				captureBarcode = true
			case "image":
				img := Image{}
				parseImageAttrs(t.Attr, &img)
				c.Images = append(c.Images, img)
				curImage = &c.Images[len(c.Images)-1]
			case "creationDate":
				captureCreate = true
			case "device":
				if curImage != nil {
					parseDeviceAttrs(t.Attr, curImage)
				}
			case "pixels":
				if curImage != nil {
					parsePixelsAttrs(t.Attr, curImage)
				}
			case "dimension":
				if curImage != nil {
					curImage.Dimensions = append(curImage.Dimensions, parseDimensionAttrs(t.Attr))
				}
			case "view":
				if curImage != nil {
					parseViewAttrs(t.Attr, curImage)
				}
			case "objective":
				if curImage != nil {
					captureObjective(dec, curImage)
				}
			case "numericalAperture":
				if curImage != nil {
					captureNumericalAperture(dec, curImage)
				}
			case "illuminationSource":
				if curImage != nil {
					captureIlluminationSource(dec, curImage)
				}
			case "channel":
				if curImage != nil {
					curImage.Channels = append(curImage.Channels, Channel{})
					curChannel = &curImage.Channels[len(curImage.Channels)-1]
					parseChannelAttrs(t.Attr, curChannel)
				}
			case "fluorescenceCube":
				captureFcCube = true
			case "fluorescenceFilter":
				captureFcFilter = true
			case "shortName":
				if captureFcCube && curChannel != nil {
					curCubeShortName = readText(dec)
					_ = curCubeShortName
				}
			case "excitationFilter":
				if curChannel != nil {
					v := readText(dec)
					if captureFcCube {
						curChannel.ExcitationFilter = v
					}
					_ = captureFcFilter
				}
			case "dichromaticMirror":
				if curChannel != nil {
					curChannel.DichromaticMirror = readText(dec)
				}
			case "suppressionFilter":
				if curChannel != nil {
					curChannel.SuppressionFilter = readText(dec)
				}
			case "exposureTime":
				if curChannel != nil {
					captureExp = true
				}
			case "ccdGain":
				if curChannel != nil {
					captureGain = true
				}
			}

			if captureExp && t.Name.Local == "exposureTime" && curChannel != nil {
				v := readText(dec)
				curChannel.ExposureTimeMicros = parseInt64(v)
				captureExp = false
			}
			if captureGain && t.Name.Local == "ccdGain" && curChannel != nil {
				v := readText(dec)
				curChannel.CCDGain = int(parseInt64(v))
				captureGain = false
			}
			_ = curFilterValue

		case xml.CharData:
			if captureBarcode {
				c.Barcode += string(t)
			}
			if captureCreate && curImage != nil {
				curImage.CreationDate += string(t)
			}

		case xml.EndElement:
			switch t.Name.Local {
			case "barcode":
				captureBarcode = false
				c.Barcode = strings.TrimSpace(c.Barcode)
			case "creationDate":
				captureCreate = false
				if curImage != nil {
					curImage.CreationDate = strings.TrimSpace(curImage.CreationDate)
				}
			case "image":
				curImage = nil
			case "channel":
				curChannel = nil
			case "fluorescenceCube":
				captureFcCube = false
			case "fluorescenceFilter":
				captureFcFilter = false
			}
		}
	}
	return c, nil
}

// readText consumes character data until the next end element and
// returns the trimmed text. Used for elements like <objective>20</objective>.
func readText(dec *xml.Decoder) string {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return strings.TrimSpace(sb.String())
		}
	}
	return strings.TrimSpace(sb.String())
}

// captureObjective reads the next <objective> text content into curImage.
func captureObjective(dec *xml.Decoder, img *Image) {
	img.Objective = parseFloat(readText(dec))
}

func captureNumericalAperture(dec *xml.Decoder, img *Image) {
	img.NumericalAperture = parseFloat(readText(dec))
}

func captureIlluminationSource(dec *xml.Decoder, img *Image) {
	img.IlluminationSource = readText(dec)
}

func parseCollectionAttrs(attrs []xml.Attr, c *Collection) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "uuid":
			c.UUID = a.Value
		case "name":
			c.Name = a.Value
		case "sizeX":
			c.SizeXNm = parseUint64(a.Value)
		case "sizeY":
			c.SizeYNm = parseUint64(a.Value)
		}
	}
}

func parseImageAttrs(attrs []xml.Attr, img *Image) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "name":
			img.Name = a.Value
		case "uuid":
			img.UUID = a.Value
		}
	}
}

func parseDeviceAttrs(attrs []xml.Attr, img *Image) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "model":
			img.DeviceModel = a.Value
		case "version":
			img.DeviceVersion = a.Value
		}
	}
}

func parsePixelsAttrs(attrs []xml.Attr, img *Image) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "sizeX":
			img.PixelsSizeX = uint32(parseUint64(a.Value))
		case "sizeY":
			img.PixelsSizeY = uint32(parseUint64(a.Value))
		}
	}
}

func parseDimensionAttrs(attrs []xml.Attr) Dimension {
	d := Dimension{}
	for _, a := range attrs {
		switch a.Name.Local {
		case "r":
			d.R = int(parseUint64(a.Value))
		case "c":
			d.C = int(parseUint64(a.Value))
		case "sizeX":
			d.SizeX = uint32(parseUint64(a.Value))
		case "sizeY":
			d.SizeY = uint32(parseUint64(a.Value))
		case "ifd":
			d.IFD = int(parseUint64(a.Value))
		}
	}
	return d
}

func parseViewAttrs(attrs []xml.Attr, img *Image) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "sizeX":
			img.ViewSizeXNm = parseUint64(a.Value)
		case "sizeY":
			img.ViewSizeYNm = parseUint64(a.Value)
		case "offsetX":
			img.ViewOffsetXNm = parseUint64(a.Value)
		case "offsetY":
			img.ViewOffsetYNm = parseUint64(a.Value)
		case "spacingZ":
			img.SpacingZNm = parseUint64(a.Value)
		}
	}
}

func parseChannelAttrs(attrs []xml.Attr, ch *Channel) {
	for _, a := range attrs {
		switch a.Name.Local {
		case "index":
			ch.Index = int(parseUint64(a.Value))
		case "name":
			ch.Name = a.Value
		case "rgb":
			ch.RGB = a.Value
		}
	}
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func parseUint64(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}
