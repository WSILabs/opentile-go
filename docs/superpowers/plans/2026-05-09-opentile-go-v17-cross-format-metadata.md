# opentile-go v0.17 — cross-format Metadata expansion implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close R20 — expand cross-format `opentile.Metadata` with 4 new typed fields (MicronsPerPixel, MicronsPerPixelX, MicronsPerPixelY, ImageDescription) + a `Properties map[string]string` for canonical extensions and vendor-namespaced passthrough. Update every format reader to populate the new fields; clean up format-specific Metadata structs per Q4 Option B.

**Architecture:** 8 tasks, single batch. T1 lands the cross-format struct + helpers. T2-T7 update each format reader (one or two formats per task, batched by complexity). T8 lands the cross-format parity test + docs + ship.

**Tech stack:** Go 1.23+; existing `opentile` package + every `formats/*` package.

**Spec:** [`docs/superpowers/specs/2026-05-09-opentile-go-v17-cross-format-metadata-design.md`](../specs/2026-05-09-opentile-go-v17-cross-format-metadata-design.md).

**Existing pattern (audited 2026-05-09):**
- Every format reader has `t.md` of type format-specific Metadata struct embedding `opentile.Metadata`.
- `Tiler.Metadata()` returns `t.md.Metadata` (the embedded cross-format part).
- Format-specific metadata files: `formats/svs/metadata.go` (127 LOC), `formats/ndpi/metadata.go` (120), `formats/philipstiff/metadata.go` (137), `formats/ometiff/metadata.go` (147), `formats/bif/metadata.go` (186), `formats/ife/metadata.go` (590), `formats/szi/metadata.go` (253). `leicascn` and `generictiff` have no separate metadata.go — they populate cross-format directly.
- `leicascn.Tiler.Metadata()` currently returns `opentile.Metadata{}` (empty) — T6 populates it from SCN-XML.

---

## Task layout

8 tasks, single batch:

- T1 — `opentile.Metadata` struct expansion (4 typed fields + Properties map + canonical key constants + helpers + unit tests)
- T2 — SVS + NDPI populate
- T3 — Philips TIFF + Generic TIFF populate
- T4 — OME-TIFF populate
- T5 — BIF + IFE populate
- T6 — Leica SCN populate (currently empty — first-time population)
- T7 — SZI cleanup (Option B: strip cross-format duplicates from `szi.Metadata`; keep raw native fields)
- T8 — cross-format parity test + per-format docs sweep + CHANGELOG + CLAUDE.md milestone bump + deferred §8k

---

## T1 — `opentile.Metadata` struct expansion + helpers

**Files:**
- Modify: `metadata.go`
- Modify or create: `metadata_test.go`

- [ ] **Step 1: Expand the `Metadata` struct**

Edit `/Users/cornish/GitHub/opentile-go/metadata.go`. Append after `AcquisitionDateTime`:

```go
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
```

- [ ] **Step 2: Add canonical key constants**

In `metadata.go`, add a const block before the struct:

```go
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
```

- [ ] **Step 3: Add helper methods**

In the same file:

```go
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
```

- [ ] **Step 4: Write unit tests**

Create or edit `/Users/cornish/GitHub/opentile-go/metadata_test.go`:

```go
package opentile

import "testing"

func TestSetMPPSymmetric_Equal(t *testing.T) {
	m := Metadata{MicronsPerPixelX: 0.4, MicronsPerPixelY: 0.4}
	m.SetMPPSymmetric()
	if m.MicronsPerPixel != 0.4 {
		t.Errorf("MicronsPerPixel = %v, want 0.4", m.MicronsPerPixel)
	}
}

func TestSetMPPSymmetric_Asymmetric(t *testing.T) {
	m := Metadata{MicronsPerPixelX: 0.4, MicronsPerPixelY: 0.5}
	m.MicronsPerPixel = 0.45 // pre-set; should be cleared
	m.SetMPPSymmetric()
	if m.MicronsPerPixel != 0 {
		t.Errorf("asymmetric: MicronsPerPixel = %v, want 0", m.MicronsPerPixel)
	}
}

func TestSetMPPSymmetric_OneZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y float64
	}{
		{"X zero", 0, 0.4},
		{"Y zero", 0.4, 0},
		{"both zero", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Metadata{MicronsPerPixelX: tc.x, MicronsPerPixelY: tc.y}
			m.SetMPPSymmetric()
			if m.MicronsPerPixel != 0 {
				t.Errorf("MicronsPerPixel = %v, want 0", m.MicronsPerPixel)
			}
		})
	}
}

func TestSetProperty_NilMap(t *testing.T) {
	var m Metadata // Properties nil
	m.SetProperty("foo", "bar")
	if got := m.Properties["foo"]; got != "bar" {
		t.Errorf("Properties[foo] = %q, want bar", got)
	}
}

func TestSetProperty_Overwrite(t *testing.T) {
	m := Metadata{Properties: map[string]string{"foo": "old"}}
	m.SetProperty("foo", "new")
	if got := m.Properties["foo"]; got != "new" {
		t.Errorf("Properties[foo] = %q, want new", got)
	}
}

func TestPropertyConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"PropertyCaseNumber", PropertyCaseNumber, "case-number"},
		{"PropertyUserName", PropertyUserName, "user-name"},
		{"PropertyScannedAreaMM2", PropertyScannedAreaMM2, "scanned-area-mm2"},
		{"PropertyScanDurationSec", PropertyScanDurationSec, "scan-duration-seconds"},
		{"PropertyComments", PropertyComments, "comments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
go test -count=1 -run "TestSet|TestPropertyConstants" . 2>&1 | tail -10
gofmt -l metadata.go metadata_test.go
```

