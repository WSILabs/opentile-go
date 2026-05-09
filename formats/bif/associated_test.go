package bif

import (
	"bytes"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// TestAssociatedSpecCompliantHasOverviewAndProbability: a synthetic
// spec-compliant BIF (Label_Image + Probability_Image associated
// IFDs, plus a level=0 pyramid IFD) exposes both associated images
// via Tiler.Associated().
func TestAssociatedSpecCompliantHasOverviewAndProbability(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{description: "Probability_Image"},
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
	ai := tiler.Associated()
	if len(ai) != 2 {
		t.Fatalf("Associated count: got %d, want 2 (overview + probability)", len(ai))
	}
	want := map[string]bool{"overview": true, "probability": true}
	for _, a := range ai {
		if !want[a.Type()] {
			t.Errorf("unexpected associated kind %q", a.Type())
		}
		delete(want, a.Type())
	}
	if len(want) != 0 {
		t.Errorf("missing associated kinds: %v", want)
	}
}

// TestAssociatedLegacyHasOverviewAndThumbnail: a synthetic legacy
// iScan BIF (Label Image + Thumbnail) exposes both as associated.
func TestAssociatedLegacyHasOverviewAndThumbnail(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan Magnification="40"/>`), description: "Label Image"},
		{description: "Thumbnail"},
		{description: "level=0 mag=40 quality=90", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ai := tiler.Associated()
	if len(ai) != 2 {
		t.Fatalf("Associated count: got %d, want 2 (overview + thumbnail)", len(ai))
	}
	wantSet := map[string]bool{"overview": true, "thumbnail": true}
	for _, a := range ai {
		if !wantSet[a.Type()] {
			t.Errorf("unexpected associated kind %q", a.Type())
		}
		delete(wantSet, a.Type())
	}
	if len(wantSet) != 0 {
		t.Errorf("missing associated kinds: %v", wantSet)
	}
}

// TestAssociatedDimensionsAndCompression: dimensions / compression
// are surfaced from the underlying IFD tags.
func TestAssociatedDimensionsAndCompression(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image", imageWidth: 100, imageLength: 200},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	ai := tiler.Associated()
	if len(ai) != 1 {
		t.Fatalf("Associated count: got %d, want 1", len(ai))
	}
	a := ai[0]
	if got, want := a.Size(), (opentile.Size{W: 100, H: 200}); got != want {
		t.Errorf("Size: got %v, want %v", got, want)
	}
	// Synthetic non-tiled IFDs have no Compression tag → CompressionUnknown.
	if got := a.Compression(); got != opentile.CompressionUnknown {
		t.Errorf("Compression: got %v, want CompressionUnknown (synthetic non-tiled IFDs lack the tag)", got)
	}
}

// TestAssociatedReturnsCopy: Associated returns a fresh slice.
// Callers can mutate the slice header without affecting Tiler state.
func TestAssociatedReturnsCopy(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: 64, imageLength: 64, tileWidth: 64, tileLength: 64},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	first := tiler.Associated()
	second := tiler.Associated()
	if &first == &second {
		t.Error("Associated() returned same slice header pointer twice (should be a fresh copy)")
	}
}

// TestKindFromIFDRoleMapping pins the role→kind mapping that
// ties layout classification (T12) to public AssociatedImage kinds.
func TestKindFromIFDRoleMapping(t *testing.T) {
	cases := []struct {
		role ifdRole
		want string
	}{
		{ifdRoleLabel, "overview"},
		{ifdRoleProbability, "probability"},
		{ifdRoleThumbnail, "thumbnail"},
		{ifdRolePyramid, ""},
		{ifdRoleUnknown, ""},
	}
	for _, c := range cases {
		if got := kindFromIFDRole(c.role); got != c.want {
			t.Errorf("kindFromIFDRole(%v): got %q, want %q", c.role, got, c.want)
		}
	}
}
