package cogwsi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/internal/cog"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestValidateGhost runs each ghost-area invariant per spec §3 +
// confirms the happy path passes. validateGhost is a pure value-in
// value-out check; no TIFF construction needed.
func TestValidateGhost(t *testing.T) {
	good := cog.GhostArea{
		Layout:                   expectedLayout,
		BlockOrder:               expectedBlockOrder,
		BlockLeader:              expectedBlockLeader,
		BlockTrailer:             expectedBlockTrailer,
		KnownIncompatibleEdition: expectedKnownIncompatibleEdition,
		COGWSIVersion:            "0.1",
	}
	if err := validateGhost(good); err != nil {
		t.Fatalf("validateGhost(good) = %v, want nil", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*cog.GhostArea)
	}{
		{"bad LAYOUT", func(g *cog.GhostArea) { g.Layout = "OTHER" }},
		{"bad BLOCK_ORDER", func(g *cog.GhostArea) { g.BlockOrder = "COLUMN_MAJOR" }},
		{"bad BLOCK_LEADER", func(g *cog.GhostArea) { g.BlockLeader = "NONE" }},
		{"bad BLOCK_TRAILER", func(g *cog.GhostArea) { g.BlockTrailer = "NONE" }},
		{"KNOWN_INCOMPATIBLE_EDITION=YES", func(g *cog.GhostArea) { g.KnownIncompatibleEdition = "YES" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := good
			tc.mutate(&g)
			err := validateGhost(g)
			if err == nil {
				t.Fatal("validateGhost: want error, got nil")
			}
			if !errors.Is(err, ErrNotConformantCOGWSI) {
				t.Errorf("err = %v, want ErrNotConformantCOGWSI", err)
			}
		})
	}
}

// TestValidateIFDs_SpecViolations exercises the per-IFD spec
// checks (§5.2 + §6) via synthetic minimal TIFFs. Each test case
// builds a deliberately non-conformant page set and asserts the
// validator catches the specific violation.
//
// The happy path is exercised separately by the integration tests
// in tiler_test.go against the real CMU-1-Small-Region fixture.
func TestValidateIFDs_SpecViolations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() []byte
	}{
		{
			// Pyramid IFD without WSIImageType — every IFD must carry it.
			name: "missing WSIImageType",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: true, omitImageType: true, levelIdx: 0, levelCount: 1},
				})
			},
		},
		{
			// Pyramid IFD with WSIImageType not in spec enum.
			name: "invalid WSIImageType value",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: true, imageType: "weird", levelIdx: 0, levelCount: 1},
				})
			},
		},
		{
			// WSIImageType=pyramid on a striped IFD (no TileWidth/Length).
			name: "pyramid IFD not tiled",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: false, imageType: "pyramid", levelIdx: 0, levelCount: 1},
				})
			},
		},
		{
			// Two pyramid IFDs with WSILevelIndex {0, 2} — gap at 1.
			name: "WSILevelIndex not contiguous",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: true, imageType: "pyramid", levelIdx: 0, levelCount: 2},
					{tiled: true, imageType: "pyramid", levelIdx: 2, levelCount: 2},
				})
			},
		},
		{
			// WSILevelCount=5 but only 2 pyramid IFDs present.
			name: "WSILevelCount mismatch",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: true, imageType: "pyramid", levelIdx: 0, levelCount: 5},
					{tiled: true, imageType: "pyramid", levelIdx: 1, levelCount: 5},
				})
			},
		},
		{
			// Associated IFD (label) appears BEFORE pyramid IFD —
			// violates spec §6 ordering.
			name: "associated before pyramid",
			build: func() []byte {
				return buildMultiIFDTIFF(t, []ifdSpec{
					{tiled: true, imageType: "label", levelIdx: 0, levelCount: 1},
					{tiled: true, imageType: "pyramid", levelIdx: 0, levelCount: 1},
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.build()
			tf, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("tiff.Open: %v", err)
			}
			err = validateIFDs(tf.Pages())
			if err == nil {
				t.Fatal("validateIFDs: want error, got nil")
			}
			if !errors.Is(err, ErrNotConformantCOGWSI) {
				t.Errorf("err = %v, want ErrNotConformantCOGWSI", err)
			}
		})
	}
}

// ifdSpec describes how to assemble one synthetic IFD for the
// validation tests. Only the WSI-tag-relevant knobs are exposed —
// every IFD gets the same minimal structural skeleton (ImageWidth=
// ImageLength=512, 8-bit YCbCr JPEG; tiled vs striped controlled
// by the tiled bool).
type ifdSpec struct {
	tiled         bool
	imageType     string // WSIImageType value
	omitImageType bool   // skip the WSIImageType tag entirely
	levelIdx      uint32 // WSILevelIndex value (only emitted when tiled && imageType==pyramid)
	levelCount    uint32 // WSILevelCount value (same constraint)
}

