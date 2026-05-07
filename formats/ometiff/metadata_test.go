package ometiff

import (
	"reflect"
	"testing"
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
