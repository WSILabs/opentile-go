package bif

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestEmptyTilePathReturnsBlank: a synthetic BIF with serpentine
// index 1 marked empty (TileOffsets[1] = 0, TileByteCounts[1] = 0)
// returns a JPEG-decodable blank tile, not an error.
//
// Spec: BIF whitepaper §"AOI Positions" mandates filling unscanned
// tiles with the slide's `<iScan>/@ScanWhitePoint` luminance.
//
// Both real fixtures have zero empty tiles per the T4 gate, so this
// path is fixture-untested in production data — synthetic-only
// coverage is the v0.7 reality (spec §10).
func TestEmptyTilePathReturnsBlank(t *testing.T) {
	// 2x2 grid, one tile marked empty at row-major storage slot 1. White
	// point 200 so the resulting blank decodes to ~RGB(200, 200, 200).
	const tw, th, gridW, gridH = 32, 32, 2, 2
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" ScanWhitePoint="200"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
			tileFill:         0xCC, // non-empty tiles get 0xCC fill
			emptyTileIndices: []int{1},
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
	lvl := tiler.levelImpls[0]

	// Storage index 1 maps row-major to image (col, row).
	emptyCol, emptyRow := 1%gridW, 1/gridW

	tile, err := lvl.Tile(emptyCol, emptyRow)
	if err != nil {
		t.Fatalf("Tile(%d, %d): %v", emptyCol, emptyRow, err)
	}
	if len(tile) < 4 || tile[0] != 0xFF || tile[1] != 0xD8 {
		t.Fatalf("blank tile not a JPEG: first bytes %x", tile[:min(8, len(tile))])
	}
	// Decode and sample the centre pixel.
	img, err := jpeg.Decode(bytes.NewReader(tile))
	if err != nil {
		t.Fatalf("decode blank tile: %v", err)
	}
	bnds := img.Bounds()
	r, g, b, _ := img.At(bnds.Dx()/2, bnds.Dy()/2).RGBA()
	// JPEG round-trip: tolerate ±2 from the expected ScanWhitePoint=200.
	for i, ch := range []uint32{r >> 8, g >> 8, b >> 8} {
		if int(ch) < 200-2 || int(ch) > 200+2 {
			t.Errorf("blank tile pixel channel %d: got %d, want ~200 ± 2", i, ch)
		}
	}
}

// TestEmptyTileFallsBackTo255WhenScanWhitePointAbsent: legacy-iScan
// fixtures don't carry ScanWhitePoint. The blank-tile path must
// default to 255 (true white) — exercised here via a synthetic XMP
// that omits the attribute.
func TestEmptyTileFallsBackTo255WhenScanWhitePointAbsent(t *testing.T) {
	const tw, th, gridW, gridH = 32, 32, 2, 2
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan Magnification="40"/>`), description: "Label Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
			emptyTileIndices: []int{0},
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lvl := tiler.levelImpls[0]

	emptyCol, emptyRow := 0%gridW, 0/gridW
	tile, err := lvl.Tile(emptyCol, emptyRow)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(tile))
	if err != nil {
		t.Fatalf("decode blank tile: %v", err)
	}
	bnds := img.Bounds()
	r, g, b, _ := img.At(bnds.Dx()/2, bnds.Dy()/2).RGBA()
	// Legacy default is 255 (true white). JPEG quality 95 round-trip
	// preserves saturation cleanly; tolerate ±2 anyway.
	for i, ch := range []uint32{r >> 8, g >> 8, b >> 8} {
		if int(ch) < 253 {
			t.Errorf("legacy default fill: channel %d got %d, want ~255 (≥253)", i, ch)
		}
	}
}

// TestEmptyTileReaderReturnsBlank: TileReader on an empty entry
// streams the same blank-tile bytes as Tile.
func TestEmptyTileReaderReturnsBlank(t *testing.T) {
	const tw, th, gridW, gridH = 32, 32, 2, 2
	xmp := []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25" ScanWhitePoint="220"/>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: xmp, description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  tw * gridW, imageLength: th * gridH,
			tileWidth: tw, tileLength: th,
			emptyTileIndices: []int{2},
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl := tiler.levelImpls[0]
	emptyCol, emptyRow := 2%gridW, 2/gridW

	want, err := lvl.Tile(emptyCol, emptyRow)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	rc, err := lvl.TileReader(emptyCol, emptyRow)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got := make([]byte, len(want))
	rc.Read(got)
	if !bytes.Equal(got, want) {
		t.Error("TileReader bytes differ from Tile bytes on empty entry")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
