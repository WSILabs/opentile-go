package main

import (
	"encoding/json"
	"time"

	opentile "github.com/wsilabs/opentile-go"
)

// JSON documents returned by ot_metadata_json / ot_*tiff_tags_json. The
// metadata schema is a superset of the one python/cshim produced: existing
// keys keep their meaning; new keys are only ever added.

type jsonLevel struct {
	Size        [2]int  `json:"size"`
	TileSize    [2]int  `json:"tile_size"`
	Grid        [2]int  `json:"grid"`
	Downsample  float64 `json:"downsample"`
	Overlapping bool    `json:"overlapping"`
}

type jsonPyramidLevel struct {
	jsonLevel
	StitchedGrid [2]int      `json:"stitched_grid"`
	OverlapMode  string      `json:"overlap_mode"`
	TileOverlap  [2]int      `json:"tile_overlap"`
	Compression  string      `json:"compression"`
	MPP          *[2]float64 `json:"mpp"`
	FocalPlane   float64     `json:"focal_plane"`
}

type jsonPyramid struct {
	Index  int                `json:"index"`
	Name   string             `json:"name"`
	Levels []jsonPyramidLevel `json:"levels"`
}

type jsonScanner struct {
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Software     []string `json:"software"`
	Serial       string   `json:"serial"`
}

type jsonAssociated struct {
	Index       int    `json:"index"`
	Type        string `json:"type"`
	Size        [2]int `json:"size"`
	Compression string `json:"compression"`
}

type jsonMetadata struct {
	APIVersion          [2]int            `json:"api_version"`
	Format              string            `json:"format"`
	MPP                 *[2]float64       `json:"mpp"`
	Magnification       *float64          `json:"magnification"`
	Scanner             jsonScanner       `json:"scanner"`
	Writer              string            `json:"writer"`
	AcquisitionDateTime *string           `json:"acquisition_datetime"`
	ImageDescription    string            `json:"image_description"`
	Properties          map[string]string `json:"properties"`
	Levels              []jsonLevel       `json:"levels"`
	Pyramids            []jsonPyramid     `json:"pyramids"`
	Associated          []string          `json:"associated"`
	AssociatedImages    []jsonAssociated  `json:"associated_images"`
}

func mppPtr(m opentile.MPP) *[2]float64 {
	if m.X == 0 && m.Y == 0 {
		return nil
	}
	return &[2]float64{m.X, m.Y}
}

func levelJSON(l *opentile.Level) jsonLevel {
	return jsonLevel{
		Size:        [2]int{l.Size.W, l.Size.H},
		TileSize:    [2]int{l.TileSize.W, l.TileSize.H},
		Grid:        [2]int{l.Grid.W, l.Grid.H},
		Downsample:  l.Downsample,
		Overlapping: l.Overlapping,
	}
}

func metadataJSON(s *opentile.Slide) ([]byte, error) {
	md := s.Metadata()
	doc := jsonMetadata{
		APIVersion: [2]int{apiVersionMajor, apiVersionMinor},
		Format:     string(s.Format()),
		MPP:        mppPtr(md.MPP),
		Scanner: jsonScanner{
			Manufacturer: md.ScannerManufacturer,
			Model:        md.ScannerModel,
			Software:     md.ScannerSoftware,
			Serial:       md.ScannerSerial,
		},
		Writer:           md.Writer,
		ImageDescription: md.ImageDescription,
		Properties:       md.Properties,
		Levels:           []jsonLevel{},
		Pyramids:         []jsonPyramid{},
		Associated:       []string{},
		AssociatedImages: []jsonAssociated{},
	}
	if doc.Scanner.Software == nil {
		doc.Scanner.Software = []string{}
	}
	if doc.Properties == nil {
		doc.Properties = map[string]string{} // {} not null
	}
	if md.Magnification != 0 {
		m := md.Magnification
		doc.Magnification = &m
	}
	if !md.AcquisitionDateTime.IsZero() {
		t := md.AcquisitionDateTime.Format(time.RFC3339Nano)
		doc.AcquisitionDateTime = &t
	}
	for _, l := range s.Levels() {
		doc.Levels = append(doc.Levels, levelJSON(l))
	}
	for _, p := range s.Pyramids() {
		jp := jsonPyramid{Index: p.Index, Name: p.Name, Levels: []jsonPyramidLevel{}}
		for i := range p.Levels {
			l := &p.Levels[i]
			sg := l.StitchedGrid()
			jp.Levels = append(jp.Levels, jsonPyramidLevel{
				jsonLevel:    levelJSON(l),
				StitchedGrid: [2]int{sg.W, sg.H},
				OverlapMode:  l.OverlapMode.String(),
				TileOverlap:  [2]int{l.TileOverlap.X, l.TileOverlap.Y},
				Compression:  l.Compression.String(),
				MPP:          mppPtr(l.MPP),
				FocalPlane:   l.FocalPlane,
			})
		}
		doc.Pyramids = append(doc.Pyramids, jp)
	}
	for i, a := range s.AssociatedImages() {
		sz := a.Size()
		doc.Associated = append(doc.Associated, string(a.Type()))
		doc.AssociatedImages = append(doc.AssociatedImages, jsonAssociated{
			Index:       i,
			Type:        string(a.Type()),
			Size:        [2]int{sz.W, sz.H},
			Compression: a.Compression().String(),
		})
	}
	return json.Marshal(doc)
}

type jsonRational [2]uint32

type jsonTag struct {
	Number    uint16         `json:"number"`
	Name      string         `json:"name"`
	Type      uint16         `json:"type"`
	Count     int            `json:"count"`
	ASCII     *string        `json:"ascii,omitempty"`
	Uints     []uint64       `json:"uints,omitempty"`
	Rationals []jsonRational `json:"rationals,omitempty"`
	Raw       []byte         `json:"raw_b64"` // base64 via encoding/json
}

func tiffTagsJSON(tags opentile.TIFFTags) ([]byte, error) {
	out := make([]jsonTag, 0, len(tags))
	for _, t := range tags {
		jt := jsonTag{Number: t.Number, Name: t.Name, Type: uint16(t.Type), Count: t.Count, Raw: t.Raw}
		if jt.Raw == nil {
			jt.Raw = []byte{}
		}
		if a, ok := t.ASCII(); ok {
			jt.ASCII = &a
		} else if u, ok := t.Uints(); ok {
			jt.Uints = u
		} else if r, ok := t.Rationals(); ok {
			for _, x := range r {
				jt.Rationals = append(jt.Rationals, jsonRational{x.Num, x.Denom})
			}
		}
		out = append(out, jt)
	}
	return json.Marshal(out)
}
