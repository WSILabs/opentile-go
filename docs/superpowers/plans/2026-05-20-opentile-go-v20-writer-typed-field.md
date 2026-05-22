# opentile-go v0.20 — cross-format Writer typed field implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close R22 — add `Writer string` typed field to `opentile.Metadata`; populate per-format. Pure-additive milestone; no API breakage; no behavior changes for existing consumers.

**Architecture:** 5 tasks single batch. T1 lands the struct field. T2-T4 populate per format (batched by complexity). T5 lands the cross-format parity test + docs + ship.

**Tech stack:** Go 1.23+; existing `opentile` package + every `formats/*` package's metadata source.

**Spec:** [`docs/superpowers/specs/2026-05-20-opentile-go-v20-writer-typed-field-design.md`](../specs/2026-05-20-opentile-go-v20-writer-typed-field-design.md).

**Existing per-format Software-population call sites (audited 2026-05-20):**

| Format | Source line | Pattern |
|---|---|---|
| SVS | `formats/svs/metadata.go:119,126` | `md.ScannerSoftware = w.softwares` (uses v0.18 `writerVendor` detection) |
| NDPI | `formats/ndpi/metadata.go:100` | `md.ScannerSoftware = []string{f.Model}` (f.Model is the NDPI Software-like field) |
| Philips | `formats/philipstiff/metadata.go:82` | `md.ScannerSoftware = splitMultiValue(v)` (multi-value Software field) |
| OME-TIFF | (in ometiff metadata.go; populates from OME-XML) | OME-XML `<OME Creator>` already captured as `ome.creator` Property |
| BIF | `formats/bif/metadata.go:144` | `md.ScannerSoftware = []string{t.iscan.BuildVersion}` |
| IFE | (in ife metadata.go) | format-specific; verify per probe |
| Leica SCN | `formats/leicascn/tiler.go:253` | `md.ScannerSoftware = []string{primaryImg.DeviceVersion}` |
| Generic-TIFF | `formats/generictiff/tiler.go:138` | `md.ScannerSoftware = splitSoftware(v)` (TIFF Software tag, split) |
| SZI | `formats/szi/metadata.go:266` | `cross.ScannerSoftware = []string{s}` (combined SoftwareName + SoftwareVersion) |
| COG-WSI | `formats/cogwsi/metadata.go:58` | `md.ScannerSoftware = splitSoftware(v)` (delegated path via TIFF Software) |

Implementer pattern per format: at the same site as the existing ScannerSoftware population, ALSO set `md.Writer = <source>` per the Q-decision table from the spec.

---

## Task layout

5 tasks, single batch:

- T1 — `opentile.Metadata.Writer string` field + unit-test on field zero value + struct-literal compile-check
- T2 — SVS + NDPI + Philips populate Writer
- T3 — OME-TIFF + Leica SCN + BIF + IFE populate Writer
- T4 — SZI + Generic-TIFF (wsi-tools) + COG-WSI populate Writer
- T5 — Cross-format parity test extension + per-format docs sweep + CHANGELOG `[0.20.0]` + CLAUDE.md milestone bump + `docs/deferred.md §8n` retirement audit (closes R22)

---

## T1 — `opentile.Metadata.Writer` field

**Files:**
- Modify: `metadata.go`
- Modify: `metadata_test.go`

- [ ] **Step 1: Add Writer field to Metadata struct**

Edit `/Users/cornish/GitHub/opentile-go/metadata.go`. Append after the existing v0.17 fields (after `Properties map[string]string`):

