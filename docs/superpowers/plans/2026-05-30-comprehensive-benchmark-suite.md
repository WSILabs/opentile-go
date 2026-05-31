# Comprehensive Benchmark Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A standing, cross-format benchmark suite that emits competitive numbers (vs openslide + python opentile), gates per-format throughput regressions in CI, and supports profiling.

**Architecture:** Hybrid two layers sharing one matrix + one openslide shim. A Go-benchmark layer (`bench/`, table-driven over format × pattern × threading; openslide compared in-process via a build-tagged in-house cgo shim) gives the regression gate + profiling. A `cmd/bench/compare` report harness drives the openslide shim in-process and python opentile as a subprocess, emitting an opentile-go-vs-references table. The openslide shim and the compare main are behind `//go:build openslidebench`, so the shipping library keeps its single cgo dependency (`internal/jpegturbo`).

**Tech Stack:** Go 1.23+, `testing.B` + `b.ReportMetric`, cgo against libopenslide (`pkg-config openslide`, build-tagged), Python 3 (python opentile 0.20.0 via `OPENTILE_ORACLE_PYTHON`), Make.

**Spec:** `docs/superpowers/specs/2026-05-30-comprehensive-benchmark-suite-design.md`

**Key facts for the implementer (verified):**
- `opentile.OpenFile(path) (*Slide, error)`; `defer s.Close()`. Import the registration packages: `_ "github.com/wsilabs/opentile-go/decoder/all"` and `_ "github.com/wsilabs/opentile-go/formats/all"`.
- `s.Levels() []Level`; a `Level` has `.Size opentile.Size` (`.W/.H`), `.TileSize opentile.Size`, `.Grid() opentile.Size`, `.Tile(x, y) ([]byte, error)`.
- `s.ImageDecodedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error)` — image=0, level=0 for these benches.
- `s.ReadRegion(level, x, y, w, h int, opts ...DecodeOption) (*decoder.Image, error)` — x,y,w,h in level-pixel coords.
- `decoder.Image` has `Width, Height, Stride int`, `Format`, `Pix []byte`.
- Existing C reference: `cmd/bench/ndpi/openslide_ref/bench-openslide.c` (openslide_open / openslide_get_level_dimensions / openslide_read_region into ARGB uint32 / openslide_get_error / openslide_close).
- Python interpreter resolution convention (from `tests/oracle/openslide_session.go`): `OPENTILE_OPENSLIDE_PYTHON` > `OPENTILE_ORACLE_PYTHON` > `python3`.
- Fixture root: `OPENTILE_TESTDIR` (Makefile sets it to `$(PWD)/sample_files`).

---

## File Structure

| File | Responsibility |
|---|---|
| `bench/matrix.go` (pkg `bench`) | Canonical fixture matrix (format → fixture + overlap flags + floors), `FixturePath`, `MpixPerSec`. Imported by the benchmarks and the compare main. |
| `bench/matrix_test.go` (pkg `bench`) | Unit tests for the matrix helpers (no fixtures needed). |
| `bench/read_bench_test.go` (pkg `bench_test`) | Internal-axis benchmarks: `BenchmarkRead/<format>/<pattern>/<threads>`. |
| `internal/openslideshim/openslide.go` (`//go:build openslidebench`) | In-house cgo shim: Open/LevelDimensions/ReadRegion/Close. |
| `internal/openslideshim/openslide_test.go` (`//go:build openslidebench`) | Smoke test for the shim. |
| `bench/openslide_bench_test.go` (`//go:build openslidebench`, pkg `bench_test`) | `BenchmarkOpenslide/<format>/readregion`. |
| `cmd/bench/compare/opentile_perf.py` | python-opentile perf runner (prints JSON). |
| `cmd/bench/compare/main.go` (`//go:build openslidebench`) | Cross-language report: opentile-go vs openslide vs python. |
| `cmd/bench/compare/README.md` | How to run the competitive report + benchstat. |
| `Makefile` | `bench-all` (per-format floor gate) + `bench-compare` (report) + floor vars. |
| `docs/perf.md` | Document the suite. |

---

## Task 1: Matrix package + helpers

**Files:**
- Create: `bench/matrix.go`
- Create: `bench/matrix_test.go`

- [ ] **Step 1: Write the failing test**

Create `bench/matrix_test.go`:

```go
package bench

import (
	"testing"
	"time"
)

func TestMpixPerSec(t *testing.T) {
	// 2,000,000 pixels in 1s = 2 Mpix/s.
	if got := MpixPerSec(2_000_000, time.Second); got != 2.0 {
		t.Fatalf("MpixPerSec = %v, want 2.0", got)
	}
	if got := MpixPerSec(100, 0); got != 0 {
		t.Fatalf("MpixPerSec(_, 0) = %v, want 0 (no div-by-zero)", got)
	}
}

func TestMatrixWellFormed(t *testing.T) {
	if len(Matrix) != 10 {
		t.Fatalf("Matrix has %d entries, want 10 (one per supported format)", len(Matrix))
	}
	seen := map[string]bool{}
	for _, e := range Matrix {
		if e.Format == "" || e.Fixture == "" {
			t.Errorf("entry %+v has empty Format or Fixture", e)
		}
		if seen[e.Format] {
			t.Errorf("duplicate format %q in Matrix", e.Format)
		}
		seen[e.Format] = true
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./bench/ -run 'TestMpixPerSec|TestMatrixWellFormed' -count=1`
Expected: FAIL — `undefined: MpixPerSec`, `Matrix`, `FormatEntry`. (A `ld: warning: ignoring duplicate libraries` line is benign.)

