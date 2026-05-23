# opentile-go v0.13 — TilePrefix + TileBodyInto implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land three additive `Level` interface methods (`TilePrefix`, `TileBodyInto`, `TileBodyMaxSize`) plus the `opentile.SpliceJPEGTile` top-level helper, enabling client-server consumers to deduplicate per-level JPEG splice prefix bytes. Bundle a bench harness measuring Pattern-A-vs-B bandwidth savings.

**Architecture:** Additive interface evolution. T1 lands the interface signatures + helper + no-op default implementations on every existing Level type, keeping the build green. T2-T5 specialize splice-format Levels to return real prefix/body bytes. T6 verifies the byte-equality invariant (`SpliceJPEGTile(prefix, body) == Tile()`). T7 ships the bench harness. T8 closes with docs.

**Tech stack:** Go 1.23+; `bytes.Index` for SOS-marker detection in the helper; existing v0.9 `splicePrefix` cache reused unchanged on the server side.

**Spec:** [`docs/superpowers/specs/2026-05-07-opentile-go-v13-tile-prefix-design.md`](../specs/2026-05-07-opentile-go-v13-tile-prefix-design.md).

---

## Task layout

7 tasks across one batch:

- T1 — Interface additions + helper + no-op defaults
- T2 — SVS splice specialization (with APP14)
- T3 — Philips + OME + leicascn + generictiff splice specialization (no APP14)
- T4 — BIF specialization (mixed shared vs per-tile-embedded JPEGTables)
- T5 — Per-format byte-equality unit tests
- T6 — Bench harness
- T7 — Docs + ship

End-of-task verification: every task runs `go vet ./...` + targeted tests + `gofmt -l` on touched files. End-of-plan: full module green + bench harness produces a report.

---

## T1 — Interface + `SpliceJPEGTile` helper + no-op defaults across all Level types

**Files:**

- Modify: `image.go` (add 3 method signatures to `Level` interface)
- Create: `splice.go` (new top-level `SpliceJPEGTile` function + tests)
- Create: `splice_test.go` (unit tests for `SpliceJPEGTile`)
- Modify: every Level implementation file to add no-op default methods:
  - `formats/svs/tiled.go`
  - `formats/ndpi/stripped.go`
  - `formats/ndpi/oneframe.go`
  - `formats/philipstiff/tiled.go`
  - `formats/ometiff/tiled.go`
  - `formats/bif/level.go`
  - `formats/ife/level.go`
  - `formats/generictiff/tiled.go`
  - `formats/leicascn/tiled.go`
- Possibly: `opentile/opentiletest/*.go` (if it has a mock Level — check at audit step)

- [ ] **Step 1: Audit all Level implementers**

```bash
cd /Users/cornish/GitHub/opentile-go
grep -rn "TileMaxSize() int\|TileInto(x, y int" --include="*.go" .
```

This finds every type that implements `TileMaxSize` / `TileInto` — these are all the Level implementers that need the 3 new methods. Note the file list.

- [ ] **Step 2: Add 3 method signatures to Level interface**

Edit `image.go` to add the new methods after `TileMaxSize() int`:

```go
// TilePrefix returns the constant per-level JPEG splice prefix bytes.
// When non-nil, callers can ship the prefix once per level + per-tile
// TileBodyInto output, then reconstitute the full JPEG on the client
// side via opentile.SpliceJPEGTile. When nil, no splice prefix exists
// for this level — TileBodyInto returns the same bytes as TileInto.
//
// Use case: bandwidth-efficient client-server tile transfer. SVS /
// Philips / OME / leicascn / generictiff levels with shared JPEGTables
// typically have ~1 KB of prefix per level applied to 100k+ tiles per
// slide; deduplicating saves ~100 MB bandwidth per slide.
//
// Added in v0.13.
TilePrefix() []byte

// TileBodyInto writes the on-disk tile bytes (the "body" — what gets
// spliced with TilePrefix to form a complete JPEG) into dst. Returns
// the number of bytes written.
//
// For levels where TilePrefix() returns nil (non-splice path),
// TileBodyInto is functionally equivalent to TileInto.
//
// Caller must size dst to at least TileBodyMaxSize().
//
// Added in v0.13.
TileBodyInto(x, y int, dst []byte) (int, error)

// TileBodyMaxSize returns the upper bound on TileBodyInto output size
// across all tiles in this level. For levels with shared JPEGTables
// (TilePrefix() != nil), this is strictly less than TileMaxSize() (no
// splice prefix added). For levels without shared JPEGTables
// (TilePrefix() == nil), equal to TileMaxSize().
//
// Added in v0.13.
TileBodyMaxSize() int
```

- [ ] **Step 3: Write `splice.go` with `SpliceJPEGTile`**

Create `splice.go`:

```go
package opentile

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrBadJPEGSplice is returned by SpliceJPEGTile when the body bytes
// don't conform to the expected SOS-bearing JPEG layout.
var ErrBadJPEGSplice = errors.New("opentile: bad JPEG splice input")

// SpliceJPEGTile reconstitutes a complete JPEG from a level's
// TilePrefix bytes and one tile's TileBodyInto output. Inserts the
// prefix at the on-disk tile's SOS boundary (the same operation
// opentile-go does internally during Tile/TileInto).
//
// Returns body verbatim (defensively copied) if prefix is empty / nil
// — degenerate case for levels without splice (e.g., non-JPEG
// compressions, NDPI stripped levels, IFE).
//
// Returns ErrBadJPEGSplice if body is empty or doesn't contain an
// SOS marker (0xFF 0xDA per JPEG spec).
//
// Algorithm (documented for non-Go consumers reimplementing
// client-side):
//
//   1. If prefix is empty: return body verbatim.
//   2. Find offset of the first 0xFF 0xDA byte sequence in body
//      ("Start of Scan" marker).
//   3. Output = body[0:sosIdx] + prefix + body[sosIdx:]
//
// Added in v0.13 alongside Level.TilePrefix and Level.TileBodyInto.
func SpliceJPEGTile(prefix, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: body is empty", ErrBadJPEGSplice)
	}
	if len(prefix) == 0 {
		out := make([]byte, len(body))
		copy(out, body)
		return out, nil
	}
	sosIdx := bytes.Index(body, []byte{0xFF, 0xDA})
	if sosIdx < 0 {
		return nil, fmt.Errorf("%w: SOS marker (0xFF 0xDA) not found in body", ErrBadJPEGSplice)
	}
	out := make([]byte, len(body)+len(prefix))
	copy(out[0:sosIdx], body[0:sosIdx])
	copy(out[sosIdx:sosIdx+len(prefix)], prefix)
	copy(out[sosIdx+len(prefix):], body[sosIdx:])
	return out, nil
}
```

- [ ] **Step 4: Write `splice_test.go` unit tests**

```go
package opentile

import (
	"bytes"
	"errors"
	"testing"
)

func TestSpliceJPEGTile_HappyPath(t *testing.T) {
	// body = SOI(FF D8) + APP0(FF E0 00 04 00 00) + SOS(FF DA 01 02) + scan + EOI(FF D9)
	body := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, // pre-SOS metadata
		0xFF, 0xDA, 0x01, 0x02, // SOS + scan
		0xAA, 0xBB, // entropy
		0xFF, 0xD9, // EOI
	}
	// prefix = DQT(FF DB 00 02) + DHT(FF C4 00 02)
	prefix := []byte{0xFF, 0xDB, 0x00, 0x02, 0xFF, 0xC4, 0x00, 0x02}

	out, err := SpliceJPEGTile(prefix, body)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: SOI + APP0 + DQT + DHT + SOS + scan + EOI
	want := []byte{
		0xFF, 0xD8,
		0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00,
		0xFF, 0xDB, 0x00, 0x02, 0xFF, 0xC4, 0x00, 0x02, // prefix inserted at SOS boundary
		0xFF, 0xDA, 0x01, 0x02,
		0xAA, 0xBB,
		0xFF, 0xD9,
	}
	if !bytes.Equal(out, want) {
		t.Errorf("output mismatch:\ngot  % x\nwant % x", out, want)
	}
}

func TestSpliceJPEGTile_NilPrefix_ReturnsBodyCopy(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x01, 0xFF, 0xD9}
	out, err := SpliceJPEGTile(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("output != body: % x vs % x", out, body)
	}
	// Confirm it's a copy, not the same slice
	out[0] = 0x00
	if body[0] == 0x00 {
		t.Error("SpliceJPEGTile returned a shared slice; output mutation leaked back to body")
	}
}

func TestSpliceJPEGTile_EmptyPrefix_ReturnsBodyCopy(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x01, 0xFF, 0xD9}
	out, err := SpliceJPEGTile([]byte{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("output != body: % x vs % x", out, body)
	}
}

func TestSpliceJPEGTile_EmptyBody_Errors(t *testing.T) {
	_, err := SpliceJPEGTile([]byte{0xFF, 0xDB}, nil)
	if !errors.Is(err, ErrBadJPEGSplice) {
		t.Errorf("got %v, want ErrBadJPEGSplice", err)
	}
}

func TestSpliceJPEGTile_NoSOS_Errors(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xD9} // SOI + EOI; no SOS
	_, err := SpliceJPEGTile([]byte{0xFF, 0xDB}, body)
	if !errors.Is(err, ErrBadJPEGSplice) {
		t.Errorf("got %v, want ErrBadJPEGSplice", err)
	}
}
```

- [ ] **Step 5: Add no-op defaults to every Level implementation**

For EACH Level implementer file (svs/tiled.go, ndpi/stripped.go, ndpi/oneframe.go, philipstiff/tiled.go, ometiff/tiled.go, bif/level.go, ife/level.go, generictiff/tiled.go, leicascn/tiled.go), add these methods using the receiver name and exact existing receiver type. The pattern is:

```go
// TilePrefix returns nil — this Level type doesn't expose a separable
// per-level splice prefix in v0.13. T2-T5 specializations override
// for the splice-format levels.
func (l *RECEIVER_TYPE) TilePrefix() []byte { return nil }

// TileBodyInto delegates to TileInto (no separation between body
// bytes and full tile output for non-splice levels). T2-T5
// specializations override for the splice-format levels.
func (l *RECEIVER_TYPE) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize equals TileMaxSize for non-splice levels (the body
// IS the full tile output). T2-T5 specializations override.
func (l *RECEIVER_TYPE) TileBodyMaxSize() int { return l.TileMaxSize() }
```

