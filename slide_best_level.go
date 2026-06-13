package opentile

// imageBestLevelForDownsample returns the index of the largest pyramid
// level whose Downsample factor is ≤ the requested value. Matches
// openslide.best_level_for_downsample semantics. Backs
// (*Pyramid).BestLevelForDownsample.
//
// Returns 0 (L0) if every level's downsample exceeds the requested
// value (the caller wants higher resolution than the slide carries).
func (s *Slide) imageBestLevelForDownsample(image int, downsample float64) int {
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
