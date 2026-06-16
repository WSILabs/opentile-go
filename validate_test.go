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

func TestProbeRollupCountOnly(t *testing.T) {
	p := newProbe(1000)
	// Three occurrences of the same (Code, level) → one finding, Count 3.
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (1,1) offset past EOF")
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (2,1) offset past EOF")
	p.Flag(CheckOffsetsOutOfBounds, 0, 2, "tile (3,1) offset past EOF")
	// A different level → separate finding.
	p.Flag(CheckOffsetsOutOfBounds, 0, 3, "tile (0,0) offset past EOF")
	// A whole-file finding (locus -1).
	p.Flag(CheckOrphanIFD, -1, -1, "1 unreferenced IFD")

	got := p.findings()
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3 (rolled up by (Code,level))", len(got))
	}

	var oob2 *Finding
	for i := range got {
		if got[i].Code == CheckOffsetsOutOfBounds && got[i].Level == 2 {
			oob2 = &got[i]
		}
	}
	if oob2 == nil {
		t.Fatal("missing rolled-up OffsetsOutOfBounds finding for level 2")
	}
	if oob2.Count != 3 {
		t.Fatalf("level-2 finding Count = %d, want 3", oob2.Count)
	}
	if oob2.Severity != Error {
		t.Fatalf("OffsetsOutOfBounds severity = %v, want Error", oob2.Severity)
	}
}

func TestProbeSize(t *testing.T) {
	if got := newProbe(4242).Size(); got != 4242 {
		t.Fatalf("Size() = %d, want 4242", got)
	}
}
