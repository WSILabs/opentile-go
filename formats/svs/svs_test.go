package svs

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

func TestSupportsAperio(t *testing.T) {
	data := buildTIFFWithDesc(t, "Aperio Image Library v10.2.0\n46000x32914 ...")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if !New().Supports(f) {
		t.Fatal("expected Supports to return true for Aperio-prefixed description")
	}
}

func TestSupportsRejectsOther(t *testing.T) {
	data := buildTIFFWithDesc(t, "Hamamatsu Ndpi\n...")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Fatal("expected Supports to return false for non-Aperio description")
	}
}

func TestSupportsHandlesEmpty(t *testing.T) {
	// Empty description → not Aperio.
	data := buildTIFFWithDesc(t, "")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Fatal("expected Supports to return false for empty description")
	}
}

// buildTIFFWithDesc creates a single-IFD TIFF whose ImageDescription tag is desc.
// Minimal: ImageWidth, ImageLength, ImageDescription, TileWidth, TileLength.
func buildTIFFWithDesc(t *testing.T, desc string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
	descBytes := append([]byte(desc), 0)
	descOff := uint32(74)
	writeU16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
	writeU32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	writeU16(5)
	writeU16(256)
	writeU16(3)
	writeU32(1)
	writeU32(1024)
	writeU16(257)
	writeU16(3)
	writeU32(1)
	writeU32(768)
	writeU16(270)
	writeU16(2)
	writeU32(uint32(len(descBytes)))
	writeU32(descOff)
	writeU16(322)
	writeU16(3)
	writeU32(1)
	writeU32(256)
	writeU16(323)
	writeU16(3)
	writeU32(1)
	writeU32(256)
	writeU32(0)
	buf.Write(descBytes)
	return buf.Bytes()
}
