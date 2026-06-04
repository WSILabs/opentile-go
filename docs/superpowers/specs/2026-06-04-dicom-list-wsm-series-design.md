# DICOM `ListWSMSeries` — design

- **Date:** 2026-06-04
- **Status:** approved (brainstorm complete)
- **Issue:** #13 (sibling of #10/#11/#12). Target release: v0.33.
- **Consumer:** wsitools CLI — multi-series ambiguity error (its plan Task B7), blocked on this.

## 1. Problem

A **directory** input can resolve to more than one WSM `SeriesInstanceUID`.
Today `OpenSeries` silently picks the dominant series (most VOLUME instances,
ties by sorted UID) with no signal that others were present. A CLI needs to
**detect** the multi-series case and refuse with an actionable error, while the
library default stays permissive.

## 2. API (additive, `formats/dicom`)

```go
// SeriesInfo summarizes one WSM series discovered under a path.
type SeriesInfo struct {
    SeriesUID     string
    LevelCount    int     // number of VOLUME instances (≈ pyramid levels)
    InstanceCount int     // number of WSM instances in the series (all roles)
    Manufacturer  string  // from a VOLUME instance
    Model         string
    Magnification float64 // ObjectivePower
}

// ListWSMSeries enumerates the WSM series under a path WITHOUT fully opening a
// slide. For a directory it returns one SeriesInfo per distinct WSM SeriesUID
// (sorted by SeriesUID). For a single .dcm it returns exactly one entry,
// anchored to that file's SeriesUID. A valid directory with no WSM instances
// returns an empty slice and nil error; a stat error returns an error.
func ListWSMSeries(path string) ([]SeriesInfo, error)
```

`OpenSeries` is **unchanged** — its deterministic dominant-pick remains the
permissive library default. `ListWSMSeries` is purely additive.

## 3. Behavior

- **Directory:** `filepath.Glob("*.dcm")` → `hasDICMMagic` cheap pre-filter
  (132 bytes) → parse the survivors → filter to WSM
  (`SOPClassUID == WSMStorageUID`, plus the existing series hygiene: skip
  zero/missing total-pixel-matrix instances) → group by `SeriesUID` →
  aggregate one `SeriesInfo` per group → sort by `SeriesUID`.
- **Single `.dcm`:** parse it; if WSM, return one `SeriesInfo` anchored to its
  `SeriesUID`, counting only same-directory same-`SeriesUID` siblings (mirrors
  `OpenSeries`' anchored contract). A non-WSM single file → error.
- **Aggregation:** `InstanceCount` = group size; `LevelCount` = count of VOLUME
  instances; `Manufacturer` / `Model` / `Magnification` taken from a
  representative VOLUME instance (consistent within a series).

## 4. Enormous-directory robustness (sealed decision)

Grouping is inherently O(files) — a file's `SeriesUID` is only knowable by
reading its header — and it is the **same cost `OpenSeries` already pays**.
Mitigations, all in scope:
- **DICM-magic pre-filter** fast-rejects non-DICOM `.dcm`-named files.
- **Metadata-only parse** (the cold path already stops before `PixelData`).
- **Bounded-concurrency** parse pass (`min(NumCPU, 8)` workers) for wall-clock.
- **No silent truncation cap** — a cap could miss a series and defeat ambiguity
  detection; correctness over a partial fast answer.

**Deferred (logged follow-up):** a `DICOMDIR` fast-path — when a `DICOMDIR`
index is present, enumerate series from it in O(1) file reads instead of
scanning every instance. Out of scope for this iteration.

## 5. Internals / DRY

Extract the group-by-`SeriesUID` logic currently inline in
`assemble.go selectDominantSeries` into a shared helper (e.g.
`groupBySeriesUID([]idicom.Instance)`), used by both `selectDominantSeries`
and `ListWSMSeries`. Targeted cleanup of code being touched; no behavior
change to `selectDominantSeries`.

## 6. Testing (TDD)

- **Single-series fixture:** `ListWSMSeries(Leica-4)` → 1 entry; assert
  `SeriesUID`, `LevelCount > 0`, `Manufacturer`/`Model` populated.
- **Multi-series:** a `t.TempDir()` seeded with `.dcm` copied from two
  different-`SeriesUID` fixtures → 2 entries, sorted.
- **Single file:** `ListWSMSeries(oneInstance.dcm)` → exactly 1 anchored entry.
- **No-WSM dir:** empty dir (or non-WSM `.dcm`) → empty slice, nil error.
- **Aggregation unit test:** synthetic `idicom.Instance` literals (matching
  `assemble_test.go`) → correct `LevelCount` / `InstanceCount` grouping.
- `make test` green under `-race`.

## 7. Out of scope

`OpenSeries` behavior changes; a typed `AmbiguousSeriesError`; the `DICOMDIR`
fast-path; multi-directory recursion (same-directory contract preserved).
