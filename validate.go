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
