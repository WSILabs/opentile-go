package ndpi

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
	"github.com/wsilabs/opentile-go/opentile/opentiletest"
)

// buildNDPIStub returns a tiny NDPI-layout TIFF with:
//   - FileFormat (65420, SHORT=1) — detection marker
//   - Magnification (65421, FLOAT=20.0) — page classification via tag 65421
//   - Make (271, ASCII="Hamamatsu") — required by Supports (per tifffile line 10608)
//   - TileOffsets / TileByteCounts so Open's newStrippedImage can parse the page (strips path)
//
// Image: 1024×768, TileWidth=640, TileLength=8 → 1×96 strips.
func buildNDPIStub(t *testing.T) []byte {
	t.Helper()
	// NDPI IFD layout at offset 12:
	//   2 bytes:  tagno (9)
	//   9×12=108 bytes: entries
	//   8 bytes:  next-IFD (uint64)
	//   9×4=36 bytes:   hi-bits extension
	//   IFD end = 12+2+108+8+36 = 166
	//
	// External data:
	//   "Hamamatsu\0" (10 bytes) at 166  → Make tag
	//   TileOffsets: 96×uint32 at 176    (384 bytes)
	//   TileByteCounts: 96×uint32 at 560 (384 bytes)
	const (
		nTags          = 9
		nTiles         = 96 // ceil(768/8)
		ifdBase        = 12
		makeStrOff     = 166 // "Hamamatsu\0" lives here
		makeStrLen     = 10
		offsetsDataOff = makeStrOff + makeStrLen   // 176
		countsDataOff  = offsetsDataOff + nTiles*4 // 560
	)
	buf := new(bytes.Buffer)
	w16 := func(v uint16) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
	}
	w32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	w64 := func(v uint64) {
		w32(uint32(v))
		w32(uint32(v >> 32))
	}
	// Header (12 bytes — NDPI uses 8-byte first-IFD pointer)
	buf.Write([]byte{'I', 'I', 42, 0})
	w64(ifdBase)
	// IFD
	w16(nTags)
	// Entries (sorted by tag, 12 bytes each: tag u16, type u16, count u32, value/offset u32)
	w16(256)
	w16(3)
	w32(1)
	w32(1024) // ImageWidth SHORT
	w16(257)
	w16(3)
	w32(1)
	w32(768) // ImageLength SHORT
	w16(271)
	w16(2)
	w32(makeStrLen)
	w32(makeStrOff) // Make ASCII -> "Hamamatsu\0"
	w16(322)
	w16(3)
	w32(1)
	w32(640) // TileWidth SHORT
	w16(323)
	w16(3)
	w32(1)
	w32(8) // TileLength SHORT (8-pixel strips)
	w16(324)
	w16(4)
	w32(nTiles)
	w32(offsetsDataOff) // TileOffsets LONG[96]
	w16(325)
	w16(4)
	w32(nTiles)
	w32(countsDataOff) // TileByteCounts LONG[96]
	w16(65420)
	w16(3)
	w32(1)
	w32(1) // FileFormat SHORT = 1 (sentinel)
	w16(65421)
	w16(11)
	w32(1)
	w32(0x41A00000) // Magnification FLOAT = 20.0
	// Next-IFD (8 bytes uint64) = 0
	w64(0)
	// Hi-bits extension (4 bytes × nTags) = all zeros
	for i := 0; i < nTags; i++ {
		w32(0)
	}
	// External data: Make string "Hamamatsu\0"
	buf.Write([]byte("Hamamatsu\x00"))
	// TileOffsets data: 96 × uint32 = 0 (tiles never read in Open test)
	for i := 0; i < nTiles; i++ {
		w32(0)
	}
	// TileByteCounts data: 96 × uint32 = 0
	for i := 0; i < nTiles; i++ {
		w32(0)
	}
	return buf.Bytes()
}

func TestSupportsDetectsNDPI(t *testing.T) {
	data := buildNDPIStub(t)
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	if !New().Supports(f) {
		t.Fatal("Supports: expected true for FileFormat+Make-bearing NDPI TIFF")
	}
}

func TestSupportsRejectsNonNDPI(t *testing.T) {
	// Same builder pattern but WITHOUT the SourceLens entry. This is a plain
	// classic TIFF (NOT NDPI mode). buildNonNDPIStub emits 4 tags and uses
	// the classic 4-byte first-IFD header.
	data := buildNonNDPIStub(t)
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	if New().Supports(f) {
		t.Fatal("Supports: expected false for non-NDPI TIFF")
	}
}

func buildNonNDPIStub(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w16 := func(v uint16) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
	}
	w32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0}) // classic 4-byte firstIFD = 8
	w16(4)                                            // 4 tags
	w16(256)
	w16(3)
	w32(1)
	w32(1024)
	w16(257)
	w16(3)
	w32(1)
	w32(768)
	w16(322)
	w16(3)
	w32(1)
	w32(640)
	w16(323)
	w16(3)
	w32(1)
	w32(8)
	w32(0) // next-IFD = 0
	return buf.Bytes()
}

func TestNdpiOpenClassifiesPages(t *testing.T) {
	data := buildNDPIStub(t)
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	cfg := opentiletest.NewConfig(opentile.Size{W: 640, H: 640}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()
	if tiler.Format() != opentile.FormatNDPI {
		t.Errorf("Format: got %q, want %q", tiler.Format(), opentile.FormatNDPI)
	}
	imgs := tiler.Pyramids()
	if len(imgs) != 1 || len(imgs[0].Levels) != 1 {
		t.Errorf("images/levels: got %d images, want 1; levels[0]=%d, want 1",
			len(imgs), func() int {
				if len(imgs) > 0 {
					return len(imgs[0].Levels)
				}
				return 0
			}())
	}
	if got := len(tiler.Associated()); got != 0 {
		t.Errorf("associated: got %d, want 0", got)
	}
	md, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf returned ok=false")
	}
	if md.SourceLens != 20.0 {
		t.Errorf("SourceLens: got %v, want 20.0", md.SourceLens)
	}
}
