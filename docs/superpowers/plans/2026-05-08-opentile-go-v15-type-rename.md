# opentile-go v0.15 — Kind→Type + overview-canonical alignment implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `AssociatedImage.Kind()` → `Type()` across every reader + test, and flip generic-TIFF + Leica SCN's emitted value from `"macro"` → `"overview"` (aligning with DICOM + Python opentile). IFE preserves `"macro"` and `"overview"` as distinct values per its spec.

**Architecture:** Single-batch rename milestone. T1 lands the method rename (sweeping interface + 8 implementations + every method-call site). T2 lands generictiff constants. T3 lands the Leica SCN value flip. T4 lands the test-side struct field rename + assertion updates. T5 + T6 close with docs + ship.

**Tech stack:** Go 1.23+; existing `formats/*` packages.

**Spec:** [`docs/superpowers/specs/2026-05-08-opentile-go-v15-type-rename-design.md`](../specs/2026-05-08-opentile-go-v15-type-rename-design.md).

**v0.12 lesson (recall):** BSD `sed` silently misses identifiers on word-boundary patterns. Pair every sed pass with a `grep` audit; use `Edit` for surgical fixes when sed misses.

---

## Task layout

6 tasks, single batch:

- T1 — `Kind() string` → `Type() string` method rename across interface + 8 readers + every method-call site
- T2 — generictiff `KindXxx` constants → `TypeXxx`; value flip `KindMacro = "macro"` → `TypeOverview = "overview"`
- T3 — Leica SCN reader emits `"overview"` instead of `"macro"`
- T4 — Test surface: `genericAssocExpect.Kind` field → `Type`; geometry assertions for value flip
- T5 — Docs: README + image.go interface comment + per-format docs (generictiff, leicascn, ife)
- T6 — Ship: CHANGELOG [0.15.0] + CLAUDE.md milestone bump + `docs/deferred.md §8i` + final verification gate

---

## T1 — `Kind()` → `Type()` method rename (sweeping)

**Files (mechanical sweep):**
- Modify: `image.go` (interface definition + doc comment)
- Modify: `opentile_test.go` (mock implementation)
- Modify: `formats/svs/associated.go` + any other svs files declaring Kind()
- Modify: `formats/ndpi/associated.go`, `formats/ndpi/mappage.go`
- Modify: `formats/philipstiff/associated.go`
- Modify: `formats/ometiff/associated.go`
- Modify: `formats/bif/associated.go`
- Modify: `formats/ife/metadata.go` (and any other ife files with Kind())
- Modify: `formats/generictiff/associated.go`
- Modify: `formats/leicascn/associated.go`
- Modify: every `*_test.go` file that calls `.Kind()`
- Modify: `tests/parity/generic_geometry_test.go` (calls `.Kind()`)
- Modify: `tests/integration_test.go` (if it calls `.Kind()`)

