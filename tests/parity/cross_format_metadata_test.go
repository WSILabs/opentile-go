package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// crossFormatMetadataExpect describes what each format reader must
// populate on cross-format opentile.Metadata. Per-fixture geometry
// stays in format-specific geometry tests; this gate is for the
// cross-format API surface only.
//
// Field semantics:
//
//   - wantMagnification: format reports objective magnification.
//   - wantMPPPerAxis: per-axis MicronsPerPixelX/Y > 0.
//   - wantMPPSymmetric: MicronsPerPixel > 0 (X == Y on this fixture).
//   - wantImageDesc: ImageDescription non-empty.
//   - wantUserName: Properties[PropertyUserName] non-empty.
//   - wantScannerMfr: ScannerManufacturer non-empty.
//   - wantPropKey/wantPropPrefix: Properties has at least one entry
//     starting with the given prefix (used to assert vendor-namespaced
//     passthrough is present, e.g., "aperio.", "ventana.", "philips.",
//     "ome.", "leica.", "iris.aperio.", "wsi-tools.").
//   - wantWriterContains: substring expected to appear in
//     Metadata.Writer (substring match for version flexibility;
//     added in v0.20 alongside the typed Writer field).
//
// The expectations table reflects probe-confirmed truth for each
// fixture as observed in T2-T7 of v0.17 and T2-T4 of v0.20.
type crossFormatMetadataExpect struct {
	fixture            string
	subdir             string // path component under OPENTILE_TESTDIR
	wantMagnification  bool
	wantMPPPerAxis     bool
	wantMPPSymmetric   bool
	wantImageDesc      bool
	wantUserName       bool
	wantScannerMfr     bool
	wantPropPrefix     string
	wantWriterContains string
}

var cfmExpect = []crossFormatMetadataExpect{
	{
		// SVS small-region: Aperio ImageDescription kv, MPP symmetric.
		fixture:            "CMU-1-Small-Region.svs",
		subdir:             "svs",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantUserName:       true, // aperio.User → user-name canonical key
		wantScannerMfr:     true, // "Aperio"
		wantPropPrefix:     "aperio.",
		wantWriterContains: "Aperio Image Library", // canonical Aperio SoftwareLine
	},
	{
		// SVS Grundium scan_620_: non-canonical Aperio writer. v0.18
		// detection sets Writer to the comma-suffix vendor "Grundium Ocus".
		fixture:            "scan_620_.svs",
		subdir:             "svs",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantScannerMfr:     true, // "Grundium" per v0.18
		wantWriterContains: "Grundium Ocus",
	},
	{
		// NDPI: per-axis MPP from XResolution/YResolution. Both fixture
		// values asymmetric by tiny amounts → MicronsPerPixel = 0.
		// Hamamatsu vendor tags surfaced under "hamamatsu." prefix.
		fixture:            "CMU-1.ndpi",
		subdir:             "ndpi",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   false, // asymmetric pixels
		wantImageDesc:      false, // NDPI has no ImageDescription tag
		wantScannerMfr:     true,
		wantPropPrefix:     "hamamatsu.",
		wantWriterContains: "NanoZoomer", // NDPI Model identifier
	},
	{
		// Philips-1: Hamamatsu-scanned. DICOM_PIXEL_SPACING asymmetric;
		// no PIM_DP_SCANNER_OPERATOR_ID so user-name absent. Many
		// philips.* keys.
		fixture:            "Philips-1.tiff",
		subdir:             "philips-tiff",
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   false, // X != Y
		wantImageDesc:      true,
		wantScannerMfr:     true, // "Hamamatsu"
		wantPropPrefix:     "philips.",
		wantWriterContains: "4.0.3", // raw DICOM_SOFTWARE_VERSIONS (quoted)
	},
	{
		// Philips-4: Philips-scanned, DICOM_PIXEL_SPACING symmetric.
		fixture:          "Philips-4.tiff",
		subdir:           "philips-tiff",
		wantMPPPerAxis:   true,
		wantMPPSymmetric: true,
		wantImageDesc:    true,
		wantScannerMfr:   true, // "PHILIPS"
		wantPropPrefix:   "philips.",
	},
	{
		// OME-TIFF Leica-1: Bio-Formats. Magnification from objective
		// NominalMagnification; MPP symmetric (PhysicalSizeX==Y);
		// ImageDescription from <Image Description>; ome.creator/uuid
		// surfaced. Bio-Formats lacks Experimenter → no user-name.
		fixture:            "Leica-1.ome.tiff",
		subdir:             "ome-tiff",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantPropPrefix:     "ome.",
		wantWriterContains: "Bio-Formats", // OME Creator attribute
	},
	{
		// BIF Ventana-1: per-axis from iScan XML, symmetric (0.25).
		// PropertyUserName from iScan UserName attr.
		// ScannerManufacturer = "Roche". 13-18 ventana.<key>.
		fixture:            "Ventana-1.bif",
		subdir:             "bif",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantUserName:       true,
		wantScannerMfr:     true,
		wantPropPrefix:     "ventana.",
		wantWriterContains: "1.1", // iScan BuildVersion "1.1.0.15854"
	},
	{
		// IFE cervix: isotropic MPP from IFE-spec MPP attribute.
		// Magnification (0.625); IFE encoder doesn't write
		// ScannerManufacturer/Model. iris.<key> passthrough (24).
		fixture:            "cervix_2x_jpeg.iris",
		subdir:             "ife",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantPropPrefix:     "iris.",
		wantWriterContains: "GT450", // IFE ImageDescription first line
	},
	{
		// Leica SCN: T6 first-time cross-format Metadata population.
		// MPP from <view>/<pixels> nm→µm; symmetric on all fixtures.
		// ImageDescription = full SCN-XML. leica.<key> passthrough.
		fixture:            "Leica-1.scn",
		subdir:             "scn",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantScannerMfr:     true,
		wantPropPrefix:     "leica.",
		wantWriterContains: "1.4.0", // primary image's DeviceVersion
	},
	{
		// Generic TIFF wsi-tools fixture: MPP from wsi-tools
		// ImageDescription parser. Provenance under wsi-tools.<key>.
		// Aperio also surfaced because wsi-tools fixture preserves
		// upstream Aperio metadata.
		fixture:            "avif-out.tiff",
		subdir:             "generic-tiff",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantImageDesc:      true,
		wantScannerMfr:     true,
		wantPropPrefix:     "wsi-tools.",
		wantWriterContains: "wsitools", // wsi-tools override "wsitools/<version>"
	},
	{
		// Generic TIFF non-wsi-tools fixture: stripped CMU-1 (no MPP).
		// Has ImageDescription verbatim but no parsed cross-format MPP.
		fixture:       "CMU-1.stripped.tiff",
		subdir:        "generic-tiff",
		wantImageDesc: true,
	},
	{
		// SZI CMU-1: spec-example fixture; full canonical-key suite
		// populated (case-number, user-name, scanned-area-mm2,
		// scan-duration-seconds, comments). Scanner = "TestCompany".
		fixture:            "CMU-1.szi",
		subdir:             "szi",
		wantMagnification:  true,
		wantMPPPerAxis:     true,
		wantMPPSymmetric:   true,
		wantUserName:       true,
		wantScannerMfr:     true,
		wantWriterContains: "Scan it", // spec-example SoftwareName+Version
	},
	{
		// SZI Grundium: scanner = "Grundium" / "Ocus"; symmetric MPP.
		// No user/case/comments. Validates real-scanner population.
		fixture:           "scan_618_grundium_SZI.szi",
		subdir:            "szi",
		wantMagnification: true,
		wantMPPPerAxis:    true,
		wantMPPSymmetric:  true,
		wantScannerMfr:    true,
		// Grundium SZI has no SoftwareName/Version; Writer stays empty.
	},
	{
		// COG-WSI: wsitools/<WSIToolsVersion> from private tag 65084.
		// Writer is the file producer; source scanner attribution
		// stays in ScannerManufacturer per the COG-WSI spec.
		fixture:            "CMU-1-Small-Region_cog-wsi.tiff",
		subdir:             "cog-wsi",
		wantImageDesc:      true,
		wantWriterContains: "wsitools",
	},
}

