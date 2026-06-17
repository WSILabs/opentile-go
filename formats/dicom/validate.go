package dicom

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
)

// Validate checks each frame fragment's byte range against its containing
// instance file's size. DICOM is multi-file, so each level owns its own
// instance (with its own mmap-backed byte slice). The Slide-level
// p.Size() is 0 for a DICOM series and MUST NOT be used here; we use
// len(e.data) — the actual size of the per-instance byte slice — instead.
//
// Sparse DICOM (TILED_SPARSE DimensionOrganizationType): absent tile
// positions simply have no entry in the level's tileMap — they do not
// produce spans. All spans returned by walkEncapsulatedFrames correspond
// to real pixel-data fragments, so iterating e.spans covers exactly the
// frames that exist on disk.
//
// Implements [opentile.Validator]; discovered by the validation engine
// directly via type-assertion on s.r (*Tiler is stored unwrapped in the
// Slide for DICOM, with no fileCloser/mmapCloser wrapper).
func (t *Tiler) Validate(p *opentile.ValidationProbe) {
	for level, e := range t.levels {
		fsize := uint64(len(e.data))
		for _, sp := range e.spans {
			off := uint64(sp.off)
			ln := uint64(sp.length)
			end := off + ln
			// Flag when arithmetic overflows OR the fragment extends past the
			// instance file's boundary.
			if end < off || end > fsize {
				p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level,
					fmt.Sprintf("level %d frame offset=%d length=%d exceeds instance size %d",
						level, off, ln, fsize))
			}
		}
	}
}
