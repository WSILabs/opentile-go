package ndpi

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// buildPageWithMag constructs a minimal single-page TIFF with a
// Magnification (65421) FLOAT tag. Value is packed as IEEE 754 bits.
func buildPageWithMag(t *testing.T, mag float32) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // 1 tag
	_ = binary.Write(buf, binary.LittleEndian, uint16(65421))
	_ = binary.Write(buf, binary.LittleEndian, uint16(11)) // FLOAT
	_ = binary.Write(buf, binary.LittleEndian, uint32(1))
	_ = binary.Write(buf, binary.LittleEndian, math.Float32bits(mag))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // next IFD
	return buf.Bytes()
}

func TestClassifyPage(t *testing.T) {
	cases := []struct {
		mag  float32
		want pageType
	}{
		{20.0, pageLevel},
		{10.0, pageLevel},
		{40.0, pageLevel},
		{-1.0, pageMacro},
		{-2.0, pageMap},
	}
	for _, c := range cases {
		data := buildPageWithMag(t, c.mag)
		f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("tiff.Open(mag=%v): %v", c.mag, err)
		}
		got := classifyPage(f.Pages()[0])
		if got != c.want {
			t.Errorf("classifyPage(mag=%v): got %v, want %v", c.mag, got, c.want)
		}
	}
}

func TestClassifyPageMissingTag(t *testing.T) {
	// Page with no Magnification tag → pageUnknown.
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(256))
	_ = binary.Write(buf, binary.LittleEndian, uint16(3))
	_ = binary.Write(buf, binary.LittleEndian, uint32(1))
	_ = binary.Write(buf, binary.LittleEndian, uint32(100))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	data := buf.Bytes()
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	got := classifyPage(f.Pages()[0])
	if got != pageUnknown {
		t.Errorf("classifyPage missing tag: got %v, want pageUnknown", got)
	}
}
