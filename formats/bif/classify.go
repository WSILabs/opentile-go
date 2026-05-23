package bif

import (
	"strings"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// Generation discriminates between BIF format generations. The
// classification controls which behavioural path each Tiler takes —
// see spec §4 for the rationale.
type Generation int

const (
	// GenerationSpecCompliant routes a slide whose IFD-0
	// `<iScan>/@ScannerModel` starts with `"VENTANA DP"` (DP 200,
	// DP 600, future DP scanners) to the Roche BIF whitepaper's
	// behaviour: probability-map IFD, ScanWhitePoint filling of empty
	// tiles, AOI-origin metadata, all four `Direction` values
	// accepted on `<TileJointInfo>`.
	GenerationSpecCompliant Generation = iota

	// GenerationLegacyIScan routes everything else iScan-tagged
	// (missing ScannerModel, iScan Coreo, iScan HT, ScannerModel that
	// doesn't start with "VENTANA DP") to behaviour matching
	// openslide's existing reader: thumbnail not probability,
	// ScanWhitePoint defaults to 255, etc.
	GenerationLegacyIScan
)

// String returns a stable string representation suitable for
// `Tiler.Metadata().Get("bif.generation")`.
func (g Generation) String() string {
	switch g {
	case GenerationSpecCompliant:
		return "spec-compliant"
	case GenerationLegacyIScan:
		return "legacy-iscan"
	default:
		return "unknown"
	}
}

// scannerModelPrefix is the literal prefix that routes a slide to
// `GenerationSpecCompliant`. The trailing-space-less form catches
// `"VENTANA DP200"` (no space, hypothetical) and `"VENTANA DP 200"`,
// `"VENTANA DP 600"`, etc. (with space — current real values).
const scannerModelPrefix = "VENTANA DP"

// classifyGeneration routes a parsed `<iScan>` block to one of the
// two behavioural paths. A nil iScan or empty ScannerModel both fall
// to `GenerationLegacyIScan`.
func classifyGeneration(iscan *bifxml.IScan) Generation {
	if iscan == nil {
		return GenerationLegacyIScan
	}
	if strings.HasPrefix(iscan.ScannerModel, scannerModelPrefix) {
		return GenerationSpecCompliant
	}
	return GenerationLegacyIScan
}