// buildMultiIFDTIFF assembles a classic-TIFF byte stream with one
// IFD per spec. Each IFD carries the minimal structural tags +
// the requested WSI tags. Tile/strip offsets point to a 1-byte
// payload area at the file tail — the bytes don't need to decode,
// they just need TIFF to accept the IFD chain.
func buildMultiIFDTIFF(t *testing.T, specs []ifdSpec) []byte {
	t.Helper()
	if len(specs) == 0 {
		t.Fatal("buildMultiIFDTIFF: at least one IFD required")
	}

	const (
		tImageWidth   uint16 = 256
		tImageLength  uint16 = 257
		tCompression  uint16 = 259
		tPhotometric  uint16 = 262
		tStripOffsets uint16 = 273
		tRowsPerStrip uint16 = 278
		tStripBC      uint16 = 279
		tTileWidth    uint16 = 322
		tTileLength   uint16 = 323
		tTileOffsets  uint16 = 324
		tTileByteCnts uint16 = 325
	)

	// Two-pass: compute per-IFD entry counts + offsets, then write.
	// Each entry is 12 bytes; IFD = 2 (count) + entries*12 + 4 (next).
	type built struct {
		entryCount uint16
		size       int    // bytes occupied in the file by this IFD
		offset     uint32 // file offset where this IFD starts
		external   []byte // per-IFD external data appended right after
		extOffset  uint32 // file offset where this IFD's external block starts
	}

	results := make([]built, len(specs))
	// Header is 8 bytes for classic LE TIFF.
	cursor := uint32(8)

	for i, s := range specs {
		// Count entries.
		ec := uint16(4) // ImageWidth, ImageLength, Compression, Photometric
		if s.tiled {
			ec += 4 // TileWidth, TileLength, TileOffsets, TileByteCounts
		} else {
			ec += 3 // StripOffsets, RowsPerStrip, StripByteCounts
		}
		if !s.omitImageType {
			ec += 1
		}
		// WSILevelIndex + WSILevelCount only emitted when pyramid
		// (matches what the COG-WSI writer does in practice).
		if !s.omitImageType && s.imageType == "pyramid" {
			ec += 2
		}
		results[i].entryCount = ec
		results[i].size = 2 + int(ec)*12 + 4
		results[i].offset = cursor
		cursor += uint32(results[i].size)
	}
	// External data blocks follow all IFDs. Allocate enough space
	// for each IFD's ASCII WSIImageType value + 1-byte payload area.
	for i, s := range specs {
		ext := new(bytes.Buffer)
		// Per-IFD payload byte (TIFF needs offsets to be non-zero
		// and within the file; a 1-byte byte-count is fine).
		ext.WriteByte(0xFF)
		// WSIImageType ASCII value (if longer than 4 bytes it lives
		// external).
		if !s.omitImageType && len(s.imageType)+1 > 4 {
			ext.WriteString(s.imageType)
			ext.WriteByte(0)
		}
		results[i].external = ext.Bytes()
		results[i].extOffset = cursor
		cursor += uint32(len(results[i].external))
	}

	buf := new(bytes.Buffer)
	// Header: II, 42, first-IFD offset = 8.
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})

	writeEntry := func(tag uint16, typ tiff.DataType, count uint32, voc uint32) {
		_ = binary.Write(buf, binary.LittleEndian, tag)
		_ = binary.Write(buf, binary.LittleEndian, uint16(typ))
		_ = binary.Write(buf, binary.LittleEndian, count)
		_ = binary.Write(buf, binary.LittleEndian, voc)
	}
	writeEntryInlineBytes := func(tag uint16, typ tiff.DataType, count uint32, payload []byte) {
		var inline [4]byte
		copy(inline[:], payload)
		_ = binary.Write(buf, binary.LittleEndian, tag)
		_ = binary.Write(buf, binary.LittleEndian, uint16(typ))
		_ = binary.Write(buf, binary.LittleEndian, count)
		buf.Write(inline[:])
	}

	for i, s := range specs {
		_ = binary.Write(buf, binary.LittleEndian, results[i].entryCount)

		writeEntry(tImageWidth, tiff.DTShort, 1, 512)
		writeEntry(tImageLength, tiff.DTShort, 1, 512)
		writeEntry(tCompression, tiff.DTShort, 1, 7)
		writeEntry(tPhotometric, tiff.DTShort, 1, 6)

		// The tile/strip offset/bytecount tags point at the 1-byte
		// payload at the start of this IFD's external block.
		payloadOff := results[i].extOffset

		if s.tiled {
			writeEntry(tTileWidth, tiff.DTShort, 1, 256)
			writeEntry(tTileLength, tiff.DTShort, 1, 256)
			writeEntry(tTileOffsets, tiff.DTLong, 1, payloadOff)
			writeEntry(tTileByteCnts, tiff.DTLong, 1, 1)
		} else {
			writeEntry(tStripOffsets, tiff.DTLong, 1, payloadOff)
			writeEntry(tRowsPerStrip, tiff.DTShort, 1, 512)
			writeEntry(tStripBC, tiff.DTLong, 1, 1)
		}

		if !s.omitImageType {
			// ASCII = imageType + NUL.
			ascii := []byte(s.imageType)
			ascii = append(ascii, 0)
			if len(ascii) <= 4 {
				writeEntryInlineBytes(tiff.TagWSIImageType, tiff.DTASCII, uint32(len(ascii)), ascii)
			} else {
				// External — the value was appended right after the
				// payload byte in the external block.
				writeEntry(tiff.TagWSIImageType, tiff.DTASCII, uint32(len(ascii)), payloadOff+1)
			}
			if s.imageType == "pyramid" {
				writeEntry(tiff.TagWSILevelIndex, tiff.DTLong, 1, s.levelIdx)
				writeEntry(tiff.TagWSILevelCount, tiff.DTLong, 1, s.levelCount)
			}
		}

		// Next-IFD pointer.
		var next uint32
		if i+1 < len(specs) {
			next = results[i+1].offset
		}
		_ = binary.Write(buf, binary.LittleEndian, next)
	}

	// External data blocks, in order.
	for _, r := range results {
		buf.Write(r.external)
	}

	return buf.Bytes()
}
