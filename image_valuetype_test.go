package opentile_test

import (
	"image"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

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
		MPP:          opentile.SizeMm{W: 0.25, H: 0.25},
		FocalPlane:   0,
		TileOverlap:  image.Point{},
	}
	if lvl.Size.W != 1024 || lvl.TileSize.H != 256 || lvl.Grid.H != 3 {
		t.Errorf("literal fields not preserved: %+v", lvl)
	}
}

func TestImageIsValueType(t *testing.T) {
	img := opentile.Image{
		Name:   "primary",
		Index:  0,
		Levels: []opentile.Level{{Index: 0}, {Index: 1}},
	}
	if img.Name != "primary" || len(img.Levels) != 2 {
		t.Errorf("Image literal not preserved: %+v", img)
	}
}
