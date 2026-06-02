package dicom

import (
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func inst(role string, cols, rows int) idicom.Instance {
	return idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", role, "NONE"},
		TotalCols: cols, TotalRows: rows, TileCols: 256, TileRows: 256,
		DimOrg: "TILED_FULL",
	}
}

func TestAssembleSeries(t *testing.T) {
	in := []idicom.Instance{
		inst("VOLUME", 1460, 1379),
		inst("VOLUME", 23374, 22079),
		inst("VOLUME", 5843, 5519),
		inst("LABEL", 608, 547),
		inst("OVERVIEW", 1491, 605),
		{SOPClassUID: "1.2.99", TotalCols: 100}, // non-WSM, must be dropped
		inst("THUMBNAIL", 1920, 1813),
	}
	s, err := assembleSeries(in)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(s.levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(s.levels))
	}
	// Sorted largest-first.
	if s.levels[0].inst.TotalCols != 23374 || s.levels[2].inst.TotalCols != 1460 {
		t.Errorf("levels not sorted desc: %d..%d", s.levels[0].inst.TotalCols, s.levels[2].inst.TotalCols)
	}
	// Downsample derived from L0.
	if s.levels[1].downsample != float64(23374)/float64(5843) {
		t.Errorf("L1 downsample = %v", s.levels[1].downsample)
	}
	roles := map[string]bool{}
	for _, a := range s.associated {
		roles[a.role] = true
	}
	for _, want := range []string{"LABEL", "OVERVIEW", "THUMBNAIL"} {
		if !roles[want] {
			t.Errorf("missing associated %s", want)
		}
	}
}

func TestAssembleNoVolume(t *testing.T) {
	if _, err := assembleSeries([]idicom.Instance{inst("LABEL", 1, 1)}); err == nil {
		t.Fatal("expected error when no VOLUME level present")
	}
}
