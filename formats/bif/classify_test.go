package bif

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

func TestClassifyGeneration(t *testing.T) {
	cases := []struct {
		name  string
		iscan *bifxml.IScan
		want  Generation
	}{
		{"nil", nil, GenerationLegacyIScan},
		{"empty-model", &bifxml.IScan{}, GenerationLegacyIScan},
		{"dp200", &bifxml.IScan{ScannerModel: "VENTANA DP 200"}, GenerationSpecCompliant},
		{"dp600", &bifxml.IScan{ScannerModel: "VENTANA DP 600"}, GenerationSpecCompliant},
		{"dp-bare-prefix", &bifxml.IScan{ScannerModel: "VENTANA DP"}, GenerationSpecCompliant},
		{"dp-no-space", &bifxml.IScan{ScannerModel: "VENTANA DP200"}, GenerationSpecCompliant},
		{"future-dp300", &bifxml.IScan{ScannerModel: "VENTANA DP 300"}, GenerationSpecCompliant},
		{"future-dp600s", &bifxml.IScan{ScannerModel: "VENTANA DP 600S"}, GenerationSpecCompliant},
		{"iscan-coreo", &bifxml.IScan{ScannerModel: "VENTANA iScan Coreo"}, GenerationLegacyIScan},
		{"iscan-ht", &bifxml.IScan{ScannerModel: "VENTANA iScan HT"}, GenerationLegacyIScan},
		{"foreign-scanner", &bifxml.IScan{ScannerModel: "Some Other Scanner"}, GenerationLegacyIScan},
		{"prefix-mismatch-leading-space", &bifxml.IScan{ScannerModel: " VENTANA DP 200"}, GenerationLegacyIScan},
		{"prefix-mismatch-lowercase", &bifxml.IScan{ScannerModel: "ventana dp 200"}, GenerationLegacyIScan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyGeneration(tc.iscan); got != tc.want {
				t.Errorf("classifyGeneration: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerationString(t *testing.T) {
	cases := []struct {
		gen  Generation
		want string
	}{
		{GenerationSpecCompliant, "spec-compliant"},
		{GenerationLegacyIScan, "legacy-iscan"},
		{Generation(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.gen.String(); got != tc.want {
			t.Errorf("Generation(%d).String(): got %q, want %q", tc.gen, got, tc.want)
		}
	}
}

// TestOpenClassifiesSpecCompliant: a synthetic BIF whose IFD-0 XMP
// has `<iScan ScannerModel="VENTANA DP 200"/>` opens with
// gen == GenerationSpecCompliant.
func TestOpenClassifiesSpecCompliant(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 256, imageLength: 256, tileWidth: 256, tileLength: 256},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bt := tiler
	if bt.gen != GenerationSpecCompliant {
		t.Errorf("gen: got %v, want GenerationSpecCompliant", bt.gen)
	}
	if bt.iscan == nil {
		t.Fatal("iscan: got nil, want parsed *IScan")
	}
	if bt.iscan.ScannerModel != "VENTANA DP 200" {
		t.Errorf("iscan.ScannerModel: got %q, want %q", bt.iscan.ScannerModel, "VENTANA DP 200")
	}
}

// TestOpenClassifiesLegacy: a synthetic BIF whose IFD-0 XMP has
// `<iScan>` with no ScannerModel attribute opens with
// gen == GenerationLegacyIScan.
func TestOpenClassifiesLegacy(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan Magnification="40"/>`), description: "Label Image"},
		{description: "level=0 mag=40 quality=90", imageWidth: 256, imageLength: 256, tileWidth: 256, tileLength: 256},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bt := tiler
	if bt.gen != GenerationLegacyIScan {
		t.Errorf("gen: got %v, want GenerationLegacyIScan", bt.gen)
	}
}
