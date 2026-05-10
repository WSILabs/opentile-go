# OME-TIFF

The Open Microscopy Environment's TIFF dialect, written by Bio-Formats and most QuPath / OMERO / ImageJ exports. File extension `.ome.tiff` (or `.ome.tif`). Common in research microscopy and as an interchange format from clinical scanners.

## Format basics

- **TIFF dialect**: classic TIFF or BigTIFF (the latter is dominant for WSI).
- **Detection**: page 0 `ImageDescription`'s last 10 characters, after stripping trailing whitespace, end with `OME>` (i.e., the closing tag of the `<OME>` root element). Direct port of tifffile's `is_ome` predicate (`tifffile.py:10125-10129`).
- **Pyramid layout**: TIFF SubIFDs (tag 330) of the base page rather than top-level IFDs (the SVS / NDPI / Philips pattern). For each main-pyramid Image, the top-level IFD is L0; SubIFDs are L1..LN.
- **Multi-image**: a single OME-TIFF file can carry multiple main pyramids (e.g., `Leica-2.ome.tiff` carries 4). Bio-Formats writes them all; opentile-go exposes them all via `Tiler.Images()`.
- **Compression**: JPEG only in our fixtures (the spec allows others; we error on non-JPEG).
- **Metadata**: an OME-XML document in page 0's `ImageDescription`, namespace `http://www.openmicroscopy.org/Schemas/OME/2016-06`. Each `<Image Name="...">` has a `<Pixels>` child carrying `PhysicalSizeX/Y` (µm), `SizeX/Y`, and `Type` (uint8 only supported).

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Tiled pyramid levels | ✅ | Tile bytes are self-contained — OME doesn't carry shared JPEGTables on either of our fixtures, so no splice needed (verified per the v0.6 T5 audit) |
| OneFrame (non-tiled) levels | ✅ | Mixed tiled/non-tiled within a pyramid. L0/L1 typically tiled; L2+ typically OneFrame. Shared `internal/oneframe` package (factored from NDPI in v0.6) drives both formats |
| SubIFD-based pyramid traversal | ✅ via `internal/tiff.Page.SubIFDOffsets()` + `tiff.File.PageAtOffset()` (added in v0.6) |
| Multi-image files | ✅ | All main pyramids exposed via `Tiler.Images()`. Single-image files (Leica-1) return a one-element slice; multi-image files (Leica-2) return N |
| Associated macro / label / thumbnail | ✅ | Single-strip raw bytes (no splice on our fixtures); multi-strip planar pages take strip 0 only matching upstream |
| BigTIFF | ✅ (both fixtures are BigTIFF) |
| OME-XML metadata | ✅ via `ometiff.MetadataOf(t)` — exposes PhysicalSize per Image |

## What's not supported

| Capability | Status | Why |
|---|---|---|
| Non-uint8 pixel types | ❌ errored | Our fixtures all use uint8. Higher bit-depth and float pixel types are valid OME-XML but beyond opentile-go's tile-passthrough scope |
| Non-RGB photometric / non-JPEG compression | ❌ errored | The spec allows them; our fixtures don't exercise them |
| Per-image pyramid for macro/label | ❌ ignored | Macro pages have their own SubIFDs (the macro pyramid). We expose only macro L0 as the AssociatedImage, matching upstream |
| Multi-Z / multi-T / multi-C OME `TileAt(z != 0)` | ⚠️ deferred — half-supported (since v0.7 multi-dim) | `Image.SizeZ()/SizeC()/SizeT()` reflect the OME-XML's `<Pixels SizeZ/SizeT>` + `<Channel>` count (per the T2 gate outcome); but `Level.TileAt(coord{Z != 0, ...})` returns `ErrDimensionUnavailable` because the per-IFD addressing logic isn't wired yet. See "Active limitations" below. |

## Multi-dimensional reading (since v0.7)

opentile-go's v0.7 multi-dim closeout introduced cross-format
`Level.TileAt(TileCoord)` + `Image.SizeZ/SizeC/SizeT/ChannelName/
ZPlaneFocus`. OME's read-path implementation is partial:

