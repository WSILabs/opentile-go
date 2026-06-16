//go:build cgo && !nocgo && !nowebp

// Package webp implements the WebP decoder via libwebp.
// TIFF Compression=50001 (libtiff convention).
package webp

/*
#cgo pkg-config: libwebp
#include <webp/decode.h>

// wsi_webp_decode decodes src into a caller-provided RGB (channels=3) or RGBA
// (channels=4) buffer at the requested output dimensions, using libwebp's
// internal rescaler (use_scaling) when out_w/out_h differ from the source size.
// This gives codec-domain, anti-aliased downscale (GH #11) — the WebPDecoderConfig
// union is awkward to drive from cgo Go, so it lives here. Returns 0 on success.
static int wsi_webp_decode(const uint8_t *src, size_t src_len, int channels,
                           int out_w, int out_h,
                           uint8_t *dst, size_t dst_size, int dst_stride) {
    WebPDecoderConfig config;
    if (!WebPInitDecoderConfig(&config)) return -1;
    if (WebPGetFeatures(src, src_len, &config.input) != VP8_STATUS_OK) return -1;
    if (out_w != config.input.width || out_h != config.input.height) {
        config.options.use_scaling   = 1;
        config.options.scaled_width  = out_w;
        config.options.scaled_height = out_h;
    }
    config.output.colorspace        = (channels == 4) ? MODE_RGBA : MODE_RGB;
    config.output.is_external_memory = 1;
    config.output.u.RGBA.rgba   = dst;
    config.output.u.RGBA.stride = dst_stride;
    config.output.u.RGBA.size   = dst_size;
    VP8StatusCode st = WebPDecode(src, src_len, &config);
    WebPFreeDecBuffer(&config.output); // no-op for external memory; resets state
    return (st == VP8_STATUS_OK) ? 0 : -1;
}
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
	scale := opts.Scale
	if scale == 0 {
		scale = 1
	}
	if scale != 1 && scale != 2 && scale != 4 && scale != 8 {
		return nil, fmt.Errorf("decoder/webp: scale=%d (want 1,2,4,8): %w", scale, decoder.ErrUnsupportedScale)
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

	// Scaled output: ceil(srcDim / scale), matching the jpeg decoder's contract.
	outW := (int(w) + scale - 1) / scale
	outH := (int(h) + scale - 1) / scale
	if outW <= 0 || outH <= 0 {
		return nil, fmt.Errorf("decoder/webp: invalid dimensions %dx%d: %w", outW, outH, decoder.ErrCorruptInput)
	}

	var dst *decoder.Image
	if opts.Dst != nil {
		if opts.Dst.Width != outW || opts.Dst.Height != outH {
			return nil, fmt.Errorf("decoder/webp: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, outW, outH, decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	} else {
		dst = decoder.NewImageFormat(outW, outH, opts.Format)
	}

	channels := 3
	if opts.Format == decoder.PixelFormatRGBA {
		channels = 4
	}
	rc := C.wsi_webp_decode(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(channels),
		C.int(outW), C.int(outH),
		(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
		C.size_t(len(dst.Pix)),
		C.int(dst.Stride))
	if rc != 0 {
		return nil, fmt.Errorf("decoder/webp: decode failed: %w", decoder.ErrCorruptInput)
	}
	return dst, nil
}

func (d *cgoDecoder) Close() error {
	d.closed = true
	return nil
}
