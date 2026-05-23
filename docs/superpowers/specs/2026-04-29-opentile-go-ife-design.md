# opentile-go IFE Design Spec — Iris File Extension support

**Status:** Draft, 2026-04-29. Format-specific spec (not yet bound to
a milestone — IFE may land in v0.8 or later depending on
prioritisation against alternatives). **Predecessors:** v0.1 – v0.7
(BIF closeout pending).
**Reference plan:** [`sample_files/ife/ife-format-spec-for-opentile-go.md`](../../../sample_files/ife/ife-format-spec-for-opentile-go.md)
(extracted from upstream Iris-File-Extension repo, 2026-04-28).
**Upstream reference (read-for-understanding only):**
[`IrisDigitalPathology/Iris-File-Extension`](https://github.com/IrisDigitalPathology/Iris-File-Extension)
(MIT-licensed implementation), [`Iris-Headers`](https://github.com/IrisDigitalPathology/Iris-Headers)
(format types). Spec itself is CC BY-ND 4.0.

**Decisions sealed 2026-04-29** (owner sign-off):

| § | Decision |
|---|----------|
| 3 | **Option B — `FormatFactory.SupportsRaw`** for non-TIFF dispatch. The current `Supports(*tiff.File)` becomes a sub-case; a new `SupportsRaw(io.ReaderAt, int64)` runs first and lets non-TIFF formats short-circuit before `tiff.Open`. |
| 4 | **README framing widens**, package name unchanged. opentile-go remains the import path; the README's first paragraph + the supported-formats table broaden to "WSI files, including TIFF dialects and Iris IFE." No package rename, no API breakage. |
| 5 | **New `Compression` values**: `CompressionAVIF` (decodable by future consumers via libavif/dav1d) and `CompressionIRIS` (proprietary; reported but undecodable in opentile-go — consumers either ship an Iris codec or 501 the request). |
| 6 | **Layer ordering inverted at parse time.** IFE stores layers coarsest-first; opentile-go's `Levels()` is native-first. Reader builds an inverted slice + a `layerCumulative` prefix sum and never exposes the file's storage order across the API. |
| 7 | **Parity strategy: sample-tile SHA fixtures only.** No external pixel oracle in v0.8/IFE-1.0 — IFE is bleeding-edge with no analogue of tifffile or openslide for cross-validation. Coverage is `TestSlideParity` SHA hashes against committed fixtures + synthetic-IFE-writer unit tests. Cross-tool parity (e.g. `tile_server_iris` HTTP byte-for-byte) is a future work item. |
| 10 Q7 | **Cervix-only fixture for v0.8.** Download `cervix_2x_jpeg.iris` (~2.16 GB, SHA256 `b080859913d2…`) from `irisdigitalpathology.s3.us-east-2.amazonaws.com/example-slides/cervix_2x_jpeg.iris` into `sample_files/ife/` (gitignored). No regen tooling in v0.8 — locally-encoded fixtures via the user's separate Iris workspace can be added later if needed. |
| 10 Q8 | **Plumbing refactor + IFE ship together as v0.8.0.** Refactor lands as Batch B of the IFE milestone — cleaner story, single tag. The refactor is additive (default `SupportsRaw` returns false; default `OpenRaw` returns `ErrUnsupportedFormat`) so no caller breakage. |
| 10 Q9 | **No AVIF decoder integration.** opentile-go remains byte-passthrough — we expose `CompressionAVIF` so consumers know what they're getting, but linking `libavif` or any decoder is the consumer's call. Keeps the cgo footprint at `internal/jpegturbo/` only. Same model as JPEG / JP2K today. |

---

## 1. One-paragraph scope

IFE adds the **first non-TIFF format** opentile-go has handled. It
ships as `formats/ife/`, ~520 LoC of reader code per the
sample_files plan. Three new building blocks: a **non-TIFF dispatch
path** in `opentile.Open` (new `FormatFactory.SupportsRaw` method),
**two new `Compression` enum values** (`CompressionAVIF` and
`CompressionIRIS`), and a **layer-ordering inversion** at parse
time so consumers see native-first Levels. Tile size is fixed at
256×256 (codec-internal, not configurable); the reader is otherwise
mechanical — magic-byte sniff → FILE_HEADER (38 B) → TILE_TABLE
(44 B) → LAYER_EXTENTS + TILE_OFFSETS arrays → `ReadAt(offset, size)`
per tile. No JPEGTables splice (tiles are self-contained), no AOI
math (single image, full-rect tile grid), no overlap (always
non-overlapping). Sparse-tile sentinel `0xFFFFFFFFFF` in the 40-bit
offset field returns `ErrSparseTile`.

## 2. Universal task contract

Same as v0.4 / v0.5 / v0.6 / v0.7: every plan task starts with
`Step 0: Confirm upstream`. Upstream sources for IFE are layered:

1. **`sample_files/ife/ife-format-spec-for-opentile-go.md`** — the
   plan dropped 2026-04-29. Authoritative project-internal mirror of
   the upstream byte-layout specification, distilled for
   opentile-go's reader-only path.
2. The Iris-Headers C++ source (`IrisCodecTypes.hpp`, `IrisTypes.hpp`)
   for enum values + structure layouts — read-for-understanding;
   never copied verbatim (MIT permits the port, but the spec doc
   itself is CC BY-ND 4.0).
3. Sample IFE files when available (none committed at design time;
   provisioning in §10).

## 3. Format-dispatch refactor — `FormatFactory.SupportsRaw`

Current `opentile.Open(r io.ReaderAt, size int64, opts ...Option)`
flow:

```go
file, err := tiff.Open(r, size)
if err != nil { return nil, ... }   // ❌ IFE fails here today
for _, f := range registered {
    if f.Supports(file) { return f.Open(file, cfg) }
}
```

**Refactor.** Add `SupportsRaw` to the factory interface; run it
*before* `tiff.Open`:

```go
type FormatFactory interface {
    Format() opentile.Format

    // SupportsRaw is consulted before tiff.Open. Format packages whose
    // files are NOT classic/BigTIFF (e.g., Iris IFE) implement it to
    // sniff their own magic bytes from r. Returns true to take the
    // dispatch off the TIFF path. The default (zero-value) impl
    // returns false; pre-IFE format factories don't need to override.
    SupportsRaw(r io.ReaderAt, size int64) bool

    // Supports + Open continue to operate on a parsed *tiff.File.
    Supports(file *tiff.File) bool
    Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error)
}
```

**Plus** a sister `OpenRaw` for the non-TIFF path. The dispatch loop
becomes:

```go
for _, f := range registered {
    if f.SupportsRaw(r, size) {
        return f.OpenRaw(r, size, cfg)
    }
}
file, err := tiff.Open(r, size)
if err != nil { ... }
for _, f := range registered {
    if f.Supports(file) { return f.Open(file, cfg) }
}
```

**Backward compatibility.** The four existing format factories
(SVS, NDPI, Philips, OME, BIF) get default `SupportsRaw` (returning
false) + default `OpenRaw` (returning `ErrUnsupportedFormat`)
embedded as a base struct. Their existing `Supports`/`Open`
unchanged. Zero caller-visible API breakage.

**Why not Option A (magic-byte sniff in `opentile.Open` directly).**
A is fine for one non-TIFF format but doesn't compose. Adding a
second non-TIFF later (e.g., DICOM-WSI, vendor-specific BigTIFF
mutations) means more conditional branches in `Open`; B keeps the
dispatch table-driven and lets each format own its detection. The
extra ~30 lines of plumbing pays back the first time we add the
second non-TIFF.

## 4. Project naming + README framing

opentile-go remains the import path
(`github.com/wsilabs/opentile-go`). No package rename in v0.8/IFE.

**README updates** (small):

- Opening line: "raw compressed tiles from whole-slide imaging
  (WSI) **TIFF files**" → "raw compressed tiles from whole-slide
  imaging (WSI) files, including TIFF dialects and Iris IFE."
- Supported-formats table gains an IFE row.
- Detection paragraph: add a sentence about the
  `SupportsRaw`-before-`tiff.Open` dispatch order.

## 5. New `Compression` enum values

In `compression.go`:

```go
const (
    CompressionUnknown Compression = iota
    CompressionNone
    CompressionJPEG
    CompressionJP2K
    CompressionLZW
    CompressionAVIF       // NEW: tile bytes are an AVIF image
    CompressionIRIS       // NEW: Iris-proprietary tile codec; opentile-go
                          // reports raw bytes and CAN'T decode them.
                          // Consumers either embed an Iris codec or
                          // serve as 501 / fall back to JPEG/AVIF
                          // tiles when the slide carries them.
)
```

`Compression.String()` gains the two new cases; the existing
`tiffCompressionToOpentile` mapping in each format package is
unchanged (TIFF doesn't use these codes).

**Deviation from the BIF/SVS pattern.** SVS/BIF tiles carry
"abbreviated scan" JPEG bytes that need a JPEGTables splice before
they decode standalone. **IFE tiles are self-contained** — each
tile is a complete JPEG (or AVIF or IRIS) bytestream. No splice
needed; `Tile()` returns raw bytes verbatim.

## 6. Layer ordering inversion

IFE stores layers coarsest-first (per the C++ implementation's
`extents.back().scale` is the highest scale = native resolution).
opentile-go's API is native-first (`Levels()[0]` is the highest
resolution).

**Reader contract**: parse the file's coarsest-first `LAYER_EXTENTS`
array, **reverse it** to produce the native-first slice consumed by
the `Level` interface. Build a `layerCumulative` prefix-sum array
keyed in the FILE's storage order (still coarsest-first) so the
`TILE_OFFSETS` lookup math stays simple:

```go
// Per-slide, computed once at Open time:
layerExtentsFileOrder []LayerExtent  // coarsest at [0], native at [N-1]
layerExtentsAPIOrder  []LayerExtent  // native at [0], coarsest at [N-1]; reverse of above
layerCumulative       []int          // prefix sum keyed in FILE order
//   layerCumulative[i] = sum(x_tiles[k] * y_tiles[k] for k in [0, i)) in FILE order

// API call: levelImpl.Tile(col, row) where this Level's API index is `apiIdx`
fileIdx := len(layerExtentsAPIOrder) - 1 - apiIdx
ext := layerExtentsFileOrder[fileIdx]
linearIdx := layerCumulative[fileIdx] + row*int(ext.XTiles) + col
// ReadAt → tile bytes
```

Pin both directions in unit tests: a 3-layer synthetic IFE where
each layer's tile content is distinguishable confirms (a) `Levels()`
returns them native-first, (b) `Tile(c, r)` on each Level reads the
correct underlying file offsets.

## 7. Parity / correctness strategy

IFE is **bleeding-edge for opentile-go** — there's no Python
analogue (no tifffile-equivalent, no opentile-equivalent) and no
established pixel oracle to cross-validate against. v0.7's
correctness bar (tifffile byte-equality + sample-tile SHAs) doesn't
fully port.

**v0.8/IFE-1.0 correctness bar:**

- **Sample-tile SHA fixtures** (`tests/fixtures/<slide>.ife.json`)
  via `TestSlideParity`. Same shape as the v0.7 BIF fixtures; locks
  in opentile-go's own output across regressions.
- **Synthetic-IFE-writer unit tests.** A minimal-write helper in
  `formats/ife/ife_test.go` constructs a hand-rolled IFE byte
  buffer for a known 1- or 2-layer 2×2 fake slide, then drives the
  reader and asserts metadata + tile bytes match the bytes the
  helper put in. Catches reader bugs without depending on a real
  fixture file.
- **Geometry sanity tests** (`tests/parity/ife_geometry_test.go`,
  no build tag) — per-fixture pinning of level count, dimensions,
  tile count, sparse-tile counts. Mirrors `tests/parity/bif_geometry_test.go`.

**What's deferred (v0.9+):**

- Cross-tool parity vs `tile_server_iris` HTTP output — would
  require a runner that shells out to the Iris server, similar in
  shape to the openslide oracle from v0.7 but cross-language. Not
  critical when the fixtures are committed and SHA-pinned.
- AVIF pixel-decode parity (compare opentile-go's raw AVIF tile
  bytes vs a known decoder). Out of scope: opentile-go is a
  byte-passthrough library; AVIF decode is the consumer's problem.

**Active gap:** the synthetic-writer unit test plus committed
sample-tile SHAs cover regressions in our own output but DON'T cross
us against any external reference. The first divergence story
(opentile-go produces byte X, but consumer Y observes byte Z) we hit
will be debugged from scratch. Acceptable risk for a bleeding-edge
format; flag in CHANGELOG.

## 8. Implementation outline

Mirrors v0.6 / v0.7 plan structure — 5 batches, ~16-20 tasks, each
batch ending with a controller checkpoint.

### Batch A — JIT verification gates (3-4 tasks)

- Confirm magic bytes + endianness on a real fixture (cervix sample
  or one of the locally-encoded `.iris` files).
- Confirm structure offsets (FILE_HEADER size, TILE_TABLE size,
  LAYER_EXTENTS entry size = 12, TILE_OFFSETS entry size = 8).
- Confirm layer ordering — read the file, parse `LAYER_EXTENTS`,
  verify `extents[0].scale < extents[N-1].scale` (coarsest-first
  storage).
- Confirm sparse-tile sentinel value = `0xFFFFFFFFFF` (40-bit all-1s).

### Batch B — Plumbing (3 tasks)

- New `opentile.FormatFactory.SupportsRaw` + `OpenRaw`; default
  impls in a base struct that the existing four factories embed.
- `Compression` enum extension (`CompressionAVIF`, `CompressionIRIS`)
  + String() update.
- Refactor `opentile.Open` to walk `SupportsRaw` before `tiff.Open`.
  Backward-compat test: every existing factory still routes through
  the TIFF path on its existing fixtures.

### Batch C — `formats/ife/` core (4-5 tasks)

- `formats/ife/reader.go` — FILE_HEADER + TILE_TABLE +
  LAYER_EXTENTS + TILE_OFFSETS parsing. `readUint40LE` /
  `readUint24LE` helpers.
- `formats/ife/ife.go` — Factory + `SupportsRaw` (magic-byte sniff)
  + `OpenRaw`.
- `formats/ife/tiler.go` — Tiler + Level impls; Tile() does the
  layer-inversion + prefix-sum lookup + `ReadAt`. Sparse-tile
  detection + `ErrSparseTile`.
- `formats/ife/encoding.go` — IFE Encoding enum →
  `opentile.Compression` mapping (JPEG=2 → JPEG; AVIF=3 → AVIF;
  IRIS=1 → IRIS; UNDEFINED=0 → error).
- Synthetic-writer test helper for unit tests.

### Batch D — Integration + tests (3-4 tasks)

- `tests/integration_test.go` — slideCandidates + IFE fixtures;
  resolveSlide gains `"ife"` subdir; fixtureJSONFor gains
  `.ife → <stem>.ife.json` case.
- `tests/generate_test.go` — sampledByDefault for any IFE fixtures
  over the 100 MB threshold.
- `tests/parity/ife_geometry_test.go` — per-fixture geometry pin.
- Generate sample-tile SHA fixtures via TestGenerateFixtures.

### Batch E — Docs + ship (4 tasks)

- `docs/deferred.md` — register the new R-row (e.g., R18 IFE), §1a
  deviations entries, "Retired in v0.8" subsection.
- `docs/formats/ife.md` — per-format reader notes (mirror
  formats/bif.md template).
- `README.md` — broaden first paragraph; add IFE row to the
  supported-formats table; update deviations list.
- `CHANGELOG.md` `[0.8.0]` entry + `CLAUDE.md` milestone bump.

## 9. Active limitations parked for later milestones

- **METADATA block parsing.** The IFE spec defines a METADATA block
  carrying slide-info (vendor-specific properties, possibly
  associated images). v0.8/IFE-1.0 reads the offset but doesn't
  parse the contents — `Tiler.Metadata()` returns the
  zero-valued `opentile.Metadata{}`. Tracked as a v0.9+ task.
- **Annotations + attributes + associated images.** All defined in
  the v1.0 spec; out of scope for the bench/tile-serving use case
  motivating this milestone. Add when a consumer surfaces.
- **Cipher block.** Reserved for future Iris-Codec features;
  current files have it as `NULL_OFFSET`. Read it; ignore the
  contents.
- **`TILE_ENCODING_IRIS` decoding.** Iris's proprietary tile
  codec. opentile-go reports `CompressionIRIS` and returns raw
  bytes; integrating an Iris codec is the consumer's call. (This
  is the only non-decodable encoding; JPEG and AVIF tiles can be
  decoded by stdlib + libavif respectively.)
- **Spec v2.0 fields.** v2.0 isn't out yet; the C++ source has
  `VERSION CONTROL` markers showing where v2.0 fields would slot
  in. v0.8/IFE-1.0 errors on `extension_major != 1` rather than
  silently misparsing future-format files.
- **Cross-tool parity vs `tile_server_iris`.** §7 above. Useful
  but not load-bearing.

## 10. Open questions for sign-off

| § | Question | Provisional answer |
|---|----------|---------------------|
| 7 | Where do test fixtures come from? | Best path: download Iris's public `cervix_2x_jpeg.iris` from S3 (~2 GB; gitignored), commit a derived JSON fixture only. Fallback: locally-encoded `.iris` files via the user's `bench/regen_iris_fixture.sh`. Either way, fixtures live under `sample_files/ife/` and are gitignored. |
| 8 | Does Batch B's refactor land before IFE work, or together? | Together (Batch B = first task block of the IFE milestone). Splitting feels artificial; the refactor's only consumer is IFE. |
| 9 | Does opentile-go grow an AVIF decoder integration? | **No.** Out of scope. Tile bytes are passthrough; the consumer decodes (libavif via cgo, or `golang.org/x/image/avif` when stdlib gains it). Same model as JPEG/JP2K today — opentile-go never decodes pyramid tiles. |

After sign-off, this becomes the executable spec; a follow-up plan
doc (`docs/superpowers/plans/<date>-opentile-go-ife.md`) lays out
the per-task batches.

## 11. Sign-off log

| Date | § | Decision | Owner |
|------|---|----------|-------|
| 2026-04-29 | 3 | Option B — `FormatFactory.SupportsRaw` (table-driven non-TIFF dispatch) | Toby |
| 2026-04-29 | 4 | README framing widens; package name unchanged | Toby |
| 2026-04-29 | 5 | New `CompressionAVIF` + `CompressionIRIS` enum values | Toby |
| 2026-04-29 | 6 | Layer ordering inverted at parse time | Toby |
| 2026-04-29 | 7 | Sample-tile SHA fixtures + synthetic-writer unit tests; cross-tool parity deferred | Toby |
| 2026-04-29 | 10 Q7 | Cervix-only fixture for v0.8; no regen tooling | Toby |
| 2026-04-29 | 10 Q8 | Plumbing refactor + IFE ship together as v0.8.0 | Toby |
| 2026-04-29 | 10 Q9 | No AVIF decoder integration; byte-passthrough preserved | Toby |
