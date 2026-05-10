# opentile-go v0.18 — SVS writer-vendor detection implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the misattribution bug where SVS files written by non-Aperio scanners (Grundium observed) get `ScannerManufacturer = "Aperio"`. Detect actual writer-vendor; namespace Properties keys per detected writer; document supported writers explicitly. Audit OME-TIFF for similar risk; ship docs-only clarification.

**Architecture:** 3 tasks single batch. T1 lands the SVS writer-detection. T2 audits OME-TIFF (likely docs-only). T3 closes with docs sweep + ship.

**Tech stack:** Go 1.23+; existing `formats/svs/metadata.go` (parseDescription) + `formats/ometiff/metadata.go`.

**Spec:** [`docs/superpowers/specs/2026-05-09-opentile-go-v18-svs-writer-detection-design.md`](../specs/2026-05-09-opentile-go-v18-svs-writer-detection-design.md).

**Existing pattern audited:**
- `formats/svs/metadata.go::parseDescription` is the entry point. Lines 50/55 hardcode `md.ScannerManufacturer = "Aperio"` — the bug.
- `splitKV(body)` parses the pipe-delimited key-value pairs.
- Properties keys currently namespace `aperio.<key>` unconditionally for every kv (line 100).
- `md.ScannerSoftware = []string{md.SoftwareLine}` jams the entire first line as a single software identifier (line 56).
- The Grundium fixtures' first line is `"Aperio Image, Grundium Ocus"`; the post-comma part `"Grundium Ocus"` should split into manufacturer "Grundium" + model "Ocus".

---

## Task layout

3 tasks, single batch:

- T1 — SVS writer-vendor detection in `formats/svs/metadata.go` + unit tests + Grundium fixture parity test updates + fixture JSON updates
- T2 — OME-TIFF audit (likely docs-only)
- T3 — `docs/formats/svs.md` "Recognized SVS writers" section + CHANGELOG `[0.18.0]` + CLAUDE.md milestone bump + `docs/deferred.md §8l` retirement audit

---

## T1 — SVS writer-vendor detection

**Files:**
- Modify: `formats/svs/metadata.go` (new `detectWriter` function; modify `parseDescription`)
- Modify: `formats/svs/metadata_test.go` (unit tests on detectWriter; integration tests on Grundium fixtures)
- Modify: `tests/fixtures/scan_620_.svs.json`, `tests/fixtures/svs_40x_bigtiff.svs.json` (update `metadata.scanner_manufacturer` from "Aperio" → "Grundium")

- [ ] **Step 1: Add `detectWriter` function**

In `formats/svs/metadata.go`, before `parseDescription`:

```go
// writerVendor describes the SVS writer detected from the
// ImageDescription first line. SVS is the WSI ad-hoc standard and
// is written by multiple vendors (Aperio canonical, Grundium,
// likely 3DHistech via export). The first line of the
// ImageDescription names the writer:
//
//   "Aperio Image Library v11.2.1"     → Aperio canonical
//   "Aperio Image, Grundium Ocus"      → Grundium-written; "Ocus" model
//   "Aperio Image, <other>"            → other writer named in suffix
//   anything else                       → undetected; format-default fallback
//
// When undetected, the parser falls back to the "svs" namespace
// for Properties keys; standardized SVS-format keys (MPP, AppMag,
// ScanScope ID) still populate cross-format Metadata regardless.
type writerVendor struct {
	manufacturer string // e.g., "Aperio", "Grundium", "" if undetected
	model        string // e.g., "Ocus", "" if not declared
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
```

- [ ] **Step 2: Modify parseDescription to use detected writer**

Find lines 46-56 of the current parseDescription (the "Split off the software banner" + hardcoded "Aperio" assignments). Replace with:

```go
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
```

- [ ] **Step 3: Modify Properties namespace for writer-detection**

In the existing `for k, v := range kv` loop (around line 96-101), replace the hardcoded `"aperio."` prefix with the writer-detected namespace:

```go
	// Cross-format vendor passthrough: surface every kv under the
	// detected writer's namespace. Falls back to "svs" if writer
	// was not detected.
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
```

The `w` variable from Step 2 is reused; no need to re-detect.

- [ ] **Step 4: Add unit tests for detectWriter**

Append to `formats/svs/metadata_test.go`:

```go
func TestDetectWriter(t *testing.T) {
	for _, tc := range []struct {
		name             string
		input            string
		wantManufacturer string
		wantModel        string
		wantSoftwares    []string
	}{
		{
			name:             "Aperio canonical",
			input:            "Aperio Image Library v11.2.1",
			wantManufacturer: "Aperio",
			wantModel:        "",
			wantSoftwares:    []string{"Aperio Image Library v11.2.1"},
		},
		{
			name:             "Grundium Ocus (observed)",
			input:            "Aperio Image, Grundium Ocus",
			wantManufacturer: "Grundium",
			wantModel:        "Ocus",
			wantSoftwares:    []string{"Aperio Image", "Grundium Ocus"},
		},
		{
			name:             "Grundium with whitespace",
			input:            "  Aperio Image,  Grundium Ocus  ",
			wantManufacturer: "Grundium",
			wantModel:        "Ocus",
			wantSoftwares:    []string{"Aperio Image", "Grundium Ocus"},
		},
		{
			name:             "Hypothetical multi-word model",
			input:            "Aperio Image, MyVendor Pro 5",
			wantManufacturer: "MyVendor",
			wantModel:        "Pro 5",
			wantSoftwares:    []string{"Aperio Image", "MyVendor Pro 5"},
		},
		{
			name:             "Empty input",
			input:            "",
			wantManufacturer: "",
			wantModel:        "",
			wantSoftwares:    nil,
		},
		{
			name:             "Undetected pattern",
			input:            "SomethingElse v2.0",
			wantManufacturer: "",
			wantModel:        "",
			wantSoftwares:    []string{"SomethingElse v2.0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := detectWriter(tc.input)
			if got.manufacturer != tc.wantManufacturer {
				t.Errorf("manufacturer = %q, want %q", got.manufacturer, tc.wantManufacturer)
			}
			if got.model != tc.wantModel {
				t.Errorf("model = %q, want %q", got.model, tc.wantModel)
			}
			if !slicesEqual(got.softwares, tc.wantSoftwares) {
				t.Errorf("softwares = %v, want %v", got.softwares, tc.wantSoftwares)
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

(If `slicesEqual` already exists in the test file or in a stdlib import, reuse rather than re-declare.)

- [ ] **Step 5: Update existing fixture-driven Metadata tests**

The existing `formats/svs/metadata_test.go` likely has tests asserting Aperio behavior on Aperio fixtures. Add or extend tests for the Grundium fixtures:

```go
func TestParseDescription_GrundiumFixture(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "scan_620_.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skip("scan_620_.svs not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()
	md := tlr.Metadata()
	if md.ScannerManufacturer != "Grundium" {
		t.Errorf("ScannerManufacturer = %q, want Grundium", md.ScannerManufacturer)
	}
	if md.ScannerModel != "Ocus" {
		t.Errorf("ScannerModel = %q, want Ocus", md.ScannerModel)
	}
	if got := md.Properties["grundium.MPP"]; got == "" {
		t.Errorf("Properties[grundium.MPP] empty; want non-empty (Grundium namespace)")
	}
	if got := md.Properties["aperio.MPP"]; got != "" {
		t.Errorf("Properties[aperio.MPP] = %q, want empty (writer is Grundium not Aperio)", got)
	}
}
```

- [ ] **Step 6: Update fixture JSON parity files**

The TestSlideParity SHA-fixture JSONs pin `metadata.scanner_manufacturer`. For Grundium fixtures it currently says "Aperio"; needs to flip to "Grundium":

```bash
grep -n '"scanner_manufacturer"' tests/fixtures/scan_620_.svs.json tests/fixtures/svs_40x_bigtiff.svs.json
```

Update each from `"Aperio"` to `"Grundium"`. Other pinned fields (mpp, magnification, etc.) should be unaffected.

If `scanner_model` is also pinned (and was empty), update to "Ocus".

- [ ] **Step 7: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/svs/ ./tests/ 2>&1 | tail -10
gofmt -l formats/svs/
```

