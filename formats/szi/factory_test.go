package szi

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
)

func TestFactory_Format(t *testing.T) {
	f := New()
	if got := f.Format(); got != opentile.FormatSZI {
		t.Errorf("Format() = %q, want %q", got, opentile.FormatSZI)
	}
}

func TestFactory_SupportsRaw_TinyFile(t *testing.T) {
	f := New()
	data := []byte{0x00, 0x01, 0x02}
	r := bytes.NewReader(data)
	if got := f.SupportsRaw(r, int64(len(data))); got {
		t.Errorf("SupportsRaw with 3 bytes: got true, want false")
	}
}

func TestFactory_SupportsRaw_ZIPMagic(t *testing.T) {
	f := New()
	// ZIP local file header magic: PK\x03\x04 (0x04034B50 little-endian)
	data := []byte{0x50, 0x4B, 0x03, 0x04}
	r := bytes.NewReader(data)
	if got := f.SupportsRaw(r, int64(len(data))); !got {
		t.Errorf("SupportsRaw with ZIP magic: got false, want true")
	}
}

func TestFactory_SupportsRaw_NoZIPMagic(t *testing.T) {
	f := New()
	data := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	r := bytes.NewReader(data)
	if got := f.SupportsRaw(r, int64(len(data))); got {
		t.Errorf("SupportsRaw with non-ZIP magic: got true, want false")
	}
}

func TestFactory_SupportsRaw_ReadError(t *testing.T) {
	f := New()
	errReader := &errorReader{}
	if got := f.SupportsRaw(errReader, 100); got {
		t.Errorf("SupportsRaw with read error: got true, want false")
	}
}

// errorReader always returns an error on read.
type errorReader struct{}

func (e *errorReader) ReadAt(p []byte, off int64) (int, error) {
	return 0, errors.New("synthetic read error")
}

func TestFactory_Supports_AlwaysFalse(t *testing.T) {
	f := New()
	// Factory.Supports always returns false for SZI (never a TIFF).
	if got := f.Supports(nil); got {
		t.Errorf("Supports(nil): got true, want false")
	}
}

func TestFactory_Open_ReturnsErrUnsupportedFormat(t *testing.T) {
	f := New()
	_, err := f.Open(nil, nil)
	if !errors.Is(err, opentile.ErrUnsupportedFormat) {
		t.Errorf("Open returned %v, want ErrUnsupportedFormat", err)
	}
}

func TestFactory_OpenRaw_ZIPWithNoDZI(t *testing.T) {
	f := New()
	// Create a minimal ZIP with no .dzi entry.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/some_file.txt")
	zw.Close()

	data := buf.Bytes()
	if !f.SupportsRaw(bytes.NewReader(data), int64(len(data))) {
		t.Fatalf("SupportsRaw should accept valid ZIP magic")
	}

	// Now try to open — should fail because no .dzi.
	r := bytes.NewReader(data)
	_, err := f.OpenRaw(r, int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with ZIP but no .dzi: got nil error, want error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("dzi")) {
		t.Errorf("OpenRaw error should mention .dzi: %v", err)
	}
}

func TestFactory_OpenRaw_ZIPWithMultipleRoots(t *testing.T) {
	f := New()
	// Create a ZIP with multiple root directories.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root1/file.txt")
	zw.Create("root2/file.txt")
	zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, err := f.OpenRaw(r, int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with multiple roots: got nil error, want error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("root")) {
		t.Errorf("OpenRaw error should mention root folders: %v", err)
	}
}

func TestFactory_OpenRaw_ZIPWithNoDZIInRoot(t *testing.T) {
	f := New()
	// Create a ZIP with single root but no .dzi manifest.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")
	zw.Create("root/scan-properties.xml")
	zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, err := f.OpenRaw(r, int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with no .dzi: got nil error, want error")
	}
}

func TestFactory_OpenRaw_ZIPWithEmptyRoot(t *testing.T) {
	f := New()
	// Create a ZIP with empty root (no files under it).
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Close() // Empty ZIP

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, err := f.OpenRaw(r, int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with empty ZIP: got nil error, want error")
	}
}

func TestFactory_OpenRaw_InvalidZIP(t *testing.T) {
	f := New()
	// Crafted ZIP magic but invalid structure.
	data := []byte{0x50, 0x4B, 0x03, 0x04, 0xFF, 0xFF}
	r := bytes.NewReader(data)
	_, err := f.OpenRaw(r, int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with malformed ZIP: got nil error, want error")
	}
}

func TestSZIOverlapAccepted(t *testing.T) {
	// Overlap>0 is now supported via regionLayout/subtileLayout crop.
	// Opening a manifest with Overlap=1 must NOT be rejected with ErrOverlapNotSupported.
	// (The minimal archive below still fails — it has no scan-properties.xml — but the
	// failure must be for a different reason, proving the guard is gone.)
	manifest := func(overlap int) string {
		return fmt.Sprintf(`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" `+
			`Format="jpeg" Overlap="%d" TileSize="256"><Size Width="256" Height="256"/></Image>`, overlap)
	}
	build := func(overlap int) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: "s/s.dzi", Method: zip.Store})
		w.Write([]byte(manifest(overlap)))
		zw.CreateHeader(&zip.FileHeader{Name: "s/s_files/", Method: zip.Store})
		zw.Close()
		return buf.Bytes()
	}
	// Overlap=1: must NOT return ErrOverlapNotSupported (the old guard is gone).
	bad := build(1)
	if _, err := openSZI(bytes.NewReader(bad), int64(len(bad)), nil); errors.Is(err, dzi.ErrOverlapNotSupported) {
		t.Fatalf("Overlap=1 wrongly rejected: %v", err)
	}
	// Overlap=0: still must not return ErrOverlapNotSupported.
	good := build(0)
	if _, err := openSZI(bytes.NewReader(good), int64(len(good)), nil); errors.Is(err, dzi.ErrOverlapNotSupported) {
		t.Fatalf("Overlap=0 wrongly rejected by overlap guard: %v", err)
	}
}
