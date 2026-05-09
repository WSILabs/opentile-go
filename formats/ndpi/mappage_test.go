package ndpi_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	_ "github.com/cornish/opentile-go/formats/all"
)

// TestNDPIMapPagePresentOnOS2 locks in L6/R13 — NDPI Map pages
// (Magnification tag value -2.0) are now exposed as AssociatedImage
// entries with Type() == "map". OS-2.ndpi page 11 is a 580x198
// 8-bit grayscale Map page (verified pre-task in v0.4 Task 2 audit).
//
// This is a deliberate Go-side extension. Python opentile 0.20.0
// does not expose Map pages — `tiler.maps` is not a property and
// `_is_label_series` / `_is_thumbnail_series` both return False.
// tifffile (the underlying library) does classify them via
// `name == 'Map'` in `_series_ndpi`; we're closing an opentile-
// level scope decision, not inventing a new category.
func TestNDPIMapPagePresentOnOS2(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()

	var got opentile.AssociatedImage
	for _, a := range tiler.Associated() {
		if a.Type() == "map" {
			got = a
			break
		}
	}
	if got == nil {
		t.Fatal(`OS-2.ndpi: no AssociatedImage with Type() == "map"`)
	}
	// OS-2.ndpi page 11 is 198 rows tall, 580 cols wide (verified
	// in v0.4 Task 2 audit via tifffile.TiffFile inspection).
	if size := got.Size(); size.W != 580 || size.H != 198 {
		t.Errorf("OS-2.ndpi Map size: got %dx%d, want 580x198", size.W, size.H)
	}
	// Hamamatsu OS-2 Map page is uncompressed 8-bit grayscale (TIFF
	// Compression == 1). Strip length must equal width*height bytes.
	if got.Compression() != opentile.CompressionNone {
		t.Errorf("OS-2.ndpi Map compression: got %v, want CompressionNone", got.Compression())
	}
	b, err := got.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	wantLen := 580 * 198
	if len(b) != wantLen {
		t.Errorf("OS-2.ndpi Map bytes length: got %d, want %d (= 580*198 raw grayscale)", len(b), wantLen)
	}
}

// TestNDPIMapPagePresentOnHamamatsu1 mirrors the OS-2 test for the
// second slide that carries a Map page. Hamamatsu-1.ndpi page 7 is
// a 600x205 grayscale Map.
func TestNDPIMapPagePresentOnHamamatsu1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "Hamamatsu-1.ndpi")
	if _, err := os.Stat(slide); err != nil {
		t.Skipf("slide not present: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()

	var got opentile.AssociatedImage
	for _, a := range tiler.Associated() {
		if a.Type() == "map" {
			got = a
			break
		}
	}
	if got == nil {
		t.Fatal(`Hamamatsu-1.ndpi: no AssociatedImage with Type() == "map"`)
	}
	if size := got.Size(); size.W != 600 || size.H != 205 {
		t.Errorf("Hamamatsu-1.ndpi Map size: got %dx%d, want 600x205", size.W, size.H)
	}
}

// TestNDPIMapPageAbsentOnCMU1 confirms CMU-1.ndpi (which carries no
// Map page per v0.4 Task 2 audit) does NOT have a Type() == "map"
// associated image. Catches the failure mode where a future
// regression makes mapPage emit unconditionally.
func TestNDPIMapPageAbsentOnCMU1(t *testing.T) {
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
	for _, a := range tiler.Associated() {
		if a.Type() == "map" {
			t.Errorf(`CMU-1.ndpi: unexpected AssociatedImage with Type() == "map"`)
		}
	}
}
