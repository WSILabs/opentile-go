package dicom

import (
	"fmt"
	"sort"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// selectDominantSeries groups instances by SeriesUID and returns the group
// with the most VOLUME instances. Ties are broken by the lexicographically
// first SeriesUID. If all instances share the same SeriesUID (the common
// case), the input slice is returned unchanged.
func selectDominantSeries(parsed []idicom.Instance) []idicom.Instance {
	// Group by SeriesUID.
	type group struct {
		uid   string
		insts []idicom.Instance
		vols  int
	}
	order := []string{} // insertion order for determinism
	byUID := map[string]*group{}
	for _, in := range parsed {
		g, ok := byUID[in.SeriesUID]
		if !ok {
			order = append(order, in.SeriesUID)
			g = &group{uid: in.SeriesUID}
			byUID[in.SeriesUID] = g
		}
		g.insts = append(g.insts, in)
		if roleOfInstance(in) == "VOLUME" {
			g.vols++
		}
	}
	if len(order) == 1 {
		return parsed // fast path: single series
	}
	// Sort UIDs to get a stable secondary key.
	sortedUIDs := make([]string, len(order))
	copy(sortedUIDs, order)
	sort.Strings(sortedUIDs)
	// Pick the group with the most VOLUME instances; ties → first by sorted UID.
	best := byUID[sortedUIDs[0]]
	for _, uid := range sortedUIDs[1:] {
		g := byUID[uid]
		if g.vols > best.vols {
			best = g
		}
	}
	return best.insts
}

type levelInfo struct {
	inst       idicom.Instance
	downsample float64
}

type assocInfo struct {
	inst idicom.Instance
	role string
}

type series struct {
	levels     []levelInfo
	associated []assocInfo
}

// assembleSeries filters to WSM instances with a positive total matrix,
// sorts VOLUME instances into levels (largest first), and classifies
// LABEL/OVERVIEW/THUMBNAIL as associated images.
func assembleSeries(insts []idicom.Instance) (series, error) {
	var s series
	var vols []idicom.Instance
	for _, in := range insts {
		if in.SOPClassUID != idicom.WSMStorageUID || in.TotalCols <= 0 || in.TotalRows <= 0 {
			continue
		}
		switch roleOfInstance(in) {
		case "VOLUME":
			vols = append(vols, in)
		case "LABEL", "OVERVIEW", "THUMBNAIL":
			s.associated = append(s.associated, assocInfo{inst: in, role: roleOfInstance(in)})
		}
	}
	if len(vols) == 0 {
		return series{}, fmt.Errorf("dicom: no VOLUME level in series")
	}
	sort.SliceStable(vols, func(i, j int) bool { return vols[i].TotalCols > vols[j].TotalCols })
	l0 := float64(vols[0].TotalCols)
	for _, v := range vols {
		s.levels = append(s.levels, levelInfo{inst: v, downsample: l0 / float64(v.TotalCols)})
	}
	return s, nil
}

// roleOfInstance mirrors internal/dicom role classification at the format layer.
func roleOfInstance(in idicom.Instance) string {
	for _, v := range in.ImageType {
		switch v {
		case "VOLUME", "LABEL", "OVERVIEW", "THUMBNAIL":
			return v
		}
	}
	return ""
}
