package opentile

import "testing"

// TestMPP_IsZero: zero value and non-zero MPP.
func TestMPP_IsZero(t *testing.T) {
	if z := (MPP{}); !z.IsZero() {
		t.Errorf("MPP{}.IsZero() = false, want true")
	}
	if nz := (MPP{X: 0.4, Y: 0.4}); nz.IsZero() {
		t.Errorf("MPP{0.4,0.4}.IsZero() = true, want false")
	}
	if asymX := (MPP{X: 0.4}); asymX.IsZero() {
		t.Errorf("MPP{X:0.4}.IsZero() = true, want false")
	}
}

// TestMPP_Symmetric: equal X==Y returns X; asymmetric returns 0.
func TestMPP_Symmetric(t *testing.T) {
	if got := (MPP{X: 0.4, Y: 0.4}).Symmetric(); got != 0.4 {
		t.Errorf("symmetric MPP.Symmetric() = %v, want 0.4", got)
	}
	if got := (MPP{X: 0.4, Y: 0.5}).Symmetric(); got != 0 {
		t.Errorf("asymmetric MPP.Symmetric() = %v, want 0", got)
	}
	if got := (MPP{}).Symmetric(); got != 0 {
		t.Errorf("zero MPP.Symmetric() = %v, want 0", got)
	}
}

// TestMetadata_MPP_Direct: direct MPP assignment.
func TestMetadata_MPP_Direct(t *testing.T) {
	m := Metadata{MPP: MPP{X: 0.4, Y: 0.4}}
	if m.MPP.X != 0.4 || m.MPP.Y != 0.4 {
		t.Errorf("MPP = %v, want {0.4, 0.4}", m.MPP)
	}
	if got := m.MPP.Symmetric(); got != 0.4 {
		t.Errorf("MPP.Symmetric() = %v, want 0.4", got)
	}
}

// TestMetadata_MPP_Asymmetric: asymmetric MPP has Symmetric() == 0.
func TestMetadata_MPP_Asymmetric(t *testing.T) {
	m := Metadata{MPP: MPP{X: 0.4, Y: 0.5}}
	if got := m.MPP.Symmetric(); got != 0 {
		t.Errorf("asymmetric MPP.Symmetric() = %v, want 0", got)
	}
}

// TestSetMPPSymmetric_NoOp: the shim method is a no-op in v1.0.
func TestSetMPPSymmetric_NoOp(t *testing.T) {
	m := Metadata{MPP: MPP{X: 0.4, Y: 0.4}}
	m.SetMPPSymmetric() // should not panic or change state
	if m.MPP.X != 0.4 || m.MPP.Y != 0.4 {
		t.Errorf("SetMPPSymmetric changed MPP to %v", m.MPP)
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