Expected: build clean (existing tests in dependent packages may fail until T2-T7 land — that's OK), new tests pass, gofmt empty.

- [ ] **Step 6: Commit**

```bash
git add metadata.go metadata_test.go
git commit -m "$(cat <<'EOF'
feat(v0.17): T1 — opentile.Metadata struct expansion (R20)

Cross-format Metadata gains 4 new typed fields + a Properties map
for opentile-go-canonical extensions and vendor-namespaced
passthrough. Closes R20 from deferred backlog.

Typed additions:
  MicronsPerPixel     float64  (when X==Y; zero otherwise)
  MicronsPerPixelX    float64
  MicronsPerPixelY    float64
  ImageDescription    string

Properties map[string]string with 5 canonical key constants:
  PropertyCaseNumber       = "case-number"
  PropertyUserName         = "user-name"
  PropertyScannedAreaMM2   = "scanned-area-mm2"
  PropertyScanDurationSec  = "scan-duration-seconds"
  PropertyComments         = "comments"

Plus 2 nil-safe helpers:
  Metadata.SetMPPSymmetric()  -- derives plain MPP from per-axis
  Metadata.SetProperty(k, v)  -- lazy-initializes the Properties map

T2-T7 follow with per-format population. T8 lands cross-format
parity test + docs + ship.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — SVS + NDPI populate

**Files:**
- Modify: `formats/svs/metadata.go`, `formats/svs/metadata_test.go`
- Modify: `formats/ndpi/metadata.go`, `formats/ndpi/metadata_test.go`

For each format reader, the implementer must:

1. **Read the existing metadata.go** to understand the current parsing flow.
2. **Probe the actual fixture** to identify which source fields populate which cross-format positions. Do NOT guess — open one of the format's fixtures and walk the metadata source. For SVS, this means reading the ImageDescription TIFF tag (typically `Aperio Image Library v...|key1 = value1|key2 = value2|...` form). For NDPI, this means walking the Hamamatsu-vendor TIFF tags (XResolution / YResolution + custom tags).
3. **Map source → cross-format**:
   - MicronsPerPixelX / Y from the format's MPP source
   - SetMPPSymmetric() after setting per-axis
   - ImageDescription from format's structured description (verbatim TIFF tag for SVS / NDPI)
   - Properties[PropertyCaseNumber] / [PropertyUserName] / etc. from format-specific source where present
   - Vendor-namespaced Properties for non-canonical fields (e.g., `aperio.AppMag`, `aperio.User`, `hamamatsu.SourceLens`)

### SVS specifics

The SVS ImageDescription tag is a `|`-delimited key-value list following an `Aperio Image Library` prefix. Typical keys: `MPP`, `AppMag`, `Date`, `Time`, `User`, `Filename`, etc.

- `MPP` value → `MicronsPerPixelX = MicronsPerPixelY = parsed float`; `SetMPPSymmetric()`
- ImageDescription full string → `cross.ImageDescription`
- `User` value → `Properties[PropertyUserName]` AND `Properties["aperio.User"]` (canonical + vendor)
- `Date`+`Time` → `cross.AcquisitionDateTime` (already populated; verify path still works)
- `AppMag` value → `cross.Magnification` (already populated; verify) AND `Properties["aperio.AppMag"]` (vendor passthrough)

### NDPI specifics

NDPI uses XResolution + YResolution + ResolutionUnit (TIFF standard) for MPP. Hamamatsu-specific tags (e.g., `SourceLens`, `Reference`, `Slide`) populate vendor-namespaced Properties.

- XResolution / YResolution + ResolutionUnit → MicronsPerPixelX / Y (convert per resolution unit; typical NDPI is centimeters → 10000/value microns/pixel)
- SetMPPSymmetric() after
- ImageDescription tag if present → `cross.ImageDescription`
- Hamamatsu vendor tags → `Properties["hamamatsu.<key>"]` for non-canonical surface

### Steps

- [ ] **Step 1: Read each format's metadata.go**

```bash
cd /Users/cornish/GitHub/opentile-go
cat formats/svs/metadata.go
cat formats/ndpi/metadata.go
```

Note the parsing entry point (typically a `parseMetadata` or `buildMetadata` function called once at Open() time) and where it returns the metadata struct.

- [ ] **Step 2: Probe one fixture per format**

```bash
go run /tmp/genericsmoke/main.go sample_files/svs/CMU-1-Small-Region.svs   # if probe script supports metadata dump
go run /tmp/genericsmoke/main.go sample_files/ndpi/CMU-1.ndpi
```

If the probe doesn't dump metadata, write a small inline Go program that prints `tlr.Metadata()` for each fixture.

- [ ] **Step 3: Update svs/metadata.go to populate new fields**

In the existing parse flow, after the current Magnification / ScannerManufacturer / etc. assignments, add:

```go
// v0.17: populate cross-format MPP + ImageDescription + Properties.
md.MicronsPerPixelX = parsedMPP
md.MicronsPerPixelY = parsedMPP
md.SetMPPSymmetric()
md.ImageDescription = rawImageDescription
if userValue != "" {
    md.SetProperty(opentile.PropertyUserName, userValue)
    md.SetProperty("aperio.User", userValue)
}
// ...etc per the SVS-specific mapping
```

The `parsedMPP`, `rawImageDescription`, `userValue` etc. are already computed somewhere in the parse flow — find them and reference. Don't re-parse.

- [ ] **Step 4: Update ndpi/metadata.go similarly**

- [ ] **Step 5: Update tests for both formats**

The existing tests (svs/metadata_test.go, ndpi/metadata_test.go) likely assert on `cross.Magnification`, `cross.ScannerManufacturer`, etc. Add assertions for the new fields:
- `md.MicronsPerPixelX > 0 && md.MicronsPerPixelY > 0`
- `md.MicronsPerPixel > 0` (since SVS/NDPI typically have symmetric pixels)
- `md.ImageDescription != ""`
- Spot-check `md.Properties[opentile.PropertyUserName]` if the fixture has a user

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/svs/ ./formats/ndpi/ 2>&1 | tail -10
gofmt -l formats/svs/ formats/ndpi/
```

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.17): T2 — SVS + NDPI populate cross-format Metadata

