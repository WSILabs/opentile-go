package szi

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
)

// Metadata is the SZI-specific scan metadata parsed from the file's
// scan-properties.xml. Cross-format fields (Magnification, scanner
// identity, AcquisitionDateTime, per-axis MPP, comments, user name,
// case number, scanned area, scan duration) populate the embedded
// opentile.Metadata; SZI-specific raw representations (sensor pixel
// size, scan-area dimensions, scan-job identifiers, the raw
// "0h17m22s" ElapsedTime string, and the vendor.<key> open-ended
// properties map) live on the outer struct.
//
// Consumers read the common fields via opentile.Slide.Metadata() as
// usual; to read the SZI-specific raw fields, pass the Tiler to
// szi.MetadataOf. Field reads via Go's embedded-struct promotion
// continue to compile: szi.MetadataOf(t).MicronsPerPixel routes
// through opentile.Metadata.MicronsPerPixel.
//
// v0.17 cleanup (Q4 Option B): MicronsPerPixel, MicronsPerPixelX,
// MicronsPerPixelY, Comments, UserName, CaseNumber, and ScannedArea
// were removed from this struct because they are exact duplicates
// of the embedded opentile.Metadata fields / Properties keys
// populated by parseScanProperties. Field reads via promotion
// continue to compile unchanged; only struct-literal construction
// (szi.Metadata{MicronsPerPixel: ...}) breaks — that surface is
// internal/test-only.
type Metadata struct {
	opentile.Metadata

	// Version is the <image version="..."> attribute.
	Version string
	// Date is the <image date="..."> attribute (YYYY-MM-DD).
	Date time.Time

	// SoftwareName, SoftwareVersion mirror the canonical
	// scan-properties.xml fields. ScannerSoftware on the embedded
	// opentile.Metadata carries "<SoftwareName> <SoftwareVersion>"
	// as a single-element slice when both are present.
	SoftwareName    string
	SoftwareVersion string

	// TimeStart / TimeEnd are scan-start / scan-end timestamps in
	// the local clock of the scanner (no timezone). The embedded
	// opentile.Metadata.AcquisitionDateTime mirrors TimeStart.
	TimeStart time.Time
	TimeEnd   time.Time
	// ElapsedTime is the raw "XhYmZs" string (e.g., "0h17m22s")
	// from the scan-properties.xml <ElapsedTime> property. The
	// parsed-seconds form is exposed as
	// Properties[opentile.PropertyScanDurationSec].
	ElapsedTime string

	ScanJobName string

	ScannerSerialNo string

	CameraName      string
	SensorPixelSize float64 // µm

	ScanWidth  float64 // mm
	ScanHeight float64 // mm

	// VendorProperties holds open-ended custom properties whose
	// name contains a "." separator (per spec page 9: "Just add
	// your scanner name before the field name, separated by a
	// dot, e.g., 'vendor.MicronsX' or 'ScanCompany.FilterName'").
	// Keys are surfaced as-is including the dotted prefix. This
	// map is preserved as the canonical SZI-spec surface for
	// vendor extensions even though the cross-format Properties
	// map carries similar data — VendorProperties serves SZI-
	// spec-aware consumers; Properties serves cross-format
	// consumers.
	VendorProperties map[string]string
}

