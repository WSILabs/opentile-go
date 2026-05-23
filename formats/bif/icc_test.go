package bif

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestICCProfileNilWhenAbsent: synthetic fixtures don't carry the
// InterColorProfile tag (34675), so ICCProfile() returns nil. This
// is the legacy-iScan path: OS-1 reports tag-present-with-count=0,
// which our implementation also collapses to nil.
func TestICCProfileNilWhenAbsent(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	if got := tiler.ICCProfile(); got != nil {
		t.Errorf("ICCProfile: got %d bytes, want nil (no tag in synthetic fixture)", len(got))
	}
}

// Real-fixture coverage: Ventana-1 carries a 1.8 MB ICC profile
// with "acsp" magic at offset 36; OS-1 carries no ICC. Both are
// verified end-to-end via TestSlideParity / opentile.OpenFile in
// the integration suite (Batch E); this unit test pins only the
// "no ICC" branch for synthetic fixtures.
