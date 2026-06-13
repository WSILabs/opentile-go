package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestAssociatedImageHasAccessors is a compile-time conformance check that
// the AssociatedImage interface includes the three v1.0 accessors added in
// the API breaking pass (Task 7).
func TestAssociatedImageHasAccessors(t *testing.T) {
	var ai opentile.AssociatedImage
	var _ interface {
		Encoding() (opentile.AssociatedEncoding, bool)
		TIFFTags() (opentile.TIFFTags, bool)
		IFDOffset() (int64, bool)
	} = ai
	t.Log("AssociatedImage carries Encoding/TIFFTags/IFDOffset — compile-time check passed")
}

func TestLevelIsValueType(t *testing.T) {
	// Compile-time + zero-value sanity check.
	var lvl opentile.Level
	if lvl.Index != 0 {
		t.Errorf("zero Level.Index: got %d, want 0", lvl.Index)
	}
}

func TestLevelLiteralFields(t *testing.T) {
	lvl := opentile.Level{
		Index:        2,
		PyramidIndex: 0,
		Size:         opentile.Size{W: 1024, H: 768},
		TileSize:     opentile.Size{W: 256, H: 256},
		Grid:         opentile.Size{W: 4, H: 3},
		Compression:  opentile.CompressionJPEG,
		MPP:          opentile.MPP{X: 0.25, Y: 0.25},
		FocalPlane:   0,
		TileOverlap:  opentile.Point{},
	}
	if lvl.Size.W != 1024 || lvl.TileSize.H != 256 || lvl.Grid.H != 3 {
		t.Errorf("literal fields not preserved: %+v", lvl)
	}
}

func TestImageIsValueType(t *testing.T) {
	img := opentile.Pyramid{
		Name:   "primary",
		Index:  0,
		Levels: []opentile.Level{{Index: 0}, {Index: 1}},
	}
	if img.Name != "primary" || len(img.Levels) != 2 {
		t.Errorf("Pyramid literal not preserved: %+v", img)
	}
}
