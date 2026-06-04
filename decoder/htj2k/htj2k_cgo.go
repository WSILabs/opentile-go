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
	// Map DecodeOptions.Scale to a DWT resolution factor (1/2^r), matching
	// the jpeg decoder's {1,2,4,8} contract.
	scale := opts.Scale
	if scale == 0 {
		scale = 1
	}
	var resFactor int
	switch scale {
	case 1:
		resFactor = 0
	case 2:
		resFactor = 1
	case 4:
		resFactor = 2
	case 8:
		resFactor = 3
	default:
		return nil, fmt.Errorf("decoder/htj2k: scale=%d (want 1,2,4,8): %w", scale, decoder.ErrUnsupportedScale)
	}
	// Phase 1: read header dimensions (reduced by resFactor).
	var cW, cH C.int
	rc := C.wsi_htj2k_dimensions(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(resFactor),
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

	// Phase 2: RGBA path — decode RGB into scratch, then expand to RGBA.
	if opts.Format == decoder.PixelFormatRGBA {
		scratch := decoder.NewImage(w, h) // RGB scratch buffer, stride = w*3
		rgbStride := w * 3
		rc = C.wsi_htj2k_decode(
			(*C.uint8_t)(unsafe.Pointer(&src[0])),
			C.size_t(len(src)),
			C.int(resFactor),
			(*C.uint8_t)(unsafe.Pointer(&scratch.Pix[0])),
			C.size_t(rgbStride),
			&cW, &cH,
		)
		runtime.KeepAlive(src)
		runtime.KeepAlive(scratch)
		if rc != 0 {
			return nil, fmt.Errorf("decoder/htj2k: decode failed: %w", decoder.ErrCorruptInput)
		}

		var dst *decoder.Image
		if opts.Dst == nil {
			dst = decoder.NewImageFormat(w, h, decoder.PixelFormatRGBA)
		} else {
			if opts.Dst.Width != w || opts.Dst.Height != h {
				return nil, fmt.Errorf("decoder/htj2k: dst %dx%d != decoded %dx%d: %w",
					opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
			}
			dst = opts.Dst
		}

		for y := 0; y < h; y++ {
			srow := scratch.Pix[y*scratch.Stride:]
			drow := dst.Pix[y*dst.Stride:]
			for x := 0; x < w; x++ {
				drow[x*4+0] = srow[x*3+0]
				drow[x*4+1] = srow[x*3+1]
				drow[x*4+2] = srow[x*3+2]
				drow[x*4+3] = 0xFF
			}
		}
		return dst, nil
	}

	// Phase 2 (RGB path): allocate output and decode directly.
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
		C.int(resFactor),
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
func encodeTestLossless(rgb []byte, w, h, numDecomp int) ([]byte, error) {
	if len(rgb) < w*h*3 {
		return nil, fmt.Errorf("decoder/htj2k: encodeTestLossless: buffer too small")
	}
	var outbuf *C.uint8_t
	var outsize C.size_t
	rc := C.wsi_htj2k_encode_test(
		(*C.uint8_t)(unsafe.Pointer(&rgb[0])),
		C.int(w), C.int(h), C.int(numDecomp),
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
