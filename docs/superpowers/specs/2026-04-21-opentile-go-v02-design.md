# opentile-go v0.2 — Design

**Status:** Draft
**Date:** 2026-04-21
**Builds on:** [v0.1 design](./2026-04-19-opentile-go-design.md)
**Upstream:** [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Copyright 2021–2024 Sectra AB)

## Purpose

v0.2 extends opentile-go with Hamamatsu NDPI support, BigTIFF parsing, and associated-image support (label, overview, thumbnail) for both SVS and NDPI. It introduces the first cgo dependency — libjpeg-turbo via its `tjTransform` API — scoped to a single internal package with a pure-Go fallback behind a build tag.

After v0.2, `opentile.OpenFile("slide.svs" or "slide.ndpi")` returns tile bytes for every pyramid level and every associated image of both Aperio SVS and Hamamatsu NDPI whole-slide TIFFs.

## What's new (relative to v0.1)

- **NDPI format support** — pyramid levels (striped and one-frame), label, overview (macro). Default output tile size 512 (auto-adjusted to a power-of-two multiple of native stripe width).
- **BigTIFF parsing** — classic TIFF (magic 42) and BigTIFF (magic 43) in a single internal TIFF layer. All format packages inherit BigTIFF automatically.
- **Associated images for both formats** — `Tiler.Associated()` returns non-nil. Label, overview, thumbnail (SVS only) expose the compressed bytes and a `Kind()` label. Pulls v0.1's deferred roadmap item R3 forward.
- **`internal/jpeg/`** — new, pure-Go, marker-level JPEG bitstream manipulation (no codec). Used by NDPI striped levels and SVS striped associated images.
- **`internal/jpegturbo/`** — new, cgo, single-function wrapper over `tjTransform` + `TJXOPT_CROP` + `TJXOPT_PERFECT`. Used by NDPI one-frame levels and NDPI label crop.
- **`nocgo` build tag** — preserves the v0.1 no-cgo invariant for consumers who only touch SVS. NDPI one-frame and NDPI label operations return `ErrCGORequired` under `nocgo`.
- **Python parity oracle** under `//go:build parity` — deferred from v0.1, shipped now. Invokes Python opentile in a subprocess and byte-compares tiles.
- **Public metadata accessor for NDPI** — `ndpi.MetadataOf(Tiler) (*Metadata, bool)` mirrors the v0.1 `svs.MetadataOf` pattern.

## Non-goals (v0.2)

- **NDPI >4GB files** that use Hamamatsu's proprietary 64-bit offset extension (not standard BigTIFF). Deferred pending an available test file; may reopen if the pending 6.5 GB sample requires it.
- **Aperio SVS corrupt-edge reconstruct fix** — still v1.0 per v0.1 scope. Requires JPEG re-encode, which neither `internal/jpeg` (marker-only) nor `internal/jpegturbo` (transform-only) supports.
- **Philips / 3DHistech / OME TIFF** — v0.4+ per the v0.1 roadmap.
- **Remote I/O backends** (S3, HTTP range, fsspec) — remain out of scope; consumers supply their own `io.ReaderAt`.
- **JPEG decode / encode to pixels** — the library returns compressed bitstreams only. Decoding is the caller's responsibility.
- **CLI** — library only.
- **Mandatory cgo** — `nocgo` remains supported. May flip to mandatory in a future release; the switch is small (remove three files).

## Licensing and attribution

Unchanged from v0.1. Apache 2.0, NOTICE credits the upstream, each NDPI format file carries the Sectra AB attribution where its logic mirrors upstream. No trademark ties.

## Approach

Direct port of Python opentile's NDPI code with idiomatic-Go adaptations, plus a thin cgo shim for the one operation that is both essential and impractical to re-derive in pure Go (MCU-aligned lossless JPEG crop).

- **Additive, minimal refactor** of the v0.1 codebase. No structural changes to `internal/tiff` beyond a parallel BigTIFF parse path; no breaking changes to the public API.
- **Pure-Go marker-level JPEG** in `internal/jpeg/`. Byte-level manipulation of SOI/DQT/DHT/SOF0/SOS/DRI/RST/EOI segments to assemble valid JPEGs from TIFF-embedded scan fragments. Does not decode or encode pixel data.
- **cgo JPEG crop** in `internal/jpegturbo/`. Single function (`Crop`) over `tjTransform` with `TJXOPT_CROP` and `TJXOPT_PERFECT`. Hidden behind build tags so the library still builds (with reduced capability) under `CGO_ENABLED=0` or `-tags nocgo`.
- **Format packages compose those two primitives** with TIFF-level page classification and metadata parsing.
- **No new Go-module dependencies**; `internal/jpegturbo` links libjpeg-turbo 2.1+ at build time via `pkg-config: libturbojpeg`.

