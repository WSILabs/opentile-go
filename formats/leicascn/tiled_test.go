package leicascn

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/cornish/opentile-go"
)

// helperBuildCompositeLevel builds a real compositeLevel for the
// given fixture's level 0. Shared across the multi-region tests.
func helperBuildCompositeLevel(t *testing.T, fixture string, level int) *compositeLevel {
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
	if level >= len(composite) {
		t.Fatalf("level %d out of range (composite has %d levels)", level, len(composite))
	}
	cl := composite[level]
	regions := make([]*tiledRegion, len(cl.Regions))
	for i, rl := range cl.Regions {
		tr, err := newTiledRegion(rl, tf, tf.ReaderAt())
		if err != nil {
			t.Fatal(err)
		}
		regions[i] = tr
	}
	cmpl, err := newCompositeLevel(level, level, cl, regions)
	if err != nil {
		t.Fatal(err)
	}
	return cmpl
}

func TestCompositeLevel_Leica1_SingleRegion(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-1.scn", 0)
	if cl.size.W != 36832 || cl.size.H != 38432 {
		t.Errorf("size = %v, want 36832×38432", cl.size)
	}
	if cl.tileSize.W != 512 || cl.tileSize.H != 512 {
		t.Errorf("tileSize = %v, want 512×512", cl.tileSize)
	}
	if cl.grid.W != 72 || cl.grid.H != 76 {
		t.Errorf("grid = %v, want 72×76", cl.grid)
	}
	if len(cl.regions) != 1 || len(cl.regionBounds) != 1 {
		t.Fatalf("regions=%d, bounds=%d, want 1 each", len(cl.regions), len(cl.regionBounds))
	}
	// Single-region composite: bounds[0] must be (0, 0, full size).
	rb := cl.regionBounds[0]
	if rb.OffsetX != 0 || rb.OffsetY != 0 ||
		rb.PixelSizeX != 36832 || rb.PixelSizeY != 38432 {
		t.Errorf("regionBounds[0] = %+v, want full coverage", rb)
	}

	// L0 (0, 0) → real region tile.
	b, err := cl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0): %v", err)
	}
	if !bytes.Equal(b[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("first 2 bytes = % x, want FF D8", b[:2])
	}
}

func TestCompositeLevel_Leica2_MultiRegion_DispatchesCorrectly(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-2.scn", 0)
	if got := len(cl.regions); got != 4 {
		t.Fatalf("regions = %d, want 4", got)
	}

	// Sample a tile that's inside region[0]'s bounds — should match
	// the region-local tile bytes.
	rb := cl.regionBounds[0]
	cx := rb.OffsetX/cl.tileSize.W + 0       // first tile of region 0
	cy := rb.OffsetY/cl.tileSize.H + 0
	composite, err := cl.Tile(cx, cy)
	if err != nil {
		t.Fatalf("Tile(%d,%d): %v", cx, cy, err)
	}
	regionLocal, err := cl.regions[0].Tile(0, 0, 0)
	if err != nil {
		t.Fatalf("region.Tile(0,0,0): %v", err)
	}
	if !bytes.Equal(composite, regionLocal) {
		t.Errorf("composite tile != region-local tile (%d vs %d bytes)",
			len(composite), len(regionLocal))
	}

	// Find a tile coord between two regions (Y-gap; the 4 mains are
	// vertically stacked with gaps). Pick a Y between the end of
	// region 0 and start of region 1.
	gapY := -1
	for i := 0; i+1 < len(cl.regionBounds); i++ {
		bA := cl.regionBounds[i]
		// Find the next region by increasing OffsetY > bA.OffsetY+bA.PixelSizeY.
		for j := 0; j < len(cl.regionBounds); j++ {
			if j == i {
				continue
			}
			bB := cl.regionBounds[j]
			if bB.OffsetY > bA.OffsetY+bA.PixelSizeY {
				// Pick a tile in the gap between bA and bB.
				midY := (bA.OffsetY + bA.PixelSizeY + bB.OffsetY) / 2
				gapY = midY / cl.tileSize.H
				break
			}
		}
		if gapY >= 0 {
			break
		}
	}
	if gapY < 0 {
		t.Skip("could not locate inter-region gap (test fixture-dependent)")
	}
	// Pick an X that's inside the union (i.e., the gap is along X
	// where some region exists vertically — but at gapY no region
	// covers it).
	gapX := cl.grid.W / 2
	gap, err := cl.Tile(gapX, gapY)
	if err != nil {
		t.Fatalf("Tile(gap %d,%d): %v", gapX, gapY, err)
	}
	if !bytes.Equal(gap[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("gap tile first 2 = % x, want FF D8 (synthesized blank JPEG)", gap[:2])
	}

	// Confirm gap tile == cached blank.
	bt, err := blankTile(cl.tileSize.W, cl.tileSize.H)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gap, bt) {
		t.Errorf("gap tile bytes don't match cached blank (%d vs %d bytes)",
			len(gap), len(bt))
	}
}

