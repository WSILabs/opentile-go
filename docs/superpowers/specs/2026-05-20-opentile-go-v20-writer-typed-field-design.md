# opentile-go v0.20 — cross-format `Writer` typed field (R22)

**Status:** sealed 2026-05-20.
**Work branch:** `feat/v0.20`.
**Headline:** Closes R22. Adds `Writer string` typed field to `opentile.Metadata` carrying the file-producer identifier — distinct from `ScannerManufacturer` (scanner OEM) and `ScannerSoftware []string` (broader software stack). Every WSI file has a writer; the field unifies discoverability across formats that currently expose writer info inconsistently (SVS uses `ScannerSoftware[0]`; OME-TIFF buries it in `Properties["ome.creator"]`; SZI uses `ScannerSoftware[0]`; COG-WSI has `Properties["cog-wsi.wsitools-version"]`).

## 1. Scope

### 1.1. Cross-format `opentile.Metadata` addition

One new typed field:

```go
type Metadata struct {
    // ... existing v0.19 fields ...

    // Writer identifies the software that wrote this file — the file
    // producer, distinct from the scanner OEM (ScannerManufacturer)
    // and the broader software stack (ScannerSoftware []string).
    //
    // Format-specific population:
    //   SVS Aperio canonical    "Aperio Image Library v11.2.1" (full SoftwareLine)
    //   SVS Grundium (or other  "Grundium Ocus" (comma-suffix writer; matches
    //     non-canonical writer)  v0.18's writerVendor.softwares[1])
    //   OME-TIFF                "OME Bio-Formats 6.0.0-rc1" (OME-XML Creator
    //                            attribute; promoted from Properties["ome.creator"])
    //   SZI                     "<SoftwareName> <SoftwareVersion>" (e.g.,
    //                            "OcusScan 3.1.4")
    //   COG-WSI                 "wsitools/<WSIToolsVersion>" (file producer;
    //                            source scanner stays in ScannerManufacturer
    //                            per spec)
    //   wsi-tools generic-TIFF  "wsitools/<version>" (from wsi-tools
    //     (avif/jxl/htj2k/webp)  ImageDescription parser)
    //   NDPI / Philips / BIF /  format-specific Software field (often equals
    //     IFE / Leica SCN        ScannerSoftware[0])
    //
    // Empty when the format provides no writer indication. The empty
    // value is the zero/unknown sentinel — consumers needing presence
    // checking compare against "" explicitly.
    //
    // Added in v0.20.
    Writer string
}
```

### 1.2. Per-format population strategy

| Format | Writer source | Notes |
|---|---|---|
| SVS Aperio canonical | full `SoftwareLine` (first line of ImageDescription) | preserves version; e.g., `"Aperio Image Library v11.2.1"` |
| SVS Grundium / other | comma-suffix writer string from v0.18's `writerVendor.softwares[1]` | e.g., `"Grundium Ocus"` |
| SVS undetected writer | fall back to `SoftwareLine` verbatim | best-effort |
| NDPI | first vendor software string from format-specific source | usually equals `ScannerSoftware[0]` |
| Philips TIFF | format-specific Software field | usually equals `ScannerSoftware[0]` |
| OME-TIFF | OME-XML `<OME Creator>` attribute | promote from `Properties["ome.creator"]` (which stays for backward-compat) |
| BIF (Ventana) | iScan XML Software field | usually equals `ScannerSoftware[0]` |
| IFE (Iris) | IFE-spec Software field | usually equals `ScannerSoftware[0]` |
| Leica SCN | SCN-XML Software field | usually equals `ScannerSoftware[0]` |
| Generic TIFF (no wsi-tools) | TIFF `Software` tag (305) | best-effort; no special detection |
| Generic TIFF (wsi-tools) | `"wsitools/<version>"` from the wsi-tools ImageDescription parser | distinct from source scanner |
| SZI | `"<SoftwareName> <SoftwareVersion>"` (concatenated; spaces preserved) | falls back to whichever is known when only one is set |
| COG-WSI | `"wsitools/<WSIToolsVersion>"` from `WSIToolsVersion` private tag (65084) | file producer; source scanner stays in `ScannerManufacturer` per spec |

### 1.3. Behavior on file-producer vs scanner-OEM distinction

