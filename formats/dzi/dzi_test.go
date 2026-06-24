package dzi_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// jpegTile encodes a solid-color w×h JPEG (stdlib encoder, no cgo).
func jpegTile(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func manifestXML(overlap, tileSize, w, h int) string {
	return fmt.Sprintf(`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" `+
		`Format="jpeg" Overlap="%d" TileSize="%d"><Size Width="%d" Height="%d"/></Image>`,
		overlap, tileSize, w, h)
}

// writeSyntheticDZI writes a complete bare DZI (manifest + every tile of every
// level) for a width×height image into dir, named "<base>.dzi". Returns the
// .dzi path. Each tile is a solid mid-gray JPEG sized to its clamped extent.
func writeSyntheticDZI(t *testing.T, dir, base string, width, height, tileSize, overlap int) string {
	t.Helper()
	dziPath := filepath.Join(dir, base+".dzi")
	if err := os.WriteFile(dziPath, []byte(manifestXML(overlap, tileSize, width, height)), 0o644); err != nil {
		t.Fatal(err)
	}
	filesDir := filepath.Join(dir, base+"_files")
	maxLevel := idzi.MaxLevel(width, height)
	for dziL := 0; dziL <= maxLevel; dziL++ {
		w, h := idzi.LevelDims(width, height, dziL)
		cols, rows := idzi.GridDims(w, h, tileSize)
		levelDir := filepath.Join(filesDir, fmt.Sprintf("%d", dziL))
		if err := os.MkdirAll(levelDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				tw := tileSize
				if (col+1)*tileSize > w {
					tw = w - col*tileSize
				}
				th := tileSize
				if (row+1)*tileSize > h {
					th = h - row*tileSize
				}
				if tw <= 0 || th <= 0 {
					continue
				}
				b := jpegTile(t, tw, th, color.RGBA{R: 128, G: 128, B: 128, A: 255})
				p := filepath.Join(levelDir, fmt.Sprintf("%d_%d.jpeg", col, row))
				if err := os.WriteFile(p, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return dziPath
}

func TestOpenBareDZIFromFilePath(t *testing.T) {
	dir := t.TempDir()
	dziPath := writeSyntheticDZI(t, dir, "img", 512, 512, 256, 0)

	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("OpenFile(%q): %v", dziPath, err)
	}
	defer s.Close()

	if s.Format() != opentile.FormatDZI {
		t.Fatalf("Format = %q, want dzi", s.Format())
	}
	l0, err := s.Level(0)
	if err != nil {
		t.Fatal(err)
	}
	if l0.Size != (opentile.Size{W: 512, H: 512}) {
		t.Fatalf("L0 Size = %v, want 512x512", l0.Size)
	}
	if l0.Grid != (opentile.Size{W: 2, H: 2}) {
		t.Fatalf("L0 Grid = %v, want 2x2", l0.Grid)
	}
	if l0.Overlapping {
		t.Fatal("DZI L0 must not be Overlapping (clean grid)")
	}
	raw, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0): %v", err)
	}
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Fatalf("tile not a JPEG (SOI missing): % x", raw[:min(2, len(raw))])
	}
	img, err := l0.DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("DecodedTile(0,0): %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Fatalf("DecodedTile dims = %dx%d, want 256x256", img.Width, img.Height)
	}
	if _, err := l0.Tile(9, 9); err == nil {
		t.Fatal("Tile(9,9) want out-of-bounds error")
	}
}

func TestOpenBareDZIFromDirPath(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticDZI(t, dir, "slide", 512, 512, 256, 0)
	s, err := opentile.OpenFile(dir) // directory containing exactly one .dzi
	if err != nil {
		t.Fatalf("OpenFile(dir): %v", err)
	}
	defer s.Close()
	if s.Format() != opentile.FormatDZI {
		t.Fatalf("Format = %q, want dzi", s.Format())
	}
}

func TestBareDZIMissingTile(t *testing.T) {
	dir := t.TempDir()
	dziPath := filepath.Join(dir, "img.dzi")
	if err := os.WriteFile(dziPath, []byte(manifestXML(0, 256, 512, 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	l0, _ := s.Level(0)
	if _, err := l0.Tile(0, 0); err == nil {
		t.Fatal("Tile(0,0) want missing-file error (no _files tree)")
	}
}

func TestBareDZIOverlapGuard(t *testing.T) {
	dir := t.TempDir()
	dziPath := filepath.Join(dir, "img.dzi")
	if err := os.WriteFile(dziPath, []byte(manifestXML(1, 256, 512, 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := opentile.OpenFile(dziPath); !errors.Is(err, idzi.ErrOverlapNotSupported) {
		t.Fatalf("Overlap=1 err = %v, want ErrOverlapNotSupported", err)
	}
}

func TestBareDZIDirWithoutManifestFallsThrough(t *testing.T) {
	dir := t.TempDir() // empty dir, no .dzi
	if _, err := opentile.OpenFile(dir); err == nil {
		t.Fatal("OpenFile(empty dir) should fail (hook falls through, no format matches)")
	} else if errors.Is(err, idzi.ErrOverlapNotSupported) {
		t.Fatal("empty dir must not surface the overlap sentinel")
	}
}
