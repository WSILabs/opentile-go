package svs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
	"github.com/wsilabs/opentile-go/opentile/opentiletest"
)

// buildSVSTIFF builds a TIFF with one tiled page carrying tileCount*tileCount
// synthetic tile payloads (each unique). Returns (bytes, tileBytes[idx]).
// The ImageDescription starts with "Aperio" so the SVS factory accepts it.
func buildSVSTIFF(t *testing.T, tileW, tileH, tilesX, tilesY int, extraDesc string) (data []byte, tiles [][]byte) {
	t.Helper()
	// Build tiles: each is a unique byte pattern of length 32.
	nTiles := tilesX * tilesY
	tiles = make([][]byte, nTiles)
	for i := 0; i < nTiles; i++ {
		buf := make([]byte, 32)
		for j := range buf {
			buf[j] = byte(i*7 + j) // arbitrary deterministic
		}
		tiles[i] = buf
	}
	desc := "Aperio Test\n"
	if extraDesc != "" {
		desc += extraDesc
	}
	descBytes := append([]byte(desc), 0)

	// Layout: Header (8) + IFD at 8 + external data after.
	// IFD entries: ImageWidth, ImageLength, Compression, Photometric,
	// ImageDescription, TileWidth, TileLength, TileOffsets, TileByteCounts = 9
	// IFD size = 2 + 9*12 + 4 = 114
	ifdStart := uint32(8)
	extStart := ifdStart + 114

	descOff := extStart
	extAfterDesc := descOff + uint32(len(descBytes))

	tileBCOff := extAfterDesc
	extAfterBC := tileBCOff + uint32(4*nTiles)

	tileOffOff := extAfterBC
	extAfterTO := tileOffOff + uint32(4*nTiles)

	// Tile data offsets: pack consecutively starting at extAfterTO.
	tileOffsets := make([]uint32, nTiles)
	off := extAfterTO
	for i := range tiles {
		tileOffsets[i] = off
		off += uint32(len(tiles[i]))
	}

	buf := new(bytes.Buffer)
	w16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
	w32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
	w16(9)
	entry := func(tag, typ uint16, count, voc uint32) {
		w16(tag)
		w16(typ)
		w32(count)
		w32(voc)
	}
	entry(256, 3, 1, uint32(tileW*tilesX)) // ImageWidth
	entry(257, 3, 1, uint32(tileH*tilesY)) // ImageLength
	entry(259, 3, 1, 7)                    // Compression = JPEG
	entry(262, 3, 1, 6)                    // Photometric = YCbCr
	entry(270, 2, uint32(len(descBytes)), descOff)
	entry(322, 3, 1, uint32(tileW))
	entry(323, 3, 1, uint32(tileH))
	entry(324, 4, uint32(nTiles), tileOffOff)
	entry(325, 4, uint32(nTiles), tileBCOff)
	w32(0) // next IFD

	// External region
	buf.Write(descBytes)
	// Write TileByteCounts
	for _, tb := range tiles {
		w32(uint32(len(tb)))
	}
	// Write TileOffsets
	for _, o := range tileOffsets {
		w32(o)
	}
	// Finally, write tile payloads in the same order.
	for _, tb := range tiles {
		buf.Write(tb)
	}
	return buf.Bytes(), tiles
}

func TestSvsTilerOpenAndLevel(t *testing.T) {
	data, tiles := buildSVSTIFF(t, 16, 16, 3, 2, "AppMag = 20|MPP = 0.5")
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("svs.New().Open: %v", err)
	}
	defer tiler.Close()

	levels := tiler.Levels()
	if len(levels) != 1 {
		t.Fatalf("levels: got %d, want 1", len(levels))
	}
	lvl, err := tiler.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	if got := lvl.TileSize(); got.W != 16 || got.H != 16 {
		t.Errorf("TileSize: got %v, want 16x16", got)
	}
	if got := lvl.Grid(); got.W != 3 || got.H != 2 {
		t.Errorf("Grid: got %v, want 3x2", got)
	}
	// Tile (0,0) → first tile payload
	b, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0): %v", err)
	}
	if !bytes.Equal(b, tiles[0]) {
		t.Fatalf("Tile(0,0) bytes mismatch")
	}
	// Tile (2,1) → last tile (index 5)
	b, err = lvl.Tile(2, 1)
	if err != nil {
		t.Fatalf("Tile(2,1): %v", err)
	}
	if !bytes.Equal(b, tiles[5]) {
		t.Fatalf("Tile(2,1) bytes mismatch")
	}
}

