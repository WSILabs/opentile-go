package szi

import (
	"testing"
	"time"

	opentile "github.com/wsilabs/opentile-go"
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
    <property><name>ScannedArea</name><value>123.456</value></property>
    <property><name>CaseNumber</name><value>H-2024-001</value></property>
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

	// v0.20: Writer populated from SoftwareName + SoftwareVersion.
	if cross.Writer != "OcusScan 3.1.4" {
		t.Errorf("Writer = %q, want OcusScan 3.1.4", cross.Writer)
	}

	// v0.17 cross-format MPP: per-axis populated; SetMPPSymmetric
	// collapses to the canonical slot since X == Y.
	if cross.MicronsPerPixelX != 0.25055239898989901 {
		t.Errorf("MicronsPerPixelX = %v, want 0.25055239898989901", cross.MicronsPerPixelX)
	}
	if cross.MicronsPerPixelY != 0.25055239898989901 {
		t.Errorf("MicronsPerPixelY = %v, want 0.25055239898989901", cross.MicronsPerPixelY)
	}
	if cross.MicronsPerPixel != 0.25055239898989901 {
		t.Errorf("MicronsPerPixel = %v, want 0.25055239898989901 (X==Y collapse)", cross.MicronsPerPixel)
	}

	// v0.17 cross-format Properties.
	if got := cross.Properties[opentile.PropertyComments]; got != "field comment" {
		t.Errorf("Properties[Comments] = %q, want %q", got, "field comment")
	}
	if got := cross.Properties[opentile.PropertyUserName]; got != "operator1" {
		t.Errorf("Properties[UserName] = %q, want %q", got, "operator1")
	}
	if got := cross.Properties[opentile.PropertyCaseNumber]; got != "H-2024-001" {
		t.Errorf("Properties[CaseNumber] = %q, want %q", got, "H-2024-001")
	}
	if got := cross.Properties[opentile.PropertyScannedAreaMM2]; got != "123.456" {
		t.Errorf("Properties[ScannedAreaMM2] = %q, want %q", got, "123.456")
	}
	// ElapsedTime "0h15m30s" → 0*3600 + 15*60 + 30 = 930 seconds.
	if got := cross.Properties[opentile.PropertyScanDurationSec]; got != "930" {
		t.Errorf("Properties[ScanDurationSec] = %q, want %q", got, "930")
	}

	// SZI-specific raw fields preserved.
	if szim.ElapsedTime != "0h15m30s" {
		t.Errorf("ElapsedTime = %q (raw form preserved)", szim.ElapsedTime)
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

	// Embedded cross-format fields accessible via promotion.
	if szim.Magnification != 40 {
		t.Errorf("szim.Magnification = %v, want 40", szim.Magnification)
	}
	if szim.MicronsPerPixel != 0.25055239898989901 {
		t.Errorf("szim.MicronsPerPixel (promoted) = %v, want 0.25055239898989901", szim.MicronsPerPixel)
	}
	if got := szim.Properties[opentile.PropertyUserName]; got != "operator1" {
		t.Errorf("szim.Properties[UserName] (promoted) = %q, want operator1", got)
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

func TestParseScanProperties_MicronsAsymmetric(t *testing.T) {
	// Per-axis MPP populates cross.MicronsPerPixelX/Y; when X != Y,
	// SetMPPSymmetric leaves cross.MicronsPerPixel zero (consumers
	// read per-axis values explicitly when the slide is anisotropic).
	const data = `<image><properties>
<property><name>MicronsPerPixelX</name><value>0.4</value></property>
<property><name>MicronsPerPixelY</name><value>0.6</value></property>
</properties></image>`
	cross, _, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.MicronsPerPixelX != 0.4 {
		t.Errorf("MicronsPerPixelX = %v, want 0.4", cross.MicronsPerPixelX)
	}
	if cross.MicronsPerPixelY != 0.6 {
		t.Errorf("MicronsPerPixelY = %v, want 0.6", cross.MicronsPerPixelY)
	}
	if cross.MicronsPerPixel != 0 {
		t.Errorf("MicronsPerPixel = %v, want 0 (X != Y, no symmetric collapse)", cross.MicronsPerPixel)
	}
}

func TestParseScanProperties_MicronsCanonicalOnly(t *testing.T) {
	// When only the canonical <MicronsPerPixel> is present (the
	// spec-example CMU-1.szi flavor), it propagates to per-axis
	// X/Y, then SetMPPSymmetric collapses to the canonical slot.
	const data = `<image><properties>
<property><name>MicronsPerPixel</name><value>0.402</value></property>
</properties></image>`
	cross, _, err := parseScanProperties([]byte(data))
	if err != nil {
		t.Fatalf("parseScanProperties: %v", err)
	}
	if cross.MicronsPerPixelX != 0.402 {
		t.Errorf("MicronsPerPixelX = %v, want 0.402", cross.MicronsPerPixelX)
	}
	if cross.MicronsPerPixelY != 0.402 {
		t.Errorf("MicronsPerPixelY = %v, want 0.402", cross.MicronsPerPixelY)
	}
	if cross.MicronsPerPixel != 0.402 {
		t.Errorf("MicronsPerPixel = %v, want 0.402 (X==Y collapse)", cross.MicronsPerPixel)
	}
}

func TestParseSZIDuration(t *testing.T) {
	cases := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{"0h17m22s", 1042, true},
		{"1h0m0s", 3600, true},
		{"0h0m30s", 30, true},
		{"0h0m0s", 0, true},
		{"2h30m45s", 2*3600 + 30*60 + 45, true},
		// Malformed:
		{"", 0, false},
		{"17m22s", 0, false},
		{"abc", 0, false},
		{"1:30:00", 0, false},
		{"1h30m", 0, false},
		{"30s", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseSZIDuration(tc.in)
		if ok != tc.wantOK {
			t.Errorf("parseSZIDuration(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseSZIDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
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
