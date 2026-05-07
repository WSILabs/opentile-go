package ometiff

import (
	"reflect"
	"testing"
)

func TestClassifyImagesLeica1Shape(t *testing.T) {
	// Leica-1 shape: macro + 1 main pyramid (empty Name).
	imgs := []OMEImage{
		{Name: "macro"},
		{Name: ""},
	}
	got, err := classifyImages(imgs)
	if err != nil {
		t.Fatalf("classifyImages: %v", err)
	}
	want := omeClassification{
		LevelImages: []int{1},
		Macro:       0,
		Label:       -1,
		Thumbnail:   -1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClassifyImagesLeica2Shape(t *testing.T) {
	// Leica-2 shape: macro + 4 main pyramids. v0.6 deviation: we expose
	// all 4 main pyramids; upstream silently drops 3 of them.
	imgs := []OMEImage{
		{Name: "macro"},
		{Name: ""},
		{Name: ""},
		{Name: ""},
		{Name: ""},
	}
	got, err := classifyImages(imgs)
	if err != nil {
		t.Fatalf("classifyImages: %v", err)
	}
	want := omeClassification{
		LevelImages: []int{1, 2, 3, 4},
		Macro:       0,
		Label:       -1,
		Thumbnail:   -1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClassifyImagesAllAssociated(t *testing.T) {
	// Hypothetical: main pyramid + macro + label + thumbnail.
	imgs := []OMEImage{
		{Name: ""},
		{Name: "macro"},
		{Name: "label"},
		{Name: "thumbnail"},
	}
	got, err := classifyImages(imgs)
	if err != nil {
		t.Fatalf("classifyImages: %v", err)
	}
	want := omeClassification{
		LevelImages: []int{0},
		Macro:       1,
		Label:       2,
		Thumbnail:   3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestClassifyImagesStripsWhitespace mirrors upstream's
// `series.name.strip()` semantics — names with surrounding
// whitespace classify the same as bare ones.
func TestClassifyImagesStripsWhitespace(t *testing.T) {
	imgs := []OMEImage{
		{Name: "  macro "},
		{Name: ""},
	}
	got, err := classifyImages(imgs)
	if err != nil {
		t.Fatalf("classifyImages: %v", err)
	}
	if got.Macro != 0 {
		t.Errorf("Macro: got %d, want 0 (whitespace-padded 'macro' should classify as Macro)", got.Macro)
	}
}

// TestClassifyImagesNoLevelImages: every Image is associated, no main
// pyramid. Surface as an error since the file is unusable for tile
// extraction.
func TestClassifyImagesNoLevelImages(t *testing.T) {
	imgs := []OMEImage{
		{Name: "macro"},
		{Name: "label"},
	}
	if _, err := classifyImages(imgs); err == nil {
		t.Error("expected error when no main pyramid Images present")
	}
}

// TestClassifyImagesEmpty: empty Image list is malformed.
func TestClassifyImagesEmpty(t *testing.T) {
	if _, err := classifyImages(nil); err == nil {
		t.Error("expected error on empty Image list")
	}
}

// TestClassifyImagesDuplicateMacro: upstream's loop overwrites on each
// match (last-wins) for label / macro / thumbnail. We match that
// behaviour for those three since they're "one each" by file
// convention.
func TestClassifyImagesDuplicateMacro(t *testing.T) {
	imgs := []OMEImage{
		{Name: "macro"},
		{Name: ""},
		{Name: "macro"}, // second macro — last wins
	}
	got, err := classifyImages(imgs)
	if err != nil {
		t.Fatalf("classifyImages: %v", err)
	}
	if got.Macro != 2 {
		t.Errorf("Macro: got %d, want 2 (last-wins for associated)", got.Macro)
	}
	if !reflect.DeepEqual(got.LevelImages, []int{1}) {
		t.Errorf("LevelImages: got %v, want [1]", got.LevelImages)
	}
}
