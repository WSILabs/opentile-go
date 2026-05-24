//go:build cgo && !nocgo && !nowebp

// Package webp implements the WebP decoder via libwebp.
// TIFF Compression=50001 (libtiff convention).
package webp

/*
#cgo pkg-config: libwebp
#include <webp/decode.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "webp" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{50001} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/webp: scale not supported: %w", decoder.ErrUnsupportedScale)
	}
	if d.closed {
		return nil, fmt.Errorf("decoder/webp: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/webp: empty src: %w", decoder.ErrCorruptInput)
	}

	var w, h C.int
	if C.WebPGetInfo(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		&w, &h) == 0 {
		return nil, fmt.Errorf("decoder/webp: WebPGetInfo (corrupt input): %w", decoder.ErrCorruptInput)
	}

	var dst *decoder.Image
	if opts.Dst != nil {
		if opts.Dst.Width != int(w) || opts.Dst.Height != int(h) {
			return nil, fmt.Errorf("decoder/webp: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, int(w), int(h), decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	} else {
		dst = decoder.NewImageFormat(int(w), int(h), opts.Format)
	}

	var ok *C.uint8_t
	if opts.Format == decoder.PixelFormatRGBA {
		ok = C.WebPDecodeRGBAInto(
			(*C.uint8_t)(unsafe.Pointer(&src[0])),
			C.size_t(len(src)),
			(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
			C.size_t(len(dst.Pix)),
			C.int(dst.Stride))
	} else {
		ok = C.WebPDecodeRGBInto(
			(*C.uint8_t)(unsafe.Pointer(&src[0])),
			C.size_t(len(src)),
			(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
			C.size_t(len(dst.Pix)),
			C.int(dst.Stride))
	}
	if ok == nil {
		return nil, fmt.Errorf("decoder/webp: decode failed: %w", decoder.ErrCorruptInput)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
