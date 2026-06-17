# Validate() API — Design

**Status:** approved (brainstorm) — 2026-06-16
**Scope:** A `Validate` API that reports the *general nature* of structural
problems in a WSI file, without decoding pixels. Tiers 0 (openability) and 1
(structural) only; Tier 2 (pixel decode) is reserved as an additive seam, not
built.

---

## 1. Goal

Give callers a fast, deterministic answer to "is this WSI file well-formed per
opentile-go's reader, and if not, what *kind* of thing is wrong?" — enough to
decide *re-scan / re-convert / reject / file a bug against the writer*. The
result is a structured `Report` that a CLI can render and calling code can
branch on (library-first, two consumers).

The driving observation: **nobody hand-edits a scanner-produced WSI.** So the
value is the *category* of a problem, not byte-precise coordinates for a repair.
The design optimizes for "general nature," and is ruthlessly lean everywhere
else.

## 2. What "valid" means — and does not

**Positive claim.** "Passes Validate" means the file is *well-formed per
opentile-go's reader*: it opens, its declared structure is internally consistent
(grid math, pyramid shape, required metadata present), and its tile/byte
pointers land in-bounds.

**The four fences (non-goals).** The spec and API docs MUST state these so the
API cannot mislead:

1. **Valid ≠ correct pixels.** Structural validation cannot catch "decodes fine
   but shows garbage" — e.g. the #37 BIF serpentine-descramble bug or the
   `internal/tifflzw` Clear bug that corrupted cog-wsi labels. Those files are
   *structurally perfect*. Catching them requires pixel comparison against an
   independent oracle (`tests/oracle` / tifffile / openslide / dciodvfy), which
   is fundamentally outside a self-contained `Validate()`. Even the reserved
   Tier 2 only catches "won't decode," never "decodes wrong."
2. **Valid ≠ spec-conformant.** Validate checks against opentile-go's *own
   model* of each format, not the format spec's full MUST/SHOULD. True DICOM IOD
   conformance is `dciodvfy` (already mirrored at `WSILabs/dicom3tools-mirror`);
   OME-XML schema conformance is `ome-types`. Validate does not reimplement
   them.
3. **As-detected, not as-intended.** A broken SVS that no longer sniffs as SVS
   and falls through to the generic-TIFF catch-all can report OK *as
   generic-TIFF*. The report names the detected format; matching that against
   intent is the caller's job (there is deliberately no expected-format
   parameter — see §5).
4. **Not a repair tool.** Validate reports nature; it never mutates a file.

## 3. Tiers in scope

- **Tier 0 — openability.** Does the file open at all under normal dispatch?
  Covered automatically by the from-source entry points running `Open`.
- **Tier 1 — structural, no pixel decode.** Internal consistency of the parsed
  structure: byte-range bounds, grid math, pyramid shape, format conformance the
  reader already detects, metadata presence, orphan IFDs. Fast (metadata +
  arithmetic), deterministic, `nocgo`-safe.
- **Tier 2 — pixel decode (NOT built).** Decoding tiles to catch corrupt
  bitstreams. Reserved as an additive seam (§7); explicitly out of scope for
  v1.

## 4. API surface & types

All types are public, in a new root file `validate.go` (sibling to `errors.go`,
`tifftags.go`).

