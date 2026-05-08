package opentile

import (
	"bytes"
	"errors"
	"testing"
)

func TestSpliceJPEGTile_HappyPath(t *testing.T) {
	// body = SOI(FF D8) + APP0(FF E0 00 04 00 00) + SOS(FF DA 01 02) + scan + EOI(FF D9)
	body := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, // pre-SOS metadata
		0xFF, 0xDA, 0x01, 0x02, // SOS + scan
		0xAA, 0xBB, // entropy
		0xFF, 0xD9, // EOI
	}
	// prefix = DQT(FF DB 00 02) + DHT(FF C4 00 02)
	prefix := []byte{0xFF, 0xDB, 0x00, 0x02, 0xFF, 0xC4, 0x00, 0x02}

	out, err := SpliceJPEGTile(prefix, body)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0xFF, 0xD8,
		0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00,
		0xFF, 0xDB, 0x00, 0x02, 0xFF, 0xC4, 0x00, 0x02,
		0xFF, 0xDA, 0x01, 0x02,
		0xAA, 0xBB,
		0xFF, 0xD9,
	}
	if !bytes.Equal(out, want) {
		t.Errorf("output mismatch:\ngot  % x\nwant % x", out, want)
	}
}

func TestSpliceJPEGTile_NilPrefix_ReturnsBodyCopy(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x01, 0xFF, 0xD9}
	out, err := SpliceJPEGTile(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("output != body: % x vs % x", out, body)
	}
	out[0] = 0x00
	if body[0] == 0x00 {
		t.Error("SpliceJPEGTile returned a shared slice; output mutation leaked back to body")
	}
}

func TestSpliceJPEGTile_EmptyPrefix_ReturnsBodyCopy(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x01, 0xFF, 0xD9}
	out, err := SpliceJPEGTile([]byte{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("output != body: % x vs % x", out, body)
	}
}

func TestSpliceJPEGTile_EmptyBody_Errors(t *testing.T) {
	_, err := SpliceJPEGTile([]byte{0xFF, 0xDB}, nil)
	if !errors.Is(err, ErrBadJPEGSplice) {
		t.Errorf("got %v, want ErrBadJPEGSplice", err)
	}
}

func TestSpliceJPEGTile_NoSOS_Errors(t *testing.T) {
	body := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	_, err := SpliceJPEGTile([]byte{0xFF, 0xDB}, body)
	if !errors.Is(err, ErrBadJPEGSplice) {
		t.Errorf("got %v, want ErrBadJPEGSplice", err)
	}
}
