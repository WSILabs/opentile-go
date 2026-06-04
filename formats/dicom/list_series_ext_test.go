package dicom_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wsilabs/opentile-go/formats/dicom"
)

func histech1(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "3DHISTECH-1")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return dir
}

func TestListWSMSeriesSingleSeries(t *testing.T) {
	got, err := dicom.ListWSMSeries(leica4(t))
	if err != nil {
		t.Fatalf("ListWSMSeries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d series, want 1: %+v", len(got), got)
	}
	if got[0].SeriesUID == "" || got[0].LevelCount < 1 {
		t.Errorf("series = %+v", got[0])
	}
}

// TestListWSMSeriesMultiSeries symlinks the .dcm of two different-series
// fixtures into one temp dir; the combined dir must report strictly more
// series than either fixture alone (i.e. the second series is detected).
func TestListWSMSeriesMultiSeries(t *testing.T) {
	a, b := leica4(t), histech1(t)
	soloA, err := dicom.ListWSMSeries(a)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	n := 0
	for _, srcDir := range []string{a, b} {
		dcms, _ := filepath.Glob(filepath.Join(srcDir, "*.dcm"))
		for _, p := range dcms {
			abs, err := filepath.Abs(p)
			if err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(tmp, fmt.Sprintf("%04d.dcm", n))
			if err := os.Symlink(abs, link); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			n++
		}
	}
	got, err := dicom.ListWSMSeries(tmp)
	if err != nil {
		t.Fatalf("ListWSMSeries(combined): %v", err)
	}
	if len(got) <= len(soloA) {
		t.Fatalf("combined %d series not > leica-only %d: %+v", len(got), len(soloA), got)
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].SeriesUID < got[j].SeriesUID }) {
		t.Errorf("not sorted by SeriesUID: %+v", got)
	}
}

func TestListWSMSeriesSingleFile(t *testing.T) {
	dcms, _ := filepath.Glob(filepath.Join(leica4(t), "*.dcm"))
	if len(dcms) == 0 {
		t.Skip("no .dcm in fixture")
	}
	got, err := dicom.ListWSMSeries(dcms[0])
	if err != nil {
		t.Fatalf("ListWSMSeries(file): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("single-file got %d series, want 1: %+v", len(got), got)
	}
}

func TestListWSMSeriesNoWSM(t *testing.T) {
	got, err := dicom.ListWSMSeries(t.TempDir())
	if err != nil {
		t.Fatalf("empty dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir got %d series, want 0", len(got))
	}
}
