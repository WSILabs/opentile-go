package dicom

import (
	"fmt"
	"sort"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

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
