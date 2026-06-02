# Root `.go` File Reorganization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `package opentile`'s flat root navigable by renaming 7 mislabeled source files (+ their tests) and splitting the 523-line `strip_iterator.go` into three focused files — all in-package, zero exported-API or behavior change.

**Architecture:** Pure file reorganization. Every rename is a `git mv` (preserves `git blame`); the split relocates declarations verbatim into new files in the same package. The compiler + `make test -race` are the oracle: because nothing crosses a package boundary and no logic changes, a green build and green test prove behavior-identity.

**Tech Stack:** Go 1.23+, single `package opentile` at repo root, Makefile test gates.

**Reference:** `docs/superpowers/specs/2026-06-02-root-file-reorg-design.md`

**Working directory:** repo root `/Users/cornish/GitHub/opentile-go`, branch `refactor/root-reorg` (already created, design doc already committed).

---

## Task 1: Rename mislabeled source + test files

De-overload the `slide_` prefix (it should mean *core Slide* only) and give the region/strips/tags subsystems honest names. 7 source renames + 8 matching test renames, all via `git mv`.

**Files (rename via `git mv`):**

Source:
- `opentile.go` → `open.go`
- `format_types.go` → `format.go`
- `tiff_tags.go` → `tifftags.go`
- `slide_region.go` → `region.go`
- `slide_region_scaled.go` → `region_scaled.go`
- `slide_decoded_tile.go` → `decoded_tile.go`
- `slide_scaled_strips.go` → `strips.go`

Tests:
- `tiff_tags_test.go` → `tifftags_test.go`
- `tiff_tags_crossformat_test.go` → `tifftags_crossformat_test.go`
- `tiff_tags_nontiff_test.go` → `tifftags_nontiff_test.go`
- `slide_region_test.go` → `region_test.go`
- `slide_region_layer1_test.go` → `region_layer1_test.go`
- `slide_region_scaled_test.go` → `region_scaled_test.go`
- `slide_decoded_tile_test.go` → `decoded_tile_test.go`
- `slide_decoded_tile_wb_test.go` → `decoded_tile_wb_test.go`

**Do NOT rename:** `opentile_test.go` (the umbrella package-level test for `Open`/`OpenFile` — keep its name), `slide.go`, `slide_best_level.go` (+ its test), `slide_decoder_cache.go` (these are genuine core-Slide files), and everything in the "no change" set (`geometry.go`, `compression.go`, `metadata.go`, `image.go`, `errors.go`, `splice.go`, `options.go`, `decode_options.go`, `decoded_tile_scratch.go`, `blit.go`, `strip_cache.go`, `strip_options.go`, `strip_iterator.go` and its `strip_iterator_*_test.go` files).

- [ ] **Step 1: Perform all renames**

Run from repo root:

```bash
git mv opentile.go open.go
git mv format_types.go format.go
git mv tiff_tags.go tifftags.go
git mv slide_region.go region.go
git mv slide_region_scaled.go region_scaled.go
git mv slide_decoded_tile.go decoded_tile.go
git mv slide_scaled_strips.go strips.go

git mv tiff_tags_test.go tifftags_test.go
git mv tiff_tags_crossformat_test.go tifftags_crossformat_test.go
git mv tiff_tags_nontiff_test.go tifftags_nontiff_test.go
git mv slide_region_test.go region_test.go
git mv slide_region_layer1_test.go region_layer1_test.go
git mv slide_region_scaled_test.go region_scaled_test.go
git mv slide_decoded_tile_test.go decoded_tile_test.go
git mv slide_decoded_tile_wb_test.go decoded_tile_wb_test.go
```

- [ ] **Step 2: Verify no stray references to old filenames in comments/build tags**

File renames don't affect Go symbol resolution (package-scoped, not file-scoped), but a comment or `//go:generate` directive could reference an old name. Check:

```bash
grep -rn "opentile\.go\|format_types\.go\|tiff_tags\.go\|slide_region\.go\|slide_region_scaled\.go\|slide_decoded_tile\.go\|slide_scaled_strips\.go" --include="*.go" .
```

Expected: no output (or only matches inside the design/plan docs, which are not `*.go`). If any `.go` file references an old filename in a comment, update it to the new name.

- [ ] **Step 3: Build**

Run:

```bash
go build ./...
```

Expected: clean, no output. (Renames within one package cannot change compilation; this is a sanity check that no filename collided with a Go build-constraint suffix — none of the new names do.)

- [ ] **Step 4: Vet + format**

Run:

```bash
gofmt -l . && go vet ./...
```

Expected: `gofmt -l .` prints nothing (all files already formatted); `go vet` clean.

- [ ] **Step 5: Full test under race**

Run:

```bash
make test
```

Expected: all packages PASS under `-race` (the renamed files compile and run exactly as before).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: rename mislabeled root files; slide_ prefix = core Slide only

git mv only — no symbol or logic change. opentile.go->open.go,
format_types.go->format.go, tiff_tags.go->tifftags.go, and the
slide_region*/slide_decoded_tile/slide_scaled_strips files lose the
misleading slide_ prefix (region.go, region_scaled.go, decoded_tile.go,
strips.go). Matching _test.go files renamed in lockstep.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Split `strip_iterator.go` (523 lines) into three files