- [ ] **Step 3: Implement the matrix package**

Create `bench/matrix.go`:

```go
// Package bench holds the shared benchmark fixture matrix and helpers
// used by both the Go-benchmark layer (bench/*_test.go) and the
// cross-language compare harness (cmd/bench/compare).
package bench

import (
	"os"
	"path/filepath"
	"time"
)

// FormatEntry describes one format's representative benchmark fixture
// and which external references can also read it.
type FormatEntry struct {
	Format    string // opentile-go format id (matches Slide.Format())
	Fixture   string // path relative to OPENTILE_TESTDIR
	Openslide bool   // openslide can read it (competitive on ReadRegion)
	Python    bool   // python opentile can read it (competitive on Tile)
}

// Matrix is the canonical benchmark fixture set: one representative
// fixture per supported format. Overlap flags drive the competitive
// axes; the harness still skips gracefully if a reader can't open a
// given fixture.
var Matrix = []FormatEntry{
	{"svs", "svs/CMU-1.svs", true, true},
	{"ndpi", "ndpi/CMU-1.ndpi", true, true},
	{"philips-tiff", "philips-tiff/Philips-1.tiff", true, true},
	{"ome-tiff", "ome-tiff/Leica-1.ome.tiff", false, true},
	{"bif", "bif/Ventana-1.bif", true, false},
	{"ife", "ife/cervix_2x_jpeg.iris", false, false},
	{"generic-tiff", "generic-tiff/CMU-1.tiff", true, false},
	{"leica-scn", "scn/Leica-1.scn", true, false},
	{"szi", "szi/CMU-1.szi", false, false},
	{"cog-wsi", "cog-wsi/CMU-1_cog-wsi.tiff", false, false},
}

// testDir returns the fixture root. Honors OPENTILE_TESTDIR (the
// Makefile sets it absolutely); defaults to ./sample_files.
func testDir() string {
	if d := os.Getenv("OPENTILE_TESTDIR"); d != "" {
		return d
	}
	return "sample_files"
}

// FixturePath resolves an entry's fixture path and reports whether the
// file exists on disk.
func FixturePath(rel string) (string, bool) {
	p := filepath.Join(testDir(), rel)
	if _, err := os.Stat(p); err != nil {
		return p, false
	}
	return p, true
}

// MpixPerSec returns megapixels/second for pixels processed over d.
// Returns 0 for non-positive d (no division by zero).
func MpixPerSec(pixels int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(pixels) / d.Seconds() / 1e6
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./bench/ -run 'TestMpixPerSec|TestMatrixWellFormed' -count=1`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add bench/matrix.go bench/matrix_test.go
git commit -m "bench: add shared fixture matrix + Mpix/s helper"
```

End every commit in this plan with a trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

---

## Task 2: Go-benchmark layer (internal axis)

`BenchmarkRead/<format>/<pattern>/<threads>` over the matrix, three
patterns × single/parallel. Benchmarks aren't TDD'd (no fail→pass); the
verification is running one and confirming it emits a `Mpix/s` metric.

**Files:**
- Create: `bench/read_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

Create `bench/read_bench_test.go`:

