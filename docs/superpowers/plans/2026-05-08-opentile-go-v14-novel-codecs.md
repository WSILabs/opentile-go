# opentile-go v0.14 — wsi-tools novel-codec generic-TIFF support implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Recognise four novel TIFF compression tag values produced by the user's wsi-tools transcoder (WebP/JPEG XL/AVIF/HTJ2K) + parse the wsi-tools `ImageDescription` to populate cross-format `Metadata`. Additive — no breaking changes.

**Architecture:** Single-batch additive milestone. T1-T3 land the public `Compression` enum values + tag mappings + validator whitelist. T4 lands the wsi-tools `ImageDescription` parser. T5 wires the four real-fixture files into TestSlideParity + geometry pinning. T6 closes with docs.

**Tech stack:** Go 1.23+; existing `internal/tiff` validator + `formats/generictiff` reader.

**Spec:** [`docs/superpowers/specs/2026-05-08-opentile-go-v14-novel-codecs-design.md`](../specs/2026-05-08-opentile-go-v14-novel-codecs-design.md).

---

## Task layout

6 tasks, single batch:

- T1 — `Compression` enum additions + `String()` cases + unit tests
- T2 — Validator whitelist (5 new tag values) + unit tests
- T3 — `tiffCompressionToOpentile` mapping (5 new tag values) + unit tests
- T4 — wsi-tools `ImageDescription` parser + buildMetadata integration
- T5 — Real-fixture wiring (geometry pinning + SHA fixtures + slideCandidates)
- T6 — Docs + ship

---

## T1 — `Compression` enum additions

**Files:**
- Modify: `compression.go`
- Modify: `compression_test.go`

Adds three new exported enum values + their `String()` cases. Mirrors v0.8's `CompressionAVIF` / `CompressionIRIS` precedent.

- [ ] **Step 1: Add enum values**

Edit `compression.go`. Find the `const` block declaring the existing Compression values (around line 16). Append THREE new values AFTER `CompressionDeflate`:

```go
	// CompressionWebP identifies a WebP-encoded tile (RIFF + WEBP +
	// VP8/VP8L/VP8X chunks). TIFF tag 259 value 50001 in libtiff
	// convention; same value is what the user's wsi-tools transcoder
	// emits. Tile bytes are a complete self-contained WebP file.
	// Consumer decodes via libwebp or golang.org/x/image/webp.
	//
	// Added in v0.14.
	CompressionWebP

	// CompressionJPEGXL identifies a JPEG XL codestream tile. TIFF
	// tag 259 value 50002 (wsi-tools convention; not formally
	// registered). Tile bytes are a bare JXL codestream beginning
	// with the 0xFF 0x0A marker. Consumer decodes via libjxl (cgo)
	// or stdlib image/jxl when available.
	//
	// Added in v0.14.
	CompressionJPEGXL

	// CompressionHTJ2K identifies an HTJ2K (High-Throughput JPEG
	// 2000, ISO/IEC 15444-15) codestream tile. TIFF tag 259 value
	// 60003 (wsi-tools convention). Distinct from CompressionJP2K
	// because HTJ2K uses a different entropy coder (FBCOT instead
	// of EBCOT) and a standard JP2K decoder will fail on HTJ2K
	// bytes. Consumer decodes via OpenJPEG 2.5+, OpenHTJ2K, or
	// Kakadu.
	//
	// Added in v0.14.
	CompressionHTJ2K
```

- [ ] **Step 2: Add String() cases**

In the same file, find `func (c Compression) String() string`. Add three new cases before the default:

```go
	case CompressionWebP:
		return "webp"
	case CompressionJPEGXL:
		return "jpeg-xl"
	case CompressionHTJ2K:
		return "htj2k"
```

- [ ] **Step 3: Update the unit test**

Edit `compression_test.go`. Find `TestCompressionString` and add three rows to the table:

```go
		{CompressionWebP, "webp"},
		{CompressionJPEGXL, "jpeg-xl"},
		{CompressionHTJ2K, "htj2k"},
```

- [ ] **Step 4: Run + verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
go test -count=1 -run TestCompressionString ./ 2>&1 | tail -5
gofmt -l compression.go compression_test.go
```

Expected: build clean, test passes, gofmt empty.

- [ ] **Step 5: Commit**

```bash
git add compression.go compression_test.go
git commit -m "$(cat <<'EOF'
feat(v0.14): T1 — Compression enum additions for WebP/JPEG XL/HTJ2K

Three new opentile.Compression values mirroring the v0.8
CompressionAVIF / CompressionIRIS precedent (consumer-decode codecs
each get their own enum so consumers can switch on Level.Compression()
for decoder selection):

  CompressionWebP     "webp"     libwebp / golang.org/x/image/webp
  CompressionJPEGXL   "jpeg-xl"  libjxl / future stdlib image/jxl
  CompressionHTJ2K    "htj2k"    OpenJPEG 2.5+ / OpenHTJ2K / Kakadu

CompressionAVIF already exists. CompressionHTJ2K is intentionally
distinct from CompressionJP2K — HTJ2K's FBCOT entropy coder is
incompatible with standard JP2K decoders.

Tag-value mappings + validator whitelist updates land in T2/T3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — Validator whitelist (5 new tag values)

**Files:**
- Modify: `internal/tiff/classify_pyramid.go`
- Modify: `internal/tiff/classify_pyramid_test.go`

Adds five new tag values to `validCompression` so tiled IFDs using these compressions become valid pyramid candidates.

- [ ] **Step 1: Update the whitelist**

Edit `internal/tiff/classify_pyramid.go`. Find:

```go
// validCompression reports whether comp is one of the v0.10-allowed
// values: 1 (None), 5 (LZW), 7 (JPEG), 8 (Deflate), 33003 (JPEG 2000).
func validCompression(comp uint32) bool {
	switch comp {
	case 1, 5, 7, 8, 33003:
		return true
	default:
		return false
	}
}
```

Replace with:

```go
// validCompression reports whether comp is one of the allowed
// TIFF compression tag values:
//
//   - v0.10: 1 (None), 5 (LZW), 7 (JPEG), 8 (Deflate), 33003 (JPEG 2000)
//   - v0.14 additions:
//   - 34712 — registered JP2K code (libtiff convention; we already
//     accept Aperio's nonstandard 33003)
//   - 50001 — WebP (libtiff convention)
//   - 50002 — JPEG XL (wsi-tools convention)
//   - 60001 — AVIF (wsi-tools convention; private/experimental range)
//   - 60003 — HTJ2K (wsi-tools convention; private/experimental range)
func validCompression(comp uint32) bool {
	switch comp {
	case 1, 5, 7, 8, 33003,
		34712, 50001, 50002, 60001, 60003:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 2: Add a direct unit test**

There's no existing direct test of `validCompression` (it's exercised through `TestClassifyPyramid_TiledWithBadCompressionGoesToOthers`). Add a focused test in `internal/tiff/classify_pyramid_test.go` (append to the existing test file):

```go
func TestValidCompression(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp uint32
		want bool
	}{
		// v0.10 originals
		{"None", 1, true},
		{"LZW", 5, true},
		{"JPEG", 7, true},
		{"Deflate", 8, true},
		{"JP2K_Aperio", 33003, true},

		// v0.14 additions
		{"JP2K_registered", 34712, true},
		{"WebP", 50001, true},
		{"JPEGXL", 50002, true},
		{"AVIF", 60001, true},
		{"HTJ2K", 60003, true},

		// Outside whitelist (sanity)
		{"PackBits", 32773, false},
		{"AdobeDeflate", 32946, false}, // accepted via aliasing in tiledImage but not the validator
		{"unknown_60002", 60002, false},
		{"unknown_99999", 99999, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validCompression(tc.comp); got != tc.want {
				t.Errorf("validCompression(%d) = %v, want %v", tc.comp, got, tc.want)
			}
		})
	}
}
```

NOTE: `AdobeDeflate` (32946) is accepted by `tiffCompressionToOpentile` (T3) but NOT in the validator's whitelist — this is consistent with v0.10 / v0.13 behavior where validator's whitelist is the gate for pyramid acceptance and tiffCompressionToOpentile is the post-validator mapping. Comment captures the intent.

- [ ] **Step 3: Run + verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go test -count=1 -run TestValidCompression ./internal/tiff/ 2>&1 | tail -10
go test -count=1 ./internal/tiff/ 2>&1 | tail -3
gofmt -l internal/tiff/classify_pyramid.go internal/tiff/classify_pyramid_test.go
```

Expected: 14 subtests pass, full internal/tiff suite green, gofmt empty.

- [ ] **Step 4: Commit**

```bash
git add internal/tiff/classify_pyramid.go internal/tiff/classify_pyramid_test.go
git commit -m "$(cat <<'EOF'
feat(tiff): T2 — validCompression whitelist gains 5 new tag values

internal/tiff/classify_pyramid.go::validCompression now accepts:

  34712 — registered JP2K code (alongside Aperio's nonstandard 33003)
  50001 — WebP
  50002 — JPEG XL
  60001 — AVIF
  60003 — HTJ2K

These are the wsi-tools transcoder's compression tag values. Tiled
IFDs using these now become valid pyramid candidates per the v0.11
single-level (MinLevels=1) and multi-level pyramid rules.

New TestValidCompression directly exercises the whitelist (was
previously only tested transitively via TiledWithBadCompression
GoesToOthers).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — `tiffCompressionToOpentile` mapping update

**Files:**
- Modify: `formats/generictiff/tiled.go`
- Modify: `formats/generictiff/tiled_test.go` (or wherever per-format unit tests live; create if needed)

Maps the five new TIFF tag values to the corresponding `opentile.Compression` enum values.

- [ ] **Step 1: Update the mapping**

Edit `formats/generictiff/tiled.go`. Find `tiffCompressionToOpentile` (around line 347). Replace its body:

```go
// tiffCompressionToOpentile maps the TIFF tag 259 value to opentile's
// Compression enum.
//
//	1     None             → CompressionNone
//	5     LZW              → CompressionLZW
//	7     JPEG             → CompressionJPEG
//	8     Deflate          → CompressionDeflate (v0.10 addition)
//	32946 AdobeDeflate     → CompressionDeflate (same payload as 8)
//	33003 JP2K (Aperio)    → CompressionJP2K
//	34712 JP2K (registered) → CompressionJP2K (v0.14 addition)
//	50001 WebP             → CompressionWebP   (v0.14 addition)
//	50002 JPEG XL          → CompressionJPEGXL (v0.14 addition)
//	60001 AVIF             → CompressionAVIF   (v0.14 addition)
//	60003 HTJ2K            → CompressionHTJ2K  (v0.14 addition)
//
// Other values map to CompressionUnknown — those IFDs would fail the
// validator's compression whitelist and not become pyramid candidates
// in the first place.
func tiffCompressionToOpentile(comp uint32) opentile.Compression {
	switch comp {
	case 1:
		return opentile.CompressionNone
	case 5:
		return opentile.CompressionLZW
	case 7:
		return opentile.CompressionJPEG
	case 8, 32946:
		return opentile.CompressionDeflate
	case 33003, 34712:
		return opentile.CompressionJP2K
	case 50001:
		return opentile.CompressionWebP
	case 50002:
		return opentile.CompressionJPEGXL
	case 60001:
		return opentile.CompressionAVIF
	case 60003:
		return opentile.CompressionHTJ2K
	default:
		return opentile.CompressionUnknown
	}
}
```

- [ ] **Step 2: Add a focused unit test**

Edit `formats/generictiff/tiled_test.go` (append). The existing tests are end-to-end; add a pure-function table test for the mapping:

```go
func TestTiffCompressionToOpentile(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp uint32
		want opentile.Compression
	}{
		{"None", 1, opentile.CompressionNone},
		{"LZW", 5, opentile.CompressionLZW},
		{"JPEG", 7, opentile.CompressionJPEG},
		{"Deflate", 8, opentile.CompressionDeflate},
		{"AdobeDeflate", 32946, opentile.CompressionDeflate},
		{"JP2K_Aperio", 33003, opentile.CompressionJP2K},
		{"JP2K_registered_v14", 34712, opentile.CompressionJP2K},
		{"WebP_v14", 50001, opentile.CompressionWebP},
		{"JPEGXL_v14", 50002, opentile.CompressionJPEGXL},
		{"AVIF_v14", 60001, opentile.CompressionAVIF},
		{"HTJ2K_v14", 60003, opentile.CompressionHTJ2K},
		{"unknown_99999", 99999, opentile.CompressionUnknown},
		{"unknown_60002", 60002, opentile.CompressionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tiffCompressionToOpentile(tc.comp); got != tc.want {
				t.Errorf("tiffCompressionToOpentile(%d) = %v, want %v", tc.comp, got, tc.want)
			}
		})
	}
}
```

If `formats/generictiff/tiled_test.go` doesn't exist, look for `formats/generictiff/generic_test.go` or any *_test.go file in the package and append there. Verify the imports include `opentile "github.com/wsilabs/opentile-go"`.

- [ ] **Step 3: Run + verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go test -count=1 -run TestTiffCompressionToOpentile ./formats/generictiff/ 2>&1 | tail -5
go test -count=1 ./formats/generictiff/ 2>&1 | tail -3
gofmt -l formats/generictiff/tiled.go
```

