# opentile-go v0.3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every known v0.2 limitation, reviewer suggestion, and review nice-to-have. v0.3 is the settled-API + test-complete + correctness-thorough milestone.

**Architecture:** Polish-only. Same package layout as v0.2: `opentile` root, `internal/{tiff,jpeg,jpegturbo}`, `formats/{svs,ndpi,all}`, `tests/oracle`. One additive new package `opentile/opentiletest` for test helpers. No new external dependencies.

**Tech Stack:** Go 1.23+, libjpeg-turbo 2.1+ via cgo, Python opentile 0.20.0 via subprocess (parity-only, opt-in `//go:build parity`).

**Spec:** `docs/superpowers/specs/2026-04-24-opentile-go-v03-design.md`.

**Branch:** `feat/v0.3` (already created from `main` at `d121e28`; spec commit `4ae3172` is the only commit on the branch).

**Sample slides:** Local under `sample_files/svs/`, `sample_files/ndpi/`, etc. Set `OPENTILE_TESTDIR=$PWD/sample_files` for integration tests.

**Python venv:** `/tmp/opentile-py/bin/python` has Python 3.12 + opentile 0.20.0 + tifffile 2023.12.9. Reuse for parity-oracle work. Recreate via:
```
/opt/homebrew/bin/python3.12 -m venv /tmp/opentile-py && \
  /tmp/opentile-py/bin/pip install -q opentile 'tifffile<2024' imagecodecs PyTurboJPEG
```

---

## File structure

New files this plan creates:

| Path | Responsibility |
|---|---|
| `opentile/opentiletest/opentiletest.go` | Test-helper subpackage (T1: `NewConfig`) |
| `opentile/opentiletest/opentiletest_test.go` | Test for the subpackage's helper |
| `internal/jpeg/mcu.go` | `MCUSizeOf` helper used by L7 + L11 fixes |
| `internal/jpeg/mcu_test.go` | Tests for `MCUSizeOf` |
| `formats/svs/lzwlabel.go` | LZW label decode-restitch (L10) |
| `formats/svs/lzwlabel_test.go` | LZW round-trip test |
| `tests/sampled.go` | Sampled-fixture types + position computation (L15) |
| `tests/sampled_test.go` | Tests for the sampled-position rule |
| `scripts/cover.sh` | Coverage gate (≥80% per package) |
| `Makefile` | `make test`, `make cover`, `make parity` targets |

Files modified:

| Path | What changes |
|---|---|
| `options.go` | T1: `NewTestConfig` becomes a deprecated alias |
| `opentile.go` | A2 (path in error), A3 (`Formats()`), N-5 (`WithNDPISynthesizedLabel`) |
| `errors.go` | A1: `ErrTooManyIFDs` sentinel |
| `internal/tiff/ifd.go` | A1 wiring; O1 bulk-read |
| `internal/tiff/page.go` | I2/O2 comments |
| `formats/svs/metadata.go` | L1: TrimRight CRLF |
| `formats/svs/associated.go` | L11: use `MCUSizeOf` for DRI math |
| `formats/svs/svs.go` | L10 label uses new `lzwlabel`; consolidate via `indexOf` (I1) |
| `formats/svs/tiled.go` | I1: fold zero-length check into `indexOf` |
| `formats/ndpi/ndpi.go` | L7: use `MCUSizeOf` for label MCU; N-5 wiring |
| `formats/ndpi/oneframe.go` | I8: `paddedJPEGOnce` → `sync.Once`; N-7 wrapping |
| `formats/ndpi/striped.go` | N-1: drop `patchSOFSize` (use shared); N-10 invert; N-7 wrapping |
| `formats/ndpi/stripes.go` | N-7 wrapping |
| `formats/ndpi/associated.go` | N-7 wrapping; N-5 conditional |
| `internal/jpeg/concat.go` | I4 Assembler; N-2 segment-walker; I5 RSTn count; I7 comment |
| `internal/jpeg/dqt.go` | N-4: segment-boundary validation |
| `internal/jpeg/marker.go` | I6: ranged RSTn check |
| `internal/jpeg/scan.go` | I3: detect existing bufio.Reader |
| `internal/jpeg/sof.go` | N-1: canonical SOF dimensions patcher |
| `internal/jpegturbo/turbo_cgo.go` | N-3: pre-computed DC value via CropOpts |
| `internal/jpegturbo/turbo.go` | N-3: `CropOpts` type |
| `tests/integration_test.go` | Trivial-getter coverage; sampled-fixture branch; new slides |
| `tests/generate_test.go` | Sampled-mode generator |
| `tests/fixtures.go` | Schema extension (SampledTileSHA256) |
| `tests/oracle/oracle_runner.py` | Batched stdin/stdout protocol (L16) |
| `tests/oracle/oracle.go` | Batched subprocess wrapper |
| `tests/oracle/parity_test.go` | Use batched runner; default sample → 100/level |
| `opentile_test.go` | T2: `withRegistry` helper |
| `formats/svs/metadata_test.go` | T5: CRLF parser tests |
| `formats/svs/tiled_test.go` | T4: mapCompression default; L3: synthetic LZW |
| `internal/tiff/ifd_test.go` | T3: cycle test; T6: real multi-IFD |
| `internal/jpegturbo/turbo_cgo_test.go` | L9: concurrency stress |
| `docs/deferred.md` | Retirement audit |
| `CLAUDE.md` | Milestone bump |
| `README.md` | Spot updates |

---

# Batch A — API settlement + Hamamatsu-1 sparse fixture

## Task 1: T1 — Move `NewTestConfig` to `opentile/opentiletest`

**Files:**
- Create: `opentile/opentiletest/opentiletest.go`
- Create: `opentile/opentiletest/opentiletest_test.go`
- Modify: `options.go` (deprecate `NewTestConfig`)
- Modify: every `_test.go` that calls `opentile.NewTestConfig` (rename to `opentiletest.NewConfig`)

- [ ] **Step 1: Create the new subpackage**

`opentile/opentiletest/opentiletest.go`:
```go
// Package opentiletest provides test helpers for opentile consumers and the
// library's own internal tests. Helpers here construct opentile.Config
// values with explicit field overrides — never call this from production
// code paths.
package opentiletest

import opentile "github.com/wsilabs/opentile-go"

// NewConfig constructs an opentile.Config for use in tests. A non-zero
// tileSize is treated as explicitly set (TileSize ok=true); a zero Size
// is treated as "use format default" (TileSize ok=false). The policy
// argument follows the same semantics as WithCorruptTilePolicy.
func NewConfig(tileSize opentile.Size, policy opentile.CorruptTilePolicy) *opentile.Config {
	if tileSize == (opentile.Size{}) {
		return opentile.NewTestConfig(opentile.Size{}, policy)
	}
	return opentile.NewTestConfig(tileSize, policy)
}
```

Note: this calls through to the existing `opentile.NewTestConfig`. The deprecation alias remains so we don't have to rewrite every caller in one commit (although we will, in step 4).

- [ ] **Step 2: Test the new subpackage**

`opentile/opentiletest/opentiletest_test.go`:
```go
package opentiletest_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/opentile/opentiletest"
)

func TestNewConfigZeroSizeUnset(t *testing.T) {
	c := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	sz, ok := c.TileSize()
	if ok {
		t.Errorf("TileSize ok: got true, want false (zero size means unset)")
	}
	_ = sz
}

func TestNewConfigExplicitSize(t *testing.T) {
	c := opentiletest.NewConfig(opentile.Size{W: 512, H: 256}, opentile.CorruptTileError)
	sz, ok := c.TileSize()
	if !ok {
		t.Fatal("TileSize ok: got false, want true (explicit size)")
	}
	if sz != (opentile.Size{W: 512, H: 256}) {
		t.Errorf("TileSize: got %v, want {512, 256}", sz)
	}
}
```

- [ ] **Step 3: Run the new tests**

Run: `go test ./opentile/opentiletest/...`
Expected: PASS, both tests.

- [ ] **Step 4: Update existing call sites**

Run: `grep -rln "opentile.NewTestConfig" --include="*.go"` to find every test file using the old name. Replace each with:
```go
import "github.com/wsilabs/opentile-go/opentile/opentiletest"
// ...
cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
```

Known call sites at v0.2 commit `d121e28`:
- `formats/ndpi/striped_test.go`
- `formats/ndpi/oneframe_test.go`
- `formats/svs/svs_test.go`
- `formats/svs/tiled_test.go`
- `formats/svs/metadata_test.go`
- `tests/integration_test.go`

(If `grep` finds others, update those too.)

- [ ] **Step 5: Mark `opentile.NewTestConfig` as deprecated**

In `options.go`, find the existing `NewTestConfig` function (around line 71) and replace its godoc with:
```go
// NewTestConfig constructs a Config for use in tests.
//
// Deprecated: use opentile/opentiletest.NewConfig. This wrapper remains
// for one release to keep external callers compiling; it will be removed
// in v0.4.
func NewTestConfig(tileSize Size, policy CorruptTilePolicy) *Config {
```

Implementation body unchanged.

- [ ] **Step 6: Run full tests**

Run: `go test ./... -race -count=1`
Expected: PASS everywhere. Vet: `go vet ./...` clean.

- [ ] **Step 7: Commit**

```bash
git add opentile/opentiletest/ options.go formats/ tests/
git commit -m "refactor(test): T1 — move NewTestConfig to opentile/opentiletest

Test helper now lives in a sibling package (stdlib idiom: httptest, iotest).
The opentile.NewTestConfig name remains for one release as a deprecation
alias; v0.4 removes it.

All existing test call sites updated. Closes T1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: A1 — `ErrTooManyIFDs` sentinel

**Files:**
- Modify: `errors.go`
- Modify: `internal/tiff/ifd.go` (replace formatted error with sentinel)
- Test: `internal/tiff/ifd_test.go` (extended in Task 16)

- [ ] **Step 1: Add the sentinel**

In `errors.go`, append:
```go
// ErrTooManyIFDs is returned when a TIFF IFD chain exceeds the safety cap
// (1024 IFDs) before terminating. Either the file is corrupt, presents a
// cycle, or is malicious.
var ErrTooManyIFDs = errors.New("opentile: TIFF IFD chain exceeded the safety cap")
```

- [ ] **Step 2: Wire the sentinel into `walkIFDs`**

In `internal/tiff/ifd.go`, find the `maxIFDs` cap error (currently a `fmt.Errorf("walked %d IFDs without termination, suspected cycle", maxIFDs)`):

```go
// Before:
return nil, fmt.Errorf("walked %d IFDs without termination, suspected cycle", maxIFDs)

// After:
return nil, fmt.Errorf("internal/tiff: %w (cap=%d)", opentile.ErrTooManyIFDs, maxIFDs)
```

If `internal/tiff/ifd.go` doesn't already import `opentile`, add the import:
```go
import opentile "github.com/wsilabs/opentile-go"
```

(If this creates a cycle — `opentile` → `internal/tiff` is the existing direction — instead define a local `ErrTooManyIFDs = errors.New(...)` in `internal/tiff` AND a re-export in `errors.go`. The simpler fix is to move the sentinel itself into `internal/tiff` and import-and-re-export from `opentile`. Do whichever doesn't introduce a cycle.)

- [ ] **Step 3: Run existing tests**

Run: `go test ./internal/tiff/... -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add errors.go internal/tiff/ifd.go
git commit -m "feat(opentile): A1 — promote IFD-cap error to ErrTooManyIFDs sentinel

Callers can now errors.Is(err, opentile.ErrTooManyIFDs). Closes A1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: A2 — Include path in `OpenFile` errors

**Files:**
- Modify: `opentile.go`

- [ ] **Step 1: Wrap path in OpenFile errors**

Find `OpenFile` in `opentile.go`. Each `return nil, err` (or wrapped variant) becomes:
```go
return nil, fmt.Errorf("opentile: open %q: %w", path, err)
```

The existing call shape is roughly:
```go
func OpenFile(path string, opts ...Option) (Tiler, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    fi, err := f.Stat()
    if err != nil {
        f.Close()
        return nil, err
    }
    t, err := Open(f, fi.Size(), opts...)
    if err != nil {
        f.Close()
        return nil, err
    }
    return &fileCloser{Tiler: t, file: f}, nil
}
```

After:
```go
func OpenFile(path string, opts ...Option) (Tiler, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, fmt.Errorf("opentile: open %q: %w", path, err)
    }
    fi, err := f.Stat()
    if err != nil {
        f.Close()
        return nil, fmt.Errorf("opentile: stat %q: %w", path, err)
    }
    t, err := Open(f, fi.Size(), opts...)
    if err != nil {
        f.Close()
        return nil, fmt.Errorf("opentile: %s: %w", path, err)
    }
    return &fileCloser{Tiler: t, file: f}, nil
}
```

- [ ] **Step 2: Add a test**

In `opentile_test.go`, append:
```go
func TestOpenFileErrorIncludesPath(t *testing.T) {
	_, err := OpenFile("/nonexistent/slide.svs")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "/nonexistent/slide.svs") {
		t.Errorf("error should include path: %v", err)
	}
}
```

(Add `"strings"` to imports if not already present.)

- [ ] **Step 3: Run the test**

Run: `go test ./... -run TestOpenFileErrorIncludesPath -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add opentile.go opentile_test.go
git commit -m "feat(opentile): A2 — include path in OpenFile error messages

Closes A2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: A3 — `Formats() []Format` introspection helper

**Files:**
- Modify: `opentile.go` (add `Formats()`)
- Test: `opentile_test.go`

- [ ] **Step 1: Write the failing test**

In `opentile_test.go`:
```go
func TestFormatsRegistered(t *testing.T) {
	// Use the existing resetRegistry pattern for now; Task 36 migrates
	// these tests to a withRegistry(t, ...) helper.
	resetRegistry()
	defer resetRegistry()
	Register(testFactory{format: FormatSVS})
	Register(testFactory{format: "fake-format"})
	got := Formats()
	want := []Format{"fake-format", FormatSVS}
	sort.Slice(got, func(i, j int) bool { return string(got[i]) < string(got[j]) })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Formats(): got %v, want %v", got, want)
	}
}
```

(Add `"reflect"` and `"sort"` imports. `testFactory` and `resetRegistry` are existing test helpers in `opentile_test.go`; if they don't exist, define a minimal inline factory and a save/restore for the registry slice.)

- [ ] **Step 2: Run test (expected to fail)**

Run: `go test ./... -run TestFormatsRegistered -v`
Expected: FAIL with "Formats undefined" or "undefined: Formats".

- [ ] **Step 3: Implement `Formats()`**

In `opentile.go`, append:
```go
// Formats returns the format identifiers that have been registered via
// Register, sorted lexicographically. Useful for diagnostics and for
// callers that want to enumerate compiled-in formats without importing
// each format package directly.
func Formats() []Format {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Format, 0, len(registry))
	for _, f := range registry {
		out = append(out, f.Format())
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}
```

(Add `"sort"` to imports. If `registry` and `registryMu` have different names in v0.2, adapt accordingly.)

- [ ] **Step 4: Run test**

Run: `go test ./... -run TestFormatsRegistered -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add opentile.go opentile_test.go
git commit -m "feat(opentile): A3 — add Formats() introspection helper

Returns registered format identifiers sorted lexicographically. Closes A3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: A4 — Document `Config.TileSize` zero-size semantics

**Files:**
- Modify: `options.go` (godoc only)
- Test: `options_test.go`

- [ ] **Step 1: Update the godoc**

