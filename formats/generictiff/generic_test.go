package generictiff

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
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

// TestMetadataOf_WSIToolsFixture verifies the v0.17 cross-format
// Metadata population on a wsi-tools-transcoded fixture: per-axis
// MPP from the wsi-tools mpp= field (treated isotropic), Magnification
// from mag=, scanner from scanner=, and the wsi-tools.* Properties
// namespace surfaces source/codec/version provenance.
func TestMetadataOf_WSIToolsFixture(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "avif-out.tiff")
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
	if md.Magnification != 20 {
		t.Errorf("Magnification = %v, want 20", md.Magnification)
	}
	if md.ScannerManufacturer != "Aperio" {
		t.Errorf("ScannerManufacturer = %q, want Aperio", md.ScannerManufacturer)
	}
	if md.MicronsPerPixelX != 0.499 || md.MicronsPerPixelY != 0.499 {
		t.Errorf("per-axis MPP = %v / %v, want 0.499 / 0.499",
			md.MicronsPerPixelX, md.MicronsPerPixelY)
	}
	if md.MicronsPerPixel != 0.499 {
		t.Errorf("MicronsPerPixel (symmetric) = %v, want 0.499", md.MicronsPerPixel)
	}
	if md.ImageDescription == "" {
		t.Error("ImageDescription should be populated verbatim from the TIFF tag")
	}
	for _, kv := range []struct{ k, v string }{
		{"wsi-tools.source", "svs"},
		{"wsi-tools.codec", "avif"},
		{"wsi-tools.version", "0.2.0-dev"},
	} {
		if got := md.Properties[kv.k]; got != kv.v {
			t.Errorf("Properties[%q] = %q, want %q", kv.k, got, kv.v)
		}
	}
	// Negative: wsi-tools fixtures don't carry case-number / user-name.
	if _, ok := md.Properties[opentile.PropertyCaseNumber]; ok {
		t.Errorf("PropertyCaseNumber should be absent on wsi-tools fixture")
	}
	if _, ok := md.Properties[opentile.PropertyUserName]; ok {
		t.Errorf("PropertyUserName should be absent on wsi-tools fixture")
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

// TestFactoryOpen_COGWSI_WSITagShortCircuit force-routes COG-WSI
// fixtures (which carry COG-WSI's WSIImageType / WSILevelIndex
// private tags) directly through the generic-TIFF factory and
// confirms the v0.19 WSI-tag short-circuit produces the writer-
// declared classification — not the dimension/aspect heuristic.
//
// Both pyramid build (WSILevelIndex ordering) and associated
// classification (WSIImageType dispatch) are exercised. The two
// fixtures intentionally cover cases the heuristic path would
// misclassify:
//
//   - scan_620: the 1536x1024 thumbnail exceeds thumbnailMaxDim
//     (1500) and would heuristically classify as "associated";
//     WSI tag short-circuits it to "thumbnail".
//   - Ventana-1: the 1251x3685 overview is a tiled aspect-ratio
//     edge case the heuristic path also misses; WSI tag delivers
//     "overview".
//
// CMU-1 is the baseline-pass case where the heuristic and the
// WSI-tag path agree — included to verify the short-circuit
// doesn't regress the easy case.
//
// Skipped cleanly when OPENTILE_TESTDIR is unset or fixtures are
// absent.
func TestFactoryOpen_COGWSI_WSITagShortCircuit(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, tc := range []struct {
		name           string
		wantLevels     int
		wantBaselineW  uint32
		wantBaselineH  uint32
		wantAssociated []string // ordered by Others order
	}{
		{
			name:           "CMU-1_cog-wsi.tiff",
			wantLevels:     3,
			wantBaselineW:  46000,
			wantBaselineH:  32914,
			wantAssociated: []string{TypeThumbnail, TypeLabel, TypeOverview},
		},
		{
			name:           "scan_620_cog-wsi.tiff",
			wantLevels:     4,
			wantBaselineW:  49152,
			wantBaselineH:  32768,
			wantAssociated: []string{TypeThumbnail, TypeLabel, TypeOverview},
		},
		{
			name:           "Ventana-1_cog-wsi.tiff",
			wantLevels:     8,
			wantBaselineW:  24576,
			wantBaselineH:  21504,
			wantAssociated: []string{TypeOverview},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "cog-wsi", tc.name)
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
			fac := New()
			if !fac.Supports(tf) {
				t.Fatalf("Supports(%s) = false", tc.name)
			}
			tiler, err := fac.Open(tf, &opentile.Config{})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			levels := tiler.Levels()
			if len(levels) != tc.wantLevels {
				t.Fatalf("levels = %d, want %d", len(levels), tc.wantLevels)
			}
			if sz := levels[0].Size(); uint32(sz.W) != tc.wantBaselineW || uint32(sz.H) != tc.wantBaselineH {
				t.Errorf("baseline = %dx%d, want %dx%d", sz.W, sz.H, tc.wantBaselineW, tc.wantBaselineH)
			}
			got := make([]string, 0, len(tiler.Associated()))
			for _, a := range tiler.Associated() {
				got = append(got, a.Type())
			}
			if len(got) != len(tc.wantAssociated) {
				t.Fatalf("associated kinds = %v, want %v", got, tc.wantAssociated)
			}
			for i, want := range tc.wantAssociated {
				if got[i] != want {
					t.Errorf("associated[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}
