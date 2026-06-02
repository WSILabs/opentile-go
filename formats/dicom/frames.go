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

// extractFirstFrame returns the bytes of the first/only frame of an
// instance's PixelData. For encapsulated PixelData (VR OB, undefined
// length) it returns the first fragment. For native/uncompressed PixelData
// (VR OW or OB, defined length) it returns the whole value blob.
// encapsulated reports which form was found.
func extractFirstFrame(b []byte) (data []byte, encapsulated bool, err error) {
	tag := []byte{0xE0, 0x7F, 0x10, 0x00}
	pos := bytes.Index(b, tag)
	if pos < 0 {
		return nil, false, fmt.Errorf("dicom: PixelData tag (7FE0,0010) not found")
	}
	// After the 4-byte tag we need at least 8 bytes: 2-byte VR + 2 reserved + 4-byte length.
	if pos+12 > len(b) {
		return nil, false, fmt.Errorf("dicom: PixelData element too short at %d", pos)
	}
	vr := b[pos+4 : pos+6]
	isOW := bytes.Equal(vr, []byte{0x4F, 0x57}) // "OW"
	isOB := bytes.Equal(vr, []byte{0x4F, 0x42}) // "OB"
	if !isOW && !isOB {
		return nil, false, fmt.Errorf("dicom: PixelData VR is %q, expected OW or OB", string(vr))
	}
	length32 := binary.LittleEndian.Uint32(b[pos+8 : pos+12])
	if length32 == 0xFFFFFFFF {
		// Undefined length → encapsulated; delegate to existing walker.
		spans, werr := walkEncapsulatedFrames(b)
		if werr != nil {
			return nil, false, werr
		}
		if len(spans) == 0 {
			return nil, false, fmt.Errorf("dicom: encapsulated PixelData has no frames")
		}
		sp := spans[0]
		return b[sp.off : sp.off+sp.length], true, nil
	}
	// Defined length → native uncompressed; the pixel blob starts right after the header.
	dataStart := pos + 12
	dataLen := int(length32)
	if dataLen < 0 || dataStart+dataLen > len(b) {
		return nil, false, fmt.Errorf("dicom: native PixelData length %d overruns file", dataLen)
	}
	return b[dataStart : dataStart+dataLen], false, nil
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
		if length < 0 || p+length > len(b) {
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
