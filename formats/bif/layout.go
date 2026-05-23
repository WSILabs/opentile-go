package bif

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// ifdRole identifies the semantic role of a BIF IFD inferred from
// its ImageDescription string. Spec §5.3 mandates
// classification-by-content (not by IFD index), since OS-1 (legacy
// iScan) has a different IFD layout from the spec-described
// Label/Probability/Pyramid layout.
type ifdRole int

const (
	ifdRoleUnknown     ifdRole = iota // ImageDescription absent or unrecognised
	ifdRoleLabel                      // "Label_Image" (DP) or "Label Image" (legacy)
	ifdRoleProbability                // "Probability_Image" (DP only)
	ifdRoleThumbnail                  // "Thumbnail" (legacy only)
	ifdRolePyramid                    // "level=N mag=M quality=Q"
)

// classifiedIFD bundles a single IFD's inferred role with the
// numeric attributes parsed out of pyramid-level ImageDescriptions.
type classifiedIFD struct {
	Index   int        // IFD index in file.Pages() order
	Role    ifdRole    // semantic role
	Level   int        // parsed `level=N` for pyramid; -1 otherwise
	Mag     float64    // parsed `mag=M` for pyramid; 0 otherwise
	Quality int        // parsed `quality=Q` for pyramid; 0 otherwise
	Page    *tiff.Page // shortcut into file.Pages()[Index]
}

// classifyIFD inspects p.ImageDescription() and returns the parsed
// classification. Pyramid descriptions follow the spec §5.3 token
// grammar:
//
//	"level=" SP "mag=" SP "quality="
//
// where each token is `key=value` separated by a single ASCII space.
// Lenient parsing: missing or malformed tokens fall through to
// zero-value numerics (the level / mag / quality defaults), but the
// role is still ifdRolePyramid as long as the description starts
// with "level=".
func classifyIFD(idx int, p *tiff.Page) classifiedIFD {
	out := classifiedIFD{Index: idx, Role: ifdRoleUnknown, Level: -1, Page: p}
	desc, ok := p.ImageDescription()
	if !ok {
		return out
	}
	desc = strings.TrimSpace(desc)
	switch desc {
	case "Label_Image", "Label Image":
		out.Role = ifdRoleLabel
		return out
	case "Probability_Image":
		out.Role = ifdRoleProbability
		return out
	case "Thumbnail":
		out.Role = ifdRoleThumbnail
		return out
	}
	if strings.HasPrefix(desc, "level=") {
		out.Role = ifdRolePyramid
		parsePyramidDescription(desc, &out)
	}
	return out
}

// parsePyramidDescription walks the SPACE-delimited `key=value`
// tokens and populates out.Level / Mag / Quality. Tokens without an
// `=` or with unrecognised keys are silently ignored. Malformed
// numeric values leave the field at its zero value.
func parsePyramidDescription(desc string, out *classifiedIFD) {
	for _, tok := range strings.Split(desc, " ") {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		key, val := tok[:eq], tok[eq+1:]
		switch key {
		case "level":
			if v, err := strconv.Atoi(val); err == nil {
				out.Level = v
			}
		case "mag":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				out.Mag = v
			}
		case "quality":
			if v, err := strconv.Atoi(val); err == nil {
				out.Quality = v
			}
		}
	}
}

// inventory walks every IFD in file.Pages(), classifies each, and
// returns:
//   - levels sorted by parsed `level=N` ascending (so caller can
//     index by pyramid level directly).
//   - associated containing label / probability / thumbnail entries
//     in IFD-index order.
//   - unknown a slice of any IFDs that didn't match a known role
//     (callers may log them; lenient — spec says nothing about them
//     but we don't want to crash on a future format extension).
func inventory(file *tiff.File) (levels, associated, unknown []classifiedIFD, err error) {
	for i, p := range file.Pages() {
		c := classifyIFD(i, p)
		switch c.Role {
		case ifdRoleLabel, ifdRoleProbability, ifdRoleThumbnail:
			associated = append(associated, c)
		case ifdRolePyramid:
			levels = append(levels, c)
		default:
			unknown = append(unknown, c)
		}
	}
	if len(levels) == 0 {
		return nil, nil, nil, fmt.Errorf("bif: no pyramid levels found (no IFDs with ImageDescription matching `level=N mag=M quality=Q`)")
	}
	// Stable sort by Level ascending. Stable so IFDs with the same
	// parsed level (anomalous; shouldn't happen) preserve their
	// IFD-index order for deterministic behaviour.
	sortLevelsByPyramidIndex(levels)
	return levels, associated, unknown, nil
}

// sortLevelsByPyramidIndex sorts levels in-place by Level ascending.
// Insertion sort is fine — pyramid depth is bounded (≤ 10 in our
// fixtures, ≤ 20 in any plausible BIF).
func sortLevelsByPyramidIndex(levels []classifiedIFD) {
	for i := 1; i < len(levels); i++ {
		for j := i; j > 0 && levels[j-1].Level > levels[j].Level; j-- {
			levels[j-1], levels[j] = levels[j], levels[j-1]
		}
	}
}
