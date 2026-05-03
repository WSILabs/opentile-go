package generic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

func TestFactoryFormat(t *testing.T) {
	if got := New().Format(); got != opentile.FormatGeneric {
		t.Errorf("Format() = %v, want %v", got, opentile.FormatGeneric)
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
		{"CMU-1-Small-Region.stripped.tiff", false}, // single-level, validator rejects
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

// TestFactoryOpenStub verifies the T6 placeholder behavior: when
// Supports() returns true, Open() returns errGenericTilerUnimplemented
// (the placeholder error). T7+ replaces this with a real Tiler.
//
// Pinning this is important so a later refactor doesn't accidentally
// regress to silent success or a different error type.
func TestFactoryOpenStub(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "synth-pyramid-jpeg.tiff")
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

	factory := New()
	if !factory.Supports(tf) {
		t.Fatal("synth-pyramid-jpeg.tiff should be Supports()-accepted")
	}
	_, err = factory.Open(tf, &opentile.Config{})
	if !errors.Is(err, errGenericTilerUnimplemented) {
		t.Errorf("Open() = %v, want errGenericTilerUnimplemented", err)
	}
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
		{"svs", "CMU-1.svs", true},          // valid pyramid; vendor still matches first
		{"ndpi", "CMU-1.ndpi", false},       // NDPI's special tile layout fails generic
		{"ome-tiff", "Leica-1.ome.tiff", false}, // OME uses SubIFDs, not top-level pyramid IFDs
		{"ife", "cervix_2x_jpeg.iris", false},   // IFE is non-TIFF; tiff.Open errors
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
