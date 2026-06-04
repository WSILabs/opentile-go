# DICOM `ListWSMSeries` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `dicom.ListWSMSeries(path) ([]SeriesInfo, error)` so a caller can enumerate the WSM series under a directory (or a single instance) without opening a slide, to detect multi-series ambiguity.

**Architecture:** Reuse the existing cold-path per-file parser (`idicom.ParseInstance`, which already skips PixelData) and the SeriesUID grouping (extracted from `selectDominantSeries` into a shared `groupBySeriesUID`). The directory scan fast-rejects non-DICOM files via `hasDICMMagic` and parses survivors with a bounded worker pool. `OpenSeries` is untouched.

**Tech Stack:** Go 1.23+, `internal/dicom` (suyashkumar/dicom per-file parser), stdlib `sync`/`runtime`/`sort`.

**Spec:** `docs/superpowers/specs/2026-06-04-dicom-list-wsm-series-design.md` (issue #13).

---

## File Structure

- `formats/dicom/assemble.go` — MODIFY. Extract `groupBySeriesUID`; `selectDominantSeries` calls it.
- `formats/dicom/list_series.go` — CREATE. `SeriesInfo`, `ListWSMSeries`, `scanWSMInstances` (bounded-concurrency directory scan).
- `formats/dicom/list_series_test.go` — CREATE. Unit (synthetic) + fixture tests.
- `docs/formats/dicom.md` — MODIFY. Document file-anchored open as the precise option + `ListWSMSeries`.
- `CHANGELOG.md` — MODIFY. `[Unreleased]` entry.

---

## Task 1: Extract `groupBySeriesUID` (refactor, behavior-preserving)

**Files:**
- Modify: `formats/dicom/assemble.go`
- Test: `formats/dicom/assemble_test.go` (existing tests must stay green)

- [ ] **Step 1: Add the helper + refactor `selectDominantSeries`.** Replace the inline grouping in `selectDominantSeries` with a shared helper:

```go
// seriesGroup is one SeriesUID and its instances, in first-seen order.
type seriesGroup struct {
	uid   string
	insts []idicom.Instance
}

// groupBySeriesUID groups instances by SeriesUID, preserving first-seen
// order of the groups.
func groupBySeriesUID(parsed []idicom.Instance) []seriesGroup {
	order := []string{}
	byUID := map[string]*seriesGroup{}
	for _, in := range parsed {
		g, ok := byUID[in.SeriesUID]
		if !ok {
			order = append(order, in.SeriesUID)
			g = &seriesGroup{uid: in.SeriesUID}
			byUID[in.SeriesUID] = g
		}
		g.insts = append(g.insts, in)
	}
	out := make([]seriesGroup, 0, len(order))
	for _, uid := range order {
		out = append(out, *byUID[uid])
	}
	return out
}
```

Rewrite `selectDominantSeries` to use it (preserving its exact semantics: most VOLUME instances, ties by lexicographically-first SeriesUID, single-series fast path returns input unchanged):

```go
func selectDominantSeries(parsed []idicom.Instance) []idicom.Instance {
	groups := groupBySeriesUID(parsed)
	if len(groups) == 1 {
		return parsed // fast path: single series
	}
	// VOLUME count per group.
	vols := map[string]int{}
	byUID := map[string][]idicom.Instance{}
	uids := make([]string, 0, len(groups))
	for _, g := range groups {
		uids = append(uids, g.uid)
		byUID[g.uid] = g.insts
		for _, in := range g.insts {
			if roleOfInstance(in) == "VOLUME" {
				vols[g.uid]++
			}
		}
	}
	sort.Strings(uids)
	best := uids[0]
	for _, uid := range uids[1:] {
		if vols[uid] > vols[best] {
			best = uid
		}
	}
	return byUID[best]
}
```

- [ ] **Step 2: Run existing tests; verify still green** (this is a behavior-preserving refactor).

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -race -v`
Expected: PASS (all existing assemble/open/parity tests unchanged).

- [ ] **Step 3: Commit.**

```bash
git add formats/dicom/assemble.go
git commit -m "refactor(dicom): extract groupBySeriesUID from selectDominantSeries"
```

---

## Task 2: `SeriesInfo` + `ListWSMSeries` directory path

**Files:**
- Create: `formats/dicom/list_series.go`
- Create: `formats/dicom/list_series_test.go`

- [ ] **Step 1: Write the failing unit test** (synthetic instances — no fixtures, deterministic). Add to `list_series_test.go`:

```go
package dicom

import (
	"sort"
	"testing"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestSeriesInfoFromInstances(t *testing.T) {
	insts := []idicom.Instance{
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 1000, TotalRows: 800, Manufacturer: "Acme", Model: "X1", ObjectivePower: 40},
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 500, TotalRows: 400},
		{SeriesUID: "B", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"LABEL"}, TotalCols: 100, TotalRows: 80},
		{SeriesUID: "A", SOPClassUID: idicom.WSMStorageUID, ImageType: []string{"VOLUME"}, TotalCols: 600, TotalRows: 600, Manufacturer: "Beta", Model: "Y2", ObjectivePower: 20},
	}
	got := seriesInfosFromInstances(insts)
	sort.Slice(got, func(i, j int) bool { return got[i].SeriesUID < got[j].SeriesUID })
	if len(got) != 2 {
		t.Fatalf("got %d series, want 2", len(got))
	}
	// Series A
	if got[0].SeriesUID != "A" || got[0].LevelCount != 1 || got[0].InstanceCount != 1 ||
		got[0].Manufacturer != "Beta" || got[0].Magnification != 20 {
		t.Errorf("series A = %+v", got[0])
	}
	// Series B: 2 VOLUME + 1 LABEL = 3 instances, 2 levels, metadata from a VOLUME.
	if got[1].SeriesUID != "B" || got[1].LevelCount != 2 || got[1].InstanceCount != 3 ||
		got[1].Manufacturer != "Acme" || got[1].Model != "X1" || got[1].Magnification != 40 {
		t.Errorf("series B = %+v", got[1])
	}
}
```

- [ ] **Step 2: Run, verify it fails.**

Run: `go test ./formats/dicom/ -run TestSeriesInfoFromInstances -v`
Expected: FAIL — `SeriesInfo` / `seriesInfosFromInstances` undefined.

- [ ] **Step 3: Implement `SeriesInfo` + aggregation + directory scan** in `list_series.go`:

```go
package dicom

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// SeriesInfo summarizes one WSM series discovered under a path.
type SeriesInfo struct {
	SeriesUID     string
	LevelCount    int     // number of VOLUME instances (≈ pyramid levels)
	InstanceCount int     // number of WSM instances in the series (all roles)
	Manufacturer  string  // from a VOLUME instance
	Model         string
	Magnification float64 // ObjectivePower
}