SVS: MPP from ImageDescription tag's MPP key (Aperio convention);
ImageDescription verbatim; User → PropertyUserName + aperio.User;
AppMag → aperio.AppMag passthrough.

NDPI: MPP from XResolution + YResolution + ResolutionUnit (TIFF
standard); ImageDescription if present; Hamamatsu vendor tags
surfaced as hamamatsu.<key> Properties.

Both formats now populate MicronsPerPixel + per-axis X/Y +
ImageDescription + canonical/vendor Properties keys.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — Philips TIFF + Generic TIFF populate

**Files:**
- Modify: `formats/philipstiff/metadata.go`, tests
- Modify: `formats/generictiff/tiler.go` (or wherever buildMetadata lives), tests
- Modify: `formats/generictiff/wsitools.go` (extend wsi-tools parser to populate canonical Properties)

### Philips specifics

Philips embeds metadata in a structured ImageDescription with `PIM_DP_X_RESOLUTION` and `PIM_DP_Y_RESOLUTION` keys. Other Philips keys: `PIM_DP_OBJECTIVE_NAME`, `PIM_DP_SCANNER_OPERATOR_ID`, `PIM_DP_SCANNER_RACK_ID`, etc.

- `PIM_DP_X_RESOLUTION` / `PIM_DP_Y_RESOLUTION` → MPP X / Y; SetMPPSymmetric()
- ImageDescription tag verbatim → `cross.ImageDescription`
- `PIM_DP_SCANNER_OPERATOR_ID` → `Properties[PropertyUserName]` + `Properties["philips.PIM_DP_SCANNER_OPERATOR_ID"]`
- Other PIM_DP_* keys → `Properties["philips.<key>"]` passthrough

### Generic TIFF specifics

Generic TIFF's existing v0.10 buildMetadata reads standard TIFF tags. v0.14's wsi-tools parser (formats/generictiff/wsitools.go) adds wsi-tools-specific extensions (mpp / mag / scanner / date).

- v0.10 path: `XResolution + ResolutionUnit` → MicronsPerPixelX / Y (already partially done; expand to per-axis and call SetMPPSymmetric)
- v0.14 wsi-tools path: `mpp=<float>` → MicronsPerPixelX = MicronsPerPixelY; SetMPPSymmetric. wsi-tools `scanner` already populates ScannerManufacturer; verify still works.
- `cross.ImageDescription` from the raw TIFF ImageDescription (verbatim, regardless of wsi-tools prefix)
- Properties: wsi-tools fixtures may not have CaseNumber / UserName / etc.; populate where source has them.

### Steps

- [ ] **Step 1: Read existing files**

```bash
cat formats/philipstiff/metadata.go
cat formats/generictiff/tiler.go | grep -A 60 buildMetadata
cat formats/generictiff/wsitools.go
```

- [ ] **Step 2-7: Same pattern as T2** — probe, map, populate, test, gofmt, commit.

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.17): T3 — Philips TIFF + Generic TIFF populate cross-format Metadata

Philips: PIM_DP_X_RESOLUTION + Y → MPP; PIM_DP_SCANNER_OPERATOR_ID
→ PropertyUserName + philips.PIM_DP_SCANNER_OPERATOR_ID;
ImageDescription verbatim.

Generic TIFF: XResolution + ResolutionUnit derivation now sets
MicronsPerPixelX/Y separately + SetMPPSymmetric(); v0.14 wsi-tools
parser extended to populate canonical Properties where source has
them.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — OME-TIFF populate

**Files:**
- Modify: `formats/ometiff/metadata.go`, tests
- Possibly modify: `formats/ometiff/tiler.go` if metadata wiring changes

### OME-TIFF specifics

OME-XML embeds metadata in `<Image>` elements:
- `<Image><Pixels PhysicalSizeX="0.25" PhysicalSizeY="0.25" PhysicalSizeXUnit="µm" PhysicalSizeYUnit="µm" />` — per-axis MPP. Convert from `PhysicalSizeXUnit` (typically "µm" or "nm") to microns.
- `<Image Description="...">` — structured description → `cross.ImageDescription`
- `<Image><AcquisitionDate>...</AcquisitionDate>` — already populates cross.AcquisitionDateTime; verify
- `<Image><Experimenter>...</Experimenter>` → `Properties[PropertyUserName]` if present
- Other OME-XML elements: `Properties["ome.<key>"]` passthrough on a case-by-case basis (probe to identify what's commonly present)

### Steps

Same pattern as T2/T3. Probe `sample_files/ome-tiff/Leica-1.ome.tiff` and `Leica-2.ome.tiff` to confirm which OME-XML elements are present.

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.17): T4 — OME-TIFF populate cross-format Metadata

