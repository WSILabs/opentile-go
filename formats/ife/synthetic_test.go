package ife

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

// synthBuilder hand-rolls a complete IFE v1.0 byte buffer for unit
// testing. Tile bytes are arbitrary recognizable patterns; no real
// codec is invoked. The builder sets up the FILE_HEADER and chains
// every block at predictable offsets.
type synthBuilder struct {
	layers   []synthLayer
	encoding uint8 // TILE_ENCODING_*; defaults to JPEG (2)
	format   uint8 // pixel format; defaults to 3 (B8G8R8A8)
}

type synthLayer struct {
	xTiles, yTiles uint32
	scale          float32
	tiles          [][]byte // row-major; nil entry = sparse
}

// build returns the complete IFE byte buffer + the linear-indexed
// tile-byte slice that openIFE will read. Layout:
//
//	[0]            FILE_HEADER (38 B)
//	[38]           TILE_TABLE (44 B)
//	[82]           LAYER_EXTENTS (16 + 12*N B)
//	[then]         TILE_OFFSETS (16 + 8*total B)
//	[then]         METADATA placeholder (1 B; we don't parse it)
//	[then]         tile bytes laid out in linear order
func (sb *synthBuilder) build() (data []byte, totalTiles uint32) {
	enc := sb.encoding
	if enc == 0 {
		enc = encodingJPEG
	}
	pf := sb.format
	if pf == 0 {
		pf = 3
	}

	for _, l := range sb.layers {
		totalTiles += l.xTiles * l.yTiles
	}

	const fileHdrOff = 0
	const ttOff = uint64(fileHeaderSize)
	leOff := ttOff + uint64(tileTableSize)
	leSize := uint64(blockHeaderValidation) + uint64(len(sb.layers))*uint64(layerExtentEntrySize)
	toOff := leOff + leSize
	toSize := uint64(blockHeaderValidation) + uint64(totalTiles)*uint64(tileEntrySize)
	mdOff := toOff + toSize
	const mdSize = 1 // single placeholder byte; reader doesn't parse METADATA in v0.8
	tileBytesStart := mdOff + mdSize

	// Lay out tile bytes contiguously in storage order (matches the
	// linear iteration order TILE_OFFSETS uses).
	var tilesBlob bytes.Buffer
	type entry struct{ off uint64; size uint32 }
	entries := make([]entry, 0, totalTiles)
	for _, l := range sb.layers {
		for i := uint32(0); i < l.xTiles*l.yTiles; i++ {
			b := l.tiles[i]
			if b == nil {
				entries = append(entries, entry{off: NullTile, size: 0})
				continue
			}
			entries = append(entries, entry{
				off:  tileBytesStart + uint64(tilesBlob.Len()),
				size: uint32(len(b)),
			})
			tilesBlob.Write(b)
		}
	}
	totalSize := tileBytesStart + uint64(tilesBlob.Len())

	out := make([]byte, totalSize)

	// FILE_HEADER. The original synth tests don't exercise metadata,
	// so we NULL the metadata_offset rather than pointing at a stub
	// block — the dedicated metadata_test.go covers that path.
	binary.LittleEndian.PutUint32(out[0:4], MagicBytes)
	binary.LittleEndian.PutUint64(out[6:14], totalSize)
	binary.LittleEndian.PutUint16(out[14:16], 1) // ext_major
	binary.LittleEndian.PutUint64(out[22:30], ttOff)
	binary.LittleEndian.PutUint64(out[30:38], NullOffset)

	// TILE_TABLE.
	tt := out[ttOff : ttOff+tileTableSize]
	binary.LittleEndian.PutUint64(tt[0:8], ttOff)
	tt[10] = enc
	tt[11] = pf
	binary.LittleEndian.PutUint64(tt[12:20], NullOffset)
	binary.LittleEndian.PutUint64(tt[20:28], toOff)
	binary.LittleEndian.PutUint64(tt[28:36], leOff)
	// TILE_TABLE.{x,y}_extent are tile counts in cervix; mirror that
	// (synth tests don't read these fields).
	if n := len(sb.layers); n > 0 {
		binary.LittleEndian.PutUint32(tt[36:40], sb.layers[n-1].xTiles)
		binary.LittleEndian.PutUint32(tt[40:44], sb.layers[n-1].yTiles)
	}

	// LAYER_EXTENTS.
	le := out[leOff : leOff+leSize]
	binary.LittleEndian.PutUint64(le[0:8], leOff)
	binary.LittleEndian.PutUint16(le[10:12], layerExtentEntrySize)
	binary.LittleEndian.PutUint32(le[12:16], uint32(len(sb.layers)))
	for i, l := range sb.layers {
		base := blockHeaderValidation + i*layerExtentEntrySize
		binary.LittleEndian.PutUint32(le[base:base+4], l.xTiles)
		binary.LittleEndian.PutUint32(le[base+4:base+8], l.yTiles)
		binary.LittleEndian.PutUint32(le[base+8:base+12], math.Float32bits(l.scale))
	}

	// TILE_OFFSETS.
	to := out[toOff : toOff+toSize]
	binary.LittleEndian.PutUint64(to[0:8], toOff)
	binary.LittleEndian.PutUint16(to[10:12], tileEntrySize)
	binary.LittleEndian.PutUint32(to[12:16], totalTiles)
	for i, e := range entries {
		base := blockHeaderValidation + i*tileEntrySize
		// 40-bit offset.
		to[base+0] = byte(e.off)
		to[base+1] = byte(e.off >> 8)
		to[base+2] = byte(e.off >> 16)
		to[base+3] = byte(e.off >> 24)
		to[base+4] = byte(e.off >> 32)
		// 24-bit size.
		to[base+5] = byte(e.size)
		to[base+6] = byte(e.size >> 8)
		to[base+7] = byte(e.size >> 16)
	}

	// METADATA placeholder (single byte; reader doesn't parse it in v0.8).
	out[mdOff] = 0xAA

	// Tile bytes.
	copy(out[tileBytesStart:], tilesBlob.Bytes())
	return out, totalTiles
}

