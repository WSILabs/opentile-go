package leicascn

import (
	"os"
	"path/filepath"
	"testing"
)

// loadFixtureXML reads one of the committed XML fixtures under
// testdata/. Each is a verbatim dump of the corresponding SCN file's
// IFD 0 ImageDescription value (extracted via tifffile during T1
// preparation; see scripts/regen-scn-xml-fixtures.py).
func loadFixtureXML(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func TestParseDescription_Leica1(t *testing.T) {
	c, err := ParseDescription(loadFixtureXML(t, "Leica-1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Images); got != 2 {
		t.Errorf("Images = %d, want 2", got)
	}
	if got := c.SizeXNm; got != 26564529 {
		t.Errorf("SizeXNm = %d, want 26564529", got)
	}
	if got := c.SizeYNm; got != 76734666 {
		t.Errorf("SizeYNm = %d, want 76734666", got)
	}
	if got := c.Barcode; got != "MDQwNTA2MjlD" {
		t.Errorf("Barcode = %q, want %q", got, "MDQwNTA2MjlD")
	}

	aux := c.Images[0]
	if got := aux.Objective; got != 0.60833 {
		t.Errorf("Aux Objective = %v, want 0.60833", got)
	}
	if got := aux.IlluminationSource; got != "brightfield" {
		t.Errorf("Aux IlluminationSource = %q, want brightfield", got)
	}
	if got := len(aux.Dimensions); got != 3 {
		t.Errorf("Aux Dimensions = %d, want 3", got)
	}
	if got := aux.Dimensions[0]; got.IFD != 0 || got.SizeX != 1616 || got.SizeY != 4668 {
		t.Errorf("Aux L0 = %+v, want IFD=0 1616×4668", got)
	}

	main := c.Images[1]
	if got := main.Objective; got != 20 {
		t.Errorf("Main Objective = %v, want 20", got)
	}
	if got := len(main.Dimensions); got != 5 {
		t.Errorf("Main Dimensions = %d, want 5", got)
	}
	if got := main.Dimensions[0]; got.IFD != 3 || got.SizeX != 36832 || got.SizeY != 38432 {
		t.Errorf("Main L0 = %+v, want IFD=3 36832×38432", got)
	}
	if got := main.ViewOffsetXNm; got != 5389341 {
		t.Errorf("Main ViewOffsetXNm = %d, want 5389341", got)
	}
	if got := main.ViewOffsetYNm; got != 17548313 {
		t.Errorf("Main ViewOffsetYNm = %d, want 17548313", got)
	}
}

func TestParseDescription_Leica2_FourMains(t *testing.T) {
	c, err := ParseDescription(loadFixtureXML(t, "Leica-2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Images); got != 5 {
		t.Errorf("Images = %d, want 5", got)
	}
	// First image is auxiliary; remaining 4 are mains at 40×.
	for i := 1; i <= 4; i++ {
		main := c.Images[i]
		if got := main.Objective; got != 40 {
			t.Errorf("Main[%d] Objective = %v, want 40", i, got)
		}
		if got := len(main.Dimensions); got != 6 {
			t.Errorf("Main[%d] Dimensions = %d, want 6", i, got)
		}
	}
	// IFD layout pinning: auxiliary 0/1/2; main1 3-8; main2 9-14; main3 15-20; main4 21-26.
	for ifdSlot, expectIFD := range map[int]int{0: 0, 1: 3, 2: 9, 3: 15, 4: 21} {
		if got := c.Images[ifdSlot].Dimensions[0].IFD; got != expectIFD {
			t.Errorf("Image[%d].Dimensions[0].IFD = %d, want %d", ifdSlot, got, expectIFD)
		}
	}
}

func TestParseDescription_Fluorescence_Channels(t *testing.T) {
	c, err := ParseDescription(loadFixtureXML(t, "Leica-Fluorescence-1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Images); got != 3 {
		t.Errorf("Images = %d, want 3", got)
	}
	main := c.Images[2] // 3rd <image> is the fluor main
	if got := len(main.Channels); got != 3 {
		t.Fatalf("Channels = %d, want 3", got)
	}
	wantNames := []string{"405|Empty", "L5|Empty", "TX2|Empty"}
	for i, want := range wantNames {
		if got := main.Channels[i].Name; got != want {
			t.Errorf("Channels[%d].Name = %q, want %q", i, got, want)
		}
	}
	wantRGB := []string{"#0000ff", "#00ff00", "#ff0000"}
	for i, want := range wantRGB {
		if got := main.Channels[i].RGB; got != want {
			t.Errorf("Channels[%d].RGB = %q, want %q", i, got, want)
		}
	}
	if got := len(main.Dimensions); got != 12 { // 4 levels × 3 channels
		t.Errorf("Fluor Main Dimensions = %d, want 12", got)
	}
	// Pin the (r=0, c=0) → IFD 6 mapping (start of fluor pyramid).
	for _, d := range main.Dimensions {
		if d.R == 0 && d.C == 0 {
			if d.IFD != 6 {
				t.Errorf("Fluor main (r=0, c=0) IFD = %d, want 6", d.IFD)
			}
			if d.SizeX != 4737 || d.SizeY != 6338 {
				t.Errorf("Fluor main (r=0, c=0) size = %d×%d, want 4737×6338",
					d.SizeX, d.SizeY)
			}
		}
	}
	if got := main.IlluminationSource; got != "fluorescence" {
		t.Errorf("Fluor main IlluminationSource = %q, want fluorescence", got)
	}
	// Channel filter metadata sanity-check.
	if got := main.Channels[0].ExcitationFilter; got != "BP 405/60" {
		t.Errorf("Channels[0].ExcitationFilter = %q, want %q", got, "BP 405/60")
	}
	if got := main.Channels[0].ExposureTimeMicros; got != 105000 {
		t.Errorf("Channels[0].ExposureTimeMicros = %d, want 105000", got)
	}
}

func TestParseDescription_RejectsBadURN(t *testing.T) {
	_, err := ParseDescription(`<?xml version="1.0"?><other/>`)
	if err == nil {
		t.Error("expected schema-URN-mismatch error, got nil")
	}
}