NOTE: T1 does NOT rename the `Kind` STRUCT FIELD on `genericAssocExpect` (that's T4). T1 ONLY touches the method `Kind() string` and its call sites `obj.Kind()`.

- [ ] **Step 1: Rewrite the interface definition + doc comment in `image.go`**

Edit `image.go` lines ~160-183. Replace the existing `AssociatedImage` interface block + its doc comment with:

```go
// Standard Type() values used across opentile-go's format readers.
// The choice of names follows DICOM PS3.3 / Supplement 145
// (Whole Slide Microscopic Image IOD), where the Image Type
// attribute (0008,0008) value 3 enumerates: VOLUME / LABEL /
// OVERVIEW / THUMBNAIL. opentile-go uses the lowercase form,
// extended with format-specific kinds where the underlying file
// surfaces them:
//
//	"label"       — slide label / barcode
//	"overview"    — wide-field image of the slide. The DICOM-canonical
//	                term, also used by upstream Python opentile and by
//	                six of opentile-go's eight format readers. The
//	                seventh (Iris IFE) intentionally distinguishes
//	                "overview" from "macro" per the IFE spec.
//	"thumbnail"   — full-slide downsample (typically square, JPEG)
//	"map"         — NDPI / IFE: low-magnification map / overview-of-
//	                pyramid; semantically distinct from a wide-field
//	                slide image
//	"probability" — Ventana BIF / IFE: confidence / classification map
//	"macro"       — Iris IFE only. The IFE spec defines LABEL_MACRO
//	                as a kind distinct from LABEL_OVERVIEW. Other
//	                formats' wide-field slide images surface as
//	                "overview", not "macro".
//	"associated"  — generic-TIFF heuristic-fallback (v0.10+) when the
//	                classifier can't confidently match a kind above
//
// Format readers use the string literals directly; the values above
// are stable and part of the public API contract from v0.15 onward.
type AssociatedImage interface {
	Type() string
	Size() Size
	Compression() Compression
	Bytes() ([]byte, error)
}
```

- [ ] **Step 2: Sweep `Kind()` → `Type()` across method definitions and call sites**

Run from repo root:

```bash
cd /Users/cornish/GitHub/opentile-go

# Method-definition rewrite (declarations like "func (...) Kind() string"):
grep -rln 'func.*Kind() string' --include='*.go' . | grep -v sample_files

# Method-call rewrite (.Kind() callsites):
grep -rln '\.Kind()' --include='*.go' . | grep -v sample_files
```

For each file the greps return, use `Edit` (NOT sed) to:
- Rename `func (x ...) Kind() string` declarations → `func (x ...) Type() string` (literal find/replace)
- Rename `.Kind()` callsites → `.Type()` (literal find/replace)

The grep audit pattern from v0.12 lessons applies — run the greps AGAIN after edits to confirm 0 hits remain.

- [ ] **Step 3: Verify build + tests**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -10
go vet ./... 2>&1 | head -3
gofmt -l . | grep -v sample_files | grep -v 'docs/' | head
```

Expected: build clean, vet clean, gofmt clean. Tests will not yet pass cleanly because T2 hasn't yet renamed `KindMacro` (the geometry test references `KindMacro`); skipped tests acceptable. Defer full `go test` until T2 lands.

If build is broken at this step, the most likely cause is one of these still being a `Kind()` callsite that the grep missed (e.g., assigned to a variable). Re-grep with `\bKind\b` to widen, audit, fix.

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
refactor(v0.15): T1 — rename AssociatedImage.Kind() → Type()

Method rename across the public interface (image.go) and every
implementation in the eight format readers (SVS / NDPI / Philips /
OME / BIF / IFE / generic-TIFF / Leica SCN), plus every method-call
site in tests + the opentile_test.go mock.

Renames the v0.1-era Go-stdlib-idiomatic name to align with DICOM
PS3.3 / Supplement 145, where the analogous attribute is ImageType
(0008,0008). The interface doc comment is rewritten to cite the
DICOM standard and document the canonical values.

This commit only renames the method. Constant renames (T2 for
generictiff KindXxx → TypeXxx) and value flips (T3 for Leica SCN's
"macro" → "overview") follow.

Build clean; full test suite deferred to T2 (generic-tiff
geometry test still references KindMacro until T2 lands).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — generictiff `KindXxx` → `TypeXxx` + `KindMacro` value flip

**Files:**
- Modify: `formats/generictiff/classifier.go`
- Modify: every other file in `formats/generictiff/` referencing `KindLabel` / `KindMacro` / `KindThumbnail` / `KindAssociated`
- Modify: `tests/parity/generic_geometry_test.go` (references `generictiff.KindThumbnail` etc.)

- [ ] **Step 1: Rewrite the constants in `classifier.go`**

Edit `formats/generictiff/classifier.go` lines ~22-28. Replace the const block:

```go
const (
	KindLabel      = "label"
	KindMacro      = "macro"
	KindThumbnail  = "thumbnail"
	KindAssociated = "associated" // v0.10 addition; classifier-fallback (Q5)
)
```

with:

```go
const (
	TypeLabel      = "label"
	TypeOverview   = "overview"  // v0.15: was KindMacro = "macro"; flipped to align with DICOM + upstream Python opentile
	TypeThumbnail  = "thumbnail"
	TypeAssociated = "associated" // v0.10 addition; classifier-fallback (Q5)
)
```

- [ ] **Step 2: Sweep references**

```bash
cd /Users/cornish/GitHub/opentile-go
grep -rn 'KindLabel\|KindMacro\|KindThumbnail\|KindAssociated' --include='*.go' . | grep -v sample_files
```

For each match, use `Edit` to rename:
- `KindLabel` → `TypeLabel`
- `KindMacro` → `TypeOverview` (note: name AND semantic identity flip)
- `KindThumbnail` → `TypeThumbnail`
- `KindAssociated` → `TypeAssociated`

Then re-grep — expect 0 hits for the old names.

- [ ] **Step 3: Sweep emitted-string values**

The classifier or the associated-image construction may have hardcoded `"macro"` in addition to (or instead of) using `KindMacro`. Audit:

```bash
grep -rn '"macro"' formats/generictiff/ --include='*.go'
```

For any literal `"macro"` string, replace with `"overview"` (or the new `TypeOverview` constant when in classifier code). Re-grep to confirm 0 `"macro"` literals remain in `formats/generictiff/`.

- [ ] **Step 4: Verify build + generictiff tests**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/generictiff/ 2>&1 | tail -10
```

Expected: build clean. Generictiff package tests may fail on geometry assertions that still expect `Kind: ... TypeOverview` literals — those will be fixed in T4. T2's verification scope is "build clean + classifier-internal logic exercised."

If the package-internal tests pass, great. If geometry tests fail with "got overview want macro" — that's a test-side fix (T4), not a T2 regression. Note in commit if so.

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
refactor(v0.15): T2 — generictiff KindXxx → TypeXxx + value flip

Rename the four exported constants in formats/generictiff/classifier.go
to match the v0.15 method-rename convention. KindMacro = "macro" flips
in one move to TypeOverview = "overview"; this aligns generic-TIFF's
emitted Type() value with DICOM PS3.3 / Supplement 145 (which uses
"OVERVIEW") and with Python opentile (the upstream we directly port).

  KindLabel       → TypeLabel       (value unchanged: "label")
  KindMacro       → TypeOverview    (value flips: "macro" → "overview")
  KindThumbnail   → TypeThumbnail   (value unchanged: "thumbnail")
  KindAssociated  → TypeAssociated  (value unchanged: "associated")

The "macro" → "overview" value change is a one-shot pre-1.0 break
per the v0.15 sole-consumer sign-off. No transitional aliasing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — Leica SCN reader emits `"overview"`

**Files:**
- Modify: `formats/leicascn/associated.go`
- Modify: any leicascn test files referencing the literal `"macro"` for the format's emitted value
- Modify: `formats/leicascn/tiler.go` (per the v0.15 spec audit, this file's docstring at line 33 references `kind="macro"` — update to `type="overview"`)

- [ ] **Step 1: Update the Type() return value in associated.go**

Edit `formats/leicascn/associated.go`. Find the line that returns `"macro"` (currently around line 38, post-T1 the method is named `Type()`):

```go
func (a *associatedImage) Type() string                      { return "macro" }
```

Replace `return "macro"` with `return "overview"`.

- [ ] **Step 2: Update the doc comment**

The associated.go doc comment likely cites Q8 sealing `Kind() == "macro"`. Update to reflect the v0.15 alignment:

Look for comments like:
```go
// Kind() == "macro" per sealed Q8; format-specific metadata
```

Replace with:
```go
// Type() == "overview" (v0.15: aligned with DICOM ImageType +
// Python opentile + 5 sibling format readers; was "macro" pre-v0.15
// per Q8). Format-specific metadata...
```

Adapt to the actual comment text — preserve any Q8-related semantics that aren't about the string value itself.

- [ ] **Step 3: Update tiler.go reference**

Edit `formats/leicascn/tiler.go` line 33 area:

Find:
```go
// (one element with kind="macro" in the AssociatedImage list,
```

Replace with:
```go
// (one element with Type() == "overview" in the AssociatedImage list,
```

- [ ] **Step 4: Sweep any test files in `formats/leicascn/` that reference the literal `"macro"`**

```bash
grep -rn '"macro"' formats/leicascn/ --include='*.go'
```

For each match, decide:
- If the reference is testing the Type() value: change literal to `"overview"`
- If the reference is in a comment about historical naming: optionally update or leave with a v0.15 note

Re-grep to confirm 0 `"macro"` literals (or 0 active assertions; comments about historical naming are OK).

- [ ] **Step 5: Verify build + leicascn tests**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -3
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./formats/leicascn/ 2>&1 | tail -10
```

Expected: build clean, leicascn tests green.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
refactor(v0.15): T3 — leicascn Type() emits "overview" not "macro"

formats/leicascn/associated.go now returns "overview" from Type()
for the auxiliary <image> element, aligning with the v0.15 Q5/Q8
seal: every opentile-go format except Iris IFE emits "overview" for
the wide-field slide image (matching DICOM PS3.3 + upstream Python
opentile).

The pre-v0.15 "macro" choice (sealed in v0.11 Q8) is corrected here.
Iris IFE remains untouched — its spec defines LABEL_MACRO as a
distinct kind from LABEL_OVERVIEW.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — Test surface: `genericAssocExpect.Kind` field + assertion updates

**Files:**
- Modify: `tests/parity/generic_geometry_test.go`

The test struct `genericAssocExpect` has a field literally named `Kind` (independent from the renamed `Kind()` method). Per v0.15 spec §5, this field renames to `Type` for consistency.

Geometry assertions referencing the now-`TypeOverview` constant flow naturally from T2; only the field name + any assertion-side fixture-row ordering needs care.

- [ ] **Step 1: Rename `Kind` field on `genericAssocExpect` struct**

Edit `tests/parity/generic_geometry_test.go`. Find the struct definition (around line 29):

```go
type genericAssocExpect struct {
    Kind        string
    ...
    ByteCount   int
}
```

Rename `Kind` → `Type`.

- [ ] **Step 2: Update all literal struct references**

Sweep the file for `Kind:` field references in struct literals:

```bash
grep -n 'Kind:' tests/parity/generic_geometry_test.go
```

For each, rename `Kind: ...` → `Type: ...` in place.

- [ ] **Step 3: Update assertion field reference**

The test at line ~215-216 uses `exp.Kind` and the runtime check `a.Kind()`. After T1 + this step, both should be `exp.Type` and `a.Type()`. Verify and fix.

```bash
grep -n 'exp\.Kind\|a\.Kind\|\.Kind\b' tests/parity/generic_geometry_test.go
```

Should return 0 hits after the sweep.

- [ ] **Step 4: Verify the per-fixture rows reflect the value flip**

The four v0.14 wsi-tools fixtures + earlier generic-TIFF fixtures may have rows that previously referenced `KindMacro` (now `TypeOverview` per T2). T2 already renamed the constant, so those references should already point at `TypeOverview = "overview"`. Verify by grepping:

```bash
grep -n 'TypeOverview\|TypeLabel\|TypeThumbnail\|TypeAssociated' tests/parity/generic_geometry_test.go
```

Confirm rows match the format-emitted values:
- generic-TIFF fixtures (avif-out, htj2k-out, jxl-out, webp-out, plus pre-v0.14 generic-TIFF fixtures): the wide-field slide image now expects `Type: generictiff.TypeOverview` (string value "overview")
- Per-fixture associated lists: scan for any row using a now-stale `TypeMacro` reference (shouldn't exist after T2; sanity check anyway)

- [ ] **Step 5: Verify full test suite**

```bash
cd /Users/cornish/GitHub/opentile-go
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -15
```

Expected: every package green. TestGenericGeometry passes for all generic-TIFF fixtures; TestSlideParity 28 fixtures all pass.

If any test fails with "got overview want macro" or similar — that's an individual fixture-row that wasn't caught; use Edit to fix the specific row.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
test(v0.15): T4 — genericAssocExpect.Kind field → Type + value updates

tests/parity/generic_geometry_test.go renames the test struct's
`Kind` field to `Type` for naming consistency with T1+T2's renames,
and confirms all per-fixture geometry rows reflect the v0.15
value flip (generic-TIFF + Leica SCN now expect "overview"; pre-
existing rows at "label"/"thumbnail"/"associated" unchanged).

Full TestSlideParity (28 fixtures) + TestGenericGeometry pass with
the post-rename type values.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/formats/generictiff.md`
- Modify: `docs/formats/leicascn.md`
- Modify: `docs/formats/ife.md`

(`image.go` interface doc was already rewritten in T1 — no further docs work there.)

- [ ] **Step 1: Fix README rows**

Edit `README.md`. The Supported-formats table has rows for each format. Three rows need updates:

Find OME-TIFF row (currently listing `macro, label, thumbnail`):
```
| **OME-TIFF** | `.ome.tiff` | tiled (SubIFD) + OneFrame | macro, label, thumbnail | ...
```
Replace `macro, label, thumbnail` → `overview, label, thumbnail`. (This was already wrong pre-v0.15; the OME reader has always emitted "overview"; the README row was stale.)

Find generic-TIFF row (currently listing `label, macro, thumbnail, "associated"`):
```
| **Generic TIFF\*** | ... | classifier-assigned: label, macro, thumbnail, or "associated" fallback | ...
```
Replace `label, macro, thumbnail` → `label, overview, thumbnail`. ("associated" stays.)

Find Leica SCN row (currently listing `macro per auxiliary <image>`):
```
| **Leica SCN\*** | ... | classifier-assigned: macro per auxiliary <image> | ...
```
Replace `macro per auxiliary <image>` → `overview per auxiliary <image>`.

Use `Edit` per row — sed risks markdown-table corruption.

Also: any README narrative paragraphs that reference `.Kind()` should be updated to `.Type()`. Search:

```bash
grep -n 'Kind()\|\.Kind\b' README.md
```

- [ ] **Step 2: Update `docs/formats/generictiff.md`**

Find the section describing emitted associated-image values. Update the canonical list:

Pre-v0.15 (likely text):
> `Tiler.Associated()` for generic TIFFs emits one of: `"label"`, `"macro"`, `"thumbnail"`, `"associated"` (heuristic-fallback)

Post-v0.15:
> `Tiler.Associated()` for generic TIFFs emits one of: `"label"`, `"overview"`, `"thumbnail"`, `"associated"` (heuristic-fallback)
>
> The wide-field slide image (when the heuristic classifier identifies one) is emitted as `"overview"` from v0.15 onward, matching DICOM PS3.3 + upstream Python opentile + opentile-go's other format readers. Pre-v0.15 (v0.10–v0.14) this was emitted as `"macro"`. Consumer migration: where you switch on `Type()` for generic-TIFF associated images, replace `case "macro":` with `case "overview":`.

Also: update any references to `Kind()` → `Type()` in this doc.

- [ ] **Step 3: Update `docs/formats/leicascn.md`**

Find references to `Kind() == "macro"` (likely around lines 64, 128 per v0.15 audit). Update to `Type() == "overview"`. Add a v0.15 migration note similar to the generictiff.md one (pre-v0.15 was `"macro"`, v0.15+ is `"overview"`, IFE-distinct context preserved).

- [ ] **Step 4: Update `docs/formats/ife.md`**

IFE preserves both `"overview"` AND `"macro"` as distinct values per the IFE spec. Add or strengthen a section calling this out:

> ## v0.15 note: `"overview"` and `"macro"` are distinct in IFE
>
> The Iris IFE spec defines `LABEL_OVERVIEW` and `LABEL_MACRO` as separate kind values. opentile-go's IFE reader preserves this distinction: an IFE file may carry both. **This is the only opentile-go format where `"macro"` is a valid Type() value.** Other formats (SVS, NDPI, Philips, OME-TIFF, BIF, generic-TIFF, Leica SCN) all use `"overview"` for the wide-field slide image (per the DICOM PS3.3 convention).

Also: replace any `Kind()` → `Type()` references in this doc.

- [ ] **Step 5: Verify**

```bash
cd /Users/cornish/GitHub/opentile-go
grep -rn '\.Kind()\|`Kind`\|"Kind\b' docs/formats/ README.md
```

Expected: 0 hits (`Kind` references should now be `Type`).

```bash
grep -rn '"macro"' docs/formats/generictiff.md docs/formats/leicascn.md
```

Expected: hits only in migration-note context (e.g., "pre-v0.15 was `"macro"`"); no active "this format emits macro" claims.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "$(cat <<'EOF'
docs(v0.15): T5 — README + per-format docs (Kind→Type, macro→overview)

README.md Supported-formats table: OME-TIFF / generic-TIFF / Leica
SCN rows updated to reflect actual emitted Type() values. (OME-TIFF
row was stale even pre-v0.15; bundled fix.)

docs/formats/generictiff.md: emitted-Type list now lists "overview"
not "macro"; added v0.15 migration note for consumers.

docs/formats/leicascn.md: ditto.

docs/formats/ife.md: explicit note that IFE is the only opentile-go
format keeping both "overview" and "macro" as Type() values per
the IFE spec; cross-references the DICOM convention used elsewhere.

All `.Kind()` references in narrative docs updated to `.Type()`.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Ship: CHANGELOG + CLAUDE.md + deferred §8i + final gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md`
- Modify: `docs/deferred.md`

- [ ] **Step 1: CHANGELOG [0.15.0] block**

Insert before [0.14.0]:

```markdown
## [0.15.0] — 2026-05-08

Naming-cleanup milestone — renames the `AssociatedImage.Kind()`
method to `Type()` (DICOM ImageType convention) and aligns every
format except Iris IFE on `"overview"` as the canonical name for
the wide-field slide image. Breaking change; pre-1.0; sole-consumer
sign-off granted.

### Breaking changes

- **`AssociatedImage.Kind()` renamed → `Type()`.** Every format
  reader's implementation, every test call site, and the public
  interface in `image.go` updated in lockstep.
- **`formats/generictiff` constants renamed:**
  `KindLabel` → `TypeLabel`, `KindMacro` → `TypeOverview`,
  `KindThumbnail` → `TypeThumbnail`, `KindAssociated` → `TypeAssociated`.
- **Generic-TIFF and Leica SCN emitted-value flip.** Pre-v0.15,
  these two readers emitted `Type() == "macro"` (drift introduced
  in v0.10 / v0.11). v0.15 flips them to `"overview"`, matching:
  - DICOM PS3.3 / Supplement 145 (Image Type 0008,0008 value 3 =
    `OVERVIEW`)
  - Upstream Python opentile (the project we directly port; uses
    `"overview"` everywhere, mapping native OME-XML `"macro"` to
    `"overview"` via the OME tiler)
  - opentile-go's own SVS / NDPI / Philips / OME-TIFF / BIF readers
    (which already emitted `"overview"` from v0.1 / v0.5 / v0.6 / v0.7)

  **Iris IFE is intentionally exempt** — the IFE spec defines
  `LABEL_MACRO` and `LABEL_OVERVIEW` as distinct kinds, and opentile-
  go's IFE reader preserves both.
- **README OME-TIFF row corrected.** Pre-v0.15 the row claimed the
  format emitted `"macro"`; in fact it emitted `"overview"` since
  v0.6. README was stale, now matches code.

### Consumer migration

```text
Method:
  a.Kind()                            → a.Type()

Constants (formats/generictiff):
  generictiff.KindLabel               → generictiff.TypeLabel
  generictiff.KindMacro               → generictiff.TypeOverview
  generictiff.KindThumbnail           → generictiff.TypeThumbnail
  generictiff.KindAssociated          → generictiff.TypeAssociated

Switch-statement values:
  case "macro":  // generic-TIFF      → case "overview":
  case "macro":  // Leica SCN         → case "overview":
  case "macro":  // Iris IFE          (UNCHANGED — IFE-spec-distinct)
  case "overview": // every other     (UNCHANGED)
```

### Notes

- v0.15 is rename-only. No format-support changes, no new fixtures,
  no perf changes, no behavioral changes for code that already used
  the right name.
- `TestSlideParity` 28 fixtures unchanged from v0.14.
- v1.0 cut still pending.
- cgo footprint unchanged.
```

[Unreleased] block: bump to "after v0.15."

- [ ] **Step 2: CLAUDE.md milestone bump**

Find the v0.14 "Current milestone" header and replace with v0.15:

```markdown
## Current milestone — v0.15 (shipped)

- **Scope:** Naming-cleanup milestone — `AssociatedImage.Kind()`
  renamed to `Type()` (DICOM ImageType convention); generic-TIFF +
  Leica SCN emitted `"macro"` flipped to `"overview"` (aligning
  with DICOM + Python opentile + 6 sibling format readers). Iris
  IFE preserves both as IFE-spec-distinct values. Breaking change;
  pre-1.0; sole-consumer sign-off granted. 6 plan tasks single batch.
- **API additions:** none (rename-only milestone).
- **API breaks:** `AssociatedImage.Kind()` → `Type()`. generictiff
  `KindXxx` constants → `TypeXxx`. Generic-TIFF + Leica SCN value
  flip from `"macro"` to `"overview"`.
- **Active limitations:** unchanged from v0.14. No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  `docs/deferred.md §1a`): two pre-v0.15 deviations RETIRED here —
  generic-TIFF + Leica SCN `"macro"` (now aligned with upstream's
  `"overview"`).
- **Correctness bar:** `make test` green; TestSlideParity 28
  fixtures (unchanged from v0.14).
- **Sealed Q-decisions (8):** Q1 `Kind()` → `Type()` rename; Q2
  constants in lockstep; Q3 stays `string` (no typed enum); Q4
  one-shot value flip (no aliasing); Q5 IFE preserves both kinds;
  Q6 no migration helper; Q7 v0.15.0 tag; Q8 explicit CHANGELOG
  migration block.
- **Deferred forward:** L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9,
  R15. v1.0 cut still pending.
- **Design:** `docs/superpowers/specs/2026-05-08-opentile-go-v15-type-rename-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md`
- **Work branch:** `feat/v0.15`

## Previous milestone — v0.14 (shipped 2026-05-08)

Novel-codec milestone — generic-TIFF reader recognises 4 new tile
compression tag values (WebP / JPEG XL / AVIF / HTJ2K) produced by
the user's wsi-tools transcoder. Plus a wsi-tools ImageDescription
parser. Additive — no breaking changes.
```

The earlier "Previous milestone — v0.13" content collapses into the "Earlier milestones" bullet list.

- [ ] **Step 3: docs/deferred.md §8i**

Insert §8i before §8h (newest-first ordering):

```markdown
## 8i. Retired in v0.15

v0.15 is a small naming-cleanup milestone. Renames the
`AssociatedImage.Kind()` method to `Type()` (DICOM ImageType
convention) and aligns every format except Iris IFE on
`"overview"` as the canonical name for the wide-field slide
image. Breaking change; pre-1.0; sole-consumer sign-off.

**Items shipped:**

- `AssociatedImage.Kind() string` → `Type() string` interface
  rename (image.go); 8 format readers + opentile_test.go mock +
  every test call site updated in lockstep.
- `formats/generictiff` constants:
  `KindLabel` / `KindMacro` / `KindThumbnail` / `KindAssociated`
  →
  `TypeLabel` / `TypeOverview` / `TypeThumbnail` / `TypeAssociated`.
  `KindMacro = "macro"` → `TypeOverview = "overview"` flips name
  AND value in one move.
- `formats/leicascn`: `Type() == "macro"` → `Type() == "overview"`
  for the auxiliary `<image>` element. Pre-v0.15 (sealed in v0.11
  Q8) corrected.
- `formats/ife`: untouched. IFE spec defines `LABEL_MACRO` and
  `LABEL_OVERVIEW` as distinct kinds; both preserved.

**Deviations retired:**

- Pre-v0.15 generic-TIFF emitting `"macro"` instead of upstream's
  `"overview"`. v0.10 introduced this; v0.15 corrects.
- Pre-v0.15 Leica SCN emitting `"macro"` instead of upstream's
  `"overview"`. v0.11 introduced this; v0.15 corrects.

**Architecture invariants preserved:**

- Public API broken intentionally (pre-1.0; sole-consumer sign-off);
  semver-respectful via v0.15.0 tag.
- DICOM-standard naming honored (Image Type 0008,0008 value 3 =
  `OVERVIEW`).
- Upstream Python opentile parity restored on Type() naming.
- v1.0 cut still pending.
- cgo footprint unchanged.

**v0.15 lessons:** sweeping renames of method names + constants in
one milestone benefit from per-task isolation: the method rename
(T1) lands first as a pure mechanical sweep across the entire
codebase; the constant + value flip (T2) lands after; the format-
specific value flip for SCN (T3) lands after that; tests adapt
last (T4). This minimizes the "build temporarily broken" window
and makes individual commits cleanly bisectable.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md`](superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md).
```

- [ ] **Step 4: Final pre-commit verification gate**

```bash
cd /Users/cornish/GitHub/opentile-go
go vet ./... 2>&1 | tail -3
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=$PWD/sample_files go test -count=1 ./... 2>&1 | tail -10
```

Expected: vet clean, gofmt clean, every package green, TestSlideParity 28 fixtures green.

Per-format probe sweep:

```bash
cd /Users/cornish/GitHub/opentile-go
for f in sample_files/svs/CMU-1-Small-Region.svs sample_files/ndpi/CMU-1.ndpi sample_files/philips-tiff/Philips-1.tiff sample_files/ome-tiff/Leica-1.ome.tiff sample_files/bif/Ventana-1.bif sample_files/generic-tiff/avif-out.tiff sample_files/scn/Leica-1.scn; do
  echo "=== $f ===";
  go run /tmp/genericsmoke/main.go "$f" 2>&1 | head -10;
done
```

Expected: every fixture's associated-image lines show `Type() == "overview"` for the wide-field image (no `"macro"` outside IFE fixtures, which we don't probe here). If a stray `"macro"` appears, audit + fix.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md docs/deferred.md
git commit -m "$(cat <<'EOF'
docs(v0.15): T6 — CHANGELOG [0.15.0] + CLAUDE.md milestone bump + deferred §8i

