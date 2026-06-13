package opentile

// BestLevelForDownsample returns the index of the largest pyramid
// level whose Downsample factor is ≤ the requested value. Matches
// openslide.best_level_for_downsample semantics.
//
// Returns 0 (L0) if every level's downsample exceeds the requested
// value (the caller wants higher resolution than the slide carries).
//
// Shortcut for ImageBestLevelForDownsample(0, downsample).
//
// Added in v0.25 alongside the ReadRegion family.
func (s *Slide) BestLevelForDownsample(downsample float64) int {
	return s.ImageBestLevelForDownsample(0, downsample)
}

// ImageBestLevelForDownsample is the multi-image variant of
// BestLevelForDownsample.
func (s *Slide) ImageBestLevelForDownsample(image int, downsample float64) int {
	pyrs := s.r.Pyramids()
	if image < 0 || image >= len(pyrs) {
		return 0
	}
	levels := pyrs[image].Levels
	best := 0
	for i, lvl := range levels {
		if lvl.Downsample <= downsample {
			best = i
		}
	}
	return best
}
