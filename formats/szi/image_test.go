package szi_test

import (
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestImage_Name_ReturnsEmpty(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Pyramids()[0]
	if got := img.Name; got != "" {
		t.Errorf("Pyramid.Name = %q, want empty string", got)
	}
}

func TestImage_Level_Valid(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Pyramids()[0]
	levels := img.Levels

	// Level(0) should return the first level.
	got, err := tlr.Level(0)
	if err != nil {
		t.Fatalf("Slide.Level(0): %v", err)
	}
	if got != levels[0] {
		t.Errorf("Slide.Level(0) mismatch with Pyramids()[0].Levels[0]")
	}
}

func TestImage_Level_OutOfRange(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	// Negative index
	_, err := tlr.Level(-1)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(-1): got %v, want ErrLevelOutOfRange", err)
	}

	// Beyond max
	numLevels := len(tlr.Levels())
	_, err = tlr.Level(numLevels)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(%d): got %v, want ErrLevelOutOfRange", numLevels, err)
	}
}

func TestImage_MPP_EmptyLevels(t *testing.T) {
	// Verify MPP field on level 0 is zero (SZI surfaces MPP at metadata level).
	tlr := openCMU1(t)
	defer tlr.Close()

	if len(tlr.Levels()) > 0 {
		got := tlr.Levels()[0].MPP
		if got.X != 0 || got.Y != 0 {
			t.Errorf("L0.MPP = %v, want {0, 0} (SZI: MPP from scan-properties, not per-level)", got)
		}
	}
}

func TestTiler_Level_ValidIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	got, err := tlr.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	if got != levels[0] {
		t.Errorf("Level(0) mismatch with Levels()[0]")
	}
}

func TestTiler_Level_OutOfRange(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	numLevels := len(tlr.Levels())

	// Negative index
	_, err := tlr.Level(-1)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(-1): got %v, want ErrLevelOutOfRange", err)
	}

	// Beyond max
	_, err = tlr.Level(numLevels)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Level(%d): got %v, want ErrLevelOutOfRange", numLevels, err)
	}
}

func TestTiler_ICCProfile_ReturnsNil(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	if got := tlr.ICCProfile(); got != nil {
		t.Errorf("ICCProfile() = %v, want nil", got)
	}
}

func TestLevel_TileOverlap_ReturnsZeroPoint(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	level := tlr.Levels()[0]
	got := level.TileOverlap

	if got.X != 0 || got.Y != 0 {
		t.Errorf("Level.TileOverlap = %v, want {0, 0}", got)
	}
}

func TestLevel_Index_ReturnsOpentileSideIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	for i, level := range levels {
		if got := level.Index; got != i {
			t.Errorf("Level[%d].Index = %d, want %d", i, got, i)
		}
	}
}

func TestLevel_PyramidIndex_MatchesIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	for i, level := range levels {
		if got := level.PyramidIndex; got != level.Index {
			t.Errorf("Level[%d].PyramidIndex = %d, want %d (= Index)", i, got, level.Index)
		}
	}
}

func TestLevel_MPP_ReturnsZero(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	for i, level := range tlr.Levels() {
		got := level.MPP
		if got.X != 0 || got.Y != 0 {
			t.Errorf("Level[%d].MPP = %v, want {0, 0}", i, got)
		}
	}
}

func TestLevel_FocalPlane_ReturnsZero(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	for i, level := range tlr.Levels() {
		got := level.FocalPlane
		if got != 0 {
			t.Errorf("Level[%d].FocalPlane = %v, want 0", i, got)
		}
	}
}
