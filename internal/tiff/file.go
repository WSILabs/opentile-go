package tiff

import (
	"encoding/binary"
	"fmt"
	"io"
)

// File is a parsed TIFF file: a list of pages in IFD order, plus the reader
// and byte order needed to decode tag values and read tile payloads.
type File struct {
	r       io.ReaderAt
	size    int64
	reader  *byteReader
	pages   []*Page
	bigTIFF bool // true when file uses BigTIFF (magic 43, 8-byte offsets)
	ndpi    bool // true when file was detected as Hamamatsu NDPI via SourceLens sniff
}

// Open parses the header and every IFD in r, producing a File ready for use by
// format packages. Open does not read tile payloads. The caller retains
// ownership of r; File does not close it (the io.ReaderAt contract does not
// include Close). size is the total readable size of r in bytes and is stored
// for future offset-bounds validation.
func Open(r io.ReaderAt, size int64) (*File, error) {
	h, err := parseHeader(r)
	if err != nil {
		return nil, err
	}
	br := newByteReader(r, h.littleEndian)
	mode := modeClassic
	if h.bigTIFF {
		mode = modeBigTIFF
	} else {
		// Classic magic 42 header. File may be classic TIFF, <4GB NDPI, or
		// >4GB NDPI. Try the classic-offset sniff first.
		isNDPI, sniffErr := sniffNDPI(br, int64(h.firstIFD))
		if sniffErr != nil {
			isNDPI = false // classic sniff failed; fall through
		}
		if isNDPI {
			// <4GB NDPI or a small NDPI file. The 4-byte classic offset is
			// correct, but re-read as uint64 so the upper 4 bytes (expected
			// to be zero here) are captured for any downstream bookkeeping.
			mode = modeNDPI
			h.ndpi = true
			fullOffset, err := br.uint64(4)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
			}
			h.firstIFD = fullOffset
		} else if size > (1 << 32) {
			// File is >4GB; the classic 4-byte first-IFD read would have
			// truncated. Re-read as uint64 (NDPI places an 8-byte offset
			// there) and sniff at the full offset.
			fullOffset, err := br.uint64(4)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidTIFF, err)
			}
			if int64(fullOffset) > 0 && int64(fullOffset) < size {
				isNDPILarge, sniffErr2 := sniffNDPI(br, int64(fullOffset))
				if sniffErr2 == nil && isNDPILarge {
					mode = modeNDPI
					h.ndpi = true
					h.firstIFD = fullOffset
				}
			}
		}
	}
	ifds, err := walkIFDs(br, int64(h.firstIFD), mode)
	if err != nil {
		return nil, err
	}
	pages := make([]*Page, 0, len(ifds))
	for _, i := range ifds {
		pages = append(pages, newPage(i, br))
	}
	return &File{r: r, size: size, reader: br, pages: pages, bigTIFF: h.bigTIFF, ndpi: h.ndpi}, nil
}

// Pages returns the pages in IFD order. The slice is owned by File; do not mutate.
func (f *File) Pages() []*Page { return f.pages }

// LittleEndian reports whether the file is stored little-endian.
func (f *File) LittleEndian() bool { return f.reader.order == binary.LittleEndian }

// BigTIFF reports whether the file uses BigTIFF (magic 43, 8-byte offsets).
func (f *File) BigTIFF() bool { return f.bigTIFF }

// NDPI reports whether the file was detected as a Hamamatsu NDPI file via
// the presence of the SourceLens tag (65420) in the first IFD.
func (f *File) NDPI() bool { return f.ndpi }

// ReaderAt returns the underlying reader for use by format packages reading
// tile byte ranges.
func (f *File) ReaderAt() io.ReaderAt { return f.r }

// Size returns the total byte size of the underlying reader as provided to Open.
func (f *File) Size() int64 { return f.size }

// PageAtOffset reads a single IFD at offset off and returns it as a Page.
// Used by callers that need to follow SubIFD pointers (TIFF tag 330) — the
// SubIFD chain is not part of the top-level next-IFD walk.
//
// Picks the classic vs BigTIFF IFD layout based on the file's mode (set
// at Open time). Does not advance any walker; returns one Page per call.
// Caller validates the resulting Page (ImageWidth/ImageLength etc.).
func (f *File) PageAtOffset(off uint64) (*Page, error) {
	var ifd *ifd
	var err error
	if f.bigTIFF {
		ifd, err = readSingleBigIFD(f.reader, int64(off))
	} else {
		// Classic TIFF and NDPI both use the 12-byte entry layout.
		ifd, err = readSingleClassicIFD(f.reader, int64(off))
	}
	if err != nil {
		return nil, err
	}
	return newPage(ifd, f.reader), nil
}

// readSingleClassicIFD reads one classic-TIFF IFD at offset and returns it.
// Mirrors one loop iteration of walkClassicIFDs but ignores the next-IFD
// pointer — used for SubIFD traversal where the IFD is reachable only via
// a tag-330 offset, not the top-level chain.
func readSingleClassicIFD(b *byteReader, offset int64) (*ifd, error) {
	count, err := b.uint16(offset)
	if err != nil {
		return nil, fmt.Errorf("tiff: SubIFD entry count at %d: %w", offset, err)
	}
	body, err := b.bytes(offset+2, int(count)*12+4)
	if err != nil {
		return nil, fmt.Errorf("tiff: SubIFD body at %d: %w", offset, err)
	}
	out := &ifd{entries: make(map[uint16]Entry, count), offset: offset}
	for i := 0; i < int(count); i++ {
		entry := decodeClassicEntry(body[i*12:i*12+12], b.order)
		entry.inlineCap = 4
		out.entries[entry.Tag] = entry
	}
	return out, nil
}

// readSingleBigIFD reads one BigTIFF IFD at offset and returns it. Mirrors
// one loop iteration of walkBigIFDs without advancing the next-IFD chain.
func readSingleBigIFD(b *byteReader, offset int64) (*ifd, error) {
	count64, err := b.uint64(offset)
	if err != nil {
		return nil, fmt.Errorf("tiff: BigTIFF SubIFD entry count at %d: %w", offset, err)
	}
	if count64 > (1<<31-1)/20 {
		return nil, fmt.Errorf("tiff: BigTIFF SubIFD entry count %d implausibly large at %d", count64, offset)
	}
	count := int(count64)
	body, err := b.bytes(offset+8, count*20+8)
	if err != nil {
		return nil, fmt.Errorf("tiff: BigTIFF SubIFD body at %d: %w", offset, err)
	}
	out := &ifd{entries: make(map[uint16]Entry, count), offset: offset}
	for i := 0; i < count; i++ {
		entry := decodeBigEntry(body[i*20:i*20+20], b.order)
		out.entries[entry.Tag] = entry
	}
	return out, nil
}