The Writer field answers "what software wrote this bytestream." This is distinct from:
- `ScannerManufacturer` — who made the scanner hardware (e.g., "Aperio", "Grundium", "Roche")
- `ScannerModel` — the scanner model (e.g., "Ocus", "VENTANA DP 200")
- `ScannerSoftware []string` — the broader software stack (may include both the writer AND the scanner software, depending on format)

For converted files (OME-TIFF via Bio-Formats; COG-WSI via wsitools; wsi-tools-converted generic-TIFF): Writer = the converter; scanner attribution stays in ScannerManufacturer/Model/Software (preserved from source per format spec). This matters because consumers want to dispatch on "who produced this file" (e.g., to apply converter-specific quirks) separately from "what scanner originally captured the slide."

### 1.4. Properties keys stay for backward-compat

The existing `Properties["ome.creator"]` and `Properties["cog-wsi.wsitools-version"]` etc. continue to populate as before (already implemented in v0.17 / v0.19). The new typed `Writer` field is the primary surface; Properties keys remain accessible at zero cost for any consumer already reading them.

## 2. Out of scope

- **Structured Writer (split into Name / Version / Vendor)**. Q1 sealed as single-string. If a consumer regularly needs version-dispatch, a future R can add `WriterVersion string` additively. Not v0.20 scope.
- **Re-applying v0.18 SVS writer detection to COG-WSI** (the Grundium-COG-WSI misattribution flagged during v0.20 brainstorm). Filed as **R23** in `docs/deferred.md §1`. Independent fix; out of v0.20 scope.
- **R19 (bare DZI)**, **R23**, **older parked items**. Unchanged.
- **Changes to the existing 9 typed fields** on `opentile.Metadata` (Magnification, ScannerManufacturer, ScannerModel, ScannerSoftware, ScannerSerial, AcquisitionDateTime, MicronsPerPixel + per-axis, ImageDescription). Stay as-is.
- **Per-format Metadata struct changes**. The format-specific structs (e.g., `szi.Metadata`, `philipstiff.Metadata`) inherit the new Writer field via embedded `opentile.Metadata` — no separate Writer field on those.
- **Cross-format parity test extension**. v0.17's `TestCrossFormatMetadata` framework extends to cover Writer per fixture; no new framework needed.
- **v1.0 cut.** Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Field shape | **`Writer string` — single field**. Not split into Vendor/Version. Matches the existing `ScannerSoftware []string` typed-but-simple convention. |
| Q2 | SVS Aperio canonical Writer value | **Full SoftwareLine** (`"Aperio Image Library v11.2.1"`). Preserves version. |
| Q3 | SVS Grundium Writer value | **Comma-suffix writer** (`"Grundium Ocus"`). Matches v0.18's `writerVendor.softwares[1]`. |
| Q4 | OME-TIFF Writer value | **`ome.creator` verbatim**. Promote from Properties to typed; Properties key stays for backward-compat. |
| Q5 | COG-WSI Writer value | **`"wsitools/<WSIToolsVersion>"`**. Writer = file producer (wsitools); source scanner stays in ScannerManufacturer per spec. User-sealed in brainstorm: scanner attribution is elsewhere in the file. |
| Q6 | wsi-tools generic-TIFF Writer value | **`"wsitools/<version>"`** from wsi-tools ImageDescription parser. Mirrors Q5. |
| Q7 | NDPI / Philips / BIF / IFE / SCN | **Format-specific Software field** (often equals `ScannerSoftware[0]`; verify per format during implementation). |
| Q8 | SZI | **`"<SoftwareName> <SoftwareVersion>"`** when both known; fall back to whichever is known. |
| Q9 | Empty Writer semantics | **`""` is zero/unknown sentinel**. No special-case sentinel. |
| Q10 | Keep Properties keys (`ome.creator`, `cog-wsi.wsitools-version`) | **Keep**. Writer is primary; Properties stay at zero extra cost for backward-compat. |
| Q11 | Tagged version | **v0.20.0**. Pre-1.0; pure-additive (new typed field; existing consumers unaffected). |

## 4. Fixtures

No new fixtures. Existing v0.19 + earlier fixtures stay byte-identical. The cross-format parity gate (`tests/parity/cross_format_metadata_test.go`) extends to cover Writer per existing fixture.