```go
	// Writer identifies the software that wrote this file — the
	// file producer, distinct from ScannerManufacturer (scanner OEM)
	// and ScannerSoftware []string (broader software stack).
	//
	// Format-specific population:
	//   SVS Aperio canonical    "Aperio Image Library v11.2.1"
	//   SVS Grundium / other    "Grundium Ocus" (comma-suffix writer
	//                            from v0.18 detection)
	//   OME-TIFF                "OME Bio-Formats 6.0.0-rc1" (Creator
	//                            attribute; promoted from Properties)
	//   SZI                     "<SoftwareName> <SoftwareVersion>"
	//                            (e.g., "OcusScan 3.1.4")
	//   COG-WSI                 "wsitools/<WSIToolsVersion>" (file
	//                            producer; source scanner stays in
	//                            ScannerManufacturer per spec)
	//   Generic-TIFF (wsi-tools  "wsitools/<version>" from the
	//     fixtures avif/jxl/      wsi-tools ImageDescription parser
	//     htj2k/webp)
	//   NDPI / Philips / BIF /  format-specific Software field (often
	//     IFE / Leica SCN        equals ScannerSoftware[0])
	//
	// Empty when the format provides no writer indication. Consumers
	// checking presence compare against "" explicitly.
	//
	// Added in v0.20.
	Writer string
```

- [ ] **Step 2: Add a unit test for the zero value**

Append to `/Users/cornish/GitHub/opentile-go/metadata_test.go`:

```go
func TestMetadataWriter_ZeroValue(t *testing.T) {
	var m Metadata
	if m.Writer != "" {
		t.Errorf("zero-value Writer = %q, want empty", m.Writer)
	}
}

func TestMetadataWriter_SetGet(t *testing.T) {
	m := Metadata{Writer: "Aperio Image Library v11.2.1"}
	if m.Writer != "Aperio Image Library v11.2.1" {
		t.Errorf("Writer = %q", m.Writer)
	}
}
```

- [ ] **Step 3: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
go test -count=1 -run TestMetadataWriter ./ 2>&1 | tail -5
gofmt -l metadata.go metadata_test.go
```

Expected: build clean (no compile errors from existing struct-literal sites — purely additive); new tests pass; gofmt clean.

- [ ] **Step 4: Commit**

```bash
git add metadata.go metadata_test.go
git commit -m "$(cat <<'EOF'
feat(v0.20): T1 — opentile.Metadata.Writer field

Pure-additive cross-format field for the file-producer identifier.
Distinct from ScannerManufacturer (scanner OEM) and ScannerSoftware
[]string (broader software stack).

Per-format population lands in T2 (SVS+NDPI+Philips), T3 (OME+SCN+
BIF+IFE), T4 (SZI+Generic-TIFF wsi-tools+COG-WSI). T5 wires the
cross-format parity test + docs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — SVS + NDPI + Philips populate Writer

**Files:**
- Modify: `formats/svs/metadata.go` (~line 119, 126 — same site as `md.ScannerSoftware = w.softwares`)
- Modify: `formats/svs/metadata_test.go` (new assertions on Writer)
- Modify: `formats/ndpi/metadata.go` (~line 100)
- Modify: `formats/ndpi/metadata_test.go`
- Modify: `formats/philipstiff/metadata.go` (~line 82)
- Modify: `formats/philipstiff/metadata_test.go`

### SVS specifics

The v0.18 `writerVendor` struct already detects the writer:
- For canonical Aperio: `w.softwares = ["Aperio Image Library v11.2.1"]` (single element; full SoftwareLine)
- For Grundium: `w.softwares = ["Aperio Image", "Grundium Ocus"]` (two elements; format label + writer)
- For undetected: `w.softwares = [full SoftwareLine verbatim]` (single element)

Per Q2/Q3:
- Aperio canonical → Writer = `w.softwares[0]` (full SoftwareLine; preserves version)
- Grundium → Writer = `w.softwares[1]` (writer-vendor identifier)
- Undetected → Writer = `w.softwares[0]` (best-effort fallback)

Helper: `if len(w.softwares) == 2 { md.Writer = w.softwares[1] } else if len(w.softwares) >= 1 { md.Writer = w.softwares[0] }`. Apply at both line 119 and line 126 (the two `md.ScannerSoftware = w.softwares` sites in parseDescription).

