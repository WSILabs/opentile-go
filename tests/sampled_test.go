package tests

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestSamplePositionsDeduplicates(t *testing.T) {
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
	got := SamplePositions(
		opentile.Size{W: 10, H: 10},
		opentile.Size{W: 9999, H: 9999},
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
		opentile.Size{W: 10000, H: 10000},
		opentile.Size{W: 1000, H: 1000},
	)
	for _, p := range got {
		if p.Reason == "right-edge mid-column; OOB fill in x via 'right of image' callback" {
			t.Error("Layer 2 right-edge should not fire when image is exactly divisible")
		}
	}
}
