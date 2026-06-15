//go:build cgo && !nocgo

// This test decodes JPEG via libjpeg-turbo (the oneframe engine and the
// ground-truth comparison), so it is gated to cgo builds — matching the
// repo convention for decode-dependent tests.

package opentile_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// This file gives the OME-TIFF strip-based (non-tiled) level path end-to-end
// integration coverage (GH #24). The reader dispatches per page on TileWidth:
// tiled pages → tiledImage, non-tiled pages → internal/oneframe (decode the
// whole level, crop per tile). Leica-1/Leica-2 are both fully tiled, so no
// committed real fixture exercised the oneframe dispatch. Rather than ship a
// large fixture, we synthesise a tiny in-tree OME-TIFF: a tiled JPEG base page
// with a strip-based JPEG SubIFD level (no TileWidth) that routes through
// oneframe.
//
// The boundary the tests pin: internal/oneframe is JPEG-only, and the OneFrame
// tile size is defaulted from the base page's TileWidth — so a *mixed* file
// (tiled base + strip SubIFD JPEG levels) is readable, but a *pure* strip-based
// OME (non-tiled base) is rejected because there is no base TileWidth to derive
// the tile size from. See docs/formats/ometiff.md.

// omeIFDEntry is one classic-TIFF IFD entry with a precomputed 4-byte value
// field (inline scalar for SHORT/LONG count=1, or an external byte offset).
type omeIFDEntry struct {
	tag, typ uint16
	count    uint32
	val      uint32
}

func omeGradImage(w, h, phase int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.Set(x, y, color.RGBA{
				R: uint8((x*4 + phase) % 256),
				G: uint8((y*4 + phase) % 256),
				B: uint8((x + y) * 2 % 256),
				A: 255,
			})
		}
	}
	return im
}

func omeJPEG(im image.Image) []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, im, &jpeg.Options{Quality: 92})
	return b.Bytes()
}

func omeXML(w, h int) []byte {
	xml := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06">` +
		`<Image ID="Image:0" Name="strip-oneframe-test"><Pixels ID="Pixels:0" ` +
		`DimensionOrder="XYCZT" Type="uint8" SizeX="` + strconv.Itoa(w) + `" SizeY="` +
		strconv.Itoa(h) + `" SizeC="3" SizeZ="1" SizeT="1"/></Image></OME>`
	return append([]byte(xml), 0) // NUL-terminated TIFF ASCII
}

// writeClassicIFD writes a little-endian classic-TIFF IFD (entry count, 12-byte
// entries in ascending-tag order, next-IFD pointer) into buf at offset at.
func writeClassicIFD(buf []byte, at uint32, entries []omeIFDEntry, next uint32) {
	le := binary.LittleEndian
	le.PutUint16(buf[at:], uint16(len(entries)))
	p := at + 2
	for _, e := range entries {
		le.PutUint16(buf[p:], e.tag)
		le.PutUint16(buf[p+2:], e.typ)
		le.PutUint32(buf[p+4:], e.count)
		le.PutUint32(buf[p+8:], e.val) // SHORT scalars sit in the low 2 bytes (LE = left-justified)
		p += 12
	}
	le.PutUint32(buf[p:], next)
}

