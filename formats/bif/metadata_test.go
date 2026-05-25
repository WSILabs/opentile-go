package bif

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestMetadataPopulatesIScanFields: Open + Metadata() returns the
// common fields populated from <iScan>; MetadataOf returns BIF-only
// fields (Generation, ScanRes, AOIs, ...) on the same tiler.
func TestMetadataPopulatesIScanFields(t *testing.T) {
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" Magnification="40" ScanRes="0.25" UnitNumber="2000515" BuildVersion="1.1.0.15854" ScanWhitePoint="235" Z-layers="1"><AOI0 Left="297" Top="2323" Right="574" Bottom="2069"/></iScan>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	common := tiler.Metadata()
	if common.ScannerModel != "VENTANA DP 200" {
		t.Errorf("ScannerModel: got %q, want %q", common.ScannerModel, "VENTANA DP 200")
	}
	if common.ScannerManufacturer != "Roche" {
		t.Errorf("ScannerManufacturer: got %q, want %q", common.ScannerManufacturer, "Roche")
	}
	if common.Magnification != 40 {
		t.Errorf("Magnification: got %v, want 40", common.Magnification)
	}
	if common.ScannerSerial != "2000515" {
		t.Errorf("ScannerSerial: got %q, want %q", common.ScannerSerial, "2000515")
	}
	if len(common.ScannerSoftware) != 1 || common.ScannerSoftware[0] != "1.1.0.15854" {
		t.Errorf("ScannerSoftware: got %v, want [1.1.0.15854]", common.ScannerSoftware)
	}
	if common.Writer != "1.1.0.15854" {
		t.Errorf("Writer (v0.20): got %q, want %q", common.Writer, "1.1.0.15854")
	}

	// v0.17 cross-format additions: per-axis MPP populated from
	// ScanRes (single-value applied to both axes); SetMPPSymmetric
	// collapses to the symmetric slot.
	if common.MicronsPerPixelX != 0.25 || common.MicronsPerPixelY != 0.25 {
		t.Errorf("MicronsPerPixelX/Y: got %v / %v, want 0.25 / 0.25", common.MicronsPerPixelX, common.MicronsPerPixelY)
	}
	if common.MicronsPerPixel != 0.25 {
		t.Errorf("MicronsPerPixel: got %v, want 0.25 (BIF reports symmetric pixels)", common.MicronsPerPixel)
	}
	// ImageDescription is now on the cross-format struct (Q4 Option B);
	// access it via field promotion off bm or directly off common.
	if common.ImageDescription != "level=0 mag=40 quality=95" {
		t.Errorf("cross.ImageDescription: got %q", common.ImageDescription)
	}
	// Vendor passthrough: every iScan attribute under "ventana." namespace.
	if got := common.Properties["ventana.ScannerModel"]; got != "VENTANA DP 200" {
		t.Errorf("Properties[ventana.ScannerModel]: got %q, want %q", got, "VENTANA DP 200")
	}
	if got := common.Properties["ventana.ScanRes"]; got != "0.25" {
		t.Errorf("Properties[ventana.ScanRes]: got %q, want %q", got, "0.25")
	}
	if got := common.Properties["ventana.UnitNumber"]; got != "2000515" {
		t.Errorf("Properties[ventana.UnitNumber]: got %q, want %q", got, "2000515")
	}
	// Negative: this fixture has no UserName attribute, so canonical
	// PropertyUserName must be absent.
	if _, ok := common.Properties[opentile.PropertyUserName]; ok {
		t.Errorf("Properties[%s]: present but fixture has no UserName", opentile.PropertyUserName)
	}

	bm, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf: ok=false on a real BIF Tiler")
	}
	if bm.Generation != "spec-compliant" {
		t.Errorf("Generation: got %q, want %q", bm.Generation, "spec-compliant")
	}
	if bm.ScanRes != 0.25 {
		t.Errorf("ScanRes: got %v, want 0.25", bm.ScanRes)
	}
	if !bm.ScanWhitePointPresent {
		t.Error("ScanWhitePointPresent: false, want true")
	}
	if bm.ScanWhitePoint != 235 {
		t.Errorf("ScanWhitePoint: got %d, want 235", bm.ScanWhitePoint)
	}
	if bm.ZLayers != 1 {
		t.Errorf("ZLayers: got %d, want 1", bm.ZLayers)
	}
	if bm.ImageDescription != "level=0 mag=40 quality=95" {
		t.Errorf("ImageDescription: got %q", bm.ImageDescription)
	}
	if len(bm.AOIs) != 1 {
		t.Errorf("AOIs: got %d, want 1", len(bm.AOIs))
	}
}