### NDPI specifics

Existing site: `md.ScannerSoftware = []string{f.Model}` at line 100. Set `md.Writer = f.Model` at the same site.

### Philips specifics

Existing site: `md.ScannerSoftware = splitMultiValue(v)` at line 82 (v is the Philips Software field string). The split form is the cross-format list; the Writer should be the original unsplit string (or the first element after split, whichever feels more natural — probe to decide).

### Steps

- [ ] **Step 1: SVS — modify `parseDescription`**

Edit `formats/svs/metadata.go` around lines 117-127. At each of the two `md.ScannerSoftware = w.softwares` lines, ALSO set `md.Writer`:

```go
	md.ScannerSoftware = w.softwares
	// v0.20: Writer = the actual writer (per v0.18's detection).
	// For Aperio canonical (single-element softwares), use the full
	// SoftwareLine. For comma-suffix patterns (Grundium etc., two-
	// element), use the writer-vendor identifier from the suffix.
	if len(w.softwares) >= 2 {
		md.Writer = w.softwares[1]
	} else if len(w.softwares) == 1 {
		md.Writer = w.softwares[0]
	}
```

Repeat at both sites.

- [ ] **Step 2: NDPI — set Writer at line 100**

Edit `formats/ndpi/metadata.go`. After `md.ScannerSoftware = []string{f.Model}`:

```go
	md.ScannerSoftware = []string{f.Model}
	md.Writer = f.Model // v0.20: NDPI's Model is the writer identifier
```

- [ ] **Step 3: Philips — set Writer at line 82**

Edit `formats/philipstiff/metadata.go`. Look at how `splitMultiValue(v)` is used; pick the appropriate Writer source. Suggestion:

```go
	md.ScannerSoftware = splitMultiValue(v)
	md.Writer = v // v0.20: Writer is the raw multi-value Software string
```

(or `md.ScannerSoftware[0]` if v is a list-shaped string). Probe the fixture to verify which is more semantically correct.

- [ ] **Step 4: Update tests**

Each format's test file: add an assertion that `md.Writer != ""` and matches expected value. Use the existing test fixtures.

Examples:
- SVS canonical (CMU-1-Small-Region): `md.Writer == "Aperio Image Library v11.2.1"`
- SVS Grundium (scan_620_.svs): `md.Writer == "Grundium Ocus"`
- NDPI (CMU-1.ndpi): `md.Writer` whatever f.Model returns (probe to confirm)
- Philips (Philips-1.tiff): probe

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/svs/ ./formats/ndpi/ ./formats/philipstiff/ 2>&1 | tail -10
gofmt -l formats/svs/ formats/ndpi/ formats/philipstiff/
```

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.20): T2 — SVS + NDPI + Philips populate Writer

SVS:
  Aperio canonical → Writer = full SoftwareLine
                      ("Aperio Image Library v11.2.1")
  Grundium / other  → Writer = comma-suffix writer
                      ("Grundium Ocus"; matches v0.18 detection)
  Undetected        → Writer = SoftwareLine verbatim (best-effort)

NDPI: Writer = f.Model (the format-specific Software identifier)
Philips: Writer = raw Software field value

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — OME-TIFF + Leica SCN + BIF + IFE populate Writer

**Files:**
- Modify: `formats/ometiff/metadata.go` (find OME-XML Creator parsing; promote to Writer)
- Modify: `formats/ometiff/metadata_test.go`
- Modify: `formats/leicascn/tiler.go` (~line 253)
- Modify: `formats/leicascn/leicascn_test.go`
- Modify: `formats/bif/metadata.go` (~line 144)
- Modify: `formats/bif/metadata_test.go`
- Modify: `formats/ife/metadata.go` (find Software population site)
- Modify: `formats/ife/metadata_test.go`

### OME-TIFF specifics

OME-XML `<OME Creator>` attribute is already extracted and surfaced as `Properties["ome.creator"]` (per v0.17 T4 + audited in v0.18 T2). Find the parsing site; ALSO assign `md.Writer = <creator value>`. The Properties key stays for backward-compat per Q10.

### Leica SCN specifics

Existing site: `md.ScannerSoftware = []string{primaryImg.DeviceVersion}` at `formats/leicascn/tiler.go:253`. Set `md.Writer = primaryImg.DeviceVersion` at the same site.

### BIF specifics

Existing site: `md.ScannerSoftware = []string{t.iscan.BuildVersion}` at `formats/bif/metadata.go:144`. Set `md.Writer = t.iscan.BuildVersion`.

### IFE specifics

Find the existing Software-population site in `formats/ife/metadata.go` (search for `ScannerSoftware`). Set `md.Writer` from the same source.

### Steps

- [ ] **Step 1: OME-TIFF — promote ome.creator to Writer**

Edit `formats/ometiff/metadata.go`. Find where `Properties["ome.creator"]` is populated. Add a parallel line setting `md.Writer`:

```go
	if raw.Creator != "" {
		// existing: cross.SetProperty("ome.creator", raw.Creator)
		md.Writer = raw.Creator // v0.20: promote OME Creator to typed Writer field
	}