OME-XML <Pixels PhysicalSizeX/Y> → MicronsPerPixelX/Y +
SetMPPSymmetric(); <Image Description> → cross.ImageDescription;
<Image Experimenter> → PropertyUserName when present.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — BIF + IFE populate

**Files:**
- Modify: `formats/bif/metadata.go`, tests
- Modify: `formats/ife/metadata.go`, tests

### BIF specifics

Ventana BIF stores metadata in TIFF tags + an XML metadata block. Per-axis MPP comes from the iScan-specific XML.

- Per-axis MPP from BIF XML → MicronsPerPixelX / Y; SetMPPSymmetric()
- ImageDescription if present
- Vendor tags → `Properties["ventana.<key>"]`

### IFE specifics

Iris IFE has format-defined fields per its spec. Read `formats/ife/metadata.go` (590 LOC — substantial) to identify the spec-defined sources.

- IFE-spec MPP fields → MicronsPerPixelX / Y
- IFE-spec description → cross.ImageDescription
- IFE-spec attributes → `Properties["iris.<key>"]` for non-canonical

### Steps

Same pattern. Read each format's existing metadata.go in detail before editing.

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.17): T5 — BIF + IFE populate cross-format Metadata

BIF: per-axis MPP from iScan XML metadata; ImageDescription;
ventana.<key> vendor passthrough.

IFE: spec-defined MPP fields → per-axis + SetMPPSymmetric();
spec-defined description → cross.ImageDescription;
iris.<key> vendor passthrough for non-canonical fields.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Leica SCN populate

**Files:**
- Create: `formats/leicascn/metadata.go` (new — currently no separate file)
- Modify: `formats/leicascn/tiler.go` (currently returns `opentile.Metadata{}`)
- Possibly modify: `formats/leicascn/leicascn_test.go`

### SCN specifics

SCN-XML embeds per-region scale info in `<view scale="0.000250" />` elements (nm/pixel). Some SCN files have multiple regions; cross-format Metadata reports the primary (region 0) scale.

- `<view scaleX scaleY>` → MicronsPerPixelX = scaleX × 1e3 (nm → µm); same for Y; SetMPPSymmetric()
- SCN-XML root description → cross.ImageDescription if present
- SCN-XML auxiliary metadata → `Properties["leica.<key>"]` passthrough

This is a NEW metadata implementation (the format currently returns empty). The implementer should:
1. Read `formats/leicascn/scnxml.go` to understand the SCN-XML schema as parsed
2. Identify where to extract the relevant scale + description fields
3. Wire into tiler.go's Metadata() return path

### Steps

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.17): T6 — Leica SCN populate cross-format Metadata

leicascn.Tiler.Metadata() previously returned an empty struct
(formats/leicascn/tiler.go). v0.17 wires the SCN-XML primary
view scale → MicronsPerPixelX/Y + SetMPPSymmetric; the SCN-XML
description (where present) → cross.ImageDescription; auxiliary
SCN-XML fields → leica.<key> Properties.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T7 — SZI cleanup (Option B)

**Files:**
- Modify: `formats/szi/metadata.go`
- Modify: `formats/szi/metadata_test.go`

Per Q4 Option B, strip cross-format-canonical duplicates from `szi.Metadata`; keep raw native fields + the spec-defined `VendorProperties` map.

- [ ] **Step 1: Strip duplicates**

Edit `/Users/cornish/GitHub/opentile-go/formats/szi/metadata.go`. Remove these fields from the `Metadata` struct:
- `MicronsPerPixel` (now via embedded opentile.Metadata)
- `MicronsPerPixelX`
- `MicronsPerPixelY`
- `Comments` (now via Properties[PropertyComments])
- `UserName` (now via Properties[PropertyUserName])
- `CaseNumber` (now via Properties[PropertyCaseNumber])
- `ScannedArea` (now via Properties[PropertyScannedAreaMM2])

Keep:
- `Version`, `Date` (SZI format meta)
- `SoftwareName`, `SoftwareVersion` (already populates ScannerSoftware; format-specific raw forms preserved)
- `TimeStart`, `TimeEnd` (TimeStart populates AcquisitionDateTime; raw timestamps preserved here)
- `ElapsedTime` (raw "0h17m22s" string preserved; cross-format Properties[PropertyScanDurationSec] is the parsed-seconds form)
- `ScanJobName`, `ScannerSerialNo`, `CameraName`, `SensorPixelSize`, `ScanWidth`, `ScanHeight`, `VendorProperties`

Embedded `opentile.Metadata` continues providing the typed cross-format fields via field promotion.

- [ ] **Step 2: Update parseScanProperties**

The parseScanProperties function (currently populates szi.Metadata fields directly) now populates the embedded opentile.Metadata + szi.Metadata raw fields:

```go
// v0.17: populate cross-format fields on the embedded opentile.Metadata.
case "MicronsPerPixelX":
    if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
        cross.MicronsPerPixelX = f
    }
case "MicronsPerPixelY":
    if f, e := strconv.ParseFloat(p.Value, 64); e == nil {
        cross.MicronsPerPixelY = f
    }
case "MicronsPerPixel":
    // The single-MPP value, if present, populates per-axis when
    // X/Y aren't separately specified. Don't pre-set
    // cross.MicronsPerPixel directly — SetMPPSymmetric handles
    // the X==Y case at the end.
    if f, e := strconv.ParseFloat(p.Value, 64); e == nil && cross.MicronsPerPixelX == 0 {
        cross.MicronsPerPixelX = f
        cross.MicronsPerPixelY = f
    }
case "Comments":
    cross.SetProperty(opentile.PropertyComments, p.Value)
    // v0.17: removed szi.Comments; raw value lives only in the cross-format Properties map.
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
```

After all fields parsed, call `cross.SetMPPSymmetric()` once at the end.

For VendorProperties: keep populating `szim.VendorProperties[p.Name] = p.Value` for vendor.X keys. Also surface them as `cross.SetProperty("szi.vendor."+strippedName, p.Value)` if you want cross-format consumers to see them — but the szi.Metadata.VendorProperties stays as the canonical source per Q4.

- [ ] **Step 3: parseSZIDuration helper**

In `formats/szi/metadata.go`, add:

```go
// parseSZIDuration parses the SZI ElapsedTime format ("XhYmZs",
// e.g., "0h17m22s") and returns total seconds. Returns 0, false
// on malformed input.
func parseSZIDuration(s string) (float64, bool) {
	var hours, minutes, seconds float64
	if _, err := fmt.Sscanf(s, "%fh%fm%fs", &hours, &minutes, &seconds); err != nil {
		return 0, false
	}
	return hours*3600 + minutes*60 + seconds, true
}
```

Add `import "fmt"` if not already imported.

- [ ] **Step 4: Update szi tests**

The existing tests assert on `szim.MicronsPerPixel` etc. Update to assert on the embedded `cross.MicronsPerPixelX/Y` + `cross.MicronsPerPixel` (after SetMPPSymmetric) instead. Spot-check `cross.Properties[opentile.PropertyComments]` etc.

Tests for parseSZIDuration: happy path ("0h17m22s" → 1042), malformed → (0, false), zero ("0h0m0s" → 0).

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/szi/ 2>&1 | tail -10
gofmt -l formats/szi/
```

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
refactor(v0.17): T7 — szi.Metadata cleanup (Option B)

Per Q4 Option B, strip cross-format-canonical duplicates from
szi.Metadata; keep raw native fields:

REMOVED from szi.Metadata (now via embedded opentile.Metadata):
  MicronsPerPixel, MicronsPerPixelX/Y, Comments, UserName,
  CaseNumber, ScannedArea

KEPT (format-specific or raw representations):
  Version, Date, SoftwareName, SoftwareVersion, TimeStart, TimeEnd,
  ElapsedTime (raw "0h17m22s" string; cross-format
  Properties["scan-duration-seconds"] is the parsed-seconds form),
  ScanJobName, ScannerSerialNo, CameraName, SensorPixelSize,
  ScanWidth, ScanHeight, VendorProperties (SZI-spec convention)

parseScanProperties() populates the embedded opentile.Metadata
fields; SetMPPSymmetric() called once at end. parseSZIDuration()
parses SZI's "XhYmZs" elapsed-time format to seconds.

Field reads via promotion still work: szi.MetadataOf(t).MicronsPerPixel
continues to compile and return the embedded value. Narrow break
only for struct-literal construction of szi.Metadata.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T8 — Cross-format parity test + docs + ship

**Files:**
- Create: `tests/parity/cross_format_metadata_test.go`
- Modify: `docs/formats/svs.md`, `docs/formats/ndpi.md`, `docs/formats/philipstiff.md`, `docs/formats/ometiff.md`, `docs/formats/bif.md`, `docs/formats/ife.md`, `docs/formats/leicascn.md`, `docs/formats/generictiff.md`, `docs/formats/szi.md` (each gets a v0.17 cross-format-mapping note)
- Modify: `docs/deferred.md` (§8k retirement audit; R20 retired)
- Modify: `CHANGELOG.md` ([0.17.0])
- Modify: `CLAUDE.md` (milestone bump)

- [ ] **Step 1: Write `tests/parity/cross_format_metadata_test.go`**

```go
package parity

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// crossFormatMetadataExpect describes what each format reader must
// populate on cross-format opentile.Metadata. Per-fixture geometry
// stays in format-specific geometry tests; this gate is for the
// cross-format API surface only.
type crossFormatMetadataExpect struct {
	fixture       string  // path under OPENTILE_TESTDIR
	wantMagnification bool  // true if format reports
	wantMPPPerAxis    bool  // true if format reports per-axis MPP
	wantMPPSymmetric  bool  // true if X==Y for this fixture
	wantImageDesc     bool  // true if format populates ImageDescription
	wantUserName      bool  // true if fixture has a user/operator
	// Add other expectations as fixtures and formats demand.
}