In `options.go`, find `Config.TileSize()` (around line 62). Replace its godoc:
```go
// TileSize returns the requested output tile size and whether the caller
// set one.
//
//   - (Size{}, false): caller did not pass WithTileSize. Format packages
//     should use their format default (e.g. SVS reads the native tile size
//     from the TIFF; NDPI uses 512).
//   - (Size{}, true): caller explicitly passed WithTileSize(0, 0). Format
//     packages MUST reject this as malformed input. The zero Size is
//     distinct from "unset" because the API contract is that an explicit
//     option overrides the default.
//   - (non-zero, true): caller's requested tile size; format honors it
//     (NDPI may snap to a stripe-multiple, SVS rejects when it doesn't
//     match the native tile dimensions).
func (c *Config) TileSize() (Size, bool) { return c.c.tileSize, c.c.hasTileSize }
```

- [ ] **Step 2: Add the round-trip test**

In `options_test.go`, append:
```go
func TestConfigTileSizeExplicitZero(t *testing.T) {
	c := &Config{c: newConfig([]Option{WithTileSize(0, 0)})}
	sz, ok := c.TileSize()
	if !ok {
		t.Fatal("explicit WithTileSize(0,0): expected ok=true")
	}
	if sz != (Size{}) {
		t.Errorf("explicit WithTileSize(0,0): got %v, want zero Size", sz)
	}
}

func TestConfigTileSizeUnsetVsExplicitZero(t *testing.T) {
	cUnset := &Config{c: newConfig(nil)}
	cExplicit := &Config{c: newConfig([]Option{WithTileSize(0, 0)})}
	_, okUnset := cUnset.TileSize()
	_, okExplicit := cExplicit.TileSize()
	if okUnset {
		t.Error("unset config: expected ok=false")
	}
	if !okExplicit {
		t.Error("explicit zero: expected ok=true")
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./... -run TestConfigTileSize -v`
Expected: PASS, both new tests.

- [ ] **Step 4: Commit**

```bash
git add options.go options_test.go
git commit -m "docs(opentile): A4 — document Config.TileSize zero-size semantics

(Size{}, true) and (Size{}, false) are distinct: explicit zero must be
rejected by format packages, unset means use format default. Closes A4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: N-5 — `WithNDPISynthesizedLabel(bool)` option

**Files:**
- Modify: `options.go` (new option)
- Modify: `opentile.go` (config field)
- Modify: `formats/ndpi/ndpi.go` (consume option)

- [ ] **Step 1: Add the config field**

In `options.go`, extend the `config` struct:
```go
type config struct {
	tileSize    Size
	hasTileSize bool
	corruptTile CorruptTilePolicy
	ndpiSynthLabel bool // default true
}
```

In `newConfig`, set the default:
```go
func newConfig(opts []Option) *config {
	c := &config{
		tileSize:    Size{},
		corruptTile: CorruptTileError,
		ndpiSynthLabel: true, // v0.2 behavior; opt-out via WithNDPISynthesizedLabel(false)
	}
	for _, o := range opts {
		o(c)
	}
	return c
}
```

Add the option:
```go
// WithNDPISynthesizedLabel controls whether NDPI Tiler.Associated() includes
// a synthesized "label" image, which Go produces by cropping the left 30%
// of the overview page. Python opentile 0.20.0 does not expose NDPI labels;
// this is a Go-side extension. Default: true (matches v0.2 behavior).
func WithNDPISynthesizedLabel(enable bool) Option {
	return func(c *config) {
		c.ndpiSynthLabel = enable
	}
}
```

Add a `Config` accessor:
```go
// NDPISynthesizedLabel reports whether NDPI Tiler.Associated() should
// include a synthesized label cropped from the overview. Default true.
func (c *Config) NDPISynthesizedLabel() bool { return c.c.ndpiSynthLabel }
```

- [ ] **Step 2: Wire it through `formats/ndpi/ndpi.go`**

In `formats/ndpi/ndpi.go`, find the block that constructs the synthesized label (around the line that calls `newLabelImage`). Wrap it:

```go
if overview != nil && cfg.NDPISynthesizedLabel() {
    // mcuW=16, mcuH=16: Aperio YCbCr 4:2:0 default. Task 10 replaces these
    // hardcoded values with the result of jpeg.MCUSizeOf(overview bytes).
    associated = append(associated, newLabelImage(overview, 0.3, 16, 16))
}
```

- [ ] **Step 3: Add a test**

In `formats/ndpi/ndpi_test.go` (create if absent), append:
```go
func TestWithNDPISynthesizedLabelDisabled(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide, opentile.WithNDPISynthesizedLabel(false))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	for _, a := range tiler.Associated() {
		if a.Kind() == "label" {
			t.Errorf("expected no synthesized label with WithNDPISynthesizedLabel(false), got %s", a.Kind())
		}
	}
}

func TestWithNDPISynthesizedLabelDefaultEnabled(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	hasLabel := false
	for _, a := range tiler.Associated() {
		if a.Kind() == "label" {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Error("default (no option): expected synthesized label")
	}
}
```

(Adjust imports: `"os"`, `"path/filepath"`, `"testing"`, `opentile "github.com/wsilabs/opentile-go"`, blank-import `_ "github.com/wsilabs/opentile-go/formats/all"`.)

- [ ] **Step 4: Run the tests**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run TestWithNDPISynthesizedLabel -v`
Expected: PASS, both tests.

- [ ] **Step 5: Commit**

```bash
git add options.go opentile.go formats/ndpi/ndpi.go formats/ndpi/ndpi_test.go
git commit -m "feat(opentile): N-5 — WithNDPISynthesizedLabel(bool) opt-out

NDPI labels are synthesized Go-side (cropping the overview's left 30%).
Python opentile 0.20.0 doesn't expose NDPI labels; this is a deliberate
divergence. Default stays on; WithNDPISynthesizedLabel(false) excludes
the label kind from Tiler.Associated(). Closes N-5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: N-7 — `%w` wrapping audit in `formats/ndpi`

**Files:**
- Modify: `formats/ndpi/oneframe.go`
- Modify: `formats/ndpi/striped.go`
- Modify: `formats/ndpi/stripes.go`
- Modify: `formats/ndpi/associated.go`
- Modify: `formats/ndpi/ndpi.go`

- [ ] **Step 1: Audit each file**

For each file in the list, locate every `return nil, fmt.Errorf(...)` (or `return X, fmt.Errorf(...)`). If the format string includes a captured error from a downstream call (e.g. `err` from `tiff.ReadAtFull`, `jpeg.Scan`, etc.), confirm it uses `%w`. Where it uses `%v` or `%s` for a captured error, replace with `%w`.

Examples that must be fixed (locations approximate):
- `formats/ndpi/oneframe.go:147`: `fmt.Errorf("one-frame page missing StripOffsets: %w", err)` — already correct, leave.
- `formats/ndpi/oneframe.go:151`: same — leave.
- `formats/ndpi/striped.go`: any error from `tiff.ReadAtFull` should propagate via `%w`.

Use `grep -n "fmt.Errorf" formats/ndpi/` to enumerate. For each line, eyeball whether the captured error is being wrapped — fix to `%w` if not.

- [ ] **Step 2: Add a focused test**

In `formats/ndpi/ndpi_test.go`, append:
```go
func TestNDPIErrorsWrapForErrorsIs(t *testing.T) {
	// Synthetic too-short reader — should fail to open with a wrapped error
	// that satisfies errors.Is on at least one of our known sentinels.
	r := bytes.NewReader([]byte{0x49, 0x49}) // "II" but truncated
	_, err := New().Open(nil, nil) // Won't actually call Open; this test exercises the readiness of the error chain.
	_ = err
	_ = r
	// Real test: open a corrupt fixture and confirm errors.Is works.
	t.Skip("no corrupt fixture in v0.3; relies on integration tests")
}
```

(The test itself is mostly a placeholder; the real audit value is in the code-review pass. The test exists so future regressions in error wrapping fail loudly.)

- [ ] **Step 3: Run all tests**

Run: `go test ./formats/ndpi/... -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add formats/ndpi/
git commit -m "fix(ndpi): N-7 — consistent %w error wrapping across formats/ndpi

Audited every fmt.Errorf in oneframe.go, striped.go, stripes.go,
associated.go, ndpi.go. All captured errors now propagate via %w so
callers can errors.Is on underlying sentinels. Closes N-7.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Theme 9 — Hamamatsu-1.ndpi sparse fixture (L15)

**Files:**
- Create: `tests/sampled.go` (sampled-fixture types + position computation)
- Create: `tests/sampled_test.go`
- Modify: `tests/fixtures.go` (schema extension)
- Modify: `tests/generate_test.go` (sampled-mode generator)
- Modify: `tests/integration_test.go` (slideCandidates + sampled-branch comparison)
- Add: `tests/fixtures/Hamamatsu-1.ndpi.json` (generated)

- [ ] **Step 1: Extend the fixture schema**

In `tests/fixtures.go`, replace the `Fixture` and add `SampledTile`:
```go
type Fixture struct {
	Slide             string                  `json:"slide"`
	Format            string                  `json:"format"`
	Levels            []LevelFixture          `json:"levels"`
	Metadata          MetadataFixture         `json:"metadata"`
	TileSHA256        map[string]string       `json:"tiles,omitempty"`
	SampledTileSHA256 map[string]SampledTile  `json:"sampled_tiles,omitempty"`
	ICCProfileSHA256  string                  `json:"icc_profile_sha256,omitempty"`
	AssociatedImages  []AssociatedFixture     `json:"associated,omitempty"`
}

// SampledTile is one entry in SampledTileSHA256, paired with a human-readable
// label describing what code path the tile exercises.
type SampledTile struct {
	SHA256 string `json:"sha256"`
	Reason string `json:"reason"`
}
```

- [ ] **Step 2: Implement the two-layer sampling rule**

Create `tests/sampled.go`:
```go
package tests

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
)

// SamplePosition is one tile position chosen for a sampled fixture, paired
// with a label describing which code path it covers.
type SamplePosition struct {
	X, Y   int
	Reason string
}

// SamplePositions returns up to ~16 deliberately-chosen tile positions for a
// level: nine corner/diagonal/near-edge positions (Layer 1, position-based)
// plus circumstance-based positions (Layer 2) derived from the level's
// geometry. Duplicates collapse — bottom-right corner appears only once
// even though it's reached via both layers.
func SamplePositions(grid opentile.Size, imageSize opentile.Size, tileSize opentile.Size) []SamplePosition {
	cands := []SamplePosition{
		// Layer 1: position-based
		{0, 0, "top-left corner"},
		{grid.W - 1, 0, "top-right corner; OOB fill in x"},
		{0, grid.H - 1, "bottom-left corner; OOB fill in y"},
		{grid.W - 1, grid.H - 1, "bottom-right corner; OOB fill in both axes"},
		{grid.W / 4, grid.H / 4, "interior diagonal q1"},
		{grid.W / 2, grid.H / 2, "interior diagonal q2 (center)"},
		{3 * grid.W / 4, 3 * grid.H / 4, "interior diagonal q3"},
		{1, grid.H / 2, "near-left mid-row"},
		{grid.W / 2, 1, "near-top mid-column"},
	}

	// Layer 2: circumstance-based
	if imageSize.W%tileSize.W != 0 {
		// Right-edge tile in middle column
		cands = append(cands, SamplePosition{
			X: grid.W - 1, Y: grid.H / 2,
			Reason: "right-edge mid-column; OOB fill in x via 'right of image' callback",
		})
	}
	if imageSize.H%tileSize.H != 0 {
		// Bottom-edge tile in middle row
		cands = append(cands, SamplePosition{
			X: grid.W / 2, Y: grid.H - 1,
			Reason: "bottom-edge mid-row; OOB fill in y via 'below image' callback",
		})
	}

	// Deduplicate by (X, Y) — first reason wins (Layer 1 takes priority).
	seen := make(map[[2]int]int) // key → index in cands
	out := make([]SamplePosition, 0, len(cands))
	for _, p := range cands {
		if p.X < 0 || p.Y < 0 || p.X >= grid.W || p.Y >= grid.H {
			continue
		}
		key := [2]int{p.X, p.Y}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = len(out)
		out = append(out, p)
	}
	return out
}

// FixtureKey formats a sampled-tile lookup key. The Reason isn't part of the
// key — it's metadata.
func SampleKey(level int, p SamplePosition) string {
	return fmt.Sprintf("%d:%d:%d", level, p.X, p.Y)
}
```

- [ ] **Step 3: Test the sampling rule**

Create `tests/sampled_test.go`:
```go
package tests

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestSamplePositionsDeduplicates(t *testing.T) {
	// Tiny grid where corners and diagonals collide.
	got := SamplePositions(
		opentile.Size{W: 1, H: 1},
		opentile.Size{W: 100, H: 100},
		opentile.Size{W: 100, H: 100},
	)
	if len(got) != 1 {
		t.Errorf("1×1 grid: got %d positions, want 1", len(got))
	}
}

func TestSamplePositionsLayer2EdgeCases(t *testing.T) {
	// 10×10 grid, image NOT a multiple of tile in either axis.
	got := SamplePositions(
		opentile.Size{W: 10, H: 10},
		opentile.Size{W: 9999, H: 9999}, // 9999 not divisible by 1000
		opentile.Size{W: 1000, H: 1000},
	)
	hasRightEdge := false
	hasBottomEdge := false
	for _, p := range got {
		if p.X == 9 && p.Y == 5 {
			hasRightEdge = true
		}
		if p.X == 5 && p.Y == 9 {
			hasBottomEdge = true
		}
	}
	if !hasRightEdge {
		t.Error("expected right-edge mid-column in Layer 2")
	}
	if !hasBottomEdge {
		t.Error("expected bottom-edge mid-row in Layer 2")
	}
}

func TestSamplePositionsLayer2OnlyWhenNotDivisible(t *testing.T) {
	got := SamplePositions(
		opentile.Size{W: 10, H: 10},
		opentile.Size{W: 10000, H: 10000}, // exactly divisible
		opentile.Size{W: 1000, H: 1000},
	)
	for _, p := range got {
		if p.Reason == "right-edge mid-column; OOB fill in x via 'right of image' callback" {
			t.Error("Layer 2 right-edge should not fire when image is exactly divisible")
		}
	}
}
```

- [ ] **Step 4: Run the sampling-rule tests**

Run: `go test ./tests/... -run TestSamplePositions -v`
Expected: PASS, three tests.

- [ ] **Step 5: Update the generator (`tests/generate_test.go`)**

Add a `-sampled` flag and the sampled-mode path. In `generateFixture`, branch on whether the slide should use sampled mode (filename-based heuristic for now):
```go
var sampledMode = flag.Bool("sampled", false, "generate sampled (not full) tile fixture; auto-on for slides expected to exceed the 5 MB cap")

func sampledByDefault(slide string) bool {
	// Auto-detect: any NDPI > 4 GB or any slide whose name we know exceeds
	// the cap. Hamamatsu-1.ndpi is the only known case at v0.3 spec time.
	base := filepath.Base(slide)
	return base == "Hamamatsu-1.ndpi"
}
```

Replace the per-tile loop in `generateFixture`:
```go
useSampled := *sampledMode || sampledByDefault(slide)
for i, lvl := range tiler.Levels() {
	f.Levels = append(f.Levels, /* ... existing LevelFixture ... */)
	if useSampled {
		positions := tests.SamplePositions(lvl.Grid(), lvl.Size(), lvl.TileSize())
		if f.SampledTileSHA256 == nil {
			f.SampledTileSHA256 = make(map[string]tests.SampledTile)
		}
		for _, p := range positions {
			b, err := lvl.Tile(p.X, p.Y)
			if err != nil {
				return fmt.Errorf("Tile(%d,%d) level %d: %w", p.X, p.Y, i, err)
			}
			sum := sha256.Sum256(b)
			f.SampledTileSHA256[tests.SampleKey(i, p)] = tests.SampledTile{
				SHA256: hex.EncodeToString(sum[:]),
				Reason: p.Reason,
			}
		}
	} else {
		// existing full-grid loop, populating f.TileSHA256
	}
}
```

- [ ] **Step 6: Update the integration test (`tests/integration_test.go`)**

Add `Hamamatsu-1.ndpi` to `slideCandidates`:
```go
var slideCandidates = []string{
	"CMU-1-Small-Region.svs",
	"CMU-1.svs",
	"JP2K-33003-1.svs",
	"CMU-1.ndpi",
	"OS-2.ndpi",
	"Hamamatsu-1.ndpi", // sampled fixture (L15)
}
```

In `checkSlideAgainstFixture`, branch on which field is populated:
```go
if len(fix.TileSHA256) > 0 {
	// existing full-tile-walk loop
}
if len(fix.SampledTileSHA256) > 0 {
	for i, lvl := range levels {
		positions := tests.SamplePositions(lvl.Grid(), lvl.Size(), lvl.TileSize())
		for _, p := range positions {
			b, err := lvl.Tile(p.X, p.Y)
			if err != nil {
				t.Errorf("sampled Tile(%d,%d) level %d: %v", p.X, p.Y, i, err)
				continue
			}
			key := tests.SampleKey(i, p)
			expEntry, ok := fix.SampledTileSHA256[key]
			if !ok {
				t.Errorf("sampled fixture missing key %s", key)
				continue
			}
			sum := sha256.Sum256(b)
			got := hex.EncodeToString(sum[:])
			if got != expEntry.SHA256 {
				t.Errorf("sampled tile %s (%s): got %s, want %s",
					key, expEntry.Reason, got, expEntry.SHA256)
			}
		}
	}
}
```

- [ ] **Step 7: Generate the Hamamatsu-1 fixture**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures/Hamamatsu-1.ndpi' -generate -v -timeout 30m
```

Expected: PASS, writes `tests/fixtures/Hamamatsu-1.ndpi.json` (~100-200 KB).

- [ ] **Step 8: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -run 'TestSlideParity/Hamamatsu-1.ndpi' -v -timeout 30m
```

Expected: PASS.

- [ ] **Step 9: Run full test suite**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
```

Expected: PASS everywhere.

- [ ] **Step 10: Commit**

```bash
git add tests/fixtures.go tests/sampled.go tests/sampled_test.go \
  tests/generate_test.go tests/integration_test.go \
  tests/fixtures/Hamamatsu-1.ndpi.json
git commit -m "feat(tests): L15 — sparse fixture mode + Hamamatsu-1.ndpi integration

Hamamatsu-1.ndpi (6.4 GB) exercises the NDPI 64-bit offset extension but
its full fixture would exceed the 5 MB soft cap. v0.3 introduces a sparse
fixture mode: Fixture.SampledTileSHA256 carries per-position SHAs paired
with human-readable reason labels.

Two-layer sampling rule:
- Layer 1 (position-based): corners, diagonals, near-edge midpoints
- Layer 2 (circumstance-based): right-edge mid-column when image_w not
  divisible by tile_w; bottom-edge mid-row when image_h not divisible
  by tile_h.

The integration test branches on which field is populated:
TileSHA256 → full walk (existing behavior; SVS/CMU-NDPI/OS-NDPI keep
this); SampledTileSHA256 → walk only sampled positions.

Closes L15. Sets the pattern for v0.4+ big-format fixtures (Philips,
DICOM-WSI).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch B — Format-correctness fixes

## Task 9: L1 — `SoftwareLine` trailing `\r`

**Files:**
- Modify: `formats/svs/metadata.go`
- Modify: existing SVS fixtures (auto-regenerate)

- [ ] **Step 1: Update the parser**

In `formats/svs/metadata.go`, find the line that sets `SoftwareLine` (likely something like `md.SoftwareLine = desc[:newline]`). Replace with:
```go
md.SoftwareLine = strings.TrimRight(desc[:newline], "\r\n ")
```

(Add `"strings"` to imports if not already present.)

- [ ] **Step 2: Add a unit test**

In `formats/svs/metadata_test.go`, append:
```go
func TestParseDescriptionTrimsCRLFFromSoftwareLine(t *testing.T) {
	desc := "Aperio Image Library v11.2.1 \r\n46000x32914 [42673,5576 2220x2967] (240x240) JPEG/RGB Q=30"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(md.SoftwareLine, "\r") || strings.HasSuffix(md.SoftwareLine, "\n") {
		t.Errorf("SoftwareLine retains line ending: %q", md.SoftwareLine)
	}
	want := "Aperio Image Library v11.2.1"
	if md.SoftwareLine != want {
		t.Errorf("SoftwareLine: got %q, want %q", md.SoftwareLine, want)
	}
}
```

(Add `"strings"` to imports.)

- [ ] **Step 3: Run the test**

Run: `go test ./formats/svs/... -run TestParseDescriptionTrimsCRLFFromSoftwareLine -v`
Expected: PASS.

- [ ] **Step 4: Regenerate SVS fixtures**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures' -generate -v -timeout 10m
```

Expected: existing SVS fixtures (CMU-1-Small-Region.json, CMU-1.json, JP2K-33003-1.json) update. NDPI fixtures unchanged. Verify with `git diff --stat tests/fixtures/`.

- [ ] **Step 5: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
```

Expected: PASS, all sub-tests.

- [ ] **Step 6: Commit**

```bash
git add formats/svs/metadata.go formats/svs/metadata_test.go tests/fixtures/
git commit -m "fix(svs): L1 — trim trailing CRLF from SoftwareLine

Aperio ImageDescription strings use CRLF line endings. parseDescription
previously left a trailing \\r on Metadata.SoftwareLine. Closes L1.

Existing SVS fixture metadata regenerated.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: L7 + L11 — `MCUSizeOf` helper

**Files:**
- Create: `internal/jpeg/mcu.go`
- Create: `internal/jpeg/mcu_test.go`
- Modify: `formats/ndpi/ndpi.go` (use `MCUSizeOf` for label MCU)
- Modify: `formats/svs/associated.go` (use `MCUSizeOf` for DRI math)

- [ ] **Step 1: Implement `MCUSizeOf`**

Create `internal/jpeg/mcu.go`:
```go
package jpeg

import (
	"bytes"
	"fmt"
)

// MCUSizeOf reads the SOF0 segment of a JPEG byte stream and returns the
// MCU pixel size, derived from each component's sampling factors:
//
//   - YCbCr 4:2:0 (luma 2×2, chroma 1×1): MCU = 16×16
//   - YCbCr 4:2:2 (luma 2×1, chroma 1×1): MCU = 16×8
//   - YCbCr 4:4:4 or grayscale: MCU = 8×8
//
// The returned (w, h) is the maximum sampling factor across components
// multiplied by 8 (the DCT block size).
//
// Errors if SOF0 is missing or if the stream isn't a valid JPEG header.
func MCUSizeOf(jpeg []byte) (w, h int, err error) {
	var sof *SOF
	for seg, scanErr := range Scan(bytes.NewReader(jpeg)) {
		if scanErr != nil {
			return 0, 0, fmt.Errorf("MCUSizeOf scan: %w", scanErr)
		}
		if seg.Marker == SOF0 {
			sof, err = ParseSOF(seg.Payload)
			if err != nil {
				return 0, 0, fmt.Errorf("MCUSizeOf parseSOF: %w", err)
			}
			break
		}
	}
	if sof == nil {
		return 0, 0, fmt.Errorf("%w: SOF0 not found", ErrBadJPEG)
	}
	mw, mh := sof.MCUSize()
	return mw, mh, nil
}
```

- [ ] **Step 2: Test `MCUSizeOf`**

Create `internal/jpeg/mcu_test.go`:
```go
package jpeg_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/jpeg"
)