```

The exact code shape depends on the existing ometiff metadata parser flow. Verify the OME-XML Creator parsing site (probably in the OME-XML parse function).

- [ ] **Step 2: Leica SCN — set Writer at tiler.go:253**

```go
	md.ScannerSoftware = []string{primaryImg.DeviceVersion}
	md.Writer = primaryImg.DeviceVersion // v0.20
```

- [ ] **Step 3: BIF — set Writer at metadata.go:144**

```go
	md.ScannerSoftware = []string{t.iscan.BuildVersion}
	md.Writer = t.iscan.BuildVersion // v0.20
```

- [ ] **Step 4: IFE — set Writer**

Probe to find the Software-population site. Add a parallel `md.Writer` line.

- [ ] **Step 5: Update tests**

Add Writer assertions to each format's test file. Examples:
- OME-TIFF (Leica-1.ome.tiff): `md.Writer == "OME Bio-Formats 6.0.0-rc1"` (or whatever the actual Creator is)
- Leica SCN: `md.Writer == primaryImg.DeviceVersion` (probe per fixture)
- BIF: `md.Writer == t.iscan.BuildVersion` (probe per fixture)
- IFE: probe per fixture

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/ometiff/ ./formats/leicascn/ ./formats/bif/ ./formats/ife/ 2>&1 | tail -10
gofmt -l formats/ometiff/ formats/leicascn/ formats/bif/ formats/ife/
```

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.20): T3 — OME-TIFF + Leica SCN + BIF + IFE populate Writer

OME-TIFF: Writer = OME-XML Creator attribute (promoted from
  Properties[ome.creator]; Properties key stays for backward-compat)
Leica SCN: Writer = primary image's DeviceVersion
BIF: Writer = iScan BuildVersion
IFE: Writer = format-specific Software identifier

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — SZI + Generic-TIFF (wsi-tools) + COG-WSI populate Writer

**Files:**
- Modify: `formats/szi/metadata.go` (~line 260-266 — the SoftwareName+SoftwareVersion concat site)
- Modify: `formats/szi/metadata_test.go`
- Modify: `formats/generictiff/tiler.go` (~line 138 — splitSoftware) AND/OR `formats/generictiff/wsitools.go` (wsi-tools ImageDescription parser)
- Modify: `formats/generictiff/generic_test.go` or similar
- Modify: `formats/cogwsi/metadata.go` (~line 58)
- Modify: `formats/cogwsi/metadata_test.go`

### SZI specifics

Existing site: `cross.ScannerSoftware = []string{s}` where `s = "<SoftwareName> <SoftwareVersion>"`. Set `cross.Writer = s` at the same site.

