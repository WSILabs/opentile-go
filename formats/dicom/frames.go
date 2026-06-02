package dicom

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// span is the byte range of one frame's compressed data in the instance.
type span struct {
	off    int
	length int
}

// walkEncapsulatedFrames locates the encapsulated PixelData (VR OB,
// undefined length) and walks its fragment items, returning one span per
// frame. Assumes one fragment per frame (true for all v1 fixtures; a
// future multi-fragment case is unsupported). The Basic Offset Table item
// (first item) is skipped; opentile-go always derives offsets from this
// walk rather than the BOT (empty across all observed scanners).
func walkEncapsulatedFrames(b []byte) ([]span, error) {
	sig := []byte{0xE0, 0x7F, 0x10, 0x00, 0x4F, 0x42, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF}
	pos := bytes.Index(b, sig)
	if pos < 0 {
		return nil, fmt.Errorf("dicom: encapsulated PixelData (OB) not found")
	}
	p := pos + len(sig)
	itemTag := []byte{0xFE, 0xFF, 0x00, 0xE0}
	seqDelim := []byte{0xFE, 0xFF, 0xDD, 0xE0}
	var frames []span
	first := true
	for p+8 <= len(b) {
		t := b[p : p+4]
		if bytes.Equal(t, seqDelim) {
			break
		}
		if !bytes.Equal(t, itemTag) {
			return nil, fmt.Errorf("dicom: unexpected item tag at %d: % x", p, t)
		}
		length := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		p += 8
		if p+length > len(b) {
			return nil, fmt.Errorf("dicom: fragment at %d overruns file", p)
		}
		if first {
			first = false // skip BOT
			p += length
			continue
		}
		frames = append(frames, span{off: p, length: length})
		p += length
	}
	return frames, nil
}