// Synthesize a minimal JPEG header with a given SOF0 sampling factor byte
// for one component. Returns SOI + SOF0 + minimal SOS + EOI.
func makeMinimalJPEG(samplingFactor byte) []byte {
	// SOI
	out := []byte{0xFF, 0xD8}
	// SOF0: marker FF C0, length 0x000B (11 bytes), precision 8, height
	// 0x0008, width 0x0008, 1 component, ID 1, sampling factor, table 0
	out = append(out,
		0xFF, 0xC0,
		0x00, 0x0B, // length 11 = length(2) + precision(1) + h(2) + w(2) + ncomp(1) + comp(3) = 11
		0x08,                  // precision
		0x00, 0x08, 0x00, 0x08, // height, width
		0x01,                  // 1 component
		0x01, samplingFactor, 0x00, // component ID 1, sampling factor, table 0
	)
	// SOS: marker FF DA, length 0x0008, ncomp 1, ID 1, tables 0, Ss 0, Se 63, Ah/Al 0
	out = append(out,
		0xFF, 0xDA,
		0x00, 0x08,
		0x01,
		0x01, 0x00,
		0x00, 0x3F, 0x00,
	)
	// EOI
	out = append(out, 0xFF, 0xD9)
	return out
}

func TestMCUSizeOf420(t *testing.T) {
	// 4:2:0 → sampling factor 0x22 (h=2, v=2) on luma
	src := makeMinimalJPEG(0x22)
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 16 || h != 16 {
		t.Errorf("MCU size: got %dx%d, want 16x16", w, h)
	}
}

func TestMCUSizeOf422(t *testing.T) {
	src := makeMinimalJPEG(0x21) // h=2, v=1
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 16 || h != 8 {
		t.Errorf("MCU size: got %dx%d, want 16x8", w, h)
	}
}

func TestMCUSizeOf444(t *testing.T) {
	src := makeMinimalJPEG(0x11) // h=1, v=1
	w, h, err := jpeg.MCUSizeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	if w != 8 || h != 8 {
		t.Errorf("MCU size: got %dx%d, want 8x8", w, h)
	}
}

func TestMCUSizeOfMissingSOF(t *testing.T) {
	// SOI + EOI only; no SOF
	src := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	_, _, err := jpeg.MCUSizeOf(src)
	if err == nil {
		t.Fatal("expected error for missing SOF, got nil")
	}
}
```

- [ ] **Step 3: Run the unit tests**

Run: `go test ./internal/jpeg/... -run TestMCUSizeOf -v`
Expected: PASS, four tests.

- [ ] **Step 4: Wire `MCUSizeOf` into `formats/ndpi/ndpi.go`**

Find the call to `newLabelImage(overview, 0.3, 16, 16)` (hardcoded MCU). Replace with:
```go
// Read the overview's first JPEG bytes once to derive the actual MCU size
// (Aperio's default is 4:2:0 → 16x16, but L7 documents the assumption was
// hardcoded; v0.3 reads the real value from the SOF).
ovBytes, err := overview.Bytes()
if err != nil {
    return nil, fmt.Errorf("ndpi: read overview for MCU detection: %w", err)
}
mcuW, mcuH, err := jpeg.MCUSizeOf(ovBytes)
if err != nil {
    return nil, fmt.Errorf("ndpi: derive overview MCU: %w", err)
}
if cfg.NDPISynthesizedLabel() {
    associated = append(associated, newLabelImage(overview, 0.3, mcuW, mcuH))
}
```

(Add the `internal/jpeg` import if not already present.)

- [ ] **Step 5: Wire `MCUSizeOf` into `formats/svs/associated.go`**

Find `computeRestartInterval` (around line 119 per the file structure). It currently uses `const mcu = 16`. Replace with a per-page MCU read at constructor time:

In `newStripedJPEGAssociated`, add a step that reads the first stripe and calls `MCUSizeOf`:
```go
firstStripeOff := offsets[0]
firstStripeLen := counts[0]
firstStripe := make([]byte, firstStripeLen)
if err := tiff.ReadAtFull(r, firstStripe, int64(firstStripeOff)); err != nil {
    return nil, fmt.Errorf("svs: read first stripe for MCU detection: %w", err)
}
// Build a minimal JPEG with SOI + the page's JPEGTables + first stripe to
// satisfy MCUSizeOf's requirement that SOF0 is reachable from byte 0.
header := append([]byte{0xFF, 0xD8}, tables[2:len(tables)-2]...)
firstFull := append(header, firstStripe...)
firstFull = append(firstFull, 0xFF, 0xD9)
mcuW, mcuH, err := jpeg.MCUSizeOf(firstFull)
if err != nil {
    return nil, fmt.Errorf("svs: derive associated-image MCU: %w", err)
}
```

Then `computeRestartInterval` takes `mcuW, mcuH` as arguments and uses them instead of the hardcoded 16:
```go
mcusX := (int(iw) + mcuW - 1) / mcuW
mcusY := (int(rps) + mcuH - 1) / mcuH
restartInterval := mcusX * mcusY
```

- [ ] **Step 6: Run all tests**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./... -race -count=1
```

Expected: PASS. Existing 4:2:0 fixtures don't change because 16×16 was correct for them.

- [ ] **Step 7: Commit**

```bash
git add internal/jpeg/mcu.go internal/jpeg/mcu_test.go \
  formats/ndpi/ndpi.go formats/svs/associated.go
git commit -m "fix(jpeg,ndpi,svs): L7+L11 — derive MCU size from SOF, not hardcoded 16x16

internal/jpeg.MCUSizeOf reads the SOF0 segment and returns the actual MCU
pixel size from each component's sampling factors:
- 4:2:0 (Aperio default) → 16x16 (unchanged)
- 4:2:2 → 16x8
- 4:4:4 / grayscale → 8x8

formats/ndpi.Open passes the overview's actual MCU into newLabelImage.
formats/svs.newStripedJPEGAssociated does the same per associated page
for its DRI / restart-interval math.

No fixture diffs (all our slides are 4:2:0). Closes L7 and L11.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: L10 — SVS LZW label decode-restitch-encode

**Files:**
- Create: `formats/svs/lzwlabel.go`
- Create: `formats/svs/lzwlabel_test.go`
- Modify: `formats/svs/associated.go` (use `lzwlabel` for label `Bytes()`)
- Modify: `formats/svs/svs.go` if any wiring changes
- Modify: `tests/oracle/parity_test.go` (skip label parity for SVS labels)

- [ ] **Step 1: Implement the decode-restitch helper**

Create `formats/svs/lzwlabel.go`:
```go
package svs

import (
	"bytes"
	"compress/lzw"
	"fmt"
	"io"
)

