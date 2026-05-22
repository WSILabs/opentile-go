package svs

import (
	"slices"
	"strings"
	"testing"
	"time"

	opentile "github.com/cornish/opentile-go"
)

func TestDetectWriter(t *testing.T) {
	for _, tc := range []struct {
		name             string
		input            string
		wantManufacturer string
		wantModel        string
		wantSoftwares    []string
	}{
		{
			name:             "Aperio canonical",
			input:            "Aperio Image Library v11.2.1",
			wantManufacturer: "Aperio",
			wantModel:        "",
			wantSoftwares:    []string{"Aperio Image Library v11.2.1"},
		},
		{
			name:             "Grundium Ocus (observed)",
			input:            "Aperio Image, Grundium Ocus",
			wantManufacturer: "Grundium",
			wantModel:        "Ocus",
			wantSoftwares:    []string{"Aperio Image", "Grundium Ocus"},
		},
		{
			name:             "Grundium with whitespace",
			input:            "  Aperio Image,  Grundium Ocus  ",
			wantManufacturer: "Grundium",
			wantModel:        "Ocus",
			wantSoftwares:    []string{"Aperio Image", "Grundium Ocus"},
		},
		{
			name:             "Hypothetical multi-word model",
			input:            "Aperio Image, MyVendor Pro 5",
			wantManufacturer: "MyVendor",
			wantModel:        "Pro 5",
			wantSoftwares:    []string{"Aperio Image", "MyVendor Pro 5"},
		},
		{
			name:             "Empty input",
			input:            "",
			wantManufacturer: "",
			wantModel:        "",
			wantSoftwares:    nil,
		},
		{
			name:             "Undetected pattern",
			input:            "SomethingElse v2.0",
			wantManufacturer: "",
			wantModel:        "",
			wantSoftwares:    []string{"SomethingElse v2.0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectWriter(tc.input)
			if got.Manufacturer != tc.wantManufacturer {
				t.Errorf("manufacturer = %q, want %q", got.Manufacturer, tc.wantManufacturer)
			}
			if got.Model != tc.wantModel {
				t.Errorf("model = %q, want %q", got.Model, tc.wantModel)
			}
			if !slices.Equal(got.Softwares, tc.wantSoftwares) {
				t.Errorf("softwares = %v, want %v", got.Softwares, tc.wantSoftwares)
			}
		})
	}
}

func TestParseDescription(t *testing.T) {
	desc := "Aperio Image Library v11.2.1\n" +
		"46000x32914 [0,100 46000x32714] (240x240) JPEG/RGB Q=30|" +
		"AppMag = 20|MPP = 0.4990|Date = 02/02/2017|Time = 11:22:33|" +
		"ScanScope ID = SS1234|Filename = CMU-1"

	md, err := parseDescription(desc)
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	if md.Magnification != 20 {
		t.Errorf("Magnification: got %v, want 20", md.Magnification)
	}
	if md.MPP != 0.499 {
		t.Errorf("MPP: got %v, want 0.499", md.MPP)
	}
	if md.ScannerSerial != "SS1234" {
		t.Errorf("ScannerSerial: got %q, want SS1234", md.ScannerSerial)
	}
	if md.SoftwareLine != "Aperio Image Library v11.2.1" {
		t.Errorf("SoftwareLine: got %q", md.SoftwareLine)
	}
	want := time.Date(2017, 2, 2, 11, 22, 33, 0, time.UTC)
	if !md.AcquisitionDateTime.Equal(want) {
		t.Errorf("AcquisitionDateTime: got %v, want %v", md.AcquisitionDateTime, want)
	}
}

func TestParseDescriptionMissingFields(t *testing.T) {
	md, err := parseDescription("Aperio Image Library v11.2.1\n256x256 (16x16) JPEG/RGB")
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	if md.Magnification != 0 || md.MPP != 0 || md.ScannerSerial != "" {
		t.Errorf("expected zero values for missing fields, got %+v", md)
	}
}

func TestParseDescriptionRejectsNonAperio(t *testing.T) {
	_, err := parseDescription("Hamamatsu Ndpi\n...")
	if err == nil {
		t.Fatal("expected error on non-Aperio description")
	}
}

func TestParseDescriptionRejectsGarbageAppMag(t *testing.T) {
	desc := "Aperio Image Library v1.0\n1x1 (1x1)|AppMag = notanumber"
	_, err := parseDescription(desc)
	if err == nil {
		t.Fatal("expected error on garbage AppMag")
	}
}

