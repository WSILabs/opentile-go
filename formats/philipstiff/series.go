package philipstiff

import (
	"errors"
	"strings"
)

// errNoPages is returned when classifyPages is called on an empty page list.
var errNoPages = errors.New("philips: file has no pages")

// errBaseNotTiled is returned when page 0 is not tiled. tifffile's
// Philips path requires the baseline to be tiled.
var errBaseNotTiled = errors.New("philips: base page (0) must be tiled")

// philipsPageMeta is the minimal Philips-relevant metadata for a single
// TIFF page, extracted once per Open() call so the classifier can be a
// pure function over a small struct.
type philipsPageMeta struct {
	Tiled       bool
	Description string
}

// philipsClassification names which page indices map to which Philips
// series. Absent associated images carry index -1.
type philipsClassification struct {
	Levels    []int // page indices forming the Baseline pyramid, in order
	Macro     int   // -1 if absent ("overview" in opentile-go's public API)
	Label     int   // -1 if absent
	Thumbnail int   // -1 if absent
}

// classifyPages applies upstream's substring-based predicates
// (philips_tiff_tiler.py:111-137) to assign Philips TIFF pages to
// series.
//
// Algorithm:
//  1. Page 0 must be tiled; it is the first Baseline level.
//  2. Walk forward while pages are tiled — all are Baseline levels.
//  3. Walk remaining pages (non-tiled) and classify by description
//     substring: "Macro" → overview, "Label" → label, "Thumbnail" →
//     thumbnail. Unknown descriptions are silently ignored (matches
//     upstream's predicate-by-predicate scan).
//
// Pure over its input metadata; no I/O.
func classifyPages(metas []philipsPageMeta) (philipsClassification, error) {
	if len(metas) == 0 {
		return philipsClassification{}, errNoPages
	}
	if !metas[0].Tiled {
		return philipsClassification{}, errBaseNotTiled
	}

	c := philipsClassification{
		Levels:    []int{0},
		Macro:     -1,
		Label:     -1,
		Thumbnail: -1,
	}

	idx := 1
	for idx < len(metas) && metas[idx].Tiled {
		c.Levels = append(c.Levels, idx)
		idx++
	}

	for ; idx < len(metas); idx++ {
		desc := metas[idx].Description
		switch {
		case strings.Contains(desc, "Macro") && c.Macro < 0:
			c.Macro = idx
		case strings.Contains(desc, "Label") && c.Label < 0:
			c.Label = idx
		case strings.Contains(desc, "Thumbnail") && c.Thumbnail < 0:
			c.Thumbnail = idx
		}
	}

	return c, nil
}