func TestSvsLevelTileOutOfBounds(t *testing.T) {
	data, _ := buildSVSTIFF(t, 16, 16, 2, 2, "")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, _ := New().Open(f, cfg)
	lvl, _ := tiler.Level(0)
	_, err := lvl.Tile(99, 99)
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Fatalf("expected ErrTileOutOfBounds, got %v", err)
	}
	var te *opentile.TileError
	if !errors.As(err, &te) {
		t.Fatal("expected TileError wrapping")
	}
	if te.X != 99 || te.Y != 99 {
		t.Errorf("TileError coords: got %d,%d", te.X, te.Y)
	}
}

func TestSvsLevelTilesIterator(t *testing.T) {
	data, tiles := buildSVSTIFF(t, 16, 16, 2, 2, "")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, _ := New().Open(f, cfg)
	lvl, _ := tiler.Level(0)

	ctx := context.Background()
	count := 0
	for pos, res := range lvl.Tiles(ctx) {
		if res.Err != nil {
			t.Fatalf("Tiles err at %v: %v", pos, res.Err)
		}
		idx := pos.Y*2 + pos.X
		if !bytes.Equal(res.Bytes, tiles[idx]) {
			t.Errorf("mismatch at %v", pos)
		}
		count++
	}
	if count != 4 {
		t.Errorf("count: got %d, want 4", count)
	}
}

func TestSvsLevelTileReader(t *testing.T) {
	data, tiles := buildSVSTIFF(t, 16, 16, 2, 2, "")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, _ := New().Open(f, cfg)
	lvl, _ := tiler.Level(0)
	rc, err := lvl.TileReader(1, 1)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, tiles[3]) {
		t.Fatalf("TileReader(1,1) bytes mismatch")
	}
}

// truncatingReaderAt wraps an io.ReaderAt and returns (n, io.EOF) when a read
// lands exactly at the reader's end, even if all requested bytes were delivered.
// Mirrors bytes.Reader.ReadAt semantics on boundary reads.
type truncatingReaderAt struct {
	r    io.ReaderAt
	size int64
}

func (t *truncatingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := t.r.ReadAt(p, off)
	if err == nil && off+int64(n) == t.size {
		return n, io.EOF
	}
	return n, err
}

func TestSvsLevelTileBenignEOF(t *testing.T) {
	// Use a 2×1 grid so TileOffsets/TileByteCounts have count=2 and are stored
	// externally (2*4=8 > 4 bytes inline limit). The second tile (x=1,y=0)
	// occupies the very last bytes of the file, so the truncatingReaderAt wrapper
	// surfaces (n=len(buf), io.EOF) on that read, exercising the benign-EOF path.
	data, tiles := buildSVSTIFF(t, 16, 16, 2, 1, "")
	// Wrap the reader so the final boundary read surfaces (n, io.EOF).
	base := bytes.NewReader(data)
	trunc := &truncatingReaderAt{r: base, size: int64(len(data))}
	f, err := tiff.Open(trunc, int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()
	lvl, _ := tiler.Level(0)
	// Tile (1,0) is the last tile and lands exactly at end-of-file.
	got, err := lvl.Tile(1, 0)
	if err != nil {
		t.Fatalf("Tile: unexpected error (likely benign-EOF bug): %v", err)
	}
	if !bytes.Equal(got, tiles[1]) {
		t.Fatal("tile bytes mismatch on benign-EOF path")
	}
}

func TestMetadataOfExtractsAperioExtras(t *testing.T) {
	data, _ := buildSVSTIFF(t, 16, 16, 1, 1, "AppMag = 40|MPP = 0.25|Filename = slide-x")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()

	md, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf: expected ok=true for SVS tiler")
	}
	if md.MPP != 0.25 {
		t.Errorf("MPP: got %v, want 0.25", md.MPP)
	}
	if md.Filename != "slide-x" {
		t.Errorf("Filename: got %q, want slide-x", md.Filename)
	}
	if md.Magnification != 40 {
		t.Errorf("Magnification: got %v, want 40", md.Magnification)
	}
}