// TestMetadataLegacyIScanDefaults: a slide without ScannerModel
// reports manufacturer "Roche" + a sensible model fallback, and
// the BIF Generation is "legacy-iscan".
func TestMetadataLegacyIScanDefaults(t *testing.T) {
	xmp := []byte(`<iScan Magnification="40" ScanRes="0.2325" UnitNumber="BI10N0306" BuildVersion="3.3.1.1"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label Image"},
		{description: "level=0 mag=40 quality=90", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	common := tiler.Metadata()
	if common.ScannerModel != "VENTANA iScan" {
		t.Errorf("ScannerModel: got %q, want fallback %q", common.ScannerModel, "VENTANA iScan")
	}
	if common.ScannerManufacturer != "Roche" {
		t.Errorf("ScannerManufacturer: got %q, want %q", common.ScannerManufacturer, "Roche")
	}
	bm, _ := MetadataOf(tiler)
	if bm.Generation != "legacy-iscan" {
		t.Errorf("Generation: got %q, want %q", bm.Generation, "legacy-iscan")
	}
	if bm.ScanWhitePointPresent {
		t.Error("ScanWhitePointPresent: true, want false (legacy fixture has no attribute)")
	}
}

// TestMetadataOfRejectsNonBIFTiler: MetadataOf returns (nil, false)
// for any non-BIF Tiler (mirrors svs.MetadataOf).
func TestMetadataOfRejectsNonBIFTiler(t *testing.T) {
	if md, ok := MetadataOf(nonBIFTiler{}); md != nil || ok {
		t.Errorf("MetadataOf(non-BIF): got (%v, %v), want (nil, false)", md, ok)
	}
}

// nonBIFTiler is a stub so MetadataOf has a non-*Tiler input to reject.
// It only needs to implement UnwrapReader (or not); MetadataOf uses
// UnwrapReader to chain-walk so a bare struct with no UnwrapReader
// terminates the walk and returns (nil, false).
type nonBIFTiler struct{}

// TestMetadataIsCachedNotRecomputed: two consecutive Metadata calls
// return equal common-field structs; MetadataOf returns the same
// pointer.
// TestMetadataMultiZFields covers the v0.7 multi-dim closeout:
// ZSpacing + ZPlaneFoci on bif.Metadata mirror the format-specific
// XMP attribute and bifImage.zPlaneFocus respectively.
func TestMetadataMultiZFields(t *testing.T) {
	const tw, th = 32, 32
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" Z-layers="3" Z-spacing="1.5"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw, imageLength: th, tileWidth: tw, tileLength: th,
			imageDepth: 3,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bm, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf: not BIF tiler")
	}
	if bm.ZLayers != 3 {
		t.Errorf("ZLayers: got %d, want 3", bm.ZLayers)
	}
	if bm.ZSpacing != 1.5 {
		t.Errorf("ZSpacing: got %v, want 1.5", bm.ZSpacing)
	}
	wantFoci := []float64{0, -1.5, +1.5}
	if len(bm.ZPlaneFoci) != len(wantFoci) {
		t.Fatalf("ZPlaneFoci len: got %d, want %d", len(bm.ZPlaneFoci), len(wantFoci))
	}
	for i, want := range wantFoci {
		if bm.ZPlaneFoci[i] != want {
			t.Errorf("ZPlaneFoci[%d]: got %v, want %v", i, bm.ZPlaneFoci[i], want)
		}
	}
}

// TestMetadataSingleZFields: non-volumetric slide reports ZLayers=1,
// ZSpacing=0, ZPlaneFoci=[0] — the single-element table for Z=0
// nominal.
func TestMetadataSingleZFields(t *testing.T) {
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	bm, _ := MetadataOf(tiler)
	if len(bm.ZPlaneFoci) != 1 {
		t.Errorf("ZPlaneFoci len: got %d, want 1 (Z=0 nominal only)", len(bm.ZPlaneFoci))
	}
	if len(bm.ZPlaneFoci) > 0 && bm.ZPlaneFoci[0] != 0 {
		t.Errorf("ZPlaneFoci[0]: got %v, want 0 (nominal)", bm.ZPlaneFoci[0])
	}
}

func TestMetadataIsCached(t *testing.T) {
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	a, _ := MetadataOf(tiler)
	b, _ := MetadataOf(tiler)
	if a != b {
		t.Error("MetadataOf returned different pointers; the second call should hit the cache")
	}
}
