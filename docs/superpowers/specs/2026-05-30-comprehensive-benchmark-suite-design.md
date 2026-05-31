# opentile-go — comprehensive benchmark suite design

**Status:** DESIGN for review (2026-05-30)
**Author:** brainstorm 2026-05-30
**Work branch (proposed):** `feat/bench-suite`

---

## 0. Problem statement

opentile-go's performance benchmarking is NDPI-heavy and partial. Today
there are perf benches for NDPI (single + multi, the only one compared
against openslide) and SVS (single + multi); the other **8 supported
formats have no perf bench**, and only one access pattern axis is well
covered. There is no standing, cross-format way to (a) state honestly how
opentile-go compares to other readers, (b) catch perf regressions per
format/pattern in CI, or (c) see where decode time goes per format.

This milestone builds **one standing benchmark suite serving all three
goals.**

## 1. Goals

1. **Competitive** — honest opentile-go-vs-reference numbers across
   formats and access patterns, emitted as a report worth citing.
2. **Regression gate** — per-(format, pattern) throughput floors that
   fail CI on a regression, for every supported format.
3. **Profiling map** — standard pprof hooks to see where time goes per
   format/pattern.

## 2. Sealed decisions

| # | Decision |
|---|---|
| D1 | **Hybrid two-layer** architecture (§3): a Go-benchmark layer + a cross-language report harness, sharing one matrix definition and one openslide shim. |
| D2 | **References: openslide + python opentile.** openslide via an in-house cgo shim; python opentile via a subprocess runner. |
| D3 | **Access patterns: `Tile` (compressed), `DecodedTile`, `ReadRegion`.** ScaledStrips is out of scope (covered by v0.30's `bench-ndpi-mem`). |
| D4 | **Threading: both** single-thread and N-thread. |
| D5 | **Comparability mapping:** openslide ↔ `ReadRegion` (decoded); python opentile ↔ `Tile` (compressed); `DecodedTile` is internal-only. (§4) |
| D6 | **openslide via an in-house ~40-line cgo shim**, build-tagged `//go:build openslidebench` — preserves the "one narrowly-scoped cgo dep" invariant (no third-party binding, no `go.mod` change). |
| D7 | **Internal axis = all 10 formats; openslide competitive = the format overlap** (SVS, NDPI, Philips-TIFF, BIF, Leica SCN, generic-TIFF), skipping gracefully when a fixture is missing or openslide can't open it; python competitive = whatever python opentile 0.20.0 opens (≈ SVS, NDPI, Philips, OME-TIFF), same skip-gracefully rule. |
| D8 | **Regression gate = per-(format, pattern) Mpix/s floors in the Makefile** (extends the existing `bench-ndpi` / `MIN_NDPI_MPIXS` pattern). `benchstat` is an on-demand A/B tool, not the gate. |
| D9 | **One representative fixture per format** for the standing matrix; the harness accepts more via `OPENTILE_TESTDIR` and skips absent fixtures. |

## 3. Architecture

Two layers share a single matrix definition and the openslide shim:

```
┌─ Go-benchmark layer  (regression gate · profiling · openslide-competitive) ─┐
│  bench/read_bench_test.go     table-driven over format × pattern × threads  │
│    BenchmarkRead/<format>/<pattern>/<threads>  → ns/op, B/op, allocs/op,     │
│                                                  + Mpix/s (ReportMetric)     │
│  bench/openslide_bench_test.go   (//go:build openslidebench)                 │
│    BenchmarkOpenslide/<format>/readregion  ──uses──┐                         │
└────────────────────────────────────────────────────┼────────────────────────┘
                                                      ▼
                              internal/openslideshim  (//go:build openslidebench)
                              in-house cgo: Open / LevelDimensions /
                              ReadRegion(into) / Close
                                                      ▲
┌─ cmd/bench/compare  (cross-language competitive REPORT) ─────────────────────┐
│  drives openslideshim (in-process) + python opentile (subprocess)            │
│  → normalized Mpix/s Markdown table + JSON: opentile-go vs openslide vs py    │
│  python runner: cmd/bench/compare/opentile_perf.py (OPENTILE_ORACLE_PYTHON)   │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Why hybrid (not one harness):** the three goals want different tools.
`go test -bench` + `benchstat` is the right regression/profiling
instrument and gives `allocs/op`/`B/op` for free (relevant after the
v0.29/v0.30 allocation work). A subprocess/in-process report harness is
the right cross-language competitive tool. Forcing all three into one
mechanism makes the regression axis weaker (no benchstat) or the
competitive axis hacky (shelling out inside `testing.B`). The layers
share fixtures and the `(format, pattern)` matrix table, so there is no
real duplication.

## 4. The matrix and comparability

### 4.1. Internal axis (regression + profiling) — all 10 formats

For each supported format, one representative fixture, three patterns,
two threadings:

| Pattern | opentile-go call | Measures |
|---|---|---|
| `Tile` | `Level.Tile(x,y)` / `TileInto` | compressed-tile read (server passthrough) |
| `DecodedTile` | `Slide.ImageDecodedTile(...)` | raw decode throughput (the v0.27 fast-path) |
| `ReadRegion` | `Slide.ReadRegion(level,x,y,w,h)` | decoded pixel region |

Formats: SVS, NDPI, Philips-TIFF, OME-TIFF, BIF, IFE, generic-TIFF,
Leica SCN, SZI, COG-WSI.

### 4.2. Competitive overlaps

| Reference | opentile-go pattern | Comparator API | Overlap formats |
|---|---|---|---|
| **openslide** | `ReadRegion` | `openslide_read_region` (decoded ARGB) | SVS, NDPI, Philips-TIFF, BIF, Leica SCN, generic-TIFF |
| **python opentile** | `Tile` | `OpenTile…get_tile()` (compressed bytes) | whatever python opentile 0.20.0 opens (≈ SVS, NDPI, Philips, OME-TIFF) |

Normalization: all throughput reported as **Mpix/s** = decoded (or
tile-covered) pixel count ÷ wall time. openslide returns 4-byte ARGB,
opentile-go `ReadRegion` returns 3-byte RGB; both normalize to the same
pixel count so the comparison is decode-work-per-second, not bytes.

`DecodedTile` has no clean external comparator and is **internal-only**
(isolates the decode step from region assembly).

## 5. Components

### 5.1. Go-benchmark layer (`bench/`)

- New directory `bench/` with benchmark files in **`package bench_test`**
  (an external test package; imports `opentile` via the public API only,
  so it can never depend on unexported internals).
- A single matrix table: `[]struct{ format string; fixture string;
  newPattern func(*Slide) func(b *testing.B) }` keyed by `(format,
  pattern)`. Benchmarks generated with `b.Run(name, …)`.
- Single-thread = the plain loop; N-thread = `b.RunParallel`.
- `b.ReportMetric(mpixPerSec, "Mpix/s")` so throughput rides next to
  `ns/op`.
- `b.Skip` when the fixture is absent (`OPENTILE_TESTDIR`), so the suite
  is green on machines without the full corpus.
- Profiling: standard `go test -bench … -cpuprofile/-memprofile`.

### 5.2. openslide shim (`internal/openslideshim`, `//go:build openslidebench`)

Minimal cgo wrapper ported from the existing 40-line
`cmd/bench/ndpi/openslide_ref/bench-openslide.c`. Exposes only what the
benchmark needs:

```go
type Slide struct{ /* opaque *C.openslide_t */ }
func Open(path string) (*Slide, error)
func (s *Slide) LevelDimensions(level int) (w, h int64)
func (s *Slide) ReadRegion(dst []uint32, level int, x, y, w, h int64) error // ARGB into dst
func (s *Slide) Close()
```

- `//go:build openslidebench` → compiled **only** when the tag is passed
  and openslide is installed. Normal `go build`/`go test`/CI/`nocgo`
  never touch it, so the shipping library keeps its single cgo dep
  (`internal/jpegturbo`). cgo flags from `pkg-config --cflags --libs
  openslide` via `#cgo pkg-config: openslide`.
- Shared by the `BenchmarkOpenslide/*` benchmarks and the
  `cmd/bench/compare` report.

### 5.3. python-opentile runner (`cmd/bench/compare/opentile_perf.py`)

A small script: open a slide with python opentile, time `N` `get_tile`
calls over a deterministic tile walk, print one JSON line
(`{format, tiles, pixels, seconds, mpix_per_s}`). The compare main shells
out via the existing `OPENTILE_ORACLE_PYTHON` interpreter convention
(reused from `tests/oracle/`).

### 5.4. compare main (`cmd/bench/compare`)

- Walks the overlap fixtures, runs opentile-go in-process, the openslide
  shim in-process (`openslidebench` tag), and the python runner as a
  subprocess.
- Emits a unified Markdown table + a JSON sidecar: per (format, engine,
  pattern) Mpix/s and the opentile-go-relative ratio.
- Skips any engine/format pair that isn't available (missing fixture,
  openslide can't open it, no python interpreter) with a logged note —
  never a hard failure (so a partial environment still produces a
  partial report).

## 6. Regression gating & CI

- **CI gate (pure Go, no cgo/openslide/python):** the internal-axis
  benchmarks run under a Makefile target that parses each
  `BenchmarkRead/<format>/<pattern>` Mpix/s and fails if below a
  committed per-format floor (extends the `bench-ndpi` /
  `MIN_NDPI_MPIXS` pattern; one floor variable per format/pattern of
  interest). Floors set at ~90% of measured baseline, the existing
  convention.
- **Competitive report (on-demand, not CI):** `make bench-compare`
  builds with `-tags openslidebench`, requires openslide + a
  python-opentile venv, and prints the table. Documented as on-demand.
- `benchstat` usage (before/after A/B on a change) documented in
  `cmd/bench/compare/README.md` and `docs/perf.md`, but not wired into
  CI.

## 7. Testing the benchmark suite itself

- The matrix table and Mpix/s computation get a tiny unit test
  (deterministic pixel-count math; table has no duplicate keys; every
  listed fixture path is well-formed).
- The openslide shim gets a smoke test under `-tags openslidebench`:
  open CMU-1, read a region, assert non-empty + correct dimensions.
- A `-short`-mode guard so `go test -short` skips the heavy benchmark
  bodies (CI default test run stays fast; the gate target runs them
  explicitly).
- `go vet` clean; `make test` unaffected (benchmarks don't run under
  plain `go test` without `-bench`).

## 8. Out of scope

- **ScaledStrips perf + memory** — already covered by v0.30's
  `bench-ndpi-mem` and the `ScaledStrips` throughput inheritance.
- **libvips / tifffile / bioformats** references — not selected (openslide
  + python opentile only).
- **Fixing the `tests/oracle/` parity build break** — separate; the
  python *perf* runner is new code and does not depend on the broken
  oracle build, though it mirrors the `OPENTILE_ORACLE_PYTHON` pattern.
- **MRXS / DICOM** — opentile-go has no reader; openslide reads them but
  there's nothing to compare against.

## 9. Success criteria

- `make bench` (or a new `make bench-all`) runs the internal-axis
  benchmarks for all 10 formats × 3 patterns × {1, N}-thread and reports
  Mpix/s + allocs/op, skipping absent fixtures.
- Per-format Mpix/s floors gate CI; a deliberate slowdown trips a floor.
- `make bench-compare` (with `-tags openslidebench` + python venv)
  produces the opentile-go vs openslide vs python-opentile table for the
  overlap formats.
- The openslide shim adds **zero** to the shipping library: `go build
  ./...`, `make test`, and the `nocgo` build are unchanged; the shim
  compiles only under `-tags openslidebench`.
- Profiling: `go test -bench BenchmarkRead/ndpi -cpuprofile` yields a
  usable profile.

## 10. References

- Existing: `cmd/bench/ndpi` (+ `openslide_ref/bench-openslide.c`),
  `cmd/bench/svs`, `cmd/bench/ndpi-strips`, `tests/oracle/`
  (`OPENTILE_ORACLE_PYTHON` subprocess pattern), `Makefile` bench targets.
- openslide C API: `openslide.org/api/openslide_8h.html`.
