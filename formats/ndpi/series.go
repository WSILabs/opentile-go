package ndpi

import (
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// pageKind classifies an NDPI TIFF page by its semantic role.
type pageKind int

const (
	pageUnknown pageKind = iota
	pageLevel            // pyramid level (tiled or one-frame)
	pageMacro            // overview / macro (associated image)
	pageMap              // map-view (skipped in v0.2; associated in v0.3+)
)

// classifyPage maps an NDPI TIFF page to its semantic role by reading tag
// 65421 (Magnification, FLOAT). This ports tifffile's _series_ndpi logic:
//
//	mag > 0   → pyramid level
//	mag == -1 → Macro (overview)
//	mag == -2 → Map
//
// Direct port of cgohlke/tifffile _series_ndpi (around line 5394 of
// tifffile.py at the time of writing). If tag 65421 is missing or not
// parseable, the page is classified as pageUnknown — upstream treats such
// pages as skip-worthy rather than erroring, and we match that behavior.
func classifyPage(p *tiff.Page) pageKind {
	mag, ok := p.Float32(65421)
	if !ok {
		return pageUnknown
	}
	switch {
	case mag > 0:
		return pageLevel
	case mag == -1.0:
		return pageMacro
	case mag == -2.0:
		return pageMap
	default:
		return pageUnknown
	}
}
