# Validate() API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a structural WSI validator (`opentile.Validate`/`ValidateFile`/`(*Slide).Validate`) that reports the general nature of structural problems — tiers 0 (openability) and 1 (structural, no pixel decode) — as a findings-as-data `Report`.

**Architecture:** A two-layer engine in the root `opentile` package: a format-agnostic layer computes checks from the public `Level`/`Pyramid` geometry, and a per-reader `Validator` hook (discovered through the `UnwrapReader` chain, exactly like the existing `TIFFDirectories()` provider) contributes checks needing reader internals. TIFF readers delegate byte-range and orphan-IFD checks to a shared `internal/tiffvalidate` helper; non-TIFF readers (IFE/SZI/DICOM) yield their own byte ranges. A central `ValidationProbe` collects per-occurrence flags and rolls them up by `(Code, Level)` into count-only findings.

**Tech Stack:** Go 1.23, `internal/tiff` (TIFF/BigTIFF parser), `internal/format` (registry), existing `UnwrapReader` provider pattern.

**Spec:** `docs/superpowers/specs/2026-06-16-validate-api-design.md`

**Conventions reminder:** `make test` runs `go test ./... -race -count=1`. The project follows TDD strictly (write failing test, watch it fail, implement minimally). Spelling is `stripped` (never `striped`). The `nocgo` CI step builds with `-tags nocgo`; Tier 0/1 validation is decode-free and must build/pass under it.

---

## File Structure

- `validate.go` (root, new) — public types (`Severity`, `CheckCode`, `Finding`, `Report`, `ValidateOption`, `ValidationProbe`, `Validator`), entry points, `validatorOf` discovery, format-agnostic engine, central rollup.
- `format.go` (root, modify) — add `FormatUnknown Format = ""`.
- `slide.go` (root, modify) — add `size int64` field to `Slide`; `open.go` populates it.
- `internal/tiffvalidate/tiffvalidate.go` (new) — shared TIFF byte-range + orphan-IFD walker.
- `formats/<name>/validate.go` (new, per reader) — the reader's `Validate(p *opentile.ValidationProbe)` hook.
- `validate_test.go` (root, new) — hermetic per-check tests, contract tests.
- `validate_corpus_test.go` (root, new) — corpus-gated no-false-positive gate.
- `formats/<name>/validate_test.go` (per reader, where reader-specific) — hook tests.

---

## Task 1: Public result types

**Files:**
- Create: `validate.go`
- Modify: `format.go` (add `FormatUnknown`)
- Test: `validate_test.go`

- [ ] **Step 1: Write the failing test**

Create `validate_test.go`:

```go
package opentile

import "testing"

func TestReportOK(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Severity: Warning, Code: CheckMissingMetadata},
		{Severity: Info, Code: CheckOrphanIFD},
	}}
	if !r.OK() {
		t.Fatal("OK() should be true with no Error findings")
	}
	r.Findings = append(r.Findings, Finding{Severity: Error, Code: CheckOffsetsOutOfBounds})
	if r.OK() {
		t.Fatal("OK() should be false once an Error finding is present")
	}
}

func TestReportWorst(t *testing.T) {
	if got := (&Report{}).Worst(); got != Info {
		t.Fatalf("empty report Worst() = %v, want Info", got)
	}
	r := &Report{Findings: []Finding{{Severity: Info}, {Severity: Warning}}}
	if got := r.Worst(); got != Warning {
		t.Fatalf("Worst() = %v, want Warning", got)
	}
}

func TestFormatUnknownIsZeroValue(t *testing.T) {
	if FormatUnknown != "" {
		t.Fatalf("FormatUnknown = %q, want empty string (the zero value)", FormatUnknown)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestReportOK|TestReportWorst|TestFormatUnknownIsZeroValue'`
Expected: FAIL — undefined: `Report`, `Finding`, `Severity`, `CheckCode`, constants, `FormatUnknown`.

- [ ] **Step 3: Add the `FormatUnknown` constant**

In `format.go`, inside the existing `const ( ... )` block (after `FormatDICOM`), add:

```go
	// FormatUnknown is the zero value of Format, used when a file did not
	// open as any known format (e.g. Report.Format on an Unopenable file).
	FormatUnknown Format = ""
```

- [ ] **Step 4: Write the types**

Create `validate.go`:

```go
package opentile

// Severity ranks a validation Finding.
type Severity int

const (
	// Info is a legal-but-unusual observation (e.g. an orphan IFD).
	Info Severity = iota
	// Warning is unusual and possibly wrong, but not provably broken.
	Warning
	// Error is a structural defect: the file is not well-formed per
	// opentile-go's reader.
	Error
)

// String renders a Severity for human output.
func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// CheckCode is a stable, human-meaningful category of validation problem —
// the "general nature" of a Finding and the primary thing callers branch on.
// String-underlying so the catalog is open-ended (new codes are additive).
type CheckCode string

const (
	CheckUnopenable          CheckCode = "unopenable"
	CheckOffsetsOutOfBounds  CheckCode = "offsets-out-of-bounds"
	CheckTileGridMismatch    CheckCode = "tile-grid-mismatch"
	CheckNonConformantFormat CheckCode = "non-conformant-format"
	CheckInconsistentPyramid CheckCode = "inconsistent-pyramid"
	CheckMissingMetadata     CheckCode = "missing-metadata"
	CheckOrphanIFD           CheckCode = "orphan-ifd"
)

// Finding is one rolled-up validation problem. Many occurrences of the same
// (Code, locus) are aggregated into a single Finding with a Count — the report
// conveys the general nature and scale, not a per-occurrence repair list.
type Finding struct {
	Severity Severity
	Code     CheckCode
	Message  string // human context, e.g. "200 tiles reference offsets past EOF"
	Pyramid  int    // coarse locus; -1 when whole-file / not applicable
	Level    int    // coarse locus; -1 when not applicable
	Count    int    // occurrences rolled up under this (Code, locus); >= 1
}

// Report is the result of validating one file.
type Report struct {
	Format   Format // detected format; FormatUnknown when Unopenable
	Findings []Finding
}

// OK reports whether the file is well-formed per opentile-go's reader:
// true iff there are no Error-severity findings. Warning/Info do not affect OK.
func (r *Report) OK() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return false
		}
	}
	return true
}

// Worst returns the highest severity present, or Info for an empty report.
func (r *Report) Worst() Severity {
	worst := Info
	for _, f := range r.Findings {
		if f.Severity > worst {
			worst = f.Severity
		}
	}
	return worst
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test . -run 'TestReportOK|TestReportWorst|TestFormatUnknownIsZeroValue'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add validate.go format.go validate_test.go
git commit -m "feat(validate): public result types (Report/Finding/Severity/CheckCode) + FormatUnknown"
```

---

## Task 2: ValidationProbe + central count-only rollup

**Files:**
- Modify: `validate.go`
- Test: `validate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `validate_test.go`:

```go
func TestProbeRollupCountOnly(t *testing.T) {
	p := newProbe(1000)
	// Three occurrences of the same (Code, level) → one finding, Count 3.
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (1,1) offset past EOF")
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (2,1) offset past EOF")
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (3,1) offset past EOF")
	// A different level → separate finding.
	p.Flag(CheckOffsetsOutOfBounds, 0, 3, "tile (0,0) offset past EOF")
	// A whole-file finding (locus -1).
	p.Flag(CheckOrphanIFD, -1, -1, "1 unreferenced IFD")

	got := p.findings()
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3 (rolled up by (Code,level))", len(got))
	}

	var oob2 *Finding
	for i := range got {
		if got[i].Code == CheckOffsetsOutOfBounds && got[i].Level == 2 {
			oob2 = &got[i]
		}
	}
	if oob2 == nil {
		t.Fatal("missing rolled-up OffsetsOutOfBounds finding for level 2")
	}
	if oob2.Count != 3 {
		t.Fatalf("level-2 finding Count = %d, want 3", oob2.Count)
	}
	if oob2.Severity != Error {
		t.Fatalf("OffsetsOutOfBounds severity = %v, want Error", oob2.Severity)
	}
}