var cfmExpect = []crossFormatMetadataExpect{
	{fixture: "svs/CMU-1-Small-Region.svs", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantImageDesc: true},
	{fixture: "ndpi/CMU-1.ndpi", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantImageDesc: true},
	{fixture: "philips-tiff/Philips-1.tiff", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantImageDesc: true},
	{fixture: "ome-tiff/Leica-1.ome.tiff", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantImageDesc: true},
	{fixture: "bif/Ventana-1.bif", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true},
	{fixture: "ife/cervix_2x_jpeg.iris", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true},
	{fixture: "scn/Leica-1.scn", wantMPPPerAxis: true, wantMPPSymmetric: true},
	{fixture: "generic-tiff/avif-out.tiff", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantImageDesc: true},
	{fixture: "szi/CMU-1.szi", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true, wantUserName: true},
	{fixture: "szi/scan_618_grundium_SZI.szi", wantMagnification: true, wantMPPPerAxis: true, wantMPPSymmetric: true},
}

func TestCrossFormatMetadata(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	for _, fx := range cfmExpect {
		t.Run(fx.fixture, func(t *testing.T) {
			path := filepath.Join(dir, fx.fixture)
			if _, err := os.Stat(path); err != nil {
				t.Skip(fx.fixture + " not present")
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
				if md.MicronsPerPixelX == 0 || md.MicronsPerPixelY == 0 {
					t.Errorf("MicronsPerPixelX/Y = %v/%v; want both > 0",
						md.MicronsPerPixelX, md.MicronsPerPixelY)
				}
			}
			if fx.wantMPPSymmetric && fx.wantMPPPerAxis {
				if md.MicronsPerPixel == 0 {
					t.Errorf("MicronsPerPixel = 0; want > 0 (X==Y on this fixture)")
				}
			}
			if fx.wantImageDesc && md.ImageDescription == "" {
				t.Errorf("ImageDescription empty; want non-empty")
			}
			if fx.wantUserName {
				if got := md.Properties[opentile.PropertyUserName]; got == "" {
					t.Errorf("Properties[PropertyUserName] empty; want non-empty")
				}
			}
		})
	}
}
```

Adapt the `cfmExpect` table during implementation: probe each format's actual fixture to confirm whether each `want*` field is populated. The probe-based-truth approach mirrors the v0.10/v0.14 geometry test pattern.

- [ ] **Step 2: Per-format docs sweep**

For each `docs/formats/<fmt>.md`, add a small "Cross-format Metadata mapping (v0.17)" section listing the source-field → cross-format-position table for that format. Concise: 3-5 rows per format.

Example for `docs/formats/szi.md`:

```markdown
## Cross-format Metadata mapping (v0.17)

| scan-properties.xml field | cross-format Metadata position |
|---|---|
| `VendorName` | `Metadata.ScannerManufacturer` |
| `ScannerName` | `Metadata.ScannerModel` |
| `ObjectiveMagnification` | `Metadata.Magnification` |
| `MicronsPerPixelX/Y` | `Metadata.MicronsPerPixelX/Y` (and `Metadata.MicronsPerPixel` when X==Y) |
| `Comments` | `Metadata.Properties["comments"]` |
| `UserName` | `Metadata.Properties["user-name"]` |
| `CaseNumber` | `Metadata.Properties["case-number"]` |
| `ElapsedTime` | `Metadata.Properties["scan-duration-seconds"]` (parsed) + raw `szi.MetadataOf(t).ElapsedTime` |
| `vendor.<key>` | `szi.MetadataOf(t).VendorProperties[<key>]` (preserved per SZI spec) |
```

Adapt for each format; the per-format mapping was sealed during T2-T7.

- [ ] **Step 3: docs/deferred.md §8k**

Insert §8k BEFORE §8j (newest-first):

```markdown
## 8k. Retired in v0.17

v0.17 closes R20 — cross-format `opentile.Metadata` expansion.
Hybrid approach: typed additions for what OpenSlide standardizes
(MicronsPerPixel + per-axis X/Y; ImageDescription); flat
`Properties map[string]string` for opentile-go-canonical extensions
(case-number, user-name, scanned-area-mm2, scan-duration-seconds,
comments) and vendor-namespaced passthrough.

**Items shipped:**

- 4 new typed fields on `opentile.Metadata`: `MicronsPerPixel`,
  `MicronsPerPixelX`, `MicronsPerPixelY`, `ImageDescription`
- `Properties map[string]string` for canonical + vendor-namespaced
  extensions
- 5 canonical key constants: `PropertyCaseNumber`, `PropertyUserName`,
  `PropertyScannedAreaMM2`, `PropertyScanDurationSec`, `PropertyComments`
- 2 helper methods: `Metadata.SetMPPSymmetric()`, `Metadata.SetProperty()`
- Every format reader populates the new fields where source data is
  present (SVS, NDPI, Philips, OME-TIFF, BIF, IFE, Leica SCN
  (first-time population), Generic TIFF, SZI)
- Format-specific Metadata structs cleaned up per Q4 Option B:
  cross-format-canonical duplicates removed; raw native
  representations preserved (e.g., SZI's ElapsedTime "0h17m22s"
  string + VendorProperties for SZI-spec convention)
- New `tests/parity/cross_format_metadata_test.go` exercises every
  format's cross-format Metadata population

**Architecture invariants preserved:**

- Public API additive on cross-format struct (existing consumers
  unaffected); narrow break only for struct-literal construction
  of format-specific Metadata structs (struct field reads via
  embedded promotion still work).
- OpenSlide flat-property convention mirrored for the property bag.
- Python opentile precedent for typed cross-format fields preserved
  (Magnification, ScannerManufacturer, etc., and now
  ImageDescription and MPP).
