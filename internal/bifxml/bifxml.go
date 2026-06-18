// Package bifxml parses the XMP metadata embedded in Ventana BIF TIFF IFDs.
//
// BIF files carry two XMP payloads:
//   - IFD 0 XMP: an <iScan> element with scanner settings and AOI geometry.
//   - IFD 2 XMP: an <EncodeInfo> element with tile grid layout, tile joints
//     (the serpentine stitch-graph numbering), row-major <Frame> ordering, and
//     AOI origin coordinates.
//
// Parsing is lenient: missing attributes produce zero/default values rather than
// errors. Unknown attributes are collected in IScan.RawAttributes for caller use
// (e.g. metadata mirroring).
//
// The two public entry points are [ParseIScan] and [ParseEncodeInfo].
package bifxml

import (
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// IScan holds the scanner settings block from IFD 0 XMP.
// It appears either as <Metadata><iScan ...> or as a bare <iScan ...> root.
type IScan struct {
	ScannerModel  string  // empty if missing
	Magnification float64 // 0 if missing
	ScanRes       float64 // microns per pixel; 0 if missing

	ScanWhitePoint        uint8 // 0..255 when present; consult ScanWhitePointPresent to detect absence
	ScanWhitePointPresent bool  // false when the attribute is absent entirely

	ZLayers      int     // default 1; 1 = single nominal focus plane (not volumetric)
	ZSpacing     float64 // microns per Z-plane step (per <iScan>/@Z-spacing); 0 when absent
	BuildVersion string
	BuildDate    string
	UnitNumber   string
	UserName     string

	AOIs []AOI // one per <AOI0>, <AOI1>, ... child element

	// RawAttributes contains every iScan XML attribute that does not map to a
	// typed field above, keyed by the exact attribute name as it appears in the
	// XML. Callers can use this to mirror all metadata without hard-coding every
	// attribute name.
	RawAttributes map[string]string
}

// AOI is one scanned region from an <AOI<N>> child of <iScan>.
type AOI struct {
	Index         int
	Left, Top     int
	Right, Bottom int
}

// EncodeInfo holds the tile-encoding block from IFD 2 XMP.
type EncodeInfo struct {
	Ver int // Must be >= 2 per spec; ParseEncodeInfo returns an error if < 2.

	AoiInfo AoiInfo // from <SlideInfo><AoiInfo>

	ImageInfos []ImageInfo // one per <SlideStitchInfo><ImageInfo>
	AoiOrigins []AoiOrigin // one per <AoiOrigin><AOI<N>>
}

// AoiInfo carries the tile-grid dimensions from <AoiInfo> inside <SlideInfo>.
type AoiInfo struct {
	XImageSize, YImageSize int // tile width/height in pixels
	NumRows, NumCols       int
	PosX, PosY             int
}

// ImageInfo carries per-AOI stitch information from <ImageInfo> inside
// <SlideStitchInfo>. TileJoints contains every <TileJointInfo> child;
// Frames contains every <Frame> from the nested <FrameInfo> child.
type ImageInfo struct {
	AOIScanned       bool
	AOIIndex         int
	NumRows, NumCols int
	Width, Height    int
	PosX, PosY       int

	Joints []TileJoint
	Frames []Frame
}

// TileJoint is one <TileJointInfo> entry inside an <ImageInfo>.
// Direction is stored verbatim from the XML; all four compass values
// (LEFT, RIGHT, UP, DOWN) are valid per the BIF whitepaper. Openslide
// rejects LEFT and DOWN — we do not.
type TileJoint struct {
	FlagJoined         bool
	Direction          string // pass-through; "LEFT"|"RIGHT"|"UP"|"DOWN" or any value
	Tile1, Tile2       int
	OverlapX, OverlapY int
	Confidence         int // 0..100; whitepaper §"Image Stitching": only Confidence==100 joins are trusted
}

// Frame is one <Frame> element inside a <FrameInfo> child of <ImageInfo>.
// Col and Row are extracted from the XY="C,R" attribute.
type Frame struct {
	Col, Row int
	Z        int
}

// AoiOrigin is the pixel origin for one AOI region, from <AoiOrigin><AOI<N>>.
type AoiOrigin struct {
	Index           int
	OriginX, OriginY int
}

// aoiNameRE matches element names like AOI0, AOI1, AOI12, etc.
var aoiNameRE = regexp.MustCompile(`^AOI(\d+)$`)

// ParseIScan parses the XMP byte slice from IFD 0 and returns an IScan.
//
// The parser tolerates two layouts:
//   - <Metadata><iScan ...> (Ventana-1 / spec-compliant BIF)
//   - bare <iScan ...> as the document root (OS-1 / legacy BIF)
//
// Missing or empty attributes produce zero values. The ScanWhitePoint field's
// value must be interpreted with ScanWhitePointPresent: when Present is false,
// the attribute was absent (caller's responsibility to apply a default).
// Unknown attributes are collected in RawAttributes.
func ParseIScan(xmp []byte) (*IScan, error) {
	dec := xml.NewDecoder(strings.NewReader(string(xmp)))
	dec.Strict = false

	result := &IScan{
		ZLayers:       1,
		RawAttributes: make(map[string]string),
	}
	var inIScan bool

	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF or parse error; return what we have
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			if inIScan {
				if ee, ok := tok.(xml.EndElement); ok && ee.Name.Local == "iScan" {
					inIScan = false
				}
			}
			continue
		}

		switch se.Name.Local {
		case "iScan":
			inIScan = true
			parseIScanAttrs(se.Attr, result)
		default:
			if inIScan {
				m := aoiNameRE.FindStringSubmatch(se.Name.Local)
				if m != nil {
					idx, _ := strconv.Atoi(m[1])
					result.AOIs = append(result.AOIs, parseAOIAttrs(idx, se.Attr))
				}
			}
		}
	}
	return result, nil
}

