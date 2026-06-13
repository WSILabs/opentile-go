package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestDecodedTileWithJPEGDecoder(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide, err := opentile.OpenFile(filepath.Join(dir, "svs/CMU-1-Small-Region.svs"))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	img, err := mustLevel(t, slide, 0).DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width == 0 || img.Height == 0 {
		t.Errorf("decoded image has zero dimensions: %+v", img)
	}
	if img.Format != decoder.PixelFormatRGB {
		t.Errorf("default format: got %v, want PixelFormatRGB", img.Format)
	}
}

func TestImageDecodedTileWithJPEGDecoder(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide, err := opentile.OpenFile(filepath.Join(dir, "svs/CMU-1-Small-Region.svs"))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	img, err := mustImageLevel(t, slide, 0, 0).DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("ImageDecodedTile: %v", err)
	}
	if img.Width == 0 || img.Height == 0 {
		t.Errorf("decoded image has zero dimensions: %+v", img)
	}
}

func TestDecodedTileWithRGBAFormat(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide, err := opentile.OpenFile(filepath.Join(dir, "svs/CMU-1-Small-Region.svs"))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	img, err := mustLevel(t, slide, 0).DecodedTile(0, 0, opentile.WithFormat(decoder.PixelFormatRGBA))
	if err != nil {
		t.Fatalf("DecodedTile(RGBA): %v", err)
	}
	if img.Format != decoder.PixelFormatRGBA {
		t.Errorf("WithFormat(RGBA): got %v, want PixelFormatRGBA", img.Format)
	}
}
