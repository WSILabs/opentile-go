package dicom

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildEncapsulated synthesizes an encapsulated PixelData (OB, undefined
// length): the 12-byte header, an empty Basic Offset Table item, then one
// item per frame, then the sequence delimiter.
func buildEncapsulated(frames [][]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 0x4F, 0x42, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF})
	item := func(data []byte) {
		b.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(data)))
		b.Write(l[:])
		b.Write(data)
	}
	item(nil) // empty BOT
	for _, f := range frames {
		item(f)
	}
	b.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0x00, 0x00, 0x00, 0x00}) // seq delimiter
	return b.Bytes()
}

func TestWalkEncapsulatedFrames(t *testing.T) {
	frames := [][]byte{{0xAA, 0xBB}, {0x01, 0x02, 0x03}, {0xFF}}
	// prefix some bytes so offsets are not trivially small
	blob := append([]byte("PREAMBLE-AND-DATASET"), buildEncapsulated(frames)...)
	spans, err := walkEncapsulatedFrames(blob)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	for i, want := range frames {
		got := blob[spans[i].off : spans[i].off+spans[i].length]
		if !bytes.Equal(got, want) {
			t.Errorf("frame %d = % x, want % x", i, got, want)
		}
	}
}

func TestWalkEncapsulatedFramesMissingSignature(t *testing.T) {
	if _, err := walkEncapsulatedFrames([]byte("no pixel data here")); err == nil {
		t.Fatal("expected error when signature absent")
	}
}

// buildNativePixelData synthesizes a native (uncompressed) PixelData element
// with VR OW (Explicit VR Little Endian): tag + VR + 2 reserved bytes + 4-byte
// length + payload. A few prefix bytes are prepended to exercise offset handling.
func buildNativePixelData(payload []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte("PREFIX"))               // arbitrary dataset prefix bytes
	b.Write([]byte{0xE0, 0x7F, 0x10, 0x00}) // PixelData tag (7FE0,0010)
	b.Write([]byte{0x4F, 0x57})             // VR: OW
	b.Write([]byte{0x00, 0x00})             // 2 reserved bytes
	var l [4]byte
	binary.LittleEndian.PutUint32(l[:], uint32(len(payload)))
	b.Write(l[:])
	b.Write(payload)
	return b.Bytes()
}

func TestExtractFirstFrameEncapsulated(t *testing.T) {
	frames := [][]byte{{0xAA, 0xBB, 0xCC}, {0x01, 0x02}}
	blob := append([]byte("PREAMBLE"), buildEncapsulated(frames)...)
	data, enc, err := extractFirstFrame(blob)
	if err != nil {
		t.Fatalf("extractFirstFrame: %v", err)
	}
	if !enc {
		t.Fatal("encapsulated = false, want true")
	}
	if !bytes.Equal(data, frames[0]) {
		t.Errorf("data = % x, want % x", data, frames[0])
	}
}

func TestExtractFirstFrameNative(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60}
	blob := buildNativePixelData(payload)
	data, enc, err := extractFirstFrame(blob)
	if err != nil {
		t.Fatalf("extractFirstFrame: %v", err)
	}
	if enc {
		t.Fatal("encapsulated = true, want false")
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("data = % x, want % x", data, payload)
	}
}
