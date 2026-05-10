# opentile-go v0.17 — cross-format Metadata expansion (R20)

**Status:** sealed 2026-05-09.
**Work branch:** `feat/v0.17`.
**Headline:** Closes R20. Expands cross-format `opentile.Metadata` with the four most universally-meaningful fields (MicronsPerPixel + per-axis X/Y; ImageDescription) plus a `Properties map[string]string` for opentile-go-canonical extensions and vendor-namespaced passthrough. Mirrors OpenSlide's flat-property convention where it's standard; falls back to typed fields for the well-precedented WSI cross-cutting fields. Every format reader updated to populate the new fields. Driven by a real consumer (the user's viewer pipeline + a separate consumer that flagged the same gap).

## 1. Scope

### 1.1. Cross-format `opentile.Metadata` additions

Four new typed fields:

```go
type Metadata struct {
    // ... existing 6 fields (Magnification, ScannerManufacturer, ScannerModel,
    //                       ScannerSoftware, ScannerSerial, AcquisitionDateTime) ...

    // v0.17 additions:

    // MicronsPerPixel is populated when MicronsPerPixelX and
    // MicronsPerPixelY are both set and equal (strict ==). When the
    // format reports asymmetric pixel size, MicronsPerPixel is zero
    // and consumers should use the per-axis fields. Zero indicates
    // "unknown OR asymmetric" — check MicronsPerPixelX/Y to disambiguate.
    MicronsPerPixel  float64

    // MicronsPerPixelX / MicronsPerPixelY are the per-axis pixel
    // size in microns. Zero indicates "unknown" (the format didn't
    // report it).
    MicronsPerPixelX float64
    MicronsPerPixelY float64

    // ImageDescription is the structured per-format description
    // (e.g., SVS ImageDescription TIFF tag, OME-XML <Image Description>
    // attribute). Empty when the format has no equivalent. For free-
    // text user comments, see Properties["comments"].
    ImageDescription string

    // Properties is a flat key-value map for additional metadata
    // that doesn't fit the typed fields. Two key conventions:
    //
    //   - opentile-go-canonical keys (lowercase-with-hyphens):
    //     "case-number", "user-name", "scanned-area-mm2",
    //     "scan-duration-seconds", "comments". These are the
    //     opentile-go cross-format extensions; populated by format
    //     readers when their format exposes the equivalent.
    //
    //   - vendor-namespaced keys (vendor.<key>): vendor-specific
    //     fields surfaced as-is for consumer access. E.g.,
    //     "szi.vendor.SerialNumber", "aperio.AppMag".
    //
    // Missing keys mean the format didn't expose that field.
    // Numeric values are string-formatted floats (parseable via
    // strconv.ParseFloat).
    Properties       map[string]string
}
```

Constants for the canonical keys to avoid typo errors:

```go
// PropertyXxx are the canonical opentile-go cross-format keys for
// Metadata.Properties.
const (
    PropertyCaseNumber         = "case-number"
    PropertyUserName           = "user-name"
    PropertyScannedAreaMM2     = "scanned-area-mm2"
    PropertyScanDurationSec    = "scan-duration-seconds"
    PropertyComments           = "comments"
)
```

### 1.2. Per-format population

Each format reader's metadata parser populates the new typed fields + the canonical Properties keys where the source data is present:

| Format | MPP X/Y source | ImageDescription source | Other Properties keys populated |
|---|---|---|---|
| SVS | `MPP` field in ImageDescription tag | full ImageDescription tag | `aperio.X` for vendor-specific keys |
| NDPI | `XResolution` + `YResolution` (per Hamamatsu spec, in nm/pixel) | ImageDescription tag if present | `hamamatsu.X` for vendor-specific |
| Philips TIFF | `PIM_DP_X_RESOLUTION` + `PIM_DP_Y_RESOLUTION` | ImageDescription tag | `philips.X` for vendor-specific |
| OME-TIFF | `PhysicalSizeX` + `PhysicalSizeY` (with PhysicalSizeXUnit/Unit) | OME-XML `<Image Description>` | (probe for OME-XML extensions on demand) |
| BIF (Ventana) | `MPP` from XML metadata | `ImageDescription` if present | `ventana.X` for vendor-specific |
| IFE (Iris) | IFE-spec MPP fields | IFE-spec description field | (TBD per IFE spec; possibly `iris.X`) |
| Leica SCN | SCN-XML `<view>` scale per region | SCN-XML description | `leica.X` for vendor-specific |
| Generic TIFF | `XResolution + ResolutionUnit` derivation; wsi-tools fixtures use parsed mpp | `ImageDescription` tag verbatim | (no vendor namespace; generic) |
| SZI | `MicronsPerPixelX` + `MicronsPerPixelY` from `scan-properties.xml` | empty (SZI uses Comments instead; surfaced via `Properties["comments"]`) | `case-number`, `user-name`, `scanned-area-mm2`, `scan-duration-seconds` from spec-defined fields; `szi.vendor.X` for vendor.* custom props |

For each format, the reader probes the source to determine which keys to populate. Missing source data → key absent from `Properties` map (NOT empty string).

### 1.3. Format-specific Metadata struct cleanup (Option B)

Format-specific structs (e.g., `szi.Metadata`, `philipstiff.Metadata`, `ometiff.Metadata`) lose typed fields that are now duplicated in the embedded cross-format `opentile.Metadata`. They keep:

- Format-only fields (e.g., `szi.Metadata.Version`, `szi.Metadata.ScanJobName`, `szi.Metadata.CameraName`, `szi.Metadata.SensorPixelSize`) — no cross-format equivalent
- **Raw native representations** where the format-specific form is meaningfully different from the cross-format canonical form. Examples:
  - `szi.Metadata.ElapsedTime string` (raw "0h17m22s") stays; cross-format `Properties["scan-duration-seconds"]` parses it to a float-string
  - `szi.Metadata.VendorProperties map[string]string` (SZI's spec-defined `vendor.<key>` convention) stays; cross-format `Properties["szi.vendor.<key>"]` mirrors it (with the `szi.` namespace prefix)

This honors the SZI spec's "vendor.X" convention at the format-specific layer while the cross-format `Properties` map provides a unified flat-bag view for cross-format consumers.

Per-format struct cleanup details:

| Format struct | Fields removed (now via embedded opentile.Metadata) | Fields kept (format-specific or raw) |
|---|---|---|
| `szi.Metadata` | `MicronsPerPixel`, `MicronsPerPixelX`, `MicronsPerPixelY`, `Comments`, `UserName`, `CaseNumber`, `ScannedArea` | `Version`, `Date`, `SoftwareName`, `SoftwareVersion`, `TimeStart`, `TimeEnd`, `ElapsedTime` (raw string), `ScanJobName`, `ScannerSerialNo`, `CameraName`, `SensorPixelSize`, `ScanWidth`, `ScanHeight`, `VendorProperties` |
| `philipstiff.Metadata` | (audit during T3) | Format-specific |
| `ometiff.Metadata` | (audit during T4) | Format-specific |
| `bif.Metadata` | (audit during T5) | Format-specific |
| `ife.Metadata` | (audit during T5) | Format-specific |
| `leicascn.Metadata` | (audit during T6) | Format-specific |
| Other formats | Likely no format-specific Metadata struct (SVS/NDPI/generictiff use cross-format only) | n/a |

### 1.4. Consumer-facing API

No new accessor methods. Cross-format consumers continue to call `tlr.Metadata()` and read the new typed fields + the `Properties` map directly. Format-specific consumers continue to call `szi.MetadataOf(t)` etc.; the format-specific struct still embeds `opentile.Metadata`, so field promotion makes typed cross-format fields accessible via the format-specific struct.

```go
md := tlr.Metadata()
mppX := md.MicronsPerPixelX                                  // typed
mppY := md.MicronsPerPixelY                                  // typed
caseNum := md.Properties[opentile.PropertyCaseNumber]        // canonical key
operator := md.Properties[opentile.PropertyUserName]
vendorSerial := md.Properties["szi.vendor.SerialNumber"]     // vendor namespace
```

## 2. Out of scope

- **Changing the existing 6 typed fields.** Magnification, ScannerManufacturer, ScannerModel, ScannerSoftware, ScannerSerial, AcquisitionDateTime stay as-is. R20 is purely additive on the cross-format struct.
- **DZI bare format reader (R19).** Still parked.
- **Standard OpenSlide property keys** (e.g., `openslide.background-color`, `openslide.bounds-*`, `openslide.icc-size`, `openslide.quickhash-1`). opentile-go's Properties map uses opentile-go-canonical keys, not OpenSlide-prefixed keys. Consumers needing OpenSlide-compat surface translate at consumption time.
- **Removing format-specific MetadataOf accessors.** Each format keeps its `MetadataOf(t opentile.Tiler) (Metadata, bool)` accessor for SZI-specific / vendor-format-specific fields not surfaced cross-format.
- **Per-format docs reorganization.** Each format's existing doc gets a small update for the new cross-format mapping (T11), but no structural reorganization.
- **v1.0 cut.** Still pending.

## 3. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Cross-format scope | **Hybrid** (typed MPP X/Y + ImageDescription; Properties map for opentile-go-canonical extensions). Mirrors OpenSlide where standard; uses Properties for opentile-go originals. |
| Q2 | Smart MicronsPerPixel | **Yes:** populate plain MicronsPerPixel only when MicronsPerPixelX == MicronsPerPixelY (strict equality). Zero otherwise (consumer must consult per-axis). |
| Q3 | Properties map keys | **Lowercase-with-hyphens** for opentile-go-canonical (mirrors OpenSlide convention); **`vendor.<key>`** prefix for vendor-namespaced. Constants in `opentile` package for canonical keys. |
| Q4 | Format-specific struct cleanup | **Option B:** strip duplicates where canonically equivalent; keep raw/native representations where format semantics differ (e.g., SZI's `ElapsedTime` string stays alongside cross-format parsed-seconds). |
| Q5 | Properties value types | **Strings throughout.** Numeric values string-formatted via `strconv.FormatFloat`; consumers parse with `strconv.ParseFloat`. Mirrors OpenSlide's all-strings property bag. |
| Q6 | Missing-field semantics | **Key absent from map** (NOT empty string). Consumers use `v, ok := md.Properties[key]` for presence checking. |
| Q7 | Tagged version | **v0.17.0**. Pre-1.0; mostly additive on cross-format struct (no break for existing consumers); narrow break only for struct-literal construction of format-specific Metadata structs. |
| Q8 | Per-format MetadataOf accessor evolution | **Keep all** (`szi.MetadataOf`, `philipstiff.MetadataOf`, etc.). Consumers using format-specific accessors continue working — they get format-specific fields plus typed cross-format via embedded struct. |

## 4. Per-format probe + mapping audit

T1 lands the cross-format struct. T2-T9 (one task per format) each:
1. Probes the format reader's metadata source files (the actual fixture's tag values / XML elements / etc.)
2. Maps them to the new typed cross-format fields
3. Maps them to the canonical Properties keys
4. Adds vendor-namespaced Properties keys for non-canonical format-specific fields that consumers may want
5. Removes typed duplicates from the format-specific Metadata struct (Option B)
6. Updates the format reader's existing tests to reflect the new fields

Each format implementer reads the v0.16 SZI implementation (`formats/szi/metadata.go`) as the reference shape — it's the most recent and applies the v0.17 conventions cleanly.

## 5. Test strategy

- **Unit tests** on the new typed fields (zero-MPP-when-asymmetric semantic + Properties presence checks) for each format.
- **Cross-format parity test:** new `tests/parity/cross_format_metadata_test.go` that opens one fixture per format and confirms:
  - `Magnification > 0` for all formats that report it
  - `MicronsPerPixelX > 0 && MicronsPerPixelY > 0` for all formats that report MPP
  - `MicronsPerPixel > 0` only when X == Y
  - `ImageDescription` is non-empty for formats that have a TIFF ImageDescription tag (SVS, NDPI, Philips, etc.)
  - `Properties[PropertyCaseNumber]` populated where source data has a case number
- **Existing per-format Metadata tests** updated for the field-removal in format-specific structs.
- **TestSlideParity** unchanged — still 30 fixtures.

## 6. Active limitations introduced

None new. v0.17 is purely additive on cross-format Metadata + format-specific struct cleanup. No new L items.

## 7. Plan outline

7 tasks, single batch:

- **T1**: `opentile.Metadata` struct expansion (4 new typed fields + Properties map + canonical key constants); helper `setMPPSymmetric(md *Metadata)` + `parseDuration("0h17m22s") → seconds` utility; tests.
- **T2**: SVS + NDPI populate the new fields (TIFF-classic tag conventions; minimal format-specific Metadata struct work).
- **T3**: Philips TIFF + Generic TIFF populate (ImageDescription substring + wsi-tools parser already populates; mostly cleanup).
- **T4**: OME-TIFF populate (PhysicalSize XML + Image Description XML element).
- **T5**: BIF + IFE populate (XML / spec-defined fields).
- **T6**: Leica SCN populate (SCN-XML view scale).
- **T7**: SZI cleanup (move from format-specific to cross-format; preserve ElapsedTime + VendorProperties per Q4 Option B).
- **T8**: Cross-format parity test + per-format Metadata test updates + docs sweep + ship.

Wait — that's 8 not 7. Let me recount.

Adjusted to **8 tasks single batch**: T1 (struct), T2 (SVS+NDPI), T3 (Philips+Generic), T4 (OME), T5 (BIF+IFE), T6 (SCN), T7 (SZI cleanup), T8 (cross-format parity test + docs + ship).

Plan written separately at `docs/superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md`.

## 8. Verification gates

End-of-milestone:
- `go vet ./...` clean
- `gofmt -l .` clean (excluding sample_files, docs)
- `make test` green
- TestSlideParity 30 fixtures green
- New cross-format parity test exercises every format
- Per-format probe: opening one fixture per format reports `MicronsPerPixelX > 0 && MicronsPerPixelY > 0` (where format supports it); `ImageDescription` non-empty for SVS / NDPI / Philips / OME / Generic; `Properties[PropertyCaseNumber]` populated for SZI / OME (where source has it).

## 9. References

- OpenSlide standard property names: `https://github.com/openslide/openslide/blob/main/src/openslide.h` — `OPENSLIDE_PROPERTY_NAME_MPP_X`, `OPENSLIDE_PROPERTY_NAME_MPP_Y`, `OPENSLIDE_PROPERTY_NAME_OBJECTIVE_POWER`, `OPENSLIDE_PROPERTY_NAME_COMMENT`, etc.
- Python opentile base Metadata class: `https://github.com/imi-bigpicture/opentile/blob/main/opentile/metadata.py` — class with typed properties + `properties: Dict[str, Any]` for "other metadata".
- v0.16 R20 flag (T4 surfacing): `docs/deferred.md §11` row "R20 — opentile.Metadata: add MicronsPerPixel + ImageDescription".

## 10. Lessons feeding into v0.17 execution

- **v0.16 T2 import-cycle resolution:** when test code needs to import `formats/all` for factory registration, use external test package (`package szi_test`) to break the cycle.
- **v0.16 T3 plan-vs-code drift:** plan asserted method names; implementer caught 7 missing Level interface methods + 3 missing Image methods by reading the actual interface first. Continue this discipline — every task implementer should READ the relevant interface(s) before writing code.
- **v0.16 T4 cross-format gap discovery:** R20 itself was discovered during v0.16 T4; this kind of "wait, this should be cross-format" insight is worth catching mid-milestone and recording for follow-up rather than expanding scope on the fly.
- **v0.17-specific caveat:** every format reader's existing Metadata test will likely need a small update for the field-removal in format-specific structs. Plan for this; budget time per task accordingly.
