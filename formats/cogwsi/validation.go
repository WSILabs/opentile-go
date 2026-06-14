package cogwsi

import (
	"fmt"
	"sort"

	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Expected ghost-area values per COG-WSI spec §3 (inherited from
// the GDAL COG ghost-area convention). validateGhost rejects any
// drift from these — a COG-WSI-tagged file that mutates them is
// not spec-conformant.
const (
	expectedLayout                   = "IFDS_BEFORE_DATA"
	expectedBlockOrder               = "ROW_MAJOR"
	expectedBlockLeader              = "SIZE_AS_UINT4"
	expectedBlockTrailer             = "LAST_4_BYTES_REPEATED"
	expectedKnownIncompatibleEdition = "NO"
)

// validWSIImageTypes is the closed enum the COG-WSI spec §5.2
// declares for the WSIImageType tag. Any other value violates
// the spec.
var validWSIImageTypes = map[string]struct{}{
	"pyramid":   {},
	"label":     {},
	"macro":     {},
	"thumbnail": {},
	"overview":  {},
}

// validateGhost checks the parsed ghost area satisfies the five
// required-value invariants per spec §3. Returns nil on conformance;
// an ErrNotConformantCOGWSI-wrapped error on the first violation.
func validateGhost(g cog.GhostArea) error {
	checks := []struct {
		key, got, want string
	}{
		{"LAYOUT", g.Layout, expectedLayout},
		{"BLOCK_ORDER", g.BlockOrder, expectedBlockOrder},
		{"BLOCK_LEADER", g.BlockLeader, expectedBlockLeader},
		{"BLOCK_TRAILER", g.BlockTrailer, expectedBlockTrailer},
		{"KNOWN_INCOMPATIBLE_EDITION", g.KnownIncompatibleEdition, expectedKnownIncompatibleEdition},
	}
	for _, c := range checks {
		if c.got != c.want {
			return fmt.Errorf("%w: ghost area %s=%q, want %q",
				ErrNotConformantCOGWSI, c.key, c.got, c.want)
		}
	}
	return nil
}

// validateIFDs runs the per-IFD spec checks per §5.2 + §6:
//   - every IFD carries WSIImageType set to a value in the closed enum
//   - pyramid IFDs are tiled (no strips)
//   - WSILevelIndex on pyramid IFDs is contiguous from 0 to N-1
//     where N matches the WSILevelCount tag on L0
//   - IFD ordering: every pyramid IFD precedes every associated IFD
func validateIFDs(pages []*tiff.Page) error {
	if len(pages) == 0 {
		return fmt.Errorf("%w: file has no IFDs", ErrNotConformantCOGWSI)
	}

	// Pass 1: WSIImageType presence + enum membership, classify each
	// IFD as pyramid vs associated. Track pyramid level indices.
	var pyrs []pyrEntry
	pyramidSeenAt := -1
	for i, p := range pages {
		wt, ok := p.WSIImageType()
		if !ok {
			return fmt.Errorf("%w: IFD %d missing WSIImageType tag",
				ErrNotConformantCOGWSI, i)
		}
		if _, ok := validWSIImageTypes[wt]; !ok {
			return fmt.Errorf("%w: IFD %d has WSIImageType=%q (not in spec enum pyramid|label|macro|thumbnail|overview)",
				ErrNotConformantCOGWSI, i, wt)
		}
		if wt != "pyramid" {
			continue
		}
		// pyramid IFD checks
		if !pageIsTiled(p) {
			return fmt.Errorf("%w: IFD %d is WSIImageType=pyramid but not tiled (TileWidth/TileLength absent)",
				ErrNotConformantCOGWSI, i)
		}
		lvl, ok := p.WSILevelIndex()
		if !ok {
			return fmt.Errorf("%w: IFD %d is WSIImageType=pyramid but lacks WSILevelIndex",
				ErrNotConformantCOGWSI, i)
		}
		pyrs = append(pyrs, pyrEntry{i, lvl})
		pyramidSeenAt = i
	}

	if len(pyrs) == 0 {
		return fmt.Errorf("%w: no pyramid IFDs present", ErrNotConformantCOGWSI)
	}

	// Pass 2: IFD ordering — every associated IFD must appear after
	// the last pyramid IFD per spec §6.
	for i, p := range pages {
		if i > pyramidSeenAt {
			break
		}
		wt, _ := p.WSIImageType()
		if wt != "" && wt != "pyramid" {
			return fmt.Errorf("%w: IFD %d is associated (WSIImageType=%q) but appears before last pyramid IFD %d",
				ErrNotConformantCOGWSI, i, wt, pyramidSeenAt)
		}
	}

	// Pass 3: WSILevelIndex contiguous from 0 to WSILevelCount-1.
	// WSILevelCount is read from the first pyramid IFD (per spec
	// §5.2, every pyramid IFD carries WSILevelCount with the same
	// value; we anchor on the first).
	firstPyr := pages[pyrs[0].ifdIndex]
	declaredCount, ok := firstPyr.WSILevelCount()
	if !ok {
		return fmt.Errorf("%w: first pyramid IFD %d lacks WSILevelCount",
			ErrNotConformantCOGWSI, pyrs[0].ifdIndex)
	}
	if int(declaredCount) != len(pyrs) {
		return fmt.Errorf("%w: WSILevelCount=%d but %d pyramid IFDs present",
			ErrNotConformantCOGWSI, declaredCount, len(pyrs))
	}
	sorted := make([]pyrEntry, len(pyrs))
	copy(sorted, pyrs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].level < sorted[j].level })
	for i, e := range sorted {
		if e.level != uint32(i) {
			return fmt.Errorf("%w: WSILevelIndex set is %v, want contiguous 0..%d",
				ErrNotConformantCOGWSI, collectLevels(sorted), len(sorted)-1)
		}
	}
	return nil
}

// pyrEntry pairs an IFD index with its declared WSILevelIndex —
// used during pyramid validation in [validateIFDs].
type pyrEntry struct {
	ifdIndex int
	level    uint32
}

func collectLevels(es []pyrEntry) []uint32 {
	out := make([]uint32, len(es))
	for i, e := range es {
		out[i] = e.level
	}
	return out
}

// pageIsTiled reports whether the IFD has TileWidth + TileLength
// tags (== tiled storage, not stripped). internal/tiff doesn't
// expose an IsTiled() helper as of v0.19; we read the tags
// directly via ScalarU32.
func pageIsTiled(p *tiff.Page) bool {
	if _, ok := p.TileWidth(); !ok {
		return false
	}
	if _, ok := p.TileLength(); !ok {
		return false
	}
	return true
}