func TestMetadataOfRejectsNonSVSTiler(t *testing.T) {
	// An arbitrary opentile.Tiler that is not *svs.tiler — use a zero-value
	// fakeTiler implementation.
	fake := &fakeNonSVSTiler{}
	_, ok := MetadataOf(fake)
	if ok {
		t.Fatal("MetadataOf: expected ok=false for non-SVS Tiler")
	}
}

type fakeNonSVSTiler struct{}

func (f *fakeNonSVSTiler) Format() opentile.Format { return opentile.Format("fake") }
func (f *fakeNonSVSTiler) Images() []opentile.Image {
	return []opentile.Image{opentile.NewSingleImage(nil)}
}
func (f *fakeNonSVSTiler) Levels() []opentile.Level { return nil }
func (f *fakeNonSVSTiler) Level(i int) (opentile.Level, error) {
	return nil, opentile.ErrLevelOutOfRange
}
func (f *fakeNonSVSTiler) Associated() []opentile.AssociatedImage { return nil }
func (f *fakeNonSVSTiler) Metadata() opentile.Metadata            { return opentile.Metadata{} }
func (f *fakeNonSVSTiler) ICCProfile() []byte                     { return nil }
func (f *fakeNonSVSTiler) Close() error                           { return nil }
func (f *fakeNonSVSTiler) WarmLevel(int) error                    { return nil }

// buildSVSTIFFWithStrippedPage builds a 2-page SVS-like TIFF where page 0 is
// tiled (a normal level) and page 1 is non-tiled (simulates a thumbnail /
// label / macro). The non-tiled page has ImageWidth/Length/Compression but
// omits TileWidth/TileLength.
func buildSVSTIFFWithStrippedPage(t *testing.T) (data []byte, tiles [][]byte) {
	t.Helper()
	// Build a 1-tile tiled page's worth of synthetic tile data first.
	nTiles := 1
	tiles = make([][]byte, nTiles)
	for i := 0; i < nTiles; i++ {
		buf := make([]byte, 16)
		for j := range buf {
			buf[j] = byte(i*3 + j)
		}
		tiles[i] = buf
	}

	desc := []byte("Aperio Test\x00")
	stripBytes := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}

	// Layout:
	// Offset 0-7:    TIFF Header (II 42 0x08)
	// Offset 8-121:  Page 0 IFD (9 entries, size 2+9*12+4=114)
	// Offset 122-199: Page 1 IFD (6 entries, size 2+6*12+4=78)
	// Offset 200+:   External data (description, TileByteCounts, TileOffsets, tiles, strips)

	page0IFDOff := uint32(8)
	page1IFDOff := page0IFDOff + 114 // 122
	extDataOff := page1IFDOff + 78   // 200

	descOff := extDataOff
	descEnd := descOff + uint32(len(desc))

	tileBCOff := descEnd
	tileBCEnd := tileBCOff + 4*uint32(nTiles)

	tileOffOff := tileBCEnd
	tileOffEnd := tileOffOff + 4*uint32(nTiles)

	tileDataOff := tileOffEnd
	tileDataEnd := tileDataOff
	tileOffsets := make([]uint32, nTiles)
	for i := range tiles {
		tileOffsets[i] = tileDataOff + uint32(i)*uint32(len(tiles[i]))
		tileDataEnd = tileOffsets[i] + uint32(len(tiles[i]))
	}

	stripOff := tileDataEnd

	buf := new(bytes.Buffer)
	w16 := func(v uint16) { buf.WriteByte(byte(v)); buf.WriteByte(byte(v >> 8)) }
	w32 := func(v uint32) {
		buf.WriteByte(byte(v))
		buf.WriteByte(byte(v >> 8))
		buf.WriteByte(byte(v >> 16))
		buf.WriteByte(byte(v >> 24))
	}
	entry := func(tag, typ uint16, count, voc uint32) {
		w16(tag)
		w16(typ)
		w32(count)
		w32(voc)
	}

	// TIFF Header
	buf.Write([]byte{'I', 'I', 42, 0})
	w32(page0IFDOff)

	// Page 0 IFD (tiled)
	w16(9)
	entry(256, 3, 1, 16)                      // ImageWidth = 16
	entry(257, 3, 1, 16)                      // ImageLength = 16
	entry(259, 3, 1, 7)                       // Compression = JPEG
	entry(262, 3, 1, 6)                       // Photometric = YCbCr
	entry(270, 2, uint32(len(desc)), descOff) // ImageDescription
	entry(322, 3, 1, 16)                      // TileWidth = 16
	entry(323, 3, 1, 16)                      // TileLength = 16
	// For nTiles=1: TileOffsets and TileByteCounts values fit inline (4 bytes each)
	entry(324, 4, 1, tileOffsets[0])        // TileOffsets: single value inline
	entry(325, 4, 1, uint32(len(tiles[0]))) // TileByteCounts: single value inline
	w32(page1IFDOff)                        // offset to page 1 IFD

	// Page 1 IFD (stripped—no TileWidth/TileLength)
	w16(6)
	entry(256, 3, 1, 32)                      // ImageWidth = 32
	entry(257, 3, 1, 16)                      // ImageLength = 16
	entry(259, 3, 1, 7)                       // Compression = JPEG
	entry(262, 3, 1, 6)                       // Photometric = YCbCr
	entry(273, 4, 1, stripOff)                // StripOffsets
	entry(279, 4, 1, uint32(len(stripBytes))) // StripByteCounts
	w32(0)                                    // next IFD = 0

	// External data region
	buf.Write(desc)
	for _, tb := range tiles {
		w32(uint32(len(tb)))
	}
	for _, o := range tileOffsets {
		w32(o)
	}
	for _, tb := range tiles {
		buf.Write(tb)
	}
	buf.Write(stripBytes)

	return buf.Bytes(), tiles
}

