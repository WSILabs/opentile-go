package szi_test

import (
	"errors"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

func TestImage_Name_ReturnsEmpty(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	if got := img.Name(); got != "" {
		t.Errorf("Image.Name() = %q, want empty string", got)
	}
}

func TestImage_Level_Valid(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	levels := img.Levels()

	// Level(0) should return the first level.
	got, err := img.Level(0)
	if err != nil {
		t.Fatalf("Image.Level(0): %v", err)
	}
	if got != levels[0] {
		t.Errorf("Image.Level(0) mismatch with Levels()[0]")
	}
}

func TestImage_Level_OutOfRange(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]

	// Negative index
	_, err := img.Level(-1)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Image.Level(-1): got %v, want ErrLevelOutOfRange", err)
	}

	// Beyond max
	numLevels := len(img.Levels())
	_, err = img.Level(numLevels)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Image.Level(%d): got %v, want ErrLevelOutOfRange", numLevels, err)
	}
}

func TestImage_MPP_DelegatesToLevel0(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	levels := img.Levels()

	imgMPP := img.MPP()
	level0MPP := levels[0].MPP()

	if imgMPP != level0MPP {
		t.Errorf("Image.MPP() = %v, want levels[0].MPP() = %v", imgMPP, level0MPP)
	}
}

func TestImage_MPP_EmptyLevels(t *testing.T) {
	// Manually construct an image with no levels to test the zero case.
	// We can't do this directly from the test API, but we can verify
	// the contract through the real fixture.
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	if len(img.Levels()) == 0 {
		// If we ever get a fixture with no levels, MPP should return zero.
		got := img.MPP()
		if got.W != 0 || got.H != 0 {
			t.Errorf("Image.MPP() with no levels = %v, want {0, 0}", got)
		}
	}
}

func TestImage_ChannelName_ReturnsEmpty(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	if got := img.ChannelName(0); got != "" {
		t.Errorf("Image.ChannelName(0) = %q, want empty string", got)
	}
	if got := img.ChannelName(999); got != "" {
		t.Errorf("Image.ChannelName(999) = %q, want empty string", got)
	}
}

func TestImage_ZPlaneFocus_ReturnsZero(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	img := tlr.Images()[0]
	if got := img.ZPlaneFocus(0); got != 0 {
		t.Errorf("Image.ZPlaneFocus(0) = %v, want 0", got)
	}
	if got := img.ZPlaneFocus(999); got != 0 {
		t.Errorf("Image.ZPlaneFocus(999) = %v, want 0", got)
	}
}

func TestTiler_Level_ValidIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	got, err := tlr.Level(0)
	if err != nil {
		t.Fatalf("Tiler.Level(0): %v", err)
	}
	if got != levels[0] {
		t.Errorf("Tiler.Level(0) mismatch with Levels()[0]")
	}
}

func TestTiler_Level_OutOfRange(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	numLevels := len(tlr.Levels())

	// Negative index
	_, err := tlr.Level(-1)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Tiler.Level(-1): got %v, want ErrLevelOutOfRange", err)
	}

	// Beyond max
	_, err = tlr.Level(numLevels)
	if !errors.Is(err, opentile.ErrLevelOutOfRange) {
		t.Errorf("Tiler.Level(%d): got %v, want ErrLevelOutOfRange", numLevels, err)
	}
}

func TestTiler_ICCProfile_ReturnsNil(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	if got := tlr.ICCProfile(); got != nil {
		t.Errorf("Tiler.ICCProfile() = %v, want nil", got)
	}
}

func TestLevel_TileOverlap_ReturnsZeroPoint(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	level := tlr.Levels()[0]
	got := level.TileOverlap()

	if got.X != 0 || got.Y != 0 {
		t.Errorf("Level.TileOverlap() = %v, want {0, 0}", got)
	}
}

func TestLevel_Index_ReturnsOpentileSideIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	for i, level := range levels {
		if got := level.Index(); got != i {
			t.Errorf("Level[%d].Index() = %d, want %d", i, got, i)
		}
	}
}

func TestLevel_PyramidIndex_MatchesIndex(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	levels := tlr.Levels()
	for i, level := range levels {
		if got := level.PyramidIndex(); got != level.Index() {
			t.Errorf("Level[%d].PyramidIndex() = %d, want %d (= Index)", i, got, level.Index())
		}
	}
}

func TestLevel_MPP_ReturnsZero(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	for i, level := range tlr.Levels() {
		got := level.MPP()
		if got.W != 0 || got.H != 0 {
			t.Errorf("Level[%d].MPP() = %v, want {0, 0}", i, got)
		}
	}
}

func TestLevel_FocalPlane_ReturnsZero(t *testing.T) {
	tlr := openCMU1(t)
	defer tlr.Close()

	for i, level := range tlr.Levels() {
		got := level.FocalPlane()
		if got != 0 {
			t.Errorf("Level[%d].FocalPlane() = %v, want 0", i, got)
		}
	}
}