## Module path and layout

Module path unchanged: `github.com/wsilabs/opentile-go`.

New directories and files (additions to the v0.1 tree):

```
opentile-go/
├── internal/tiff/              # EXTENDED
│   ├── bigheader.go            # NEW — parses BigTIFF header (magic 43, uint64 first-IFD)
│   ├── bigifd.go               # NEW — BigTIFF IFD walker (uint64 entry count, 20-byte entries, 8-byte inline)
│   ├── tag.go                  # EXTENDED — adds LONG8 (type 16), IFD (13), IFD8 (18)
│   ├── page.go                 # EXTENDED — uint64-aware internally; public accessors unchanged
│   └── header.go               # EXTENDED — dispatches to bigheader on magic 43
│
├── internal/jpeg/              # NEW, pure Go
│   ├── marker.go               # Marker constants, iterator-first Segment Scan
│   ├── segment.go              # byte-stuffed scan reader (ReadScan)
│   ├── header.go               # ReplaceSOFDimensions; MCU-align helpers
│   ├── concat.go               # ConcatenateScans (the hot NDPI operation)
│   ├── tables.go               # SplitJPEGTables (TIFF JPEGTables tag → DQT/DHT fragments)
│   └── sof.go                  # ParseSOF / BuildSOF
│
├── internal/jpegturbo/         # NEW, cgo
│   ├── turbo.go                # always compiled: Region type, ErrCGORequired
│   ├── turbo_cgo.go            # //go:build cgo && !nocgo
│   └── turbo_nocgo.go          # //go:build !cgo || nocgo
│
├── formats/svs/                # EXTENDED
│   ├── svs.go                  # Tiler.Associated() now populated from new associated.go
│   ├── associated.go           # NEW — striped label/overview/thumbnail via internal/jpeg
│   └── image.go                # unchanged
│
├── formats/ndpi/               # NEW
│   ├── ndpi.go                 # Factory, Supports, Open
│   ├── metadata.go             # ndpi.Metadata + parsing; MetadataOf
│   ├── striped.go              # NdpiStripedImage (pyramid levels, stripes → tile)
│   ├── oneframe.go             # NdpiOneFrameImage (pyramid levels needing crop)
│   ├── associated.go           # NdpiLabel, NdpiOverview
│   ├── tilesize.go             # AdjustTileSize(requested, stripeWidth)
│   └── *_test.go
│
├── formats/all/all.go          # +1 line: opentile.Register(ndpi.New())
│
├── tests/oracle/               # NEW, //go:build parity
│   ├── oracle.go               # Python subprocess runner
│   ├── oracle_runner.py        # Python harness importing opentile
│   ├── parity_test.go          # per-slide byte-compare tests
│   └── requirements.txt        # pinned opentile + PyTurboJPEG
│
└── tests/fixtures/             # fixture set grows
    ├── <existing SVS>.json     # regenerated with associated-image hashes
    ├── CMU-1.ndpi.json         # NEW
    └── OS-2.ndpi.json          # NEW (committed if under ~5 MB)
```

Rules that carry over unchanged from v0.1:

- `internal/tiff` and `internal/jpeg` are internal — shaped for opentile's needs, not published utilities.
- `internal/jpegturbo` is internal for the same reason — users who need libjpeg-turbo bindings should take a real dependency, not reach into ours.
- Format subpackages remain public.
- Registration is explicit; `formats/all` is the umbrella that blank-imports all known formats.

## Core types and public API

### Unchanged

`Tiler`, `Level`, `Metadata`, `AssociatedImage`, `TilePos`, `TileResult`, `Format`, `Compression`, `CorruptTilePolicy`, `Option`, `WithTileSize`, `WithCorruptTilePolicy`, `Open`, `OpenFile`, `Register`, `ErrUnsupportedFormat`, `ErrUnsupportedCompression`, `ErrTileOutOfBounds`, `ErrCorruptTile`, `ErrLevelOutOfRange`, `ErrInvalidTIFF`, `TileError`.

### Additions