```go
package bench_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/bench"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// coordGrid returns up to a 16×16 block of interior tile coordinates,
// offset one tile in from the origin to avoid edge tiles. Iterations
// cycle through these so the working set is bounded and deterministic.
func coordGrid(base opentile.Level) [][2]int {
	g := base.Grid()
	maxX, maxY := g.W, g.H
	if maxX > 17 {
		maxX = 17
	}
	if maxY > 17 {
		maxY = 17
	}
	var out [][2]int
	for y := 1; y < maxY; y++ {
		for x := 1; x < maxX; x++ {
			out = append(out, [2]int{x, y})
		}
	}
	if len(out) == 0 {
		out = [][2]int{{0, 0}} // tiny fixtures: one tile
	}
	return out
}

// pattern is one read workload. read() performs a single read at the
// given tile coords and returns the pixel count covered (for Mpix/s).
type pattern struct {
	name string
	read func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error)
}

var patterns = []pattern{
	{"tile", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		b, err := base.Tile(tx, ty)
		ts := base.TileSize()
		_ = b
		return int64(ts.W) * int64(ts.H), err
	}},
	{"decodedtile", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		img, err := s.ImageDecodedTile(0, 0, tx, ty)
		ts := base.TileSize()
		_ = img
		return int64(ts.W) * int64(ts.H), err
	}},
	{"readregion", func(s *opentile.Slide, base opentile.Level, tx, ty int) (int64, error) {
		ts := base.TileSize()
		img, err := s.ReadRegion(0, tx*ts.W, ty*ts.H, ts.W, ts.H)
		_ = img
		return int64(ts.W) * int64(ts.H), err
	}},
}

func BenchmarkRead(b *testing.B) {
	for _, e := range bench.Matrix {
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("fixture missing: %s", path) })
			continue
		}
		s, err := opentile.OpenFile(path)
		if err != nil {
			b.Run(e.Format, func(b *testing.B) { b.Fatalf("open %s: %v", path, err) })
			continue
		}
		base := s.Levels()[0]
		coords := coordGrid(base)
		tilePix := int64(base.TileSize().W) * int64(base.TileSize().H)

		for _, p := range patterns {
			p := p
			b.Run(e.Format+"/"+p.name+"/single", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					c := coords[i%len(coords)]
					if _, err := p.read(s, base, c[0], c[1]); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
			})
			b.Run(e.Format+"/"+p.name+"/parallel", func(b *testing.B) {
				b.ReportAllocs()
				var i int
				b.RunParallel(func(pb *testing.PB) {
					local := i
					i++
					n := 0
					for pb.Next() {
						c := coords[(local+n)%len(coords)]
						n++
						if _, err := p.read(s, base, c[0], c[1]); err != nil {
							b.Fatal(err)
						}
					}
				})
				// total ops not directly available under RunParallel;
				// report throughput via elapsed + b.N (b.N == total ops).
				b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
			})
		}
		s.Close()
	}
}
```

- [ ] **Step 2: Run one format to verify it emits Mpix/s**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./bench/ -bench 'BenchmarkRead/ndpi' -run '^$' -benchmem 2>&1 | grep -vE "duplicate libraries"`
Expected: lines like `BenchmarkRead/ndpi/tile/single-10  ...  N Mpix/s  ... allocs/op`, three patterns × {single, parallel}, all completing without `FAIL`.

- [ ] **Step 3: Verify absent fixtures skip cleanly (don't fail)**

Run: `OPENTILE_TESTDIR=/tmp/nonexistent go test ./bench/ -bench 'BenchmarkRead' -run '^$' 2>&1 | grep -vE "duplicate libraries" | grep -E "SKIP|ok|FAIL" | head`
Expected: `--- SKIP` lines per format, final `ok` (no FAIL — missing fixtures skip, never error).

- [ ] **Step 4: Verify the whole matrix runs (present fixtures)**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./bench/ -bench 'BenchmarkRead' -run '^$' -benchtime 50x 2>&1 | grep -vE "duplicate libraries" | grep -cE "Mpix/s"`
Expected: a count > 0 (a Mpix/s line per present format×pattern×threading); no `FAIL`. (`-benchtime 50x` keeps it quick.)

- [ ] **Step 5: Commit**

```bash
git add bench/read_bench_test.go
git commit -m "bench: table-driven BenchmarkRead across formats/patterns/threading"
```

---

## Task 3: openslide cgo shim

In-house, build-tagged. Ported from the existing 40-line C reference.

**Files:**
- Create: `internal/openslideshim/openslide.go`
- Create: `internal/openslideshim/openslide_test.go`

- [ ] **Step 1: Write the failing smoke test**

Create `internal/openslideshim/openslide_test.go`:

```go
//go:build openslidebench

package openslideshim

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadClose(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %s", path)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	w, h := s.LevelDimensions(0)
	if w <= 0 || h <= 0 {
		t.Fatalf("LevelDimensions = %dx%d, want positive", w, h)
	}
	buf := make([]uint32, 256*256)
	if err := s.ReadRegion(buf, 0, 0, 0, 256, 256); err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails (package doesn't exist yet)**

Run: `go test -tags openslidebench ./internal/openslideshim/ -run TestOpenReadClose -count=1 2>&1 | grep -vE "duplicate libraries"`
Expected: FAIL — build error `undefined: Open` (no `openslide.go` yet).

- [ ] **Step 3: Implement the shim**

Create `internal/openslideshim/openslide.go`:

```go
//go:build openslidebench

// Package openslideshim is a minimal cgo wrapper over libopenslide,
// used ONLY by the benchmark suite to compare opentile-go against
// openslide. It is gated behind the `openslidebench` build tag so the
// shipping library keeps its single cgo dependency (internal/jpegturbo)
// — normal builds, `go test`, CI, and the `nocgo` build never compile
// this file.
package openslideshim

