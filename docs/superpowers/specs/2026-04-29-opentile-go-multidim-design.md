# opentile-go Multi-Dimensional WSI Design Spec

**Status:** Draft, 2026-04-29. Cross-format infrastructure spec.
**Predecessors:** v0.1 – v0.7 (BIF in progress; v0.7 closeout
extends to include this work).
**Scope-grant:** v0.7 milestone, owner sign-off 2026-04-29 to grow
v0.7 from BIF-only to BIF + cross-format multi-dim Z-stack
abstractions. Driven by L21 (BIF volumetric Z-stacks) plus the
forward-looking goal of supporting OME multi-Z, multi-channel
fluorescence, and time-series WSI without re-shaping the public API
again.

**Decisions sealed 2026-04-29** (owner sign-off):

| § | Decision |
|---|----------|
| 3 | **Shape D — additive multi-dim addressing.** Keep `Tile(x, y)` for backward-compatible 2D access; add `TileAt(TileCoord)` taking explicit (X, Y, Z, C, T). Add `SizeZ()`, `SizeC()`, `SizeT()`, `ChannelName(c)`, `ZPlaneFocus(z)` to `Image`. Defaults are 2D-equivalent (Sizes = 1, names empty, focus 0). |
| 4 | **Z-axis indexing convention: storage-order with Z=0 = nominal.** Z indices are 0..SizeZ-1 in the file's storage order. Z=0 is always the nominal focal plane. The mapping from Z index to physical focal distance (microns) is exposed via `ZPlaneFocus(z) float64`. Consumers iterate by index and read focus distance from the accessor. |
| 5 | **Channel model: index + name only this milestone.** `SizeC()` reports channel count; `ChannelName(c)` returns the human-readable name (e.g., `"DAPI"`, `""` for brightfield). Per-channel color, excitation/emission wavelengths, fluorophore IDs are deferred to a follow-up — they're OME-specific richer metadata that no v0.7 format produces. |
| 6 | **Time series: dimension only, no per-T metadata.** `SizeT()` reports time-point count; T values are simple integer indices 0..SizeT-1. Pathology rarely uses time series; no use case justifies more API surface in v0.7. |
| 7 | **2D-format compatibility: zero behavioural change.** Every existing format (SVS, NDPI, Philips, OME, BIF when not Z-stacked, IFE when it lands) reports `SizeZ() = SizeC() = SizeT() = 1`. `TileAt({X, Y, Z:0, C:0, T:0})` is byte-identical to `Tile(X, Y)`. Existing test fixtures stay green. |
| 8 | **Iteration: 2D for v0.7.** `Level.Tiles(ctx)` continues to yield 2D positions only (Z=0, C=0, T=0). Multi-dim iteration is consumer-driven via nested loops over `SizeZ/C/T`. A future `Level.TilesND(ctx)` can be added when a real consumer needs it; YAGNI for now. |

---

## 1. One-paragraph scope

This spec defines the cross-format API for representing
multi-dimensional WSI data: focal-plane stacks (Z), fluorescence
channels (C), and time series (T). It is **format-agnostic** — every
supported format describes its own multi-dim semantics through the
same interface. The design is additive: existing 2D-only callers
keep working without code changes, and existing 2D-only format
implementations keep working without behavioural changes (each just
defaults `SizeZ/C/T` to 1). v0.7's BIF reader is the first
real consumer (`IMAGE_DEPTH` tag-driven Z-stack support); OME's
existing 2D-only path is verified to fit the new abstractions even
though OME multi-Z parsing itself stays deferred to a future
milestone. The new public API surface is small — one new struct
(`TileCoord`), one new `Level` method (`TileAt`), and five new
`Image` methods (`SizeZ`, `SizeC`, `SizeT`, `ChannelName`,
`ZPlaneFocus`).

## 2. Universal task contract

Same as v0.4 / v0.5 / v0.6 / v0.7: every implementation task in the
follow-up plan starts with `Step 0: Confirm upstream`. For
multi-dim, "upstream" means three layered references:

1. **The format spec** for whichever format we're implementing
   multi-dim support for (BIF whitepaper for v0.7 closeout; OME-XML
   schema docs when OME multi-Z is implemented).
2. **opentile-go's existing v0.6 `Image` interface** (introduced
   for multi-image OME) — the multi-dim additions extend it without
   breaking the v0.6 contract.
3. **Notes file** at `docs/superpowers/notes/2026-04-27-bif-research.md`
   §1 (BIF whitepaper digest, the IMAGE_DEPTH paragraph) for v0.7
   closeout.

---

## 3. The shape question — why Shape D

Four architectural shapes considered for multi-dim addressing.
Summarised here for context; D is the chosen shape.

### Shape A: each (Z, C, T) tuple is its own `Image`

```go
// Hypothetical
tiler.Images()  // [(pyramid 0, Z=0, C=0, T=0), (pyramid 0, Z=1, C=0, T=0), ...]
```

**Rejected.** `Image` already represents "logical sub-image"
(introduced in v0.6 for multi-image OME). Conflating logical
sub-image identity with axis position explodes the slice for a
multi-pyramid OME with Z-stacks (4 main pyramids × 5 Z planes ×
4 channels = 80 Images), which (a) is unwieldy in callsites that
iterate Images thinking of pyramid identity and (b) loses the
"Image == one pyramid" semantic v0.6 worked to establish.

### Shape B: each (level, Z, C, T) tuple is its own `Level`

```go
// Hypothetical
img.Levels()  // [(L0, Z0, C0, T0), (L0, Z1, C0, T0), ..., (L7, Z4, C3, T2)]
```

**Rejected.** Same explosion problem one level deeper. An 8-level
pyramid × 5 Z × 4 C × 3 T = 480 Levels per Image. Callers that
walk `img.Levels()` thinking "one entry per resolution step" break
catastrophically.

### Shape C: replace `Tile(x, y)` with `TileAt(coord)`

```go
// Hypothetical: breaking change
type Level interface {
    TileAt(coord TileCoord) ([]byte, error)  // replaces Tile(x, y)
}
```

**Rejected.** Forces every existing 2D caller to migrate. The whole
opentile-go consumer base would break with no transitional path.
Hard backward-compatibility cost for limited new value.

### Shape D: additive multi-dim addressing (CHOSEN)

```go
type Level interface {
    // ... existing methods unchanged
    Tile(x, y int) ([]byte, error)              // existing — for 2D access
    TileAt(coord TileCoord) ([]byte, error)     // NEW — for multi-dim access
}

type Image interface {
    // ... existing methods unchanged
    SizeZ() int                  // NEW; default 1
    SizeC() int                  // NEW; default 1
    SizeT() int                  // NEW; default 1
    ChannelName(c int) string    // NEW; default "" for non-fluorescence
    ZPlaneFocus(z int) float64   // NEW; microns from nominal; 0 for non-stacks
}
```

**Chosen.** Existing callers see no change. New callers opt into
multi-dim by reading `Image.SizeZ/C/T` and calling `TileAt`.
Dimension-default semantics (sizes = 1) mean 2D-only formats work
without overriding any of the new methods if they embed a base struct
with the defaults.

---

## 4. Public API additions

### `TileCoord` struct

```go
// Package opentile

// TileCoord identifies a tile by its position in the
// multi-dimensional WSI space. X and Y are the existing 2D grid
// position; Z, C, and T select among focal planes, fluorescence
// channels, and time points respectively.
//
// Z, C, T default to zero — a TileCoord literal {X: x, Y: y} addresses
// the same tile that Level.Tile(x, y) returns. Zero is the
// "nominal" / "first" / "T=0" plane in every dimension; non-zero
// values index into the higher-dimensional axes when the underlying
// Image carries them.
//
// The valid range for each axis is determined by the parent Image:
//   0 <= X < Level.Grid().W
//   0 <= Y < Level.Grid().H
//   0 <= Z < Image.SizeZ()
//   0 <= C < Image.SizeC()
//   0 <= T < Image.SizeT()
//
// Out-of-range values yield an *opentile.TileError wrapping
// ErrTileOutOfBounds (or ErrDimensionUnavailable when the named
// dimension doesn't exist on the underlying format).
type TileCoord struct {
    X, Y int
    Z    int
    C    int
    T    int
}
```