- [ ] **Step 4: Commit**

```bash
git add formats/generictiff/
git commit -m "$(cat <<'EOF'
feat(generictiff): T3 — tiffCompressionToOpentile mappings for v0.14 codecs

Five new mappings:

  34712 → CompressionJP2K     (registered JP2K code; we already
                                accepted Aperio's nonstandard 33003)
  50001 → CompressionWebP     (libtiff convention)
  50002 → CompressionJPEGXL   (wsi-tools convention)
  60001 → CompressionAVIF     (wsi-tools choice; uses the existing
                                v0.8 enum value)
  60003 → CompressionHTJ2K    (wsi-tools choice)

Plus a TestTiffCompressionToOpentile table exercising the full
mapping including the v0.14 additions.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — wsi-tools `ImageDescription` parser + buildMetadata integration

**Files:**
- Create: `formats/generictiff/wsitools.go`
- Create: `formats/generictiff/wsitools_test.go`
- Modify: `formats/generictiff/tiler.go` (integrate parser into `buildMetadata`)

Parses the wsi-tools `ImageDescription` format:
```
wsi-tools/<version> transcode source=<src> codec=<codec> mpp=<float> mag=<N>x scanner="<name>" date=<YYYY-MM-DD>
```

Gated by the `wsi-tools/` prefix; non-wsi-tools `ImageDescription` strings unaffected.

- [ ] **Step 1: Write `wsitools.go`**

```go
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
	hasMag             bool
	magnification      float64
	hasScanner         bool
	scannerManufacturer string
	hasDate            bool
	acquisitionDate    time.Time
	hasMPP             bool
	micronsPerPixel    float64
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

	// Tokenise into key=value pairs, respecting double-quoted values.
	// e.g.  scanner="Aperio Image Library" → key=scanner value=Aperio Image Library
	for _, tok := range tokeniseKVPairs(desc) {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		key := tok[:eq]
		val := strings.Trim(tok[eq+1:], `"`)
		switch key {
		case "mag":
			// "20x" → 20.0; "40x" → 40.0; tolerate trailing "x"
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
			// YYYY-MM-DD; interpret as 00:00 UTC.
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

// tokeniseKVPairs splits a wsi-tools ImageDescription line into
// key=value tokens, treating double-quoted values as a single token.
// The leading `wsi-tools/<version> transcode` prefix is dropped.
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
```

- [ ] **Step 2: Wire `parseWSIToolsDescription` into `buildMetadata`**

Edit `formats/generictiff/tiler.go`. Find `buildMetadata` (around line 127). Replace with:

```go
// buildMetadata reads the cross-format + generic-specific metadata
// from the level-0 IFD's standard TIFF tags. Per spec §7:
//
//	Make (271)             → ScannerManufacturer
//	Model (272)            → ScannerModel
//	Software (305)         → ScannerSoftware (semicolon/newline-split)
//	DateTime (306)         → AcquisitionDateTime (TIFF "YYYY:MM:DD HH:MM:SS")
//	XResolution (282)      → MicronsPerPixel (via ResolutionUnit)
//	ResolutionUnit (296)
//	ImageDescription (270) → ImageDescription verbatim
//
// v0.14 addition: when ImageDescription begins with `wsi-tools/`,
// the wsi-tools parser populates Magnification / ScannerManufacturer /
// AcquisitionDateTime / MicronsPerPixel from the parsed fields,
// overriding any standard-TIFF-tag-derived values. The raw
// ImageDescription remains stored verbatim for consumers who want
// full provenance (source, codec, wsi-tools version).
//
// Magnification has no standard TIFF tag → 0 unless wsi-tools sets it.
func buildMetadata(p *tiff.Page) Metadata {
	var md Metadata
	if v, ok := p.ASCII(tagMake); ok {
		md.ScannerManufacturer = strings.TrimSpace(v)
	}
	if v, ok := p.ASCII(tagModel); ok {
		md.ScannerModel = strings.TrimSpace(v)
	}
	if v, ok := p.Software(); ok {
		md.ScannerSoftware = splitSoftware(v)
	}
	if v, ok := p.ASCII(tiff.TagDateTime); ok {
		if ts, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(v)); err == nil {
			md.AcquisitionDateTime = ts
		}
	}
	if v, ok := p.ImageDescription(); ok {
		md.ImageDescription = strings.TrimSpace(v)
	}
	md.MicronsPerPixel = micronsPerPixel(p)

	// v0.14: wsi-tools ImageDescription override.
	if md.ImageDescription != "" {
		if wt, ok := parseWSIToolsDescription(md.ImageDescription); ok {
			if wt.hasMag {
				md.Magnification = wt.magnification
			}
			if wt.hasScanner {
				md.ScannerManufacturer = wt.scannerManufacturer
			}
			if wt.hasDate {
				md.AcquisitionDateTime = wt.acquisitionDate
			}
			if wt.hasMPP {
				md.MicronsPerPixel = wt.micronsPerPixel
			}
		}
	}

	return md
}
```

- [ ] **Step 3: Write `wsitools_test.go`**

```go
package generictiff

import (
	"testing"
	"time"
)

func TestParseWSIToolsDescription_HappyPath(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode source=svs codec=avif mpp=0.499 mag=20x scanner="Aperio" date=2009-12-29`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true on wsi-tools-prefixed input")
	}
	if !md.hasMag || md.magnification != 20.0 {
		t.Errorf("magnification = %v, want 20.0", md.magnification)
	}
	if !md.hasScanner || md.scannerManufacturer != "Aperio" {
		t.Errorf("scanner = %q, want %q", md.scannerManufacturer, "Aperio")
	}
	if !md.hasMPP || md.micronsPerPixel != 0.499 {
		t.Errorf("mpp = %v, want 0.499", md.micronsPerPixel)
	}
	if !md.hasDate {
		t.Fatal("date not parsed")
	}
	want := time.Date(2009, 12, 29, 0, 0, 0, 0, time.UTC)
	if !md.acquisitionDate.Equal(want) {
		t.Errorf("date = %v, want %v", md.acquisitionDate, want)
	}
}

func TestParseWSIToolsDescription_QuotedScannerWithSpace(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode scanner="Acme WSI Scanner X100" mpp=0.25`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if md.scannerManufacturer != "Acme WSI Scanner X100" {
		t.Errorf("scanner = %q, want %q", md.scannerManufacturer, "Acme WSI Scanner X100")
	}
	if md.micronsPerPixel != 0.25 {
		t.Errorf("mpp = %v, want 0.25", md.micronsPerPixel)
	}
}

func TestParseWSIToolsDescription_MissingFields(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode codec=webp`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if md.hasMag {
		t.Error("hasMag should be false (no mag in description)")
	}
	if md.hasMPP {
		t.Error("hasMPP should be false")
	}
	if md.hasScanner {
		t.Error("hasScanner should be false")
	}
	if md.hasDate {
		t.Error("hasDate should be false")
	}
}

func TestParseWSIToolsDescription_MalformedMPP(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode mpp=not-a-number`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true (parse is lenient on bad values)")
	}
	if md.hasMPP {
		t.Error("hasMPP should be false on malformed mpp value")
	}
	if md.micronsPerPixel != 0 {
		t.Errorf("mpp = %v, want 0", md.micronsPerPixel)
	}
}

