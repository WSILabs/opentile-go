package opentile_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func openSampleSlide(t *testing.T) *opentile.Slide {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs/CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	t.Cleanup(func() { slide.Close() })
	return slide
}

func TestReadRegionFullTile(t *testing.T) {
	slide := openSampleSlide(t)
	if len(slide.Levels()) == 0 {
		t.Fatalf("no levels")
	}
	lvl := slide.Levels()[0]
	tileW := lvl.TileSize.W
	tileH := lvl.TileSize.H
	img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: tileW, H: tileH}})
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Width != tileW || img.Height != tileH {
		t.Errorf("dims: got %dx%d, want %dx%d", img.Width, img.Height, tileW, tileH)
	}
	tileImg, err := slide.DecodedTile(0, 0, 0)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	// Tile may be smaller than full TileSize at edge; if so, only compare the actual decoded region.
	if img.Width != tileImg.Width || img.Height != tileImg.Height {
		t.Skipf("edge tile (%dx%d vs requested %dx%d) — byte-equality skipped",
			tileImg.Width, tileImg.Height, tileW, tileH)
	}
	if !bytes.Equal(img.Pix, tileImg.Pix) {
		t.Errorf("ReadRegion bytes differ from DecodedTile")
	}
}

func TestReadRegionAcrossTileBoundary(t *testing.T) {
	slide := openSampleSlide(t)
	lvl := slide.Levels()[0]
	tileW := lvl.TileSize.W
	if tileW < 4 || lvl.Size.W < tileW*2 {
		t.Skip("slide too small for boundary test")
	}
	img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: tileW / 2, Y: 0}, Size: opentile.Size{W: tileW, H: 64}})
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Width != tileW || img.Height != 64 {
		t.Errorf("dims: got %dx%d, want %dx64", img.Width, img.Height, tileW)
	}
	tile00, err := slide.DecodedTile(0, 0, 0)
	if err != nil {
		t.Fatalf("DecodedTile(0,0,0): %v", err)
	}
	tile10, err := slide.DecodedTile(0, 1, 0)
	if err != nil {
		t.Fatalf("DecodedTile(0,1,0): %v", err)
	}
	// Left half: tile(0,0) columns [tileW/2..tileW-1]
	for y := 0; y < 64; y++ {
		for x := 0; x < tileW/2; x++ {
			tileOff := y*tile00.Stride + (x+tileW/2)*3
			imgOff := y*img.Stride + x*3
			if !bytes.Equal(img.Pix[imgOff:imgOff+3], tile00.Pix[tileOff:tileOff+3]) {
				t.Fatalf("left half mismatch at (%d,%d)", x, y)
			}
		}
		// Right half: tile(1,0) columns [0..tileW/2-1]
		for x := tileW / 2; x < tileW; x++ {
			tileOff := y*tile10.Stride + (x-tileW/2)*3
			imgOff := y*img.Stride + x*3
			if !bytes.Equal(img.Pix[imgOff:imgOff+3], tile10.Pix[tileOff:tileOff+3]) {
				t.Fatalf("right half mismatch at (%d,%d)", x, y)
			}
		}
	}
}

func TestReadRegionOutOfBoundsRightEdge(t *testing.T) {
	slide := openSampleSlide(t)
	lvl := slide.Levels()[0]
	x := lvl.Size.W - 64
	img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: x, Y: 0}, Size: opentile.Size{W: 192, H: 64}})
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Width != 192 || img.Height != 64 {
		t.Errorf("dims: got %dx%d, want 192x64", img.Width, img.Height)
	}
	// Right 128 columns must be white.
	for y := 0; y < 64; y++ {
		for ix := 64; ix < 192; ix++ {
			off := y*img.Stride + ix*3
			if img.Pix[off] != 0xFF || img.Pix[off+1] != 0xFF || img.Pix[off+2] != 0xFF {
				t.Fatalf("expected white at (%d,%d): got %v", ix, y, img.Pix[off:off+3])
			}
		}
	}
}

func TestReadRegionOutOfBoundsNegative(t *testing.T) {
	slide := openSampleSlide(t)
	img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: -64, Y: 0}, Size: opentile.Size{W: 128, H: 64}})
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	for y := 0; y < 64; y++ {
		for ix := 0; ix < 64; ix++ {
			off := y*img.Stride + ix*3
			if img.Pix[off] != 0xFF || img.Pix[off+1] != 0xFF || img.Pix[off+2] != 0xFF {
				t.Fatalf("expected white at (%d,%d): got %v", ix, y, img.Pix[off:off+3])
			}
		}
	}
}

func TestReadRegionEntirelyOutOfBounds(t *testing.T) {
	slide := openSampleSlide(t)
	lvl := slide.Levels()[0]
	_, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: lvl.Size.W + 100, Y: 0}, Size: opentile.Size{W: 64, H: 64}})
	if !errors.Is(err, opentile.ErrRegionEmpty) {
		t.Errorf("got %v, want ErrRegionEmpty", err)
	}
}

func TestReadRegionWithRGBA(t *testing.T) {
	slide := openSampleSlide(t)
	img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: 64, H: 64}}, opentile.WithFormat(decoder.PixelFormatRGBA))
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Format != decoder.PixelFormatRGBA {
		t.Errorf("format: got %v, want RGBA", img.Format)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xFF {
			t.Fatalf("alpha != 0xFF at offset %d", i)
		}
	}
}

func TestReadRegionInto(t *testing.T) {
	slide := openSampleSlide(t)
	dst := decoder.NewImage(64, 64)
	for i := range dst.Pix {
		dst.Pix[i] = 0xAA // marker
	}
	if err := slide.ReadRegionInto(0, opentile.Point{X: 0, Y: 0}, dst); err != nil {
		t.Fatalf("ReadRegionInto: %v", err)
	}
	// Some byte must have changed from 0xAA.
	allMarker := true
	for _, b := range dst.Pix {
		if b != 0xAA {
			allMarker = false
			break
		}
	}
	if allMarker {
		t.Error("dst not overwritten")
	}
}

func TestReadRegionAllFormats(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	samples := []string{
		"svs/CMU-1-Small-Region.svs",
		"philips/Philips-1.tiff",
		"ome-tiff/Leica-1.ome.tiff",
		"bif/OS-1.bif",
	}
	for _, rel := range samples {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(dir, rel)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("missing %s", rel)
			}
			slide, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile %s: %v", rel, err)
			}
			defer slide.Close()
			if len(slide.Levels()) == 0 {
				t.Skipf("no levels in %s", rel)
			}
			lvl := slide.Levels()[0]
			w := 64
			if lvl.Size.W < w {
				w = lvl.Size.W
			}
			h := 64
			if lvl.Size.H < h {
				h = lvl.Size.H
			}
			img, err := slide.ReadRegion(0, opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: opentile.Size{W: w, H: h}})
			if err != nil {
				t.Fatalf("ReadRegion: %v", err)
			}
			if img.Width != w || img.Height != h {
				t.Errorf("dims: got %dx%d, want %dx%d", img.Width, img.Height, w, h)
			}
		})
	}
}