Re-probe to confirm AFTER state:

```bash
go run /tmp/svsmeta.go sample_files/svs/scan_620_.svs 2>&1 | head -20
go run /tmp/svsmeta.go sample_files/svs/CMU-1-Small-Region.svs 2>&1 | head -20
```

Expected: Grundium fixtures show `ScannerManufacturer: "Grundium"`, `ScannerModel: "Ocus"`, Properties keys under `grundium.<key>`. Aperio fixtures unchanged (`ScannerManufacturer: "Aperio"`, Properties keys under `aperio.<key>`).

- [ ] **Step 8: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
fix(v0.18): T1 — SVS writer-vendor detection (closes Grundium misattribution)

Adds detectWriter() to formats/svs/metadata.go's parseDescription
flow. Parses the ImageDescription first line to identify the
actual writer:

  "Aperio Image Library v..."     → Aperio canonical
  "Aperio Image, Grundium Ocus"   → Grundium-written; model "Ocus"
  "Aperio Image, <other>"         → comma-suffix writer
  anything else                    → undetected; "svs" Properties fallback

Bug fixed: pre-v0.18, all SVS files got ScannerManufacturer="Aperio"
because the parser hardcoded the format-vendor as the writer-vendor.
Grundium SVS fixtures (scan_620_.svs + svs_40x_bigtiff.svs) now
correctly report ScannerManufacturer="Grundium", ScannerModel="Ocus".

Properties keys namespace under the detected writer:
  - aperio.MPP for Aperio-written
  - grundium.MPP for Grundium-written
  - svs.<key> for undetected (format-default fallback)

ScannerSoftware split sensibly:
  - Pre-v0.18: ["Aperio Image, Grundium Ocus"] (single jammed string)
  - Post-v0.18: ["Aperio Image", "Grundium Ocus"] (two-element)

Standardized SVS keys (MPP, AppMag, ScanScope ID, User, Date, Time)
populate cross-format Metadata regardless of writer.

Updated fixture JSONs: scan_620_.svs.json + svs_40x_bigtiff.svs.json
metadata.scanner_manufacturer flipped from "Aperio" → "Grundium".

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — OME-TIFF audit

**Files:**
- Read-only audit: `formats/ometiff/metadata.go`
- Possibly modify: `docs/formats/ometiff.md` if behavior is fine but needs documentation clarification

OME-TIFF currently captures the OME-XML root `Creator` attribute as `Properties["ome.creator"]` (e.g., `"OME Bio-Formats 6.0.0-rc1"`). It does NOT use this to set `ScannerManufacturer`. Per Q4 of the spec, this is correct: Creator is the OME-XML writer (typically conversion software), not the scanner OEM.

- [ ] **Step 1: Re-read formats/ometiff/metadata.go**

Confirm:
- `ome.creator` is populated from `<OME Creator="...">` root attribute
- `ScannerManufacturer` comes from `<Microscope Manufacturer="...">` (or similar) in the OME-XML if present, else empty
- The two are NOT conflated

- [ ] **Step 2: Probe both OME-TIFF fixtures**

```bash
go run /tmp/svsmeta.go sample_files/ome-tiff/Leica-1.ome.tiff 2>&1 | head -15
go run /tmp/svsmeta.go sample_files/ome-tiff/Leica-2.ome.tiff 2>&1 | head -15
```

Confirm `ome.creator` and `ScannerManufacturer` reflect the expected separation.

- [ ] **Step 3: If no code change needed, document**

If the behavior is correct (likely outcome per spec Q4), no code change. Add or extend a brief note in `docs/formats/ometiff.md` clarifying:

```markdown
## OME-XML writer attribution

OME-TIFF files are written by many sources (Bio-Formats conversions
from vendor formats, QuPath exports, OMERO pipelines, custom code).
opentile-go captures the OME-XML root `Creator` attribute as
`Properties["ome.creator"]` (e.g., `"OME Bio-Formats 6.0.0-rc1"`)
to identify the WRITER. The cross-format `ScannerManufacturer`
field is populated separately from `<Microscope>` elements when
present and reflects the SCANNER OEM (which is distinct from the
writer software).

Consumers needing writer-vendor info should read `ome.creator`;
consumers needing scanner identity should read `ScannerManufacturer`.

This distinction (writer vs. scanner OEM) is intentional per the
v0.18 spec — see `docs/superpowers/specs/2026-05-09-opentile-go-v18-svs-writer-detection-design.md`.
```

If a Bio-Formats-converted OME-TIFF *should* surface the original scanner OEM through `ScannerManufacturer`, that's a separate audit (probe for Microscope-element presence in our fixtures; if absent, `ScannerManufacturer` stays empty for those — which is correct).

- [ ] **Step 4: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/ometiff/ 2>&1 | tail -3
```

If only doc changes: confirm tests still green.

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
docs(v0.18): T2 — OME-TIFF writer-vendor audit (docs-only)

OME-TIFF correctly separates the OME-XML writer (Properties[
"ome.creator"]) from the scanner OEM (ScannerManufacturer from
<Microscope> when present). Per v0.18 spec Q4, this separation
is intentional — Creator is the OME-XML writer (typically
conversion software like Bio-Formats); ScannerManufacturer
should reflect the scanner that captured the slide, not the
software that wrote the OME-TIFF.

No code change. Documentation extended to make the writer-vs-
scanner-OEM distinction explicit so consumers know which field
to consult.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — Docs sweep + ship

**Files:**
- Modify: `docs/formats/svs.md` (new "Recognized SVS writers" section)
- Modify: `docs/deferred.md` (§8l retirement audit)
- Modify: `CHANGELOG.md` ([0.18.0] section)
- Modify: `CLAUDE.md` (milestone bump)

- [ ] **Step 1: docs/formats/svs.md "Recognized SVS writers"**

Find the existing "Edge tile semantics" or "What's not supported" section. Insert before "What's not supported":

```markdown
## Recognized SVS writers

SVS is the WSI ad-hoc standard — the format originated with Aperio but is
now written by multiple vendors. opentile-go detects the writer from the
ImageDescription tag's first line and adjusts `ScannerManufacturer`,
`ScannerModel`, and Properties namespacing accordingly.

| Writer first-line marker | Detected `ScannerManufacturer` | Detected `ScannerModel` | Properties namespace | Status |
|---|---|---|---|---|
| `Aperio Image Library v...` | `Aperio` | empty | `aperio.<key>` | ✅ canonical; verified on `CMU-1-Small-Region.svs`, `CMU-1.svs`, `JP2K-33003-1.svs` |
| `Aperio Image, Grundium Ocus` | `Grundium` | `Ocus` | `grundium.<key>` | ✅ verified on `scan_620_.svs`, `svs_40x_bigtiff.svs` |
| `Aperio Image, <vendor> [<model>]` | `<vendor>` (first whitespace-separated word) | `<model>` (remainder) | `<vendor>.<key>` (lowercased) | best-effort; pattern extension when fixtures surface |
| Any other first-line pattern | empty | empty | `svs.<key>` (format-default fallback) | best-effort; standardized SVS keys (MPP, AppMag) still populate cross-format Metadata |

**Standardized vs. vendor-specific keys.** SVS-format-defined keys
(`MPP`, `AppMag`, `ScanScope ID`, `Filename`, `User`, `Date`, `Time`)
populate cross-format `Metadata` (MicronsPerPixel, Magnification,
ScannerSerial, etc.) regardless of writer. Vendor-specific extensions
land under the writer-namespaced Properties bucket.

