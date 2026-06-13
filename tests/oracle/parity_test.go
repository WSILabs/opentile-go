//go:build parity

package oracle_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/tests"
	"github.com/wsilabs/opentile-go/tests/oracle"
)

var fullParity = flag.Bool("parity-full", false, "walk every tile (slow) instead of sampling up to 10 positions per level")

var slideCandidates = []string{
	"CMU-1-Small-Region.svs",
	"CMU-1.svs",
	"JP2K-33003-1.svs",
	"scan_620_.svs",
	"svs_40x_bigtiff.svs",
	"CMU-1.ndpi",
	"OS-2.ndpi",
	"Philips-1.tiff",
	"Philips-2.tiff",
	"Philips-3.tiff",
	"Philips-4.tiff",
	"Leica-1.ome.tiff",
	"Leica-2.ome.tiff",
}

// tileSize is the output tile size both Go and Python use for parity. Keep
// these in lockstep: WithTileSize on the Go side and OPENTILE_TILE_SIZE on
// the Python side (set by oracle.Tile). Use 1024 matching the upstream
// Python opentile default.
const tileSize = 1024

func TestParityAgainstPython(t *testing.T) {
	dir := tests.TestdataDir()
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set; skipping parity test")
	}
	for _, name := range slideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			runParityOnSlide(t, slide)
		})
	}
}

func runParityOnSlide(t *testing.T, slide string) {
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(tileSize, tileSize))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()

	// One Python subprocess per slide — keeps the ~200 ms import + open
	// cost out of the per-tile path so we can sample ~100 positions per
	// level in the same wall-time budget as the v0.2 ~10-positions runs.
	sess, err := oracle.NewSession(slide, tileSize)
	if err != nil {
		t.Fatalf("oracle session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Logf("oracle session close: %v", err)
		}
	}()

	// Choose which Image to compare against Python opentile. For
	// single-image formats (SVS, NDPI, Philips, Leica-1) Pyramids() has
	// one entry; for multi-image OME (Leica-2) Python's last-wins
	// loop exposes the LAST main pyramid (Pyramids()[N-1]). Mirror that
	// selection so byte parity is comparing the same pyramid.
	images := tiler.Pyramids()
	if len(images) == 0 {
		t.Fatalf("slide %s exposes zero Pyramids", filepath.Base(slide))
	}
	pyImage := images[len(images)-1]
	for li, lvl := range pyImage.Levels {
		positions := samplePositions(lvl.Grid, *fullParity)
		for _, pos := range positions {
			our, err := tiler.ImageRawTile(pyImage.Index, li, pos.X, pos.Y)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): Go error: %v", li, pos.X, pos.Y, err)
				continue
			}
			theirs, err := sess.Tile(li, pos.X, pos.Y)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): Python oracle error: %v", li, pos.X, pos.Y, err)
				continue
			}
			if !bytes.Equal(our, theirs) {
				t.Errorf("slide %s level %d tile (%d,%d): byte-level divergence (go=%d bytes, py=%d bytes)",
					filepath.Base(slide), li, pos.X, pos.Y, len(our), len(theirs))
			}
		}
	}

	// Associated-image parity: byte-compare each Go AssociatedImage against
	// the Python equivalent. Python opentile exposes label/overview/thumbnail
	// via tiler.labels / tiler.overviews / tiler.thumbnails; the runner
	// dispatches on argv count and fetches the first image of the requested
	// type. If Python has no image of that type on this slide, the runner
	// emits zero-length stdout and we treat that as "skip" (the Go side may
	// still expose a synthesized image, e.g. NDPI's cropped-overview label).
	for _, a := range tiler.AssociatedImages() {
		// Skip parity for label images on every format. Python opentile
		// 0.20.0 returns only strip 0 of multi-strip labels (an upstream
		// bug — see L10); our Go side decode-restitch-encodes the full
		// image, so the byte streams legitimately diverge. We'll file an
		// upstream PR so this skip can be removed once Python lands the
		// same fix. Until then, skip uniformly (NDPI labels are also
		// affected since some are synthesized from cropped overviews).
		if a.Type() == "label" {
			t.Logf("slide %s associated %q: skipping parity (Python opentile 0.20.0 returns strip 0 only — see L10)",
				filepath.Base(slide), a.Type())
			continue
		}
		ourB, err := a.Bytes()
		if err != nil {
			t.Errorf("slide %s associated %q: Go error: %v", filepath.Base(slide), a.Type(), err)
			continue
		}
		theirB, err := sess.Associated(a.Type())
		if err != nil {
			t.Errorf("slide %s associated %q: Python oracle error: %v", filepath.Base(slide), a.Type(), err)
			continue
		}
		if len(theirB) == 0 {
			t.Logf("slide %s associated %q: not exposed by Python opentile; skipping parity check (Go synthesized %d bytes)",
				filepath.Base(slide), a.Type(), len(ourB))
			continue
		}
		if !bytes.Equal(ourB, theirB) {
			t.Errorf("slide %s associated %q: byte-level divergence (go=%d bytes, py=%d bytes)",
				filepath.Base(slide), a.Type(), len(ourB), len(theirB))
		}
	}
}

// samplePositions returns a reproducible set of tile positions covering the
// corners, edges, and interior, clamped to grid bounds. Full mode enumerates
// every position — opt in with -parity-full; it's minutes per slide, hours
// for OS-2.ndpi.
//
// Default (non-full) mode aims for ~100 distinct positions per level: the
// 10 deliberate corner / edge / diagonal anchors that have always covered
// the documented edge-tile code paths, plus a stride-based fill that
// covers ~10x10 = 100 interior samples on grids large enough to support
// the stride. Smaller grids contribute fewer samples (capped by the grid
// size itself). Deduplicated; ordering is row-major after the dedup.
func samplePositions(grid opentile.Size, full bool) []opentile.TilePos {
	if full {
		out := make([]opentile.TilePos, 0, grid.W*grid.H)
		for y := 0; y < grid.H; y++ {
			for x := 0; x < grid.W; x++ {
				out = append(out, opentile.TilePos{X: x, Y: y})
			}
		}
		return out
	}
	cand := []opentile.TilePos{
		{X: 0, Y: 0},
		{X: grid.W - 1, Y: 0},
		{X: 0, Y: grid.H - 1},
		{X: grid.W - 1, Y: grid.H - 1},
		{X: grid.W / 4, Y: grid.H / 4},
		{X: grid.W / 2, Y: grid.H / 2},
		{X: 3 * grid.W / 4, Y: 3 * grid.H / 4},
		{X: 1, Y: grid.H / 2},
		{X: grid.W / 2, Y: 1},
		{X: grid.W - 2, Y: grid.H - 2},
	}
	// Stride-based 10x10 fill. For grids smaller than 10 cells on either
	// axis, stride=1 and we just enumerate every position on that axis.
	const stride = 10
	stepX := grid.W / stride
	if stepX < 1 {
		stepX = 1
	}
	stepY := grid.H / stride
	if stepY < 1 {
		stepY = 1
	}
	for y := 0; y < grid.H; y += stepY {
		for x := 0; x < grid.W; x += stepX {
			cand = append(cand, opentile.TilePos{X: x, Y: y})
		}
	}
	seen := make(map[opentile.TilePos]bool)
	out := cand[:0]
	for _, p := range cand {
		if p.X < 0 || p.Y < 0 || p.X >= grid.W || p.Y >= grid.H {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func resolveSlide(dir, name string) (string, bool) {
	for _, sub := range []string{"", "svs", "ndpi", "philips-tiff", "ome-tiff", "bif"} {
		p := filepath.Join(dir, sub, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}
