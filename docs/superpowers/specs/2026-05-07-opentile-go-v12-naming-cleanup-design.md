# opentile-go v0.12 — naming cleanup

**Status:** sealed 2026-05-07.
**Work branch:** `feat/v0.12`.
**Headline:** breaking-API rename milestone consolidating four deferred naming items from `docs/deferred.md §11` into one v0.12 cut. No new format support, no new features — entirely a public-API and internal-naming polish pass that pre-pays the eventual v1.0 cleanliness cost without committing to v1.0 yet.

## 1. Scope

Four discrete renames, each independently derived from `docs/deferred.md §11` and confirmed during the 2026-05-07 brainstorm:

### R1. `striped` → `stripped` terminology (full public rename)

TIFF 6.0 spec uses "strip", not "stripe" — the canonical tags are `StripOffsets` (273), `StripByteCounts` (279), `RowsPerStrip` (278). Our codebase has used "stripe/striped" inconsistently since v0.2's NDPI work. v0.12 renames the public NDPI API and all internal usages.

**Public API renames** (in `formats/ndpi`):

| Old name | New name | Justification |
|---|---|---|
| `StripeInfo` (struct) | `StripInfo` | TIFF spec uses bare singular "Strip" |
| `StripeOffsets` (field) | `StripOffsets` | Exact TIFF tag name (273) |
| `StripeByteCounts` (field) | `StripByteCounts` | Exact TIFF tag name (279) |
| `StripeW`, `StripeH` (fields) | `StripW`, `StripH` | NDPI's 2D-strip pixel dimensions; no TIFF-spec equivalent (TIFF strips are 1D); `Strip` matches singular spec form |
| `StripedW`, `StripedH` (fields) | `GridW`, `GridH` | These are the strip-grid count, not pixel dims. `StrippedW` would be grammatically wrong (a count is not a width). `Grid` mirrors our existing `Level.Grid()` API and matches openslide's `tiles_across`/`tiles_down` semantic |

**Internal renames:**
- File renames: `formats/ndpi/striped.go` → `stripped.go`, `formats/ndpi/striped_test.go` → `stripped_test.go`, `formats/ndpi/stripes.go` → `strips.go`.
- Internal type rename: `stripedImage` → `strippedImage` (and any ad-hoc helpers using the same root).
- All comments referring to "stripe", "stripes", "striped", "Stripped" — audit and rename to the strip/stripped equivalents per their context.

### R2. `FormatPhilips` value + identifier rename

Both the Format constant value and the Go identifier change. Mirrors the v0.10/v0.11 naming convention established by `FormatGenericTIFF` and `FormatLeicaSCN`.

| Surface | Old | New |
|---|---|---|
| Go identifier | `opentile.FormatPhilips` | `opentile.FormatPhilipsTIFF` |
| String value | `"philips"` | `"philips-tiff"` |
| Tiler's Format() returns | `"philips"` | `"philips-tiff"` |

Justification: Philips has multiple WSI file formats (TIFF; iSyntax; future). The bare `"philips"` identifier is ambiguous; the `-tiff` suffix locks it to the TIFF dialect. Consumers comparing `tiler.Format() == "philips"` break.

### R3. `FormatOME` value + identifier rename

Symmetric to R2.

| Surface | Old | New |
|---|---|---|
| Go identifier | `opentile.FormatOME` | `opentile.FormatOMETIFF` |
| String value | `"ome"` | `"ome-tiff"` |
| Tiler's Format() returns | `"ome"` | `"ome-tiff"` |

Justification: OME has multiple file formats (OME-TIFF; OME-Zarr; OME-NGFF). Bare `"ome"` ambiguously claims the family. Consumers comparing `tiler.Format() == "ome"` break.

### R4. Package directory renames

| Old | New |
|---|---|
| `formats/philips/` | `formats/philipstiff/` |
| `formats/ome/` | `formats/ometiff/` |

Mirrors `formats/generictiff/` and `formats/leicascn/` directory naming. Every direct importer (e.g., consumers calling `philips.MetadataOf` or `ome.MetadataOf`) must update import paths and package qualifier:

```go
// Before
import "github.com/wsilabs/opentile-go/formats/philips"
md, _ := philips.MetadataOf(t)

// After
import "github.com/wsilabs/opentile-go/formats/philipstiff"
md, _ := philipstiff.MetadataOf(t)
```

The package name follows the directory: `package philips` → `package philipstiff`, `package ome` → `package ometiff`.

## 2. Out of scope

- v1.0 cut. v0.12 stays in pre-1.0 territory; subsequent breaking changes remain possible without major-version ceremony. v1.0 is left as a future deliberate cut.
- New format support.
- New API surface (functions, methods, types beyond renames).
- Performance changes.
- Parity oracle changes.
- Test infrastructure changes (TestSlideParity etc. continue working byte-identically; only the Format string they compare against changes).

## 3. Migration impact

