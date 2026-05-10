# Leica SCN

Leica's WSI format for the SCN400 / SCN400F brightfield + fluorescence scanner family. File extension `.scn`. **Scanner production discontinued ~2015** — this is a legacy-format reader covering existing slides; new SCN files in the wild are rare. The format is undocumented publicly; both bio-formats and openslide ship readers and our implementation cross-references both.

**v0.11 is the third opentile-go format beyond upstream Python opentile's coverage** (after BIF in v0.7 and IFE in v0.8). Upstream doesn't read SCN.

## Format basics

- **TIFF dialect**: BigTIFF only (verified across all 3 fixtures; the SCN XML schema URN appears to assume BigTIFF).
- **Detection**: BigTIFF + IFD 0's ImageDescription contains the SCN schema URN `http://www.leica-microsystems.com/scn/2010/10/01`. Cheap substring search; full XML parse happens at `Open` time. Sealed at v0.11 design Q1.
- **Mental model**: a SCN file is **one slide, discontinuously sampled**. The scanner scans only the rectangles containing tissue (not the whole slide bounding box like SVS does). Multiple `<image>` elements share one slide-level coordinate system via `<view offsetX/Y>` attributes. Whitespace between scan rectangles isn't stored at all — our reader fills those gaps with synthesised white JPEG tiles so the consumer sees one continuous slide.
- **XML mapping**: IFD 0's ImageDescription carries the SCN XML. `<scn>/<collection>` defines slide physical extents (in nm); each `<image>` has `<pixels>/<dimension r="N" c="C" ifd="K">` entries mapping (resolution × channel) → TIFF IFD index, plus `<view>` (slide-physical offset/size) and `<scanSettings>/<objective>` + `<illuminationSource>` metadata. **Without the XML the file is a pile of unlabeled IFDs.**
- **Auxiliary vs main classification (sealed Q2)**: an `<image>` is auxiliary iff its `<view>` covers the entire `<collection>` (offset 0,0 + dims match). Otherwise it's a main scan. Magnification is metadata only — geometric matters. Mirrors openslide's `is_macro` check at `src/openslide-vendor-leica.c:469` and bio-formats's classification.
- **Multi-channel**: fluorescence main scans carry `<dimension c="N">` attributes mapping each (resolution, channel) pair to its own IFD. `Image.SizeC()` returns `max(c)+1`; per-channel access via `Level.TileAt(TileCoord{C: c, X, Y})` reads from the correct IFD.
- **Compression**: JPEG (tag 7) on every pyramid IFD across all 3 fixtures. Our reader assumes JPEG-only; non-JPEG SCN files would error.
- **JPEG splice**: pyramid + auxiliary IFDs that carry shared `JPEGTables` (tag 347) get the v0.9 in-place splice template applied per tile (`internal/jpeg.BuildSplicePrefix` + `InsertPrefixInPlace`). No APP14 — SCN tiles are standard YCbCr JPEG, not Aperio's Adobe-marker variant.

## Fixture inventory

| File | Bytes | Layout | `<image>` count | SizeC | Headline coverage |
|---|---:|---|---:|---:|---|
| `Leica-1.scn` | 278 MB | 1 macro + 1 main scan (5-level pyramid) | 2 | 1 | Single-region brightfield; sanity case |
| `Leica-2.scn` | 2.1 GB | 1 macro + **4 main scans** (each 6-level pyramid) | 5 | 1 | **Multi-region "discontinuous scanning"** — one slide with 4 disjoint tissue rectangles |
| `Leica-Fluorescence-1.scn` | 21 MB | 2 auxiliaries (1 brightfield + 1 fluorescence whole-slide) + 1 fluorescence main (4-level × 3-channel) | 3 | 3 | **First real fixture exercising `Image.SizeC() > 1`** in opentile-go |

**Permanent fixture limitation.** SCN scanner production stopped ~2015. Additional fixtures are hard to come by — these 3 (downloaded from openslide-testdata 2026-05-01) are likely our complete coverage forever. We supplement with bio-formats CLI parity (`tests/oracle/leicascn_bf_test.go`) to cover the long tail of real-world SCN files that may exist outside our slate. Real-world divergences will be debugged from-scratch when reported.

## SCN structure (canonical)

A `Leica-2.scn` walkthrough makes the file structure concrete:

```
collection sizeX=26.5mm sizeY=76.7mm
├─ <image>  AUXILIARY (whole-slide macro, 0.6× magnification)
│  └─ 3 IFDs:  IFD0 1616×4668  IFD1 404×1167  IFD2 101×291 (lowest-res)
│                                                            ↑ surfaced as
│                                                              AssociatedImage(macro)
│
├─ <image>  MAIN SCAN at slide offset (10.3, 18.6) mm, 40× magnification
│  └─ 6 IFDs:  IFD3 39168×26048  ... IFD8 38×25
│
├─ <image>  MAIN SCAN at slide offset (10.4, 12.5) mm
│  └─ 6 IFDs:  IFD9 39360×23360  ... IFD14 38×22
│
├─ <image>  MAIN SCAN at slide offset (9.0, 34.6) mm
│  └─ 6 IFDs:  IFD15 ... IFD20
│
└─ <image>  MAIN SCAN at slide offset (9.0, 40.8) mm
   └─ 6 IFDs:  IFD21 ... IFD26
```

The 4 main scans are 4 vertically-stacked tissue regions on one slide. SCN stores only the rectangles containing tissue; the whitespace between them isn't in the file. Our reader composites the 4 mains into a single Image; the whitespace gaps render as synthesised white JPEG tiles so consumers see one continuous slide.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| BigTIFF detection + SCN XML parse | ✅ | `formats/leicascn/scnxml.go`; ParseDescription is exercised on golden XML strings (in `formats/leicascn/testdata/`) without requiring fixture files |
| Auxiliary/main classification | ✅ | View-extent matching (sealed Q2). Magnification is metadata; not part of the role decision |
| Single-region pyramid | ✅ | Leica-1, Leica-Fluorescence-1 |
| Multi-region composite pyramid | ✅ | Leica-2; sealed Q4. Multi-region offsets are tile-snapped (rounded down to nearest tile boundary) for raw-tile-bytes API alignment — see "Position imprecision" below |
| Inter-region blank fill | ✅ | Sealed Q6. Synthesised white JPEG cached per tile size; consumer never sees the discontinuity |
| Multi-channel fluorescence | ✅ | Sealed Q7. `Image.SizeC()` + `Level.TileAt(TileCoord{C, X, Y})` via the v0.7 multi-dim API |
| Auxiliary `<image>` → AssociatedImage | ✅ | Sealed Q8. Each auxiliary surfaces its lowest-resolution IFD as a single AssociatedImage with `Type() == "overview"`; multiple auxiliaries (Fluorescence has 2) surfaced in XML order |
| Format-specific metadata via `leicascn.MetadataOf` | ✅ | CollectionUUID, Barcode, Auxiliaries (per-aux illumination + objective), Regions (per-main offset/size in nm + objective + illumination), Channels (per-channel filter + exposure metadata for fluorescence) |
| ICC profile passthrough | ✅ | `Tiler.ICCProfile()` reads tag 34675 from level-0 IFD verbatim |
| JPEG splice via shared JPEGTables | ✅ | v0.9 in-place splice template; zero-alloc TileInto |
| `WarmLevel(i)` page-cache pre-warm | ✅ | Standard v0.9 pattern |
| Cross-backing parity (mmap default vs pread) | ✅ | `tests/parity/leicascn_geometry_test.go::TestSCNOpenFileBackingsByteIdentical` |

## What's not supported

| Capability | Status | Tracking |
|---|---|---|
| Multi-Z stack | ❌ — no fixture in slate; XML carries `spacingZ` + `<dimension z="N">` but our 3 fixtures are z=0 | `docs/deferred.md` §2 L30 — fixture-driven |
| Pixel-precise multi-region positioning | ❌ — region offsets are tile-snapped (≤ 1 tile = ~128 µm worst-case position error at 250 nm/px) | "Position imprecision" below; consumers wanting pixel precision composite manually using `Metadata.Regions` |
| AOI-cropped Tile variant | ❌ — out of scope; consumers re-tile the composite if needed | `docs/deferred.md` §2 L31 — YAGNI |
| Mismatched-objective regions | ❌ — `ErrUnsupportedSCN` if main scans have different objective magnification, illumination, or pyramid depth | `docs/deferred.md` §2 L32 — fixture-driven |
| Byte-equality oracle vs bio-formats | ❌ — permanent | `docs/deferred.md` §2 L33 — bio-formats decode + re-encode differs from our passthrough by design |
| Real-world SCN files outside the 3-fixture slate | ⚠️ — debug-from-scratch | `docs/deferred.md` §2 L34 — permanent (production discontinued) |

