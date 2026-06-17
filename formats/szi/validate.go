package szi

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
)

// Validate checks that every referenced ZIP entry's byte range lies within the
// file. SZI tiles are stored uncompressed (zip.Store), so the on-disk length
// equals the tile's uncompressed size; CompressedSize64 is the authoritative
// on-disk byte count.
//
// Implements [opentile.Validator]; discovered by the validation engine via the
// UnwrapReader chain (fileCloser/mmapCloser → *Tiler).
func (t *Tiler) Validate(p *opentile.ValidationProbe) {
	size := uint64(p.Size())
	for _, f := range t.entries {
		off, err := f.DataOffset()
		if err != nil {
			// DataOffset errors when the local file header cannot be read —
			// that is a different class of corruption; skip rather than mis-flag.
			continue
		}
		ln := f.CompressedSize64
		if ln == 0 {
			continue
		}
		end := uint64(off) + ln
		// Flag when arithmetic overflows OR the range extends past the file.
		if end < uint64(off) || end > size {
			p.Flag(opentile.CheckOffsetsOutOfBounds, 0, -1,
				fmt.Sprintf("zip entry %q offset=%d length=%d exceeds file size %d",
					f.Name, off, ln, size))
		}
	}
}
