package szi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/formats/szi"
)

func TestAssociated_CMU1HasAllThree(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	got := tlr.Associated()
	if len(got) != 3 {
		t.Fatalf("Associated count = %d, want 3", len(got))
	}

	wantTypes := map[string]bool{"label": false, "overview": false, "thumbnail": false}
	for _, a := range got {
		typ := a.Type()
		if _, ok := wantTypes[typ]; !ok {
			t.Errorf("unexpected Type() = %q", typ)
			continue
		}
		wantTypes[typ] = true

		// SZI mandates JPEG for associated_images/.
		if a.Compression() != opentile.CompressionJPEG {
			t.Errorf("%s Compression = %v, want JPEG", typ, a.Compression())
		}

		// Header-decoded dimensions should be positive.
		sz := a.Size()
		if sz.W <= 0 || sz.H <= 0 {
			t.Errorf("%s Size = %v, want positive dims", typ, sz)
		}

		// Each should fetch + start with the JPEG SOI marker.
		raw, err := a.Bytes()
		if err != nil {
			t.Errorf("%s Bytes(): %v", typ, err)
			continue
		}
		if !bytes.HasPrefix(raw, []byte{0xFF, 0xD8, 0xFF}) {
			n := min(4, len(raw))
			t.Errorf("%s does not start with JPEG SOI (got % x)", typ, raw[:n])
		}
	}
	for typ, found := range wantTypes {
		if !found {
			t.Errorf("missing Type() = %q in associated set", typ)
		}
	}
}

func TestMetadataOf_CMU1(t *testing.T) {
	path := filepath.Join(testdataDir(t), "CMU-1.szi")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1.szi not present")
	}
	tlr, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tlr.Close()

	szim, ok := szi.MetadataOf(tlr)
	if !ok {
		t.Fatalf("MetadataOf: ok = false on SZI Tiler")
	}
	if szim == nil {
		t.Fatalf("MetadataOf: szim is nil")
	}
	// CMU-1.szi has the spec-example values per probe 2026-05-08.
	if szim.ScannerManufacturer != "TestCompany" {
		t.Errorf("ScannerManufacturer = %q, want TestCompany", szim.ScannerManufacturer)
	}
	if szim.ScannerModel != "Super Scan 2" {
		t.Errorf("ScannerModel = %q, want Super Scan 2", szim.ScannerModel)
	}
	if szim.Magnification != 10 {
		t.Errorf("Magnification = %v, want 10", szim.Magnification)
	}
	// MicronsPerPixel reads via embedded-struct promotion through
	// opentile.Metadata after v0.17 cleanup (Q4 Option B).
	if szim.MicronsPerPixel != 0.402 {
		t.Errorf("MicronsPerPixel = %v, want 0.402", szim.MicronsPerPixel)
	}
	// v0.17: UserName / CaseNumber moved to cross-format Properties.
	if got := szim.Properties[opentile.PropertyUserName]; got != "thomas" {
		t.Errorf("Properties[UserName] = %q, want thomas", got)
	}
	if got := szim.Properties[opentile.PropertyCaseNumber]; got != "H-2017-234" {
		t.Errorf("Properties[CaseNumber] = %q, want H-2017-234", got)
	}
	// Tiler.Metadata() should return the same cross-format values.
	cross := tlr.Metadata()
	if cross.ScannerManufacturer != szim.ScannerManufacturer {
		t.Errorf("Tiler.Metadata().ScannerManufacturer = %q, szim = %q",
			cross.ScannerManufacturer, szim.ScannerManufacturer)
	}
}
