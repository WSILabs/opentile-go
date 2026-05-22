package opentile

import "testing"

func TestSetMPPSymmetric_Equal(t *testing.T) {
	m := Metadata{MicronsPerPixelX: 0.4, MicronsPerPixelY: 0.4}
	m.SetMPPSymmetric()
	if m.MicronsPerPixel != 0.4 {
		t.Errorf("MicronsPerPixel = %v, want 0.4", m.MicronsPerPixel)
	}
}

func TestSetMPPSymmetric_Asymmetric(t *testing.T) {
	m := Metadata{MicronsPerPixelX: 0.4, MicronsPerPixelY: 0.5}
	m.MicronsPerPixel = 0.45 // pre-set; should be cleared
	m.SetMPPSymmetric()
	if m.MicronsPerPixel != 0 {
		t.Errorf("asymmetric: MicronsPerPixel = %v, want 0", m.MicronsPerPixel)
	}
}

func TestSetMPPSymmetric_OneZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y float64
	}{
		{"X zero", 0, 0.4},
		{"Y zero", 0.4, 0},
		{"both zero", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Metadata{MicronsPerPixelX: tc.x, MicronsPerPixelY: tc.y}
			m.SetMPPSymmetric()
			if m.MicronsPerPixel != 0 {
				t.Errorf("MicronsPerPixel = %v, want 0", m.MicronsPerPixel)
			}
		})
	}
}

func TestSetProperty_NilMap(t *testing.T) {
	var m Metadata // Properties nil
	m.SetProperty("foo", "bar")
	if got := m.Properties["foo"]; got != "bar" {
		t.Errorf("Properties[foo] = %q, want bar", got)
	}
}

func TestSetProperty_Overwrite(t *testing.T) {
	m := Metadata{Properties: map[string]string{"foo": "old"}}
	m.SetProperty("foo", "new")
	if got := m.Properties["foo"]; got != "new" {
		t.Errorf("Properties[foo] = %q, want new", got)
	}
}

func TestPropertyConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"PropertyCaseNumber", PropertyCaseNumber, "case-number"},
		{"PropertyUserName", PropertyUserName, "user-name"},
		{"PropertyScannedAreaMM2", PropertyScannedAreaMM2, "scanned-area-mm2"},
		{"PropertyScanDurationSec", PropertyScanDurationSec, "scan-duration-seconds"},
		{"PropertyComments", PropertyComments, "comments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestMetadataWriter_ZeroValue(t *testing.T) {
	var m Metadata
	if m.Writer != "" {
		t.Errorf("zero-value Writer = %q, want empty", m.Writer)
	}
}

func TestMetadataWriter_SetGet(t *testing.T) {
	m := Metadata{Writer: "Aperio Image Library v11.2.1"}
	if m.Writer != "Aperio Image Library v11.2.1" {
		t.Errorf("Writer = %q", m.Writer)
	}
}