// parseIScanAttrs fills the typed fields of IScan from the iScan element's
// attribute list. Unrecognised attributes go into RawAttributes.
func parseIScanAttrs(attrs []xml.Attr, s *IScan) {
	knownAttrs := map[string]struct{}{
		"ScannerModel": {}, "Magnification": {}, "ScanRes": {},
		"ScanWhitePoint": {}, "Z-layers": {}, "Z-spacing": {},
		"BuildVersion": {}, "BuildDate": {}, "UnitNumber": {},
		"UserName": {},
		"Mode": {}, // consumed but not typed
	}
	for _, a := range attrs {
		name := a.Name.Local
		val := a.Value
		switch name {
		case "ScannerModel":
			s.ScannerModel = val
		case "Magnification":
			s.Magnification = parseFloat(val)
		case "ScanRes":
			s.ScanRes = parseFloat(val)
		case "ScanWhitePoint":
			if val != "" {
				f := parseFloat(val)
				clamped := math.Max(0, math.Min(255, math.Round(f)))
				s.ScanWhitePoint = uint8(clamped)
				s.ScanWhitePointPresent = true
			}
		case "Z-layers":
			if v := parseInt(val); v > 0 {
				s.ZLayers = v
			}
		case "Z-spacing":
			// Lenient: ParseFloat returns 0 on empty/malformed input,
			// matching the ZLayers convention.
			s.ZSpacing = parseFloat(val)
		case "BuildVersion":
			s.BuildVersion = val
		case "BuildDate":
			s.BuildDate = val
		case "UnitNumber":
			s.UnitNumber = val
		case "UserName":
			s.UserName = val
		}
		if _, known := knownAttrs[name]; !known {
			s.RawAttributes[name] = val
		}
	}
}

// parseAOIAttrs builds an AOI from <AOI<N>> attributes.
func parseAOIAttrs(idx int, attrs []xml.Attr) AOI {
	a := AOI{Index: idx}
	for _, attr := range attrs {
		switch attr.Name.Local {
		case "Left":
			a.Left = parseInt(attr.Value)
		case "Top":
			a.Top = parseInt(attr.Value)
		case "Right":
			a.Right = parseInt(attr.Value)
		case "Bottom":
			a.Bottom = parseInt(attr.Value)
		}
	}
	return a
}

