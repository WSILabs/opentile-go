package svs_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestSVSTileReaderMatchesTile locks in that Level.TileReader returns the
// same bytes as Level.Tile for every level of a real SVS slide. v0.2 had no
// direct test for TileReader; correctness was inferred from the integration
// suite via Tile-only checks.
func TestSVSTileReaderMatchesTile(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	for i, lvl := range tiler.Levels() {
		direct, err := lvl.Tile(0, 0)
		if err != nil {
			t.Errorf("Tile(0,0) level %d: %v", i, err)
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
			t.Errorf("level %d: TileReader bytes (%d) != Tile bytes (%d)",
				i, len(streamed), len(direct))
		}
	}
}

// TestSVSTilesIterRowMajor locks in that Level.Tiles yields every (x,y)
// position in row-major order with byte-identical content to Tile(x,y) at
// the same position. Exercised on L0 of CMU-1-Small-Region.svs (12 tiles —
// small enough for a full walk).
func TestSVSTilesIterRowMajor(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	lvl, err := tiler.Level(0)
	if err != nil {
		t.Fatal(err)
	}
	g := lvl.Grid()
	want := make([]opentile.TilePos, 0, g.W*g.H)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			want = append(want, opentile.TilePos{X: x, Y: y})
		}
	}
	got := make([]opentile.TilePos, 0, len(want))
	for pos, res := range lvl.Tiles(context.Background()) {
		if res.Err != nil {
			t.Errorf("Tiles iter at %v: %v", pos, res.Err)
			continue
		}
		direct, err := lvl.Tile(pos.X, pos.Y)
		if err != nil {
			t.Errorf("Tile(%d,%d): %v", pos.X, pos.Y, err)
			continue
		}
		if !bytes.Equal(direct, res.Bytes) {
			t.Errorf("tile (%d,%d): iter bytes (%d) != Tile bytes (%d)",
				pos.X, pos.Y, len(res.Bytes), len(direct))
		}
		got = append(got, pos)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordering mismatch: got %d positions (first %v...), want %d (first %v...)",
			len(got), firstN(got, 4), len(want), firstN(want, 4))
	}
}

func firstN(s []opentile.TilePos, n int) []opentile.TilePos {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// TestSpliceReconstitutionInvariant verifies the v0.13 invariant:
// SpliceJPEGTile(TilePrefix(), TileBodyInto(p)) ==byte== Tile(p)
// for sampled positions on every pyramid level of CMU-1-Small-Region.svs.
func TestSpliceReconstitutionInvariant(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	tiler, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()

	for li, lvl := range tiler.Levels() {
		prefix := lvl.TilePrefix()
		if len(prefix) == 0 {
			t.Errorf("L%d: TilePrefix is empty (SVS pyramid levels always have shared JPEGTables)", li)
			continue
		}
		bodyBuf := make([]byte, lvl.TileBodyMaxSize())
		grid := lvl.Grid()
		if grid.W == 0 || grid.H == 0 {
			continue
		}
		positions := []struct{ x, y int }{
			{0, 0},
			{grid.W - 1, 0},
			{0, grid.H - 1},
			{grid.W - 1, grid.H - 1},
		}
		if grid.W > 2 && grid.H > 2 {
			positions = append(positions, struct{ x, y int }{grid.W / 2, grid.H / 2})
		}
		for _, p := range positions {
			full, errFull := lvl.Tile(p.x, p.y)
			if errFull != nil {
				continue
			}
			n, errBody := lvl.TileBodyInto(p.x, p.y, bodyBuf)
			if errBody != nil {
				t.Errorf("L%d (%d,%d) TileBodyInto: %v", li, p.x, p.y, errBody)
				continue
			}
			reconstituted, err := opentile.SpliceJPEGTile(prefix, bodyBuf[:n])
			if err != nil {
				t.Errorf("L%d (%d,%d) SpliceJPEGTile: %v", li, p.x, p.y, err)
				continue
			}
			if !bytes.Equal(full, reconstituted) {
				t.Errorf("L%d (%d,%d): reconstituted (%d bytes) != Tile() (%d bytes)",
					li, p.x, p.y, len(reconstituted), len(full))
			}
		}
	}
}

// TestParseDescription_GrundiumFixture verifies the v0.18 writer-vendor
// detection on a real Grundium-written SVS fixture. Pre-v0.18 the parser
// hardcoded ScannerManufacturer="Aperio" for all SVS regardless of writer,
// causing Grundium-scanned slides to misattribute. Now detectWriter parses
// the ImageDescription first line ("Aperio Image, Grundium Ocus") and
// surfaces the actual writer + namespaces Properties under "grundium.<key>"
// instead of "aperio.<key>".
func TestParseDescription_GrundiumFixture(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "scan_620_.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skip("scan_620_.svs not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()
	md := tlr.Metadata()
	if md.ScannerManufacturer != "Grundium" {
		t.Errorf("ScannerManufacturer = %q, want Grundium", md.ScannerManufacturer)
	}
	if md.ScannerModel != "Ocus" {
		t.Errorf("ScannerModel = %q, want Ocus", md.ScannerModel)
	}
	if got := md.Properties["grundium.MPP"]; got == "" {
		t.Errorf("Properties[grundium.MPP] empty; want non-empty (Grundium namespace)")
	}
	if got := md.Properties["aperio.MPP"]; got != "" {
		t.Errorf("Properties[aperio.MPP] = %q, want empty (writer is Grundium not Aperio)", got)
	}
}
