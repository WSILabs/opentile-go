# opentile-go v0.19 — COG-WSI support (issues #5 + #6)

**Status:** sealed 2026-05-20.
**Work branch:** `feat/v0.19`.
**Headline:** Ship support for Cloud Optimized GeoTIFF for WSI (COG-WSI) — a strict COG extension produced by the user's wsitools transcoder. Two coupled pieces: (#5) extend `generic-tiff` to honor the WSI private tag set + relax mixed-ratio pyramid validation; (#6) add dedicated `formats/cogwsi/` reader with ghost-area dispatch + spec validation + canonical metadata. 10 real fixtures across every source format (SVS / OME / Philips / BIF / IFE) already in `sample_files/cog-wsi/`. Bundled in one milestone per owner sealing of v0.19 brainstorm Q2.

## 1. Scope

### 1.1. Issue #5 — generic-tiff WSI tag awareness + mixed-ratio pyramid validator

Two complementary changes in `formats/generictiff/` + `internal/tiff/classify_pyramid.go`:

**(A) Honor WSI private tags as authoritative when present.** New tag readers in `internal/tiff` (or a shared helper) for the COG-WSI private tag set (IDs 65080-65087 per spec §5). When an IFD carries `WSIImageType` (65080), `WSILevelIndex` (65081), and `WSILevelCount` (65082), use those values directly — no dimension-ratio heuristic. Pyramid classification skips the drift check entirely when all candidate IFDs carry the WSI tag set. Associated-image classification routes `WSIImageType ∈ {label, macro, thumbnail, overview}` directly to typed `AssociatedImage` instances (per v0.15 Type() naming).

**(B) Relax `InterLevelTolerance` to allow integer-multiple jumps.** `internal/tiff/classify_pyramid.go::buildPyramidChain` currently rejects a candidate IFD whose scale ratio drifts > 5% from the established chain ratio. Replace with a predicate that accepts:
- ratio matches a prior step within tolerance (existing behavior), OR
- ratio is a clean integer multiple/divisor (2.0×, 4.0×, 8.0×, …) of any prior step within tolerance.

Needed independently of (A) for legacy generic TIFFs with mixed-ratio pyramids (e.g., Aperio/Grundium SVS routed through `generic-tiff` rather than `svs`).

### 1.2. Issue #6 — new `formats/cogwsi/` package

Dedicated format reader for files marked by the COG-WSI ghost-area `COG_WSI_VERSION=` line:

- **New `opentile.FormatCOGWSI = "cog-wsi"`** enum value. Hyphenated slug matches the spec + the existing `philips-tiff` / `ome-tiff` / `leica-scn` / `generic-tiff` convention.
- **New `formats/cogwsi/` package** structured like other format packages (`factory.go`, `tiler.go`, `classifier.go`, `metadata.go`, `level.go`, etc.).
- **Detection:** read TIFF header, then parse the ghost area (offset 8 for classic TIFF, 16 for BigTIFF) for `COG_WSI_VERSION=`. Presence → format is COG-WSI. Reader's supported version range: `0.x` (current spec is v0.1; v0.x minor bumps are backward-compatible).
- **Tile + Image surface:** delegate to the same pyramid + associated parsing as generic-tiff (which now honors WSI tags per #5). Cogwsi just becomes a format-identified-and-validated wrapper.
- **Spec validation at open time** (`ErrNotConformantCOGWSI` sentinel):
  - Ghost area keys include the required set: `LAYOUT=IFDS_BEFORE_DATA`, `BLOCK_ORDER=ROW_MAJOR`, `BLOCK_LEADER=SIZE_AS_UINT4`, `BLOCK_TRAILER=LAST_4_BYTES_REPEATED`, `KNOWN_INCOMPATIBLE_EDITION=NO`, `COG_WSI_VERSION=<x.y>`.
  - All pyramid IFDs carry `WSIImageType=pyramid`.
  - Pyramid IFDs are tiled (no strips).
  - IFD order: full-res pyramid first → decreasing overviews → associated IFDs after pyramid.
  - Tile data 16-byte aligned (sample-based; not exhaustive).
  - Tile data layout in reverse IFD order (smallest overview first). Spot-check via tile offsets.
- **Canonical metadata exposure** from WSI private tags (spec §5.2):
  - `WSIMPPX` (65085) → `Metadata.MicronsPerPixelX`
  - `WSIMPPY` (65086) → `Metadata.MicronsPerPixelY` + `SetMPPSymmetric()`
  - `WSIMagnification` (65087) → `Metadata.Magnification`
  - `WSISourceFormat` (65083) → `Properties["cog-wsi.source-format"]` (e.g., `"svs"`, `"philips"`)
  - `WSIToolsVersion` (65084) → `Properties["cog-wsi.wsitools-version"]`
  - Standard TIFF `Make` / `Model` / `Software` / `DateTime` → existing cross-format fields per the v0.17 hybrid pattern
  - `ImageDescription` → `Metadata.ImageDescription` (preserves `wsitools/<version> convert source=<fmt>` provenance string)
- **Associated images:** macro / label / thumbnail / overview routed from `WSIImageType` tag with v0.15 canonical Type() values (`"label"`, `"overview"`, `"thumbnail"`; `"macro"` reserved for IFE-spec-distinct kinds — COG-WSI's overview ↔ Type("overview"), and macro from non-IFE sources also maps to Type("overview") per v0.15 naming convention).
- **Factory registration:** register `cogwsi.Factory` in `formats/all` BEFORE `generic-tiff` (mirrors v0.11 leicascn-before-generictiff dispatch ordering — format-specific detector wins over the catch-all).

### 1.3. New `internal/cog` package (shared helpers)

Pure-function helpers used by both `formats/cogwsi/` (validation) and `formats/generictiff/` (WSI-tag tolerance):

- `cog.ParseGhostArea(data []byte) (GhostArea, error)` — parses the GDAL-style key/value block; returns typed `GhostArea` struct + raw key map for forward-compat
- `cog.GhostArea` struct fields: `Layout`, `BlockOrder`, `BlockLeader`, `BlockTrailer`, `KnownIncompatibleEdition`, `COGWSIVersion`, plus a `RawKeys map[string]string` for unknown keys
- `cog.ParseCOGWSIVersion(s string) (major, minor int, err error)` — semver-style parser
- `cog.GhostAreaOffset(bigTIFF bool) int64` — 8 for classic TIFF, 16 for BigTIFF (caller already knows from header)

No I/O dependencies; pure data + parsing. Designed to support future bare-COG detection (R21) by exposing the ghost-area parser without WSI-specific logic.

### 1.4. New WSI tag readers in `internal/tiff`

Add typed accessors on `tiff.Page` for the WSI private tag set (spec §5):

```go
func (p *Page) WSIImageType() (string, bool)
func (p *Page) WSILevelIndex() (uint32, bool)
func (p *Page) WSILevelCount() (uint32, bool)
func (p *Page) WSISourceFormat() (string, bool)
func (p *Page) WSIToolsVersion() (string, bool)
func (p *Page) WSIMPPX() (float64, bool)
func (p *Page) WSIMPPY() (float64, bool)
func (p *Page) WSIMagnification() (float64, bool)
```

Tag IDs hardcoded per spec (65080-65087). `bool` return indicates tag presence; readers honor "absent → unknown" semantics. Type assertions match the spec's TIFF type column.

## 2. Out of scope

- **Writing COG-WSI files.** That lives in `wsitools` (the user's separate transcoder); opentile-go is reader-only.
- **Validation as a standalone CLI** (`wsitools cog-wsi validate`). Reader exposes validation programmatically via `ErrNotConformantCOGWSI`; CLI wrapping is wsitools's concern.
- **HTTP-range backing.** R21's bigger story. v0.19 reads COG-WSI files via the v0.9 mmap-default path; HTTP backing is a separate axis.
- **General COG (non-WSI)** first-class support. R21 stays parked; v0.19's `cogwsi.Factory.SupportsRaw` is gated on `COG_WSI_VERSION=` presence, so generic COG files (geospatial Landsat etc.) continue to fall through to `generic-tiff`.
- **DZI / SZI changes.** R19 unchanged; this milestone doesn't touch `internal/dzi/` or `formats/szi/`.
- **COG-WSI v0.2+ structural changes.** The reader supports v0.x minor bumps as additive (unknown keys ignored per spec §4.1). v1.0+ would require a major-version reader update.
- **Multi-channel / multi-Z COG-WSI.** Spec §8 defers; v0.1 is brightfield RGB. Reader doesn't attempt multi-dim semantics.
- **v1.0 cut.** Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Bundle #5 + #6 in one milestone vs split into v0.19 + v0.20? | **Bundle.** User-sealed in pre-spec brainstorm. v0.19 single milestone, 8 tasks. |
| Q2 | Treat spec as stable for tag IDs + ghost-area keys? | **Yes.** User-sealed. Hardcode tag IDs 65080-65087; hardcode required ghost-area key set per spec §4.1. v0.2+ additions are backward-compatible (new optional keys / tags). |
| Q3 | Where do WSI tag readers live? | `internal/tiff` (shared between cogwsi + generictiff). Typed accessor methods on `tiff.Page`. |
| Q4 | Where does ghost-area parsing live? | New `internal/cog/` package — pure-function. Forward-compat for R21 bare-COG support. |
| Q5 | Factory dispatch order | `cogwsi.Factory` BEFORE `generic-tiff` in formats/all. Mirrors v0.11 leicascn-before-generictiff precedent. |
| Q6 | Associated-image Type() naming | Per v0.15 canonical: `"label"`, `"overview"`, `"thumbnail"`. COG-WSI spec admits `overview` directly + `macro` as a `WSIImageType` value; both map to `Type() == "overview"` (matches v0.15 convention; "macro" is IFE-spec-distinct only). |
| Q7 | Pyramid validator relaxation scope | Apply (B) integer-multiple acceptance globally in `internal/tiff/classify_pyramid.go` — benefits all generic-TIFF consumers, not just COG-WSI. |
| Q8 | Validation strictness on COG-WSI files | Hard-error (`ErrNotConformantCOGWSI`) on any spec violation. Don't be lenient — the spec is the contract; lenient acceptance hides writer bugs. |

## 4. Fixtures (already in `sample_files/cog-wsi/`)

10 fixtures, comprehensive coverage of every source format opentile-go reads:

| Source format | Fixture | Size |
|---|---|---:|
| SVS Aperio canonical (CMU-1-Small-Region) | `CMU-1-Small-Region_cog-wsi.tiff` | 1.9 MB |
| SVS Aperio canonical (CMU-1) | `CMU-1_cog-wsi.tiff` | 185 MB |
| SVS Aperio JP2K | `JP2K-33003-1_cog-wsi.tiff` | 64 MB |
| SVS Grundium scan_617 | `scan_617_cog-wsi.tiff` | 330 MB |
| SVS Grundium scan_620 | `scan_620_cog-wsi.tiff` | 270 MB |
| SVS Grundium 40x BigTIFF | `svs_40x_bigtiff_cog-wsi.tiff` | 4.8 GB |
| OME-TIFF (Leica-1) | `Leica-1_cog-wsi.tiff` | 226 MB |
| Philips TIFF (Philips-1) | `Philips-1_cog-wsi.tiff` | 331 MB |
| BIF Ventana-1 | `Ventana-1_cog-wsi.tiff` | 225 MB |
| IFE Iris cervix | `cervix_2x_jpeg_cog-wsi.tiff` | 2.1 GB |

Header probe confirmed: `CMU-1-Small-Region_cog-wsi.tiff` is classic TIFF (little-endian + 0x2A) with `GDAL_STRUCTURAL_METADATA_SIZE=000159 bytes` ghost area starting at offset 8. Spec conformance: structurally valid.

Plus `sample_files/cog-wsi/2026-05-20-cog-wsi-format.md` — a copy of the spec mirroring `docs/specs/2026-05-20-cog-wsi-format.md` (only difference is the vendored-copy notice we added in `docs/specs/`).

**Small fixture for full-walk testing:** `CMU-1-Small-Region_cog-wsi.tiff` (1.9 MB). Larger fixtures are sampled per the 5 MB JSON cap.

## 5. Test strategy

### 5.1. `internal/cog` unit tests

- `TestParseGhostArea_HappyPath` — golden ghost-area byte string (the spec example); confirm all required keys parsed.
- `TestParseGhostArea_MissingRequiredKey` — drop `LAYOUT=IFDS_BEFORE_DATA`; expect error.
- `TestParseGhostArea_UnknownKey` — extra `FUTURE_KEY=value` line surfaces in `RawKeys`; required keys still parsed; no error.
- `TestParseCOGWSIVersion` — `0.1`, `0.2`, `1.0`, malformed `abc`.

### 5.2. `internal/tiff` WSI tag reader unit tests

- `TestPage_WSIImageType` — golden Page with each `WSIImageType` value (`pyramid`, `label`, `macro`, `thumbnail`, `overview`).
- Negative: `bool` is false when tag absent.
- Type assertions match spec (LONG / DOUBLE / ASCII per tag).

### 5.3. `formats/generictiff` regression + extension tests

- Existing TestGenericGeometry suite continues to pass on its current fixtures (none of them carry WSI tags; classifier falls through to dimension heuristic per existing behavior).
- New test: when forced to route a COG-WSI file through `generic-tiff` (bypass cogwsi factory), the reader correctly extracts pyramid + associated using WSI tags as authoritative.
- New test: integer-multiple pyramid drift acceptance (synthetic IFD chain with 4×/2×/2× ratios).

### 5.4. `formats/cogwsi` unit + integration tests

- Per-fixture geometry pinning (new `tests/parity/cogwsi_geometry_test.go`; mirrors `tests/parity/generic_geometry_test.go` shape). Pin per-level Size / TileSize / Grid / Compression + associated-image Type / Compression / ByteCount across all 10 fixtures.
- Spec validation negative cases — synthetic malformed COG-WSI bytes:
  - Missing `COG_WSI_VERSION` ghost-area key → `ErrNotConformantCOGWSI`
  - `LAYOUT=NOT_IFDS_BEFORE_DATA` → `ErrNotConformantCOGWSI`
  - Major version 1.x → `ErrNotConformantCOGWSI` (unsupported major; current reader supports v0.x)
  - Pyramid IFD with strips (no tiles) → `ErrNotConformantCOGWSI`
  - WSIImageType missing from a pyramid IFD → `ErrNotConformantCOGWSI`

### 5.5. Per-tile SHA fixtures

Generate `tests/fixtures/*_cog-wsi.tiff.json` via `TestGenerateFixtures` for all 10 fixtures. Sampled per the 5 MB cap for the larger fixtures (CMU-1_cog-wsi.tiff 185 MB; Philips-1_cog-wsi.tiff 331 MB; cervix_2x_jpeg_cog-wsi.tiff 2.1 GB; svs_40x_bigtiff_cog-wsi.tiff 4.8 GB). Wire into `slideCandidates`.

**TestSlideParity total post-v0.19:** 40 fixtures (was 30; +10 cog-wsi).

### 5.6. Cross-fixture parity check

For each pair `<source>` ↔ `<source>_cog-wsi.tiff`, verify tile bytes match where the COG-WSI writer was supposed to preserve them bit-exact (per spec — `Compression` / `BitsPerSample` / tile bytes preserved verbatim; padded edge-tile bytes from the source are also preserved). Implement as a sampled-tile comparison gate (new test or extension to existing parity infra). Specifically valuable: confirms our reader and the user's writer agree on what "passthrough" means.

## 6. Architecture

### 6.1. Package structure

```
internal/cog/
├── doc.go
├── ghost.go          // GhostArea struct + ParseGhostArea + ParseCOGWSIVersion
└── ghost_test.go

internal/tiff/        // existing package; add:
└── wsi_tags.go       // Page.WSIImageType() etc.
└── wsi_tags_test.go

formats/cogwsi/
├── doc.go
├── factory.go        // FormatFactory: SupportsRaw via ghost-area parse
├── tiler.go          // *Tiler with full pyramid + associated
├── tiler_test.go
├── classifier.go     // WSIImageType-driven dispatch (overrides generic-tiff heuristics)
├── metadata.go       // WSIMPPX/Y/Mag → cross-format; cog-wsi.X Properties
├── metadata_test.go
└── (delegate to internal/tiff for Page reads; share tile-fetch code with generictiff where natural)
```

### 6.2. Hot path

Tile lookup mirrors `formats/generictiff/tiled.go` — pyramid IFDs are tiled, tile bytes are passthrough from the file via `*tiff.Page`. v0.9 mmap-default backing applies. v0.13 splice-prefix path applies if any pyramid IFD has shared `JPEGTables` (spec §5: writer preserves abbreviated JPEG; tile prefix splice happens at read time).

### 6.3. Pyramid validator change in `internal/tiff/classify_pyramid.go`

`buildPyramidChain` keeps its existing drift check but extends acceptance:

```go
// Existing: drift > tolerance → reject
// New: drift > tolerance → check integer-multiple ratio
if drift > cfg.InterLevelTolerance {
    if !isIntegerMultipleRatio(rW, ratios) {
        leftoverTiled = append(leftoverTiled, cand)
        continue
    }
    // Accept; record new ratio as additional acceptable step.
}
```

`isIntegerMultipleRatio(r, prior)` returns true when `r` is a clean integer multiple/divisor (within tolerance) of any prior chain ratio.

### 6.4. WSI-tag-aware classifier short-circuit in `formats/generictiff/classifier.go`

When ALL pyramid candidate IFDs carry `WSIImageType=pyramid`, build the pyramid from `WSILevelIndex` ordering directly — skip the dimension-ratio drift check entirely. Same approach for associated dispatch: `WSIImageType ∈ {label, macro, thumbnail, overview}` routes directly.

When tags are absent or only some IFDs carry them, fall back to the existing heuristic classifier.

### 6.5. cogwsi.Factory dispatch

```go
func (f *Factory) SupportsRaw(r io.ReaderAt, size int64) bool {
    // 1. TIFF header check (II + 0x2A or 0x2B).
    // 2. Read ghost area starting at offset 8 (classic) or 16 (BigTIFF).
    //    Up to GDAL_STRUCTURAL_METADATA_SIZE bytes.
    // 3. Parse via internal/cog.ParseGhostArea.
    // 4. Return true iff COG_WSI_VERSION key is present.
}
```

Cheap check — single short read + small parse. Cogwsi files dispatch here; generic COGs (no WSI version line) fall through.

`OpenRaw` re-uses the parsed ghost area (passed via Tiler state), validates the full conformance ruleset, returns `*Tiler` or `ErrNotConformantCOGWSI`.

## 7. Plan outline

Single batch, 8 tasks:

- **T1**: `internal/cog/` package — `ParseGhostArea` + `GhostArea` struct + `ParseCOGWSIVersion` + unit tests
- **T2**: `internal/tiff` WSI tag readers — 8 typed accessors on `*tiff.Page` + unit tests
- **T3**: `internal/tiff/classify_pyramid.go` — integer-multiple ratio acceptance (Issue #5 part B); golden tests on synthetic 4×/2×/2× chain
- **T4**: `formats/generictiff/classifier.go` — WSI-tag-aware short-circuit (Issue #5 part A); test against forced-route COG-WSI fixture
- **T5**: `formats/cogwsi/` skeleton — factory + ghost-area dispatch + new `opentile.FormatCOGWSI` enum + register in formats/all (before generic-tiff) + smoke test on CMU-1-Small-Region_cog-wsi.tiff
- **T6**: `formats/cogwsi/` Tiler + spec validation + metadata — full pyramid + associated wired; `ErrNotConformantCOGWSI` sentinel; WSI metadata tags → cross-format Metadata + cog-wsi.X Properties; spec-validation negative-case tests
- **T7**: Fixtures + tests — wire all 10 cog-wsi fixtures into slideCandidates + new `tests/parity/cogwsi_geometry_test.go` + per-tile SHA generation + cross-fixture parity gate
- **T8**: Docs + ship — `docs/formats/cogwsi.md` (new); README supported-formats row; `docs/deferred.md §8m` retirement audit (closes #5 + #6; updates R21 status); CHANGELOG `[0.19.0]`; CLAUDE.md milestone bump

Plan written separately at `docs/superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md`.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding pre-existing unrelated drift, sample_files, docs)
- `make test` green
- `TestSlideParity` 40 fixtures green (was 30; +10 cog-wsi)
- New `TestCOGWSIGeometry` green across all 10 fixtures
- `TestCrossFormatMetadata` (v0.17) green
- Per-fixture probe of COG-WSI fixtures: `Format() == "cog-wsi"`; first L0 tile starts with appropriate magic per source (JPEG SOI / JP2K SOC / etc.); `Metadata.MicronsPerPixelX/Y` populated from WSIMPP* tags
- Spec-validation negative cases return `ErrNotConformantCOGWSI`
- Cross-fixture parity: COG-WSI tiles match source-format tiles bit-exact on sampled positions (where source had a comparable tile at the same pyramid level + position)
- Issue #5 + #6 closed at ship

## 9. R21 status update

R21 in `docs/deferred.md §1` + §11 ("Cloud Optimized GeoTIFF (COG) first-class support") is **partially superseded** by v0.19:

- The COG-WSI-specific story (10 real fixtures, explicit consumer, dedicated reader) is fully shipped in v0.19 via `formats/cogwsi/`.
- General COG (non-WSI; geospatial COG without WSI tags) remains parked under R21. The new `internal/cog/` package pre-pares it (ghost-area parser is generic).
- T8 updates R21's status to "partially landed — cog-wsi shipped in v0.19; general COG awareness still parked pending HTTP-range backing or specific consumer ask."

## 10. References

- COG-WSI spec v0.1 (vendored): `docs/specs/2026-05-20-cog-wsi-format.md`
- COG-WSI spec source (canonical): `https://github.com/cornish/wsitools/blob/main/docs/superpowers/specs/2026-05-20-cog-wsi-format.md`
- Issue #5: `https://github.com/cornish/opentile-go/issues/5` (generic-tiff WSI-tag awareness + mixed-ratio relax)
- Issue #6: `https://github.com/cornish/opentile-go/issues/6` (dedicated cog-wsi reader)
- GDAL COG spec: `https://gdal.org/en/stable/drivers/raster/cog.html`
- OGC COG standard: `https://docs.ogc.org/is/21-026/21-026.html`

## 11. Active limitations introduced

None new. v0.19 is purely additive — new format reader + extensions to generic-tiff classifier. Existing consumers unaffected.

The §11 backlog row for R21 gets a partial-supersedure annotation (T8). R22 (Writer typed field) is unaffected; could be folded into a future milestone or paired with the new `Properties["cog-wsi.wsitools-version"]` if a consumer needs writer info on COG-WSI files.

## 12. Lessons feeding into v0.19 execution

- **v0.16 SZI lesson:** the `internal/dzi/` shared-core split paid off (clean separation between manifest parsing and storage backend; bare DZI can ride on it). v0.19 mirrors this with `internal/cog/` (shared between cogwsi reader + generic-tiff WSI-tag path + future R21 bare-COG reader).
- **v0.17 lesson:** plan vs. code drift on per-format metadata source naming. The COG-WSI spec is precise about tag IDs + types; implementer should hardcode per spec rather than re-derive.
- **v0.18 lesson:** when the writer (wsitools in this case) authors a clean spec, the reader-side fixture probing is fast — every fixture should validate against the spec without surprises. Sanity-check by probing one fixture before locking spec-validation assertions in T6.
- **v0.19-specific:** the cross-fixture parity gate (each `<source>_cog-wsi.tiff` vs its `<source>` original) is a powerful correctness check unique to this milestone — exercise it carefully in T7.
