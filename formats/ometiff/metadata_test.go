package ometiff

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// TestParseOMEMetadataLeica1: Leica-1.ome.tiff carries 2 Images
// (macro + 1 main pyramid). PhysicalSize values are sampled from the
// real fixture's OME-XML.
func TestParseOMEMetadataLeica1(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"
     xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
     Creator="OME Bio-Formats 6.0.0-rc1">
  <Image ID="Image:0" Name="macro">
    <Pixels BigEndian="false" DimensionOrder="XYCZT" ID="Pixels:0"
            PhysicalSizeX="16.438446163366336" PhysicalSizeXUnit="µm"
            PhysicalSizeY="16.438446015424162" PhysicalSizeYUnit="µm"
            SizeC="3" SizeT="1" SizeX="1616" SizeY="4668" SizeZ="1"
            Type="uint8"/>
  </Image>
  <Image ID="Image:1" Name="">
    <Pixels BigEndian="false" DimensionOrder="XYCZT" ID="Pixels:1"
            PhysicalSizeX="0.5" PhysicalSizeXUnit="µm"
            PhysicalSizeY="0.5" PhysicalSizeYUnit="µm"
            SizeC="3" SizeT="1" SizeX="36832" SizeY="38432" SizeZ="1"
            Type="uint8"/>
  </Image>
</OME>`
	md, err := parseOMEMetadata(xml)
	if err != nil {
		t.Fatalf("parseOMEMetadata: %v", err)
	}
	if len(md.Images) != 2 {
		t.Fatalf("Images: got %d, want 2", len(md.Images))
	}
	want := []OMEImage{
		{Name: "macro", PhysicalSizeX: 16.438446163366336, PhysicalSizeY: 16.438446015424162, PhysicalSizeXUnit: "µm", PhysicalSizeYUnit: "µm", SizeX: 1616, SizeY: 4668, SizeZ: 1, SizeC: 3, SizeT: 1, ChannelNames: []string{}, Type: "uint8"},
		{Name: "", PhysicalSizeX: 0.5, PhysicalSizeY: 0.5, PhysicalSizeXUnit: "µm", PhysicalSizeYUnit: "µm", SizeX: 36832, SizeY: 38432, SizeZ: 1, SizeC: 3, SizeT: 1, ChannelNames: []string{}, Type: "uint8"},
	}
	if !reflect.DeepEqual(md.Images, want) {
		t.Errorf("Images mismatch:\n  got  %+v\n  want %+v", md.Images, want)
	}
}

// TestParseOMEMetadataLeica2: Leica-2 has 5 Images (macro + 4 main
// pyramids). The 4 main images all have Name="" — the v0.6 multi-image
// API exposes them all.
func TestParseOMEMetadataLeica2(t *testing.T) {
	xml := `<?xml version="1.0"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06">
  <Image Name="macro"><Pixels PhysicalSizeX="16.4" PhysicalSizeXUnit="µm" PhysicalSizeY="16.4" PhysicalSizeYUnit="µm" SizeX="1616" SizeY="4668" Type="uint8"/></Image>
  <Image Name=""><Pixels PhysicalSizeX="0.25" PhysicalSizeXUnit="µm" PhysicalSizeY="0.25" PhysicalSizeYUnit="µm" SizeX="39168" SizeY="26048" Type="uint8"/></Image>
  <Image Name=""><Pixels PhysicalSizeX="0.25" PhysicalSizeXUnit="µm" PhysicalSizeY="0.25" PhysicalSizeYUnit="µm" SizeX="39360" SizeY="23360" Type="uint8"/></Image>
  <Image Name=""><Pixels PhysicalSizeX="0.25" PhysicalSizeXUnit="µm" PhysicalSizeY="0.25" PhysicalSizeYUnit="µm" SizeX="39360" SizeY="23360" Type="uint8"/></Image>
  <Image Name=""><Pixels PhysicalSizeX="0.25" PhysicalSizeXUnit="µm" PhysicalSizeY="0.25" PhysicalSizeYUnit="µm" SizeX="39168" SizeY="26048" Type="uint8"/></Image>
</OME>`
	md, err := parseOMEMetadata(xml)
	if err != nil {
		t.Fatalf("parseOMEMetadata: %v", err)
	}
	if len(md.Images) != 5 {
		t.Fatalf("Images: got %d, want 5", len(md.Images))
	}
	if md.Images[0].Name != "macro" {
		t.Errorf("Images[0].Name: got %q, want %q", md.Images[0].Name, "macro")
	}
	for i := 1; i < 5; i++ {
		if md.Images[i].Name != "" {
			t.Errorf("Images[%d].Name: got %q, want empty (main pyramid)", i, md.Images[i].Name)
		}
	}
	// Main images alternate dims (39168×26048 / 39360×23360 / 39360×23360 / 39168×26048).
	if md.Images[1].SizeX != 39168 || md.Images[2].SizeX != 39360 {
		t.Errorf("main image SizeX inventory wrong: got [%d, %d], want [39168, 39360]", md.Images[1].SizeX, md.Images[2].SizeX)
	}
}

// TestParseOMEMetadataMissingFields: tolerate Images that omit
// PhysicalSize attributes — return zero values rather than erroring.
func TestParseOMEMetadataMissingFields(t *testing.T) {
	xml := `<?xml version="1.0"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06">
  <Image Name="">
    <Pixels SizeX="100" SizeY="200" Type="uint8"/>
  </Image>
</OME>`
	md, err := parseOMEMetadata(xml)
	if err != nil {
		t.Fatalf("parseOMEMetadata: %v", err)
	}
	if len(md.Images) != 1 {
		t.Fatalf("Images: got %d, want 1", len(md.Images))
	}
	if md.Images[0].PhysicalSizeX != 0 || md.Images[0].PhysicalSizeY != 0 {
		t.Errorf("expected zero PhysicalSize on missing attrs, got X=%v Y=%v",
			md.Images[0].PhysicalSizeX, md.Images[0].PhysicalSizeY)
	}
	if md.Images[0].SizeX != 100 || md.Images[0].SizeY != 200 {
		t.Errorf("SizeX/Y: got %d/%d, want 100/200", md.Images[0].SizeX, md.Images[0].SizeY)
	}
}

// TestParseOMEMetadataMalformedXML: malformed XML returns an error,
// not a panic.
func TestParseOMEMetadataMalformedXML(t *testing.T) {
	if _, err := parseOMEMetadata("<OME><unclosed"); err == nil {
		t.Error("expected parse error on malformed XML")
	}
}

// TestParseOMEMetadataEmpty: zero Image elements is a malformed OME
// document; surface as an error.
func TestParseOMEMetadataEmpty(t *testing.T) {
	xml := `<?xml version="1.0"?><OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"></OME>`
	if _, err := parseOMEMetadata(xml); err == nil {
		t.Error("expected error on zero-Image OME doc")
	}
}

