# opentile-go v0.12 — naming cleanup implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land four breaking-API renames (R1 striped→stripped public API, R2 FormatPhilips, R3 FormatOME, R4 package directories) consolidated in one v0.12 cut. No new features.

**Architecture:** Mechanical rename pass. Each rename is independent; ordering minimizes mid-batch test breakage by renaming a self-contained unit (NDPI's strip API; Philips package; OME package) per task and confirming `make test` green between tasks.

**Tech stack:** `git mv` for file/directory moves (preserves history); BSD `sed -i ''` for in-file text replacements; targeted `Edit` calls for nuanced changes; existing `TestSlideParity` + `make test` as the green-bar gate.

**Spec:** [`docs/superpowers/specs/2026-05-07-opentile-go-v12-naming-cleanup-design.md`](../specs/2026-05-07-opentile-go-v12-naming-cleanup-design.md).

---

## Task layout

9 tasks across 2 batches:

- **Batch A — Renames** (T1-T5)
- **Batch B — Docs + ship** (T6-T9)

Each task is one git commit. End-of-batch checkpoint runs full `OPENTILE_TESTDIR=$PWD/sample_files go test ./...` before proceeding.

---

## Batch A — Renames (5 tasks)

### T1 — NDPI strip rename: public API (`StripeInfo` → `StripInfo` + 6 fields)

**Files:**
- Modify: `formats/ndpi/stripes.go` (struct + field renames)
- Modify: `formats/ndpi/ndpi.go`, `formats/ndpi/striped.go`, `formats/ndpi/tilesize.go`, `formats/ndpi/striped_test.go`, `formats/ndpi/tilereader_test.go`, `formats/ndpi/ndpi_test.go`, `formats/ndpi/l12_test.go` (every internal caller of the renamed struct/fields)

Renames mandated by spec §1.R1:

| Old | New |
|---|---|
| `StripeInfo` (type) | `StripInfo` |
| `StripeOffsets` (field) | `StripOffsets` |
| `StripeByteCounts` (field) | `StripByteCounts` |
| `StripeW` (field) | `StripW` |
| `StripeH` (field) | `StripH` |
| `StripedW` (field) | `GridW` |
| `StripedH` (field) | `GridH` |

- [ ] **Step 1: Audit all use sites**

```bash
grep -rn "StripeInfo\b\|StripeOffsets\b\|StripeByteCounts\b\|StripeW\b\|StripeH\b\|StripedW\b\|StripedH\b" --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Expected: hits in `formats/ndpi/*.go` (production + tests). No hits outside `formats/ndpi/`. If hits exist elsewhere, add those files to the modify list.

- [ ] **Step 2: Apply renames**

For type and 4 of the 6 fields (the renames don't collide with existing identifiers):

```bash
cd /Users/cornish/GitHub/opentile-go
find formats/ndpi -name '*.go' -exec sed -i '' \
  -e 's/\bStripeInfo\b/StripInfo/g' \
  -e 's/\bStripeOffsets\b/StripOffsets/g' \
  -e 's/\bStripeByteCounts\b/StripByteCounts/g' \
  {} \;
```

For `StripeW`/`StripeH` → `StripW`/`StripH`: sed `\b` doesn't reliably catch single-letter boundaries on BSD. Do these as targeted replacements:

```bash
find formats/ndpi -name '*.go' -exec sed -i '' \
  -e 's/StripeW /StripW /g' \
  -e 's/StripeW,/StripW,/g' \
  -e 's/StripeW)/StripW)/g' \
  -e 's/StripeW$/StripW/g' \
  -e 's/StripeH /StripH /g' \
  -e 's/StripeH,/StripH,/g' \
  -e 's/StripeH)/StripH)/g' \
  -e 's/StripeH$/StripH/g' \
  -e 's/\.StripeW/\.StripW/g' \
  -e 's/\.StripeH/\.StripH/g' \
  {} \;
```

For `StripedW`/`StripedH` → `GridW`/`GridH` (these are field names, not free-floating identifiers):

```bash
find formats/ndpi -name '*.go' -exec sed -i '' \
  -e 's/StripedW /GridW /g' \
  -e 's/StripedW,/GridW,/g' \
  -e 's/StripedW)/GridW)/g' \
  -e 's/\.StripedW/\.GridW/g' \
  -e 's/StripedH /GridH /g' \
  -e 's/StripedH,/GridH,/g' \
  -e 's/StripedH)/GridH)/g' \
  -e 's/\.StripedH/\.GridH/g' \
  {} \;
```

- [ ] **Step 3: Verify no stale identifiers**

```bash
grep -rn "StripeInfo\b\|StripeOffsets\b\|StripeByteCounts\b\|StripeW\b\|StripeH\b\|StripedW\b\|StripedH\b" --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Expected: zero hits. If any remain, fix them with targeted Edit calls.

- [ ] **Step 4: Audit comments referring to the renamed identifiers**

```bash
grep -rn "StripeInfo\|stripe-info\|stripe info\|StripeOffsets\|StripeByteCounts" --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Update comment text to use the new names. Use `Edit` for surgical changes (sed will mangle prose).

- [ ] **Step 5: Run tests**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./formats/ndpi/ 2>&1 | tail -5
```

Expected: `ok` for the package.

- [ ] **Step 6: Run full module test**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: every package `ok`. The NDPI rename is internal to one package; no other packages should break.

- [ ] **Step 7: Commit**

```bash
git add formats/ndpi/
git commit -m "refactor(ndpi): T1 — rename StripeInfo → StripInfo + 6 fields (R1 public API)

Per v0.12 spec §1.R1, NDPI's public stripe-related identifiers rename to
match TIFF spec terminology + opentile-go internal-API conventions:

  StripeInfo                 → StripInfo
  StripeOffsets              → StripOffsets    (TIFF tag 273)
  StripeByteCounts           → StripByteCounts (TIFF tag 279)
  StripeW, StripeH           → StripW, StripH  (NDPI 2D-strip pixel dims)
  StripedW, StripedH         → GridW, GridH    (mirrors Level.Grid())

Breaking change to the formats/ndpi public API; no external callers
today (per v0.3 invariant). Internal NDPI callers + tests updated.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T2 — NDPI strip rename: internal types + file renames

**Files:**
- Rename: `formats/ndpi/striped.go` → `formats/ndpi/stripped.go`
- Rename: `formats/ndpi/striped_test.go` → `formats/ndpi/stripped_test.go`
- Rename: `formats/ndpi/stripes.go` → `formats/ndpi/strips.go`
- Modify (renamed): all three files for `stripedImage` → `strippedImage` + comment audit

- [ ] **Step 1: Audit internal type usage**

```bash
grep -rn "stripedImage\|stripeImage\|striped image\|stripe image" --include="*.go" /Users/cornish/GitHub/opentile-go/formats/ndpi/
```

Expected: `stripedImage` references across `striped.go`, `striped_test.go`, `ndpi.go`, possibly others.

- [ ] **Step 2: Rename files via git mv (preserves history)**

```bash
cd /Users/cornish/GitHub/opentile-go
git mv formats/ndpi/striped.go formats/ndpi/stripped.go
git mv formats/ndpi/striped_test.go formats/ndpi/stripped_test.go
git mv formats/ndpi/stripes.go formats/ndpi/strips.go
git status | head -10
```

Expected: 3 renames staged.

- [ ] **Step 3: Rename internal type identifier**

```bash
find formats/ndpi -name '*.go' -exec sed -i '' \
  -e 's/\bstripedImage\b/strippedImage/g' \
  {} \;
```

- [ ] **Step 4: Audit comments + remaining "stripe"/"striped" references**

```bash
grep -rn "stripe\|Stripe\|striped\|Striped" --include="*.go" /Users/cornish/GitHub/opentile-go/formats/ndpi/
```

Expected: hits only in legitimate cases:
- Comments describing TIFF tag concepts (review case-by-case — most should rename to "strip"/"strips"/"stripped")
- Function names where "Stripe" was the legacy spelling — rename via Edit calls

Use `Edit` to rename per-comment (don't sed prose).

- [ ] **Step 5: Run tests**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./formats/ndpi/ 2>&1 | tail -5
```

Expected: `ok`.

- [ ] **Step 6: Verify clean grep**

```bash
grep -rn "stripedImage\|stripeImage" --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Expected: zero hits.

- [ ] **Step 7: Commit**

```bash
git add formats/ndpi/
git commit -m "refactor(ndpi): T2 — rename file paths + stripedImage internal type

Per v0.12 spec §1.R1 internal-rename block:

  formats/ndpi/striped.go        → stripped.go
  formats/ndpi/striped_test.go   → stripped_test.go
  formats/ndpi/stripes.go        → strips.go
  internal stripedImage type     → strippedImage

Comment audit updates 'stripe'/'striped' references to 'strip'/
'stripped' per the TIFF spec usage; legacy external function names
preserved in test files where they document historical context.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T3 — Philips package rename + FormatPhilipsTIFF

**Files:**
- Rename: `formats/philips/` → `formats/philipstiff/` (entire directory)
- Modify (in renamed dir): `formats/philipstiff/*.go` — `package philips` → `package philipstiff`
- Modify: `tiler.go` — `FormatPhilips Format = "philips"` → `FormatPhilipsTIFF Format = "philips-tiff"` + comment update
- Modify: `formats/all/all.go` — import path + `philips.New()` → `philipstiff.New()`
- Modify: any other `formats/philips` import in module (tests, oracle code)

- [ ] **Step 1: Audit current Philips touch points**

```bash
grep -rln "formats/philips\|FormatPhilips\b\|philips\.New\|philips\.MetadataOf" --include="*.go" /Users/cornish/GitHub/opentile-go/
grep -rln '"philips"' --include="*.go" /Users/cornish/GitHub/opentile-go/
```

- [ ] **Step 2: Rename directory**

```bash
cd /Users/cornish/GitHub/opentile-go
git mv formats/philips formats/philipstiff
git status | head -25
```

Expected: ~10-15 file renames staged.

- [ ] **Step 3: Update package declarations**

```bash
find formats/philipstiff -name '*.go' -exec sed -i '' \
  -e 's/^package philips$/package philipstiff/' \
  {} \;
grep -l "^package philips$\|^package philipstiff$" formats/philipstiff/*.go | head
```

Expected: every .go file in `formats/philipstiff/` shows `package philipstiff` only (no `philips`).

Also fix the package doc comment (the `// Package philips` line). Find it:

```bash
grep -rn "^// Package philips\b" formats/philipstiff/
```

Use `Edit` to update each match: `Package philips` → `Package philipstiff`.

- [ ] **Step 4: Update import path + identifier across the module**

```bash
cd /Users/cornish/GitHub/opentile-go
# Use protective placeholder dance to avoid double-renames if any
# imports already mention philipstiff (none should yet, but defensive).
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's|"github.com/cornish/opentile-go/formats/philipstiff"|XXX_FPT_XXX|g' \
  -e 's|"github.com/cornish/opentile-go/formats/philips"|"github.com/cornish/opentile-go/formats/philipstiff"|g' \
  -e 's|XXX_FPT_XXX|"github.com/cornish/opentile-go/formats/philipstiff"|g' \
  {} \;
```

- [ ] **Step 5: Update package qualifier `philips.X` → `philipstiff.X`**

```bash
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's/\bphilips\.New(/philipstiff.New(/g' \
  -e 's/\bphilips\.MetadataOf(/philipstiff.MetadataOf(/g' \
  -e 's/\bphilips\.Metadata\b/philipstiff.Metadata/g' \
  {} \;
```

Verify no stale `philips.` refs:

```bash
grep -rn "\bphilips\." --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Expected: zero hits (the `package philipstiff` dir uses bare identifiers, not `philips.X`).

- [ ] **Step 6: Update Format constant**

Edit `tiler.go`:

```go
// Before
FormatPhilips Format = "philips"

// After
// FormatPhilipsTIFF is the Philips TIFF reader. Renamed in v0.12
// to align with v0.10/v0.11's `FormatGenericTIFF`/`FormatLeicaSCN`
// convention; Philips has multiple WSI file formats (TIFF; iSyntax)
// so the bare "philips" identifier was ambiguous.
FormatPhilipsTIFF Format = "philips-tiff"
```

Then update all references in module:

```bash
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's/\bFormatPhilips\b/FormatPhilipsTIFF/g' \
  {} \;
```

- [ ] **Step 7: Build + test**

```bash
go build ./... 2>&1 | head -10
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: build clean. Tests *may* fail on `TestSlideParity` for Philips fixtures (the `"format": "philips"` string in fixture JSON no longer matches `FormatPhilipsTIFF == "philips-tiff"`). T5 fixes the fixtures.

If only Philips fixture-parity tests fail, that's expected — proceed. If anything else fails, fix before commit.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(philips): T3 — rename formats/philips → formats/philipstiff (R2 + R4)

Per v0.12 spec §1.R2 + §1.R4 — both the package directory and the
Format constant rename land in this single commit:

  formats/philips/             → formats/philipstiff/
  package philips              → package philipstiff
  opentile.FormatPhilips       → opentile.FormatPhilipsTIFF
  string value 'philips'       → 'philips-tiff'
  Tiler.Format() return value  → 'philips-tiff'

Mirrors v0.10's FormatGenericTIFF and v0.11's FormatLeicaSCN naming
convention. Philips has multiple WSI file formats (TIFF; iSyntax);
the bare 'philips' was ambiguous.

Test fixture updates (4 Philips fixture JSONs carrying the legacy
'format' string) land in T5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T4 — OME package rename + FormatOMETIFF

Symmetric to T3. Same commands with `philips` → `ome` substitutions.

**Files:**
- Rename: `formats/ome/` → `formats/ometiff/`
- Modify (in renamed dir): `formats/ometiff/*.go` — `package ome` → `package ometiff`
- Modify: `tiler.go` — `FormatOME` → `FormatOMETIFF`, value `"ome"` → `"ome-tiff"`
- Modify: `formats/all/all.go` — import + `ome.New()` → `ometiff.New()`
- Modify: any other `formats/ome` import in module

- [ ] **Step 1: Audit current OME touch points**

```bash
grep -rln "formats/ome\b\|formats/ome/\|FormatOME\b\|ome\.New\|ome\.MetadataOf" --include="*.go" /Users/cornish/GitHub/opentile-go/
grep -rln '"ome"' --include="*.go" /Users/cornish/GitHub/opentile-go/
```

- [ ] **Step 2: Rename directory**

```bash
cd /Users/cornish/GitHub/opentile-go
git mv formats/ome formats/ometiff
```

- [ ] **Step 3: Update package declarations**

```bash
find formats/ometiff -name '*.go' -exec sed -i '' \
  -e 's/^package ome$/package ometiff/' \
  {} \;
```

Edit the package doc comment (`// Package ome ...` → `// Package ometiff ...`) via `Edit`.

- [ ] **Step 4: Update import path**

```bash
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's|"github.com/cornish/opentile-go/formats/ometiff"|XXX_FOT_XXX|g' \
  -e 's|"github.com/cornish/opentile-go/formats/ome"|"github.com/cornish/opentile-go/formats/ometiff"|g' \
  -e 's|XXX_FOT_XXX|"github.com/cornish/opentile-go/formats/ometiff"|g' \
  {} \;
```

- [ ] **Step 5: Update package qualifier**

```bash
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's/\bome\.New(/ometiff.New(/g' \
  -e 's/\bome\.MetadataOf(/ometiff.MetadataOf(/g' \
  -e 's/\bome\.Metadata\b/ometiff.Metadata/g' \
  {} \;
```

Verify no stale `ome.` refs:

```bash
grep -rn "\bome\." --include="*.go" /Users/cornish/GitHub/opentile-go/
```

Expected: zero hits.

- [ ] **Step 6: Update Format constant**

Edit `tiler.go`:

```go
// Before
FormatOME     Format = "ome"

// After
// FormatOMETIFF is the OME-TIFF reader. Renamed in v0.12 to align
// with v0.10/v0.11's `FormatGenericTIFF`/`FormatLeicaSCN` convention;
// OME has multiple file formats (OME-TIFF, OME-Zarr, OME-NGFF) so
// the bare "ome" identifier was ambiguous.
FormatOMETIFF Format = "ome-tiff"
```

```bash
find . -name '*.go' -not -path './sample_files/*' -exec sed -i '' \
  -e 's/\bFormatOME\b/FormatOMETIFF/g' \
  {} \;
```

- [ ] **Step 7: Build + test**

```bash
go build ./... 2>&1 | head -10
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: build clean. Philips fixtures + OME fixtures may fail TestSlideParity due to old format strings; T5 fixes them.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(ometiff): T4 — rename formats/ome → formats/ometiff (R3 + R4)

Symmetric to T3:

  formats/ome/                 → formats/ometiff/
  package ome                  → package ometiff
  opentile.FormatOME           → opentile.FormatOMETIFF
  string value 'ome'           → 'ome-tiff'
  Tiler.Format() return value  → 'ome-tiff'

OME has multiple file formats (OME-TIFF, OME-Zarr, OME-NGFF). The
bare 'ome' constant ambiguously claimed the family.

Test fixture updates land in T5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T5 — Test fixture format-string updates

**Files:**
- Modify: `tests/fixtures/Philips-1.tiff.json`, `Philips-2.tiff.json`, `Philips-3.tiff.json`, `Philips-4.tiff.json` (4 files)
- Modify: `tests/fixtures/Leica-1.ome.tiff.json`, `Leica-2.ome.tiff.json` (2 files)

The fixture JSONs carry `"format": "philips"` or `"format": "ome"` at the top level. Update via sed (each file has exactly one such line).

- [ ] **Step 1: Verify the fixtures need updating**

```bash
grep -l '"format":\s*"philips"' tests/fixtures/Philips-*.tiff.json
grep -l '"format":\s*"ome"' tests/fixtures/Leica-*.ome.tiff.json
```

Expected: 4 Philips files + 2 OME files.

- [ ] **Step 2: Apply sed renames**

```bash
sed -i '' \
  -e 's/"format": "philips"/"format": "philips-tiff"/g' \
  tests/fixtures/Philips-*.tiff.json

sed -i '' \
  -e 's/"format": "ome"/"format": "ome-tiff"/g' \
  tests/fixtures/Leica-*.ome.tiff.json
```

- [ ] **Step 3: Verify no stale strings**

```bash
grep -l '"format": "philips"' tests/fixtures/*.json
grep -l '"format": "ome"' tests/fixtures/*.json
```

Expected: no matches.

- [ ] **Step 4: Run TestSlideParity**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 -run TestSlideParity ./tests/ 2>&1 | tail -10
```

Expected: green. All Philips + OME parity tests now pass against the new format strings.

- [ ] **Step 5: Run full module test**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: every package `ok`. End-of-Batch-A green bar.

- [ ] **Step 6: Commit**

```bash
git add tests/fixtures/Philips-*.tiff.json tests/fixtures/Leica-*.ome.tiff.json
git commit -m "test(v0.12): T5 — update fixture format strings (philips→philips-tiff, ome→ome-tiff)

Sample-tile SHA fixtures committed under v0.7-v0.11 record
'format': 'philips' / 'ome' at the top level. Post-T3/T4 the
Tiler.Format() returns 'philips-tiff' / 'ome-tiff'; fixtures must
match for TestSlideParity to pass.

Updated 6 fixture JSONs:
  Philips-1.tiff.json  Philips-2.tiff.json  Philips-3.tiff.json
  Philips-4.tiff.json  Leica-1.ome.tiff.json  Leica-2.ome.tiff.json

End of Batch A: all renames in place; full module test green.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

End-of-batch-A checkpoint: `make test` green; `go vet ./...` clean. Public API renames in place; no docs or CHANGELOG updates yet (Batch B).

---

## Batch B — Docs + ship (4 tasks)

### T6 — `docs/deferred.md` updates

**Files:**
- Modify: `docs/deferred.md`

Updates:
1. Add §8f "Retired in v0.12" subsection (mirrors §8e shape).
2. Remove the 2 retired backlog rows from §11 ("Striped → stripped" and "Naming corrections").
3. Update existing §1a entries that mention `FormatPhilips` / `FormatOME` to reflect the new identifiers.
4. Update existing §2 / §11 entries that reference renamed packages.

- [ ] **Step 1: Add §8f retirement audit**

Insert before `## 8e. Retired in v0.11`:

```markdown
## 8f. Retired in v0.12

v0.12 is a focused naming-cleanup milestone. No new format support;
no new features; no API additions — entirely a breaking-API rename
pass consolidating four `docs/deferred.md §11` items.

**Items shipped:**

- **R1 — `striped` → `stripped` terminology rename.** Public NDPI
  API renamed: `formats/ndpi.StripeInfo` → `StripInfo`; 6 field
  renames. File renames: `striped.go` → `stripped.go`,
  `stripes.go` → `strips.go`, `striped_test.go` →
  `stripped_test.go`. Internal `stripedImage` → `strippedImage`.
- **R2 — `FormatPhilips` rename.** Identifier
  `opentile.FormatPhilips` → `FormatPhilipsTIFF`; string value
  `"philips"` → `"philips-tiff"`.
- **R3 — `FormatOME` rename.** Identifier `opentile.FormatOME` →
  `FormatOMETIFF`; string value `"ome"` → `"ome-tiff"`.
- **R4 — Package directory renames.** `formats/philips/` →
  `formats/philipstiff/`; `formats/ome/` → `formats/ometiff/`.
  Package names follow: `package philips` → `package philipstiff`;
  `package ome` → `package ometiff`.

**Test impact:** 6 fixture JSONs (4 Philips + 2 OME) updated to
record the new format strings; all 24-fixture `TestSlideParity`
green post-rename.

**Architecture invariants preserved:**

- Public API is more consistent post-rename: every Format constant
  now follows the `Format<Vendor><Tag>` convention
  (FormatGenericTIFF, FormatLeicaSCN, FormatPhilipsTIFF,
  FormatOMETIFF) plus the unambiguous-vendor short forms (FormatSVS,
  FormatNDPI, FormatBIF, FormatIFE).
- v1.0 cut not committed (per Q1). v0.12 stays in pre-1.0 territory;
  subsequent breaking changes remain possible without major-version
  ceremony.
- cgo footprint unchanged.
- No new active limitations.

**Migration guide:** see `CHANGELOG.md [0.12.0]` Breaking changes
section for the per-symbol old → new table.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md`](superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md)
(9 tasks across Batches A–B).

---
```

- [ ] **Step 2: Remove retired §11 backlog rows**

Find the §11 backlog table rows for the two retired items:

```bash
grep -n "Fix .striped. → .stripped." docs/deferred.md
grep -n "Naming corrections.*Format constants.*format-package" docs/deferred.md
```

Use `Edit` to delete both rows. The backlog table loses these two entries; the related "Note on the terminology fix" + "Note on the naming corrections" subsections may stay (they document v0.12-pre context for future readers).

- [ ] **Step 3: Update §1a entries referencing renamed packages**

Find any §1a deviation entries that mention `formats/philips` or `formats/ome` paths or `FormatPhilips` / `FormatOME` constants:

```bash
grep -n "formats/philips\b\|formats/ome\b\|FormatPhilips\b\|FormatOME\b" docs/deferred.md
```

Update via `Edit`. Most occurrences will be in §1a entries (e.g., the multi-image OME deviation references `FormatOME`).

- [ ] **Step 4: Update §2 + §11 entries referencing renamed packages**

Same grep + Edit pattern across the rest of `docs/deferred.md`.

- [ ] **Step 5: Verify no stale references**

```bash
grep -n "formats/philips\b\|formats/ome\b\|FormatPhilips\b\|FormatOME\b\|StripeInfo\b" docs/deferred.md
```

Expected: zero hits (or only inside §8f retirement audit text where the OLD names are explicitly documented).

- [ ] **Step 6: Commit**

```bash
git add docs/deferred.md
git commit -m "docs(v0.12): T6 — deferred.md updates (§8f retirement audit + §11 cleanup)

§8f new — Retired in v0.12: lists R1-R4 renames, test impact,
architecture invariants preserved, migration-guide pointer.

§11 — removes the two retired backlog rows ('Fix striped →
stripped terminology' and 'Naming corrections — Format constants
+ format-package naming'). The 'Note on the terminology fix' and
'Note on the naming corrections' subsections kept for
historical-context reference.

§1a + §2 entries that referenced the renamed packages or constants
(FormatPhilips, FormatOME, formats/philips, formats/ome) updated
to use the new names (FormatPhilipsTIFF, FormatOMETIFF,
formats/philipstiff, formats/ometiff).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T7 — Format-doc renames (`philips.md` → `philipstiff.md`, `ome.md` → `ometiff.md`)

**Files:**
- Rename: `docs/formats/philips.md` → `docs/formats/philipstiff.md`
- Rename: `docs/formats/ome.md` → `docs/formats/ometiff.md`
- Modify (renamed): both files for content updates (header H1 + references to old constant/package names)
- Modify: any other `docs/**/*.md` referencing the renamed format-doc paths

- [ ] **Step 1: Rename files**

```bash
cd /Users/cornish/GitHub/opentile-go
git mv docs/formats/philips.md docs/formats/philipstiff.md
git mv docs/formats/ome.md docs/formats/ometiff.md
```

- [ ] **Step 2: Update H1 + content of each renamed file**

For `docs/formats/philipstiff.md`:
- H1: `# Philips TIFF` (already uses "Philips TIFF" likely; verify)
- Replace `FormatPhilips` → `FormatPhilipsTIFF` across the file.
- Replace `formats/philips/` → `formats/philipstiff/`.
- Replace `philips.MetadataOf` → `philipstiff.MetadataOf`.
- Replace `package philips` → `package philipstiff`.

```bash
sed -i '' \
  -e 's/\bFormatPhilips\b/FormatPhilipsTIFF/g' \
  -e 's|formats/philips/|formats/philipstiff/|g' \
  -e 's|formats/philips\b|formats/philipstiff|g' \
  -e 's/\bphilips\.MetadataOf\b/philipstiff.MetadataOf/g' \
  -e 's/\bpackage philips\b/package philipstiff/g' \
  docs/formats/philipstiff.md
```

Symmetric for `docs/formats/ometiff.md`:

```bash
sed -i '' \
  -e 's/\bFormatOME\b/FormatOMETIFF/g' \
  -e 's|formats/ome/|formats/ometiff/|g' \
  -e 's|formats/ome\b|formats/ometiff|g' \
  -e 's/\bome\.MetadataOf\b/ometiff.MetadataOf/g' \
  -e 's/\bpackage ome\b/package ometiff/g' \
  docs/formats/ometiff.md
```

- [ ] **Step 3: Update cross-references in other docs**

```bash
grep -rln "docs/formats/philips\.md\|docs/formats/ome\.md\|formats/philips\.md\|formats/ome\.md" docs/ README.md CLAUDE.md CHANGELOG.md
```

For each match, update via `Edit`.

```bash
# Most direct matches
sed -i '' \
  -e 's|docs/formats/philips\.md|docs/formats/philipstiff.md|g' \
  -e 's|docs/formats/ome\.md|docs/formats/ometiff.md|g' \
  -e 's|formats/philips\.md|formats/philipstiff.md|g' \
  -e 's|formats/ome\.md|formats/ometiff.md|g' \
  docs/deferred.md docs/formats/*.md README.md CLAUDE.md CHANGELOG.md
```

Verify clean:

```bash
grep -rn "docs/formats/philips\.md\|docs/formats/ome\.md" docs/ README.md CLAUDE.md CHANGELOG.md
```

Expected: zero hits.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs(v0.12): T7 — rename format docs (philips.md → philipstiff.md, ome.md → ometiff.md)

Mirrors v0.10's docs/formats/generictiff.md and v0.11's
docs/formats/leicascn.md naming convention. File contents updated:

  docs/formats/philips.md → docs/formats/philipstiff.md
    FormatPhilips → FormatPhilipsTIFF
    formats/philips/ → formats/philipstiff/
    philips.MetadataOf → philipstiff.MetadataOf
    package philips → package philipstiff

  docs/formats/ome.md → docs/formats/ometiff.md
    (symmetric updates for OME)

Cross-references in docs/deferred.md, README.md, CLAUDE.md, and
CHANGELOG.md updated to point at the new doc paths.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T8 — `CHANGELOG.md [0.12.0]` + migration guide

**Files:**
- Modify: `CHANGELOG.md` — new `[0.12.0]` section + `[Unreleased]` reset

- [ ] **Step 1: Update [Unreleased] block**

Find:

```markdown
## [Unreleased]

Active limitations after v0.11: ...
```

Update post-v0.11 → post-v0.12 (no new active limitations introduced; just update the version reference).

- [ ] **Step 2: Insert [0.12.0] section before [0.11.0]**

```markdown
## [0.12.0] — 2026-05-07

Naming-cleanup milestone — breaking-API rename pass consolidating
four deferred items from `docs/deferred.md §11`. No new format
support, no new features, no API additions. The renames pre-pay
the eventual v1.0 naming-cleanliness cost without committing to
v1.0 (per sealed Q1).

### Breaking changes

#### Format constants

| v0.11 | v0.12 | String value |
|---|---|---|
| `opentile.FormatPhilips` | `opentile.FormatPhilipsTIFF` | `"philips"` → `"philips-tiff"` |
| `opentile.FormatOME` | `opentile.FormatOMETIFF` | `"ome"` → `"ome-tiff"` |

Callers comparing against the old string values or identifiers
must update. Mirrors v0.10 / v0.11's `FormatGenericTIFF` /
`FormatLeicaSCN` naming convention. Philips has multiple file
formats (TIFF; iSyntax); OME has multiple file formats (OME-TIFF,
OME-Zarr, OME-NGFF). The bare names were ambiguous.

#### Package import paths

| v0.11 | v0.12 |
|---|---|
| `github.com/cornish/opentile-go/formats/philips` | `github.com/cornish/opentile-go/formats/philipstiff` |
| `github.com/cornish/opentile-go/formats/ome` | `github.com/cornish/opentile-go/formats/ometiff` |

The package qualifier follows: `philips.MetadataOf` →
`philipstiff.MetadataOf`; `ome.MetadataOf` → `ometiff.MetadataOf`.

#### NDPI public API

| v0.11 | v0.12 |
|---|---|
| `formats/ndpi.StripeInfo` | `formats/ndpi.StripInfo` |
| `StripeInfo.StripeOffsets` | `StripInfo.StripOffsets` |
| `StripeInfo.StripeByteCounts` | `StripInfo.StripByteCounts` |
| `StripeInfo.StripeW`, `StripeH` | `StripInfo.StripW`, `StripH` |
| `StripeInfo.StripedW`, `StripedH` | `StripInfo.GridW`, `GridH` |

TIFF spec uses bare singular "Strip" (tags 273 `StripOffsets`,
279 `StripByteCounts`); the v0.2 NDPI work used "stripe"
inconsistently. v0.12 renames to the spec-faithful form. The
strip-grid count fields (formerly `StripedW` / `StripedH`) are
renamed to `GridW` / `GridH` to mirror our existing `Level.Grid()`
API and avoid the awkward "Stripped width" reading.

### Changed

- File renames preserving git history:
  - `formats/ndpi/striped.go` → `stripped.go`
  - `formats/ndpi/striped_test.go` → `stripped_test.go`
  - `formats/ndpi/stripes.go` → `strips.go`
  - `formats/philips/*` → `formats/philipstiff/*`
  - `formats/ome/*` → `formats/ometiff/*`
  - `docs/formats/philipstiff.md` → `philipstiff.md`
  - `docs/formats/ometiff.md` → `ometiff.md`
- Test fixture format strings: `Philips-{1..4}.tiff.json` and
  `Leica-{1,2}.ome.tiff.json` updated to record the new format
  strings.
- `docs/deferred.md` §8f new (v0.12 retirement audit); §11
  backlog rows for "Fix striped → stripped" and "Naming
  corrections" removed.

### Notes

- Public API is more consistent post-rename: every Format constant
  now follows `Format<Vendor><Tag>` for vendor-disambiguated
  formats (FormatGenericTIFF, FormatLeicaSCN, FormatPhilipsTIFF,
  FormatOMETIFF) plus unambiguous-vendor short forms (FormatSVS,
  FormatNDPI, FormatBIF, FormatIFE).
- v1.0 cut not committed (sealed Q1). v0.12 stays in pre-1.0
  territory.
- No new active limitations.
- cgo footprint unchanged.

```

- [ ] **Step 3: Verify**

```bash
grep -n "## \[0\." CHANGELOG.md | head -5
```

Expected: `[Unreleased]`, `[0.12.0]`, `[0.11.0]`, `[0.10.0]` ... in order.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(v0.12): T8 — CHANGELOG [0.12.0] + breaking-changes migration guide

New [0.12.0] section dated 2026-05-07. Sections:

  Breaking changes:
    - Format constants table (FormatPhilips → FormatPhilipsTIFF,
      FormatOME → FormatOMETIFF + value changes)
    - Package import paths table
    - NDPI public API table (StripeInfo → StripInfo + 6 fields,
      including the StripedW/H → GridW/H rename per Q5)

  Changed: file renames, fixture updates, deferred.md updates

  Notes: post-rename API consistency, v1.0 cut not committed,
    no new limitations, cgo footprint unchanged

[Unreleased] block updated post-v0.11 → post-v0.12.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### T9 — `CLAUDE.md` milestone bump + `README.md` touch-ups

**Files:**
- Modify: `CLAUDE.md` — Current milestone bump v0.11 → v0.12
- Modify: `README.md` — Format() example values + Deviations table

- [ ] **Step 1: CLAUDE.md milestone bump**

Move v0.11 from "Current milestone" to "Previous milestone (shipped 2026-05-06)" with one-paragraph summary. Insert v0.12 as the new current milestone.

The new "Current milestone" block:

```markdown
## Current milestone — v0.12 (shipped)

- **Scope:** Naming-cleanup milestone — breaking-API rename pass
  consolidating four deferred items from `docs/deferred.md §11`. No
  new format support; no new features; no API additions. 9 tasks
  across Batches A–B shipped: NDPI strip rename (R1: StripeInfo →
  StripInfo + 6 fields including StripedW/H → GridW/H per Q5);
  FormatPhilips → FormatPhilipsTIFF + value (R2); FormatOME →
  FormatOMETIFF + value (R3); package directory renames
  formats/philips/ → formats/philipstiff/ and formats/ome/ →
  formats/ometiff/ (R4); test fixture updates (6 JSONs); docs
  refresh.
- **Headline coverage:** every Format constant now follows the
  `Format<Vendor><Tag>` convention for vendor-disambiguated
  formats (FormatGenericTIFF, FormatLeicaSCN, FormatPhilipsTIFF,
  FormatOMETIFF) plus unambiguous-vendor short forms (FormatSVS,
  FormatNDPI, FormatBIF, FormatIFE). NDPI's StripInfo public API
  matches TIFF spec terminology (bare "Strip"; matches tag names
  273 / 279) and our internal Level.Grid() convention.
- **API additions:** none. v0.12 is purely a rename milestone.
- **Behavior change:** every renamed identifier breaks consumers
  that compared against the old name. v0.3 invariant says no
  external users yet; rename safe.
- **Active limitations:** unchanged from v0.11 (L4, L5, L14
  Permanent; L19, L20 v0.7-deferred; L23-L25 v0.8-deferred; L26-L29
  v0.10-deferred; L30-L34 v0.11-deferred). No new L items.
- **Deviations from upstream Python opentile** (canonical list at
  `docs/deferred.md §1a`): unchanged from v0.11 (Format constant
  names + values updated to reflect the new identifiers but no
  new deviations introduced).
- **Correctness bar:** `make test` green; `make parity` green.
  TestSlideParity total still 24 fixtures (no new fixtures); 6
  fixture JSONs (4 Philips + 2 OME) updated to record the new
  format strings.
- **Sealed Q-decisions:** Q1 v0.12 (NOT v1.0 — pre-1.0 territory
  preserved); Q2 full-public striped→stripped rename
  ("clowns with striped"); Q3 both value + identifier; Q4 yes both
  packages; Q5 GridW/GridH (TIFF-spec-grounded analysis).
- **Deferred forward:** L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9,
  R15. v1.0 cut still pending (sealed Q1); future cleanup batches
  may consolidate before cutting.
- **Design:** `docs/superpowers/specs/2026-05-07-opentile-go-v12-naming-cleanup-design.md`
- **Plan:** `docs/superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md`
- **Work branch:** `feat/v0.12`

## Previous milestone — v0.11 (shipped 2026-05-06)

Leica SCN reader (`formats/leicascn/`) covering all 3 openslide-
testdata fixtures + folded-in generictiff validator-cap relaxations
covering Grundium scanner output. First real-fixture exercise of
`Image.SizeC() > 1` (Leica-Fluorescence-1) and first multi-region
"discontinuous scanning" reader (Leica-2). Design + plan + format
doc at
`docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md`,
`docs/superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md`,
`docs/formats/leicascn.md`.

## Earlier milestones

- v0.10 (2026-05-06): generic-TIFF catch-all reader.
- v0.9 (2026-05-01): perf milestone — mmap default + TileInto +
  WarmLevel + splice template.
- ...
```

(Demote earlier `Previous milestone — v0.10` content + collapse v0.9 + earlier into a `## Earlier milestones` bullet list.)

Use `Edit` for these structural changes (sed isn't safe for prose).

- [ ] **Step 2: README.md updates**

```bash
grep -n '"svs", "ndpi", "philips", "ome", "bif", "ife", "generic-tiff", "leica-scn"' README.md
```

Update the Format() example values:

```go
// Old
fmt.Println("format:", t.Format())                 // "svs", "ndpi", "philips", "ome", "bif", "ife", "generic-tiff", "leica-scn"

// New
fmt.Println("format:", t.Format())                 // "svs", "ndpi", "philips-tiff", "ome-tiff", "bif", "ife", "generic-tiff", "leica-scn"
```

Update Supported-formats table — Philips and OME row links may need pointing at the renamed format docs. Check:

```bash
grep -n "docs/formats/philips\|docs/formats/ome" README.md
```

Already updated by T7's sed pass; verify clean.

Update Deviations table (if it references `FormatPhilips` / `FormatOME` constants):

```bash
grep -n "FormatPhilips\|FormatOME" README.md
```

Update via `Edit` if hits.

- [ ] **Step 3: Final pre-commit checks**

```bash
go vet ./... 2>&1 | tail -5
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: clean vet, every package green.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs(v0.12): T9 — CLAUDE.md milestone bump + README.md polish

CLAUDE.md: bump 'Current milestone' from v0.11 → v0.12. Captures
scope (4 renames consolidated), headline (every Format constant
follows the FormatVendorTag convention now), no new API surface,
no new active limitations, sealed Q-decisions (5), deferred-forward
list. v0.11 demoted to 'Previous milestone'; v0.10 + v0.9 + earlier
collapsed into 'Earlier milestones' bullets.

README.md: Format() example values updated to new strings
('philips-tiff', 'ome-tiff'). Supported-formats table doc-link
paths updated by T7's sed pass; final sanity check confirms clean.

End of Batch B; v0.12 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage:**
- §1.R1 striped → stripped public + internal → T1 + T2.
- §1.R2 FormatPhilips rename → T3.
- §1.R3 FormatOME rename → T4.
- §1.R4 package directory renames → T3 + T4 (folded in with the constant rename per task to keep each commit a self-contained unit).
- §3 migration impact — surfaced in CHANGELOG (T8) + deferred.md §8f (T6).
- §4 test fixture updates → T5.
- §5 sealed Q-decisions — referenced in CHANGELOG (T8) + CLAUDE.md (T9).
- §6 active limitations — none introduced; reflected in §8f (T6) and CLAUDE.md (T9).
- §7 plan outline — this document.
- §8 verification gates — `go vet` + `make test` runs at each task; final pre-commit gate at T9.

No spec section uncovered.

**Placeholder scan:** every step has exact commands, exact paths, expected outputs, and code blocks where rewrites are needed. No "TODO", "TBD", "implement later", "add appropriate error handling".

**Type consistency:** the rename table at the top of T1 matches the table in CHANGELOG (T8) Breaking changes section + the renames called out in `docs/deferred.md §8f` (T6). No method-signature drift since this is a pure rename pass — no new types are introduced.

**Risks:**

- **R1 — sed misses an edge case** (e.g., a field reference inside a multi-line struct literal where the whitespace pattern doesn't match the sed rules). Mitigated by `grep -rn` audit at the end of each rename task confirming zero residual stale identifiers.
- **R2 — file rename + content rewrite ordering**. We do `git mv` first, then `sed` on the renamed file. If a step fails mid-task, `git status` shows both rename + content changes; recovering means undoing the rename or re-running the content edits. Each task is committed atomically so this risk is bounded by one task scope.
- **R3 — fixture format-string update breaks parity tests if other format fields drift**. Test impact is ONLY the format string per the v0.11 generator; nothing else in the JSON depends on Format const naming. T5's grep audit confirms.
- **R4 — Package qualifier sed catches false positives**. e.g. `philips` as a comment word vs `philips.MetadataOf` qualifier. Mitigated by anchoring sed to `\bphilips\.New(`, `\bphilips\.MetadataOf(`, `\bphilips\.Metadata\b` — only function/type qualifiers, not bare prose.
