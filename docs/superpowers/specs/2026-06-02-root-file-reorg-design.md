# Root `.go` File Reorganization — Design

**Date:** 2026-06-02
**Status:** Approved (Approach A)
**Roadmap item:** #20 — untangle the flat root `package opentile`

## Problem

`package opentile` lives flat at the repo root: 22 non-test `.go` files
(~3,159 LOC) plus 34 test files. Navigation is poor — the `slide_`
prefix is overloaded (it tags both core-Slide files and unrelated
region/strips subsystems), and `strip_iterator.go` has grown to 523
lines spanning the iterator type, its worker pool, and geometry helpers.

## Hard constraint (why this is a *naming* reorg, not a *foldering* one)

Go is one-package-per-directory. 19 of the 22 files define an exported
type (`Level`, `Image`, `TIFFTag`, `Compression`, `StripIterator`,
`SpliceJPEGTile`, …) or a `func (s *Slide)` method. Those **must** stay
in `package opentile` at root — moving them into subdirectories would
change the public import path (`opentile.ReadRegion` →
`opentile/region.ReadRegion`), a breaking API change. The sibling
projects `wsitools` and `openscope` import this package directly, so the
exported surface must keep working unchanged.

Therefore the reorg stays 100% in-package. It delivers navigability via
(a) consistent file naming so each subsystem reads as a group in `ls`,
and (b) splitting the oversized `strip_iterator.go`. **No symbol moves
across packages, no behavior change, no exported-API change.**

Extracting the three pure-internal files (`strip_cache.go` /
`decoded_tile_scratch.go` / `blit.go`) into `internal/` packages was
considered (Approach B) and rejected: it pokes the concurrency-sensitive
`tileCache` (which carried the v0.30 in-flight-eviction deadlock) for a
cosmetic 3-file reduction, while the bulk is forced to stay at root
anyway.

## Final layout (after reorg)

Grouped by subsystem; **all remain `package opentile` at repo root.**

```
PUBLIC VALUE TYPES        OPEN / CONFIG            SLIDE CORE
  geometry.go               open.go                  slide.go
  compression.go            options.go               slide_best_level.go
  format.go                 decode_options.go        slide_decoder_cache.go
  metadata.go
  image.go                REGION / DECODE PATH     STRIPS / DZI
  errors.go                 region.go                strips.go
  tifftags.go               region_scaled.go         strip_iterator.go
  splice.go                 decoded_tile.go          strip_workers.go
                            decoded_tile_scratch.go  strip_geometry.go
                            blit.go                  strip_cache.go
                                                     strip_options.go
```

After the reorg, the `slide_` prefix means **core Slide** only.

## Change manifest

### Source renames (7) — via `git mv` to preserve blame

| Old | New |
|-----|-----|
| `opentile.go` | `open.go` |
| `format_types.go` | `format.go` |
| `tiff_tags.go` | `tifftags.go` |
| `slide_region.go` | `region.go` |
| `slide_region_scaled.go` | `region_scaled.go` |
| `slide_decoded_tile.go` | `decoded_tile.go` |
| `slide_scaled_strips.go` | `strips.go` |

### Matching test renames (carry by prefix)

| Old | New |
|-----|-----|
| `tiff_tags_test.go` | `tifftags_test.go` |
| `tiff_tags_crossformat_test.go` | `tifftags_crossformat_test.go` |
| `tiff_tags_nontiff_test.go` | `tifftags_nontiff_test.go` |
| `slide_region_test.go` | `region_test.go` |
| `slide_region_layer1_test.go` | `region_layer1_test.go` |
| `slide_region_scaled_test.go` | `region_scaled_test.go` |
| `slide_decoded_tile_test.go` | `decoded_tile_test.go` |
| `slide_decoded_tile_wb_test.go` | `decoded_tile_wb_test.go` |

(`opentile.go` and `slide_scaled_strips.go` have no same-named test
files. `opentile_test.go` tests `Open`/`OpenFile` and stays — it is the
package-level test; renaming it to `open_test.go` is optional and
**not** done, to avoid colliding with the convention that
`opentile_test.go` is the umbrella package test.)

### Split: `strip_iterator.go` (523 lines) → 3 files

Same package, same symbols, just relocated declarations:

- **`strip_iterator.go`** — `type StripIterator`, `newStripIterator`,
  `(*StripIterator) Strips`, `(*StripIterator) Next`,
  `(*StripIterator) Close`.
- **`strip_workers.go`** — `(*StripIterator) decodeWorker`,
  `(*StripIterator) decodeAndStore`, `(*StripIterator) lookahead`,
  `(*StripIterator) tilesForStrip`.
- **`strip_geometry.go`** — `resampleImageIntoUsing`,
  `stripCacheCapacity`, `autoIDCTScale`, `(s *Slide) bestLevelForRegion`.

The four `strip_iterator_*_test.go` files keep their names (they test
`StripIterator` behavior regardless of which file the method lives in).

## Non-goals

- No exported-symbol renames or moves (public API byte-stable).
- No `internal/` extraction (Approach B, rejected).
- No logic changes — every moved declaration is copied verbatim.

## Verification

- `gofmt`/`go vet ./...` clean.
- `make test` green under `-race` (the move must be behavior-identical).
- `git mv` for every rename so `git blame` survives.
- Final `git diff --stat` should show only renames + the 3-way split —
  zero net line changes beyond declaration relocation.
```