```go
// Root package errors.go:
var (
    // Returned (wrapped in TileError) when internal/jpeg cannot parse a JPEG
    // bitstream or assemble a valid one from TIFF fragments.
    ErrBadJPEGBitstream = errors.New("opentile: invalid JPEG bitstream")

    // Returned when an operation requires an MCU-aligned region and the
    // computed or requested region is not. Primarily an internal invariant
    // guard; users encounter it only on malformed slides.
    ErrMCUAlignment = errors.New("opentile: operation requires MCU alignment")

    // Returned from NDPI one-frame levels and NDPI label on builds compiled
    // without cgo (CGO_ENABLED=0 or -tags nocgo). Re-exports the error from
    // internal/jpegturbo so consumers don't import the internal package.
    ErrCGORequired = errors.New("opentile: operation requires cgo build with libjpeg-turbo")

    // Reserved for future use; currently unfired because v0.2 defaults the
    // NDPI tile size to 512 rather than erroring. Predefined so a future
    // export is not a breaking change.
    ErrTileSizeRequired = errors.New("opentile: tile size not representable for this format")
)
```

### New format identifier

```go
// In opentile/tiler.go:
const FormatNDPI Format = "ndpi" // (was already defined in v0.1 for future use; now live)
```

### Format-specific metadata access

```go
// In formats/ndpi/metadata.go:

// Metadata is the NDPI-specific slide metadata, embedding opentile.Metadata
// for the common fields.
type Metadata struct {
    opentile.Metadata
    SourceLens  float64 // Hamamatsu SourceLens tag (objective magnification)
    FocalDepth  float64 // FocalDepth tag, mm
    FocalOffset float64 // ZOffsetFromSlideCenter tag, mm
    Reference   string  // scanner serial / reference
}

// MetadataOf returns the NDPI-specific metadata if t is an NDPI Tiler.
// Walks Tiler wrappers (matches svs.MetadataOf).
func MetadataOf(t opentile.Tiler) (*Metadata, bool)
```

## Concurrency contract

Unchanged from v0.1. `Tile(x, y)` and `TileReader(x, y)` remain safe to call concurrently from multiple goroutines, provided the underlying `io.ReaderAt` is. All internal caches are populated at Open time and immutable thereafter.

**cgo-specific concurrency notes:**

- `tjTransform` is re-entrant; each call creates a fresh transform handle via `tjInitTransform`, uses it, and destroys it in the same call. No shared handle or mutex in `internal/jpegturbo`.
- `libjpeg-turbo` itself is thread-safe at the call-site level for `tjTransform` when each goroutine uses its own handle, which our wrapper ensures by creating one per `Crop`.
- Allocation-side: each `Crop` call allocates a destination buffer via `tjAlloc` and frees it after copying to a Go slice. No Go pointers pass into C beyond the call lifetime.

## Data flow

### Open — unchanged at the top-level factory

`opentile.OpenFile(path)` → `tiff.Open(r, size)` → dispatch to first matching `FormatFactory.Supports()`. Only `Supports` logic changes (NDPI factory added).

### NDPI Open specifics

1. `ndpi.Factory.Supports(file)` checks page 0 for Hamamatsu's `65420` (SourceLens) vendor-private tag. Present ⇒ NDPI.
2. `ndpi.Factory.Open(file, cfg)`:
   - Resolve tile size: `cfg.TileSize()` if set (must be square); else default to `Size{512, 512}`. Compute smallest stripe width across tiled pages; call `AdjustTileSize(requested, stripe) → adjusted`. Every level uses the adjusted size.
   - Parse page 0 metadata into `ndpi.Metadata`. Scanner manufacturer `"Hamamatsu"`. Acquisition datetime parsed from TIFF `DateTime` (layout `"2006:01:02 15:04:05"`).
   - Iterate pages. Classify:
     - Page in the level series (series index 0) + tiled → `NdpiStripedImage`
     - Page in the level series + not tiled → `NdpiOneFrameImage`
     - Page with series name `"Macro"` → both `NdpiOverview` and (cropped left portion) `NdpiLabel`
     - Others (thumbnail, label-only pages) → append as AssociatedImage when present
   - Return `&tiler{levels, associated, metadata, icc}`.

### NDPI hot path — `NdpiStripedImage.Tile(x, y)`

1. Bounds-check `(x, y)` against `Grid()` → `ErrTileOutOfBounds` wrapped in `TileError`.
2. Compute native-stripe coverage:
   - `nx = adjustedTileW / nativeStripeW` (≥ 1 by construction)
   - `ny = adjustedTileH / nativeStripeH` (≥ 1, typically ≫ 1 since native stripes are 8 px tall)
