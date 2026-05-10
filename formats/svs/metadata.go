package svs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	opentile "github.com/cornish/opentile-go"
)

// Metadata is the SVS-specific slide metadata. It embeds opentile.Metadata so
// the common fields (magnification, scanner identity, acquisition datetime)
// are populated via the embedded struct; Aperio-specific fields (MPP,
// SoftwareLine, Filename) live on the outer struct.
//
// Consumers read the common fields via opentile.Tiler.Metadata() as usual;
// to read the Aperio-specific fields, pass the Tiler to svs.MetadataOf.
//
// AcquisitionDateTime on the embedded opentile.Metadata carries the Aperio
// Date+Time fields parsed verbatim, with no timezone conversion; Aperio does
// not record a timezone and callers should treat the value as local wall-clock
// time from the scanner.
type Metadata struct {
	opentile.Metadata
	MPP          float64 // microns per pixel
	SoftwareLine string  // first line of ImageDescription
	Filename     string  // Aperio "Filename" key if present
}

// writerVendor describes the SVS writer detected from the
// ImageDescription first line. SVS is the WSI ad-hoc standard and
// is written by multiple vendors (Aperio canonical, Grundium,
// likely 3DHistech via export). The first line of the
// ImageDescription names the writer:
//
//	"Aperio Image Library v11.2.1"     → Aperio canonical
//	"Aperio Image, Grundium Ocus"      → Grundium-written; "Ocus" model
//	"Aperio Image, <other>"            → other writer named in suffix
//	anything else                       → undetected; format-default fallback
//
// When undetected, the parser falls back to the "svs" namespace
// for Properties keys; standardized SVS-format keys (MPP, AppMag,
// ScanScope ID) still populate cross-format Metadata regardless.
type writerVendor struct {
	manufacturer string   // e.g., "Aperio", "Grundium", "" if undetected
	model        string   // e.g., "Ocus", "" if not declared
	softwares    []string // sensibly-split software components
}

// detectWriter parses the SVS ImageDescription's first line to
// identify the writer-vendor. See writerVendor for pattern map.
func detectWriter(firstLine string) writerVendor {
	line := strings.TrimSpace(firstLine)
	if line == "" {
		return writerVendor{}
	}
	// "Aperio Image Library v..." → canonical Aperio
	if strings.HasPrefix(line, "Aperio Image Library") {
		return writerVendor{
			manufacturer: "Aperio",
			model:        "",
			softwares:    []string{line},
		}
	}
	// "Aperio Image, <name>" → writer is in the comma-suffix
	if after, found := strings.CutPrefix(line, "Aperio Image,"); found {
		writerID := strings.TrimSpace(after)
		// First word = manufacturer; rest = model.
		// e.g., "Grundium Ocus" → manufacturer="Grundium", model="Ocus"
		// e.g., "Some Vendor Pro 5" → manufacturer="Some", model="Vendor Pro 5"
		// (Conservative: treat the first whitespace-separated word as
		// manufacturer; remainder as model. Vendors with multi-word
		// names will need extension when fixtures surface.)
		fields := strings.Fields(writerID)
		var mfr, mod string
		if len(fields) > 0 {
			mfr = fields[0]
		}
		if len(fields) > 1 {
			mod = strings.Join(fields[1:], " ")
		}
		return writerVendor{
			manufacturer: mfr,
			model:        mod,
			softwares:    []string{"Aperio Image", writerID},
		}
	}
	// Undetected: fall back to "" manufacturer; surface raw line as software.
	return writerVendor{
		manufacturer: "",
		model:        "",
		softwares:    []string{line},
	}
}

