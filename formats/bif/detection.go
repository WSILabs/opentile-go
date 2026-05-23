package bif

import (
	"bytes"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// iScanMarker is the substring opentile-go looks for in any IFD's XMP
// packet to identify a BIF candidate. Mirrors openslide's detection
// rule (`INITIAL_XML_ISCAN = "iScan"`) but matches the opening tag
// `<iScan` to avoid false positives on substrings appearing inside
// arbitrary text (e.g., a comment that contains the word "iScan"
// without an XML element).
const iScanMarker = "<iScan"

// Detect reports whether file is a BIF candidate. The rule, per spec
// §5.1: BigTIFF AND at least one IFD's XMP packet (TIFF tag 700)
// contains the substring `<iScan`. Both predicates are necessary —
// classic-TIFF iScan files don't exist (the BIF whitepaper mandates
// BigTIFF), and the substring catches both spec-compliant DP scanners
// (whose IFD 0 XMP starts `<?xml ... ?><Metadata><iScan ...>`) and
// legacy iScan slides (whose IFD 0 XMP starts directly `<iScan ...>`).
//
// Verified across all 17 sample fixtures: 2 BIFs match, 0 false
// positives across the 15 non-BIF fixtures (5 SVS, 3 NDPI, 1 generic
// TIFF, 2 OME-TIFF, 4 Philips TIFF). See `docs/deferred.md §9 → v0.7
// gates → Task 1` for the gate outcome record.
func Detect(file *tiff.File) bool {
	if !file.BigTIFF() {
		return false
	}
	for _, p := range file.Pages() {
		xmp, ok := p.XMP()
		if !ok {
			continue
		}
		if bytes.Contains(xmp, []byte(iScanMarker)) {
			return true
		}
	}
	return false
}
