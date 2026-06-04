package dicom

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// SeriesInfo summarizes one WSM series discovered under a path.
type SeriesInfo struct {
	SeriesUID     string
	LevelCount    int     // number of VOLUME instances (≈ pyramid levels)
	InstanceCount int     // number of WSM instances in the series (all roles)
	Manufacturer  string  // from a VOLUME instance
	Model         string
	Magnification float64 // ObjectivePower
}

// seriesInfosFromInstances groups WSM instances by SeriesUID and summarizes
// each. Display metadata is taken from the series' first VOLUME instance.
func seriesInfosFromInstances(insts []idicom.Instance) []SeriesInfo {
	groups := groupBySeriesUID(insts)
	out := make([]SeriesInfo, 0, len(groups))
	for _, g := range groups {
		si := SeriesInfo{SeriesUID: g.uid, InstanceCount: len(g.insts)}
		for _, in := range g.insts {
			if roleOfInstance(in) == "VOLUME" {
				si.LevelCount++
				if si.Manufacturer == "" && si.Model == "" && si.Magnification == 0 {
					si.Manufacturer = in.Manufacturer
					si.Model = in.Model
					si.Magnification = in.ObjectivePower
				}
			}
		}
		out = append(out, si)
	}
	return out
}

// scanWSMInstances parses all valid WSM instances in dir. It fast-rejects
// non-DICOM files with the DICM magic pre-filter and parses the survivors
// with a bounded worker pool. The parse is metadata-only (ParseInstance
// skips PixelData), so per-file cost is a small header read.
func scanWSMInstances(dir string) []idicom.Instance {
	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var candidates []string
	for _, p := range entries {
		if hasDICMMagic(p) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan string)
	var mu sync.Mutex
	var out []idicom.Instance
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				in, err := idicom.ParseInstance(p)
				if err != nil || in.SOPClassUID != idicom.WSMStorageUID ||
					in.TotalCols <= 0 || in.TotalRows <= 0 {
					continue
				}
				mu.Lock()
				out = append(out, in)
				mu.Unlock()
			}
		}()
	}
	for _, p := range candidates {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	return out
}

// ListWSMSeries enumerates the WSM series under a path without opening a
// slide. For a directory it returns one SeriesInfo per distinct WSM
// SeriesUID, sorted by SeriesUID. For a single .dcm it returns exactly one
// entry, anchored to that file's SeriesUID (same-series siblings only). A
// valid directory with no WSM instances returns an empty slice and nil
// error; a stat error returns an error.
//
// OpenSeries' dominant-pick remains the permissive library default;
// ListWSMSeries is purely a probe so a caller can detect (len > 1) and
// refuse an ambiguous directory.
func ListWSMSeries(path string) ([]SeriesInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("dicom: stat %s: %w", path, err)
	}
	dir := path
	var anchor string
	if !fi.IsDir() {
		dir = filepath.Dir(path)
		in, err := idicom.ParseInstance(path)
		if err != nil {
			return nil, fmt.Errorf("dicom: parse %s: %w", path, err)
		}
		if in.SOPClassUID != idicom.WSMStorageUID {
			return nil, fmt.Errorf("dicom: %s is not a WSM instance (SOP class %s)", path, in.SOPClassUID)
		}
		anchor = in.SeriesUID
	}

	insts := scanWSMInstances(dir)
	if anchor != "" {
		kept := make([]idicom.Instance, 0, len(insts))
		for _, in := range insts {
			if in.SeriesUID == anchor {
				kept = append(kept, in)
			}
		}
		insts = kept
	}

	out := seriesInfosFromInstances(insts)
	sort.Slice(out, func(i, j int) bool { return out[i].SeriesUID < out[j].SeriesUID })
	return out, nil
}
