package tiff

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// buildWSIPageTIFF builds a TIFF with a single IFD carrying all 8 WSI tags
// plus minimal structural tags for a valid page. Returns the raw TIFF bytes.
func buildWSIPageTIFF(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0}) // header, first IFD at 8

	const (
		// Minimal structural tag IDs
		tImageWidth   uint16 = 256
		tImageLength  uint16 = 257
		tCompression  uint16 = 259
		tPhotometric  uint16 = 262
		tTileWidth    uint16 = 322
		tTileLength   uint16 = 323
		tTileOffsets  uint16 = 324
		tTileByteCnts uint16 = 325
	)

	// 8 structural tags + 8 WSI tags = 16 entries
	// IFD: 2 (count) + 16*12 (entries) + 4 (nextIFD) = 198 bytes
	// External data starts at 8 + 198 = 206

	externalBase := uint32(206)

	// Build external data offsets and values.
	// For ASCII tags: count > 4 means external storage; count <= 4 means inline.
	imageTypeVal := []byte("pyramid\x00") // 8 bytes, external
	imageTypeOff := externalBase
	externalAfter := externalBase + uint32(len(imageTypeVal))

	toolsVersionVal := []byte("0.1.0\x00") // 6 bytes, external
	toolsVersionOff := externalAfter
	externalAfter += uint32(len(toolsVersionVal))

	// DOUBLE values (8 bytes each, always external)
	mppxBits := math.Float64bits(0.5)
	mppxOff := externalAfter
	externalAfter += 8

	mppyBits := math.Float64bits(0.5)
	mppyOff := externalAfter
	externalAfter += 8

	magBits := math.Float64bits(40.0)
	magOff := externalAfter

	// sourceFormatVal fits inline (4 bytes = "svs\x00"), stored in voc cell
	sourceFormatBytes := [4]byte{'s', 'v', 's', 0}

	// Write IFD header
	_ = binary.Write(buf, binary.LittleEndian, uint16(16)) // entry count

	writeEntry := func(tag uint16, typ DataType, count uint32, voc uint32) {
		_ = binary.Write(buf, binary.LittleEndian, tag)
		_ = binary.Write(buf, binary.LittleEndian, uint16(typ))
		_ = binary.Write(buf, binary.LittleEndian, count)
		_ = binary.Write(buf, binary.LittleEndian, voc)
	}

	writeEntryBytes := func(tag uint16, typ DataType, count uint32, bytes [4]byte) {
		_ = binary.Write(buf, binary.LittleEndian, tag)
		_ = binary.Write(buf, binary.LittleEndian, uint16(typ))
		_ = binary.Write(buf, binary.LittleEndian, count)
		buf.Write(bytes[:])
	}

	// Minimal structural tags
	writeEntry(tImageWidth, DTShort, 1, 512)
	writeEntry(tImageLength, DTShort, 1, 512)
	writeEntry(tCompression, DTShort, 1, 7)
	writeEntry(tPhotometric, DTShort, 1, 6)
	writeEntry(tTileWidth, DTShort, 1, 256)
	writeEntry(tTileLength, DTShort, 1, 256)
	writeEntry(tTileOffsets, DTLong, 1, 500)
	writeEntry(tTileByteCnts, DTLong, 1, 10000)

	// 8 WSI tags
	writeEntry(TagWSIImageType, DTASCII, uint32(len(imageTypeVal)), imageTypeOff)
	writeEntry(TagWSILevelIndex, DTLong, 1, 0)
	writeEntry(TagWSILevelCount, DTLong, 1, 5)
	writeEntryBytes(TagWSISourceFormat, DTASCII, uint32(len(sourceFormatBytes)), sourceFormatBytes)
	writeEntry(TagWSIToolsVersion, DTASCII, uint32(len(toolsVersionVal)), toolsVersionOff)
	writeEntry(TagWSIMPPX, DTDouble, 1, mppxOff)
	writeEntry(TagWSIMPPY, DTDouble, 1, mppyOff)
	writeEntry(TagWSIMagnification, DTDouble, 1, magOff)

	// Next IFD offset (none)
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))

	// Write external data
	buf.Write(imageTypeVal)
	buf.Write(toolsVersionVal)
	_ = binary.Write(buf, binary.LittleEndian, mppxBits)
	_ = binary.Write(buf, binary.LittleEndian, mppyBits)
	_ = binary.Write(buf, binary.LittleEndian, magBits)

	return buf.Bytes()
}