func TestParseDescriptionRejectsGarbageMPP(t *testing.T) {
	desc := "Aperio Image Library v1.0\n1x1 (1x1)|MPP = not_a_float"
	_, err := parseDescription(desc)
	if err == nil {
		t.Fatal("expected error on garbage MPP")
	}
}

func TestParseDescriptionTwoDigitYear(t *testing.T) {
	// Real-world Aperio slides (CMU-1-Small-Region.svs, CMU-1.svs, JP2K-33003-1.svs)
	// use two-digit years in Date field.
	desc := "Aperio Image Library v11.2.1\n" +
		"256x256 (16x16) JPEG/RGB|" +
		"Date = 12/29/09|Time = 09:59:15"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	want := time.Date(2009, 12, 29, 9, 59, 15, 0, time.UTC)
	if !md.AcquisitionDateTime.Equal(want) {
		t.Errorf("AcquisitionDateTime: got %v, want %v", md.AcquisitionDateTime, want)
	}
}

func TestParseDescriptionRejectsMalformedDate(t *testing.T) {
	desc := "Aperio Image Library v11.2.1\n1x1 (1x1)|Date = garbage|Time = 09:59:15"
	_, err := parseDescription(desc)
	if err == nil {
		t.Fatal("expected error on malformed date")
	}
}

func TestParseDescriptionTrimsCRLFFromSoftwareLine(t *testing.T) {
	desc := "Aperio Image Library v11.2.1 \r\n46000x32914 [42673,5576 2220x2967] (240x240) JPEG/RGB Q=30"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(md.SoftwareLine, "\r") || strings.HasSuffix(md.SoftwareLine, "\n") {
		t.Errorf("SoftwareLine retains line ending: %q", md.SoftwareLine)
	}
	want := "Aperio Image Library v11.2.1"
	if md.SoftwareLine != want {
		t.Errorf("SoftwareLine: got %q, want %q", md.SoftwareLine, want)
	}
}

// TestParseDescriptionLineEndings exercises every line-ending and
// trailing-whitespace variant we've seen on real Aperio slides plus a
// minimal "no whitespace before newline" case. Each variant must produce
// the same trimmed SoftwareLine. Locks in the L1 fix so future edits to
// parseDescription cannot regress without breaking a test.
func TestParseDescriptionLineEndings(t *testing.T) {
	const wantSoftware = "Aperio v1.0"
	cases := []struct {
		name string
		desc string
	}{
		{
			name: "CRLF",
			desc: "Aperio v1.0 \r\n100x100 [0,0 100x100] (256x256) JPEG/RGB",
		},
		{
			name: "LF only",
			desc: "Aperio v1.0 \n100x100 [0,0 100x100] (256x256) JPEG/RGB",
		},
		{
			name: "trailing whitespace",
			desc: "Aperio v1.0   \n100x100 [0,0 100x100] (256x256) JPEG/RGB",
		},
		{
			name: "no whitespace before newline",
			desc: "Aperio v1.0\n100x100 [0,0 100x100] (256x256) JPEG/RGB",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			md, err := parseDescription(c.desc)
			if err != nil {
				t.Fatal(err)
			}
			if md.SoftwareLine != wantSoftware {
				t.Errorf("SoftwareLine: got %q, want %q", md.SoftwareLine, wantSoftware)
			}
		})
	}
}

// TestParseDescriptionDuplicateKeyLastWins documents the v0.1 parser
// convention for repeated key=value pairs in the Aperio header: the last
// occurrence wins. Locks the convention in so future parser refactors
// cannot silently change semantics.
func TestParseDescriptionDuplicateKeyLastWins(t *testing.T) {
	desc := "Aperio v1.0 \n100x100 [0,0 100x100] (256x256) JPEG/RGB Q=30|MPP = 0.5|MPP = 0.25"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatal(err)
	}
	if md.MPP != 0.25 {
		t.Errorf("duplicate MPP: got %v, want 0.25 (last-wins)", md.MPP)
	}
}