### Generic-TIFF specifics

Two source paths:
1. **v0.10 path** (TIFF Software tag): `md.ScannerSoftware = splitSoftware(v)`. Set `md.Writer = v` (the unsplit raw Software string).
2. **v0.14 wsi-tools path** (ImageDescription `wsitools/<version> convert source=<fmt> ...`): the wsi-tools parser already extracts a version. Set `md.Writer = "wsitools/<version>"` when the wsi-tools path triggers.

Verify the wsi-tools parser in `formats/generictiff/wsitools.go` to find the version-extraction site; populate Writer there.

### COG-WSI specifics

Existing site: `md.ScannerSoftware = splitSoftware(v)` at `formats/cogwsi/metadata.go:58`. The COG-WSI metadata parser already extracts `WSIToolsVersion` (private tag 65084) per v0.19. Set:

```go
	// v0.20: Writer = wsitools/<WSIToolsVersion>. Q5: file producer
	// is wsitools; source scanner stays in ScannerManufacturer per spec.
	if wsiToolsVer, ok := page.WSIToolsVersion(); ok {
		md.Writer = "wsitools/" + wsiToolsVer
	}
```

(or however the existing code structures the WSIToolsVersion read; verify by reading the existing metadata.go).

### Steps

- [ ] **Step 1: SZI — set Writer at line 266**

```go
	if s != "" {
		cross.ScannerSoftware = []string{s}
		cross.Writer = s // v0.20
	}
```

- [ ] **Step 2: Generic-TIFF — set Writer at line 138 (TIFF Software path)**

```go
	if v, ok := p.Software(); ok {
		md.ScannerSoftware = splitSoftware(v)
		md.Writer = v // v0.20: raw Software string
	}
```

- [ ] **Step 3: Generic-TIFF wsi-tools path — set Writer when wsi-tools parser triggers**

In `formats/generictiff/wsitools.go` or wherever the wsi-tools parser is called from `buildMetadata` (`formats/generictiff/tiler.go`), AFTER the wsi-tools parser populates other Properties, set:

```go
	if wt, ok := parseWSIToolsDescription(md.ImageDescription); ok {
		// ... existing population ...
		md.Writer = "wsitools/" + wt.Version  // v0.20: wsi-tools is the writer
	}
```

Override the Step 2 setting (which would have been the raw ImageDescription string). The wsi-tools-derived Writer is more semantically accurate when the file is wsi-tools-produced.

- [ ] **Step 4: COG-WSI — set Writer from WSIToolsVersion private tag**

Edit `formats/cogwsi/metadata.go`. Find where WSI private tags are read. Set:

```go
	if ver, ok := page.WSIToolsVersion(); ok {
		md.Writer = "wsitools/" + ver  // v0.20: Q5 — file producer
	}
```

This OVERRIDES whatever the delegated generic-TIFF metadata might have set, because COG-WSI's writer is always wsitools (per spec).

- [ ] **Step 5: Update tests**

Add Writer assertions:
- SZI (CMU-1.szi): `md.Writer == "OcusScan 3.1.4"` (spec-example values; verify by probe)
- SZI (Grundium scan_618): `md.Writer` whatever the Grundium fixture says (probe)
- Generic-TIFF wsi-tools fixtures (avif-out.tiff): `md.Writer == "wsitools/0.2.0-dev"`
- COG-WSI (CMU-1-Small-Region_cog-wsi.tiff): `md.Writer == "wsitools/0.6.0-dev"`