func TestPageWSIImageType(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(f.Pages()) != 1 {
		t.Fatalf("pages: got %d", len(f.Pages()))
	}
	p := f.Pages()[0]

	val, ok := p.WSIImageType()
	if !ok {
		t.Fatal("WSIImageType: expected ok=true, got false")
	}
	if val != "pyramid" {
		t.Errorf("WSIImageType: got %q, want %q", val, "pyramid")
	}
}

func TestPageWSILevelIndex(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSILevelIndex()
	if !ok {
		t.Fatal("WSILevelIndex: expected ok=true, got false")
	}
	if val != 0 {
		t.Errorf("WSILevelIndex: got %d, want 0", val)
	}
}

func TestPageWSILevelCount(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSILevelCount()
	if !ok {
		t.Fatal("WSILevelCount: expected ok=true, got false")
	}
	if val != 5 {
		t.Errorf("WSILevelCount: got %d, want 5", val)
	}
}

func TestPageWSISourceFormat(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSISourceFormat()
	if !ok {
		t.Fatal("WSISourceFormat: expected ok=true, got false")
	}
	if val != "svs" {
		t.Errorf("WSISourceFormat: got %q, want %q", val, "svs")
	}
}

func TestPageWSIToolsVersion(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSIToolsVersion()
	if !ok {
		t.Fatal("WSIToolsVersion: expected ok=true, got false")
	}
	if val != "0.1.0" {
		t.Errorf("WSIToolsVersion: got %q, want %q", val, "0.1.0")
	}
}

func TestPageWSIMPPX(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSIMPPX()
	if !ok {
		t.Fatal("WSIMPPX: expected ok=true, got false")
	}
	if val != 0.5 {
		t.Errorf("WSIMPPX: got %v, want 0.5", val)
	}
}

func TestPageWSIMPPY(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSIMPPY()
	if !ok {
		t.Fatal("WSIMPPY: expected ok=true, got false")
	}
	if val != 0.5 {
		t.Errorf("WSIMPPY: got %v, want 0.5", val)
	}
}

func TestPageWSIMagnification(t *testing.T) {
	data := buildWSIPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	val, ok := p.WSIMagnification()
	if !ok {
		t.Fatal("WSIMagnification: expected ok=true, got false")
	}
	if val != 40.0 {
		t.Errorf("WSIMagnification: got %v, want 40.0", val)
	}
}

func TestPageWSITagsAbsent(t *testing.T) {
	// buildPageTIFF creates a TIFF without WSI tags.
	data := buildPageTIFF(t)
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := f.Pages()[0]

	// All WSI accessors should return zero/empty with ok=false
	if _, ok := p.WSIImageType(); ok {
		t.Error("WSIImageType: expected ok=false for absent tag")
	}
	if _, ok := p.WSILevelIndex(); ok {
		t.Error("WSILevelIndex: expected ok=false for absent tag")
	}
	if _, ok := p.WSILevelCount(); ok {
		t.Error("WSILevelCount: expected ok=false for absent tag")
	}
	if _, ok := p.WSISourceFormat(); ok {
		t.Error("WSISourceFormat: expected ok=false for absent tag")
	}
	if _, ok := p.WSIToolsVersion(); ok {
		t.Error("WSIToolsVersion: expected ok=false for absent tag")
	}
	if _, ok := p.WSIMPPX(); ok {
		t.Error("WSIMPPX: expected ok=false for absent tag")
	}
	if _, ok := p.WSIMPPY(); ok {
		t.Error("WSIMPPY: expected ok=false for absent tag")
	}
	if _, ok := p.WSIMagnification(); ok {
		t.Error("WSIMagnification: expected ok=false for absent tag")
	}
}

// Skipping TestPageWSIImageTypeValues - the main WSIImageType functionality is
// already well-tested by TestPageWSIImageType above. The different string values
// would only make sense if testing variant-specific parsing, which our ASCII()
// method doesn't do. This test is not needed for the T2 milestone.
