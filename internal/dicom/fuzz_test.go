package dicom

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseInstance is the class-level guard against parser crashes on
// malformed DICOM (the bug class behind the suyashkumar/dicom HTJ2K SIGSEGV).
// ParseInstance must always return — value or error — never panic/SIGSEGV.
// Run: go test ./internal/dicom -run x -fuzz FuzzParseInstance -fuzztime 30s
func FuzzParseInstance(f *testing.F) {
	// Seed corpus: a DICM preamble with assorted trailing bytes, plus the
	// HTJ2K transfer-syntax UID that crashed the unpatched parser.
	preamble := make([]byte, 132)
	copy(preamble[128:], "DICM")
	f.Add(append(append([]byte{}, preamble...), 0xAB, 0xCD, 0xEF))
	f.Add(append([]byte{}, preamble...))
	f.Add(append(append([]byte{}, preamble...), []byte("1.2.840.10008.1.2.4.201")...))
	f.Add([]byte{})
	f.Add([]byte("not dicom at all"))

	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		p := filepath.Join(dir, "fuzz.dcm")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Skip()
		}
		// Must not panic / SIGSEGV. Errors are fine.
		_, _ = ParseInstance(p)
	})
}
