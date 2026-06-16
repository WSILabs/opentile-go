package opentile

import (
	"fmt"
	"io"
	"os"
	"sort"
)

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
	code           CheckCode
	pyramid, level int
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

// findings returns the rolled-up findings in a deterministic order: by severity
// (Error first), then code, then level.
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

// ValidateOption is the additive seam for future decode-based checks (Tier 2).
// v1 ships zero options.
type ValidateOption func(*validateConfig)

type validateConfig struct{}

// ValidateFile opens path and validates it (tiers 0 + 1). The returned error is
// operational only (path missing / unreadable); a file that fails to open or is
// structurally broken yields a *Report whose findings describe the problem.
//
// We stat first so a genuinely absent/unreadable path is an operational error,
// while a path that exists but fails to open/parse becomes a CheckUnopenable
// finding. OpenFile handles both the single-file and DICOM series-directory
// cases.
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
		if l.Size.W <= 0 || l.Size.H <= 0 ||
			l.TileSize.W <= 0 || l.TileSize.H <= 0 {
			p.Flag(CheckTileGridMismatch, pyramid, l.Index,
				fmt.Sprintf("level %d has degenerate size %dx%d / tile %dx%d",
					l.Index, l.Size.W, l.Size.H, l.TileSize.W, l.TileSize.H))
			continue
		}
		wantW := ceilDiv(l.Size.W, l.TileSize.W)
		wantH := ceilDiv(l.Size.H, l.TileSize.H)
		if l.Grid.W != wantW || l.Grid.H != wantH {
			p.Flag(CheckTileGridMismatch, pyramid, l.Index,
				fmt.Sprintf("level %d grid %dx%d != ceil(size/tile) %dx%d",
					l.Index, l.Grid.W, l.Grid.H, wantW, wantH))
		}
		if l.MPP.IsZero() {
			p.Flag(CheckMissingMetadata, pyramid, l.Index,
				fmt.Sprintf("level %d has no MPP (microns-per-pixel) metadata", l.Index))
		}
	}
	for i := 1; i < len(levels); i++ {
		if levels[i].Size.W > levels[i-1].Size.W ||
			levels[i].Size.H > levels[i-1].Size.H {
			p.Flag(CheckInconsistentPyramid, pyramid, levels[i].Index,
				fmt.Sprintf("level %d (%dx%d) is larger than level %d (%dx%d)",
					levels[i].Index, levels[i].Size.W, levels[i].Size.H,
					levels[i-1].Index, levels[i-1].Size.W, levels[i-1].Size.H))
		}
	}
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