The receiver type is `*tiledImage` for SVS/Philips/OME/generictiff/leicascn-compositeLevel; `*strippedImage` for NDPI; `*oneFrameImage` for NDPI/OME; `*Level` (capitalized) for BIF/IFE; etc. Verify each via the audit grep from Step 1.

- [ ] **Step 6: Build + test**

```bash
cd /Users/cornish/GitHub/opentile-go
go build ./... 2>&1 | head -10
go test -count=1 -v -run TestSpliceJPEGTile ./ 2>&1 | tail -10
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: build clean; `SpliceJPEGTile` tests pass; full module green (no behavioral regressions because all defaults preserve existing `TileInto`/`TileMaxSize` behavior).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
feat(v0.13): T1 — Level interface evolution + SpliceJPEGTile + defaults

Adds three new methods to opentile.Level (TilePrefix, TileBodyInto,
TileBodyMaxSize) for the prefix-deduplication wire pattern. Plus
the opentile.SpliceJPEGTile top-level helper that reconstitutes
full JPEGs from (prefix, body) pairs on the client side.

T1 ships no-op defaults on every existing Level implementation
(returns nil prefix; delegates Body* to Tile*). Subsequent tasks
specialize the splice-format Levels (SVS, Philips, OME, BIF,
generictiff, leicascn) to return real prefix bytes + skip the
splice in TileBodyInto.

splice.go's SpliceJPEGTile algorithm documented inline for non-Go
consumers (web viewer JS reimplementation): find SOS marker
(0xFF 0xDA) in body, splice prefix in at that boundary.

Build + module tests green. No behavioral regressions on existing
Tile / TileInto paths.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T2 — SVS splice specialization

**Files:**
- Modify: `formats/svs/tiled.go` (replace T1 no-op defaults with SVS splice impl)
- Modify: `formats/svs/tiled_test.go` (test the new methods specifically)

SVS levels with shared JPEGTables (most pyramid levels) have a cached `splicePrefix` (DQT + DHT + APP14) computed at level open. T2 exposes that prefix and adds a body-only read path.

- [ ] **Step 1: Audit SVS tiledImage state**

Read `formats/svs/tiled.go`. Verify the existing `tiledImage` struct has:
- `splicePrefix []byte` — already cached (set from `jpeg.BuildSplicePrefix(jpegTables, true)`)
- `maxTileSize int` — current `TileMaxSize`; equals `max(counts) + len(splicePrefix)` for splice-applied tiles
- `offsets`, `counts` — tile-table data

The body bytes are just the on-disk tile bytes — `max(counts)`. Compute that as a new cached field.

- [ ] **Step 2: Add `bodyMaxSize` field + populate at construction**

Edit the `tiledImage` struct definition to add `bodyMaxSize int`. In the constructor (look for `newTiledImage` or similar), populate after `maxTileSize` is computed:

```go
// In the constructor, after the existing maxTileSize calculation:
bodyMaxSize := 0
for _, c := range counts {
    if int(c) > bodyMaxSize {
        bodyMaxSize = int(c)
    }
}
```

(Or extract from the existing `maxTileSize` computation if `maxTileSize == bodyMaxSize + len(splicePrefix)` — confirm by reading the existing code; they should be related.)

Then assign to the struct: `bodyMaxSize: bodyMaxSize`.

- [ ] **Step 3: Replace T1 no-op defaults with SVS splice impls**

Replace these methods on `*tiledImage`:

```go
// TilePrefix returns the cached SVS splice prefix (DQT + DHT + APP14)
// or nil if this level doesn't carry shared JPEGTables. SVS pyramid
// levels typically all carry shared tables; the nil case applies if
// a future SVS variant ships without tag 347.
func (l *tiledImage) TilePrefix() []byte {
	if len(l.splicePrefix) == 0 {
		return nil
	}
	out := make([]byte, len(l.splicePrefix))
	copy(out, l.splicePrefix)
	return out
}

