// Package fastpath holds dispatch sentinel errors shared between the
// opentile root and format-specific readers. The sentinels signal
// "this reader does not implement the requested fast path; use the
// slow path instead" without growing the public opentile API.
//
// This package has no other purpose. Adding new exported symbols here
// is reserved for additional fast-path dispatch in future milestones.
package fastpath

import "errors"

// ErrUnsupported is returned by a format reader's fast-path method
// (e.g., ImageDecodedTile on the ndpi tiler) when the reader cannot
// handle the requested operation via the fast path and the caller
// should fall through to the slow path. NOT an error condition — the
// caller treats this sentinel as a signal, not as a failure.
//
// Use errors.Is to detect; do not compare with ==.
var ErrUnsupported = errors.New("opentile: fast path unsupported, fall back to slow path")
