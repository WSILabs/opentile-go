package opentile

import "sort"

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
