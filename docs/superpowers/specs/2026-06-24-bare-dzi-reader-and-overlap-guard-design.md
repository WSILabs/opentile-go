# Bare DZI Reader + Overlap>0 Guard — Design

**Status:** Draft for review (no code written)
**Date:** 2026-06-24
**Scope:** (1) a filesystem-backed bare Deep Zoom Image (DZI) reader, `Overlap=0`
only (closes the long-parked R19); (2) a shared `Overlap>0` rejection guard that
makes both the new DZI reader *and* the existing SZI reader fail loudly instead
of silently mis-rendering. Full `Overlap>0` support is **explicitly deferred** to
a separate design conversation.

---

## Goal

Read bare DZI slides (`<name>.dzi` manifest + sibling `<name>_files/<level>/<col>_<row>.<ext>`
tile tree) as opentile-go's 12th format, reusing the existing `internal/dzi`
pyramid math and following the DICOM multi-file open model. Simultaneously close
a latent correctness footgun: SZI currently parses `Overlap` but ignores it,
silently mis-rendering any `Overlap>0` slide; a shared guard converts that into a
clear error in both readers.

## Background

`internal/dzi` already exists (pure manifest parse + pyramid/tile-coordinate
math: `ParseManifest`, `MaxLevel`, `LevelDims`, `GridDims`, `TilePath`) and
backs the ZIP-wrapped SZI reader (`formats/szi`). R19 (bare DZI) has been parked
since v0.16 with "no consumer signal"; `internal/dzi` was deliberately built to
pre-pare it.

**The `Overlap` situation (grounded in code):** DZI `Overlap` is *redundant
border pixels in storage* — a tile stores its `TileSize` core plus `Overlap`
extra pixels on each edge that has a neighbor. The *output* grid stays clean
(`Grid` tiles `Size`, `Overlapping=false`); reconstructing the image means
*cropping* each tile's border, not stitching overlapping tiles (this is unlike
BIF). Today `formats/szi/level.go:61-64` hardcodes `TileOverlap()={0,0}` and
`manifest.Overlap` is parsed but dropped, so an `Overlap>0` SZI is silently
treated as `Overlap=0` → every interior tile mis-placed by `Overlap` px. Every
committed SZI/DZI fixture is `Overlap=0`, so this is latent, not observed — but
it is a silent-corruption trap. Per the owner's steer, we ship `Overlap=0`
support now and a loud guard for `Overlap>0`, and defer the crop/placement design
for `Overlap>0` to a later conversation (the owner may have opinions on the
model).

## Open model (consistent with DICOM)

DICOM (`open.go:136-167`) uses a path-aware hook consulted in `OpenFile` before
single-file content dispatch: the hook takes a path (directory → opens the
series; or a single `.dcm` → expands to its sibling series), and returns
`ErrUnsupportedFormat` to fall through. Bare DZI mirrors this exactly:

- A `dziPathOpenHook` (package-level var in root `opentile`, set by
  `formats/dzi.init()`), consulted in `OpenFile` **after** the DICOM hook and
  **before** single-file dispatch.
