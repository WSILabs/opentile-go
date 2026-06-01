package tiff

import (
	"fmt"
	"strings"
)

// DataType is the TIFF tag data type as defined in TIFF 6.0 + common extensions.
type DataType uint16

const (
	DTByte      DataType = 1
	DTASCII     DataType = 2
	DTShort     DataType = 3
	DTLong      DataType = 4
	DTRational  DataType = 5
	DTUndefined DataType = 7
	DTSShort    DataType = 8  // signed 16-bit integer
	DTSLong     DataType = 9  // signed 32-bit integer (NDPI XOffset, etc.)
	DTSRational DataType = 10 // signed rational (two SLONGs)
	DTFloat     DataType = 11 // IEEE 754 single-precision (NDPI Magnification)
	DTDouble    DataType = 12 // IEEE 754 double-precision
	DTIFD       DataType = 13 // uint32 offset to sub-IFD
	DTLong8     DataType = 16 // uint64 (BigTIFF)
	DTIFD8      DataType = 18 // uint64 offset to sub-IFD (BigTIFF)
)

// Size returns the byte size of a single value of the given data type.
// Returns 1 for unknown types so callers reading external offsets can still
// bound-check; callers should check Size() > 0 in a validity guard before use
// for types they care about.
func (d DataType) Size() int {
	switch d {
	case DTByte, DTASCII, DTUndefined:
		return 1
	case DTShort, DTSShort:
		return 2
	case DTLong, DTIFD, DTSLong, DTFloat:
		return 4
	case DTRational, DTLong8, DTIFD8, DTSRational, DTDouble:
		return 8
	default:
		return 1
	}
}

// Entry is a raw IFD entry: tag id, type, count, and a value-or-offset cell.
// For classic TIFF the cell is 4 bytes wide; for BigTIFF it is 8 bytes wide.
// inlineCap controls which interpretation applies: 0 (uninitialized) is treated
// as 4 (classic TIFF semantics); 8 selects BigTIFF semantics.
type Entry struct {
	Tag           uint16
	Type          DataType
	Count         uint64  // CHANGED from uint32 — BigTIFF entries carry uint64 counts
	valueOrOffset uint64  // CHANGED from uint32 — BigTIFF cell carries 8-byte value/offset
	valueBytes    [8]byte // CHANGED from [4]byte — inline cell is 8 bytes wide in BigTIFF
	inlineCap     int     // NEW — 4 for classic (default 0 treated as 4), 8 for BigTIFF
}

// fitsInline reports whether the tag value fits in the inline cell.
func (e Entry) fitsInline() bool {
	cap := e.inlineCap
	if cap == 0 {
		cap = 4 // defensive: treat uninitialized as classic TIFF
	}
	return int64(e.Count)*int64(e.Type.Size()) <= int64(cap)
}

// decodeInline decodes the inline cell (cell) as a slice of uint32 values
// according to the entry's Type. cell must be the raw bytes in file order.
//
// The *byteReader argument is used only for b.order when interpreting
// multi-byte values; the reader's position / file state is irrelevant
// here. Passing the whole reader keeps the call sites uniform with the
// other decode helpers in this file (decodeExternal, decodeASCII)
// which actually do read from the underlying file.
func (e Entry) decodeInline(b *byteReader, cell []byte) ([]uint32, error) {
	if !e.fitsInline() {
		return nil, fmt.Errorf("tiff: tag %d: value does not fit inline", e.Tag)
	}
	return e.decodeBuffer(b, cell)
}

// decodeExternal decodes values stored at e.valueOrOffset in the underlying file.
func (e Entry) decodeExternal(b *byteReader) ([]uint32, error) {
	n := int64(e.Count) * int64(e.Type.Size())
	if n > int64(^uint(0)>>1) { // would truncate on int conversion
		return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, n)
	}
	buf, err := b.bytes(int64(e.valueOrOffset), int(n))
	if err != nil {
		return nil, fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
	}
	return e.decodeBuffer(b, buf)
}

// Values returns the decoded uint32 values for this entry, reading the inline
// cell when possible or the external offset otherwise.
func (e Entry) Values(b *byteReader) ([]uint32, error) {
	if e.fitsInline() {
		return e.decodeInline(b, e.valueBytes[:])
	}
	return e.decodeExternal(b)
}

// decodeBuffer decodes buf (which must be at least Count*Type.Size() bytes)
// into uint32 values. Rational and unknown types return raw byte groups as
// uint32s (use dedicated helpers for those cases).
func (e Entry) decodeBuffer(b *byteReader, buf []byte) ([]uint32, error) {
	need := int64(e.Count) * int64(e.Type.Size())
	if int64(len(buf)) < need {
		return nil, fmt.Errorf("tiff: tag %d: buffer %d < needed %d bytes", e.Tag, len(buf), need)
	}
	out := make([]uint32, 0, e.Count)
	switch e.Type {
	case DTByte, DTUndefined:
		for _, v := range buf[:e.Count] {
			out = append(out, uint32(v))
		}
	case DTShort:
		for i := uint64(0); i < e.Count; i++ {
			out = append(out, uint32(b.order.Uint16(buf[i*2:])))
		}
	case DTLong, DTIFD:
		for i := uint64(0); i < e.Count; i++ {
			out = append(out, b.order.Uint32(buf[i*4:]))
		}
	default:
		return nil, fmt.Errorf("tiff: tag %d: unsupported type %d for uint decode", e.Tag, e.Type)
	}
	return out, nil
}

