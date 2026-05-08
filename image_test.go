package opentile

import (
	"context"
	"errors"
	"image"
	"io"
	"iter"
	"testing"
)

// TestSingleImage exercises the v0.6 SingleImage helper used by all
// single-pyramid formats (SVS, NDPI, Philips) to satisfy
// Tiler.Images().
func TestSingleImage(t *testing.T) {
	a := &fakeLevel{mpp: SizeMm{W: 0.25, H: 0.25}}
	b := &fakeLevel{mpp: SizeMm{W: 0.5, H: 0.5}}
	img := NewSingleImage([]Level{a, b})

	if got := img.Index(); got != 0 {
		t.Errorf("Index: got %d, want 0", got)
	}
	if got := img.Name(); got != "" {
		t.Errorf("Name: got %q, want empty", got)
	}
	if got := img.MPP(); got != (SizeMm{W: 0.25, H: 0.25}) {
		t.Errorf("MPP: got %v, want base level's MPP %v", got, a.mpp)
	}

	// Levels returns a fresh copy.
	lvls := img.Levels()
	if len(lvls) != 2 {
		t.Fatalf("Levels len: got %d, want 2", len(lvls))
	}
	lvls[0] = nil // mutate the returned slice
	if l, _ := img.Level(0); l != a {
		t.Errorf("internal state mutated when caller modified Levels() return")
	}

	if l, err := img.Level(1); err != nil || l != b {
		t.Errorf("Level(1): got (%v, %v), want (b, nil)", l, err)
	}
	if _, err := img.Level(-1); !errors.Is(err, ErrLevelOutOfRange) {
		t.Errorf("Level(-1): got err %v, want ErrLevelOutOfRange", err)
	}
	if _, err := img.Level(99); !errors.Is(err, ErrLevelOutOfRange) {
		t.Errorf("Level(99): got err %v, want ErrLevelOutOfRange", err)
	}
}

// TestSingleImageEmpty: NewSingleImage(nil) is the noopTiler / no-data
// case; MPP is zero, Level returns ErrLevelOutOfRange for any index.
func TestSingleImageEmpty(t *testing.T) {
	img := NewSingleImage(nil)
	if got := img.MPP(); got != (SizeMm{}) {
		t.Errorf("MPP on empty image: got %v, want zero SizeMm", got)
	}
	if l := img.Levels(); len(l) != 0 {
		t.Errorf("Levels on empty image: got len %d, want 0", len(l))
	}
	if _, err := img.Level(0); !errors.Is(err, ErrLevelOutOfRange) {
		t.Errorf("Level(0) on empty image: got err %v, want ErrLevelOutOfRange", err)
	}
}

// fakeLevel is a minimal Level for SingleImage tests; only MPP is
// exercised. The other accessors return zero values.
type fakeLevel struct {
	mpp SizeMm
}

func (f *fakeLevel) Index() int                                               { return 0 }
func (f *fakeLevel) PyramidIndex() int                                        { return 0 }
func (f *fakeLevel) Size() Size                                               { return Size{} }
func (f *fakeLevel) TileSize() Size                                           { return Size{} }
func (f *fakeLevel) Grid() Size                                               { return Size{} }
func (f *fakeLevel) Compression() Compression                                 { return CompressionUnknown }
func (f *fakeLevel) MPP() SizeMm                                              { return f.mpp }
func (f *fakeLevel) FocalPlane() float64                                      { return 0 }
func (f *fakeLevel) TileOverlap() image.Point                                 { return image.Point{} }
func (f *fakeLevel) Tile(x, y int) ([]byte, error)                            { return nil, nil }
func (f *fakeLevel) TileInto(x, y int, dst []byte) (int, error)               { return 0, nil }
func (f *fakeLevel) TileMaxSize() int                                         { return 0 }
func (f *fakeLevel) TilePrefix() []byte                                       { return nil }
func (f *fakeLevel) TileBodyInto(x, y int, dst []byte) (int, error)           { return 0, nil }
func (f *fakeLevel) TileBodyMaxSize() int                                     { return 0 }
func (f *fakeLevel) TileAt(coord TileCoord) ([]byte, error)                   { return nil, nil }
func (f *fakeLevel) TileReader(x, y int) (io.ReadCloser, error)               { return nil, nil }
func (f *fakeLevel) Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult] { return nil }