### Internal callers (we update everything in v0.12)

- `formats/all/all.go` — registration imports.
- Cross-format helpers / tests that import the renamed packages.
- Test fixture files that reference the old Format string in JSON (`tests/fixtures/*.json` carry `"format": "philips"` or `"format": "ome"` — these need updates per fixture).
- Documentation (`docs/formats/philips.md` → `docs/formats/philipstiff.md`; `docs/formats/ome.md` → `docs/formats/ometiff.md`).

### External callers (none today; documented for future)

We have no external users yet (per the v0.3 invariant in CLAUDE.md). The migration guide for hypothetical future callers lives in `CHANGELOG.md [0.12.0]` Breaking changes section:

```
v0.11 → v0.12 migration:

  Format constants (api compare):
    FormatPhilips      → FormatPhilipsTIFF       ("philips" → "philips-tiff")
    FormatOME          → FormatOMETIFF           ("ome" → "ome-tiff")

  Package import paths:
    formats/philips/   → formats/philipstiff/
    formats/ome/       → formats/ometiff/

  NDPI public types:
    ndpi.StripeInfo            → ndpi.StripInfo
    StripeInfo.StripeOffsets   → StripInfo.StripOffsets
    StripeInfo.StripeByteCounts → StripInfo.StripByteCounts
    StripeInfo.StripeW/H       → StripInfo.StripW/H
    StripeInfo.StripedW/H      → StripInfo.GridW/H
```

## 4. Test fixture file updates

The `tests/fixtures/*.json` files committed under v0.7-v0.11 record `"format": "<value>"` per fixture. Affected fixtures (those for Philips and OME slides):

- `Philips-1.tiff.json` through `Philips-4.tiff.json` (4 fixtures): `"philips"` → `"philips-tiff"`
- `Leica-1.ome.tiff.json`, `Leica-2.ome.tiff.json` (2 fixtures): `"ome"` → `"ome-tiff"`

These fixtures are committed JSON snapshots regenerated via `TestGenerateFixtures`. Re-running the generator with the v0.12 code produces files with the new Format string; we commit those updated JSONs.

`TestSlideParity` does string equality on the Format field, so without the fixture update the test would fail post-rename.

## 5. Sealed Q-decisions log

| ID | Question | Decision | Owner |
|---|---|---|---|
| Q1 | Version cut — v1.0 or v0.12? | **v0.12** | Toby |
| Q2 | striped → stripped: full rename or internal-only? | **Full public rename (R1)** ("we look like clowns with 'striped'") | Toby |
| Q3 | Format constant rename: value-only or value + identifier? | **Both — value + identifier** (R2 + R3) | Toby |
| Q4 | Package directory rename? | **Yes, both** (R4) | Toby |
| Q5 | NDPI grid-count fields name | **`GridW` / `GridH`** (mirrors `Level.Grid()`; spec-grounded analysis showed mechanical "Strapped" rename is grammatically wrong) | Toby |

## 6. Active limitations introduced

None. v0.12 is a rename pass; no new behavior or limitations added.

The four §11 backlog items (R1–R4) move from "deferred" to "retired in v0.12" in `docs/deferred.md §8f` (new retirement audit subsection).

## 7. Plan outline

Single-batch plan tentatively scoped at ~10 tasks. Can be split into 2 batches if review cadence prefers. Headline path:

- **Batch A — NDPI strip rename (R1):** rename file + internal types + public StripInfo struct + 6 fields. Update all callers (formats/ndpi internal use + tests + parity oracle). Update CHANGELOG migration note.
- **Batch B — Format constants + packages (R2–R4):** rename `formats/philips` → `formats/philipstiff` directory + `package` decl + `FormatPhilips` → `FormatPhilipsTIFF` + value `"philips"` → `"philips-tiff"`. Then mirror for OME. Update `formats/all/all.go`. Update `docs/formats/philips.md` → `docs/formats/philipstiff.md` (rename file). Update test fixtures.
- **Batch C — Docs + ship:** new `docs/deferred.md §8f` v0.12 retirement audit. CHANGELOG.md `[0.12.0]` section with full migration guide. CLAUDE.md milestone bump. README touch-ups (Format() example values; deviation table refresh if needed).

Plan written separately at `docs/superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md`.

## 8. Verification

End-of-milestone gates:

- `go vet ./...` clean.
- `make test` green (full module): `go test ./... -race -count=1` with `OPENTILE_TESTDIR` set.
- `make parity` green (Python opentile + tifffile oracles): no Format-string-comparison failures.
- `TestSlideParity` green on all 24 fixtures with the regenerated Philips + OME fixture JSONs.
- Format() string sanity: every test that compares against a Format constant uses the new identifier.
- Grep audit at end of milestone: zero `Stripe`/`StripeI`/`Striped` outside committed XML test fixtures (`testdata/*.xml`) and historical CHANGELOG entries.
