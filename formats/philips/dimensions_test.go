package philips

import (
	"reflect"
	"testing"
)

// TestComputeCorrectedSizes confirms the per-tiled-page (W, H) computation
// matches tifffile._philips_load_pages: the first DICOM_PIXEL_SPACING
// entry calibrates the baseline mm scale; each subsequent entry produces
// a corrected size for the next tiled page in document order. So N PS
// entries yield N-1 corrected sizes.
func TestComputeCorrectedSizes(t *testing.T) {
	xml := `<DataObject>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000247746" "0.000247746"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000495492" "0.000495492"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000990984" "0.000990984"</Attribute>
    </DataObject>`
	got, err := computeCorrectedSizes(xml, 4096, 3072)
	if err != nil {
		t.Fatalf("computeCorrectedSizes: %v", err)
	}
	want := [][2]int{
		{2048, 1536}, // PS[1] applied to first tiled page
		{1024, 768},  // PS[2] applied to second tiled page
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sizes: got %v, want %v", got, want)
	}
}

// TestComputeCorrectedSizesRagged checks ceil math on a non-power-of-2
// ratio — base 100×100, mm = 0.01, ratio 0.0003 → ceil(0.01/0.0003) = 34.
func TestComputeCorrectedSizesRagged(t *testing.T) {
	xml := `<DataObject>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.0001" "0.0001"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.0003" "0.0003"</Attribute>
    </DataObject>`
	got, err := computeCorrectedSizes(xml, 100, 100)
	if err != nil {
		t.Fatalf("computeCorrectedSizes: %v", err)
	}
	want := [][2]int{{34, 34}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestComputeCorrectedSizesAsymmetric exercises a level whose w and h
// scale differently — upstream applies them independently.
func TestComputeCorrectedSizesAsymmetric(t *testing.T) {
	xml := `<DataObject>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.0001" "0.0002"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.0002" "0.0008"</Attribute>
    </DataObject>`
	// Base W=200, H=400. mm-W = 200*0.0001 = 0.02; mm-H = 400*0.0002 = 0.08.
	// Corrected[0]: W = ceil(0.02 / 0.0002) = 100; H = ceil(0.08 / 0.0008) = 100.
	got, err := computeCorrectedSizes(xml, 200, 400)
	if err != nil {
		t.Fatalf("computeCorrectedSizes: %v", err)
	}
	want := [][2]int{{100, 100}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestComputeCorrectedSizesNoEntries — XML with zero DICOM_PIXEL_SPACING
// elements is a malformed Philips slide; upstream asserts. We surface
// an error.
func TestComputeCorrectedSizesNoEntries(t *testing.T) {
	xml := `<DataObject><Attribute Name="DICOM_OTHER">x</Attribute></DataObject>`
	if _, err := computeCorrectedSizes(xml, 100, 100); err == nil {
		t.Error("expected error on missing DICOM_PIXEL_SPACING")
	}
}

// TestComputeCorrectedSizesBaseOnly — single DICOM_PIXEL_SPACING entry
// calibrates the baseline but produces zero corrected sizes (no levels
// described). Result is an empty slice (not an error).
func TestComputeCorrectedSizesBaseOnly(t *testing.T) {
	xml := `<DataObject>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.0001" "0.0001"</Attribute>
    </DataObject>`
	got, err := computeCorrectedSizes(xml, 256, 128)
	if err != nil {
		t.Fatalf("computeCorrectedSizes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty slice", got)
	}
}

// TestComputeCorrectedSizesMalformedXML — broken XML returns an error,
// not a panic.
func TestComputeCorrectedSizesMalformedXML(t *testing.T) {
	if _, err := computeCorrectedSizes("<DataObject><unclosed", 100, 100); err == nil {
		t.Error("expected parse error on malformed XML")
	}
}

// TestComputeCorrectedSizesPhilips1Real cross-checks against tifffile's
// output for sample_files/philips-tiff/Philips-1.tiff. Encoded inputs
// are extracted from the real fixture so this test does not need the
// fixture present at runtime — it validates the algorithm matches
// upstream byte-for-byte on real DICOM-XML and real raw page-0 dims.
func TestComputeCorrectedSizesPhilips1Real(t *testing.T) {
	// First 4 DICOM_PIXEL_SPACING entries from Philips-1.tiff. The
	// fixture has ~9 entries total covering the full 8-level pyramid;
	// we cover the first 3 corrected levels here to keep the test data
	// inline and self-explanatory.
	xml := `<DataObject>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000226891" "0.000226907"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000227273" "0.000227273"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000454545" "0.000454545"</Attribute>
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000909091" "0.000909091"</Attribute>
    </DataObject>`
	// Raw on-disk page-0 dims: 45056 × 35840. tifffile-corrected per-level
	// dims: L0=44981×35783, L1=22491×17892, L2=11246×8946.
	got, err := computeCorrectedSizes(xml, 45056, 35840)
	if err != nil {
		t.Fatalf("computeCorrectedSizes: %v", err)
	}
	want := [][2]int{
		{44981, 35783},
		{22491, 17892},
		{11246, 8946},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Philips-1 corrected sizes:\n  got  %v\n  want %v", got, want)
	}
}
