# opentile-go v0.19 — COG-WSI implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship COG-WSI reader support (issues #5 + #6). Two coupled pieces: (1) extend `formats/generictiff/` to honor the WSI private tag set + relax mixed-ratio pyramid validation; (2) add dedicated `formats/cogwsi/` reader with ghost-area dispatch + spec validation + canonical metadata.

**Architecture:** 8 tasks single batch. T1-T2 land foundation packages (`internal/cog/` ghost-area parser + `internal/tiff` WSI tag readers). T3-T4 land generic-TIFF extensions (closes #5). T5-T6 land the dedicated cogwsi reader (closes #6). T7 fixtures + tests. T8 docs + ship.

**Tech stack:** Go 1.23+; existing `internal/tiff` parser; existing `formats/generictiff` infrastructure.

**Spec:** [`docs/superpowers/specs/2026-05-20-opentile-go-v19-cog-wsi-design.md`](../specs/2026-05-20-opentile-go-v19-cog-wsi-design.md).
**Vendored COG-WSI v0.1 spec:** [`docs/specs/2026-05-20-cog-wsi-format.md`](../../specs/2026-05-20-cog-wsi-format.md).

**Existing code anchors (audited 2026-05-20):**
- Pyramid validator drift check: `internal/tiff/classify_pyramid.go:228-234` — the site T3 modifies for integer-multiple acceptance
- Associated-image classifier: `formats/generictiff/classifier.go:86-119::ClassifyAssociated` — the entry point T4 extends with WSI-tag short-circuit
- Format dispatch order: `formats/all/all.go` — T5 inserts cogwsi BEFORE generictiff
- Page accessor pattern (for T2): `internal/tiff/page.go` has typed methods like `ASCII(tag)`, `ScalarArrayU64(tag)`, etc. WSI tag readers follow the same pattern.

**Fixtures (10, in `sample_files/cog-wsi/`):**
- Small: `CMU-1-Small-Region_cog-wsi.tiff` (1.9 MB) — full-walk test fixture
- Medium: `CMU-1_cog-wsi.tiff` (185 MB), `JP2K-33003-1_cog-wsi.tiff` (64 MB), `Ventana-1_cog-wsi.tiff` (225 MB), `Leica-1_cog-wsi.tiff` (226 MB), `scan_620_cog-wsi.tiff` (270 MB), `scan_617_cog-wsi.tiff` (330 MB), `Philips-1_cog-wsi.tiff` (331 MB)
- Large (sampled per 5 MB JSON cap): `cervix_2x_jpeg_cog-wsi.tiff` (2.1 GB), `svs_40x_bigtiff_cog-wsi.tiff` (4.8 GB)

---

## Task layout

8 tasks, single batch:

- T1 — `internal/cog/` ghost-area parser package (GhostArea + ParseGhostArea + ParseCOGWSIVersion + unit tests)
- T2 — `internal/tiff` WSI tag readers (8 typed accessors on `*tiff.Page`)
- T3 — `internal/tiff/classify_pyramid.go` integer-multiple ratio acceptance (Issue #5 part B)
- T4 — `formats/generictiff/classifier.go` WSI-tag-aware short-circuit (Issue #5 part A)
- T5 — `formats/cogwsi/` skeleton (factory + ghost-area dispatch + `opentile.FormatCOGWSI` + register)
- T6 — `formats/cogwsi/` Tiler + spec validation + metadata (closes #6)
- T7 — Fixtures + tests (wire 10 fixtures; new geometry test; SHA fixtures; cross-fixture parity gate)
- T8 — Docs + ship (formats/cogwsi.md + README + deferred §8m retirement + R21 fully retired + CHANGELOG + CLAUDE.md)

---

## T1 — `internal/cog/` ghost-area parser

**Files:**
- Create: `internal/cog/doc.go`, `internal/cog/ghost.go`, `internal/cog/ghost_test.go`

Pure-function package. No I/O. Provides the GDAL ghost-area parser used by `formats/cogwsi/` for both detection (factory) and validation (open time).

- [ ] **Step 1: `internal/cog/doc.go`**

```go
// Package cog parses the GDAL Cloud Optimized GeoTIFF ghost-area
// (the contiguous block of ASCII key-value metadata immediately
// following the TIFF header).
//
// The ghost area lets a reader probe COG (and COG-WSI) structural
// properties without walking IFDs. Per the GDAL convention:
//
//	GDAL_STRUCTURAL_METADATA_SIZE=NNNNNN bytes
//	LAYOUT=IFDS_BEFORE_DATA
//	BLOCK_ORDER=ROW_MAJOR
//	BLOCK_LEADER=SIZE_AS_UINT4
//	BLOCK_TRAILER=LAST_4_BYTES_REPEATED
//	KNOWN_INCOMPATIBLE_EDITION=NO
//	COG_WSI_VERSION=0.1   (COG-WSI files only; absent in plain COG)
//
// This package is pure: no I/O, no TIFF parsing — callers read the
// raw bytes from the file (after the TIFF header) and pass them in.
// Designed to support both COG-WSI detection (the v0.19 use case)
// and any future plain-COG awareness (a side-benefit, not the
// purpose of this package).
package cog
```

- [ ] **Step 2: `internal/cog/ghost.go`**

```go
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
```

- [ ] **Step 3: `internal/cog/ghost_test.go`**

```go
package cog

import (
	"errors"
	"strings"
	"testing"
)

const happyGhost = "GDAL_STRUCTURAL_METADATA_SIZE=000159 bytes\n" +
	"LAYOUT=IFDS_BEFORE_DATA\n" +
	"BLOCK_ORDER=ROW_MAJOR\n" +
	"BLOCK_LEADER=SIZE_AS_UINT4\n" +
	"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
	"KNOWN_INCOMPATIBLE_EDITION=NO\n" +
	"COG_WSI_VERSION=0.1\n"

func TestParseGhostArea_HappyPath(t *testing.T) {
	g, err := ParseGhostArea([]byte(happyGhost))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if g.Layout != "IFDS_BEFORE_DATA" {
		t.Errorf("Layout = %q, want IFDS_BEFORE_DATA", g.Layout)
	}
	if g.BlockOrder != "ROW_MAJOR" {
		t.Errorf("BlockOrder = %q", g.BlockOrder)
	}
	if g.COGWSIVersion != "0.1" {
		t.Errorf("COGWSIVersion = %q, want 0.1", g.COGWSIVersion)
	}
	if g.SizeBytes != 159 {
		t.Errorf("SizeBytes = %d, want 159", g.SizeBytes)
	}
	if len(g.RawKeys) != 6 {
		t.Errorf("RawKeys count = %d, want 6", len(g.RawKeys))
	}
}

func TestParseGhostArea_PlainCOG(t *testing.T) {
	// No COG_WSI_VERSION line — represents a plain (non-WSI) COG.
	const plain = "GDAL_STRUCTURAL_METADATA_SIZE=000131 bytes\n" +
		"LAYOUT=IFDS_BEFORE_DATA\n" +
		"BLOCK_ORDER=ROW_MAJOR\n" +
		"BLOCK_LEADER=SIZE_AS_UINT4\n" +
		"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
		"KNOWN_INCOMPATIBLE_EDITION=NO\n"
	g, err := ParseGhostArea([]byte(plain))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if g.COGWSIVersion != "" {
		t.Errorf("COGWSIVersion = %q, want empty (plain COG)", g.COGWSIVersion)
	}
}

func TestParseGhostArea_UnknownKey(t *testing.T) {
	withUnknown := strings.TrimRight(happyGhost, "\n") +
		"\nFUTURE_KEY=somevalue\n"
	// Adjust size header to include the new line. For test simplicity,
	// craft a fresh ghost area with corrected size.
	const data = "GDAL_STRUCTURAL_METADATA_SIZE=000181 bytes\n" +
		"LAYOUT=IFDS_BEFORE_DATA\n" +
		"BLOCK_ORDER=ROW_MAJOR\n" +
		"BLOCK_LEADER=SIZE_AS_UINT4\n" +
		"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
		"KNOWN_INCOMPATIBLE_EDITION=NO\n" +
		"COG_WSI_VERSION=0.1\n" +
		"FUTURE_KEY=somevalue\n"
	g, err := ParseGhostArea([]byte(data))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if got := g.RawKeys["FUTURE_KEY"]; got != "somevalue" {
		t.Errorf("RawKeys[FUTURE_KEY] = %q, want somevalue", got)
	}
	// Required keys still parsed despite unknown key.
	if g.Layout != "IFDS_BEFORE_DATA" {
		t.Errorf("Layout = %q", g.Layout)
	}
	_ = withUnknown // suppress unused (kept for clarity)
}

func TestParseGhostArea_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"no size header", "LAYOUT=IFDS_BEFORE_DATA\n"},
		{"unterminated size header", "GDAL_STRUCTURAL_METADATA_SIZE=000010 bytes"},
		{"missing ' bytes' suffix", "GDAL_STRUCTURAL_METADATA_SIZE=10\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"invalid size", "GDAL_STRUCTURAL_METADATA_SIZE=NOTANUM bytes\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"declared size exceeds data",
			"GDAL_STRUCTURAL_METADATA_SIZE=999999 bytes\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"missing LAYOUT",
			"GDAL_STRUCTURAL_METADATA_SIZE=000110 bytes\n" +
				"BLOCK_ORDER=ROW_MAJOR\n" +
				"BLOCK_LEADER=SIZE_AS_UINT4\n" +
				"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
				"KNOWN_INCOMPATIBLE_EDITION=NO\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseGhostArea([]byte(tc.data))
			if err == nil {
				t.Error("want error, got nil")
				return
			}
			if !errors.Is(err, ErrGhostAreaMalformed) {
				t.Errorf("err = %v, want ErrGhostAreaMalformed", err)
			}
		})
	}
}

func TestParseCOGWSIVersion(t *testing.T) {
	for _, tc := range []struct {
		input              string
		wantMajor, wantMinor int
		wantErr            bool
	}{
		{"0.1", 0, 1, false},
		{"0.2", 0, 2, false},
		{"1.0", 1, 0, false},
		{"10.20", 10, 20, false},
		{"", 0, 0, true},
		{"abc", 0, 0, true},
		{"1", 0, 0, true},   // missing minor
		{"1.x", 0, 0, true},
		{"x.1", 0, 0, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			maj, min, err := ParseCOGWSIVersion(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil {
				if maj != tc.wantMajor || min != tc.wantMinor {
					t.Errorf("got %d.%d, want %d.%d", maj, min, tc.wantMajor, tc.wantMinor)
				}
			}
		})
	}
}
```

- [ ] **Step 4: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./internal/cog/ 2>&1 | head -3
go test -count=1 ./internal/cog/ 2>&1 | tail -10
gofmt -l internal/cog/
```

- [ ] **Step 5: Commit**

```bash
git add internal/cog/
git commit -m "$(cat <<'EOF'
feat(cog): T1 — internal/cog/ ghost-area parser

New internal/cog/ package: pure GDAL ghost-area parser (the
ASCII key-value block immediately after the TIFF header in COG /
COG-WSI files). No I/O; designed to support COG-WSI detection +
validation (v0.19 use case) and any future plain-COG awareness.

Surface:
  GhostArea struct (typed required fields + RawKeys map[string]string)
  ParseGhostArea([]byte) (GhostArea, error)
  ParseCOGWSIVersion("0.1") → (major, minor int, error)
  ErrGhostAreaMalformed sentinel

Required keys per GDAL COG spec: LAYOUT, BLOCK_ORDER, BLOCK_LEADER,
BLOCK_TRAILER, KNOWN_INCOMPATIBLE_EDITION. Optional: COG_WSI_VERSION
(present only on COG-WSI files). Unknown keys land in RawKeys for
forward-compat with spec v0.2+ extensions.

Verified against the canonical COG-WSI happy-path ghost area
(matches the bytes observed in CMU-1-Small-Region_cog-wsi.tiff
at offset 8).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — `internal/tiff` WSI tag readers

**Files:**
- Create: `internal/tiff/wsi_tags.go`, `internal/tiff/wsi_tags_test.go`

Typed accessor methods on `*tiff.Page` for the 8 WSI private tags per COG-WSI spec §5.

- [ ] **Step 1: Read `internal/tiff/page.go`** to confirm the existing accessor pattern (e.g., `ASCII(tag)`, `ScalarU32(tag)`, `Double(tag)` or similar). T2 mirrors that pattern.

```bash
grep -n "func (p \*Page)" internal/tiff/page.go | head -20
```

- [ ] **Step 2: `internal/tiff/wsi_tags.go`**

```go
package tiff

// COG-WSI private tag IDs per the v0.1 spec §5.2. These are part
// of the wsitools writer's namespace (range >= 65000); they are
// not registered TIFF tags.
const (
	TagWSIImageType     = 65080 // ASCII; every IFD
	TagWSILevelIndex    = 65081 // LONG; pyramid only
	TagWSILevelCount    = 65082 // LONG; pyramid only
	TagWSISourceFormat  = 65083 // ASCII; L0 only
	TagWSIToolsVersion  = 65084 // ASCII; L0 only
	TagWSIMPPX          = 65085 // DOUBLE; L0 only
	TagWSIMPPY          = 65086 // DOUBLE; L0 only
	TagWSIMagnification = 65087 // DOUBLE; L0 only
)

// WSIImageType returns the WSIImageType tag value (e.g., "pyramid",
// "label", "macro", "thumbnail", "overview") and a presence flag.
// Returns (empty, false) when the tag is absent — readers should
// treat absence as "this IFD doesn't carry WSI classification."
func (p *Page) WSIImageType() (string, bool) {
	return p.ASCII(TagWSIImageType)
}

// WSILevelIndex returns the 0-based pyramid level index declared by
// the IFD's WSILevelIndex tag (COG-WSI spec §5.2). Returns
// (0, false) when absent.
func (p *Page) WSILevelIndex() (uint32, bool) {
	return p.scalarU32Tag(TagWSILevelIndex)
}

// WSILevelCount returns the total pyramid level count declared by
// the IFD's WSILevelCount tag (COG-WSI spec §5.2). Returns
// (0, false) when absent.
func (p *Page) WSILevelCount() (uint32, bool) {
	return p.scalarU32Tag(TagWSILevelCount)
}

// WSISourceFormat returns the original source container identifier
// (e.g., "svs", "philips") and a presence flag. Per spec, populated
// on the L0 IFD only.
func (p *Page) WSISourceFormat() (string, bool) {
	return p.ASCII(TagWSISourceFormat)
}

// WSIToolsVersion returns the wsitools version that wrote the file.
// Per spec, populated on the L0 IFD only.
func (p *Page) WSIToolsVersion() (string, bool) {
	return p.ASCII(TagWSIToolsVersion)
}

// WSIMPPX returns the per-X-axis microns-per-pixel and a presence
// flag. Per spec, populated on the L0 IFD only.
func (p *Page) WSIMPPX() (float64, bool) {
	return p.doubleTag(TagWSIMPPX)
}

// WSIMPPY returns the per-Y-axis microns-per-pixel and a presence
// flag.
func (p *Page) WSIMPPY() (float64, bool) {
	return p.doubleTag(TagWSIMPPY)
}

// WSIMagnification returns the optical magnification (e.g., 40.0)
// and a presence flag. Per spec, populated on the L0 IFD only.
func (p *Page) WSIMagnification() (float64, bool) {
	return p.doubleTag(TagWSIMagnification)
}
```

`scalarU32Tag` and `doubleTag` are existing or new internal helpers — the implementer must verify against `internal/tiff/page.go`. If they don't exist with those names, adapt to whatever the existing API exposes (likely `ScalarU32(tag uint16) (uint32, bool)` or similar). Read page.go first; don't guess.

- [ ] **Step 3: `internal/tiff/wsi_tags_test.go`** — golden tests on synthetic Page values (use existing test helpers in the package). Cover absent-tag returns (false), each WSIImageType value, integer + double type reads.

The exact test shape depends on how `internal/tiff` tests construct `*Page` values for unit testing. Look at existing accessor tests (e.g., `TestPage_Software`) and mirror their pattern.

- [ ] **Step 4: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./internal/tiff/ 2>&1 | head -3
go test -count=1 -run TestPage_WSI ./internal/tiff/ 2>&1 | tail -10
gofmt -l internal/tiff/wsi_tags.go internal/tiff/wsi_tags_test.go
```

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/wsi_tags.go internal/tiff/wsi_tags_test.go
git commit -m "$(cat <<'EOF'
feat(tiff): T2 — internal/tiff WSI private tag readers

8 typed accessors on *tiff.Page for the COG-WSI private tag set
(spec §5.2). Tag IDs hardcoded per spec; presence-flag return
semantics match the existing internal/tiff accessor pattern.

  WSIImageType()      → string, bool   (tag 65080; ASCII)
  WSILevelIndex()     → uint32, bool   (tag 65081; LONG)
  WSILevelCount()     → uint32, bool   (tag 65082; LONG)
  WSISourceFormat()   → string, bool   (tag 65083; ASCII)
  WSIToolsVersion()   → string, bool   (tag 65084; ASCII)
  WSIMPPX()           → float64, bool  (tag 65085; DOUBLE)
  WSIMPPY()           → float64, bool  (tag 65086; DOUBLE)
  WSIMagnification()  → float64, bool  (tag 65087; DOUBLE)

Shared between formats/cogwsi (dedicated reader; T5/T6) and
formats/generictiff (WSI-tag short-circuit; T4) per spec Q3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — Integer-multiple ratio pyramid validator (Issue #5 part B)

**Files:**
- Modify: `internal/tiff/classify_pyramid.go` (lines 207-237 — the greedy chain builder)
- Modify or create: `internal/tiff/classify_pyramid_test.go`

Closes Issue #5 part B: relax the strict drift check to accept clean integer-multiple step ratios. Synthetic 4×/2×/2× chain must parse as a 4-level pyramid (currently drops to 3).

- [ ] **Step 1: Read the current `buildPyramidChain` code** at `internal/tiff/classify_pyramid.go:200-242`. Note the existing drift check at lines 228-234. Note `cfg.InterLevelTolerance` (5% default).

- [ ] **Step 2: Add the integer-multiple acceptance predicate**

In the same file (or a co-located helper file), add:

```go
// isIntegerMultipleRatio reports whether candidate ratio r matches
// any prior chain ratio scaled by a small clean integer factor
// (2, 4, 8, ...). Tolerance reuses cfg.InterLevelTolerance to
// allow encoder rounding (a "2.0×" downsample often computes
// to 2.001× or 1.999× depending on which dimension is dominant).
//
// Pre-v0.19 the validator rejected any drift > tolerance, dropping
// pyramid levels from files with mixed-ratio chains (e.g., a
// 4×/2×/2× pyramid produced by Aperio Image Library on certain
// scan sizes). v0.19 accepts these via this predicate.
func isIntegerMultipleRatio(r float64, prior []float64, tolerance float64) bool {
	for _, p := range prior {
		for _, mult := range []float64{2, 4, 8, 16} {
			for _, target := range []float64{p * mult, p / mult} {
				if target <= 0 {
					continue
				}
				if math.Abs(r-target)/target <= tolerance {
					return true
				}
			}
		}
	}
	return false
}
```

- [ ] **Step 3: Modify the greedy chain builder**

Replace lines 228-234 (the drift check block) with:

```go
if len(ratios) > 0 {
	drift := math.Abs(rW-ratios[len(ratios)-1]) / ratios[len(ratios)-1]
	if drift > cfg.InterLevelTolerance {
		// v0.19 (Issue #5 part B): accept clean integer-multiple
		// jumps as a deliberate step change (e.g., 4×/2×/2×
		// pyramid chains in Aperio/Grundium SVS routed through
		// generic-tiff). Reject only when the ratio doesn't match
		// any prior ratio scaled by a clean integer factor.
		if !isIntegerMultipleRatio(rW, ratios, cfg.InterLevelTolerance) {
			leftoverTiled = append(leftoverTiled, cand)
			continue
		}
	}
}
```

- [ ] **Step 4: Unit tests**

Add to `internal/tiff/classify_pyramid_test.go`:

```go
func TestClassifyPyramid_IntegerMultipleChain(t *testing.T) {
	// Synthetic 4×/2×/2× chain: 49152 → 12288 → 6144 → 3072 wide.
	// Pre-v0.19 the second step (49152→12288 = 4×) sets the chain
	// ratio at 4×; the third step (12288→6144 = 2×) drifts 50% from
	// 4× and is rejected. v0.19 accepts via integer-multiple match
	// (2× is a clean half of 4×).
	ifds := []PyramidLevelInfo{
		{Width: 49152, Height: 32768, ifdIndex: 0, /* IsTiled+Compression set per existing test helpers */},
		{Width: 12288, Height: 8192, ifdIndex: 1},
		{Width: 6144, Height: 4096, ifdIndex: 2},
		{Width: 3072, Height: 2048, ifdIndex: 3},
	}
	// ... build a synthetic tiff.File with these IFDs (use existing
	// test helpers; mirror how TestClassifyPyramid_* tests construct
	// inputs). Then call ClassifyPyramid and assert len(pyramid)==4.
}

func TestIsIntegerMultipleRatio(t *testing.T) {
	for _, tc := range []struct {
		name      string
		r         float64
		prior     []float64
		tolerance float64
		want      bool
	}{
		{"2x is half of 4x prior", 2.0, []float64{4.0}, 0.05, true},
		{"4x is double of 2x prior", 4.0, []float64{2.0}, 0.05, true},
		{"3x is not integer-multiple of 2x", 3.0, []float64{2.0}, 0.05, false},
		{"2.001x within tolerance of 2x prior", 2.001, []float64{2.0}, 0.05, true},
		{"empty prior", 4.0, nil, 0.05, false},
		{"8x with mixed prior", 8.0, []float64{2.0, 4.0}, 0.05, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := isIntegerMultipleRatio(tc.r, tc.prior, tc.tolerance)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
```

The synthetic-IFD test (`TestClassifyPyramid_IntegerMultipleChain`) requires the existing test helper patterns from `classify_pyramid_test.go`. Read that file to understand how PyramidLevelInfo + tiff.File are constructed for unit tests.

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go test -count=1 -run "TestClassifyPyramid|TestIsInteger" ./internal/tiff/ 2>&1 | tail -10
gofmt -l internal/tiff/
```

Expected: existing TestClassifyPyramid_* tests still pass (none of the existing fixtures exercises integer-multiple ratios — they should be unaffected). New tests pass.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(tiff): T3 — integer-multiple ratio pyramid validator (Issue #5 part B)

internal/tiff/classify_pyramid.go::buildPyramidChain extended to
accept clean integer-multiple step ratios. Pre-v0.19 the strict
drift check (InterLevelTolerance default 5%) rejected mixed-ratio
chains like Aperio/Grundium SVS 4×/2×/2× pyramids when routed
through generic-tiff.

New isIntegerMultipleRatio predicate: a candidate ratio r is
accepted when it matches any prior chain ratio scaled by 2, 4, 8,
or 16 (or 1/2, 1/4, 1/8, 1/16) within tolerance. The strict-match
path remains the fast path; integer-multiple acceptance is the
fallback.

Standalone benefit (independent of COG-WSI / Issue #5 part A):
generic-TIFF now correctly handles any mixed-ratio pyramid the
SVS / NDPI / Philips / etc. format-specific readers already handle.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — Generic-TIFF WSI-tag short-circuit (Issue #5 part A)

**Files:**
- Modify: `formats/generictiff/classifier.go` (ClassifyAssociated)
- Modify: `formats/generictiff/tiler.go` (or wherever the pyramid build/classify entry sits — read to confirm)
- Modify: `formats/generictiff/classifier_test.go` (or equivalent)

Closes Issue #5 part A: when an IFD carries `WSIImageType`, use it as authoritative for classification — skip the dimension/aspect heuristics in `ClassifyAssociated`. Similarly, when ALL pyramid candidate IFDs carry `WSIImageType=pyramid` + `WSILevelIndex`, use that for ordering.

- [ ] **Step 1: Read existing ClassifyAssociated** (formats/generictiff/classifier.go:86-119) + the pyramid-build path that calls it (likely in tiler.go or formats/generictiff/all.go — search for `ClassifyAssociated(` callers).

- [ ] **Step 2: Modify ClassifyAssociated to honor WSIImageType**

Update the function signature to accept the tiff.Page (so the implementer can read the WSI tag):

```go
// ClassifyAssociated assigns a Type() value to an IFD that the
// pyramid validator routed to "associated" (i.e., not a pyramid
// level). When the IFD carries an explicit WSIImageType tag
// (COG-WSI spec §5.2 / v0.19), that value is authoritative and
// the heuristic path is skipped. Otherwise, dimension/aspect
// heuristics apply (the pre-v0.19 path).
func ClassifyAssociated(page *tiff.Page, ifd, baseline tiff.PyramidLevelInfo) string {
	// v0.19: WSI-tag short-circuit. Honors COG-WSI's authoritative
	// classification when present.
	if wt, ok := page.WSIImageType(); ok {
		switch wt {
		case "label":
			return TypeLabel
		case "macro", "overview":
			return TypeOverview // v0.15 canonical: macro+overview→overview
		case "thumbnail":
			return TypeThumbnail
		case "pyramid":
			// Should not reach here — pyramid IFDs aren't routed to
			// ClassifyAssociated. Defensive fallthrough to associated.
			return TypeAssociated
		}
	}

	// Pre-v0.19 heuristic path unchanged.
	w, h := ifd.Width, ifd.Height
	// ... existing body ...
}
```

NOTE: changing the signature is a private-package call site change. Audit all callers (`grep -rn "ClassifyAssociated(" formats/generictiff/`); update each to pass the new `page` argument. Tests likewise.

If the call site signature change is invasive, an alternative is a wrapper:

```go
// ClassifyAssociatedFromPage is the WSI-tag-aware entry point used
// by v0.19+ generic-TIFF. Delegates to the legacy ClassifyAssociated
// when no WSI tag is present.
func ClassifyAssociatedFromPage(page *tiff.Page, ifd, baseline tiff.PyramidLevelInfo) string {
	if wt, ok := page.WSIImageType(); ok {
		// ... WSI short-circuit ...
	}
	return ClassifyAssociated(ifd, baseline)
}
```

Pick whichever approach minimizes diff surface; both are acceptable.

- [ ] **Step 3: Pyramid build path — honor WSILevelIndex when all candidates carry it**

In the generic-TIFF pyramid construction (read `formats/generictiff/tiler.go` to find the entry point — likely a `buildPyramid` or similar function that processes the `ClassifyPyramidResult`):

```go
// v0.19: when ALL candidate pyramid IFDs carry WSILevelIndex,
// short-circuit the dimension-ratio drift check and use the
// declared level indices directly.
allHaveWSITag := true
levels := make(map[uint32]*tiff.Page)
for _, p := range candidatePages {
	idx, ok := p.WSILevelIndex()
	if !ok {
		allHaveWSITag = false
		break
	}
	levels[idx] = p
}
if allHaveWSITag {
	// Build pyramid from WSILevelIndex ordering; trust the writer.
	// ...
} else {
	// Fall back to existing dimension-ratio chain builder.
	// ...
}
```

Adapt to the actual generic-TIFF code structure. The key behavior change: when WSI tags are present, the validator trusts them; otherwise the existing path runs unchanged.

- [ ] **Step 4: Tests**

- Existing TestGenericGeometry tests must continue to pass (none of those fixtures carry WSI tags; they exercise the heuristic path).
- New test: force-route a small COG-WSI fixture (e.g., `CMU-1-Small-Region_cog-wsi.tiff`) through `generic-tiff` by bypassing the cogwsi factory (use a direct call to generictiff's Open with the file). Confirm pyramid levels + associated images classify correctly via WSI tags.

The "force-route" test pattern depends on internal vs external test packages — read existing tests to see how the generic-tiff entry point is callable.

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/generictiff/ 2>&1 | tail -10
gofmt -l formats/generictiff/
```

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(generictiff): T4 — WSI-tag-aware classification (Issue #5 part A)

When an IFD carries the WSIImageType private tag (COG-WSI spec §5.2 /
opentile-go v0.19), the generic-tiff classifier honors it as
authoritative instead of falling through to dimension/aspect
heuristics:

  WSIImageType=label      → TypeLabel
  WSIImageType=macro      → TypeOverview (v0.15 canonical naming)
  WSIImageType=overview   → TypeOverview
  WSIImageType=thumbnail  → TypeThumbnail
  WSIImageType=pyramid    → routed to pyramid build (not Associated)

Pyramid build similarly: when ALL candidate IFDs carry
WSILevelIndex, the dimension-ratio drift check is short-circuited
and the writer's declared ordering is trusted.

When WSI tags are absent (pre-v0.19 generic TIFFs, vips-converted
files, etc.), the existing heuristic path runs unchanged. No
regression for existing generic-tiff fixtures.

Combined with T3 (integer-multiple ratio acceptance), Issue #5
is closed: generic-tiff correctly reads any COG-WSI file's pyramid
+ associated structure.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — `formats/cogwsi/` skeleton + factory + register

**Files:**
- Modify: `tiler.go` (add `FormatCOGWSI = "cog-wsi"`)
- Create: `formats/cogwsi/doc.go`, `formats/cogwsi/factory.go`, `formats/cogwsi/tiler.go`, `formats/cogwsi/tiler_test.go`
- Modify: `formats/all/all.go` (register cogwsi BEFORE generictiff)

T5 lands a working but minimal cogwsi Tiler: Open() succeeds on COG-WSI fixtures via ghost-area dispatch; Format() returns `"cog-wsi"`; Levels/Images/Associated/Metadata return placeholders (T6 fills them).

- [ ] **Step 1: Add `opentile.FormatCOGWSI`** in `tiler.go`:

```go
// FormatCOGWSI identifies a Cloud Optimized GeoTIFF for WSI file —
// a strict extension of GDAL Cloud Optimized GeoTIFF carrying
// WSI-specific private tags + ghost-area marker. Spec at
// docs/specs/2026-05-20-cog-wsi-format.md. Added in v0.19.
FormatCOGWSI Format = "cog-wsi"
```

- [ ] **Step 2: `formats/cogwsi/doc.go`**

```go
// Package cogwsi reads Cloud Optimized GeoTIFF for WSI (COG-WSI)
// files — a strict COG extension produced by the wsitools
// transcoder (cornish/wsitools). COG-WSI carries WSI-specific
// private tags (65080-65087) for level/associated classification
// and metadata, plus a COG_WSI_VERSION marker in the ghost area.
//
// Detection: ghost-area parsing via internal/cog. A file is
// COG-WSI iff its ghost area contains a COG_WSI_VERSION=<x.y> key.
//
// Reading: pyramid + associated extraction delegates to generic-
// TIFF's WSI-tag-aware path (closes Issue #5 + #6).
//
// Spec validation: open-time conformance check via
// ErrNotConformantCOGWSI sentinel.
//
// Spec: docs/specs/2026-05-20-cog-wsi-format.md.
package cogwsi
```

- [ ] **Step 3: `formats/cogwsi/factory.go`**

```go
package cogwsi

import (
	"encoding/binary"
	"errors"
	"io"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/cog"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Factory implements opentile.FormatFactory for COG-WSI files.
type Factory struct{ opentile.RawUnsupported }

func New() *Factory { return &Factory{} }

func (f *Factory) Format() opentile.Format { return opentile.FormatCOGWSI }

// Supports is the TIFF-path entry point. Reads the ghost area
// (after the TIFF header) and returns true iff the COG_WSI_VERSION
// key is present.
func (f *Factory) Supports(tf *tiff.File) bool {
	ghost, err := readGhostArea(tf)
	if err != nil {
		return false
	}
	return ghost.COGWSIVersion != ""
}

// Open parses a COG-WSI file. Validates spec conformance and
// returns ErrNotConformantCOGWSI on violations.
func (f *Factory) Open(tf *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	return openCOGWSI(tf, cfg)
}

// readGhostArea reads the ghost-area bytes from the file. The
// ghost area starts at offset 8 for classic TIFF and offset 16
// for BigTIFF (after the TIFF header proper).
//
// Reads up to GhostAreaMaxBytes; if the declared SIZE in the ghost
// area exceeds this cap, returns ErrGhostAreaMalformed.
func readGhostArea(tf *tiff.File) (cog.GhostArea, error) {
	// Implementer must read the TIFF header offset from the tf.File
	// API. The exact accessor depends on the internal/tiff surface
	// (e.g., tf.IsBigTIFF()). 8 for classic, 16 for BigTIFF.
	// ...
}

const GhostAreaMaxBytes = 16384 // generous upper bound; ghost areas are typically <200 bytes
```

The implementer fills in `readGhostArea` per the `internal/tiff` accessor surface. Look at how `formats/leicascn` reads TIFF-level structure for reference. The `tf.IsBigTIFF()` (or equivalent) method tells you whether the ghost area starts at offset 8 or 16.

- [ ] **Step 4: `formats/cogwsi/tiler.go` skeleton**

```go
package cogwsi

import (
	"errors"
	"fmt"
	"io"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/cog"
	"github.com/cornish/opentile-go/internal/tiff"
)

// ErrNotConformantCOGWSI is returned by Open when the file claims
// to be COG-WSI (via the COG_WSI_VERSION ghost-area key) but
// violates the spec.
var ErrNotConformantCOGWSI = errors.New("cogwsi: file is not spec-conformant")

// Tiler is the COG-WSI format implementation of opentile.Tiler.
type Tiler struct {
	tf    *tiff.File
	ghost cog.GhostArea
	cfg   *opentile.Config
	// T6: pyramid + associated + metadata
}

func openCOGWSI(tf *tiff.File, cfg *opentile.Config) (*Tiler, error) {
	ghost, err := readGhostArea(tf)
	if err != nil {
		return nil, fmt.Errorf("cogwsi: ghost-area read: %w", err)
	}
	if ghost.COGWSIVersion == "" {
		// Shouldn't reach here — Supports() returned true. Defensive.
		return nil, fmt.Errorf("%w: ghost area lacks COG_WSI_VERSION", ErrNotConformantCOGWSI)
	}

	major, _, err := cog.ParseCOGWSIVersion(ghost.COGWSIVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConformantCOGWSI, err)
	}
	if major > 0 {
		return nil, fmt.Errorf("%w: unsupported major version %d (reader supports 0.x)",
			ErrNotConformantCOGWSI, major)
	}

	t := &Tiler{
		tf:    tf,
		ghost: ghost,
		cfg:   cfg,
	}
	// T6: spec validation + pyramid + associated + metadata wiring.
	return t, nil
}

func (t *Tiler) Format() opentile.Format { return opentile.FormatCOGWSI }
func (t *Tiler) Close() error { t.tf = nil; return nil }

// T6 will replace these with real implementations.
func (t *Tiler) Levels() []opentile.Level                  { return nil }
func (t *Tiler) Level(i int) (opentile.Level, error)      { return nil, opentile.ErrLevelOutOfRange }
func (t *Tiler) Images() []opentile.Image                  { return nil }
func (t *Tiler) Associated() []opentile.AssociatedImage    { return nil }
func (t *Tiler) Metadata() opentile.Metadata              { return opentile.Metadata{} }
func (t *Tiler) ICCProfile() []byte                        { return nil }
func (t *Tiler) WarmLevel(i int) error                    { return opentile.ErrLevelOutOfRange }
```

NOTE: the Tiler interface has additional methods (TilePrefix etc. on Level, plus any v0.17+ additions). T5's scope is just the skeleton; T6 fills in real behavior. Verify the full Tiler interface in tiler.go before committing.

- [ ] **Step 5: `formats/cogwsi/tiler_test.go`** — smoke test on the small fixture:

```go
package cogwsi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

func TestOpen_CMU1SmallRegion(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()
	if got := tlr.Format(); got != opentile.FormatCOGWSI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatCOGWSI)
	}
}
```

External test package (`cogwsi_test`) to avoid import cycle with formats/all.

- [ ] **Step 6: Register in `formats/all/all.go` — BEFORE generictiff**

Read formats/all/all.go to confirm the current order. Insert cogwsi registration BEFORE generictiff (mirrors v0.11 leicascn-before-generictiff precedent).

```go
opentile.Register(cogwsi.New())
// ... existing registrations ...
opentile.Register(generictiff.New())
```

- [ ] **Step 7: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/cogwsi/ 2>&1 | tail -10
gofmt -l formats/cogwsi/ formats/all/ tiler.go
```

Expected: smoke test passes; CMU-1-Small-Region_cog-wsi.tiff opens via `opentile.OpenFile` and reports `Format() == "cog-wsi"`. Levels/Images return placeholders (T6 fills in).

- [ ] **Step 8: Commit**

```bash
git add -u formats/cogwsi/
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.19): T5 — opentile.FormatCOGWSI + formats/cogwsi/ skeleton

New public surface:
  opentile.FormatCOGWSI = "cog-wsi"      (Tiler.Format())
  formats/cogwsi/ package + Factory       (registered via formats/all)
  cogwsi.ErrNotConformantCOGWSI sentinel  (returned on spec violations)

Tiler skeleton:
  - Supports() reads ghost area via internal/cog; returns true iff
    COG_WSI_VERSION key present
  - Open() parses ghost area; validates COG-WSI major version (0.x
    supported; 1.x+ rejected as unsupported); placeholder Levels/
    Images/Associated/Metadata until T6
  - Factory registered in formats/all BEFORE generictiff
    (format-specific detector wins over the catch-all)

T5 smoke test verifies CMU-1-Small-Region_cog-wsi.tiff opens via
opentile.OpenFile() and reports Format() == "cog-wsi".

T6 fills in pyramid + associated + metadata; T7 wires the full
fixture set.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Tiler + spec validation + metadata

**Files:**
- Modify: `formats/cogwsi/tiler.go` (pyramid + associated + metadata wiring)
- Create: `formats/cogwsi/classifier.go` (or inline in tiler.go) for WSI-tag-driven IFD dispatch
- Create: `formats/cogwsi/metadata.go` (WSI metadata tags → cross-format + Properties)
- Create: `formats/cogwsi/validation.go` (spec-conformance checks)
- Modify: `formats/cogwsi/tiler_test.go` (integration tests on CMU-1-Small-Region + spec-violation tests)

This is the largest task. Implements the actual COG-WSI reading + spec validation per spec §3-§6.

- [ ] **Step 1: Spec validation** — `formats/cogwsi/validation.go`

Add helpers that check each spec rule. Return `ErrNotConformantCOGWSI` on any violation. Called from openCOGWSI after the ghost-area parse succeeds.

Required checks (spec §3-§6):
- Ghost area required keys present (already done by `cog.ParseGhostArea`; just check values are as expected: `LAYOUT=IFDS_BEFORE_DATA`, `BLOCK_ORDER=ROW_MAJOR`, `BLOCK_LEADER=SIZE_AS_UINT4`, `BLOCK_TRAILER=LAST_4_BYTES_REPEATED`, `KNOWN_INCOMPATIBLE_EDITION=NO`)
- Every IFD with `NewSubfileType=0` or `=1` carries `WSIImageType`
- Pyramid IFDs (`WSIImageType=pyramid`) are tiled (no strips)
- L0 carries `WSILevelIndex=0` and `WSILevelCount=N` where N matches the count of pyramid IFDs
- Pyramid IFDs ordered by decreasing resolution
- Associated IFDs (`WSIImageType ∈ {label, macro, thumbnail, overview}`) appear AFTER all pyramid IFDs

The plan provides the rule set; the implementer codes the assertions. Each spec violation should produce a specific descriptive error wrapped with `ErrNotConformantCOGWSI` (so callers can `errors.Is(err, ErrNotConformantCOGWSI)`).

- [ ] **Step 2: Pyramid + associated build** — `formats/cogwsi/classifier.go`

Walk the IFD chain; partition by `WSIImageType`:
- `pyramid` IFDs → pyramid levels (sorted by `WSILevelIndex`)
- `label/macro/thumbnail/overview` IFDs → associated images (with Type() per v0.15 canonical naming — `"macro"` from non-IFE source maps to `Type("overview")`)

Reuse `formats/generictiff`'s tile-fetch infrastructure where natural (the actual tile bytes path is identical; only classification differs).

- [ ] **Step 3: Metadata** — `formats/cogwsi/metadata.go`

Populate cross-format `Metadata`:
- `MicronsPerPixelX` from `WSIMPPX` (tag 65085)
- `MicronsPerPixelY` from `WSIMPPY` (tag 65086)
- `Magnification` from `WSIMagnification` (tag 65087)
- `SetMPPSymmetric()` after setting X/Y
- `ScannerManufacturer` from `Make` (tag 271; standard TIFF)
- `ScannerModel` from `Model` (tag 272)
- `ScannerSoftware` from `Software` (tag 305)
- `AcquisitionDateTime` from `DateTime` (tag 306)
- `ImageDescription` from `ImageDescription` (tag 270; per spec, contains the wsitools provenance string)
- `Properties["cog-wsi.source-format"]` from `WSISourceFormat` (tag 65083)
- `Properties["cog-wsi.wsitools-version"]` from `WSIToolsVersion` (tag 65084)
- `Properties["cog-wsi.spec-version"]` from `ghost.COGWSIVersion` (e.g., `"0.1"`)

Per Q-Decisions, mirror v0.17's hybrid pattern (typed for canonical; Properties for extensions).

Note: per spec §5.2, the COG-WSI writer preserves the source format's standard TIFF tags (Make/Model/Software/DateTime/ImageDescription). So scanner attribution should match the underlying source (e.g., Aperio for SVS-derived COG-WSI; Grundium for Grundium-SVS-derived).

- [ ] **Step 4: Level + Tile** — wire pyramid IFDs to `opentile.Level` instances

The tile-byte path is generic-TIFF-equivalent: each pyramid IFD is tiled; tiles are fetched via the existing `internal/tiff` tile-fetch path. Reuse the generictiff level shape where natural; cogwsi's distinction is detection + validation + metadata, not the hot path.

If generic-TIFF's level type is unexported, either:
- Export it (small refactor, additive)
- Or re-implement cogwsi-specific level type that wraps `*tiff.Page` directly (more code; cleaner separation)

The implementer picks based on what's cheaper. Note v0.13 splice-prefix path applies when shared JPEGTables are present (spec §5: writer preserves abbreviated JPEG).

- [ ] **Step 5: Integration + spec-violation tests**

`formats/cogwsi/tiler_test.go` extensions:

- TestTiler_CMU1SmallRegion_FullPyramid — open the small fixture; verify all pyramid levels enumerate; first tile bytes start with `FF D8 FF` (JPEG SOI); associated set matches expectations.
- TestTiler_Metadata — verify cross-format Metadata populates from WSIMPP*/WSIMag tags + Properties[cog-wsi.*] populated from WSISourceFormat/WSIToolsVersion.

Spec-violation negative cases — synthetic malformed COG-WSI bytes. The implementer constructs minimal byte payloads triggering each violation; assert `errors.Is(err, ErrNotConformantCOGWSI)`.

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/cogwsi/ 2>&1 | tail -15
gofmt -l formats/cogwsi/
```

Expected: full pyramid + metadata reads on CMU-1-Small-Region_cog-wsi.tiff; all spec-violation negative tests return ErrNotConformantCOGWSI.

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.19): T6 — formats/cogwsi/ Tiler + spec validation + metadata

Tiler reads pyramid + associated images from COG-WSI files using
the WSIImageType tag as authoritative dispatch (closes Issue #6).
Reuses generic-TIFF's tile-fetch infrastructure for the hot path
(byte passthrough; v0.9 mmap-aliased; v0.13 splice-prefix path
when shared JPEGTables present).

Spec validation at open time (ErrNotConformantCOGWSI):
  - Ghost area required keys (LAYOUT, BLOCK_ORDER, BLOCK_LEADER,
    BLOCK_TRAILER, KNOWN_INCOMPATIBLE_EDITION) present with
    expected values
  - Every classifiable IFD carries WSIImageType
  - Pyramid IFDs (WSIImageType=pyramid) are tiled (no strips)
  - WSILevelIndex contiguous from 0 to WSILevelCount-1
  - IFD ordering: pyramid → associated (associated after all
    pyramid IFDs per spec §6)

Cross-format Metadata populated from WSI private tags + standard
TIFF tags:
  WSIMPPX/Y      → MicronsPerPixelX/Y + SetMPPSymmetric
  WSIMagnification → Magnification
  Make/Model/Software/DateTime → cross-format (preserves source
    format's scanner attribution per spec)
  ImageDescription → cross-format (wsitools provenance string)
  WSISourceFormat  → Properties[cog-wsi.source-format]
  WSIToolsVersion  → Properties[cog-wsi.wsitools-version]
  ghost COG_WSI_VERSION → Properties[cog-wsi.spec-version]

Associated-image Type() per v0.15 canonical:
  label → "label", thumbnail → "thumbnail",
  macro / overview → "overview" (v0.15: macro from non-IFE
    source maps to overview)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T7 — Fixtures + tests

**Files:**
- Modify: `tests/integration_test.go` (slideCandidates)
- Create: `tests/parity/cogwsi_geometry_test.go`
- Generate: 10 `tests/fixtures/*_cog-wsi.tiff.json` files
- Possibly: cross-fixture parity gate (separate test file)

- [ ] **Step 1: Probe each cog-wsi fixture for geometry**

Build a small inline Go program that opens each cog-wsi fixture and dumps per-level Size / TileSize / Grid / Compression + associated counts. Capture the output for each of the 10 fixtures.

Expected per-fixture geometry should match the source format's geometry (the COG-WSI writer preserves pyramid + associated bit-exact per spec). For example, `CMU-1-Small-Region_cog-wsi.tiff` should report the same L0 dims (2220×2967) and per-level pyramid as `CMU-1-Small-Region.svs` reports through the SVS reader.

- [ ] **Step 2: Wire fixtures into slideCandidates**

Edit `tests/integration_test.go`. Find `slideCandidates`. Append after the SZI entries:

```go
// COG-WSI fixtures (v0.19): wsitools-converted from each source
// format. Geometry matches the original; tile bytes match where
// the COG-WSI writer preserved them bit-exact per spec.
"CMU-1-Small-Region_cog-wsi.tiff",
"CMU-1_cog-wsi.tiff",
"JP2K-33003-1_cog-wsi.tiff",
"scan_617_cog-wsi.tiff",
"scan_620_cog-wsi.tiff",
"svs_40x_bigtiff_cog-wsi.tiff",
"Leica-1_cog-wsi.tiff",
"Philips-1_cog-wsi.tiff",
"Ventana-1_cog-wsi.tiff",
"cervix_2x_jpeg_cog-wsi.tiff",
```

Verify the `resolveSlide` helper handles the `cog-wsi/` subdirectory (extend if needed).

- [ ] **Step 3: Create `tests/parity/cogwsi_geometry_test.go`**

Mirror the existing `tests/parity/szi_geometry_test.go` shape. Read it first to confirm exact struct field names + test runner pattern.

Per-fixture rows pin per-level Size / TileSize / Grid / Compression + associated-image Type / Compression / ByteCount. Use the probed values from Step 1.

- [ ] **Step 4: Generate per-tile SHA fixtures**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run TestGenerateFixtures -generate -v 2>&1 | tail -30
```

Expected: 10 new JSON files at `tests/fixtures/*_cog-wsi.tiff.json`. The large fixtures (cervix, svs_40x_bigtiff) are sampled per the 5 MB cap.

- [ ] **Step 5: Cross-fixture parity gate (new test)**

Create or extend a parity test that for each `<source>` / `<source>_cog-wsi.tiff` pair, compares:
- L0 dims (must match)
- Per-level pyramid structure (must match)
- Per-tile bytes on sampled positions (should match where the COG-WSI writer preserved bit-exact)

The "should match" guard is intentional — there may be format-specific edge cases where the writer reformats (e.g., abbreviated-JPEG vs standalone). Document any divergences observed; flag in CHANGELOG if non-trivial.

- [ ] **Step 6: Verify full suite**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -run TestCOGWSIGeometry ./tests/parity/ 2>&1 | tail -10
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 -run "TestSlideParity/.*cog-wsi" ./tests/ 2>&1 | tail -10
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: TestCOGWSIGeometry green; cog-wsi TestSlideParity entries green; full module green. TestSlideParity total 40 fixtures.

- [ ] **Step 7: Commit**

```bash
git add -u tests/integration_test.go tests/parity/cogwsi_geometry_test.go tests/fixtures/*_cog-wsi.tiff.json
git commit -m "$(cat <<'EOF'
test(v0.19): T7 — wire 10 COG-WSI fixtures into TestSlideParity (30 → 40)

10 fixtures cover every source format opentile-go reads:
  CMU-1-Small-Region_cog-wsi.tiff  (SVS Aperio canonical, 1.9 MB)
  CMU-1_cog-wsi.tiff                (SVS Aperio canonical, 185 MB)
  JP2K-33003-1_cog-wsi.tiff         (SVS Aperio JP2K, 64 MB)
  scan_617_cog-wsi.tiff             (SVS Grundium, 330 MB)
  scan_620_cog-wsi.tiff             (SVS Grundium, 270 MB)
  svs_40x_bigtiff_cog-wsi.tiff      (SVS Grundium BigTIFF, 4.8 GB)
  Leica-1_cog-wsi.tiff              (OME-TIFF, 226 MB)
  Philips-1_cog-wsi.tiff            (Philips TIFF, 331 MB)
  Ventana-1_cog-wsi.tiff            (BIF, 225 MB)
  cervix_2x_jpeg_cog-wsi.tiff       (IFE, 2.1 GB)

Per-fixture geometry pinned in tests/parity/cogwsi_geometry_test.go.
Per-tile SHA fixtures generated (sampled per 5 MB cap on large
fixtures).

Cross-fixture parity gate: each <source>_cog-wsi.tiff vs the
original <source> file confirms the writer preserves bit-exact
geometry + tile bytes per spec.

TestSlideParity total: 40 fixtures (was 30).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T8 — Docs + ship

**Files:**
- Create: `docs/formats/cogwsi.md`
- Modify: `README.md` (Supported-formats row)
- Modify: `docs/deferred.md` (§8m retirement + R21 fully retired in §1 + remove R21 from §11)
- Modify: `CHANGELOG.md` ([0.19.0])
- Modify: `CLAUDE.md` (milestone bump)

- [ ] **Step 1: `docs/formats/cogwsi.md`** — new format doc

Mirror existing format docs (svs.md / szi.md). Cover:

- Format origin (wsitools converter; spec v0.1 at `docs/specs/`)
- License + spec authorship
- Architecture (COG + WSI extensions: private tags 65080-87 + COG_WSI_VERSION ghost-area marker + spec-defined IFD ordering)
- Supported features
- Tile semantics + Edge tile semantics (TIFF tile model; padded edge tiles same as other TIFF-based formats — consumer clips per the cross-format formula)
- Associated images: label / overview (from macro or overview WSIImageType) / thumbnail per v0.15 canonical
- Metadata mapping (WSIMPP* / WSIMagnification → cross-format; cog-wsi.* Properties)
- Spec validation (ErrNotConformantCOGWSI)
- What's not supported (writing; multi-channel / multi-Z; standalone CLI)
- References (spec, wsitools, GDAL COG, OGC 21-026)

- [ ] **Step 2: README.md Supported-formats table**

Insert COG-WSI row (probably after SZI):

```
| **COG-WSI** | `.tiff` | strict GDAL Cloud Optimized GeoTIFF + WSI private tags (65080-87) + COG_WSI_VERSION ghost-area marker | label, overview (from `macro` or `overview` WSIImageType), thumbnail | source-format preserving (JPEG, JP2K, LZW, ...) | per-fixture geometry pin + cross-fixture parity vs source format + ErrNotConformantCOGWSI spec validation | [docs/formats/cogwsi.md](./docs/formats/cogwsi.md) |
```

- [ ] **Step 3: `docs/deferred.md`**

- Insert §8m BEFORE §8l (newest-first ordering).
- §8m content: "Retired in v0.19" — list items shipped (closes #5 + #6 + R21 fully); architecture invariants preserved; v0.19 lessons.
- §1: mark R21 fully retired in the Status column: `✅ retired (COG-WSI portion shipped in v0.19; plain COG deemed permanently YAGNI — opentile-go is WSI-domain, geospatial COG isn't our domain)`.
- §11: remove the active R21 row entirely (no longer parked; permanently retired).
- §11: add note about Issue #5 + #6 being closed by v0.19.

- [ ] **Step 4: `CHANGELOG.md` [0.19.0]**

Insert before [0.18.0]. Use 2026-05-20 date (or today's actual date if shipping later).

```markdown
## [0.19.0] — 2026-05-20

COG-WSI support — closes user's two GH issues (#5 + #6). New
`formats/cogwsi/` package + `internal/cog/` ghost-area parser +
WSI private tag readers in `internal/tiff`. Extends `formats/
generictiff/` to honor WSI tags as authoritative + accept clean
integer-multiple pyramid ratios (Issue #5 standalone benefit).

### Added

- New `opentile.FormatCOGWSI = "cog-wsi"` enum value.
- New `formats/cogwsi/` reader with ghost-area dispatch + spec
  validation + canonical metadata via WSI private tags.
- New `internal/cog/` package: GDAL ghost-area parser (designed
  for COG-WSI's primary use + plain-COG forward-compat).
- New `internal/tiff` WSI private tag readers (tag IDs 65080-65087
  per COG-WSI spec §5.2).
- `formats/generictiff/` extended: WSIImageType-aware
  classification (Issue #5 part A) + integer-multiple pyramid
  ratio acceptance (Issue #5 part B).
- 10 new test fixtures wired into TestSlideParity
  (30 → 40 fixtures): wsitools-converted from every source
  format opentile-go reads.
- `tests/parity/cogwsi_geometry_test.go` per-fixture geometry pin.
- Cross-fixture parity gate: each `<source>_cog-wsi.tiff` vs the
  original `<source>` confirms writer preserves bit-exact
  geometry + tile bytes per spec.

### Changed

- `internal/tiff/classify_pyramid.go::buildPyramidChain` now
  accepts clean integer-multiple step ratios (2×, 4×, 8×, …).
  Pre-v0.19 the strict drift check rejected mixed-ratio chains.
- `formats/generictiff/classifier.go::ClassifyAssociated` signature
  extended to accept `*tiff.Page` for WSI tag dispatch.
- `formats/all/all.go` registers `cogwsi.Factory` BEFORE
  `generictiff.Factory` (format-specific detector wins).

### Removed / retired

- R21 (general COG first-class support) — **fully retired**.
  COG-WSI shipped in v0.19 covers the WSI-context demand; general
  COG awareness is permanently YAGNI for opentile-go (we're WSI-
  domain, not geospatial). Generic COG files continue to read via
  `generic-tiff` as structurally-valid pyramid TIFFs.

### Notes

- **Spec-validation strictness:** COG-WSI files that fail
  conformance return `cogwsi.ErrNotConformantCOGWSI`. The spec is
  the contract; we don't bend.
- **Cross-format parity:** the writer (user's wsitools) and our
  reader agree on byte-passthrough semantics — confirmed by the
  cross-fixture parity gate across all 10 fixtures.
- **v0.18 SVS writer detection** carries through COG-WSI: when a
  COG-WSI file's source was Grundium SVS, `ScannerManufacturer`
  reports "Grundium" (via the preserved Make/Model TIFF tags).
- v1.0 cut still pending.
- cgo footprint unchanged.
```

- [ ] **Step 5: `CLAUDE.md` milestone bump**

Replace `## Current milestone — v0.18` block with v0.19 (similar template to prior milestones). Demote v0.18 to "Previous milestone."

- [ ] **Step 6: Final pre-commit verification gate**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: vet clean, gofmt clean (or only pre-existing drift), every package green, TestSlideParity 40 fixtures green.

- [ ] **Step 7: Commit**

```bash
git add docs/formats/cogwsi.md README.md docs/deferred.md CHANGELOG.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs(v0.19): T8 — cogwsi.md + README + deferred §8m + CHANGELOG + CLAUDE.md

docs/formats/cogwsi.md: new — COG-WSI architecture + spec ref +
metadata mapping + spec validation + edge tile semantics + limits.

README.md: new Supported-formats row for COG-WSI.

docs/deferred.md:
  §8m new — Retired in v0.19: closes issues #5 + #6; R21 fully
    retired (plain COG = permanently YAGNI per v0.19 brainstorm
    seal — opentile-go is WSI-domain).
  §1: R21 marked retired in Status column.
  §11: R21 row removed (no longer parked).

CHANGELOG.md [0.19.0]: explicit Added block (FormatCOGWSI,
formats/cogwsi, internal/cog, internal/tiff WSI tag readers,
generic-TIFF WSI-tag awareness + integer-multiple ratio relax, 10
fixtures, TestSlideParity 40); Changed (classifier signature +
formats/all dispatch order); Removed (R21 retired).

CLAUDE.md: bump Current milestone v0.18 → v0.19.

End of milestone; v0.19 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (#5 generic-tiff WSI awareness + ratio relax) → T3 + T4.
- §1.2 (#6 cogwsi package) → T5 + T6.
- §1.3 (internal/cog ghost-area parser) → T1.
- §1.4 (internal/tiff WSI tag readers) → T2.
- §3 (Q-decisions) → reflected throughout.
- §4 (fixtures) → T7.
- §5 (test strategy) → T1 unit tests + T2 unit tests + T3 unit tests + T4 force-route integration + T6 spec-validation tests + T7 cross-fixture parity gate.
- §6 (architecture) → T1 + T5 follow the package layout sketch.
- §7 (plan outline) → matches.
- §8 (verification gates) → T8 step 6.
- §9 (R21 fully retired) → T8 step 3.

**Placeholder scan:** T3 step 2 + T6 steps 1-4 sketch the code without providing complete bodies — the implementer adapts to the existing `internal/tiff` and `formats/generictiff` surfaces (which the plan doesn't pre-export). This is intentional: the work is "wire up real APIs" rather than "type these literal blocks." T5 step 3 has a placeholder `readGhostArea` — implementer fills based on tiff.File API.

**Type consistency:** `Tiler` + `ErrNotConformantCOGWSI` + `cog.GhostArea` + `FormatCOGWSI` used consistently across T5 → T8. WSI tag method names match the spec verbatim.

**Risks:**

- **R1 — internal/tiff Page accessor surface.** T2 assumes `page.ASCII(tag)` / `page.scalarU32Tag` / `page.doubleTag` patterns. Implementer must read page.go first to confirm. If APIs differ, adapt.
- **R2 — ClassifyAssociated signature change.** T4 modifies a function signature; all callers in formats/generictiff/ must update. Could be a small audit task. Wrapper-function alternative noted.
- **R3 — Generic-TIFF level type re-use.** T6 step 4 may need to export generictiff's level type OR re-implement in cogwsi. Implementer picks based on cheapest path.
- **R4 — Cross-fixture parity gate strictness.** Spec says writer preserves tile bytes "bit-exact" but with abbreviated-JPEG preservation. There may be edge cases (associated-image re-encoding, JPEGTables differences) where bytes diverge. Plan instructs the implementer to document any observed divergences in CHANGELOG and treat them as expected per spec rather than regressions.
- **R5 — Geometry probe for 10 fixtures.** T7 step 1 requires probing each fixture. The probe Go program needs writing once; ~10 lines. Output capture for 10 fixtures takes a few minutes given the 4.8 GB svs_40x_bigtiff fixture.
- **R6 — Pyramid integer-multiple acceptance might affect existing fixtures.** T3's relaxation could change behavior on existing generic-TIFF fixtures if any of them have mixed-ratio chains. Probe existing TestGenericGeometry fixtures during T3 to verify no regression.
