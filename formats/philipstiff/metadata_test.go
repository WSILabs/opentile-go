package philipstiff

import (
	"reflect"
	"testing"
	"time"
)

func TestParseMetadataFullPhilips4(t *testing.T) {
	// Sampled from Philips-4.tiff via tifffile.philips_metadata. Contains
	// every TAG upstream cares about, including ACQUISITION_DATETIME and
	// DEVICE_SERIAL_NUMBER (the two fields missing on 3/4 fixtures).
	xml := `<DataObject ObjectType="DPUfsImport">
      <Attribute Name="DICOM_PIXEL_SPACING">"0.00025" "0.00025"</Attribute>
      <Attribute Name="DICOM_ACQUISITION_DATETIME">20160718122300.000000</Attribute>
      <Attribute Name="DICOM_MANUFACTURER">PHILIPS</Attribute>
      <Attribute Name="DICOM_SOFTWARE_VERSIONS">"1.6.6186" "20150402_R48" "4.0.3"</Attribute>
      <Attribute Name="DICOM_DEVICE_SERIAL_NUMBER">FMT0107</Attribute>
      <Attribute Name="DICOM_LOSSY_IMAGE_COMPRESSION_METHOD">"PHILIPS_DP_1_0" "PHILIPS_TIFF_1_0"</Attribute>
      <Attribute Name="DICOM_LOSSY_IMAGE_COMPRESSION_RATIO">"2" "3"</Attribute>
      <Attribute Name="DICOM_BITS_ALLOCATED">8</Attribute>
      <Attribute Name="DICOM_BITS_STORED">8</Attribute>
      <Attribute Name="DICOM_HIGH_BIT">7</Attribute>
      <Attribute Name="DICOM_PIXEL_REPRESENTATION">0</Attribute>
    </DataObject>`
	md, err := parseMetadata(xml)
	if err != nil {
		t.Fatalf("parseMetadata: %v", err)
	}
	if md.ScannerManufacturer != "PHILIPS" {
		t.Errorf("ScannerManufacturer: got %q, want %q", md.ScannerManufacturer, "PHILIPS")
	}
	wantSW := []string{"1.6.6186", "20150402_R48", "4.0.3"}
	if !reflect.DeepEqual(md.ScannerSoftware, wantSW) {
		t.Errorf("ScannerSoftware: got %v, want %v", md.ScannerSoftware, wantSW)
	}
	if md.ScannerSerial != "FMT0107" {
		t.Errorf("ScannerSerial: got %q, want %q", md.ScannerSerial, "FMT0107")
	}
	wantTime := time.Date(2016, 7, 18, 12, 23, 0, 0, time.UTC)
	if !md.AcquisitionDateTime.Equal(wantTime) {
		t.Errorf("AcquisitionDateTime: got %v, want %v", md.AcquisitionDateTime, wantTime)
	}
	if md.PixelSpacing != [2]float64{0.00025, 0.00025} {
		t.Errorf("PixelSpacing: got %v, want [0.00025, 0.00025]", md.PixelSpacing)
	}
	wantMethods := []string{"PHILIPS_DP_1_0", "PHILIPS_TIFF_1_0"}
	if !reflect.DeepEqual(md.LossyImageCompressionMethod, wantMethods) {
		t.Errorf("LossyImageCompressionMethod: got %v, want %v", md.LossyImageCompressionMethod, wantMethods)
	}
	if md.LossyImageCompressionRatio != 2 {
		t.Errorf("LossyImageCompressionRatio: got %v, want 2", md.LossyImageCompressionRatio)
	}
	if md.BitsAllocated != 8 || md.BitsStored != 8 || md.HighBit != 7 {
		t.Errorf("bits: got %d/%d/%d; want 8/8/7", md.BitsAllocated, md.BitsStored, md.HighBit)
	}
	if md.PixelRepresentation != "0" {
		t.Errorf("PixelRepresentation: got %q, want %q", md.PixelRepresentation, "0")
	}
}

