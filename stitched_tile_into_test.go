package opentile

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// imageStitchedTileInto must produce byte-identical pixels to the allocating
// imageStitchedTile for every canonical display tile.
func TestStitchedTileIntoEqualsStitchedTile(t *testing.T) {
	f := newFakeStitchReader()
	s := &Slide{r: f, readBudget: 64 << 20}
	lvl, _ := f.Level(0, 0)
	gw := ceilDiv(lvl.Size.W, lvl.TileSize.W)
	gh := ceilDiv(lvl.Size.H, lvl.TileSize.H)

	for vy := 0; vy < gh; vy++ {
		for vx := 0; vx < gw; vx++ {
			want, err := s.imageStitchedTile(0, 0, vx, vy)
			if err != nil {
				t.Fatalf("StitchedTile(%d,%d): %v", vx, vy, err)
			}
			dst := decoder.NewImageFormat(lvl.TileSize.W, lvl.TileSize.H, decoder.PixelFormatRGB)
			if err := s.imageStitchedTileInto(0, 0, vx, vy, dst); err != nil {
				t.Fatalf("StitchedTileInto(%d,%d): %v", vx, vy, err)
			}
			if !bytes.Equal(dst.Pix, want.Pix) {
				t.Fatalf("tile (%d,%d): Into pixels differ from StitchedTile", vx, vy)
			}
		}
	}
}

// Reusing one dst across tiles must yield correct, independent results — the
// white-fill + composite must fully overwrite the previous tile.
func TestStitchedTileIntoReuseBuffer(t *testing.T) {
	f := newFakeStitchReader()
	s := &Slide{r: f, readBudget: 64 << 20}
	lvl, _ := f.Level(0, 0)
	dst := decoder.NewImageFormat(lvl.TileSize.W, lvl.TileSize.H, decoder.PixelFormatRGB)

	for _, c := range [][2]int{{0, 0}, {2, 1}, {1, 0}} {
		if err := s.imageStitchedTileInto(0, 0, c[0], c[1], dst); err != nil {
			t.Fatalf("Into(%d,%d): %v", c[0], c[1], err)
		}
		want, err := s.imageStitchedTile(0, 0, c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dst.Pix, want.Pix) {
			t.Fatalf("reused dst for tile (%d,%d) does not match a fresh StitchedTile", c[0], c[1])
		}
	}
}

func TestStitchedTileIntoWrongDims(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(), readBudget: 64 << 20}
	dst := decoder.NewImageFormat(50, 50, decoder.PixelFormatRGB) // not TileSize (100x100)
	if err := s.imageStitchedTileInto(0, 0, 0, 0, dst); err == nil {
		t.Fatal("want error for dst that is not TileSize")
	}
}

func TestStitchedTileIntoNilDst(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(), readBudget: 64 << 20}
	if err := s.imageStitchedTileInto(0, 0, 0, 0, nil); err == nil {
		t.Fatal("want error for nil dst")
	}
}

// Without a regionLayout, StitchedTileInto must delegate to DecodedTileInto and
// produce the same pixels as DecodedTile.
func TestStitchedTileIntoDelegatesWithoutLayout(t *testing.T) {
	s := &Slide{r: &noLayout{newFakeStitchReader()}, readBudget: 64 << 20}
	dst := decoder.NewImageFormat(100, 100, decoder.PixelFormatRGB)
	if err := s.imageStitchedTileInto(0, 0, 1, 1, dst); err != nil {
		t.Fatal(err)
	}
	want, err := s.imageDecodedTile(0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Pix, want.Pix) {
		t.Fatal("without regionLayout, StitchedTileInto must equal DecodedTile")
	}
}
