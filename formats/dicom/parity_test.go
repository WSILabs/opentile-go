package dicom_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestDICOMBackingParity(t *testing.T) {
	dir := leica4(t) // from open_test.go (same package _test)
	probe := func(backing opentile.Backing) string {
		s, err := opentile.OpenFile(dir, opentile.WithBacking(backing))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()
		b, err := s.RawTile(2, 3, 3)
		if err != nil {
			t.Fatalf("rawtile: %v", err)
		}
		h := sha256.Sum256(b)
		return string(h[:])
	}
	if probe(opentile.BackingMmap) != probe(opentile.BackingPread) {
		t.Error("mmap vs pread raw tile differ")
	}
	_ = filepath.Join
	_ = os.Stat
}