// TestCrossFormatMetadata exercises the v0.17 cross-format Metadata
// surface (MicronsPerPixel, MicronsPerPixelX/Y, ImageDescription,
// Properties) on at least one fixture per format. Skips cleanly when
// OPENTILE_TESTDIR or a specific fixture is missing.
func TestCrossFormatMetadata(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, fx := range cfmExpect {
		t.Run(filepath.Join(fx.subdir, fx.fixture), func(t *testing.T) {
			path := filepath.Join(dir, fx.subdir, fx.fixture)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tlr, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tlr.Close()
			md := tlr.Metadata()

			if fx.wantMagnification && md.Magnification == 0 {
				t.Errorf("Magnification = 0; want > 0")
			}
			if fx.wantMPPPerAxis {
				if md.MicronsPerPixelX <= 0 || md.MicronsPerPixelY <= 0 {
					t.Errorf("MicronsPerPixelX/Y = %v/%v; want both > 0",
						md.MicronsPerPixelX, md.MicronsPerPixelY)
				}
			}
			if fx.wantMPPSymmetric {
				if md.MicronsPerPixel <= 0 {
					t.Errorf("MicronsPerPixel = %v; want > 0 (X==Y on this fixture)",
						md.MicronsPerPixel)
				}
			} else if fx.wantMPPPerAxis {
				// Asymmetric expectation: per-axis populated but
				// MicronsPerPixel must be zero (Q2: SetMPPSymmetric
				// only when X == Y strictly).
				if md.MicronsPerPixel != 0 {
					t.Errorf("MicronsPerPixel = %v; want 0 (asymmetric fixture)",
						md.MicronsPerPixel)
				}
			}
			if fx.wantImageDesc && md.ImageDescription == "" {
				t.Errorf("ImageDescription empty; want non-empty")
			}
			if fx.wantScannerMfr && md.ScannerManufacturer == "" {
				t.Errorf("ScannerManufacturer empty; want non-empty")
			}
			if fx.wantUserName {
				if got := md.Properties[opentile.PropertyUserName]; got == "" {
					t.Errorf("Properties[%q] empty; want non-empty",
						opentile.PropertyUserName)
				}
			}
			if fx.wantWriterContains != "" {
				if !strings.Contains(md.Writer, fx.wantWriterContains) {
					t.Errorf("Writer = %q; want substring %q",
						md.Writer, fx.wantWriterContains)
				}
			}
			if fx.wantPropPrefix != "" {
				found := false
				for k := range md.Properties {
					if len(k) >= len(fx.wantPropPrefix) &&
						k[:len(fx.wantPropPrefix)] == fx.wantPropPrefix {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no Properties key with prefix %q; want at least one",
						fx.wantPropPrefix)
				}
			}
		})
	}
}
