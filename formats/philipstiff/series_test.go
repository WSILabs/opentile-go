package philipstiff

import (
	"reflect"
	"testing"
)

func TestClassifyPagesLevelsOnly(t *testing.T) {
	// Philips-1.tiff shape: 8 tiled pages, no associated.
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>...</DataObject>"},
		{Tiled: true, Description: "level=1 mag=22 quality=80"},
		{Tiled: true, Description: "level=2 mag=11 quality=80"},
		{Tiled: true, Description: "level=3 mag=5.5 quality=80"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	want := philipsClassification{
		Levels:    []int{0, 1, 2, 3},
		Macro:     -1,
		Label:     -1,
		Thumbnail: -1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClassifyPagesLevelsPlusMacro(t *testing.T) {
	// Philips-2.tiff shape: 10 tiled pages + Macro.
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>...</DataObject>"},
		{Tiled: true, Description: "level=1 mag=20.5 quality=80"},
		{Tiled: false, Description: "Macro"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	if !reflect.DeepEqual(got.Levels, []int{0, 1}) {
		t.Errorf("Levels: got %v, want [0,1]", got.Levels)
	}
	if got.Macro != 2 {
		t.Errorf("Macro: got %d, want 2", got.Macro)
	}
	if got.Label != -1 || got.Thumbnail != -1 {
		t.Errorf("Label/Thumbnail should be -1; got Label=%d Thumbnail=%d", got.Label, got.Thumbnail)
	}
}

func TestClassifyPagesLevelsMacroLabel(t *testing.T) {
	// Philips-3.tiff shape: 9 tiled + Macro + Label.
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>"},
		{Tiled: true, Description: "level=1"},
		{Tiled: true, Description: "level=2"},
		{Tiled: false, Description: "Macro"},
		{Tiled: false, Description: "Label"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	if !reflect.DeepEqual(got.Levels, []int{0, 1, 2}) {
		t.Errorf("Levels: got %v, want [0,1,2]", got.Levels)
	}
	if got.Macro != 3 {
		t.Errorf("Macro: got %d, want 3", got.Macro)
	}
	if got.Label != 4 {
		t.Errorf("Label: got %d, want 4", got.Label)
	}
}

func TestClassifyPagesMacroWithSuffix(t *testing.T) {
	// Philips-4.tiff: macro description is "Macro -offset=(0,0)-pixelsize=(0.0315,0.0315)..."
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>"},
		{Tiled: false, Description: "Macro -offset=(0,0)-pixelsize=(0.0315,0.0315)-rois=(...)"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	if got.Macro != 1 {
		t.Errorf("Macro: got %d, want 1 (substring match should accept suffix params)", got.Macro)
	}
}

func TestClassifyPagesAllAssociated(t *testing.T) {
	// Hypothetical: levels + macro + label + thumbnail. None of our 4
	// fixtures has a thumbnail, but upstream supports it.
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>"},
		{Tiled: false, Description: "Macro"},
		{Tiled: false, Description: "Label"},
		{Tiled: false, Description: "Thumbnail"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	if got.Macro != 1 || got.Label != 2 || got.Thumbnail != 3 {
		t.Errorf("got Macro=%d Label=%d Thumbnail=%d; want 1, 2, 3", got.Macro, got.Label, got.Thumbnail)
	}
}

func TestClassifyPagesEmptyError(t *testing.T) {
	if _, err := classifyPages(nil); err == nil {
		t.Error("expected error on empty page list")
	}
}

func TestClassifyPagesBaseNotTiledError(t *testing.T) {
	metas := []philipsPageMeta{
		{Tiled: false, Description: "<DataObject>"},
	}
	if _, err := classifyPages(metas); err == nil {
		t.Error("expected error when base page is not tiled")
	}
}

func TestClassifyPagesUnknownAssociated(t *testing.T) {
	// Non-tiled trailing page with a description that doesn't match any
	// known type — silently ignored, matching upstream.
	metas := []philipsPageMeta{
		{Tiled: true, Description: "<DataObject>"},
		{Tiled: false, Description: "SomethingElse"},
	}
	got, err := classifyPages(metas)
	if err != nil {
		t.Fatalf("classifyPages: %v", err)
	}
	if got.Macro != -1 || got.Label != -1 || got.Thumbnail != -1 {
		t.Errorf("expected all associated -1 on unknown trailing description; got %+v", got)
	}
}