**Why this matters:** pre-v0.18, every SVS got `ScannerManufacturer = "Aperio"`
even when the actual writer was Grundium. v0.18's per-writer detection
fixes this attribution bug. Future writers (3DHistech via SVS export;
others) follow the same pattern automatically — the fallback
namespace ensures parsing doesn't break for unrecognized writers.
```

- [ ] **Step 2: docs/deferred.md §8l retirement audit**

Insert §8l BEFORE §8k (newest-first):

```markdown
## 8l. Retired in v0.18

v0.18 closes a misattribution bug discovered post-v0.17 brainstorm:
SVS files written by non-Aperio scanners (Grundium observed) got
`ScannerManufacturer = "Aperio"` because the SVS reader assumed
format-vendor = writer-vendor. v0.18 detects the actual writer
from ImageDescription first-line + TIFF Software/Make tags;
namespaces Properties keys per detected writer; documents the
recognized writer set explicitly.

**Items shipped:**

- `formats/svs/metadata.go::detectWriter()` — heuristic parser for
  the ImageDescription first line. Patterns recognized:
  - `"Aperio Image Library v..."` → Aperio canonical
  - `"Aperio Image, <name>"` → comma-suffix writer (first word =
    manufacturer; remainder = model)
  - undetected → empty manufacturer; `svs.<key>` Properties fallback
- `parseDescription` updated to use the detected writer; previously
  hardcoded `"Aperio"` removed.
- `ScannerSoftware` now sensibly split (was: single jammed first
  line; now: two-element list for comma-prefix patterns).
- Properties keys namespace per detected writer:
  - `aperio.<key>` for Aperio-written
  - `grundium.<key>` for Grundium-written
  - `svs.<key>` fallback for undetected
- Standardized SVS keys (MPP, AppMag, ScanScope ID, User, Date,
  Time) continue to populate cross-format Metadata regardless of
  writer.
- Fixture JSONs updated for Grundium SVS:
  `scan_620_.svs.json` + `svs_40x_bigtiff.svs.json`
  metadata.scanner_manufacturer flipped from "Aperio" → "Grundium".
- `docs/formats/svs.md` gains "Recognized SVS writers" section
  with the detection table.
- `formats/ometiff/metadata.go` audited (no code change needed):
  Creator is correctly captured as `ome.creator` and kept distinct
  from ScannerManufacturer per v0.18 Q4. Documentation extended
  to make the distinction explicit.

**Architecture invariants preserved:**

- Public API additive (writer detection refines existing
  ScannerManufacturer / ScannerModel / Properties surface; no new
  typed fields).
- Narrow break: consumer code that hardcoded Aperio expectations
  on Grundium SVS files (e.g., test code asserting
  `ScannerManufacturer == "Aperio"` on a Grundium fixture) now
  sees correct attribution and may need updating.
- v0.17 cross-format Metadata structure preserved; per-format
  Metadata structs unchanged.
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- Misattribution of Grundium-written SVS files as Aperio (was
  silently bad pre-v0.18).

**Still parked:**

- R19 (bare DZI) — internal/dzi pre-pares it.
- R21 (COG first-class support) — pairs naturally with HTTP-range backing.
- L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9, R15.

**v0.18 lessons:** The "writer-vendor vs format-vendor" distinction
is real. SVS is the worst-affected (ad-hoc multi-vendor) but the
pattern (heuristic detection + per-writer namespacing + standardized-key
preservation) generalizes if other formats later need similar
treatment.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md`](superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md).
```

- [ ] **Step 3: CHANGELOG.md [0.18.0]**

Insert before [0.17.0]:

