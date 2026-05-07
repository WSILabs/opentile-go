package ometiff

import (
	"bytes"
	"testing"

	"github.com/cornish/opentile-go/internal/tiff"
)

// TestSupportsOME: description ending in </OME> → match. Mirrors
// upstream's `is_ome` rule (tifffile.py:10125-10129):
//
//	page.description[-10:].strip().endswith('OME>')
func TestSupportsOME(t *testing.T) {
	data := buildOMELikeTIFF(t, `<?xml version="1.0"?><OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"><Image Name="macro"/></OME>`)
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if !New().Supports(f) {
		t.Fatal("expected Supports=true on description ending in </OME>")
	}
}

// TestSupportsTrailingWhitespace: upstream strips trailing whitespace
// from description[-10:] before checking the suffix. Verify our port
// matches.
func TestSupportsTrailingWhitespace(t *testing.T) {
	data := buildOMELikeTIFF(t, `<?xml version="1.0"?><OME>x</OME>   `)
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if !New().Supports(f) {
		t.Fatal("expected Supports=true on </OME> with trailing whitespace")
	}
}

// TestSupportsRejectsAperio: SVS-style description must not match.
func TestSupportsRejectsAperio(t *testing.T) {
	data := buildOMELikeTIFF(t, "Aperio Image Library v10.2|MPP=0.5")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Fatal("expected Supports=false on Aperio metadata")
	}
}

// TestSupportsRejectsPhilips: Philips description (ends in </DataObject>)
// must not match.
func TestSupportsRejectsPhilips(t *testing.T) {
	data := buildOMELikeTIFF(t, `<DataObject>...</DataObject>`)
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Fatal("expected Supports=false on Philips metadata")
	}
}

// TestSupportsShortDescription: description shorter than 10 chars
// shouldn't crash; must return false.
func TestSupportsShortDescription(t *testing.T) {
	data := buildOMELikeTIFF(t, "OME>")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	// "OME>" stripped of trailing whitespace ends in "OME>" — would
	// match if we don't bound-check correctly. Per upstream's rule
	// we look at the last 10 chars; a 4-char description still ends
	// in "OME>" so it WOULD match. Confirm both behaviors are
	// consistent: our port should also accept a 4-char "OME>".
	if !New().Supports(f) {
		t.Error("expected Supports=true on bare 'OME>' (matches upstream's [-10:].strip().endswith)")
	}
}

// TestSupportsEmptyDescription: empty description → no match, no crash.
func TestSupportsEmptyDescription(t *testing.T) {
	data := buildOMELikeTIFF(t, "")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if New().Supports(f) {
		t.Error("expected Supports=false on empty description")
	}
}

// TestFormatIdentity confirms the FormatOMETIFF constant.
func TestFormatIdentity(t *testing.T) {
	if got := New().Format(); string(got) != "ome-tiff" {
		t.Errorf("Format(): got %q, want %q", got, "ome-tiff")
	}
}

// buildOMELikeTIFF builds a single-IFD classic TIFF with the supplied
// ImageDescription. Minimal layout sufficient for tiff.Open + Supports
// to consume.
func buildOMELikeTIFF(t *testing.T, description string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})

	descBytes := append([]byte(description), 0)

	// 5 entries → IFD body = 2 + 5*12 + 4 = 66; data starts at 8 + 66 = 74.
	dataOff := uint32(74)
	descOff := dataOff

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
	writeU32(1024) // ImageWidth
	writeU16(257)
	writeU16(3)
	writeU32(1)
	writeU32(768) // ImageLength
	writeU16(270)
	writeU16(2)
	writeU32(uint32(len(descBytes)))
	writeU32(descOff) // ImageDescription
	writeU16(322)
	writeU16(3)
	writeU32(1)
	writeU32(256) // TileWidth
	writeU16(323)
	writeU16(3)
	writeU32(1)
	writeU32(256) // TileLength
	writeU32(0)

	buf.Write(descBytes)
	return buf.Bytes()
}