// buildMixedStripOME builds a tiled JPEG base page (128×128, 64×64 tiles) with a
// strip-based JPEG SubIFD level (64×64, one strip, no TileWidth) — the SubIFD
// routes through the oneframe engine. Returns the file bytes and the SubIFD
// level's JPEG (so the test can compare oneframe output against a direct decode).
func buildMixedStripOME() (file, subJPEG []byte) {
	const baseDim, tileDim, subDim = 128, 64, 64
	baseJPEG := omeJPEG(omeGradImage(tileDim, tileDim, 0)) // one 64×64 tile, reused 4× (base is never decoded by the test)
	subJPEG = omeJPEG(omeGradImage(subDim, subDim, 97))
	xml := omeXML(baseDim, baseDim)
	le := binary.LittleEndian

	const ifd0N, ifd1N = 12, 9
	ifd0Off := uint32(8)
	ext0Off := ifd0Off + uint32(2+12*ifd0N+4)
	bps0Off := ext0Off
	xmlOff := bps0Off + 6
	baseJPEGOff := xmlOff + uint32(len(xml))
	tileOffArrOff := baseJPEGOff + uint32(len(baseJPEG))
	tileCntArrOff := tileOffArrOff + 16 // 4 × LONG
	ifd1Off := tileCntArrOff + 16
	ext1Off := ifd1Off + uint32(2+12*ifd1N+4)
	bps1Off := ext1Off
	subJPEGOff := bps1Off + 6
	total := subJPEGOff + uint32(len(subJPEG))

	buf := make([]byte, total)
	copy(buf[0:2], "II")
	le.PutUint16(buf[2:], 42)
	le.PutUint32(buf[4:], ifd0Off)

	writeClassicIFD(buf, ifd0Off, []omeIFDEntry{
		{256, 3, 1, baseDim}, {257, 3, 1, baseDim}, {258, 3, 3, bps0Off},
		{259, 3, 1, 7}, {262, 3, 1, 6}, {270, 2, uint32(len(xml)), xmlOff},
		{277, 3, 1, 3}, {322, 3, 1, tileDim}, {323, 3, 1, tileDim},
		{324, 4, 4, tileOffArrOff}, {325, 4, 4, tileCntArrOff}, {330, 4, 1, ifd1Off},
	}, 0)
	le.PutUint16(buf[bps0Off:], 8)
	le.PutUint16(buf[bps0Off+2:], 8)
	le.PutUint16(buf[bps0Off+4:], 8)
	copy(buf[xmlOff:], xml)
	copy(buf[baseJPEGOff:], baseJPEG)
	for i := uint32(0); i < 4; i++ { // 4 tiles all point at the one base JPEG
		le.PutUint32(buf[tileOffArrOff+i*4:], baseJPEGOff)
		le.PutUint32(buf[tileCntArrOff+i*4:], uint32(len(baseJPEG)))
	}

	writeClassicIFD(buf, ifd1Off, []omeIFDEntry{
		{256, 3, 1, subDim}, {257, 3, 1, subDim}, {258, 3, 3, bps1Off},
		{259, 3, 1, 7}, {262, 3, 1, 6}, {273, 4, 1, subJPEGOff}, {277, 3, 1, 3},
		{278, 3, 1, subDim}, {279, 4, 1, uint32(len(subJPEG))},
	}, 0)
	le.PutUint16(buf[bps1Off:], 8)
	le.PutUint16(buf[bps1Off+2:], 8)
	le.PutUint16(buf[bps1Off+4:], 8)
	copy(buf[subJPEGOff:], subJPEG)
	return buf, subJPEG
}

// buildStripOnlyOME builds a single strip-based (non-tiled) OME page — no
// TileWidth, no SubIFD pyramid. Used to pin the documented boundary: a
// pure strip-based OME is rejected at Open (no base TileWidth to derive the
// OneFrame tile size from).
func buildStripOnlyOME() []byte {
	const dim = 64
	j := omeJPEG(omeGradImage(dim, dim, 0))
	xml := omeXML(dim, dim)
	le := binary.LittleEndian

	const ifdN = 10
	ifdOff := uint32(8)
	extOff := ifdOff + uint32(2+12*ifdN+4)
	bpsOff := extOff
	xmlOff := bpsOff + 6
	jOff := xmlOff + uint32(len(xml))
	total := jOff + uint32(len(j))

	buf := make([]byte, total)
	copy(buf[0:2], "II")
	le.PutUint16(buf[2:], 42)
	le.PutUint32(buf[4:], ifdOff)
	writeClassicIFD(buf, ifdOff, []omeIFDEntry{
		{256, 3, 1, dim}, {257, 3, 1, dim}, {258, 3, 3, bpsOff},
		{259, 3, 1, 7}, {262, 3, 1, 6}, {270, 2, uint32(len(xml)), xmlOff},
		{273, 4, 1, jOff}, {277, 3, 1, 3}, {278, 3, 1, dim}, {279, 4, 1, uint32(len(j))},
	}, 0)
	le.PutUint16(buf[bpsOff:], 8)
	le.PutUint16(buf[bpsOff+2:], 8)
	le.PutUint16(buf[bpsOff+4:], 8)
	copy(buf[xmlOff:], xml)
	copy(buf[jOff:], j)
	return buf
}