### Position imprecision (multi-region trade-off)

A SCN file's `<view offsetX/Y>` values are nm-precise slide-physical coordinates. When we composite multiple main scans into one Image, each main's offset must be expressed in composite-pixel-space (not nm). Pixel offsets generally don't align with our tile-grid resolution: e.g., Leica-2's region 0 lands at composite Y=24427 px, but the tile grid is at Y=24064, 24576, ... — region origin is 71% of a tile off-grid.

Our raw-tile-bytes API can't decode-and-recomposite at tile boundaries (would require libjpeg overhead per tile). So we **tile-snap region offsets down** to the nearest tile boundary at construction. Cost: a region's apparent composite position may be up to one tile (~128 µm at typical 250 nm/px scanner resolution) earlier than its true slide-physical position. Pathology-rendering-acceptable.

Consumers needing pixel-precise slide positioning can read `leicascn.MetadataOf(tiler).Regions` for the original nm-space (offset, size) of each main scan and composite from raw region tiles themselves.

## Parity

Two layered oracles cover v0.11 SCN correctness:

1. **Sample-tile SHA256 fixtures** (`tests/integration_test.go::TestSlideParity`) — all 3 fixtures. Records per-tile SHA256 hashes in `tests/fixtures/Leica-{1,2,Fluorescence-1}.scn.json`. Catches regressions in our own output. Leica-1 (278 MB) and Leica-2 (2.1 GB) are sampled-mode (corner + center probes); Leica-Fluorescence-1 (21 MB) is full-walk channel-0 only. Channel 1 / 2 distinct-bytes coverage lives in the geometry test below.

2. **Geometry pinning + cross-backing parity** (`tests/parity/leicascn_geometry_test.go`) — all 3 fixtures, no build tag, runs in `make test`. Pins per-level Size / TileSize / Grid / Compression, AssociatedImage kinds + sizes, Image.SizeC + ChannelName, distinct-bytes-per-channel sanity (3 distinct hashes for Fluorescence), Metadata fields (ScannerModel, Barcode, Regions count, per-Auxiliary IlluminationSource), and tile-byte equality across mmap / pread backings.

3. **Bio-formats CLI parity** (`tests/oracle/leicascn_bf_test.go`, build tag `bfparity`) — all 3 fixtures. Per sealed Q9: structural-equivalence parity, NOT byte-equality. Compares opentile-go's IFD-to-image-pyramid mapping against `/opt/bftools/showinf` output. Single-region files: per-level (Width, Height) must appear in bio-formats's series list. Multi-region files: bio-formats series count must equal `regionCount × levels + auxCount × auxDepth(3)`. Confirms our reader sees the same file structure as the de-facto reference reader.

Tile byte-equality oracle vs bio-formats is **not feasible** — bio-formats decodes + re-encodes JPEG; our raw passthrough won't match.

## Deviations from upstream Python opentile

