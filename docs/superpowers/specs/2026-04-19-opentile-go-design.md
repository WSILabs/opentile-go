# opentile-go — Design

**Status:** Draft
**Date:** 2026-04-19
**Upstream:** [imi-bigpicture/opentile](https://github.com/imi-bigpicture/opentile) (Apache 2.0, Copyright 2021–2024 Sectra AB)

## Purpose

A pure-Go port of `opentile`, a Python library for reading tiles from whole-slide imaging (WSI) TIFF files used in digital pathology. The Go version aims for behavioral parity with upstream while adopting idiomatic Go conventions where doing so does not complicate cross-reference with the Python source during porting.

The core value opentile provides over generic TIFF readers is:

- **Lossless tile extraction** — tiles are returned as the raw compressed bitstreams (JPEG, JPEG 2000) that are stored in the TIFF, without decode/re-encode. For vendor formats whose on-disk tile layout does not match a rectangular grid (notably NDPI), opentile reconstructs rectangular tiles at the JPEG marker level (stripe concatenation, table rewriting) so the result is still lossless.
- **Unified metadata** — magnification, scanner identity, acquisition datetime, microns-per-pixel surfaced consistently across formats.
- **Format quirk handling** — Aperio SVS corrupt-edge detection; Philips sparse-tile filling; NDPI stripe-to-tile reshaping.

opentile does **not** provide region reads (`get_region`). For that, users compose opentile with a consumer such as `openslide`, `tiffslide`, or `wsidicomizer`. This scope is preserved in the Go port.

## Non-goals (v1)

- Associated images (label, overview, thumbnail) for any format. The Python library supports these via paths that re-encode with `imagecodecs` / `PyTurboJPEG`; deferring them keeps v1 fully codec-free.
- Aperio SVS corrupt-edge **fix** (the reconstruct-from-parent-level path). v1 **detects** corruption and returns `ErrCorruptTile`. Users opted in via `WithCorruptTilePolicy` can choose alternative behaviors once implemented.
- JPEG 2000 decode/encode. Native JP2K tiles in SVS work in v1 as byte passthrough; no JP2K path needs a codec.
- Philips / 3DHistech / OME TIFF support. v1 covers SVS; v1.1 adds NDPI; the remaining formats follow.
- Remote I/O backends (S3, HTTP range, fsspec equivalents). The core accepts `io.ReaderAt`; consumers supply their own backend. A convenience wrapper over `*os.File` is bundled.
- CLI. Library only in v1.

## Licensing and attribution

- Distributed under Apache 2.0.
- `NOTICE` file credits the upstream project, retaining the Sectra AB copyright notice for any files derived from or closely structurally following the upstream.
- Ported file headers acknowledge upstream provenance where the code mirrors upstream logic directly.

## Approach

Direct port of the Python library, with the following adaptations:

- Pure Go, no cgo. Viable because the core tile path never requires a raster codec — it is TIFF parsing plus (for NDPI in v0.2) JPEG marker manipulation. v0.1 is pure TIFF parsing; SVS tiles pass through unmodified.
- Structured as three layers: a narrowly-scoped internal TIFF reader, a narrowly-scoped internal JPEG marker package, and format-specific packages layered on top. Upstream's `tiler`/`tiff_image`/`formats/*`/`jpeg/*` layout is preserved, but TIFF IFD parsing and JPEG segment manipulation are each made explicit subpackages with independent test suites.

## Module path and layout

Module path: `github.com/wsilabs/opentile-go` (tentative — adjust at repo creation).

```
opentile-go/
├── go.mod
├── LICENSE                 # Apache 2.0
├── NOTICE                  # attribution to Sectra AB (imi-bigpicture/opentile)
├── README.md
│
├── opentile.go             # Open() factory, format sniff, OpenFile convenience
├── tiler.go                # Tiler interface + shared base
├── image.go                # Level / AssociatedImage types and helpers
├── metadata.go             # Metadata struct + format-specific extension type
├── geometry.go             # Point, Size, Region, SizeMm
├── compression.go          # Compression enum
├── errors.go               # sentinel errors + TileError wrapper
│
├── internal/tiff/          # minimal WSI-aware TIFF/BigTIFF reader
│   ├── reader.go           # IFD walker, endian, BigTIFF
│   ├── tag.go              # tag type decoders
│   └── page.go             # TiffPage: tile offsets/lengths, jpegtables, compression
│
├── internal/jpeg/          # pure-Go JPEG marker/segment work (v0.2 with NDPI)
│   └── ...                 # deferred in v0.1; not required for SVS pass-through
│
├── formats/
│   ├── svs/
│   │   ├── svs.go          # SvsTiler (supports() + New)
│   │   ├── image.go        # SvsTiledImage (passthrough), stubs for associated
│   │   └── metadata.go     # ImageDescription parser (mpp, scanner, datetime)
│   └── ndpi/               # v1.1
│
├── testdata/               # small synthetic fixtures only (no large slides)
└── tests/
    ├── fixtures/           # generated: sha256 hashes per (slide, level, x, y)
    ├── download/           # downloader for openslide public testdata
    └── oracle/             # //go:build parity — Python oracle harness
```

`internal/tiff` is internal because its API is shaped for opentile's needs — raw compressed tile byte access, WSI vendor tag support — rather than as a general-purpose TIFF library. `internal/jpeg` (deferred to v0.2) will be internal for the same reason: a port-support utility, not a published API.

Format subpackages are public so that consumers can import only what they need. The top-level `opentile.Open` discovers registered format tilers; registration is explicit (no `init()` side-effects) to keep binary size predictable. A convenience umbrella package `opentile/formats/all` registers every known format for users who want the kitchen sink.

## Core types and interfaces

```go
// Top-level factory.
func Open(r io.ReaderAt, size int64, opts ...Option) (Tiler, error)
func OpenFile(path string, opts ...Option) (Tiler, error)  // wraps *os.File

type Option func(*config)

// WithTileSize requests output tile dimensions. If unset, the format default is
// used (SVS: native tile size; NDPI: required — no default).
func WithTileSize(w, h int) Option

// WithCorruptTilePolicy controls how SVS corrupt-edge tiles are reported.
// v1 supports only CorruptTileError (default).
func WithCorruptTilePolicy(p CorruptTilePolicy) Option

type CorruptTilePolicy uint8
const (
    CorruptTileError CorruptTilePolicy = iota // return ErrCorruptTile (default, v1)
    CorruptTileBlank                          // return a typed blank tile (v1.1)
    CorruptTileFix                            // reconstruct from parent level (v1.0+)
)

type Tiler interface {
    Format()      Format              // "svs", "ndpi", ...
    Levels()      []Level
    Level(i int) (Level, error)
    Associated()  []AssociatedImage   // v1: empty slice
    Metadata()    Metadata
    ICCProfile()  []byte              // nil if absent
    Close()       error
}

type Level interface {
    Index()         int
    PyramidIndex()  int               // log2 scale from base
    Size()          Size              // pixels, this level
    TileSize()      Size              // pixels per tile
    Grid()          Size              // tile count in x, y
    Compression()   Compression
    MPP()           SizeMm            // microns per pixel
    FocalPlane()    float64

    // Raw compressed tile bytes (JPEG or JP2K codestream as stored in TIFF,
    // possibly rewritten for lossless header fixup).
    Tile(x, y int) ([]byte, error)

    // Streaming variant for large tiles / memory-sensitive consumers.
    TileReader(x, y int) (io.ReadCloser, error)

    // Serial row-major iteration over every tile position.
    Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult]
}

type TilePos    struct{ X, Y int }
type TileResult struct {
    Bytes []byte
    Err   error
}

type Compression uint8
const (
    CompressionUnknown Compression = iota // zero value; unset or unrecognized
    CompressionNone
    CompressionJPEG
    CompressionJP2K
)

type Format string
const (
    FormatSVS  Format = "svs"
    FormatNDPI Format = "ndpi"
)

type Metadata struct {
    Magnification       float64    // 0 if unknown
    ScannerManufacturer string
    ScannerModel        string
    ScannerSoftware     []string
    ScannerSerial       string
    AcquisitionDateTime time.Time  // zero if unknown
}
```

Format-specific metadata (e.g., `svs.Metadata` with Aperio-proprietary fields) is exposed via a per-format accessor function (`svs.MetadataOf(tiler)`). The common `Metadata` struct carries the cross-format fields and is returned by `Tiler.Metadata()`; format packages embed it in their own metadata struct and provide a typed `XxxOf(Tiler) (*XxxMetadata, bool)` accessor so consumers can import the format package directly when they need the extras. This avoids making `Metadata` an interface (and losing ergonomic field access) while still making format-specific data reachable from an `opentile.Tiler` handle.

### Design notes on the public surface

- `Tile(x, y int)` takes positional ints rather than a `TilePos` value — ergonomic for the common call. `TilePos` exists on the iterator return side.
- `TileReader` is a separate method from `Tile` rather than a union return. Consumers pick at call site; no runtime polymorphism cost.
- `io.ReaderAt` + `int64` size is the core input. This mirrors `os.File` semantics and is the natural shape for BigTIFF readers that need arbitrary-offset access. It is also the Go-idiomatic concurrency-safe input: the `io.ReaderAt` contract explicitly permits parallel calls.
- No `context.Context` on `Tile(x, y)`. Callers that want cancellation run the call on a goroutine and select on their own context. A per-call context threaded through every tile read would be noise that does not pay off until we have a real cancellation story.

## Concurrency contract

`Level.Tile(x, y)` and `Level.TileReader(x, y)` are **safe to call concurrently** from multiple goroutines — on the same or different `Level` — provided the underlying `io.ReaderAt` is safe for concurrent use. The stdlib `*os.File` satisfies this.

All internal caches (parsed IFDs, per-page jpegtables blobs, per-tile `(offset, length)` arrays, metadata) are populated at `Open()` time and then immutable. There is no lazy mutation on the hot path and no mutex is taken on tile reads.

`Tiler.Close()` must not race with in-flight tile reads. Consumers drain before closing.

This design leaves room for a future `Level.TilesParallel(ctx, workers int) iter.Seq2[...]` without breaking changes; adding it is additive. v1 does not ship it (YAGNI), but the lock-free hot path ensures it will work trivially when added.

## Data flow

### `Open()`

1. Caller provides `io.ReaderAt` + size, or a path via `OpenFile`.
2. `internal/tiff` parses the TIFF header (`II`/`MM` byte order, magic 42 for classic TIFF, 43 for BigTIFF). IFDs are walked eagerly; every `TiffPage` is cached.
3. The factory iterates registered format tilers calling `Supports(*tiff.File) bool`. First match wins.
   - SVS: ImageDescription on the first IFD begins with `"Aperio"`.
   - NDPI: presence of a vendor-private tag in Hamamatsu's range (v1.1).
4. The matching format tiler constructs:
   - classifies IFDs into `Level | Thumbnail | Label | Overview` via series heuristics matching upstream's `_is_level_series` etc.
   - extracts format-specific metadata (`ImageDescription` parse, OME-XML if present, etc.)
   - reads ICC profile from `InterColorProfile` tag if present
   - attaches per-page `jpegtables`
5. Returns an immutable `Tiler`.

### `Level.Tile(x, y)` — SVS tiled level (v1 hot path)

1. Bounds check `(x, y)` against `Grid()` → `ErrTileOutOfBounds` on failure.
2. Compute linear index `idx = y * grid.W + x`.
3. Lookup `offset, length := page.TileOffsets[idx], page.TileByteCounts[idx]`.
4. Corruption heuristic: `length == 0` or (for non-base pyramid levels at the right/bottom edge) the upstream SVS corrupt-tile detector. If corrupt → `ErrCorruptTile` per `WithCorruptTilePolicy`.
5. Allocate `buf := make([]byte, length)`; call `r.ReadAt(buf, offset)`; return `buf`.

That is the entire SVS hot path — no JPEG manipulation, no codec. Works equivalently for `COMPRESSION.JPEG` and `COMPRESSION.APERIO_JP2000_RGB`.

### `Level.Tile(x, y)` — NDPI (v1.1)

1. Same bounds check.
2. Determine which native stripes cover the requested output tile (stripes are typically 8 pixels tall in NDPI; a tile spans many stripes).
3. Read each stripe's raw JPEG scan bytes via `ReadAt`.
4. `internal/jpeg.ConcatenateScans(scans, jpegtables, colorspaceFix)` stitches the scans into a single JPEG bitstream with a correct header (merged DQT/DHT, SOF matching the tile geometry, restart markers renumbered if needed).
5. If the stripe concatenation exceeds the requested tile width, perform a lossless JPEG crop (boundary-aligned to MCU; NDPI tile sizes are constrained to MCU multiples — this is an upstream precondition preserved here).
6. Return the synthesized JPEG bytes.

### `Level.TileReader(x, y)`

For passthrough paths (SVS tiled), returns an `io.SectionReader` over the underlying `io.ReaderAt`. Zero-copy, independent seek state per reader.

For synthesized paths (NDPI), constructs the JPEG bytes in a buffer and returns a `*bytes.Reader` wrapped in `io.NopCloser`.

### `Level.Tiles(ctx)`

Serial row-major iteration. Yields `(TilePos{x,y}, TileResult{bytes, err})`. Checks `ctx.Err()` before each yield and exits early on cancellation. Does not fan out — callers parallelize at the `Tile(x,y)` level when needed.

## Error handling

Sentinel errors are exported at package root:

```go
var (
    ErrUnsupportedFormat      = errors.New("opentile: unsupported format")
    ErrUnsupportedCompression = errors.New("opentile: unsupported compression")
    ErrTileOutOfBounds        = errors.New("opentile: tile position out of bounds")
    ErrCorruptTile            = errors.New("opentile: corrupt tile")
    ErrLevelOutOfRange        = errors.New("opentile: level index out of range")
    ErrInvalidTIFF            = errors.New("opentile: invalid TIFF structure")
)

type TileError struct {
    Level int
    X, Y  int
    Err   error
}
func (e *TileError) Error() string { ... }
func (e *TileError) Unwrap() error { return e.Err }
```

Rules:

- Bounds checks and format detection return sentinel errors directly. Callers use `errors.Is`.
- Tile-level failures wrap sentinels in `*TileError` to preserve `(level, x, y)` context. Callers use `errors.As`.
- Underlying I/O errors (from the `io.ReaderAt`) propagate wrapped with `%w`, never swallowed.
- No panics from user input; panic only on clear library-internal invariants (e.g., a computed index that cannot be negative).
- `ErrCorruptTile` is subject to `WithCorruptTilePolicy`. v1 default returns the error; future policies (blank tile, parent-level reconstruct) plug in at this point without changing the hot path.

## Testing strategy

Three test layers, each runnable independently.

### Unit tests (no testdata, no network, always run)

- `internal/tiff/` — hand-crafted in-memory TIFF/BigTIFF byte slices. Cover endianness, IFD walks, tag type decode, BigTIFF offsets, tile offset/bytecount arrays. Table-driven.
- `internal/jpeg/` — known JPEG segment blobs. Cover marker scanning, DQT/DHT extraction, table merging, scan concatenation round-trip (split then concat produces identical bytes).
- `formats/svs/metadata_test.go` — real `ImageDescription` strings as fixtures (small and anonymizable). Assert parsed fields.
- `geometry_test.go`, `errors_test.go` — trivial table tests.

### Integration tests (real slides, opt-in)

- Slides resolved via `OPENTILE_TESTDIR` env var (mirrors upstream convention). If unset, integration tests call `t.Skip`.
- `tests/download/` — a Go program that downloads openslide's public test dataset (Aperio SVS, Hamamatsu NDPI) into `OPENTILE_TESTDIR`. Run manually, not as part of `go test`.
- Pre-computed fixtures at `tests/fixtures/<slide>.json`:
  ```json
  {
    "slide": "CMU-1.svs",
    "format": "svs",
    "levels": [
      { "index": 0, "size": [46000, 32914], "tile_size": [240, 240], "grid": [192, 138], "compression": "jpeg", "mpp_um": 0.499 },
      ...
    ],
    "metadata": { "magnification": 20.0, "scanner_manufacturer": "Aperio", ... },
    "tiles": { "0:0:0": "sha256:...", "0:1:0": "sha256:...", ... }
  }
  ```
  Generated by a one-shot `go test -run GenerateFixtures -generate` flow that hashes every tile.
- Integration tests read each slide, iterate every `(level, x, y)`, compare tile sha256 to fixture, and assert geometry and metadata matches.
- Upstream's pytest suite is ported to Go tests alongside fixture comparisons — tile counts, level sizes, pyramid index math, metadata fields. Upstream is the specification.

### Parity harness (opt-in, Python required)

- Built behind `//go:build parity`. Not compiled or run by default `go test ./...`.
- `tests/oracle/` invokes Python opentile via subprocess on the same slides, byte-compares returned tiles to the Go implementation.
- Purposes:
  - **Fixture regeneration** after upstream bug fixes.
  - **Divergence detection** when our internal code paths change.
  - **New-slide validation** before committing a fixture.
- Requires `python` + `opentile` installed locally. Skipped (build tag disabled) otherwise.

### CI shape

- **Default CI:** unit tests + integration tests using a single small, freely-redistributable slide cached on the runner (or fetched once and cached). Fast, hermetic, no Python.
- **Scheduled / label-triggered:** parity job installs Python opentile and runs the `parity`-tagged tests against a fuller slide set.

### Coverage targets

- `internal/tiff`, `internal/jpeg`: ≥90% (small, spec-bound code).
- `formats/svs`: ≥80% (integration-dependent).
- Top-level: ≥75%.

## Roadmap (informative)

| Milestone | Scope |
|-----------|-------|
| **v0.1** | `internal/tiff` IFD walker, `internal/jpeg` marker parser, SVS tiled-level passthrough, metadata, unit + integration tests, fixture harness. |
| **v0.2** | NDPI stripe concatenation and tile-size reshaping. JPEG header rewriting. Upstream pytest cases ported. |
| **v0.3** | SVS associated images (label / overview / thumbnail). Requires pure-Go JPEG encoder for the re-encode path — evaluate stdlib vs. a vendored encoder at that point. |
| **v0.4** | Philips TIFF with sparse-tile filler. |
| **v0.5** | 3DHistech, OME TIFF. |
| **v1.0** | Corrupt-edge fix for SVS. Evaluate optional cgo backend for users who need libjpeg-turbo-grade performance (opt-in build tag). |

Performance optimization is deferred until parity is achieved and profiling on realistic workloads identifies the actual hot paths.
