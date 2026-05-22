package generictiff

import (
	"strconv"
	"strings"
	"time"
)

// wsiToolsPrefix is the marker that identifies an ImageDescription
// produced by the user's wsi-tools transcoder. Match is by string
// prefix; non-wsi-tools ImageDescriptions skip the parse entirely.
const wsiToolsPrefix = "wsi-tools/"

// wsiToolsMetadata is the parsed wsi-tools ImageDescription. Populated
// by parseWSIToolsDescription; consumed by buildMetadata to override
// or supplement standard-TIFF-tag-derived metadata fields.
//
// Per v0.14 sealed Q2: the parsed fields populate the existing
// cross-format Metadata struct (Magnification, ScannerManufacturer,
// AcquisitionDateTime, MicronsPerPixel). The wsi-tools format is
// not exposed via a separate public accessor; consumers wanting full
// provenance (source, codec, version) read the raw ImageDescription
// string.
type wsiToolsMetadata struct {
	hasMag              bool
	magnification       float64
	hasScanner          bool
	scannerManufacturer string
	hasDate             bool
	acquisitionDate     time.Time
	hasMPP              bool
	micronsPerPixel     float64
	Version             string // v0.20: wsi-tools version for Writer population
}

// parseWSIToolsDescription parses an ImageDescription string in the
// wsi-tools transcoder format. Returns (parsed, true) when the
// string starts with `wsi-tools/`; otherwise returns (zero, false)
// and the caller should not consult the parsed value.
//
// Lenient: malformed values (e.g., non-numeric mpp) yield a zero
// value on that field but don't fail the parse. Unknown keys are
// ignored — forward-compatible with future wsi-tools fields.
//
// Format: `wsi-tools/<version> transcode key=value key="quoted value" ...`
func parseWSIToolsDescription(desc string) (wsiToolsMetadata, bool) {
	if !strings.HasPrefix(desc, wsiToolsPrefix) {
		return wsiToolsMetadata{}, false
	}
	var md wsiToolsMetadata

	// Extract version: between "wsi-tools/" and the first space or end of string.
	rest := strings.TrimPrefix(desc, wsiToolsPrefix)
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		md.Version = rest[:sp]
	} else {
		md.Version = rest
	}

	for _, tok := range tokeniseKVPairs(desc) {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		key := tok[:eq]
		val := strings.Trim(tok[eq+1:], `"`)
		switch key {
		case "mag":
			val = strings.TrimSuffix(val, "x")
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				md.magnification = f
				md.hasMag = true
			}
		case "scanner":
			if val != "" {
				md.scannerManufacturer = val
				md.hasScanner = true
			}
		case "date":
			if ts, err := time.Parse("2006-01-02", val); err == nil {
				md.acquisitionDate = ts.UTC()
				md.hasDate = true
			}
		case "mpp":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				md.micronsPerPixel = f
				md.hasMPP = true
			}
		}
	}
	return md, true
}

// populateWSIToolsProperties surfaces wsi-tools-specific provenance
// fields (source, codec, version) under the "wsi-tools." Properties
// namespace on md. Caller has already verified desc starts with the
// wsi-tools prefix. Per v0.17 spec §1.2: keys absent from desc remain
// absent from Properties (NOT empty string).
//
// The version string is the suffix immediately after "wsi-tools/" up
// to the next whitespace. source / codec come from the kv pairs.
func populateWSIToolsProperties(md *Metadata, desc string) {
	// version: between "wsi-tools/" and the first space.
	if rest := strings.TrimPrefix(desc, wsiToolsPrefix); rest != desc {
		if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			rest = rest[:sp]
		}
		if rest != "" {
			md.SetProperty("wsi-tools.version", rest)
		}
	}
	// source / codec from kv pairs.
	for _, tok := range tokeniseKVPairs(desc) {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		key := tok[:eq]
		val := strings.Trim(tok[eq+1:], `"`)
		switch key {
		case "source":
			if val != "" {
				md.SetProperty("wsi-tools.source", val)
			}
		case "codec":
			if val != "" {
				md.SetProperty("wsi-tools.codec", val)
			}
		}
	}
}

// tokeniseKVPairs splits a wsi-tools ImageDescription line into
// key=value tokens, treating double-quoted values as a single token.
func tokeniseKVPairs(desc string) []string {
	var out []string
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(desc); i++ {
		c := desc[i]
		if c == '"' {
			inQuotes = !inQuotes
			current.WriteByte(c)
			continue
		}
		if c == ' ' && !inQuotes {
			if current.Len() > 0 {
				out = append(out, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}