### `Level.TileAt`

```go
// Level interface gains:

// TileAt returns the raw compressed tile bytes at the given
// multi-dimensional coordinate. Tile(x, y) is shorthand for
// TileAt(TileCoord{X: x, Y: y}) — they are byte-identical for
// every format that doesn't carry Z/C/T axes.
//
// Sub-axis values out of range return *opentile.TileError. For
// 2D-only formats (SizeZ/C/T == 1), any non-zero Z, C, or T value
// is out of range.
TileAt(coord TileCoord) ([]byte, error)
```

### `Image.SizeZ`, `SizeC`, `SizeT`

```go
// Image interface gains:

// SizeZ returns the number of focal planes carried by this Image.
// Returns 1 for non-Z-stack slides (every existing 2D format,
// every BIF slide whose IMAGE_DEPTH tag is absent or 1, every
// 2D OME slide).
SizeZ() int

// SizeC returns the number of fluorescence/spectral channels.
// Returns 1 for brightfield slides (every existing 2D format,
// every BIF slide, OME files whose Pixels[@SizeC]=1).
SizeC() int

// SizeT returns the number of time points. Returns 1 for
// non-time-series slides (every existing format).
SizeT() int

// ChannelName returns the human-readable name of channel c —
// e.g., "DAPI", "FITC", "TRITC" for fluorescence; "" for
// brightfield slides where the single channel is implicit RGB.
//
// c must be in [0, SizeC()); panics with index-out-of-range
// otherwise (matching slice-access conventions).
ChannelName(c int) string

// ZPlaneFocus returns the focal distance (microns) of plane z
// from the nominal focal plane. ZPlaneFocus(0) is always 0
// (Z=0 is by convention the nominal plane). Negative values
// indicate planes below the nominal plane (near focus); positive
// values indicate planes above (far focus).
//
// z must be in [0, SizeZ()); panics with index-out-of-range
// otherwise.
ZPlaneFocus(z int) float64
```

### Backward-compatible base struct

To keep 2D-only format Image implementations from having to
implement five new methods, package `opentile` exposes a base
struct:

```go
// SingleImage already exists (introduced v0.6). Its method set
// expands to include the new dimension accessors with default
// 2D semantics:

func (s *SingleImage) SizeZ() int                  { return 1 }
func (s *SingleImage) SizeC() int                  { return 1 }
func (s *SingleImage) SizeT() int                  { return 1 }
func (s *SingleImage) ChannelName(c int) string    { return "" }
func (s *SingleImage) ZPlaneFocus(z int) float64   { return 0 }
```

Multi-dim format Image impls (BIF when stacked, future OME
multi-Z) override these with format-specific implementations.

---

## 5. Z-axis convention — storage-order, Z=0 = nominal

### The convention

Z indices are 0..SizeZ-1 in the **storage order** the file uses.
Z=0 is **always** the nominal focal plane. Mapping from Z index to
physical focal distance is the format's responsibility, exposed via
`Image.ZPlaneFocus(z) float64`.

### BIF realisation (the v0.7 use case)

Per the BIF whitepaper §"Whole slide imaging process":

> Volumetric scans including multiple image layers are stored in the
> BIF-file using the private tag IMAGE_DEPTH (0x80E5). The first M x N
> entries in the TILE_OFFSETS-tag (0x0144) correspond to the nominal
> focus layer 0, followed by M x N entries for each of the near
> focus layers, followed by M x N entries for each of the image
> tiles making up the far focus layers.

Storage order maps to Z index as:

| Z index | Plane | Storage block | Z-spacing offset (microns) |
|--------:|:------|:--------------|:--------------------------:|
| 0       | Nominal | bytes 0..M*N | 0 |
| 1       | Near focus -1 | bytes M*N..2*M*N | -1 × `Z-spacing` |
| 2       | Near focus -2 | bytes 2*M*N..3*M*N | -2 × `Z-spacing` |
| ... | ... | ... | ... |
| n_near  | Near focus -n_near | | -n_near × `Z-spacing` |
| n_near+1 | Far focus +1 | | +1 × `Z-spacing` |
| ... | ... | ... | ... |
| n_near+n_far | Far focus +n_far | | +n_far × `Z-spacing` |

Where `n_near = (Z-layers - 1) / 2` and `n_far = Z-layers - 1 - n_near`,
and the `<iScan>/@Z-spacing` attribute (microns per plane) drives
ZPlaneFocus's return values.

### OME realisation (forward verification only — no v0.7 work)

OME-XML's `<Pixels DimensionOrder="XYZCT">` (or `XYZTC`, `XYCZT`,
`XYCTZ`, `XYTZC`, `XYTCZ`) determines the IFD order. For OME's most
common ordering (XYZCT):

```
ifd_index = T * (SizeC * SizeZ) + C * SizeZ + Z
```

Each (Z, C, T) tuple gets its own top-level IFD. Z=0 corresponds to
the first IFD in the Z dimension, which OME conventionally treats
as the nominal/in-focus plane. This realisation is **verified to
fit the proposed API** but not implemented in v0.7 — OME multi-Z
support is a separate format-package work item.

### Why storage-order rather than signed-offset (-2, -1, 0, +1, +2)

Considered: indexing Z as a signed integer where Z=-2 is two near,
Z=+2 is two far. Rejected because:

- `[]Level` and `Tiles(ctx)` iteration semantics expect non-negative
  indexing.
- The user-facing semantic of "give me the first / next focal plane"
  is unaffected by the index convention.
- ZPlaneFocus already exposes the signed physical offset for callers
  that care about absolute focal position; the index is just a
  storage-order key.

---

## 6. Channel model

For v0.7, channel support is minimal:

- `SizeC() int` — count of separately-stored channels.
- `ChannelName(c int) string` — human-readable name, empty if
  unset.

Brightfield pathology slides report `SizeC() == 1` and
`ChannelName(0) == ""`. The single tile carries composite RGB; this
is the only channel a brightfield slide has, and consumers treat
the JPEG-decoded output as RGB without further channel-specific
processing.

Fluorescence slides (no v0.7 format produces them; future support)
would report `SizeC() > 1` and `ChannelName(c)` like `"DAPI"`,
`"FITC"`, `"TRITC"`. Each `Tile()` call returns a single channel's
grayscale image; consumers compose the multi-channel pixel
themselves.

### Deferred to a follow-up milestone

Per-channel rich metadata that OME-XML carries:

- `Color` (uint32 RGBA — visualisation hint for the channel)
- `ExcitationWavelength` (nm)
- `EmissionWavelength` (nm)
- `Fluor` (fluorophore ID)
- `IlluminationType` (Epifluorescence / Transmitted / Reflected /
  ...)

These would extend a `bif.MetadataOf`-style format-specific accessor,
not the public `Image` interface. Adding `Image.ChannelColor(c)`
etc. would be premature — none of v0.7's formats produce them, and
the OME format implementation that consumes them is its own
milestone.

### Why not a `Channel` struct?

Considered: a richer `Channel` struct on `Image`, returned by
`Channels() []Channel`. Each `Channel` carrying `Name`, `Index`,
`Color`, etc.

Rejected for this milestone — over-builds for the only consumer
(BIF, which has no channel metadata) and locks in a struct shape
before we know what fields OME's implementation will need. The flat
`SizeC()` + `ChannelName(c)` approach is forward-compatible: a
future `Image.Channels() []Channel` can be added additively when
OME multi-channel support lands.

---

## 7. Time-series convention

`SizeT() int` reports the count of time points. T values are simple
integer indices 0..SizeT-1.