// TileBodyInto reads the on-disk tile bytes into dst WITHOUT
// applying the splice prefix. Caller can call opentile.SpliceJPEGTile
// with TilePrefix() output to reconstitute the full JPEG.
func (l *tiledImage) TileBodyInto(x, y int, dst []byte) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	count := int(l.counts[idx])
	if count == 0 {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	if len(dst) < count {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(l.reader, dst[:count], int64(l.offsets[idx])); err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return count, nil
}

// TileBodyMaxSize returns max(counts) — the upper bound on body
// (on-disk tile) bytes. Strictly less than TileMaxSize when the
// level carries shared JPEGTables.
func (l *tiledImage) TileBodyMaxSize() int { return l.bodyMaxSize }
```

The exact field accessors (`l.grid`, `l.counts`, `l.offsets`, `l.reader`, `l.index`) must match what's already on the SVS tiledImage struct. Check the existing `TileInto` for reference — its body-read path is exactly what `TileBodyInto` does, just without the splice.

- [ ] **Step 4: Add SVS-specific tests**

In `formats/svs/tiled_test.go`, add:

```go
// TestTilePrefixAndBodyReconstituteFullJPEG verifies the v0.13
// invariant: SpliceJPEGTile(TilePrefix(), TileBodyInto(...)) is
// byte-identical to Tile(...) for every tested tile coord.
func TestTilePrefixAndBodyReconstituteFullJPEG(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	tiler, err := opentile.OpenFile(filepath.Join(dir, "svs", "CMU-1-Small-Region.svs"))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()

	for li, lvl := range tiler.Levels() {
		prefix := lvl.TilePrefix()
		if len(prefix) == 0 {
			t.Errorf("L%d: TilePrefix is empty (SVS pyramid levels always have shared JPEGTables)", li)
			continue
		}
		bodyBuf := make([]byte, lvl.TileBodyMaxSize())
		grid := lvl.Grid()
		for _, p := range []struct{ x, y int }{
			{0, 0}, {grid.W - 1, 0}, {0, grid.H - 1}, {grid.W - 1, grid.H - 1}, {grid.W / 2, grid.H / 2},
		} {
			full, err := lvl.Tile(p.x, p.y)
			if err != nil {
				continue // skip empty/missing tiles
			}
			n, err := lvl.TileBodyInto(p.x, p.y, bodyBuf)
			if err != nil {
				t.Errorf("L%d (%d,%d) TileBodyInto: %v", li, p.x, p.y, err)
				continue
			}
			reconstituted, err := opentile.SpliceJPEGTile(prefix, bodyBuf[:n])
			if err != nil {
				t.Errorf("L%d (%d,%d) SpliceJPEGTile: %v", li, p.x, p.y, err)
				continue
			}
			if !bytes.Equal(full, reconstituted) {
				t.Errorf("L%d (%d,%d): reconstituted (%d bytes) != Tile() (%d bytes)",
					li, p.x, p.y, len(reconstituted), len(full))
			}
		}
	}
}
```

- [ ] **Step 5: Build + test**

```bash
go build ./... 2>&1 | head
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./formats/svs/ 2>&1 | tail -3
gofmt -l formats/svs/
```

Expected: build clean, SVS tests pass, gofmt clean.

- [ ] **Step 6: Commit**

```bash
git add formats/svs/
git commit -m "$(cat <<'EOF'
feat(svs): T2 — TilePrefix/TileBodyInto/TileBodyMaxSize specialization

Replaces the v0.13 T1 no-op defaults on SVS *tiledImage with the
splice-aware implementations:

  TilePrefix()      → defensive copy of cached splicePrefix
                       (DQT + DHT + APP14, set at level construction
                       via jpeg.BuildSplicePrefix(jpegTables, true))
  TileBodyInto()    → reads on-disk tile bytes via tiff.ReadAtFull
                       directly into dst[:count]; skips the splice
                       step that Tile/TileInto perform
  TileBodyMaxSize() → returns max(counts) — strictly less than
                       TileMaxSize() (which adds len(splicePrefix))

Verified per-tile via TestTilePrefixAndBodyReconstituteFullJPEG:
SpliceJPEGTile(TilePrefix(), TileBodyInto(p)) byte-identical to
Tile(p) across L0 corners + center on CMU-1-Small-Region.svs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T3 — Philips + OME + leicascn + generictiff splice specialization

**Files:**
- Modify: `formats/philipstiff/tiled.go`
- Modify: `formats/ometiff/tiled.go`
- Modify: `formats/leicascn/tiled.go` (compositeLevel) + `formats/leicascn/tiled_region.go` (tiledRegion's body-read used by composite)
- Modify: `formats/generictiff/tiled.go`

All four formats use the same JPEGTables-shared-tables splice pattern as SVS but WITHOUT the APP14 marker. Apply the SAME pattern as T2:

- `TilePrefix()` returns the cached `splicePrefix` (or nil if no shared tables).
- `TileBodyInto()` reads on-disk tile bytes without splicing.
- `TileBodyMaxSize()` returns the per-level `bodyMaxSize` cached at construction.

- [ ] **Step 1: For each format, audit the existing `*tiledImage` (or equivalent) struct for the cached splicePrefix + offsets/counts/reader/grid fields**

```bash
for f in formats/philipstiff/tiled.go formats/ometiff/tiled.go formats/generictiff/tiled.go formats/leicascn/tiled.go formats/leicascn/tiled_region.go; do
    echo "==> $f"
    grep -n "splicePrefix\|maxTileSize\|jpegTables\|offsets\|counts" "$f" | head -10
done
```

For each format, identify:
- The Level type's struct name + receiver
- The cached splicePrefix field name (typically `splicePrefix`)
- The per-tile read fields (offsets/counts/reader)
- The TileMaxSize / bodyMaxSize relationship

- [ ] **Step 2: Apply the SVS pattern from T2 to each file**

For Philips, OME, generictiff: same pattern as T2 step 3. Add `bodyMaxSize int` to the struct + populate at construction; replace the T1 no-op `TilePrefix`/`TileBodyInto`/`TileBodyMaxSize` methods with splice-aware implementations.

For leicascn's `compositeLevel`: this is more complex because of multi-region dispatch + blank-tile fill. The `TilePrefix` returns the per-region prefix (assume all regions share — they do per Q5 of v0.11 spec). `TileBodyInto` dispatches to the per-region body read OR returns the synthesized blank tile (per Q4 of v0.13 spec — blank fill is wasteful-but-valid through SpliceJPEGTile).

```go
// formats/leicascn/tiled.go (compositeLevel)

func (l *compositeLevel) TilePrefix() []byte {
	// All regions share tile size + JPEGTables (Q5); pull from the first
	// region. nil if no region has a splice prefix (degenerate; shouldn't
	// happen on real SCN fixtures).
	if len(l.regions) == 0 || l.regions[0].splicePrefix == nil {
		return nil
	}
	out := make([]byte, len(l.regions[0].splicePrefix))
	copy(out, l.regions[0].splicePrefix)
	return out
}

func (l *compositeLevel) TileBodyInto(x, y int, dst []byte) (int, error) {
	// Find which region contains (x, y); reuse compositeLevel.findRegion
	// (existing helper from v0.11 T8). For inter-region gap tiles, return
	// the synthesised blank-fill tile — body equals full tile (the blank
	// is self-contained); SpliceJPEGTile will produce a redundantly-prefixed
	// JPEG that's still valid (Q4).
	regionIdx := l.findRegion(x, y)
	if regionIdx < 0 {
		// Blank-fill path
		bt := l.blankTile()
		if len(dst) < len(bt) {
			return 0, io.ErrShortBuffer
		}
		copy(dst, bt)
		return len(bt), nil
	}
	// Real region: forward to per-region body read
	region := l.regions[regionIdx]
	rx, ry := l.regionLocalCoord(x, y, region) // existing helper
	return region.tileBodyInto(rx, ry, dst)
}

func (l *compositeLevel) TileBodyMaxSize() int {
	// max body size across regions, OR the blank-tile size (whichever larger).
	maxBody := 0
	for _, r := range l.regions {
		if r.bodyMaxSize > maxBody {
			maxBody = r.bodyMaxSize
		}
	}
	if bt := len(l.blankTileBytes()); bt > maxBody {
		maxBody = bt
	}
	return maxBody
}
```

The `tiledRegion` (in `tiled_region.go`) needs a parallel `tileBodyInto` helper if it doesn't have one, plus `bodyMaxSize` field.

(The exact API on `tiledRegion` may differ — check the existing implementation. The point is: each composite tile dispatch goes through to a per-region body read.)

- [ ] **Step 3: Build + test per format**

After implementing each file:

```bash
go build ./... 2>&1 | head
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./formats/philipstiff/ ./formats/ometiff/ ./formats/leicascn/ ./formats/generictiff/ 2>&1 | tail -10
gofmt -l formats/philipstiff/ formats/ometiff/ formats/leicascn/ formats/generictiff/
```

Expected: clean.

T6 below adds the per-format byte-equality test for these formats.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
feat(splice): T3 — TilePrefix/TileBodyInto specialization for non-APP14 splice formats

Applies the v0.13 T2 SVS pattern to the four other JPEG-with-shared-
JPEGTables formats:

  formats/philipstiff/tiled.go  *tiledImage
  formats/ometiff/tiled.go      *tiledImage
  formats/generictiff/tiled.go  *tiledImage
  formats/leicascn/tiled.go     *compositeLevel
  formats/leicascn/tiled_region.go  *tiledRegion (per-region body read)

All cache splicePrefix at level construction (no APP14, unlike SVS).
TileBodyInto reads on-disk tile bytes without applying the splice.
TileBodyMaxSize returns max(counts).

leicascn compositeLevel: per Q4 of v0.13 spec, inter-region blank-
fill tiles return the synthesised blank as body (body IS a full
self-contained JPEG); SpliceJPEGTile produces a redundantly-prefixed
JPEG that's structurally valid. Bandwidth savings on these tiles
are negligible (small + infrequent).

T6 adds the per-format byte-equality reconstitution tests.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T4 — BIF specialization (mixed shared vs per-tile JPEGTables)

**Files:**
- Modify: `formats/bif/level.go`
- Modify: `formats/bif/level_test.go` (or wherever BIF tile tests live)

BIF is the format with mixed cases:
- **OS-1 fixture**: shared JPEGTables (tag 347) at the page level — same pattern as SVS/Philips. `TilePrefix` returns the cached splicePrefix; `TileBodyInto` reads body-only.
- **Ventana-1 fixture**: per-tile-embedded JPEGTables. No shared splice prefix. `TilePrefix` returns nil; `TileBodyInto` is equivalent to `TileInto` (reads full self-contained tile).

The level type needs to detect at construction which case applies and set `splicePrefix` to nil for per-tile-embedded files.

- [ ] **Step 1: Audit BIF level construction**

Read `formats/bif/level.go`. Verify:
- The Level struct's name + receiver
- Whether it caches a `splicePrefix` already (probably yes — to support the OS-1 case)
- Whether `TileMaxSize()` already accounts for both cases

- [ ] **Step 2: Verify mixed behavior + add T2-pattern methods**

For BIF: replace T1 no-op defaults with implementations that switch on the cached splicePrefix:

```go
// In formats/bif/level.go on the Level receiver:

func (l *Level) TilePrefix() []byte {
	if len(l.splicePrefix) == 0 {
		return nil // Ventana-1 case: per-tile embedded JPEGTables
	}
	out := make([]byte, len(l.splicePrefix))
	copy(out, l.splicePrefix)
	return out
}

func (l *Level) TileBodyInto(x, y int, dst []byte) (int, error) {
	if len(l.splicePrefix) == 0 {
		// Per-tile embedded case (Ventana-1): body IS the full tile.
		return l.TileInto(x, y, dst)
	}
	// Shared-tables case (OS-1): read on-disk bytes without splice.
	// Mirror the OS-1 read path in TileInto, minus the splice.
	// ... [similar to T2's body-read logic, using BIF's offsets/counts/reader]
}

func (l *Level) TileBodyMaxSize() int {
	if len(l.splicePrefix) == 0 {
		return l.TileMaxSize()
	}
	return l.bodyMaxSize // populated at construction
}
```

- [ ] **Step 3: Tests + build**

Add a per-fixture test:
- OS-1 fixture (shared JPEGTables): `TilePrefix() != nil`; reconstitution invariant holds.
- Ventana-1 fixture (per-tile embedded): `TilePrefix() == nil`; `TileBodyInto == TileInto` byte-identical; `SpliceJPEGTile(nil, body) == body`.

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./formats/bif/ 2>&1 | tail -3
```

- [ ] **Step 4: Commit**

```bash
git add formats/bif/
git commit -m "$(cat <<'EOF'
feat(bif): T4 — TilePrefix/TileBodyInto specialization (mixed cases)

BIF has two real-world cases that require switching at level
construction:

  - OS-1 (shared JPEGTables, tag 347): splicePrefix cached;
    TilePrefix returns the cache; TileBodyInto reads on-disk
    body bytes without splice.
  - Ventana-1 (per-tile-embedded JPEGTables): splicePrefix nil;
    TilePrefix returns nil; TileBodyInto delegates to TileInto
    (body == full self-contained tile).

The level constructor decides which case applies based on whether
the IFD carries tag 347. TileBodyMaxSize follows: equals
TileMaxSize on Ventana-1, max(counts) on OS-1.

Per-fixture tests verify both paths.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T5 — Per-format byte-equality unit tests across the slate

**Files:**
- Create / extend: `tests/parity/tilebody_parity_test.go` (centralized cross-format test)

T2/T3/T4 already added per-format reconstitution tests. T5 adds a centralized test that runs the same invariant across every format in the slate, ensuring no format slipped through.

- [ ] **Step 1: Write `tests/parity/tilebody_parity_test.go`**

```go
package parity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestTileBodyReconstitutionInvariant_AllFormats walks one fixture
// per format, reads 5 sampled tiles per L0, and confirms:
//
//   SpliceJPEGTile(Level.TilePrefix(), Level.TileBodyInto(p)) ==byte== Level.Tile(p)
//
// Catches any format that implements TilePrefix/TileBodyInto
// inconsistently with what the existing Tile() output is.
func TestTileBodyReconstitutionInvariant_AllFormats(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	for _, tc := range []struct {
		subdir, name string
	}{
		{"svs", "CMU-1-Small-Region.svs"},
		{"ndpi", "CMU-1.ndpi"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"bif", "Ventana-1.bif"}, // per-tile embedded — TilePrefix nil
		{"bif", "OS-1.bif"},       // shared — TilePrefix non-nil
		{"ife", "cervix_2x_jpeg.iris"},
		{"generic-tiff", "CMU-1.tiff"},
		{"scn", "Leica-1.scn"},
		{"scn", "Leica-2.scn"}, // multi-region; tests blank-tile via spec Q4
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer tiler.Close()

			lvl := tiler.Levels()[0]
			grid := lvl.Grid()
			prefix := lvl.TilePrefix()
			bodyBuf := make([]byte, lvl.TileBodyMaxSize())

			positions := []struct{ x, y int }{
				{0, 0},
				{grid.W - 1, 0},
				{0, grid.H - 1},
				{grid.W - 1, grid.H - 1},
				{grid.W / 2, grid.H / 2},
			}
			for _, p := range positions {
				full, errFull := lvl.Tile(p.x, p.y)
				n, errBody := lvl.TileBodyInto(p.x, p.y, bodyBuf)
				if (errFull == nil) != (errBody == nil) {
					t.Errorf("(%d,%d): Tile err=%v, TileBodyInto err=%v", p.x, p.y, errFull, errBody)
					continue
				}
				if errFull != nil {
					continue
				}
				reconstituted, err := opentile.SpliceJPEGTile(prefix, bodyBuf[:n])
				if err != nil {
					t.Errorf("(%d,%d): SpliceJPEGTile: %v", p.x, p.y, err)
					continue
				}
				if !bytes.Equal(full, reconstituted) {
					t.Errorf("(%d,%d): reconstituted (%d bytes) != Tile() (%d bytes)",
						p.x, p.y, len(reconstituted), len(full))
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run + commit**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 -v -run TestTileBodyReconstitutionInvariant ./tests/parity/ 2>&1 | tail -15
```

Expected: all subtests pass (or skip if fixture absent).

```bash
git add tests/parity/tilebody_parity_test.go
git commit -m "$(cat <<'EOF'
test(v0.13): T5 — cross-format TileBody reconstitution invariant

Runs the v0.13 invariant on one fixture per format:

  SpliceJPEGTile(TilePrefix(), TileBodyInto(p)) ==byte== Tile(p)

Coverage:
  SVS, NDPI, Philips, OME, BIF (both Ventana-1 + OS-1 to exercise
  the per-tile-embedded vs shared-JPEGTables split), IFE,
  generictiff, Leica SCN (single-region + multi-region; the latter
  exercises Q4 blank-fill behavior).

5 sampled positions per L0 (corners + center). Catches any format
that implemented the new methods inconsistently with what Tile()
returns.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T6 — Bench harness (Pattern A vs B bandwidth comparison)

**Files:**
- Create: `tests/parity/tilebody_bench_test.go`

- [ ] **Step 1: Write the bench**

```go
//go:build benchgate

package parity

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// BenchmarkTileBandwidth_PatternsAB measures total bytes shipped
// when iterating every tile in a representative L0 under two
// patterns:
//
//   Pattern A (current): server splices and ships full JPEG per tile.
//                        Bytes shipped = sum of len(Tile()).
//   Pattern B (v0.13):   server ships TilePrefix() once + per-tile
//                        TileBodyInto() output; client reconstitutes.
//                        Bytes shipped = len(prefix) + sum of body lengths.
//
// Reports per-fixture: Pattern A total, Pattern B total, savings %.
//
// Run via: go test -tags benchgate -count=1 -bench BenchmarkTileBandwidth -run '^$' ./tests/parity/
//
// Build-tag-gated; skipped in default `go test ./...`.
func BenchmarkTileBandwidth_PatternsAB(b *testing.B) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		b.Skip("OPENTILE_TESTDIR unset")
	}
	for _, tc := range []struct {
		subdir, name string
	}{
		{"svs", "CMU-1.svs"},
		{"philips-tiff", "Philips-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
		{"scn", "Leica-1.scn"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				b.Skipf("%s not present", path)
			}
			tiler, err := opentile.OpenFile(path)
			if err != nil {
				b.Fatal(err)
			}
			defer tiler.Close()

			lvl := tiler.Levels()[0]
			prefix := lvl.TilePrefix()
			bodyBuf := make([]byte, lvl.TileBodyMaxSize())

			var totalA, totalB int64
			ctx := context.Background()
			for pos, res := range lvl.Tiles(ctx) {
				if res.Err != nil {
					continue
				}
				totalA += int64(len(res.Bytes))
				n, err := lvl.TileBodyInto(pos.X, pos.Y, bodyBuf)
				if err != nil {
					b.Fatal(err)
				}
				totalB += int64(n)
			}
			totalB += int64(len(prefix)) // prefix shipped once

			savings := 100.0 * float64(totalA-totalB) / float64(totalA)
			b.Logf("%s L0: PatternA=%d bytes, PatternB=%d bytes, savings=%.1f%% (prefix=%d bytes, %d tiles)",
				tc.name, totalA, totalB, savings, len(prefix), lvl.Grid().W*lvl.Grid().H)
			_ = bytes.Equal // silence import
		})
	}
}
```

- [ ] **Step 2: Run + commit**

```bash
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -tags benchgate -bench BenchmarkTileBandwidth -benchtime 1x -run '^$' ./tests/parity/ 2>&1 | tail -20
```

Expected: each fixture logs `PatternA=N bytes, PatternB=M bytes, savings=X.X%`. Splice-format fixtures should show meaningful savings (CMU-1.svs typically ~50%+ reduction at L0 since each tile is ~2 KB and prefix is ~1 KB).

```bash
git add tests/parity/tilebody_bench_test.go
git commit -m "$(cat <<'EOF'
test(v0.13): T6 — Pattern A vs B bandwidth bench harness

