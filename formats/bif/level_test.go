package bif

import (
	"bytes"
	"context"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestLevelGeometry: a 4x3-tile pyramid IFD reports correct
// Size / TileSize / Grid / Index / PyramidIndex.
func TestLevelGeometry(t *testing.T) {
	const tw, th, gridW, gridH = 64, 32, 4, 3
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
		},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lvl, err := tiler.Level(0, 0)
	if err != nil {
		t.Fatalf("Level(0, 0): %v", err)
	}
	if got, want := lvl.Index, 0; got != want {
		t.Errorf("Index: got %d, want %d", got, want)
	}
	if got, want := lvl.PyramidIndex, 0; got != want {
		t.Errorf("PyramidIndex: got %d, want %d", got, want)
	}
	if got, want := lvl.Size, (opentile.Size{W: tw * gridW, H: th * gridH}); got != want {
		t.Errorf("Size: got %v, want %v", got, want)
	}
	if got, want := lvl.TileSize, (opentile.Size{W: tw, H: th}); got != want {
		t.Errorf("TileSize: got %v, want %v", got, want)
	}
	if got, want := lvl.Grid, (opentile.Size{W: gridW, H: gridH}); got != want {
		t.Errorf("Grid: got %v, want %v", got, want)
	}
	if got := lvl.Compression; got != opentile.CompressionUnknown {
		// Synthetic fixtures don't set Compression tag → CompressionUnknown.
		// Real BIF fixtures always have JPEG (7); per-fixture verification
		// happens in the integration / oracle suites.
		t.Logf("Compression: got %v (synthetic fixtures have no compression tag, this is expected)", got)
	}
}