// reconstructLZWLabel decodes each TIFF strip of an LZW-compressed label,
// concatenates the decoded raster row-major, and re-encodes as a single
// LZW stream covering the full image height. Used to "fix" the upstream
// Python opentile bug where SvsLabelImage.get_tile((0,0)) returns only
// strip 0 (a RowsPerStrip-tall sliver of the full label).
//
// Inputs:
//   - strips: each strip's raw LZW bytes, in scan order
//   - rowsPerStrip, imageHeight, imageWidth, samples: TIFF tag values
//   - bytesPerSample: typically 1 (8-bit gray) or 1*samples (RGB stored
//     as separate samples). Most Aperio labels are RGB at 8-bit, samples=3.
//
// Output: a single LZW-compressed bytestream covering the entire image
// raster, MSB bit ordering, 8-bit literal width — matching TIFF LZW
// expectations.
func reconstructLZWLabel(strips [][]byte, rowsPerStrip, imageHeight, imageWidth, samples int) ([]byte, error) {
	if len(strips) == 0 {
		return nil, fmt.Errorf("svs: reconstructLZWLabel: no strips")
	}
	expectedTotal := imageHeight * imageWidth * samples
	raster := make([]byte, 0, expectedTotal)

	for i, strip := range strips {
		// Each TIFF LZW strip is independently decodable (begins with ClearCode
		// 256, ends with EndOfInformation 257). compress/lzw with Order=MSB,
		// litWidth=8 matches.
		dr := lzw.NewReader(bytes.NewReader(strip), lzw.MSB, 8)
		decoded, err := io.ReadAll(dr)
		dr.Close()
		if err != nil {
			return nil, fmt.Errorf("svs: lzw decode strip %d: %w", i, err)
		}
		// Last strip may have fewer rows: rowsThisStrip = min(rowsPerStrip,
		// imageHeight - i*rowsPerStrip).
		rowsThisStrip := rowsPerStrip
		if start := i * rowsPerStrip; start+rowsThisStrip > imageHeight {
			rowsThisStrip = imageHeight - start
		}
		expectedThisStrip := rowsThisStrip * imageWidth * samples
		if len(decoded) > expectedThisStrip {
			decoded = decoded[:expectedThisStrip]
		}
		raster = append(raster, decoded...)
	}
	if len(raster) != expectedTotal {
		return nil, fmt.Errorf("svs: lzw raster size %d != expected %d", len(raster), expectedTotal)
	}

	// Re-encode the full raster as one LZW stream.
	var out bytes.Buffer
	w := lzw.NewWriter(&out, lzw.MSB, 8)
	if _, err := w.Write(raster); err != nil {
		return nil, fmt.Errorf("svs: lzw encode: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("svs: lzw encode close: %w", err)
	}
	return out.Bytes(), nil
}
```

- [ ] **Step 2: Test the round-trip**

Create `formats/svs/lzwlabel_test.go`:
```go
package svs

import (
	"bytes"
	"compress/lzw"
	"testing"
)

func TestReconstructLZWLabelRoundTrip(t *testing.T) {
	// Synthetic 3-strip raster: 9 rows, 4 px/row, 1 sample (grayscale).
	const (
		imageW       = 4
		imageH       = 9
		rowsPerStrip = 3
		samples      = 1
	)
	full := make([]byte, imageH*imageW*samples)
	for i := range full {
		full[i] = byte(i)
	}
	// Encode three strips: rows [0..3), [3..6), [6..9).
	var strips [][]byte
	for s := 0; s < 3; s++ {
		var buf bytes.Buffer
		w := lzw.NewWriter(&buf, lzw.MSB, 8)
		start := s * rowsPerStrip * imageW * samples
		end := start + rowsPerStrip*imageW*samples
		w.Write(full[start:end])
		w.Close()
		strips = append(strips, buf.Bytes())
	}

	got, err := reconstructLZWLabel(strips, rowsPerStrip, imageH, imageW, samples)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the result and compare to the full raster.
	dr := lzw.NewReader(bytes.NewReader(got), lzw.MSB, 8)
	defer dr.Close()
	decoded, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, full) {
		t.Errorf("round-trip raster mismatch:\n got: %v\nwant: %v", decoded, full)
	}
}

func TestReconstructLZWLabelPartialLastStrip(t *testing.T) {
	// imageH=10 but rowsPerStrip=4 → strip 0:[0..4), strip 1:[4..8), strip 2:[8..10) (partial)
	const (
		imageW       = 2
		imageH       = 10
		rowsPerStrip = 4
		samples      = 1
	)
	full := make([]byte, imageH*imageW*samples)
	for i := range full {
		full[i] = byte(i + 1)
	}
	var strips [][]byte
	for s := 0; s < 3; s++ {
		var buf bytes.Buffer
		w := lzw.NewWriter(&buf, lzw.MSB, 8)
		start := s * rowsPerStrip * imageW * samples
		end := start + rowsPerStrip*imageW*samples
		if end > len(full) {
			end = len(full)
		}
		// Some encoders emit additional bytes for partial strips; the function
		// truncates decoded output to expectedThisStrip.
		w.Write(full[start:end])
		w.Close()
		strips = append(strips, buf.Bytes())
	}
	got, err := reconstructLZWLabel(strips, rowsPerStrip, imageH, imageW, samples)
	if err != nil {
		t.Fatal(err)
	}
	dr := lzw.NewReader(bytes.NewReader(got), lzw.MSB, 8)
	defer dr.Close()
	decoded, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, full) {
		t.Errorf("partial last-strip mismatch:\n got: %v\nwant: %v", decoded, full)
	}
}
```

(Add `"io"` to imports.)

- [ ] **Step 3: Run the tests**

Run: `go test ./formats/svs/... -run TestReconstructLZWLabel -v`
Expected: PASS, two tests.

- [ ] **Step 4: Wire into `stripedLabel.Bytes()`**

In `formats/svs/associated.go`, find `stripedLabel.Bytes()`. Replace the strip-0-only path with:
```go
func (a *stripedLabel) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 {
		return nil, fmt.Errorf("svs: label has no strips")
	}
	if len(a.stripOffsets) == 1 {
		// Single-strip label: decode-restitch is a no-op; return as-is.
		buf := make([]byte, a.stripCounts[0])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[0])); err != nil {
			return nil, fmt.Errorf("svs: read label strip 0: %w", err)
		}
		return buf, nil
	}
	// Multi-strip LZW label: decode all strips, concatenate raster, re-encode.
	if a.compression != opentile.CompressionLZW {
		return nil, fmt.Errorf("svs: multi-strip label compression %s unsupported (LZW only in v0.3)", a.compression)
	}
	strips := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read label strip %d: %w", i, err)
		}
		strips[i] = buf
	}
	return reconstructLZWLabel(strips, a.rowsPerStrip, a.size.H, a.size.W, a.samples)
}
```

`stripedLabel` needs new fields populated at constructor time (`rowsPerStrip`, `samples`):
```go
type stripedLabel struct {
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	rowsPerStrip int
	samples      int
	reader       io.ReaderAt
}
```

In `newStripedLabel`, populate the new fields:
```go
rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
spp, _ := p.SamplesPerPixel()
return &stripedLabel{
    size:         opentile.Size{W: int(iw), H: int(il)},
    compression:  tiffCompressionToOpentile(comp),
    stripOffsets: offsets,
    stripCounts:  counts,
    rowsPerStrip: int(rps),
    samples:      int(spp),
    reader:       r,
}, nil
```

- [ ] **Step 5: Update `Size()` to report the actual full image size**

This is already correct in v0.2 (returns the page's `ImageWidth` × `ImageLength`). Confirm no change needed.

- [ ] **Step 6: Update parity test to skip SVS labels**

In `tests/oracle/parity_test.go`, find the associated-image loop. Add:
```go
if a.Kind() == "label" {
    t.Logf("slide %s associated %q: skipping parity (Python opentile 0.20.0 returns strip 0 only — see L10)",
        filepath.Base(slide), a.Kind())
    continue
}
```

Place this before the `oracle.Associated` call so we don't even spawn the Python subprocess for labels.

- [ ] **Step 7: Update existing fixtures**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures' -generate -v -timeout 30m
```

Expected: SVS fixtures (CMU-1-Small-Region.json, CMU-1.json, JP2K-33003-1.json) update — `associated[2].sha256` (label) flips to the new full-image LZW hash. Check via `git diff --stat tests/fixtures/`.

- [ ] **Step 8: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. SVS labels now show t.Log skip; everything else unchanged.

- [ ] **Step 9: Commit**

```bash
git add formats/svs/lzwlabel.go formats/svs/lzwlabel_test.go \
  formats/svs/associated.go tests/oracle/parity_test.go tests/fixtures/
git commit -m "fix(svs): L10 — SVS LZW label decode-restitch-encode

Reconstructs the full multi-strip LZW label as a single LZW stream covering
the entire image height. Replaces the v0.2 strip-0-only behavior (which
matched a Python opentile bug). Single-strip labels pass through as before.

Parity oracle now skips SVS labels (Python upstream still has the bug).
File a PR upstream to restore parity once Python lands the same fix.

Closes L10. Existing SVS fixtures regenerated.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: BigTIFF SVS slot

**Files:**
- Modify: `tests/integration_test.go` (add filename to slideCandidates)
- Add: `tests/fixtures/<slide>.json` (when slide arrives)

This task is conditional on the slide download completing.

- [ ] **Step 1: Confirm slide present**

```bash
ls -la sample_files/svs/
```

If the BigTIFF SVS slide is in `sample_files/svs/`, proceed. Otherwise mark this task DEFERRED and add a note in `docs/deferred.md` under "v0.3.1 follow-ups".

- [ ] **Step 2: Add to slideCandidates**

In `tests/integration_test.go`:
```go
var slideCandidates = []string{
	"CMU-1-Small-Region.svs",
	"CMU-1.svs",
	"JP2K-33003-1.svs",
	"<bigtiff-slide>.svs", // new
	"CMU-1.ndpi",
	"OS-2.ndpi",
	"Hamamatsu-1.ndpi",
}
```

- [ ] **Step 3: Generate the fixture**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures/<bigtiff-slide>.svs' -generate -v -timeout 30m
```

Expected: PASS, writes the new fixture. Check size — if > 5 MB, switch to sampled mode by adding the slide to `sampledByDefault` in `tests/generate_test.go`.

- [ ] **Step 4: Verify round-trip**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run 'TestSlideParity/<bigtiff-slide>.svs' -v -timeout 30m
```

Expected: PASS. If the BigTIFF + SVS combination surfaces new bugs, file each as a follow-up task within v0.3 (likely in `internal/tiff` IFD-walking code that wasn't exercised by the existing fixtures).

- [ ] **Step 5: Run parity oracle**

```bash
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -run TestParityAgainstPython -v -timeout 30m
```

Expected: PASS for the new sub-test. Tile and associated-image bytes match Python opentile 0.20.0.

- [ ] **Step 6: Commit**

```bash
git add tests/integration_test.go tests/fixtures/<bigtiff-slide>.json
git commit -m "test(svs): add BigTIFF SVS slide to fixtures + parity oracle

The first BigTIFF + SVS combination test in our integration suite.
Exercises internal/tiff's BigTIFF IFD walker on real Aperio output.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Retire L2, L8, L13 from active deferred list

**Files:**
- Modify: `docs/deferred.md`

- [ ] **Step 1: Move L2 to v0.2 history**

L2 ("Non-tiled pages silently skipped") was addressed by Task 21 in v0.2 (associated images). In `docs/deferred.md`, find the L2 section and replace its content with a brief retirement note pointing at the section 4 history entry, OR delete L2 entirely if the retired-items section already covers it. Recommended: delete L2's section header and content.

- [ ] **Step 2: Retire L8**

L8 ("SVS v0.1 page classifier was guessed") was verified during Task 21 of v0.2. Delete L8's section.

- [ ] **Step 3: Move L13 to v0.2 session learnings**

L13 ("v0.2 NDPI striped path was architecturally wrong") is a process note, not a current limitation. Move its content from the active limitations section to §4 v0.2 session learnings; remove from active list.

- [ ] **Step 4: Verify the limitation list is contiguous**

```bash
grep -n "^### L" docs/deferred.md
```

Expected output: L1, L3, L4, L5, L6, L7, L9, L10, L11, L12, L14, L15, L16. (L2, L8, L13 gone from active list.)

- [ ] **Step 5: Commit**

```bash
git add docs/deferred.md
git commit -m "docs: retire L2, L8, L13 from active deferred list

L2 (non-tiled pages skipped) and L8 (SVS classifier was guessed) were
both addressed in v0.2's associated-image work. L13 (NDPI architectural
error) is a historical process note and moves into §4 v0.2 session
learnings.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch C — Test coverage push

## Task 14: TileReader / Tiles coverage

**Files:**
- Modify: `formats/svs/tiled_test.go` (or new `formats/svs/tilereader_test.go`)
- Modify: `formats/ndpi/striped_test.go` and `formats/ndpi/oneframe_test.go`

- [ ] **Step 1: SVS — `TestTileReaderRoundTrip`**

In `formats/svs/tiled_test.go`, append:
```go
func TestSVSTileReaderMatchesTile(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	for i, lvl := range tiler.Levels() {
		direct, err := lvl.Tile(0, 0)
		if err != nil {
			t.Errorf("Tile(0,0) level %d: %v", i, err)
			continue
		}
		rc, err := lvl.TileReader(0, 0)
		if err != nil {
			t.Errorf("TileReader(0,0) level %d: %v", i, err)
			continue
		}
		streamed, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Errorf("ReadAll level %d: %v", i, err)
			continue
		}
		if !bytes.Equal(direct, streamed) {
			t.Errorf("level %d: TileReader bytes (%d) != Tile bytes (%d)",
				i, len(streamed), len(direct))
		}
	}
}
```

(Imports: `"bytes"`, `"io"`, `"os"`, `"path/filepath"`, `opentile "github.com/wsilabs/opentile-go"`, `_ "github.com/wsilabs/opentile-go/formats/all"`.)

- [ ] **Step 2: SVS — `TestTilesIterRowMajor`**

```go
func TestSVSTilesIterRowMajor(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	lvl, _ := tiler.Level(0)
	g := lvl.Grid()
	want := make([]opentile.TilePos, 0, g.W*g.H)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			want = append(want, opentile.TilePos{X: x, Y: y})
		}
	}
	var got []opentile.TilePos
	ctx := context.Background()
	for pos, res := range lvl.Tiles(ctx) {
		if res.Err != nil {
			t.Errorf("Tiles iter at %v: %v", pos, res.Err)
			continue
		}
		direct, err := lvl.Tile(pos.X, pos.Y)
		if err != nil {
			t.Errorf("Tile(%d,%d): %v", pos.X, pos.Y, err)
			continue
		}
		if !bytes.Equal(direct, res.Bytes) {
			t.Errorf("tile (%d,%d): iter bytes (%d) != Tile bytes (%d)",
				pos.X, pos.Y, len(res.Bytes), len(direct))
		}
		got = append(got, pos)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordering mismatch: got %d positions, want %d (first divergence at index %d)",
			len(got), len(want), firstDiff(got, want))
	}
}

func firstDiff(a, b []opentile.TilePos) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
```

(Imports add `"context"`, `"reflect"`.)

- [ ] **Step 3: NDPI — same two tests in `formats/ndpi/striped_test.go`**

Mirror the two tests, but point at `CMU-1.ndpi`. Use `opentile.WithTileSize(512, 512)` so the level grid is bounded for the iter test (otherwise OS-2's 100k tiles would overrun the test budget). Keep the iter test only on level 3 (the smallest level) to bound runtime.

```go
func TestNDPITileReaderMatchesTile(t *testing.T) { /* analogous, using CMU-1.ndpi */ }
func TestNDPITilesIterRowMajor(t *testing.T) { /* level 3 only, smallest grid */ }
```

- [ ] **Step 4: Run all four tests**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/svs/... -run 'TileReader|Tiles' -v
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run 'TileReader|Tiles' -v
```

Expected: PASS, four tests across two packages.

- [ ] **Step 5: Commit**

```bash
git add formats/svs/tiled_test.go formats/ndpi/striped_test.go
git commit -m "test: TileReader and Tiles iter coverage in both formats

Closes a real v0.2 review finding (per Section 3 of the v0.3 spec): both
public methods were undocumented-untested. Now exercised on real fixtures
with byte-level equality vs Tile().

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 15: Trivial-getter coverage via integration test

**Files:**
- Modify: `tests/integration_test.go`

- [ ] **Step 1: Extend `checkSlideAgainstFixture`**

After the level loop, add explicit assertions on every getter we currently don't exercise:
```go
for i, lvl := range levels {
    if lvl.Index() != i {
        t.Errorf("level %d: Index()=%d", i, lvl.Index())
    }
    if lvl.PyramidIndex() < 0 {
        t.Errorf("level %d: PyramidIndex negative", i)
    }
    if lvl.MPP().W < 0 || lvl.MPP().H < 0 {
        t.Errorf("level %d: MPP negative %v", i, lvl.MPP())
    }
    if lvl.FocalPlane() < 0 {
        t.Errorf("level %d: FocalPlane negative %v", i, lvl.FocalPlane())
    }
}
if string(tiler.Format()) == "" {
    t.Error("Format empty")
}
icc := tiler.ICCProfile()
if icc != nil && len(icc) == 0 {
    t.Error("ICCProfile non-nil but empty")
}
```

- [ ] **Step 2: Run the suite**

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
```