// TestParseDescriptionCrossFormatFields verifies the v0.17 cross-format
// Metadata positions (MicronsPerPixel*, ImageDescription, Properties) are
// populated alongside the SVS-specific fields. Asserts symmetric MPP
// (Aperio reports a single value), the verbatim ImageDescription tag,
// the canonical PropertyUserName slot, and "aperio." vendor passthrough.
func TestParseDescriptionCrossFormatFields(t *testing.T) {
	desc := "Aperio Image Library v11.2.1\n" +
		"46000x32914 [0,100 46000x32714] (240x240) JPEG/RGB Q=30|" +
		"AppMag = 20|MPP = 0.4990|Date = 02/02/2017|Time = 11:22:33|" +
		"ScanScope ID = SS1234|Filename = CMU-1|User = scanner-op-1|" +
		"ICC Profile = ScanScope v1"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	if md.MicronsPerPixelX != 0.499 {
		t.Errorf("MicronsPerPixelX: got %v, want 0.499", md.MicronsPerPixelX)
	}
	if md.MicronsPerPixelY != 0.499 {
		t.Errorf("MicronsPerPixelY: got %v, want 0.499", md.MicronsPerPixelY)
	}
	if md.MicronsPerPixel != 0.499 {
		t.Errorf("MicronsPerPixel (symmetric): got %v, want 0.499", md.MicronsPerPixel)
	}
	if md.ImageDescription != desc {
		t.Errorf("ImageDescription: got %q, want verbatim tag", md.ImageDescription)
	}
	if got := md.Properties[opentile.PropertyUserName]; got != "scanner-op-1" {
		t.Errorf("PropertyUserName: got %q, want scanner-op-1", got)
	}
	if got := md.Properties["aperio.User"]; got != "scanner-op-1" {
		t.Errorf("aperio.User: got %q, want scanner-op-1", got)
	}
	if got := md.Properties["aperio.AppMag"]; got != "20" {
		t.Errorf("aperio.AppMag: got %q, want 20", got)
	}
	if got := md.Properties["aperio.MPP"]; got != "0.4990" {
		t.Errorf("aperio.MPP: got %q, want 0.4990", got)
	}
	if got := md.Properties["aperio.Filename"]; got != "CMU-1" {
		t.Errorf("aperio.Filename: got %q, want CMU-1", got)
	}
	// Key with embedded space round-trips correctly.
	if got := md.Properties["aperio.ICC Profile"]; got != "ScanScope v1" {
		t.Errorf("aperio.ICC Profile: got %q, want ScanScope v1", got)
	}
	// The geometry/codec prelude line that splitKV may capture as a
	// junk "key" must not be surfaced in Properties.
	for k := range md.Properties {
		if strings.ContainsAny(k, "[](),/;\n") {
			t.Errorf("Properties contains junk key %q", k)
		}
	}
}

// TestParseDescriptionAsymmetricMPPNotApplicable documents that Aperio
// only ever reports a single MPP, so MicronsPerPixel is always populated
// (never zero-because-asymmetric) when MPP is present.
func TestParseDescriptionMPPSymmetric(t *testing.T) {
	desc := "Aperio v1.0\n100x100 (256x256) JPEG/RGB Q=30|MPP = 0.25"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatal(err)
	}
	if md.MicronsPerPixel != 0.25 || md.MicronsPerPixelX != 0.25 || md.MicronsPerPixelY != 0.25 {
		t.Errorf("expected MPP=X=Y=0.25, got MPP=%v X=%v Y=%v",
			md.MicronsPerPixel, md.MicronsPerPixelX, md.MicronsPerPixelY)
	}
}

// TestParseDescriptionWriterAperioCanonical verifies Writer field
// population for canonical Aperio (v0.20 addition).
func TestParseDescriptionWriterAperioCanonical(t *testing.T) {
	desc := "Aperio Image Library v11.2.1\n" +
		"46000x32914 [0,100 46000x32714] (240x240) JPEG/RGB Q=30|" +
		"AppMag = 20|MPP = 0.4990"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	if md.Writer != "Aperio Image Library v11.2.1" {
		t.Errorf("Writer = %q, want %q", md.Writer, "Aperio Image Library v11.2.1")
	}
}

// TestParseDescriptionWriterGrundum verifies Writer field for Grundium
// (comma-suffix pattern).
func TestParseDescriptionWriterGrundum(t *testing.T) {
	desc := "Aperio Image, Grundium Ocus\n" +
		"100x100 (256x256) JPEG/RGB Q=30"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatalf("parseDescription: %v", err)
	}
	if md.Writer != "Grundium Ocus" {
		t.Errorf("Writer = %q, want %q", md.Writer, "Grundium Ocus")
	}
}