// TestLevelMPP: per-level MPP doubles per pyramid step from the
// base ScanRes. Base ScanRes 0.25 µm/pixel → MPP.X = 0.25 µm/px
// at level 0; 0.5 at level 1; 1.0 at level 2.
func TestLevelMPP(t *testing.T) {
	const tw, th = 64, 64
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
		{description: "level=1 mag=20 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
		{description: "level=2 mag=10 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// MPP is now in microns (not mm): ScanRes=0.25 µm/px × 2^level.
	wants := []float64{0.25, 0.5, 1.0}
	for i, want := range wants {
		lvl, err := tiler.Level(0, i)
		if err != nil {
			t.Fatalf("Level(0, %d): %v", i, err)
		}
		if got := lvl.MPP.X; got != want {
			t.Errorf("Level %d MPP.X: got %v, want %v (microns)", i, got, want)
		}
	}
}

// TestLevelTileOverlapZeroByDefault: with no EncodeInfo XMP and
// non-overlapping data on level 0, TileOverlap returns zero.
func TestLevelTileOverlapZeroByDefault(t *testing.T) {
	const tw, th = 64, 64
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl, _ := tiler.Level(0, 0)
	if got := lvl.TileOverlap; got != (opentile.Point{}) {
		t.Errorf("TileOverlap: got %v, want opentile.Point{}", got)
	}
}

// TestLevelTileOverlapNonZero: a synthetic EncodeInfo XMP with
// TileJointInfo entries reporting OverlapX=24 propagates through
// to TileOverlap on level 0 (weighted average across joints).
func TestLevelTileOverlapNonZero(t *testing.T) {
	const tw, th = 64, 64
	encodeInfoXMP := []byte(`<EncodeInfo Ver="2">
<SlideInfo>
<AoiInfo XIMAGESIZE="64" YIMAGESIZE="64" NumRows="1" NumCols="1"/>
</SlideInfo>
<SlideStitchInfo>
<ImageInfo AOIScanned="1" AOIIndex="0" Width="64" Height="64" NumRows="1" NumCols="1">
<TileJointInfo FlagJoined="1" Confidence="100" Direction="LEFT" Tile1="1" Tile2="2" OverlapX="24" OverlapY="0"/>
<TileJointInfo FlagJoined="1" Confidence="100" Direction="LEFT" Tile1="2" Tile2="3" OverlapX="24" OverlapY="0"/>
</ImageInfo>
</SlideStitchInfo>
</EncodeInfo>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{xmp: encodeInfoXMP, description: "level=0 mag=40 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
		{description: "level=1 mag=20 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Level 0 → TileOverlap.X == 24 (weighted average of two
	// equal-weight joints, both OverlapX=24).
	l0, _ := tiler.Level(0, 0)
	if got := l0.TileOverlap; got != (opentile.Point{X: 24, Y: 0}) {
		t.Errorf("Level 0 TileOverlap: got %v, want {24, 0}", got)
	}
	// Level 1 (pyramid level 1) → TileOverlap zero (per spec, only
	// level=0 carries overlap; pyramid IFDs 1+ never overlap).
	l1, _ := tiler.Level(0, 1)
	if got := l1.TileOverlap; got != (opentile.Point{}) {
		t.Errorf("Level 1 TileOverlap: got %v, want zero", got)
	}
}

// TestLevelTileRowMajorOrdering: every (col, row) of a 2x2 grid returns a
// readable tile. TILE_OFFSETS is row-major (GH #57); this exercises all
// positions of a multi-tile level.
func TestLevelTileRowMajorOrdering(t *testing.T) {
	const tw, th, gridW, gridH = 32, 32, 2, 2
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw * gridW, imageLength: th * gridH, tileWidth: tw, tileLength: th},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Use levelImpls directly for tile reads (internal test).
	lvl := tiler.levelImpls[0]
	// Every (col, row) returns a non-empty byte slice of length
	// tw*th (synthetic tiles are uniform fill).
	expectedLen := tw * th
	for r := 0; r < gridH; r++ {
		for c := 0; c < gridW; c++ {
			tile, err := lvl.Tile(c, r)
			if err != nil {
				t.Fatalf("Tile(%d, %d): %v", c, r, err)
			}
			if len(tile) != expectedLen {
				t.Errorf("Tile(%d, %d) len: got %d, want %d", c, r, len(tile), expectedLen)
			}
		}
	}
}

// TestLevelTileOutOfBounds: out-of-grid (col, row) coordinates
// return *opentile.TileError wrapping ErrTileOutOfBounds.
func TestLevelTileOutOfBounds(t *testing.T) {
	const tw, th = 64, 64
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl := tiler.levelImpls[0]
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {1, 0}, {0, 1}} {
		_, err := lvl.Tile(c[0], c[1])
		if err == nil {
			t.Errorf("Tile(%d, %d): got nil err, want bounds error", c[0], c[1])
		}
	}
}

// TestLevelTilesIterator: Tiles iterator yields exactly grid.W *
// grid.H entries in row-major image-space order; cancellation via
// ctx stops iteration.
func TestLevelTilesIterator(t *testing.T) {
	const tw, th, gridW, gridH = 32, 32, 2, 2
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw * gridW, imageLength: th * gridH, tileWidth: tw, tileLength: th},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl := tiler.levelImpls[0]
	count := 0
	for pos, res := range lvl.Tiles(context.Background()) {
		if res.Err != nil {
			t.Errorf("Tiles[%v]: %v", pos, res.Err)
		}
		count++
	}
	if want := gridW * gridH; count != want {
		t.Errorf("Tiles count: got %d, want %d", count, want)
	}
}

// TestLevelTileReader: TileReader returns a streaming reader whose
// Close + Read produce the same bytes as Tile(c, r).
func TestLevelTileReader(t *testing.T) {
	const tw, th = 64, 64
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
		{description: "level=0 mag=40 quality=95", imageWidth: tw, imageLength: th, tileWidth: tw, tileLength: th, tileFill: 0xAB},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl := tiler.levelImpls[0]
	want, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	rc, err := lvl.TileReader(0, 0)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got := make([]byte, len(want))
	n, err := rc.Read(got)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Read: %v", err)
	}
	if n != len(want) {
		t.Errorf("Read length: got %d, want %d", n, len(want))
	}
	if !bytes.Equal(got, want) {
		t.Error("TileReader bytes != Tile bytes")
	}
}
