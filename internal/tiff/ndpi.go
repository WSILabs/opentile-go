package tiff

import "fmt"

// ndpiSourceLensTag is the Hamamatsu vendor tag that identifies NDPI files.
// The tiff package sniffs for it in the first IFD; format packages re-check it
// in their Supports() method. Kept internal to avoid exporting a tag constant
// that's format-specific.
const ndpiSourceLensTag uint16 = 65420

// walkNDPIIFDs walks an NDPI-extended IFD chain. NDPI files use classic TIFF
// magic 42 but embed 64-bit offsets:
//
//   - First-IFD offset in the header is 8 bytes (uint64), which for <4GB
//     files has upper 4 bytes = 0 (compatible with classic 4-byte reading).
//   - Within each IFD: tagno (u16) + 12-byte standard entries + 8-byte
//     next-IFD offset (uint64) + 4 × tagno hi-bits extension.
//   - Each tag's effective valueOrOffset is reconstructed as
//     (hi_bits << 32) | low (the 4-byte inline cell from the standard entry).
//
// Bulk-read like walkClassicIFDs / walkBigIFDs: one ReadAt for the 2-byte
// count, then one for the entries + next-IFD + hi-bits combined block.
func walkNDPIIFDs(b *byteReader, offset int64) ([]*ifd, error) {
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

		count16, err := b.uint16(offset)
		if err != nil {
			return nil, fmt.Errorf("tiff: NDPI IFD entry count at %d: %w", offset, err)
		}
		count := int(count16)
		// Body: 12*count entries + 8 next-IFD + 4*count hi-bits.
		bodyLen := 12*count + 8 + 4*count
		body, err := b.bytes(offset+2, bodyLen)
		if err != nil {
			return nil, fmt.Errorf("tiff: NDPI IFD body at %d: %w", offset, err)
		}
		entriesBase := 0
		nextIFDOff := entriesBase + 12*count
		hibitsBase := nextIFDOff + 8

		ifd := &ifd{entries: make(map[uint16]Entry, count), offset: offset}
		for i := 0; i < count; i++ {
			entry := decodeClassicEntry(body[entriesBase+i*12:entriesBase+i*12+12], b.order)
			valHi := b.order.Uint32(body[hibitsBase+i*4 : hibitsBase+i*4+4])
			// Stitch the 64-bit value: (hi_bits << 32) | low. The low
			// word came from valueOrOffset (which was decoded as uint32).
			entry.valueOrOffset = (uint64(valHi) << 32) | (entry.valueOrOffset & 0xFFFFFFFF)
			// NDPI inline cell is 4 bytes (the low word of the 8-byte
			// value); high bits live out-of-band.
			entry.inlineCap = 4
			ifd.entries[entry.Tag] = entry
		}
		out = append(out, ifd)

		next := b.order.Uint64(body[nextIFDOff : nextIFDOff+8])
		offset = int64(next)
	}
	return out, nil
}

// sniffNDPI parses the first IFD of a classic-TIFF file and reports whether
// it carries the Hamamatsu SourceLens tag (65420), which identifies an NDPI
// file. Used by File.Open to decide whether to re-parse in NDPI mode.
//
// This is a deliberate format-specific peek inside an otherwise
// format-agnostic package. NDPI files share classic TIFF magic 42 with
// no header-level distinguisher — sniffing a vendor tag is the cleanest
// way to dispatch the NDPI-layout IFD walker without making every
// caller pre-classify. See docs/deferred.md L5.
func sniffNDPI(b *byteReader, firstIFDOffset int64) (bool, error) {
	count16, err := b.uint16(firstIFDOffset)
	if err != nil {
		return false, fmt.Errorf("tiff: NDPI sniff count: %w", err)
	}
	for i := uint16(0); i < count16; i++ {
		tag, err := b.uint16(firstIFDOffset + 2 + int64(12*i))
		if err != nil {
			return false, fmt.Errorf("tiff: NDPI sniff tag: %w", err)
		}
		if tag == ndpiSourceLensTag {
			return true, nil
		}
	}
	return false, nil
}
