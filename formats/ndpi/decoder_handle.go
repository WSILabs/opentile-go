//go:build cgo && !nocgo

package ndpi

import (
	"errors"
	"sync"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// decoderHandle wraps a single long-lived decoder.Decoder used by the
// strippedImage fast pixel path. Replaces today's per-tile fac.New()
// + defer dec.Close() pattern (which costs ~7s on CMU-1.ndpi from
// tjDestroy churn).
//
// Concurrency: Decode is serialized by mu because libjpeg-turbo's
// tjhandle is not concurrent-safe. The contention window is small
// because strippedImage's pixel cache absorbs most calls (~1 decode
// per frame in row-major iteration).
//
// Lifetime: created lazily at first DecodedTile call via
// strippedImage.decHandleOnce. Closed by strippedImage.closeResources
// which is called from the parent tiler's Close.
type decoderHandle struct {
	mu     sync.Mutex
	dec    decoder.Decoder // nil after Close
	closed bool
}

// newDecoderHandle constructs a handle wrapping a fresh decoder for
// the given compression. Returns a non-nil handle; if no decoder is
// registered for the compression, the handle.dec is nil and every
// Decode returns errNoDecoder.
func newDecoderHandle(c opentile.Compression) *decoderHandle {
	tag := opentile.CompressionToTIFFTag(c)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return &decoderHandle{}
	}
	return &decoderHandle{dec: fac.New()}
}

// errHandleClosed is returned by Decode if the handle has been closed.
var errHandleClosed = errors.New("ndpi: decoderHandle: closed")

// errNoDecoder is returned by Decode if no decoder was registered at
// construction time for the strippedImage's compression.
var errNoDecoder = errors.New("ndpi: decoderHandle: no decoder registered")

// Decode runs the wrapped decoder on src under the handle's mutex.
// Safe for concurrent invocation from multiple goroutines; calls are
// serialized.
func (h *decoderHandle) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHandleClosed
	}
	if h.dec == nil {
		return nil, errNoDecoder
	}
	return h.dec.Decode(src, opts)
}

// Close releases the wrapped decoder. Safe to call multiple times;
// subsequent calls are no-ops.
func (h *decoderHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.dec == nil {
		return nil
	}
	err := h.dec.Close()
	h.dec = nil
	return err
}