```markdown
## [0.18.0] — 2026-05-09

SVS writer-vendor detection — closes a misattribution bug where
SVS files written by non-Aperio scanners (Grundium observed)
incorrectly reported `ScannerManufacturer = "Aperio"`. v0.18
detects the actual writer from ImageDescription first-line + TIFF
Software/Make tags; namespaces Properties keys per detected writer.

### Added

- `formats/svs/metadata.go::detectWriter()` — heuristic parser
  for the SVS ImageDescription first-line writer marker.
- "Recognized SVS writers" documentation in `docs/formats/svs.md`
  listing the supported writer first-line patterns + their
  detected vendor/model + Properties namespacing + status.
- "OME-XML writer attribution" documentation in
  `docs/formats/ometiff.md` clarifying the separation between
  `ome.creator` (writer) and `ScannerManufacturer` (scanner OEM).

### Fixed

- **SVS misattribution bug:** Grundium-written SVS files
  (scan_620_.svs, svs_40x_bigtiff.svs) now correctly report
  `ScannerManufacturer = "Grundium"`, `ScannerModel = "Ocus"`.
  Properties keys namespace under `grundium.<key>` instead of
  the misleading `aperio.<key>`.
- `ScannerSoftware` for SVS files no longer jams the first-line
  banner into a single string when the comma-suffix pattern
  is present; now sensibly split (e.g., `["Aperio Image", "Grundium Ocus"]`).

### Changed

- Fixture JSON parity files updated for Grundium SVS:
  `tests/fixtures/scan_620_.svs.json` and `tests/fixtures/svs_40x_bigtiff.svs.json`
  metadata.scanner_manufacturer flipped from "Aperio" → "Grundium".

### Notes

- **Narrow break:** consumer code hardcoding "Aperio" expectations
  on Grundium SVS files now sees correct attribution and may need
  updating. This is a bug fix; consumers should read
  `ScannerManufacturer` rather than assuming.
- **Standardized SVS keys** (MPP, AppMag, ScanScope ID, User,
  Date, Time) continue to populate cross-format Metadata regardless
  of writer.
- **Vendor-specific keys** namespace under the writer's lowercase
  first word: `aperio.<key>` for Aperio-written, `grundium.<key>`
  for Grundium-written, `svs.<key>` for undetected fallback.
- **Future writers** (3DHistech via SVS export; others) follow the
  same pattern automatically — the fallback namespace ensures
  parsing doesn't break for unrecognized writers.
- **OME-TIFF** writer attribution unchanged (was already correct);
  documentation extended.
- v1.0 cut still pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump from "after v0.17" → "after v0.18."

- [ ] **Step 4: CLAUDE.md milestone bump**

Replace `## Current milestone — v0.17` block:

```markdown
## Current milestone — v0.18 (shipped)

- **Scope:** SVS writer-vendor detection. Closes misattribution
  bug where Grundium-written SVS files (and any other non-Aperio
  writer) reported ScannerManufacturer="Aperio". v0.18 detects
  the actual writer from ImageDescription first-line + TIFF
  Software/Make tags; namespaces Properties keys per detected
  writer. Documents recognized writer set explicitly. OME-TIFF
  audited (already correct; docs extended). 3 plan tasks single
  batch.
- **API additions:** none (refines existing ScannerManufacturer /
  ScannerModel / Properties population).
- **API breaks:** narrow — consumer code hardcoding "Aperio" on
  Grundium SVS files sees correct attribution post-v0.18.
- **Active limitations:** unchanged from v0.17. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  docs/deferred.md §1a): unchanged. Python opentile has the same
  format-vendor=writer-vendor conflation; v0.18 improves on this.
- **Correctness bar:** make test green; TestSlideParity 30 fixtures
  green; TestCrossFormatMetadata green.
- **Sealed Q-decisions (6):** Q1 single parser with vendor-detection
  dispatch (Option A; rejects sub-readers); Q2 detection signal
  order (ImageDescription → Software → Make); Q3 unknown-writer
  fallback (svs.<key> namespace); Q4 OME-TIFF Creator stays
  separate from ScannerManufacturer; Q5 v0.18.0 (mostly additive);
  Q6 lowercased first-word vendor namespace.
- **Deferred forward:** R19 (bare DZI), R21 (COG first-class +
  HTTP-range backing). L19, L20, L23-L25, L26-L29, L30-L34,
  R4/R6/R9, R15. v1.0 cut still pending.
- **Design:** docs/superpowers/specs/2026-05-09-opentile-go-v18-svs-writer-detection-design.md
- **Plan:** docs/superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md
- **Work branch:** feat/v0.18

## Previous milestone — v0.17 (shipped 2026-05-09)

Cross-format Metadata expansion — closes R20. Hybrid: typed
additions (MicronsPerPixel + per-axis X/Y; ImageDescription) +
Properties map[string]string for opentile-go-canonical extensions
and vendor-namespaced passthrough. Every format reader updated
to populate the new fields. Format-specific Metadata structs
cleaned up per Q4 Option B.
```