func TestParseMetadataPartialPhilips1(t *testing.T) {
	// Sampled from Philips-1.tiff: DICOM_ACQUISITION_DATETIME and
	// DICOM_DEVICE_SERIAL_NUMBER are missing (None upstream); the
	// parser must tolerate this without erroring.
	xml := `<DataObject ObjectType="DPUfsImport">
      <Attribute Name="DICOM_PIXEL_SPACING">"0.000226891" "0.000226907"</Attribute>
      <Attribute Name="DICOM_MANUFACTURER">Hamamatsu</Attribute>
      <Attribute Name="DICOM_SOFTWARE_VERSIONS">"4.0.3"</Attribute>
      <Attribute Name="DICOM_LOSSY_IMAGE_COMPRESSION_METHOD">"PHILIPS_TIFF_1_0"</Attribute>
      <Attribute Name="DICOM_LOSSY_IMAGE_COMPRESSION_RATIO">"3"</Attribute>
      <Attribute Name="DICOM_BITS_ALLOCATED">8</Attribute>
      <Attribute Name="DICOM_BITS_STORED">8</Attribute>
      <Attribute Name="DICOM_HIGH_BIT">7</Attribute>
      <Attribute Name="DICOM_PIXEL_REPRESENTATION">0</Attribute>
    </DataObject>`
	md, err := parseMetadata(xml)
	if err != nil {
		t.Fatalf("parseMetadata: %v", err)
	}
	if md.ScannerManufacturer != "Hamamatsu" {
		t.Errorf("ScannerManufacturer: got %q, want Hamamatsu", md.ScannerManufacturer)
	}
	if md.ScannerSerial != "" {
		t.Errorf("ScannerSerial should be empty when DEVICE_SERIAL_NUMBER absent; got %q", md.ScannerSerial)
	}
	if !md.AcquisitionDateTime.IsZero() {
		t.Errorf("AcquisitionDateTime should be zero when missing; got %v", md.AcquisitionDateTime)
	}
	if md.PixelSpacing != [2]float64{0.000226891, 0.000226907} {
		t.Errorf("PixelSpacing: got %v", md.PixelSpacing)
	}
	if !reflect.DeepEqual(md.ScannerSoftware, []string{"4.0.3"}) {
		t.Errorf("ScannerSoftware: got %v, want [4.0.3]", md.ScannerSoftware)
	}
}

func TestParseMetadataFirstWins(t *testing.T) {
	// Upstream's loop only takes the first non-None value for each tag
	// ("if name in self._tags and self._tags[name] is None"). Subsequent
	// duplicates are ignored.
	xml := `<DataObject>
      <Attribute Name="DICOM_MANUFACTURER">First</Attribute>
      <Attribute Name="DICOM_MANUFACTURER">Second</Attribute>
    </DataObject>`
	md, err := parseMetadata(xml)
	if err != nil {
		t.Fatalf("parseMetadata: %v", err)
	}
	if md.ScannerManufacturer != "First" {
		t.Errorf("ScannerManufacturer: got %q, want %q", md.ScannerManufacturer, "First")
	}
}

func TestParseMetadataMalformedDateZero(t *testing.T) {
	// Garbage timestamp → AcquisitionDateTime stays zero (matches upstream's
	// try/except ValueError → None).
	xml := `<DataObject>
      <Attribute Name="DICOM_ACQUISITION_DATETIME">not-a-date</Attribute>
    </DataObject>`
	md, err := parseMetadata(xml)
	if err != nil {
		t.Fatalf("parseMetadata: %v", err)
	}
	if !md.AcquisitionDateTime.IsZero() {
		t.Errorf("expected zero time on malformed DICOM_ACQUISITION_DATETIME; got %v", md.AcquisitionDateTime)
	}
}

func TestParseMetadataIgnoresUnknownTags(t *testing.T) {
	// Tags upstream doesn't extract are silently skipped.
	xml := `<DataObject>
      <Attribute Name="DICOM_MANUFACTURER">PHILIPS</Attribute>
      <Attribute Name="DICOM_SOMETHING_ELSE">x</Attribute>
    </DataObject>`
	md, err := parseMetadata(xml)
	if err != nil {
		t.Fatalf("parseMetadata: %v", err)
	}
	if md.ScannerManufacturer != "PHILIPS" {
		t.Errorf("ScannerManufacturer: got %q, want PHILIPS", md.ScannerManufacturer)
	}
}

func TestParseMetadataMalformedXMLError(t *testing.T) {
	if _, err := parseMetadata("<DataObject><unclosed"); err == nil {
		t.Error("expected error on malformed XML")
	}
}
