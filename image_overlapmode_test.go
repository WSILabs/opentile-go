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
