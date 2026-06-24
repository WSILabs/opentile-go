package opentile

import (
	"context"
	"errors"
	"io"
	"iter"
	"sync/atomic"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// fillTile makes a tileW x tileH RGB image whose every pixel is (r,g,0).
func fillTile(w, h int, r, g byte) *decoder.Image {
	img := decoder.NewImageFormat(w, h, decoder.PixelFormatRGB)
	for i := 0; i+2 < len(img.Pix); i += 3 {
		img.Pix[i] = r
		img.Pix[i+1] = g
	}
	return img
}

func TestLevelStitchedGrid(t *testing.T) {
	l := &Level{Size: Size{W: 260, H: 180}, TileSize: Size{W: 100, H: 100}}
	if g := l.StitchedGrid(); g != (Size{W: 3, H: 2}) {
		t.Fatalf("StitchedGrid = %v, want {3,2}", g)
	}
}

func TestCompositeStitchedLoopBlitsIntersectingTiles(t *testing.T) {
	// One tile at origin (0,0), 100x100; dst covers stitched rect [0,0,100,100).
	rl := &fakeLayoutReader{originX: 0}
	dst := decoder.NewImageFormat(100, 100, decoder.PixelFormatRGB)
	fillWhite(dst)
	err := compositeStitchedLoop(rl, 0, 0, 0, 0, 0, 100, 100, 100, 100, dst,
		func(col, row int) (*decoder.Image, error) { return fillTile(100, 100, 42, 7), nil })
	if err != nil {
		t.Fatal(err)
	}
	if dst.Pix[0] != 42 || dst.Pix[1] != 7 {
		t.Fatalf("top-left = (%d,%d), want (42,7) — tile not blitted", dst.Pix[0], dst.Pix[1])
	}
}

// fakeStitchReader: a 3x2 grid of 100x100 tiles, origins at col*80, row*80
// (20px overlap). StitchedSize is 260x180. Each raw tile decodes to a solid
// color (R=col+1, G=row+1) and bumps decodeCount. Implements slideReader +
// regionLayout + decodedTiler.
type fakeStitchReader struct {
	decodeCount int64
	tileW       int
	tileH       int
}

const fakeRawCols, fakeRawRows, fakeStride = 3, 2, 80

func newFakeStitchReader() *fakeStitchReader {
	return &fakeStitchReader{tileW: 100, tileH: 100}
}

// --- regionLayout ---

func (f *fakeStitchReader) TileOrigin(level, col, row int) (int, int, bool) {
	if col < 0 || col >= fakeRawCols || row < 0 || row >= fakeRawRows {
		return 0, 0, false
	}
	return col * fakeStride, row * fakeStride, true
}

func (f *fakeStitchReader) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	var out []struct{ Col, Row int }
	x1, y1 := x+w, y+h
	for r := 0; r < fakeRawRows; r++ {
		for c := 0; c < fakeRawCols; c++ {
			ox, oy := c*fakeStride, r*fakeStride
			if ox < x1 && ox+f.tileW > x && oy < y1 && oy+f.tileH > y {
				out = append(out, struct{ Col, Row int }{c, r})
			}
		}
	}
	return out
}

func (f *fakeStitchReader) StitchedSize(level int) (int, int, bool) {
	return (fakeRawCols-1)*fakeStride + f.tileW, (fakeRawRows-1)*fakeStride + f.tileH, true
}

// --- decodedTiler ---

func (f *fakeStitchReader) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	atomic.AddInt64(&f.decodeCount, 1)
	return fillTile(f.tileW, f.tileH, byte(tx+1), byte(ty+1)), nil
}

// --- slideReader (only Level / Pyramids are exercised; the rest are stubs) ---

func (f *fakeStitchReader) Format() Format { return FormatBIF }
func (f *fakeStitchReader) Level(image, level int) (Level, error) {
	return Level{Size: Size{W: 260, H: 180}, TileSize: Size{W: f.tileW, H: f.tileH},
		Compression: CompressionJPEG, Overlapping: true}, nil
}
func (f *fakeStitchReader) Pyramids() []Pyramid {
	lvl, _ := f.Level(0, 0)
	return []Pyramid{{Levels: []Level{lvl}}}
}
func (f *fakeStitchReader) AssociatedImages() []AssociatedImage { return nil }
func (f *fakeStitchReader) Metadata() Metadata                 { return Metadata{} }
func (f *fakeStitchReader) ICCProfile() []byte                 { return nil }
func (f *fakeStitchReader) WarmLevel(image, level int) error   { return nil }
func (f *fakeStitchReader) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	return nil, errors.New("unused")
}
func (f *fakeStitchReader) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("unused")
}
func (f *fakeStitchReader) ImageTileMaxSize(image, level int) int     { return 0 }
func (f *fakeStitchReader) ImageTilePrefix(image, level int) []byte   { return nil }
func (f *fakeStitchReader) ImageTileBodyMaxSize(image, level int) int { return 0 }
func (f *fakeStitchReader) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("unused")
}
func (f *fakeStitchReader) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (f *fakeStitchReader) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[Point, TileResult] {
	return func(yield func(Point, TileResult) bool) {}
}
func (f *fakeStitchReader) Close() error { return nil }