tests/parity/tilebody_bench_test.go (build tag benchgate):

  Pattern A: ship full Tile() bytes per tile (current).
  Pattern B: ship TilePrefix() once per level + TileBodyInto bytes
             per tile.

Iterates every tile in L0 of representative SVS / Philips / OME /
SCN fixtures; reports total bytes shipped under each pattern and
savings %.

Provides a baseline regression-protection number for the prefix-
deduplication story. Run via:

  OPENTILE_TESTDIR=$PWD/sample_files go test -tags benchgate \
    -bench BenchmarkTileBandwidth -benchtime 1x -run '^$' \
    ./tests/parity/

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## T7 — Docs + ship

**Files:**
- Modify: `docs/perf.md` (addendum on Pattern A vs B)
- Modify: `docs/deferred.md` (§8g new — v0.13 retirement audit; §11 — remove `Level.TilePrefix()` accessor + `TileBorrow` partial — `TilePrefix` shipped, `TileBorrow` still parked)
- Modify: `CHANGELOG.md` ([0.13.0] section + [Unreleased] reset)
- Modify: `CLAUDE.md` (Current milestone bump v0.12 → v0.13; demote v0.12 to Previous)
- Modify: `README.md` (add interface evolution to Deviations table; mention SpliceJPEGTile in API section if applicable)

