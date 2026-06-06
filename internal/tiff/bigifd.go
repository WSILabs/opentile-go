package tiff

import "fmt"

// walkBigIFDs is the BigTIFF variant of walkIFDs. BigTIFF IFDs use uint64
// entry counts, 20-byte entries (tag u16, type u16, count u64, value u64),
// and uint64 next-IFD offsets. Bulk-read like walkClassicIFDs: one ReadAt
// for the count, one for the body+next-pointer.
func walkBigIFDs(b *byteReader, offset int64) ([]*ifd, error) {
	var out []*ifd
	seen := make(map[int64]bool)
	for offset != 0 {
		if len(out) >= maxIFDs {
			return nil, fmt.Errorf("%w (cap=%d)", ErrTooManyIFDs, maxIFDs)
		}
		if seen[offset] {
			return nil, fmt.Errorf("tiff: IFD cycle at offset %d", offset)
		}
		seen[offset] = true

		count64, err := b.uint64(offset)
		if err != nil {
			return nil, fmt.Errorf("tiff: BigTIFF IFD entry count at %d: %w", offset, err)
		}
		// Guard against a corrupt count that would overflow int when
		// computing the body length on 64-bit platforms (and would
		// trivially break the ReadAt on 32-bit).
		if count64 > (1<<31-1)/20 {
			return nil, fmt.Errorf("tiff: BigTIFF IFD entry count %d implausibly large at %d", count64, offset)
		}
		count := int(count64)
		body, err := b.bytes(offset+8, count*20+8)
		if err != nil {
			return nil, fmt.Errorf("tiff: BigTIFF IFD body at %d: %w", offset, err)
		}
		ifd := &ifd{entries: make(map[uint16]Entry, count), offset: offset}
		for i := 0; i < count; i++ {
			entry := decodeBigEntry(body[i*20:i*20+20], b.order)
			ifd.entries[entry.Tag] = entry
		}
		out = append(out, ifd)
		next := b.order.Uint64(body[count*20 : count*20+8])
		offset = int64(next)
	}
	return out, nil
}

// decodeBigEntry decodes a 20-byte BigTIFF IFD entry from buf (length must
// be >= 20). Layout: tag(u16) type(u16) count(u64) valueOrOffset(u64).
func decodeBigEntry(buf []byte, order interface {
	Uint16([]byte) uint16
	Uint64([]byte) uint64
}) Entry {
	var e Entry
	e.Tag = order.Uint16(buf[0:2])
	e.Type = DataType(order.Uint16(buf[2:4]))
	e.Count = order.Uint64(buf[4:12])
	e.valueOrOffset = order.Uint64(buf[12:20])
	copy(e.valueBytes[:], buf[12:20])
	e.inlineCap = 8
	return e
}