// decodeASCII reads the string value for an ASCII entry. cell is the
// inline cell used when the value fits inline.
//
// NUL terminators are silently tolerated: TIFF 6.0 mandates NUL
// termination for ASCII tags but Aperio / Hamamatsu slides in the wild
// occasionally omit it on non-final tags. The returned string is the
// authoritative content; callers should treat it as already trimmed.
func (e Entry) decodeASCII(b *byteReader, cell []byte) (string, error) {
	var data []byte
	if e.fitsInline() {
		data = cell[:e.Count]
	} else {
		buf, err := b.bytes(int64(e.valueOrOffset), int(e.Count))
		if err != nil {
			return "", fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
		}
		data = buf
	}
	// TIFF ASCII values are NUL-terminated; strip trailing NULs.
	return strings.TrimRight(string(data), "\x00"), nil
}

// decodeRational reads the uint32 numerator/denominator pairs.
func (e Entry) decodeRational(b *byteReader) ([][2]uint32, error) {
	n := int64(e.Count) * 8
	buf, err := b.bytes(int64(e.valueOrOffset), int(n))
	if err != nil {
		return nil, err
	}
	out := make([][2]uint32, 0, e.Count)
	for i := uint64(0); i < e.Count; i++ {
		num := b.order.Uint32(buf[i*8:])
		den := b.order.Uint32(buf[i*8+4:])
		out = append(out, [2]uint32{num, den})
	}
	return out, nil
}

// RawTag is a decoded TIFF tag exposed to the opentile root package so it
// can build the public opentile.TIFFTag. Internal package only.
type RawTag struct {
	Number    uint16
	Type      DataType
	Count     int
	Raw       []byte      // verbatim payload bytes, file byte order
	ASCII     string      // set when Type==DTASCII
	Uints     []uint64    // BYTE/SHORT/LONG/LONG8 (best-effort)
	Rationals [][2]uint32 // num/denom pairs when Type==DTRational
}

// RawBytes returns the verbatim value payload for the entry (the bytes a
// re-encoder would re-emit), in file byte order.
func (e Entry) RawBytes(b *byteReader) ([]byte, error) {
	need := int64(e.Count) * int64(e.Type.Size())
	if need <= 0 {
		return nil, nil
	}
	if e.fitsInline() {
		capn := e.inlineCap
		if capn == 0 {
			capn = 4
		}
		if need > int64(capn) {
			need = int64(capn)
		}
		return append([]byte(nil), e.valueBytes[:need]...), nil
	}
	if need > int64(^uint(0)>>1) {
		return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, need)
	}
	return b.bytes(int64(e.valueOrOffset), int(need))
}

// decode populates a RawTag from the entry using the page's byteReader.
// Best-effort: a field that doesn't apply to the type is left zero.
func (e Entry) decode(b *byteReader) RawTag {
	rt := RawTag{Number: e.Tag, Type: e.Type, Count: int(e.Count)}
	rt.Raw, _ = e.RawBytes(b)
	switch e.Type {
	case DTASCII:
		rt.ASCII, _ = e.decodeASCII(b, e.valueBytes[:])
	case DTRational, DTSRational:
		rt.Rationals, _ = e.decodeRational(b)
	default:
		rt.Uints, _ = e.Values64(b)
	}
	return rt
}

// Values64 returns decoded values as []uint64, accepting Short, Long, Long8,
// IFD, and IFD8 entry types. Prefer this over Values for entries that might
// carry BigTIFF LONG8 data (tile offsets, for instance).
func (e Entry) Values64(b *byteReader) ([]uint64, error) {
	need := int64(e.Count) * int64(e.Type.Size())
	var buf []byte
	if e.fitsInline() {
		if e.Count == 1 && e.Type.Size() <= 8 {
			// Use valueOrOffset directly: it holds the fully-reconstructed
			// scalar value for all TIFF dialects (classic uint32-widened,
			// BigTIFF uint64, and NDPI 64-bit-extended). This avoids losing
			// high bits that NDPI stores out-of-band from the raw inline cell.
			return []uint64{e.valueOrOffset}, nil
		}
		// Multi-value inline: decode from the raw cell bytes.
		buf = append([]byte(nil), e.valueBytes[:need]...)
	} else {
		if need > int64(^uint(0)>>1) {
			return nil, fmt.Errorf("tiff: tag %d: value size %d exceeds platform int range", e.Tag, need)
		}
		payload, err := b.bytes(int64(e.valueOrOffset), int(need))
		if err != nil {
			return nil, fmt.Errorf("tiff: tag %d: %w", e.Tag, err)
		}
		buf = payload
	}
	out := make([]uint64, 0, e.Count)
	size := e.Type.Size()
	switch size {
	case 2:
		for i := uint64(0); i < e.Count; i++ {
			out = append(out, uint64(b.order.Uint16(buf[i*2:])))
		}
	case 4:
		for i := uint64(0); i < e.Count; i++ {
			out = append(out, uint64(b.order.Uint32(buf[i*4:])))
		}
	case 8:
		for i := uint64(0); i < e.Count; i++ {
			out = append(out, b.order.Uint64(buf[i*8:]))
		}
	default:
		return nil, fmt.Errorf("tiff: tag %d: unsupported type %d for uint64 decode", e.Tag, e.Type)
	}
	return out, nil
}
