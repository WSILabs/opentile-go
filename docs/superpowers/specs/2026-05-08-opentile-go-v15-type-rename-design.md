# opentile-go v0.15 — AssociatedImage `Kind()` → `Type()` rename + overview-canonical alignment

**Status:** sealed 2026-05-08.
**Work branch:** `feat/v0.15`.
**Headline:** Rename the `AssociatedImage.Kind()` method to `Type()` (DICOM-standard term) and align all formats except Iris IFE on `"overview"` as the canonical name for wide-field slide images. Single-batch breaking-change milestone; pre-1.0; user-as-sole-consumer sign-off granted. v1.0 still pending.

## 1. Motivation

Today's API has two pre-1.0 misnomers that bit a real consumer (the user's wsi-tools-driven viewer):

1. **`Kind()` is an opentile-go-original method name** picked at v0.1 from Go-stdlib idiom (`reflect.Kind`, `os.FileMode.Type()`). It doesn't match the WSI-domain convention. **DICOM PS3.3 / Supplement 145** calls this attribute `ImageType` (tag 0008,0008). Renaming to `Type()` aligns with the formal standards-body term.

2. **`"overview"` vs `"macro"` is split inconsistently across formats.** opentile-go v0.1-v0.9 inherited Python opentile's `"overview"` convention (also matches DICOM ImageType value 3 `OVERVIEW`). v0.10 (generic-TIFF) and v0.11 (Leica SCN) silently introduced `"macro"`, breaking parity with both upstream and the older formats. Iris IFE legitimately uses both as **distinct** values per its own spec (`LABEL_OVERVIEW` ≠ `LABEL_MACRO`); only IFE has standing to keep `"macro"`.

Authority audit (verified 2026-05-08 against canonical sources):

| Source | Canonical name for wide-field slide image | Notes |
|---|---|---|
| **DICOM PS3.3 (formal standard)** | `OVERVIEW` | DICOM ImageType value 3; reference: `openslide-vendor-dicom.c::OVERVIEW_TYPE` |
| **Iris IFE spec** | distinguishes `LABEL_OVERVIEW` from `LABEL_MACRO` | `formats/ife/metadata.go:488-498` preserves both |
| **Python opentile (upstream we port)** | `overview` | `get_overview()` accessor; classifier remaps OME-XML's native `"macro"` to `overview` |
| **OpenSlide** | `macro` | normalises every native name (incl. DICOM `OVERVIEW`) → `"macro"` |
| **WSI vendor formats** | `macro` colloquially | Aperio, Hamamatsu, Philips, Leica, Ventana, 3DHistech all use `Macro` in metadata |
| **opentile-go v0.1-v0.9** | `overview` | matches DICOM + upstream |
| **opentile-go v0.10/v0.11** | `macro` | drift from upstream — corrected by this milestone |

DICOM and Python opentile (the upstream we directly port) agree on `"overview"`. OpenSlide alone uses `"macro"`. Per CLAUDE.md's invariant *"Direct port under Apache 2.0 with attribution retained in NOTICE"* and *"Parity with upstream is the correctness bar"*, **the upstream-faithful + DICOM-compliant choice wins**: align everything except IFE on `"overview"`.

## 2. Scope

### 2.1. Method rename (interface-level)

`/image.go`:

```go
// Before
type AssociatedImage interface {
    Kind() string
    ...
}

// After
type AssociatedImage interface {
    Type() string
    ...
}
```