- The hook accepts:
  - a **`.dzi` file path** (primary; the OpenSeadragon convention) — reads the
    manifest from that file, tiles from `<dir>/<base>_files/`;
  - a **directory containing exactly one `*.dzi`** (convenience, mirroring
    DICOM's file-or-dir flexibility) — locates the single `.dzi` within.
  - Anything else → `ErrUnsupportedFormat` (fall through to normal dispatch).
- `Open(io.ReaderAt)` does **not** support bare DZI (no path to locate tiles),
  exactly like DICOM. (A `.dzi` handed to content dispatch matches no format and
  would fail `ErrUnknownFormat` — acceptable; bare DZI is a path format.)

## Architecture / components

```
internal/dzi/errors.go         NEW   ErrOverlapNotSupported sentinel (shared)
internal/dzi/                  reuse  ParseManifest/MaxLevel/LevelDims/GridDims/TilePath (unchanged)
formats/dzi/factory.go         NEW   init(): install dziPathOpenHook; path detection
formats/dzi/tiler.go           NEW   Tiler: parse manifest, Overlap guard, buildLevels, FS dir state
formats/dzi/level.go           NEW   level: FS tile fetch via os.ReadFile(TilePath)
formats/szi/tiler.go           MOD   apply dzi.ErrOverlapNotSupported guard at loadManifest
open.go                        MOD   dziPathOpenHook var + OpenFile consult (after dicom hook)
format.go                      MOD   FormatDZI = "dzi"
```

### Tile reconstruction (Overlap=0 only)

With `Overlap=0`, a stored tile *is* exactly its core (`TileSize`, clamped at the
far edges), placed at `(col*TileSize, row*TileSize)` on a clean grid. So the DZI
`level` is the filesystem analogue of the SZI `level`: identical pyramid math
(`internal/dzi`), identical clean grid, only the *byte source* differs (ZIP entry
→ `os.ReadFile`). `Tile(x,y)` returns the raw on-disk JPEG/PNG bytes verbatim;
`DecodedTile`/`ReadRegion`/`ScaledStrips` decode through the normal pipeline
unchanged (tiles are already `TileSize`).

### The Overlap>0 guard

`internal/dzi/errors.go`:
```go
// ErrOverlapNotSupported is returned at open when a DZI manifest declares
// Overlap > 0. Only Overlap=0 is implemented; tile-border cropping for
// Overlap > 0 is deferred. Both formats/dzi and formats/szi enforce this.
var ErrOverlapNotSupported = errors.New("dzi: tile overlap > 0 not supported")
```
Applied immediately after `ParseManifest` in both readers:
```go
if m.Overlap > 0 {
    return fmt.Errorf("...: Overlap=%d: %w", m.Overlap, dzi.ErrOverlapNotSupported)
}
```
For SZI this is a **behavior change only for `Overlap>0` inputs** (which today
mis-render) — it turns silent corruption into a clear open-time error. `Overlap=0`
SZI (all current fixtures, all current consumers) is byte-for-byte unaffected.

### Metadata / associated images

A bare `.dzi` carries only the manifest. `Metadata()` returns an effectively
empty `opentile.Metadata{}` (Format-derived fields only); `AssociatedImages()`
returns `nil`. (No `scan-properties.xml`, no `associated_images/`.) MPP/scanner
identity are absent by construction.

### Sparse tiles

DZI is normally dense, but some exporters omit fully-blank tiles. v1 follows the
SZI convention: a missing tile within the addressable grid is an error
(`TileError` wrapping a `dzi`-package sentinel), not a blank fill. A real
sparse-DZI fixture (none today) would motivate an opt-in lenient mode later.

## Error handling / edge cases

- `Overlap>0` → `ErrOverlapNotSupported` at open (both readers).
- `.dzi` present but `<base>_files/` missing → open error (clear message).
- Missing individual tile in-range → `TileError` (dense-DZI assumption).
- Directory path with zero or >1 `.dzi` → `ErrUnsupportedFormat` from the hook
  (ambiguous → fall through, not a hard error).
- `Open(io.ReaderAt)` on DZI content → unsupported (no path), like DICOM.

## API surface

- **Added (public):** `opentile.FormatDZI`. No new methods or options — bare DZI
  uses the existing `Slide`/`Level` surface. Purely additive.
- **Internal:** `internal/dzi.ErrOverlapNotSupported`; `dziPathOpenHook` (root,
  unexported); `formats/dzi` package.
- **Behavior change:** SZI `Overlap>0` now errors at open instead of
  mis-rendering (no `Overlap=0` impact).

## Testing strategy

- **Fixture-free core (CI-safe):** a test writes a tiny synthetic bare-DZI tree
  to `t.TempDir()` (a hand-built `.dzi` manifest + a few solid-color JPEG/PNG
  tiles across 2 levels), opens via `OpenFile`, and asserts: pyramid level count
  / `Size` / `Grid` from `internal/dzi`, `Tile`/`DecodedTile` bytes+pixels, tile
  out-of-bounds, and missing-tile error.
- **Open model:** `OpenFile("x.dzi")` and `OpenFile("<dir-with-one-.dzi>")` both
  open; a directory with no/many `.dzi` falls through; `Open(reader)` on `.dzi`
  bytes is unsupported.
- **Overlap guard (both readers):** a synthetic `Overlap=1` `.dzi` →
  `errors.Is(err, dzi.ErrOverlapNotSupported)` for `formats/dzi`; a synthetic
  `Overlap=1` SZI ZIP → same for `formats/szi`. Assert an `Overlap=0` SZI still
  opens and reads byte-identically (no regression).
- **Optional local parity:** unzip `CMU-1.szi` into a temp dir to get a real
  bare-DZI tree; assert its tiles equal the SZI reader's tiles for the same
  coords (gated on the local SZI fixture; skipped in CI).
- `go test ./... -race`, `go vet`, nocgo build, and `make cover` (≥80% for the
  new `formats/dzi`).

## Out of scope / deferred

- **`Overlap>0` support** (tile-border cropping + placement) — deferred to a
  separate design conversation per the owner; the guard makes it a clean,
  explicit error until then.
- Bare-DZI metadata/associated-image conventions (none standard).
- Sparse-DZI lenient mode (no fixture).
- A `Validate()` hook for `formats/dzi` (additive; can follow).

## Open questions

1. **Tile I/O:** `os.ReadFile` per tile (simple, stateless) vs. an `*os.File`/fd
   strategy. Leaning `os.ReadFile` — tiles are small and independent; revisit
   only if a perf consumer appears.
2. **`.dzi` extension vs. content sniff in the hook:** key detection on the
   `.dzi` extension (cheap, canonical) and validate by parsing the manifest, vs.
   content-sniffing the XML root. Leaning extension-primary + parse-to-confirm.
3. **Registration name / Format string:** `FormatDZI = "dzi"` (parallels
   `FormatSZI = "szi"`). Assumed; flag if a different identifier is wanted.
