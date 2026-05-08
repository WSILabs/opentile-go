# opentile-go v0.13 — TilePrefix + TileBodyInto

**Status:** sealed 2026-05-07.
**Work branch:** `feat/v0.13`.
**Headline:** small additive-API milestone exposing the JPEG splice prefix and on-disk tile body bytes separately, so client-server consumers can deduplicate the per-level prefix across tiles. Motivated by personal-viewer profiling work (compare bandwidth patterns: full-JPEG-per-tile vs prefix-once-plus-body-per-tile).

## 1. Scope

Three additions, all to the JPEG-with-shared-JPEGTables case:

### 1.1. `Level.TilePrefix() []byte`

Returns the constant per-level JPEG-table prefix bytes that get spliced into every tile's payload to form a complete JPEG. Specifically the bytes derived from `internal/jpeg.BuildSplicePrefix(jpegTables, includeAPP14)` — the existing v0.9 cached splice prefix, exposed.

`nil` for levels where no splice applies:
- Non-JPEG compressions (LZW, JP2K, Deflate, None)
- JPEG compressions without shared JPEGTables (e.g., NDPI OneFrame, IFE, Ventana-1 BIF per-tile-embedded)
- NDPI stripped levels (use a different prefix model — patched JPEG header — and don't fit this API; their full `Tile()` output continues to work, just without the deduplication benefit)

### 1.2. `Level.TileBodyInto(x, y int, dst []byte) (int, error)`

Reads the on-disk tile bytes (the "body" — what gets spliced with TilePrefix to form a complete JPEG) into `dst`. Returns the number of bytes written.

Buffer-sizing contract:
- Caller pre-allocates `dst` to at least `Level.TileBodyMaxSize()`.
- For levels where `TilePrefix() != nil` (splice path): `body bytes < Tile() bytes` because no splice has been applied.
- For levels where `TilePrefix() == nil` (non-splice path): `TileBodyInto` is functionally equivalent to `TileInto` — body bytes ARE the full tile output. Provided for API uniformity (consumers can always call `TileBodyInto`; only need to query `TilePrefix() != nil` to decide deduplication strategy).

This implies a fourth addition: `Level.TileBodyMaxSize() int`.

### 1.3. `opentile.SpliceJPEGTile(prefix, body []byte) ([]byte, error)` helper

Top-level public function reconstituting a complete JPEG from a level's `TilePrefix()` bytes and one tile's `TileBodyInto()` output. Inserts the prefix at the on-disk tile's SOS boundary (matching what `internal/jpeg.InsertPrefixInPlace` does internally on the server side).

Algorithm:
1. Find SOS marker (`0xFF 0xDA`) in `body`.
2. Output = `body[0:sosIdx] + prefix + body[sosIdx:]`.
3. Return error if SOS not found in `body`.

Edge cases:
- `prefix == nil` (non-applicable level): return `body` verbatim (no splice needed; body is already a complete tile).
- `body == nil` or empty: return error.

The algorithm is documented inline so non-Go consumers (e.g., a JavaScript web viewer) can reimplement it.

### 1.4. Bench harness

`tests/parity/tilebody_bench_test.go` measuring on representative SVS/Philips/OME/leicascn fixtures:
- Pattern A: total bytes shipped using `TileInto()` per tile across full level walk.
- Pattern B: total bytes shipped using `TilePrefix()` once + `TileBodyInto()` per tile.

Output: per-fixture `bytes/tile` and total-level bandwidth comparison. Run via `make bench` (or under `-tags benchgate` like v0.9's perf baseline).

Provides regression protection going forward: if a future refactor breaks the prefix-deduplication pattern, the bench gate fires.

## 2. Out of scope

- **`TileBorrow` zero-copy mmap aliasing.** Different axis (memory pattern, not network bandwidth); doesn't help the profiling story. Stays parked.
- **NDPI stripped prefix exposure.** NDPI's per-level patched JPEG header is conceptually a prefix, but it splices via simple-prepend (not SOS-boundary insertion) and the bandwidth savings are marginal (~2% per stripe). Out of scope; revisit if profiling shows it matters.
- **Alloc-form `TileBody(x, y) ([]byte, error)`.** Per Q1 sealed: only `TileBodyInto` ships. Consumers wanting alloc-per-call write a 5-line wrapper.
- **v1.0 cut.** Not committed. v0.13 is additive (no breaking changes), so v1.0 ceremony stays parked.
- **NDPI OneFrame / IFE prefix exposure.** These formats build per-tile self-contained outputs internally; no shared prefix to expose.
- **Format-aware splice modes.** Single splice mode shipped (SOS-boundary insertion). NDPI-style simple-prepend not supported in `SpliceJPEGTile`. If a consumer needs that, they can splice manually.

## 3. API surface

### 3.1. `Level` interface additions

```go
type Level interface {
    // ... existing v0.7-v0.12 methods

    // TilePrefix returns the constant per-level JPEG splice prefix
    // bytes. When non-nil, callers can ship the prefix once per level
    // + per-tile TileBodyInto output, then reconstitute the full
    // JPEG on the client side via opentile.SpliceJPEGTile. When nil,
    // no splice prefix exists for this level — TileBodyInto returns
    // the same bytes as TileInto.
    //
    // Use case: bandwidth-efficient client-server tile transfer.
    // SVS / Philips / OME / leicascn / generictiff levels with
    // shared JPEGTables typically have ~1 KB of prefix per level
    // applied to 100k+ tiles per slide; deduplicating saves ~100 MB
    // bandwidth per slide.
    //
    // Added in v0.13. Returns nil on existing-API consumers' Level
    // implementations until they explicitly opt in.
    TilePrefix() []byte

    // TileBodyInto writes the on-disk tile bytes (the "body" — what
    // gets spliced with TilePrefix to form a complete JPEG) into
    // dst. Returns the number of bytes written.
    //
    // For levels where TilePrefix() returns nil (non-splice path),
    // TileBodyInto is functionally equivalent to TileInto.
    //
    // Caller must size dst to at least TileBodyMaxSize().
    //
    // Added in v0.13.
    TileBodyInto(x, y int, dst []byte) (int, error)

    // TileBodyMaxSize returns the upper bound on TileBodyInto output
    // size across all tiles in this level. For levels with shared
    // JPEGTables (TilePrefix() != nil), this is strictly less than
    // TileMaxSize() (no splice prefix added). For levels without
    // shared JPEGTables (TilePrefix() == nil), equal to TileMaxSize().
    //
    // Added in v0.13.
    TileBodyMaxSize() int
}
```

### 3.2. Top-level helper

```go
// SpliceJPEGTile reconstitutes a complete JPEG from a level's
// TilePrefix bytes and one tile's TileBodyInto output. Inserts the
// prefix at the on-disk tile's SOS boundary (the same operation
// opentile-go does internally during Tile/TileInto).
//
// Returns body verbatim if prefix is empty / nil — degenerate case
// for levels without splice (e.g., non-JPEG compressions).
//
// Returns an error if body is empty or doesn't contain an SOS
// marker. SOS = 0xFF 0xDA per JPEG spec.
//
// Algorithm (documented for non-Go consumers):
//
//   1. Find offset of the first `0xFF 0xDA` byte sequence in body
//      ("Start of Scan" marker).
//   2. Output = body[0:sosIdx] + prefix + body[sosIdx:]
//   3. Done.
//
// Added in v0.13.
func SpliceJPEGTile(prefix, body []byte) ([]byte, error)
```

### 3.3. Behavior matrix per format

| Format / level type | `TilePrefix()` | `TileBodyInto` returns | `TileBodyMaxSize()` |
|---|---|---|---|
| SVS pyramid level | DQT+DHT+APP14 | on-disk tile (SOI...SOS...EOI) | < TileMaxSize |
| Philips pyramid level (with JPEGTables) | DQT+DHT | on-disk tile | < TileMaxSize |
| OME pyramid level (with JPEGTables) | DQT+DHT | on-disk tile | < TileMaxSize |
| BIF OS-1 (shared JPEGTables) | DQT+DHT | on-disk tile | < TileMaxSize |
| BIF Ventana-1 (per-tile-embedded JPEGTables) | nil | full tile (SOI...EOI) | == TileMaxSize |
| leicascn pyramid level | DQT+DHT | on-disk tile (per-region; one or N regions composited) | < TileMaxSize |
| generictiff JPEG-with-shared-tables | DQT+DHT (+APP14 if SVS-style) | on-disk tile | < TileMaxSize |
| generictiff JPEG-without-shared-tables | nil | full tile | == TileMaxSize |
| generictiff JP2K / LZW / Deflate / None | nil | full tile | == TileMaxSize |
| NDPI stripped levels | nil (different prefix model; out of scope) | full assembled stripe JPEG | == TileMaxSize |
| NDPI OneFrame levels | nil | full DCT-cropped JPEG | == TileMaxSize |
| IFE levels | nil | full per-tile native codec bytes | == TileMaxSize |

**Multi-region leicascn blank-fill tiles** (sealed Q4): inter-region "gap" tiles in Leica-2's composite levels are synthesised full self-contained JPEGs (their own SOI + DQT/DHT + SOS + EOI). They don't conform to the split-prefix-and-body invariant — the prefix bytes are already inside the body. `SpliceJPEGTile` still produces a structurally valid JPEG when applied to a blank-fill body (it inserts redundant DQT/DHT, which a JPEG decoder tolerates), but the result is wasteful. Bandwidth savings on these tiles are negligible because blank tiles are small (~few hundred bytes) and infrequent. Documented behavior; no per-tile signaling.

## 4. Test strategy

### 4.1. Per-format unit tests

For each format, on at least one fixture, assert byte-equality:

```go
func TestTileBodyAndPrefixReconstituteFullJPEG(t *testing.T) {
    // Open fixture; for L0 (0,0):
    fullViaTile := tiler.Levels()[0].Tile(0, 0)         // existing path
    prefix := tiler.Levels()[0].TilePrefix()
    bodyBuf := make([]byte, tiler.Levels()[0].TileBodyMaxSize())
    n, _ := tiler.Levels()[0].TileBodyInto(0, 0, bodyBuf)
    reconstituted, _ := opentile.SpliceJPEGTile(prefix, bodyBuf[:n])

    if !bytes.Equal(fullViaTile, reconstituted) {
        t.Errorf("reconstituted tile != Tile() output")
    }
}
```

This is the correctness invariant: the v0.13 split API's reconstituted output is byte-identical to the existing `Tile()` API's spliced output.

Coverage: SVS, Philips, OME, BIF (OS-1 shared + Ventana-1 per-tile), generictiff (CMU-1.tiff has shared tables; CMU-1.stripped.tiff inherits), leicascn (Leica-1 single-region + Leica-2 multi-region), NDPI (stripped + OneFrame), IFE.

### 4.2. Bench harness

`tests/parity/tilebody_bench_test.go` (build tag `bench` per project convention):

```go
func BenchmarkTilePatternsACrossSlides(b *testing.B) {
    // For each fixture in {CMU-1.svs, Philips-1.tiff, Leica-1.ome.tiff, Leica-1.scn}:
    //   Pattern A: sum of TileInto bytes across full L0 walk
    //   Pattern B: TilePrefix bytes + sum of TileBodyInto bytes across full L0 walk
    //   Report: total bytes A, total bytes B, ratio B/A, savings %
}
```

Captures the bandwidth-savings story per representative format.

### 4.3. SpliceJPEGTile unit tests

Pure function tests (no fixture dependency):
- Splice with valid prefix + valid body → verify output starts with SOI, contains prefix at SOS boundary, ends with EOI.
- Splice with nil prefix → returns body verbatim.
- Splice with nil/empty body → error.
- Splice with body lacking SOS marker → error.

## 5. Sealed Q-decisions

| ID | Question | Decision |
|---|---|---|
| Q1 | Alloc-form TileBody too, or zero-alloc only? | Zero-alloc only (TileBodyInto + TileBodyMaxSize) |
| Q2 | Splice helper as public function? | Yes — `opentile.SpliceJPEGTile` |
| Q3 | Bundle bench harness in milestone? | Yes — `tests/parity/tilebody_bench_test.go` |
| Q4 | Multi-region leicascn blank-fill tiles | Documented as wasteful-but-valid: blank-fill bodies are self-contained JPEGs; `SpliceJPEGTile` produces a redundantly-prefixed JPEG that's still valid |

## 6. Active limitations introduced

None. v0.13 is additive — three new public API surfaces (`Level.TilePrefix`, `Level.TileBodyInto`, `Level.TileBodyMaxSize`, plus `opentile.SpliceJPEGTile`); no breaking changes; no behavior changes to existing `Tile`/`TileInto` paths.

The `Level` interface gains 3 methods. Existing format implementations (SVS, NDPI, Philips, OME, BIF, IFE, generictiff, leicascn) all need to implement them. Default behavior for non-applicable levels: `TilePrefix() == nil`, `TileBodyInto == TileInto`, `TileBodyMaxSize() == TileMaxSize()`.

## 7. Plan outline

Single-batch plan tentatively scoped at 6 tasks:

- **T1**: `Level` interface additions in `image.go` + `opentile.SpliceJPEGTile` top-level helper. Pure interface evolution + public utility.
- **T2**: Implement `TilePrefix` / `TileBodyInto` / `TileBodyMaxSize` on splice-format Levels (SVS, Philips, OME tiled, leicascn, generictiff JPEG-with-tables, BIF when shared tables).
- **T3**: Implement defaults on non-splice Levels (NDPI stripped/OneFrame, IFE, BIF Ventana-1, generictiff non-JPEG, generictiff JPEG-without-tables, leicascn blank-fill behavior).
- **T4**: Per-format byte-equality unit tests (the reconstituted-tile-equals-full-Tile invariant).
- **T5**: `tests/parity/tilebody_bench_test.go` + `make bench` integration.
- **T6**: Docs (`docs/perf.md` addendum on Pattern A vs Pattern B; CHANGELOG `[0.13.0]`; CLAUDE.md milestone bump; deferred.md §8g retirement audit).

Plan written separately at `docs/superpowers/plans/2026-05-07-opentile-go-v13-tile-prefix.md`.

## 8. Verification

End-of-milestone gates:
- `go vet ./...` clean.
- `make test` green.
- `make parity` green (no behavioral regressions in existing `Tile`/`TileInto` paths).
- `make bench` produces a report showing Pattern B saves >50% bandwidth on slides with deep pyramids and shared JPEGTables (representative SVS/Philips/OME).
- Per-format unit tests pass: `TileBodyInto + TilePrefix → SpliceJPEGTile == Tile()` byte-identical across the slate.
