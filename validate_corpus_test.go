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
	p := filepath.Join(corpusDir(t), "leica-scn", "Leica-1.scn")
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
