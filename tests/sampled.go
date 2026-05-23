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

// SamplePositions returns up to ~16 deliberately-chosen tile positions for
// a level: nine corner/diagonal/near-edge positions (Layer 1, position-
// based) plus circumstance-based positions (Layer 2) derived from the
// level's geometry. Duplicates collapse — bottom-right corner appears
// only once even though it's reached via both layers.
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
	if tileSize.W > 0 && imageSize.W%tileSize.W != 0 {
		cands = append(cands, SamplePosition{
			X: grid.W - 1, Y: grid.H / 2,
			Reason: "right-edge mid-column; OOB fill in x via 'right of image' callback",
		})
	}
	if tileSize.H > 0 && imageSize.H%tileSize.H != 0 {
		cands = append(cands, SamplePosition{
			X: grid.W / 2, Y: grid.H - 1,
			Reason: "bottom-edge mid-row; OOB fill in y via 'below image' callback",
		})
	}

	// Deduplicate by (X, Y) — first reason wins (Layer 1 takes priority).
	seen := make(map[[2]int]bool)
	out := make([]SamplePosition, 0, len(cands))
	for _, p := range cands {
		if p.X < 0 || p.Y < 0 || p.X >= grid.W || p.Y >= grid.H {
			continue
		}
		key := [2]int{p.X, p.Y}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// SampleKey formats a sampled-tile lookup key. The Reason isn't part of the
// key — it's metadata.
func SampleKey(level int, p SamplePosition) string {
	return fmt.Sprintf("%d:%d:%d", level, p.X, p.Y)
}
