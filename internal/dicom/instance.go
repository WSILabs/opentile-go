// Package dicom parses the metadata of DICOM VL Whole Slide Microscopy
// (WSM) SOP instances for the formats/dicom reader. It is the only place
// in opentile-go that imports github.com/suyashkumar/dicom, and no
// suyashkumar type is exported from here. It parses metadata only — it
// does not read pixel data and does not import the root opentile package.
package dicom

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

// WSMStorageUID is the SOP Class UID for VL Whole Slide Microscopy Image Storage.
const WSMStorageUID = "1.2.840.10008.5.1.4.1.1.77.1.6"

// FramePos is the 1-based top-left pixel position of a frame in the total
// pixel matrix (TILED_SPARSE only).
type FramePos struct{ Col, Row int }

// Instance is the parsed metadata of one WSM SOP instance.
type Instance struct {
	Path            string
	SOPClassUID     string
	SeriesUID       string
	ImageType       []string
	TransferSyntax  string
	TotalCols       int
	TotalRows       int
	TileCols        int
	TileRows        int
	NumFrames       int
	DimOrg          string // TILED_FULL | TILED_SPARSE
	Photometric     string
	SamplesPerPixel int // 0028,0002 (1 monochrome, 3 RGB); authoritative for native decode
	PixelSpacingX   float64
	PixelSpacingY   float64
	ObjectivePower  float64
	Manufacturer    string
	Model           string
	Software        string
	Writer          string
	ICCProfile      []byte
	FramePositions  []FramePos // len == NumFrames for SPARSE; nil for FULL
}

var (
	tTransferSyntax = tag.Tag{Group: 0x0002, Element: 0x0010}
	tWriter         = tag.Tag{Group: 0x0002, Element: 0x0013} // ImplementationVersionName
	tSourceAE       = tag.Tag{Group: 0x0002, Element: 0x0016}
	tSOPClass       = tag.Tag{Group: 0x0008, Element: 0x0016}
	tImageType      = tag.Tag{Group: 0x0008, Element: 0x0008}
	tManufacturer   = tag.Tag{Group: 0x0008, Element: 0x0070}
	tModel          = tag.Tag{Group: 0x0008, Element: 0x1090}
	tSoftware       = tag.Tag{Group: 0x0018, Element: 0x1020}
	tSeries         = tag.Tag{Group: 0x0020, Element: 0x000E}
	tTotalCols      = tag.Tag{Group: 0x0048, Element: 0x0006}
	tTotalRows      = tag.Tag{Group: 0x0048, Element: 0x0007}
	tRows           = tag.Tag{Group: 0x0028, Element: 0x0010}
	tCols           = tag.Tag{Group: 0x0028, Element: 0x0011}
	tNumFrames      = tag.Tag{Group: 0x0028, Element: 0x0008}
	tPhotometric    = tag.Tag{Group: 0x0028, Element: 0x0004}
	tSamplesPerPix  = tag.Tag{Group: 0x0028, Element: 0x0002}
	tDimOrg         = tag.Tag{Group: 0x0020, Element: 0x9311}
	tObjective      = tag.Tag{Group: 0x0048, Element: 0x0112}
	tPixelSpacing   = tag.Tag{Group: 0x0028, Element: 0x0030}
	tPerFrameFG     = tag.Tag{Group: 0x5200, Element: 0x9230}
	tPlanePosSlide  = tag.Tag{Group: 0x0048, Element: 0x021A}
	tColPos         = tag.Tag{Group: 0x0048, Element: 0x021E}
	tRowPos         = tag.Tag{Group: 0x0048, Element: 0x021F}
)

// ParseInstance parses one instance's metadata (pixel data skipped). It never
// panics: a malformed instance (e.g. an input that trips a parser bug)
// returns an error. HTJ2K transfer syntaxes are handled via parseDataset.
func ParseInstance(path string) (inst Instance, err error) {
	defer func() {
		if r := recover(); r != nil {
			inst, err = Instance{}, fmt.Errorf("dicom: recovered from panic parsing %q: %v", path, r)
		}
	}()
	ds, realTS, err := parseDataset(path)
	if err != nil {
		return Instance{}, err
	}
	in := Instance{
		Path:            path,
		SOPClassUID:     firstStr(ds, tSOPClass),
		SeriesUID:       firstStr(ds, tSeries),
		ImageType:       allStr(ds, tImageType),
		TransferSyntax:  firstStr(ds, tTransferSyntax),
		TotalCols:       firstInt(ds, tTotalCols),
		TotalRows:       firstInt(ds, tTotalRows),
		TileCols:        firstInt(ds, tCols),
		TileRows:        firstInt(ds, tRows),
		DimOrg:          firstStr(ds, tDimOrg),
		Photometric:     firstStr(ds, tPhotometric),
		SamplesPerPixel: firstInt(ds, tSamplesPerPix),
		ObjectivePower:  nestedFloat(ds, tObjective),
		Manufacturer:    firstStr(ds, tManufacturer),
		Model:           firstStr(ds, tModel),
		Software:        firstStr(ds, tSoftware),
	}
	in.NumFrames = atoiSafe(firstStr(ds, tNumFrames))
	in.Writer = firstStr(ds, tWriter)
	if in.Writer == "" {
		in.Writer = firstStr(ds, tSourceAE)
	}
	if sx, sy, ok := nestedPixelSpacing(ds); ok {
		in.PixelSpacingX, in.PixelSpacingY = sx, sy
	}
	if in.DimOrg == "TILED_SPARSE" {
		in.FramePositions = parseFramePositions(ds)
	}
	if e, err := ds.FindElementByTagNested(tag.Tag{Group: 0x0028, Element: 0x2000}); err == nil {
		if v, ok := e.Value.GetValue().([]byte); ok {
			in.ICCProfile = v
		}
	}
	// parseDataset substitutes a proxy transfer syntax for HTJ2K; restore the
	// real one it reported.
	if realTS != "" {
		in.TransferSyntax = realTS
	}
	return in, nil
}

