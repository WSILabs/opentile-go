package ndpi_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPITileReaderMatchesTile locks in that TileReader returns the
// same bytes as RawTile for every level of a real NDPI slide. NDPI's
// stripped path assembles tiles from JPEG restart markers; this confirms the
// streamed and one-shot paths produce byte-identical output even on the
// non-trivial tile-assembly code path.
func TestNDPITileReaderMatchesTile(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(512, 512))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	for i, lvl := range tiler.Levels() {
		direct, err := lvl.Tile(0, 0)
		if err != nil {
			t.Errorf("RawTile(0,0) level %d: %v", i, err)
			continue
		}
		rc, err := lvl.TileReader(0, 0)
		if err != nil {
			t.Errorf("TileReader(0,0) level %d: %v", i, err)
			continue
		}
		streamed, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Errorf("ReadAll level %d: %v", i, err)
			continue
		}
		if !bytes.Equal(direct, streamed) {
			t.Errorf("level %d: TileReader bytes (%d) != RawTile bytes (%d)",
				i, len(streamed), len(direct))
		}
	}
}

// TestNDPITilesIterRowMajor locks in that RangeTiles yields every (x,y)
// position in row-major order with byte-identical content to RawTile(i,x,y) at
// the same position. Exercised on L3 of CMU-1.ndpi (the smallest grid —
// 2x2 = 4 tiles); L0 would be 7,500 tiles which is unreasonable for a unit
// test budget.
func TestNDPITilesIterRowMajor(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(512, 512))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	const levelIdx = 3
	lvl, err := tiler.Level(levelIdx)
	if err != nil {
		t.Fatal(err)
	}
	g := lvl.Grid
	want := make([]opentile.Point, 0, g.W*g.H)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			want = append(want, opentile.Point{X: x, Y: y})
		}
	}
	got := make([]opentile.Point, 0, len(want))
	for pos, res := range lvl.Tiles(context.Background()) {
		if res.Err != nil {
			t.Errorf("RangeTiles iter at %v: %v", pos, res.Err)
			continue
		}
		direct, err := lvl.Tile(pos.X, pos.Y)
		if err != nil {
			t.Errorf("RawTile(%d,%d): %v", pos.X, pos.Y, err)
			continue
		}
		if !bytes.Equal(direct, res.Bytes) {
			t.Errorf("tile (%d,%d): iter bytes (%d) != RawTile bytes (%d)",
				pos.X, pos.Y, len(res.Bytes), len(direct))
		}
		got = append(got, pos)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordering mismatch: got %v, want %v", got, want)
	}
}
