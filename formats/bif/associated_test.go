package bif

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestAssociatedSpecCompliantHasOverviewAndProbability: a synthetic
// spec-compliant BIF (Label_Image + Probability_Image associated
// IFDs, plus a level=0 pyramid IFD) exposes both associated images
// via Tiler.AssociatedImages().
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
	ai := tiler.AssociatedImages()
	if len(ai) != 3 {
		t.Fatalf("Associated count: got %d, want 3 (overview + synthesized label + probability)", len(ai))
	}
	want := map[opentile.AssociatedType]bool{opentile.AssociatedOverview: true, opentile.AssociatedLabel: true, opentile.AssociatedProbability: true}
	for _, a := range ai {
		if !want[a.Type()] {
			t.Errorf("unexpected associated type %q", a.Type())
		}
		delete(want, a.Type())
	}
	if len(want) != 0 {
		t.Errorf("missing associated types: %v", want)
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
	ai := tiler.AssociatedImages()
	if len(ai) != 3 {
		t.Fatalf("Associated count: got %d, want 3 (overview + synthesized label + thumbnail)", len(ai))
	}
	wantSet := map[opentile.AssociatedType]bool{opentile.AssociatedOverview: true, opentile.AssociatedLabel: true, opentile.AssociatedThumbnail: true}
	for _, a := range ai {
		if !wantSet[a.Type()] {
			t.Errorf("unexpected associated type %q", a.Type())
		}
		delete(wantSet, a.Type())
	}
	if len(wantSet) != 0 {
		t.Errorf("missing associated types: %v", wantSet)
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
	ai := tiler.AssociatedImages()
	if len(ai) != 2 {
		t.Fatalf("Associated count: got %d, want 2 (overview + synthesized label)", len(ai))
	}
	a := ai[0] // overview is appended before the synthesized label
	if a.Type() != opentile.AssociatedOverview {
		t.Fatalf("ai[0] type: got %q, want overview", a.Type())
	}
	if got, want := a.Size(), (opentile.Size{W: 100, H: 200}); got != want {
		t.Errorf("Size: got %v, want %v", got, want)
	}
	// The synthesized label is the top 1/3 of the overview (200/3 = 66).
	if got, want := ai[1].Size(), (opentile.Size{W: 100, H: 66}); ai[1].Type() != opentile.AssociatedLabel || got != want {
		t.Errorf("label: got type %q size %v, want label 100x66", ai[1].Type(), got)
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
	first := tiler.AssociatedImages()
	second := tiler.AssociatedImages()
	if &first == &second {
		t.Error("Associated() returned same slice header pointer twice (should be a fresh copy)")
	}
}

// TestTypeFromIFDRoleMapping pins the role→type mapping that
// ties layout classification (T12) to public AssociatedImage types.
func TestTypeFromIFDRoleMapping(t *testing.T) {
	cases := []struct {
		role ifdRole
		want opentile.AssociatedType
	}{
		{ifdRoleLabel, opentile.AssociatedOverview},
		{ifdRoleProbability, opentile.AssociatedProbability},
		{ifdRoleThumbnail, opentile.AssociatedThumbnail},
		{ifdRolePyramid, ""},
		{ifdRoleUnknown, ""},
	}
	for _, c := range cases {
		if got := typeFromIFDRole(c.role); got != c.want {
			t.Errorf("typeFromIFDRole(%v): got %q, want %q", c.role, got, c.want)
		}
	}
}
