package ife

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
)

// Validate checks that every tile entry's byte range lies within the file.
// Sparse entries (Offset == NullTile) carry no on-disk data and are skipped.
// A zero-size entry (Size == 0) is also skipped — the tile has no bytes.
//
// Implements [opentile.Validator]; discovered by the engine via the
// UnwrapReader chain (fileCloser → *tiler).
func (t *tiler) Validate(p *opentile.ValidationProbe) {
	size := uint64(p.Size())
	for _, e := range t.tileOffsets {
		// Skip sparse entries and zero-size entries — no on-disk bytes.
		if e.Offset == NullTile || e.Size == 0 {
			continue
		}
		off, ln := e.Offset, uint64(e.Size)
		end := off + ln
		// Flag when arithmetic overflows OR the range extends past the file.
		if end < off || end > size {
			p.Flag(opentile.CheckOffsetsOutOfBounds, 0, -1,
				fmt.Sprintf("tile offset=%d length=%d extends past file size %d",
					off, ln, size))
		}
	}
}