```go
// Severity of a finding.
type Severity int

const (
    Info Severity = iota
    Warning
    Error
)

// CheckCode is a stable, human-meaningful problem category — the "general
// nature" of a finding, and the primary thing callers branch on. String-
// underlying so the catalog is open-ended (new codes are additive).
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

// Finding is one rolled-up problem. Many occurrences of the same (Code, locus)
// are aggregated into a single Finding with a Count — the report conveys the
// general nature and scale, not a per-occurrence repair list.
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
    Format   Format    // detected format; FormatUnknown when Unopenable
    Findings []Finding
}

// NOTE: `Format` (format.go) currently has no zero-value name. This work adds
// one additive constant so `Report.Format` reads cleanly when the file did not
// open:
//
//     FormatUnknown Format = "" // the zero value, named for clarity
//
// Adding an exported name is always allowed by the project invariants.

// OK reports whether the file is well-formed per opentile-go's reader:
// true iff there are no Error-severity findings. Warning/Info do not affect OK.
func (r *Report) OK() bool

// Worst returns the highest severity present (for exit codes / quick branching);
// returns Info for an empty report.
func (r *Report) Worst() Severity

// ValidateOption is the additive seam for future decode-based checks (Tier 2).
// v1 defines the type but ships zero options. Functional-option pattern.
type ValidateOption func(*validateConfig)

// Entry points.
//
// The Go error is OPERATIONAL ONLY: returned when the bytes are genuinely
// unusable (path missing, size <= 0, I/O fault). A file that opens-but-is-broken
// — or even one that FAILS TO OPEN — yields a *Report (with a CheckUnopenable
// Error finding and Format == FormatUnknown), not a Go error. Callers check
// report.OK() uniformly.
func ValidateFile(path string, opts ...ValidateOption) (*Report, error)
func Validate(r io.ReaderAt, size int64, opts ...ValidateOption) (*Report, error)

// Slide method: Tier 1 only (already open, so Tier 0 is moot and no operation
// can fail — hence no error return). Reuses the Slide's parsed state.
func (s *Slide) Validate(opts ...ValidateOption) *Report
```

### Sealed API decisions

- **Result model: findings-as-data, severity-tiered.** Every validity issue is a
  `Finding`; the Go `error` is operational-only. `report.OK()` ⇔ no `Error`
  findings.
- **Finding shape: lean, category-first.** `Severity + Code + Message`, plus a
  coarse `(Pyramid, Level)` locus kept only because "which level" is part of the
  general picture and it is free, and a `Count` for rolled-up scale. No
  structured per-occurrence location (nobody repairs coordinates).
- **Aggregation: roll up by `(Code, Level)`, count only.** Many occurrences of
  the same problem collapse to one Finding with `Count == N` and a summary
  Message; no example coordinates.
- **Format target: as-detected, always reported.** Normal `Open` dispatch picks
  the format; the `Report.Format` always names it. No expected-format parameter.
- **Inputs: free funcs + Slide method.** `ValidateFile`/`Validate` cover Tier
  0+1 (run `Open` internally); `slide.Validate()` covers Tier 1 on an already-open
  slide. The free funcs Open then delegate to the same Slide-level checks. The
  path form handles DICOM's multi-file case.

## 5. Check catalog (v1)

Seven checks. Errors are genuine corruption; Warning/Info are cheap "nature"
signal. The catalog is extensible — new checks are additive `CheckCode`
constants.

| Code | Severity | Fires when | Source of truth |
|------|----------|-----------|-----------------|
| `CheckUnopenable` | Error | `Open` dispatch fails (no format matches, or the matched reader errors). Wrapped reason in `Message`; `Report.Format == FormatUnknown`. | Tier 0; from-source entry points only |
| `CheckOffsetsOutOfBounds` | Error | Any tile/strip/frame `offset+length` points past EOF, overflows, or is negative. The headline check — catches truncation and dangling pointers that `Open` silently passes today. | per-reader byte ranges + file size |
| `CheckTileGridMismatch` | Error | The tile offset-array length disagrees with `ceil(W/tw)·ceil(H/th)`, or a level reports zero dims / zero tiles. | public `Pyramid`/`Level` geometry (format-agnostic) where possible; reader hook for offset-array length |
| `CheckNonConformantFormat` | Error | Format-specific spec violations the reader already detects (e.g. COG-WSI `validateGhost`/`validateIFDs`). Surfaced as findings rather than a bare sentinel. | per-format hook |
| `CheckInconsistentPyramid` | Warning | Level dimensions not monotonically shrinking, or downsample ratios drift beyond the existing generic-TIFF tolerance. | public `Pyramid`/`Level` geometry (format-agnostic) |
| `CheckMissingMetadata` | Warning | Expected-but-optional fields absent (e.g. MPP, objective power). | per-format hook |
| `CheckOrphanIFD` | Info | Unreferenced IFDs present (generic-TIFF already flags these as `DirOther`). Legal-but-unusual. | per-format hook (TIFF) |

