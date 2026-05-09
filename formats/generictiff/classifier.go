// Package generictiff implements opentile-go format support for
// generic tiled pyramidal TIFF — the catch-all reader for tiled
// WSI TIFFs without vendor metadata. Registered last in the
// dispatch order; activates on any TIFF that no vendor factory
// claims.
//
// Spec: docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md.
package generictiff

import (
	"github.com/cornish/opentile-go/internal/tiff"
)

// AssociatedType values returned by [ClassifyAssociated]. Match the
// existing taxonomy used by other format readers, plus a new
// "associated" fallback (sealed in spec §6 / Q5) for IFDs the
// heuristics can't confidently classify.
//
// Other format readers in opentile-go don't import these constants;
// they hardcode the string literals matching this set. Centralizing
// here keeps the generic reader's classifier consistent with the
// existing convention.
//
// v0.15: KindMacro = "macro" was renamed to TypeOverview = "overview"
// to align Type() values with DICOM PS3.3 / Supplement 145 (which uses
// "OVERVIEW") and with the upstream Python opentile we directly port.
const (
	TypeLabel      = "label"
	TypeOverview   = "overview" // v0.15: was KindMacro = "macro"; flipped to align with DICOM + upstream Python opentile
	TypeThumbnail  = "thumbnail"
	TypeAssociated = "associated" // v0.10 addition; classifier-fallback (Q5)
)

// Classifier-tuning thresholds. Sealed at v0.10; not currently
// exposed via a config struct because Q6 deferred the
// WithAssociatedClassifier override Option.
const (
	// labelMaxDim bounds the pixel dimensions for the LZW-label
	// heuristic. Real-world SVS labels are typically <500x500;
	// 1500 is generous to accommodate scanner variance.
	labelMaxDim = 1500

	// macroAspectRatio is the min max/min ratio for the wide-JPEG
	// macro heuristic. Real macros are typically 2.5-4.0; 2.0 is
	// the floor.
	macroAspectRatio = 2.0
	// macroMinDim is the min "larger axis" dimension for a macro
	// (excludes tiny stripped-JPEG IFDs from being mis-classified).
	macroMinDim = 1000
	// macroMaxDim caps the "larger axis" so we don't accidentally
	// grab a whole pyramid level that somehow ended up here.
	macroMaxDim = 5000

	// thumbnailMaxDim bounds dimensions for the stripped-JPEG
	// thumbnail heuristic.
	thumbnailMaxDim = 1500

	// tiledAssocMaxAreaRatio bounds the area of a tiled-but-not-
	// pyramid IFD for it to be considered a tiled associated image.
	// Matches ClassifyPyramid's LeftoverTiledMaxAreaRatio (1%).
	tiledAssocMaxAreaRatio = 0.01
	// tiledThumbnailMaxAreaRatio: tiled IFDs much smaller than the
	// 1% threshold (0.1%) get classified as "thumbnail" rather than
	// "overview" (was "macro" pre-v0.15).
	tiledThumbnailMaxAreaRatio = 0.001
)

// ClassifyAssociated assigns a Type() value to an IFD that the
// pyramid validator routed into Others (i.e., a non-pyramid IFD —
// stripped, or tiled-but-didn't-fit-the-pyramid-scale). Applies the
// spec §6 heuristics in order; first match wins. Falls through to
// [TypeAssociated] when no heuristic fires.
//
// baseline is the pyramid's largest level (used for area-relative
// checks on tiled associated images).
//
// Heuristics summary (spec §6, heuristic-revision history at the
// bottom of §6 explains the LZW-vs-aspect-ratio reasoning):
//
//  1. Stripped + LZW (comp 5) + dims < 1500×1500           → "label"
//  2. Stripped + JPEG + aspect ≥ 2.0 + larger dim ≥ 1000   → "overview"
//  3. Stripped + JPEG + dims < 1500×1500                   → "thumbnail"
//  4. Tiled + tiny (area < 0.1% baseline)                  → "thumbnail"
//  5. Tiled + small (area < 1% baseline)                   → "overview"
//  6. Anything else                                        → "associated"
func ClassifyAssociated(ifd, baseline tiff.PyramidLevelInfo) string {
	w, h := ifd.Width, ifd.Height
	larger, smaller := w, h
	if h > w {
		larger, smaller = h, w
	}

	if !ifd.IsTiled() {
		// Stripped paths.
		switch {
		case ifd.Compression == 5 && w < labelMaxDim && h < labelMaxDim:
			return TypeLabel
		case ifd.Compression == 7 &&
			larger >= macroMinDim && larger <= macroMaxDim &&
			float64(larger)/float64(smaller) >= macroAspectRatio:
			return TypeOverview
		case ifd.Compression == 7 && w < thumbnailMaxDim && h < thumbnailMaxDim:
			return TypeThumbnail
		}
		return TypeAssociated
	}

	// Tiled-but-not-pyramid paths (area-relative).
	if baseline.Area() > 0 {
		ratio := float64(ifd.Area()) / float64(baseline.Area())
		switch {
		case ratio < tiledThumbnailMaxAreaRatio:
			return TypeThumbnail
		case ratio < tiledAssocMaxAreaRatio:
			return TypeOverview
		}
	}
	return TypeAssociated
}
