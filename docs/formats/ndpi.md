# Hamamatsu NDPI

Hamamatsu's NanoZoomer scanner output. File extension `.ndpi`. Common in pathology research and Asian/European clinical environments.

## Format basics

- **TIFF dialect**: classic TIFF magic (42), but with a Hamamatsu-specific 64-bit-offset extension that lets files exceed 4 GB without committing to BigTIFF magic. Detection sniffs tag 65420 (NDPI FileFormat) in the first IFD and dispatches a custom IFD walker.
- **Pyramid layout**: top-level IFDs. Pages alternate by `Magnification` (tag 65421, IEEE-754 single-precision); positive magnifications are pyramid levels, `Magnification == -1.0` is the overview, `-2.0` is the Map page.
- **Compression**: JPEG only.
- **Pyramid level shapes**: each level is a single big JPEG ("OneFrame") OR a striped JPEG with restart markers driving stripe boundaries (the McuStarts table at tag 65426 lists per-stripe RST offsets).

## What's supported

| Capability | Status | Notes |
|---|---|---|
| Striped pyramid levels | ✅ | Per-frame assembly from RST-bounded stripes; tile output via libjpeg-turbo MCU-aligned crop |
| OneFrame pyramid levels | ✅ | Single big JPEG per page, virtualised into tile cells via `internal/oneframe` (factored in v0.6 from `formats/ndpi/oneframe.go`) |
| Edge-tile out-of-bounds fill | ✅ | DCT-domain white fill via libjpeg-turbo CUSTOMFILTER, matching Python's `__need_fill_background` gate exactly (L12 fix in v0.4) |
| 64-bit offset extension | ✅ | Files >4 GB (`Hamamatsu-1.ndpi` exercises this) |
| Associated overview | ✅ | The `Magnification == -1.0` page; cropped + pixel-equivalent re-encode |
| Associated label (synthesised) | ✅ deviation | Cropped from the left 30% of overview — Go-side extension. Disable with `opentile.WithNDPISynthesizedLabel(false)` |
| Associated Map pages | ✅ deviation | The `Magnification == -2.0` page (single-channel grayscale uncompressed strip) surfaced as `AssociatedImage` with `Type() == "map"`. Go-side extension; Python opentile filters them out |
| Format-specific metadata | ✅ via `ndpi.MetadataOf(t)` — source-lens magnification, focal offset, scanner serial |

## What's not supported

None of our 3 local NDPI fixtures (`CMU-1.ndpi`, `OS-2.ndpi`, `Hamamatsu-1.ndpi`) hits any unsupported path. Documented limitations:

| Capability | Status |
|---|---|
| Multi-Z / multi-T NDPI files | ❌ — opentile-go targets 2D pathology slides only |

## Parity

**Byte-identical to Python opentile 0.20.0** on every sampled tile + every associated image (except synthesised label and Map page — see Deviations below), across our 3 fixtures. Verified by `tests/oracle/parity_test.go`.

## Deviations from upstream Python opentile

| Deviation | Since | Opt-out | Reason |
|---|---|---|---|
| Synthesised label cropped from overview | v0.2 | `opentile.WithNDPISynthesizedLabel(false)` | Upstream's `NdpiTiler.labels` returns empty — Python opentile does not surface NDPI labels at all. Aperio-style label affordance is more useful for downstream consumers. (L14 in `docs/deferred.md`) |
| Map pages exposed as `AssociatedImage` | v0.4 | not opt-out-able | tifffile already classifies `series.name == 'Map'`; surfacing matches what the underlying TIFF carries. Upstream chose not to. (R13 in `docs/deferred.md`) |

## Cross-format Metadata mapping (v0.17)

NDPI's pixel-size lives on the per-axis TIFF resolution tags — and on every fixture we have, X and Y differ by a few parts in 10⁴. v0.17 surfaces both axes faithfully and leaves `MicronsPerPixel` zero per Q2 (smart-MPP-only-when-X==Y):

| NDPI source | cross-format Metadata position |
|---|---|
| `XResolution` / `YResolution` + `ResolutionUnit` (centimeters → 10000/value µm/px) | `MicronsPerPixelX/Y`; `MicronsPerPixel` = 0 when asymmetric |
| `Magnification` tag (65421) on the level-0 page | `Magnification` |
| `SourceLens` tag (65421) | `Properties["hamamatsu.SourceLens"]` |
| `Reference` tag (65422) | `Properties["hamamatsu.Reference"]` (when present) |
| `FocalPlaneTolerance` (focal offset) | `Properties["hamamatsu.FocalOffsetMM"]` (when present) |
| — | NDPI has no `ImageDescription` tag and no operator/user field; `ImageDescription` and `Properties[PropertyUserName]` remain empty |

`ndpi.MetadataOf(t)` continues to expose the raw NDPI scanner / source-lens fields.

## Implementation references

- Our package: `formats/ndpi/`
- Our metadata accessor: `ndpi.MetadataOf(opentile.Tiler) (*Metadata, bool)`.
- Shared OneFrame machinery: `internal/oneframe/` (factored from NDPI in v0.6 to support OME-TIFF).
- Upstream Python: [`opentile/formats/ndpi/`](https://github.com/imi-bigpicture/opentile/tree/main/opentile/formats/ndpi).
- The `__need_fill_background` gate ported from PyTurboJPEG (`turbojpeg.py:839-863`); see `formats/ndpi/striped.go`.

## Known issues + history

- **L12** (closed v0.4): pre-v0.4 dispatched plain `Crop` first and silently returned mid-gray OOB fills (DC=0) on edge tiles where `Crop` succeeded despite extending past the image. Fix: dispatch geometry-first against image size, matching Python's gate exactly.
- **L17** (closed v0.4): pre-v0.4 floored label `cropH` to a whole-MCU multiple, dropping the last partial-MCU row. Now passes the full image height; `TJXOPT_PERFECT` accepts the partial last MCU row when the crop ends at the image edge.

See [`docs/deferred.md`](../deferred.md) for the full reasoning + commit references.
