package szi

import (
	"testing"
	"time"
)

func TestParseScanProperties_GrundiumFlavored(t *testing.T) {
	const data = `<?xml version="1.0"?>
<image xmlns="http://www.pathozoom.com/SZI" date="2024-01-15" version="1.0">
  <properties>
    <property><name>VendorName</name><value>Grundium</value></property>
    <property><name>ScannerName</name><value>Ocus</value></property>
    <property><name>ScannerSerialNo</name><value>OCUS-1234</value></property>
    <property><name>ObjectiveMagnification</name><value>40</value></property>
    <property><name>MicronsPerPixel</name><value>0.25055239898989901</value></property>
    <property><name>MicronsPerPixelX</name><value>0.25055239898989901</value></property>
    <property><name>MicronsPerPixelY</name><value>0.25055239898989901</value></property>
    <property><name>TimeStart</name><value>2024-01-15T10:30:00</value></property>
    <property><name>TimeEnd</name><value>2024-01-15T10:45:30</value></property>
    <property><name>ElapsedTime</name><value>0h15m30s</value></property>
    <property><name>SoftwareName</name><value>OcusScan</value></property>
    <property><name>SoftwareVersion</name><value>3.1.4</value></property>
    <property><name>UserName</name><value>operator1</value></property>
    <property><name>Comments</name><value>field comment</value></property>
    <property><name>vendor.SerialNumber</name><value>OCUS-1234</value></property>
    <property><name>Grundium.CustomField</name><value>customvalue</value></property>
  </properties>
</image>`

	cross, szim, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}

	// Cross-format opentile.Metadata fields.
	if cross.ScannerManufacturer != "Grundium" {
		t.Errorf("ScannerManufacturer = %q, want Grundium", cross.ScannerManufacturer)
	}
	if cross.ScannerModel != "Ocus" {
		t.Errorf("ScannerModel = %q, want Ocus", cross.ScannerModel)
	}
	if cross.ScannerSerial != "OCUS-1234" {
		t.Errorf("ScannerSerial = %q, want OCUS-1234", cross.ScannerSerial)
	}
	if cross.Magnification != 40 {
		t.Errorf("Magnification = %v, want 40", cross.Magnification)
	}
	wantTimeStart := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !cross.AcquisitionDateTime.Equal(wantTimeStart) {
		t.Errorf("AcquisitionDateTime = %v, want %v", cross.AcquisitionDateTime, wantTimeStart)
	}
	if len(cross.ScannerSoftware) != 1 || cross.ScannerSoftware[0] != "OcusScan 3.1.4" {
		t.Errorf("ScannerSoftware = %v, want [OcusScan 3.1.4]", cross.ScannerSoftware)
	}

	// SZI-specific fields.
	if szim.UserName != "operator1" {
		t.Errorf("UserName = %q", szim.UserName)
	}
	if szim.ElapsedTime != "0h15m30s" {
		t.Errorf("ElapsedTime = %q", szim.ElapsedTime)
	}
	if szim.Comments != "field comment" {
		t.Errorf("Comments = %q", szim.Comments)
	}
	if szim.MicronsPerPixel != 0.25055239898989901 {
		t.Errorf("MicronsPerPixel = %v", szim.MicronsPerPixel)
	}
	wantTimeEnd := time.Date(2024, 1, 15, 10, 45, 30, 0, time.UTC)
	if !szim.TimeEnd.Equal(wantTimeEnd) {
		t.Errorf("TimeEnd = %v, want %v", szim.TimeEnd, wantTimeEnd)
	}
	if szim.SoftwareName != "OcusScan" {
		t.Errorf("SoftwareName = %q", szim.SoftwareName)
	}
	if szim.SoftwareVersion != "3.1.4" {
		t.Errorf("SoftwareVersion = %q", szim.SoftwareVersion)
	}
	if szim.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", szim.Version)
	}
	wantDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !szim.Date.Equal(wantDate) {
		t.Errorf("Date = %v, want %v", szim.Date, wantDate)
	}

	// Embedded cross-format fields accessible via szim.
	if szim.Magnification != 40 {
		t.Errorf("szim.Magnification = %v, want 40", szim.Magnification)
	}

	// Vendor-prefixed properties land in VendorProperties; canonical
	// fields without "." in the name do not.
	if got := szim.VendorProperties["vendor.SerialNumber"]; got != "OCUS-1234" {
		t.Errorf("vendor.SerialNumber = %q", got)
	}
	if got := szim.VendorProperties["Grundium.CustomField"]; got != "customvalue" {
		t.Errorf("Grundium.CustomField = %q", got)
	}
	if _, ok := szim.VendorProperties["VendorName"]; ok {
		t.Errorf("VendorName should not appear in VendorProperties (no dot)")
	}
}

