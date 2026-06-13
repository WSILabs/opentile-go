package ometiff_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/ometiff"
)

// TestOMEAccessors exercises Image / Tiler accessors that the unit
// tests don't cover (TileReader, RangeTiles iterator, Level shortcuts,
// MetadataOf). Skips when no Leica fixture is reachable, so it stays
// out of CI without integration data.
func TestOMEAccessors(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ome-tiff", "Leica-1.ome.tiff")
	if _, err := os.Stat(slide); err != nil {
		t.Skip("Leica-1.ome.tiff not present under OPENTILE_TESTDIR/ome-tiff/")
	}

	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(1024, 1024))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tiler.Close()

	// Tiler-level shortcuts
	if got := tiler.Format(); got != opentile.FormatOMETIFF {
		t.Errorf("Format: got %q, want %q", got, opentile.FormatOMETIFF)
	}
	if got := tiler.Levels(); len(got) == 0 {
		t.Error("Levels: empty slice")
	}
	if _, err := tiler.Level(0); err != nil {
		t.Errorf("Level(0): %v", err)
	}
	if _, err := tiler.Level(-1); !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(-1): want ErrLevelOutOfRange, got %v", err)
	}
	_ = tiler.Metadata()
	_ = tiler.ICCProfile()

	// Pyramid-level (value-type struct fields)
	imgs := tiler.Pyramids()
	if len(imgs) == 0 {
		t.Fatal("Pyramids: empty slice")
	}
	img := imgs[0]
	if img.Index != 0 {
		t.Errorf("Image.Index: got %d, want 0", img.Index)
	}
	_ = img.Name // may be empty for main pyramid
	if got := img.Levels; len(got) == 0 {
		t.Error("Image.Levels: empty slice")
	}
	if _, err := tiler.Level(0); err != nil {
		t.Errorf("Level(0): %v", err)
	}
	if _, err := tiler.Level(-1); !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(-1): want ErrLevelOutOfRange, got %v", err)
	}

	// Level-level: TileReader + RangeTiles iterator
	rc, err := mustLevel(t, tiler, 0).TileReader(0, 0)
	if err != nil {
		t.Fatalf("TileReader(0, 0, 0): %v", err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Errorf("TileReader read: %v", err)
	}

	// Iterate just a couple of tiles via RangeTiles (canceling
	// after a few yields keeps test runtime bounded).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := 0
	for pos, res := range mustLevel(t, tiler, 0).Tiles(ctx) {
		_ = pos
		if res.Err != nil {
			t.Errorf("RangeTiles iterator yielded error: %v", res.Err)
			break
		}
		count++
		if count >= 3 {
			cancel()
			break
		}
	}
	if count == 0 {
		t.Error("RangeTiles iterator yielded zero entries")
	}

	// Format-specific metadata accessor
	if md, ok := ometiff.MetadataOf(tiler); !ok {
		t.Error("ometiff.MetadataOf: false on an OME tiler")
	} else if len(md.Images) == 0 {
		t.Error("ometiff.MetadataOf returned zero images")
	}
}