// seriesInfosFromInstances groups WSM instances by SeriesUID and summarizes
// each. Display metadata is taken from the series' first VOLUME instance.
func seriesInfosFromInstances(insts []idicom.Instance) []SeriesInfo {
	groups := groupBySeriesUID(insts)
	out := make([]SeriesInfo, 0, len(groups))
	for _, g := range groups {
		si := SeriesInfo{SeriesUID: g.uid, InstanceCount: len(g.insts)}
		for _, in := range g.insts {
			if roleOfInstance(in) == "VOLUME" {
				si.LevelCount++
				if si.Manufacturer == "" && si.Model == "" && si.Magnification == 0 {
					si.Manufacturer = in.Manufacturer
					si.Model = in.Model
					si.Magnification = in.ObjectivePower
				}
			}
		}
		out = append(out, si)
	}
	return out
}

// scanWSMInstances parses all valid WSM instances in dir, fast-rejecting
// non-DICOM files via the DICM magic pre-filter and parsing survivors with a
// bounded worker pool. Metadata-only (ParseInstance skips PixelData).
func scanWSMInstances(dir string) []idicom.Instance {
	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var candidates []string
	for _, p := range entries {
		if hasDICMMagic(p) {
			candidates = append(candidates, p)
		}
	}
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan string)
	var mu sync.Mutex
	var out []idicom.Instance
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				in, err := idicom.ParseInstance(p)
				if err != nil || in.SOPClassUID != idicom.WSMStorageUID ||
					in.TotalCols <= 0 || in.TotalRows <= 0 {
					continue
				}
				mu.Lock()
				out = append(out, in)
				mu.Unlock()
			}
		}()
	}
	for _, p := range candidates {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	return out
}