- [ ] **Step 6: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/szi/ ./formats/generictiff/ ./formats/cogwsi/ 2>&1 | tail -10
gofmt -l formats/szi/ formats/generictiff/ formats/cogwsi/
```

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
feat(v0.20): T4 — SZI + Generic-TIFF (wsi-tools) + COG-WSI populate Writer

SZI: Writer = "<SoftwareName> <SoftwareVersion>" combined
Generic-TIFF (TIFF Software path): Writer = raw Software field
Generic-TIFF (wsi-tools path): Writer = "wsitools/<version>"
  (overrides Software-derived Writer; wsi-tools is the actual
  producer when the parser triggers)
COG-WSI: Writer = "wsitools/<WSIToolsVersion>" from private tag
  65084 (Q5: file producer is wsitools; source scanner stays in
  ScannerManufacturer per spec)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — Cross-format parity test + per-format docs + ship

**Files:**
- Modify: `tests/parity/cross_format_metadata_test.go` (extend with Writer assertions per fixture)
- Modify: each `docs/formats/*.md` (small mention of Writer field; what each format populates)
- Modify: `docs/deferred.md` (§8n retirement audit; R22 retired)
- Modify: `CHANGELOG.md` ([0.20.0] section)
- Modify: `CLAUDE.md` (milestone bump)

### Step 1: Extend TestCrossFormatMetadata

Edit `tests/parity/cross_format_metadata_test.go`. Add a new field to the `crossFormatMetadataExpect` struct:

```go
type crossFormatMetadataExpect struct {
    // ... existing fields ...
    wantWriterContains string // substring match for version flexibility
}
```

Add `wantWriterContains` values per fixture row (probed values from T2/T3/T4 implementation reports):

```go
{fixture: "svs/CMU-1-Small-Region.svs", wantWriterContains: "Aperio Image Library"},
{fixture: "svs/scan_620_.svs", wantWriterContains: "Grundium Ocus"},
{fixture: "ome-tiff/Leica-1.ome.tiff", wantWriterContains: "Bio-Formats"},
{fixture: "szi/CMU-1.szi", wantWriterContains: "OcusScan"},
{fixture: "cog-wsi/CMU-1-Small-Region_cog-wsi.tiff", wantWriterContains: "wsitools"},
{fixture: "generic-tiff/avif-out.tiff", wantWriterContains: "wsitools"},
// ... etc per format
```

Add assertion in the test runner:

```go
if fx.wantWriterContains != "" {
    if !strings.Contains(md.Writer, fx.wantWriterContains) {
        t.Errorf("Writer = %q; want substring %q", md.Writer, fx.wantWriterContains)
    }
}
```

### Step 2: Per-format docs sweep

For each `docs/formats/<fmt>.md`, add a brief mention of the Writer field under the "Cross-format Metadata mapping" section (added in v0.17). Example:

```markdown
| `<format-source>` | `Metadata.Writer` (v0.20) |
```

Concise; one line per format doc. Mirrors the existing v0.17 mapping table style.

### Step 3: docs/deferred.md §8n retirement audit

Insert §8n BEFORE §8m (newest-first):

```markdown
## 8n. Retired in v0.20

v0.20 closes R22 — cross-format `Writer string` typed field added
to `opentile.Metadata`. Pure-additive; no API breakage.

**Items shipped:**

- `opentile.Metadata.Writer string` typed field. File-producer
  identifier; distinct from `ScannerManufacturer` (scanner OEM)
  and `ScannerSoftware []string` (broader software stack).
- Per-format population in all 10 readers (SVS canonical + Grundium
  detection from v0.18; NDPI; Philips; OME-TIFF promoting ome.creator;
  Leica SCN; BIF; IFE; SZI combined SoftwareName+Version; Generic-TIFF
  TIFF Software path + wsi-tools override; COG-WSI wsitools/<version>
  from WSIToolsVersion private tag 65084).
- TestCrossFormatMetadata extension: per-fixture Writer substring
  assertion via `wantWriterContains`.

**Architecture invariants preserved:**

- Pure-additive on cross-format Metadata; existing consumers
  unaffected.
- Properties keys (ome.creator, cog-wsi.wsitools-version) retained
  for backward-compat.
- v0.18 SVS writer detection (`writerVendor`) reused for SVS Writer
  population.
- Scanner attribution semantics unchanged (ScannerManufacturer is
  still the scanner OEM; Writer is the file producer).
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R22 (cross-format Writer typed field) — landed.

**Still parked:**

- R19 (bare DZI)
- R23 (re-apply v0.18 SVS detectWriter to Grundium-source COG-WSI)
- Other §11 backlog rows unchanged.

**v0.20 lessons:** the typed/Properties hybrid pattern from v0.17
generalized cleanly to Writer — populate the typed field at the
same site as existing ScannerSoftware population; keep the older
Properties keys for backward-compat at zero extra cost.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md`](superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md).
```

Update §1 R22 status column: `✅ landed in v0.20 (see §8n)`.

Update §11: remove the active R22 row entirely.

### Step 4: CHANGELOG.md [0.20.0]

Insert before [0.19.1]. Use 2026-05-20 date.

```markdown
## [0.20.0] — 2026-05-20

Cross-format Writer typed field — closes R22. Adds `Writer string`
to `opentile.Metadata` carrying the file-producer identifier.
Pure-additive; no API breakage; no behavior changes for existing
consumers.

### Added

- **`opentile.Metadata.Writer string`** — typed field for the file
  producer (the software that wrote the file). Distinct from:
  - `ScannerManufacturer` (scanner OEM — who made the hardware)
  - `ScannerSoftware []string` (broader software stack — may include
    both writer and scanner software)
- Per-format Writer population:
  - **SVS Aperio canonical**: `"Aperio Image Library v11.2.1"` (full
    SoftwareLine; preserves version)
  - **SVS Grundium / non-canonical**: `"Grundium Ocus"` (comma-suffix
    writer from v0.18 detection)
  - **NDPI**: format-specific Model identifier
  - **Philips TIFF**: raw Software field
  - **OME-TIFF**: `"OME Bio-Formats X.Y.Z"` (Creator attribute
    promoted from `Properties["ome.creator"]`)
  - **Leica SCN**: primary image's DeviceVersion
  - **BIF**: iScan BuildVersion
  - **IFE**: format-specific Software identifier
  - **SZI**: `"<SoftwareName> <SoftwareVersion>"` combined
  - **Generic-TIFF (no wsi-tools)**: TIFF Software tag value
  - **Generic-TIFF (wsi-tools)**: `"wsitools/<version>"` (overrides
    Software-derived value when wsi-tools parser triggers)
  - **COG-WSI**: `"wsitools/<WSIToolsVersion>"` from private tag 65084
    (file producer; source scanner stays in ScannerManufacturer per spec)
- `TestCrossFormatMetadata` extended with per-fixture `wantWriterContains`
  substring assertions.

### Notes

- **Backward-compat**: Properties keys (`ome.creator`,
  `cog-wsi.wsitools-version`) continue to populate as before. The
  new typed `Writer` field is the primary surface; Properties keys
  remain accessible at zero extra cost.
- **Q5 semantics**: for converted files (OME-TIFF via Bio-Formats;
  COG-WSI via wsitools; wsi-tools-converted generic-TIFF), Writer
  is the converter and ScannerManufacturer/Model/Software preserve
  the source scanner attribution per format spec.
- **R23 derived bug**: Grundium-source COG-WSI files report
  `ScannerManufacturer = "Aperio"` (pre-v0.18 attribution leaked
  through the wsitools-preserved TIFF Make tag). Filed as R23 in
  the deferred backlog for a separate fix; out of v0.20 scope.
- v1.0 cut still pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump from "after v0.19.1" → "after v0.20."

### Step 5: CLAUDE.md milestone bump

Replace `## Current milestone — v0.19` (plus the v0.19.1 Patch annotation) with `## Current milestone — v0.20 (shipped)`. Demote v0.19 to "Previous milestone." Use 2026-05-20.

### Step 6: Final verification gate

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
make cover 2>&1 | tail -5
```

Expected: vet clean, gofmt clean (or pre-existing drift only), every package green, TestSlideParity 40 fixtures green, TestCrossFormatMetadata green, `make cover` PASS (≥80% per package).

### Step 7: Commit

```bash
git add docs/formats/ docs/deferred.md CHANGELOG.md CLAUDE.md tests/parity/cross_format_metadata_test.go
git commit -m "$(cat <<'EOF'
docs(v0.20): T5 — cross-format parity test extension + per-format docs + CHANGELOG + CLAUDE.md milestone bump

tests/parity/cross_format_metadata_test.go: extended with per-
fixture wantWriterContains substring assertions across every
format (substring match for version flexibility).

Per-format docs: brief mention of Writer field under each format's
Cross-format Metadata mapping section (v0.17 style).

docs/deferred.md §8n: Retired in v0.20 — closes R22. R22 marked
landed in §1 status column; §11 active row removed.

CHANGELOG.md [0.20.0]: explicit Added block (Writer typed field +
per-format population across 10 readers + cross-format parity
extension); Notes (backward-compat; Q5 semantics; R23 derived bug
deferred).

CLAUDE.md: bump Current milestone v0.19 → v0.20. v0.19 demoted to
Previous; v0.18 / v0.17 / earlier collapsed.

End of milestone; v0.20 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §1.1 (struct expansion) → T1.
- §1.2 (per-format population) → T2 + T3 + T4 (one per format batched by complexity).
- §1.3 (file-producer vs scanner-OEM distinction) → reflected in T2-T4 per-format design.
- §1.4 (Properties keys backward-compat) → reflected throughout; no Properties removals.
- §3 (Q-decisions) → reflected throughout.
- §5 (test strategy) → T2/T3/T4 per-format integration tests + T5 cross-format parity extension.
- §6 (no new limitations) → T5 docs confirm.
- §7 (plan outline) → matches.
- §8 (verification gates) → T5 step 6.

**Placeholder scan:** T2/T3/T4 each say "probe per fixture" for some Writer values (NDPI / Philips / IFE / SCN / BIF / SZI Grundium). The plan provides scaffolding + the actual fixture mapping needs implementer-side probing. This is intentional — the work is "read existing source field; assign to Writer at same site," not "type literal Writer values from the plan."

**Type consistency:** `Writer string` used identically in T1 → T5. `wantWriterContains` test field naming consistent across T5.

**Risks:**

- **R1 — Generic-TIFF wsi-tools override path.** T4 step 3 needs the wsi-tools parser to override the Step 2 Software-derived Writer. Order matters: the wsi-tools branch runs AFTER the TIFF Software branch in `buildMetadata`. Verify the actual buildMetadata flow during T4; if order is reversed, swap the override site.
- **R2 — COG-WSI Writer override via delegation.** cogwsi delegates to generictiff for the bulk of metadata; the WSIToolsVersion-derived Writer must be applied AT the cogwsi layer (after delegation) to override whatever generictiff set. T4 step 4 calls this out explicitly.
- **R3 — Fixture JSON schema.** If `TestSlideParity` fixture JSONs pin Metadata fields, adding Writer might require either extending the schema (regenerate all fixtures with Writer in metadata block) OR being careful that the fixture-JSON loader doesn't fail on missing Writer. T5 step 6 verifies via `make test`; if regression appears, investigate.
- **R4 — Some formats have no clear writer.** NDPI may have no useful Software identifier; same for some Generic-TIFF fixtures. Empty Writer is acceptable per Q9 (`""` is the sentinel). Don't force-populate.
- **R5 — Per-fixture probe required for T5 Writer values.** The plan sketches "wantWriterContains" entries for popular fixtures but the implementer must probe each fixture to confirm the actual Writer string contains the expected substring. Substring match (not equality) gives flexibility against version drift.
