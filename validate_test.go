package opentile

import (
	"bytes"
	"testing"
)

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

// fakeValidatorReader implements just enough to test validatorOf discovery
// through the UnwrapReader chain.
type fakeValidatorReader struct {
	flagged bool
}

func (f *fakeValidatorReader) Validate(p *ValidationProbe) {
	f.flagged = true
	p.Flag(CheckNonConformantFormat, -1, -1, "fake reader says non-conformant")
}

// validatorWrapper mimics fileCloser/mmapCloser: it delegates via UnwrapReader.
type validatorWrapper struct{ inner any }

func (w validatorWrapper) UnwrapReader() any { return w.inner }

func TestValidatorOfWalksUnwrapChain(t *testing.T) {
	fv := &fakeValidatorReader{}
	got, ok := validatorOfAny(validatorWrapper{inner: validatorWrapper{inner: fv}})
	if !ok || got == nil {
		t.Fatal("validatorOfAny should find the validator through the unwrap chain")
	}
	got.Validate(newProbe(0))
	if !fv.flagged {
		t.Fatal("discovered validator's Validate was not called")
	}
}

func TestValidatorOfMissing(t *testing.T) {
	if _, ok := validatorOfAny(struct{}{}); ok {
		t.Fatal("a non-validator with no UnwrapReader should yield ok=false")
	}
}

func TestValidateNonPositiveSize(t *testing.T) {
	if _, err := Validate(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("size<=0 should be an operational error")
	}
}

func TestValidateFileUnopenable(t *testing.T) {
	if _, err := ValidateFile("/nonexistent/path/zzz.svs"); err == nil {
		t.Fatal("ValidateFile on a missing path should return an operational error")
	}
}

func TestValidateUnopenableBytes(t *testing.T) {
	garbage := bytes.Repeat([]byte{0xAB}, 512)
	rep, err := Validate(bytes.NewReader(garbage), int64(len(garbage)))
	if err != nil {
		t.Fatalf("Validate returned operational error %v; want a report with Unopenable", err)
	}
	if rep.Format != FormatUnknown {
		t.Fatalf("Format = %q, want FormatUnknown", rep.Format)
	}
	if rep.OK() {
		t.Fatal("garbage should not be OK")
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Code != CheckUnopenable {
		t.Fatalf("findings = %+v, want exactly one CheckUnopenable", rep.Findings)
	}
}

func TestEngineGridMismatchFromLevels(t *testing.T) {
	lvls := []Level{{
		Index: 0, PyramidIndex: 0,
		Size:     Size{W: 1024, H: 1024},
		TileSize: Size{W: 256, H: 256},
		Grid:     Size{W: 2, H: 2}, // wrong: should be 4x4
		MPP:      MPP{X: 0.25, Y: 0.25},
	}}
	p := newProbe(0)
	checkLevelGeometry(p, 0, lvls, false)
	got := p.findings()
	if len(got) != 1 || got[0].Code != CheckTileGridMismatch {
		t.Fatalf("findings = %+v, want one CheckTileGridMismatch", got)
	}
}

// TestEngineMPPSlideLevelSuppresses: a level with zero Level.MPP must NOT
// produce a missing-metadata finding when the slide carries MPP at the slide
// level (Metadata.MPP). Guards the GH #55 false-positive on readers that
// populate only the slide-level MPP (ndpi/leica-scn/dicom/generic-tiff/...).
func TestEngineMPPSlideLevelSuppresses(t *testing.T) {
	lvls := []Level{{
		Index: 0, Size: Size{W: 512, H: 512}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 2, H: 2},
		// Level.MPP intentionally zero.
	}}
	p := newProbe(0)
	checkLevelGeometry(p, 0, lvls, true /* slide has MPP */)
	for _, f := range p.findings() {
		if f.Code == CheckMissingMetadata {
			t.Fatalf("missing-metadata flagged despite slide-level MPP: %+v", f)
		}
	}
}

// TestEngineMPPMissingEverywhere: when neither the slide nor any level carries
// MPP, exactly one missing-metadata finding fires, once per pyramid (locus -1),
// not once per level.
func TestEngineMPPMissingEverywhere(t *testing.T) {
	lvls := []Level{
		{Index: 0, Size: Size{W: 512, H: 512}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 2, H: 2}},
		{Index: 1, Size: Size{W: 256, H: 256}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 1, H: 1}},
	}
	p := newProbe(0)
	checkLevelGeometry(p, 0, lvls, false /* no slide MPP */)
	var mm []Finding
	for _, f := range p.findings() {
		if f.Code == CheckMissingMetadata {
			mm = append(mm, f)
		}
	}
	if len(mm) != 1 {
		t.Fatalf("want exactly one missing-metadata finding, got %d: %+v", len(mm), mm)
	}
	if mm[0].Level != -1 {
		t.Fatalf("missing-metadata locus level = %d, want -1 (per-pyramid)", mm[0].Level)
	}
}

func TestEngineInconsistentPyramid(t *testing.T) {
	lvls := []Level{
		{Index: 0, Size: Size{W: 512, H: 512}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 2, H: 2}},
		{Index: 1, Size: Size{W: 1024, H: 1024}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 4, H: 4}},
	}
	p := newProbe(0)
	checkLevelGeometry(p, 0, lvls, true)
	found := false
	for _, f := range p.findings() {
		if f.Code == CheckInconsistentPyramid {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CheckInconsistentPyramid, got %+v", p.findings())
	}
}
