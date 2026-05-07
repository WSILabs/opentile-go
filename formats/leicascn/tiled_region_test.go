package leicascn

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/cornish/opentile-go"
)

// helperBuildRegionLevel0 builds a tiledRegion for the given fixture's
// first main scan at level 0. Shared across the per-region tests.
func helperBuildRegionLevel0(t *testing.T, fixture string) *tiledRegion {
	t.Helper()
	c, tf, _ := openSCNTestFile(t, fixture)
	var mains []Image
	for _, img := range c.Images {
		if !IsAuxiliary(img, c) {
			mains = append(mains, img)
		}
	}
	composite, err := ComposePyramid(mains, c)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := newTiledRegion(composite[0].Regions[0], tf, tf.ReaderAt())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestTiledRegion_Leica1_L0_Tile00_IsValidJPEG(t *testing.T) {
	tr := helperBuildRegionLevel0(t, "Leica-1.scn")
	if tr.tileSize.W != 512 || tr.tileSize.H != 512 {
		t.Errorf("tileSize = %v, want 512×512", tr.tileSize)
	}
	if tr.grid.W != 72 || tr.grid.H != 76 { // 36832/512 = 72; 38432/512 = 75.0625 → 76
		t.Errorf("grid = %v, want 72×76", tr.grid)
	}
	b, err := tr.Tile(0, 0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0,0): %v", err)
	}
	if !bytes.Equal(b[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("first 2 bytes = % x, want FF D8 (JPEG SOI)", b[:2])
	}
	tail := b
	if len(tail) > 64 {
		tail = tail[len(tail)-64:]
	}
	if !bytes.Contains(tail, []byte{0xFF, 0xD9}) {
		t.Errorf("trailing bytes don't contain JPEG EOI")
	}
}

func TestTiledRegion_TileEqualsTileInto(t *testing.T) {
	tr := helperBuildRegionLevel0(t, "Leica-1.scn")
	buf := make([]byte, tr.maxTileSize())
	for _, p := range []struct{ x, y int }{
		{0, 0},
		{tr.grid.W - 1, 0},
		{0, tr.grid.H - 1},
		{tr.grid.W - 1, tr.grid.H - 1},
		{tr.grid.W / 2, tr.grid.H / 2},
	} {
		a, errA := tr.Tile(0, p.x, p.y)
		n, errB := tr.TileInto(0, p.x, p.y, buf)
		if (errA == nil) != (errB == nil) {
			t.Errorf("(%d,%d): Tile err=%v, TileInto err=%v", p.x, p.y, errA, errB)
			continue
		}
		if errA != nil {
			continue
		}
		if !bytes.Equal(a, buf[:n]) {
			t.Errorf("(%d,%d): Tile %d bytes != TileInto %d bytes", p.x, p.y, len(a), n)
		}
	}
}

func TestTiledRegion_OOBReturnsErrTileOutOfBounds(t *testing.T) {
	tr := helperBuildRegionLevel0(t, "Leica-1.scn")
	for _, p := range []struct{ x, y int }{
		{-1, 0},
		{0, -1},
		{tr.grid.W, 0},
		{0, tr.grid.H},
	} {
		_, err := tr.Tile(0, p.x, p.y)
		if !errors.Is(err, opentile.ErrTileOutOfBounds) {
			t.Errorf("(%d,%d): got %v, want ErrTileOutOfBounds", p.x, p.y, err)
		}
	}
}

func TestTiledRegion_Fluorescence_PerChannelDispatch(t *testing.T) {
	tr := helperBuildRegionLevel0(t, "Leica-Fluorescence-1.scn")
	if got := len(tr.perChannel); got != 3 {
		t.Fatalf("perChannel = %d, want 3", got)
	}
	// Read tile (0,0) from each channel; they should differ in
	// content (separate IFDs holding different fluorescence-channel
	// pixel data).
	tiles := make([][]byte, 3)
	for c := 0; c < 3; c++ {
		b, err := tr.Tile(c, 0, 0)
		if err != nil {
			t.Fatalf("Tile(%d,0,0): %v", c, err)
		}
		tiles[c] = b
		if !bytes.Equal(b[:2], []byte{0xFF, 0xD8}) {
			t.Errorf("Channel %d first 2 = % x, want FF D8", c, b[:2])
		}
	}
	if bytes.Equal(tiles[0], tiles[1]) {
		t.Error("Channels 0 and 1 returned identical bytes (should differ — different fluorescence channels)")
	}
}

func TestTiledRegion_ChannelOOBReturnsErrDimensionUnavailable(t *testing.T) {
	tr := helperBuildRegionLevel0(t, "Leica-Fluorescence-1.scn")
	_, err := tr.Tile(3, 0, 0) // SizeC=3 → valid range [0..2]
	if !errors.Is(err, opentile.ErrDimensionUnavailable) {
		t.Errorf("got %v, want ErrDimensionUnavailable", err)
	}
}
