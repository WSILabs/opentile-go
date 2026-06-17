// Package tiffvalidate provides structural validation helpers shared by the
// TIFF-based format readers: byte-range bounds for tile/strip data and
// orphan-IFD detection. It must NOT import the root opentile package (the
// readers do, so importing it here would create a cycle); results are reported
// through the Sink interface, which the reader adapts to opentile's probe.
package tiffvalidate

import (
	"fmt"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Sink receives validation findings. The reader implements it by forwarding to
// opentile.ValidationProbe.Flag with the right CheckCode.
type Sink interface {
	// OffsetOutOfBounds reports one tile/strip whose byte range escapes the
	// file. level is the page's level index (or -1 if unknown).
	OffsetOutOfBounds(level int, msg string)
	// OrphanIFD reports one IFD not reachable as a level/associated page.
	OrphanIFD(msg string)
}

// checkRange flags a single byte range [offset, offset+length) that does not
// fit within [0, size). Detects offset+length overflow.
func checkRange(s Sink, level int, offset, length, size uint64) {
	end := offset + length
	if end < offset { // unsigned overflow
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d overflows", offset, length))
		return
	}
	if end > size {
		s.OffsetOutOfBounds(level, fmt.Sprintf("byte range offset=%d length=%d exceeds file size %d", offset, length, size))
	}
}

// Check walks every page of f, flagging out-of-bounds tile/strip byte ranges
// and orphan IFDs. levelOf maps a page's IFD offset to a level index for locus
// reporting (return -1 if unknown). reachable reports whether a page's IFD
// offset is referenced as a level or associated image; pages that are not
// reachable are flagged as orphan IFDs. Pass a reachable that always returns
// true to disable orphan reporting.
func Check(f *tiff.File, s Sink, levelOf func(ifdOffset int64) int, reachable func(ifdOffset int64) bool) {
	size := uint64(f.Size())
	for _, p := range f.Pages() {
		lvl := levelOf(p.IFDOffset())

		if offs, err := p.TileOffsets64(); err == nil && len(offs) > 0 {
			counts, _ := p.TileByteCounts64()
			for i, off := range offs {
				var length uint64
				if i < len(counts) {
					length = counts[i]
				}
				checkRange(s, lvl, off, length, size)
			}
		}

		if offs, err := p.ScalarArrayU64(tiff.TagStripOffsets); err == nil && len(offs) > 0 {
			counts, _ := p.ScalarArrayU64(tiff.TagStripByteCounts)
			for i, off := range offs {
				var length uint64
				if i < len(counts) {
					length = counts[i]
				}
				checkRange(s, lvl, off, length, size)
			}
		}

		if reachable != nil && !reachable(p.IFDOffset()) {
			s.OrphanIFD(fmt.Sprintf("IFD at offset %d is not referenced as a level or associated image", p.IFDOffset()))
		}
	}
}