Mirror the v0.12 docs sweep pattern from T6+T8+T9 of v0.12's plan. The CHANGELOG should have a "Breaking changes" section labeled "Interface additions (additive only)" — the new methods break external Level implementations but no external implementations exist.

- [ ] **Step 1-3: Apply each doc update**

Use the existing v0.12 milestone as the reference template. Each doc section follows the established shape (CHANGELOG sections; CLAUDE.md milestone block; deferred.md retirement audit; README example values).

For deferred.md §8g: lists `Level.TilePrefix()` and `Level.TileBodyInto` as items shipped, notes that `TileBorrow` (the second v0.9 follow-on) remains parked.

- [ ] **Step 4: Final pre-commit verification**

```bash
go vet ./... 2>&1 | tail -5
gofmt -l . 2>&1 | grep -v sample_files | grep -v 'docs/' | head
OPENTILE_TESTDIR=/Users/cornish/GitHub/opentile-go/sample_files go test -count=1 ./... 2>&1 | tail -8
```

Expected: vet clean; gofmt clean on v0.13-touched files (pre-existing drift acceptable per v0.12 lessons); every package green.

- [ ] **Step 5: Commit**

```bash
git add docs/perf.md docs/deferred.md CHANGELOG.md CLAUDE.md README.md
git commit -m "$(cat <<'EOF'
docs(v0.13): T7 — perf addendum + CHANGELOG + CLAUDE.md milestone bump + README

docs/perf.md addendum: Pattern A (current Tile) vs Pattern B
(TilePrefix once + TileBodyInto per tile) bandwidth comparison.

docs/deferred.md §8g new — Retired in v0.13: lists Level.TilePrefix /
TileBodyInto / TileBodyMaxSize / opentile.SpliceJPEGTile as the
v0.9 perf-followon TilePrefix half (shipped); TileBorrow zero-copy
mmap aliasing remains parked (out of scope for this milestone).
§11 backlog updated.

CHANGELOG.md [0.13.0] section: Added (3 Level methods + helper),
Notes (additive interface evolution; bench harness; v1.0 still
not committed).

CLAUDE.md: bump Current milestone v0.12 → v0.13.

README.md: mention SpliceJPEGTile in client-server section if
applicable.

End of milestone; v0.13 ready to merge + tag.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage check:**
- §1.1 `Level.TilePrefix()` → T1 (signature) + T2/T3/T4 (specializations).
- §1.2 `Level.TileBodyInto` + `TileBodyMaxSize` → T1 + T2/T3/T4.
- §1.3 `opentile.SpliceJPEGTile` → T1.
- §1.4 Bench harness → T6.
- §3.1 Interface signatures → T1.
- §3.2 Top-level helper → T1.
- §3.3 Per-format applicability matrix → T2 (SVS), T3 (Philips/OME/leicascn/generictiff), T4 (BIF mixed), T1 defaults (NDPI/IFE/non-applicable).
- §4 Test strategy → T2/T3/T4 per-format tests + T5 cross-format invariant.
- §5 Sealed Q-decisions → reflected throughout.
- §6 No new active limitations → confirmed in T7 docs.

No spec section uncovered.

**Placeholder scan:** every step has exact code blocks, exact paths, expected outputs, and HEREDOC commit messages. T3 has one judgment-call instruction ("for each format, audit + apply T2 pattern") but the pattern itself is shown explicitly in T2 + T3's leicascn block.

**Type consistency:** `TilePrefix` / `TileBodyInto` / `TileBodyMaxSize` / `SpliceJPEGTile` / `ErrBadJPEGSplice` used identically across T1 → T7. The cached field `bodyMaxSize int` and `splicePrefix []byte` mentioned consistently.

**Risks:**

- **R1 — leicascn compositeLevel complexity.** Multi-region + blank-fill make T3's leicascn block more involved than the other splice formats. The Q4 design call (blank-fill bodies = wasteful-but-valid) is documented but the implementation needs care. Mitigation: T5 cross-format test specifically includes Leica-2 (multi-region + blank tiles) to verify the invariant holds.
- **R2 — BIF mixed cases at construction.** T4 needs the level constructor to detect shared vs per-tile embedded JPEGTables. Existing v0.7 BIF code already handles this distinction; T4 just exposes the existing state. Mitigation: T4 step 1 explicitly audits the existing splicePrefix population logic.
- **R3 — Interface change blocks build.** Adding 3 methods to Level is a hard build-break for any implementer that doesn't get all 3 added in T1. Mitigation: T1 step 5 ships no-op defaults on every implementer; subsequent tasks specialize without breaking.
- **R4 — sync.Pool buffer-size mismatch.** Consumers using existing `sync.Pool` keyed on `TileMaxSize()` won't have buffers sized for `TileBodyMaxSize()` (which is smaller). Acceptable: the buffer is bigger than needed, not smaller; over-sized dst is fine. Documented in the TileBodyMaxSize doc comment.
