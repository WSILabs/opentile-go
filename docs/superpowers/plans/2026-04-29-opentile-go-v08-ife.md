# opentile-go v0.8 IFE Implementation Plan

> **For agentic workers:** Sequential in-thread execution per recent
> v0.7 closeout precedent (the user is on remote control). Each task
> ends with a commit; batch boundaries are controller checkpoints.
> Tasks use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land **Iris File Extension (IFE) v1.0** read support — the
first non-TIFF format opentile-go handles. Ships under
`formats/ife/`, ~520 LoC of reader code per the upstream spec.
Includes the format-dispatch refactor (`FormatFactory.SupportsRaw`
+ `OpenRaw` + `Open` reorder) and two new `Compression` enum values
(`CompressionAVIF`, `CompressionIRIS`).

**Architecture:** Non-TIFF dispatch via additive
`FormatFactory.SupportsRaw` (existing four factories embed a base
struct returning `false`/`ErrUnsupportedFormat`). IFE reader is
mechanical — magic-byte sniff → FILE_HEADER (38 B) → TILE_TABLE
(44 B) → LAYER_EXTENTS + TILE_OFFSETS arrays → `ReadAt(offset, size)`
per tile. Layer ordering inverted at parse time (file is
coarsest-first; API is native-first). Tile bytes are self-contained
(no JPEGTables splice). Sparse tiles (`offset == 0xFFFFFFFFFF`)
return `ErrSparseTile`. No AVIF decoder; opentile-go remains
byte-passthrough.

**Tech Stack:** Go 1.23+, no new cgo. libjpeg-turbo footprint
unchanged.

**Spec:** [`docs/superpowers/specs/2026-04-29-opentile-go-ife-design.md`](../specs/2026-04-29-opentile-go-ife-design.md)
(sealed 2026-04-29; §11 sign-off log records eight locked-in
decisions).

**Reference plan:** [`sample_files/ife/ife-format-spec-for-opentile-go.md`](../../../sample_files/ife/ife-format-spec-for-opentile-go.md)
— byte-layout + offsets distilled from the upstream Iris-File-Extension repo.

