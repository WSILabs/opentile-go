package ndpi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestWithNDPISynthesizedLabelDisabled(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide, opentile.WithNDPISynthesizedLabel(false))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	for _, a := range tiler.Associated() {
		if a.Type() == "label" {
			t.Errorf("expected no synthesized label with WithNDPISynthesizedLabel(false), got %s", a.Type())
		}
	}
}

func TestWithNDPISynthesizedLabelDefaultEnabled(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	hasLabel := false
	for _, a := range tiler.Associated() {
		if a.Type() == "label" {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Error("default (no option): expected synthesized label")
	}
}