func TestCompositeLevel_Fluorescence_PerChannelTileAt(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-Fluorescence-1.scn", 0)
	if cl.sizeC != 3 {
		t.Fatalf("sizeC = %d, want 3", cl.sizeC)
	}
	tiles := make([][]byte, 3)
	for c := 0; c < 3; c++ {
		b, err := cl.TileAt(opentile.TileCoord{C: c, X: 0, Y: 0})
		if err != nil {
			t.Fatalf("TileAt(C=%d,0,0): %v", c, err)
		}
		tiles[c] = b
	}
	if bytes.Equal(tiles[0], tiles[1]) || bytes.Equal(tiles[0], tiles[2]) {
		t.Error("TileAt returned identical bytes across channels (should differ)")
	}
}

func TestCompositeLevel_OOBReturnsErrTileOutOfBounds(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-1.scn", 0)
	for _, p := range []struct{ x, y int }{
		{-1, 0},
		{0, -1},
		{cl.grid.W, 0},
		{0, cl.grid.H},
	} {
		_, err := cl.Tile(p.x, p.y)
		if !errors.Is(err, opentile.ErrTileOutOfBounds) {
			t.Errorf("(%d,%d): got %v, want ErrTileOutOfBounds", p.x, p.y, err)
		}
	}
}

func TestCompositeLevel_TileAt_RejectsNonZeroZT(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-1.scn", 0)
	for _, coord := range []opentile.TileCoord{
		{Z: 1},
		{T: 1},
	} {
		_, err := cl.TileAt(coord)
		if !errors.Is(err, opentile.ErrDimensionUnavailable) {
			t.Errorf("TileAt(%+v): got %v, want ErrDimensionUnavailable", coord, err)
		}
	}
}

func TestCompositeLevel_TileEqualsTileInto(t *testing.T) {
	cl := helperBuildCompositeLevel(t, "Leica-1.scn", 0)
	buf := make([]byte, cl.TileMaxSize())
	for _, p := range []struct{ x, y int }{
		{0, 0},
		{cl.grid.W - 1, 0},
		{cl.grid.W / 2, cl.grid.H / 2},
	} {
		a, errA := cl.Tile(p.x, p.y)
		n, errB := cl.TileInto(p.x, p.y, buf)
		if (errA == nil) != (errB == nil) {
			t.Errorf("(%d,%d): Tile err=%v, TileInto err=%v", p.x, p.y, errA, errB)
			continue
		}
		if errA != nil {
			continue
		}
		if !bytes.Equal(a, buf[:n]) {
			t.Errorf("(%d,%d): Tile %d bytes != TileInto %d bytes",
				p.x, p.y, len(a), n)
		}
	}
}

func TestBlankTile_ValidJPEG(t *testing.T) {
	b, err := blankTile(512, 512)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("first 2 = % x, want FF D8 (SOI)", b[:2])
	}
	if !bytes.Equal(b[len(b)-2:], []byte{0xFF, 0xD9}) {
		t.Errorf("last 2 = % x, want FF D9 (EOI)", b[len(b)-2:])
	}
}

func TestBlankTile_Cached(t *testing.T) {
	a, _ := blankTile(256, 256)
	b, _ := blankTile(256, 256)
	// Cache returns the same underlying slice. We don't require
	// pointer-equality (the cache could be defensive in a future
	// refactor), but the byte content must match.
	if !bytes.Equal(a, b) {
		t.Error("blankTile cache returned divergent bytes for the same key")
	}
}

func TestBlankTile_RejectsBadDims(t *testing.T) {
	if _, err := blankTile(0, 100); err == nil {
		t.Error("expected error for w=0")
	}
	if _, err := blankTile(100, -1); err == nil {
		t.Error("expected error for h<0")
	}
}