func TestParseWSIToolsDescription_NonWSIToolsInput(t *testing.T) {
	for _, desc := range []string{
		"",
		"Aperio Image Library v11.2.1",
		"some random text",
		"wsi-toolsx/0.2.0", // close prefix but not exact
	} {
		_, ok := parseWSIToolsDescription(desc)
		if ok {
			t.Errorf("parseWSIToolsDescription(%q) = ok=true, want false", desc)
		}
	}
}

func TestParseWSIToolsDescription_UnknownKeys(t *testing.T) {
	// Forward-compat: unknown keys ignored, known keys still parsed.
	desc := `wsi-tools/0.3.0-dev transcode source=svs unknownkey=somevalue mag=40x newfield="future use"`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !md.hasMag || md.magnification != 40.0 {
		t.Errorf("magnification = %v, want 40.0 (known keys parsed even with unknown keys present)", md.magnification)
	}
}
```

- [ ] **Step 4: Run + verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
go test -count=1 -v -run TestParseWSIToolsDescription ./formats/generictiff/ 2>&1 | tail -15
gofmt -l formats/generictiff/wsitools.go formats/generictiff/wsitools_test.go formats/generictiff/tiler.go
```

Expected: build clean, 6 subtests pass, gofmt empty.

- [ ] **Step 5: Commit**

