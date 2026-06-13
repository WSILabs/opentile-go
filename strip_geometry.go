package opentile

import (
	"image"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// resampleImageIntoUsing is a thin wrapper over resample.ImageInto that
// preserves the kernel choice. Allows future no-op fast-path when src
// dims match dst dims.
func resampleImageIntoUsing(src, dst *decoder.Image, kernel resample.Kernel) error {
	if src.Width == dst.Width && src.Height == dst.Height {
		copy(dst.Pix, src.Pix)
		return nil
	}
	return resample.ImageInto(src, dst, kernel)
}

// stripCacheCapacity converts a byte budget into a tile-count cap for
// the per-iterator decoded-tile cache (C1). The result is floored at
// max(workers, 8) so each worker always has an in-flight slot and tiny
// budgets don't livelock, and capped at the original count-formula
// value so a generous budget never over-provisions a narrow level.
func stripCacheCapacity(budgetBytes, bytesPerTile int64, workers, countFormulaCap int) int {
	if bytesPerTile < 1 {
		bytesPerTile = 1
	}
	byteCap := int(budgetBytes / bytesPerTile)
	floor := workers
	if floor < 8 {
		floor = 8
	}
	capacity := byteCap
	if capacity < floor {
		capacity = floor
	}
	if capacity > countFormulaCap {
		capacity = countFormulaCap
	}
	return capacity
}

// scaleCapable reports whether a level's codec honors DecodeOptions.Scale
// (decode to ceil(dim/Scale)): jpeg (IDCT), jpeg2000 + htj2k (DWT resolution
// decode). Other codecs return ErrUnsupportedScale for Scale != 1, so the
// strip path must not request a scale on them.
func scaleCapable(c Compression) bool {
	switch c {
	case CompressionJPEG, CompressionJP2K, CompressionHTJ2K:
		return true
	default:
		return false
	}
}

// autoIDCTScale picks the codec-domain scale factor for a scale-capable
// source level based on the effective downsample from level dims to output
// dims. Returns 1, 2, 4, or 8 (1 for codecs that can't scale).
func autoIDCTScale(level Level, l0Rect image.Rectangle, outSize image.Point) int {
	if !scaleCapable(level.Compression) {
		return 1
	}
	// Effective downsample from level to output.
	dx := float64(l0Rect.Dx()) / (level.Downsample * float64(outSize.X))
	dy := float64(l0Rect.Dy()) / (level.Downsample * float64(outSize.Y))
	d := dx
	if dy > d {
		d = dy
	}
	switch {
	case d >= 8:
		return 8
	case d >= 4:
		return 4
	case d >= 2:
		return 2
	default:
		return 1
	}
}

// bestLevelForRegion is a thin wrapper around imageBestLevelForDownsample
// that computes the downsample from l0Rect + outSize.
func (s *Slide) bestLevelForRegion(imageIdx int, l0Rect image.Rectangle, outSize image.Point) Level {
	dx := float64(l0Rect.Dx()) / float64(outSize.X)
	dy := float64(l0Rect.Dy()) / float64(outSize.Y)
	d := dx
	if dy > d {
		d = dy
	}
	if d < 1.0 {
		d = 1.0
	}
	levelIdx := s.imageBestLevelForDownsample(imageIdx, d)
	level, err := s.r.Level(imageIdx, levelIdx)
	if err != nil {
		// Should never happen if imageBestLevelForDownsample is correct.
		return Level{}
	}
	return level
}
