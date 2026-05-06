package leicascn

import (
	"errors"
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

// TestFactory_Open_Placeholder pins the T4 stub behavior: Open()
// returns errSCNTilerUnimplemented when called on a real fixture.
// T6 replaces this with a real Tiler.
func TestFactory_Open_Placeholder(t *testing.T) {
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
	_, err = New().Open(tf, &opentile.Config{})
	if !errors.Is(err, errSCNTilerUnimplemented) {
		t.Errorf("Open() = %v, want errSCNTilerUnimplemented (T4 stub)", err)
	}
}
