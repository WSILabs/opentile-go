package opentile

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// fillTile makes a tileW x tileH RGB image whose every pixel is (r,g,0).
func fillTile(w, h int, r, g byte) *decoder.Image {
	img := decoder.NewImageFormat(w, h, decoder.PixelFormatRGB)
	for i := 0; i+2 < len(img.Pix); i += 3 {
		img.Pix[i] = r
		img.Pix[i+1] = g
	}
	return img
}

func TestLevelStitchedGrid(t *testing.T) {
	l := &Level{Size: Size{W: 260, H: 180}, TileSize: Size{W: 100, H: 100}}
	if g := l.StitchedGrid(); g != (Size{W: 3, H: 2}) {
		t.Fatalf("StitchedGrid = %v, want {3,2}", g)
	}
}

func TestCompositeStitchedLoopBlitsIntersectingTiles(t *testing.T) {
	// One tile at origin (0,0), 100x100; dst covers stitched rect [0,0,100,100).
	rl := &fakeLayoutReader{originX: 0}
	dst := decoder.NewImageFormat(100, 100, decoder.PixelFormatRGB)
	fillWhite(dst)
	err := compositeStitchedLoop(rl, 0, 0, 0, 0, 0, 100, 100, 100, 100, dst,
		func(col, row int) (*decoder.Image, error) { return fillTile(100, 100, 42, 7), nil })
	if err != nil {
		t.Fatal(err)
	}
	if dst.Pix[0] != 42 || dst.Pix[1] != 7 {
		t.Fatalf("top-left = (%d,%d), want (42,7) — tile not blitted", dst.Pix[0], dst.Pix[1])
	}
}