// TestOMEStripBasedOneFrameLevel covers the OME strip-based (non-tiled) level
// read path end-to-end through the public API (GH #24). A tiled base page +
// strip-based JPEG SubIFD level opens as a 2-level pyramid; the strip level is
// served by the oneframe engine, and its decoded pixels match a direct decode
// of the level's JPEG.
func TestOMEStripBasedOneFrameLevel(t *testing.T) {
	data, subJPEG := buildMixedStripOME()
	s, err := opentile.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open mixed strip OME: %v", err)
	}
	defer s.Close()

	if s.Format() != opentile.FormatOMETIFF {
		t.Fatalf("Format = %s, want ome-tiff", s.Format())
	}
	if n := len(s.Pyramids()); n != 1 {
		t.Fatalf("pyramids = %d, want 1", n)
	}
	levels := s.Pyramid(0).Levels
	if len(levels) != 2 {
		t.Fatalf("levels = %d, want 2 (tiled base + strip SubIFD)", len(levels))
	}
	// L0 is the tiled base (64×64 tiles over 128×128).
	if levels[0].Size != (opentile.Size{W: 128, H: 128}) || levels[0].TileSize != (opentile.Size{W: 64, H: 64}) {
		t.Errorf("L0 = %v tile %v, want 128x128 tile 64x64", levels[0].Size, levels[0].TileSize)
	}
	// L1 is the strip-based SubIFD (no TileWidth → oneframe). Its tile size is
	// defaulted from the base page's TileWidth (64×64).
	if levels[1].Size != (opentile.Size{W: 64, H: 64}) {
		t.Errorf("L1 size = %v, want 64x64", levels[1].Size)
	}
	if levels[1].TileSize != (opentile.Size{W: 64, H: 64}) {
		t.Errorf("L1 tile size = %v, want 64x64 (defaulted from base TileWidth)", levels[1].TileSize)
	}
	if levels[1].Downsample != 2 {
		t.Errorf("L1 downsample = %v, want 2", levels[1].Downsample)
	}

	lv1, err := s.Level(1)
	if err != nil {
		t.Fatalf("Level(1): %v", err)
	}
	got, err := lv1.DecodedTile(0, 0, opentile.WithFormat(decoder.PixelFormatRGB))
	if err != nil {
		t.Fatalf("L1 DecodedTile via oneframe: %v", err)
	}
	if got.Width != 64 || got.Height != 64 {
		t.Fatalf("oneframe tile = %dx%d, want 64x64", got.Width, got.Height)
	}

	// Ground truth: decode the SubIFD level's JPEG directly. oneframe performs a
	// lossless MCU-aligned crop of the whole frame, so the whole-level tile must
	// be byte-identical to a plain decode of the same JPEG.
	fac, ok := decoder.GetByCompressionTag(7)
	if !ok {
		t.Skip("no JPEG decoder registered")
	}
	dec := fac.New()
	defer dec.Close()
	want, err := dec.Decode(subJPEG, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
	if err != nil {
		t.Fatalf("direct decode of SubIFD JPEG: %v", err)
	}
	if !bytes.Equal(got.Pix[:got.Height*got.Width*3], want.Pix[:want.Height*want.Width*3]) {
		t.Error("oneframe-decoded tile is not byte-identical to a direct decode of the SubIFD JPEG")
	}

	// ReadRegion over the oneframe level returns the same pixels for a sub-rect.
	reg, err := lv1.ReadRegion(opentile.Region{Origin: opentile.Point{X: 8, Y: 8}, Size: opentile.Size{W: 32, H: 16}},
		opentile.WithFormat(decoder.PixelFormatRGB))
	if err != nil {
		t.Fatalf("L1 ReadRegion: %v", err)
	}
	if reg.Width != 32 || reg.Height != 16 {
		t.Fatalf("ReadRegion = %dx%d, want 32x16", reg.Width, reg.Height)
	}
	for y := 0; y < 16; y++ {
		so := (y + 8) * want.Stride
		do := y * reg.Stride
		if !bytes.Equal(reg.Pix[do:do+32*3], want.Pix[so+8*3:so+8*3+32*3]) {
			t.Errorf("ReadRegion row %d differs from the directly-decoded level", y)
			break
		}
	}
}

// TestOMEStripOnlyBaseRejected pins the documented boundary (GH #24 item 3): a
// pure strip-based OME (non-tiled base page) cannot be opened, because the
// OneFrame tile size is derived from the base page's TileWidth, which a
// strip-only base lacks.
func TestOMEStripOnlyBaseRejected(t *testing.T) {
	data := buildStripOnlyOME()
	_, err := opentile.Open(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("expected strip-only-base OME to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "TileWidth") {
		t.Errorf("error %q does not mention the missing base TileWidth boundary", err)
	}
}
