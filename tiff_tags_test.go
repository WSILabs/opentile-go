package opentile

import "testing"

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
