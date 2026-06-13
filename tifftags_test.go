package opentile

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

func TestTIFFTagGetters(t *testing.T) {
	ascii := TIFFTag{Number: 270, Type: TIFFASCII, Count: 3, ascii: "abc"}
	if s, ok := ascii.ASCII(); !ok || s != "abc" {
		t.Fatalf("ASCII() = %q,%v", s, ok)
	}
	if _, ok := ascii.Uints(); ok {
		t.Fatalf("Uints() should be false for ASCII tag")
	}
	u := TIFFTag{Number: 256, Type: TIFFShort, Count: 1, uints: []uint64{512}}
	if v, ok := u.Uints(); !ok || len(v) != 1 || v[0] != 512 {
		t.Fatalf("Uints() = %v,%v", v, ok)
	}
	r := TIFFTag{Number: 282, Type: TIFFRational, Count: 1, rationals: []Rational{{Num: 40, Denom: 1}}}
	if v, ok := r.Rationals(); !ok || v[0].Num != 40 {
		t.Fatalf("Rationals() = %v,%v", v, ok)
	}
}

func TestTIFFTagsLookup(t *testing.T) {
	ts := TIFFTags{
		{Number: 256, Name: "ImageWidth", Type: TIFFShort},
		{Number: 270, Name: "ImageDescription", Type: TIFFASCII},
	}
	if tag, ok := ts.Tag(270); !ok || tag.Name != "ImageDescription" {
		t.Fatalf("Tag(270) = %+v,%v", tag, ok)
	}
	if _, ok := ts.Tag(999); ok {
		t.Fatalf("Tag(999) should be false")
	}
	if tag, ok := ts.ByName("ImageWidth"); !ok || tag.Number != 256 {
		t.Fatalf("ByName = %+v,%v", tag, ok)
	}
}

func TestTiffTagsFromTranslatesAndFilters(t *testing.T) {
	raw := []tiff.RawTag{
		{Number: 256, Type: tiff.DTShort, Count: 1, Uints: []uint64{512}},
		{Number: 270, Type: tiff.DTASCII, Count: 3, ASCII: "abc"},
		{Number: 273, Type: tiff.DTLong, Count: 9, Uints: []uint64{1, 2, 3}}, // StripOffsets — must be dropped
		{Number: 324, Type: tiff.DTLong, Count: 9, Uints: []uint64{4, 5}},    // TileOffsets — must be dropped
		{Number: 65420, Type: tiff.DTLong, Count: 1, Uints: []uint64{7}},     // vendor/private — kept, no name
	}
	ts := tiffTagsFrom(raw)
	if _, ok := ts.Tag(273); ok {
		t.Fatalf("StripOffsets (273) should be filtered")
	}
	if _, ok := ts.Tag(324); ok {
		t.Fatalf("TileOffsets (324) should be filtered")
	}
	w, ok := ts.Tag(256)
	if !ok || w.Name != "ImageWidth" || w.Type != TIFFShort {
		t.Fatalf("ImageWidth not translated: %+v %v", w, ok)
	}
	v, ok := ts.Tag(65420)
	if !ok || v.Name != "" {
		t.Fatalf("vendor tag 65420 should be kept with empty name: %+v %v", v, ok)
	}
}

// fakeTagReader is a slideReader stub that also implements tiffTagProvider.
type fakeTagReader struct {
	slideReader
	dirs []TIFFDirectory
}

func (f fakeTagReader) TIFFDirectories() []TIFFDirectory { return f.dirs }

func TestSlideTIFFAccessors(t *testing.T) {
	s := &Slide{r: fakeTagReader{dirs: []TIFFDirectory{
		{Type: DirLevel, Image: 0, Level: 0, Tags: TIFFTags{{Number: 270, Name: "ImageDescription", Type: TIFFASCII}}},
		{Type: DirLevel, Image: 0, Level: 1, Tags: TIFFTags{{Number: 256, Name: "ImageWidth", Type: TIFFShort}}},
		{Type: DirAssociated, AssociatedType: "label", Tags: TIFFTags{{Number: 305, Name: "Software", Type: TIFFASCII}}},
		{Type: DirOther, Tags: TIFFTags{{Number: 65500, Type: TIFFLong}}},
	}}}

	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("LevelTIFFTags(0) ok=false")
	}
	if _, ok := tags.Tag(270); !ok {
		t.Fatal("level 0 missing tag 270")
	}
	tags1, ok := s.LevelTIFFTags(1)
	if !ok {
		t.Fatal("LevelTIFFTags(1) ok=false")
	}
	if _, ok := tags1.Tag(256); !ok {
		t.Fatal("level 1 missing tag 256")
	}
	if _, ok := s.LevelTIFFTags(9); ok {
		t.Fatal("out-of-range level should be ok=false")
	}
	all, ok := TIFFDirectoriesOf(s)
	if !ok || len(all) != 4 {
		t.Fatalf("TIFFDirectoriesOf = %d dirs, ok=%v", len(all), ok)
	}
}

// nonTIFFReader is a minimal slideReader stub that does NOT implement
// tiffTagProvider. Used to verify that non-TIFF accessors return ok=false.
type nonTIFFReader struct{}

func (nonTIFFReader) Format() Format                { return Format("test") }
func (nonTIFFReader) Pyramids() []Pyramid            { return nil }
func (nonTIFFReader) Level(_, _ int) (Level, error) { return Level{}, errors.New("stub") }
func (nonTIFFReader) AssociatedImages() []AssociatedImage { return nil }
func (nonTIFFReader) Metadata() Metadata            { return Metadata{} }
func (nonTIFFReader) ICCProfile() []byte            { return nil }
func (nonTIFFReader) WarmLevel(_, _ int) error      { return nil }
func (nonTIFFReader) ImageRawTile(_, _, _, _ int) ([]byte, error) {
	return nil, errors.New("stub")
}
func (nonTIFFReader) ImageRawTileInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, errors.New("stub")
}
func (nonTIFFReader) ImageTileMaxSize(_, _ int) int     { return 0 }
func (nonTIFFReader) ImageTilePrefix(_, _ int) []byte   { return nil }
func (nonTIFFReader) ImageTileBodyMaxSize(_, _ int) int { return 0 }
func (nonTIFFReader) ImageTileBodyInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, errors.New("stub")
}
func (nonTIFFReader) ImageTileReader(_, _, _, _ int) (io.ReadCloser, error) {
	return nil, errors.New("stub")
}
func (nonTIFFReader) ImageRangeTiles(_ context.Context, _, _ int) iter.Seq2[TilePos, TileResult] {
	return func(yield func(TilePos, TileResult) bool) {}
}
func (nonTIFFReader) Close() error { return nil }

func TestSlideTIFFAccessorsNonTIFF(t *testing.T) {
	s := &Slide{r: nonTIFFReader{}}
	if _, ok := s.LevelTIFFTags(0); ok {
		t.Fatal("non-TIFF should return ok=false")
	}
	if _, ok := TIFFDirectoriesOf(s); ok {
		t.Fatal("non-TIFF TIFFDirectoriesOf should be ok=false")
	}
}
