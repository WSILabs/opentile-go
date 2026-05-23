package philipstiff

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestSupportsPhilips: software starts with "Philips DP" AND description
// ends in </DataObject> → match.
func TestSupportsPhilips(t *testing.T) {
	data := buildPhilipsLikeTIFF(t, "Philips DP v1.0", `<DataObject><Attribute Name="X">y</Attribute></DataObject>`)
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if !New().Supports(f) {
		t.Fatal("expected Supports to return true for Philips DP + DataObject suffix")
	}
}

// TestSupportsRejectsAperio: software/description from an SVS slide must
// not match Philips.
func TestSupportsRejectsAperio(t *testing.T) {
	data := buildPhilipsLikeTIFF(t, "Aperio Image Library v10.2", "Aperio Image Library v10.2\n46000x32914")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Fatal("expected Supports to return false on Aperio metadata")
	}
}

// TestSupportsRequiresBothSignals: software-only is not enough; needs the
// description suffix as well.
func TestSupportsRequiresBothSignals(t *testing.T) {
	// Software matches but description does not end in </DataObject>.
	data1 := buildPhilipsLikeTIFF(t, "Philips DP v1.0", "<wrong>tag</wrong>")
	f1, _ := tiff.Open(bytes.NewReader(data1), int64(len(data1)))
	if New().Supports(f1) {
		t.Error("expected false: software-only match (no DataObject suffix)")
	}

	// Description matches but software does not start with Philips DP.
	data2 := buildPhilipsLikeTIFF(t, "Hamamatsu", `<DataObject></DataObject>`)
	f2, _ := tiff.Open(bytes.NewReader(data2), int64(len(data2)))
	if New().Supports(f2) {
		t.Error("expected false: description-only match (no Philips DP software)")
	}
}

// TestSupportsTrailingWhitespace: upstream's check is
// `description[-16:].strip().endswith('</DataObject>')` so trailing
// whitespace is permitted.
func TestSupportsTrailingWhitespace(t *testing.T) {
	data := buildPhilipsLikeTIFF(t, "Philips DP v1.0", "<DataObject></DataObject>\n  ")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if !New().Supports(f) {
		t.Fatal("expected Supports to accept trailing whitespace after </DataObject>")
	}
}

// TestFormatIdentity: Format() returns FormatPhilipsTIFF.
func TestFormatIdentity(t *testing.T) {
	if got := New().Format(); string(got) != "philips-tiff" {
		t.Errorf("Format(): got %q, want %q", got, "philips-tiff")
	}
}

// buildPhilipsLikeTIFF builds a single-IFD classic TIFF with Software
// (305) and ImageDescription (270) tags set to the supplied values, plus
// minimal ImageWidth/ImageLength/TileWidth/TileLength so the parser
// doesn't reject the file.
func buildPhilipsLikeTIFF(t *testing.T, software, description string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})

	swBytes := append([]byte(software), 0)
	descBytes := append([]byte(description), 0)

	// 6 entries → IFD body = 2 + 6*12 + 4 = 78 bytes; data starts at 8 + 78 = 86.
	dataOff := uint32(86)
	swOff := dataOff
	descOff := swOff + uint32(len(swBytes))

	writeU16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
	writeU32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	writeU16(6)
	// ImageWidth (256) SHORT 1
	writeU16(256)
	writeU16(3)
	writeU32(1)
	writeU32(1024)
	// ImageLength (257) SHORT 1
	writeU16(257)
	writeU16(3)
	writeU32(1)
	writeU32(768)
	// ImageDescription (270) ASCII
	writeU16(270)
	writeU16(2)
	writeU32(uint32(len(descBytes)))
	writeU32(descOff)
	// Software (305) ASCII
	writeU16(305)
	writeU16(2)
	writeU32(uint32(len(swBytes)))
	writeU32(swOff)
	// TileWidth (322) SHORT 1
	writeU16(322)
	writeU16(3)
	writeU32(1)
	writeU32(256)
	// TileLength (323) SHORT 1
	writeU16(323)
	writeU16(3)
	writeU32(1)
	writeU32(256)
	writeU32(0)

	buf.Write(swBytes)
	buf.Write(descBytes)
	return buf.Bytes()
}