// parseDescription decodes the ImageDescription tag stored by Aperio scanners.
// Format: first line is a free-form software banner; subsequent content is
// '|'-separated "key = value" pairs embedded in the same string.
func parseDescription(desc string) (Metadata, error) {
	if !strings.HasPrefix(desc, aperioPrefix) {
		return Metadata{}, errors.New("svs: description is not Aperio")
	}
	var md Metadata

	// Cross-format: surface the full raw ImageDescription tag verbatim so
	// consumers that want the structured Aperio header can read it without
	// reaching back into the TIFF.
	md.ImageDescription = desc

	// Split off the software banner (first line).
	newline := strings.IndexByte(desc, '\n')
	if newline < 0 {
		md.SoftwareLine = desc
		w := detectWriter(desc)
		md.ScannerManufacturer = w.manufacturer
		md.ScannerModel = w.model
		md.ScannerSoftware = w.softwares
		return md, nil
	}
	md.SoftwareLine = strings.TrimRight(desc[:newline], "\r\n ")
	w := detectWriter(md.SoftwareLine)
	md.ScannerManufacturer = w.manufacturer
	md.ScannerModel = w.model
	md.ScannerSoftware = w.softwares

	// Parse '|' separated key-value pairs in the remainder.
	body := desc[newline+1:]
	kv := splitKV(body)

	if v, ok := kv["AppMag"]; ok {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return md, fmt.Errorf("svs: parse AppMag %q: %w", v, err)
		}
		md.Magnification = parsed
	}
	if v, ok := kv["MPP"]; ok {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return md, fmt.Errorf("svs: parse MPP %q: %w", v, err)
		}
		md.MPP = parsed
		// Aperio reports a single MPP; pixels are square, so X == Y.
		md.MicronsPerPixelX = parsed
		md.MicronsPerPixelY = parsed
		md.SetMPPSymmetric()
	}
	if v, ok := kv["ScanScope ID"]; ok {
		md.ScannerSerial = v
	}
	if v, ok := kv["Filename"]; ok {
		md.Filename = v
	}
	if v, ok := kv["User"]; ok {
		md.SetProperty(opentile.PropertyUserName, v)
	}

	// Cross-format vendor passthrough: surface every kv under the
	// detected writer's namespace so consumers can reach format-
	// specific fields without reparsing the raw ImageDescription.
	// Falls back to "svs" if the writer was not detected. Skip keys
	// that contain characters never seen in real Aperio key names —
	// splitKV sees the codec/geometry prelude (e.g. "46000x32914 [...]
	// JPEG/RGB Q=30") as a "key" because it embeds an '=' sign, but
	// it's not a real kv pair.
	namespace := strings.ToLower(w.manufacturer)
	if namespace == "" {
		namespace = "svs"
	}
	for k, v := range kv {
		if !isAperioKey(k) {
			continue
		}
		md.SetProperty(namespace+"."+k, v)
	}

	// Aperio Date/Time are separate fields in MM/DD/YYYY and HH:MM:SS form.
	date, hasDate := kv["Date"]
	tm, hasTime := kv["Time"]
	if hasDate && hasTime {
		parsed, err := parseAperioDateTime(date, tm)
		if err != nil {
			return md, fmt.Errorf("svs: parse Date/Time %q %q: %w", date, tm, err)
		}
		md.AcquisitionDateTime = parsed
	}
	return md, nil
}

// parseAperioDateTime accepts the Aperio MM/DD/YY or MM/DD/YYYY + HH:MM:SS
// formats. Two-digit years are what real-world slides emit as of v11.2.1;
// four-digit years are supported for forward compatibility.
func parseAperioDateTime(date, tm string) (time.Time, error) {
	layouts := []string{
		"01/02/06 15:04:05",   // two-digit year (the observed real-world form)
		"01/02/2006 15:04:05", // four-digit year (forward-compatible)
	}
	input := date + " " + tm
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, input)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// isAperioKey returns true if s looks like a real Aperio header key (e.g.
// "AppMag", "ScanScope ID", "Focus Offset"). Real keys are
// alphanumeric-plus-space; the codec/geometry prelude line that splitKV
// occasionally captures as a junk "key" contains '[', '(', '/', ',',
// ';', or newlines — none of which appear in any real Aperio key.
func isAperioKey(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == ' ' || c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// splitKV parses "k1 = v1|k2 = v2|..." into a map. Whitespace around keys and
// values is trimmed. Tokens without '=' are ignored.
func splitKV(s string) map[string]string {
	out := make(map[string]string)
	for _, tok := range strings.Split(s, "|") {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(tok[:eq])
		v := strings.TrimSpace(tok[eq+1:])
		if k != "" {
			out[k] = v
		}
	}
	return out
}