// MetadataOf returns the SZI-specific Metadata if v is (or wraps) an SZI
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
//
//	if md, ok := szi.MetadataOf(slide); ok {
//	    fmt.Println(md.MicronsPerPixel, md.VendorProperties["vendor.SerialNumber"])
//	}
func MetadataOf(v any) (*Metadata, bool) {
	const maxUnwrapHops = 16
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if tt, ok := v.(*Tiler); ok {
			return &tt.szim, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}

// rawScanProperties is the wire form of scan-properties.xml. The
// spec example uses lowercase root <image>; the namespace varies
// (spec page lists "http://www.pathozoom.com/SZI", probed CMU-1
// fixture uses "http://www.pathozoom.com/szi"). xml.Unmarshal
// matches on the local element name regardless of namespace, which
// is the lenient behaviour we want.
type rawScanProperties struct {
	XMLName    xml.Name      `xml:"image"`
	Date       string        `xml:"date,attr"`
	Version    string        `xml:"version,attr"`
	Properties []rawProperty `xml:"properties>property"`
}

type rawProperty struct {
	Name  string `xml:"name"`
	Value string `xml:"value"`
}

// parseScanProperties decodes the scan-properties.xml bytes and
// returns the canonical opentile.Metadata + the SZI-specific
// Metadata. Missing fields land as zero values; malformed numerics
// likewise — the parser is lenient and returns an error only on
// outright XML malformation.
func parseScanProperties(data []byte) (cross opentile.Metadata, szim Metadata, err error) {
	var raw rawScanProperties
	if err = xml.Unmarshal(data, &raw); err != nil {
		return cross, szim, err
	}
	szim.Version = raw.Version
	if raw.Date != "" {
		if d, e := time.Parse("2006-01-02", raw.Date); e == nil {
			szim.Date = d.UTC()
		}
	}
	szim.VendorProperties = make(map[string]string)

	var softwareName, softwareVersion string
	for _, p := range raw.Properties {
		// Vendor-prefixed properties (key contains "."): surface in
		// the typed VendorProperties map, never on the cross-format
		// or canonical SZI struct.
		if strings.Contains(p.Name, ".") {
			szim.VendorProperties[p.Name] = p.Value
			continue
		}
		switch p.Name {
		// Cross-format opentile.Metadata fields.
		case "VendorName":
			cross.ScannerManufacturer = p.Value
		case "ScannerName":
			cross.ScannerModel = p.Value
		case "ObjectiveMagnification":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.Magnification = f
			}
		case "ScannerSerialNo":
			cross.ScannerSerial = p.Value
			szim.ScannerSerialNo = p.Value
		case "TimeStart":
			if t, e := time.Parse("2006-01-02T15:04:05", p.Value); e == nil {
				cross.AcquisitionDateTime = t
				szim.TimeStart = t
			}
		case "SoftwareName":
			softwareName = p.Value
			szim.SoftwareName = p.Value
		case "SoftwareVersion":
			softwareVersion = p.Value
			szim.SoftwareVersion = p.Value

		// v0.17 Q4 Option B: cross-format-canonical fields populate
		// the embedded opentile.Metadata only; format-specific
		// duplicates were removed from szi.Metadata.
		case "MicronsPerPixelX":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.MPP.X = f
			}
		case "MicronsPerPixelY":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.MPP.Y = f
			}
		case "MicronsPerPixel":
			// The single-MPP value, when present, populates per-axis
			// when X/Y aren't separately specified. SZI fixtures in the
			// wild typically emit all three values with X==Y; the
			// spec-example CMU-1.szi emits only the single MicronsPerPixel.
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil && cross.MPP.X == 0 {
				cross.MPP.X = f
				cross.MPP.Y = f
			}
		case "Comments":
			cross.SetProperty(opentile.PropertyComments, p.Value)
		case "UserName":
			cross.SetProperty(opentile.PropertyUserName, p.Value)
		case "CaseNumber":
			cross.SetProperty(opentile.PropertyCaseNumber, p.Value)
		case "ScannedArea":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				cross.SetProperty(opentile.PropertyScannedAreaMM2,
					strconv.FormatFloat(f, 'f', -1, 64))
			}
		case "ElapsedTime":
			szim.ElapsedTime = p.Value // raw "0h17m22s" preserved
			if seconds, ok := parseSZIDuration(p.Value); ok {
				cross.SetProperty(opentile.PropertyScanDurationSec,
					strconv.FormatFloat(seconds, 'f', -1, 64))
			}

		// SZI-specific raw fields without a cross-format equivalent.
		case "TimeEnd":
			if t, e := time.Parse("2006-01-02T15:04:05", p.Value); e == nil {
				szim.TimeEnd = t
			}
		case "ScanJobName":
			szim.ScanJobName = p.Value
		case "CameraName":
			szim.CameraName = p.Value
		case "SensorPixelSize":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.SensorPixelSize = f
			}
		case "ScanWidth":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanWidth = f
			}
		case "ScanHeight":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanHeight = f
			}
		}
	}

	// Populate ScannerSoftware as "<name> <version>" if both present;
	// just <name> if version is absent; nil otherwise.
	if softwareName != "" {
		s := softwareName
		if softwareVersion != "" {
			s += " " + softwareVersion
		}
		cross.ScannerSoftware = []string{s}
		cross.Writer = s // v0.20: SZI's combined SoftwareName+Version is the writer
	}

	// MPP is now a single opentile.MPP{X, Y} struct; X and Y are
	// set independently above from MicronsPerPixelX/Y/MicronsPerPixel.
	// No collapse step needed — MPP.Symmetric() handles X==Y queries.

	// Mirror the cross-format metadata onto the embedded struct so
	// szi.MetadataOf consumers can read both layers from one value.
	szim.Metadata = cross
	return cross, szim, nil
}

// parseSZIDuration parses the SZI ElapsedTime format ("XhYmZs",
// e.g., "0h17m22s") and returns total seconds. Returns 0, false
// on malformed input. The format is loose: hours, minutes, and
// seconds may be any non-negative float; a single literal mismatch
// (missing 'h' / 'm' / 's', extra text, empty input) yields a
// false return rather than a partial parse.
func parseSZIDuration(s string) (float64, bool) {
	var hours, minutes, seconds float64
	n, err := fmt.Sscanf(s, "%fh%fm%fs", &hours, &minutes, &seconds)
	if err != nil || n != 3 {
		return 0, false
	}
	return hours*3600 + minutes*60 + seconds, true
}
