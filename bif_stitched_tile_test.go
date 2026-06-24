package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestBIFStitchedTileEqualsReadRegion(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "bif", "Ventana-1.bif")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	s, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	var lvl *opentile.Level
	for _, l := range s.Levels() {
		if l.Overlapping {
			lvl = l
			break
		}
	}
	if lvl == nil {
		t.Skip("no overlapping level in fixture")
	}

	g := lvl.StitchedGrid()
	tw, th := lvl.TileSize.W, lvl.TileSize.H
	if g.W != (lvl.Size.W+tw-1)/tw || g.H != (lvl.Size.H+th-1)/th {
		t.Fatalf("StitchedGrid %v does not tile Size %v with TileSize %v", g, lvl.Size, lvl.TileSize)
	}

	coords := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {g.W / 2, g.H / 2}}
	for _, c := range coords {
		vx, vy := c[0], c[1]
		if vx >= g.W || vy >= g.H {
			continue
		}
		st, err := lvl.StitchedTile(vx, vy)
		if err != nil {
			t.Fatalf("StitchedTile(%d,%d): %v", vx, vy, err)
		}
		rr, err := lvl.ReadRegion(opentile.Region{
			Origin: opentile.Point{X: vx * tw, Y: vy * th},
			Size:   opentile.Size{W: tw, H: th},
		})
		if err != nil {
			t.Fatalf("ReadRegion(%d,%d): %v", vx, vy, err)
		}
		if len(st.Pix) != len(rr.Pix) {
			t.Fatalf("tile (%d,%d): len %d != region len %d", vx, vy, len(st.Pix), len(rr.Pix))
		}
		for i := range st.Pix {
			if st.Pix[i] != rr.Pix[i] {
				t.Fatalf("tile (%d,%d): pixel %d differs (stitched %d, region %d)", vx, vy, i, st.Pix[i], rr.Pix[i])
			}
		}
	}
}
