package ife

import (
	"bytes"
	"encoding/binary"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

// writeTileEntry5_3 encodes a TileEntry into dst[bodyOff:bodyOff+8] using
// the IFE on-disk format: 5-byte LE offset, 3-byte LE size.
func writeTileEntry5_3(dst []byte, bodyOff int, offset uint64, size uint32) {
	dst[bodyOff+0] = byte(offset)
	dst[bodyOff+1] = byte(offset >> 8)
	dst[bodyOff+2] = byte(offset >> 16)
	dst[bodyOff+3] = byte(offset >> 24)
	dst[bodyOff+4] = byte(offset >> 32)
	dst[bodyOff+5] = byte(size)
	dst[bodyOff+6] = byte(size >> 8)
	dst[bodyOff+7] = byte(size >> 16)
}

// tileOffsetsBodyStart returns the byte offset in the raw IFE buffer where
// TILE_OFFSETS entries begin (after the 16-byte block header).
//
// Layout from synthBuilder.build:
//
//	FILE_HEADER (fileHeaderSize B) | TILE_TABLE (tileTableSize B) |
//	LAYER_EXTENTS (16 + 12*N B) | TILE_OFFSETS hdr (16 B) | entries
func tileOffsetsBodyStart(nLayers int) int {
	leOff := fileHeaderSize + tileTableSize
	leSize := blockHeaderValidation + nLayers*layerExtentEntrySize
	toOff := leOff + leSize
	return toOff + blockHeaderValidation
}

// synthBasic returns a minimal valid 2-layer IFE buffer (3 tiles total).
// Storage order (coarsest-first): L0 coarsest (1×1), L1 finest (2×1).
func synthBasic(t *testing.T) (data []byte, fileSize int64) {
	t.Helper()
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{
				[]byte("COARSE"),
			}},
			{xTiles: 2, yTiles: 1, scale: 2, tiles: [][]byte{
				[]byte("TILE_A"), []byte("TILE_B"),
			}},
		},
	}
	d, _ := sb.build()
	return d, int64(len(d))
}

// TestValidateIFEHookExists is a compile-time assertion that *tiler
// implements opentile.Validator. This will fail to compile until
// formats/ife/validate.go adds the Validate method.
func TestValidateIFEHookExists(t *testing.T) {
	data, size := synthBasic(t)
	rdr, err := openIFE(bytes.NewReader(data), size, &format.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer rdr.Close()
	// If *tiler does not implement Validator, this line won't compile.
	var _ opentile.Validator = rdr.(*tiler)
}

// TestValidateIFESynthValid confirms no false-positive CheckOffsetsOutOfBounds
// on a well-formed synthetic IFE.
func TestValidateIFESynthValid(t *testing.T) {
	data, size := synthBasic(t)

	rep, err := opentile.Validate(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			t.Errorf("valid synthetic IFE got false-positive finding: %+v", f)
		}
	}
}

// TestValidateIFEOOBEntry is the primary TDD test. It mutates entry 0 in
// the TILE_OFFSETS block so that offset+size > fileSize, then calls
// opentile.Validate. readTileOffsets at Open time does NOT check individual
// tile byte ranges (only that the block header fits), so the file opens
// cleanly. Our Validate hook is expected to flag CheckOffsetsOutOfBounds.
func TestValidateIFEOOBEntry(t *testing.T) {
	data, size := synthBasic(t)

	// Set tile entry 0: offset = size-1, size = 100 → end = size+99 > size.
	bodyStart := tileOffsetsBodyStart(2 /* nLayers */)
	writeTileEntry5_3(data, bodyStart, uint64(size)-1, 100)

	// Sanity: FILE_HEADER.file_size must still match len(data).
	headerFileSize := binary.LittleEndian.Uint64(data[6:14])
	if int64(headerFileSize) != size {
		t.Fatalf("pre-check: header file_size %d != expected %d", headerFileSize, size)
	}

	rep, err := opentile.Validate(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("OOB tile entry not flagged; got findings: %+v", rep.Findings)
	}
}

// TestValidateIFESparseNotFlagged confirms that sparse entries
// (Offset == NullTile = 0xFFFFFFFFFF) are not flagged as out-of-bounds.
// Sparse entries carry no on-disk data and must be skipped by the hook.
func TestValidateIFESparseNotFlagged(t *testing.T) {
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 2, yTiles: 1, scale: 1, tiles: [][]byte{
				[]byte("PRESENT"),
				nil, // sparse — synthBuilder writes NullTile sentinel
			}},
		},
	}
	data, _ := sb.build()

	rep, err := opentile.Validate(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			t.Errorf("sparse entry incorrectly flagged: %+v", f)
		}
	}
}

// TestValidateIFENearMaxOffset confirms that a tile entry whose offset is
// near the 40-bit maximum (but not NullTile = 0xFFFFFFFFFF) is flagged
// when offset+size > fileSize.
func TestValidateIFENearMaxOffset(t *testing.T) {
	data, size := synthBasic(t)

	// 0xFFFFFFFFFE = 40-bit max minus 1 (avoids NullTile sentinel).
	// size=1 → end = 0xFFFFFFFFFE + 1 = 0xFFFFFFFFFF > any real fileSize.
	bodyStart := tileOffsetsBodyStart(2)
	writeTileEntry5_3(data, bodyStart, 0xFFFFFFFFFE, 1)

	rep, err := opentile.Validate(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("near-max-offset tile entry not flagged; got findings: %+v", rep.Findings)
	}
}