Relocate declarations verbatim into focused files. Same package, same symbols — only which file each declaration lives in changes. Current symbol map (line numbers in the pre-split file):

| Symbol | Line | Destination file |
|--------|------|------------------|
| `type StripIterator struct` | 17 | `strip_iterator.go` (stays) |
| `func newStripIterator(...)` | 55 | `strip_iterator.go` (stays) |
| `func (it *StripIterator) Strips()` | 141 | `strip_iterator.go` (stays) |
| `func (it *StripIterator) Next()` | 147 | `strip_iterator.go` (stays) |
| `func (it *StripIterator) Close()` | 287 | `strip_iterator.go` (stays) |
| `func resampleImageIntoUsing(...)` | 277 | `strip_geometry.go` |
| `func (it *StripIterator) decodeWorker()` | 320 | `strip_workers.go` |
| `func (it *StripIterator) decodeAndStore(k tileKey)` | 335 | `strip_workers.go` |
| `func (it *StripIterator) lookahead()` | 347 | `strip_workers.go` |
| `func (it *StripIterator) tilesForStrip(stripIdx int)` | 395 | `strip_workers.go` |
| `func stripCacheCapacity(...)` | 456 | `strip_geometry.go` |
| `func autoIDCTScale(...)` | 481 | `strip_geometry.go` |
| `func (s *Slide) bestLevelForRegion(...)` | 506 | `strip_geometry.go` |

**Files:**
- Modify: `strip_iterator.go` (remove the relocated decls)
- Create: `strip_workers.go` (the 4 worker-pool methods)
- Create: `strip_geometry.go` (resample + sizing/level-selection helpers)

The pre-split import block of `strip_iterator.go` is:

```go
import (
	"context"
	"fmt"
	"image"
	"io"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)
```

Each new file needs `package opentile` plus whatever subset of those imports its functions actually use. Don't guess the subset — paste the full import block into each new file, then let `go build` flag unused imports (Go errors on them) and remove exactly those.

- [ ] **Step 1: Create `strip_workers.go`**

Create the file with `package opentile`, the full import block above, and the four methods `decodeWorker`, `decodeAndStore`, `lookahead`, `tilesForStrip` cut verbatim from `strip_iterator.go` (lines 320–455 region). Preserve each function's doc comment.

- [ ] **Step 2: Create `strip_geometry.go`**

Create the file with `package opentile`, the full import block above, and `resampleImageIntoUsing` (277–286), `stripCacheCapacity` (456–480), `autoIDCTScale` (481–505), `bestLevelForRegion` (506–end) cut verbatim from `strip_iterator.go`. Preserve doc comments.

- [ ] **Step 3: Trim `strip_iterator.go`**

After the cuts, `strip_iterator.go` retains only `type StripIterator`, `newStripIterator`, `Strips`, `Next`, `Close`, and the leftover comments/imports.

- [ ] **Step 4: Resolve imports via the compiler**

Run:

```bash
go build ./...
```

Expected on first run: likely `imported and not used` errors in one or more of the three files. Remove exactly the flagged imports from the file the compiler names. Re-run `go build ./...` until clean (no output).

- [ ] **Step 5: Format + vet**

Run:

```bash
gofmt -w strip_iterator.go strip_workers.go strip_geometry.go && gofmt -l . && go vet ./...
```

Expected: `gofmt -l .` prints nothing; `go vet` clean.

- [ ] **Step 6: Full test under race**

Run:

```bash
make test
```

Expected: all packages PASS under `-race`. The strip_iterator suite (`strip_iterator_test.go`, `strip_iterator_budget_test.go`, `strip_iterator_decode_test.go`, `strip_iterator_integration_test.go`) exercises every relocated method and must stay green.

- [ ] **Step 7: Confirm the diff is a pure relocation**

Run:

```bash
git diff --stat
```

Expected: `strip_iterator.go` shows deletions, `strip_workers.go` + `strip_geometry.go` show additions, and the totals roughly balance (modulo the added `package`/`import` scaffolding per new file). No other files touched.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor: split strip_iterator.go into iterator/workers/geometry

Verbatim declaration relocation, same package opentile. strip_iterator.go
keeps the StripIterator type + Next/Strips/Close; strip_workers.go gets
the decode-worker pool methods; strip_geometry.go gets resample +
sizing/level-selection helpers. No logic change.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (after both tasks)

- [ ] **Step 1: Confirm root layout matches the design**

```bash
ls *.go | grep -v _test.go | sort
```

Expected set (25 files): `blit.go compression.go decode_options.go decoded_tile.go decoded_tile_scratch.go errors.go format.go geometry.go image.go metadata.go open.go options.go region.go region_scaled.go slide.go slide_best_level.go slide_decoder_cache.go splice.go strip_cache.go strip_geometry.go strip_iterator.go strip_options.go strip_workers.go strips.go tifftags.go`.

- [ ] **Step 2: Full gate green**

```bash
make test && go vet ./...
```

Expected: PASS, clean.

- [ ] **Step 3: Public API unchanged (sanity)**

```bash
go doc . | head -50
```

Expected: the exported surface (`Open`, `OpenFile`, `Slide`, `Level`, `Image`, `TIFFTag`, `SpliceJPEGTile`, …) is identical to before the reorg — no exported symbol was renamed or moved.
```