```bash
git add formats/generictiff/wsitools.go formats/generictiff/wsitools_test.go formats/generictiff/tiler.go
git commit -m "$(cat <<'EOF'
feat(generictiff): T4 — wsi-tools ImageDescription parser + Metadata integration

When the level-0 ImageDescription starts with `wsi-tools/`, parse the
key=value form:

  wsi-tools/<ver> transcode source=<src> codec=<codec> mpp=<float>
                            mag=<N>x scanner="<name>" date=<YYYY-MM-DD>

Parsed fields override standard-TIFF-tag-derived values in
Metadata.{Magnification, ScannerManufacturer, AcquisitionDateTime,
MicronsPerPixel}. wsi-tools-only fields (source, codec, version)
remain accessible via the raw ImageDescription string per sealed Q2
— no wsi-tools-specific public accessor.

Lenient parser:
  - Quoted strings with embedded spaces handled
  - Malformed values (e.g., non-numeric mpp) skip that field, no error
  - Unknown keys ignored — forward-compat with future wsi-tools fields
  - Non-wsi-tools-prefixed ImageDescriptions return ok=false; existing
    behavior preserved for non-wsi-tools generic TIFFs

6 unit tests cover happy path, quoted scanner, missing fields,
malformed mpp, non-wsi-tools input (5 negative cases), and unknown-
key forward compat.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — Real-fixture wiring (geometry + SHA fixtures + slideCandidates)

**Files:**
- Modify: `tests/integration_test.go` (extend slideCandidates)
- Modify: `tests/parity/generic_geometry_test.go` (add 4 fixture rows)
- Generate: `tests/fixtures/avif-out.tiff.json`, `htj2k-out.tiff.json`, `jxl-out.tiff.json`, `webp-out.tiff.json`

Wires the four wsi-tools fixtures into the existing test infrastructure.

- [ ] **Step 1: Probe each fixture for exact geometry**

```bash
cd /Users/cornish/GitHub/opentile-go
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/avif-out.tiff 2>&1
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/htj2k-out.tiff 2>&1
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/jxl-out.tiff 2>&1
go run /tmp/genericsmoke/main.go sample_files/generic-tiff/webp-out.tiff 2>&1
```

Note the Levels output for each (should show one level at 2220×2967 with 240×240 tiles + the right Compression() value). Capture exact grid dims for the geometry table.

If `/tmp/genericsmoke/main.go` doesn't exist or doesn't show Compression / grid clearly, write a small probe similar to the one used in earlier milestones (Read tool to find it under /tmp/genericsmoke/).

- [ ] **Step 2: Add geometry fixture rows**

Edit `tests/parity/generic_geometry_test.go`. Find `genericFixtures` (around line 46). Append AFTER the last existing fixture entry (the scan_620 Grundium one):

```go
	{
		// avif-out.tiff (v0.14): wsi-tools transcode of CMU-1-Small-
		// Region.svs to AVIF tile codec. Tag 60001 → CompressionAVIF.
		// Single-level pyramid + 3 stripped associated images
		// preserved from the source SVS.
		filename: "avif-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionAVIF},
		},
		associated: []genericAssocExpect{
			{Kind: generictiff.KindThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: -1},
			{Kind: generictiff.KindLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: -1},
			{Kind: generictiff.KindMacro, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: -1},
		},
		tileMagic: []byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70, 0x61, 0x76, 0x69, 0x66}, // ftyp avif
	},
	{
		// htj2k-out.tiff (v0.14): tag 60003 → CompressionHTJ2K.
		filename: "htj2k-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionHTJ2K},
		},
		associated: []genericAssocExpect{
			{Kind: generictiff.KindThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: -1},
			{Kind: generictiff.KindLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: -1},
			{Kind: generictiff.KindMacro, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: -1},
		},
		tileMagic: []byte{0xFF, 0x4F, 0xFF, 0x51}, // J2K SOC + SIZ
	},
	{
		// jxl-out.tiff (v0.14): tag 50002 → CompressionJPEGXL.
		filename: "jxl-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionJPEGXL},
		},
		associated: []genericAssocExpect{
			{Kind: generictiff.KindThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: -1},
			{Kind: generictiff.KindLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: -1},
			{Kind: generictiff.KindMacro, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: -1},
		},
		tileMagic: []byte{0xFF, 0x0A}, // JXL codestream marker
	},
	{
		// webp-out.tiff (v0.14): tag 50001 → CompressionWebP.
		filename: "webp-out.tiff",
		levels: []genericLevelExpect{
			{W: 2220, H: 2967, TileW: 240, TileH: 240, GridW: 10, GridH: 13, Compression: opentile.CompressionWebP},
		},
		associated: []genericAssocExpect{
			{Kind: generictiff.KindThumbnail, W: 574, H: 768, Compression: opentile.CompressionJPEG, ByteCount: -1},
			{Kind: generictiff.KindLabel, W: 387, H: 463, Compression: opentile.CompressionLZW, ByteCount: -1},
			{Kind: generictiff.KindMacro, W: 1280, H: 431, Compression: opentile.CompressionJPEG, ByteCount: -1},
		},
		tileMagic: []byte{0x52, 0x49, 0x46, 0x46}, // RIFF
	},
```

NOTE: `ByteCount: -1` is a sentinel meaning "don't pin byte count" — the wsi-tools-preserved associated IFDs are byte-equivalent across all 4 fixtures (they all share the same source SVS), but their precise byte counts depend on libtiff's exact passthrough. If the existing geometry test interprets `ByteCount == -1` as "skip", great; if it strictly matches, replace with the actual measured byte counts after T5 step 4 generates fixtures.

If the existing `genericAssocExpect` struct's `ByteCount` field is uint and can't take -1: change the test struct to use a pointer or add a `SkipByteCount bool` flag, OR populate the actual byte counts from the probe in step 1.

Inspect `tests/parity/generic_geometry_test.go::genericAssocExpect` first to confirm the field type before locking in the table.

If grid dims (10×13) don't match what the probe reports, update the table.

- [ ] **Step 3: Wire into TestSlideParity**

Edit `tests/integration_test.go`. Find the `slideCandidates` slice. Append AFTER the existing Grundium scan_620 entry (or wherever the last generic-tiff entry sits):

```go
	// Generic TIFF v0.14 (wsi-tools novel codecs):
	"avif-out.tiff",
	"htj2k-out.tiff",
	"jxl-out.tiff",
	"webp-out.tiff",
