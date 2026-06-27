package opentile

import "testing"

func TestOverlapModeString(t *testing.T) {
	cases := map[OverlapMode]string{
		OverlapNone:     "none",
		OverlapBordered: "bordered",
		OverlapStitched: "stitched",
		OverlapMode(99): "OverlapMode(99)",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("OverlapMode(%d).String() = %q, want %q", int(m), got, want)
		}
	}
}

func TestOverlapNoneIsZeroValue(t *testing.T) {
	var m OverlapMode
	if m != OverlapNone {
		t.Errorf("zero value = %v, want OverlapNone", m)
	}
}

func TestLevelOverlapModeField(t *testing.T) {
	// The OverlapMode field exists and round-trips. Overlapping is set by
	// readers to (OverlapMode != OverlapNone); that the two stay in sync is a
	// reader invariant verified in the format-package tests (Task 5/7), not
	// here — this struct does not derive Overlapping.
	l := Level{OverlapMode: OverlapBordered, Overlapping: true, TileOverlap: Point{X: 1, Y: 1}}
	if l.OverlapMode != OverlapBordered {
		t.Errorf("OverlapMode = %v, want OverlapBordered", l.OverlapMode)
	}
	if !l.Overlapping {
		t.Error("Overlapping = false, want true")
	}
	var zero Level
	if zero.OverlapMode != OverlapNone {
		t.Errorf("zero Level OverlapMode = %v, want OverlapNone", zero.OverlapMode)
	}
}
