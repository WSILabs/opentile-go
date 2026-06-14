# opentile-go

> ⚠️ **Status: pre-1.0, under rapid active development.** The public API is not yet stable and may change between releases — pin a version and check the [CHANGELOG](./CHANGELOG.md) before upgrading.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/wsilabs/opentile-go.svg)](https://pkg.go.dev/github.com/wsilabs/opentile-go)
[![CI](https://github.com/WSILabs/opentile-go/actions/workflows/ci.yml/badge.svg)](https://github.com/WSILabs/opentile-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/wsilabs/opentile-go)](https://goreportcard.com/report/github.com/wsilabs/opentile-go)
[![Release](https://img.shields.io/github/v/tag/WSILabs/opentile-go?label=release&sort=semver)](https://github.com/WSILabs/opentile-go/releases)
[![Go 1.23+](https://img.shields.io/github/go-mod/go-version/WSILabs/opentile-go)](./go.mod)

**opentile-go reads whole-slide pathology images in Go** — extracting raw compressed tiles *and* decoding pixel regions from **11 WSI formats**, with pure-Go raw-tile reads and a single cgo dependency for codec decode.

It began as a Go port of the Python [opentile](https://github.com/imi-bigpicture/opentile) library — staying byte-identical on the four formats opentile covers (SVS, NDPI, Philips, OME-TIFF) — and is now a **superset**: it adds openslide-style decoded-region reading and seven more formats than upstream.

**What it does:**

- **Raw tile extraction** — `level.Tile(x, y)` returns the compressed bitstream exactly as stored on disk. Pure Go, no cgo — the zero-copy fast path for tile servers and transcoders.
- **Decoded pixels** — `ReadRegion` (arbitrary regions), `DecodedTile` (single tiles), and `ReadRegionScaled` (downsampled output) return `*decoder.Image`, via cgo codec decoders (JPEG, JPEG 2000, HTJ2K, WebP, AVIF, JPEG XL).
- **Scaled strips / DZI** — `ScaledStrips`, a libvips-style whole-slide region iterator with **byte-bounded** peak memory, for Deep Zoom / tile-pyramid generation.
- **11 formats, auto-detected** — Aperio SVS, Hamamatsu NDPI, Philips TIFF, OME-TIFF, Ventana BIF, Leica SCN, generic tiled TIFF, COG-WSI, [Iris IFE](https://github.com/IrisDigitalPathology/Iris-File-Extension), [SZI](https://github.com/smartinmedia/SZI-Format), and multi-file **DICOM WSI**.
- **Associated images & metadata** — label / overview / thumbnail / macro, MPP, magnification, vendor properties, and raw per-level / per-image **TIFF-tag** access.
- **Built for throughput** — mmap-backed reads, pool-friendly zero-alloc `TileInto`, concurrent-safe hot path; decoded-region throughput **3–14× openslide** on the in-repo benchmark ([Performance](#performance)).

```go
import (
    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
)

t, err := opentile.OpenFile("slide.svs")
if err != nil { /* ... */ }
defer t.Close()

base, _ := t.Level(0)
tile, err := base.Tile(0, 0) // raw compressed JPEG / JP2K / etc. bytes
```

`Tile(x, y)` returns the raw compressed bitstream exactly as stored on disk — pure Go, no cgo, zero-copy. Hand it to any JPEG / JPEG 2000 / etc. decoder downstream, or let opentile-go decode for you — see [decoded pixel regions](#reading-pixel-regions-and-scaled-strips) below.

**Decoded pixels (shipped).** For decoded regions instead of raw bytes — `level.ReadRegion`, `level.DecodedTile`, `pyr.ReadRegionScaled`, `pyr.ScaledStrips`, all returning `*decoder.Image` — also register the codec decoders, the same side-effect-import pattern as `formats/all`:

```go
import (
    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all" // register format readers
    _ "github.com/wsilabs/opentile-go/decoder/all" // register codec decoders (enables decode)
)
```

See [Reading pixel regions and scaled strips](#reading-pixel-regions-and-scaled-strips) for the API, and [`decoder/`](./decoder/) / [`resample/`](./resample/) for the codec and resampling layers.

## Supported formats

| Format | Extension | Levels | Associated | Compression | Parity bar | Detail |
|---|---|---|---|---|---|---|
| **Aperio SVS** | `.svs` | tiled | label, overview, thumbnail | JPEG, JP2K (passthrough) | byte-parity vs. Python opentile | [docs/formats/svs.md](./docs/formats/svs.md) |
| **Hamamatsu NDPI** | `.ndpi` | tiled (stripped + OneFrame) | overview, synthesised label\*, Map\* | JPEG | byte-parity vs. Python opentile | [docs/formats/ndpi.md](./docs/formats/ndpi.md) |
| **Philips TIFF** | `.tiff` | tiled, with sparse-tile fill | label, overview, thumbnail | JPEG | byte-parity vs. Python opentile | [docs/formats/philipstiff.md](./docs/formats/philipstiff.md) |
| **OME-TIFF** | `.ome.tiff` | tiled (SubIFD) + OneFrame | overview, label, thumbnail | JPEG (uint8 RGB only) | byte-parity vs. Python opentile + tifffile | [docs/formats/ometiff.md](./docs/formats/ometiff.md) |
| **Ventana BIF** | `.bif` | tiled, serpentine remap, with overlap metadata\* + ScanWhitePoint blank-tile fill | overview, probability\*, thumbnail | JPEG | tifffile (DP 200) + sampled-tile SHAs (both fixtures) | [docs/formats/bif.md](./docs/formats/bif.md) |
| **Iris IFE\*** | `.iris` | tiled (256×256, native-first inversion) with sparse-tile sentinel | label, overview, thumbnail, macro, map, probability + free-form titles + ICC profile + free-form attribute map | JPEG, AVIF (passthrough), Iris-proprietary (passthrough) | sampled-tile SHAs + synthetic-writer + per-fixture geometry pin | [docs/formats/ife.md](./docs/formats/ife.md) |
| **Generic TIFF\*** | `.tiff`, `.tif` | tiled pyramidal (≥1 level, geometric scale chain) | classifier-assigned: label, overview, thumbnail, or `"associated"` fallback | JPEG, JP2K, LZW, Deflate, None, WebP, JPEG XL, AVIF, HTJ2K (all passthrough) | sampled-tile SHAs + per-fixture geometry pin + cross-backing parity | [docs/formats/generictiff.md](./docs/formats/generictiff.md) |
| **Leica SCN\*** | `.scn` | tiled BigTIFF; multi-region "discontinuous scanning"; multi-channel fluorescence | classifier-assigned: overview per auxiliary `<image>` | JPEG | sampled-tile SHAs + per-fixture geometry pin + bio-formats CLI parity oracle | [docs/formats/leicascn.md](./docs/formats/leicascn.md) |
| **Smart Zoom Image (SZI)\*** | `.szi` | ZIP-wrapped Microsoft Deep Zoom pyramid; per-level dim halving; sparse images not supported per spec | label, overview (from `macro.jpg`), thumbnail | JPEG / PNG (all passthrough) | sampled-tile SHAs + per-fixture geometry pin | [docs/formats/szi.md](./docs/formats/szi.md) |
| **COG-WSI\*** | `.tiff` | strict GDAL Cloud Optimized GeoTIFF + WSI private tags (65080-87) + COG_WSI_VERSION ghost-area marker | label, overview (from `macro` or `overview` WSIImageType), thumbnail | source-format preserving (JPEG, JP2K, LZW, …) | per-fixture geometry pin + cross-fixture parity vs source format + ErrNotConformantCOGWSI spec validation | [docs/formats/cogwsi.md](./docs/formats/cogwsi.md) |
| **DICOM WSI\*** | `.dcm` (or directory) | multi-file directory series (TILED_FULL + TILED_SPARSE); first multi-file format; `OpenFile` only — accepts a directory or any one `.dcm` | label, overview, thumbnail (from `ImageType` LABEL/OVERVIEW/THUMBNAIL) | JPEG Baseline + uncompressed (pure-Go `suyashkumar/dicom` parser; no new cgo) | sampled-tile SHAs + per-fixture geometry pin; verified on Leica GT450 / 3DHISTECH / Grundium | [docs/formats/dicom.md](./docs/formats/dicom.md) |

\* Marks Go-side extensions beyond upstream Python opentile; see [Deviations](#deviations-from-upstream-python-opentile) below.

**Detection** is automatic. `opentile.OpenFile` walks the registered factories — first asking each for `SupportsRaw(r, size)` against the raw byte stream, then falling through to TIFF-parsed `Supports(file)` — and dispatches the first match. The two-stage dispatch lets non-TIFF formats (IFE) short-circuit before `tiff.Open`. The generic-TIFF reader registers LAST so vendor format detectors get first crack at any TIFF; it activates as a catch-all only when no vendor factory claims the file. Format packages register at import time via `_ "github.com/wsilabs/opentile-go/formats/all"`.

**Format coverage**: opentile-go ports the four TIFF formats Python opentile 0.20.0 supports for tile extraction. 3DHistech TIFF (the fifth upstream format) is parked at [#2](https://github.com/wsilabs/opentile-go/issues/2). Ventana BIF — the first beyond upstream's coverage — landed in v0.7. Iris IFE — the first non-TIFF format — landed in v0.8. Generic TIFF — a catch-all reader for tiled pyramidal TIFFs without vendor metadata — landed in v0.10. Leica SCN — the legacy SCN400/SCN400F format, including the first multi-channel fluorescence support — landed in v0.11. Smart Zoom Image (SZI) — a ZIP-wrapped Microsoft Deep Zoom pyramid backed by a shared `internal/dzi/` core — landed in v0.16. DICOM WSI — the first multi-file format, reading VL Whole Slide Microscopy Image series via `OpenFile` on a directory or any `.dcm` — landed in v0.32. Sakura SVSlide is parked at [#3](https://github.com/wsilabs/opentile-go/issues/3).

## Prerequisites

- **Go 1.23+** (uses `iter.Seq2`).
- **libjpeg-turbo 2.1+** — JPEG decode + tile-domain ops (NDPI edge-tile fill, Philips sparse-tile fill, OME OneFrame). macOS: `brew install jpeg-turbo`; Debian / Ubuntu: `apt-get install libturbojpeg0-dev`.
- **Optional codec libraries**, each disableable with a `no<codec>` build tag if you don't have it: OpenJPEG / JPEG 2000 (`nojp2k`), libjxl (`nojxl`), libwebp (`nowebp`), libavif (`noavif`), openjph / HTJ2K (`nohtj2k`). libjpeg-turbo is the only codec linked under every cgo build.
- **`pkg-config`** to resolve the above at build time.

opentile-go uses **cgo for codec decode** — `internal/jpegturbo/` wraps libjpeg-turbo (incl. its `tjTransform` lossless DCT-domain crops); the `decoder/*` packages link the other codec libraries above. **Raw-tile reads (`level.Tile`) are pure Go and need no cgo.** Building without cgo (`-tags nocgo` or `CGO_ENABLED=0`) is supported: raw-tile reads and SVS / NDPI-stripped passthrough work; decode paths (NDPI OneFrame / edge-tile fill, Philips sparse-tile fill, OME OneFrame, and any non-JPEG codec) return `ErrCGORequired`.

## Install

```sh
go get github.com/wsilabs/opentile-go
```

Pin to v0.5.1 or later (v0.5.0 shipped with a wrong module path; see [CHANGELOG](./CHANGELOG.md)).

## API

### Opening a slide

```go
t, err := opentile.OpenFile("slide.tiff")
if err != nil { /* ErrUnsupportedFormat or open error */ }
defer t.Close()

fmt.Println("format:", t.Format())                 // "svs", "ndpi", "philips-tiff", "ome-tiff", "bif", "ife", "generic-tiff", "leica-scn", "szi", "cog-wsi", "dicom"
fmt.Println("levels:", len(t.Levels()))
```

Pass options to override defaults:

```go
t, err := opentile.OpenFile("slide.ndpi",
    opentile.WithTileSize(1024, 1024),                     // virtual tile size for OneFrame levels
    opentile.WithNDPISynthesizedLabel(false),              // disable the v0.2 NDPI label synthesis
)
```

For an `io.ReaderAt` source (S3, in-memory, etc.) instead of a filename:

```go
t, err := opentile.Open(reader, size, opts...)
```

### Reading tiles

```go
base, _ := t.Level(0)

// Per-tile metadata.
fmt.Printf("base: %v tiles of %v pixels, compression %s, mpp %v\n",
    base.Grid, base.TileSize, base.Compression, base.MPP)

// Get one tile's raw compressed bytes.
tile, err := base.Tile(0, 0)
```

Stream a tile via `io.ReadCloser`:

```go
rc, err := base.TileReader(0, 0)
defer rc.Close()
io.Copy(dst, rc)
```

Iterate every tile in row-major order:

```go
for pos, res := range base.Tiles(ctx) {
    if res.Err != nil { /* ... */ }
    process(pos.X, pos.Y, res.Bytes)
}
```

### Multi-image files

OME-TIFF can carry multiple main pyramids in a single file. `s.Pyramids()` returns them all; `s.Levels()` is a shortcut to `s.Pyramids()[0].LevelPtrs()` for callers that don't need to distinguish.

```go
for _, pyr := range t.Pyramids() {
    l0, _ := pyr.Level(0)
    fmt.Printf("Pyramid %d (%q): %d levels, %v µm/px\n",
        pyr.Index, pyr.Name, len(pyr.Levels), l0.MPP)
    tile, _ := l0.Tile(0, 0)
    _ = tile
}
```

For SVS, NDPI, and Philips, `Pyramids()` always returns a one-element slice — `Levels()` / `Level(i)` work as before.

### Reading pixel regions and scaled strips

The `Tile*` methods above return one tile's *compressed* bytes. For
*decoded* pixels — arbitrary regions, downsampled output, or whole-slide
streaming (DZI conversion, tile servers, region extract) — use the
region/strip API. All of these return `*decoder.Image` (`Width`,
`Height`, `Stride`, `Format`, `Pix []byte`).

```go
// A decoded pixel region at a given level (level coords).
base, _ := t.Level(0)
img, err := base.ReadRegion(opentile.Region{
    Origin: opentile.Point{X: x, Y: y},
    Size:   opentile.Size{W: w, H: h},
})

// An L0 region scaled to an explicit output size (e.g. a thumbnail
// of the whole slide). IDCT-time downscale + resample under the hood.
l0 := t.Levels()[0]
pyr := t.Pyramid(0)
thumb, err := pyr.ReadRegionScaled(
    opentile.Region{Size: l0.Size}, // full L0 extent
    opentile.Size{W: 1024, H: 1024},
)
```

For whole-slide scaled output that is too large to hold in memory at
once — DZI/deep-zoom builders, libvips-style pipelines — iterate it in
horizontal **strips**. `ScaledStrips` runs parallel decode workers + a
bounded internal cache + lookahead prefetch; you pull one strip at a
time:

```go
l0 := t.Levels()[0]
pyr := t.Pyramid(0)
it := pyr.ScaledStrips(
    opentile.Region{Size: l0.Size},          // L0 region (here: whole slide)
    l0.Size,                                 // output size (here: native res)
    256,                                     // strip height in output rows
    // opts: WithStripWorkers, WithStripLookahead, WithStripKernel,
    // WithStripIDCTScale, WithStripContext
)
defer it.Close() // mandatory — reaps the worker goroutines

for {
    strip, err := it.Next()
    if err == io.EOF {
        break
    }
    if err != nil { /* ... */ }
    consume(strip) // strip is one *decoder.Image band, outSize.W wide
}
```

Peak memory for the strip path is **bounded and independent of slide
width** — see [Performance → Memory](#performance) below for the budget
knob and tuning.

### Associated images

`s.AssociatedImages()` returns label / overview / thumbnail / map images where the format provides them:

```go
for _, a := range t.AssociatedImages() {
    // Raw: compressed bytes as stored on disk.
    b, err := a.Bytes()
    if err != nil { continue }
    fmt.Printf("%s: %v, %s, %d bytes\n", a.Type(), a.Size(), a.Compression(), len(b))

    // Decoded: faithful RGB(A) pixels (needs `_ "…/decoder/all"`).
    img, err := a.Decode(decoder.DecodeOptions{}) // or {Format: decoder.PixelFormatRGBA}
    // img is *decoder.Image{Width, Height, Stride, Format, Pix}
}
```

- `a.Bytes()` returns the compressed bytes in whatever codec the source carries (JPEG, LZW, …). For multi-strip **LZW** labels this is a re-encoded stream — use `Decode` if you need pixels.
- `a.Decode(opts)` returns faithfully-decoded pixels for **any** codec (JPEG / JP2K / HTJ2K / WebP / AVIF / JPEG XL / LZW incl. Predictor=2 / Deflate / uncompressed), owning all codec / strip / predictor handling. Returns `decoder.ErrCodecUnavailable` when the codec isn't compiled in (e.g. JP2K under `nojp2k`).
- `a.Type()` returns an `AssociatedType` — `"label"`, `"overview"`, `"thumbnail"`, `"macro"` (IFE), `"map"` (NDPI/IFE), `"probability"` (BIF/IFE), or `"associated"`. Use typed constants `opentile.AssociatedLabel`, `AssociatedOverview`, `AssociatedThumbnail`, `AssociatedMap`, `AssociatedProbability`, `AssociatedMacro`, `AssociatedGeneric` rather than string literals.
- `a.Encoding()` returns the **on-disk encoded strips + TIFF tags** so a consumer can re-emit the associated image into a fresh standalone single-IFD TIFF with **no re-encode** — byte-identical strip bytes plus the `Compression` / `Predictor` / `JPEGTables` / `RowsPerStrip` / `Samples` / `Photometric` tags needed to write a conforming IFD. `ok=false` for associated images with no faithful single-IFD strip form — self-contained JPEGs (use `Bytes()`), DICOM frames, OME planar pages, tiled, and synthesized labels.

### Format-specific metadata

Cross-format fields (manufacturer, scanner serial, acquisition datetime, magnification) are surfaced via `t.Metadata()`. Format-specific fields are accessible by type-asserting through a per-format helper:

```go
import (
    svs "github.com/wsilabs/opentile-go/formats/svs"
    ndpi "github.com/wsilabs/opentile-go/formats/ndpi"
    philips "github.com/wsilabs/opentile-go/formats/philips"
    ome "github.com/wsilabs/opentile-go/formats/ome"
)

if md, ok := svs.MetadataOf(t); ok {
    fmt.Println("MPP (SVS):", md.MPP, "µm/px")
}
if md, ok := ndpi.MetadataOf(t); ok {
    fmt.Println("source lens (NDPI):", md.SourceLens, "x")
}
if md, ok := philips.MetadataOf(t); ok {
    fmt.Println("PixelSpacing (Philips):", md.PixelSpacing, "mm")
}
if md, ok := ome.MetadataOf(t); ok {
    fmt.Println("OME images:", len(md.Images))
}
```

`MetadataOf` walks any number of wrapper Tilers (e.g., `*fileCloser` from `OpenFile`) before asserting on the concrete type, so the helper works regardless of how the Tiler was obtained.

### Raw TIFF tags

For TIFF-based formats, raw tags — including vendor/private tags not surfaced
as typed `Metadata` fields — are available per IFD, anchored to the level or
associated image you already hold:

```go
base, _ := slide.Level(0)
tags, ok := base.TIFFTags()                  // level-0 IFD tags
if ok {
    if tag, ok := tags.Tag(65420); ok {      // a vendor/private tag by number
        s, _ := tag.ASCII()
        _ = s
    }
}
a.TIFFTags()                                  // an associated image's tags
slide.TIFFDirectories()                       // every IFD incl. orphans (Map/hidden)
```

`TIFFTag` carries `Number`, best-effort `Name`, `Type`, `Count`, verbatim
`Raw` bytes, and typed getters (`ASCII`/`Uints`/`Rationals`). Non-TIFF
formats (IFE, SZI) return `ok=false`. Pixel-pointer tags
(`StripOffsets`/`TileOffsets`/…) are excluded. Implemented for all
TIFF-based formats (SVS, NDPI, Philips, OME-TIFF, BIF, generic-TIFF,
Leica-SCN, COG-WSI).

### Concurrency

`Level.Tile`, `Level.TileInto`, `Level.TileAt`, and `Level.TileReader` are safe to call concurrently from multiple goroutines. SVS / Philips / OME tiled / BIF / IFE have no internal locks on the tile hot path. NDPI's stripped reader takes a per-page mutex on its assembled-frame cache; concurrent reads of *different* pages run in parallel, concurrent reads of the *same* page serialize. OME OneFrame is similar.

All internal caches (parsed IFDs, per-tile offset / length tables, metadata) are populated at `Open()` time and then immutable — no locks on the tile hot path. Format packages with shared lazy caches use `sync.Once` and produce byte-deterministic output regardless of which goroutine populates them first.

`Close()` must not race with in-flight tile reads — drain before closing. Under the v0.9 default mmap backing, this is non-negotiable: closing unmaps the file, and subsequent reads through the mapping raise SIGBUS.

### Performance

opentile-go's tile reads are designed for high-RPS HTTP serving and per-frame desktop viewers. See [`docs/perf.md`](./docs/perf.md) for the full guide. Quick summary:

- **`OpenFile` is mmap-backed by default** since v0.9. Tile reads become userspace memcpy; no `pread(2)` syscall per call. Opt out via `opentile.WithBacking(opentile.BackingPread)`.
- **Use `Level.TileInto(x, y, dst) (int, error)`** with a `sync.Pool` of `[]byte` buffers sized to `Level.TileMaxSize()` for zero-allocation tile reads. Cervix serial: 152 ns/op, 0 allocs (vs v0.8's 22µs).
- **`level.Warm() error`** pre-warms the page cache for predictable warm-cache latency.
- **Bandwidth deduplication (v0.13):** `Level.TilePrefix()` returns the constant JPEG prefix; `Level.TileBodyInto(x, y, dst)` returns on-disk bytes without the prefix. Client-server consumers can send the prefix once per session and body bytes per tile. `opentile.SpliceJPEGTile(prefix, body)` reconstitutes a complete JPEG on the client side. Savings are fixture-author-dependent — see [`docs/perf.md`](./docs/perf.md) for details.

### Memory (ScaledStrips / DZI path)

The `ScaledStrips` decode path keeps a bounded internal cache of decoded
source tiles. Since **v0.30** that cache is **byte-bounded**, so peak
memory is flat regardless of slide width — a 19-gigapixel NDPI slide and
a 2-gigapixel one peak at roughly the same level.

- **`opentile.WithMemoryBudget(bytes)`** — per-`Slide` budget for the
  read-path cache. Default **1 GiB**. Also settable without recompiling
  via the **`OPENTILE_READ_MEMORY_BUDGET`** env var (bytes); the option
  wins over the env var, which wins over the default.
- **Set `GOMEMLIMIT`** for the tightest peak. The budget bounds the
  *live* working set; Go's GC headroom (`GOGC=100`) lets the heap grow
  ~2× live before collecting. A `GOMEMLIMIT` (e.g. `2GiB`) clamps that
  headroom — and when set, opentile-go's default budget auto-shrinks to
  ≤ half of it. opentile-go never *sets* `GOMEMLIMIT` itself.
- **Keep the DZI tile size at 256** (the default) for the lowest peak.
  Larger tiles (512/1024) need a proportionally larger full-width output
  strip buffer, which the cache budget does not cover.

Measured peak RSS on the widest test slide (Hamamatsu, 188k×101k px),
worst case (no consumer backpressure):

| Config | peak RSS |
|---|---|
| 256 tile + `GOMEMLIMIT=2GiB` (recommended) | ~2.1 GB |
| 256 tile, no env | ~3.3 GB |
| 1024 tile, no env (heaviest) | ~5.8 GB |

Even on a hypothetical maximum-size 2″×1″ 40× slide the recommended
config stays ~2.3 GB; the absolute ceiling across all configs is ~7 GB.
The peak is a fixed ceiling, not the unbounded climb of pre-v0.30. See
[`docs/perf.md`](./docs/perf.md) for the full breakdown.

### Benchmarks & comparison

The repo carries a standing cross-format benchmark suite (`bench/`):

- **`go test ./bench/ -bench BenchmarkRead`** — per-format throughput for
  `Tile` (compressed), `DecodedTile`, and `ReadRegion`, single and
  parallel, reporting `Mpix/s` + `allocs/op`. The profiling / A/B
  instrument (`benchstat`-friendly).
- **`make bench-all`** — a local per-format throughput regression gate.
- **`make bench-compare`** — an on-demand competitive report against
  [openslide](https://openslide.org/) (decoded `read_region`) and Python
  opentile (compressed `get_tile`). Requires libopenslide + a
  python-opentile interpreter.

On the in-repo benchmark (one fixture per format, bounded interior grid,
v0.34.1, 10-core Apple Silicon), opentile-go's **decoded-region
throughput is 3–14× openslide** (e.g. generic-TIFF 14.3×, Philips 11.1×,
SVS 9.5×, NDPI 3.2×). Raw compressed-tile fetch is ≈parity with Python
opentile — both return the same compressed bytes, so it's an mmap-slice
on both sides. These are single-machine, single-run figures; run `make
bench-compare` for current numbers on your hardware, and see
[`docs/perf.md`](./docs/perf.md#measured-competitive-numbers-v0341) for
the full table, methodology, and caveats (region alignment, single
machine, the multi-region SCN bounds offset).

## Deviations from upstream Python opentile

opentile-go aims for byte-parity with Python opentile 0.20.0. A small number of deviations exist where matching upstream would encode an upstream oversight or where opentile-go provides a strictly more useful affordance:

| Deviation | Format | Since | Opt-out / API | Why |
|---|---|---|---|---|
| Synthesised label | NDPI | v0.2 | `WithNDPISynthesizedLabel(false)` | Upstream doesn't surface NDPI labels at all; we crop the left 30% of the overview to provide an Aperio-style label affordance. |
| Map pages exposed | NDPI | v0.4 | not opt-out-able (silent absence) | tifffile already classifies them as `series.name == 'Map'`; surfacing matches the underlying TIFF carrying. |
| Multi-image OME pyramids | OME | v0.6 | use `s.Levels()` instead of `s.Pyramids()` for first-pyramid-only behaviour | Upstream's base Tiler loop silently drops 3 of 4 main pyramids in multi-image files via an unintentional last-wins assignment. We expose all of them via `s.Pyramids()`. |
| Probability map exposed as `type="probability"` | BIF | v0.7 | iterate `s.AssociatedImages()` and skip the type | Upstream doesn't read BIF; openslide drops the probability map. We surface it for downstream tools that want it. |
| `Level.TileOverlap` field | BIF + all | v0.7 | non-BIF formats return `Point{}` (zero) — no caller change needed | BIF level-0 stores tiles with horizontal overlap; consumer needs the value to position raw tile bytes correctly. |
| Non-strict `ScannerModel` acceptance | BIF | v0.7 | not opt-out-able | The BIF spec mandates rejecting any slide whose `ScannerModel != "VENTANA DP 200"`; we accept any iScan-tagged BigTIFF and route via `HasPrefix("VENTANA DP")` so legacy iScan slides aren't worse-than-openslide. |
| Multi-dimensional WSI API addition (`TileCoord` + `Level.TileAt`) | All formats | v0.7 | additive — 2D-only formats return defaults | Modern WSI consumers (fluorescence, focal-plane viewers, time series) need explicit multi-dim addressing. BIF reads multi-Z natively; full Z/C/T surface deferred to a future format-package milestone. |
| Non-TIFF dispatch path (`FormatFactory.SupportsRaw` + `OpenRaw` + `RawUnsupported` base) | All formats | v0.8 | additive — TIFF factories embed `RawUnsupported` and inherit defaults | Iris IFE is the first non-TIFF format opentile-go reads. Table-driven dispatch lets each format own its detection; future non-TIFF formats drop in additively. |
| `TILE_TABLE.x_extent` / `y_extent` ignored | IFE | v0.8 | not opt-out-able | The IFE v1.0 spec doc claims these fields carry image pixel dims, but the cervix fixture stores tile counts (matching `LAYER_EXTENTS.x_tiles`). Reader derives image dims from `LAYER_EXTENTS × 256` instead — unambiguous either way. |
| Default mmap-backed `OpenFile` | All formats | v0.9 | `WithBacking(BackingPread)` | Universal perf win on the hot path (8–145× speedup; cervix serial Tile dropped from 22µs to 0.75µs). Auto-fallback to pread on mmap failure; SIGBUS on file truncation documented in the OpenFile docstring. |
| `Level.TileInto` + `Level.TileMaxSize` interface evolution | All formats | v0.9 | additive — existing `Tile()` unchanged | Pool-friendly tile-read API. With `sync.Pool` of `[]byte` buffers sized to `TileMaxSize()`, the caller does zero allocations per tile on every TIFF format and IFE. NDPI / OME OneFrame still allocate internal scratch. |
| `Level.Warm()` interface evolution | All formats | v0.9 | additive — hint operation, callers can ignore | Page-cache pre-warm for predictable warm-cache latency. Useful for slide-server pre-warm at startup. |
| Generic-TIFF reader for non-vendor tiled pyramidal TIFFs | Generic TIFF | v0.10 | not opt-out-able once registered; any TIFF that no vendor factory claims AND that passes the validator routes here | Real-world WSI authoring outside Aperio / Hamamatsu / Philips is common (Grundium, Roche legacy iScan, vendor-stripped derivatives, libtiff-encoded research outputs). A catch-all reader makes opentile-go consume any structurally valid pyramid TIFF without per-vendor reverse-engineering. |
| `"associated"` AssociatedImage Type value addition | Generic TIFF | v0.10 | iterate `s.AssociatedImages()` and skip the type | Generic TIFFs may carry non-pyramid IFDs the heuristic classifier can't confidently match to label / macro / thumbnail; surfacing them as `"associated"` lets the consumer access Bytes() / Size() without a wrong-but-plausible type label. |
| Leica SCN reader for legacy SCN400 / SCN400F output | Leica SCN | v0.11 | not opt-out-able once registered | First real-fixture multi-region "discontinuous scanning" reader. Architecturally valuable beyond just SCN coverage. |
| `Level.TilePrefix` / `TileBodyInto` / `TileBodyMaxSize` + `opentile.SpliceJPEGTile` interface evolution | All formats (JPEG splice formats benefit) | v0.13 | additive — existing `Tile()` / `TileInto()` unchanged | Bandwidth-deduplication API for client-server consumers: send the per-level prefix once, send per-tile body bytes per request, reconstitute on client. Savings fixture-author-dependent (only slides with shared JPEGTables benefit). |
| Smart Zoom Image (SZI) reader | Smart Zoom Image | v0.16 | not opt-out-able once registered | First ZIP-backed format opentile-go reads; first format to surface `CompressionPNG`. Spec-mandated uncompressed-stored ZIP entries preserve the v0.9 mmap-aliased fast path. Backed by a new shared `internal/dzi/` core designed for additive bare-DZI support in v0.17+. |
| COG-WSI reader + integer-multiple pyramid ratio acceptance | COG-WSI + generic-TIFF | v0.19 | not opt-out-able once registered | First spec-validated COG-profile reader opentile-go ships; pairs WSI-domain private tags 65080-87 + `COG_WSI_VERSION` ghost-area marker with the GDAL Cloud Optimized GeoTIFF base structure. Closes Issues [#5](https://github.com/wsilabs/opentile-go/issues/5) + [#6](https://github.com/wsilabs/opentile-go/issues/6). Generic-TIFF standalone benefit: relaxed strict-drift check now accepts clean integer-multiple pyramid ratios (Aperio / Grundium SVS-style 4×/2×/2× chains). |

Full reasoning + per-deviation commit references are in [`docs/deferred.md`](./docs/deferred.md).

## Testing

```sh
make test     # go test ./... -race -count=1
make vet      # go vet ./...
make cover    # ≥80% per package; needs OPENTILE_TESTDIR
make parity   # batched parity oracle vs Python opentile 0.20.0 + tifffile
make bench    # NDPI per-tile throughput regression gate
```

Integration tests and the parity oracle require real slide files at `$OPENTILE_TESTDIR`. Fixture JSONs (committed) are at `tests/fixtures/`. Slides themselves are not redistributable and are gitignored.

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/... -v
```

For parity testing against Python opentile + tifffile, set the Python interpreter and run with the `parity` build tag:

```sh
pip install -r tests/oracle/requirements.txt
OPENTILE_ORACLE_PYTHON=$(which python) \
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v
```

The default run samples ~100 tile positions per level per slide. A persistent stdin / stdout protocol keeps one Python subprocess resident per slide; full sweep on the v0.6 13-slide oracle slate completes in under 10 seconds.

## License + attribution

Apache 2.0. Independent Go port of the Python [opentile](https://github.com/imi-bigpicture/opentile) library (Copyright 2021–2024 Sectra AB); see [NOTICE](./NOTICE) for attribution. Not affiliated with or endorsed by Sectra AB or the BigPicture project.
