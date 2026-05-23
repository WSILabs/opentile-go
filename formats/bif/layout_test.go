package bif

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestClassifyIFDPatterns covers every ImageDescription pattern
// listed in spec §5.3, with the variants seen across both fixtures.
func TestClassifyIFDPatterns(t *testing.T) {
	// classifyIFD takes a *tiff.Page; build minimal pages via the
	// detection-test BigTIFF helper.
	cases := []struct {
		desc     string
		wantRole ifdRole
		wantLvl  int
		wantMag  float64
		wantQ    int
	}{
		{"Label_Image", ifdRoleLabel, -1, 0, 0},
		{"Label Image", ifdRoleLabel, -1, 0, 0},
		{"Probability_Image", ifdRoleProbability, -1, 0, 0},
		{"Thumbnail", ifdRoleThumbnail, -1, 0, 0},
		{"level=0 mag=40 quality=95", ifdRolePyramid, 0, 40, 95},
		{"level=7 mag=0.3125 quality=95", ifdRolePyramid, 7, 0.3125, 95},
		{"level=9 mag=0.078125 quality=90", ifdRolePyramid, 9, 0.078125, 90},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			data := buildBIFLikeBigTIFF(t, []iFDSpec{{description: tc.desc}})
			f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("tiff.Open: %v", err)
			}
			c := classifyIFD(0, f.Pages()[0])
			if c.Role != tc.wantRole {
				t.Errorf("Role: got %v, want %v", c.Role, tc.wantRole)
			}
			if c.Level != tc.wantLvl {
				t.Errorf("Level: got %d, want %d", c.Level, tc.wantLvl)
			}
			if c.Mag != tc.wantMag {
				t.Errorf("Mag: got %v, want %v", c.Mag, tc.wantMag)
			}
			if c.Quality != tc.wantQ {
				t.Errorf("Quality: got %d, want %d", c.Quality, tc.wantQ)
			}
		})
	}
}

// TestClassifyIFDUnknown: ImageDescription not matching any spec
// pattern → ifdRoleUnknown (lenient — don't crash, don't false-route).
func TestClassifyIFDUnknown(t *testing.T) {
	cases := []string{
		"",
		"random text",
		"levelmissing=0", // doesn't start with "level="
		"level",          // no = sign
	}
	for _, desc := range cases {
		t.Run(desc, func(t *testing.T) {
			ifds := []iFDSpec{{}}
			if desc != "" {
				ifds[0].description = desc
			}
			data := buildBIFLikeBigTIFF(t, ifds)
			f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("tiff.Open: %v", err)
			}
			c := classifyIFD(0, f.Pages()[0])
			if c.Role != ifdRoleUnknown {
				t.Errorf("Role: got %v, want ifdRoleUnknown for desc=%q", c.Role, desc)
			}
		})
	}
}

// TestClassifyIFDLeniency: a level= description with malformed
// numeric tokens still classifies as ifdRolePyramid; missing tokens
// leave the corresponding fields at their zero / -1 defaults.
func TestClassifyIFDLeniency(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{{description: "level=foo mag=bar quality=baz"}})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	c := classifyIFD(0, f.Pages()[0])
	if c.Role != ifdRolePyramid {
		t.Errorf("Role: got %v, want ifdRolePyramid (parser should be lenient on numeric values)", c.Role)
	}
	// Malformed numerics leave fields at default zero / -1.
	if c.Level != -1 || c.Mag != 0 || c.Quality != 0 {
		t.Errorf("malformed-numeric defaults: got Level=%d Mag=%v Quality=%d, want -1 / 0 / 0", c.Level, c.Mag, c.Quality)
	}
}

// TestInventorySortsByLevel: pyramid IFDs returned by inventory()
// must be in `level=N` ascending order, regardless of IFD index
// order in the file. (Real fixtures happen to be in level order
// already, but the spec doesn't promise that.)
func TestInventorySortsByLevel(t *testing.T) {
	// Build a synthetic BIF where pyramid IFDs are out of order:
	// IFD 1 = level=2, IFD 2 = level=0, IFD 3 = level=1.
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
		{description: "level=2 mag=10 quality=95"},
		{description: "level=0 mag=40 quality=95"},
		{description: "level=1 mag=20 quality=95"},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	levels, _, _, err := inventory(f)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("levels: got %d, want 3", len(levels))
	}
	for i, lvl := range levels {
		if lvl.Level != i {
			t.Errorf("levels[%d].Level: got %d, want %d (must be sorted ascending)", i, lvl.Level, i)
		}
	}
}

// TestInventoryFailsWithoutPyramid: at least one pyramid IFD is
// required for a valid BIF (every fixture has one). Open must fail
// if there are zero — covers a malformed file gracefully.
func TestInventoryFailsWithoutPyramid(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if _, _, _, err := inventory(f); err == nil {
		t.Fatal("inventory: expected error for BIF without pyramid IFDs")
	}
}

// TestInventoryGroupsAssociated: label / probability / thumbnail
// IFDs land in the associated slice, not levels.
func TestInventoryGroupsAssociated(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan/>`), description: "Label_Image"},
		{description: "Probability_Image"},
		{description: "Thumbnail"},
		{description: "level=0 mag=40 quality=95"},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	levels, associated, _, err := inventory(f)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(levels) != 1 {
		t.Errorf("levels: got %d, want 1", len(levels))
	}
	if len(associated) != 3 {
		t.Fatalf("associated: got %d, want 3", len(associated))
	}
	wantRoles := []ifdRole{ifdRoleLabel, ifdRoleProbability, ifdRoleThumbnail}
	for i, want := range wantRoles {
		if associated[i].Role != want {
			t.Errorf("associated[%d].Role: got %v, want %v", i, associated[i].Role, want)
		}
	}
}
