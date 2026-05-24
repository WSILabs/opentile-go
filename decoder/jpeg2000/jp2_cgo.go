//go:build cgo && !nocgo

// Package jpeg2000 implements the JPEG 2000 decoder via openjp2.
// TIFF Compression=33003 (Aperio convention) and 34712 (libtiff
// convention). Does not support IDCT-time scaling; Decode rejects
// DecodeOptions.Scale != 1.
package jpeg2000

/*
#cgo pkg-config: libopenjp2
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <openjpeg.h>

// Buffer stream state for in-memory decode.
typedef struct {
    const uint8_t *data;
    size_t         len;
    size_t         pos;
} buf_stream_state_t;

static OPJ_SIZE_T buf_read(void *dst, OPJ_SIZE_T nb, void *ud) {
    buf_stream_state_t *s = (buf_stream_state_t *)ud;
    if (s->pos >= s->len) return (OPJ_SIZE_T)-1;
    size_t avail = s->len - s->pos;
    if ((size_t)nb > avail) nb = (OPJ_SIZE_T)avail;
    memcpy(dst, s->data + s->pos, (size_t)nb);
    s->pos += (size_t)nb;
    return nb;
}

static OPJ_OFF_T buf_skip(OPJ_OFF_T nb, void *ud) {
    buf_stream_state_t *s = (buf_stream_state_t *)ud;
    if (nb < 0) return -1;
    size_t avail = s->len - s->pos;
    size_t skip = (size_t)nb > avail ? avail : (size_t)nb;
    s->pos += skip;
    return (OPJ_OFF_T)skip;
}

static OPJ_BOOL buf_seek(OPJ_OFF_T nb, void *ud) {
    buf_stream_state_t *s = (buf_stream_state_t *)ud;
    if (nb < 0 || (size_t)nb > s->len) return OPJ_FALSE;
    s->pos = (size_t)nb;
    return OPJ_TRUE;
}

// No-op message handlers to suppress stderr noise.
static void noop_handler(const char *msg, void *client_data) {
    (void)msg; (void)client_data;
}

// opj_jpeg2000_dimensions reads the image header and returns the
// decoded image width/height. Returns 0 on success, -1 on failure.
// codec_format: OPJ_CODEC_J2K or OPJ_CODEC_JP2
static int opj_jpeg2000_dimensions(const uint8_t *in, size_t in_len,
                                   int codec_format,
                                   int *out_w, int *out_h) {
    buf_stream_state_t state = { in, in_len, 0 };

    opj_stream_t *stream = opj_stream_default_create(OPJ_TRUE);
    if (!stream) return -1;
    opj_stream_set_user_data(stream, &state, NULL);
    opj_stream_set_user_data_length(stream, (OPJ_UINT64)in_len);
    opj_stream_set_read_function(stream, buf_read);
    opj_stream_set_skip_function(stream, buf_skip);
    opj_stream_set_seek_function(stream, buf_seek);

    opj_codec_t *codec = opj_create_decompress((OPJ_CODEC_FORMAT)codec_format);
    if (!codec) {
        opj_stream_destroy(stream);
        return -1;
    }
    opj_set_info_handler(codec, noop_handler, NULL);
    opj_set_warning_handler(codec, noop_handler, NULL);
    opj_set_error_handler(codec, noop_handler, NULL);

    opj_dparameters_t params;
    opj_set_default_decoder_parameters(&params);
    if (!opj_setup_decoder(codec, &params)) {
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }

    opj_image_t *image = NULL;
    if (!opj_read_header(stream, codec, &image)) {
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }

    *out_w = (int)(image->x1 - image->x0);
    *out_h = (int)(image->y1 - image->y0);

    opj_image_destroy(image);
    opj_destroy_codec(codec);
    opj_stream_destroy(stream);
    return 0;
}

// opj_jpeg2000_decode decodes the J2K/JP2 codestream and writes packed
// RGB888 into out (which must be w*h*3 bytes). Returns 0 on success, -1 on failure.
// The color_space_out argument receives the opj_image_t color_space value.
static int opj_jpeg2000_decode(const uint8_t *in, size_t in_len,
                               int codec_format,
                               uint8_t *out, int w, int h,
                               int *color_space_out) {
    buf_stream_state_t state = { in, in_len, 0 };

    opj_stream_t *stream = opj_stream_default_create(OPJ_TRUE);
    if (!stream) return -1;
    opj_stream_set_user_data(stream, &state, NULL);
    opj_stream_set_user_data_length(stream, (OPJ_UINT64)in_len);
    opj_stream_set_read_function(stream, buf_read);
    opj_stream_set_skip_function(stream, buf_skip);
    opj_stream_set_seek_function(stream, buf_seek);

    opj_codec_t *codec = opj_create_decompress((OPJ_CODEC_FORMAT)codec_format);
    if (!codec) {
        opj_stream_destroy(stream);
        return -1;
    }
    opj_set_info_handler(codec, noop_handler, NULL);
    opj_set_warning_handler(codec, noop_handler, NULL);
    opj_set_error_handler(codec, noop_handler, NULL);

    opj_dparameters_t params;
    opj_set_default_decoder_parameters(&params);
    if (!opj_setup_decoder(codec, &params)) {
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }

    opj_image_t *image = NULL;
    if (!opj_read_header(stream, codec, &image)) {
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }

    if (!opj_decode(codec, stream, image)) {
        opj_image_destroy(image);
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }
    if (!opj_end_decompress(codec, stream)) {
        opj_image_destroy(image);
        opj_destroy_codec(codec);
        opj_stream_destroy(stream);
        return -1;
    }

    *color_space_out = (int)image->color_space;

    // Pack component planes into packed RGB (or YCbCr -> RGB).
    // For Aperio 33003 tiles, color_space is typically OPJ_CLRSPC_SYCC or
    // OPJ_CLRSPC_UNSPECIFIED; treat 3-component as YCbCr by default.
    // For Aperio 33005 (RGB), color_space == OPJ_CLRSPC_SRGB.
    int is_ycbcr = (image->numcomps == 3 && image->color_space != OPJ_CLRSPC_SRGB);

    int n = w * h;
    for (int i = 0; i < n; i++) {
        int v0 = image->comps[0].data[i];
        int v1 = image->comps[1].data[i];
        int v2 = image->comps[2].data[i];

        int r, g, b;
        if (is_ycbcr) {
            // Standard YCbCr -> RGB:
            // Y = v0, Cb = v1, Cr = v2
            // Aperio stores as offset (signed) components; values are typically
            // in range 0-255 for Y, -128..127 for Cb/Cr (or 0..255 with offset).
            // OpenJPEG decodes them as unsigned integers for OPJ_CLRSPC_SYCC.
            // For SYCC the chroma components have offset 128 already factored in
            // by OpenJPEG; treat them directly here as 0..255 with 128 center.
            int Y  = v0;
            int Cb = v1 - 128;
            int Cr = v2 - 128;
            r = (int)(Y + 1.402  * Cr);
            g = (int)(Y - 0.34414 * Cb - 0.71414 * Cr);
            b = (int)(Y + 1.772  * Cb);
        } else {
            r = v0;
            g = v1;
            b = v2;
        }

        // Clamp to [0, 255].
        r = r < 0 ? 0 : (r > 255 ? 255 : r);
        g = g < 0 ? 0 : (g > 255 ? 255 : g);
        b = b < 0 ? 0 : (b > 255 ? 255 : b);

        out[i*3+0] = (uint8_t)r;
        out[i*3+1] = (uint8_t)g;
        out[i*3+2] = (uint8_t)b;
    }

    opj_image_destroy(image);
    opj_destroy_codec(codec);
    opj_stream_destroy(stream);
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

func (f *factory) Name() string                  { return "jpeg2000" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{33003, 34712} }
func (f *factory) New() decoder.Decoder          { return &cgoDecoder{} }

type cgoDecoder struct{}

// detectCodecFormat returns OPJ_CODEC_J2K or OPJ_CODEC_JP2 based on
// the first 2 bytes of the codestream.
// J2K SOC marker = FF 4F; JP2 box signature starts with 00 00 00 0C.
func detectCodecFormat(src []byte) C.int {
	if len(src) >= 2 && src[0] == 0xFF && src[1] == 0x4F {
		return C.OPJ_CODEC_J2K
	}
	return C.OPJ_CODEC_JP2
}

func (d *cgoDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("decoder/jpeg2000: empty input: %w", decoder.ErrCorruptInput)
	}

	scale := opts.Scale
	if scale != 0 && scale != 1 {
		return nil, fmt.Errorf("decoder/jpeg2000: scale=%d not supported: %w", scale, decoder.ErrUnsupportedScale)
	}

	codecFmt := detectCodecFormat(src)

	// Phase 1: read header to get dimensions.
	var cW, cH C.int
	rc := C.opj_jpeg2000_dimensions(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(codecFmt),
		&cW, &cH,
	)
	if rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg2000: failed to read header dimensions: %w", decoder.ErrCorruptInput)
	}
	w, h := int(cW), int(cH)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("decoder/jpeg2000: invalid dimensions %dx%d: %w", w, h, decoder.ErrCorruptInput)
	}

	// Phase 2: allocate output image and decode.
	var dst *decoder.Image
	if opts.Dst == nil {
		dst = decoder.NewImage(w, h)
	} else {
		if opts.Dst.Width != w || opts.Dst.Height != h {
			return nil, fmt.Errorf("decoder/jpeg2000: dst %dx%d != decoded %dx%d: %w",
				opts.Dst.Width, opts.Dst.Height, w, h, decoder.ErrDestinationSize)
		}
		dst = opts.Dst
	}

	var colorSpaceOut C.int
	rc = C.opj_jpeg2000_decode(
		(*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.size_t(len(src)),
		C.int(codecFmt),
		(*C.uint8_t)(unsafe.Pointer(&dst.Pix[0])),
		cW, cH,
		&colorSpaceOut,
	)
	runtime.KeepAlive(src)
	runtime.KeepAlive(dst)
	if rc != 0 {
		return nil, fmt.Errorf("decoder/jpeg2000: decode failed (color_space=%d): %w",
			int(colorSpaceOut), decoder.ErrCorruptInput)
	}

	return dst, nil
}

func (d *cgoDecoder) Close() error { return nil }