Upstream Python opentile doesn't read SCN, so every v0.11 behaviour in this package is technically a deviation. The interesting one — captured in [`docs/deferred.md` §1a](../deferred.md#1a-deviations-from-upstream-python-opentile) — is:

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Leica SCN reader for legacy SCN400 / SCN400F output | v0.11 | not opt-out-able once registered | First real-fixture exercise of `Image.SizeC() > 1` in opentile-go (Leica-Fluorescence-1's separated-channel data); also the first multi-region "discontinuous scanning" reader. Architecturally valuable beyond just SCN coverage |

## v0.15 — Type() rename + value alignment

Leica SCN's auxiliary `<image>` elements now emit `Type() == "overview"` (was `"macro"` pre-v0.15), aligning with the v0.15 Q5 seal: every opentile-go format except Iris IFE emits `"overview"` for the wide-field slide image (matching DICOM PS3.3 + upstream Python opentile).

Additionally, the `AssociatedImage.Kind()` method was renamed to `Type()` (DICOM ImageType convention).

**Consumer migration:** where you reference `Type()` for Leica SCN auxiliary images, replace `case "macro":` with `case "overview":`. Update any references to `AssociatedImage.Kind()` → `AssociatedImage.Type()`.

## Cross-format Metadata mapping (v0.17)

Pre-v0.17 `leicascn.Tiler.Metadata()` returned an empty struct. v0.17 (T6) wires it from the parsed SCN-XML:

| SCN-XML source | cross-format Metadata position |
|---|---|
| `<view sizeX>` / `<view sizeY>` (slide-physical extent in nm) ÷ `<pixels sizeX>` / `<pixels sizeY>` (level-0 pixel extent), nm → µm | `MicronsPerPixelX/Y`; `MicronsPerPixel` set when X == Y (all 3 fixtures symmetric) |
| objective magnification element | `Magnification` (already populated since v0.11) |
| `Leica` (constant) / `<scanSettings><scannerSettings>` model | `ScannerManufacturer` / `ScannerModel` (already populated since v0.11) |
| full SCN-XML document | `ImageDescription` |
| `<barcode>` text | `Properties["leica.barcode"]` |
| `<collection name>` / `<collection uuid>` | `Properties["leica.collection.name"]` / `Properties["leica.collection.uuid"]` |
| `<illuminationSource>` | `Properties["leica.illumination_source"]` |
| classified region count | `Properties["leica.region_count"]` |

For multi-region SCN files, the cross-format Metadata reflects region 0; `leicascn.MetadataOf(t)` exposes the full per-region detail.

## Implementation references

- Our package: `formats/leicascn/`
- Public API: `leicascn.New() opentile.FormatFactory` + the existing `Tiler` / `Image` / `Level` / `AssociatedImage` interfaces. `Image.SizeC()` + `Image.ChannelName(c)` + `Level.TileAt(TileCoord{C, X, Y})` are the multi-channel entry points (added in v0.7).
- Our metadata accessor: `leicascn.MetadataOf(opentile.Tiler) (*Metadata, bool)` — exposes CollectionUUID, Barcode, Auxiliaries, Regions, Channels in addition to the embedded `opentile.Metadata` cross-format fields.
- XML schema parser: `formats/leicascn/scnxml.go`. Hand-rolled walker over `xml.Decoder` tokens (mirrors `internal/bifxml/`). Schema URN: `http://www.leica-microsystems.com/scn/2010/10/01`.
- Multi-region compositor: `formats/leicascn/classify.go::ComposePyramid` — value-in / value-out helper enforcing the v0.11 sealed Q5 invariants (matching depth + illumination + objective + ±2% per-level resolution similarity).
- Per-region Level: `formats/leicascn/tiled_region.go`. Per-channel offsets/counts/JPEGTables/splice prefix.
- Composite multi-region Level: `formats/leicascn/tiled.go`. O(N) findRegion + tile-snapped per-region bounds + cached blank-tile fill.
- Blank-tile generator: `formats/leicascn/blanktile.go`. Lifted shape from `formats/bif/blanktile.go` (white-fill JPEG, cached per (w, h) key).
- v0.11 SCN design: [`docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md`](../superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md).
- v0.11 SCN plan: [`docs/superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md`](../superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md).
- openslide reader (LGPL 2.1, read-for-understanding only): `openslide/openslide` `src/openslide-vendor-leica.c`.
- bio-formats reader (BSD-2-Clause, read-for-understanding only): `ome/bioformats` `components/formats-bsd/src/loci/formats/in/LeicaSCNReader.java`.

## Known issues + history

- **Multi-region offsets are tile-snapped** (rounded down to nearest tile boundary) per the position-imprecision trade-off described above. The spec sealed this as Q4 + Q6's "consumer never sees discontinuity" obligation rather than as a separate Q-decision; documented inline in `formats/leicascn/tiled.go`.
- **Two auxiliary `<image>` elements with `Type() == "overview"`** are allowed (Leica-Fluorescence-1's brightfield + fluorescence whole-slide pair). Consumers can disambiguate via `leicascn.MetadataOf(tiler).Auxiliaries[i].IlluminationSource`. This differs from openslide which actively rejects files with >1 macro at line 524.
- **scn_620 orphan IFD silently drops** — this is a generictiff issue not SCN-specific, documented in `docs/formats/generictiff.md`. SCN's classifier doesn't have orphan-IFDs (each `<image>` is fully accounted for via the XML).

See [`docs/deferred.md`](../deferred.md) §8e for the full v0.11 retirement audit.
