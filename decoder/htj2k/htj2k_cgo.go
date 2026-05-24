//go:build cgo && !nocgo && !nohtj2k

// Package htj2k implements the HTJ2K (High-Throughput JPEG 2000) decoder
// via OpenJPH (https://github.com/aous72/OpenJPH).
// TIFF Compression=60003 (wsi-tools private/experimental).
//
// NOTE: The cgo build path is a best-effort skeleton — it has NOT been
// compiled or tested against an installed openjph library. Build with
// -tags nohtj2k or CGO_ENABLED=0 to use the stub instead.
package htj2k

/*
#cgo pkg-config: openjph
#cgo CXXFLAGS: -std=c++17

#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <openjph/ojph_codestream.h>
#include <openjph/ojph_params.h>
#include <openjph/ojph_mem.h>
#include <openjph/ojph_file.h>

// NOTE: This shim is a best-effort skeleton based on the openjph API
// (mirrored from wsitools' encoder shim). It has NOT been compiled or
// run against an installed openjph library. The decode logic is
// structurally correct; pixel-level correctness requires testing with
// real HTJ2K codestreams once openjph is installed on the build host.

// wsi_htj2k_dimensions reads only the codestream header and populates
// *out_w and *out_h. Returns 0 on success, -1 on error.
static int wsi_htj2k_dimensions(const uint8_t *src, size_t src_len,
                                 int *out_w, int *out_h) {
    if (!src || src_len == 0 || !out_w || !out_h) return -1;
    try {
        using namespace ojph;
        mem_infile in;
        in.open(const_cast<uint8_t *>(src), src_len);

        codestream cs;
        cs.enable_resilience();
        cs.read_headers(&in);

        param_siz siz = cs.access_siz();
        *out_w = (int)(siz.get_image_extent().x - siz.get_image_offset().x);
        *out_h = (int)(siz.get_image_extent().y - siz.get_image_offset().y);
        return 0;
    } catch (...) {
        return -1;
    }
}

// wsi_htj2k_decode decodes a HTJ2K codestream into packed RGB888.
// dst_rgb must be at least dst_stride * (*out_h) bytes; dst_stride >= *out_w * 3.
// Returns 0 on success, -1 on error.
static int wsi_htj2k_decode(const uint8_t *src, size_t src_len,
                              uint8_t *dst_rgb, size_t dst_stride,
                              int *out_w, int *out_h) {
    if (!src || src_len == 0 || !dst_rgb || !out_w || !out_h) return -1;
    try {
        using namespace ojph;
        mem_infile in;
        in.open(const_cast<uint8_t *>(src), src_len);

        codestream cs;
        cs.enable_resilience();
        cs.read_headers(&in);

        param_siz siz = cs.access_siz();
        int w = (int)(siz.get_image_extent().x - siz.get_image_offset().x);
        int h = (int)(siz.get_image_extent().y - siz.get_image_offset().y);
        *out_w = w;
        *out_h = h;

        if (w <= 0 || h <= 0) return -1;
        if (dst_stride < (size_t)(w * 3)) return -1;

        // set_planar(false): component-interleaved exchange.
        // For each row push component 0, then 1, then 2 (matching
        // the encoder's set_planar(false) in wsitools/internal/codec/htj2k/shim.cpp).
        cs.set_planar(false);
        cs.create();

        ui32 next_comp = 0;
        line_buf *cur = cs.exchange(NULL, next_comp);
        for (int y = 0; y < h; ++y) {
            for (int c = 0; c < 3; ++c) {
                if (!cur || !cur->i32) return -1;
                const si32 *src_line = cur->i32;
                uint8_t *row = dst_rgb + (size_t)y * dst_stride;
                for (int x = 0; x < w; ++x) {
                    int v = src_line[x];
                    v = v < 0 ? 0 : (v > 255 ? 255 : v);
                    row[x * 3 + c] = (uint8_t)v;
                }
                cur = cs.exchange(cur, next_comp);
            }
        }

        cs.close();
        return 0;
    } catch (...) {
        return -1;
    }
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
