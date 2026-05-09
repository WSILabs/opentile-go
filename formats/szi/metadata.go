package szi

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
)

// Metadata is the SZI-specific scan metadata parsed from the file's
// scan-properties.xml. Cross-format fields (Magnification, scanner
// identity, AcquisitionDateTime) populate the embedded
// opentile.Metadata; SZI-specific fields (per-axis MPP, sensor
// pixel size, scan-area dimensions, case/job identifiers, vendor-
// prefixed open-ended properties) live on the outer struct.
//
// Consumers read the common fields via opentile.Tiler.Metadata() as
// usual; to read the SZI-specific fields, pass the Tiler to
// szi.MetadataOf.
type Metadata struct {
	opentile.Metadata

	// Version is the <image version="..."> attribute.
	Version string
	// Date is the <image date="..."> attribute (YYYY-MM-DD).
	Date time.Time

	// UserName, SoftwareName, SoftwareVersion mirror the canonical
	// scan-properties.xml fields. ScannerSoftware on the embedded
	// opentile.Metadata carries "<SoftwareName> <SoftwareVersion>"
	// as a single-element slice when both are present.
	UserName        string
	SoftwareName    string
	SoftwareVersion string

	// TimeStart / TimeEnd are scan-start / scan-end timestamps in
	// the local clock of the scanner (no timezone). The embedded
	// opentile.Metadata.AcquisitionDateTime mirrors TimeStart.
	TimeStart   time.Time
	TimeEnd     time.Time
	ElapsedTime string

	CaseNumber  string
	ScanJobName string

	ScannerSerialNo string

	CameraName      string
	SensorPixelSize float64 // µm

	ScannedArea float64 // mm²
	ScanWidth   float64 // mm
	ScanHeight  float64 // mm

	// MicronsPerPixel is the canonical per-slide MPP from the
	// <MicronsPerPixel> property if present, else the average of
	// MicronsPerPixelX / MicronsPerPixelY when both are present.
	// Zero when neither path yields a value.
	MicronsPerPixel  float64
	MicronsPerPixelX float64
	MicronsPerPixelY float64

	// Comments is the free-form <Comments> property.
	Comments string

	// VendorProperties holds open-ended custom properties whose
	// name contains a "." separator (per spec page 9: "Just add
	// your scanner name before the field name, separated by a
	// dot, e.g., 'vendor.MicronsX' or 'ScanCompany.FilterName'").
	// Keys are surfaced as-is including the dotted prefix.
	VendorProperties map[string]string
}

// tilerUnwrapper is implemented by opentile wrapper types (e.g., the
// *fileCloser returned by opentile.OpenFile) that hold an inner Tiler.
// Kept unexported because it is a coordination interface between
// opentile and its format packages.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

// maxTilerUnwrapHops caps the number of UnwrapTiler calls MetadataOf
// will make. Mirrors the SVS / NDPI / Philips / OME / BIF / IFE / SCN
// /generictiff precedent: realistic chain length is 1, 16 is ample
// headroom against an accidental cycle.
const maxTilerUnwrapHops = 16

// MetadataOf returns the SZI-specific Metadata if t is an SZI-format
// Tiler (possibly wrapped by opentile.OpenFile's *fileCloser /
// *mmapCloser), otherwise (nil, false). Walks any number of
// wrappers via UnwrapTiler before asserting on the concrete type.
//
//	if md, ok := szi.MetadataOf(tiler); ok {
//	    fmt.Println(md.MicronsPerPixel, md.VendorProperties["vendor.SerialNumber"])
//	}
//
// Mirrors the v0.6+/Philips/OME/IFE/SCN format-specific accessor
// pattern.
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if tt, ok := t.(*Tiler); ok {
			return &tt.szim, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
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

		// SZI-specific Metadata fields.
		case "UserName":
			szim.UserName = p.Value
		case "TimeEnd":
			if t, e := time.Parse("2006-01-02T15:04:05", p.Value); e == nil {
				szim.TimeEnd = t
			}
		case "ElapsedTime":
			szim.ElapsedTime = p.Value
		case "CaseNumber":
			szim.CaseNumber = p.Value
		case "ScanJobName":
			szim.ScanJobName = p.Value
		case "CameraName":
			szim.CameraName = p.Value
		case "SensorPixelSize":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.SensorPixelSize = f
			}
		case "ScannedArea":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScannedArea = f
			}
		case "ScanWidth":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanWidth = f
			}
		case "ScanHeight":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.ScanHeight = f
			}
		case "MicronsPerPixel":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.MicronsPerPixel = f
			}
		case "MicronsPerPixelX":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.MicronsPerPixelX = f
			}
		case "MicronsPerPixelY":
			if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
				szim.MicronsPerPixelY = f
			}
		case "Comments":
			szim.Comments = p.Value
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
	}

	// MicronsPerPixel fallback: average of X/Y if canonical field
	// missing and both axes are present.
	if szim.MicronsPerPixel == 0 && szim.MicronsPerPixelX > 0 && szim.MicronsPerPixelY > 0 {
		szim.MicronsPerPixel = (szim.MicronsPerPixelX + szim.MicronsPerPixelY) / 2
	}

	// Mirror the cross-format metadata onto the embedded struct so
	// szi.MetadataOf consumers can read both layers from one value.
	szim.Metadata = cross
	return cross, szim, nil
}
