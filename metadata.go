package opentile

import "time"

// PropertyXxx are the canonical opentile-go cross-format keys for
// Metadata.Properties. Format readers use these constants to
// populate well-known cross-format fields that don't have typed
// struct positions.
//
// Added in v0.17.
const (
	// PropertyCaseNumber is the clinical / specimen case identifier.
	PropertyCaseNumber = "case-number"
	// PropertyUserName is the scan operator / user name.
	PropertyUserName = "user-name"
	// PropertyScannedAreaMM2 is the physical scanned area in mm²
	// (string-formatted float; parse via strconv.ParseFloat).
	PropertyScannedAreaMM2 = "scanned-area-mm2"
	// PropertyScanDurationSec is the wall-clock scan duration in
	// seconds (string-formatted float; parse via strconv.ParseFloat).
	PropertyScanDurationSec = "scan-duration-seconds"
	// PropertyComments is free-text user comments (distinct from
	// ImageDescription, which is the structured per-format description).
	PropertyComments = "comments"
)

// Metadata is the common subset of slide metadata surfaced across all formats.
// Format packages embed this struct to add format-specific fields exposed via
// type assertion on Tiler.Metadata().
type Metadata struct {
	Magnification       float64 // 0 if unknown
	ScannerManufacturer string
	ScannerModel        string
	ScannerSoftware     []string
	ScannerSerial       string
	// AcquisitionDateTime is the time the slide was scanned. Partial Date
	// or Time values that fail time.Parse yield the zero value
	// (time.Time{}); time.Time{}.IsZero() == true is the "unknown"
	// sentinel. Callers should always check IsZero rather than comparing
	// against a specific time — different scanner vendors emit dates in
	// different formats and our parsers are lenient but not exhaustive.
	AcquisitionDateTime time.Time

	// MicronsPerPixel is populated when MicronsPerPixelX and
	// MicronsPerPixelY are both set and equal (strict ==). When the
	// format reports asymmetric pixel size, MicronsPerPixel is zero
	// and consumers should consult the per-axis fields. Zero indicates
	// "unknown OR asymmetric"; check MicronsPerPixelX/Y to disambiguate.
	//
	// Added in v0.17.
	MicronsPerPixel float64

	// MicronsPerPixelX / MicronsPerPixelY are the per-axis pixel size
	// in microns. Zero indicates the format didn't report it.
	//
	// Added in v0.17.
	MicronsPerPixelX float64
	MicronsPerPixelY float64

	// ImageDescription is the structured per-format description (e.g.,
	// SVS ImageDescription TIFF tag, OME-XML <Image Description>
	// attribute). Empty when the format has no equivalent. For free-
	// text user comments, see Properties[PropertyComments].
	//
	// Added in v0.17.
	ImageDescription string

	// Properties is a flat key-value map for additional metadata
	// that doesn't fit the typed fields. Two key conventions:
	//
	//   - opentile-go-canonical keys (lowercase-with-hyphens):
	//     PropertyCaseNumber, PropertyUserName, PropertyScannedAreaMM2,
	//     PropertyScanDurationSec, PropertyComments. Populated by
	//     format readers when their format exposes the equivalent.
	//
	//   - vendor-namespaced keys (vendor.<key>): vendor-specific
	//     fields surfaced as-is. Format-prefixed: e.g., "szi.vendor.
	//     SerialNumber", "aperio.AppMag".
	//
	// Missing keys mean the format didn't expose that field. Numeric
	// values are string-formatted floats (parseable via
	// strconv.ParseFloat).
	//
	// Added in v0.17.
	Properties map[string]string

	// Writer identifies the software that wrote this file — the
	// file producer, distinct from ScannerManufacturer (scanner OEM)
	// and ScannerSoftware []string (broader software stack).
	//
	// Format-specific population:
	//   SVS Aperio canonical    "Aperio Image Library v11.2.1"
	//   SVS Grundium / other    "Grundium Ocus" (comma-suffix writer
	//                            from v0.18 detection)
	//   OME-TIFF                "OME Bio-Formats 6.0.0-rc1" (Creator
	//                            attribute; promoted from Properties)
	//   SZI                     "<SoftwareName> <SoftwareVersion>"
	//                            (e.g., "OcusScan 3.1.4")
	//   COG-WSI                 "wsitools/<WSIToolsVersion>" (file
	//                            producer; source scanner stays in
	//                            ScannerManufacturer per spec)
	//   Generic-TIFF (wsi-tools  "wsitools/<version>" from the
	//     fixtures avif/jxl/      wsi-tools ImageDescription parser
	//     htj2k/webp)
	//   NDPI / Philips / BIF /  format-specific Software field (often
	//     IFE / Leica SCN        equals ScannerSoftware[0])
	//
	// Empty when the format provides no writer indication. Consumers
	// checking presence compare against "" explicitly.
	//
	// Added in v0.20.
	Writer string
}

// SetMPPSymmetric populates MicronsPerPixel from MicronsPerPixelX and
// MicronsPerPixelY when they are equal (strict ==). When asymmetric,
// MicronsPerPixel is zeroed.
//
// Format readers call this after setting the per-axis fields.
//
// Added in v0.17.
func (m *Metadata) SetMPPSymmetric() {
	if m.MicronsPerPixelX > 0 && m.MicronsPerPixelX == m.MicronsPerPixelY {
		m.MicronsPerPixel = m.MicronsPerPixelX
	} else {
		m.MicronsPerPixel = 0
	}
}

// SetProperty is a nil-safe setter for Properties. Lazily initializes
// the map on first use.
//
// Added in v0.17.
func (m *Metadata) SetProperty(key, value string) {
	if m.Properties == nil {
		m.Properties = make(map[string]string)
	}
	m.Properties[key] = value
}
