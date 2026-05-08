package tiff

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// PyramidLevelInfo is the value-type input to [ClassifyPyramid] —
// the small subset of TIFF tags the validator needs from each IFD.
// Format-package callers project this from a *Page slice via a
// trivial loop; the validator itself is pure value-in / value-out
// for testability.
type PyramidLevelInfo struct {
	Index int // caller-defined index (typically the IFD's position)

	Width, Height uint32 // ImageWidth (256), ImageLength (257)
	TileWidth     uint32 // TileWidth (322); 0 means stripped
	TileLength    uint32 // TileLength (323); 0 means stripped

	Compression     uint32 // Compression (259)
	Photometric     uint32 // PhotometricInterpretation (262)
	SamplesPerPixel uint32 // SamplesPerPixel (277)
	BitsPerSample   uint32 // BitsPerSample (258); first sample's value
}

// IsTiled reports whether the IFD has tile tags (i.e., a candidate
// pyramid level). Stripped IFDs are not tiled and are routed to
// Others by [ClassifyPyramid].
func (p PyramidLevelInfo) IsTiled() bool {
	return p.TileWidth != 0 && p.TileLength != 0
}

// Area returns the IFD's pixel area for sort ordering. int64 to
// avoid overflow on huge WSI baselines (~5 GiB pixels in BIF land).
func (p PyramidLevelInfo) Area() int64 {
	return int64(p.Width) * int64(p.Height)
}

// ClassifyPyramidConfig holds the validator tolerances. Use
// [DefaultClassifyPyramidConfig] for the v0.10-sealed values.
type ClassifyPyramidConfig struct {
	// MinLevels is the minimum pyramid depth (≥3 per Q2 sealed).
	MinLevels int

	// InterAxisTolerance bounds |ratio_W - ratio_H| / ratio_W within
	// a single level transition (Q1: ±2% = 0.02). Anisotropic
	// downsampling (W and H scaled differently) is essentially never
	// legitimate WSI; this catches it.
	InterAxisTolerance float64

	// InterLevelTolerance bounds drift between consecutive scale
	// ratios (Q1: ±5% = 0.05). Tolerates ceil/floor rounding on
	// odd dimensions across deep pyramids.
	InterLevelTolerance float64

	// MaxLeftoverTiled bounds the count of tiled IFDs that fail to
	// fit the pyramid scale chain. ≤2 reflects the "tiled
	// associated images" allowance; >2 leftover tiled IFDs likely
	// indicates a multi-pyramid TIFF (Q7: rejected; OME's job).
	MaxLeftoverTiled int

	// LeftoverTiledMaxAreaRatio bounds the area of leftover tiled
	// IFDs as a fraction of baseline area. 0.01 = 1%. Bigger
	// leftover tiled IFDs are likely a competing pyramid; reject
	// the file.
	LeftoverTiledMaxAreaRatio float64
}

// DefaultClassifyPyramidConfig returns the v0.11-sealed thresholds.
// v0.11 relaxed two caps from the v0.10 originals (R1 + R2 in the
// v0.11 design spec) to handle real-world Grundium output:
//
//   - MinLevels: 1 (was 3 in v0.10) — single-level tiled TIFFs are
//     a valid encoder pattern (Grundium scan_619 has only one IFD).
//   - InterAxisTolerance: 0.02 / ±2% (Q1; unchanged)
//   - InterLevelTolerance: 0.05 / ±5% (Q1; unchanged)
//   - MaxLeftoverTiled: 2 (Q7; unchanged)
//   - LeftoverTiledMaxAreaRatio: 0.05 (was 0.01 in v0.10) — bumped to
//     accept mixed-ratio chains (Grundium scan_620's 4× then 2×/2×/2×
//     pyramid layout where the orphan level is 1.56% of baseline).
func DefaultClassifyPyramidConfig() ClassifyPyramidConfig {
	return ClassifyPyramidConfig{
		MinLevels:                 1,
		InterAxisTolerance:        0.02,
		InterLevelTolerance:       0.05,
		MaxLeftoverTiled:          2,
		LeftoverTiledMaxAreaRatio: 0.05,
	}
}

// ClassifyPyramidResult is the outcome of a successful classification.
// Pyramid is the validated pyramid (largest-first; ≥cfg.MinLevels
// entries); Others is the leftover non-pyramid IFDs (stripped, plus
// any tiled IFDs that didn't fit the pyramid scale chain — those are
// associated-image candidates for the format reader's classifier).
type ClassifyPyramidResult struct {
	Pyramid []PyramidLevelInfo
	Others  []PyramidLevelInfo
}