```

- [ ] **Step 4: Generate SHA fixtures**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files \
    go test ./tests -tags generate -run TestGenerateFixtures \
    -generate -v 2>&1 | tail -20
```

Expected: 4 new JSON files appear at `tests/fixtures/avif-out.tiff.json`, `htj2k-out.tiff.json`, `jxl-out.tiff.json`, `webp-out.tiff.json`. Each well under 5 MB cap (these are small fixtures: 1.3-3.1 MB inputs with ~130 tiles per L0).

- [ ] **Step 5: Run + verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files \
    go test -count=1 -run TestGenericGeometry ./tests/parity/ 2>&1 | tail -10
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files \
    go test -count=1 -run "TestSlideParity/(avif|htj2k|jxl|webp)-out\\.tiff" ./tests/ 2>&1 | tail -10
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files \
    go test -count=1 ./... 2>&1 | tail -5
```

Expected: geometry tests for the 4 new fixtures pass; SHA-fixture tests pass; full module green.

- [ ] **Step 6: Commit**

```bash
git add tests/integration_test.go tests/parity/generic_geometry_test.go tests/fixtures/avif-out.tiff.json tests/fixtures/htj2k-out.tiff.json tests/fixtures/jxl-out.tiff.json tests/fixtures/webp-out.tiff.json
git commit -m "$(cat <<'EOF'
test(v0.14): T5 — wire 4 wsi-tools novel-codec fixtures into TestSlideParity

  avif-out.tiff   2220x2967  AVIF (tag 60001)
  htj2k-out.tiff  2220x2967  HTJ2K (tag 60003)
  jxl-out.tiff    2220x2967  JPEG XL (tag 50002)
  webp-out.tiff   2220x2967  WebP (tag 50001)

Each is a single-level pyramid (10x13 grid of 240px tiles) plus 3
stripped associated images preserved from the source SVS. Geometry
pinned in tests/parity/generic_geometry_test.go; tile magic bytes
verified per codec.

TestSlideParity total now 28 fixtures (was 24 post-v0.13).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Docs + ship

**Files:**
- Modify: `docs/formats/generictiff.md` (compression list update + decoder responsibility section + wsi-tools parser note)
- Modify: `docs/deferred.md` (§8h new — v0.14 retirement audit)
- Modify: `CHANGELOG.md` ([0.14.0] section + [Unreleased] reset)
- Modify: `CLAUDE.md` (Current milestone bump v0.13 → v0.14)
- Modify: `README.md` (add WebP/JXL/AVIF/HTJ2K to Supported-formats compression column for generictiff row)

- [ ] **Step 1: docs/formats/generictiff.md updates**

Find the compression-whitelist section. Add the v0.14 codecs:

```markdown
## v0.14 (this milestone) — novel tile codecs

opentile-go's generic-TIFF reader recognises four additional TIFF
compression tag values produced by `wsi-tools` (the user's WSI
transcoder) plus the registered JP2K code:

| Tag | Codec | opentile.Compression | Magic bytes |
|---:|---|---|---|
| 34712 | JP2K (registered) | `CompressionJP2K` | `FF 4F FF 51` |
| 50001 | WebP | `CompressionWebP` | `52 49 46 46` (RIFF) |
| 50002 | JPEG XL | `CompressionJPEGXL` | `FF 0A` |
| 60001 | AVIF | `CompressionAVIF` | `00 00 00 20 66 74 79 70 61 76 69 66` |
| 60003 | HTJ2K | `CompressionHTJ2K` | `FF 4F FF 51` (J2K SOC + SIZ) |

### Decoder responsibility

opentile-go ships byte-passthrough — we don't decode tiles. Per-codec
consumer responsibility:

- `CompressionWebP` → libwebp or `golang.org/x/image/webp`
- `CompressionJPEGXL` → libjxl (cgo) or stdlib image/jxl when available
- `CompressionAVIF` → libavif (cgo) or stdlib image/avif when available
- `CompressionHTJ2K` → OpenJPEG 2.5+, OpenHTJ2K, or Kakadu
- `CompressionJP2K` → OpenJPEG (any recent version)

Magic-byte validation lets consumers sanity-check their decoder dispatch
before paying the decode cost.

### wsi-tools ImageDescription parser

When a generic TIFF's level-0 ImageDescription starts with `wsi-tools/`,
opentile-go parses the structured key=value form to populate the
standard cross-format Metadata fields:

- `mag=20x` → `Tiler.Metadata().Magnification`
- `scanner="Aperio"` → `Tiler.Metadata().ScannerManufacturer`
- `date=YYYY-MM-DD` → `Tiler.Metadata().AcquisitionDateTime` (00:00 UTC)
- `mpp=0.499` → `generictiff.MetadataOf(t).MicronsPerPixel`

The raw ImageDescription remains stored verbatim for consumers who want
full provenance (`source=svs`, `codec=avif`, wsi-tools version). Non-
wsi-tools ImageDescriptions are unaffected.
```

(Adapt to the existing doc shape — find the "What's supported" or "Format basics" section and slot the v0.14 content where it fits.)

- [ ] **Step 2: docs/deferred.md §8h**

Insert §8h "Retired in v0.14" BEFORE §8g (newest-first ordering):