- **`Image.SizeZ()`** reads `<Pixels SizeZ>` from the OME-XML — every Leica
  fixture reports `SizeZ() == 1` (no Z-stacks). A multi-Z OME slide
  would honestly surface `SizeZ() > 1` here.
- **`Image.SizeC()`** uses **`<Channel>` element count** as the
  discriminator (NOT `<Pixels SizeC>`). The latter describes
  per-pixel sample count (3 on RGB brightfield) — the wrong axis.
  Both Leica fixtures have `<Pixels SizeC=3>` but exactly one
  `<Channel>` element per `<Image>` → `SizeC() == 1` (single
  composite RGB channel per tile, the brightfield convention).
- **`Image.SizeT()`** reads `<Pixels SizeT>`; every Leica fixture is 1.
- **`Image.ChannelName(c)`** reads each `<Channel Name>` attribute.
  Every Leica fixture has empty channel names → `ChannelName(0) == ""`.
- **`Image.ZPlaneFocus(z)`** returns 0 for now (no `<Plane PositionZ>`
  parsing). When OME multi-Z reading lands, this hooks into the
  `<Plane>` element's PositionZ attribute.
- **`Level.TileAt(coord{Z, C, or T != 0})`** returns
  `ErrDimensionUnavailable` until the per-IFD reader is implemented.

### Future implementation strategy for multi-Z OME `TileAt`

A future format-package milestone implements the actual multi-Z
`TileAt` read path. The plan, sketched here so the future implementer
has a reference:

1. **Per-Image IFD ordering.** OME stores each (Z, C, T) plane as
   its own top-level IFD (or, for SubIFD-pyramid OMEs, as its own
   SubIFD). The IFD index for a given `(Z, C, T)` tuple is computed
   from the `<Pixels DimensionOrder>` attribute. The standard
   orderings are XYZCT / XYZTC / XYCZT / XYCTZ / XYTZC / XYTCZ; for
   the most common (XYZCT):

   ```
   ifdIndex = T * (SizeC * SizeZ) + C * SizeZ + Z
   ```

2. **Resolve via SubIFD chain.** For SubIFD-pyramid OMEs (the
   common case), the multi-Z planes live as SubIFDs of each
   pyramid IFD, indexed identically. For top-level-IFD OMEs, walk
   `tf.Pages()` directly.

3. **`Level.TileAt(coord)`** drops the current
   `ErrDimensionUnavailable` branch; instead computes the IFD index,
   resolves the page (existing `tiff.Page.SubIFDOffsets` plumbing
   from v0.6 makes this cheap), and reads the tile.

4. **`Image.ZPlaneFocus(z)`** parses each `<Plane PositionZ>` from
   the OME-XML and returns the relative offset from `Plane[0]`
   (or, with metadata work, from the in-focus plane).

Coverage: real multi-Z OME fixtures from Leica / Hamamatsu / 3DHistech
research scanners. None in our local set; gated on a real fixture
surfacing.

## Parity

**Two parity references**, since opentile-py's last-wins loop drops 3 of 4 main pyramids in `Leica-2.ome.tiff`:

1. **Python opentile 0.20.0** (post-splice) covers Leica-1 and Leica-2's last main pyramid + macro. Verified byte-identical via `tests/oracle/parity_test.go` (compares against `Tiler.Images()[len-1]` to match Python's exposure).
2. **tifffile** (raw tile bytes) covers every Image's tiled levels — including the 3 Leica-2 pyramids opentile-py drops. Verified byte-identical via `tests/oracle/tifffile_test.go`.

OneFrame levels of the dropped Leica-2 pyramids have no straight-byte Python reference (would require PyTurboJPEG pad-extend-crop replication). Coverage there is via integration-fixture SHA snapshots in `TestSlideParity` plus transitive correctness from the shared `internal/oneframe` package validated against NDPI.

