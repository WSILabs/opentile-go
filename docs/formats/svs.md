# Aperio SVS

Aperio's scanned-slide format, produced by Leica Aperio scanners (most common digital pathology format in the United States as of 2026). File extension `.svs`.

## Format basics

- **TIFF dialect**: classic TIFF or BigTIFF; either is detected automatically.
- **Detection**: page 0 `ImageDescription` (tag 270) starts with `Aperio`.
- **Pyramid layout**: top-level IFDs. Page 0 is the base level; subsequent tiled pages are reduced levels until a non-tiled or `NewSubfileType`-flagged page begins the associated-image trailer.
- **Compression**: tiles can be JPEG (`Compression=7`) or JPEG 2000 (`Compression=33003` / `33005`, Aperio-specific values).
- **Metadata**: `ImageDescription` carries an Aperio software banner on line 1 followed by `|`-separated `key = value` pairs (`MPP`, `AppMag`, `Filename`, etc.).

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Tiled levels (JPEG) | ✅ | JPEGTables spliced + Adobe APP14 prepended for Aperio's RGB-not-YCbCr colourspace; matches Python opentile byte-for-byte |
| Tiled levels (JPEG 2000) | ✅ passthrough | We emit raw JP2K codestream bytes; downstream caller decodes. Decode/encode is parked at [#1](https://github.com/cornish/opentile-go/issues/1) |
| Associated label | ✅ | LZW-compressed strip page; multi-strip decode → raster restitch → re-encode as single LZW stream (L10 fix in v0.3) |
| Associated overview | ✅ | JPEG strip page; assembled via `internal/jpeg.ConcatenateScans` with restart-interval byte-equality vs Python |
| Associated thumbnail | ✅ | Same shape as overview |
| BigTIFF | ✅ since v0.2 (`scan_620_.svs`, `svs_40x_bigtiff.svs` exercise this) |
| Format-specific metadata | ✅ via `svs.MetadataOf(t)` — exposes MPP, SoftwareLine, Filename |

## Edge tile semantics

Tiles are stored at full `TileSize` regardless of position; right-edge and bottom-edge tiles include padding bytes in the unused region (the TIFF tile format stores them this way — opentile-go does not add the padding). The padding region's pixel content is encoder-specific (typically replicated edge pixels). opentile-go returns the bytes verbatim per the byte-passthrough invariant. Consumers should clip rendered output to the meaningful sub-rect:

```go
contentW := min(ts.W, sz.W - x*ts.W)
contentH := min(ts.H, sz.H - y*ts.H)
```

Matches upstream Python opentile (`get_tile()` is also byte-passthrough). SZI/DZI is the exception — its readers return border-sized tiles per spec; see `docs/formats/szi.md`.

## Recognized SVS writers

SVS is the WSI ad-hoc standard — the format originated with Aperio but is
now written by multiple vendors. opentile-go detects the writer from the
ImageDescription tag's first line and adjusts `ScannerManufacturer`,
`ScannerModel`, and Properties namespacing accordingly.

| Writer first-line marker | Detected `ScannerManufacturer` | Detected `ScannerModel` | Properties namespace | Status |
|---|---|---|---|---|
| `Aperio Image Library v...` | `Aperio` | empty | `aperio.<key>` | ✅ canonical; verified on `CMU-1-Small-Region.svs`, `CMU-1.svs`, `JP2K-33003-1.svs` |
| `Aperio Image, Grundium Ocus` | `Grundium` | `Ocus` | `grundium.<key>` | ✅ verified on `scan_620_.svs`, `svs_40x_bigtiff.svs` |
| `Aperio Image, <vendor> [<model>]` | `<vendor>` (first whitespace-separated word) | `<model>` (remainder) | `<vendor>.<key>` (lowercased) | best-effort; pattern extension when fixtures surface |
| Any other first-line pattern | empty | empty | `svs.<key>` (format-default fallback) | best-effort; standardized SVS keys (MPP, AppMag) still populate cross-format Metadata |

**Standardized vs. vendor-specific keys.** SVS-format-defined keys
(`MPP`, `AppMag`, `ScanScope ID`, `Filename`, `User`, `Date`, `Time`)
populate cross-format `Metadata` (MicronsPerPixel, Magnification,
ScannerSerial, etc.) regardless of writer. Vendor-specific extensions
land under the writer-namespaced Properties bucket.

**Why this matters:** pre-v0.18, every SVS got `ScannerManufacturer = "Aperio"`
even when the actual writer was Grundium. v0.18's per-writer detection
fixes this attribution bug. Future writers (3DHistech via SVS export;
others) follow the same pattern automatically — the fallback
namespace ensures parsing doesn't break for unrecognized writers.

## What's not supported

| Capability | Status | Why |
|---|---|---|
| Corrupt-edge tile reconstruct | ❌ deferred → [#1](https://github.com/cornish/opentile-go/issues/1) | None of our local SVS fixtures exhibits the bug. Upstream's reconstruct chain is ~12 tasks of new cgo + a Pillow BILINEAR port; speculation without a real triggering slide. Tile() returns `ErrCorruptTile` for `TileByteCounts[idx] == 0`. |
| JPEG 2000 decode/encode | ❌ deferred → [#1](https://github.com/cornish/opentile-go/issues/1) | Only consumer is the corrupt-edge reconstruct chain. Native JP2K passthrough (the v0.1+ behaviour) is unaffected. |

## Parity

**Byte-identical to Python opentile 0.20.0** on every sampled tile and every associated image, across our 5 fixtures (`CMU-1-Small-Region.svs`, `CMU-1.svs`, `JP2K-33003-1.svs`, `scan_620_.svs`, `svs_40x_bigtiff.svs`). Verified by `tests/oracle/parity_test.go`.

## Deviations from upstream

None. Behaviour matches Python opentile 0.20.0 exactly.

## Cross-format Metadata mapping (v0.17)

Aperio's `ImageDescription` carries `key = value` pairs (`MPP`, `AppMag`, `User`, etc.). v0.17 surfaces them on the cross-format `opentile.Metadata`:

| Aperio source | cross-format Metadata position |
|---|---|
| `MPP` | `MicronsPerPixelX/Y` (set both; `SetMPPSymmetric()` populates `MicronsPerPixel` since X == Y by construction) |
| `AppMag` | `Magnification` |
| ImageDescription verbatim | `ImageDescription` |
| `User` | `Properties[PropertyUserName]` (canonical) AND `Properties["aperio.User"]` (vendor passthrough) |
| every other Aperio kv | `Properties["aperio.<key>"]` (vendor passthrough — all keys passing the SVS reader's `isAperioKey` filter) |

`svs.MetadataOf(t)` continues to expose the format-specific `MPP`, `SoftwareLine`, and `Filename` fields; the cross-format additions are duplicates surfaced through the embedded `opentile.Metadata`.

## Implementation references

- Our package: `formats/svs/`
- Our metadata accessor: `svs.MetadataOf(opentile.Tiler) (*Metadata, bool)` exposing the embedded `opentile.Metadata` plus `MPP`, `SoftwareLine`, `Filename`.
- Upstream Python: [`opentile/formats/svs/`](https://github.com/imi-bigpicture/opentile/tree/main/opentile/formats/svs).
- Upstream tifffile detection: `tifffile.TiffPage.is_svs` (the `Aperio` prefix check).
- Aperio APP14 byte sequence ported verbatim from `opentile/jpeg/jpeg.py` (preserved as `internal/jpeg.adobeAPP14`).

## Known issues + history

- **L10** (closed v0.3): SVS LZW labels in multi-strip layout previously returned strip 0 only. Now decoded/restitched/re-encoded as a single LZW stream.
- **L18** (closed v0.3): `ConcatenateScans` rejected `ColorspaceFix=true` without JPEGTables; matches Python's gate now (skip splice + APP14 when tables absent — required for Grundium SVS).
- **L7 + L11** (closed v0.3): MCU size derived from SOF rather than hard-coded 16×16. Affected NDPI overview crop and SVS associated-image DRI; CMU-1-Small-Region.svs uses 4:4:4 (MCU 8×8) and tripped the hardcode.
- **L1** (closed v0.3): `SoftwareLine` had a trailing `\r` (CRLF parsing fix in `formats/svs/metadata.go`).

See [`docs/deferred.md`](../deferred.md) for the full reasoning + commit references.
