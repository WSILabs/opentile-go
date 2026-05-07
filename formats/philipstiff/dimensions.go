// Package philipstiff reads tiles from Philips IntelliSite Pathology Solution
// TIFF whole-slide images. It is a direct port of Python opentile's
// formats/philips/ subtree (Apache 2.0, Sectra AB).
package philipstiff

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// philipsAttribute is a single (Name, Text) pair extracted from the
// Philips DICOM-XML document at any depth. Captured by walkAttributes.
type philipsAttribute struct {
	Name string
	Text string
}

// walkAttributes returns every <Attribute Name="..."> element's
// (Name, immediate text) in document order, descending into nested
// elements (Philips wraps level-specific Attributes inside
// <PIM_DP_SCANNED_IMAGES><Array><DataObject>...). Mirrors upstream's
// `ElementTree.iter('Attribute')` traversal.
//
// "Immediate text" is the CharData appearing as a direct child of the
// Attribute element BEFORE any nested child element starts — matching
// ElementTree's `element.text` semantics. For Attributes that contain
// nested Attributes (the wrapper case), the immediate text is just the
// inter-tag whitespace and is effectively empty after trimming; the
// nested Attributes are emitted as separate entries.
func walkAttributes(xmlStr string) ([]philipsAttribute, error) {
	dec := xml.NewDecoder(strings.NewReader(xmlStr))

	type pending struct {
		name     string
		text     strings.Builder
		sawChild bool
	}
	var stack []*pending
	var out []philipsAttribute

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("philips: parse metadata XML: %w", err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			// Any child start marks the current top-of-stack Attribute
			// as past its .text phase (matches ElementTree semantics).
			if len(stack) > 0 {
				stack[len(stack)-1].sawChild = true
			}
			if v.Name.Local == "Attribute" {
				name := ""
				for _, a := range v.Attr {
					if a.Name.Local == "Name" {
						name = a.Value
						break
					}
				}
				stack = append(stack, &pending{name: name})
			}
		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				if !top.sawChild {
					top.text.Write(v)
				}
			}
		case xml.EndElement:
			if v.Name.Local == "Attribute" && len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				out = append(out, philipsAttribute{Name: top.name, Text: top.text.String()})
			}
		}
	}
	return out, nil
}

// parsePixelSpacingPairs walks the XML and returns every
// DICOM_PIXEL_SPACING entry's (w, h) ratio, in document order. Each
// entry's text is a whitespace-separated pair of quoted floats — e.g.
// `"0.000247746" "0.000247746"`. Quotes are stripped before parsing.
func parsePixelSpacingPairs(xmlStr string) ([][2]float64, error) {
	attrs, err := walkAttributes(xmlStr)
	if err != nil {
		return nil, err
	}
	var out [][2]float64
	for _, a := range attrs {
		if a.Name != "DICOM_PIXEL_SPACING" {
			continue
		}
		w, h, ok := parseTwoFloats(a.Text)
		if !ok {
			return nil, fmt.Errorf("philips: malformed DICOM_PIXEL_SPACING value %q", a.Text)
		}
		out = append(out, [2]float64{w, h})
	}
	return out, nil
}

// parseTwoFloats strips literal quote characters from s and splits on
// whitespace, returning the first two parsed floats. Mirrors
// upstream's `text.replace('"', ”).split()`.
func parseTwoFloats(s string) (float64, float64, bool) {
	s = strings.ReplaceAll(s, `"`, "")
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return 0, 0, false
	}
	w, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, false
	}
	h, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, false
	}
	return w, h, true
}

// computeCorrectedSizes parses Philips DICOM-XML metadata and returns
// the corrected (W, H) for each tiled pyramid page in document order.
//
// Direct port of tifffile._philips_load_pages (tifffile.py:6477-6540).
// The first DICOM_PIXEL_SPACING entry calibrates the baseline mm scale
// from baseW/baseH (the on-disk page-0 dimensions); each subsequent
// entry produces a corrected size for the next tiled page via:
//
//	mmW = baseW * w_base
//	mmH = baseH * h_base
//	corrected[k].W = ceil(mmW / w_{k+1})
//	corrected[k].H = ceil(mmH / h_{k+1})
//
// So N PS entries produce N-1 corrected sizes; the first tiled page
// (the "baseline" pyramid level) gets corrected[0], the second tiled
// page gets corrected[1], etc.
//
// Returns an error if no DICOM_PIXEL_SPACING entries are present (a
// malformed Philips slide; upstream asserts).
func computeCorrectedSizes(xmlStr string, baseW, baseH int) ([][2]int, error) {
	pairs, err := parsePixelSpacingPairs(xmlStr)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("philips: no DICOM_PIXEL_SPACING entries in metadata")
	}

	wBase, hBase := pairs[0][0], pairs[0][1]
	mmW := float64(baseW) * wBase
	mmH := float64(baseH) * hBase

	out := make([][2]int, 0, len(pairs)-1)
	for _, p := range pairs[1:] {
		w := int(math.Ceil(mmW / p[0]))
		h := int(math.Ceil(mmH / p[1]))
		out = append(out, [2]int{w, h})
	}
	return out, nil
}