// ParseEncodeInfo parses the XMP byte slice from IFD 2 and returns an EncodeInfo.
//
// The document root must be <EncodeInfo Ver="N">. Returns an error if Ver < 2
// per the BIF spec requirement. Parsing otherwise proceeds leniently.
//
// Actual nesting in BIF fixtures (confirmed against Ventana-1 and OS-1):
//
//	SlideStitchInfo
//	  ImageInfo  → TileJointInfo children
//	  FrameInfo  → Frame children  (sibling of ImageInfo, not a child)
//
// The parser associates each FrameInfo's frames with the last ImageInfo that
// shares the same AOIIndex. When FrameInfo carries no AOIIndex (or there is
// only one ImageInfo), frames are appended to the first/only ImageInfo.
// Frames are stored in the flat []Frame slice on ImageInfo for ergonomic access.
func ParseEncodeInfo(xmp []byte) (*EncodeInfo, error) {
	dec := xml.NewDecoder(strings.NewReader(string(xmp)))
	dec.Strict = false

	result := &EncodeInfo{}

	// State machine depths.
	var (
		inSlideInfo      bool
		inSlideStitch    bool
		inImageInfo      bool
		inFrameInfo      bool
		inAoiOrigin      bool
		currentImageInfo *ImageInfo // points into result.ImageInfos slice
		frameTargetIdx   int        // index of ImageInfo that receives frames
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "EncodeInfo":
				for _, a := range t.Attr {
					if a.Name.Local == "Ver" {
						result.Ver = parseInt(a.Value)
					}
				}
				if result.Ver < 2 {
					return nil, fmt.Errorf("bifxml: EncodeInfo Ver=%d is < 2; not a supported BIF", result.Ver)
				}
			case "SlideInfo":
				inSlideInfo = true
			case "AoiInfo":
				if inSlideInfo {
					result.AoiInfo = parseAoiInfo(t.Attr)
				}
			case "SlideStitchInfo":
				inSlideStitch = true
			case "ImageInfo":
				if inSlideStitch && !inImageInfo {
					inImageInfo = true
					ii := parseImageInfo(t.Attr)
					result.ImageInfos = append(result.ImageInfos, ii)
					currentImageInfo = &result.ImageInfos[len(result.ImageInfos)-1]
				}
			case "TileJointInfo":
				if inImageInfo && currentImageInfo != nil {
					currentImageInfo.Joints = append(currentImageInfo.Joints, parseTileJoint(t.Attr))
				}
			case "FrameInfo":
				// FrameInfo is a sibling of ImageInfo under SlideStitchInfo
				// (confirmed against Ventana-1: </ImageInfo> appears before <FrameInfo>).
				// It may also appear nested inside ImageInfo on other scanners;
				// handle both by checking inSlideStitch.
				if inSlideStitch {
					inFrameInfo = true
					// Determine which ImageInfo receives these frames.
					// FrameInfo carries an AOIIndex attribute matching ImageInfo.AOIIndex.
					aoiIdx := -1
					for _, a := range t.Attr {
						if a.Name.Local == "AOIIndex" {
							aoiIdx = parseInt(a.Value)
						}
					}
					frameTargetIdx = 0 // default: first ImageInfo
					if aoiIdx >= 0 {
						for i := range result.ImageInfos {
							if result.ImageInfos[i].AOIIndex == aoiIdx {
								frameTargetIdx = i
								break
							}
						}
					}
				}
			case "Frame":
				if inFrameInfo && frameTargetIdx < len(result.ImageInfos) {
					result.ImageInfos[frameTargetIdx].Frames = append(
						result.ImageInfos[frameTargetIdx].Frames,
						parseFrame(t.Attr),
					)
				}
			case "AoiOrigin":
				inAoiOrigin = true
			default:
				if inAoiOrigin {
					m := aoiNameRE.FindStringSubmatch(t.Name.Local)
					if m != nil {
						idx, _ := strconv.Atoi(m[1])
						origin := AoiOrigin{Index: idx}
						for _, a := range t.Attr {
							switch a.Name.Local {
							case "OriginX":
								origin.OriginX = parseInt(a.Value)
							case "OriginY":
								origin.OriginY = parseInt(a.Value)
							}
						}
						result.AoiOrigins = append(result.AoiOrigins, origin)
					}
				}
			}

		case xml.EndElement:
			switch t.Name.Local {
			case "SlideInfo":
				inSlideInfo = false
			case "SlideStitchInfo":
				inSlideStitch = false
				currentImageInfo = nil
				inImageInfo = false
				inFrameInfo = false
			case "ImageInfo":
				inImageInfo = false
			case "FrameInfo":
				inFrameInfo = false
			case "AoiOrigin":
				inAoiOrigin = false
			}
		}
	}
	return result, nil
}