- v0.16 SZI architecture validated: szi.Metadata's previous
  cross-format duplicates now flow through embedded opentile.Metadata
  cleanly.
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R20 (cross-format Metadata gap) — landed.

**Still parked:**

- R19 (bare DZI) — internal/dzi pre-pares it.
- Other §11 backlog items (L19, L20, L23-L25, L26-L29, L30-L34,
  R4/R6/R9, R15) unchanged.

**v0.17 lessons:** the format-specific-struct-cleanup pattern
(Option B from sealed Q4) is generalizable to future cross-format
API expansions. When a new field becomes cross-format, format
packages strip the typed duplicate but preserve raw/native
representations; field promotion keeps consumer code working
without the cross-format-only refactoring break.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md`](superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md).
```

Also update §11 backlog table: mark R20 retired (annotation pattern matches R16/R18 from prior milestones).

- [ ] **Step 4: CHANGELOG [0.17.0]**

Insert before [0.16.0]:

```markdown
## [0.17.0] — 2026-05-09

Cross-format Metadata expansion — closes R20. Typed additions
(MicronsPerPixel + per-axis X/Y; ImageDescription) plus a flat
Properties map[string]string for opentile-go-canonical extensions
and vendor-namespaced passthrough. Mirrors OpenSlide's flat-
property convention where it's standard; falls back to typed
fields for the well-precedented WSI cross-cutting fields.

### Added

- 4 new typed fields on `opentile.Metadata`:
  - `MicronsPerPixel float64` (populated when X == Y; zero
    otherwise)
  - `MicronsPerPixelX float64`
  - `MicronsPerPixelY float64`
  - `ImageDescription string` (structured per-format description)
- `Properties map[string]string` for additional cross-format
  metadata. Two key conventions:
  - opentile-go-canonical (lowercase-with-hyphens): see new
    constants below
  - vendor-namespaced (`<format>.<key>`): vendor-specific fields
    surfaced as-is, e.g., `aperio.AppMag`, `philips.PIM_DP_*`,
    `szi.vendor.<key>`
- 5 canonical key constants:
  - `opentile.PropertyCaseNumber = "case-number"`
  - `opentile.PropertyUserName = "user-name"`
  - `opentile.PropertyScannedAreaMM2 = "scanned-area-mm2"`
  - `opentile.PropertyScanDurationSec = "scan-duration-seconds"`
  - `opentile.PropertyComments = "comments"`
- 2 helper methods:
  - `Metadata.SetMPPSymmetric()` — derives plain MPP from per-axis
    when X == Y (strict equality)
  - `Metadata.SetProperty(key, value string)` — nil-safe Properties
    setter (lazily initializes the map)
- New `tests/parity/cross_format_metadata_test.go` cross-format
  metadata parity gate (one fixture per format, asserts the
  expected populated fields).

### Changed

- Every format reader (SVS, NDPI, Philips, OME-TIFF, BIF, IFE,
  Leica SCN, Generic TIFF, SZI) now populates the new typed fields
  + canonical Properties keys where source data is present.
- Format-specific Metadata structs (`szi.Metadata`, etc.) lose
  cross-format-canonical duplicates per Q4 Option B; raw native
  representations preserved (e.g., SZI's `ElapsedTime` string).
- `leicascn.Tiler.Metadata()` now populates from SCN-XML view scale
  (was previously empty).

### Notes

- **No break for existing consumers reading via Tiler.Metadata().**
  New fields default to zero values; existing typed fields
  (Magnification, ScannerManufacturer, etc.) unchanged.
- **Narrow break for struct-literal construction of format-specific
  Metadata structs.** E.g., `szi.Metadata{MicronsPerPixel: 0.4}` no
  longer compiles — set on the embedded opentile.Metadata instead
  (`szi.Metadata{Metadata: opentile.Metadata{MicronsPerPixel: 0.4}}`),
  or use `SetMPPSymmetric()` from the per-axis fields. Surface is
  narrow — mostly internal/test code.
- **Hybrid design rationale:** typed fields land at OpenSlide's
  precedent (MPP, comment); Properties map handles opentile-go
  originals (case-number, user-name, etc.). See spec §1 for the
  authority audit comparing OpenSlide / Python opentile.
- v1.0 cut still pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump from "after v0.16" → "after v0.17."

- [ ] **Step 5: CLAUDE.md milestone bump**

Replace `## Current milestone — v0.16` block:

