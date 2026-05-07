package leicascn

import (
	"errors"
	"testing"
)

func TestIsAuxiliary_Leica1(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-1.xml"))
	if !IsAuxiliary(c.Images[0], c) {
		t.Error("Leica-1 image[0] should be auxiliary (whole-slide view)")
	}
	if IsAuxiliary(c.Images[1], c) {
		t.Error("Leica-1 image[1] should NOT be auxiliary (sub-region view)")
	}
}

func TestIsAuxiliary_Leica2(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-2.xml"))
	if !IsAuxiliary(c.Images[0], c) {
		t.Error("Leica-2 image[0] should be auxiliary")
	}
	for i := 1; i <= 4; i++ {
		if IsAuxiliary(c.Images[i], c) {
			t.Errorf("Leica-2 image[%d] should NOT be auxiliary", i)
		}
	}
}

func TestIsAuxiliary_Fluorescence(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-Fluorescence-1.xml"))
	for i := 0; i <= 1; i++ {
		if !IsAuxiliary(c.Images[i], c) {
			t.Errorf("Fluorescence image[%d] should be auxiliary", i)
		}
	}
	if IsAuxiliary(c.Images[2], c) {
		t.Error("Fluorescence image[2] (the multi-channel main) should NOT be auxiliary")
	}
}

func TestComposePyramid_Leica1_SingleMain(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-1.xml"))
	mains := []Image{c.Images[1]}
	levels, err := ComposePyramid(mains, c)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(levels); got != 5 {
		t.Errorf("levels = %d, want 5", got)
	}
	l0 := levels[0]
	if got := l0.PixelSizeX; got != 36832 {
		t.Errorf("L0 PixelSizeX = %d, want 36832", got)
	}
	if got := l0.SizeC; got != 1 {
		t.Errorf("L0 SizeC = %d, want 1", got)
	}
	if got := len(l0.Regions); got != 1 {
		t.Errorf("L0 Regions = %d, want 1", got)
	}
	r0 := l0.Regions[0]
	if got := r0.OffsetX; got != 0 {
		t.Errorf("L0 R0 OffsetX = %d, want 0 (single-region collapses to 0)", got)
	}
	if got := r0.IFDPerChannel; len(got) != 1 || got[0] != 3 {
		t.Errorf("L0 R0 IFDPerChannel = %v, want [3]", got)
	}
}

func TestComposePyramid_Leica2_FourRegions(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-2.xml"))
	var mains []Image
	for _, img := range c.Images {
		if !IsAuxiliary(img, c) {
			mains = append(mains, img)
		}
	}
	if got := len(mains); got != 4 {
		t.Fatalf("mains = %d, want 4", got)
	}
	levels, err := ComposePyramid(mains, c)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(levels); got != 6 {
		t.Errorf("levels = %d, want 6", got)
	}
	l0 := levels[0]
	if got := len(l0.Regions); got != 4 {
		t.Errorf("L0 Regions = %d, want 4", got)
	}
	// Union extent: bounding-box across all 4 mains. The 4 mains are
	// stacked vertically with small gaps; X-extent is roughly 11M nm,
	// Y-extent is ~35M nm. At ~250 nm/px that's ~45000 × ~140000 px.
	// We pin a sanity range rather than an exact value (rounding can
	// produce ±1px drift across nm divisions).
	if l0.PixelSizeX < 40000 || l0.PixelSizeX > 50000 {
		t.Errorf("L0 PixelSizeX = %d, want ~45000±5000", l0.PixelSizeX)
	}
	if l0.PixelSizeY < 130000 || l0.PixelSizeY > 145000 {
		t.Errorf("L0 PixelSizeY = %d, want ~140000±5000", l0.PixelSizeY)
	}

	// First region (Y-min in slide coords) should have OffsetY == 0
	// after the union normalization.
	zeroOffsetCount := 0
	for _, r := range l0.Regions {
		if r.OffsetY == 0 {
			zeroOffsetCount++
		}
	}
	if zeroOffsetCount != 1 {
		t.Errorf("Exactly one region should have OffsetY == 0; got %d", zeroOffsetCount)
	}
}

func TestComposePyramid_Fluorescence_SizeC3(t *testing.T) {
	c, _ := ParseDescription(loadFixtureXML(t, "Leica-Fluorescence-1.xml"))
	mains := []Image{c.Images[2]}
	levels, err := ComposePyramid(mains, c)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(levels); got != 4 {
		t.Errorf("levels = %d, want 4", got)
	}
	l0 := levels[0]
	if got := l0.SizeC; got != 3 {
		t.Errorf("L0 SizeC = %d, want 3", got)
	}
	r0 := l0.Regions[0]
	if got := r0.IFDPerChannel; len(got) != 3 || got[0] != 6 || got[1] != 7 || got[2] != 8 {
		t.Errorf("L0 R0 IFDPerChannel = %v, want [6 7 8]", got)
	}
	// Last level's per-channel IFDs are 15, 16, 17.
	rN := levels[3].Regions[0]
	if got := rN.IFDPerChannel; len(got) != 3 || got[0] != 15 || got[1] != 16 || got[2] != 17 {
		t.Errorf("L3 R0 IFDPerChannel = %v, want [15 16 17]", got)
	}
}

func TestComposePyramid_RejectsMixedDepth(t *testing.T) {
	a := Image{
		PixelsSizeX: 100, PixelsSizeY: 100,
		ViewSizeXNm: 100000, ViewSizeYNm: 100000,
		IlluminationSource: "brightfield",
		Objective:          20,
		Dimensions: []Dimension{
			{R: 0, SizeX: 100, SizeY: 100, IFD: 0},
			{R: 1, SizeX: 50, SizeY: 50, IFD: 1},
		},
	}
	b := Image{
		PixelsSizeX: 100, PixelsSizeY: 100,
		ViewSizeXNm: 100000, ViewSizeYNm: 100000,
		IlluminationSource: "brightfield",
		Objective:          20,
		Dimensions: []Dimension{
			{R: 0, SizeX: 100, SizeY: 100, IFD: 2},
		},
	}
	c := &Collection{SizeXNm: 200000, SizeYNm: 200000}
	_, err := ComposePyramid([]Image{a, b}, c)
	if !errors.Is(err, ErrUnsupportedSCN) {
		t.Errorf("got %v, want ErrUnsupportedSCN-wrapped (depth mismatch)", err)
	}
}

func TestComposePyramid_RejectsMixedIllumination(t *testing.T) {
	a := Image{
		PixelsSizeX: 100, PixelsSizeY: 100,
		ViewSizeXNm: 100000, ViewSizeYNm: 100000,
		IlluminationSource: "brightfield", Objective: 20,
		Dimensions: []Dimension{{R: 0, SizeX: 100, SizeY: 100, IFD: 0}},
	}
	b := a
	b.IlluminationSource = "fluorescence"
	b.Dimensions = []Dimension{{R: 0, SizeX: 100, SizeY: 100, IFD: 1}}
	c := &Collection{SizeXNm: 200000, SizeYNm: 200000}
	_, err := ComposePyramid([]Image{a, b}, c)
	if !errors.Is(err, ErrUnsupportedSCN) {
		t.Errorf("got %v, want ErrUnsupportedSCN-wrapped (illumination mismatch)", err)
	}
}
