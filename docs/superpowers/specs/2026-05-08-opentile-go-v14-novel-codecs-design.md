# opentile-go v0.14 — wsi-tools novel-codec generic-TIFF support

**Status:** sealed 2026-05-08.
**Work branch:** `feat/v0.14`.
**Headline:** generic-TIFF reader extended to recognise four novel tile compression values produced by the user's `wsi-tools` transcoder (WebP, JPEG XL, AVIF, HTJ2K). Plus opportunistic parsing of the wsi-tools `ImageDescription` format to populate cross-format Metadata fields. Additive — no breaking changes.

## 1. Scope

### 1.1. Three new `opentile.Compression` enum values

`CompressionAVIF` already exists (added in v0.8 for IFE). v0.14 adds three companions for the other novel codecs:

```go
CompressionWebP    Compression = ...  // tile bytes are a complete WebP RIFF file
CompressionJPEGXL  Compression = ...  // tile bytes are a JPEG XL codestream
CompressionHTJ2K   Compression = ...  // tile bytes are an HTJ2K (JPEG 2000 Part 15) codestream
```

Each codec gets its own enum value (per sealed Q1) so consumers can switch on `Level.Compression()` for decoder selection. HTJ2K is intentionally distinct from `CompressionJP2K` — a standard JP2K decoder will fail on HTJ2K bytes (different entropy coder), so consumers must dispatch to an HTJ2K-capable decoder (OpenJPEG 2.5+, OpenHTJ2K, etc.).

`Compression.String()` cases added: `"webp"`, `"jpeg-xl"`, `"htj2k"` (matching the existing `"jpeg"`, `"jp2k"`, `"avif"` casing convention).

### 1.2. TIFF compression tag value mappings

The user's wsi-tools transcoder uses these tag-259 values:

| Tag value | Codec | Mapping |
|---:|---|---|
| 50001 | WebP | `→ CompressionWebP` (matches libtiff convention) |
| 50002 | JPEG XL | `→ CompressionJPEGXL` (matches some libtiff branches; not a registered TIFF code) |
| 60001 | AVIF | `→ CompressionAVIF` (private/experimental range; wsi-tools choice) |
| 60003 | HTJ2K | `→ CompressionHTJ2K` (private/experimental range; wsi-tools choice) |

Plus a bonus mapping while we're touching the validator:

| Tag value | Codec | Mapping |
|---:|---|---|
| 34712 | JP2K (registered) | `→ CompressionJP2K` (we already accept Aperio's nonstandard 33003; 34712 is the libtiff/registered-IANA code) |

Both `formats/generictiff/tiled.go::tiffCompressionToOpentile` and `internal/tiff/classify_pyramid.go::validCompression` get these five new values.

### 1.3. wsi-tools `ImageDescription` parser

When the level-0 `ImageDescription` starts with `wsi-tools/`, opentile-go parses the format:

```
wsi-tools/<version> transcode source=<src> codec=<codec> mpp=<float> mag=<N>x scanner="<name>" date=<YYYY-MM-DD>
```

Parsed fields populate the existing standard `Metadata` surface (per sealed Q2):

| ImageDescription field | Populates |
|---|---|
| `mpp=<float>` | `generictiff.Metadata.MicronsPerPixel` — overrides the XResolution+ResolutionUnit derivation when both are present (wsi-tools value is more authoritative) |
| `mag=<N>x` | `opentile.Metadata.Magnification` (via embedded struct) — parses leading float |
| `scanner="<name>"` | `opentile.Metadata.ScannerManufacturer` |
| `date=<YYYY-MM-DD>` | `opentile.Metadata.AcquisitionDateTime` — interpreted as 00:00:00 UTC of that date |
| `source=<src>` | not surfaced (provenance-only; consumers reading the raw `ImageDescription` string see it) |
| `codec=<codec>` | not surfaced (already inferable from `Level.Compression()`) |

**Non-wsi-tools `ImageDescription` strings are unaffected.** The parser is gated by the `wsi-tools/` prefix; existing v0.10 generic TIFFs that carry arbitrary `ImageDescription` text continue through the existing path (raw text only, no field population).

The parser is lenient: unknown keys are ignored (forward-compatible with future wsi-tools additions); malformed values produce zero on that field but don't fail the file load.

### 1.4. Validator + classifier behaviour

`internal/tiff/classify_pyramid.go::validCompression` accepts the five new tag values (50001, 50002, 60001, 60003, 34712). Tiled IFDs using these compressions become valid pyramid candidates per the existing v0.11 single-level (MinLevels=1) and multi-level pyramid rules — no other validator changes.

`formats/generictiff/classifier.go::ClassifyAssociated` is unchanged. The four wsi-tools fixtures all have stripped JPEG / LZW associated IFDs preserved from the source SVS; the new compressions only appear on the main pyramid IFD. Future fixtures with WebP/JXL/AVIF/HTJ2K-encoded associated images would need a follow-on extension to the heuristic classifier; out of scope for v0.14.

## 2. Out of scope

- Decoder integration. opentile-go ships byte-passthrough only. Consumers bring their own libwebp / libjxl / libavif / OpenJPEG-HTJ2K (mirrors the v0.8 IFE precedent for AVIF / Iris-proprietary codecs).
- New `WSIToolsMetadataOf(tiler)` accessor. Sealed Q2: parse populates standard cross-format fields only; the raw `ImageDescription` string remains accessible via `generictiff.MetadataOf` for consumers who want full provenance (`source`, `codec`, `wsi-tools` version).
- Detection-side support for non-wsi-tools producers of these compressions. We don't probe the codec bytes; we trust the TIFF compression tag value. If a different tool emits tag 60001 with non-AVIF bytes, we'd report `Compression() == CompressionAVIF` and the consumer would attempt AVIF decode and fail — same contract as IFE has today.
- Per-codec content validation (e.g., verifying first-tile magic bytes match the declared compression). Out of scope; YAGNI.
- v1.0 cut. Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Naming + count of new Compression enum values | Four enum values total: 3 new (`CompressionWebP`, `CompressionJPEGXL`, `CompressionHTJ2K`) + existing `CompressionAVIF`. HTJ2K stays distinct from JP2K because their decoders aren't interchangeable. |
| Q2 | wsi-tools `ImageDescription` parser scope | Parse to populate standard `Metadata` fields (Magnification, ScannerManufacturer, AcquisitionDateTime, MicronsPerPixel). No wsi-tools-specific public accessor. |

## 4. Fixtures

Four real-fixture files placed in `sample_files/generic-tiff/` by the user (2026-05-08):

| File | Bytes | Codec | Tag |
|---|---:|---|---:|
| `avif-out.tiff` | 1.8 MB | AVIF | 60001 |
| `htj2k-out.tiff` | 3.1 MB | HTJ2K | 60003 |
| `jxl-out.tiff` | 1.3 MB | JPEG XL | 50002 |
| `webp-out.tiff` | 1.6 MB | WebP | 50001 |

Each file's structure: 1 tiled IFD (2220×2967, 240×240 tile) carrying the novel codec + 3 stripped associated IFDs (574×768 JPEG thumbnail, 387×463 LZW label, 1280×431 JPEG macro) preserved from the source SVS.

First-tile magic bytes verified per fixture during the v0.14 brainstorm:
- AVIF: `00 00 00 20 66 74 79 70 61 76 69 66` (ftyp box, "avif" major brand)
- HTJ2K: `FF 4F FF 51` (J2K SOC + SIZ markers — bare codestream form, no JP2 box)
- JPEG XL: `FF 0A` (JXL codestream marker, bare form)
- WebP: `52 49 46 46` (RIFF) `57 45 42 50` (WEBP)

## 5. Test strategy

### 5.1. Per-fixture geometry pinning

Extend `tests/parity/generic_geometry_test.go::genericFixtures` with rows for the four new files. Pin per-level Size / TileSize / Grid / Compression, plus the 3 associated images.

### 5.2. Per-tile SHA fixtures

Generate `tests/fixtures/avif-out.tiff.json`, `htj2k-out.tiff.json`, `jxl-out.tiff.json`, `webp-out.tiff.json` via `TestGenerateFixtures`. Wire into `slideCandidates` so `TestSlideParity` exercises tile-byte parity going forward.

### 5.3. Compression-recognition unit tests

`internal/tiff/classify_pyramid_test.go`: extend `TestValidCompression` with the five new tag values.

`formats/generictiff/tiled_test.go` (or equivalent): unit-test `tiffCompressionToOpentile` for the new mappings.

### 5.4. wsi-tools parser unit tests

New `formats/generictiff/wsitools_test.go` (or extend an existing test file): pure-function tests on the parser:
- Happy path with all 6 fields
- Quoted scanner with embedded space
- Missing optional fields (mpp / mag / etc.)
- Malformed mpp value → zero, no error
- Non-wsi-tools `ImageDescription` → no parse, no field population
- Forward-compat: extra unknown key → ignored

## 6. Active limitations introduced

None new. v0.14 is additive-only (3 new enum values + 1 parser). Existing consumers using v0.13 surfaces see no behaviour change.

The four §11 backlog rows for the wsi-tools-style novel-codec generic TIFFs (which weren't actually parked items, but a brand-new requirement) shipped here.

## 7. Plan outline

Single batch, 6 tasks:

- **T1**: `compression.go` — add 3 enum values + `String()` cases + unit tests.
- **T2**: `internal/tiff/classify_pyramid.go::validCompression` — add 5 new tag values to whitelist + unit tests.
- **T3**: `formats/generictiff/tiled.go::tiffCompressionToOpentile` — add 5 new mappings + unit tests.
- **T4**: `formats/generictiff` — wsi-tools `ImageDescription` parser. New `wsitools.go` (parser) + `wsitools_test.go` (golden tests). Wire into `buildMetadata` (or equivalent) so the parsed fields populate the standard Metadata struct.
- **T5**: Tests + fixtures — wire 4 fixtures into `slideCandidates`, generate SHA JSONs, extend `generic_geometry_test.go` with 4 fixture rows.
- **T6**: Docs + ship — `docs/formats/generictiff.md` (compression list update + wsi-tools parser note + decoder responsibility statement); `docs/deferred.md §8h` (v0.14 retirement audit); `CHANGELOG.md [0.14.0]`; `CLAUDE.md` milestone bump.

Plan written separately at `docs/superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md`.

## 8. Verification

End-of-milestone gates:
- `go vet ./...` clean.
- `make test` green.
- `TestSlideParity` extended to 28 fixtures (24 post-v0.13 + 4 new wsi-tools fixtures); all green.
- Per-codec recognition: opening each new fixture reports `Tiler.Levels()[0].Compression()` matching the expected enum value; first-tile bytes start with the documented codec magic.
- wsi-tools parser: opening any of the 4 fixtures yields `Tiler.Metadata().Magnification == 20`, `ScannerManufacturer == "Aperio"`, `AcquisitionDateTime != zero` (2009-12-29), `generictiff.MetadataOf(t).MicronsPerPixel == 0.499`.

## 9. Decoder responsibility (consumer-facing documentation)

`docs/formats/generictiff.md` gains a section explicitly noting:

- opentile-go ships byte-passthrough for the new compressions.
- Per-codec decoder responsibility:
  - `CompressionWebP` → libwebp or `golang.org/x/image/webp`
  - `CompressionJPEGXL` → libjxl (cgo) or wait for stdlib `image/jxl`
  - `CompressionAVIF` → libavif (cgo) or wait for stdlib `image/avif`
  - `CompressionHTJ2K` → OpenJPEG 2.5+, OpenHTJ2K, or Kakadu
- Magic-byte cheatsheet so consumers can sanity-check their decoder dispatch.
- Mirror of v0.8 IFE precedent: opentile-go reports the compression via `Level.Compression()`; consumers ship the right decoder.