**Severity grounding (implementer constraint).** Severity assignments must be
grounded in upstream/format-spec behavior, not guessed — e.g. an orphan IFD is
legal-but-unusual (Info), not broken. This applies the project's standing "read
upstream, don't guess" invariant to the catalog.

**Not in v1 scope:** structured DICOM/OME cross-count checks (NumFrames vs
frame-position count; OME plane counts vs SizeZ/C/T). These can be added later
through the same per-format `NonConformantFormat` hook without touching the core
catalog.

## 6. Architecture

The format reader packages import the root `opentile` package (verified:
`formats/svs/svs.go`, `formats/generictiff/tiler.go`, etc.), and root `opentile`
never imports `formats/*` (registration goes through `internal/format`). So
public types defined in root can be referenced by the readers with no import
cycle — exactly how `TIFFDirectories()` already works (`tifftags.go:165-195`:
readers implement a method returning the public `opentile.TIFFDirectory`; the
`Slide` type-asserts it through the `UnwrapReader` chain).

**Two layers of checks:**

1. **Format-agnostic engine (root).** Runs checks computable from the open
   Slide's public `Pyramid`/`Level` geometry alone — `CheckTileGridMismatch`
   (the grid-math portion) and `CheckInconsistentPyramid` — for *every* format,
   with zero reader cooperation. This is the floor every slide gets.

2. **Per-reader `Validator` hook.** Discovered via a `validatorOf(s)` helper
   that mirrors `tiffProviderOf` (`tifftags.go:169`), walking the `UnwrapReader`
   chain. Contributes the checks needing internals: `CheckOffsetsOutOfBounds`
   (byte-range vs file size), `CheckNonConformantFormat`, `CheckMissingMetadata`,
   `CheckOrphanIFD`. A reader that does not implement the hook still gets layer 1
   — graceful degradation, exactly like `TIFFDirectories` returning `ok=false`
   for non-TIFF.

**The hook takes a collector, not a return slice.** A reader implements:

```go
// implemented by format readers that contribute Tier-1 findings
Validate(p *ValidationProbe)
```

where `ValidationProbe` carries `Size() int64` and a `Flag(code CheckCode,
pyramid, level int, msg string)` that records *one occurrence*. The engine rolls
occurrences up by `(Code, Level)` into count-only findings centrally. Readers
never count — keeping the aggregation policy in one place. (`ValidationProbe` is
public so readers can name its type; its method set is the only surface readers
touch.)

**Shared TIFF helper.** The 8 TIFF-based readers delegate to one
`internal/tiffvalidate` helper that walks parsed pages' tile/strip
offset+length arrays and orphan IFDs, flagging `CheckOffsetsOutOfBounds` and
`CheckOrphanIFD` against the probe. Each TIFF reader's `Validate` is then a thin
call into that helper plus any format-specific additions (e.g. COG-WSI
conformance). The 3 non-TIFF readers (IFE / SZI / DICOM) yield their own byte
ranges (ZIP entries / blocks / DICOM frame offsets).

**File size source.** The free funcs have it directly (path stat / the `size`
argument). The `Slide` already retains its backing size for tile reads, so
`slide.Validate()` has it too — no new plumbing.

**Why `CheckOffsetsOutOfBounds` is a reader hook, not pure engine.** Pixel-pointer
tags (`StripOffsets`/`TileOffsets`/...) are deliberately excluded from the public
`TIFFDirectories` surface, and non-TIFF formats source byte ranges differently
(ZIP/DICOM/IFE). The *check* (`offset+length <= size`) is generic, but the
*enumeration* of byte ranges is format-specific — so it lives behind the hook,
with the TIFF case shared via `internal/tiffvalidate`.

## 7. Tier-2 seam (reserved, not built)

Tier 2 slots in purely additively:

1. **`...ValidateOption` is the seam.** v1 defines the opaque functional-option
   type and ships zero options; a future `WithTileDecodeCheck(sample)` adds
   without changing any of the three signatures.
2. **`CheckCode` is open-ended.** Tier-2 codes (`CheckCorruptTile`, ...) are
   additive constants; `Finding`/`Report`/`Severity` shapes are unchanged — a
   corrupt tile is just another rolled-up Error finding.
