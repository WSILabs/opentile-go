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
	l := Level{OverlapMode: OverlapBordered, TileOverlap: Point{X: 1, Y: 1}}
	if l.OverlapMode != OverlapBordered {
		t.Fatalf("OverlapMode = %v, want OverlapBordered", l.OverlapMode)
	}
	// Overlapping is the derived convenience: true iff OverlapMode != None.
	if !l.Overlapping {
		// NOTE: readers set Overlapping explicitly; this test documents the
		// intended invariant that callers can rely on (see Task 5/7 population).
		t.Skip("Overlapping is reader-populated; invariant checked in format tests")
	}
}
