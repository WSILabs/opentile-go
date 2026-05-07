package ometiff

import (
	"errors"
	"strings"
)

// omeClassification names which OME Image indices map to which series
// kind. Multi-image OME files (Leica-2.ome.tiff) carry multiple main
// pyramids — LevelImages exposes them all, in document order.
//
// Upstream Python opentile silently drops all but the last main
// pyramid via an unintentional last-wins loop in its base
// Tiler.__init__; opentile-go v0.6 deliberately exposes all of them
// via opentile.Tiler.Images(). See docs/deferred.md "Deviations from
// upstream Python opentile".
type omeClassification struct {
	LevelImages []int // OMEMetadata.Images indices, in document order
	Macro       int   // -1 if absent
	Label       int   // -1 if absent
	Thumbnail   int   // -1 if absent
}

// errNoLevelImages is returned when an OME document carries only
// associated images (macro / label / thumbnail) and no main pyramid.
// Such a file is not usable for tile extraction.
var errNoLevelImages = errors.New("ome: no main pyramid Images in OME document")

// classifyImages applies upstream's `_is_*_series` predicates
// (philips_tiff_tiler.py:77-91) — each strips surrounding whitespace
// from the OME Image's Name attribute and exact-matches against
// "label" / "macro" / "thumbnail". Anything else (including empty
// Name) classifies as a main pyramid.
//
// Behaviour deviates from upstream for multi-image OME files: rather
// than overwriting `level_series_index` on each match (last wins),
// we collect every main-pyramid Image index in LevelImages. The
// associated kinds (Macro / Label / Thumbnail) preserve last-wins
// semantics since by convention an OME file carries at most one of
// each, and upstream's behaviour is well-defined when more than one
// appears.
func classifyImages(metas []OMEImage) (omeClassification, error) {
	if len(metas) == 0 {
		return omeClassification{}, errors.New("ome: empty Image list")
	}
	c := omeClassification{Macro: -1, Label: -1, Thumbnail: -1}
	for i, m := range metas {
		switch strings.TrimSpace(m.Name) {
		case "label":
			c.Label = i
		case "macro":
			c.Macro = i
		case "thumbnail":
			c.Thumbnail = i
		default:
			c.LevelImages = append(c.LevelImages, i)
		}
	}
	if len(c.LevelImages) == 0 {
		return c, errNoLevelImages
	}
	return c, nil
}
