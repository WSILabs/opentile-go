package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestLevelOverlappingContract verifies the GH #71 signal: stitched BIF levels
// report Overlapping=true (Grid does NOT tile Size), and ordinary formats
// report false (Grid tiles Size). Fixture-gated.
func TestLevelOverlappingContract(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	t.Run("stitched BIF L0 overlapping", func(t *testing.T) {
		path := filepath.Join(dir, "bif", "Ventana-1.bif")
		if _, err := os.Stat(path); err != nil {
			t.Skip("Ventana-1.bif not present")
		}
		s, err := opentile.OpenFile(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		levels := s.Levels()
		l0 := levels[0]
		if !l0.Overlapping {
			t.Errorf("stitched BIF L0: Overlapping=false, want true")
		}
		// The defining property: the raw grid does NOT tile the stitched Size.
		if l0.Grid.W*l0.TileSize.W <= l0.Size.W {
			t.Errorf("L0 Grid.W×TileSize.W (%d) should exceed Size.W (%d) for an overlapping level",
				l0.Grid.W*l0.TileSize.W, l0.Size.W)
		}
		// A lower (pre-stitched) level must NOT be overlapping.
		if len(levels) > 1 {
			last := levels[len(levels)-1]
			if last.Overlapping {
				t.Errorf("BIF lower level %d: Overlapping=true, want false (pre-stitched)", last.Index)
			}
		}
	})

	t.Run("ordinary format not overlapping", func(t *testing.T) {
		path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
		if _, err := os.Stat(path); err != nil {
			t.Skip("CMU-1-Small-Region.svs not present")
		}
		s, err := opentile.OpenFile(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for _, l := range s.Levels() {
			if l.Overlapping {
				t.Errorf("SVS level %d: Overlapping=true, want false", l.Index)
			}
			// Grid must tile Size (ceil) for a non-overlapping level.
			if l.Grid.W*l.TileSize.W < l.Size.W || l.Grid.H*l.TileSize.H < l.Size.H {
				t.Errorf("SVS level %d: Grid does not cover Size", l.Index)
			}
		}
	})
}