// openIFETiler opens a synthetic IFE buffer and returns the concrete *tiler.
func openIFETiler(t *testing.T, data []byte) *tiler {
	t.Helper()
	r, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	return r.(*tiler)
}

// TestSynthLayerInversion verifies that a 3-layer synthetic IFE with
// distinguishable per-layer tile bytes round-trips correctly through
// the layer-inversion logic. Images()[0].Levels[0] is native (highest
// scale in storage = last entry); Levels[N-1] is coarsest.
func TestSynthLayerInversion(t *testing.T) {
	// File-storage order is COARSEST-FIRST, scales must strictly
	// increase. So storage[0] = coarsest (scale=1), storage[2] =
	// native (scale=4). After inversion, API[0] = native (scale=4).
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{
				[]byte("COARSEST_LAYER_TILE_0"),
			}},
			{xTiles: 2, yTiles: 1, scale: 2, tiles: [][]byte{
				[]byte("MID_LAYER_TILE_0"),
				[]byte("MID_LAYER_TILE_1"),
			}},
			{xTiles: 2, yTiles: 2, scale: 4, tiles: [][]byte{
				[]byte("NATIVE_LAYER_TILE_0_0"),
				[]byte("NATIVE_LAYER_TILE_1_0"),
				[]byte("NATIVE_LAYER_TILE_0_1"),
				[]byte("NATIVE_LAYER_TILE_1_1"),
			}},
		},
	}
	data, _ := sb.build()
	tiler := openIFETiler(t, data)
	defer tiler.Close()

	levels := tiler.Pyramids()[0].Levels
	if len(levels) != 3 {
		t.Fatalf("level count = %d, want 3", len(levels))
	}

	// Levels[0] is native (scale=4 in storage).
	if got, want := levels[0].Grid, (opentile.Size{W: 2, H: 2}); got != want {
		t.Errorf("L0 (native) Grid = %v, want %v", got, want)
	}
	if got, want := levels[2].Grid, (opentile.Size{W: 1, H: 1}); got != want {
		t.Errorf("L2 (coarsest) Grid = %v, want %v", got, want)
	}

	// Read tile bytes and confirm they're the native layer's bytes.
	b00, err := tiler.levelImpls[0].Tile(0, 0)
	if err != nil {
		t.Fatalf("L0 Tile(0,0): %v", err)
	}
	if string(b00) != "NATIVE_LAYER_TILE_0_0" {
		t.Errorf("L0 (0,0) bytes = %q, want NATIVE_LAYER_TILE_0_0", b00)
	}
	b11, err := tiler.levelImpls[0].Tile(1, 1)
	if err != nil {
		t.Fatalf("L0 Tile(1,1): %v", err)
	}
	if string(b11) != "NATIVE_LAYER_TILE_1_1" {
		t.Errorf("L0 (1,1) bytes = %q", b11)
	}

	// Coarsest single tile.
	bC, err := tiler.levelImpls[2].Tile(0, 0)
	if err != nil {
		t.Fatalf("L2 Tile: %v", err)
	}
	if string(bC) != "COARSEST_LAYER_TILE_0" {
		t.Errorf("L2 bytes = %q", bC)
	}

	// Mid layer 1×2 grid.
	bM0, _ := tiler.levelImpls[1].Tile(0, 0)
	if string(bM0) != "MID_LAYER_TILE_0" {
		t.Errorf("L1 (0,0) bytes = %q", bM0)
	}
	bM1, _ := tiler.levelImpls[1].Tile(1, 0)
	if string(bM1) != "MID_LAYER_TILE_1" {
		t.Errorf("L1 (1,0) bytes = %q", bM1)
	}
}

