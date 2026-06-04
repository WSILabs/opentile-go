package dicom

import (
	"fmt"
	"sort"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// seriesGroup is one SeriesUID and its instances, in first-seen order.
type seriesGroup struct {
	uid   string
	insts []idicom.Instance
}

// groupBySeriesUID groups instances by SeriesUID, preserving the first-seen
// order of the groups.
func groupBySeriesUID(parsed []idicom.Instance) []seriesGroup {
	order := []string{}
	byUID := map[string]*seriesGroup{}
	for _, in := range parsed {
		g, ok := byUID[in.SeriesUID]
		if !ok {
			order = append(order, in.SeriesUID)
			g = &seriesGroup{uid: in.SeriesUID}
			byUID[in.SeriesUID] = g
		}
		g.insts = append(g.insts, in)
	}
	out := make([]seriesGroup, 0, len(order))
	for _, uid := range order {
		out = append(out, *byUID[uid])
	}
	return out
}

// selectDominantSeries groups instances by SeriesUID and returns the group
// with the most VOLUME instances. Ties are broken by the lexicographically
// first SeriesUID. If all instances share the same SeriesUID (the common
// case), the input slice is returned unchanged.
func selectDominantSeries(parsed []idicom.Instance) []idicom.Instance {
	groups := groupBySeriesUID(parsed)
	if len(groups) == 1 {
		return parsed // fast path: single series
	}
	vols := map[string]int{}
	byUID := map[string][]idicom.Instance{}
	uids := make([]string, 0, len(groups))
	for _, g := range groups {
		uids = append(uids, g.uid)
		byUID[g.uid] = g.insts
		for _, in := range g.insts {
			if roleOfInstance(in) == "VOLUME" {
				vols[g.uid]++
			}
		}
	}
	// Sort UIDs for a stable secondary key; pick most VOLUME, ties → first UID.
	sort.Strings(uids)
	best := uids[0]
	for _, uid := range uids[1:] {
		if vols[uid] > vols[best] {
			best = uid
		}
	}
	return byUID[best]
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