3. For each of the `nx * ny` native stripes covering the output tile, `ReadAt(offsets[idx], byteCounts[idx])` returns the raw entropy-coded scan bytes.
4. `internal/jpeg.ConcatenateScans(fragments, ConcatOpts{Width: T, Height: T, JPEGTables: page.JPEGTables(), RestartInterval: stripeMCUs})` produces the full JPEG. `stripeMCUs` is the MCU count per native stripe — `(nativeStripeW / mcuW) * (nativeStripeH / mcuH)` — so one restart marker is emitted at each stripe boundary, resetting the DC predictor exactly where the fragments were split.
5. Return bytes (or `TileReader` returns a streaming reader over them).

### NDPI cgo path — `NdpiOneFrameImage.Tile(x, y)`

1. Read the entire page JPEG (one ReadAt; typically ~KB to low-MB for non-base levels).
2. If the page dimensions aren't MCU-aligned, `internal/jpeg.ReplaceSOFDimensions(buf, roundUpToMCU(pageW, mcuW), roundUpToMCU(pageH, mcuH))`. This is a ~12-byte header rewrite; the encoded coefficients are unchanged.
3. `internal/jpegturbo.Crop(paddedJPEG, Region{X: x*T, Y: y*T, Width: T, Height: T})`.
4. Return the cropped JPEG.

Under `nocgo`, step 3 returns `ErrCGORequired`; the wrapper surfaces it as `&TileError{..., Err: opentile.ErrCGORequired}`.

### NDPI associated images

- **Overview / macro**: raw page JPEG bytes, passthrough. Exposed via `Tiler.Associated()` with `Kind() == "overview"`.
- **Label**: cropped from the macro image. Crop region computed from upstream's `label_crop_position` convention (default 0.0–0.3 of macro width). Requires cgo. `Kind() == "label"`.

### SVS associated images (new to v0.2)

- SVS stores thumbnail, label, and overview as **striped JPEGs** alongside the tiled pyramid levels. Page classification in `SvsTiler` now routes these pages to new `SvsStripedAssociated` types instead of skipping them.
- Their `Bytes()` returns `internal/jpeg.ConcatenateScans(stripes, ConcatOpts{Width, Height, JPEGTables, ColorspaceFix: svsNeedsAPP14})`. No cgo required.
- `Tiler.Associated()` now returns a non-nil slice for SVS tilers. The Aperio associated-image compressions are always JPEG per upstream contract — the ColorspaceFix flag handles the APP14-lacking RGB photometric case.

### BigTIFF (transparent to format packages)

`internal/tiff.Open(r, size)`:
- Reads the first 4 bytes of the file. `II` or `MM` sets endianness; magic 42 → classic, magic 43 → BigTIFF.
- On BigTIFF, reads bytes 4–5 as offset size (must be 8) and bytes 6–7 as constant (must be 0). Reads bytes 8–15 as the first IFD offset (uint64).
- Calls `bigifd.walkIFDs(reader, firstOffset)` instead of `ifd.walkIFDs`. Returns the same `[]*ifd` shape; `Page` accessors are unchanged.
- `Entry.valueOrOffset` widens to `uint64`; `valueBytes` widens to `[8]byte` under the hood. Accessors like `Values(b) ([]uint32, error)` return the same types — if a BigTIFF value would overflow uint32, a future data type (`LONG8`) returns `[]uint64` via a new accessor. For v0.2, no format we target needs LONG8 arrays, so the existing surface covers us; LONG8 support lands incrementally as needed.

The BigTIFF path is fully parallel — the classic path has zero changes.

## Error handling

Carryover rules from v0.1:

- Sentinel errors at package root, `errors.Is`-checkable.
- `TileError` wrapping for per-tile errors with `(level, x, y)` context.
- Wrap underlying errors with `%w`.
- No panics from user input.

v0.2 additions:

- `ErrBadJPEGBitstream` — surfaced from `internal/jpeg` for marker-parse failures, malformed JPEGTables, scan-data boundary problems.
- `ErrMCUAlignment` — surfaced from `internal/jpegturbo` when `TJXOPT_PERFECT` rejects a non-aligned crop. Format packages avoid triggering this by computing MCU-aligned regions up-front; it's a defense-in-depth guard.
- `ErrCGORequired` — surfaced from `internal/jpegturbo` under `nocgo` and re-exported at the public error surface.