```markdown
## 8h. Retired in v0.14

v0.14 is a small additive milestone extending generic-TIFF
compression support to 4 novel tile codecs (WebP / JPEG XL / AVIF /
HTJ2K) plus the registered JP2K tag 34712, all produced by the
user's `wsi-tools` transcoder. No new format support; no breaking
changes.

**Items shipped:**

- 3 new `opentile.Compression` enum values (`CompressionWebP`,
  `CompressionJPEGXL`, `CompressionHTJ2K`); existing
  `CompressionAVIF` reused for tag 60001.
- Validator whitelist (`internal/tiff.validCompression`) accepts
  5 new tag values: 34712, 50001, 50002, 60001, 60003.
- generic-TIFF reader (`tiffCompressionToOpentile`) maps the 5
  new values to the corresponding enum.
- wsi-tools ImageDescription parser populates standard Metadata
  fields (Magnification, ScannerManufacturer, AcquisitionDateTime,
  MicronsPerPixel) when the prefix `wsi-tools/` is present.
- 4 new test fixtures (avif/htj2k/jxl/webp wsi-tools transcodes of
  CMU-1-Small-Region.svs); TestSlideParity total 28 (was 24).

**Architecture invariants preserved:**

- Public API additive only; existing consumers unaffected.
- Byte-passthrough contract preserved: opentile-go reports the
  compression via `Level.Compression()`; consumers ship the right
  decoder. Mirrors v0.8 IFE precedent for AVIF / Iris-proprietary.
- v1.0 cut still pending.
- cgo footprint unchanged.

**v0.14 lessons:** none new. The codec-passthrough pattern from
v0.8 IFE applied cleanly; wsi-tools ImageDescription parsing was
straightforward once gated by the prefix marker.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md`](superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md).
```

- [ ] **Step 3: CHANGELOG.md [0.14.0]**

Mirror the v0.13 [0.13.0] structure. Insert before [0.13.0]:

```markdown
## [0.14.0] — 2026-05-08

Novel-codec milestone — generic-TIFF reader recognises four new
tile compression tag values produced by the user's wsi-tools
transcoder (WebP, JPEG XL, AVIF, HTJ2K). Plus opportunistic parsing
of the wsi-tools ImageDescription format to populate standard
Metadata fields. Additive — no breaking changes.

### Added

- **3 new `opentile.Compression` enum values:**
  - `CompressionWebP` (TIFF tag 50001 — libtiff convention)
  - `CompressionJPEGXL` (TIFF tag 50002 — wsi-tools convention)
  - `CompressionHTJ2K` (TIFF tag 60003 — wsi-tools convention).
    Distinct from `CompressionJP2K` because HTJ2K's FBCOT entropy
    coder is incompatible with standard JP2K decoders.
- **5 new TIFF compression tag mappings** in
  `formats/generictiff/tiled.go::tiffCompressionToOpentile`:
  34712 (registered JP2K), 50001 (WebP), 50002 (JPEG XL),
  60001 (AVIF — uses existing `CompressionAVIF`), 60003 (HTJ2K).
- **Validator whitelist** (`internal/tiff.validCompression`) accepts
  the 5 new tag values.
- **wsi-tools ImageDescription parser**
  (`formats/generictiff/wsitools.go`). When the level-0
  ImageDescription starts with `wsi-tools/`, parses the structured
  key=value form to populate Magnification / ScannerManufacturer /
  AcquisitionDateTime / MicronsPerPixel. Lenient on missing /
  malformed values; forward-compatible with future wsi-tools fields
  (unknown keys ignored). Non-wsi-tools ImageDescriptions are
  unaffected.

### Changed

- 4 new test fixtures wired into TestSlideParity:
  - `avif-out.tiff` (AVIF, 2220×2967)
  - `htj2k-out.tiff` (HTJ2K, 2220×2967)
  - `jxl-out.tiff` (JPEG XL, 2220×2967)
  - `webp-out.tiff` (WebP, 2220×2967)
  TestSlideParity total: 28 fixtures (was 24).

### Notes

- **Byte-passthrough contract.** Per the v0.8 IFE precedent for
  AVIF and Iris-proprietary codecs, opentile-go reports each tile's
  Compression() value but doesn't decode. Consumers bring their
  own libwebp / libjxl / libavif / OpenJPEG-HTJ2K decoder.
- **Tag value mappings are wsi-tools-specific** for AVIF (60001)
  and HTJ2K (60003) — not formally registered TIFF codes. Files
  produced by other tooling using different tag values for these
  codecs would not be recognised.
- v0.14 introduced no new active limitations.
- v1.0 cut remains pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump "after v0.13" → "after v0.14"; v0.14 introduced no new active limitations.

- [ ] **Step 4: CLAUDE.md milestone bump**

Replace "Current milestone — v0.13" block with v0.14. Demote v0.13 to "Previous milestone — v0.13 (shipped 2026-05-08)" with a one-paragraph summary:

```markdown
## Current milestone — v0.14 (shipped)

- **Scope:** Novel-codec milestone — generic-TIFF reader recognises
  4 new tile compression tag values produced by the user's wsi-tools
  transcoder (WebP, JPEG XL, AVIF, HTJ2K). Plus a wsi-tools
  ImageDescription parser populating standard Metadata fields.
  Additive — no breaking changes. 6 plan tasks single batch.
- **API additions:** 3 new `opentile.Compression` enum values
  (`CompressionWebP`, `CompressionJPEGXL`, `CompressionHTJ2K`);
  validator + reader recognise tags 34712 (registered JP2K),
  50001 (WebP), 50002 (JPEG XL), 60001 (AVIF — existing
  `CompressionAVIF` enum), 60003 (HTJ2K). wsi-tools
  ImageDescription parser hidden behind the existing
  `Tiler.Metadata()` / `generictiff.MetadataOf` accessors.
- **Behavior change:** none. Existing consumers using v0.13 surfaces
  see no behavior change.
- **Active limitations:** unchanged from v0.13. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  `docs/deferred.md §1a`): unchanged from v0.13 (additive-only API
  doesn't introduce new deviations).
- **Correctness bar:** `make test` green. TestSlideParity total
  now 28 fixtures (was 24); 4 wsi-tools fixtures + tile magic byte
  pinning + per-fixture geometry.
- **Decoder responsibility:** byte-passthrough contract preserved
  per v0.8 IFE precedent. Consumers bring libwebp / libjxl /
  libavif / OpenJPEG-HTJ2K decoders.
- **Sealed Q-decisions (2):** Q1 four enum values total (3 new +
  existing AVIF; HTJ2K distinct from JP2K); Q2 parse populates
  standard fields only — no wsi-tools-specific public accessor.
- **Deferred forward:** L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9,
  R15. v1.0 cut still pending.
- **Design:** `docs/superpowers/specs/2026-05-08-opentile-go-v14-novel-codecs-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md`
- **Work branch:** `feat/v0.14`

## Previous milestone — v0.13 (shipped 2026-05-08)

Bandwidth-deduplication API — Level.TilePrefix() + TileBodyInto +
TileBodyMaxSize + opentile.SpliceJPEGTile helper. Additive (no
breaking changes). Pattern B savings depend on fixture-author choice
(shared JPEGTables tag 347 vs per-tile-embedded). Cross-format
byte-equality invariant verified; bench harness committed.
```