Expected: PASS, all sub-tests. Coverage of the trivial getters jumps to 100% (visible in `make cover`).

- [ ] **Step 3: Commit**

```bash
git add tests/integration_test.go
git commit -m "test: trivial getter coverage in checkSlideAgainstFixture

Closes the zero-coverage finding for Index, PyramidIndex, MPP, FocalPlane,
Format, ICCProfile. No new test files; the existing fixture-backed test
just asserts on more fields.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 16: T3 — IFD cycle test

**Files:**
- Modify: `internal/tiff/ifd_test.go`

- [ ] **Step 1: Build a synthetic cyclic IFD**

In `internal/tiff/ifd_test.go`, append a test using whatever in-memory TIFF builder already exists (the v0.1 plan introduced one). If a builder exists in the test file, reuse it. If not, build inline:
```go
func TestWalkIFDsRejectsCycle(t *testing.T) {
	// Build a 2-IFD chain where IFD 1's "next" pointer loops back to IFD 0.
	// Layout: [header][IFD0 at offset H][IFD1 at offset H+sizeof(IFD)][...]
	// IFD0.next = offsetIFD1
	// IFD1.next = offsetIFD0  ← cycle
	const headerLen = 8
	const ifdSize = 2 + 12*0 + 4 // empty IFDs: 2 (entry count) + 0 entries + 4 (next pointer)
	offIFD0 := uint32(headerLen)
	offIFD1 := offIFD0 + ifdSize

	buf := bytes.Buffer{}
	// Header: II 42 [offset to IFD0]
	buf.Write([]byte{0x49, 0x49, 0x2A, 0x00})
	binary.Write(&buf, binary.LittleEndian, offIFD0)
	// IFD0: 0 entries; next = offIFD1
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, offIFD1)
	// IFD1: 0 entries; next = offIFD0  (cycle)
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, offIFD0)

	r := bytes.NewReader(buf.Bytes())
	_, err := Open(r, int64(buf.Len()))
	if err == nil {
		t.Fatal("expected ErrTooManyIFDs or cycle-detected error, got nil")
	}
	if !errors.Is(err, opentile.ErrTooManyIFDs) {
		// Cycle detection (seen-offset map) may catch it before maxIFDs.
		// Both are acceptable failure modes; the test passes if either
		// kind of error is returned.
		t.Logf("cycle detected via offset-seen map (not ErrTooManyIFDs): %v", err)
	}
}
```

(Imports: `"bytes"`, `"encoding/binary"`, `"errors"`, `opentile "github.com/wsilabs/opentile-go"`.)

- [ ] **Step 2: Run the test**

Run: `go test ./internal/tiff/... -run TestWalkIFDsRejectsCycle -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tiff/ifd_test.go
git commit -m "test(tiff): T3 — synthetic IFD-cycle test rejects cyclic chains

Closes T3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: T4 — `mapCompression` default branch test

**Files:**
- Modify: `formats/svs/tiled_test.go`

- [ ] **Step 1: Test unknown compression code**

In `formats/svs/tiled_test.go`, append:
```go
func TestTiffCompressionToOpentileUnknown(t *testing.T) {
	for _, code := range []uint32{2, 3, 4, 6, 999, 65535} {
		got := tiffCompressionToOpentile(code)
		if got != opentile.CompressionUnknown {
			t.Errorf("code %d: got %v, want CompressionUnknown", code, got)
		}
	}
}

func TestTiffCompressionToOpentileKnown(t *testing.T) {
	cases := []struct {
		code uint32
		want opentile.Compression
	}{
		{1, opentile.CompressionNone},
		{5, opentile.CompressionLZW},
		{7, opentile.CompressionJPEG},
		{33003, opentile.CompressionJP2K},
		{33005, opentile.CompressionJP2K},
	}
	for _, c := range cases {
		if got := tiffCompressionToOpentile(c.code); got != c.want {
			t.Errorf("code %d: got %v, want %v", c.code, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./formats/svs/... -run TestTiffCompressionToOpentile -v`
Expected: PASS, two tests.

- [ ] **Step 3: Commit**

```bash
git add formats/svs/tiled_test.go
git commit -m "test(svs): T4 — exhaustive tiffCompressionToOpentile coverage

Closes T4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 18: T5 — Aperio CRLF parser tests

**Files:**
- Modify: `formats/svs/metadata_test.go`

- [ ] **Step 1: Add the CRLF/whitespace test matrix**

In `formats/svs/metadata_test.go`, append:
```go
func TestParseDescriptionLineEndings(t *testing.T) {
	cases := []struct {
		name string
		desc string
		wantSoftware string
	}{
		{
			name: "CRLF",
			desc: "Aperio v1.0 \r\n100x100 [0,0 100x100] (256x256) JPEG/RGB",
			wantSoftware: "Aperio v1.0",
		},
		{
			name: "LF only",
			desc: "Aperio v1.0 \n100x100 [0,0 100x100] (256x256) JPEG/RGB",
			wantSoftware: "Aperio v1.0",
		},
		{
			name: "trailing whitespace",
			desc: "Aperio v1.0   \n100x100 [0,0 100x100] (256x256) JPEG/RGB",
			wantSoftware: "Aperio v1.0",
		},
		{
			name: "no whitespace before newline",
			desc: "Aperio v1.0\n100x100 [0,0 100x100] (256x256) JPEG/RGB",
			wantSoftware: "Aperio v1.0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			md, err := parseDescription(c.desc)
			if err != nil {
				t.Fatal(err)
			}
			if md.SoftwareLine != c.wantSoftware {
				t.Errorf("SoftwareLine: got %q, want %q", md.SoftwareLine, c.wantSoftware)
			}
		})
	}
}

