package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// corpusDir returns the OPENTILE_TESTDIR path, or skips if unset.
func corpusDir(t *testing.T) string {
	t.Helper()
	d := os.Getenv("OPENTILE_TESTDIR")
	if d == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	return d
}

func TestValidateNDPIFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean NDPI fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatNDPI {
		t.Fatalf("Format = %q, want ndpi", rep.Format)
	}
}

func TestValidatePhilipsTIFFFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "philips-tiff", "Philips-1.tiff")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean Philips TIFF fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatPhilipsTIFF {
		t.Fatalf("Format = %q, want philips-tiff", rep.Format)
	}
}

func TestValidateLeicaSCNFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "scn", "Leica-1.scn")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean Leica SCN fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatLeicaSCN {
		t.Fatalf("Format = %q, want leica-scn", rep.Format)
	}
}

func TestValidateOMETIFFFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "ome-tiff", "Leica-1.ome.tiff")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean OME-TIFF fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatOMETIFF {
		t.Fatalf("Format = %q, want ome-tiff", rep.Format)
	}
}

func TestValidateBIFFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "bif", "Ventana-1.bif")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean BIF fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatBIF {
		t.Fatalf("Format = %q, want bif", rep.Format)
	}
}

// TestValidateCOGWSIFixtureOK confirms that ValidateFile on a COG-WSI file
// returns OK and correctly reports FormatCOGWSI. COG-WSI delegates its
// tile-byte machinery to an inner generictiff reader via UnwrapReader(); the
// validatorOfAny chain therefore reaches the generictiff Validate hook
// automatically — no separate cogwsi/validate.go is required. All COG-WSI
// conformance checks (ghost-area invariants, WSIImageType enum, IFD ordering)
// are fatal at Open time (ErrNotConformantCOGWSI), so a successfully-opened
// file carries no soft post-open nonconformant findings.
func TestValidateCOGWSIFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean cog-wsi fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatCOGWSI {
		t.Fatalf("Format = %q, want cog-wsi", rep.Format)
	}
}

// TestValidateHamamatsu1NDPIOK validates the 64-bit-offset NDPI fixture
// (Hamamatsu-1.ndpi) and confirms no false CheckOffsetsOutOfBounds are
// emitted — this is the key regression test for the 64-bit accessor fix.
func TestValidateHamamatsu1NDPIOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "ndpi", "Hamamatsu-1.ndpi")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("64-bit-offset NDPI fixture not OK (false positive?): %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatNDPI {
		t.Fatalf("Format = %q, want ndpi", rep.Format)
	}
}

// TestValidateIFEFixtureOK validates the cervix_2x_jpeg.iris IFE fixture and
// confirms no CheckOffsetsOutOfBounds (or other Error-severity) findings.
// The fixture is large (~2 GB) so the test is skipped when OPENTILE_TESTDIR
// is unset or the file is absent.
func TestValidateIFEFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "ife", "cervix_2x_jpeg.iris")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean IFE fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatIFE {
		t.Fatalf("Format = %q, want ife", rep.Format)
	}
}

// TestValidateSZIFixtureOK validates the CMU-1.szi SZI fixture and confirms
// no CheckOffsetsOutOfBounds (or other Error-severity) findings.
// The test is skipped when OPENTILE_TESTDIR is unset or the file is absent.
func TestValidateSZIFixtureOK(t *testing.T) {
	p := filepath.Join(corpusDir(t), "szi", "CMU-1.szi")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(p)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean SZI fixture not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatSZI {
		t.Fatalf("Format = %q, want szi", rep.Format)
	}
}

// TestValidateDICOMFixtureOK validates a known-good DICOM WSM series
// (scan_621_grundium_dicom) and confirms no CheckOffsetsOutOfBounds
// (or other Error-severity) findings. DICOM is multi-file, so the test
// passes the series directory path to ValidateFile. The test is skipped
// when OPENTILE_TESTDIR is unset or the fixture directory is absent.
func TestValidateDICOMFixtureOK(t *testing.T) {
	dir := filepath.Join(corpusDir(t), "dicom", "scan_621_grundium_dicom")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	rep, err := opentile.ValidateFile(dir)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("clean DICOM series not OK: %+v", rep.Findings)
	}
	if rep.Format != opentile.FormatDICOM {
		t.Fatalf("Format = %q, want dicom", rep.Format)
	}
}