CHANGELOG.md [0.15.0] section: explicit consumer-side migration
block (Kind() → Type(), KindXxx → TypeXxx, macro → overview value
flips); breaking-change call-out; IFE preservation noted.

CLAUDE.md: bump Current milestone v0.14 → v0.15. v0.14 demoted to
Previous; v0.13 / v0.12 / earlier collapsed.

docs/deferred.md §8i new — Retired in v0.15: lists method rename,
constants rename, leicascn value flip, two retired upstream-parity
deviations.

End of milestone; v0.15 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage:**
- §2.1 (method rename) → T1.
- §2.2 (constants rename + KindMacro value flip) → T2.
- §2.3 (per-format value flip) → T2 (generic-TIFF) + T3 (Leica SCN); IFE explicitly out of scope.
- §2.4 (doc fixes bundled) → T1 (image.go interface comment) + T5 (README + per-format docs) + T6 (CHANGELOG / CLAUDE.md / deferred).
- §3 (out of scope) → respected throughout: no aliasing, no typed enum, no IFE changes, no DICOM reader.
- §4 (Q-decisions) → reflected in tasks.
- §5 (test strategy) → T4 (test struct field + assertions) + T6 step 4 (full suite + per-format probe).
- §6 (no new limitations) → T6 docs confirm.
- §7 (plan outline) → matches.
- §8 (verification gates) → T6 step 4.
- §9 (lessons) → T1 step 2's grep audit + T2/T3 sweeps.
- §10 (migration note preview) → T6 step 1 CHANGELOG.

