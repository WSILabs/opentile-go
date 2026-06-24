package dzi

import "errors"

// ErrOverlapNotSupported is returned at open time when a DZI manifest declares
// Overlap > 0. Only Overlap=0 is implemented; tile-border cropping for
// Overlap > 0 is deferred. Both formats/dzi and formats/szi enforce this so an
// Overlap>0 slide fails loudly instead of being silently mis-placed.
var ErrOverlapNotSupported = errors.New("dzi: tile overlap > 0 not supported")
