package opentile_test

// TestValidateFlagsOutOfBoundsOffsetGenericTIFF verifies that Validate flags
// CheckOffsetsOutOfBounds when a tiled TIFF level has a TileOffsets entry
// whose byte range extends past the end of the file.
//
// The synthetic TIFF is a minimal classic (magic-42) little-endian TIFF with
// one tiled IFD. All TileOffsets entries are valid except the last, which
// points well beyond the end of the byte slice, so tiffvalidate.Check (invoked
// through the generictiff Validate hook) must flag the bad range.

import (
	"bytes"
	"encoding/binary"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/generictiff" // registers generictiff catch-all
)

// buildTiledTIFFWithBadOffset constructs a valid little-endian classic TIFF
// with one tiled IFD. Image is 256x256 with 256x256 tiles → 1x1 grid (one
// tile). The single TileOffsets entry is set to 0xFFFF_FF00, which far exceeds
// the file's actual size, triggering CheckOffsetsOutOfBounds.
//
// Using a 1x1 tile grid avoids external-offset arrays: TileOffsets and
// TileByteCounts each have count=1, which fits inline in the 4-byte IFD cell.
//
// Layout:
//
//	[0..7]   TIFF header  (II, 42, ifdOffset=8)
//	[8..]    IFD          (entry-count + 10 entries + next-IFD=0)
func buildTiledTIFFWithBadOffset(t *testing.T) []byte {
	t.Helper()

	const (
		// TIFF classic type codes.
		tShort uint16 = 3 // SHORT  (2-byte unsigned)
		tLong  uint16 = 4 // LONG   (4-byte unsigned)

		// Tag numbers.
		tagImageWidth      uint16 = 256
		tagImageLength     uint16 = 257
		tagBitsPerSample   uint16 = 258
		tagCompression     uint16 = 259
		tagPhotometric     uint16 = 262
		tagSamplesPerPixel uint16 = 277
		tagTileWidth       uint16 = 322
		tagTileLength      uint16 = 323
		tagTileOffsets     uint16 = 324
		tagTileByteCounts  uint16 = 325
	)

	// Image: 256x256, tile 256x256 → 1x1 grid (one tile).
	// TileOffsets count=1 fits inline; value points far past EOF.
	type entry struct {
		tag   uint16
		typ   uint16
		count uint32
		value uint32 // inline value (fits in 4 bytes for all entries here)
	}

	entries := []entry{
		{tagImageWidth, tShort, 1, 256},  // 256-pixel wide
		{tagImageLength, tShort, 1, 256}, // 256-pixel tall
		{tagBitsPerSample, tShort, 1, 8}, // 8 bits per sample
		{tagCompression, tShort, 1, 1},   // Compression=1 (None)
		{tagPhotometric, tShort, 1, 2},   // RGB
		{tagSamplesPerPixel, tShort, 1, 3}, // 3 samples (RGB)
		{tagTileWidth, tShort, 1, 256},   // tile width
		{tagTileLength, tShort, 1, 256},  // tile height
		// TileOffsets: the one tile's data starts at a huge offset — past EOF.
		{tagTileOffsets, tLong, 1, 0xFFFF_FF00},
		// TileByteCounts: plausible size (256*256*3 = 196608).
		{tagTileByteCounts, tLong, 1, 256 * 256 * 3},
	}

	le := binary.LittleEndian

	// Header: "II", magic 42, IFD offset = 8.
	var buf bytes.Buffer
	tmp16 := make([]byte, 2)
	tmp32 := make([]byte, 4)
	buf.WriteString("II")
	le.PutUint16(tmp16, 42)
	buf.Write(tmp16)
	le.PutUint32(tmp32, 8) // IFD at offset 8
	buf.Write(tmp32)

	// IFD: entry count (uint16).
	le.PutUint16(tmp16, uint16(len(entries)))
	buf.Write(tmp16)

	// IFD entries: 12 bytes each (tag + type + count + value/offset).
	entryBuf := make([]byte, 12)
	for _, e := range entries {
		le.PutUint16(entryBuf[0:2], e.tag)
		le.PutUint16(entryBuf[2:4], e.typ)
		le.PutUint32(entryBuf[4:8], e.count)
		le.PutUint32(entryBuf[8:12], e.value)
		buf.Write(entryBuf)
	}

	// Next IFD offset = 0 (end of chain).
	le.PutUint32(tmp32, 0)
	buf.Write(tmp32)

	return buf.Bytes()
}

func TestValidateFlagsOutOfBoundsOffsetGenericTIFF(t *testing.T) {
	raw := buildTiledTIFFWithBadOffset(t)
	rep, err := opentile.Validate(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("operational error: %v", err)
	}
	var oob *opentile.Finding
	for i := range rep.Findings {
		if rep.Findings[i].Code == opentile.CheckOffsetsOutOfBounds {
			oob = &rep.Findings[i]
		}
	}
	if oob == nil {
		t.Fatalf("expected CheckOffsetsOutOfBounds, got %+v", rep.Findings)
	}
	if oob.Severity != opentile.Error {
		t.Fatalf("severity = %v, want Error", oob.Severity)
	}
}
