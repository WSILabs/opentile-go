package opentile_test

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestLevelReadMethods verifies the v1.0 *Level receiver-method read API
// delegates to the equivalent Slide.Image* method byte-for-byte and
// returns sane decoded geometry.
func TestLevelReadMethods(t *testing.T) {
	s := openSampleSlide(t)

	lvl, err := s.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}

	// Level.Tile via two navigation paths (Slide.Level vs
	// Slide.Pyramid(0).Level(0)) must return identical bytes.
	got, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Level.Tile(0,0): %v", err)
	}
	want, err := mustImageLevel(t, s, 0, 0).Tile(0, 0)
	if err != nil {
		t.Fatalf("Pyramid(0).Level(0).Tile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Level.Tile bytes differ across navigation paths: %d vs %d bytes", len(got), len(want))
	}

	// TileMaxSize / TilePrefix are non-panicking metadata reads.
	if max := lvl.TileMaxSize(); max <= 0 {
		t.Errorf("Level.TileMaxSize() = %d, want > 0", max)
	}

	// DecodedTile returns a tile-sized decoded image.
	img, err := lvl.DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("Level.DecodedTile(0,0): %v", err)
	}
	if img.Width != lvl.TileSize.W || img.Height != lvl.TileSize.H {
		t.Errorf("DecodedTile dims = %dx%d, want %dx%d",
			img.Width, img.Height, lvl.TileSize.W, lvl.TileSize.H)
	}

	// ReadRegion returns a region of the requested size.
	region, err := lvl.ReadRegion(opentile.Region{Size: opentile.Size{W: 64, H: 48}})
	if err != nil {
		t.Fatalf("Level.ReadRegion: %v", err)
	}
	if region.Width != 64 || region.Height != 48 {
		t.Errorf("ReadRegion dims = %dx%d, want 64x48", region.Width, region.Height)
	}
}