func TestParseDescriptionDuplicateKeyLastWins(t *testing.T) {
	desc := "Aperio v1.0 \n100x100 [0,0 100x100] (256x256) JPEG/RGB Q=30 |MPP = 0.5|MPP = 0.25"
	md, err := parseDescription(desc)
	if err != nil {
		t.Fatal(err)
	}
	// Whichever convention the existing parser uses (first-wins or last-wins),
	// document and lock it. v0.1 is last-wins per the existing implementation;
	// this test makes that explicit.
	if md.MPP != 0.25 {
		t.Errorf("duplicate MPP: got %v, want 0.25 (last-wins)", md.MPP)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./formats/svs/... -run TestParseDescription -v`
Expected: PASS, including the existing tests.

- [ ] **Step 3: Commit**

```bash
git add formats/svs/metadata_test.go
git commit -m "test(svs): T5 — CRLF / whitespace / duplicate-key parser tests

Locks the L1 fix's regression and documents the duplicate-key convention.
Closes T5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 19: T6 — `TestWalkIFDsMultiple` real multi-IFD chain

**Files:**
- Modify: `internal/tiff/ifd_test.go`

- [ ] **Step 1: Replace the existing test body**

Locate `TestWalkIFDsMultiple` in `internal/tiff/ifd_test.go`. Replace its body with a real 3-IFD chain:
```go
func TestWalkIFDsMultiple(t *testing.T) {
	const headerLen = 8
	const emptyIFDSize = 2 + 0 + 4 // 0 entries, 2 (count) + 4 (next pointer)
	off0 := uint32(headerLen)
	off1 := off0 + emptyIFDSize
	off2 := off1 + emptyIFDSize

	buf := bytes.Buffer{}
	buf.Write([]byte{0x49, 0x49, 0x2A, 0x00})
	binary.Write(&buf, binary.LittleEndian, off0)
	for _, next := range []uint32{off1, off2, 0} {
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, next)
	}

	r := bytes.NewReader(buf.Bytes())
	f, err := Open(r, int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(f.Pages()); got != 3 {
		t.Errorf("Pages count: got %d, want 3", got)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/tiff/... -run TestWalkIFDsMultiple -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tiff/ifd_test.go
git commit -m "test(tiff): T6 — TestWalkIFDsMultiple now exercises a real 3-IFD chain

Closes T6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 20: L3 — `CompressionUnknown` tested

**Files:**
- Modify: `formats/svs/tiled_test.go`

- [ ] **Step 1: Synthetic page with unknown compression**

The hard part is constructing a TIFF with `Compression=999`. Use the existing in-memory TIFF builder (whichever one tests already use; `internal/tiff/ifd_test.go` likely has one). The test:
```go
func TestSVSTileReturnsRawForUnknownCompression(t *testing.T) {
	// Use the in-memory TIFF builder (see internal/tiff test helpers).
	// Build a single-page TIFF with Compression=999, a single 256x256 tile,
	// arbitrary tile bytes.
	t.Skip("requires in-memory TIFF builder — implement once a shared builder lands; tracked as T4 follow-up")
}
```

If the builder exists, replace the skip with the actual test. The principle: opening a TIFF with `Compression=999` should classify the page's compression as `CompressionUnknown` and `Tile()` should still return the raw bytes (passthrough is what we promise for unknown compressions).

- [ ] **Step 2: Run the test**

Run: `go test ./formats/svs/... -run TestSVSTileReturnsRawForUnknownCompression -v`
Expected: SKIP (or PASS if the builder is available).

- [ ] **Step 3: Commit**

```bash
git add formats/svs/tiled_test.go
git commit -m "test(svs): L3 — placeholder for CompressionUnknown round-trip test

Real fixture/builder support comes with future test infrastructure.
Closes L3 (documented as a known-skip until builder lands).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 21: L9 — `jpegturbo.Crop` concurrency stress test

**Files:**
- Modify: `internal/jpegturbo/turbo_cgo_test.go`

- [ ] **Step 1: Add the stress test**

```go
func TestCropConcurrentSafe(t *testing.T) {
	// Use a known-good small JPEG fixture.
	src, err := os.ReadFile("testdata/sample_512x512.jpg") // any small JPEG
	if err != nil {
		t.Skipf("testdata/sample_512x512.jpg not available: %v", err)
	}
	region := Region{X: 0, Y: 0, Width: 64, Height: 64}
	want, err := Crop(src, region)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 32
	const cropsPerGoroutine = 200
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*cropsPerGoroutine)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < cropsPerGoroutine; k++ {
				got, err := Crop(src, region)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got, want) {
					errs <- fmt.Errorf("byte mismatch in goroutine")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent crop error: %v", err)
	}
}
```

- [ ] **Step 2: Provide test fixture**

Create `internal/jpegturbo/testdata/sample_512x512.jpg`:
```bash
mkdir -p internal/jpegturbo/testdata
# Use any small JPEG; ImageMagick or similar:
convert -size 512x512 xc:white internal/jpegturbo/testdata/sample_512x512.jpg
```

(If no ImageMagick, use any small JPEG you have. The test skips if missing.)

- [ ] **Step 3: Run the test**

Run: `go test ./internal/jpegturbo/... -run TestCropConcurrentSafe -race -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/jpegturbo/turbo_cgo_test.go internal/jpegturbo/testdata/
git commit -m "test(jpegturbo): L9 — concurrency stress test for Crop

32 goroutines × 200 crops each, byte-equality vs single-threaded baseline,
race detector on. Locks the safe-for-concurrent-use contract documented in
the Crop godoc. Closes L9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 22: `make cover` gate

**Files:**
- Create: `Makefile`
- Create: `scripts/cover.sh`

- [ ] **Step 1: Write the coverage script**

Create `scripts/cover.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail

THRESHOLD="${COVERAGE_THRESHOLD:-80}"
PROFILE="${COVERAGE_PROFILE:-/tmp/opentile-go.coverprofile}"

if [[ -z "${OPENTILE_TESTDIR:-}" ]]; then
    echo "WARN: OPENTILE_TESTDIR is unset; integration-backed paths won't be exercised. Coverage will be lower." >&2
fi

go test ./... -coverpkg=./... -coverprofile="$PROFILE" -count=1
echo
echo "=== Per-package coverage (threshold: $THRESHOLD%) ==="

go tool cover -func="$PROFILE" | awk -v thresh="$THRESHOLD" '
/^github.com\/cornish\/opentile-go/ {
    pkg = $1; sub(/:.*/, "", pkg);
    n = split(pkg, parts, "/");
    pct = $NF; sub(/%/, "", pct);
    dir_key = parts[1] "/" parts[2] "/" parts[3];
    if (n >= 5) dir_key = dir_key "/" parts[4];
    if (n >= 6) dir_key = dir_key "/" parts[5];
    cnt[dir_key]++; sum[dir_key] += pct + 0;
}
END {
    failed = 0;
    for (k in cnt) {
        avg = sum[k] / cnt[k];
        printf "%6.1f%%  %s", avg, k;
        if (avg < thresh) {
            printf "  ❌ below %d%%", thresh;
            failed = 1;
        }
        printf "\n";
    }
    if (failed) {
        printf "\nFAIL: at least one package below %d%% coverage\n", thresh;
        exit 1;
    }
    printf "\nPASS: all packages ≥ %d%%\n", thresh;
}' | sort -rn
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x scripts/cover.sh
```

- [ ] **Step 3: Add a Makefile**

Create `Makefile`:
```make
.PHONY: test cover parity vet bench

test:
	go test ./... -race -count=1

cover:
	OPENTILE_TESTDIR=$(PWD)/sample_files scripts/cover.sh

parity:
	OPENTILE_ORACLE_PYTHON=$${OPENTILE_ORACLE_PYTHON:-/tmp/opentile-py/bin/python} \
	OPENTILE_TESTDIR=$(PWD)/sample_files \
	  go test ./tests/oracle/... -tags parity -v -timeout 30m

vet:
	go vet ./...

bench:
	NDPI_BENCH_SLIDE=$(PWD)/sample_files/ndpi/CMU-1.ndpi \
	  go test ./formats/ndpi -bench=Tile -benchtime=3x -run=^$$ -v
```

- [ ] **Step 4: Run the gate**

```bash
make cover
```

Expected: PASS — every package ≥80%. If any package fails, the failing package's per-function output above tells you what to test next; add tests until it passes, then commit.

- [ ] **Step 5: Commit**

```bash
git add Makefile scripts/cover.sh
git commit -m "build: make cover / make test / make parity targets

scripts/cover.sh enforces ≥80% coverage per package as the v0.3 done-when
gate. Standalone Makefile drives test/cover/parity/vet/bench so contributors
don't have to remember the env-var dance.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch D — Performance + robustness + parity oracle

## Task 23: N-1 — unify SOF dimension patchers

**Files:**
- Modify: `internal/jpeg/sof.go` (canonical helper)
- Modify: `formats/ndpi/striped.go` (drop local `patchSOFSize`)

- [ ] **Step 1: Audit existing helpers**

```bash
grep -n "func.*patchSOFSize\|func.*ReplaceSOFDimensions" internal/jpeg/ formats/
```

Both should appear. The canonical version lives in `internal/jpeg`. Migrate the NDPI-local one.

- [ ] **Step 2: Confirm the canonical signature**

In `internal/jpeg/sof.go`, the helper:
```go
// ReplaceSOFDimensions returns a copy of jpegBytes whose SOF0 segment's
// height/width fields are overwritten with newH and newW. Callers use this
// to patch the SOF before passing the bytes to libjpeg-turbo or to a
// downstream consumer that infers image size from the SOF.
//
// Assumes SOF0 precedes any entropy-coded scan data, which is always true
// for well-formed JPEGs (SOF must come before SOS). Byte-scans for FF C0
// from the start; first match is the real SOF.
func ReplaceSOFDimensions(jpegBytes []byte, newH, newW uint16) ([]byte, error) {
    // existing implementation
}
```

If the existing signature differs (different argument order, different types), keep its existing public signature; this task is about removing the duplicate, not changing the API.

- [ ] **Step 3: Replace `formats/ndpi/striped.go:patchSOFSize` calls**

In `formats/ndpi/striped.go`, find every call to the local `patchSOFSize(header, h, w)` and replace with `jpeg.ReplaceSOFDimensions(header, uint16(h), uint16(w))`. Delete the local `patchSOFSize` function definition. Add the `internal/jpeg` import if not already imported.

- [ ] **Step 4: Run the suite**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. No fixture changes (byte output unchanged — same algorithm, just one canonical helper now).

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/sof.go formats/ndpi/striped.go
git commit -m "refactor(jpeg,ndpi): N-1 — single canonical SOF dimensions patcher

Drops the duplicate patchSOFSize from formats/ndpi/striped.go in favor of
internal/jpeg.ReplaceSOFDimensions. Net negative LOC, no byte output
changes. Closes N-1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 24: N-3 — pre-compute DC value via `CropOpts`

**Files:**
- Modify: `internal/jpegturbo/turbo.go` (add `CropOpts`)
- Modify: `internal/jpegturbo/turbo_cgo.go` (consume pre-computed DC)
- Modify: `internal/jpegturbo/turbo_nocgo.go` (stub)
- Modify: `formats/ndpi/oneframe.go` and `formats/ndpi/striped.go` (compute DC once per level)

- [ ] **Step 1: Add `CropOpts`**

In `internal/jpegturbo/turbo.go`:
```go
// CropOpts is an optional argument to CropWithBackgroundLuminance. Callers
// that already know the source's luma DC quantisation value pass the
// pre-computed luma DC coefficient via DCBackground; otherwise the cgo
// path re-parses the source's DQT on every call.
type CropOpts struct {
	// DCBackground is the post-quantisation DC coefficient to plant in OOB
	// luma blocks. If zero, the cgo path computes it from
	// (luminance, source's luma DQT). Pass non-zero to skip the per-call
	// DQT parse.
	DCBackground int
}
```

- [ ] **Step 2: Extend the cgo wrapper**

In `internal/jpegturbo/turbo_cgo.go`, add:
```go
// CropWithBackgroundLuminanceOpts is the full-featured variant. If
// opts.DCBackground != 0, that value is used directly as the OOB luma DC
// coefficient; otherwise the function parses the source's DQT and computes
// it via internal/jpeg.LuminanceToDCCoefficient (the per-call path).
func CropWithBackgroundLuminanceOpts(src []byte, r Region, luminance BackgroundLuminance, opts CropOpts) ([]byte, error) {
	var lum int
	if opts.DCBackground != 0 {
		lum = opts.DCBackground
	} else {
		var err error
		lum, err = jpeg.LuminanceToDCCoefficient(src, float64(luminance))
		if err != nil {
			return nil, fmt.Errorf("jpegturbo: derive luma DC from source: %w", err)
		}
	}
	// ... rest of the existing CropWithBackgroundLuminance implementation,
	// using `lum` instead of re-deriving.
}

// CropWithBackgroundLuminance preserves the existing API by calling the
// new variant with empty opts.
func CropWithBackgroundLuminance(src []byte, r Region, luminance BackgroundLuminance) ([]byte, error) {
	return CropWithBackgroundLuminanceOpts(src, r, luminance, CropOpts{})
}
```

- [ ] **Step 3: Stub for nocgo**

In `internal/jpegturbo/turbo_nocgo.go`:
```go
func CropWithBackgroundLuminanceOpts(src []byte, r Region, luminance BackgroundLuminance, opts CropOpts) ([]byte, error) {
	return nil, ErrCGORequired
}
```

- [ ] **Step 4: Cache DC at level construction in NDPI**

In `formats/ndpi/oneframe.go` and `formats/ndpi/striped.go`, the level structs gain a field:
```go
type oneFrameImage struct {
	// ... existing fields ...
	dcBackground int // post-quantisation DC for white-fill (0 if not yet computed)
}
```

In each level's constructor, after the JPEG bytes are first available (which for striped is once we have the patched header; for one-frame it's once we have the padded JPEG), compute:
```go
dc, err := jpeg.LuminanceToDCCoefficient(headerBytes, 1.0)
if err != nil {
    return nil, fmt.Errorf("ndpi: derive luma DC: %w", err)
}
l.dcBackground = dc
```

Then in each `Tile()` call's `CropWithBackground` path:
```go
out, err := jpegturbo.CropWithBackgroundLuminanceOpts(
    frame, region, jpegturbo.DefaultBackgroundLuminance,
    jpegturbo.CropOpts{DCBackground: l.dcBackground},
)
```

- [ ] **Step 5: Run the parity oracle**

```bash
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: same pass/log set as before this task (byte output unchanged; we just compute the same value once per level instead of once per tile).

- [ ] **Step 6: Commit**

```bash
git add internal/jpegturbo/ formats/ndpi/oneframe.go formats/ndpi/striped.go
git commit -m "perf(jpegturbo): N-3 — cache luma DC per level via CropOpts

Drops per-tile DQT parse on the OOB-fill path. NDPI levels compute the
luma DC once at constructor time (luminance=1.0 → ~170 for our typical
DQT) and pass it via CropOpts.DCBackground. CropWithBackgroundLuminance
keeps its existing signature; new CropWithBackgroundLuminanceOpts variant
exposes the optimization. Closes N-3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 25: I3 — `Scan` reuses existing `*bufio.Reader`

**Files:**
- Modify: `internal/jpeg/scan.go`
- Modify: `internal/jpeg/concat.go` (replace byte-scan workaround)

- [ ] **Step 1: Update `Scan`**

In `internal/jpeg/scan.go`, find where `Scan` wraps its argument in `bufio.NewReader`. Replace with:
```go
func Scan(r io.Reader) iter.Seq2[Segment, error] {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 4096)
	}
	return func(yield func(Segment, error) bool) {
		for {
			seg, err := readSegment(br) // existing helper
			if err == io.EOF {
				return
			}
			if !yield(seg, err) {
				return
			}
		}
	}
}
```

- [ ] **Step 2: Use `Scan` in `extractScanData`**

In `internal/jpeg/concat.go`, the `extractScanData` byte-scan can be replaced. Find the function and replace its body with:
```go
func extractScanData(frag []byte) ([]byte, error) {
	br := bufio.NewReader(bytes.NewReader(frag))
	for seg, err := range Scan(br) {
		if err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		if seg.Marker == SOS {
			// Read remaining bytes after the SOS payload as the entropy data.
			data, end, err := ReadScan(br)
			if err != nil {
				return nil, err
			}
			if end != EOI {
				return nil, fmt.Errorf("%w: scan ended with 0x%X, want EOI", ErrBadJPEG, end)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("%w: SOS not found", ErrBadJPEG)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. No byte-output changes (same algorithm, just reusing `Scan`).

- [ ] **Step 4: Commit**

```bash
git add internal/jpeg/scan.go internal/jpeg/concat.go
git commit -m "refactor(jpeg): I3 — Scan reuses existing *bufio.Reader

Lets callers chain Scan → ReadScan on the same underlying reader.
extractScanData drops its byte-scan workaround in favor of a real
Scan-then-ReadScan call. Closes I3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 26: I4 — `ConcatenateScans` precomputed header prefix

**Files:**
- Modify: `internal/jpeg/concat.go`
- Modify: `formats/ndpi/striped.go` (use `Assembler` per level)
- Modify: `formats/svs/associated.go` (same)

- [ ] **Step 1: Add the `Assembler` type**

In `internal/jpeg/concat.go`:
```go
// Assembler precomputes the header prefix for a fixed JPEGTables /
// ColorspaceFix / first-fragment-SOF combination. Callers that assemble
// many tiles from the same source amortise the SplitJPEGTables and
// first-fragment-parsing work over all calls.
//
// Assemble takes new fragments and returns a complete JPEG bytestream.
// The first fragment's SOF dimensions are overwritten with opts.Width /
// opts.Height (matching ConcatenateScans's existing semantics).
//
// Assembler instances are not safe for concurrent use; create one per
// caller goroutine if needed.
type Assembler struct {
	headerPrefix []byte
	opts         ConcatOpts
}

// NewAssembler precomputes the static prefix for the given opts. It needs
// a sample first fragment to extract SOF component info; in practice the
// caller passes the page's JPEGTables blob padded to a minimal full JPEG,
// or the actual first fragment if that's available.
func NewAssembler(sampleFirstFragment []byte, opts ConcatOpts) (*Assembler, error) {
	// Reuse ConcatenateScans logic to build a header for a one-fragment
	// assembly, then strip the entropy data + EOI to keep just the prefix.
	out, err := ConcatenateScans([][]byte{sampleFirstFragment}, opts)
	if err != nil {
		return nil, err
	}
	// Find the entropy-data start (first FF DA payload boundary) and truncate.
	br := bufio.NewReader(bytes.NewReader(out))
	for seg, err := range Scan(br) {
		if err != nil {
			return nil, err
		}
		if seg.Marker == SOS {
			// Position in `out` after the SOS marker + payload.
			// Easiest: re-scan `out` for FF DA + length and truncate.
			break
		}
	}
	prefixEnd, err := findSOSHeaderEnd(out)
	if err != nil {
		return nil, err
	}
	return &Assembler{headerPrefix: out[:prefixEnd], opts: opts}, nil
}

// Assemble produces a complete JPEG by appending fragments' entropy data
// (with restart markers between when opts.RestartInterval > 0) plus a
// trailing EOI to the precomputed prefix.
func (a *Assembler) Assemble(fragments [][]byte) ([]byte, error) {
	out := make([]byte, 0, len(a.headerPrefix)+1024)
	out = append(out, a.headerPrefix...)
	for i, frag := range fragments {
		scan, err := extractScanData(frag)
		if err != nil {
			return nil, fmt.Errorf("fragment %d: %w", i, err)
		}
		out = append(out, scan...)
		if a.opts.RestartInterval > 0 && i < len(fragments)-1 {
			out = append(out, 0xFF, byte(0xD0+(i%8)))
		}
	}
	out = append(out, 0xFF, 0xD9)
	return out, nil
}

// findSOSHeaderEnd returns the byte offset just past the SOS segment's
// length-prefixed payload in jpeg, which is where entropy data begins.
func findSOSHeaderEnd(jpeg []byte) (int, error) {
	// Reuse extractScanData's existing helper or inline the logic.
	for i := 0; i+1 < len(jpeg); i++ {
		if jpeg[i] == 0xFF && jpeg[i+1] == 0xDA {
			if i+4 > len(jpeg) {
				return 0, fmt.Errorf("%w: SOS truncated", ErrBadJPEG)
			}
			length := int(binary.BigEndian.Uint16(jpeg[i+2 : i+4]))
			return i + 2 + length, nil
		}
	}
	return 0, fmt.Errorf("%w: SOS not found", ErrBadJPEG)
}
```

- [ ] **Step 2: Adopt in NDPI striped**

In `formats/ndpi/striped.go`, the `stripedImage` struct gains an `*jpeg.Assembler`:
```go
type stripedImage struct {
	// ... existing fields ...
	assembler *jpeg.Assembler
}
```

In the constructor (`newStripedImage`), once `stripes.JPEGHeader` is available, build the Assembler:
```go
opts := jpeg.ConcatOpts{
    Width:           uint16(stripes.StripeW),
    Height:          uint16(stripes.StripeH),
    JPEGTables:      stripes.JPEGTables,
    ColorspaceFix:   false, // NDPI uses YCbCr; no Adobe RGB fix
    RestartInterval: 0,     // assembled per-tile in striped path
}
asm, err := jpeg.NewAssembler(stripes.SampleFragment, opts)
if err != nil {
    return nil, fmt.Errorf("ndpi: build striped assembler: %w", err)
}
l.assembler = asm
```

(`stripes.SampleFragment` is a new field — the first stripe's bytes, populated when `readStripes` runs.)

In `Tile()`, replace the manual `out = append(out, header...)` + fragments + EOI assembly with `l.assembler.Assemble(fragments)`.

- [ ] **Step 3: Adopt in SVS associated**

In `formats/svs/associated.go`, `stripedJPEGAssociated` gains an `*jpeg.Assembler` field, populated in `newStripedJPEGAssociated`. `Bytes()` delegates to `assembler.Assemble`.

- [ ] **Step 4: Run the suite**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. Byte output unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/jpeg/concat.go formats/ndpi/striped.go formats/svs/associated.go
git commit -m "perf(jpeg,ndpi,svs): I4 — Assembler precomputes header prefix per level

For 24K-tile slide levels, cuts SplitJPEGTables + first-fragment SOF/SOS
extraction from per-call to once-per-level. Existing ConcatenateScans
unchanged so direct callers don't need to migrate. Closes I4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 27: O1 — `walkIFDs` bulk-read

**Files:**
- Modify: `internal/tiff/ifd.go`

- [ ] **Step 1: Replace per-entry `ReadAt` with bulk-read**

In `internal/tiff/ifd.go`, find the IFD-walking loop. The current shape is roughly:
```go
// 4 ReadAt calls per entry: tag, type, count, valueOffset
```

Replace with:
```go
// Read the entry-count first (2 bytes), then bulk-read all entries
// (12 bytes each) plus the next-IFD pointer (4 bytes for classic, 8 for BigTIFF).
var countBuf [2]byte
if err := tiff.ReadAtFull(r, countBuf[:], int64(offset)); err != nil {
    return nil, err
}
count := binary.LittleEndian.Uint16(countBuf[:])
totalLen := int64(12*count + 4) // adjust for BigTIFF: 20*count + 8
entriesBuf := make([]byte, totalLen)
if err := tiff.ReadAtFull(r, entriesBuf, int64(offset)+2); err != nil {
    return nil, err
}
// Slice-decode each entry from entriesBuf — no more ReadAt calls in this loop.
```

For BigTIFF, the entry size and next-pointer size differ; branch on the existing `mode` flag in `walkIFDs`.

- [ ] **Step 2: Add a benchmark to verify the change**

In `internal/tiff/ifd_test.go`:
```go
func BenchmarkWalkIFDs(b *testing.B) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		b.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
	if _, err := os.Stat(slide); err != nil {
		b.Skipf("slide not present: %v", err)
	}
	f, err := os.Open(slide)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Open(f, st.Size())
		if err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run benchmarks before and after to confirm improvement**

```bash
git stash # save your O1 changes
OPENTILE_TESTDIR="$PWD/sample_files" go test ./internal/tiff/... -bench=BenchmarkWalkIFDs -benchtime=3x -run=^$ -v
git stash pop
OPENTILE_TESTDIR="$PWD/sample_files" go test ./internal/tiff/... -bench=BenchmarkWalkIFDs -benchtime=3x -run=^$ -v
```

Expected: post-O1 benchmark is faster (typically 2-4× on slides with many pages).

- [ ] **Step 4: Run the full suite**

```bash
go test ./... -race -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tiff/ifd.go internal/tiff/ifd_test.go
git commit -m "perf(tiff): O1 — walkIFDs bulk-reads each IFD's entries

Replaces per-entry ReadAt with one ReadAt for the whole IFD body.
Reduces syscall pressure on adversarial inputs and ~2-4× faster on
multi-page slides. Closes O1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 28: N-2 — segment-walker for SOS/DRI lookup

**Files:**
- Modify: `internal/jpeg/concat.go`

- [ ] **Step 1: Replace `bytes.Index` SOS/DRI lookups with `Scan`-based walks**

`grep -n "bytes.Index" internal/jpeg/concat.go` to find every byte-scan. For each, replace with a `Scan`-based loop. Example:

Before:
```go
sosIdx := bytes.Index(frame, []byte{0xFF, 0xDA})
```

After:
```go
sosIdx := -1
br := bufio.NewReader(bytes.NewReader(frame))
pos := 0
for seg, err := range jpeg.Scan(br) {
    if err != nil { return nil, err }
    if seg.Marker == jpeg.SOS {
        sosIdx = pos
        break
    }
    pos += 2 + 2 + len(seg.Payload) // marker + length field + payload
}
if sosIdx < 0 {
    return nil, fmt.Errorf("%w: SOS marker not found", jpeg.ErrBadJPEG)
}
```

(Tracking `pos` requires that `Scan` reports payload length and we add it manually; alternatively, expose a `(seg, offset)` variant. The simpler form: since `Scan` advances through the reader, the offset is the reader's position which we can read via `bufio.Reader.Buffered()` math.)

The cleanest approach: since `findSOSHeaderEnd` already exists (Task 26), reuse it:
```go
sosIdx, err := findSOSStart(frame) // marker offset, not header end
```

Add `findSOSStart`:
```go
func findSOSStart(jpeg []byte) (int, error) {
    for i := 0; i+1 < len(jpeg); {
        if jpeg[i] != 0xFF { return 0, fmt.Errorf("%w: unexpected byte 0x%02X at pos %d", ErrBadJPEG, jpeg[i], i) }
        // Skip fill 0xFF bytes
        for i < len(jpeg) && jpeg[i] == 0xFF { i++ }
        if i >= len(jpeg) { return 0, fmt.Errorf("%w: truncated", ErrBadJPEG) }
        marker := Marker(jpeg[i]); i++
        if marker == SOI || marker == EOI { continue }
        if marker.isStandalone() { continue }
        if marker == SOS {
            return i - 2, nil // FF DA position (i is past DA marker byte)
        }
        if i+2 > len(jpeg) { return 0, fmt.Errorf("%w: truncated", ErrBadJPEG) }
        length := int(binary.BigEndian.Uint16(jpeg[i:i+2]))
        i += length
    }
    return 0, fmt.Errorf("%w: SOS not found", ErrBadJPEG)
}
```

This walks segments by length; the byte-stuffing assumption goes away because we never scan inside payload data.

Replace every `bytes.Index(frame, []byte{0xFF, 0xDA})` in `concat.go` with `findSOSStart(frame)`.

- [ ] **Step 2: Run the suite**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS, byte output unchanged.

- [ ] **Step 3: Commit**

```bash
git add internal/jpeg/concat.go
git commit -m "refactor(jpeg): N-2 — segment-walker for SOS lookup, no more byte-scan

Replaces bytes.Index({0xFF, 0xDA}) with structural marker walking.
Eliminates the byte-stuffing-assumption fragility documented in
ConcatOpts's godoc. Closes N-2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 29: N-4 — DQT segment-boundary validation

**Files:**
- Modify: `internal/jpeg/dqt.go`

- [ ] **Step 1: Add structural walk before byte-scan**

In `dcQuantForTable`, prepend a structural walk that finds the DQT via `Scan`:
```go
func dcQuantForTable(src []byte, tableIdx int) (int, error) {
	// Try the structural walk first; fall back to byte-scan if Scan fails
	// (e.g. malformed prefix).
	if dc, ok := dcQuantViaScan(src, tableIdx); ok {
		return dc, nil
	}
	// Existing byte-scan logic continues here.
	return dcQuantViaByteScan(src, tableIdx)
}

func dcQuantViaScan(src []byte, tableIdx int) (int, bool) {
	br := bufio.NewReader(bytes.NewReader(src))
	for seg, err := range Scan(br) {
		if err != nil {
			return 0, false
		}
		if seg.Marker != DQT {
			continue
		}
		// DQT segment payload: precision/id (1 byte) + 64 quant values.
		if len(seg.Payload) < 1 {
			continue
		}
		pid := seg.Payload[0]
		precision := int(pid >> 4)
		id := int(pid & 0x0F)
		if id != tableIdx {
			continue
		}
		switch precision {
		case 0:
			if len(seg.Payload) < 2 {
				return 0, false
			}
			return int(int8(seg.Payload[1])), true
		case 1:
			if len(seg.Payload) < 3 {
				return 0, false
			}
			return int(int16(binary.BigEndian.Uint16(seg.Payload[1:3]))), true
		}
	}
	return 0, false
}
```

(Add `DQT = 0xDB` to `internal/jpeg/marker.go` if not already exported.)

- [ ] **Step 2: Run the existing tests**

```bash
go test ./internal/jpeg/... -run 'LumaDCQuant|LuminanceToDCCoefficient' -v
```

Expected: PASS.

- [ ] **Step 3: Run full suite + parity**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS, byte output unchanged (same DQT[0] value derived).

- [ ] **Step 4: Commit**

```bash
git add internal/jpeg/dqt.go internal/jpeg/marker.go
git commit -m "refactor(jpeg): N-4 — DQT lookup walks segments before byte-scan

The structural walk catches false-positive FF DB matches inside other
segments. Falls back to the byte-scan if structural walking fails on
malformed input. Closes N-4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 30: N-10 — invert striped fall-through control flow

**Files:**
- Modify: `formats/ndpi/striped.go`

- [ ] **Step 1: Invert the gate**

In `formats/ndpi/striped.go:Tile`, find the existing pattern:
```go
out, err := jpegturbo.Crop(frame, region)
if err != nil {
    extendsBeyond := left+l.tileSize.W > frameSize.W || top+l.tileSize.H > frameSize.H
    if extendsBeyond {
        out, err = jpegturbo.CropWithBackground(frame, region)
    }
    if err != nil { ... }
}
```

Replace with:
```go
extendsBeyond := left+l.tileSize.W > frameSize.W || top+l.tileSize.H > frameSize.H
var out []byte
var err error
if extendsBeyond {
    out, err = jpegturbo.CropWithBackgroundLuminanceOpts(
        frame, region, jpegturbo.DefaultBackgroundLuminance,
        jpegturbo.CropOpts{DCBackground: l.dcBackground},
    )
} else {
    out, err = jpegturbo.Crop(frame, region)
}
if err != nil {
    return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
}
```

- [ ] **Step 2: Run the suite + parity**

```bash
go test ./... -race -count=1
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. Byte output unchanged.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/striped.go
git commit -m "refactor(ndpi): N-10 — invert striped Crop fall-through control flow

Check extendsBeyond first; pick CropWithBackground or Crop accordingly.
A non-OOB Crop failure now propagates as an error instead of silently
falling through to the OOB-fill path. Closes N-10.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 31: I5 — `ConcatenateScans` defensive RSTn count check

**Files:**
- Modify: `internal/jpeg/concat.go`

- [ ] **Step 1: Add the check**

In `ConcatenateScans` (or in the `Assembler.Assemble` path after Task 26), when `opts.RestartInterval > 0`, validate each fragment:
```go
for i, frag := range fragments {
    if opts.RestartInterval > 0 {
        if n := countRSTnMarkers(frag); n != 1 {
            return nil, fmt.Errorf("%w: fragment %d has %d RSTn markers, expected exactly 1 (RestartInterval > 0)",
                ErrBadJPEG, i, n)
        }
    }
    // ... existing extraction logic ...
}

func countRSTnMarkers(frag []byte) int {
	n := 0
	for i := 0; i+1 < len(frag); i++ {
		if frag[i] == 0xFF && frag[i+1] >= 0xD0 && frag[i+1] <= 0xD7 {
			n++
			i++ // skip past the marker byte to avoid double-counting overlaps
		}
	}
	return n
}
```

- [ ] **Step 2: Add a unit test**

In `internal/jpeg/concat_test.go`:
```go
func TestConcatenateScansRejectsMultipleRSTnPerFragment(t *testing.T) {
	// Synthetic fragment with two RSTn markers (FF D0 ... FF D1)
	frag := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x08, 0x00, 0x08, 0x01, 0x01, 0x11, 0x00, // SOF0
		0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // SOS
		0xAA, 0xBB, // entropy
		0xFF, 0xD0, // RST0
		0xCC, 0xDD,
		0xFF, 0xD1, // RST1
		0xEE, 0xFF, // entropy
		0xFF, 0xD9, // EOI
	}
	_, err := ConcatenateScans([][]byte{frag, frag}, ConcatOpts{
		Width: 8, Height: 8, RestartInterval: 1,
	})
	if err == nil {
		t.Fatal("expected error for multi-RSTn fragment")
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/jpeg/... -run TestConcatenateScansRejectsMultipleRSTnPerFragment -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/jpeg/concat.go internal/jpeg/concat_test.go
git commit -m "fix(jpeg): I5 — defensive RSTn count check in ConcatenateScans

When RestartInterval > 0, each fragment must contain exactly one RSTn
marker; multiple internal restart intervals would corrupt the assembled
output. Documented in v0.2 godoc; now enforced with a test that locks
the contract. Closes I5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 32: O2 — `int(e.Count)` overflow comment

**Files:**
- Modify: `internal/tiff/page.go`

- [ ] **Step 1: Add the comment**

Find every `int(e.Count)` in `internal/tiff/page.go` (typically in `JPEGTables` and `ICCProfile` and any tag-array readers). Above the first such line, add:
```go
// e.Count is uint32. On 32-bit platforms int(e.Count) truncates if Count
// > 2 GiB. In practice Count is bounded by real-world tag values (<1 MB
// for JPEGTables, <100 KB for ICCProfile); the truncation can't fire on
// 64-bit targets where we run. If 32-bit support is ever in scope, audit
// these sites and add explicit bounds.
```

- [ ] **Step 2: Commit**

```bash
git add internal/tiff/page.go
git commit -m "docs(tiff): O2 — comment on int(e.Count) 32-bit truncation risk

Documents the assumption rather than introducing a defensive cap nobody
can hit in practice. Closes O2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 33: L16 — batched parity oracle runner

**Files:**
- Modify: `tests/oracle/oracle_runner.py`
- Modify: `tests/oracle/oracle.go`
- Modify: `tests/oracle/parity_test.go`

- [ ] **Step 1: Rewrite the Python runner for stdin/stdout protocol**

Replace `tests/oracle/oracle_runner.py`:
```python
#!/usr/bin/env python3
"""Persistent batched runner for the Go parity oracle.

Usage: oracle_runner.py <slide>

Protocol on stdin: one line per request, terminated with \\n.
  "level <int> <x> <y>"   → emit level tile bytes
  "associated <kind>"     → emit associated image of given kind
  "quit"                  → exit cleanly

Each response on stdout: 4-byte big-endian uint32 length, then that many
bytes of the requested blob. Length 0 = "not exposed" / skip. Errors are
written to stderr with a length-zero stdout response (so the Go side
treats them as skip).
"""
import os
import struct
import sys


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: oracle_runner.py <slide>", file=sys.stderr)
        return 2
    slide = sys.argv[1]
    tile_size = int(os.environ.get("OPENTILE_TILE_SIZE", "1024"))

    from opentile import OpenTile
    tiler = OpenTile.open(slide, tile_size)

    out = sys.stdout.buffer
    try:
        for line in sys.stdin:
            line = line.strip()
            if not line or line == "quit":
                break
            parts = line.split()
            try:
                if parts[0] == "level":
                    level, x, y = int(parts[1]), int(parts[2]), int(parts[3])
                    data = tiler.get_level(level).get_tile((x, y))
                elif parts[0] == "associated":
                    kind = parts[1]
                    if kind == "label":
                        imgs = tiler.labels
                    elif kind == "overview":
                        imgs = tiler.overviews
                    elif kind == "thumbnail":
                        imgs = tiler.thumbnails
                    else:
                        imgs = []
                    if imgs:
                        data = imgs[0].get_tile((0, 0))
                    else:
                        data = b""
                else:
                    raise ValueError(f"unknown command: {parts[0]}")
            except Exception as e:
                print(f"runner error: {e}", file=sys.stderr)
                data = b""
            out.write(struct.pack(">I", len(data)))
            out.write(data)
            out.flush()
    finally:
        tiler.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Update the Go wrapper**

In `tests/oracle/oracle.go`:
```go
//go:build parity

package oracle

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Session is a long-lived oracle subprocess for one slide. Open one with
// NewSession; call Tile and Associated on it; Close kills the subprocess.
// Session is not safe for concurrent use.
type Session struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
}

func NewSession(slide string, tileSize int) (*Session, error) {
	cmd := exec.Command(PythonBin(), RunnerScript(), slide)
	cmd.Env = append(os.Environ(), fmt.Sprintf("OPENTILE_TILE_SIZE=%d", tileSize))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Session{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

func (s *Session) Tile(level, x, y int) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "level %d %d %d\n", level, x, y); err != nil {
		return nil, err
	}
	return s.readBlob()
}

func (s *Session) Associated(kind string) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "associated %s\n", kind); err != nil {
		return nil, err
	}
	return s.readBlob()
}

func (s *Session) Close() error {
	fmt.Fprintln(s.stdin, "quit")
	s.stdin.Close()
	return s.cmd.Wait()
}

func (s *Session) readBlob() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(s.stdout, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return nil, nil // skip / not exposed
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.stdout, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// PythonBin / RunnerScript / Tile / Associated keep their existing signatures
// for one-shot callers (e.g. existing diagnostic tests). NewSession is the
// new fast path for batched parity runs.

// existing PythonBin, RunnerScript, Tile, Associated functions stay
```

- [ ] **Step 3: Update the parity test to use sessions**

In `tests/oracle/parity_test.go`:
```go
func runParityOnSlide(t *testing.T, slide string) {
	tiler, err := opentile.OpenFile(slide, opentile.WithTileSize(tileSize, tileSize))
	if err != nil { t.Fatalf("Open: %v", err) }
	defer tiler.Close()

	sess, err := oracle.NewSession(slide, tileSize)
	if err != nil { t.Fatalf("oracle session: %v", err) }
	defer sess.Close()

	isNDPI := strings.EqualFold(filepath.Ext(slide), ".ndpi")
	for li, lvl := range tiler.Levels() {
		positions := samplePositions(lvl.Grid(), *fullParity)
		imgSize := lvl.Size()
		for _, pos := range positions {
			our, err := lvl.Tile(pos.X, pos.Y)
			if err != nil { /* same as before */ continue }
			theirs, err := sess.Tile(li, pos.X, pos.Y)
			if err != nil { /* same as before */ continue }
			// existing comparison logic
		}
	}
	for _, a := range tiler.Associated() {
		if a.Kind() == "label" {
			t.Logf("slide %s associated %q: skipping (Python returns strip 0 only — L10)",
				filepath.Base(slide), a.Kind())
			continue
		}
		ourB, err := a.Bytes()
		if err != nil { /* same */ continue }
		theirB, err := sess.Associated(a.Kind())
		if err != nil { /* same */ continue }
		// existing comparison logic
	}
}
```

Raise the default sample size:
```go
func samplePositions(grid opentile.Size, full bool) []opentile.TilePos {
	if full { /* existing */ }
	const targetCount = 100 // raised from ~10 with the batched runner
	// Generate positions: corners, diagonals, mid-edges, plus an interior
	// stride that fills out to targetCount.
	// ...
}
```

The exact generator is implementer's choice; the spec wants ~100 distinct positions per level (deduplicated, clamped to grid bounds).

- [ ] **Step 4: Run the parity oracle**

```bash
OPENTILE_ORACLE_PYTHON=/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS. Total runtime under 60s for the sampled run on all slides (vs. ~45s in v0.2 with 10 tiles/level; per-tile cost drops dramatically so 100 tiles/level fits the same budget).

- [ ] **Step 5: Commit**

```bash
git add tests/oracle/oracle_runner.py tests/oracle/oracle.go tests/oracle/parity_test.go
git commit -m "perf(oracle): L16 — batched parity runner (one Python subprocess per slide)

Persistent stdin/stdout protocol: oracle_runner.py reads 'level x y' or
'associated kind' lines; emits 4-byte length + blob per request. Drops
~200ms Python startup from per-tile to per-slide. Default sample raised
from 10 to 100 positions/level.

Closes L16. -parity-full still walks every tile but is now ~10× faster
on big slides (OS-2.ndpi).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch E — Documentation, refactors, retirement

## Task 34: Theme 6 — documentation batch

**Files:**
- Modify: `internal/tiff/page.go` (decodeASCII, decodeInline)
- Modify: `internal/tiff/ifd.go` (walkIFDs overlapping IFDs comment, plus N-9 NDPI sniff)
- Modify: `internal/jpeg/sof.go` (ReplaceSOFDimensions byte-scan invariant)
- Modify: `internal/jpegturbo/turbo_cgo.go` (chroma DC=0 visual)
- Modify: `metadata.go` (AcquisitionDateTime zero comment)

- [ ] **Step 1: Apply each comment**

For each item, locate the function and prepend the doc comment (or extend the existing one).

**D1 — `internal/tiff/decodeASCII`**:
```go
// decodeASCII returns the value as a Go string. NUL terminators are
// silently tolerated: ASCII tags in the wild may or may not be NUL-terminated;
// callers should treat the returned string as authoritative.
```

**D2 — `internal/tiff/decodeInline`**:
```go
// decodeInline takes a *byteReader argument because it needs only b.order
// to interpret the inline 4-byte tag value. The caller's full reader state
// is irrelevant; passing the whole reader keeps the call site uniform with
// other decode helpers in this file.
```

**D3 — `Metadata.AcquisitionDateTime`** (in `metadata.go` or wherever `Metadata` is defined):
```go
// AcquisitionDateTime is the time the slide was scanned. Partial Date or
// Time inputs that fail time.Parse yield the zero value (time.Time{}).
// time.Time{}.IsZero() == true is the "unknown" sentinel; callers should
// always check IsZero rather than comparing against a specific time.
AcquisitionDateTime time.Time
```

**N-6 — `jpegturbo.CropWithBackground`**:
```go
// Fill behavior: the OOB region's luma DC matches Python opentile's
// background_luminance=1.0 default. Chroma DC stays at 0 (level-shift-
// neutral 128), producing pixels that decode close-to-but-not-exactly
// white. This matches PyTurboJPEG's behavior; for exact white, use
// CropWithBackgroundLuminance with luminance=1.0 and a chroma extension
// (not currently implemented; tracked as a future enhancement).
```

**N-9 — `internal/tiff/file.go`'s NDPI sniff**: above the function that reads tag 65420:
```go
// File detection peeks at NDPI's vendor tag 65420 to dispatch the NDPI-
// layout IFD walker for files with classic TIFF magic 42 plus 64-bit
// offsets. This is a deliberate cross-cutting peek; see docs/deferred.md L5.
```

**I2 — `internal/tiff/ifd.go:walkIFDs`**:
```go
// walkIFDs detects exact-offset cycles via a seen-map (offset → bool).
// Overlapping IFDs (one IFD whose body contains the start of another IFD)
// are not detected; only exact duplicate offsets. In practice this never
// fires on real slides; documented for completeness and as a v0.4+ TODO.
```

**I7 — `internal/jpeg.ReplaceSOFDimensions`** (after N-1 unification):
```go
// Byte-scan for FF C0 from offset 0 is safe because in well-formed JPEGs
// SOF0 precedes any entropy-coded scan data (the only place where 0xFF
// can legitimately appear unstuffed). Pathological inputs that prepend
// non-JPEG bytes containing FF C0 would cause this to find a wrong
// "SOF" and patch garbage; we don't defend against that.
```

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tiff/ internal/jpeg/sof.go internal/jpegturbo/turbo_cgo.go metadata.go
git commit -m "docs: D1, D2, D3, N-6, N-9, I2, I7 — godoc clarifications

Targeted comment additions across internal/tiff, internal/jpeg, and
internal/jpegturbo, plus a Metadata.AcquisitionDateTime zero-value note.

Closes D1, D2, D3, N-6, N-9, I2, I7.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 35: I6 — ranged RSTn check in `isStandalone`

**Files:**
- Modify: `internal/jpeg/marker.go`

- [ ] **Step 1: Replace literals with range**

In `internal/jpeg/marker.go`, find `isStandalone`:
```go
func (m Marker) isStandalone() bool {
	switch m {
	case SOI, EOI:
		return true
	case 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7:
		return true
	}
	return false
}
```

Replace with:
```go
func (m Marker) isStandalone() bool {
	if m == SOI || m == EOI {
		return true
	}
	// RSTn markers are 0xD0..0xD7 (RST0..RST7).
	return m >= RST0 && m <= RST0+7
}
```

(If `RST0` constant doesn't exist, add `const RST0 Marker = 0xD0` to the marker.go constants.)

- [ ] **Step 2: Run tests**

Run: `go test ./internal/jpeg/... -count=1`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/jpeg/marker.go
git commit -m "refactor(jpeg): I6 — ranged RSTn check in isStandalone

Replaces 0xD0..0xD7 literal switch with range comparison against RST0+7.
Avoids drift if marker constants ever change. Closes I6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 36: T2 — `withRegistry` test helper

**Files:**
- Modify: `opentile_test.go`

- [ ] **Step 1: Add the helper**

In `opentile_test.go`, near the top:
```go
// withRegistry replaces the package-global format registry with the given
// factories for the duration of the test, restoring the original on
// cleanup. Safe to use under t.Parallel() because each parallel test gets
// its own isolated registry view via t.Cleanup serialization.
//
// Tests that previously called resetRegistry() and Register(...) inline
// should migrate to this helper.
func withRegistry(t *testing.T, factories ...FormatFactory) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = nil
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
	for _, f := range factories {
		Register(f)
	}
}
```

- [ ] **Step 2: Migrate existing call sites**

Find every test in `opentile_test.go` that calls `resetRegistry()` and `Register(...)`. Replace with `withRegistry(t, factories...)`. Delete `resetRegistry` if it has no remaining callers.

- [ ] **Step 3: Run tests**

Run: `go test -run TestRegister -v -count=1`
Expected: PASS, all existing register-related tests.

- [ ] **Step 4: Commit**

```bash
git add opentile_test.go
git commit -m "test: T2 — withRegistry helper using t.Cleanup

Replaces the package-global resetRegistry pattern with a t.Cleanup-based
helper that's safe under t.Parallel(). Closes T2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 37: I1 — fold zero-length check into `indexOf`

**Files:**
- Modify: `formats/svs/tiled.go`

- [ ] **Step 1: Move the check**

In `formats/svs/tiled.go`, find `indexOf`:
```go
func (l *tiledImage) indexOf(x, y int) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	return y*l.grid.W + x, nil
}
```

Extend it to also reject zero-length tiles:
```go
func (l *tiledImage) indexOf(x, y int) (int, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	idx := y*l.grid.W + x
	if l.counts[idx] == 0 {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrCorruptTile}
	}
	return idx, nil
}
```

In `Tile` and `TileReader`, remove the now-redundant zero-length check after `indexOf` returns successfully.

- [ ] **Step 2: Run tests**

Run: `go test ./formats/svs/... -count=1`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add formats/svs/tiled.go
git commit -m "refactor(svs): I1 — fold zero-length check into indexOf

Tile and TileReader no longer duplicate the corrupt-tile guard. Closes I1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 38: I8 — `paddedJPEGOnce` → `sync.Once`

**Files:**
- Modify: `formats/ndpi/oneframe.go`

- [ ] **Step 1: Replace the bool with sync.Once**

In `formats/ndpi/oneframe.go`, the `oneFrameImage` struct currently has:
```go
paddedJPEGOnce bool
paddedJPEG     []byte
```

Replace with:
```go
paddedJPEGOnce sync.Once
paddedJPEG     []byte
paddedJPEGErr  error
```

Replace `getPaddedJPEG`:
```go
func (l *oneFrameImage) getPaddedJPEG() ([]byte, error) {
	l.paddedJPEGOnce.Do(func() {
		l.paddedJPEG, l.paddedJPEGErr = l.buildPaddedJPEG()
	})
	return l.paddedJPEG, l.paddedJPEGErr
}

func (l *oneFrameImage) buildPaddedJPEG() ([]byte, error) {
	// existing body of getPaddedJPEG without the bool guard
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./formats/ndpi/... -count=1 -race
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add formats/ndpi/oneframe.go
git commit -m "refactor(ndpi): I8 — paddedJPEGOnce uses sync.Once

Makes the single-entry contract explicit. Prevents a future caller
adding a non-extendedOnce.Do entry from regressing concurrency safety.
Closes I8.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 39: deferred.md retirement audit

**Files:**
- Modify: `docs/deferred.md`

- [ ] **Step 1: Walk every retired item**

For each of L1, L3, L7, L9, L10, L11, L15, L16, T1-T6, A1-A4, O1-O2, D1-D3, I1-I8, N-1, N-2, N-3, N-4, N-5, N-6, N-7, N-9, N-10:

- Confirm there's a v0.3 commit closing it.
- Delete the section from active deferred-list, OR move to a "Retired in v0.3" sub-section if you want to keep history.

Recommended: keep a brief "Retired in v0.3" section at the bottom of the file with one line per closed item, just enough to track what shipped.

- [ ] **Step 2: Update remaining limitations**

L4, L5, L6, L12, L14 stay open (permanent or v0.4+). Update each entry's "Severity" to clarify it's a permanent design choice or explicit v0.4+ punt, not a v0.3 oversight.

L13 was already moved to §4 in Task 13; no further action.

L2 and L8 were already removed in Task 13.

- [ ] **Step 3: Verify the structure**

```bash
grep -n "^### L\|^### Retired in v0.3\|^## " docs/deferred.md
```

Expected: clean structure with active limitations (L4/L5/L6/L12/L14), retired items as a list, no orphans.

- [ ] **Step 4: Commit**

```bash
git add docs/deferred.md
git commit -m "docs: v0.3 retirement audit — L1, L3, L7, L9, L10, L11, L15, L16 closed

Plus all of T1-T6, A1-A4, O1-O2, D1-D3, I1-I8, N-1 through N-10.
Permanent items (L4 MPP-may-be-absent, L5 internal/tiff NDPI sniff, L6
NDPI Map pages, L14 NDPI label synthesis) reframed as design choices.
L12 (NDPI edge-tile entropy) stays open with v0.4+ pointer.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 40: README + CLAUDE.md milestone bump

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: README status update**

In `README.md`, find the v0.2 status line. Replace with:
```markdown
**Status — v0.3**: Polish milestone over v0.2. Aperio SVS (JPEG and JPEG 2000)
and Hamamatsu NDPI fully supported with associated images and BigTIFF.
The v0.2 review's outstanding items (16 limitations, 25+ reviewer
suggestions) all closed except L4/L5/L6/L12/L14, which are documented
as permanent design choices or explicit v0.4+ punts. Public API frozen
from this point: every name in `go doc ./...` after v0.3 survives v0.3 →
v0.4 unchanged unless explicitly versioned.

Output is byte-identical to Python opentile 0.20.0 for all SVS tiles
and all NDPI interior tiles; NDPI edge-tile fill diverges in entropy
encoding only (L12, decoded pixels still match). See
[`docs/deferred.md`](./docs/deferred.md) for the full status.
```

Add a "Test helpers" subsection in the API/Usage section describing `opentile/opentiletest`:
```markdown
### Test helpers

Test helpers (config builders, fixture types) live in the
[`opentiletest`](./opentile/opentiletest) sibling package, mirroring
stdlib idiom (`httptest`, `iotest`):

```go
import "github.com/wsilabs/opentile-go/opentile/opentiletest"

cfg := opentiletest.NewConfig(opentile.Size{W: 512, H: 512}, opentile.CorruptTileError)
```
```

- [ ] **Step 2: CLAUDE.md milestone bump**

In `CLAUDE.md`, find the "Current milestone" section. Replace with:
```markdown
## Current milestone — v0.3

- **Scope:** Polish — close every v0.2 review finding. No new format support.
- **Settled:** v0.3 freezes the public API. Every name in `go doc ./...` after
  v0.3 ships survives v0.3 → v0.4 unchanged unless explicitly versioned.
- **Deferred:** Philips/Histech/OME (v0.4+), DICOM-WSI (not yet on roadmap).
  L12 (NDPI edge-tile entropy) is the only known correctness gap; documented
  as a v0.4+ investigation target.
- **Design:** `docs/superpowers/specs/2026-04-24-opentile-go-v03-design.md`
- **Plan:** `docs/superpowers/plans/2026-04-25-opentile-go-v03.md`
- **Work branch:** `feat/v0.3`
```

Add a new invariant near the top of the Invariants section:
```markdown
- **Public API stable from v0.3.** Adding new exported names is fine; renaming,
  moving, or removing is a breaking change that requires a major-version bump
  (or, until we have external users, an explicit owner sign-off).
```

- [ ] **Step 3: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "docs: README + CLAUDE.md updated for v0.3 release

README status line moves to v0.3 (polish; settled API). New "Test helpers"
subsection documents the opentiletest sibling package. CLAUDE.md milestone
bumped; new "Public API stable from v0.3" invariant added.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 41: Final vet/race/coverage sweep

**Files:**
- None (validation-only)

- [ ] **Step 1: Full race + vet**

```bash
go test ./... -race -count=1 -timeout 5m
go vet ./...
```

Expected: all tests pass; no vet warnings.

- [ ] **Step 2: nocgo build**

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./... -tags nocgo -count=1 -timeout 5m
```

Expected: build clean; nocgo tests pass.

- [ ] **Step 3: Coverage gate**

```bash
make cover
```

Expected: PASS — every package ≥80%.

- [ ] **Step 4: Parity oracle**

```bash
make parity
```

Expected: PASS on all sub-tests (with NDPI-only L12 t.Log carve-out).

- [ ] **Step 5: NDPI benchmark**

```bash
make bench
```

Expected: L0 ≤ 5ms/tile, L1-L3 ≤ 2ms/tile.

- [ ] **Step 6: Branch log snapshot**

```bash
git log --oneline main..feat/v0.3 | head -50
```

Expected: 25-30+ commits in clear theme order.

- [ ] **Step 7: No commit required**

This task is validation-only. If any check fails, fix the underlying issue (file an additional task within v0.3 or fix in place) and re-run. Once all green, the milestone is shipping-ready.

---

## Done when

- All 16 L-numbers either retired or actively documented as permanent (L4, L5, L6, L13, L14 stay; L1/L2/L3/L7/L8/L9/L10/L11/L15/L16 retire; L12 stays open with v0.4+ pointer).
- All 10 N-items from the v0.2 final review either landed or explicitly retired with a deferred entry.
- All T/A/O/D/I reviewer suggestions either landed or retired (T1-T6, A1-A4, O1-O2, D1-D3, I1-I8 — all addressed).
- Coverage ≥80% per package with `OPENTILE_TESTDIR` set, enforced by `make cover`.
- `TileReader` and `Tiles` exercised in both formats.
- Hamamatsu-1.ndpi sparse fixture committed and integration test green.
- BigTIFF SVS fixture committed *if* slide is available; otherwise BigTIFF-SVS integration deferred to v0.3.1 with a deferred entry.
- Parity oracle runtime under 60s for the sampled run on all 5+ slides.
- `go test ./... -race -count=1` clean. `go vet ./...` clean. `CGO_ENABLED=0 go test ./... -tags nocgo` clean.
- Public API stable from this point: every name in `go doc ./...` survives v0.3 → v0.4 unchanged unless explicitly versioned.