No further metadata accessor is added. Pathology rarely captures
time series; the OME-TIFF format supports it but no v0.7 format
produces it, and no consumer has surfaced. If/when a real time-series
slide appears, a `TimePoint(t int) TimePointInfo` accessor with
`AcquisitionTime time.Time` and `DeltaT float64` (seconds from t=0)
can be added without disturbing the v0.7 API.

---

## 8. 2D-format compatibility

Every existing format reports `SizeZ() = SizeC() = SizeT() = 1`.
This is the default behaviour from `SingleImage`'s embedded
implementation, so no per-format override is required for SVS,
NDPI, Philips, OME (the existing 2D-only path), or IFE (when it
lands).

For these formats:

- `Tile(x, y)` returns the same bytes it does today.
- `TileAt(TileCoord{X: x, Y: y})` is byte-identical to `Tile(x, y)`.
- `TileAt(TileCoord{X: x, Y: y, Z: z, C: c, T: t})` with any non-zero
  Z, C, or T returns `*TileError` wrapping `ErrTileOutOfBounds` (or
  `ErrDimensionUnavailable` — see §11 open question).
- `ChannelName(0)` returns `""`.
- `ZPlaneFocus(0)` returns `0`.

Existing tests stay green. The v0.6 `TestSlideParity`-style fixture
hashes computed against `Tile(x, y)` continue to match without
regeneration.

### BIF as a special case (v0.7 multi-Z slide)

When a BIF slide carries `IMAGE_DEPTH > 1`:

- `Image.SizeZ()` returns the IMAGE_DEPTH value.
- `Image.ZPlaneFocus(z)` returns the signed micron offset based on
  `<iScan>/@Z-spacing`.
- `TileAt(TileCoord{X, Y, Z, ...})` reads from `offsets[Z*M*N +
  Y*M + X]` (with serpentine remap applied to (X, Y)).
- `Tile(x, y)` continues to return the nominal-plane tile (Z=0),
  matching v0.7 behaviour before this multi-dim work.

When IMAGE_DEPTH is absent or equal to 1 (every fixture in
`sample_files/ventana-bif/` so far), BIF behaves as a 2D-only
format — `SizeZ() == 1` and the multi-dim path is dormant.

---

## 9. Per-format applicability table

| Format | SizeZ | SizeC | SizeT | Notes |
|--------|------:|------:|------:|-------|
| **Aperio SVS** | 1 | 1 | 1 | 2D brightfield only |
| **Hamamatsu NDPI** | 1 | 1 | 1 | 2D brightfield only |
| **Philips TIFF** | 1 | 1 | 1 | 2D brightfield only |
| **OME-TIFF (current 2D path)** | 1 | 1 | 1 | OME-XML's SizeZ/C/T fields are read but not exposed via the public API in v0.7. SizeZ/C/T-aware OME parsing is a future format-package milestone |
| **Ventana BIF (DP 200, IMAGE_DEPTH=1)** | 1 | 1 | 1 | Both v0.7 fixtures have ImageDepth = 1 (not present, treated as 1) |
| **Ventana BIF (DP 200, volumetric)** | N (per IMAGE_DEPTH) | 1 | 1 | Z=0 nominal; ZPlaneFocus from `<iScan>/@Z-spacing` |
| **Iris IFE** (when it lands) | 1 | 1 | 1 | IFE 1.0 spec is 2D-only |

Forward-looking:

| Format | Hypothetical maximum support | Implementation cost |
|--------|------------------------------|---------------------|
| OME-TIFF multi-Z | SizeZ via `<Pixels SizeZ>` | medium — OME-XML parser extension + per-IFD addressing |
| OME-TIFF fluorescence | SizeC via `<Pixels SizeC>`; ChannelName via `<Channel Name>` | medium — same as Z + Channel-element parser |
| OME-TIFF time series | SizeT via `<Pixels SizeT>` | small — usually pyramid-incompatible (rare for WSI) |
| Sakura SVSlide | unknown until format reverse-engineered | depends on format |

