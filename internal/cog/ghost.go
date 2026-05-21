package cog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// GhostArea is the parsed GDAL ghost-area block. Required keys land
// on typed fields; unknown keys land in RawKeys for forward-compat.
type GhostArea struct {
	// SizeBytes is the byte length declared by the
	// GDAL_STRUCTURAL_METADATA_SIZE header line (excluding the size
	// line itself).
	SizeBytes int

	// Required keys (per GDAL COG spec):
	Layout                   string // expected: "IFDS_BEFORE_DATA"
	BlockOrder               string // expected: "ROW_MAJOR"
	BlockLeader              string // expected: "SIZE_AS_UINT4"
	BlockTrailer             string // expected: "LAST_4_BYTES_REPEATED"
	KnownIncompatibleEdition string // expected: "NO"

	// COG-WSI marker. Empty when the ghost area is plain GDAL COG
	// (no WSI extension). Format: "<major>.<minor>" (e.g., "0.1").
	COGWSIVersion string

	// RawKeys carries every key parsed from the ghost area, including
	// the required ones. Forward-compat for spec v0.2+ additions and
	// for vendor extensions.
	RawKeys map[string]string
}

// Required ghost-area keys per the GDAL COG convention. ParseGhostArea
// returns an error if any are missing.
var requiredKeys = []string{
	"LAYOUT",
	"BLOCK_ORDER",
	"BLOCK_LEADER",
	"BLOCK_TRAILER",
	"KNOWN_INCOMPATIBLE_EDITION",
}

// ErrGhostAreaMalformed is returned when the input bytes don't
// match the expected GDAL ghost-area shape (missing size header,
// truncated data, required keys absent, etc.).
var ErrGhostAreaMalformed = errors.New("cog: ghost area malformed")

// ParseGhostArea decodes the ghost-area bytes starting from the
// GDAL_STRUCTURAL_METADATA_SIZE header line. Returns the parsed
// struct with required keys populated; unknown keys land in
// RawKeys for forward-compat. ParseGhostArea does not assert the
// ghost area's COG-WSI-ness — callers check the COGWSIVersion
// field explicitly.
func ParseGhostArea(data []byte) (GhostArea, error) {
	// First line: "GDAL_STRUCTURAL_METADATA_SIZE=NNNNNN bytes"
	const sizePrefix = "GDAL_STRUCTURAL_METADATA_SIZE="
	const sizeSuffix = " bytes"
	if !bytes.HasPrefix(data, []byte(sizePrefix)) {
		return GhostArea{}, fmt.Errorf("%w: missing size header", ErrGhostAreaMalformed)
	}
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return GhostArea{}, fmt.Errorf("%w: size header not terminated", ErrGhostAreaMalformed)
	}
	sizeLine := string(data[len(sizePrefix):nl])
	if !strings.HasSuffix(sizeLine, sizeSuffix) {
		return GhostArea{}, fmt.Errorf("%w: size header missing ' bytes' suffix", ErrGhostAreaMalformed)
	}
	sizeStr := strings.TrimSpace(strings.TrimSuffix(sizeLine, sizeSuffix))
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return GhostArea{}, fmt.Errorf("%w: invalid size %q: %v", ErrGhostAreaMalformed, sizeStr, err)
	}
	if size < 0 {
		return GhostArea{}, fmt.Errorf("%w: negative size %d", ErrGhostAreaMalformed, size)
	}

	// Body lines come after the size header. Parse to end of declared size.
	bodyStart := nl + 1
	bodyEnd := bodyStart + size
	if bodyEnd > len(data) {
		return GhostArea{}, fmt.Errorf("%w: declared size %d exceeds available %d bytes",
			ErrGhostAreaMalformed, size, len(data)-bodyStart)
	}
	body := data[bodyStart:bodyEnd]

	ghost := GhostArea{
		SizeBytes: size,
		RawKeys:   make(map[string]string),
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := line[eq+1:]
		ghost.RawKeys[key] = val
		switch key {
		case "LAYOUT":
			ghost.Layout = val
		case "BLOCK_ORDER":
			ghost.BlockOrder = val
		case "BLOCK_LEADER":
			ghost.BlockLeader = val
		case "BLOCK_TRAILER":
			ghost.BlockTrailer = val
		case "KNOWN_INCOMPATIBLE_EDITION":
			ghost.KnownIncompatibleEdition = val
		case "COG_WSI_VERSION":
			ghost.COGWSIVersion = val
		}
	}

	for _, k := range requiredKeys {
		if _, ok := ghost.RawKeys[k]; !ok {
			return GhostArea{}, fmt.Errorf("%w: missing required key %q", ErrGhostAreaMalformed, k)
		}
	}

	return ghost, nil
}

// ParseCOGWSIVersion parses a "major.minor" string (e.g., "0.1")
// into integer parts. Returns an error on malformed input.
func ParseCOGWSIVersion(s string) (major, minor int, err error) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("cog: malformed version %q (want major.minor)", s)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("cog: malformed major %q: %v", parts[0], err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("cog: malformed minor %q: %v", parts[1], err)
	}
	return major, minor, nil
}
