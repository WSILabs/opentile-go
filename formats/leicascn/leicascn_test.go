package leicascn

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

func TestFactory_Format(t *testing.T) {
	if got := New().Format(); got != opentile.FormatLeicaSCN {
		t.Errorf("Format() = %v, want %v", got, opentile.FormatLeicaSCN)
	}
}

func TestFactory_Supports_RealFixtures(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "scn", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				t.Fatal(err)
			}
			if !New().Supports(tf) {
				t.Errorf("Supports() = false on real SCN fixture %s; want true", name)
			}
		})
	}
}

// TestFactory_Supports_RejectsVendorTIFFs verifies the SCN factory
// declines non-SCN TIFFs. Includes one fixture per vendor format
// where available; skipped when the file isn't on disk.
func TestFactory_Supports_RejectsVendorTIFFs(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	for _, tc := range []struct{ subdir, name string }{
		{"svs", "CMU-1.svs"},
		{"generic-tiff", "CMU-1.tiff"},
		{"ome-tiff", "Leica-1.ome.tiff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.subdir, tc.name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				t.Fatal(err)
			}
			if New().Supports(tf) {
				t.Errorf("Supports() = true on non-SCN %s; want false", tc.name)
			}
		})
	}
}

func TestFactory_Open_Leica1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	path := filepath.Join(dir, "scn", "Leica-1.scn")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	tlr, err := New().Open(tf, &opentile.Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Format(); got != opentile.FormatLeicaSCN {
		t.Errorf("Format() = %v, want %v", got, opentile.FormatLeicaSCN)
	}
	// Leica-1 main scan has 5 pyramid levels.
	if got := len(tlr.Levels()); got != 5 {
		t.Errorf("len(Levels()) = %d, want 5", got)
	}
	if got := len(tlr.Associated()); got != 1 {
		t.Errorf("len(Associated()) = %d, want 1", got)
	}
	if got := tlr.Associated()[0].Type(); got != "overview" {
		t.Errorf("Associated[0].Type() = %q, want %q", got, "overview")
	}
	// Multi-image API: SizeC == 1 for brightfield Leica-1.
	if got := tlr.Images()[0].SizeC(); got != 1 {
		t.Errorf("SizeC() = %d, want 1", got)
	}
}

func TestFactory_Open_Fluorescence_SizeC(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	path := filepath.Join(dir, "scn", "Leica-Fluorescence-1.scn")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	tlr, err := New().Open(tf, &opentile.Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tlr.Close()

	if got := tlr.Images()[0].SizeC(); got != 3 {
		t.Errorf("SizeC() = %d, want 3", got)
	}
	for i, want := range []string{"405|Empty", "L5|Empty", "TX2|Empty"} {
		if got := tlr.Images()[0].ChannelName(i); got != want {
			t.Errorf("ChannelName(%d) = %q, want %q", i, got, want)
		}
	}
	// 2 auxiliaries (brightfield + fluorescence overview).
	if got := len(tlr.Associated()); got != 2 {
		t.Errorf("len(Associated()) = %d, want 2", got)
	}
}

func TestMetadataOf_Leica1(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	path := filepath.Join(dir, "scn", "Leica-1.scn")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	tlr, _ := New().Open(tf, &opentile.Config{})
	md, ok := MetadataOf(tlr)
	if !ok {
		t.Fatal("MetadataOf returned !ok on a SCN Tiler")
	}
	if md.Barcode != "MDQwNTA2MjlD" {
		t.Errorf("Barcode = %q, want %q", md.Barcode, "MDQwNTA2MjlD")
	}
	if got := len(md.Auxiliaries); got != 1 {
		t.Errorf("Auxiliaries = %d, want 1", got)
	}
	if got := len(md.Regions); got != 1 {
		t.Errorf("Regions = %d, want 1", got)
	}
	if got := md.ScannerManufacturer; got != "Leica" {
		t.Errorf("ScannerManufacturer = %q, want %q", got, "Leica")
	}
	if got := md.ScannerModel; got != "Leica SCN400" {
		t.Errorf("ScannerModel = %q, want %q", got, "Leica SCN400")
	}
	if md.AcquisitionDateTime.IsZero() {
		t.Error("AcquisitionDateTime should be parsed (Leica-1 carries 2011-05-31T09:33:14.31Z)")
	}
}

// TestFactory_Open_AllFixtures_ReadL0Corner does an end-to-end smoke
// test on all 3 fixtures: opens, reads L0 (0, 0), confirms valid JPEG.
// For fluorescence, also reads channel 1 via TileAt to verify multi-
// channel dispatch works through the public API path.
func TestFactory_Open_AllFixtures_ReadL0Corner(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	for _, name := range []string{"Leica-1.scn", "Leica-2.scn", "Leica-Fluorescence-1.scn"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "scn", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				t.Fatal(err)
			}
			tlr, err := New().Open(tf, &opentile.Config{})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer tlr.Close()
			if got := len(tlr.Levels()); got == 0 {
				t.Fatal("len(Levels()) == 0; want > 0")
			}
			b, err := tlr.Levels()[0].Tile(0, 0)
			if err != nil {
				t.Fatalf("Tile(0,0): %v", err)
			}
			if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
				t.Errorf("first 2 bytes = % x, want FF D8", b[:2])
			}

			// Multi-channel check on Fluorescence: channel 1 tile is
			// distinct from channel 0.
			img := tlr.Images()[0]
			if img.SizeC() == 3 {
				c0, _ := tlr.Levels()[0].Tile(0, 0)
				c1, err := tlr.Levels()[0].TileAt(opentile.TileCoord{C: 1, X: 0, Y: 0})
				if err != nil {
					t.Errorf("TileAt(C=1): %v", err)
				}
				if len(c0) > 0 && len(c1) > 0 && string(c0) == string(c1) {
					t.Error("channel 0 and channel 1 returned identical bytes")
				}
			}
		})
	}
}
