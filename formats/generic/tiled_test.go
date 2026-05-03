package generic

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// openCMU1Generic opens CMU-1.tiff via the validator + level
// constructor, returning the level slice and a cleanup function.
// Used by the integration tests below.
func openCMU1Generic(t *testing.T) ([]*tiledImage, func()) {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		f.Close()
		t.Fatalf("tiff.Open: %v", err)
	}

	pages := tf.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	res, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	if err != nil {
		f.Close()
		t.Fatalf("ClassifyPyramid: %v", err)
	}

	levels := make([]*tiledImage, len(res.Pyramid))
	for i, info := range res.Pyramid {
		level, err := newTiledImage(i, i, pages[info.Index], f)
		if err != nil {
			f.Close()
			t.Fatalf("newTiledImage L%d: %v", i, err)
		}
		levels[i] = level
	}
	return levels, func() { f.Close() }
}

func TestTiledImage_CMU1_DimsAndCompression(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	if got := len(levels); got != 9 {
		t.Errorf("level count = %d, want 9", got)
	}
	// Spot-check L0 + L8 dims and tile size against the T1 gate
	// findings.
	if l0 := levels[0]; l0.Size().W != 46000 || l0.Size().H != 32914 {
		t.Errorf("L0 size = %v, want 46000×32914", l0.Size())
	}
	if l0 := levels[0]; l0.TileSize().W != 256 || l0.TileSize().H != 256 {
		t.Errorf("L0 tile size = %v, want 256×256", l0.TileSize())
	}
	if l8 := levels[8]; l8.Size().W != 179 || l8.Size().H != 128 {
		t.Errorf("L8 size = %v, want 179×128", l8.Size())
	}
	for i, l := range levels {
		if got := l.Compression(); got != opentile.CompressionJPEG {
			t.Errorf("L%d compression = %v, want JPEG", i, got)
		}
	}
}

func TestTiledImage_CMU1_TileBytesAreValidJPEG(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	// Sample one tile from each level; verify SOI + EOI markers
	// (the splice produces standalone valid JPEGs).
	for i, l := range levels {
		grid := l.Grid()
		if grid.W == 0 || grid.H == 0 {
			t.Errorf("L%d empty grid", i)
			continue
		}
		// Tile (0, 0) is always valid in a TIFF pyramid.
		b, err := l.Tile(0, 0)
		if err != nil {
			t.Errorf("L%d Tile(0,0): %v", i, err)
			continue
		}
		if len(b) < 4 {
			t.Errorf("L%d Tile(0,0) too short: %d bytes", i, len(b))
			continue
		}
		// SOI = ff d8.
		if b[0] != 0xFF || b[1] != 0xD8 {
			t.Errorf("L%d Tile(0,0) missing SOI marker; first 4 = % x", i, b[:4])
		}
		// EOI = ff d9. Search the last 64 bytes (should be at the end).
		tail := b
		if len(tail) > 64 {
			tail = tail[len(tail)-64:]
		}
		if !bytes.Contains(tail, []byte{0xFF, 0xD9}) {
			t.Errorf("L%d Tile(0,0) missing EOI marker in trailing bytes", i)
		}
	}
}

func TestTiledImage_CMU1_TileEqualsTileInto(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	for i, l := range levels {
		grid := l.Grid()
		if grid.W == 0 || grid.H == 0 {
			continue
		}
		// Sample 4 deterministic positions: corners + middle.
		positions := []struct{ x, y int }{
			{0, 0},
			{grid.W - 1, 0},
			{0, grid.H - 1},
			{grid.W - 1, grid.H - 1},
		}
		if grid.W > 2 && grid.H > 2 {
			positions = append(positions, struct{ x, y int }{grid.W / 2, grid.H / 2})
		}

		buf := make([]byte, l.TileMaxSize())
		for _, p := range positions {
			a, errA := l.Tile(p.x, p.y)
			n, errB := l.TileInto(p.x, p.y, buf)
			if (errA == nil) != (errB == nil) {
				t.Errorf("L%d (%d,%d): Tile err=%v, TileInto err=%v",
					i, p.x, p.y, errA, errB)
				continue
			}
			if errA != nil {
				continue
			}
			if !bytes.Equal(a, buf[:n]) {
				t.Errorf("L%d (%d,%d): Tile %d bytes != TileInto %d bytes",
					i, p.x, p.y, len(a), n)
			}
		}
	}
}

