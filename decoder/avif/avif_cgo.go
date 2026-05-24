//go:build cgo && !nocgo && !noavif

// Package avif implements the AVIF decoder via libavif.
// TIFF Compression=60001 (wsi-tools convention; not registered with Adobe).
package avif

/*
#cgo pkg-config: libavif
#include <stdlib.h>
#include <string.h>
#include <avif/avif.h>

// wsi_avif_decode decodes an AVIF image into packed UINT8 RGB or RGBA pixels.
// channels must be 3 (RGB) or 4 (RGBA).
//
// On success, *out_w and *out_h contain the image dimensions, and *out_buf
// points to a malloc'd buffer of (*out_w * *out_h * channels) bytes that
// the caller must free.
// Returns 0 on success, -1 on error.
static int wsi_avif_decode(
    const uint8_t *src, size_t src_len,
    int channels,
    uint32_t *out_w, uint32_t *out_h,
    uint8_t **out_buf)
{
    *out_w   = 0;
    *out_h   = 0;
    *out_buf = NULL;

    avifDecoder *dec = avifDecoderCreate();
    if (!dec) return -1;

    avifResult r = avifDecoderSetIOMemory(dec, src, src_len);
    if (r != AVIF_RESULT_OK) {
        avifDecoderDestroy(dec);
        return -1;
    }

    r = avifDecoderParse(dec);
    if (r != AVIF_RESULT_OK) {
        avifDecoderDestroy(dec);
        return -1;
    }

    r = avifDecoderNextImage(dec);
    if (r != AVIF_RESULT_OK) {
        avifDecoderDestroy(dec);
        return -1;
    }

    avifRGBImage rgb;
    avifRGBImageSetDefaults(&rgb, dec->image);
    rgb.format = (channels == 4) ? AVIF_RGB_FORMAT_RGBA : AVIF_RGB_FORMAT_RGB;
    rgb.depth  = 8;

    r = avifRGBImageAllocatePixels(&rgb);
    if (r != AVIF_RESULT_OK) {
        avifDecoderDestroy(dec);
        return -1;
    }

    r = avifImageYUVToRGB(dec->image, &rgb);
    if (r != AVIF_RESULT_OK) {
        avifRGBImageFreePixels(&rgb);
        avifDecoderDestroy(dec);
        return -1;
    }

    uint32_t w = dec->image->width;
    uint32_t h = dec->image->height;
    size_t   bsz = (size_t)w * (size_t)h * (size_t)channels;

    uint8_t *buf = (uint8_t *)malloc(bsz);
    if (!buf) {
        avifRGBImageFreePixels(&rgb);
        avifDecoderDestroy(dec);
        return -1;
    }
    memcpy(buf, rgb.pixels, bsz);

    avifRGBImageFreePixels(&rgb);
    avifDecoderDestroy(dec);

    *out_w   = w;
    *out_h   = h;
    *out_buf = buf;
    return 0;
}
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

func (f *factory) Name() string                  { return "avif" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{60001} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if d.closed {
		return nil, fmt.Errorf("decoder/avif: decoder closed")
	}
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/avif: scale=%d not supported: %w", opts.Scale, decoder.ErrUnsupportedScale)
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/avif: empty input: %w", decoder.ErrCorruptInput)
	}

	channels := 3
	if opts.Format == decoder.PixelFormatRGBA {
		channels = 4
	}

	var outW, outH C.uint32_t
	var outBuf *C.uint8_t

	rc := C.wsi_avif_decode(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(channels),
		&outW, &outH,
		&outBuf,
	)
	runtime.KeepAlive(src)

	if rc != 0 {
		return nil, fmt.Errorf("decoder/avif: decode failed: %w", decoder.ErrCorruptInput)
	}
	if outBuf == nil {
		return nil, fmt.Errorf("decoder/avif: nil output buffer: %w", decoder.ErrCorruptInput)
	}
	defer C.free(unsafe.Pointer(outBuf))

	w, h := int(outW), int(outH)

	if opts.Dst != nil {
		if opts.Dst.Width != w || opts.Dst.Height != h {
			return nil, fmt.Errorf("decoder/avif: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
		}
		pixLen := w * h * channels
		copy(opts.Dst.Pix[:pixLen], C.GoBytes(unsafe.Pointer(outBuf), C.int(pixLen)))
		return opts.Dst, nil
	}

	dst := decoder.NewImageFormat(w, h, opts.Format)
	copy(dst.Pix, C.GoBytes(unsafe.Pointer(outBuf), C.int(w*h*channels)))
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