// parseAoiInfo extracts AoiInfo fields from <AoiInfo> attributes.
// The attribute names use hyphens (Pos-X, Pos-Y) as they appear in the XML.
func parseAoiInfo(attrs []xml.Attr) AoiInfo {
	ai := AoiInfo{}
	for _, a := range attrs {
		switch a.Name.Local {
		case "XIMAGESIZE":
			ai.XImageSize = parseInt(a.Value)
		case "YIMAGESIZE":
			ai.YImageSize = parseInt(a.Value)
		case "NumRows":
			ai.NumRows = parseInt(a.Value)
		case "NumCols":
			ai.NumCols = parseInt(a.Value)
		case "Pos-X":
			ai.PosX = parseInt(a.Value)
		case "Pos-Y":
			ai.PosY = parseInt(a.Value)
		}
	}
	return ai
}

// parseImageInfo extracts ImageInfo fields from <ImageInfo> attributes.
func parseImageInfo(attrs []xml.Attr) ImageInfo {
	ii := ImageInfo{}
	for _, a := range attrs {
		switch a.Name.Local {
		case "AOIScanned":
			ii.AOIScanned = parseBool(a.Value)
		case "AOIIndex":
			ii.AOIIndex = parseInt(a.Value)
		case "NumRows":
			ii.NumRows = parseInt(a.Value)
		case "NumCols":
			ii.NumCols = parseInt(a.Value)
		case "Width":
			ii.Width = parseInt(a.Value)
		case "Height":
			ii.Height = parseInt(a.Value)
		case "Pos-X":
			ii.PosX = parseInt(a.Value)
		case "Pos-Y":
			ii.PosY = parseInt(a.Value)
		}
	}
	return ii
}

// parseTileJoint extracts TileJoint fields from <TileJointInfo> attributes.
func parseTileJoint(attrs []xml.Attr) TileJoint {
	tj := TileJoint{}
	for _, a := range attrs {
		switch a.Name.Local {
		case "FlagJoined":
			tj.FlagJoined = parseBool(a.Value)
		case "Direction":
			tj.Direction = a.Value
		case "Tile1":
			tj.Tile1 = parseInt(a.Value)
		case "Tile2":
			tj.Tile2 = parseInt(a.Value)
		case "OverlapX":
			tj.OverlapX = parseInt(a.Value)
		case "OverlapY":
			tj.OverlapY = parseInt(a.Value)
		case "Confidence":
			tj.Confidence = parseInt(a.Value)
		}
	}
	return tj
}

// parseFrame extracts Frame fields from a <Frame> element.
// The XY attribute is "col,row" e.g. XY="3,7".
func parseFrame(attrs []xml.Attr) Frame {
	f := Frame{}
	for _, a := range attrs {
		switch a.Name.Local {
		case "XY":
			parts := strings.SplitN(a.Value, ",", 2)
			if len(parts) == 2 {
				f.Col = parseInt(parts[0])
				f.Row = parseInt(parts[1])
			}
		case "Z":
			f.Z = parseInt(a.Value)
		}
	}
	return f
}

// parseFloat converts a string to float64, returning 0 on error or empty input.
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// parseInt converts a string to int, returning 0 on error or empty input.
func parseInt(s string) int {
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

// parseBool returns true for "1" and false for everything else (including "0"
// and the empty string), matching BIF's use of numeric flags.
func parseBool(s string) bool {
	return strings.TrimSpace(s) == "1"
}