func TestSvsTilerSkipsNonTiledPages(t *testing.T) {
	data, tiles := buildSVSTIFFWithStrippedPage(t)
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	if len(f.Pages()) != 2 {
		t.Fatalf("expected 2 TIFF pages, got %d", len(f.Pages()))
	}
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("Open: non-tiled page should not cause Open to fail: %v", err)
	}
	defer tiler.Close()
	levels := tiler.Levels()
	if len(levels) != 1 {
		t.Fatalf("levels: got %d, want 1 (non-tiled page should be skipped)", len(levels))
	}
	lvl := levels[0]
	if lvl.Index() != 0 {
		t.Errorf("level Index: got %d, want 0 (contiguous level indexing)", lvl.Index())
	}
	got, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if !bytes.Equal(got, tiles[0]) {
		t.Fatal("tile bytes mismatch on level 0 of mixed-page TIFF")
	}
}

// wrapperTiler is a test double that wraps a Tiler through an UnwrapTiler
// method. Used to verify MetadataOf unwraps arbitrary wrappers.
type wrapperTiler struct {
	opentile.Tiler
}

func (w *wrapperTiler) UnwrapTiler() opentile.Tiler { return w.Tiler }

func TestMetadataOfUnwrapsWrappers(t *testing.T) {
	data, _ := buildSVSTIFF(t, 16, 16, 1, 1, "MPP = 0.25")
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tiler.Close()

	// Wrap the concrete SVS tiler in one level of wrapper.
	wrapped := &wrapperTiler{Tiler: tiler}
	md, ok := MetadataOf(wrapped)
	if !ok {
		t.Fatal("MetadataOf: expected ok=true through one wrapper")
	}
	if md.MPP != 0.25 {
		t.Errorf("MPP through wrapper: got %v, want 0.25", md.MPP)
	}

	// Wrap twice to confirm it walks multiple layers.
	doubleWrapped := &wrapperTiler{Tiler: wrapped}
	md, ok = MetadataOf(doubleWrapped)
	if !ok {
		t.Fatal("MetadataOf: expected ok=true through two wrappers")
	}
	if md.MPP != 0.25 {
		t.Errorf("MPP through double wrapper: got %v, want 0.25", md.MPP)
	}
}

