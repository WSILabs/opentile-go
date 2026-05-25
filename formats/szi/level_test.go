package szi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// openCMU1 is a small helper that opens the CMU-1.szi fixture or
// skips the test when the fixture is unavailable.
func openCMU1(t *testing.T) *opentile.Slide {
	t.Helper()
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return tlr
}

func TestLevels_CMU1Geometry(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	// Image is 2220×2967; max(W,H) = 2967 → MaxLevel =
	// ceil(log2(2967)) = 12. So 13 total levels (0..12).
	if len(levels) != 13 {
		t.Fatalf("Levels: got %d, want 13", len(levels))
	}

	// opentile L0 = DZI L12 = full resolution 2220×2967.
	if got := levels[0].Size(); got.W != 2220 || got.H != 2967 {
		t.Errorf("L0 Size = %v, want {W:2220 H:2967}", got)
	}
	if got := levels[0].TileSize(); got.W != 256 || got.H != 256 {
		t.Errorf("L0 TileSize = %v, want {256, 256}", got)
	}
	// 9 = ceil(2220/256), 12 = ceil(2967/256).
	if got := levels[0].Grid(); got.W != 9 || got.H != 12 {
		t.Errorf("L0 Grid = %v, want {9, 12}", got)
	}
	if got := levels[0].Compression(); got != opentile.CompressionJPEG {
		t.Errorf("L0 Compression = %v, want %v", got, opentile.CompressionJPEG)
	}

	// Lowest opentile level (highest DZI = 0): 1×1, single tile.
	last := levels[len(levels)-1]
	if got := last.Size(); got.W != 1 || got.H != 1 {
		t.Errorf("L%d Size = %v, want {1, 1}", len(levels)-1, got)
	}
	if got := last.Grid(); got.W != 1 || got.H != 1 {
		t.Errorf("L%d Grid = %v, want {1, 1}", len(levels)-1, got)
	}
}

func TestImages_CMU1SingleImage(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	images := tlr.Images()
	if len(images) != 1 {
		t.Fatalf("Images: got %d, want 1", len(images))
	}
	if got := images[0].Index(); got != 0 {
		t.Errorf("Image.Index = %d, want 0", got)
	}
	if got := images[0].SizeZ(); got != 1 {
		t.Errorf("Image.SizeZ = %d, want 1", got)
	}
	if got := images[0].SizeC(); got != 1 {
		t.Errorf("Image.SizeC = %d, want 1", got)
	}
	if got := images[0].SizeT(); got != 1 {
		t.Errorf("Image.SizeT = %d, want 1", got)
	}
	if got := len(images[0].Levels()); got != 13 {
		t.Errorf("Image.Levels(): got %d, want 13", got)
	}
}

func TestTile_CMU1_FirstTileIsJPEG(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	tile, err := tlr.Levels()[0].Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0, 0): %v", err)
	}
	// JPEG SOI marker: FF D8 FF.
	soi := []byte{0xFF, 0xD8, 0xFF}
	if !bytes.HasPrefix(tile, soi) {
		end := 8
		if len(tile) < end {
			end = len(tile)
		}
		t.Errorf("L0 tile does not start with JPEG SOI: got % x", tile[:end])
	}
}

func TestTileInto_CMU1_MatchesTile(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	l0 := tlr.Levels()[0]
	want, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0, 0): %v", err)
	}

	dst := make([]byte, l0.TileMaxSize())
	n, err := l0.TileInto(0, 0, dst)
	if err != nil {
		t.Fatalf("TileInto(0, 0): %v", err)
	}
	if !bytes.Equal(dst[:n], want) {
		t.Errorf("TileInto != Tile: TileInto produced %d bytes, Tile produced %d", n, len(want))
	}
}

func TestTileInto_ShortBuffer(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	dst := make([]byte, 1)
	n, err := tlr.Levels()[0].TileInto(0, 0, dst)
	if !errors.Is(err, io.ErrShortBuffer) {
		t.Errorf("TileInto with tiny dst: got (%d, %v), want (_, io.ErrShortBuffer)", n, err)
	}
}

func TestTileBody_CMU1_DelegatesToTile(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	l0 := tlr.Levels()[0]
	if got := l0.TilePrefix(); got != nil {
		t.Errorf("TilePrefix: got %d bytes, want nil", len(got))
	}
	if got, want := l0.TileBodyMaxSize(), l0.TileMaxSize(); got != want {
		t.Errorf("TileBodyMaxSize = %d, want TileMaxSize = %d", got, want)
	}

	want, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0, 0): %v", err)
	}
	dst := make([]byte, l0.TileBodyMaxSize())
	n, err := l0.TileBodyInto(0, 0, dst)
	if err != nil {
		t.Fatalf("TileBodyInto(0, 0): %v", err)
	}
	if !bytes.Equal(dst[:n], want) {
		t.Errorf("TileBodyInto != Tile (TilePrefix is nil — should be identical)")
	}
}

func TestTile_OutOfBoundsReturnsSentinel(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	_, err := tlr.Levels()[0].Tile(99, 99)
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("OOB tile: got %v, want ErrTileOutOfBounds", err)
	}
}

func TestTileReader_CMU1(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	rc, err := tlr.Levels()[0].TileReader(0, 0)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	soi := []byte{0xFF, 0xD8, 0xFF}
	if !bytes.HasPrefix(got, soi) {
		t.Errorf("TileReader bytes do not start with JPEG SOI")
	}
}

func TestTileAt_RejectsNonZeroDimension(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	_, err := tlr.Levels()[0].TileAt(opentile.TileCoord{X: 0, Y: 0, Z: 1})
	if !errors.Is(err, opentile.ErrDimensionUnavailable) {
		t.Errorf("TileAt with Z=1: got %v, want ErrDimensionUnavailable", err)
	}
}

func TestTiles_IteratesGrid(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	// Use a small (low-res) level so the iteration is cheap.
	levels := tlr.Levels()
	last := levels[len(levels)-1]
	count := 0
	for _, res := range last.Tiles(context.Background()) {
		if res.Err != nil {
			t.Fatalf("Tiles iter: %v", res.Err)
		}
		count++
	}
	want := last.Grid().W * last.Grid().H
	if count != want {
		t.Errorf("Tiles count = %d, want %d", count, want)
	}
}

func TestWarmLevel_OutOfRange(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	if err := tlr.WarmLevel(-1); !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("WarmLevel(-1): got %v, want ErrLevelOutOfRange", err)
	}
	if err := tlr.WarmLevel(99); !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("WarmLevel(99): got %v, want ErrLevelOutOfRange", err)
	}
	// In-range warm — currently a no-op stub for v0.16 but must
	// still return nil for valid indices.
	if err := tlr.WarmLevel(0); err != nil {
		t.Errorf("WarmLevel(0): got %v, want nil", err)
	}
}