func TestParseScanProperties_MissingFieldsLenient(t *testing.T) {
	const data = `<?xml version="1.0"?>
<image><properties>
<property><name>VendorName</name><value>X</value></property>
</properties></image>`
	cross, szim, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.ScannerManufacturer != "X" {
		t.Errorf("ScannerManufacturer = %q", cross.ScannerManufacturer)
	}
	// Missing fields → zero values; should not error.
	if cross.Magnification != 0 {
		t.Errorf("Magnification = %v, want 0", cross.Magnification)
	}
	if !cross.AcquisitionDateTime.IsZero() {
		t.Errorf("AcquisitionDateTime should be zero, got %v", cross.AcquisitionDateTime)
	}
	if cross.ScannerSoftware != nil {
		t.Errorf("ScannerSoftware should be nil, got %v", cross.ScannerSoftware)
	}
	if szim.VendorProperties == nil {
		t.Error("VendorProperties should be non-nil even when empty")
	}
	if len(szim.VendorProperties) != 0 {
		t.Errorf("VendorProperties = %v, want empty", szim.VendorProperties)
	}
}

func TestParseScanProperties_MicronsAvg(t *testing.T) {
	// Canonical MicronsPerPixel missing → average of X/Y populates
	// szi.Metadata.MicronsPerPixel.
	const data = `<image><properties>
<property><name>MicronsPerPixelX</name><value>0.4</value></property>
<property><name>MicronsPerPixelY</name><value>0.6</value></property>
</properties></image>`
	_, szim, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if szim.MicronsPerPixel != 0.5 {
		t.Errorf("MicronsPerPixel avg = %v, want 0.5", szim.MicronsPerPixel)
	}
	if szim.MicronsPerPixelX != 0.4 {
		t.Errorf("MicronsPerPixelX = %v, want 0.4", szim.MicronsPerPixelX)
	}
	if szim.MicronsPerPixelY != 0.6 {
		t.Errorf("MicronsPerPixelY = %v, want 0.6", szim.MicronsPerPixelY)
	}
}

func TestParseScanProperties_MalformedNumericLenient(t *testing.T) {
	// Malformed numeric values yield zero on that field but don't
	// fail the file load.
	const data = `<image><properties>
<property><name>ObjectiveMagnification</name><value>not-a-number</value></property>
<property><name>VendorName</name><value>OK</value></property>
</properties></image>`
	cross, _, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.Magnification != 0 {
		t.Errorf("Magnification = %v, want 0 on malformed value", cross.Magnification)
	}
	if cross.ScannerManufacturer != "OK" {
		t.Errorf("ScannerManufacturer = %q, want OK", cross.ScannerManufacturer)
	}
}

func TestParseScanProperties_XMLMalformed(t *testing.T) {
	// Outright XML malformation does return an error.
	const data = `<image><not a valid xml`
	if _, _, err := parseScanProperties([]byte(data)); err == nil {
		t.Error("parseScanProperties: want error on malformed XML, got nil")
	}
}