(Existing "Earlier milestones" bullet list stays, just push v0.13 down into it as needed.)

- [ ] **Step 5: README.md** — light touch

Find the Supported-formats table's generic-TIFF row. Update the Compression column to mention the new codecs. Currently:

```
| **Generic TIFF\*** | `.tiff`, `.tif` | tiled pyramidal (≥1 level, geometric scale chain) | classifier-assigned: label, macro, thumbnail, or `"associated"` fallback | JPEG, JP2K, LZW, Deflate, None (all passthrough) | sampled-tile SHAs + per-fixture geometry pin + cross-backing parity | [docs/formats/generictiff.md](./docs/formats/generictiff.md) |
```

Update the "Compression" column to:
```
JPEG, JP2K, LZW, Deflate, None, WebP, JPEG XL, AVIF, HTJ2K (all passthrough)
```

(Use Edit; sed risks markdown corruption.)

- [ ] **Step 6: Final pre-commit verification**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files \
    go test -count=1 ./... 2>&1 | tail -5
```

Expected: vet clean; gofmt clean on v0.14-touched files; every package green.

- [ ] **Step 7: Commit**

```bash
git add docs/formats/generictiff.md docs/deferred.md CHANGELOG.md CLAUDE.md README.md
git commit -m "$(cat <<'EOF'
docs(v0.14): T6 — generictiff.md + deferred §8h + CHANGELOG + CLAUDE.md milestone bump

docs/formats/generictiff.md: new section listing the 4 v0.14
compression tag mappings + decoder-responsibility table + wsi-tools
ImageDescription parser note.

docs/deferred.md §8h new — Retired in v0.14: lists 5 tag-value
additions, parser, fixture wiring. v0.14 introduced no new active
limitations.

CHANGELOG.md [0.14.0] section: Added (3 enum values + 5 mappings +
parser), Changed (4 fixtures wired), Notes (byte-passthrough,
wsi-tools-specific tag values, v1.0 still pending). [Unreleased]
reset.

CLAUDE.md: bump Current milestone v0.13 → v0.14. v0.13 demoted to
Previous; v0.12 / v0.11 / earlier collapsed.

README.md: Supported-formats table generic-TIFF row gains WebP /
JPEG XL / AVIF / HTJ2K in the Compression column.

End of milestone; v0.14 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (3 new enum values) → T1.
- §1.2 (5 new tag mappings) → T2 (validator) + T3 (mapping function).
- §1.3 (wsi-tools parser) → T4.
- §1.4 (validator behaviour) → T2.
- §3 (Q-decisions) → reflected throughout.
- §4 (4 fixtures) → T5.
- §5 (test strategy) → T1/T2/T3 unit tests + T4 parser tests + T5 fixtures + geometry pinning.
- §6 (no new limitations) → T6 docs confirm.
- §7 (plan outline) → matches.
- §8 (verification gates) → T6 step 6.
- §9 (decoder responsibility doc) → T6 step 1.

No spec section uncovered.

**Placeholder scan:** every step has exact code blocks, exact paths, expected outputs. T5 step 2 has one `ByteCount: -1` sentinel that depends on the existing test struct supporting it — flagged inline ("inspect the field type before locking in the table"). T5 step 1 references `/tmp/genericsmoke/main.go` which is from earlier milestones; the implementer can write a tiny inline probe if it's gone. Both are minor adaptive points, not unspecified work.

**Type consistency:** `CompressionWebP`/`JPEGXL`/`HTJ2K` used identically across T1 → T6. `wsiToolsMetadata` struct + `parseWSIToolsDescription` function consistent T4 → T6 (T6 doesn't reference them by name; it documents user-visible fields they populate).

**Risks:**

- **R1 — `ByteCount: -1` sentinel.** `genericAssocExpect`'s ByteCount field type may not allow -1 cleanly. Mitigation: T5 step 2 instructs the implementer to inspect the struct first; if it can't take -1, they populate measured byte counts instead. Either path works.
- **R2 — wsi-tools format may evolve.** The user's transcoder is `0.2.0-dev`. Future versions might add fields or change syntax. Mitigation: T4's parser is forward-compat (unknown keys ignored), gated by the prefix marker. Future fields surface only when needed.
- **R3 — Magnification override semantics.** wsi-tools `mag=20x` overrides `Tiler.Metadata().Magnification`. If a future wsi-tools file has BOTH a Software-tag-derived magnification AND a wsi-tools `mag=` field, wsi-tools wins. Documented as intentional in T4.
- **R4 — Tag 60002 reserved.** wsi-tools uses 60001 (AVIF) and 60003 (HTJ2K), skipping 60002. We don't reserve 60002; future codecs from wsi-tools might use it without our recognition. Mitigation: out of scope; v0.15+ if needed.
