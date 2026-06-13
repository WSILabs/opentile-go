package ndpi

import (
	"testing"
	"time"

	opentile "github.com/wsilabs/opentile-go"
)

func TestParseMetadataFromFields(t *testing.T) {
	got := parseFromFields(metadataFields{
		Magnification:          20.0,
		Model:                  "NanoZoomer 2.0-HT",
		DateTime:               "2014:01:07 11:22:33",
		XResolution:            [2]uint32{100000, 1},
		YResolution:            [2]uint32{100000, 1},
		ResolutionUnit:         3,    // centimeters
		ZOffsetFromSlideCenter: 2500, // nm
		Reference:              "SN-1234",
	})
	if got.Magnification != 20 {
		t.Errorf("Magnification: got %v, want 20", got.Magnification)
	}
	if got.ScannerModel != "NanoZoomer 2.0-HT" {
		t.Errorf("Model: got %q", got.ScannerModel)
	}
	want := time.Date(2014, 1, 7, 11, 22, 33, 0, time.UTC)
	if !got.AcquisitionDateTime.Equal(want) {
		t.Errorf("Acq: got %v, want %v", got.AcquisitionDateTime, want)
	}
	if got.SourceLens != 20 {
		t.Errorf("SourceLens: got %v, want 20", got.SourceLens)
	}
	// FocalOffset: nm → mm (divide by 1,000,000).
	if got.FocalOffset != 2.5e-3 {
		t.Errorf("FocalOffset: got %v mm, want 0.0025", got.FocalOffset)
	}
	if got.Reference != "SN-1234" {
		t.Errorf("Reference: got %q, want SN-1234", got.Reference)
	}
	if got.ScannerManufacturer != "Hamamatsu" {
		t.Errorf("ScannerManufacturer: got %q, want Hamamatsu", got.ScannerManufacturer)
	}
}

func TestParseMetadataMissingFields(t *testing.T) {
	// All-zero fields yield zero-value metadata, no errors.
	got := parseFromFields(metadataFields{})
	if got.Magnification != 0 {
		t.Errorf("Magnification: got %v, want 0", got.Magnification)
	}
	if got.ScannerManufacturer != "Hamamatsu" {
		t.Errorf("ScannerManufacturer: got %q", got.ScannerManufacturer)
	}
	if !got.AcquisitionDateTime.IsZero() {
		t.Errorf("AcquisitionDateTime: got %v, want zero", got.AcquisitionDateTime)
	}
	if !got.MPP.IsZero() {
		t.Errorf("MPP: got %v, want zero", got.MPP)
	}
	if got.ImageDescription != "" {
		t.Errorf("ImageDescription: got %q, want empty", got.ImageDescription)
	}
}

// TestParseMetadataCrossFormatMPPAsymmetric exercises the v0.17 MPP
// computation on real-fixture-shaped inputs. NDPI fixtures observed
// in the suite have asymmetric per-axis pixel size (CMU-1.ndpi
// X≈0.4564, Y≈0.4551), so MPP.Symmetric() reports 0 while X/Y carry
// the true values.
func TestParseMetadataCrossFormatMPPAsymmetric(t *testing.T) {
	got := parseFromFields(metadataFields{
		Magnification:  20.0,
		XResolution:    [2]uint32{21910, 1}, // CMU-1.ndpi values
		YResolution:    [2]uint32{21975, 1},
		ResolutionUnit: 3, // centimeters
	})
	// 10000 / (21910/1) ≈ 0.45641
	if got.MPP.X < 0.456 || got.MPP.X > 0.457 {
		t.Errorf("MPP.X: got %v, want ≈0.4564", got.MPP.X)
	}
	if got.MPP.Y < 0.455 || got.MPP.Y > 0.456 {
		t.Errorf("MPP.Y: got %v, want ≈0.4551", got.MPP.Y)
	}
	if got.MPP.Symmetric() != 0 {
		t.Errorf("MPP.Symmetric() (asymmetric): got %v, want 0", got.MPP.Symmetric())
	}
}

