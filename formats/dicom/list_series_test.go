package dicom

import (
	"sort"
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestSeriesInfoFromInstances(t *testing.T) {
	insts := []idicom.Instance{
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 1000, TotalRows: 800, Manufacturer: "Acme", Model: "X1", ObjectivePower: 40},
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 500, TotalRows: 400},
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"LABEL"}, TotalCols: 100, TotalRows: 80},
		{SeriesUID: "A", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 600, TotalRows: 600, Manufacturer: "Beta", Model: "Y2", ObjectivePower: 20},
	}
	got := seriesInfosFromInstances(insts)
	sort.Slice(got, func(i, j int) bool { return got[i].SeriesUID < got[j].SeriesUID })
	if len(got) != 2 {
		t.Fatalf("got %d series, want 2", len(got))
	}
	if got[0].SeriesUID != "A" || got[0].LevelCount != 1 || got[0].InstanceCount != 1 ||
		got[0].Manufacturer != "Beta" || got[0].Magnification != 20 {
		t.Errorf("series A = %+v", got[0])
	}
	if got[1].SeriesUID != "B" || got[1].LevelCount != 2 || got[1].InstanceCount != 3 ||
		got[1].Manufacturer != "Acme" || got[1].Model != "X1" || got[1].Magnification != 40 {
		t.Errorf("series B = %+v", got[1])
	}
}