/*
#cgo pkg-config: openslide
#include <openslide.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Slide is an open openslide handle.
type Slide struct {
	h *C.openslide_t
}

// Open opens path with openslide.
func Open(path string) (*Slide, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	h := C.openslide_open(cpath)
	if h == nil {
		return nil, fmt.Errorf("openslideshim: openslide_open(%q) returned NULL", path)
	}
	if errp := C.openslide_get_error(h); errp != nil {
		msg := C.GoString(errp)
		C.openslide_close(h)
		return nil, fmt.Errorf("openslideshim: open error: %s", msg)
	}
	return &Slide{h: h}, nil
}

// LevelDimensions returns the pixel dimensions of the given level.
func (s *Slide) LevelDimensions(level int) (w, h int64) {
	var cw, ch C.int64_t
	C.openslide_get_level_dimensions(s.h, C.int32_t(level), &cw, &ch)
	return int64(cw), int64(ch)
}

// ReadRegion reads a w×h region whose top-left is (x, y) in level-0
// reference coordinates, at the given level, into dst as packed ARGB
// (pre-multiplied) uint32 pixels. dst must hold at least w*h elements.
func (s *Slide) ReadRegion(dst []uint32, level int, x, y, w, h int64) error {
	if int64(len(dst)) < w*h {
		return fmt.Errorf("openslideshim: dst len %d < w*h %d", len(dst), w*h)
	}
	C.openslide_read_region(s.h,
		(*C.uint32_t)(unsafe.Pointer(&dst[0])),
		C.int64_t(x), C.int64_t(y), C.int32_t(level),
		C.int64_t(w), C.int64_t(h))
	if errp := C.openslide_get_error(s.h); errp != nil {
		return fmt.Errorf("openslideshim: read_region error: %s", C.GoString(errp))
	}
	return nil
}

// Close releases the handle.
func (s *Slide) Close() {
	if s.h != nil {
		C.openslide_close(s.h)
		s.h = nil
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags openslidebench ./internal/openslideshim/ -run TestOpenReadClose -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL|---)"`
Expected: `ok` (or `--- SKIP` if CMU-1.ndpi absent). Requires openslide installed (`pkg-config --exists openslide`).

- [ ] **Step 5: Verify the shim does NOT compile into normal builds**

Run: `go build ./... 2>&1 | grep -vE "duplicate libraries|^#" ; echo "build exit=$?"`
Expected: clean build, exit 0 — the shim is invisible without the tag (the `internal/openslideshim` package compiles to nothing under a normal build because its only file is tagged out).

- [ ] **Step 6: Commit**

```bash
git add internal/openslideshim/openslide.go internal/openslideshim/openslide_test.go
git commit -m "bench: in-house build-tagged openslide cgo shim (openslidebench)"
```

---

## Task 4: openslide benchmark layer

`BenchmarkOpenslide/<format>/readregion` mirroring the opentile-go
ReadRegion benchmark, so the two are benchstat-comparable.

**Files:**
- Create: `bench/openslide_bench_test.go`

- [ ] **Step 1: Write the build-tagged benchmark**

Create `bench/openslide_bench_test.go`:

```go
//go:build openslidebench

package bench_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/bench"
	"github.com/wsilabs/opentile-go/internal/openslideshim"
)

// BenchmarkOpenslide mirrors BenchmarkRead/<format>/readregion using
// openslide, for the formats openslide can read. 256×256 regions on a
// bounded interior grid, single + parallel.
func BenchmarkOpenslide(b *testing.B) {
	const ts = 256
	for _, e := range bench.Matrix {
		if !e.Openslide {
			continue
		}
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("fixture missing: %s", path) })
			continue
		}
		s, err := openslideshim.Open(path)
		if err != nil {
			b.Run(e.Format, func(b *testing.B) { b.Skipf("openslide cannot open %s: %v", path, err) })
			continue
		}
		w, h := s.LevelDimensions(0)
		// bounded interior grid of 256-tiles, offset 1 in.
		var coords [][2]int64
		for ty := int64(1); ty < 17 && (ty+1)*ts <= h; ty++ {
			for tx := int64(1); tx < 17 && (tx+1)*ts <= w; tx++ {
				coords = append(coords, [2]int64{tx * ts, ty * ts})
			}
		}
		if len(coords) == 0 {
			coords = [][2]int64{{0, 0}}
		}
		const tilePix = int64(ts) * int64(ts)

		b.Run(e.Format+"/readregion/single", func(b *testing.B) {
			buf := make([]uint32, ts*ts)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := coords[i%len(coords)]
				if err := s.ReadRegion(buf, 0, c[0], c[1], ts, ts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
		})
		b.Run(e.Format+"/readregion/parallel", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				buf := make([]uint32, ts*ts) // openslide is thread-safe; per-goroutine buffer
				n := 0
				for pb.Next() {
					c := coords[n%len(coords)]
					n++
					if err := s.ReadRegion(buf, 0, c[0], c[1], ts, ts); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.ReportMetric(bench.MpixPerSec(int64(b.N)*tilePix, b.Elapsed()), "Mpix/s")
		})
		s.Close()
	}
}
```

- [ ] **Step 2: Run the openslide benchmark for one format**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags openslidebench ./bench/ -bench 'BenchmarkOpenslide/ndpi' -run '^$' 2>&1 | grep -vE "duplicate libraries"`
Expected: `BenchmarkOpenslide/ndpi/readregion/single-10 ... N Mpix/s` etc., no FAIL.

- [ ] **Step 3: Sanity — compare side by side**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test -tags openslidebench ./bench/ -bench '(BenchmarkRead|BenchmarkOpenslide)/ndpi/readregion/single' -run '^$' -benchtime 100x 2>&1 | grep -E "Mpix/s"`
Expected: two lines (opentile-go and openslide) with comparable Mpix/s for NDPI ReadRegion — the head-to-head the user asked about, now in one harness.

- [ ] **Step 4: Commit**

```bash
git add bench/openslide_bench_test.go
git commit -m "bench: BenchmarkOpenslide ReadRegion via the cgo shim (openslidebench)"
```

---

## Task 5: python-opentile perf runner

A standalone script the compare main shells out to. Prints one JSON line.

**Files:**
- Create: `cmd/bench/compare/opentile_perf.py`

- [ ] **Step 1: Write the runner**

Create `cmd/bench/compare/opentile_perf.py`:

```python
#!/usr/bin/env python3
"""Perf runner for python opentile 0.20.0.

Opens a slide, times N get_tile() calls over a bounded interior tile
grid on level 0, and prints one JSON line:
  {"format": str, "tiles": int, "pixels": int, "seconds": float,
   "mpix_per_s": float}
Exits non-zero with a JSON {"error": ...} line if the slide can't be
opened by python opentile (the Go caller treats that as "skip").
"""
import json
import sys
import time

try:
    from opentile import OpenTile
except Exception as e:  # noqa: BLE001
    print(json.dumps({"error": f"import opentile: {e}"}))
    sys.exit(3)


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "usage: opentile_perf.py <slide>"}))
        sys.exit(2)
    path = sys.argv[1]
    try:
        tiler = OpenTile.open(path)
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"error": f"open: {e}"}))
        sys.exit(1)

    level = tiler.get_level(0)
    cols, rows = level.tiled_size.width, level.tiled_size.height
    tw, th = level.tile_size.width, level.tile_size.height

    coords = []
    for ty in range(1, min(rows, 17)):
        for tx in range(1, min(cols, 17)):
            coords.append((tx, ty))
    if not coords:
        coords = [(0, 0)]

    # Warm one tile (first-access decode/IO), then time a full pass.
    level.get_tile(coords[0])
    iters = max(len(coords), 50)
    t0 = time.perf_counter()
    for i in range(iters):
        tx, ty = coords[i % len(coords)]
        level.get_tile((tx, ty))
    el = time.perf_counter() - t0

    pixels = iters * tw * th
    print(json.dumps({
        "format": getattr(tiler, "metadata", None) and "" or "",
        "tiles": iters,
        "pixels": pixels,
        "seconds": el,
        "mpix_per_s": (pixels / el / 1e6) if el > 0 else 0.0,
    }))


if __name__ == "__main__":
    main()
```

Note: the exact python opentile API (`OpenTile.open`, `get_level`,
`tiled_size`, `tile_size`, `get_tile`) follows python opentile 0.20.0.
If an attribute differs in the installed version, fix the access to
match — the contract is "time N get_tile calls, print the JSON line."

- [ ] **Step 2: Verify it runs against a fixture**

Run: `${OPENTILE_ORACLE_PYTHON:-python3} cmd/bench/compare/opentile_perf.py "$PWD/sample_files/svs/CMU-1.svs"`
Expected: one JSON line with a positive `mpix_per_s` (using the oracle venv that has python opentile installed). If you get `{"error": "import opentile..."}`, set `OPENTILE_ORACLE_PYTHON` to the venv interpreter and retry.

- [ ] **Step 3: Verify graceful error on an unreadable format**

Run: `${OPENTILE_ORACLE_PYTHON:-python3} cmd/bench/compare/opentile_perf.py "$PWD/sample_files/szi/CMU-1.szi"; echo "exit=$?"`
Expected: a `{"error": ...}` JSON line and non-zero exit (python opentile doesn't read SZI) — the Go caller will treat this as "skip", not a crash.

- [ ] **Step 4: Commit**

```bash
git add cmd/bench/compare/opentile_perf.py
git commit -m "bench: python-opentile perf runner (JSON throughput)"
```

---

## Task 6: compare main (cross-language report)

Build-tagged (needs the openslide shim). Drives all three engines and
prints a table + JSON.

**Files:**
- Create: `cmd/bench/compare/main.go`
- Create: `cmd/bench/compare/README.md`

- [ ] **Step 1: Write the compare main**

Create `cmd/bench/compare/main.go`:

```go
//go:build openslidebench

// Command compare emits the cross-language competitive benchmark report:
// opentile-go vs openslide (ReadRegion) and vs python opentile (Tile),
// across the format overlap. Build with -tags openslidebench (needs
// libopenslide); the python axis needs a python-opentile interpreter via
// OPENTILE_ORACLE_PYTHON.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/bench"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/internal/openslideshim"
)

const ts = 256

type row struct {
	Format          string  `json:"format"`
	OpentileRegion  float64 `json:"opentile_readregion_mpixs"`
	OpenslideRegion float64 `json:"openslide_readregion_mpixs"`
	OpentileTile    float64 `json:"opentile_tile_mpixs"`
	PythonTile      float64 `json:"python_tile_mpixs"`
}

func pythonBin() string {
	for _, k := range []string{"OPENTILE_OPENSLIDE_PYTHON", "OPENTILE_ORACLE_PYTHON"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "python3"
}

// timeOpentileRegion times opentile-go ReadRegion over a bounded grid.
func timeOpentileRegion(path string) float64 {
	s, err := opentile.OpenFile(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	base := s.Levels()[0]
	tw, th := base.TileSize().W, base.TileSize().H
	g := base.Grid()
	n := 0
	t0 := time.Now()
	for ty := 1; ty < 17 && ty < g.H; ty++ {
		for tx := 1; tx < 17 && tx < g.W; tx++ {
			if _, err := s.ReadRegion(0, tx*tw, ty*th, tw, th); err == nil {
				n++
			}
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*int64(tw)*int64(th), el)
}

// timeOpentileTile times opentile-go Tile (compressed) over the grid.
func timeOpentileTile(path string) float64 {
	s, err := opentile.OpenFile(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	base := s.Levels()[0]
	tw, th := base.TileSize().W, base.TileSize().H
	g := base.Grid()
	n := 0
	t0 := time.Now()
	for ty := 1; ty < 17 && ty < g.H; ty++ {
		for tx := 1; tx < 17 && tx < g.W; tx++ {
			if _, err := base.Tile(tx, ty); err == nil {
				n++
			}
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*int64(tw)*int64(th), el)
}

func timeOpenslideRegion(path string) float64 {
	s, err := openslideshim.Open(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	w, h := s.LevelDimensions(0)
	buf := make([]uint32, ts*ts)
	n := 0
	t0 := time.Now()
	for ty := int64(1); ty < 17 && (ty+1)*ts <= h; ty++ {
		for tx := int64(1); tx < 17 && (tx+1)*ts <= w; tx++ {
			if err := s.ReadRegion(buf, 0, tx*ts, ty*ts, ts, ts); err == nil {
				n++
			}
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*ts*ts, el)
}

func timePythonTile(path string) float64 {
	script := filepath.Join("cmd", "bench", "compare", "opentile_perf.py")
	out, err := exec.Command(pythonBin(), script, path).Output()
	if err != nil {
		return 0
	}
	var res struct {
		MpixPerS float64 `json:"mpix_per_s"`
		Error    string  `json:"error"`
	}
	if json.Unmarshal(out, &res) != nil || res.Error != "" {
		return 0
	}
	return res.MpixPerS
}

func main() {
	fmt.Printf("compare: %d-core %s/%s\n\n", runtime.NumCPU(), runtime.GOOS, runtime.GOARCH)
	var rows []row
	for _, e := range bench.Matrix {
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			fmt.Fprintf(os.Stderr, "skip %s: fixture missing %s\n", e.Format, path)
			continue
		}
		r := row{Format: e.Format}
		r.OpentileRegion = timeOpentileRegion(path)
		r.OpentileTile = timeOpentileTile(path)
		if e.Openslide {
			r.OpenslideRegion = timeOpenslideRegion(path)
		}
		if e.Python {
			r.PythonTile = timePythonTile(path)
		}
		rows = append(rows, r)
	}

	// Markdown table.
	fmt.Println("| format | ReadRegion: opentile-go | openslide | ratio | Tile: opentile-go | python | ratio |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, r := range rows {
		ratio := func(a, b float64) string {
			if b == 0 {
				return "—"
			}
			return fmt.Sprintf("%.2fx", a/b)
		}
		num := func(v float64) string {
			if v == 0 {
				return "—"
			}
			return fmt.Sprintf("%.0f", v)
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
			r.Format,
			num(r.OpentileRegion), num(r.OpenslideRegion), ratio(r.OpentileRegion, r.OpenslideRegion),
			num(r.OpentileTile), num(r.PythonTile), ratio(r.OpentileTile, r.PythonTile))
	}

	// JSON sidecar to stderr-free stdout tail.
	js, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Printf("\n```json\n%s\n```\n", js)
}
```

- [ ] **Step 2: Build it**

Run: `go build -tags openslidebench -o /tmp/bench-compare ./cmd/bench/compare/ 2>&1 | grep -vE "duplicate libraries|^#" ; echo "build exit=$?"`
Expected: builds, exit 0.

- [ ] **Step 3: Run the report**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" /tmp/bench-compare 2>/dev/null`
Expected: a Markdown table with one row per present format; opentile-go columns populated for all, openslide column populated for the overlap formats it can open, python column for SVS/NDPI/Philips/OME (if the oracle venv is set). Plus a JSON block. Engines that can't read a format show `—`, never crash.

- [ ] **Step 4: Write the README**

Create `cmd/bench/compare/README.md`:

```markdown
# cmd/bench/compare

Cross-language competitive benchmark report: opentile-go vs openslide
(ReadRegion) and vs python opentile (Tile), across the format overlap.

## Requirements
- libopenslide (`pkg-config --exists openslide`)
- a python interpreter with python opentile 0.20.0, via
  `OPENTILE_ORACLE_PYTHON` (or `OPENTILE_OPENSLIDE_PYTHON`)
- fixtures under `OPENTILE_TESTDIR`

## Run
```sh
go build -tags openslidebench -o /tmp/bench-compare ./cmd/bench/compare/
OPENTILE_TESTDIR="$PWD/sample_files" \
  OPENTILE_ORACLE_PYTHON=/path/to/venv/bin/python \
  /tmp/bench-compare
```

Or `make bench-compare`.

## A/B a change (benchstat)
The Go-benchmark layer (`bench/`) is benchstat-friendly:
```sh
go test ./bench/ -bench BenchmarkRead -benchmem -count 6 > /tmp/before.txt
# ...make a change...
go test ./bench/ -bench BenchmarkRead -benchmem -count 6 > /tmp/after.txt
benchstat /tmp/before.txt /tmp/after.txt
```

`—` in the table means that engine cannot read that format (expected:
openslide can't read OME/IFE/SZI/COG-WSI; python opentile reads a
narrower set). Numbers are Mpix/s on a bounded interior tile grid.
```

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/compare/main.go cmd/bench/compare/README.md
git commit -m "bench: cmd/bench/compare cross-language report (opentile-go vs openslide vs python)"
```

---

## Task 7: Makefile gate + docs

**Files:**
- Modify: `Makefile`
- Modify: `docs/perf.md`

- [ ] **Step 1: Add the `bench-all` floor gate + `bench-compare` targets**

Append to `Makefile` (after the existing bench targets). The gate runs
the pure-Go internal benchmarks and fails if any `BenchmarkRead` line's
Mpix/s is below a per-format-pattern floor from the embedded awk table.
Set the floor values from Step 3's measured baselines (use ~90% of
measured).

```makefile
.PHONY: bench-all bench-compare

# Cross-format throughput gate. Runs the pure-Go internal benchmarks
# (no cgo/openslide/python) and fails if any BenchmarkRead line is below
# its floor. Floors are keyed "format/pattern" at ~90% of measured
# baseline; update when a deliberate speedup raises the bar.
bench-all: ## Cross-format throughput regression gate
	@OPENTILE_TESTDIR="$(OPENTILE_TESTDIR)" go test ./bench/ \
		-bench 'BenchmarkRead' -run '^$$' -benchtime 100x 2>/dev/null \
		| tee /tmp/bench-all.txt
	@awk ' \
	  BEGIN { \
	    floor["ndpi/tile/single"]=200; floor["ndpi/decodedtile/single"]=200; floor["ndpi/readregion/single"]=200; \
	    floor["svs/tile/single"]=400;  floor["svs/decodedtile/single"]=400;  floor["svs/readregion/single"]=400; \
	    fail=0 \
	  } \
	  /Mpix\/s/ { \
	    name=$$1; sub(/-[0-9]+$$/,"",name); sub(/^BenchmarkRead\//,"",name); \
	    for (i=1;i<=NF;i++) if ($$i=="Mpix/s") v=$$(i-1); \
	    if (name in floor && v+0 < floor[name]+0) { \
	      printf "FAIL: %s = %.1f Mpix/s < floor %d\n", name, v, floor[name]; fail=1 \
	    } \
	  } \
	  END { if (fail) exit 1; else print "bench-all: all gated lines >= floor" } \
	' /tmp/bench-all.txt

# Cross-language competitive report (on-demand; needs openslide + python
# opentile). Not run in CI.
bench-compare: ## Competitive report: opentile-go vs openslide vs python opentile
	@go build -tags openslidebench -o /tmp/bench-compare ./cmd/bench/compare/
	@OPENTILE_TESTDIR="$(OPENTILE_TESTDIR)" /tmp/bench-compare
```

(The floor table seeds only NDPI + SVS — the formats with a CI fixture
guaranteed present and an established baseline. Add more
format/pattern keys as their baselines stabilize; absent keys are
measured but not gated, matching the existing selective-gate
convention.)

- [ ] **Step 2: Verify `bench-all` passes at the seeded floors**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" make bench-all 2>&1 | grep -vE "duplicate libraries" | grep -E "FAIL|>= floor"`
Expected: `bench-all: all gated lines >= floor` (no FAIL). If a real number is below the placeholder floor, lower the floor to ~90% of the measured value and re-run.

- [ ] **Step 3: Tune floors from measured baselines**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./bench/ -bench 'BenchmarkRead/(ndpi|svs)/.*/single' -run '^$' -benchtime 100x 2>&1 | grep Mpix/s`
Read the actual Mpix/s for each ndpi/svs pattern, set each `floor[...]`
in the Makefile to ~90% of measured (rounded down). Re-run Step 2 to
confirm green.

