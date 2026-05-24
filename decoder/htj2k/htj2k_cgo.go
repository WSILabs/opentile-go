//go:build cgo && !nocgo && !nohtj2k

// Package htj2k implements the HTJ2K (High-Throughput JPEG 2000) decoder
// via OpenJPH (https://github.com/aous72/OpenJPH).
// TIFF Compression=60003 (wsi-tools private/experimental).
package htj2k

/*
#cgo pkg-config: openjph
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "htj2k" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{60003} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if d.closed {
		return nil, fmt.Errorf("decoder/htj2k: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/htj2k: empty input: %w", decoder.ErrCorruptInput)
	}
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/htj2k: scale=%d not supported: %w", opts.Scale, decoder.ErrUnsupportedScale)
	}
	if opts.Format == decoder.PixelFormatRGBA {
		return nil, fmt.Errorf("decoder/htj2k: RGBA output not supported: %w", decoder.ErrUnsupportedFormat)
	}

	// Phase 1: read header dimensions.
	var cW, cH C.int
	rc := C.wsi_htj2k_dimensions(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		&cW, &cH,
	)
	runtime.KeepAlive(src)
	if rc != 0 {
		return nil, fmt.Errorf("decoder/htj2k: failed to read header: %w", decoder.ErrCorruptInput)
	}
	w, h := int(cW), int(cH)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("decoder/htj2k: invalid dimensions %dx%d: %w", w, h, decoder.ErrCorruptInput)
	}

	// Phase 2: allocate output and decode.
	var dst *decoder.Image
	if opts.Dst == nil {
		dst = decoder.NewImage(w, h)
	} else {
		if opts.Dst.Width != w || opts.Dst.Height != h {
			return nil, fmt.Errorf("decoder/htj2k: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	}

	stride := w * 3
	rc = C.wsi_htj2k_decode(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
		C.size_t(stride),
		&cW, &cH,
	)
	runtime.KeepAlive(src)
	runtime.KeepAlive(dst)
	if rc != 0 {
		return nil, fmt.Errorf("decoder/htj2k: decode failed: %w", decoder.ErrCorruptInput)
	}

	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}

// encodeTestLossless is a test helper that encodes packed RGB888 pixels as a
// lossless HTJ2K codestream using wsi_htj2k_encode_test. Not part of the
// public API — used only by htj2k_roundtrip_test.go.
func encodeTestLossless(rgb []byte, w, h int) ([]byte, error) {
	if len(rgb) < w*h*3 {
		return nil, fmt.Errorf("decoder/htj2k: encodeTestLossless: buffer too small")
	}
	var outbuf *C.uint8_t
	var outsize C.size_t
	rc := C.wsi_htj2k_encode_test(
		(*C.uint8_t)(unsafe.Pointer(&rgb[0])),
		C.int(w), C.int(h),
		&outbuf, &outsize,
	)
	runtime.KeepAlive(rgb)
	if rc != 0 || outbuf == nil || outsize == 0 {
		return nil, fmt.Errorf("decoder/htj2k: encodeTestLossless failed")
	}
	defer C.free(unsafe.Pointer(outbuf))
	out := make([]byte, int(outsize))
	copy(out, (*[1 << 30]byte)(unsafe.Pointer(outbuf))[:int(outsize)])
	return out, nil
}