// roleOf returns the WSM role token from ImageType, or "" if none present.
func roleOf(imageType []string) string {
	for _, v := range imageType {
		switch v {
		case "VOLUME", "LABEL", "OVERVIEW", "THUMBNAIL":
			return v
		}
	}
	return ""
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func firstStr(ds dicom.Dataset, t tag.Tag) string {
	if v := allStr(ds, t); len(v) > 0 {
		return v[0]
	}
	return ""
}

func allStr(ds dicom.Dataset, t tag.Tag) []string {
	e, err := ds.FindElementByTag(t)
	if err != nil {
		return nil
	}
	if v, ok := e.Value.GetValue().([]string); ok {
		return v
	}
	return nil
}

func firstInt(ds dicom.Dataset, t tag.Tag) int {
	e, err := ds.FindElementByTag(t)
	if err != nil {
		return 0
	}
	if v, ok := e.Value.GetValue().([]int); ok && len(v) > 0 {
		return v[0]
	}
	return 0
}

// nestedFloat finds the first DS-valued element with tag t anywhere
// (including inside sequences) and parses it as a float.
func nestedFloat(ds dicom.Dataset, t tag.Tag) float64 {
	e, err := ds.FindElementByTagNested(t)
	if err != nil {
		return 0
	}
	if v, ok := e.Value.GetValue().([]string); ok && len(v) > 0 {
		f, _ := strconv.ParseFloat(strings.TrimSpace(v[0]), 64)
		return f
	}
	return 0
}

// nestedPixelSpacing reads PixelSpacing (row\col spacing in mm) from the
// Shared Functional Groups → Pixel Measures sequence.
func nestedPixelSpacing(ds dicom.Dataset) (x, y float64, ok bool) {
	e, err := ds.FindElementByTagNested(tPixelSpacing)
	if err != nil {
		return 0, 0, false
	}
	v, vok := e.Value.GetValue().([]string)
	if !vok || len(v) < 2 {
		return 0, 0, false
	}
	// PixelSpacing is [rowSpacing, colSpacing]; Y = row, X = col.
	y, _ = strconv.ParseFloat(strings.TrimSpace(v[0]), 64)
	x, _ = strconv.ParseFloat(strings.TrimSpace(v[1]), 64)
	return x, y, true
}

// parseFramePositions walks PerFrameFunctionalGroupsSequence →
// PlanePositionSlideSequence and returns one FramePos per frame.
func parseFramePositions(ds dicom.Dataset) []FramePos {
	pf, err := ds.FindElementByTag(tPerFrameFG)
	if err != nil {
		return nil
	}
	items, ok := pf.Value.GetValue().([]*dicom.SequenceItemValue)
	if !ok {
		return nil
	}
	out := make([]FramePos, 0, len(items))
	for _, it := range items {
		els, _ := it.GetValue().([]*dicom.Element)
		pps := findIn(els, tPlanePosSlide)
		if pps == nil {
			out = append(out, FramePos{})
			continue
		}
		ppsItems, _ := pps.Value.GetValue().([]*dicom.SequenceItemValue)
		if len(ppsItems) == 0 {
			out = append(out, FramePos{})
			continue
		}
		inner, _ := ppsItems[0].GetValue().([]*dicom.Element)
		out = append(out, FramePos{Col: intOf(inner, tColPos), Row: intOf(inner, tRowPos)})
	}
	return out
}

func findIn(els []*dicom.Element, t tag.Tag) *dicom.Element {
	for _, e := range els {
		if e.Tag == t {
			return e
		}
	}
	return nil
}

func intOf(els []*dicom.Element, t tag.Tag) int {
	if e := findIn(els, t); e != nil {
		if v, ok := e.Value.GetValue().([]int); ok && len(v) > 0 {
			return v[0]
		}
	}
	return 0
}