// ListWSMSeries enumerates the WSM series under a path without opening a
// slide. For a directory it returns one SeriesInfo per distinct WSM
// SeriesUID, sorted by SeriesUID. For a single .dcm it returns exactly one
// entry anchored to that file's SeriesUID. A valid directory with no WSM
// instances returns an empty slice and nil error.
func ListWSMSeries(path string) ([]SeriesInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("dicom: stat %s: %w", path, err)
	}
	dir := path
	var anchor string
	if !fi.IsDir() {
		dir = filepath.Dir(path)
		in, err := idicom.ParseInstance(path)
		if err != nil {
			return nil, fmt.Errorf("dicom: parse %s: %w", path, err)
		}
		if in.SOPClassUID != idicom.WSMStorageUID {
			return nil, fmt.Errorf("dicom: %s is not a WSM instance (SOP class %s)", path, in.SOPClassUID)
		}
		anchor = in.SeriesUID
	}

	insts := scanWSMInstances(dir)
	if anchor != "" {
		kept := insts[:0:0]
		for _, in := range insts {
			if in.SeriesUID == anchor {
				kept = append(kept, in)
			}
		}
		insts = kept
	}

	out := seriesInfosFromInstances(insts)
	sort.Slice(out, func(i, j int) bool { return out[i].SeriesUID < out[j].SeriesUID })
	return out, nil
}
```

- [ ] **Step 4: Run the unit test; verify pass.**

Run: `go test ./formats/dicom/ -run TestSeriesInfoFromInstances -race -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add formats/dicom/list_series.go formats/dicom/list_series_test.go
git commit -m "feat(dicom): ListWSMSeries enumeration + SeriesInfo (#13)"
```

---

## Task 3: Fixture integration tests (single-series, multi-series, single-file, no-WSM)

**Files:**
- Modify: `formats/dicom/list_series_test.go`

- [ ] **Step 1: Write the integration tests.** Use the existing `leica4(t)` fixture helper (in `open_test.go`, same `_test` package — confirm its exact name/signature when implementing; it returns the Leica-4 dir path and skips when absent).

```go
import (
	"os"
	"path/filepath"
	// ... existing imports
)

func TestListWSMSeriesSingleSeries(t *testing.T) {
	dir := leica4(t) // skips if fixture absent
	got, err := ListWSMSeries(dir)
	if err != nil {
		t.Fatalf("ListWSMSeries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d series, want 1: %+v", len(got), got)
	}
	if got[0].SeriesUID == "" || got[0].LevelCount < 1 {
		t.Errorf("series = %+v", got[0])
	}
}

func TestListWSMSeriesMultiSeries(t *testing.T) {
	a := leica4(t)
	b := histech1(t) // 3DHISTECH-1 fixture helper (confirm name in open_test.go)
	tmp := t.TempDir()
	n := 0
	for _, srcDir := range []string{a, b} {
		dcms, _ := filepath.Glob(filepath.Join(srcDir, "*.dcm"))
		for _, p := range dcms {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, "inst", "_"), nil, 0); false {
				_ = err // placeholder removed below
			}
			dst := filepath.Join(tmp, fmtIndex(n)+".dcm")
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				t.Fatal(err)
			}
			n++
		}
	}
	got, err := ListWSMSeries(tmp)
	if err != nil {
		t.Fatalf("ListWSMSeries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d series, want 2: %+v", len(got), got)
	}
	if got[0].SeriesUID >= got[1].SeriesUID {
		t.Errorf("not sorted by SeriesUID: %+v", got)
	}
}

func fmtIndex(n int) string { return fmt.Sprintf("%04d", n) }