func TestTiledImage_CMU1_OOBAndCorruptErrors(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	l0 := levels[0]
	grid := l0.Grid()
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"negative x", -1, 0},
		{"negative y", 0, -1},
		{"x past grid", grid.W, 0},
		{"y past grid", 0, grid.H},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := l0.Tile(tc.x, tc.y)
			if !errors.Is(err, opentile.ErrTileOutOfBounds) {
				t.Errorf("got %v, want ErrTileOutOfBounds", err)
			}
		})
	}
}

func TestTiledImage_CMU1_TileAtRejectsNonZeroDims(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	l0 := levels[0]
	for _, coord := range []opentile.TileCoord{
		{X: 0, Y: 0, Z: 1},
		{X: 0, Y: 0, C: 1},
		{X: 0, Y: 0, T: 1},
	} {
		_, err := l0.TileAt(coord)
		if !errors.Is(err, opentile.ErrDimensionUnavailable) {
			t.Errorf("TileAt(%+v): got %v, want ErrDimensionUnavailable", coord, err)
		}
	}
}

func TestTiledImage_CMU1_TileReader(t *testing.T) {
	levels, cleanup := openCMU1Generic(t)
	defer cleanup()

	l0 := levels[0]
	// Sample tile via Tile() and TileReader(); confirm bytes match.
	want, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	rc, err := l0.TileReader(0, 0)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("TileReader bytes != Tile bytes (lengths %d / %d)", len(got), len(want))
	}
}

// TestTiledImage_CMU1_BothBackings verifies tile bytes are byte-
// identical whether opened via OpenFile (mmap default) or via
// Open with an os.File reader. This is the same contract we pinned
// in v0.9's TestOpenFileBackingsByteIdentical for the other formats;
// proving it for the generic-TIFF reader closes the same loop.
func TestTiledImage_CMU1_BothBackings(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}

	loadL0 := func(reader io.ReaderAt, size int64) (*tiledImage, error) {
		tf, err := tiff.Open(reader, size)
		if err != nil {
			return nil, err
		}
		pages := tf.Pages()
		infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
		for i, p := range pages {
			infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
		}
		res, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
		if err != nil {
			return nil, err
		}
		return newTiledImage(0, 0, pages[res.Pyramid[0].Index], reader)
	}

	// pread backing.
	f1, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pread: %v", err)
	}
	defer f1.Close()
	st, _ := f1.Stat()
	l0Pread, err := loadL0(f1, st.Size())
	if err != nil {
		t.Fatalf("loadL0 pread: %v", err)
	}

	// mmap backing.
	mm, err := tiff.OpenMmap(path)
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	defer mm.Close()
	l0Mmap, err := loadL0(mm, mm.Size())
	if err != nil {
		t.Fatalf("loadL0 mmap: %v", err)
	}

	// Sample a few tiles; compare bytes.
	grid := l0Pread.Grid()
	for _, p := range []struct{ x, y int }{
		{0, 0}, {grid.W - 1, 0}, {0, grid.H - 1}, {grid.W / 2, grid.H / 2},
	} {
		a, errA := l0Pread.Tile(p.x, p.y)
		b, errB := l0Mmap.Tile(p.x, p.y)
		if errA != nil || errB != nil {
			t.Errorf("(%d,%d): pread err=%v, mmap err=%v", p.x, p.y, errA, errB)
			continue
		}
		if !bytes.Equal(a, b) {
			t.Errorf("(%d,%d): pread %d bytes != mmap %d bytes", p.x, p.y, len(a), len(b))
		}
	}
}
