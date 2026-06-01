package opentile

import (
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