The point of the table: every realistic future case fits the
proposed `SizeZ/C/T` + `ChannelName` + `ZPlaneFocus` API without
re-shaping. Richer metadata (channel color, fluorophore IDs) is an
additive extension when needed.

---

## 10. BIF v0.7 closeout — concrete implementation outline

L21 (Volumetric Z-stacks) is the BIF-specific landing of this
infrastructure. Phases:

### Phase α (interface evolution)

- Add `TileCoord` struct in `geometry.go` (alongside `TilePos`,
  `Size`, `SizeMm`).
- Add `TileAt` to `Level` interface in `image.go`.
- Add `SizeZ`, `SizeC`, `SizeT`, `ChannelName`, `ZPlaneFocus` to
  `Image` interface.
- Add default impls to `SingleImage` (returning 1 / "" / 0).
- Add `TileAt` impl to every existing concrete `Level` type:
  delegate to `Tile(coord.X, coord.Y)` and validate
  `coord.Z == coord.C == coord.T == 0`.
- Add `ErrDimensionUnavailable` error to `errors.go`.

### Phase β (BIF Z-stack support)

- Extend `internal/bifxml.IScan` to surface `Z-spacing` (already
  scrubbing it but storing for now-purpose).
- Extend `formats/bif/bif.go::Tiler` with multi-Z state (per-Image
  `ZPlaneFocus` table — pre-computed at Open time).
- Extend `formats/bif/level.go::levelImpl` with:
  - `imageDepth int` — captured from IFD's IMAGE_DEPTH tag.
  - `TileAt(coord)` — applies serpentine remap to (X, Y), then
    `offsets[coord.Z * (gridW * gridH) + serpIdx]`.
  - Bounds checking on `coord.Z` against `imageDepth`.
- Expose multi-Z metadata via the existing `bif.MetadataOf` —
  `Metadata.ZPlanesPositions []float64` (parallel to ZPlaneFocus).
- Synthetic BIF fixture writer in tests that produces multi-Z BIF
  bytes (we don't have a real volumetric fixture; this is the only
  way to test the path).

### Phase γ (OME forward-compatibility verification)

- Read OME-XML `<Pixels SizeZ>` / `SizeC` / `SizeT` even when
  current OME path uses only Z=C=T=0. Surface via
  `Image.SizeZ/C/T()`.
- Test: confirm Leica-1 / Leica-2 still report `SizeZ/C/T == 1`
  (their pixels are 2D RGB). No actual multi-Z OME fixture; this is
  pure contract verification.
- Document the implementation strategy for OME multi-Z (per-IFD
  addressing based on `DimensionOrder`) in `docs/formats/ometiff.md` —
  for the future implementer to pick up.

### Phase δ (tests + docs)

- Round-trip tests on every existing format: every 2D fixture
  reports SizeZ/C/T = 1; `Tile(x, y)` and `TileAt({X:x, Y:y})`
  byte-identical.
- Synthetic multi-Z BIF unit tests: 3-plane Z-stack, verify (a)
  SizeZ = 3, (b) ZPlaneFocus(0/1/2) = (0, -spacing, +spacing),
  (c) `TileAt(z=k, x, y)` reads from the correct offsets.
- `docs/deferred.md` §1a: register the multi-dim API addition as
  v0.7 deviation (cross-format infrastructure).
- `docs/formats/bif.md`: extend "What's supported" with multi-Z
  surfacing; "Active limitations" loses L21.
- CHANGELOG `[0.7.0]` Added section gains the multi-dim API and
  BIF Z-stack rows.

---

## 11. Open questions for sign-off