Likely fixture JSON updates: fixture JSONs that pin `metadata.writer` (a new field) will need values — but if the existing fixture-JSON schema doesn't currently include `writer`, then no fixture JSON updates are needed unless the schema is extended. Implementer should verify during T5 (cross-format parity test + docs).

## 5. Test strategy

- **Unit tests on `opentile.Metadata.Writer`** (struct field exists; trivially exercised by per-format population).
- **Per-format integration tests** verifying Writer populates correctly:
  - SVS Aperio canonical: `Writer == "Aperio Image Library v11.2.1"` on CMU-1-Small-Region
  - SVS Grundium: `Writer == "Grundium Ocus"` on scan_620_.svs
  - OME-TIFF: `Writer == "OME Bio-Formats 6.0.0-rc1"` on Leica-1.ome.tiff
  - SZI: `Writer != ""` on CMU-1.szi (spec-example value)
  - COG-WSI: `Writer == "wsitools/0.6.0-dev"` on CMU-1-Small-Region_cog-wsi.tiff
  - Generic-TIFF wsi-tools: `Writer == "wsitools/0.2.0-dev"` on avif-out.tiff
  - NDPI / Philips / BIF / IFE / SCN: format-specific values per probe
- **`TestCrossFormatMetadata` extension** with `wantWriter bool` flag + `wantWriterContains string` (substring match for version flexibility).
- **TestSlideParity unchanged** — fixture JSONs may need `writer` field added; depends on existing schema.

## 6. Active limitations introduced

None. v0.20 is purely additive. Existing consumers unaffected.

The §11 backlog rows for R19, R23, older parked items are unchanged.

## 7. Plan outline

5 tasks single batch:

- **T1**: `opentile.Metadata.Writer` field + tests
- **T2**: SVS + NDPI + Philips populate (TIFF-classic batch)
- **T3**: OME-TIFF + Leica SCN + BIF + IFE populate (XML / structured-metadata batch)
- **T4**: SZI + Generic-TIFF (wsi-tools) + COG-WSI populate (writer-prefix batch — all three use "wsitools/<version>" pattern or similar)
- **T5**: Cross-format parity test + per-format docs + CHANGELOG + CLAUDE.md milestone bump + deferred §8n retirement audit (closes R22)

Plan written separately at `docs/superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md`.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding pre-existing unrelated drift, sample_files, docs)
- `make test` green
- `make cover` green (≥80% per package — should stay green since v0.20 is additive)
- `TestSlideParity` 40 fixtures green (unchanged from v0.19.1)
- `TestCrossFormatMetadata` extended with Writer assertions — green per fixture
- Per-format probe: opening one fixture per format reports `Metadata.Writer` populated per the per-format mapping above

## 9. References

- v0.18 SVS writer detection design: `docs/superpowers/specs/2026-05-09-opentile-go-v18-svs-writer-detection-design.md` (writerVendor struct + detectWriter)
- v0.17 cross-format Metadata expansion: `docs/superpowers/specs/2026-05-09-opentile-go-v17-cross-format-metadata-design.md` (hybrid typed + Properties pattern)
- R22 backlog entry: `docs/deferred.md §1 R22 row` + `§11 R22 row`
- R23 (related, separately filed): `docs/deferred.md §1 R23 row`

## 10. Lessons feeding into v0.20 execution

- **v0.17 lesson**: per-format population requires probing actual fixtures; documented mappings can be wrong (Philips uses `DICOM_PIXEL_SPACING` not `PIM_DP_X_RESOLUTION`; etc.). Each task should probe before pinning.
- **v0.18 lesson**: every format-specific parser has its own way of representing the Software field; verify each rather than assume `ScannerSoftware[0]` is always the right source.
- **v0.19 lesson**: T6's delegation pattern (cogwsi → generictiff) means generictiff's Writer population covers cogwsi automatically EXCEPT for the WSIToolsVersion private tag which only cogwsi sees. T4 needs to set Writer at the cogwsi layer (not just inherit from generictiff).
- **v0.19.1 lesson**: pure-additive milestones still need fixture-JSON sanity checks. v0.20 isn't expected to change fixture JSONs (unless schema is extended to pin Writer), but T5 should run `make test` to confirm no surprise regressions.
