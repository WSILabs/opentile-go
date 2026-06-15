# Ventana BIF (Roche)

Roche's WSI format for the VENTANA DP family of scanners (DP 200, DP 600, …) and predecessor iScan scanners (Coreo, HT). File extension `.bif`. The format is publicly specified by Roche (the [BIF whitepaper](https://www.roche.com/) v1.0, 2020) but only the DP 200 generation is documented in detail; legacy iScan slides require openslide-style permissive interpretation.

**v0.7 is the first opentile-go format beyond upstream Python opentile's coverage.** Upstream doesn't read BIF; openslide does (LGPL 2.1) but rejects spec-compliant DP 200 BIFs and may misinterpret modern BIFs generally — see "Parity" below.

## Format basics

- **TIFF dialect**: BigTIFF for the DP generation (the whitepaper mandates it; both committed fixtures match). Legacy iScan scanners (Coreo / HT, ~2010-2012, XMP `BuildVersion="3.x"`) predate the whitepaper and wrote **classic** (non-BigTIFF) little-endian TIFF — opentile-go reads both containers.
- **Detection**: at least one IFD whose XMP packet (TIFF tag 700) contains the substring `<iScan` — the marker **alone**, with no BigTIFF requirement (mirrors openslide's `INITIAL_XML_ISCAN` rule, which also keys solely on the marker). Earlier opentile-go additionally required BigTIFF, which wrongly rejected legacy classic-TIFF iScan slides — they then fell through to the generic-TIFF reader, which renders BIF's serpentine tile order scrambled (#37). The `<iScan` substring is BIF-specific: 0 false positives across the non-BIF fixtures (SVS / NDPI / Philips / OME / generic-TIFF).
- **Generation classification**: post-detection, the IFD-0 `<iScan>/@ScannerModel` attribute routes the slide. `strings.HasPrefix(scannerModel, "VENTANA DP")` → spec-compliant path (DP 200, DP 600, future DP); everything else → legacy-iScan path (missing attribute, iScan Coreo, iScan HT).
- **Pyramid layout**: top-level IFDs sorted by parsed `level=N` from each IFD's ImageDescription. Spec describes IFD 0 = label, IFD 1 = probability, IFD 2 = scan, IFD 3+ = pyramid; **OS-1 (legacy) violates this**: IFD 0 = label, IFD 1 = thumbnail, IFD 2..11 = pyramid (no probability). v0.7 classifies by ImageDescription content, not by IFD index.
- **Compression**: JPEG (tag 7) on every pyramid IFD. Associated images: NONE (Ventana-1 IFD 0 RGB raw strips), LZW (Ventana-1 IFD 1 grayscale probability strips), or JPEG (OS-1 IFD 0/1 single-tile).
- **Storage order**: TileOffsets is in physical-stage **serpentine** order, not image-space row-major. Stage rows count up from the bottom; even rows go left-to-right, odd rows right-to-left. The remap is in `formats/bif/serpentine.go::imageToSerpentine`. Confirmed against tifffile (`tests/oracle/tifffile_test.go::TestTifffileParityBIF` passes byte-equality on Ventana-1).
- **Tile overlap**: spec-compliant DP 200 slides record per-tile-pair overlap in the `<EncodeInfo>/<SlideStitchInfo>/<ImageInfo>/<TileJointInfo>` XMP elements. v0.7 collapses these to a single weighted-average `image.Point` per level, exposed via the new `Level.TileOverlap()` interface method. Pyramid IFDs 1+ are non-overlapping per spec.

## Fixture inventory

| File | Bytes | Generation | ScannerModel | openslide reads? | JPEGTables (tag 347) |
|---|---:|---|---|:---:|---|
| `Ventana-1.bif` | 227 MB | DP 200 (BuildVersion 1.1.0.15854, 2019) | `"VENTANA DP 200"` | ❌ rejects (`Direction="LEFT"`) | absent (per-tile embedded) |
| `OS-1.bif` | 3.6 GB | iScan Coreo (BuildVersion 3.3.1.1, 2011) | (missing) | ✅ reads | present (shared) |

The two fixtures are deliberately complementary — one tests the spec-compliant path that openslide rejects; the other tests the legacy path openslide accepts. Together they span both sides of the JPEGTables decision (per-tile embedded vs. shared) and exercise the ScanWhitePoint default-fallback.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| iScan detection + classification (BigTIFF or classic TIFF) | ✅ | T1 / T2 / T3 gates pin the discriminator behaviour (see deferred.md §9 v0.7 gates); detection keys on the `<iScan` XMP marker alone, so legacy classic-TIFF iScan slides are read too (#37) |
| Tiled pyramid levels | ✅ | Both raw-passthrough (Ventana-1: no JPEGTables) and `jpeg.InsertTables`-spliced output (OS-1: shared tables) |
| Serpentine → image-space remap | ✅ | `imageToSerpentine` + inverse, round-trip-tested |
| Empty tiles (TileOffsets[i]=0 AND TileByteCounts[i]=0) | ✅ | Filled with `ScanWhitePoint`-coloured JPEG via `formats/bif/blanktile.go` (T9). Both real fixtures have zero empty tiles — synthetic-only fixture coverage on this path |
| Probability map exposure (spec-compliant only) | ✅ | New `AssociatedImage.Type() == "probability"` (LZW grayscale; multi-strip raw passthrough) |
| Thumbnail exposure (legacy only) | ✅ | `AssociatedImage.Type() == "thumbnail"` (single-tile JPEG) |
| Label / overview exposure (every fixture) | ✅ | `AssociatedImage.Type() == "overview"`. Ventana-1: multi-strip uncompressed RGB. OS-1: single-tile JPEG |
| Synthesized `label` (every fixture) | ✅ | `AssociatedImage.Type() == "label"` — the **top 1/3** of the overview (the Label_Image's printed-label band), mirroring NDPI's macro-crop label so consumers can locate the label across formats (#19). Pixel-domain crop (the overview can be uncompressed or tiled JPEG), so `Compression()` is `none` and `Encoding()`/`TIFFTags()`/`IFDOffset()` report false (synthesized, no backing IFD). The top-25/75-mm fraction comes from the Roche whitepaper, robust where the XMP `LabelBoundary` is unreliable (1000 on OS-1, 0 on Ventana-1). `overview` is unchanged (still the full Label_Image), so the label pixels appear in both — see #19 for the deferred non-overlapping-split option |
| ICC profile passthrough | ✅ | `Tiler.ICCProfile()` returns level-0 IFD's tag 34675 (Ventana-1 has 1.8 MB; OS-1 has tag-with-zero-bytes → returns nil) |
| Generation-aware metadata via `bif.MetadataOf` | ✅ | Generation, ScanRes, ScanWhitePoint+Present, ZLayers, ZSpacing, ZPlaneFoci, AOIs, AOIOrigins, EncodeInfoVer |
| EncodeInfo Ver < 2 rejection | ✅ | spec mandates Ver≥2; `bifxml.ParseEncodeInfo` enforces; `Open` propagates the error |
| Defensive Direction value tolerance | ✅ | All 4 spec values + any unknown string passes through verbatim into `bifxml.TileJoint.Direction` (no enum validation, unlike openslide) |
| Volumetric Z-stack reading (since v0.7 multi-dim closeout) | ✅ | `IMAGE_DEPTH` (tag 32997) drives `Image.SizeZ()`; per-Z tile data read via `Level.TileAt(TileCoord{Z, X, Y})` with stride `Z * (cols*rows) + serpIdx` per BIF whitepaper §"Whole slide imaging process" storage layout. `<iScan>/@Z-spacing` drives `Image.ZPlaneFocus(z)`: Z=0 nominal, Z=1..nNear near focus, Z=nNear+1..N-1 far focus. Synthetic-fixture coverage only — both real fixtures have IMAGE_DEPTH=1 (verified via the multi-dim T1 gate) |

## Edge tile semantics

Tiles are stored at full `TileSize` regardless of position; right-edge and bottom-edge tiles include padding bytes in the unused region (the TIFF tile format stores them this way). The serpentine-remap pass changes which physical tile address corresponds to logical (x, y), but does not change per-tile dimensions. The ScanWhitePoint blank-tile fill emits a synthetic full-`TileSize` blank tile for sparse regions; same edge semantics. opentile-go returns the bytes verbatim per the byte-passthrough invariant. Consumers should clip rendered output to the meaningful sub-rect:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
```

SZI/DZI is the exception — its readers return border-sized tiles per spec; see `docs/formats/szi.md`.

## What's not supported

| Capability | Status | Why |
|---|---|---|
| openslide pixel-equivalence | ⚠️ — infrastructure-only in v0.7 | The runner / session / protocol are in `tests/oracle/openslide_*` but the assertion is gated. Resolution depends on whether opentile-go's padded-grid view or openslide's AOI-hull view is the right one to expose. Tracked as L19 |
| DP 600 verification | ⚠️ — schedule-driven | The `HasPrefix("VENTANA DP")` rule lands DP 600 on the spec-compliant path; behavioural variance from DP 200 is unverified without a fixture. Tracked as L20 |
| AOI-cropped Tile variant | ❌ — not designed yet | opentile-go's `Tile(col, row)` references the padded TIFF grid; an AOI-cropped variant would expose openslide's view. v0.8 work item |
| Multi-tile associated images | ❌ — error | Both real fixtures have single-tile or multi-strip associated pages; multi-tile seems unused in practice |

## Parity

Three layered oracles cover v0.7 BIF correctness:

1. **tifffile byte-equality** (`tests/oracle/tifffile_test.go::TestTifffileParityBIF`) — Ventana-1 only. Tests opentile-go's `Tile(col, row)` raw-passthrough output against tifffile's `page.dataoffsets[serpentine_idx]` raw bytes. Confirms (a) serpentine algebra correctness, (b) level=N → page sorting, (c) TileOffsets indexing. OS-1 excluded because shared JPEGTables modify the bytes.

2. **Sampled-tile SHA256 fixtures** (`tests/integration_test.go::TestSlideParity`) — both fixtures. Records corner / centre / edge probe SHA256 hashes in `tests/fixtures/Ventana-1.bif.json` and `OS-1.bif.json`; regenerate via `OPENTILE_TESTDIR=$PWD/sample_files go test ./tests -tags generate -run TestGenerateFixtures -generate -v`. Catches regressions in our own output across both fixture types.

3. **Geometry sanity tests** (`tests/parity/bif_geometry_test.go::TestBIFGeometry`) — both fixtures, no build tag, runs in `make test`. Pins per-level Size / TileSize / Grid / TileOverlap, JPEG markers, ICC presence, AOI origin alignment, EncodeInfo Ver, Generation, ScanRes.

**openslide pixel-equivalence is NOT a v0.7 correctness bar.** The original v0.7 design (spec §7) intended it as the primary oracle; mid-implementation we found that (a) openslide rejects DP 200 BIFs entirely, and (b) for OS-1 it uses an AOI-hull coordinate system that differs from opentile-go's padded TIFF grid. Anecdotal community note: openslide is also believed to misread modern BIF generally. The runner / session / test scaffold ship in v0.7 (T20) for v0.8 follow-up; the test currently `t.Skip`s with a clear gap explanation.

## Deviations from upstream Python opentile

Upstream Python opentile doesn't read BIF, so every v0.7 behaviour is technically a deviation. The interesting ones — captured in [`docs/deferred.md` §1a](../deferred.md#1a-deviations-from-upstream-python-opentile) — are:

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Probability map exposure as `type="probability"` | v0.7 | iterate `Associated()` and skip the type | Slide author embedded it; throwing it away is value loss. Joins the existing type taxonomy (overview / macro / thumbnail / label / map / probability) |
| `Level.TileOverlap() image.Point` interface evolution | v0.7 | non-BIF formats return `image.Point{}` (zero) — no caller change needed | Tile() returns raw compressed bytes (preserving byte-passthrough hot path); consumer needs the overlap value to position tiles correctly |
| Non-strict `ScannerModel` acceptance | v0.7 | not opt-out-able | Spec mandates `ScannerModel == "VENTANA DP 200"` rejection-otherwise; we accept any iScan-tagged TIFF (BigTIFF or classic, per #37) and route via `HasPrefix("VENTANA DP")` so legacy iScan slides aren't worse-than-openslide |

## Cross-format Metadata mapping (v0.17)

BIF's iScan XMP carries the cross-format-canonical fields. v0.17 surfaces them:

| iScan XMP source | cross-format Metadata position |
|---|---|
| `ScanRes` (X / Y when distinct) | `MPP.X`/`MPP.Y`; `MPP.Symmetric()` non-zero when X == Y (both fixtures isotropic at 0.25 / 0.2325) |
| `Magnification` | `Magnification` |
| Vendor (constant) | `ScannerManufacturer = "Roche"` |
| `ScannerModel` | `ScannerModel` (e.g., `VENTANA DP 200`) |
| Synthesised splice descriptor | `ImageDescription` (`level=0 mag=40 quality=95`) |
| `UserName` | `Properties[PropertyUserName]` (canonical) AND `Properties["ventana.UserName"]` |
| every other iScan XMP attribute | `Properties["ventana.<key>"]` (vendor passthrough — 13–18 keys per fixture) |
| `BuildVersion` (iScan XMP) | `Metadata.Writer` (v0.20) |

Per Q4 Option B, `bif.Metadata.ImageDescription` was retired (the cross-format `ImageDescription` covers it); `bif.MetadataOf(t)` continues to expose `ZSpacing` and `ZPlaneFoci`.

## Implementation references

- Our package: `formats/bif/`
- Public API: `bif.New() opentile.FormatFactory` + the existing `Tiler` / `Image` / `Level` / `AssociatedImage` interfaces; v0.7 additions: `Level.TileOverlap()`, `Level.TileAt(TileCoord)`, `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus`.
- Our metadata accessor: `bif.MetadataOf(opentile.Tiler) (*Metadata, bool)` — exposes `ZSpacing` and `ZPlaneFoci` for multi-Z slides in addition to the v0.7-base fields.
- BIF XMP walker: `internal/bifxml/`.
- Blank-tile generator (empty-tile fill): `formats/bif/blanktile.go`.
- Spec: [BIF whitepaper](https://www.roche.com/) v1.0, 2020, MC--06058 1120. Local copy at `sample_files/ventana-bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf`.
- v0.7 BIF design: [`docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md`](../superpowers/specs/2026-04-27-opentile-go-v07-design.md).
- v0.7 BIF plan: [`docs/superpowers/plans/2026-04-27-opentile-go-v07.md`](../superpowers/plans/2026-04-27-opentile-go-v07.md).
- v0.7 multi-dim closeout design: [`docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md`](../superpowers/specs/2026-04-29-opentile-go-multidim-design.md).
- v0.7 multi-dim closeout plan: [`docs/superpowers/plans/2026-04-29-opentile-go-multidim.md`](../superpowers/plans/2026-04-29-opentile-go-multidim.md).
- Research notes (whitepaper digest, fixture probes, openslide-source extraction): [`docs/superpowers/notes/2026-04-27-bif-research.md`](../superpowers/notes/2026-04-27-bif-research.md).
- openslide reader (LGPL 2.1, read-for-understanding only): [`openslide/openslide`](https://github.com/openslide/openslide) — `src/openslide-vendor-ventana.c`.

## Known issues + history

- **Detection regex** is the literal substring `<iScan` (with opening angle bracket) to discriminate against arbitrary text containing the word "iScan" outside an XML element context.
- **OS-1 has no `<EncodeInfo>/<FrameInfo>/<Frame>` elements** at all — predates that XMP feature. The serpentine algorithm works without Frame data; if a future fixture surfaces meaningful Frame disagreements, we'd add per-pair lookup.
- **Both fixtures carry NON-ZERO TileOverlap on level 0** (Ventana-1 L0=(2,0); OS-1 L0=(18,26)) — contrary to the v0.7 design spec §10's initial "fixture-untested overlap path" claim. The notes file's "all zero" claim was based on a 1500-char XMP truncation; the full XMP carries a sparse mix of zero and non-zero `<TileJointInfo>` entries.
- **Two correctness bugs caught only by writing the integration test (T19)**: (a) `loadEncodeInfo` swallowed `bifxml.ParseEncodeInfo`'s Ver<2 error, defeating the spec-mandated rejection gate; (b) `bif.MetadataOf` didn't unwrap the file-closer Tiler returned by `opentile.OpenFile`, so it always returned `(nil, false)` on real callers. Both fixed in `49849a4`.

See [`docs/deferred.md`](../deferred.md) §8a for the full reasoning + commit references.
