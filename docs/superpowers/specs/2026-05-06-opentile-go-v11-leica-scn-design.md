# opentile-go v0.11 — Leica SCN reader + generictiff relaxations

**Status:** sealed 2026-05-06.
**Work branch:** `feat/v0.11`.
**Headline:** first format reader exercising `Image.SizeC() > 1` on a real fixture (Leica-Fluorescence-1.scn), with multi-region "discontinuous scanning" semantics on multi-ROI fixtures (Leica-2.scn). Plus a small validator-relaxation closeout on `formats/generictiff` to cover real-world Grundium output.

## 1. Scope

### Headline: Leica SCN format reader (R16)

**`formats/leicascn`** package implementing Leica SCN — a BigTIFF dialect produced by Leica SCN400 / SCN400F scanners (production discontinued ~2015). Single new package mirroring the structure of `formats/svs/`, `formats/bif/`, `formats/leicascn`'s sibling vendor readers.

### Folded in: generictiff validator relaxations

Two relaxations to `formats/generictiff` — fixture-driven from the Grundium scan_619 + scan_620 files probed during v0.10:

- **R1** Single-level tiled TIFFs: drop `MinLevels` from 3 to 1. A file with one tiled IFD is a valid (zero-pyramid) tiled TIFF; the reader exposes it as a 1-level Image.
- **R2** Mixed-ratio pyramid chains: bump `LeftoverTiledMaxAreaRatio` from 1% to 5% so that 4-IFD chains with one orphan level (e.g., Grundium's 1×, 4×, 8×, 16× layout) are accepted. The orphan IFD becomes an AssociatedImage of kind `"associated"`.

These are not vendor-specific; they handle any encoder pattern that emits single-level or non-geometric-chain pyramids. Sealed in v0.10's `docs/deferred.md` §11 as v0.11 candidates.

### Not in scope

- New v0.11 multi-dim API surface. SCN reuses the v0.7 multi-dim API (TileCoord + SizeC + ChannelName + Image.SizeZ/SizeT) without additions.
- AOI-cropped `Tile()` variants. Tile coordinates remain in the slide-pixel grid; consumers rendering the slide handle inter-region empty space (we fill with white blanks, see §4.4).
- Z-stack support. SCN XML carries `spacingZ` and `<dimension z="N">` attributes, but our 3 fixtures have only z=0 data. Multi-Z deferred to a future fixture.
- Performance milestone. Existing v0.9 mmap + TileInto + WarmLevel apply automatically.

## 2. Fixtures

Three SCN files in `sample_files/scn/`, all from openslide-testdata, downloaded 2026-05-01:

| File | Bytes | `<image>` count | Role mix | Headline coverage |
|---|---:|---:|---|---|
| `Leica-1.scn` | 278 MB | 2 | 1 macro + 1 main | Single-region simple case; sanity check |
| `Leica-2.scn` | 2.1 GB | 5 | 1 macro + 4 mains | **Multi-region "discontinuous scanning"**; 4 disjoint tissue regions on one slide |
| `Leica-Fluorescence-1.scn` | 21 MB | 3 | 2 macros + 1 main (3-channel) | **Multi-channel fluorescence**; first real fixture exercising `Image.SizeC() > 1` |

**Permanent fixture limitation.** SCN scanner production stopped ~2015. Additional fixtures will be hard to find. Bio-formats has years of real SCN files in their slate; we lean on bio-formats CLI parity (§7) to cover the long tail beyond our 3-fixture coverage. **This limitation is documented prominently in `docs/formats/leicascn.md`.**

### Detection discriminator (sealed Q1)

BigTIFF + IFD 0's `ImageDescription` matches the SCN XML namespace:

```
<scn xmlns="http://www.leica-microsystems.com/scn/2010/10/01">
```

The schema URN is stable across all 3 fixtures (2011, 2012, 2014 production dates). Match is via substring search on the URN text (cheap, doesn't require XML parse to gate detection).

Detection priority: SCN registers BEFORE OME (both are BigTIFF + IFD 0 XML, but SCN's URN is unambiguous; OME's reader won't false-positive because its discriminator is the OME XML schema URN, not Leica's).

## 3. SCN file structure (canonical)

A SCN file is a BigTIFF where IFD 0's `ImageDescription` carries an XML document mapping every TIFF IFD to a logical role. Without the XML, the file is a pile of unlabeled IFDs.

### XML schema (relevant subset)

```xml
<scn xmlns="http://www.leica-microsystems.com/scn/2010/10/01">
  <collection sizeX="N" sizeY="N">                    <!-- slide physical extent in nm -->
    <image name="..." uuid="...">
      <pixels sizeX="W" sizeY="H">
        <dimension r="0" ifd="K"/>                    <!-- single-channel: r=level, ifd=K -->
        <dimension r="0" c="C" ifd="K"/>              <!-- multi-channel: r=level, c=channel -->
        ...
      </pixels>
      <view sizeX="N" sizeY="N" offsetX="N" offsetY="N"/>  <!-- slide-physical extent in nm -->
      <scanSettings>
        <objectiveSettings><objective>20</objective></objectiveSettings>
        <illuminationSettings>
          <illuminationSource>brightfield|fluorescence</illuminationSource>
        </illuminationSettings>
        <channelSettings>...</channelSettings>        <!-- present on multi-channel mains -->
      </scanSettings>
    </image>
    ...
  </collection>
</scn>
```

### Auxiliary vs main classification (Rule B, sealed Q2)

An `<image>` is **auxiliary** iff its `<view>` covers the entire `<collection>`:

```
isAuxiliary :=
  view.offsetX == 0 &&
  view.offsetY == 0 &&
  view.sizeX == collection.sizeX &&
  view.sizeY == collection.sizeY
```

Otherwise it's a **main scan**. This matches openslide (`is_macro` check at `openslide-vendor-leica.c:469`) and bio-formats. Magnification is metadata only; the role decision is geometric.

### Single slide, discontinuous scanning (sealed Q3)

A SCN file represents **one slide**, sampled discontinuously: the scanner only acquired pixel data for the rectangles containing tissue. Multiple main `<image>` elements are different rectangles of the same slide, *not* different slides. Inter-region slide area has no pixel data in the file (the scanner skipped it).

This is structurally different from SVS, which scans a single bounding rectangle covering all tissue (whitespace in the image is white pixels). SCN saves storage when the slide has sparse tissue.

## 4. Mapping SCN structure → opentile API

### 4.1. One Tiler.Image() composited from all main scans (sealed Q4)

`Tiler.Images()` returns **a single `Image`** representing the slide as a coherent canvas. Its level chain composites all main `<image>` pyramids into one coordinate space:

- **Image extent** = union bounding-rectangle of all main-scan `<view>` rectangles, expressed in baseline-pixel coordinates.
- **Level count** = the shared pyramid depth across all main scans (openslide enforces this; we mirror).
- **Per-level extent** = baseline_extent / 2^k, with the same nm-per-pixel as the corresponding level of any main scan (resolution similarity ±2% required across mains).

This mirrors openslide's choice (`openslide-vendor-leica.c:560+`). Consumers see one slide; we hide the multi-region sparsity behind the Level interface.

### 4.2. Required invariants for multi-main composition

Sealed Q5: composition fails (returns `ErrUnsupportedSCN` or similar) if any of these don't hold across main scans:

- Same pyramid depth (level count).
- Same illumination source (mixing brightfield + fluorescence not supported).
- Same objective magnification.
- Per-level resolution similarity within ±2% (matches openslide's tolerance).

These constraints land any v0.11 fixture cleanly. The first real-world violation triggers a fixture-driven design revisit.

### 4.3. Per-tile dispatch

Given `Level.Tile(x, y)` at level k:

1. Convert tile coord to slide-nm space: `nmX = x * tileW * level_nm_per_px`, `nmY = y * tileH * level_nm_per_px`.
2. Look up which main scan's `<view>` contains that point.
3. If a main scan covers it: compute the region-local tile coord and read from that region's IFD at level k. JPEG-table splice as needed (reuses v0.9's in-place splice template).
4. If no main scan covers it: return a synthesized blank tile (§4.4).

Lookup is O(N) over main scans (small N — ≤ 4 in our fixtures). A future bbox quad-tree lookup is YAGNI until N grows.

### 4.4. Inter-region tiles (sealed Q6) — synthesized blank fill

Tiles in the gap between main-scan `<view>` rectangles return a **synthesized white JPEG** of the level's tile size. Same pattern as `formats/philips`'s sparse-tile fill (`formats/philips/blank_tile.go`) and `formats/bif`'s `ScanWhitePoint` blank tiles.

**Owner directive (2026-05-06): "we need to protect the consumer from this detail."** The consumer's contract is "Tile(x, y) returns valid bytes for any (x, y) in the level grid." How we synthesize gap tiles is documented prominently in `docs/formats/leicascn.md` but is not a public-API surface.

Implementation: cache one blank tile per level (since tile size is uniform across a level). Reuses the existing blank-tile JPEG synthesis from Philips.

### 4.5. Multi-channel exposure (sealed Q7)

For main `<image>` elements with `<dimension c="N">` attributes (Leica-Fluorescence-1's main scan):

- `Image.SizeC()` = `max(c) + 1` (3 for our fixture).
- `Image.ChannelName(c)` returns the SCN XML's `<channel name="...">` value (e.g., `"405|Empty"`, `"L5|Empty"`, `"TX2|Empty"`).
- Per-channel tile access via `Level.TileAt(TileCoord{C: c, X: x, Y: y})`. Each channel's data lives in its own IFD; the XML `<dimension c="N" r="K" ifd="K">` mapping resolves which IFD.
- 2D-only `Tile(x, y)` is shorthand for `TileAt(TileCoord{X: x, Y: y})` — i.e., channel 0. Same convention as v0.7 multi-dim API on BIF.

### 4.6. Auxiliary `<image>` elements → AssociatedImages (sealed Q8)

Each auxiliary `<image>` (view==collection) becomes **one AssociatedImage at its highest-resolution IFD**. The auxiliary's own pyramid sub-levels are dropped — consumers don't typically need pyramidal labels/macros, and our existing `AssociatedImage` API is single-image.

**Kind assignment.** All SCN auxiliaries get `Kind() == "macro"`. SCN-specific metadata (illumination source, objective magnification, channel settings) is exposed via `leicascn.MetadataOf(tiler)` for consumers who need to disambiguate the brightfield-macro vs fluorescence-macro case (Leica-Fluorescence-1).

Per Q5, multiple macros in a single file are allowed (Leica-Fluorescence-1 has 2). They're surfaced in XML order — same order bio-formats reports them.

## 5. `formats/leicascn` package surface

### 5.1. Public types and functions

```go
package leicascn

// Factory implements opentile.FormatFactory.
type Factory struct{ opentile.RawUnsupported }
func New() *Factory
func (f *Factory) Format() opentile.Format             // opentile.FormatLeicaSCN
func (f *Factory) Supports(file *tiff.File) bool       // BigTIFF + SCN URN check
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error)

// Metadata is the SCN-specific slide metadata.
type Metadata struct {
    opentile.Metadata
    CollectionUUID string
    Barcode        string                  // base64-encoded slide barcode (may be empty)
    Auxiliaries    []AuxiliaryInfo         // per-auxiliary metadata (illumination, objective)
    Regions        []RegionInfo            // per-main-scan metadata (offset, view, objective)
    Channels       []ChannelInfo           // per-channel fluorescence metadata; nil for brightfield
}

type AuxiliaryInfo struct {
    Name              string
    IlluminationSource string  // "brightfield" or "fluorescence"
    Objective          float64
}

type RegionInfo struct {
    Name              string
    OffsetXNm, OffsetYNm uint64  // slide-physical offset of this region
    SizeXNm, SizeYNm     uint64  // slide-physical extent of this region
    Objective         float64
    IlluminationSource string
}

type ChannelInfo struct {
    Index            int
    Name             string         // "405|Empty"
    RGB              string         // "#0000ff"
    ExcitationFilter string         // "BP 405/60"
    SuppressionFilter string        // "470/50"
    DichromaticMirror string        // "455"
    ExposureTimeMicroseconds int64
    CCDGain          int
}

// MetadataOf returns the SCN-specific metadata if t is a SCN Tiler.
// Mirrors svs.MetadataOf / bif.MetadataOf / generictiff.MetadataOf.
func MetadataOf(t opentile.Tiler) (*Metadata, bool)
```

### 5.2. New `opentile.FormatLeicaSCN` Format constant

```go
// In tiler.go, alongside FormatSVS/NDPI/Philips/OME/BIF/IFE/GenericTIFF.
FormatLeicaSCN Format = "leica-scn"
```

The string value `"leica-scn"` mirrors the existing `formats/leicascn` package name. Future-proofs against any non-TIFF Leica formats (LIF, LMS) by including the format suffix.

### 5.3. Internal package layout

```
formats/leicascn/
  leicascn.go         Factory + Open
  scnxml.go           XML schema parser (handcrafted struct mapping)
  scnxml_test.go      golden-XML tests against committed fixture XML strings
  classify.go         auxiliary-vs-main + per-region/level mapping
  classify_test.go    classification unit tests against probed fixtures
  tiled.go            multi-region Level impl (with blank-tile fill)
  tiled_test.go       per-tile dispatch + blank-fill behavior
  associated.go       AssociatedImage impl (one per auxiliary)
  associated_test.go
  tiler.go            Tiler + Metadata + MetadataOf
  tiler_test.go
  blanktile.go        synthesized white JPEG for inter-region tiles
                      (or import formats/philips's existing blank-tile if reusable)
```

The XML parser is handcrafted (not encoding/xml struct-tagged) because (a) the schema is small and stable, and (b) handcrafting matches the `formats/bif/internal/bifxml/` convention used for Roche XML. Borrows the same XML walker pattern.

## 6. `formats/generictiff` relaxations

### 6.1. R1 — Single-level tiled TIFF support

Drop `MinLevels` from 3 to 1 in `internal/tiff.DefaultClassifyPyramidConfig()`. A 1-IFD tiled TIFF passes detection; the resulting Tiler has 1 Level with `PyramidIndex == 0`.

`tests/parity/generic_geometry_test.go` extended with a Grundium scan_619 fixture row (1 level, 43008×27136, 512×512 tiles, JPEG).

### 6.2. R2 — Mixed-ratio pyramid chains

Bump `LeftoverTiledMaxAreaRatio` from 0.01 to 0.05. The greedy chain still picks the longest geometric chain (Grundium scan_620's L0+L1+L3 at 4× ratio); the orphan L2 (1.56% of baseline) now passes the leftover area cap and surfaces as an AssociatedImage with `Kind() == "associated"` (the v0.10 fallback kind).

`tests/parity/generic_geometry_test.go` extended with a Grundium scan_620 fixture row.

### 6.3. Backward compatibility

Both relaxations are validator-cap loosenings — no v0.10 fixture changes detection or routing. CMU-1.tiff and CMU-1.stripped.tiff continue producing identical results.

The existing `internal/tiff/classify_pyramid_test.go` synthetic test cases that *deliberately* trip the old caps (e.g., 2-IFD inputs) will need their assertions updated. The v0.10 caps remain available as constants so consumers/tests can reach them if needed.

## 7. Bio-formats parity oracle

Per owner directive (2026-05-06: "lean on bioformats but also document the reality of this support where appropriate"), v0.11 ships a bio-formats parity oracle as the primary correctness bar (alongside our committed sample-tile SHA fixtures).

### 7.1. Oracle design

`tests/oracle/leicascn_bf_test.go` (build tag `bfparity`):

For each SCN fixture:

1. Run `/opt/bftools/showinf -nopix -no-upgrade <file>` and parse the output.
2. Confirm bio-formats's series count matches our IFD-to-image-pyramid mapping (modulo bio-formats' "thumbnail series" splitting — we collapse those into auxiliary single-images).
3. Confirm bio-formats's per-series Width / Height / SizeC matches our per-level Size / SizeC.

Per Q9, parity is structural-equivalence, not byte-equality. Tile byte-equality vs bio-formats is not feasible (bio-formats decodes + re-encodes differently from our raw passthrough).

### 7.2. Fixture-availability documentation

`docs/formats/leicascn.md` carries a prominent **Fixture limitation** section:

> SCN scanner production stopped ~2015. Our coverage is the 3 openslide-testdata fixtures (Leica-1, Leica-2, Leica-Fluorescence-1); additional fixtures are hard to come by. We supplement with bio-formats CLI parity to cover the long tail of SCN files that may exist in the wild but aren't in our slate. Real-world SCN files outside our coverage may exhibit edge cases that surface only when reported. Trigger-driven debugging from there.

## 8. Test fixtures

### 8.1. Sample-tile SHA fixtures (full-walk per file size)

- `tests/fixtures/Leica-1.scn.json` — full-walk on the main scan's 5 levels. ~280 MB file → JSON likely under the 5 MB cap; full-walk is feasible.
- `tests/fixtures/Leica-2.scn.json` — sampled-walk (file is 2.1 GB; per the existing `sampledByDefault` policy at >100 MB, BIF/Philips/Leica OME / IFE pattern).
- `tests/fixtures/Leica-Fluorescence-1.scn.json` — full-walk (only 21 MB). Each tile keyed by (level, channel, x, y) — extends `tests.TileKey` to include the C dimension if it doesn't already (BIF's multi-Z does this; we follow).

### 8.2. Geometry pinning

`tests/parity/leicascn_geometry_test.go` mirrors `bif_geometry_test.go` / `ife_geometry_test.go` / `generic_geometry_test.go`:

- Per-fixture: level count, per-level Size / TileSize / Grid / Compression.
- Per-fixture: AssociatedImage count, kinds, sizes.
- Per-fixture: `Image.SizeC()`, `ChannelName(c)`, `SizeZ`, `SizeT`.
- L0 (0,0) JPEG SOI marker check.
- `TestSCNOpenFileBackingsByteIdentical` — cross-backing parity (mmap vs pread).

### 8.3. Generictiff fixture additions

- `tests/parity/generic_geometry_test.go` extended with rows for `scan_619_grundium_pyramid_TIFF.tif` (1-level) and `scan_620_grundium_TIFF.tif` (4-level mixed-ratio chain).
- Sample-tile SHA fixtures for both Grundium files.

## 9. Detection-order sanity

Updated `formats/all/all.go` registration order:

1. SVS
2. NDPI
3. Philips
4. OME
5. BIF
6. IFE
7. **Leica SCN** (new — registers before generictiff)
8. Generic TIFF (catch-all, last)

Added invariant: vendor format detectors register before catch-all. Already followed; documenting the intent in the registration comment.

## 10. Sealed Q-decisions log

| ID | Question | Decision | Owner |
|---|---|---|---|
| Q1 | Detection discriminator | BigTIFF + IFD 0 ImageDescription contains schema URN `http://www.leica-microsystems.com/scn/2010/10/01` | Toby |
| Q2 | Auxiliary-vs-main classification | View extent matches collection extent (offsets 0,0 + dims match) → auxiliary | Toby (after openslide source review) |
| Q3 | Multi-region "what is this file?" framing | One slide, discontinuously sampled (not multiple slides) | Toby (correction issued) |
| Q4 | Multi-region API exposure | Single Image with multi-region levels (mirrors openslide) | Toby |
| Q5 | Multi-main composition invariants | Same depth + illumination + objective; ±2% per-level resolution similarity | Toby (mirrors openslide) |
| Q6 | Inter-region "gap" tile bytes | Synthesized white JPEG; consumer never sees the discontinuity | Toby ("protect the consumer from this detail") |
| Q7 | Multi-channel exposure | `Image.SizeC()` + `TileAt(TileCoord{C: c, X, Y})` via v0.7 multi-dim API | Toby |
| Q8 | Auxiliary `<image>` exposure | Each auxiliary → one AssociatedImage at highest-res IFD; Kind() = "macro"; SCN-specific metadata via `leicascn.MetadataOf` | Toby |
| Q9 | Bio-formats parity scope | Structural equivalence (series count, dims, channels), NOT byte-equality (bio-formats decode+re-encode differs) | Toby (confirmed during design) |

## 11. Active limitations introduced

Five new L items for `docs/deferred.md` §2:

- **L30** — SCN: multi-Z stack support deferred (no fixture in slate; XML carries `spacingZ` + `<dimension z="N">` but we have z=0 only). Fixture-driven.
- **L31** — SCN: AOI-cropped Tile variant deferred (consumer-stitching workaround documented). YAGNI.
- **L32** — SCN: regions with mismatched objectives or pyramid depth rejected via `ErrUnsupportedSCN`. Fixture-driven (haven't seen one).
- **L33** — SCN: byte-equality oracle vs bio-formats not feasible (decode+re-encode divergence). Permanent design choice.
- **L34** — SCN: 3-fixture coverage limit. SCN production discontinued ~2015; trigger-driven debugging for real-world SCN files outside our slate. Permanent.

## 12. Plan outline

15-task plan tentatively scoped across 5 batches; sealed in the v0.11 plan doc. Headline path:

- **Batch A** (3 tasks): probes + XML schema parser + auxiliary/main classifier (pure helpers, value-in/value-out, fixture-anchored).
- **Batch B** (3 tasks): Factory + Detection + tiler scaffolding (ErrUnsupportedSCN sentinel; placeholder Open).
- **Batch C** (4 tasks): single-region Level + multi-region composite Level + blank-tile fill + multi-channel `TileAt`.
- **Batch D** (2 tasks): generictiff R1 + R2 relaxations; Grundium fixture wiring.
- **Batch E** (3 tasks): integration + parity oracle + docs (`docs/formats/leicascn.md` + README + CHANGELOG + CLAUDE.md milestone bump).

Plan written separately at `docs/superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md`.