## Testing strategy

Three layers. The existing v0.1 test strategy (unit + fixture-backed integration) grows; the parity oracle is new.

### Unit tests (no testdata, always run)

- `internal/tiff/big_*_test.go` — hand-crafted BigTIFF byte slices covering magic 43, 8-byte offsets, LONG8 decode, uint64 IFD walk with cycle guard at the new data widths.
- `internal/jpeg/` tests — synthetic segment blobs; marker-scan round-trip; byte-stuffed scan preservation; SplitJPEGTables against a real-world TIFF JPEGTables blob (extracted once from SVS fixtures); ConcatenateScans against hand-built expected output; ReplaceSOFDimensions round-trip.
- `internal/jpegturbo/` tests — skipped under `nocgo`. Positive test: crop a known-good JPEG; decode the crop via stdlib `image/jpeg` (test-only; library code never imports it) and verify pixel dimensions match. Negative: malformed input errors; non-MCU-aligned crop with `TJXOPT_PERFECT` errors.
- `formats/ndpi/` tests — synthetic NDPI TIFFs built via a test helper analogous to the v0.1 SVS builder. Cover `Supports`, page classification, tile-size adjustment, stripe concat, one-frame passthrough with and without cgo.
- `formats/svs/associated_test.go` — new. Synthetic striped associated pages; verify the new path assembles a valid JPEG via `internal/jpeg`.

### Integration tests (real slides, opt-in)

Behavior unchanged from v0.1: `OPENTILE_TESTDIR`-gated; `TestSVSParity` renamed to `TestSlideParity` and iterates every slide with a committed fixture regardless of format.

- SVS fixtures **regenerate** once because the Metadata struct gained plumbing and Associated() output is now hashable.
- New: `CMU-1.ndpi.json`, `OS-2.ndpi.json` (committed if JSON under ~5 MB — log in `deferred.md` otherwise).
- Pending 6.5 GB NDPI sample: fixture generation attempted; outcome determines whether the file is classic+big offsets, BigTIFF, or Hamamatsu's 64-bit extension. Scope decision on 64-bit extension support happens at that point.
- BigTIFF SVS fixture if a sample is present.

### Parity oracle — NEW, `//go:build parity`

```
tests/oracle/
├── oracle.go              # runs `python3 -m tests.oracle_runner <slide> <level> <x> <y>`; captures stdout
├── oracle_runner.py       # imports opentile, emits tile bytes + sha256 to stdout
├── parity_test.go         # //go:build parity
└── requirements.txt       # pinned opentile, PyTurboJPEG, imagecodecs, tifffile
```

- `parity_test.go` iterates every committed fixture + every NDPI slide in `OPENTILE_TESTDIR`. For each slide, samples 10 tiles per level (configurable) from grid corners and interior. For each sample tile: invokes the Python runner, reads its bytes, byte-compares to our `Level.Tile(x,y)`. Byte-equal is the acceptance criterion.
- `-parity-full` flag walks every tile (slow for 24K-tile CMU-1 but possible).
- Not wired to default CI; triggered nightly or with a `parity` label.
- Documented in README as opt-in with Python prereqs.

### CI

- **Default CI:** unit tests + integration tests against cached slides. Fast, no Python.
- **`nocgo` CI:** `CGO_ENABLED=0 go test ./... -tags nocgo` verifies the pure-Go build compiles and NDPI one-frame/label tests skip/error correctly.
- **Parity CI:** scheduled or label-triggered; installs Python opentile and runs the `parity` tagged tests against a subset of slides.
- **Coverage targets unchanged:** ≥90% internal packages, ≥80% format packages, ≥75% top-level.

## Roadmap after v0.2

| Milestone | Scope |
|-----------|-------|
| **v0.3** | NDPI 64-bit offset extension (if needed). Additional real-world coverage gaps from `deferred.md`. |
| **v0.4** | Philips TIFF with sparse-tile filler. |
| **v0.5** | 3DHistech, OME TIFF (OME TIFF uses BigTIFF — zero additional TIFF work). |
| **v1.0** | Aperio SVS corrupt-edge reconstruct fix (requires JPEG re-encode path — evaluate pure-Go encoder vs. extending `internal/jpegturbo` to expose `tjCompress`). Consider making cgo mandatory. |

Performance profiling remains deferred; the v0.1 principle (parity first, perf after) carries forward.
