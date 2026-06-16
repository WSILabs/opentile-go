package j2kheader

import (
	"encoding/binary"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// buildCodestream assembles a minimal raw J2K main header: SOC + SIZ + COD.
func buildCodestream(components int, ssiz0 byte, mct, reversible byte) []byte {
	var b []byte
	be16 := func(v uint16) []byte { x := make([]byte, 2); binary.BigEndian.PutUint16(x, v); return x }

	// SOC
	b = append(b, 0xFF, mrkSOC)

	// SIZ
	siz := []byte{0xFF, mrkSIZ}
	lsiz := uint16(38 + 3*components)
	siz = append(siz, be16(lsiz)...) // Lsiz
	siz = append(siz, be16(0)...)    // Rsiz
	for i := 0; i < 8; i++ {         // Xsiz..YTOsiz (8 × 4 bytes)
		siz = append(siz, 0, 0, 0, 0)
	}
	siz = append(siz, be16(uint16(components))...) // Csiz
	for i := 0; i < components; i++ {
		siz = append(siz, ssiz0, 1, 1) // Ssiz, XRsiz, YRsiz
	}
	b = append(b, siz...)

	// COD
	cod := []byte{0xFF, mrkCOD}
	cod = append(cod, be16(12)...)            // Lcod
	cod = append(cod, 0x00)                   // Scod
	cod = append(cod, 0x00, 0x00, 0x01)       // SGcod: progression, layers(2)
	cod = append(cod, mct)                    // SGcod: MCT
	cod = append(cod, 0x05, 0x04, 0x04, 0x00) // SPcod: decomp, cbw, cbh, style
	cod = append(cod, reversible)             // SPcod: transformation
	b = append(b, cod...)

	// SOT to terminate the main header.
	b = append(b, 0xFF, mrkSOT)
	return b
}

func TestParseRawReversibleRGB(t *testing.T) {
	cs := buildCodestream(3, 0x07, 1, 1) // 3 comps, bitdepth 8, MCT on, 5/3 reversible
	got, err := Parse(cs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Components != 3 || got.BitDepth != 8 || !got.Reversible || !got.MCT || got.Boxed {
		t.Fatalf("got %+v, want comps=3 depth=8 reversible MCT raw", got)
	}
	// All components XRsiz=YRsiz=1 (buildCodestream default) → 4:4:4.
	if cs := got.CodestreamInfo().ChromaSubsampling; cs != decoder.Subsampling444 {
		t.Errorf("ChromaSubsampling = %s, want 4:4:4", cs)
	}
}

func TestParseRawIrreversible(t *testing.T) {
	cs := buildCodestream(3, 0x0B, 1, 0) // bitdepth 12, MCT on, 9/7 irreversible
	got, err := Parse(cs)
	if err != nil {
		t.Fatal(err)
	}
	if got.BitDepth != 12 || got.Reversible || !got.MCT {
		t.Fatalf("got %+v, want depth=12 irreversible MCT", got)
	}
}

func TestParseGrayscaleNoMCT(t *testing.T) {
	cs := buildCodestream(1, 0x07, 0, 1) // 1 comp, no MCT
	got, err := Parse(cs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Components != 1 || got.MCT {
		t.Fatalf("got %+v, want comps=1 no MCT", got)
	}
}

func TestParseBoxedJP2(t *testing.T) {
	cs := buildCodestream(3, 0x07, 1, 0)
	// JP2 signature box + a jp2h superbox with a colr (EnumCS=16 sRGB) + a jp2c
	// box wrapping the codestream.
	be32 := func(v uint32) []byte { x := make([]byte, 4); binary.BigEndian.PutUint32(x, v); return x }
	sig := []byte{0x00, 0x00, 0x00, 0x0C, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}
	colr := append([]byte{0x00, 0x00, 0x00, 0x0F, 'c', 'o', 'l', 'r', 0x01, 0x00, 0x00}, be32(16)...)
	jp2h := append(append([]byte{}, be32(uint32(8+len(colr)))...), []byte("jp2h")...)
	jp2h = append(jp2h, colr...)
	jp2c := append(append([]byte{}, be32(uint32(8+len(cs)))...), []byte("jp2c")...)
	jp2c = append(jp2c, cs...)
	boxed := append(append(append([]byte{}, sig...), jp2h...), jp2c...)

	got, err := Parse(boxed)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Boxed || got.Components != 3 || got.EnumColorspace != 16 {
		t.Fatalf("got %+v, want boxed comps=3 enumCS=16", got)
	}
}

func TestParseNotJ2K(t *testing.T) {
	if _, err := Parse([]byte{0xFF, 0xD8, 0xFF, 0xC0}); err == nil { // JPEG SOI
		t.Fatal("expected ErrNotJ2K on JPEG bytes")
	}
}