func TestProbeSize(t *testing.T) {
	if got := newProbe(4242).Size(); got != 4242 {
		t.Fatalf("Size() = %d, want 4242", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestProbeRollupCountOnly|TestProbeSize'`
Expected: FAIL — undefined: `newProbe`, `Flag`, `findings`, `Size`.

- [ ] **Step 3: Implement the probe + rollup**

Append to `validate.go`:

```go
import "sort"

// codeSeverity is the canonical severity for each CheckCode. Defining it once
// keeps severity assignment in a single place; readers/engine never pass it.
var codeSeverity = map[CheckCode]Severity{
	CheckUnopenable:          Error,
	CheckOffsetsOutOfBounds:  Error,
	CheckTileGridMismatch:    Error,
	CheckNonConformantFormat: Error,
	CheckInconsistentPyramid: Warning,
	CheckMissingMetadata:     Warning,
	CheckOrphanIFD:           Info,
}

// ValidationProbe is the collector handed to a reader's Validate hook. Readers
// call Flag once per problem occurrence; the engine rolls occurrences up by
// (Code, Pyramid, Level) into count-only Findings. Readers never count and
// never assign severity.
type ValidationProbe struct {
	size int64
	agg  map[probeKey]*Finding
	keys []probeKey // insertion order, for deterministic output
}

type probeKey struct {
	code             CheckCode
	pyramid, level   int
}

func newProbe(size int64) *ValidationProbe {
	return &ValidationProbe{size: size, agg: map[probeKey]*Finding{}}
}

// Size is the backing file size in bytes (for byte-range bounds checks).
func (p *ValidationProbe) Size() int64 { return p.size }

// Flag records one occurrence of a problem at the given locus. Use pyramid=-1
// and/or level=-1 when the locus is the whole file or not applicable. The first
// occurrence's msg is kept as the Finding's Message; later occurrences only
// bump the Count.
func (p *ValidationProbe) Flag(code CheckCode, pyramid, level int, msg string) {
	k := probeKey{code, pyramid, level}
	if f, ok := p.agg[k]; ok {
		f.Count++
		return
	}
	p.agg[k] = &Finding{
		Severity: codeSeverity[code],
		Code:     code,
		Message:  msg,
		Pyramid:  pyramid,
		Level:    level,
		Count:    1,
	}
	p.keys = append(p.keys, k)
}

// findings returns the rolled-up findings in a deterministic order
// (insertion order, then a stable sort by severity desc, code, level).
func (p *ValidationProbe) findings() []Finding {
	out := make([]Finding, 0, len(p.keys))
	for _, k := range p.keys {
		out = append(out, *p.agg[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity // Error first
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Level < out[j].Level
	})
	return out
}
```

Note: move the `import "sort"` into the existing import area — if `validate.go` has no import block yet, add one at the top below `package opentile`:

```go
import "sort"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestProbeRollupCountOnly|TestProbeSize'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add validate.go validate_test.go
git commit -m "feat(validate): ValidationProbe collector with count-only (Code,level) rollup"
```

---

## Task 3: Validator interface, discovery, and Slide.size

**Files:**
- Modify: `validate.go` (interface + `validatorOf`)
- Modify: `slide.go` (add `size int64` field)
- Modify: `open.go` (populate `size`)
- Test: `validate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `validate_test.go`:

```go
// fakeValidatorReader implements just enough to test validatorOf discovery
// through the UnwrapReader chain.
type fakeValidatorReader struct {
	flagged bool
}

func (f *fakeValidatorReader) Validate(p *ValidationProbe) {
	f.flagged = true
	p.Flag(CheckNonConformantFormat, -1, -1, "fake reader says non-conformant")
}

// wrapper mimics fileCloser/mmapCloser: it delegates discovery via UnwrapReader.
type validatorWrapper struct{ inner any }

func (w validatorWrapper) UnwrapReader() any { return w.inner }

func TestValidatorOfWalksUnwrapChain(t *testing.T) {
	fv := &fakeValidatorReader{}
	s := &Slide{r: nil}
	// Place the validator two unwrap hops deep behind wrappers.
	got, ok := validatorOfAny(validatorWrapper{inner: validatorWrapper{inner: fv}})
	if !ok || got == nil {
		t.Fatal("validatorOfAny should find the validator through the unwrap chain")
	}
	_ = s
	got.Validate(newProbe(0))
	if !fv.flagged {
		t.Fatal("discovered validator's Validate was not called")
	}
}

func TestValidatorOfMissing(t *testing.T) {
	if _, ok := validatorOfAny(struct{}{}); ok {
		t.Fatal("a non-validator with no UnwrapReader should yield ok=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestValidatorOf'`
Expected: FAIL — undefined: `Validator`, `validatorOfAny`.

- [ ] **Step 3: Add the interface + discovery**

Append to `validate.go`:

```go
// Validator is implemented by format readers that contribute Tier-1 structural
// findings. The method is exported because readers live in other packages. A
// reader that does not implement it still gets the format-agnostic engine
// checks. Readers call ValidationProbe.Flag once per problem occurrence.
type Validator interface {
	Validate(p *ValidationProbe)
}

// validatorOfAny walks the UnwrapReader chain (like tiffProviderOf) looking for
// a reader that implements Validator.
func validatorOfAny(start any) (Validator, bool) {
	cur := start
	for cur != nil {
		if v, ok := cur.(Validator); ok {
			return v, true
		}
		u, ok := cur.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		cur = u.UnwrapReader()
	}
	return nil, false
}

// validatorOf finds the Validator behind a Slide's reader, if any.
func validatorOf(s *Slide) (Validator, bool) { return validatorOfAny(s.r) }
```

- [ ] **Step 4: Add the `size` field to `Slide`**

In `slide.go`, inside `type Slide struct { ... }`, add a field (place it right after `r slideReader`):

```go
	// size is the backing file size in bytes, captured at Open. Read by
	// (*Slide).Validate for byte-range bounds checks.
	size int64
```

- [ ] **Step 5: Populate `size` at Open**

In `open.go`, find the two places that build the `*Slide` after `dispatchOpen` (in `Open` around line 85-95 and in the `OpenFile`/mmap paths around 182 and 201). Each constructs a `&Slide{r: rdr, ...}`. Add `size:` to each. For `Open(r, size, ...)`:

```go
	s := &Slide{r: rdr, size: size}
	// ... existing field assignments (readBudget, etc.) stay
```

For the `OpenFile` path that uses `info.Size()`:

```go
	s := &Slide{r: rdr, size: info.Size()}
```

For the mmap path that uses `m.Size()`:

```go
	s := &Slide{r: rdr, size: m.Size()}
```

If the existing code sets fields after construction (e.g. `s.readBudget = ...`), instead add `s.size = size` (or `info.Size()` / `m.Size()`) right next to those assignments. Read the actual construction sites and match their style — the requirement is only that `s.size` holds the backing byte size on every open path.

- [ ] **Step 6: Run test + full build to verify**

Run: `go test . -run 'TestValidatorOf' && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 7: Commit**

```bash
git add validate.go slide.go open.go validate_test.go
git commit -m "feat(validate): Validator hook interface, UnwrapReader discovery, Slide.size"
```

---

## Task 4: Format-agnostic engine + entry points (Tier 0 + grid/pyramid/MPP)

**Files:**
- Modify: `validate.go`
- Test: `validate_test.go`

This task wires the three entry points and the format-agnostic checks computable from public `Level` geometry: `CheckTileGridMismatch` (zero dims / Grid≠ceil(Size/TileSize)), `CheckInconsistentPyramid` (non-monotone level sizes), and `CheckMissingMetadata` (level MPP zero). The per-reader hook is invoked if present. Tier 0 (`CheckUnopenable`) is produced by the from-source entry points when `Open` fails.

- [ ] **Step 1: Write the failing test**

Append to `validate_test.go`:

```go
import "errors"

func TestValidateFileUnopenable(t *testing.T) {
	// A path that does not exist → operational error, not a finding.
	if _, err := ValidateFile("/nonexistent/path/zzz.svs"); err == nil {
		t.Fatal("ValidateFile on a missing path should return an operational error")
	}
}

func TestValidateUnopenableBytes(t *testing.T) {
	// Garbage bytes: opens nothing → one CheckUnopenable Error, Format unknown.
	garbage := bytes.Repeat([]byte{0xAB}, 512)
	rep, err := Validate(bytes.NewReader(garbage), int64(len(garbage)))
	if err != nil {
		t.Fatalf("Validate returned operational error %v; want a report with Unopenable", err)
	}
	if rep.Format != FormatUnknown {
		t.Fatalf("Format = %q, want FormatUnknown", rep.Format)
	}
	if rep.OK() {
		t.Fatal("garbage should not be OK")
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Code != CheckUnopenable {
		t.Fatalf("findings = %+v, want exactly one CheckUnopenable", rep.Findings)
	}
}

// gridSize returns a Size; helper for synthetic Level construction.
func TestEngineGridMismatchFromLevels(t *testing.T) {
	// Level with Grid disagreeing with ceil(Size/TileSize).
	lvls := []Level{{
		Index: 0, PyramidIndex: 0,
		Size:     Size{Width: 1024, Height: 1024},
		TileSize: Size{Width: 256, Height: 256},
		Grid:     Size{Width: 2, Height: 2}, // wrong: should be 4x4
	}}
	var p = newProbe(0)
	checkLevelGeometry(p, 0, lvls)
	got := p.findings()
	if len(got) != 1 || got[0].Code != CheckTileGridMismatch {
		t.Fatalf("findings = %+v, want one CheckTileGridMismatch", got)
	}
}

func TestEngineInconsistentPyramid(t *testing.T) {
	// Level 1 is LARGER than level 0 → non-monotone pyramid.
	lvls := []Level{
		{Index: 0, Size: Size{Width: 512, Height: 512}, TileSize: Size{Width: 256, Height: 256}, Grid: Size{Width: 2, Height: 2}},
		{Index: 1, Size: Size{Width: 1024, Height: 1024}, TileSize: Size{Width: 256, Height: 256}, Grid: Size{Width: 4, Height: 4}},
	}
	p := newProbe(0)
	checkLevelGeometry(p, 0, lvls)
	found := false
	for _, f := range p.findings() {
		if f.Code == CheckInconsistentPyramid {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CheckInconsistentPyramid, got %+v", p.findings())
	}
}
```

Add `bytes` to the test file's imports (and `errors` if not already present — remove the unused `errors` import if the compiler complains; the `TestValidateFileUnopenable` test above does not actually need `errors`, so do not import it unless used).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestValidate|TestEngine'`
Expected: FAIL — undefined: `ValidateFile`, `Validate`, `checkLevelGeometry`.

- [ ] **Step 3: Implement the engine + entry points**

Append to `validate.go`:

```go
import (
	"fmt"
	"io"
	"os"
)

// ValidateOption is the additive seam for future decode-based checks (Tier 2).
// v1 ships zero options.
type ValidateOption func(*validateConfig)

type validateConfig struct{}

// ValidateFile opens path and validates it (tiers 0 + 1). The returned error is
// operational only (path missing / unreadable); a file that fails to open or is
// structurally broken yields a *Report whose findings describe the problem.
//
// We stat first so that a genuinely absent/unreadable path is an operational
// error, while a path that exists but fails to open/parse becomes a
// CheckUnopenable finding. OpenFile handles both the single-file and DICOM
// series-directory cases.
func ValidateFile(path string, opts ...ValidateOption) (*Report, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return validateOpened(func() (*Slide, error) { return OpenFile(path) }, opts...)
}

// Validate validates an in-memory / streamed source of the given size
// (tiers 0 + 1). Operational error semantics match ValidateFile.
func Validate(r io.ReaderAt, size int64, opts ...ValidateOption) (*Report, error) {
	if size <= 0 {
		return nil, fmt.Errorf("opentile: Validate: non-positive size %d", size)
	}
	return validateOpened(func() (*Slide, error) { return Open(r, size) }, opts...)
}

// validateOpened runs Tier 0 (open) then Tier 1 (slide.Validate). An open
// failure is reported as a CheckUnopenable finding, not an operational error.
func validateOpened(open func() (*Slide, error), opts ...ValidateOption) (*Report, error) {
	s, err := open()
	if err != nil {
		return &Report{
			Format: FormatUnknown,
			Findings: []Finding{{
				Severity: Error,
				Code:     CheckUnopenable,
				Message:  err.Error(),
				Pyramid:  -1,
				Level:    -1,
				Count:    1,
			}},
		}, nil
	}
	defer s.Close()
	return s.Validate(opts...), nil
}

// Validate runs the Tier-1 structural checks on an already-open Slide. There is
// no Tier 0 (it already opened) and no operation can fail, so there is no error
// return. Reuses the Slide's parsed state.
func (s *Slide) Validate(opts ...ValidateOption) *Report {
	p := newProbe(s.size)
	// Layer 1: format-agnostic geometry checks over every pyramid's levels.
	s.ensurePyramids()
	for pi := range s.pyramids {
		checkLevelGeometry(p, s.pyramids[pi].Index, s.pyramids[pi].Levels)
	}
	// Layer 2: per-reader hook, if the reader implements Validator.
	if v, ok := validatorOf(s); ok {
		v.Validate(p)
	}
	return &Report{Format: s.r.Format(), Findings: p.findings()}
}

// checkLevelGeometry runs the format-agnostic Tier-1 checks for one pyramid's
// levels: grid math, monotone downsampling, and MPP presence.
func checkLevelGeometry(p *ValidationProbe, pyramid int, levels []Level) {
	for _, l := range levels {
		// Zero dimensions / zero tiles.
		if l.Size.Width <= 0 || l.Size.Height <= 0 ||
			l.TileSize.Width <= 0 || l.TileSize.Height <= 0 {
			p.Flag(CheckTileGridMismatch, pyramid, l.Index,
				fmt.Sprintf("level %d has degenerate size %dx%d / tile %dx%d",
					l.Index, l.Size.Width, l.Size.Height, l.TileSize.Width, l.TileSize.Height))
			continue
		}
		// Grid must equal ceil(Size/TileSize) per axis.
		wantW := ceilDiv(l.Size.Width, l.TileSize.Width)
		wantH := ceilDiv(l.Size.Height, l.TileSize.Height)
		if l.Grid.Width != wantW || l.Grid.Height != wantH {
			p.Flag(CheckTileGridMismatch, pyramid, l.Index,
				fmt.Sprintf("level %d grid %dx%d != ceil(size/tile) %dx%d",
					l.Index, l.Grid.Width, l.Grid.Height, wantW, wantH))
		}
		// Missing per-level MPP metadata (Warning).
		if l.MPP.IsZero() {
			p.Flag(CheckMissingMetadata, pyramid, l.Index,
				fmt.Sprintf("level %d has no MPP (microns-per-pixel) metadata", l.Index))
		}
	}
	// Monotone non-increasing level dimensions (Warning). Levels are
	// expected in decreasing-resolution order.
	for i := 1; i < len(levels); i++ {
		if levels[i].Size.Width > levels[i-1].Size.Width ||
			levels[i].Size.Height > levels[i-1].Size.Height {
			p.Flag(CheckInconsistentPyramid, pyramid, levels[i].Index,
				fmt.Sprintf("level %d (%dx%d) is larger than level %d (%dx%d)",
					levels[i].Index, levels[i].Size.Width, levels[i].Size.Height,
					levels[i-1].Index, levels[i-1].Size.Width, levels[i-1].Size.Height))
		}
	}
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
```

Notes for the implementer:
- Merge these `import (...)` additions (`fmt`, `io`, `os`) with the imports already added in earlier tasks (`sort`) into a single import block at the top of `validate.go`.
- Confirm the field names on `Size`: it is `Size{Width, Height int}` and `Pyramid.Levels []Level` and `Pyramid.Index int`. Verify against `image.go` (the test uses `Size{Width:..., Height:...}`). If `Size`'s fields are named differently, match the real names in both the test and the engine.
- `MPP.IsZero()` exists (per CLAUDE.md v1.0 MPP type). Verify the method name in the MPP type definition; if it is a field comparison instead, use the real predicate.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestValidate|TestEngine'`
Expected: PASS.

- [ ] **Step 5: Run the full root package tests under race**

Run: `go test . -race -count=1`
Expected: PASS (no regressions from the `Slide.size` addition).

- [ ] **Step 6: Commit**

```bash
git add validate.go validate_test.go
git commit -m "feat(validate): format-agnostic engine (grid/pyramid/MPP) + entry points + Tier-0"
```

---

## Task 5: Shared TIFF byte-range + orphan-IFD helper

**Files:**
- Create: `internal/tiffvalidate/tiffvalidate.go`
- Test: `internal/tiffvalidate/tiffvalidate_test.go`

This helper does the reader-internal TIFF checks: every tile/strip `offset+length` must lie within the file, and IFDs not reachable as a level/associated page are reported as orphans. It operates on a `*tiff.File` and reports through a small flag callback so it does not import the root `opentile` package (avoiding a cycle: `internal/tiffvalidate` must NOT import `opentile`; the reader adapts the callback to `ValidationProbe.Flag`).

- [ ] **Step 1: Write the failing test**

Create `internal/tiffvalidate/tiffvalidate_test.go`:

```go
package tiffvalidate

import "testing"

// fakeSink records flag calls.
type fakeSink struct {
	oob    int
	orphan int
}

func (s *fakeSink) OffsetOutOfBounds(level int, msg string) { s.oob++ }
func (s *fakeSink) OrphanIFD(msg string)                    { s.orphan++ }

func TestCheckByteRangeInBounds(t *testing.T) {
	// offset+length within size → no flag.
	s := &fakeSink{}
	checkRange(s, 0, 100, 50, 1000) // off=100 len=50 size=1000
	if s.oob != 0 {
		t.Fatalf("in-bounds range flagged %d times, want 0", s.oob)
	}
}

func TestCheckByteRangeOutOfBounds(t *testing.T) {
	s := &fakeSink{}
	checkRange(s, 2, 990, 50, 1000) // 990+50 = 1040 > 1000 → flag
	if s.oob != 1 {
		t.Fatalf("out-of-bounds range flagged %d times, want 1", s.oob)
	}
}

func TestCheckByteRangeOverflow(t *testing.T) {
	s := &fakeSink{}
	const maxU64 = ^uint64(0)
	checkRange(s, 0, maxU64-10, 1000, 1<<40) // offset+length overflows
	if s.oob != 1 {
		t.Fatalf("overflowing range flagged %d times, want 1", s.oob)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tiffvalidate/ -run 'TestCheckByteRange'`
Expected: FAIL — undefined: `checkRange`, package empty.

- [ ] **Step 3: Implement the core range check**

Create `internal/tiffvalidate/tiffvalidate.go`:

```go
// Package tiffvalidate provides structural validation helpers shared by the
// TIFF-based format readers: byte-range bounds for tile/strip data and
// orphan-IFD detection. It must NOT import the root opentile package (the
// readers do, so importing it here would create a cycle); results are reported
// through the Sink interface, which the reader adapts to opentile's probe.
package tiffvalidate

import (
	"fmt"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Sink receives validation findings. The reader implements it by forwarding to
// opentile.ValidationProbe.Flag with the right CheckCode.
type Sink interface {
	// OffsetOutOfBounds reports one tile/strip whose byte range escapes the
	// file. level is the page's level index (or -1 if unknown).
	OffsetOutOfBounds(level int, msg string)
	// OrphanIFD reports one IFD not reachable as a level/associated page.
	OrphanIFD(msg string)
}

// checkRange flags a single byte range [offset, offset+length) that does not
// fit within [0, size). Detects offset+length overflow.
func checkRange(s Sink, level int, offset, length, size uint64) {
	end := offset + length
	if end < offset { // unsigned overflow
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d overflows", offset, length))
		return
	}
	if offset > uint64(size) || end > uint64(size) {
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d exceeds file size %d", offset, length, size))
	}
}
```

Wait — `size` is passed as `uint64` in `checkRange` but compared to `uint64(size)`. Make the signature `checkRange(s Sink, level int, offset, length, size uint64)` and drop the redundant conversion:

```go
func checkRange(s Sink, level int, offset, length, size uint64) {
	end := offset + length
	if end < offset {
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d overflows", offset, length))
		return
	}
	if end > size {
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d exceeds file size %d", offset, length, size))
	}
}
```

Update the overflow test call accordingly: `checkRange(s, 0, maxU64-10, 1000, 1<<40)` already passes `size` as the 5th arg — fine.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tiffvalidate/ -run 'TestCheckByteRange'`
Expected: PASS.

- [ ] **Step 5: Add the file-level walk (offsets + orphan IFDs)**

Append to `internal/tiffvalidate/tiffvalidate.go`:

```go
// Check walks every page of f, flagging out-of-bounds tile/strip byte ranges
// and orphan IFDs. levelOf maps a page's IFD offset to a level index for
// locus reporting (return -1 if unknown). reachable reports whether a page's
// IFD offset is referenced as a level or associated image; pages that are not
// reachable are flagged as orphan IFDs.
func Check(f *tiff.File, s Sink, levelOf func(ifdOffset int64) int, reachable func(ifdOffset int64) bool) {
	size := uint64(f.Size())
	for _, p := range f.Pages() {
		lvl := levelOf(p.IFDOffset())

		// Tile byte ranges.
		if offs, err := p.TileOffsets(); err == nil && len(offs) > 0 {
			counts, _ := p.TileByteCounts()
			for i, off := range offs {
				var length uint64
				if i < len(counts) {
					length = uint64(counts[i])
				}
				checkRange(s, lvl, uint64(off), length, size)
			}
		}

		// Strip byte ranges.
		if offs, err := p.StripOffsets(); err == nil && len(offs) > 0 {
			counts, _ := p.StripByteCounts()
			for i, off := range offs {
				var length uint64
				if i < len(counts) {
					length = uint64(counts[i])
				}
				checkRange(s, lvl, uint64(off), length, size)
			}
		}

		// Orphan IFD: a page not referenced as level or associated image.
		if reachable != nil && !reachable(p.IFDOffset()) {
			s.OrphanIFD(fmt.Sprintf("IFD at offset %d is not referenced as a level or associated image", p.IFDOffset()))
		}
	}
}
```

Implementer notes:
- Verify `Page` exposes `StripOffsets() ([]uint32, error)` and `StripByteCounts()`. `page.go` defines `TagStripOffsets`/`TagStripByteCounts`; confirm there are array accessors (look for `arrayU32(TagStripOffsets)`-style methods, mirroring `TileOffsets()` at `page.go:211`). If the strip array accessors are missing, add them in `internal/tiff/page.go` following the exact shape of `TileOffsets`/`TileByteCounts`, and note it in the commit.
- `IFDOffset()` exists (`page.go:86`).
- For BigTIFF, offsets may exceed uint32. The existing `TileOffsets()` returns `[]uint32`. If the parser has a uint64 path for BigTIFF, prefer it; otherwise the uint32 view matches what the readers themselves use, so it is acceptable for v1. Do not silently widen — match the readers.

- [ ] **Step 6: Run package build/tests**

Run: `go test ./internal/tiffvalidate/ -count=1 && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/tiffvalidate/
git commit -m "feat(tiffvalidate): shared TIFF byte-range bounds + orphan-IFD walker"
```

---

## Task 6: Wire SVS + generic-TIFF Validator hooks

**Files:**
- Create: `formats/svs/validate.go`, `formats/generictiff/validate.go`
- Test: `formats/svs/validate_test.go` (and a root hermetic test for the offset check)

Each TIFF reader's `tiler` holds a `*tiff.File`. The hook adapts `ValidationProbe` to a `tiffvalidate.Sink` and calls `tiffvalidate.Check`. For v1, `levelOf` returns -1 and `reachable` returns true for every page except those the reader can identify as orphan (start simple: `reachable` returns true always → no orphan findings yet; SVS/generic both classify pages, so orphan detection can be added once `reachable` is wired to the page classification — but DO flag orphans only when the reader has a real reachability map; otherwise pass a `reachable` that always returns true to avoid false orphan findings).

- [ ] **Step 1: Write the failing hermetic offset test (root)**

Append to `validate_test.go` (root). This builds a minimal tiled single-IFD TIFF, points one TileOffset past EOF, and asserts the SVS/generic path flags it. Use the existing synthetic-TIFF builder from `formats/generictiff/internal/gates` if it is importable; otherwise build the bytes directly. Because constructing a full synthetic SVS is heavy, this test instead targets the shared helper through a generic-TIFF file:

```go
func TestValidateFlagsOutOfBoundsOffsetGenericTIFF(t *testing.T) {
	raw := buildTiledTIFFWithBadOffset(t) // helper defined below
	rep, err := Validate(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("operational error: %v", err)
	}
	var oob *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Code == CheckOffsetsOutOfBounds {
			oob = &rep.Findings[i]
		}
	}
	if oob == nil {
		t.Fatalf("expected CheckOffsetsOutOfBounds, got %+v", rep.Findings)
	}
	if oob.Severity != Error {
		t.Fatalf("severity = %v, want Error", oob.Severity)
	}
}
```

The helper `buildTiledTIFFWithBadOffset` constructs a valid little-endian classic TIFF with one tiled IFD (ImageWidth/Length, TileWidth/Length, TileOffsets pointing past EOF, TileByteCounts) such that `generictiff` opens it. Implement it in `validate_test.go` using `encoding/binary`. (If `formats/generictiff/internal/gates` already exposes a synthetic tiled-TIFF builder, reuse it and mutate one TileOffsets entry instead — prefer reuse. Read `formats/generictiff/internal/gates/synth_test.go` to see the existing builder.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestValidateFlagsOutOfBoundsOffsetGenericTIFF'`
Expected: FAIL — no `CheckOffsetsOutOfBounds` finding yet (generictiff has no Validate hook).

- [ ] **Step 3: Implement the generic-TIFF hook**

Create `formats/generictiff/validate.go`:

```go
package generictiff

import (
	"github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiffvalidate"
)

// probeSink adapts opentile.ValidationProbe to tiffvalidate.Sink.
type probeSink struct{ p *opentile.ValidationProbe }

func (s probeSink) OffsetOutOfBounds(level int, msg string) {
	s.p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level, msg)
}
func (s probeSink) OrphanIFD(msg string) {
	s.p.Flag(opentile.CheckOrphanIFD, -1, -1, msg)
}

// Validate contributes TIFF byte-range checks. (Orphan detection is disabled
// for now via an always-reachable predicate to avoid false positives until a
// real reachability map is wired.)
func (t *tiler) Validate(p *opentile.ValidationProbe) {
	tiffvalidate.Check(t.file, probeSink{p}, func(int64) int { return -1 }, func(int64) bool { return true })
}
```

Verify the field name: in `formats/generictiff/tiler.go`, the `tiler` holds its `*tiff.File` — confirm the field name (likely `file`). If it is named differently (e.g. `f`), use that. The `TIFFDirectories()` method at `tiler.go:87` already accesses it; match that access.

- [ ] **Step 4: Run the root test to verify it passes**

Run: `go test . -run 'TestValidateFlagsOutOfBoundsOffsetGenericTIFF'`
Expected: PASS.

- [ ] **Step 5: Implement the SVS hook (identical adapter)**

Create `formats/svs/validate.go`:

```go
package svs

import (
	"github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiffvalidate"
)

type probeSink struct{ p *opentile.ValidationProbe }

func (s probeSink) OffsetOutOfBounds(level int, msg string) {
	s.p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level, msg)
}
func (s probeSink) OrphanIFD(msg string) {
	s.p.Flag(opentile.CheckOrphanIFD, -1, -1, msg)
}

func (t *tiler) Validate(p *opentile.ValidationProbe) {
	tiffvalidate.Check(t.file, probeSink{p}, func(int64) int { return -1 }, func(int64) bool { return true })
}
```

Confirm the SVS `tiler`'s `*tiff.File` field name in `formats/svs/svs.go`/`tiled.go` (the `TIFFDirectories()` method at `svs.go:203` accesses it — match that).

- [ ] **Step 6: Run affected packages**

Run: `go test . ./formats/svs/... ./formats/generictiff/... -race -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add validate_test.go formats/svs/validate.go formats/generictiff/validate.go
git commit -m "feat(validate): SVS + generic-TIFF Validator hooks via tiffvalidate"
```

---

## Task 7: Wire NDPI + Philips + Leica-SCN + OME-TIFF + BIF hooks

**Files:**
- Create: `formats/ndpi/validate.go`, `formats/philipstiff/validate.go`, `formats/leicascn/validate.go`, `formats/ometiff/validate.go`, `formats/bif/validate.go`
- Test: `validate_corpus_test.go` exercises these against real fixtures in Task 12; this task adds the hooks and relies on a build + a smoke test.

Each is the same adapter as Task 6. The only per-reader variable is the `*tiff.File` field name and (for NDPI) that an NDPI level is one big JPEG strip — its strip offsets still get bounds-checked, which is correct.

- [ ] **Step 1: Implement each hook**

For each format, create `formats/<name>/validate.go` with this body (substitute the package name and the reader type/field):

```go
package <pkg>

import (
	"github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiffvalidate"
)

type probeSink struct{ p *opentile.ValidationProbe }

func (s probeSink) OffsetOutOfBounds(level int, msg string) {
	s.p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level, msg)
}
func (s probeSink) OrphanIFD(msg string) {
	s.p.Flag(opentile.CheckOrphanIFD, -1, -1, msg)
}

func (t *<readerType>) Validate(p *opentile.ValidationProbe) {
	tiffvalidate.Check(t.<fileField>, probeSink{p}, func(int64) int { return -1 }, func(int64) bool { return true })
}
```

Resolve `<pkg>`, `<readerType>`, `<fileField>` per format by reading each reader's existing `TIFFDirectories()` method (it already accesses the `*tiff.File`):
- `ndpi` — `formats/ndpi/tifftags.go` / `tiler.go`
- `philipstiff` — `formats/philipstiff/tiled.go` / `philips.go`
- `leicascn` — `formats/leicascn/tiler.go`
- `ometiff` — `formats/ometiff/ome.go` / `ometiff.go`
- `bif` — `formats/bif/bif.go` / `level.go`

If a reader does not currently store the `*tiff.File` on the type that is reachable through `UnwrapReader`, store it (add a field, set it at open) — match how `TIFFDirectories()` reaches the pages; the hook must sit on the same type the `UnwrapReader` chain exposes (the type that implements `TIFFDirectories`).

- [ ] **Step 2: Build to verify all compile**

Run: `go build ./... && go vet ./formats/...`
Expected: clean.

- [ ] **Step 3: Smoke test discovery for one of them (NDPI) if a fixture is present**

Add to `validate_corpus_test.go` (created here, expanded in Task 12):

```go
package opentile

import (
	"os"
	"path/filepath"
	"testing"
)

func testdir(t *testing.T) string {
	d := os.Getenv("OPENTILE_TESTDIR")
	if d == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	return d
}

func TestValidateNDPIFixtureOK(t *testing.T) {
	p := filepath.Join(testdir(t), "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean NDPI fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != FormatNDPI {
		t.Fatalf("Format = %q, want ndpi", rep.Format)
	}
}
```

- [ ] **Step 4: Run**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run 'TestValidateNDPIFixtureOK' -count=1`
Expected: PASS (or SKIP if the fixture is absent).

- [ ] **Step 5: Commit**

```bash
git add formats/ndpi/validate.go formats/philipstiff/validate.go formats/leicascn/validate.go formats/ometiff/validate.go formats/bif/validate.go validate_corpus_test.go
git commit -m "feat(validate): NDPI/Philips/Leica-SCN/OME-TIFF/BIF Validator hooks"
```

---

## Task 8: COG-WSI hook with NonConformantFormat

**Files:**
- Create: `formats/cogwsi/validate.go`
- Test: `formats/cogwsi/validate_test.go`

COG-WSI already has `ErrNotConformantCOGWSI` produced by `validateGhost`/`validateIFDs` at open. By the time `Validate` runs, the file *opened*, so it was conformant at the gross level. The hook adds the byte-range checks (via tiffvalidate, delegating to the inner generic-TIFF) and re-runs the COG-WSI structural validators in a non-fatal mode, flagging any soft violations as `CheckNonConformantFormat`.

- [ ] **Step 1: Write the failing test**

Create `formats/cogwsi/validate_test.go`:

```go
package cogwsi

import "testing"

// TestValidateHookExists is a compile/behavior smoke test: the reader type
// implements opentile.Validator (verified structurally) and the byte-range
// path runs without panicking on a minimal valid in-memory COG-WSI.
func TestValidateSurfacesByteRangeChecks(t *testing.T) {
	// Build / load a minimal valid cog-wsi the same way other cogwsi tests do
	// (reuse the package's existing test fixtures/builders). Then validate and
	// assert no panic and a *Report-able set of findings.
	t.Skip("fill in using the cogwsi package's existing test fixture/builder; assert OK() on a valid file and CheckNonConformantFormat on a soft-violation file")
}
```

Note: this is the one task where the test must be authored against the cogwsi package's existing test helpers. Read `formats/cogwsi/tiler_test.go` and `validation.go` first; replace the skip with a real test that (a) validates a known-good cogwsi → `OK()`, and (b) mutates a ghost-area/IFD soft field → `CheckNonConformantFormat` fires. If COG-WSI validation is strictly all-or-nothing at open (no "soft" violations possible post-open), then drop the NonConformantFormat re-run and have the hook only delegate byte-range checks; document that in the commit.

- [ ] **Step 2: Implement the hook**

Create `formats/cogwsi/validate.go`:

```go
package cogwsi

import (
	"github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiffvalidate"
)

type probeSink struct{ p *opentile.ValidationProbe }

func (s probeSink) OffsetOutOfBounds(level int, msg string) {
	s.p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level, msg)
}
func (s probeSink) OrphanIFD(msg string) {
	s.p.Flag(opentile.CheckOrphanIFD, -1, -1, msg)
}

// Validate delegates byte-range checks to the inner TIFF file. COG-WSI's
// conformance is enforced at Open (ErrNotConformantCOGWSI); any post-open soft
// checks would be flagged as CheckNonConformantFormat here.
func (t *tiler) Validate(p *opentile.ValidationProbe) {
	tiffvalidate.Check(t.tiffFile(), probeSink{p}, func(int64) int { return -1 }, func(int64) bool { return true })
}
```

Resolve how the cogwsi `tiler` reaches its `*tiff.File` (it wraps an inner generictiff reader — it may already expose the file or delegate via `UnwrapReader`). If cogwsi's `UnwrapReader` already returns the inner generictiff reader (which now has a `Validate` hook from Task 6), then **the cogwsi tiler may not need its own hook at all** — `validatorOf` would find the inner generictiff's `Validate` through the chain. Verify this first: if cogwsi delegates via `UnwrapReader` to generictiff, DELETE this file and instead add a test asserting `validatorOf` finds the inner hook. Prefer that (free, DRY) if the delegation exists.

- [ ] **Step 3: Run**

Run: `go test ./formats/cogwsi/... . -race -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add formats/cogwsi/validate.go formats/cogwsi/validate_test.go
git commit -m "feat(validate): COG-WSI byte-range checks (via inner TIFF) + conformance surfacing"
```

---

## Task 9: IFE Validator hook (block byte ranges)

**Files:**
- Create: `formats/ife/validate.go`
- Test: `formats/ife/validate_test.go`

IFE is non-TIFF: tiles live in blocks with an index table. The hook checks each block's `offset+length` against the file size, flagging `CheckOffsetsOutOfBounds`.

- [ ] **Step 1: Read the IFE layout**

Read `formats/ife/ife.go`, `tiler.go`, `encoding.go` to find where the per-tile/per-block offsets and lengths and the file size are held on the reader type that the `UnwrapReader` chain exposes.

- [ ] **Step 2: Write the failing test**

Create `formats/ife/validate_test.go`:

```go
package ife

import "testing"

func TestValidateBlockOffsetsInBounds(t *testing.T) {
	t.Skip("author against the package's synthetic IFE builder in synthetic_test.go: assert a valid IFE yields no CheckOffsetsOutOfBounds, and a mutated block offset (past EOF) flags one")
}
```

Read `formats/ife/synthetic_test.go` for the existing synthetic-IFE builder; replace the skip with the real assertions (valid → no finding; one mutated past-EOF block offset → one `opentile.CheckOffsetsOutOfBounds`). Run validation through `opentile.Validate(bytes.NewReader(raw), size)`.

- [ ] **Step 3: Implement the hook**

Create `formats/ife/validate.go`:

```go
package ife

import (
	"fmt"

	"github.com/wsilabs/opentile-go"
)

// Validate checks that every block's byte range lies within the file.
func (t *<readerType>) Validate(p *opentile.ValidationProbe) {
	size := uint64(p.Size())
	for _, b := range t.<blocksAccessor>() { // each b has Offset, Length
		off, ln := uint64(b.Offset), uint64(b.Length)
		end := off + ln
		if end < off || end > size {
			p.Flag(opentile.CheckOffsetsOutOfBounds, 0, b.Level,
				fmt.Sprintf("block offset=%d length=%d exceeds file size %d", off, ln, size))
		}
	}
}
```

Replace `<readerType>`, `<blocksAccessor>`, and the field names (`Offset`/`Length`/`Level`) with the real IFE structures. If blocks don't carry a level, pass `0` or `-1` as the locus consistently.

- [ ] **Step 4: Run**

Run: `go test ./formats/ife/... -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add formats/ife/validate.go formats/ife/validate_test.go
git commit -m "feat(validate): IFE block byte-range Validator hook"
```

---

## Task 10: SZI Validator hook (ZIP entry ranges)

**Files:**
- Create: `formats/szi/validate.go`
- Test: `formats/szi/validate_test.go`

SZI is a ZIP of Deep Zoom tiles. The hook checks each referenced ZIP entry's data range against the file size.

- [ ] **Step 1: Read the SZI layout**

Read `formats/szi/tiler.go`, `level.go`, `factory.go` to find the ZIP entry table (offset + compressed size) and the file size on the reader type the `UnwrapReader` chain exposes.

- [ ] **Step 2: Write the failing test**

Create `formats/szi/validate_test.go`:

```go
package szi

import "testing"

func TestValidateZipEntryRangesInBounds(t *testing.T) {
	t.Skip("author against the package's existing test fixtures (CMU-1.szi via OPENTILE_TESTDIR) or a synthetic builder: a valid SZI yields no CheckOffsetsOutOfBounds")
}
```

If SZI has no in-package synthetic builder, gate the positive test on `OPENTILE_TESTDIR` (skip-if-missing) and assert `opentile.ValidateFile(cmu1Szi).OK()`. A negative (mutated) test is optional if constructing a corrupt ZIP in-test is heavy — note that omission in the commit.

- [ ] **Step 3: Implement the hook**

Create `formats/szi/validate.go`:

```go
package szi

import (
	"fmt"

	"github.com/wsilabs/opentile-go"
)

func (t *<readerType>) Validate(p *opentile.ValidationProbe) {
	size := uint64(p.Size())
	for _, e := range t.<entriesAccessor>() { // each e: data offset + length
		off, ln := uint64(e.DataOffset), uint64(e.Length)
		end := off + ln
		if end < off || end > size {
			p.Flag(opentile.CheckOffsetsOutOfBounds, 0, -1,
				fmt.Sprintf("zip entry %q offset=%d length=%d exceeds file size %d", e.Name, off, ln, size))
		}
	}
}
```

Replace the reader type, the entries accessor, and the field names with the real SZI ZIP structures (the `archive/zip` reader exposes per-file offsets via `*zip.File`; if SZI uses `archive/zip`, derive the data offset with `f.DataOffset()` and length `f.CompressedSize64`).

- [ ] **Step 4: Run**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/szi/... -race -count=1`
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```bash
git add formats/szi/validate.go formats/szi/validate_test.go
git commit -m "feat(validate): SZI ZIP-entry byte-range Validator hook"
```

---

## Task 11: DICOM Validator hook (frame offset ranges)

**Files:**
- Create: `formats/dicom/validate.go`
- Test: `formats/dicom/validate_test.go`

DICOM stores frames as fragments; `formats/dicom` walks fragment offsets itself (per the project notes). The hook checks each frame fragment's byte range against the per-file size, and (cheaply) flags a count mismatch between declared `NumFrames` and the assembled frame table as `CheckNonConformantFormat` if the reader exposes both.

- [ ] **Step 1: Read the DICOM layout**

Read `formats/dicom/tiler.go`, `factory.go`, `open.go` to find the per-instance frame fragment offset/length table and the per-file sizes (DICOM is multi-file; each instance has its own size). Identify the reader type on the `UnwrapReader` chain.

- [ ] **Step 2: Write the failing test**

Create `formats/dicom/validate_test.go`:

```go
package dicom

import "testing"

func TestValidateDICOMFixtureOK(t *testing.T) {
	t.Skip("gate on OPENTILE_TESTDIR; assert opentile.ValidateFile(<dicom series dir>).OK() for a known-good series (e.g. scan_621_grundium_dicom or 3DHISTECH-HTJ2K)")
}
```

Replace the skip with a real `OPENTILE_TESTDIR`-gated test using a known-good DICOM series directory from `sample_files/dicom/`; assert `OK()` and `Format == opentile.FormatDICOM`.

- [ ] **Step 3: Implement the hook**

Create `formats/dicom/validate.go`:

```go
package dicom

import (
	"fmt"

	"github.com/wsilabs/opentile-go"
)

// Validate checks each frame fragment's byte range against its instance's file
// size. DICOM is multi-file, so sizes are per instance, not p.Size().
func (t *<readerType>) Validate(p *opentile.ValidationProbe) {
	for _, fr := range t.<framesAccessor>() { // each fr: file size + offset + length + level
		off, ln, fsize := uint64(fr.Offset), uint64(fr.Length), uint64(fr.FileSize)
		end := off + ln
		if end < off || end > fsize {
			p.Flag(opentile.CheckOffsetsOutOfBounds, 0, fr.Level,
				fmt.Sprintf("frame offset=%d length=%d exceeds instance size %d", off, ln, fsize))
		}
	}
}
```

Replace the reader type, frames accessor, and field names with the real DICOM structures. If per-frame file size is not readily available, use the instance's size; the requirement is that each frame's byte range is checked against the file that contains it. (The `p.Size()` engine value is the first-instance / directory entry size and is not meaningful for multi-file DICOM — use per-instance sizes from the reader.)

- [ ] **Step 4: Run**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/... -race -count=1`
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```bash
git add formats/dicom/validate.go formats/dicom/validate_test.go
git commit -m "feat(validate): DICOM frame-fragment byte-range Validator hook"
```

---

## Task 12: Cross-format clean-corpus gate + contract tests + nocgo

**Files:**
- Modify: `validate_corpus_test.go`
- Modify: `validate_test.go` (rollup + error-channel contract tests)

- [ ] **Step 1: Write the cross-format clean gate**

Append to `validate_corpus_test.go`:

```go
func TestValidateCleanCorpusOK(t *testing.T) {
	dir := testdir(t)
	cases := []struct {
		rel  string
		want Format
	}{
		{"svs/CMU-1-Small-Region.svs", FormatSVS},
		{"ndpi/CMU-1.ndpi", FormatNDPI},
		{"philips-tiff/Philips-4.tiff", FormatPhilipsTIFF},
		{"generic-tiff/CMU-1-Small-Region.stripped.tiff", FormatGenericTIFF},
		{"ome-tiff/CMU-1-Small-Region.ome.tiff", FormatOMETIFF},
		{"bif/Ventana-1.bif", FormatBIF},
		{"cog-wsi/CMU-1-Small-Region_cog-wsi.tiff", FormatCOGWSI},
	}
	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			p := filepath.Join(dir, tc.rel)
			if _, err := os.Stat(p); err != nil {
				t.Skipf("fixture absent: %v", err)
			}
			rep, err := ValidateFile(p)
			if err != nil {
				t.Fatalf("ValidateFile: %v", err)
			}
			if rep.Format != tc.want {
				t.Errorf("Format = %q, want %q", rep.Format, tc.want)
			}
			if !rep.OK() {
				t.Errorf("clean fixture not OK; Error findings: %+v", errorFindings(rep))
			}
		})
	}
}

func errorFindings(r *Report) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == Error {
			out = append(out, f)
		}
	}
	return out
}
```

Use the actual fixture paths present in `sample_files/` and the public `wsi-fixtures` corpus (the corpus extracts to `<dir>/svs/...`, etc.). Skip-if-missing keeps it green where the corpus is absent. The fixture filenames must match the corpus — verify against `wsi-fixtures` `manifest.json` / the `README` table.

- [ ] **Step 2: Write the rollup + error-channel contract tests**

Append to `validate_test.go`:

```go
func TestValidateErrorChannelMissingPath(t *testing.T) {
	if _, err := ValidateFile("/no/such/file.svs"); err == nil {
		t.Fatal("missing path should be an operational error")
	}
}

func TestValidateNonPositiveSize(t *testing.T) {
	if _, err := Validate(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("size<=0 should be an operational error")
	}
}
```

- [ ] **Step 3: Run with the corpus**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -race -count=1`
Expected: PASS (corpus tests run against local fixtures; absent ones skip).

- [ ] **Step 4: Verify the nocgo build + validation under it**

Run: `CGO_ENABLED=0 go build -tags nocgo ./... && CGO_ENABLED=0 go test -tags nocgo . -run 'TestReportOK|TestProbeRollupCountOnly|TestValidatorOf|TestEngine|TestValidateUnopenableBytes|TestValidateNonPositiveSize' -count=1`
Expected: PASS — Tier 0/1 is decode-free, so validation builds and the hermetic tests pass under nocgo.

- [ ] **Step 5: Commit**

```bash
git add validate_corpus_test.go validate_test.go
git commit -m "test(validate): clean-corpus gate, contract tests, nocgo verification"
```

---

## Task 13: Docs + CHANGELOG

**Files:**
- Modify: `README.md` (API section), `CHANGELOG.md` (`[Unreleased]`)
- Create: `docs/validate.md`

- [ ] **Step 1: Add a CHANGELOG entry**

In `CHANGELOG.md`, under `## [Unreleased]`, add:

```markdown
### Added

- **`Validate` API** (`opentile.ValidateFile`, `opentile.Validate`,
  `(*Slide).Validate`) — structural WSI validation (tiers 0 + 1: openability and
  no-decode structural integrity). Returns a findings-as-data `Report`
  (`Finding`/`Severity`/`CheckCode`, `report.OK()`), rolling repeated problems up
  by category with a count. Checks: unopenable, out-of-bounds tile/strip/frame
  offsets, tile-grid mismatch, format non-conformance, inconsistent pyramid,
  missing metadata, orphan IFD. Decode-free (nocgo-safe). Pixel-correctness and
  full spec-conformance are explicitly out of scope (see `docs/validate.md`).
  Tier 2 (pixel-decode checks) is reserved via the `ValidateOption` seam, not
  built.
```

- [ ] **Step 2: Write `docs/validate.md`**

Create `docs/validate.md` summarizing: the three entry points, the `Report`/`Finding` model, the check catalog table (from the spec §5), and — prominently — the four fences from spec §2 (valid ≠ correct pixels; ≠ spec-conformant; as-detected; not a repair tool). Keep it to ~1 page. Pull the catalog table and fences verbatim from `docs/superpowers/specs/2026-06-16-validate-api-design.md` so they stay consistent.

- [ ] **Step 3: Add a short README mention**

In `README.md`, in the API/usage section, add a `Validate` subsection with a minimal example:

```go
rep, err := opentile.ValidateFile("slide.svs")
if err != nil { /* unreadable */ }
if !rep.OK() {
    for _, f := range rep.Findings {
        fmt.Printf("%s [%s] %s (x%d)\n", f.Severity, f.Code, f.Message, f.Count)
    }
}
```

Note in prose that "OK" means well-formed per opentile-go's reader — not that pixels are correct or the file is fully spec-conformant; link to `docs/validate.md`.

- [ ] **Step 4: Verify docs build / links**

Run: `go test . -count=1 && go vet ./...`
Expected: PASS (no code change, sanity only).

- [ ] **Step 5: Commit**

```bash
git add README.md CHANGELOG.md docs/validate.md
git commit -m "docs(validate): document the Validate API, check catalog, and the four fences"
```

---

## Final verification (after all tasks)

- [ ] Run the full suite under race: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1` → all green.
- [ ] Run `make vet` → clean.
- [ ] Run the nocgo build: `CGO_ENABLED=0 go build -tags nocgo ./...` → clean.
- [ ] Confirm `make bench-ndpi` / `make bench-svs` unaffected (validation adds no hot-path code) — quick sanity, not a gate.
- [ ] Dispatch a final holistic code review over the whole branch.
- [ ] Use superpowers:finishing-a-development-branch to complete.

---

## Spec coverage notes (self-check)

- Spec §3 tiers 0+1 → Tasks 4 (Tier 0 + agnostic Tier 1), 5–11 (reader Tier 1). Tier 2 seam → `ValidateOption` in Task 4 (empty), documented Task 13.
- Spec §4 types/entry points → Tasks 1–4. Operational-error-only semantics → Task 4 (`validateOpened`) + Task 12 contract tests.
- Spec §5 catalog: `Unopenable` (T4), `OffsetsOutOfBounds` (T5–11), `TileGridMismatch` (T4), `InconsistentPyramid` (T4), `MissingMetadata` (T4, MPP), `OrphanIFD` (T5 helper; wired conservatively off by default until a real reachability map — noted in T6), `NonConformantFormat` (T8 cogwsi). NOTE: `MissingMetadata` is implemented in the format-agnostic engine (MPP is on the public `Level`), a deliberate DRY refinement over the spec's "per-format hook" placement; per-format metadata findings can still be added via the hook later.
- Spec §6 architecture: two layers (T4 engine + T5–11 hooks), `validatorOf` discovery (T3), `tiffvalidate` shared helper (T5), collector/rollup (T2).
- Spec §7 Tier-2 seam: empty `ValidateOption` (T4), open `CheckCode` (T1). Engine-orchestrated decode + nocgo→Info are future-work, not built.
- Spec §8 testing: hermetic per-check (T4, T6), corpus gate (T12), contract + nocgo (T12).
- Spec §9 file structure → matches the File Structure section above.

**Open verification items the implementer MUST confirm against real code (flagged inline):** `Size` field names; `MPP.IsZero()`; per-reader `*tiff.File` field names; strip-array accessors on `tiff.Page`; whether cogwsi reaches generictiff's hook via `UnwrapReader` (making T8's own hook unnecessary); IFE/SZI/DICOM offset-table accessors.
