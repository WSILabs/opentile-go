package opentile

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrBadJPEGSplice is returned by SpliceJPEGTile when the body bytes
// don't conform to the expected SOS-bearing JPEG layout.
var ErrBadJPEGSplice = errors.New("opentile: bad JPEG splice input")

// SpliceJPEGTile reconstitutes a complete JPEG from a level's
// TilePrefix bytes and one tile's TileBodyInto output. Inserts the
// prefix at the on-disk tile's SOS boundary (the same operation
// opentile-go does internally during Tile/TileInto).
//
// Returns body verbatim (defensively copied) if prefix is empty / nil
// — degenerate case for levels without splice (e.g., non-JPEG
// compressions, NDPI stripped levels, IFE).
//
// Returns ErrBadJPEGSplice if body is empty or doesn't contain an
// SOS marker (0xFF 0xDA per JPEG spec).
//
// Algorithm (documented for non-Go consumers reimplementing
// client-side):
//
//  1. If prefix is empty: return body verbatim.
//  2. Find offset of the first 0xFF 0xDA byte sequence in body
//     ("Start of Scan" marker).
//  3. Output = body[0:sosIdx] + prefix + body[sosIdx:]
//
// Added in v0.13 alongside Level.TilePrefix and Level.TileBodyInto.
func SpliceJPEGTile(prefix, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: body is empty", ErrBadJPEGSplice)
	}
	if len(prefix) == 0 {
		out := make([]byte, len(body))
		copy(out, body)
		return out, nil
	}
	sosIdx := bytes.Index(body, []byte{0xFF, 0xDA})
	if sosIdx < 0 {
		return nil, fmt.Errorf("%w: SOS marker (0xFF 0xDA) not found in body", ErrBadJPEGSplice)
	}
	out := make([]byte, len(body)+len(prefix))
	copy(out[0:sosIdx], body[0:sosIdx])
	copy(out[sosIdx:sosIdx+len(prefix)], prefix)
	copy(out[sosIdx+len(prefix):], body[sosIdx:])
	return out, nil
}
