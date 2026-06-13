//go:build parity

package oracle_test

import (
	"bytes"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/tests"
	"github.com/wsilabs/opentile-go/tests/oracle"
)

// tifffileSlideCandidates is the slide slate for the tifffile parity
// oracle — currently only OME-TIFF, where the multi-image deviation
// makes opentile-py insufficient as a sole reference. Each Image of a
// multi-image OME file gets compared, including the ones opentile-py
// drops via last-wins.
//
// Limited to TILED levels for now: OneFrame levels would require the
// upstream pad-extend-crop pipeline replicated in Python (PyTurboJPEG
// CUSTOMFILTER calls + JPEG SOF rewrite); committed integration
// fixture SHAs (TestSlideParity) catch regressions there.
var tifffileSlideCandidates = []string{
	"Leica-1.ome.tiff",
	"Leica-2.ome.tiff",
}

// TestTifffileParity is a multi-image-aware parity oracle. For each
// candidate slide:
//
//   - Open via opentile-go's public API.
//   - Walk every Pyramid in tiler.Pyramids() (matters for Leica-2 with
//     4 main pyramids — 3 of which opentile-py drops).
//   - For each TILED level in each Pyramid, sample tile positions and
//     byte-compare ours vs tifffile's raw bytes (no JPEGTables splice
//     for OME, per the v0.6 T5 audit). OneFrame levels are skipped.
//
// Catches: wrong SubIFD page selection, wrong tile-offset/count
// arrays, wrong planar-config indexing — all real failure modes the
// opentile-py oracle cannot test for the dropped pyramids.
func TestTifffileParity(t *testing.T) {
	dir := tests.TestdataDir()
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set; skipping tifffile parity test")
	}
	for _, name := range tifffileSlideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			runTifffileParityOnSlide(t, slide)
		})
	}
}

// bifTifffileSlideCandidates: slides where tifffile's serpentine-aware
// raw-byte read should match opentile-go's Tile() output verbatim.
// Restricted to fixtures WITHOUT shared JPEGTables (where opentile-go
// returns raw passthrough bytes — same as tifffile). Fixtures WITH
// shared JPEGTables (OS-1) modify the bytes via jpeg.InsertTables;
// those use the openslide oracle for pixel parity instead.
var bifTifffileSlideCandidates = []string{
	"Ventana-1.bif",
}

// TestTifffileParityBIF runs the tifffile oracle against BIF
// fixtures whose Tile() output is raw-passthrough (no JPEGTables
// splice). Parity bar: byte-equality between opentile-go's Tile(col,
// row) and tifffile's raw bytes at the same image-space position
// (with serpentine remap applied on the Python side).
//
// Catches: wrong serpentine algebra, wrong page selection (level=N
// ordering vs IFD index), wrong tile array indexing.
func TestTifffileParityBIF(t *testing.T) {
	dir := tests.TestdataDir()
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set; skipping tifffile-BIF parity test")
	}
	for _, name := range bifTifffileSlideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			runTifffileBIFParityOnSlide(t, slide)
		})
	}
}

func runTifffileBIFParityOnSlide(t *testing.T, slide string) {
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()
	if tiler.Format() != opentile.FormatBIF {
		t.Fatalf("not a BIF tiler: format=%s", tiler.Format())
	}

	sess, err := oracle.NewTifffileSession(slide)
	if err != nil {
		t.Fatalf("tifffile session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Logf("tifffile session close: %v", err)
		}
	}()

	for li, lvl := range tiler.Levels() {
		positions := samplePositions(lvl.Grid, false)
		for _, pos := range positions {
			our, err := lvl.Tile(pos.X, pos.Y)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): Go error: %v", li, pos.X, pos.Y, err)
				continue
			}
			theirs, err := sess.TileBIF(li, pos.X, pos.Y)
			if err != nil {
				t.Errorf("level %d tile (%d,%d): tifffile oracle error: %v", li, pos.X, pos.Y, err)
				continue
			}
			if theirs == nil {
				t.Errorf("level %d tile (%d,%d): tifffile returned zero-length (out-of-grid?)", li, pos.X, pos.Y)
				continue
			}
			if !bytes.Equal(our, theirs) {
				t.Errorf("slide %s level %d tile (%d,%d): byte-level divergence (go=%d bytes, tifffile=%d bytes)",
					filepath.Base(slide), li, pos.X, pos.Y, len(our), len(theirs))
			}
		}
	}
}

func runTifffileParityOnSlide(t *testing.T, slide string) {
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(tileSize, tileSize))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()

	sess, err := oracle.NewTifffileSession(slide)
	if err != nil {
		t.Fatalf("tifffile session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Logf("tifffile session close: %v", err)
		}
	}()

	for ii, img := range tiler.Pyramids() {
		for li, lvl := range img.Levels {
			// Only tiled levels — OneFrame uses a transformed pipeline
			// with no straight-byte tifffile reference. Detect via
			// TileSize() vs Grid() consistency: tiled pages have
			// page-derived TileSize, OneFrame pages get the
			// virtualised cfg.TileSize. We rely on Compression() and
			// the fact that OneFrame Image's underlying TileSize.W
			// matches base page tile dims (usually 512), while tiled
			// pages also have a TileWidth tag. The cleanest
			// discriminator is: ask the tifffile runner; it will
			// return zero-length on non-tiled levels.
			positions := samplePositions(lvl.Grid, false)
			for _, pos := range positions {
				our, err := lvl.Tile(pos.X, pos.Y)
				if err != nil {
					t.Errorf("image %d level %d tile (%d,%d): Go error: %v", ii, li, pos.X, pos.Y, err)
					continue
				}
				theirs, err := sess.Tile(ii, li, pos.X, pos.Y)
				if err != nil {
					t.Errorf("image %d level %d tile (%d,%d): tifffile oracle error: %v", ii, li, pos.X, pos.Y, err)
					continue
				}
				if theirs == nil {
					// Runner emitted zero-length: level is OneFrame
					// (no straight-byte tifffile reference). Skip
					// rest of this level silently.
					break
				}
				if !bytes.Equal(our, theirs) {
					t.Errorf("slide %s image %d level %d tile (%d,%d): byte-level divergence (go=%d bytes, tifffile=%d bytes)",
						filepath.Base(slide), ii, li, pos.X, pos.Y, len(our), len(theirs))
				}
			}
		}
	}
}