3. **Tier 2 is engine-orchestrated, not a reader concern.** The engine holds the
   `*Slide`, so decode checks would loop levels and call the existing
   `DecodedTile` path under the option gate. The per-reader `Validator` hook
   stays strictly decode-free, so adding Tier 2 never forces a hook-interface
   change — v1 readers are not revisited.
4. **Codec / `nocgo` degradation is pre-decided.** When a decode check is
   requested but the codec is unavailable (or it is a `nocgo` build), that
   becomes an **Info** finding ("tile-decode check skipped: codec unavailable")
   — never a Go error and never a false `Error`. Keeps `report.OK()` meaningful
   across build tags.

## 8. Testing strategy

Two complementary halves, both following the project's TDD norm (write the
failing assertion against a synthetic input, watch it fail, implement, pass).

**1. Hermetic per-check tests (always run in CI, no corpus needed).** Build a
tiny synthetic TIFF in-code (the `formats/generictiff/internal/gates` synth
builders are the precedent) and mutate a `[]byte` copy behind an `io.ReaderAt`,
then assert the exact `Code` fires:

- `CheckOffsetsOutOfBounds` — rewrite a `TileOffsets` entry past EOF (or truncate
  the slice) → Error with the right `(level, count)`.
- `CheckTileGridMismatch` — rewrite `ImageWidth`/tile dims so grid math disagrees
  with the offset-array length → Error.
- `CheckUnopenable` — garbage / truncated header → exactly one `CheckUnopenable`
  Error and `Format == FormatUnknown`.
- `CheckInconsistentPyramid` (Warning), `CheckOrphanIFD` (Info),
  `CheckMissingMetadata` (Warning) — minimal synthetic inputs each.
- `CheckNonConformantFormat` — reuse COG-WSI's existing broken-ghost-area test
  inputs.

These are deterministic and hermetic, so they actually *guard* the checks on
every CI run (clean-corpus tests can only catch false positives, not regressions
in firing).

**2. Clean-corpus gate (no false positives), corpus-gated / skip-if-missing.** A
cross-format table over the public `wsi-fixtures` corpus asserts every real
fixture → `report.OK() == true` and no *unexpected* Warnings — mirroring the
existing `TestTIFFTagsAllFormats` / `TestSlideParity` pattern.

**Contract tests:**

- **Rollup:** an input with many OOB offsets → **one** finding with `Count == N`,
  not N findings.
- **Error channel:** nonexistent path → Go `error`; valid-but-broken file →
  `(report, nil)` with Error findings.
- **`nocgo`-safe:** Tier 0/1 are decode-free, so the validation tests build and
  pass under `-tags nocgo` (guarded), keeping the `nocgo` CI step green.

## 9. File structure

- `validate.go` (root, new) — public types (`Severity`, `CheckCode`, `Finding`,
  `Report`, `ValidateOption`, `ValidationProbe`), the entry points
  (`ValidateFile`, `Validate`, `(*Slide).Validate`), `validatorOf` discovery
  helper, the format-agnostic engine (grid + pyramid checks), and the central
  rollup.
- `format.go` (root, modify) — add the additive `FormatUnknown Format = ""`
  constant (names the zero value; used by `Report.Format` on `Unopenable`).
- `internal/tiffvalidate/` (new) — shared TIFF byte-range + orphan-IFD walker
  used by the 8 TIFF readers.
- `formats/<name>/validate.go` (new, per reader that contributes) — the reader's
  `Validate(p *ValidationProbe)` hook: a call into `tiffvalidate` (TIFF formats)
  or a format-specific byte-range yield (IFE/SZI/DICOM), plus format-specific
  findings (conformance, missing metadata).
- `validate_test.go` (+ per-format `validate_test.go`) — hermetic per-check
  tests, the clean-corpus gate, and the contract tests.

## 10. Out of scope / deferred

- Tier 2 (pixel-decode checks) — reserved seam only (§7).
- DICOM/OME structured cross-count checks — future `NonConformantFormat`
  additions (§5).
- Any expected-format / "validate as X" parameter (§2 fence 3, §4).
- Pixel-correctness or spec-conformance oracles — out by design (§2 fences 1–2);
  those remain the job of `tests/oracle` and `dciodvfy`.
