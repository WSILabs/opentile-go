package philipstiff

import (
	"strconv"
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
)

// philipsTimeLayout matches Philips's DICOM_ACQUISITION_DATETIME format.
// Example value: "20160718122300.000000". Matches upstream's
// `datetime.strptime(..., r"%Y%m%d%H%M%S.%f")`.
const philipsTimeLayout = "20060102150405.000000"

// Metadata is the Philips-specific slide metadata. Embeds opentile.Metadata
// so the common cross-format fields are populated via the embedded struct;
// Philips-specific fields (PixelSpacing, BitsAllocated, etc.) live on the
// outer struct and are accessed via philipstiff.MetadataOf(tiler).
type Metadata struct {
	opentile.Metadata
	// PixelSpacing is the baseline pixel pitch in millimeters,
	// (Wmm, Hmm) — derived from the first DICOM_PIXEL_SPACING entry.
	// Upstream's TiffPageSeries `pixel_spacing` returns (h, w); we keep
	// the source order (w, h) for clarity.
	PixelSpacing [2]float64

	// DICOM bit-allocation properties (zero-valued when absent).
	BitsAllocated       int
	BitsStored          int
	HighBit             int
	PixelRepresentation string

	// LossyImageCompressionMethod is the multi-value DICOM compression
	// method list (e.g. "PHILIPS_DP_1_0", "PHILIPS_TIFF_1_0").
	LossyImageCompressionMethod []string
	// LossyImageCompressionRatio is the first ratio value upstream
	// returns from the multi-value DICOM string.
	LossyImageCompressionRatio float64
}

// parseMetadata parses the Philips DICOM-XML attribute document and
// returns a populated Metadata. Direct port of
// PhilipsTiffMetadata.__init__ + accessors
// (philips_tiff_metadata.py:29-121). Tags listed in upstream's TAGS
// constant that are absent from the input XML leave the corresponding
// Metadata field at its zero value (matching upstream's "Optional"
// accessors that return None).
func parseMetadata(xmlStr string) (Metadata, error) {
	var md Metadata

	attrs, err := walkAttributes(xmlStr)
	if err != nil {
		return md, err
	}

	tags := map[string]string{}
	for _, a := range attrs {
		if a.Text == "" {
			continue
		}
		// First non-empty value wins (upstream:
		//   if name in self._tags and self._tags[name] is None).
		if _, seen := tags[a.Name]; seen {
			continue
		}
		tags[a.Name] = a.Text
	}

	if v, ok := tags["DICOM_MANUFACTURER"]; ok {
		md.ScannerManufacturer = v
	}
	if v, ok := tags["DICOM_DEVICE_SERIAL_NUMBER"]; ok {
		md.ScannerSerial = stripQuotes(v)
	}
	if v, ok := tags["DICOM_SOFTWARE_VERSIONS"]; ok {
		md.ScannerSoftware = splitMultiValue(v)
	}
	if v, ok := tags["DICOM_ACQUISITION_DATETIME"]; ok {
		// Upstream wraps the parse in try/except → returns None on
		// ValueError. We mirror by leaving AcquisitionDateTime at zero.
		if t, err := time.Parse(philipsTimeLayout, v); err == nil {
			md.AcquisitionDateTime = t
		}
	}
	if v, ok := tags["DICOM_PIXEL_SPACING"]; ok {
		if w, h, ok := parseTwoFloats(v); ok {
			md.PixelSpacing = [2]float64{w, h}
		}
	}
	if v, ok := tags["DICOM_BITS_ALLOCATED"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			md.BitsAllocated = n
		}
	}
	if v, ok := tags["DICOM_BITS_STORED"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			md.BitsStored = n
		}
	}
	if v, ok := tags["DICOM_HIGH_BIT"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			md.HighBit = n
		}
	}
	if v, ok := tags["DICOM_PIXEL_REPRESENTATION"]; ok {
		md.PixelRepresentation = strings.TrimSpace(v)
	}
	if v, ok := tags["DICOM_LOSSY_IMAGE_COMPRESSION_METHOD"]; ok {
		md.LossyImageCompressionMethod = splitMultiValue(v)
	}
	if v, ok := tags["DICOM_LOSSY_IMAGE_COMPRESSION_RATIO"]; ok {
		// Upstream returns the first ratio value:
		//   self._split_and_cast_text(...,float)[0].
		parts := splitMultiValue(v)
		if len(parts) > 0 {
			if r, err := strconv.ParseFloat(parts[0], 64); err == nil {
				md.LossyImageCompressionRatio = r
			}
		}
	}

	return md, nil
}

// splitMultiValue strips literal quote characters from s, splits on
// whitespace, and returns the resulting tokens. Mirrors upstream's
// `string.replace('"', ”).split()`.
func splitMultiValue(s string) []string {
	return strings.Fields(strings.ReplaceAll(s, `"`, ""))
}

// stripQuotes returns s with any literal quote characters removed and
// surrounding whitespace trimmed. Used for single-value scalar tags.
func stripQuotes(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, `"`, ""))
}
