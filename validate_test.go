package opentile

import "testing"

func TestReportOK(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Severity: Warning, Code: CheckMissingMetadata},
		{Severity: Info, Code: CheckOrphanIFD},
	}}
	if !r.OK() {
		t.Fatal("OK() should be true with no Error findings")
	}
	r.Findings = append(r.Findings, Finding{Severity: Error, Code: CheckOffsetsOutOfBounds})
	if r.OK() {
		t.Fatal("OK() should be false once an Error finding is present")
	}
}

func TestReportWorst(t *testing.T) {
	if got := (&Report{}).Worst(); got != Info {
		t.Fatalf("empty report Worst() = %v, want Info", got)
	}
	r := &Report{Findings: []Finding{{Severity: Info}, {Severity: Warning}}}
	if got := r.Worst(); got != Warning {
		t.Fatalf("Worst() = %v, want Warning", got)
	}
}

func TestFormatUnknownIsZeroValue(t *testing.T) {
	if FormatUnknown != "" {
		t.Fatalf("FormatUnknown = %q, want empty string (the zero value)", FormatUnknown)
	}
}