// Sentinel errors classifying why pyramid detection failed. Format
// readers use these to distinguish "this isn't a generic pyramid
// file at all" from "this is multi-pyramid (route to OME)" from
// "this has weird metadata we can't read."
var (
	ErrPyramidTooFewLevels    = errors.New("tiff: fewer than minimum pyramid levels")
	ErrPyramidScaleMismatch   = errors.New("tiff: pyramid scale ratios fail tolerance")
	ErrPyramidPhotometric     = errors.New("tiff: pyramid IFD has unsupported photometric/sample format")
	ErrPyramidCompression     = errors.New("tiff: pyramid IFD has unsupported compression")
	ErrPyramidMultiplePyramid = errors.New("tiff: too many leftover tiled IFDs (multi-pyramid TIFF)")
)

// validPhotometric reports whether photo is one of the v0.10-allowed
// values: 1 (MinIsBlack-grayscale), 2 (RGB), 6 (YCbCr). Palette (3),
// CMYK (5), MinIsWhite (0), and others are excluded.
func validPhotometric(photo uint32) bool {
	return photo == 1 || photo == 2 || photo == 6
}

// validCompression reports whether comp is one of the allowed
// TIFF compression tag values:
//
//   - v0.10: 1 (None), 5 (LZW), 7 (JPEG), 8 (Deflate), 33003 (JPEG 2000)
//   - v0.14 additions:
//   - 34712 — registered JP2K code (libtiff convention; we already
//     accept Aperio's nonstandard 33003)
//   - 50001 — WebP (libtiff convention)
//   - 50002 — JPEG XL (wsi-tools convention)
//   - 60001 — AVIF (wsi-tools convention; private/experimental range)
//   - 60003 — HTJ2K (wsi-tools convention; private/experimental range)
func validCompression(comp uint32) bool {
	switch comp {
	case 1, 5, 7, 8, 33003,
		34712, 50001, 50002, 60001, 60003:
		return true
	default:
		return false
	}
}

// validSampleFormat reports whether the sample format is uint8 with
// 8 bits/sample, the v0.10 scope. SamplesPerPixel can be 1
// (grayscale) or 3 (RGB/YCbCr); BitsPerSample must be 8.
func validSampleFormat(spp, bps uint32) bool {
	if bps != 8 {
		return false
	}
	return spp == 1 || spp == 3
}

