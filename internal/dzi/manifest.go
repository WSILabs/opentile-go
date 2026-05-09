package dzi

import (
	"encoding/xml"
	"errors"
	"fmt"
)

// Namespace is the Microsoft Deep Zoom XML namespace declared on
// the root Image element of a DZI manifest.
const Namespace = "http://schemas.microsoft.com/deepzoom/2008"

// Manifest is a parsed DZI manifest XML document.
//
// All fields come from the XML attributes on the root <Image>
// element + its single child <Size> element. Width and Height are
// the full-resolution image dimensions.
type Manifest struct {
	Format   string // "jpeg" or "png"; spec restricts to these two
	Overlap  int    // tile-edge overlap in pixels; typically 0
	TileSize int    // standard tile dimension; typically 256
	Width    int    // image width at the deepest pyramid level
	Height   int    // image height at the deepest pyramid level
}

// rawImage is the wire representation parsed from XML.
type rawImage struct {
	XMLName  xml.Name `xml:"Image"`
	Format   string   `xml:"Format,attr"`
	Overlap  int      `xml:"Overlap,attr"`
	TileSize int      `xml:"TileSize,attr"`
	Size     rawSize  `xml:"Size"`
}

type rawSize struct {
	Width  int `xml:"Width,attr"`
	Height int `xml:"Height,attr"`
}

// ParseManifest decodes a DZI manifest XML document.
//
// Returns an error if the XML is malformed, the root element is
// not <Image>, the namespace is wrong, or required fields are
// missing/zero.
func ParseManifest(data []byte) (Manifest, error) {
	var raw rawImage
	if err := xml.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("dzi: parse manifest: %w", err)
	}
	if raw.XMLName.Local != "Image" {
		return Manifest{}, fmt.Errorf("dzi: root element %q, want Image", raw.XMLName.Local)
	}
	if raw.XMLName.Space != Namespace {
		return Manifest{}, fmt.Errorf("dzi: namespace %q, want %q", raw.XMLName.Space, Namespace)
	}
	if raw.Format == "" {
		return Manifest{}, errors.New("dzi: missing Format attribute")
	}
	if raw.TileSize <= 0 {
		return Manifest{}, fmt.Errorf("dzi: TileSize %d must be > 0", raw.TileSize)
	}
	if raw.Size.Width <= 0 || raw.Size.Height <= 0 {
		return Manifest{}, fmt.Errorf("dzi: Size %dx%d must have positive dimensions", raw.Size.Width, raw.Size.Height)
	}
	if raw.Overlap < 0 {
		return Manifest{}, fmt.Errorf("dzi: Overlap %d must be >= 0", raw.Overlap)
	}
	return Manifest{
		Format:   raw.Format,
		Overlap:  raw.Overlap,
		TileSize: raw.TileSize,
		Width:    raw.Size.Width,
		Height:   raw.Size.Height,
	}, nil
}