The doc comment is rewritten to:
- Cite DICOM ImageType (0008,0008) as the canonical reference
- List the standard `Type()` values: `"label"`, `"overview"`, `"thumbnail"`, plus format-specific extensions (`"map"`, `"probability"`, `"associated"`, IFE's distinct `"macro"`)
- Drop the obsolete claim that `"overview"` is "SVS-only synonym for macro"

### 2.2. Constant rename (`formats/generictiff/classifier.go`)

```go
// Before
const (
    KindLabel      = "label"
    KindMacro      = "macro"
    KindThumbnail  = "thumbnail"
    KindAssociated = "associated"
)

// After
const (
    TypeLabel      = "label"
    TypeOverview   = "overview"  // value flips alongside name
    TypeThumbnail  = "thumbnail"
    TypeAssociated = "associated"
)
```

The `KindMacro = "macro"` constant becomes `TypeOverview = "overview"` in one move — no transition aliasing.

### 2.3. Per-format value flip

| Format | Old emitted value | New emitted value |
|---|---|---|
| Aperio SVS | `"overview"` | `"overview"` (unchanged) |
| Hamamatsu NDPI | `"overview"` | `"overview"` (unchanged) |
| Philips TIFF | `"overview"` | `"overview"` (unchanged) |
| OME-TIFF | `"overview"` | `"overview"` (unchanged) |
| Ventana BIF | `"overview"` | `"overview"` (unchanged) |
| **Generic TIFF** | `"macro"` | `"overview"` — **flips** |
| **Leica SCN** | `"macro"` | `"overview"` — **flips** |
| Iris IFE | `"overview"` AND `"macro"` (distinct) | `"overview"` AND `"macro"` (unchanged — IFE-spec-distinct kinds) |

### 2.4. Doc fixes bundled

- `README.md` line 30 (OME-TIFF row): `macro, label, thumbnail` → `overview, label, thumbnail` (was already wrong; emits "overview")
- `README.md` line 33 (generic-TIFF row): `label, macro, thumbnail, "associated"` → `label, overview, thumbnail, "associated"`
- `README.md` line 34 (Leica SCN row): `macro per auxiliary <image>` → `overview per auxiliary <image>`
- `image.go` AssociatedImage interface doc: rewrite per §2.1
- `docs/formats/generictiff.md`: update Kind→Type narrative + emitted values list
- `docs/formats/leicascn.md`: update Kind→Type + new value
- `docs/formats/ife.md`: explicitly note IFE keeps both `"overview"` and `"macro"` as distinct values per the IFE spec

## 3. Out of scope

- **`KindMacro` / `KindLabel` / etc. compatibility aliases.** Pre-1.0 + sole-consumer sign-off; no transitional surface.
- **A typed enum for `Type()`** (e.g., `type AssociatedImageType string`). YAGNI; matches every other tag-string accessor in opentile-go (`Format()`, `Compression().String()`).
- **Renaming non-`Kind*` constants.** `KindLabel`, `KindThumbnail`, `KindAssociated` get renamed to `TypeXxx` in the same sweep but their values are correct; only `KindMacro`'s value changes.
- **IFE.** Stays untouched.
- **DICOM WSI reader.** Not on the v0.15 critical path; deferred per backlog.
- **v1.0 cut.** Still pending.

## 4. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Method rename `Kind()` → `Type()` on AssociatedImage? | Yes. Aligns with DICOM ImageType. |
| Q2 | Constant renames (`KindXxx` → `TypeXxx`) in lockstep? | Yes. Half-rename would be worse. |
| Q3 | `Type()` return type stays `string`? | Yes. Typed enum is YAGNI. |
| Q4 | `KindMacro` value flip + name flip in one rename? | Yes. Pre-1.0; no aliasing. |
| Q5 | IFE preserves both `"overview"` and `"macro"`? | Yes. IFE spec defines them distinct. |
| Q6 | Migration helper for consumers? | No. One known consumer (the user's viewer). |
| Q7 | Tagged v0.15.0? | Yes. Breaking; semver-respectful even pre-1.0. |
| Q8 | Bundle CHANGELOG explicit-migration block? | Yes. List exact `Kind()` → `Type()` + value-flip migration steps. |

## 5. Fixtures + tests

No new fixtures. Existing v0.14 + v0.13 + earlier fixtures stay byte-identical (per-tile SHA JSONs pin tile bytes, not Type values).

Test surface changes:
- Every test calling `.Kind()` switches to `.Type()` (mechanical sed-pass with grep audit per the v0.12 lesson on BSD `sed` reliability).
- Every reference to `KindLabel` / `KindMacro` / `KindThumbnail` / `KindAssociated` switches to `TypeXxx` (mechanical).
- `tests/parity/generic_geometry_test.go` rows for generic-TIFF + Leica SCN fixtures: any `Kind: ... Macro` → `Type: ... TypeOverview` (or whatever the field name becomes); the field name in the test struct also flips per §2.2.
- `tests/integration_test.go` slideCandidates unaffected (string list, no Kind references).

Verification:
- `go vet ./...` clean
- `make test` green
- `TestSlideParity` 28 fixtures (unchanged from v0.14)
- Per-format probe: open one fixture per format, confirm `Type()` returns the expected value and DOES NOT emit `"macro"` except on Iris IFE fixtures.

## 6. Active limitations introduced

None new. v0.15 is rename-only; behaviour is unchanged for any consumer code that already uses the correct name (post-rename, those that currently say `"overview"` keep working; those that currently say `"macro"` for generic-TIFF / Leica SCN must update).

The four §11 backlog rows are unaffected.

## 7. Plan outline

Single batch, 6 tasks:

- **T1**: `image.go` AssociatedImage interface — `Kind()` → `Type()` + doc rewrite citing DICOM standard.
- **T2**: Per-format `Kind()` method rename across 8 readers + `opentile_test.go` mock — mechanical method rename, no value changes.
- **T3**: `formats/generictiff/classifier.go` const rename `KindXxx` → `TypeXxx` + `KindMacro = "macro"` → `TypeOverview = "overview"` value flip; update all generic-TIFF call sites.
- **T4**: Leica SCN reader emits `"overview"` instead of `"macro"`; update format-internal tests.
- **T5**: Test surface — every `.Kind()` → `.Type()`, every `KindXxx` → `TypeXxx`, geometry assertions updated for the value flip.
- **T6**: Docs + ship — README + image.go doc + per-format docs (generictiff/leicascn/ife) + CHANGELOG [0.15.0] explicit migration block + CLAUDE.md milestone bump + `docs/deferred.md §8i` retirement audit.

Plan written separately at `docs/superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md`.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding sample_files, docs)
- `make test` green
- `TestSlideParity` 28 fixtures green
- Per-format probe at end of T6: `OPENTILE_TESTDIR=$PWD/sample_files go run /tmp/genericsmoke/main.go sample_files/<each-format>/<one-fixture>` shows `kind=` (will read `Type()` post-rename anyway) returns expected values, no stray `"macro"` outside IFE fixtures.

## 9. Lessons feeding into v0.15 execution

- **v0.12 BSD `sed` reliability:** every sed pass paired with a grep audit. Use `Edit` for surgical rewrites when sed silently misses identifiers. Avoid sweeping sed over `docs/superpowers/` — it corrupts spec/plan migration arrows.
- **v0.13 implementer self-interpretation:** verify with a separate probe before accepting an implementer's interpretation of unexpected results.
- **v0.14 agent-tool transient errors:** when the result-delivery fails mid-task, the work is often complete on disk — verify inline (`git status`, `git diff`, `go test`) before re-dispatching.

## 10. Migration note (preview of CHANGELOG block)

```
[BREAKING] Method renamed: AssociatedImage.Kind() → AssociatedImage.Type()
[BREAKING] Constants renamed: generictiff.KindLabel/Macro/Thumbnail/Associated
           → generictiff.TypeLabel/Overview/Thumbnail/Associated
[BREAKING] Value flipped: generictiff and leicascn emitted Type() == "macro"
           now emit "overview" (matches DICOM + Python opentile).
           Iris IFE preserves "macro" and "overview" as distinct values
           per the IFE spec.

Consumer migration (one-time mechanical):
- a.Kind()                 → a.Type()
- generictiff.KindLabel    → generictiff.TypeLabel
- generictiff.KindMacro    → generictiff.TypeOverview
- generictiff.KindThumbnail → generictiff.TypeThumbnail
- generictiff.KindAssociated → generictiff.TypeAssociated
- switch on Type() value:
  - case "macro":        → case "overview":  (generic-TIFF, Leica SCN)
  - case "macro":        → case "macro":     (Iris IFE only — unchanged)
```