// TestSynthSparse verifies that sparse-tile entries (Offset ==
// NullTile) propagate to ErrSparseTile via TileError, and TileReader
// is consistent.
func TestSynthSparse(t *testing.T) {
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("L0")}},
			{xTiles: 2, yTiles: 1, scale: 2, tiles: [][]byte{
				[]byte("PRESENT"),
				nil, // sparse
			}},
		},
	}
	data, _ := sb.build()
	tiler := openIFETiler(t, data)
	defer tiler.Close()

	// Native level (scale=2 in storage = API L0) has the sparse tile
	// at (1, 0).
	l0 := tiler.levelImpls[0]
	bp, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("present tile err: %v", err)
	}
	if string(bp) != "PRESENT" {
		t.Errorf("present tile bytes = %q", bp)
	}

	_, err = l0.Tile(1, 0)
	if !errors.Is(err, opentile.ErrSparseTile) {
		t.Errorf("sparse Tile: got %v, want ErrSparseTile", err)
	}
	// TileReader on a sparse tile must surface the same sentinel
	// rather than returning an empty stream.
	_, err = l0.TileReader(1, 0)
	if !errors.Is(err, opentile.ErrSparseTile) {
		t.Errorf("sparse TileReader: got %v, want ErrSparseTile", err)
	}
}

// TestSynthIrisEncoding confirms a TILE_ENCODING_IRIS file opens with
// CompressionIRIS surfaced (passthrough; opentile-go doesn't decode).
func TestSynthIrisEncoding(t *testing.T) {
	sb := &synthBuilder{
		encoding: encodingIRIS,
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("IRIS")}},
		},
	}
	data, _ := sb.build()
	tiler := openIFETiler(t, data)
	defer tiler.Close()
	if got := tiler.Pyramids()[0].Levels[0].Compression; got != opentile.CompressionIRIS {
		t.Errorf("compression = %v, want CompressionIRIS", got)
	}
}

// TestSynthAvifEncoding confirms a TILE_ENCODING_AVIF file opens with
// CompressionAVIF surfaced.
func TestSynthAvifEncoding(t *testing.T) {
	sb := &synthBuilder{
		encoding: encodingAVIF,
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("AVIF")}},
		},
	}
	data, _ := sb.build()
	tiler := openIFETiler(t, data)
	defer tiler.Close()
	if got := tiler.Pyramids()[0].Levels[0].Compression; got != opentile.CompressionAVIF {
		t.Errorf("compression = %v, want CompressionAVIF", got)
	}
}

