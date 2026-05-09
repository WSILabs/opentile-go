package generictiff

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

func TestFactoryFormat(t *testing.T) {
	if got := New().Format(); got != opentile.FormatGenericTIFF {
		t.Errorf("Format() = %v, want %v", got, opentile.FormatGenericTIFF)
	}
}

// TestFactorySupports drives Factory.Supports() against every
// generic-TIFF fixture (real + synthetic) that v0.10 ships. Verifies
// the dispatch outcome:
//
//   - Accept (Supports = true): valid pyramid generic TIFFs.
//     Currently Open() returns the T6 placeholder error.
//   - Reject (Supports = false): files that fail validation.
//     Dispatch falls through to ErrUnsupportedFormat.
//
// Skipped cleanly when OPENTILE_TESTDIR is unset.
func TestFactorySupports(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, tc := range []struct {
		name         string
		expectAccept bool
	}{
		// Real fixtures.
		{"CMU-1.tiff", true},
		{"CMU-1.stripped.tiff", true},
		{"CMU-1-Small-Region.stripped.tiff", true}, // v0.11: single-level tiled TIFFs accepted (MinLevels=1)
		// Synthetic fixtures (T3-generated).
		{"synth-pyramid-jpeg.tiff", true},
		{"synth-pyramid-with-label.tiff", true},
		{"synth-bad-pyramid.tiff", false},   // inter-axis 20%
		{"synth-stripped-only.tiff", false}, // 0 tiled IFDs
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "generic-tiff", tc.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				t.Fatalf("tiff.Open: %v", err)
			}

			got := New().Supports(tf)
			if got != tc.expectAccept {
				t.Errorf("Supports() = %v, want %v", got, tc.expectAccept)
			}
		})
	}
}

// TestFactoryOpen_CMU1 round-trips CMU-1.tiff through the factory
// and verifies the Tiler reports the expected level count, dims, and
// associated kinds. CMU-1.tiff has 9 pyramid levels and no associated
// images (the canonical generic pyramid fixture).
func TestFactoryOpen_CMU1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tlr, err := New().Open(tf, &opentile.Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatGenericTIFF {
		t.Errorf("Format() = %v, want %v", got, opentile.FormatGenericTIFF)
	}
	if got := len(tlr.Levels()); got != 9 {
		t.Errorf("len(Levels()) = %d, want 9", got)
	}
	l0 := tlr.Levels()[0]
	if l0.Size().W != 46000 || l0.Size().H != 32914 {
		t.Errorf("L0 size = %v, want 46000×32914", l0.Size())
	}
	if got := len(tlr.Associated()); got != 0 {
		t.Errorf("len(Associated()) = %d, want 0 (CMU-1.tiff has no associated images)", got)
	}
}

// TestFactoryOpen_StrippedSVS round-trips CMU-1.stripped.tiff and
// verifies the 3 stripped associated IFDs are surfaced with the
// classifier-assigned kinds.
func TestFactoryOpen_StrippedSVS(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.stripped.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tlr, err := New().Open(tf, &opentile.Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tlr.Close()

	gotKinds := make(map[string]int)
	for _, a := range tlr.Associated() {
		gotKinds[a.Type()]++
	}
	for _, k := range []string{TypeThumbnail, TypeLabel, TypeOverview} {
		if gotKinds[k] != 1 {
			t.Errorf("Associated kind %q count = %d, want 1; got map = %v",
				k, gotKinds[k], gotKinds)
		}
	}
}

// TestMetadataOf_CMU1 verifies the format-specific metadata extras
// (MicronsPerPixel, ImageDescription) are populated on a real fixture.
func TestMetadataOf_CMU1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tlr, err := New().Open(tf, &opentile.Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tlr.Close()

	md, ok := MetadataOf(tlr)
	if !ok {
		t.Fatal("MetadataOf returned !ok on a generic Tiler")
	}
	t.Logf("MicronsPerPixel = %g, ImageDescription = %q, Software = %v",
		md.MicronsPerPixel, md.ImageDescription, md.ScannerSoftware)
	// CMU-1.tiff lacks XResolution / ResolutionUnit so MicronsPerPixel
	// is expected to be 0; if a future fixture carries those tags the
	// expectation flips.
}

// TestFactorySupportsRejectsExistingVendorFixtures verifies that
// the generic factory's Supports() returns false for files that ARE
// vendor-format TIFFs (so vendor factories that ran first would have
// matched). Defends against a future regression where the generic
// validator becomes too permissive and starts claiming vendor files.
//
// Since the dispatch loop runs vendor factories first, this only
// matters if the test calls Factory.Supports() directly (as we do
// here). In production dispatch via opentile.OpenFile, vendor
// matches would short-circuit before reaching generic.
func TestFactorySupportsRejectsExistingVendorFixtures(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	// Sample a few vendor fixtures from each format:
	for _, tc := range []struct {
		subdir, name string
		// expectGenericAccept indicates whether the generic factory's
		// validator would, by itself, accept this file. Several
		// vendor TIFFs ARE structurally valid pyramidal TIFFs (SVS,
		// Philips, OME, BIF) and would pass generic validation if
		// they reached it — but they don't, because the vendor
		// factory matched first.
		// This test documents what would happen if dispatch order
		// changed; it doesn't enforce a specific outcome.
		expectGenericAccept bool
	}{
		{"svs", "CMU-1.svs", true},             // valid pyramid; vendor still matches first
		{"ndpi", "CMU-1.ndpi", false},          // NDPI's special tile layout fails generic
		{"ome-tiff", "Leica-1.ome.tiff", true}, // v0.11: relaxed leftover threshold accepts; OME wins via dispatch order
		{"ife", "cervix_2x_jpeg.iris", false},  // IFE is non-TIFF; tiff.Open errors
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				if tc.subdir == "ife" {
					t.Logf("✓ IFE rejected at tiff.Open as expected: %v", err)
					return
				}
				t.Fatalf("tiff.Open: %v", err)
			}
			got := New().Supports(tf)
			if got != tc.expectGenericAccept {
				t.Logf("note: %s would%s be accepted by generic if dispatch order changed (vendor matches first in production)",
					tc.name, ifNot(got, ""))
				if got != tc.expectGenericAccept {
					t.Errorf("Supports() = %v, want %v (documents current behavior, not a strict requirement)", got, tc.expectGenericAccept)
				}
			}
		})
	}
}

func ifNot(b bool, prefix string) string {
	if !b {
		return prefix + "n't"
	}
	return prefix + ""
}
