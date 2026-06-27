package dzi

import "errors"

// ErrOverlapNotSupported is a reserved sentinel for a DZI/SZI manifest whose
// overlap cannot be modelled. As of the Overlap>0 support, neither formats/dzi
// nor formats/szi returns it for ordinary overlap — both read Overlap>0 — but
// it is kept for any future genuinely-unmodellable overlap case.
var ErrOverlapNotSupported = errors.New("dzi: tile overlap > 0 not supported")