// TestParseOMEMetadataExtendedFields: v0.17 — verify that Description,
// AcquisitionDate, ObjectiveSettings, and OME root attributes (Creator,
// UUID) + flattened Objectives all round-trip through parseOMEMetadata.
func TestParseOMEMetadataExtendedFields(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"
     Creator="OME Bio-Formats 6.0.0-rc1"
     UUID="urn:uuid:db30c0bc-b59a-425e-9e2c-bf9bb1dc2cb5">
  <Instrument ID="Instrument:0">
    <Objective CalibratedMagnification="0.60833" ID="Objective:0:0" LensNA="0.7" NominalMagnification="0.60833"/>
    <Objective CalibratedMagnification="20.0" ID="Objective:0:1" LensNA="0.4" NominalMagnification="20.0"/>
  </Instrument>
  <Image ID="Image:0" Name="macro">
    <AcquisitionDate>2011-05-31T09:33:14.310</AcquisitionDate>
    <Description>Collection ImageCollection_0000000128</Description>
    <ObjectiveSettings ID="Objective:0:0"/>
    <Pixels PhysicalSizeX="16.4" PhysicalSizeXUnit="µm"
            PhysicalSizeY="16.4" PhysicalSizeYUnit="µm"
            SizeX="1616" SizeY="4668" Type="uint8"/>
  </Image>
  <Image ID="Image:1" Name="">
    <AcquisitionDate>2011-05-31T09:43:06.873</AcquisitionDate>
    <Description>Collection ImageCollection_0000000128</Description>
    <ObjectiveSettings ID="Objective:0:1"/>
    <Pixels PhysicalSizeX="0.5" PhysicalSizeXUnit="µm"
            PhysicalSizeY="0.5" PhysicalSizeYUnit="µm"
            SizeX="36832" SizeY="38432" Type="uint8"/>
  </Image>
</OME>`
	md, err := parseOMEMetadata(xml)
	if err != nil {
		t.Fatalf("parseOMEMetadata: %v", err)
	}
	if md.Creator != "OME Bio-Formats 6.0.0-rc1" {
		t.Errorf("Creator: got %q, want %q", md.Creator, "OME Bio-Formats 6.0.0-rc1")
	}
	if md.UUID != "urn:uuid:db30c0bc-b59a-425e-9e2c-bf9bb1dc2cb5" {
		t.Errorf("UUID: got %q, want urn:uuid:db30c0bc-b59a-425e-9e2c-bf9bb1dc2cb5", md.UUID)
	}
	if len(md.Objectives) != 2 {
		t.Fatalf("Objectives: got %d, want 2", len(md.Objectives))
	}
	if md.Objectives[1].ID != "Objective:0:1" || md.Objectives[1].NominalMagnification != 20.0 {
		t.Errorf("Objectives[1]: got %+v, want {Objective:0:1, NominalMag=20.0}", md.Objectives[1])
	}
	if md.Images[1].Description != "Collection ImageCollection_0000000128" {
		t.Errorf("Images[1].Description: got %q", md.Images[1].Description)
	}
	if md.Images[1].AcquisitionDate != "2011-05-31T09:43:06.873" {
		t.Errorf("Images[1].AcquisitionDate: got %q", md.Images[1].AcquisitionDate)
	}
	if md.Images[1].ObjectiveSettingsID != "Objective:0:1" {
		t.Errorf("Images[1].ObjectiveSettingsID: got %q", md.Images[1].ObjectiveSettingsID)
	}
}

// TestCrossMetadataPrimaryImage: v0.17 — crossMetadata picks the FIRST
// main pyramid (LevelImages[0]), not Image[0] which may be macro. Uses
// Leica-1's geometry: macro at 16.4 µm, main at 0.5 µm; cross.MPP must
// reflect 0.5 (the main pyramid).
func TestCrossMetadataPrimaryImage(t *testing.T) {
	om := OMEMetadata{
		Creator: "OME Bio-Formats 6.0.0-rc1",
		UUID:    "urn:uuid:test",
		Objectives: []OMEObjective{
			{ID: "Objective:0:0", NominalMagnification: 0.60833, CalibratedMagnification: 0.60833},
			{ID: "Objective:0:1", NominalMagnification: 20.0, CalibratedMagnification: 20.0},
		},
		Images: []OMEImage{
			{Name: "macro", PhysicalSizeX: 16.4, PhysicalSizeXUnit: "µm", PhysicalSizeY: 16.4, PhysicalSizeYUnit: "µm", ObjectiveSettingsID: "Objective:0:0"},
			{Name: "", PhysicalSizeX: 0.5, PhysicalSizeXUnit: "µm", PhysicalSizeY: 0.5, PhysicalSizeYUnit: "µm",
				Description: "main pyramid", AcquisitionDate: "2011-05-31T09:43:06.873", ObjectiveSettingsID: "Objective:0:1"},
		},
	}
	cls := omeClassification{LevelImages: []int{1}, Macro: 0, Label: -1, Thumbnail: -1}
	md := crossMetadata(om, cls)

	if md.MicronsPerPixelX != 0.5 || md.MicronsPerPixelY != 0.5 {
		t.Errorf("per-axis MPP: got X=%v Y=%v, want 0.5/0.5 (the main pyramid, NOT macro)", md.MicronsPerPixelX, md.MicronsPerPixelY)
	}
	if md.MicronsPerPixel != 0.5 {
		t.Errorf("symmetric MPP: got %v, want 0.5", md.MicronsPerPixel)
	}
	if md.ImageDescription != "main pyramid" {
		t.Errorf("ImageDescription: got %q, want %q", md.ImageDescription, "main pyramid")
	}
	if md.Magnification != 20.0 {
		t.Errorf("Magnification: got %v, want 20.0 (resolved through Objective:0:1)", md.Magnification)
	}
	if md.Properties["ome.creator"] != "OME Bio-Formats 6.0.0-rc1" {
		t.Errorf("Properties[ome.creator]: got %q", md.Properties["ome.creator"])
	}
	if md.Writer != "OME Bio-Formats 6.0.0-rc1" {
		t.Errorf("Writer (v0.20): got %q, want %q", md.Writer, "OME Bio-Formats 6.0.0-rc1")
	}
	if md.Properties["ome.uuid"] != "urn:uuid:test" {
		t.Errorf("Properties[ome.uuid]: got %q", md.Properties["ome.uuid"])
	}
	want := time.Date(2011, 5, 31, 9, 43, 6, 873000000, time.UTC)
	if !md.AcquisitionDateTime.Equal(want) {
		t.Errorf("AcquisitionDateTime: got %v, want %v", md.AcquisitionDateTime, want)
	}
}

// TestCrossMetadataAsymmetricMPP: when X/Y differ, MicronsPerPixel
// stays 0 and consumers consult the per-axis fields.
func TestCrossMetadataAsymmetricMPP(t *testing.T) {
	om := OMEMetadata{
		Images: []OMEImage{{Name: "", PhysicalSizeX: 0.25, PhysicalSizeXUnit: "µm", PhysicalSizeY: 0.30, PhysicalSizeYUnit: "µm"}},
	}
	cls := omeClassification{LevelImages: []int{0}, Macro: -1, Label: -1, Thumbnail: -1}
	md := crossMetadata(om, cls)
	if md.MicronsPerPixelX != 0.25 || md.MicronsPerPixelY != 0.30 {
		t.Errorf("per-axis: got X=%v Y=%v, want 0.25/0.30", md.MicronsPerPixelX, md.MicronsPerPixelY)
	}
	if md.MicronsPerPixel != 0 {
		t.Errorf("symmetric slot: got %v, want 0 (asymmetric)", md.MicronsPerPixel)
	}
}

// TestCrossMetadataMagnificationFallback: prefer NominalMagnification
// when set; fall back to CalibratedMagnification when Nominal is zero.
func TestCrossMetadataMagnificationFallback(t *testing.T) {
	om := OMEMetadata{
		Objectives: []OMEObjective{
			{ID: "Obj:Calib", NominalMagnification: 0, CalibratedMagnification: 0.60833},
			{ID: "Obj:Both", NominalMagnification: 40.0, CalibratedMagnification: 39.5},
		},
		Images: []OMEImage{
			{Name: "", ObjectiveSettingsID: "Obj:Calib"},
			{Name: "", ObjectiveSettingsID: "Obj:Both"},
		},
	}
	cls := omeClassification{LevelImages: []int{0}, Macro: -1, Label: -1, Thumbnail: -1}
	md := crossMetadata(om, cls)
	if md.Magnification != 0.60833 {
		t.Errorf("calibrated fallback: got %v, want 0.60833", md.Magnification)
	}
	cls.LevelImages = []int{1}
	md = crossMetadata(om, cls)
	if md.Magnification != 40.0 {
		t.Errorf("nominal preferred: got %v, want 40.0", md.Magnification)
	}
}

// TestConvertToMicrons: verify the unit table.
func TestConvertToMicrons(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want float64
	}{
		{0.5, "µm", 0.5},
		{0.5, "μm", 0.5}, // U+03BC GREEK SMALL LETTER MU
		{0.5, "um", 0.5},
		{0.5, "", 0.5}, // OME default: µm when unit absent
		{500, "nm", 0.5},
		{0.0005, "mm", 0.5},
		{0.00005, "cm", 0.5},
		{0.5, "Furlong", 0.5}, // unknown unit → assume µm (don't drop the value)
		{0, "µm", 0},
	}
	for _, c := range cases {
		got := convertToMicrons(c.v, c.unit)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("convertToMicrons(%v, %q): got %v, want %v", c.v, c.unit, got, c.want)
		}
	}
}

// TestParseOMETime: cover the layouts we accept.
func TestParseOMETime(t *testing.T) {
	cases := []struct {
		s    string
		want time.Time
	}{
		{"2011-05-31T09:43:06.873", time.Date(2011, 5, 31, 9, 43, 6, 873000000, time.UTC)},
		{"2014-11-26T14:09:07.390Z", time.Date(2014, 11, 26, 14, 9, 7, 390000000, time.UTC)},
		{"2014-11-26T14:09:07", time.Date(2014, 11, 26, 14, 9, 7, 0, time.UTC)},
	}
	for _, c := range cases {
		got, ok := parseOMETime(c.s)
		if !ok {
			t.Errorf("parseOMETime(%q): not parsed", c.s)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseOMETime(%q): got %v, want %v", c.s, got, c.want)
		}
	}
	if _, ok := parseOMETime("not a date"); ok {
		t.Error("parseOMETime(\"not a date\"): unexpectedly succeeded")
	}
}
