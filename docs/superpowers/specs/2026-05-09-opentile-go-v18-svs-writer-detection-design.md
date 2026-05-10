# opentile-go v0.18 — SVS writer-vendor detection + OME-TIFF audit

**Status:** sealed 2026-05-09.
**Work branch:** `feat/v0.18`.
**Headline:** Fix the misattribution bug in multi-vendor format readers — SVS files written by non-Aperio scanners (Grundium, future) currently get `ScannerManufacturer="Aperio"` because the SVS reader assumes the format-vendor is the writer-vendor. Detect the actual writer from ImageDescription + TIFF Software/Make tags; namespace Properties keys per detected writer; document the supported writer set explicitly. Audit OME-TIFF for the same risk (Bio-Formats / QuPath / OMERO writes are common; we capture `ome.creator` but don't use it).

## 1. Scope

### 1.1. SVS writer-vendor detection

`formats/svs/metadata.go` — parse the writer from these signals (priority order):

1. **ImageDescription first line** — most reliable. Patterns observed:
   - `"Aperio Image Library v11.2.1"` → writer = Aperio
   - `"Aperio Image, Grundium Ocus"` → writer = Grundium, model = Ocus (parse comma-suffix)
   - Other patterns: undetected; fall back to TIFF tags
2. **TIFF `Software` tag** (305) — secondary signal if ImageDescription doesn't disambiguate
3. **TIFF `Make` tag** (271) — tertiary signal

If none yield a writer-vendor name → "" (empty); namespace Properties keys under format-default `"svs.<key>"`.

**Behavior changes:**

| Field | Pre-v0.18 (Grundium SVS) | Post-v0.18 |
|---|---|---|
| `ScannerManufacturer` | "Aperio" (wrong) | "Grundium" |
| `ScannerModel` | "" | "Ocus" |
| `ScannerSoftware` | `[Aperio Image, Grundium Ocus]` (single jammed string) | `["Aperio Image", "Grundium Ocus"]` (split sensibly) |
| Properties keys | `aperio.MPP` etc. | `grundium.MPP` etc. (writer-namespaced) |
| Standardized keys (MPP, AppMag) → cross-format | unchanged (always populated) | unchanged |

### 1.2. OME-TIFF audit + (if needed) writer-vendor surfacing

`formats/ometiff/metadata.go` — currently captures `ome.creator` Property from OME-XML root `Creator` attribute (e.g., `"OME Bio-Formats 6.0.0-rc1"`). We do NOT currently use it to set `ScannerManufacturer`.

Audit decision (T2): determine whether `ome.creator` should override `ScannerManufacturer` when no `<Microscope>` element is present. For Bio-Formats-written OME-TIFFs converted from vendor formats (Aperio → Bio-Formats → OME), `Creator="OME Bio-Formats X"` is meaningfully different from "the scanner manufacturer." Probably keep them separate (Creator = software writer; ScannerManufacturer = scanner OEM). But if a viewer needs writer-vendor info, expose a clean accessor.

**Likely outcome:** no behavior change for OME-TIFF. Document the existing behavior (Creator captured as `ome.creator`; ScannerManufacturer comes from `<Microscope>` only when present) explicitly so consumers know.

### 1.3. Documentation

`docs/formats/svs.md` gains an explicit "Recognized SVS writers" section listing the supported writer first-line patterns + their detected vendor/model + Properties namespacing. Status column distinguishes verified writers (with fixture coverage) from undetected (best-effort fallback).

`docs/formats/ometiff.md` gains a brief "OME-XML writers" section noting the Creator field is captured as `ome.creator` Property, distinct from ScannerManufacturer.

## 2. Out of scope

- **Vendor-specific sub-readers** (`formats/svs/aperio.go`, `formats/svs/grundium.go`). Considered, rejected — the parser is structurally one piece; vendor detection + dispatch happens within it. See post-v0.17 brainstorm for full reasoning.
- **3DHistech-via-SVS-export support.** No fixture in the slate; if such a fixture surfaces, the format-default `svs.<key>` fallback handles it (just won't recognize "3DHistech" as the writer-vendor specifically until a pattern is observed).
- **Other multi-vendor format audits.** NDPI / Philips / BIF / IFE / SCN are vendor-mono-locked; nothing to audit. SZI's writer is parsed from `scan-properties.xml::VendorName` (already correct). Generic-TIFF is by-design any vendor and uses ImageDescription verbatim + wsi-tools-prefix detection.
- **Cross-format `WriterVendor` typed field on `opentile.Metadata`.** The hybrid approach (typed for canonical, Properties for extensions) suffices: `ScannerManufacturer` carries the writer in single-vendor formats; in multi-vendor SVS the writer === scanner manufacturer of the system that wrote the file, so the existing field works. No new typed field needed.
- **HTTP-range backing or COG first-class support.** R21; separate axis.
- **v1.0 cut.** Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Vendor-specific sub-readers (Option C from brainstorm) | **No** — single parser with vendor-detection dispatch (Option A from brainstorm). Sub-readers add architectural complexity without functional benefit; the parser is structurally one piece. |
| Q2 | Detection signal order | ImageDescription first line → TIFF Software → TIFF Make. ImageDescription is most reliable (vendor explicitly names themselves); TIFF tags are secondary. |
| Q3 | Unknown-writer fallback | `ScannerManufacturer = ""`, `ScannerModel = ""`, Properties keys under `svs.<key>` namespace. Standardized keys (MPP, AppMag) still populate cross-format Metadata regardless. |
| Q4 | OME-TIFF — should `ome.creator` override `ScannerManufacturer`? | **No.** Creator is the OME-XML writer (e.g., Bio-Formats); ScannerManufacturer is the scanner OEM. Different concepts. Document the separation; surface Creator as `ome.creator` (already do). |
| Q5 | Tagged version | **v0.18.0**. Pre-1.0; mostly additive (writer-detection is a refinement, not a contract change); narrow break only for consumer code that hardcoded "Aperio" expectations on Grundium SVS files. |
| Q6 | Per-vendor namespace key in Properties | Lowercased first word of detected vendor (`Aperio` → `aperio`, `Grundium` → `grundium`). Mirrors the established convention from v0.17. |

## 4. Per-format mapping audit

| Format | Multi-vendor risk | v0.18 action |
|---|---|---|
| **SVS** | YES (Aperio canonical + Grundium observed + likely 3DHistech) | T1 fixes detection + namespacing |
| **OME-TIFF** | YES (Bio-Formats / QuPath / OMERO / custom writers) | T2 audit; likely just docs (Q4 — keep Creator separate from ScannerManufacturer) |
| NDPI | NO (Hamamatsu-only) | none |
| Philips TIFF | NO (Philips-only) | none |
| BIF | NO (Roche/Ventana-only) | none |
| IFE | NO (Iris-only) | none |
| Leica SCN | NO (Leica-only) | none |
| Generic TIFF | by-design any-vendor; metadata wsi-tools-prefix-detected | none |
| SZI | YES, but `scan-properties.xml::VendorName` is the source of truth → already correct | none |

## 5. Test strategy

- **SVS unit tests** for the writer-detection function: golden inputs covering Aperio canonical, Grundium ("Aperio Image, Grundium Ocus"), Aperio truncated, undetected fallback. Pure-function tests; no fixture dependency.
- **SVS fixture parity tests** updated for Grundium: `scan_620_.svs` and `svs_40x_bigtiff.svs` should now have `ScannerManufacturer == "Grundium"` and `ScannerModel == "Ocus"`. CMU-1 / CMU-1-Small-Region / JP2K-33003-1 unchanged (still Aperio).
- **OME-TIFF docs-only update** — no code change expected; existing tests stay green.
- **Fixture JSON parity** — pinned `metadata.scanner_manufacturer` may need updating for Grundium SVS fixtures (was "Aperio" pre-v0.18; will be "Grundium").

## 6. Active limitations introduced

None new. The fix is a bug fix. v0.18 introduces no L items.

The post-v0.17 backlog re-triage in `docs/deferred.md §11`:
- R19 (bare DZI) — still parked
- R21 (COG first-class) — still parked
- New R22 candidate: 3DHistech-via-SVS-export support (trigger-driven; no fixture)

Add R22 in the same milestone if signal warrants.

## 7. Plan outline

3 tasks single batch:

- **T1**: SVS writer-vendor detection in `formats/svs/metadata.go` + unit tests on the detection function + per-fixture metadata test updates + Grundium fixture JSON updates if needed
- **T2**: OME-TIFF audit — confirm `ome.creator` semantics; docs-only addition to `docs/formats/ometiff.md` clarifying separation from ScannerManufacturer
- **T3**: docs sweep — `docs/formats/svs.md` "Recognized SVS writers" section + CHANGELOG `[0.18.0]` + CLAUDE.md milestone bump + `docs/deferred.md §8l` retirement audit

Plan written separately at `docs/superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md`.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding pre-existing unrelated drift, sample_files, docs)
- `make test` green
- `TestSlideParity` 30 fixtures green
- `TestCrossFormatMetadata` (added in v0.17) green
- Per-fixture probe of Grundium SVS files confirms `ScannerManufacturer == "Grundium"` and `ScannerModel == "Ocus"`
- Per-fixture probe of Aperio SVS files unchanged (still `ScannerManufacturer == "Aperio"`)

## 9. References

- Brainstorm thread captured in v0.17 ship session (2026-05-09): why Option A wins over Option C.
- Grundium SVS probe results captured in same session: `"Aperio Image, Grundium Ocus"` first-line pattern; minimal extended metadata (just MPP + AppMag).
- OpenSlide's vendor-detection precedent: same conflation pattern, no per-writer namespacing — opentile-go v0.18 improves on this.

## 10. Lessons feeding into v0.18 execution

- **v0.17 lesson:** Format-specific Metadata cleanup (Q4 Option B from v0.17) preserves consumer access via field promotion. v0.18 is small enough to skip Option B audit per format; the SVS fix is contained to the writer-detection logic and doesn't touch struct fields.
- **Probe before pinning:** v0.17 implementers caught plan-vs-code drift in T3/T6. v0.18 T1 implementer must probe Grundium fixtures before locking in writer-detection patterns; my "Aperio Image, Grundium Ocus" pattern is observed on 1 fixture; verify on the second Grundium fixture (`svs_40x_bigtiff.svs`).
