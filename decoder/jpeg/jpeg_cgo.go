//go:build cgo && !nocgo

// Package jpeg implements the JPEG decoder via libjpeg-turbo.
// TIFF Compression=7. Supports IDCT-time scale factors 1, 2, 4, 8.
package jpeg

/*
#cgo pkg-config: libturbojpeg
#include <stdlib.h>
#include <turbojpeg.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "jpeg" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{7} }
func (f *factory) New() decoder.Decoder          { return newCGODecoder() }

type cgoDecoder struct {
	mu     sync.Mutex
	handle C.tjhandle
	closed bool
}

func newCGODecoder() *cgoDecoder {
	return &cgoDecoder{handle: C.tjInitDecompress()}
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, fmt.Errorf("decoder/jpeg: decoder closed")
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/jpeg: empty src: %w", decoder.ErrCorruptInput)
	}

	scale := opts.Scale
	if scale == 0 {
		scale = 1
	}
	if scale != 1 && scale != 2 && scale != 4 && scale != 8 {
		return nil, fmt.Errorf("decoder/jpeg: scale=%d (want 1,2,4,8): %w", scale, decoder.ErrUnsupportedScale)
	}

	// Read JPEG header to get full-resolution dimensions.
	var srcW, srcH, subsamp, colorspace C.int
	if rc := C.tjDecompressHeader3(d.handle,
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		&srcW, &srcH, &subsamp, &colorspace); rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg: tjDecompressHeader3: %s: %w",
			C.GoString(C.tjGetErrorStr2(d.handle)), decoder.ErrCorruptInput)
	}

	// Compute scaled output dimensions: ceil(srcDim / scale).
	// This matches libjpeg-turbo's TJSCALED(dim, {1, scale}) behaviour.
	outW := (int(srcW) + scale - 1) / scale
	outH := (int(srcH) + scale - 1) / scale

	var pixelFormat C.int
	bpp := 3
	switch opts.Format {
	case decoder.PixelFormatRGBA:
		pixelFormat = C.TJPF_RGBA
		bpp = 4
	default:
		pixelFormat = C.TJPF_RGB
		bpp = 3
	}

	var dst *decoder.Image
	if opts.Dst == nil {
		dst = decoder.NewImageFormat(outW, outH, opts.Format)
	} else {
		if opts.Dst.Width != outW || opts.Dst.Height != outH {
			return nil, fmt.Errorf("decoder/jpeg: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, outW, outH, decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	}

	stride := outW * bpp
	if rc := C.tjDecompress2(d.handle,
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst.Pix[0])),
		C.int(outW),
		C.int(stride),
		C.int(outH),
		pixelFormat,
		0); rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg: tjDecompress2: %s: %w",
			C.GoString(C.tjGetErrorStr2(d.handle)), decoder.ErrCorruptInput)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	C.tjDestroy(d.handle)
	d.closed = true
	return nil
}