func TestListWSMSeriesSingleFile(t *testing.T) {
	dir := leica4(t)
	dcms, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if len(dcms) == 0 {
		t.Skip("no .dcm in fixture")
	}
	got, err := ListWSMSeries(dcms[0])
	if err != nil {
		t.Fatalf("ListWSMSeries(file): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("single-file got %d series, want 1", len(got))
	}
}

func TestListWSMSeriesNoWSM(t *testing.T) {
	got, err := ListWSMSeries(t.TempDir()) // empty dir
	if err != nil {
		t.Fatalf("empty dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir got %d series, want 0", len(got))
	}
}
```

(When implementing: remove the dead `os.WriteFile(..., nil, 0)` placeholder line above — it was a scratch artifact; the real copy is the `dst` write. Confirm `leica4`/`histech1` helper names against `open_test.go`; if a 3DHISTECH helper doesn't exist, add one mirroring `leica4`. Add `"fmt"` to imports.)

- [ ] **Step 2: Run the integration tests.**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -run TestListWSMSeries -race -v`
Expected: PASS (or SKIP where fixtures absent). Multi-series returns 2 sorted entries.

- [ ] **Step 3: Commit.**

```bash
git add formats/dicom/list_series_test.go
git commit -m "test(dicom): ListWSMSeries fixture integration (single/multi/file/empty)"
```

---

## Task 4: Docs + CHANGELOG + full gate

**Files:**
- Modify: `docs/formats/dicom.md`, `CHANGELOG.md`

- [ ] **Step 1: Document the API + the precise-open guidance** in `docs/formats/dicom.md`. Add a short section under the API surface: `ListWSMSeries(path) ([]SeriesInfo, error)` enumerates WSM series for ambiguity detection; note that opening a **single `.dcm`** is the precise, unambiguous entry (anchored to its SeriesUID), while opening a **directory** uses dominant-pick — a CLI can call `ListWSMSeries` to detect `len() > 1` and refuse.

- [ ] **Step 2: CHANGELOG `[Unreleased]` entry:** "decoder/dicom: `ListWSMSeries(path) ([]SeriesInfo, error)` — enumerate WSM series under a path without opening a slide, so callers can detect multi-series directories (#13). `OpenSeries` dominant-pick unchanged."

- [ ] **Step 3: Full gate.**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
go vet ./...
CGO_ENABLED=0 go build ./...
```
Expected: green.

- [ ] **Step 4: Commit + close issue.**

```bash
git add docs/formats/dicom.md CHANGELOG.md
git commit -m "docs(dicom): ListWSMSeries + precise file-open guidance (closes #13)"
```

Update issue #13 as resolved on merge.

---

## Self-Review

- **Spec coverage:** API (§2) → Task 2 (`SeriesInfo`, `ListWSMSeries`). Behavior (§3): directory → Task 2; single-file anchored → Task 2 + Task 3 test; no-WSM empty → Task 2 + Task 3 test; aggregation → Task 2 unit test. Enormous-dir robustness (§4): DICM pre-filter + metadata-only parse + bounded concurrency + no cap → Task 2 `scanWSMInstances`. DRY (§5): `groupBySeriesUID` → Task 1. Testing (§6) → Tasks 2-3. DICOMDIR deferral (§4/§7) → not implemented, documented.
- **Placeholder scan:** the Task 3 multi-series test carries one flagged dead line (`os.WriteFile(..., nil, 0)`) explicitly called out for removal during implementation — not a silent placeholder; the working copy logic (`dst` write) is complete. Helper names `leica4`/`histech1` are flagged "confirm against open_test.go."
- **Type consistency:** `SeriesInfo` fields (`SeriesUID`, `LevelCount`, `InstanceCount`, `Manufacturer`, `Model`, `Magnification`), `seriesInfosFromInstances`, `scanWSMInstances`, `groupBySeriesUID`/`seriesGroup` consistent across Tasks 1-3. Hygiene filter `WSMStorageUID && TotalCols>0 && TotalRows>0` matches `assembleSeries`.
