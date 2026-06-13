package cogwsi

import (
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/formats/svs"
	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Standard TIFF tag numbers consumed by buildMetadata. internal/tiff
// exposes TagDateTime; Make/Model are read via Page.ASCII with the
// canonical numbers.
const (
	tagMake  uint16 = 271
	tagModel uint16 = 272
)

// Property keys for COG-WSI-specific fields surfaced under the
// "cog-wsi." namespace in [opentile.Metadata.Properties].
const (
	PropSourceFormat = "cog-wsi.source-format"
	PropWSIToolsVer  = "cog-wsi.wsitools-version"
	PropSpecVersion  = "cog-wsi.spec-version"
)

// buildMetadata populates the cross-format opentile.Metadata from
// the level-0 IFD + ghost area per plan T6 step 3:
//
//	WSIMPPX/Y (65085/65086) → MicronsPerPixelX/Y (+ SetMPPSymmetric)
//	WSIMagnification (65087) → Magnification
//	Make (271)         → ScannerManufacturer
//	Model (272)        → ScannerModel
//	Software (305)     → ScannerSoftware (semicolon/newline-split)
//	DateTime (306)     → AcquisitionDateTime (TIFF format)
//	ImageDescription   → ImageDescription verbatim
//	WSISourceFormat (65083) → Properties[cog-wsi.source-format]
//	WSIToolsVersion (65084) → Properties[cog-wsi.wsitools-version]
//	ghost COG_WSI_VERSION   → Properties[cog-wsi.spec-version]
//
// Per spec §5.2, the standard TIFF Make/Model/Software/DateTime/
// ImageDescription tags are preserved by the COG-WSI writer from
// the source format — so scanner attribution naturally mirrors the
// underlying source (Aperio, Hamamatsu, Grundium, …) without any
// extra work here.
func buildMetadata(p0 *tiff.Page, ghost cog.GhostArea) opentile.Metadata {
	var md opentile.Metadata

	// Scanner identity from standard TIFF tags.
	if v, ok := p0.ASCII(tagMake); ok {
		md.ScannerManufacturer = strings.TrimSpace(v)
	}
	if v, ok := p0.ASCII(tagModel); ok {
		md.ScannerModel = strings.TrimSpace(v)
	}
	if v, ok := p0.Software(); ok {
		md.ScannerSoftware = splitSoftware(v)
	}
	if v, ok := p0.ASCII(tiff.TagDateTime); ok {
		if ts, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(v)); err == nil {
			md.AcquisitionDateTime = ts
		}
	}
	if v, ok := p0.ImageDescription(); ok {
		md.ImageDescription = strings.TrimSpace(v)
	}

	// WSI private tags — canonical microns + magnification.
	if v, ok := p0.WSIMPPX(); ok {
		md.MPP.X = v
	}
	if v, ok := p0.WSIMPPY(); ok {
		md.MPP.Y = v
	}
	if v, ok := p0.WSIMagnification(); ok {
		md.Magnification = v
	}

	// Properties — cog-wsi.* namespace for fields that don't fit
	// the typed cross-format struct.
	props := map[string]string{}
	if v, ok := p0.WSISourceFormat(); ok {
		props[PropSourceFormat] = v
		// v0.20.1 (R23): for SVS-sourced COG-WSI, re-run v0.18's
		// SVS writer detection on the preserved Software tag.
		// wsitools preserves Make/Software verbatim from source, but
		// for SVS sources the standard Make tag is just the format-
		// vendor label ("Aperio" pre-v0.18 behavior) — the actual
		// writer-vendor information lives in the comma-suffix of
		// the Software field (e.g., "Aperio Image, Grundium Ocus"
		// → manufacturer="Grundium", model="Ocus"). When the SVS
		// reader opens a Grundium SVS directly it parses this from
		// ImageDescription; the cogwsi reader gets the same info
		// via the preserved Software tag.
		if v == "svs" {
			if sw, swOK := p0.Software(); swOK {
				firstLine := sw
				if i := strings.IndexByte(sw, '\n'); i >= 0 {
					firstLine = sw[:i]
				}
				if w := svs.DetectWriter(firstLine); w.Manufacturer != "" {
					md.ScannerManufacturer = w.Manufacturer
					md.ScannerModel = w.Model
				}
			}
		}
	}
	if v, ok := p0.WSIToolsVersion(); ok {
		props[PropWSIToolsVer] = v
		// v0.20: Q5 — file producer is wsitools; overrides Software-derived Writer.
		md.Writer = "wsitools/" + v
	}
	if ghost.COGWSIVersion != "" {
		props[PropSpecVersion] = ghost.COGWSIVersion
	}
	if len(props) > 0 {
		md.Properties = props
	}

	return md
}

// splitSoftware splits the Software tag value on common delimiters
// (semicolon, newline). Trims whitespace; drops empty fragments.
// Mirrors the generictiff helper to keep cogwsi independent.
func splitSoftware(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, ";", "\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