## Deviations from upstream Python opentile

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Multi-image pyramid exposure | v0.6 | Use `Tiler.Levels()` instead of `Tiler.Images()` to see only the first | Upstream's base `Tiler.__init__` loop assumes one main pyramid per file and silently overwrites `_level_series_index` on each match. For Leica-2 (4 main pyramids), only the last is exposed — an upstream oversight, not intent. We expose all of them via the new `Image` API; legacy `Levels()` callers see Image 0 and don't break |
| Plane-0-only indexing on `PlanarConfiguration=2` | v0.6 | not opt-out-able | When OME pages use separate-plane storage (3 channels × grid entries in TileOffsets), Python opentile silently uses plane 0 only via flat `y*W + x` indexing. We mirror that for byte parity. The other planes are inaccessible through our public API |
| First-strip-only on multi-strip OneFrame | v0.6 | not opt-out-able | OME planar OneFrame pages can carry `rowsperstrip × samplesperpixel` strips (Leica-1 L2 has 7206). Python opentile's `_read_frame(0)` consumes only strip 0 (plane 0 row 0) and lets libjpeg-turbo's `TJERR_WARNING` recover from the truncated scan data. Our cgo wrapper distinguishes warning from fatal via `tjGetErrorCode` and matches Python's behaviour |

## Cross-format Metadata mapping (v0.17)

OME-TIFF carries metadata in OME-XML inside the page-0 `ImageDescription`. Pre-v0.17 the cross-format `Metadata()` returned an empty struct; v0.17 wires it from the parsed OME-XML:

| OME-XML element | cross-format Metadata position |
|---|---|
| `<Pixels PhysicalSizeX>` / `<Pixels PhysicalSizeY>` | `MicronsPerPixelX/Y`; `MicronsPerPixel` set when X == Y |
| `<Image Description>` element text | `ImageDescription` |
| `<AcquisitionDate>` element text | `AcquisitionDateTime` |
| `<Objective NominalMagnification>` | `Magnification` |
| `<Experimenter UserName>` (when present) | `Properties[PropertyUserName]` (Bio-Formats fixtures lack `<Experimenter>`, so absent today) |
| OME root `Creator` attribute | `Properties["ome.creator"]` |
| OME root `UUID` attribute | `Properties["ome.uuid"]` |

Fixture-JSON note: T4 added populated values to `Leica-1` / `Leica-2` fixture metadata snapshots (Magnification 0 → 20/40).

`ometiff.MetadataOf(t)` continues to expose the typed `OMEMetadata` accessor.

## Implementation references

- Our package: `formats/ometiff/`
- Public API: `Tiler.Images() []Image` + the `Image` interface (added in v0.6); the legacy `Tiler.Levels()` / `Level(i)` shortcut to `Images()[0]`.
- Our metadata accessor: `ometiff.MetadataOf(opentile.Tiler) (*OMEMetadata, bool)`.
- Shared OneFrame machinery: `internal/oneframe/`.
- Upstream Python: [`opentile/formats/ometiff/`](https://github.com/imi-bigpicture/opentile/tree/main/opentile/formats/ome).
- OME-XML schema reference: [openmicroscopy.org/Schemas/OME/2016-06](https://www.openmicroscopy.org/Schemas/OME/2016-06/).
- Bio-Formats (Java reference reader): [glencoesoftware/bioformats](https://github.com/ome/bioformats) — out of scope for direct comparison since it operates at decoded-pixel level.

## Known issues + history

- **PlanarConfiguration=2 indexing** (v0.6): tile-offset arrays carry plane_count × grid entries; relaxed our strict `len(offsets) == gx*gy` check to `>= gx*gy`.
- **Multi-strip OneFrame** (v0.6): added `oneframe.Options.FirstStripOnly` so OME can pass it without changing NDPI behaviour (NDPI still errors on multi-strip).
- **`tjTransform` warning vs fatal** (v0.6): cgo wrapper now treats `TJERR_WARNING` as success when `*dst` is populated. NDPI parity preserved.
- **Tile size for OneFrame**: hard-coded to base page's TileWidth/TileLength, ignoring `cfg.TileSize`. Mirrors upstream's `Size(self._base_page.tilewidth, self._base_page.tilelength)` in `OmeTiffTiler.get_level`.

See [`docs/deferred.md`](../deferred.md) for the full reasoning + commit references.
