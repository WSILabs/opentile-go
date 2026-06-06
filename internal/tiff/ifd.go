package tiff

import (
	"errors"
	"fmt"
)

// ErrTooManyIFDs is returned when a TIFF IFD chain exceeds the safety cap
// (maxIFDs) before terminating. The root opentile package re-exports this
// as opentile.ErrTooManyIFDs so callers can errors.Is against the public
// sentinel.
var ErrTooManyIFDs = errors.New("internal/tiff: TIFF IFD chain exceeded the safety cap")

// ifd is a parsed Image File Directory: a collection of Entry values indexed by tag id.
type ifd struct {
	entries map[uint16]Entry
	offset  int64 // byte offset of this IFD in the file (0 for synthetic/test ifds)
}

func (i *ifd) get(tag uint16) (Entry, bool) {
	e, ok := i.entries[tag]
	return e, ok
}

// maxIFDs guards against pathological inputs with circular or excessive IFD chains.
const maxIFDs = 1024

// tiffMode selects the TIFF dialect to parse.
type tiffMode int

const (
	modeClassic tiffMode = iota
	modeBigTIFF
	modeNDPI
)

// walkIFDs walks the IFD chain starting at offset using the given dialect.
// modeClassic: magic 42, uint16 count, 12-byte entries, uint32 offsets.
// modeBigTIFF: magic 43, uint64 count, 20-byte entries, uint64 offsets.
// modeNDPI:    magic 42 with Hamamatsu high-bits extension for 64-bit offsets.
//
// Cycle detection uses an exact-offset seen-map (offset → bool). Files
// where one IFD's body literally contains the start of another IFD
// (overlapping IFDs at non-equal offsets) are NOT detected — only
// duplicate offsets are. In practice this never fires on real slides;
// noted here as a v0.4+ TODO.
func walkIFDs(b *byteReader, offset int64, mode tiffMode) ([]*ifd, error) {
	switch mode {
	case modeBigTIFF:
		return walkBigIFDs(b, offset)
	case modeNDPI:
		return walkNDPIIFDs(b, offset)
	default:
		return walkClassicIFDs(b, offset)
	}
}

// walkClassicIFDs reads the classic TIFF IFD chain starting at offset.
// The chain terminates when next-IFD-offset is zero or maxIFDs is reached
// (returning an error in the latter case).
//
// One IFD body is read in two ReadAt calls: a 2-byte count, then the
// remainder (count * 12 entry bytes + 4 next-IFD-offset bytes) in a
// single bulk read. Replaces the v0.2 per-field reader pattern that
// issued ~4 ReadAts per entry.
func walkClassicIFDs(b *byteReader, offset int64) ([]*ifd, error) {
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

		count, err := b.uint16(offset)
		if err != nil {
			return nil, fmt.Errorf("tiff: IFD entry count at %d: %w", offset, err)
		}
		body, err := b.bytes(offset+2, int(count)*12+4)
		if err != nil {
			return nil, fmt.Errorf("tiff: IFD body at %d: %w", offset, err)
		}
		ifd := &ifd{entries: make(map[uint16]Entry, count), offset: offset}
		for i := 0; i < int(count); i++ {
			entry := decodeClassicEntry(body[i*12:i*12+12], b.order)
			entry.inlineCap = 4
			ifd.entries[entry.Tag] = entry
		}
		out = append(out, ifd)
		next := b.order.Uint32(body[int(count)*12 : int(count)*12+4])
		offset = int64(next)
	}
	return out, nil
}

// decodeClassicEntry decodes a 12-byte classic TIFF IFD entry from buf
// (length must be >= 12). The inlineCap field is left at its zero value;
// callers set it to 4 (classic / NDPI) before storing.
func decodeClassicEntry(buf []byte, order interface {
	Uint16([]byte) uint16
	Uint32([]byte) uint32
}) Entry {
	var e Entry
	e.Tag = order.Uint16(buf[0:2])
	e.Type = DataType(order.Uint16(buf[2:4]))
	e.Count = uint64(order.Uint32(buf[4:8]))
	e.valueOrOffset = uint64(order.Uint32(buf[8:12]))
	copy(e.valueBytes[:], buf[8:12])
	return e
}