- [ ] **Step 5: Final verification gate**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: vet clean, gofmt clean (or only pre-existing drift), every package green, TestSlideParity 30 fixtures green, TestCrossFormatMetadata green.

- [ ] **Step 6: Commit**

```bash
git add docs/formats/svs.md docs/formats/ometiff.md docs/deferred.md CHANGELOG.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs(v0.18): T3 — Recognized SVS writers + OME-TIFF audit + CHANGELOG + CLAUDE.md

docs/formats/svs.md: new "Recognized SVS writers" section with
detection table (Aperio canonical / Grundium Ocus / pattern-based
catch-all / undetected fallback). Status column distinguishes
verified writers (with fixture coverage) from best-effort.

docs/formats/ometiff.md: "OME-XML writer attribution" section
clarifying ome.creator (writer) vs ScannerManufacturer
(scanner OEM) per v0.18 Q4.

docs/deferred.md §8l: Retired in v0.18 — closes misattribution
bug; lists items shipped + deviations retired + still-parked
backlog rows.

CHANGELOG.md [0.18.0]: detectWriter() addition + Grundium fixture
re-attribution + ScannerSoftware split improvement + fixture JSON
flip; narrow-break call-out for consumer code hardcoding "Aperio".

CLAUDE.md: bump Current milestone v0.17 → v0.18. v0.17 demoted
to Previous; v0.16 / earlier collapsed.

End of milestone; v0.18 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (SVS writer-vendor detection) → T1.
- §1.2 (OME-TIFF audit) → T2.
- §1.3 (documentation) → T3.
- §3 (Q-decisions) → reflected in T1 (Q1, Q2, Q3, Q6) + T2 (Q4) + T3 (Q5).
- §5 (test strategy) → T1 unit + integration + fixture JSON updates; T2 docs-only; T3 final gate.
- §6 (no new limitations) → T3 docs confirm.
- §7 (plan outline) → matches.
- §8 (verification gates) → T3 step 5.

**Placeholder scan:** every step has exact code blocks, exact paths, expected outputs. T2 step 3 is conditional ("if no code change needed"); the implementer reads the existing OME-TIFF metadata.go to confirm the audit conclusion.

**Type consistency:** `writerVendor` struct + `detectWriter` function used identically across T1 + tests. `ScannerManufacturer` / `ScannerModel` / `Properties` referenced consistently (cross-format).

**Risks:**

- **R1 — splitKV may already drop the comma-prefix line.** The existing parseDescription line 60 calls `splitKV(body)` where body is everything after the first newline. If the first line is consumed as the SoftwareLine, the comma-prefix doesn't reach splitKV. T1 implementer should verify this assumption holds (it's the basis for detectWriter receiving the full first line cleanly).
- **R2 — Multi-word vendor names.** detectWriter's "first word = manufacturer; rest = model" rule is a heuristic. "Roche Ventana DP200" or similar future writers might need refinement (e.g., "Roche Ventana" as the manufacturer). For now, no fixture exercises this; document the heuristic and extend when fixtures surface.
- **R3 — Existing Aperio fixture tests may assert on `aperio.<key>` Properties.** T1 may need to update those — verify by running tests after Step 3 + adapting test files.
- **R4 — fixture JSONs may have other pinned metadata that changes.** Step 6 instructs the implementer to grep for `scanner_manufacturer` specifically; if `scanner_model` is also pinned (was empty), update it too. If unrelated fields change (shouldn't, but verify), investigate.
