//go:build cgo && !nocgo && !nojxl

// Package jpegxl implements the JPEG-XL decoder via libjxl.
// TIFF Compression=50002 (wsi-tools convention; not registered with Adobe).
package jpegxl

/*
#cgo pkg-config: libjxl libjxl_threads
#include <stdlib.h>
#include <jxl/decode.h>
#include <jxl/types.h>

// wsi_jxl_decode decodes a JPEG-XL codestream (bare or container) into
// packed UINT8 pixels.  channels must be 3 (RGB) or 4 (RGBA).
//
// On success, *out_w, *out_h contain the image dimensions, and *out_buf
// points to a malloc'd buffer of (*out_w * *out_h * channels) bytes that
// the caller must free.
// Returns 0 on success, -1 on error.
static int wsi_jxl_decode(
    const uint8_t *src, size_t src_len,
    int channels,
    uint32_t *out_w, uint32_t *out_h,
    uint8_t **out_buf)
{
    *out_w   = 0;
    *out_h   = 0;
    *out_buf = NULL;

    JxlDecoder *dec = JxlDecoderCreate(NULL);
    if (!dec) return -1;

    if (JxlDecoderSubscribeEvents(dec,
            JXL_DEC_BASIC_INFO | JXL_DEC_NEED_IMAGE_OUT_BUFFER | JXL_DEC_FULL_IMAGE)
            != JXL_DEC_SUCCESS) {
        JxlDecoderDestroy(dec);
        return -1;
    }

    if (JxlDecoderSetInput(dec, src, src_len) != JXL_DEC_SUCCESS) {
        JxlDecoderDestroy(dec);
        return -1;
    }
    JxlDecoderCloseInput(dec);

    JxlPixelFormat fmt;
    fmt.num_channels = (uint32_t)channels;
    fmt.data_type    = JXL_TYPE_UINT8;
    fmt.endianness   = JXL_NATIVE_ENDIAN;
    fmt.align        = 0;

    uint8_t *buf  = NULL;
    size_t   bsz  = 0;

    for (;;) {
        JxlDecoderStatus st = JxlDecoderProcessInput(dec);
        if (st == JXL_DEC_BASIC_INFO) {
            JxlBasicInfo info;
            if (JxlDecoderGetBasicInfo(dec, &info) != JXL_DEC_SUCCESS) goto fail;
            *out_w = info.xsize;
            *out_h = info.ysize;
        } else if (st == JXL_DEC_NEED_IMAGE_OUT_BUFFER) {
            if (*out_w == 0 || *out_h == 0) goto fail;
            bsz = (size_t)(*out_w) * (size_t)(*out_h) * (size_t)channels;
            buf = (uint8_t *)malloc(bsz);
            if (!buf) goto fail;
            if (JxlDecoderSetImageOutBuffer(dec, &fmt, buf, bsz) != JXL_DEC_SUCCESS) goto fail;
        } else if (st == JXL_DEC_FULL_IMAGE) {
            *out_buf = buf;
            JxlDecoderDestroy(dec);
            return 0;
        } else if (st == JXL_DEC_SUCCESS) {
            // Finished without seeing FULL_IMAGE — shouldn't happen normally.
            if (buf) { *out_buf = buf; JxlDecoderDestroy(dec); return 0; }
            goto fail;
        } else if (st == JXL_DEC_ERROR || st == JXL_DEC_NEED_MORE_INPUT) {
            goto fail;
        }
        // Other status codes (e.g. JXL_DEC_COLOR_ENCODING, JXL_DEC_FRAME):
        // ignore and continue pumping.
    }

fail:
    free(buf);
    JxlDecoderDestroy(dec);
    return -1;
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

func (f *factory) Name() string                  { return "jpegxl" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{50002} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct {
	closed bool
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if d.closed {
		return nil, fmt.Errorf("decoder/jpegxl: decoder closed")
	}
	if opts.Scale != 0 && opts.Scale != 1 {
		return nil, fmt.Errorf("decoder/jpegxl: scale=%d not supported: %w", opts.Scale, decoder.ErrUnsupportedScale)
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/jpegxl: empty input: %w", decoder.ErrCorruptInput)
	}

	channels := 3
	if opts.Format == decoder.PixelFormatRGBA {
		channels = 4
	}

	var outW, outH C.uint32_t
	var outBuf *C.uint8_t

	rc := C.wsi_jxl_decode(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(channels),
		&outW, &outH,
		&outBuf,
	)
	runtime.KeepAlive(src)

	if rc != 0 {
		return nil, fmt.Errorf("decoder/jpegxl: decode failed: %w", decoder.ErrCorruptInput)
	}
	if outBuf == nil {
		return nil, fmt.Errorf("decoder/jpegxl: nil output buffer: %w", decoder.ErrCorruptInput)
	}
	defer C.free(unsafe.Pointer(outBuf))

	w, h := int(outW), int(outH)

	if opts.Dst != nil {
		if opts.Dst.Width != w || opts.Dst.Height != h {
			return nil, fmt.Errorf("decoder/jpegxl: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
		}
		// Copy pixels into the provided destination image.
		pixLen := w * h * channels
		copy(opts.Dst.Pix[:pixLen], C.GoBytes(unsafe.Pointer(outBuf), C.int(w*h*channels)))
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
