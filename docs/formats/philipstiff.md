# Philips TIFF

Philips IntelliSite Pathology Solution scanner output. File extension `.tiff` (occasionally `.tif`). Common in European clinical pathology and increasingly in the US.

## Format basics

- **TIFF dialect**: classic TIFF or BigTIFF.
- **Detection**: page 0 `Software` (tag 305) starts with `Philips DP` AND `ImageDescription` ends in `</DataObject>` after stripping trailing whitespace. Both checks together; either alone has too high a false-positive rate.
- **Pyramid layout**: top-level IFDs, but on-disk `ImageWidth`/`ImageLength` for non-baseline levels are placeholders. The first DICOM_PIXEL_SPACING entry calibrates a baseline mm scale; each subsequent entry produces a corrected size for the next tiled page (so N PS entries yield N-1 corrected sizes — direct port of `tifffile._philips_load_pages`).
- **Compression**: JPEG only.
- **Metadata**: a DICOM-attribute XML document in `ImageDescription`, namespaced inside `<DataObject>`. Level-specific Attributes wrap inside `<PIM_DP_SCANNED_IMAGES><Array><DataObject>...` so the XML walker has to descend.

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Tiled pyramid levels | ✅ | JPEGTables spliced before SOS (no APP14 — Philips encodes standard YCbCr) |
| Sparse-tile blank-tile filling | ✅ | When `TileByteCounts[idx] == 0` (scanner-skipped background), Tile() returns a cached "blank tile" derived from the first valid frame via `internal/jpegturbo.FillFrame` (DCT-domain all-blocks luminance fill). Lazily computed once per level via `sync.Once` |
| BigTIFF | ✅ (`Philips-3.tiff` exercises this) |
| Per-level dimension correction | ✅ | `formats/philipstiff/dimensions.go` ports `tifffile._philips_load_pages` |
| Tile grid math | ✅ | Uses corrected dims (matches Python's `image_size.ceil_div(tile_size)`); on-disk pages may carry more tile entries than `gx*gy`, with trailing entries unused but preserved for index parity with NativeTiledTiffImage |
| Associated label / overview / thumbnail | ✅ | Single-strip JPEG passthrough with optional JPEGTables splice |
| Format-specific metadata | ✅ via `philipstiff.MetadataOf(t)` — exposes PixelSpacing, BitsAllocated, BitsStored, HighBit, PixelRepresentation, LossyImageCompressionMethod/Ratio |

## Edge tile semantics

Tiles are stored at full `TileSize` regardless of position; right-edge and bottom-edge tiles include padding bytes in the unused region (the TIFF tile format stores them this way). Sparse-tile-filled regions follow the same convention — the synthetic blank tile is also full `TileSize`. opentile-go returns the bytes verbatim per the byte-passthrough invariant. Consumers should clip rendered output to the meaningful sub-rect:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
```

Matches upstream Python opentile. SZI/DZI is the exception — its readers return border-sized tiles per spec; see `docs/formats/szi.md`.

## What's not supported

| Capability | Status | Why |
|---|---|---|
| Multi-strip associated images | ❌ errored | Our 4 fixtures are all single-strip; multi-strip would need ConcatenateScans-style assembly. Future-proofed as an explicit error rather than silent truncation |
| Non-JPEG compression | ❌ errored | All Philips slides we've seen use JPEG (Compression=7) |

## Parity

**Byte-identical to Python opentile 0.20.0** on every sampled tile and every associated image we expose, across our 4 fixtures (`Philips-{1,2,3,4}.tiff`). Verified by `tests/oracle/parity_test.go`.

## Deviations from upstream

None. Behaviour matches Python opentile 0.20.0 exactly.

## Cross-format Metadata mapping (v0.17)

Philips's DICOM-attribute XML carries the cross-format-canonical fields. v0.17 surfaces them as:

| DICOM/Philips XML attribute | cross-format Metadata position |
|---|---|
| `DICOM_PIXEL_SPACING` (mm/px × 1000 → µm) | `MicronsPerPixelX/Y` (per axis); `MicronsPerPixel` populated by `SetMPPSymmetric()` when X == Y |
| `Philips DP` `Software` tag | `ScannerManufacturer` (`Hamamatsu` or `PHILIPS` per scanner) |
| ImageDescription verbatim (full DataObject XML) | `ImageDescription` |
| `PIM_DP_SCANNER_OPERATOR_ID` | `Properties[PropertyUserName]` (when present; absent on all 4 current fixtures) |
| every other `<DataObject>` Attribute | `Properties["philips.<Name>"]` (vendor passthrough — `PIM_DP_*`, `DICOM_*`, `PIIM_*`, `UFS_*`) |
| `DICOM_SOFTWARE_VERSIONS` (raw, may be double-quoted) | `Metadata.Writer` (v0.20) |

`philipstiff.MetadataOf(t)` continues to expose the typed DICOM fields (`PixelSpacing`, `BitsAllocated`, etc.).

## Implementation references

- Our package: `formats/philipstiff/`
- Our metadata accessor: `philipstiff.MetadataOf(opentile.Tiler) (*Metadata, bool)`.
- Upstream Python: [`opentile/formats/philipstiff/`](https://github.com/imi-bigpicture/opentile/tree/main/opentile/formats/philips).
- The DICOM_PIXEL_SPACING dimension correction: `tifffile._philips_load_pages` (`tifffile.py:6477-6540`).
- `FillFrame` ports `Jpeg.fill_frame` from `opentile/jpeg/jpeg_filler.py:JpegFiller`; byte-deterministic per the v0.5 T2 gate.

## Known issues + history

- **`computeCorrectedSizes` algorithm** (v0.5 T6): the v0.5 plan's first draft mis-read the upstream loop (assumed N PS entries → N corrected sizes including baseline). Reading byte-by-byte, the actual algorithm is N → N-1 corrected sizes, with the first PS entry calibrating only.
- **Nested DICOM-XML walker** (v0.5 T9): synthesised-XML metadata tests passed under a flat `encoding/xml` schema, but real Philips fixtures wrap level-specific Attributes inside `PIM_DP_SCANNED_IMAGES > Array > DataObject`. Forced a rewrite to a stack-based token decoder mirroring `ElementTree.iter('Attribute')`.
- **Sparse-tile double-splice** (v0.5 T10): `NativeTiledTiffImage.get_tile` always splices JPEGTables onto whatever `_read_frame` returns, including the cached blank tile that already has tables inside (from `_create_blank_tile`'s pre-fill_frame splice). Result is duplicate DQT/DHT segments in the sparse-tile output — JPEG decoders accept this. Cross-check against Python at Philips-4 L0 (0,0) caught our initial single-splice version.

See [`docs/deferred.md`](../deferred.md) for the full reasoning + commit references.