// TestParseMetadataCrossFormatMPPSymmetric exercises the symmetric
// path (rare on real NDPI but covered for correctness).
func TestParseMetadataCrossFormatMPPSymmetric(t *testing.T) {
	got := parseFromFields(metadataFields{
		XResolution:    [2]uint32{20000, 1},
		YResolution:    [2]uint32{20000, 1},
		ResolutionUnit: 3,
	})
	if got.MPP.X != 0.5 || got.MPP.Y != 0.5 {
		t.Errorf("expected MPP.X=MPP.Y=0.5, got X=%v Y=%v", got.MPP.X, got.MPP.Y)
	}
	if got.MPP.Symmetric() != 0.5 {
		t.Errorf("MPP.Symmetric() = %v, want 0.5", got.MPP.Symmetric())
	}
}

// TestParseMetadataCrossFormatVendorAndSerial verifies the cross-format
// ScannerSerial slot is populated from the NDPI Reference tag (real
// fixture: OS-2.ndpi Reference="870003"), and that vendor passthrough
// surfaces the parsed Hamamatsu fields under "hamamatsu.".
func TestParseMetadataCrossFormatVendorAndSerial(t *testing.T) {
	got := parseFromFields(metadataFields{
		Magnification:          40.0,
		Reference:              "870003",
		ZOffsetFromSlideCenter: 2500,
	})
	if got.ScannerSerial != "870003" {
		t.Errorf("ScannerSerial: got %q, want 870003", got.ScannerSerial)
	}
	if v := got.Properties["hamamatsu.Reference"]; v != "870003" {
		t.Errorf("hamamatsu.Reference: got %q, want 870003", v)
	}
	if v := got.Properties["hamamatsu.SourceLens"]; v != "40" {
		t.Errorf("hamamatsu.SourceLens: got %q, want 40", v)
	}
	if v := got.Properties["hamamatsu.FocalOffsetMM"]; v == "" {
		t.Errorf("hamamatsu.FocalOffsetMM: missing")
	}
	// PropertyUserName is unused by NDPI (no User-equivalent vendor tag);
	// document that absence so a future refactor that incorrectly adds
	// such a mapping breaks this test.
	if _, ok := got.Properties[opentile.PropertyUserName]; ok {
		t.Errorf("PropertyUserName: NDPI must not populate this; got %q",
			got.Properties[opentile.PropertyUserName])
	}
}

// TestParseMetadataMPPRequiresCMUnit documents that MPP computation
// only fires when ResolutionUnit == 3 (centimeters). Real NDPI
// fixtures all use cm units; other units would produce wrong-by-orders-
// of-magnitude values, so the safer behavior is "skip and leave zero."
func TestParseMetadataMPPRequiresCMUnit(t *testing.T) {
	got := parseFromFields(metadataFields{
		XResolution:    [2]uint32{20000, 1},
		YResolution:    [2]uint32{20000, 1},
		ResolutionUnit: 2, // inches — not what NDPI uses
	})
	if !got.MPP.IsZero() {
		t.Errorf("MPP must be zero when ResolutionUnit != 3, got %v", got.MPP)
	}
}

// TestParseMetadataWriter verifies Writer field population from
// Model field (v0.20 addition).
func TestParseMetadataWriter(t *testing.T) {
	got := parseFromFields(metadataFields{
		Magnification: 20.0,
		Model:         "NanoZoomer 2.0-HT",
		DateTime:      "2014:01:07 11:22:33",
	})
	if got.Writer != "NanoZoomer 2.0-HT" {
		t.Errorf("Writer = %q, want %q", got.Writer, "NanoZoomer 2.0-HT")
	}
}

// TestParseMetadataWriterEmpty verifies Writer remains empty when
// Model is absent.
func TestParseMetadataWriterEmpty(t *testing.T) {
	got := parseFromFields(metadataFields{
		Magnification: 20.0,
	})
	if got.Writer != "" {
		t.Errorf("Writer = %q, want empty", got.Writer)
	}
}
