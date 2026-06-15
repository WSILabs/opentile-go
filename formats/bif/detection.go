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

// Detect reports whether file is a BIF candidate. The rule: at least one
// IFD's XMP packet (TIFF tag 700) contains the substring `<iScan`. This
// mirrors openslide's detection (`INITIAL_XML_ISCAN = "iScan"`), which keys
// solely on the XMP marker — and catches both spec-compliant DP scanners
// (whose IFD 0 XMP starts `<?xml ... ?><Metadata><iScan ...>`) and legacy
// iScan slides (whose IFD 0 XMP starts directly `<iScan ...>`).
//
// The marker alone is the discriminator — we do NOT additionally require
// BigTIFF. The BIF whitepaper mandates BigTIFF for the DP generation, but
// legacy iScan scanners (Coreo/HT, ~2010-2012, BuildVersion 3.x) wrote
// *classic* little-endian TIFF. The earlier BigTIFF gate wrongly rejected
// those, so they fell through to the generic-TIFF reader, which renders BIF's
// serpentine (boustrophedon, bottom-up) tile order scrambled — the "corrupt
// BIF" symptom in downstream viewers (#37). The serpentine + blank-tile +
// associated-image machinery is container-agnostic (it operates on parsed
// pages), so the reader handles classic-TIFF iScan slides once detected.
//
// The `<iScan` substring is BIF-specific: verified 0 false positives across
// the non-BIF fixtures (SVS, NDPI, generic TIFF, OME-TIFF, Philips TIFF), none
// of which carry an `<iScan` XMP element.
func Detect(file *tiff.File) bool {
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
