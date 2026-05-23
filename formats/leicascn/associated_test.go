package leicascn

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// openSCNTestFile opens a real SCN fixture and returns its parsed
// Collection plus the underlying *tiff.File so per-test paths can
// pull individual auxiliary <image> elements.
func openSCNTestFile(t *testing.T, name string) (*Collection, *tiff.File, *os.File) {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}
	path := filepath.Join(dir, "scn", name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	desc, _ := tf.Pages()[0].ImageDescription()
	c, err := ParseDescription(desc)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	return c, tf, f
}

func TestAssociatedImage_Leica1_Macro(t *testing.T) {
	c, tf, f := openSCNTestFile(t, "Leica-1.scn")
	defer f.Close()

	aux := c.Images[0] // Leica-1's only auxiliary
	a, err := newAssociatedImage(aux, tf, tf.ReaderAt())
	if err != nil {
		t.Fatalf("newAssociatedImage: %v", err)
	}
	if got := a.Type(); got != "overview" {
		t.Errorf("Type() = %q, want %q", got, "overview")
	}
	if a.Size().W != 101 || a.Size().H != 291 {
		t.Errorf("Size() = %v, want 101×291", a.Size())
	}
	if got := a.Compression(); got != opentile.CompressionJPEG {
		t.Errorf("Compression() = %v, want %v", got, opentile.CompressionJPEG)
	}
	b, err := a.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 4 {
		t.Fatalf("Bytes() too short: %d", len(b))
	}
	if !bytes.Equal(b[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("first 2 bytes = % x, want FF D8 (JPEG SOI)", b[:2])
	}
	tail := b
	if len(tail) > 64 {
		tail = tail[len(tail)-64:]
	}
	if !bytes.Contains(tail, []byte{0xFF, 0xD9}) {
		t.Errorf("trailing 64 bytes don't contain JPEG EOI marker")
	}
}

func TestAssociatedImage_Fluorescence_TwoMacros(t *testing.T) {
	c, tf, f := openSCNTestFile(t, "Leica-Fluorescence-1.scn")
	defer f.Close()

	// Fluorescence has 2 auxiliaries (brightfield + fluorescence).
	for i := 0; i < 2; i++ {
		aux := c.Images[i]
		a, err := newAssociatedImage(aux, tf, tf.ReaderAt())
		if err != nil {
			t.Fatalf("newAssociatedImage[%d]: %v", i, err)
		}
		if got := a.Type(); got != "overview" {
			t.Errorf("[%d] Type() = %q, want %q", i, got, "overview")
		}
		if a.Size().W != 101 || a.Size().H != 291 {
			t.Errorf("[%d] Size() = %v, want 101×291", i, a.Size())
		}
	}
}

func TestAssociatedImage_BytesAreCallerOwned(t *testing.T) {
	a := &associatedImage{
		size:        opentile.Size{W: 1, H: 1},
		compression: opentile.CompressionJPEG,
		bytes:       []byte{1, 2, 3, 4, 5},
	}
	b1, _ := a.Bytes()
	b1[0] = 0xFF
	b2, _ := a.Bytes()
	if b2[0] == 0xFF {
		t.Errorf("Bytes() returned a shared slice: mutation leaked back")
	}
	if b2[0] != 1 {
		t.Errorf("Bytes() corrupted cache: got %d, want 1", b2[0])
	}
}

// TestAssociatedImage_RejectsMultiTileLowestRes confirms the (rare)
// multi-tile lowest-res case returns errUnsupportedAuxiliary so the
// Tiler builder can silently drop it. None of our 3 fixtures exhibit
// this; the test uses synthetic Image data referencing a live IFD.
func TestAssociatedImage_RejectsMultiTileLowestRes(t *testing.T) {
	c, tf, f := openSCNTestFile(t, "Leica-1.scn")
	defer f.Close()

	// Pick the auxiliary's HIGHEST-resolution dimension (1616×4668)
	// as the only entry; that IFD is 4×10 = 40 tiles.
	aux := c.Images[0]
	synth := Image{
		Name:       aux.Name,
		Dimensions: []Dimension{aux.Dimensions[0]}, // r=0 entry only
	}
	_, err := newAssociatedImage(synth, tf, tf.ReaderAt())
	if !errors.Is(err, errUnsupportedAuxiliary) {
		t.Errorf("got %v, want errUnsupportedAuxiliary", err)
	}
}
