package cogwsi_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/cogwsi"
)

// TestOpen_CMU1SmallRegion is the T5 smoke test: opens the small
// COG-WSI fixture via opentile.OpenFile() (which exercises the
// formats/all registration + ghost-area dispatch) and confirms the
// returned Tiler self-reports as FormatCOGWSI.
//
// Skips when OPENTILE_TESTDIR is unset or the fixture is missing,
// to keep `go test ./...` green in CI environments without the
// sample-files directory.
func TestOpen_CMU1SmallRegion(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()
	if got := tlr.Format(); got != opentile.FormatCOGWSI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatCOGWSI)
	}
}

// TestTiler_CMU1SmallRegion_FullPyramid opens the canonical small
// COG-WSI fixture, walks every pyramid level, and confirms the first
// tile per level decodes as a valid JPEG (SOI marker FF D8 FF). Also
// verifies the associated-image set matches the spec-violating
// "macro" → Type("overview") mapping per v0.15 canonical naming.
func TestTiler_CMU1SmallRegion_FullPyramid(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	levels := tlr.Levels()
	if len(levels) == 0 {
		t.Fatal("Levels(): empty")
	}
	for i := range levels {
		b, err := tlr.RawTile(i, 0, 0)
		if err != nil {
			t.Fatalf("level %d RawTile(0,0): %v", i, err)
		}
		if len(b) < 3 || b[0] != 0xFF || b[1] != 0xD8 || b[2] != 0xFF {
			t.Errorf("level %d first tile lacks JPEG SOI: %x", i, b[:min(8, len(b))])
		}
	}

	got := make([]string, 0)
	for _, a := range tlr.Associated() {
		got = append(got, a.Type())
	}
	// Probe of CMU-1-Small-Region_cog-wsi.tiff: 4 IFDs total; one
	// pyramid + thumbnail + label + overview (in that IFD order, per
	// spec §6).
	want := []string{"thumbnail", "label", "overview"}
	if len(got) != len(want) {
		t.Fatalf("associated types = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("associated[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestTiler_Metadata verifies the cross-format Metadata populates
// from the WSI private tags (MPP, magnification) + COG-WSI ghost
// (spec version) + WSI source/tools tags, all routed per plan T6
// step 3.
func TestTiler_Metadata(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	md := tlr.Metadata()

	// MPP — probe showed 0.499 on both axes.
	if md.MicronsPerPixelX != 0.499 || md.MicronsPerPixelY != 0.499 {
		t.Errorf("MicronsPerPixel = (%g, %g), want (0.499, 0.499)",
			md.MicronsPerPixelX, md.MicronsPerPixelY)
	}
	if md.MicronsPerPixel != 0.499 {
		t.Errorf("MicronsPerPixel (symmetric) = %g, want 0.499", md.MicronsPerPixel)
	}
	if md.Magnification != 20 {
		t.Errorf("Magnification = %g, want 20", md.Magnification)
	}
	// Scanner attribution preserved from the SVS source.
	if md.ScannerManufacturer != "Aperio" {
		t.Errorf("ScannerManufacturer = %q, want %q", md.ScannerManufacturer, "Aperio")
	}
	if md.ImageDescription != "wsitools/0.23.0-dev convert source=svs" {
		t.Errorf("ImageDescription = %q", md.ImageDescription)
	}

	// v0.20: Writer = wsitools/<WSIToolsVersion> from private tag 65084.
	if md.Writer != "wsitools/0.23.0-dev" {
		t.Errorf("Writer = %q, want wsitools/0.23.0-dev", md.Writer)
	}

	// Properties[cog-wsi.*]
	checkProp := func(key, want string) {
		t.Helper()
		got := md.Properties[key]
		if got != want {
			t.Errorf("Properties[%q] = %q, want %q", key, got, want)
		}
	}
	checkProp(cogwsi.PropSourceFormat, "svs")
	checkProp(cogwsi.PropWSIToolsVer, "0.23.0-dev")
	checkProp(cogwsi.PropSpecVersion, "0.1")
}

// TestOpen_NonConformantGhost confirms a file with a COG_WSI_VERSION
// marker but a malformed required-key value (here: LAYOUT=OTHER)
// fails opening with ErrNotConformantCOGWSI.
//
// Synthesises a minimal classic-TIFF + ghost-area header that
// passes Supports() (COG_WSI_VERSION present) but trips
// validateGhost on the bad LAYOUT value.
func TestOpen_NonConformantGhost(t *testing.T) {
	const badGhost = "GDAL_STRUCTURAL_METADATA_SIZE=000148 bytes\n" +
		"LAYOUT=OTHER\n" +
		"BLOCK_ORDER=ROW_MAJOR\n" +
		"BLOCK_LEADER=SIZE_AS_UINT4\n" +
		"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
		"KNOWN_INCOMPATIBLE_EDITION=NO\n" +
		"COG_WSI_VERSION=0.1\n"

	// Classic TIFF header pointing past the ghost area to an IFD —
	// the IFD doesn't need to validate; ghost-area validation fires
	// first.
	ghostBytes := []byte(badGhost)
	headerSize := 8
	firstIFDOff := uint32(headerSize + len(ghostBytes))

	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0})
	buf.Write([]byte{byte(firstIFDOff), byte(firstIFDOff >> 8), byte(firstIFDOff >> 16), byte(firstIFDOff >> 24)})
	buf.Write(ghostBytes)
	// Empty IFD (0 entries, no next).
	buf.Write([]byte{0, 0, 0, 0, 0, 0})

	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.tiff")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := opentile.OpenFile(path)
	if err == nil {
		t.Fatal("OpenFile: want error, got nil")
	}
	if !errors.Is(err, cogwsi.ErrNotConformantCOGWSI) {
		t.Errorf("err = %v, want ErrNotConformantCOGWSI", err)
	}
}

// TestFactory_Format verifies that Factory.Format() returns the
// FormatCOGWSI identifier.
func TestFactory_Format(t *testing.T) {
	f := cogwsi.New()
	got := f.Format()
	if got != opentile.FormatCOGWSI {
		t.Errorf("Format = %q, want %q", got, opentile.FormatCOGWSI)
	}
}

// TestTiler_Level verifies Tiler.Level(i) delegates to the inner
// Image.Level(i) and handles out-of-range indices correctly.
func TestTiler_Level(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	// Test valid level index
	lvl, err := tlr.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}

	// Verify it matches Levels()[0]
	levels := tlr.Levels()
	if len(levels) == 0 {
		t.Fatal("Levels(): empty")
	}
	if lvl != levels[0] {
		t.Error("Level(0) does not match Levels()[0]")
	}

	// Test out-of-range index
	_, err = tlr.Level(99)
	if err == nil {
		t.Fatal("Level(99): want error, got nil")
	}
}

// TestTiler_WarmLevel verifies Tiler.WarmLevel(i) delegates to the
// inner Tiler and handles bounds correctly.
func TestTiler_WarmLevel(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	// WarmLevel(0) should succeed (in-range).
	if err := tlr.WarmLevel(0); err != nil {
		t.Fatalf("WarmLevel(0): %v", err)
	}

	// WarmLevel(99) should fail (out-of-range).
	if err := tlr.WarmLevel(99); err == nil {
		t.Fatal("WarmLevel(99): want error, got nil")
	}
}

// TestTiler_UnwrapTiler was a regression test for the old fileCloser/mmapCloser
// UnwrapTiler() path. As of v0.23, OpenFile returns *opentile.Slide (a concrete
// struct), so the unwrap-via-type-assertion pattern no longer applies.
// The test now verifies that the Slide's Images() and Levels() are non-empty,
// which was the original assertion's substance.
func TestTiler_UnwrapTiler(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "cog-wsi", "CMU-1-Small-Region_cog-wsi.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture not present")
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	images := slide.Pyramids()
	if len(images) == 0 {
		t.Error("slide.Pyramids(): empty")
	}
	levels := slide.Levels()
	if len(levels) == 0 {
		t.Error("slide.Levels(): empty")
	}
}
