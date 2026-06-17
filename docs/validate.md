# Validate — structural WSI validation

`opentile.ValidateFile` / `opentile.Validate` / `(*Slide).Validate` report the
*general nature* of structural problems in a WSI file without decoding pixels.
The result is enough to decide *re-scan / re-convert / reject / file a bug against
the writer*.

## Entry points

```go
// ValidateFile opens path (single file or DICOM series directory) and validates.
func ValidateFile(path string, opts ...ValidateOption) (*Report, error)

// Validate validates an in-memory or streamed source.
func Validate(r io.ReaderAt, size int64, opts ...ValidateOption) (*Report, error)

// Validate runs Tier-1 checks on an already-open slide (no Tier 0, no error
// return — the slide is already open, so nothing can fail operationally).
func (s *Slide) Validate(opts ...ValidateOption) *Report
```

**Operational error vs. finding.** The Go `error` return is *operational only*:
it is returned only when the bytes are genuinely unusable — path missing or
unreadable, `size <= 0`, I/O fault. A file that **fails to open** or is
structurally broken returns a `*Report` (never a Go error), so callers can
uniformly check `report.OK()`. A file that did not open carries exactly one
`CheckUnopenable` Error finding and `Report.Format == FormatUnknown`.

## Report and Finding model

```go
type Report struct {
    Format   Format    // detected format; FormatUnknown when unopenable
    Findings []Finding
}

type Finding struct {
    Severity Severity  // Info, Warning, or Error
    Code     CheckCode // stable problem category (see catalog below)
    Message  string    // human context, e.g. "200 tiles reference offsets past EOF"
    Pyramid  int       // coarse locus; -1 when whole-file or not applicable
    Level    int       // coarse locus; -1 when not applicable
    Count    int       // occurrences rolled up under this (Code, locus); >= 1
}
```

`report.OK()` returns `true` iff there are no `Error`-severity findings.
Warning and Info findings do not affect `OK()`.

`report.Worst()` returns the highest severity present (useful for exit codes);
returns `Info` for an empty report.

**Rollup.** Many occurrences of the same problem at the same locus collapse
to a single `Finding` with `Count == N` and a summary `Message`. The report
conveys the general nature and scale, not a per-occurrence repair list.

## Check catalog

| Code | Severity | Fires when |
|------|----------|-----------|
| `CheckUnopenable` | Error | `Open` dispatch fails (no format matches, or the matched reader errors). Wrapped reason in `Message`; `Report.Format == FormatUnknown`. |
| `CheckOffsetsOutOfBounds` | Error | Any tile/strip/frame `offset+length` points past EOF, overflows, or is negative. The headline check — catches truncation and dangling pointers that `Open` silently passes. |
| `CheckTileGridMismatch` | Error | The tile offset-array length disagrees with `ceil(W/tw)·ceil(H/th)`, or a level reports zero dims / zero tiles. |
| `CheckNonConformantFormat` | Error | Format-specific spec violations the reader already detects (e.g. COG-WSI `validateGhost`/`validateIFDs`). Surfaced as findings rather than a bare sentinel. _(v1: not emitted by any reader — COG-WSI conformance violations are fatal at Open and surface as `unopenable`; the code is reserved for future per-format soft checks.)_ |
| `CheckInconsistentPyramid` | Warning | Level dimensions not monotonically shrinking, or downsample ratios drift beyond the existing generic-TIFF tolerance. |
| `CheckMissingMetadata` | Warning | Expected-but-optional fields absent (e.g. MPP, objective power). |
| `CheckOrphanIFD` | Info | Unreferenced IFDs present (generic-TIFF already flags these as `DirOther`). Legal-but-unusual. _(v1: not emitted — orphan-IFD detection is deferred until a reader reachability map is wired; the code is reserved.)_ |

The catalog is open-ended — new `CheckCode` constants are additive.

## The four fences — what "valid" does and does not mean

"Passes Validate" means the file is *well-formed per opentile-go's reader*: it
opens, its declared structure is internally consistent (grid math, pyramid shape,
required metadata present), and its tile/byte pointers land in-bounds.

Four things it does **not** mean:

1. **Valid ≠ correct pixels.** Structural validation cannot catch "decodes fine
   but shows garbage" — e.g. a BIF serpentine-descramble bug or an LZW writer
   corruption that produces a structurally-perfect but visually-wrong label. Those
   files are *structurally perfect*. Catching them requires pixel comparison against
   an independent oracle (`tests/oracle` / tifffile / openslide / dciodvfy), which
   is fundamentally outside a self-contained `Validate()`. Even the reserved Tier 2
   only catches "won't decode," never "decodes wrong."

2. **Valid ≠ spec-conformant.** Validate checks against opentile-go's *own model*
   of each format, not the format spec's full MUST/SHOULD list. True DICOM IOD
   conformance is `dciodvfy`; OME-XML schema conformance is `ome-types`. Validate
   does not reimplement them.

3. **As-detected, not as-intended.** A broken SVS that no longer sniffs as SVS
   and falls through to the generic-TIFF catch-all can report OK *as
   generic-TIFF*. `Report.Format` names the detected format; matching that against
   intent is the caller's job (there is deliberately no expected-format parameter).

4. **Not a repair tool.** Validate reports nature; it never mutates a file.

## Example

```go
import (
    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
)

rep, err := opentile.ValidateFile("slide.svs")
if err != nil {
    // genuinely unreadable input (path missing, I/O fault)
    log.Fatal(err)
}
if !rep.OK() {
    for _, f := range rep.Findings {
        fmt.Printf("%s [%s] %s (x%d)\n", f.Severity, f.Code, f.Message, f.Count)
    }
}
fmt.Println("detected format:", rep.Format)
```

On an already-open slide:

```go
rep := slide.Validate()
if !rep.OK() { /* ... */ }
```

## Tier 2 (reserved, not built)

Pixel-decode checks (catching "won't decode" corrupt bitstreams) are reserved as
an additive seam via `...ValidateOption`. They are not built in v1; the option
type is defined but ships with zero concrete options. A future
`WithTileDecodeCheck(sample)` adds Tier 2 without changing any of the three entry
point signatures or the `Finding`/`Report` shapes.