**Upstream (read-for-understanding only):**
[`IrisDigitalPathology/Iris-File-Extension`](https://github.com/IrisDigitalPathology/Iris-File-Extension)
(MIT) and [`Iris-Headers`](https://github.com/IrisDigitalPathology/Iris-Headers)
(format type definitions). Spec doc itself is CC BY-ND 4.0 — port
the layout, don't reproduce the prose.

**Branch:** `feat/v0.8` — fresh branch off `main` post-v0.7 merge.

**Sample slides:** One real fixture committed for SHA-pinning:

- `sample_files/ife/cervix_2x_jpeg.iris` (2.16 GB, JPEG-encoded IFE
  v1.0) — downloaded from
  `irisdigitalpathology.s3.us-east-2.amazonaws.com/example-slides/`
  with SHA256 `b080859913d2ebbb20e33124ba231c0fed7b2ffd10ce87aea912819ec44ca743`.
  Gitignored (matches the existing pattern for `sample_files/*`).

**Python venv:** N/A. IFE has no Python parity oracle (per §7
sign-off). Coverage is `TestSlideParity` SHA hashes + synthetic-IFE
unit tests + per-fixture geometry pinning.

---

## Universal task contract: "confirm upstream first"

Every task starts with `Step 0: Confirm upstream` — names the
upstream rule that governs the behavior, states it, includes a
verification command. No task body proceeds until that command has
been run.

For IFE, "upstream" sources are layered:

1. **`docs/superpowers/specs/2026-04-29-opentile-go-ife-design.md`** —
   the project-internal design spec, sealed 2026-04-29. **Cite this
   first**; §11 records the eight locked-in decisions.
2. **`sample_files/ife/ife-format-spec-for-opentile-go.md`** — the
   byte-layout reference distilled from upstream. Specific offsets
   and field sizes live here.
3. **`Iris-Headers/include/IrisCodecTypes.hpp` + `IrisTypes.hpp`** in
   the upstream repo — enum values + struct layouts. Read for
   understanding; never copy verbatim (CC BY-ND 4.0 on the spec doc
   itself; MIT on the headers permits the port).
4. **`cervix_2x_jpeg.iris`** — the real fixture. When in doubt about
   a field's layout, dump bytes and confirm against the spec.

When upstream is unclear, prefer **reading the byte layout in the
real fixture** over guessing.

---

## Batch A — JIT verification gates (4 tasks)

**Goal:** before writing production code, prove every layout
assumption against the real cervix fixture. Each gate writes a
small `_test.go` probe (build-tag `//go:build gates`) under
`formats/ife/internal/gates/` (deleted at end of milestone) that
parses one structural field and prints findings.

- [ ] **T1 — Magic bytes + endianness gate.**
  Read the first 16 bytes of `cervix_2x_jpeg.iris`; confirm magic
  bytes match the upstream constant (per spec doc) and confirm
  little-endian byte order. Output: bytes-as-hex + matched-against
  log line. Commit the probe so the next gate task can extend it.

- [ ] **T2 — Structure offsets gate.**
  From `cervix_2x_jpeg.iris`: parse FILE_HEADER (38 B), confirm
  fields land at expected offsets (`extension_major == 1`,
  `tile_table_offset` non-zero, etc.). Probe: print every header
  field with offset + parsed value. Confirm against spec.

- [ ] **T3 — Layer ordering gate.**
  Parse `LAYER_EXTENTS` from the cervix fixture; confirm
  `extents[0].scale < extents[N-1].scale` (coarsest-first storage
  per §6 sealed decision). Print every layer's `(scale, x_tiles,
  y_tiles)` triple. **Critical** — if extents are native-first
  instead, §6's inversion logic is wrong and we replan.

- [ ] **T4 — Sparse-tile sentinel + TILE_OFFSETS sample gate.**
  Parse the first ~100 entries of TILE_OFFSETS; print any that
  match the sparse sentinel (`0xFFFFFFFFFF`); confirm 40-bit-offset
  + 24-bit-size encoding aligns with the spec. Cervix is
  fully-tiled (no real sparse entries expected) but the probe must
  not crash on absence.

End-of-batch checkpoint: review all four gate outputs, confirm no
surprises, then green-light Batch B.

---

## Batch B — Plumbing refactor (3 tasks)

**Goal:** add the non-TIFF dispatch infrastructure + new
`Compression` enum values. Backward-compat-tested against every
existing fixture.

- [ ] **T5 — `Compression` enum extension.**
  Add `CompressionAVIF` and `CompressionIRIS` constants to
  `compression.go`; extend `Compression.String()`. Pin in
  `compression_test.go`. No format package consumes them yet
  (added in T9 for IFE). The existing `tiffCompressionToOpentile`
  mapping in each format package is unchanged — TIFF doesn't use
  these codes.

- [ ] **T6 — `FormatFactory.SupportsRaw` + `OpenRaw` interface
  evolution.**
  Add the two methods to `formats.FormatFactory`. Provide a
  `RawUnsupported` zero-impl base struct (returns
  `false` / `ErrUnsupportedFormat`) that the existing four factories
  (SVS, NDPI, Philips, OME, BIF) embed. **Five factories**, since
  v0.7 added BIF — confirm before editing. The existing
  `Supports(*tiff.File)` + `Open(*tiff.File, *Config)` methods
  are unchanged. New `ErrUnsupportedFormat` sentinel.

- [ ] **T7 — `opentile.Open` dispatch reorder.**
  Update `opentile.Open` to walk `SupportsRaw` *before* `tiff.Open`.
  Loop:
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
  Backward-compat regression test: every existing fixture from
  `tests_test.go` still routes through the TIFF path and produces
  the same Tiler. Run `make test` — must pass green before
  proceeding.

End-of-batch checkpoint: `make test` clean across all 17 packages.

---

## Batch C — `formats/ife/` core (5 tasks)

**Goal:** the IFE reader itself — magic detection through tile
read.

- [ ] **T8 — `formats/ife/reader.go` — byte-layout primitives.**
  Pure parsing: `readUint40LE`, `readUint24LE`,
  `readFileHeader(io.ReaderAt) (FileHeader, error)`,
  `readTileTable(...)`, `readLayerExtents(...)`,
  `readTileOffsets(...)`. No Tiler / Level types yet — just byte
  parsers with table-driven unit tests using a synthetic byte
  buffer. **Step 0**: confirm against the cervix fixture via a
  one-shot debug print before the unit tests. Coverage target ≥80%.

- [ ] **T9 — `formats/ife/encoding.go` — Encoding enum mapping.**
  Mirror Iris's `IrisCodecTypes.hpp`:
  - `TILE_ENCODING_UNDEFINED` (0) → return `ErrUnsupportedFormat`.
  - `TILE_ENCODING_IRIS` (1) → `CompressionIRIS`.
  - `TILE_ENCODING_JPEG` (2) → `CompressionJPEG`.
  - `TILE_ENCODING_AVIF` (3) → `CompressionAVIF`.
  Unit test pins each mapping; future additions error.

- [ ] **T10 — `formats/ife/ife.go` — Factory + `SupportsRaw` magic
  sniff + `OpenRaw`.**
  Sniff the cervix file's magic bytes (per T1's findings); return
  `true` only on full match. `OpenRaw` allocates the Tiler (next
  task) — for now stub `return nil, errors.New("not implemented")`
  if Tiler isn't ready. Register in `formats/all/all.go`.

- [ ] **T11 — `formats/ife/tiler.go` — Tiler + Level impls.**
  Tiler reads FILE_HEADER + TILE_TABLE + LAYER_EXTENTS +
  TILE_OFFSETS once at Open time; builds the file-order vs
  api-order layer extent slices + the `layerCumulative` prefix-sum
  array (per §6 sealed decision). `Level.Tile(col, row)` does:
  ```go
  fileIdx := len(layerExtentsAPIOrder) - 1 - apiIdx
  ext := layerExtentsFileOrder[fileIdx]
  linearIdx := layerCumulative[fileIdx] + row*int(ext.XTiles) + col
  off := tileOffsets[linearIdx].Offset
  size := tileOffsets[linearIdx].Size
  if off == sparseSentinel {
      return nil, opentile.ErrSparseTile
  }
  buf := make([]byte, size)
  _, err := r.ReadAt(buf, int64(off))
  return buf, err
  ```
  Tile size is fixed at 256×256 per spec; level dimensions derive
  from `(x_tiles, y_tiles)` in each extent.
  `TileAt(TileCoord{X, Y, Z, C, T})` rejects non-zero Z/C/T with
  `ErrDimensionUnavailable` (2D-only format; same delegate
  pattern as SVS/NDPI/Philips/OME). Tile bytes are self-contained
  — no JPEGTables splice. New `ErrSparseTile` sentinel in
  `errors.go` if the existing `ErrTileOutOfBounds` doesn't fit.

- [ ] **T12 — `formats/ife/synthetic_test.go` — synthetic IFE
  writer + reader unit tests.**
  Hand-rolled IFE byte buffer for a known 2-layer 2×2 fake slide
  (no real codec needed; tile bytes are arbitrary recognizable
  patterns like `[]byte{0xAA, 0xAA}`). Drive the reader; assert
  layer ordering inversion, prefix-sum math, sparse-tile path,
  and tile bytes match what the writer put in. **Catches reader
  bugs without depending on the cervix file.** Coverage target
  drives `formats/ife` ≥80%.

End-of-batch checkpoint: `make test` clean; `make cover` ≥80%
on `formats/ife`. Cervix fixture not yet exercised in `make test`
(integration plumbing is Batch D).

---

## Batch D — Integration + parity fixtures (3 tasks)

**Goal:** wire IFE into the existing test infrastructure so
`make test` exercises the cervix fixture.

- [ ] **T13 — `tests/integration_test.go` — IFE fixture wiring.**
  Add `"ife"` to `slideCandidates`; extend `resolveSlide` to map
  the `ife` subdir; extend `fixtureJSONFor` for `.iris →
  <stem>.ife.json`. Add `"cervix_2x_jpeg.iris"` to the fixture
  list. The existing `TestSlideParity` shape Just Works once the
  JSON exists.

- [ ] **T14 — `tests/parity/ife_geometry_test.go` — geometry
  pinning.**
  No build tag; runs in `make test` when `OPENTILE_TESTDIR` is
  set. Pin level count, per-level dimensions, per-level tile
  count, sparse-tile count for the cervix fixture. Mirror
  `tests/parity/bif_geometry_test.go` shape.

- [ ] **T15 — Generate `tests/fixtures/cervix_2x_jpeg.ife.json`.**
  Run `tests/generate_test.go` with `-tags generate -generate -v`
  for the IFE fixture. Sample a deterministic ~10 tiles per level
  via the existing seeded-RNG harness; commit the JSON.
  `TestSlideParity` then locks in opentile-go's own output across
  regressions.

End-of-batch checkpoint: `make test` exercises `cervix_2x_jpeg.iris`
through `TestSlideParity` + `ife_geometry_test`. `make cover` still
≥80% per package. Coverage on `formats/ife` should be ≥85% with
both synthetic + real-fixture drives.

---

## Batch E — Docs + ship (4 tasks)

**Goal:** ship-ready docs.

- [ ] **T16 — `docs/deferred.md` updates.**
  - §1a Deviations: add an IFE-specific row if any (probably none —
    IFE has no upstream Python opentile to deviate from). Add a
    "non-TIFF dispatch path via `SupportsRaw`" row as
    architectural deviation worth pinning.
  - §2 Active limitations: add new IFE-specific rows. Likely
    candidates: METADATA block parsing deferred (v0.9+), AVIF
    decoder integration declined (passthrough only), Iris
    proprietary codec undecodable (consumer responsibility),
    cross-tool parity vs `tile_server_iris` deferred.
  - §8b (or new section): v0.8 retirement audit. Mirror v0.7's §8a
    structure.

- [ ] **T17 — `docs/formats/ife.md` — per-format reader notes.**
  Mirror `docs/formats/bif.md` template. Capability matrix,
  deviations from upstream Iris-File-Extension (probably "none —
  passthrough byte read"), implementation references, known issues
  + history.

- [ ] **T18 — `README.md` updates.**
  - Opening line broadens: "WSI files, including TIFF dialects and
    Iris IFE."
  - Supported-formats table gains an IFE row.
  - Detection paragraph gains a sentence about
    `SupportsRaw`-before-`tiff.Open` dispatch order.
  - Deviations table: add the architectural row from T16 for
    visibility.

- [ ] **T19 — `CHANGELOG.md [0.8.0]` entry + `CLAUDE.md` milestone
  bump.**
  - CHANGELOG: new `[0.8.0]` heading with Added (IFE format,
    `FormatFactory.SupportsRaw`/`OpenRaw`, `CompressionAVIF`/`IRIS`,
    `ErrSparseTile`, `ErrUnsupportedFormat`), Changed
    (`opentile.Open` dispatch order — additive, no caller break),
    Deferred (METADATA, cross-tool parity, AVIF decoder).
  - CLAUDE.md: milestone bump v0.7 → v0.8 with new scope, active
    limitations, deviations, sample slides (cervix), branch.

End-of-batch checkpoint: final validation sweep — `make vet`,
`make test`, `make cover`, branch state clean. Hand back for tag
+ merge + release.

---

## Risk notes

- **Magic bytes at T1 might not match the spec's claim.** The
  upstream byte-layout doc is a project-internal mirror, not the
  authoritative C++ source. If T1 fails, walk the cervix file's
  first 64 bytes and align the spec to reality before T2 onward.

- **Layer ordering at T3 is the highest-risk gate.** §6's
  inversion is the most invasive design choice; if cervix stores
  native-first instead of coarsest-first, `Levels()` and
  `layerCumulative` flip. Cheap to verify, expensive to discover late.

- **Sparse-tile sentinel at T4.** Spec says `0xFFFFFFFFFF` (40-bit
  all-1s). Cervix may not have any sparse entries — the probe
  must handle absence gracefully (don't error if zero matches).

- **Backward-compat at T7** is non-negotiable. If any existing
  fixture changes routing under the new dispatch, that's a bug in
  the refactor, not a fixture issue. `make test` green is the gate.

- **Coverage gate** trips at ≥80% per package; `formats/ife` is a
  new package so synthetic-writer tests + real-fixture drives both
  matter.

---

## Out of scope for v0.8

Per the design spec §9:

- METADATA block parsing — `Tiler.Metadata()` returns zero-valued.
- Annotations + attributes + associated images.
- Cipher block (currently `NULL_OFFSET` in all known files).
- `TILE_ENCODING_IRIS` decoding — reported but not decoded.
- Spec v2.0 fields — error on `extension_major != 1`.
- Cross-tool parity vs `tile_server_iris` HTTP output.
- AVIF decoder integration — consumer's call.

These surface as v0.9+ candidates if a real consumer hits them.