```markdown
## Current milestone — v0.17 (shipped)

- **Scope:** Cross-format Metadata expansion (R20). Hybrid: typed
  additions for what OpenSlide standardizes (MicronsPerPixel +
  per-axis X/Y; ImageDescription) + Properties map[string]string
  for opentile-go-canonical extensions (case-number, user-name,
  scanned-area-mm2, scan-duration-seconds, comments) and vendor-
  namespaced passthrough. Every format reader updated to populate
  the new fields. Closes R20 from deferred backlog. 8 plan tasks
  single batch.
- **API additions:** 4 typed Metadata fields (MicronsPerPixel,
  MicronsPerPixelX/Y, ImageDescription); Properties map; 5
  canonical key constants; 2 helper methods (SetMPPSymmetric,
  SetProperty).
- **API breaks:** narrow — struct-literal construction of format-
  specific Metadata structs (e.g., szi.Metadata{MicronsPerPixel:
  ...}) no longer compiles for fields moved to embedded
  opentile.Metadata. Field reads via embedded promotion continue
  to work without source change.
- **Active limitations:** unchanged from v0.16. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  docs/deferred.md §1a): unchanged. New typed fields (MPP,
  ImageDescription) align with OpenSlide; the Properties map +
  opentile-go-canonical extensions are opentile-go originals.
- **Correctness bar:** make test green; TestSlideParity 30 fixtures
  unchanged from v0.16; new TestCrossFormatMetadata exercises every
  format's cross-format Metadata population.
- **Sealed Q-decisions (8):** Q1 hybrid; Q2 smart MPP (X==Y only);
  Q3 lowercase-with-hyphens canonical keys + vendor.<key>
  namespacing; Q4 Option B (strip duplicates; keep raw native);
  Q5 strings throughout; Q6 missing = key absent; Q7 v0.17.0;
  Q8 keep all per-format MetadataOf accessors.
- **Deferred forward:** R19 (bare DZI), L19, L20, L23-L25, L26-L29,
  L30-L34, R4/R6/R9, R15. v1.0 cut still pending.
- **Design:** docs/superpowers/specs/2026-05-09-opentile-go-v17-cross-format-metadata-design.md
- **Plan:** docs/superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md
- **Work branch:** feat/v0.17

## Previous milestone — v0.16 (shipped 2026-05-09)

Smart Zoom Image (SZI) reader. Closes R18. New formats/szi/ +
internal/dzi/ packages; opentile.FormatSZI + opentile.CompressionPNG
enums; mmap-aliased ZIP-entry tile fetch; szi.MetadataOf accessor
+ szi.Metadata with VendorProperties map; 2 fixtures (CMU-1.szi
+ scan_618_grundium 709 MB) wired into TestSlideParity (28 → 30).
Bare DZI (R19) still parked but pre-prepared via internal/dzi.
```

- [ ] **Step 6: Final verification gate**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: vet clean, gofmt clean, every package green, TestSlideParity 30 fixtures green, new TestCrossFormatMetadata green.

- [ ] **Step 7: Commit**

```bash
git add docs/formats/ docs/deferred.md CHANGELOG.md CLAUDE.md tests/parity/cross_format_metadata_test.go
git commit -m "$(cat <<'EOF'
docs(v0.17): T8 — cross-format parity test + per-format docs + CHANGELOG + CLAUDE.md

tests/parity/cross_format_metadata_test.go: new — exercises every
format's cross-format Metadata population gate (one fixture per
format; asserts MagnificationKnown / MPPPerAxis / MPPSymmetric /
ImageDescription / canonical Properties as appropriate per format).

docs/formats/{svs,ndpi,philipstiff,ometiff,bif,ife,leicascn,
generictiff,szi}.md: each gets a v0.17 cross-format Metadata
mapping section showing source-field → cross-format-position table.

docs/deferred.md §8k new — Retired in v0.17: closes R20; format-
specific struct cleanup pattern (Option B) generalized for future
cross-format expansions.

CHANGELOG.md [0.17.0]: 4 typed additions + Properties map + 5
canonical key constants + 2 helpers; per-format population summary;
narrow-break call-out for struct-literal construction of format-
specific Metadata structs.

CLAUDE.md: bump Current milestone v0.16 → v0.17. v0.16 demoted to
Previous; v0.15 / v0.14 / earlier collapsed.

End of milestone; v0.17 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (struct expansion) → T1.
- §1.2 (per-format population) → T2-T7.
- §1.3 (format-specific struct cleanup, Option B) → T7 (SZI explicit; T2-T6 audit per format).
- §1.4 (consumer API) → no new accessor; field promotion handles it.
- §3 (Q-decisions) → reflected throughout.
- §5 (test strategy) → T1 unit tests + T2-T7 per-format test updates + T8 cross-format parity test.
- §6 (no new limitations) → T8 docs confirm.
- §7 (plan outline) → matches.
- §8 (verification gates) → T8 step 6.

**Placeholder scan:** Per-format mapping details (T2-T6 specifics) are sketched; the implementer is instructed to PROBE each format's actual fixture before locking in the source field names. The wsi-tools-style "vendor passthrough" Properties keys are illustrative; the implementer audits per format. T8 step 1's `cfmExpect` table is illustrative; the implementer adjusts based on probe-confirmed truth.

**Type consistency:** `Metadata`, `Properties`, `SetMPPSymmetric`, `SetProperty`, `Property*` constants used identically across T1 → T8.

**Risks:**

- **R1 — Per-format mapping discovery cost.** T2-T6 each require reading the existing format's metadata.go AND probing fixtures to identify source fields. This is exploratory work the implementer must do; the plan provides scaffolding but not full mappings. Mitigation: each task is one or two formats only; reading + probing for one format per task is bounded.
- **R2 — leicascn first-time metadata population.** Currently `leicascn.Tiler.Metadata()` returns empty. T6 is the first time SCN populates anything. Cross-format parity test (T8) will surface gaps; iterate if needed.
- **R3 — Format-specific test breakage.** Existing per-format tests may assert on fields that are removed (e.g., `szim.MicronsPerPixel`). T7 (SZI) explicitly addresses; T2-T6 implementers must check + update.
- **R4 — TestSlideParity unchanged.** SHA fixtures pin tile bytes, not metadata. v0.17 doesn't regenerate fixtures. Verify TestSlideParity stays green throughout.