// ClassifyPyramid attempts to find a coherent tiled pyramid in
// pages per the v0.10 generic-TIFF spec §4 algorithm:
//
//  1. Filter to tiled IFDs with valid photo / compression / sample
//     format. Tiled IFDs that fail those checks go to Others
//     (associated candidates that the format reader's classifier
//     handles).
//  2. Sort tiled-and-valid candidates by area, descending.
//  3. Greedily build the pyramid chain from the largest: each
//     successive candidate must satisfy both the inter-axis
//     tolerance (|ratio_W - ratio_H| / ratio_W ≤ cfg.InterAxisTolerance)
//     and the inter-level tolerance (its scale ratio must agree
//     with prior transitions within cfg.InterLevelTolerance).
//  4. Validate ≥cfg.MinLevels in the resulting chain.
//  5. Multi-pyramid rejection: leftover tiled IFDs (didn't fit
//     scale) are allowed up to cfg.MaxLeftoverTiled if each is
//     smaller than cfg.LeftoverTiledMaxAreaRatio × baseline area;
//     otherwise reject (multi-pyramid TIFF — OME's job).
//
// Stripped IFDs (TileWidth + TileLength absent or zero) always go
// to Others; the format reader's classifier decides what they are.
func ClassifyPyramid(infos []PyramidLevelInfo, cfg ClassifyPyramidConfig) (ClassifyPyramidResult, error) {
	var (
		tiledValid []PyramidLevelInfo
		others     []PyramidLevelInfo
	)
	for _, p := range infos {
		if !p.IsTiled() {
			others = append(others, p)
			continue
		}
		// Tiled IFDs with invalid photo / compression / sample format
		// are candidate associated images, not pyramid levels.
		if !validPhotometric(p.Photometric) || !validCompression(p.Compression) || !validSampleFormat(p.SamplesPerPixel, p.BitsPerSample) {
			others = append(others, p)
			continue
		}
		tiledValid = append(tiledValid, p)
	}

	// Sort by area, descending. Ties are unlikely in practice but
	// stable-sorted by index for determinism.
	sort.SliceStable(tiledValid, func(i, j int) bool {
		if tiledValid[i].Area() != tiledValid[j].Area() {
			return tiledValid[i].Area() > tiledValid[j].Area()
		}
		return tiledValid[i].Index < tiledValid[j].Index
	})

	if len(tiledValid) < cfg.MinLevels {
		return ClassifyPyramidResult{}, fmt.Errorf("%w: got %d tiled+valid IFDs, need ≥%d",
			ErrPyramidTooFewLevels, len(tiledValid), cfg.MinLevels)
	}

	// Greedy pyramid chain from the largest.
	pyramid := []PyramidLevelInfo{tiledValid[0]}
	var ratios []float64
	var leftoverTiled []PyramidLevelInfo
	for i := 1; i < len(tiledValid); i++ {
		cand := tiledValid[i]
		last := pyramid[len(pyramid)-1]
		if cand.Width >= last.Width || cand.Height >= last.Height {
			// Equal-or-larger than the last accepted: not a downsample
			// step. Treat as leftover tiled (could be a duplicate
			// or part of another pyramid).
			leftoverTiled = append(leftoverTiled, cand)
			continue
		}
		rW := float64(last.Width) / float64(cand.Width)
		rH := float64(last.Height) / float64(cand.Height)
		interAxis := math.Abs(rW-rH) / rW
		if interAxis > cfg.InterAxisTolerance {
			leftoverTiled = append(leftoverTiled, cand)
			continue
		}
		if len(ratios) > 0 {
			drift := math.Abs(rW-ratios[len(ratios)-1]) / ratios[len(ratios)-1]
			if drift > cfg.InterLevelTolerance {
				leftoverTiled = append(leftoverTiled, cand)
				continue
			}
		}
		pyramid = append(pyramid, cand)
		ratios = append(ratios, rW)
	}

	if len(pyramid) < cfg.MinLevels {
		return ClassifyPyramidResult{}, fmt.Errorf("%w: pyramid chain shrank to %d after scale filtering (need ≥%d)",
			ErrPyramidScaleMismatch, len(pyramid), cfg.MinLevels)
	}

	// Multi-pyramid rejection: too many or too-large leftover tiled
	// IFDs indicates this isn't a single-pyramid generic TIFF.
	if len(leftoverTiled) > cfg.MaxLeftoverTiled {
		return ClassifyPyramidResult{}, fmt.Errorf("%w: %d leftover tiled IFDs (max %d allowed as associated)",
			ErrPyramidMultiplePyramid, len(leftoverTiled), cfg.MaxLeftoverTiled)
	}
	baselineArea := pyramid[0].Area()
	for _, l := range leftoverTiled {
		if float64(l.Area())/float64(baselineArea) > cfg.LeftoverTiledMaxAreaRatio {
			return ClassifyPyramidResult{}, fmt.Errorf("%w: leftover tiled IFD %d is %.2f%% of baseline (max %.2f%%)",
				ErrPyramidMultiplePyramid, l.Index,
				100*float64(l.Area())/float64(baselineArea),
				100*cfg.LeftoverTiledMaxAreaRatio)
		}
	}
	// Survived: tiled leftovers are tiled associated images, append
	// to Others.
	others = append(others, leftoverTiled...)

	return ClassifyPyramidResult{Pyramid: pyramid, Others: others}, nil
}

// PyramidLevelInfoFromPage projects the v0.10-relevant tags off a
// *Page into a [PyramidLevelInfo] for [ClassifyPyramid] consumption.
// idx is the caller-defined index (typically the page's position
// in the file). Returns zero values for any tag the page doesn't
// carry — IsTiled() etc. handle the absence cases.
func PyramidLevelInfoFromPage(idx int, p *Page) PyramidLevelInfo {
	iw, _ := p.ImageWidth()
	il, _ := p.ImageLength()
	tw, _ := p.TileWidth()
	tl, _ := p.TileLength()
	comp, _ := p.Compression()
	photo, _ := p.Photometric()
	spp, _ := p.SamplesPerPixel()
	bps, _ := p.BitsPerSample()
	return PyramidLevelInfo{
		Index:           idx,
		Width:           iw,
		Height:          il,
		TileWidth:       tw,
		TileLength:      tl,
		Compression:     comp,
		Photometric:     photo,
		SamplesPerPixel: spp,
		BitsPerSample:   bps,
	}
}