| § | Question | Provisional answer |
|---|----------|---------------------|
| 4 | `ErrTileOutOfBounds` vs new `ErrDimensionUnavailable` for non-zero Z/C/T on a 2D format? | **`ErrDimensionUnavailable`**. The semantic is "this dimension doesn't exist on this slide" — different from "this (X, Y) is past the grid edge." Cleaner debugging story. |
| 4 | Should `TileCoord` carry an `Index() int` helper that linearises to a single int (e.g., for use as a map key)? | **No.** Out of scope. Maps with `TileCoord` keys work fine in Go; we don't need a packed-int representation. Add later if a real consumer does. |
| 5 | Should we expose `ChannelColor(c)` even though no v0.7 format produces it? | **No.** Premature. Add when OME multi-channel support lands. |
| 7 | `SizeT > 1` with `Levels()` — should each time point get its own pyramid? | **No.** SizeT > 1 means the same pyramid is captured at multiple time points; `TileAt` selects by T, just like Z. Time-series-with-changing-pyramids is exotic; ignore until a real fixture surfaces. |
| 10 β | Multi-Z BIF synthetic fixture — embed in test code or commit to `tests/testdata/`? | **Embed in test code.** Mirrors the existing `formats/bif/detection_test.go::buildBIFLikeBigTIFF` pattern; keeps test inputs reviewable. |
| 10 γ | Should v0.7 closeout actually implement OME `SizeZ/C/T` reading from `<Pixels>`, or only verify the API shape? | **Read but only surface as `1` in v0.7** (since multi-Z OME parsing is its own task). The XML parser extracts the values; the Image impl reports them as written if OME content is multi-Z, else 1. **Actually — surface them honestly.** If `<Pixels SizeZ>` says 3, report `SizeZ() = 3`, but `TileAt(z=1, ...)` returns `ErrDimensionUnavailable` until OME multi-Z `Tile` reading is implemented. Half-supported is more honest than silent zero. |

---

## 12. Backward compatibility audit

The interface evolution is **purely additive**. Concrete impact on
existing call sites:

- **External consumers calling `Tile(x, y)`** — zero change. The
  method is unchanged.
- **External consumers calling `Tiler.Images()` / `Image.Levels()`
  / `Image.MPP()`** — zero change. None of these methods change.
- **External consumers writing custom `Level` impls** — must add
  `TileAt(TileCoord) ([]byte, error)` to satisfy the new interface.
  (Few such consumers exist outside this repo; documenting is
  enough.)
- **External consumers writing custom `Image` impls** — must add
  five new methods (or embed `SingleImage` to inherit defaults).
  Same low-population constraint as Level.
- **Existing format packages within this repo (SVS, NDPI, Philips,
  OME, BIF, IFE-when-it-lands)** — gain `TileAt` impls that delegate
  to `Tile(x, y)` for 2D semantics. BIF additionally implements
  the multi-Z path.
- **`TestSlideParity` fixture hashes** — unchanged, since they
  compute `sha256(Tile(x, y))` and that output is unchanged.

The only pre-existing test that needs updating is the v0.7 `Level`
interface satisfaction check in `image_test.go::fakeLevel` —
gains a `TileAt` impl that delegates to `Tile`.

---

## 13. Sign-off log

| Date | § | Decision | Owner |
|------|---|----------|-------|
| 2026-04-29 | 3 | Shape D — additive multi-dim addressing (`TileAt` + `SizeZ/C/T` + `ChannelName` + `ZPlaneFocus`) | Toby |
| 2026-04-29 | 4 | Z indexed 0..SizeZ-1 in storage order, Z=0 = nominal; physical offset via `ZPlaneFocus` | Toby |
| 2026-04-29 | 5 | Channel model: index + name only this milestone; richer metadata deferred | Toby |
| 2026-04-29 | 6 | Time series: dimension-only via SizeT; no per-T metadata this milestone | Toby |
| 2026-04-29 | 7 | 2D-format compat is zero behavioural change; embedded `SingleImage` defaults | Toby |
| 2026-04-29 | 8 | Iteration stays 2D in v0.7; multi-dim `TilesND` future work item | Toby |

After sign-off, this becomes the executable spec; a follow-up plan
doc (`docs/superpowers/plans/<date>-opentile-go-multidim.md`) lays
out the per-task batches detailed in §10 above.
