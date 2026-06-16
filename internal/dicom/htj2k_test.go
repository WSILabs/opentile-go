package dicom

import (
	"os"
	"path/filepath"
	"testing"
)

func htj2kInstance(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	p := filepath.Join(base, "dicom", "3DHISTECH-HTJ2K", "000010.dcm")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return p
}

// TestParseInstanceHTJ2K: github.com/WSILabs/dicom recognizes the HTJ2K
// transfer syntaxes directly and reports the real UID. Upstream
// suyashkumar/dicom v1.1.0 SIGSEGV'd on the nil byte order, so opentile-go
// proxy-substituted the meta-header UID; the fork fixes it, so that workaround
// was retired (parse is now a plain ParseFile).
func TestParseInstanceHTJ2K(t *testing.T) {
	in, err := ParseInstance(htj2kInstance(t))
	if err != nil {
		t.Fatalf("ParseInstance: %v", err)
	}
	if in.TransferSyntax != "1.2.840.10008.1.2.4.201" {
		t.Errorf("TransferSyntax = %q, want HTJ2K .201", in.TransferSyntax)
	}
	if in.SOPClassUID != WSMStorageUID {
		t.Errorf("SOPClassUID = %q, want WSM", in.SOPClassUID)
	}
	if in.TotalCols <= 0 || in.TotalRows <= 0 {
		t.Errorf("total matrix %dx%d, want positive", in.TotalCols, in.TotalRows)
	}
}

// TestParseInstanceNoPanic: malformed DICOM must return an error, never
// SIGSEGV (the recover guard).
func TestParseInstanceNoPanic(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.dcm")
	buf := make([]byte, 256)
	copy(buf[128:], "DICM")
	for i := 132; i < len(buf); i++ {
		buf[i] = 0xAB // garbage past the magic
	}
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseInstance(tmp); err == nil {
		t.Error("expected error on garbage input, got nil")
	}
}