**Placeholder scan:** every step has exact code blocks, exact paths, and expected outputs. No TBDs.

**Type consistency:** `Type()` method, `TypeXxx` constants, `Type` struct field — all consistent across T1 → T6. The interface doc in T1 lists `"overview"` / `"macro"` / etc. consistent with what T2/T3 will emit.

**Risks:**

- **R1 — Sed reliability.** Per v0.12 lesson, BSD sed silently misses identifiers on word-boundary patterns. T1/T2/T3 instruct the implementer to use `Edit` (not sed) and pair every sweep with a `grep` audit. The plan explicitly calls this out.
- **R2 — Build temporarily broken between T1 and T2.** Generic-TIFF tests reference `KindMacro` until T2 lands. T1's verification scope is "build clean," not "all tests pass." Plan flags this and instructs the implementer to defer full `go test` to T2. Acceptable per per-task-batch model.
- **R3 — Concrete-type tests in format packages.** Some format-internal tests may construct concrete types directly and call `.Kind()` on the concrete (not interface) value. T1's grep for `\.Kind()` catches both. Audit confirms 0 hits remain after the sweep.
- **R4 — README `Kind()` references in narrative.** T5 grep audit catches these. Documented.
- **R5 — Probe script.** `/tmp/genericsmoke/main.go` is from prior milestones; script may have already been updated to use `Type()` (or may still call `Kind()`). T6 step 4 should verify the probe script compiles before running.