func TestStitchedTileComposesAndCountsDecodeOnce(t *testing.T) {
	f := newFakeStitchReader()
	s := &Slide{r: f, readBudget: 64 << 20}

	lvl, _ := f.Level(0, 0)
	gw := ceilDiv(lvl.Size.W, lvl.TileSize.W)
	gh := ceilDiv(lvl.Size.H, lvl.TileSize.H)
	if gw != 3 || gh != 2 {
		t.Fatalf("canonical grid %dx%d, want 3x2", gw, gh)
	}

	for vy := 0; vy < gh; vy++ {
		for vx := 0; vx < gw; vx++ {
			img, err := s.imageStitchedTile(0, 0, vx, vy)
			if err != nil {
				t.Fatalf("stitched tile (%d,%d): %v", vx, vy, err)
			}
			if img.Width != 100 || img.Height != 100 {
				t.Fatalf("tile (%d,%d) dims %dx%d, want 100x100", vx, vy, img.Width, img.Height)
			}
			if img.Pix[0] == 0 {
				t.Fatalf("tile (%d,%d) top-left unpainted", vx, vy)
			}
		}
	}

	if got := atomic.LoadInt64(&f.decodeCount); got != int64(fakeRawCols*fakeRawRows) {
		t.Fatalf("decodeCount = %d, want %d (each raw frame once)", got, fakeRawCols*fakeRawRows)
	}
}

func TestStitchedTileScaleUnsupported(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(), readBudget: 64 << 20}
	if _, err := s.imageStitchedTile(0, 0, 0, 0, WithScale(2)); !errors.Is(err, decoder.ErrUnsupportedScale) {
		t.Fatalf("scale=2 err = %v, want ErrUnsupportedScale", err)
	}
}

func TestStitchedTileOutOfHullIsWhite(t *testing.T) {
	s := &Slide{r: newFakeStitchReader(), readBudget: 64 << 20}
	img, err := s.imageStitchedTile(0, 0, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(img.Pix); i++ {
		if img.Pix[i] != 0xFF {
			t.Fatalf("out-of-hull tile pixel[%d] = %d, want 255 (white)", i, img.Pix[i])
		}
	}
}

// noLayout forwards slideReader + decodedTiler to an inner fakeStitchReader but
// deliberately does NOT declare the regionLayout methods, so regionLayoutOf
// misses it and StitchedTile must delegate to DecodedTile. It must NOT embed
// *fakeStitchReader (Go would promote the layout methods).
type noLayout struct{ inner *fakeStitchReader }

func (n *noLayout) Format() Format                  { return n.inner.Format() }
func (n *noLayout) Level(i, l int) (Level, error)   { return n.inner.Level(i, l) }
func (n *noLayout) Pyramids() []Pyramid             { return n.inner.Pyramids() }
func (n *noLayout) AssociatedImages() []AssociatedImage { return nil }
func (n *noLayout) Metadata() Metadata              { return Metadata{} }
func (n *noLayout) ICCProfile() []byte              { return nil }
func (n *noLayout) WarmLevel(i, l int) error        { return nil }
func (n *noLayout) ImageRawTile(i, l, x, y int) ([]byte, error) { return nil, errors.New("unused") }
func (n *noLayout) ImageRawTileInto(i, l, x, y int, d []byte) (int, error) {
	return 0, errors.New("unused")
}
func (n *noLayout) ImageTileMaxSize(i, l int) int     { return 0 }
func (n *noLayout) ImageTilePrefix(i, l int) []byte   { return nil }
func (n *noLayout) ImageTileBodyMaxSize(i, l int) int { return 0 }
func (n *noLayout) ImageTileBodyInto(i, l, x, y int, d []byte) (int, error) {
	return 0, errors.New("unused")
}
func (n *noLayout) ImageTileReader(i, l, x, y int) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (n *noLayout) ImageRangeTiles(ctx context.Context, i, l int) iter.Seq2[Point, TileResult] {
	return func(yield func(Point, TileResult) bool) {}
}
func (n *noLayout) ImageDecodedTile(i, l, x, y int, o decoder.DecodeOptions) (*decoder.Image, error) {
	return n.inner.ImageDecodedTile(i, l, x, y, o)
}
func (n *noLayout) Close() error { return nil }

func TestStitchedTileDelegatesWithoutLayout(t *testing.T) {
	s := &Slide{r: &noLayout{newFakeStitchReader()}, readBudget: 64 << 20}
	got, err := s.imageStitchedTile(0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.imageDecodedTile(0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != want.Width || got.Pix[0] != want.Pix[0] {
		t.Fatal("without regionLayout, StitchedTile must equal DecodedTile")
	}
}
