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

func instSeries(role, uid string, cols, rows int) idicom.Instance {
	i := inst(role, cols, rows)
	i.SeriesUID = uid
	return i
}

func TestSelectDominantSeries(t *testing.T) {
	// Series "A" has 1 VOLUME; series "B" has 3 VOLUMEs. B should win.
	parsed := []idicom.Instance{
		instSeries("VOLUME", "A", 512, 256),
		instSeries("VOLUME", "B", 23374, 22079),
		instSeries("VOLUME", "B", 5843, 5519),
		instSeries("VOLUME", "B", 1460, 1379),
		instSeries("LABEL", "B", 608, 547),
	}
	got := selectDominantSeries(parsed)
	for _, in := range got {
		if in.SeriesUID != "B" {
			t.Errorf("expected all instances from series B, got %q", in.SeriesUID)
		}
	}
	if len(got) != 4 { // 3 VOLUMEs + 1 LABEL from B
		t.Errorf("expected 4 instances, got %d", len(got))
	}
}

func TestSelectDominantSeriesSingleSeries(t *testing.T) {
	// Single series → returns the same slice (fast path).
	parsed := []idicom.Instance{
		inst("VOLUME", 512, 256),
		inst("LABEL", 64, 64),
	}
	got := selectDominantSeries(parsed)
	if len(got) != len(parsed) {
		t.Errorf("single-series: got %d, want %d", len(got), len(parsed))
	}
}

func TestSelectDominantSeriesTieBreak(t *testing.T) {
	// Two series with equal VOLUME count → first by sorted UID.
	parsed := []idicom.Instance{
		instSeries("VOLUME", "Z", 512, 256),
		instSeries("VOLUME", "A", 512, 256),
	}
	got := selectDominantSeries(parsed)
	for _, in := range got {
		if in.SeriesUID != "A" {
			t.Errorf("tie-break: expected series A, got %q", in.SeriesUID)
		}
	}
}