// cyclicTiler is a pathological wrapper whose UnwrapTiler returns itself.
// Used to verify MetadataOf does not spin forever.
type cyclicTiler struct {
	opentile.Tiler
}

func (c *cyclicTiler) UnwrapTiler() opentile.Tiler { return c }

func TestMetadataOfHandlesCyclicUnwrap(t *testing.T) {
	c := &cyclicTiler{}
	done := make(chan struct{})
	go func() {
		_, _ = MetadataOf(c)
		close(done)
	}()
	select {
	case <-done:
		// good — MetadataOf terminated
	case <-time.After(2 * time.Second):
		t.Fatal("MetadataOf did not terminate on a cyclic wrapper")
	}
}

// TestTiffCompressionToOpentileKnown locks in the documented mapping for
// every TIFF compression code SVS slides actually produce. v0.1 had no
// direct test for this — failures would only surface as wrong Compression()
// strings on real fixtures.
func TestTiffCompressionToOpentileKnown(t *testing.T) {
	cases := []struct {
		code uint32
		want opentile.Compression
	}{
		{1, opentile.CompressionNone},
		{5, opentile.CompressionLZW},
		{7, opentile.CompressionJPEG},
		{33003, opentile.CompressionJP2K}, // APERIO_JP2000_YCBC
		{33005, opentile.CompressionJP2K}, // APERIO_JP2000_RGB
	}
	for _, c := range cases {
		if got := tiffCompressionToOpentile(c.code); got != c.want {
			t.Errorf("code %d: got %v, want %v", c.code, got, c.want)
		}
	}
}

// TestTiffCompressionToOpentileUnknown locks in that any code outside the
// supported set falls through to CompressionUnknown rather than panicking
// or silently mapping to a wrong codec. Required by the L3 limitation
// (CompressionUnknown was previously untested).
func TestTiffCompressionToOpentileUnknown(t *testing.T) {
	for _, code := range []uint32{0, 2, 3, 4, 6, 8, 32773, 999, 65535} {
		if got := tiffCompressionToOpentile(code); got != opentile.CompressionUnknown {
			t.Errorf("code %d: got %v, want CompressionUnknown", code, got)
		}
	}
}

// patchSVSCompression rewrites the Compression tag's inline value in a
// buffer produced by buildSVSTIFF. Layout (LE): header (8) + IFD count
// (2) + ImageWidth entry (12) + ImageLength entry (12) + Compression
// entry (12). Within the Compression entry the inline value-or-offset
// cell starts at byte offset 8 (after tag+type+count = 2+2+4). Total:
// 8 + 2 + 12 + 12 + 8 = 42.
func patchSVSCompression(t *testing.T, data []byte, code uint16) {
	t.Helper()
	const compressionValueOffset = 42
	binary.LittleEndian.PutUint16(data[compressionValueOffset:], code)
}

// TestSVSTileReturnsRawForUnknownCompression closes L3: an SVS page with
// an unrecognized Compression tag must still open, advertise the level
// as CompressionUnknown, and return raw tile bytes verbatim from Tile()
// (passthrough — we don't know how to splice tables for an unknown codec
// and we don't pretend to).
func TestSVSTileReturnsRawForUnknownCompression(t *testing.T) {
	data, tiles := buildSVSTIFF(t, 16, 16, 2, 1, "")
	patchSVSCompression(t, data, 999)
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	cfg := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	tiler, err := New().Open(f, cfg)
	if err != nil {
		t.Fatalf("svs.New().Open: %v", err)
	}
	defer tiler.Close()

	lvl, err := tiler.Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	if got := lvl.Compression(); got != opentile.CompressionUnknown {
		t.Errorf("Compression: got %v, want CompressionUnknown", got)
	}
	b, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0): %v", err)
	}
	if !bytes.Equal(b, tiles[0]) {
		t.Errorf("Tile(0,0): unknown compression should passthrough; got %d bytes (first 8: %x), want %d (first 8: %x)",
			len(b), b[:min(8, len(b))], len(tiles[0]), tiles[0][:min(8, len(tiles[0]))])
	}
}