- [ ] **Step 4: Verify the gate actually catches a regression**

Temporarily set `floor["ndpi/tile/single"]=999999` in the Makefile, run
`make bench-all`, confirm it prints `FAIL` and exits non-zero, then
revert the floor.

Run: `OPENTILE_TESTDIR="$PWD/sample_files" make bench-all; echo "exit=$?"` (after the temporary edit)
Expected: `FAIL: ndpi/tile/single ...` and `exit=1`. Revert after.

- [ ] **Step 5: Document the suite in docs/perf.md**

Append a section to `docs/perf.md`:

```markdown
## Benchmark suite (cross-format)

`bench/` is the standing benchmark suite (see the design doc). Three
layers:

- **`go test ./bench/ -bench BenchmarkRead`** — per-format throughput
  for `Tile` (compressed), `DecodedTile`, and `ReadRegion`, single and
  parallel, with `Mpix/s` + `allocs/op`. The profiling + regression
  instrument (`-cpuprofile`/`-memprofile` for profiles; `benchstat`
  across `-count` runs for A/B).
- **`make bench-all`** — the CI regression gate: fails if a gated
  `format/pattern` drops below its floor.
- **`make bench-compare`** — on-demand competitive report: opentile-go
  vs openslide (ReadRegion, in-process via a build-tagged cgo shim) vs
  python opentile (Tile, subprocess). Needs libopenslide + a
  python-opentile interpreter (`OPENTILE_ORACLE_PYTHON`). See
  `cmd/bench/compare/README.md`.

The openslide comparison uses an in-house ~40-line cgo shim gated behind
`//go:build openslidebench`, so the shipping library keeps its single
cgo dependency (`internal/jpegturbo`) — normal builds and CI never
compile it.
```

- [ ] **Step 6: Confirm the library build/test surface is unchanged**

Run: `go build ./... 2>&1 | grep -vE "duplicate libraries|^#"; OPENTILE_TESTDIR="$PWD/sample_files" make test 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL)" | grep FAIL; echo "done (no FAIL lines above = green)"`
Expected: build clean; no FAIL lines (the bench package + matrix tests pass under plain `go test`; benchmarks don't run without `-bench`; the openslide shim is tagged out).

- [ ] **Step 7: Commit**

```bash
git add Makefile docs/perf.md
git commit -m "bench: bench-all floor gate + bench-compare target + perf docs"
```

---

## Self-Review Notes

**Spec coverage:**
- D1 hybrid two-layer → Tasks 2 (Go layer) + 6 (compare). ✓
- D2 references openslide + python → Tasks 3/4 (openslide) + 5 (python). ✓
- D3 patterns Tile/DecodedTile/ReadRegion → Task 2 `patterns`. ✓
- D4 single + parallel → Task 2 (`/single`, `/parallel`). ✓
- D5 comparability (openslide↔ReadRegion, python↔Tile) → Tasks 4 + 6. ✓
- D6 in-house build-tagged shim → Task 3. ✓
- D7 all-10 internal / overlap competitive (skip-gracefully) → Task 1 Matrix flags + skip logic in Tasks 2/4/6. ✓
- D8 per-(format,pattern) Mpix/s floors → Task 7 (seeded for ndpi/svs; **deviation noted below**). ✓
- D9 one fixture per format → Task 1 Matrix. ✓
- §7 testing the suite itself → Task 1 (matrix tests), Task 3 (shim smoke test), Task 7 Step 6 (build surface unchanged). ✓
- §9 success criteria → Tasks 2 (all formats run), 4 (head-to-head), 6 (report), 7 (gate + build-unchanged). ✓

**Deviation from spec (flagged for review):** D8 says per-(format,pattern)
floors for *every* format. The plan seeds the gate with **NDPI + SVS
only** — the two formats with a CI-guaranteed fixture and an established
baseline — and documents adding more keys as baselines stabilize. Gating
all 10 from day one risks flaky CI on formats whose fixtures aren't
present in the CI environment (the `make test` corpus is local/gitignored).
The benchmarks still *measure* all formats; only the gated subset grows
deliberately. If you want all 10 gated immediately, that's a one-line-per-
format expansion of the awk table once baselines are captured.

**Type consistency:** `bench.Matrix`/`FormatEntry`/`FixturePath`/
`MpixPerSec` (Task 1) are used verbatim in Tasks 2, 4, 6.
`openslideshim.Open/LevelDimensions/ReadRegion/Close` (Task 3) used
verbatim in Tasks 4, 6. The `ts=256` region size is consistent across
Tasks 4 and 6. ✓

**Placeholder scan:** floor values in Task 7 are explicitly placeholders
to be set from measurement in Step 3 (the one legitimate "measure then
fill" — the step says exactly how). No other TBDs.
