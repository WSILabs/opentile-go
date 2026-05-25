package opentile

import (
	"bytes"
	"strings"
	"testing"
)

// TestOpenWithNoFormatsRegistered: without importing any format packages,
// Open returns an error (no formats registered).
func TestOpenWithNoFormatsRegistered(t *testing.T) {
	_, err := Open(bytes.NewReader([]byte{0, 1, 2, 3}), 4)
	if err == nil {
		t.Fatal("Open with no formats registered: expected error, got nil")
	}
}

func TestOpenFileErrorIncludesPath(t *testing.T) {
	_, err := OpenFile("/nonexistent/slide.svs")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "/nonexistent/slide.svs") {
		t.Errorf("error should include path: %v", err)
	}
}

// buildTIFFWithDescription creates a 1-IFD TIFF whose ImageDescription is desc.
// Minimal: ImageWidth, ImageLength, TileWidth, TileLength, ImageDescription.
func buildTIFFWithDescription(t *testing.T, desc string) []byte {
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
	writeU16(256); writeU16(3); writeU32(1); writeU32(1024)
	writeU16(257); writeU16(3); writeU32(1); writeU32(768)
	writeU16(270); writeU16(2); writeU32(uint32(len(descBytes))); writeU32(descOff)
	writeU16(322); writeU16(3); writeU32(1); writeU32(256)
	writeU16(323); writeU16(3); writeU32(1); writeU32(256)
	writeU32(0)
	buf.Write(descBytes)
	return buf.Bytes()
}