// TestSynthTilesIterator confirms the Tiles seq2 iterator yields
// every (col, row) in row-major order with the expected tile bytes
// in the right slots.
func TestSynthTilesIterator(t *testing.T) {
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 3, yTiles: 2, scale: 1, tiles: [][]byte{
				[]byte("00"), []byte("10"), []byte("20"),
				[]byte("01"), []byte("11"), []byte("21"),
			}},
		},
	}
	data, _ := sb.build()
	tiler := openIFETiler(t, data)
	defer tiler.Close()

	var positions []opentile.TilePos
	var bodies []string
	for pos, res := range tiler.levelImpls[0].Tiles(context.Background()) {
		if res.Err != nil {
			t.Errorf("iter err at %v: %v", pos, res.Err)
			break
		}
		positions = append(positions, pos)
		bodies = append(bodies, string(res.Bytes))
	}
	if len(positions) != 6 {
		t.Errorf("position count = %d, want 6", len(positions))
	}
	wantPositions := []opentile.TilePos{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0},
		{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 2, Y: 1},
	}
	for i, w := range wantPositions {
		if positions[i] != w {
			t.Errorf("pos[%d] = %v, want %v", i, positions[i], w)
		}
	}
	wantBodies := []string{"00", "10", "20", "01", "11", "21"}
	for i, w := range wantBodies {
		if bodies[i] != w {
			t.Errorf("body[%d] = %q, want %q", i, bodies[i], w)
		}
	}
}

// TestSynthOpenRejects covers the spec-violation error paths via the
// public Factory.OpenRaw boundary.
func TestSynthOpenRejects(t *testing.T) {
	t.Run("bad encoding", func(t *testing.T) {
		sb := &synthBuilder{
			encoding: 7, // invalid
			layers: []synthLayer{
				{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("x")}},
			},
		}
		data, _ := sb.build()
		_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
		if err == nil {
			t.Fatal("want error on encoding=7")
		}
	})
	t.Run("undefined encoding", func(t *testing.T) {
		// The synthBuilder default-fills encoding=JPEG when zero, so
		// this case requires a manual override path: write the byte
		// directly.
		sb := &synthBuilder{
			encoding: 1, // IRIS (valid; placeholder for the next step)
			layers: []synthLayer{
				{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("x")}},
			},
		}
		data, _ := sb.build()
		// Now poke encoding=0 (TILE_ENCODING_UNDEFINED) at the
		// TILE_TABLE byte offset 10 + the table's own offset (38).
		data[fileHeaderSize+10] = encodingUndefined
		_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
		if err == nil {
			t.Fatal("want error on encoding=0")
		}
	})
	t.Run("strict layer order", func(t *testing.T) {
		// Synthbuilder doesn't sort, so passing decreasing scales
		// produces a file the reader rejects.
		sb := &synthBuilder{
			layers: []synthLayer{
				{xTiles: 1, yTiles: 1, scale: 4, tiles: [][]byte{[]byte("x")}}, // WRONG: should be coarsest
				{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("y")}},
			},
		}
		data, _ := sb.build()
		_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
		if err == nil {
			t.Fatal("want error on decreasing scales")
		}
	})
}

// TestFileCloserIntegration verifies that passing the synthetic
// buffer through a bytes.Reader (no fileCloser wrapper) round-trips
// cleanly via the Factory boundary, mimicking what opentile.OpenFile
// would do.
func TestSyntheticFactoryRoundtrip(t *testing.T) {
	sb := &synthBuilder{
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1, tiles: [][]byte{[]byte("L0")}},
		},
	}
	data, _ := sb.build()

	f := New()
	r := bytes.NewReader(data)
	if !f.SupportsRaw(r, int64(len(data))) {
		t.Fatal("SupportsRaw on synthetic returned false")
	}
	tiler, err := f.OpenRaw(r, int64(len(data)), &opentile.Config{})
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer tiler.Close()
	b, err := tiler.levelImpls[0].Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if string(b) != "L0" {
		t.Errorf("bytes = %q, want L0", b)
	}
	// Sanity: synth size byte.
	_ = io.SeekStart
}
