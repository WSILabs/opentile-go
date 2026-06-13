package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestBuildMetadata(t *testing.T) {
	l0 := idicom.Instance{
		Manufacturer: "Leica Biosystems", Model: "GT450", Software: "1.0.1",
		Writer: "Leica ScnUtility", ObjectivePower: 40,
		PixelSpacingX: 0.00105105, PixelSpacingY: 0.00105105,
	}
	md, _ := buildMetadata(l0, series{})
	if md.ScannerManufacturer != "Leica Biosystems" || md.ScannerModel != "GT450" {
		t.Errorf("scanner = %q/%q", md.ScannerManufacturer, md.ScannerModel)
	}
	if md.Magnification != 40 {
		t.Errorf("magnification = %v", md.Magnification)
	}
	// 0.00105105 mm = 1.05105 µm
	if got := md.MPP.X; got < 1.05 || got > 1.06 {
		t.Errorf("MPP.X = %v, want ~1.051", got)
	}
	if md.Writer != "Leica ScnUtility" {
		t.Errorf("writer = %q", md.Writer)
	}
}
