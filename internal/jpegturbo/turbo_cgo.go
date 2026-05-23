//go:build cgo && !nocgo

package jpegturbo

/*
#cgo pkg-config: libturbojpeg
#include <turbojpeg.h>
#include <stdlib.h>
#include <string.h>

// go_tj_transform_crop runs tjTransform with TJXOPT_CROP|TJXOPT_PERFECT.
// On failure, copies tjGetErrorStr2's message into err_msg (up to err_cap-1
// bytes, NUL-terminated) before the handle is destroyed. Returns 0 on
// success, non-zero on any failure.
//
// Caller owns: freeing *dst with tjFree iff *dst != NULL, regardless of rc.
static int go_tj_transform_crop(
    const unsigned char *src, unsigned long src_size,
    int x, int y, int w, int h,
    unsigned char **dst, unsigned long *dst_size,
    char *err_msg, int err_cap
) {
    if (err_msg != NULL && err_cap > 0) {
        err_msg[0] = '\0';
    }
    tjhandle h_ = tjInitTransform();
    if (h_ == NULL) {
        if (err_msg != NULL && err_cap > 0) {
            // tjGetErrorStr2 requires a handle; use the legacy tjGetErrorStr
            // when handle init failed.
            const char *m = tjGetErrorStr();
            if (m != NULL) {
                strncpy(err_msg, m, err_cap - 1);
                err_msg[err_cap - 1] = '\0';
            }
        }
        return -1;
    }
    tjtransform t = {0};
    t.r.x = x;
    t.r.y = y;
    t.r.w = w;
    t.r.h = h;
    t.op = TJXOP_NONE;
    t.options = TJXOPT_CROP | TJXOPT_PERFECT;

    int rc = tjTransform(h_, src, src_size, 1, dst, dst_size, &t, 0);
    if (rc != 0 && err_msg != NULL && err_cap > 0) {
        const char *m = tjGetErrorStr2(h_);
        if (m != NULL) {
            strncpy(err_msg, m, err_cap - 1);
            err_msg[err_cap - 1] = '\0';
        }
    }
    tjDestroy(h_);
    return rc;
}

// Data passed to go_tj_fill_background via tjtransform.data. Mirrors
// PyTurboJPEG's BackgroundStruct: image dimensions in pixels plus the
// quantised DC coefficient to write for out-of-bounds blocks.
typedef struct fill_bg_data {
    int w;
    int h;
    int lum; // DC coefficient (post-quantisation) to plant in OOB luma blocks
} fill_bg_data;

// go_tj_fill_background is a CUSTOMFILTER callback that zeros the DCT
// coefficients of blocks whose pixel position is past (w, h) and sets
// their DC coefficient to `lum`. This lets tjTransform tolerate crop
// regions that extend past the source image by filling the OOB area
// with a solid color in the DCT domain.
//
// Port of PyTurboJPEG's fill_background (turbojpeg.py:202-287). Component
// 0 (luma) is filled; chroma components are left alone — for luma-only
// fills the chroma DC will bias the color slightly toward neutral, which
// matches Python opentile's default behaviour.
//
// MCU sizes in the coeff array are always 8x8 regardless of JPEG
// subsampling (libjpeg-turbo normalizes per-component blocks).
#define MCU_SIDE 8
#define MCU_AREA 64

static int go_tj_fill_background(
    short *coeffs,
    tjregion arrayRegion,
    tjregion planeRegion,
    int componentID,
    int transformID,
    struct tjtransform *transform
) {
    (void)transformID;
    if (componentID != 0) return 1; // only touch luma
    fill_bg_data *d = (fill_bg_data *)transform->data;
    if (d == NULL) return 1;

    int aw = arrayRegion.w;
    int ah = arrayRegion.h;
    int ax = arrayRegion.x;
    int ay = arrayRegion.y;
    int pw = planeRegion.w;
    // planeRegion.h is not needed below; the "under" pass uses ay+ah.

    // coeffs is laid out as (ah/8) rows of (aw/8) blocks, 64 shorts each.
    // Access: block(row, col)[0..63], DC = [0], AC = [1..63].
    int blocks_per_row = aw / MCU_SIDE;

    // Fill MCUs to the RIGHT of the original image (columns ≥ d->w/8 in
    // plane coords) for each row within arrayRegion that is still inside
    // the original image vertically (rows < d->h/8 in plane coords).
    int left_start_row_px = ay; if (left_start_row_px > d->h) left_start_row_px = d->h;
    left_start_row_px -= ay;
    int left_end_row_px = ay + ah; if (left_end_row_px > d->h) left_end_row_px = d->h;
    left_end_row_px -= ay;

    for (int bx = d->w / MCU_SIDE; bx < pw / MCU_SIDE; bx++) {
        // Convert plane-coord block X to array-coord block X.
        int arr_bx = bx - (ax / MCU_SIDE);
        if (arr_bx < 0 || arr_bx >= blocks_per_row) continue;
        for (int by = left_start_row_px / MCU_SIDE; by < left_end_row_px / MCU_SIDE; by++) {
            short *blk = coeffs + (by * blocks_per_row + arr_bx) * MCU_AREA;
            // Mirror PyTurboJPEG fill_background (turbojpeg.py:271):
            // `coeffs[y][x][0] = background_data.lum`. AC coefficients are
            // untouched.
            blk[0] = (short)d->lum;
        }
    }

    // Fill MCUs BELOW the original image (rows ≥ d->h/8 in plane coords)
    // across the entire array width.
    int bottom_start_row_px = ay; if (bottom_start_row_px < d->h) bottom_start_row_px = d->h;
    bottom_start_row_px -= ay;
    int bottom_end_row_px = ay + ah; if (bottom_end_row_px < d->h) bottom_end_row_px = d->h;
    bottom_end_row_px -= ay;

    for (int bx = 0; bx < pw / MCU_SIDE; bx++) {
        int arr_bx = bx - (ax / MCU_SIDE);
        if (arr_bx < 0 || arr_bx >= blocks_per_row) continue;
        for (int by = bottom_start_row_px / MCU_SIDE; by < bottom_end_row_px / MCU_SIDE; by++) {
            short *blk = coeffs + (by * blocks_per_row + arr_bx) * MCU_AREA;
            // DC-only fill; see comment on the "right-of-image" pass above.
            blk[0] = (short)d->lum;
        }
    }

    return 1;
}

// go_tj_transform_crop_fill is like go_tj_transform_crop but attaches a
// fill-background CUSTOMFILTER callback. img_w and img_h are the
// ORIGINAL image dimensions (what the SOF advertises on input);
// libjpeg-turbo needs these so the callback can decide which blocks are
// OOB.
//
// `lum` is the post-quantisation DC coefficient written to OOB luma
// blocks. Pass 0 for a mid-gray fill (DC=0 maps to level-shift-neutral
// 128). Callers derive `lum` from the requested BackgroundLuminance via
// internal/jpeg.LuminanceToDCCoefficient, which ports PyTurboJPEG's
// __map_luminance_to_dc_dct_coefficient:
//
//	lum = round((luminance * 2047 - 1024) / dc_dqt)
//
// where dc_dqt is the DC element of the source's luma quantization table.
static int go_tj_transform_crop_fill(
    const unsigned char *src, unsigned long src_size,
    int x, int y, int w, int h,
    int img_w, int img_h, int lum,
    unsigned char **dst, unsigned long *dst_size,
    char *err_msg, int err_cap
) {
    if (err_msg != NULL && err_cap > 0) {
        err_msg[0] = '\0';
    }
    tjhandle h_ = tjInitTransform();
    if (h_ == NULL) {
        if (err_msg != NULL && err_cap > 0) {
            const char *m = tjGetErrorStr();
            if (m != NULL) {
                strncpy(err_msg, m, err_cap - 1);
                err_msg[err_cap - 1] = '\0';
            }
        }
        return -1;
    }

    fill_bg_data fd;
    fd.w = img_w;
    fd.h = img_h;
    fd.lum = lum;

    tjtransform t = {0};
    t.r.x = x;
    t.r.y = y;
    t.r.w = w;
    t.r.h = h;
    t.op = TJXOP_NONE;
    t.options = TJXOPT_CROP | TJXOPT_PERFECT;
    t.data = &fd;
    t.customFilter = go_tj_fill_background;

    int rc = tjTransform(h_, src, src_size, 1, dst, dst_size, &t, 0);
    // libjpeg-turbo returns rc=-1 on both warnings (recoverable —
    // truncated input but valid output produced) and fatal errors. OME
    // OneFrame inputs are page-tag-claimed-WxH JPEGs whose scan data
    // covers only the first row (the rest is library OOB-fill); the
    // result is a TJERR_WARNING ("premature end of data segment")
    // that Python opentile silently tolerates. We mirror that for
    // parity by treating warnings as success.
    if (rc != 0) {
        int code = tjGetErrorCode(h_);
        if (code == TJERR_WARNING && *dst != NULL && *dst_size > 0) {
            rc = 0; // warning, output is valid — proceed
        } else if (err_msg != NULL && err_cap > 0) {
            const char *m = tjGetErrorStr2(h_);
            if (m != NULL) {
                strncpy(err_msg, m, err_cap - 1);
                err_msg[err_cap - 1] = '\0';
            }
        }
    }
    tjDestroy(h_);
    return rc;
}

// fill_frame_data is passed to go_tj_fill_frame via tjtransform.data.
// Mirrors PyTurboJPEG's BlankStruct: only the luma DC coefficient is
// needed (chroma planes are zeroed entirely).
typedef struct fill_frame_data {
    int lum;
} fill_frame_data;

// go_tj_fill_frame is a CUSTOMFILTER callback that overwrites every DCT
// coefficient in the source image with a luminance-derived constant.
// Component 0 (luma): zero AC, set DC = `lum` on every 8x8 block.
// Components 1+ (chroma): zero everything (DC = 0 = level-shift-neutral
// 128). Together this yields a solid-color decode that preserves the
// original DQT/DHT/sampling-factor encoding.
//
// Direct port of PyTurboJPEG's BlankImage.callback (jpeg_filler.py:102-152).
// Note that upstream's chroma path computes a non-zero MCU iteration but
// only writes after zeroing — with dc_component=0 for chroma the writes
// are no-ops, so we collapse to "zero everything for chroma".
static int go_tj_fill_frame(
    short *coeffs,
    tjregion arrayRegion,
    tjregion planeRegion,
    int componentID,
    int transformID,
    struct tjtransform *transform
) {
    (void)planeRegion;
    (void)transformID;
    fill_frame_data *d = (fill_frame_data *)transform->data;
    if (d == NULL) return 1;

    int blocks_per_row = arrayRegion.w / MCU_SIDE;
    int blocks_per_col = arrayRegion.h / MCU_SIDE;
    int total_blocks = blocks_per_row * blocks_per_col;

    // Zero every coefficient first.
    memset(coeffs, 0, (size_t)total_blocks * MCU_AREA * sizeof(short));

    if (componentID == 0) {
        for (int i = 0; i < total_blocks; i++) {
            coeffs[i * MCU_AREA] = (short)d->lum;
        }
    }
    return 1;
}

// go_tj_transform_fill_frame runs tjTransform with the all-blocks
// fill-frame callback. The crop region covers the full source image
// (no actual cropping); the callback overwrites every DCT coefficient
// to produce a solid-color result while preserving the original
// encoding tables.
//
// `lum` is the post-quantisation DC coefficient written to luma blocks
// (chroma DC is zero). Caller derives `lum` from the requested
// luminance via internal/jpeg.LuminanceToDCCoefficient.
static int go_tj_transform_fill_frame(
    const unsigned char *src, unsigned long src_size,
    int img_w, int img_h, int lum,
    unsigned char **dst, unsigned long *dst_size,
    char *err_msg, int err_cap
) {
    if (err_msg != NULL && err_cap > 0) {
        err_msg[0] = '\0';
    }
    tjhandle h_ = tjInitTransform();
    if (h_ == NULL) {
        if (err_msg != NULL && err_cap > 0) {
            const char *m = tjGetErrorStr();
            if (m != NULL) {
                strncpy(err_msg, m, err_cap - 1);
                err_msg[err_cap - 1] = '\0';
            }
        }
        return -1;
    }

    fill_frame_data fd;
    fd.lum = lum;

    tjtransform t = {0};
    t.r.x = 0;
    t.r.y = 0;
    t.r.w = img_w;
    t.r.h = img_h;
    t.op = TJXOP_NONE;
    t.options = TJXOPT_PERFECT;
    t.data = &fd;
    t.customFilter = go_tj_fill_frame;

    int rc = tjTransform(h_, src, src_size, 1, dst, dst_size, &t, 0);
    if (rc != 0 && err_msg != NULL && err_cap > 0) {
        const char *m = tjGetErrorStr2(h_);
        if (m != NULL) {
            strncpy(err_msg, m, err_cap - 1);
            err_msg[err_cap - 1] = '\0';
        }
    }
    tjDestroy(h_);
    return rc;
}

// go_tj_header_dims reads the SOF of src via tjDecompressHeader3 and
// returns the image width/height. Returns 0 on success, non-zero on
// failure; error message copied into err_msg when non-NULL.
static int go_tj_header_dims(
    const unsigned char *src, unsigned long src_size,
    int *out_w, int *out_h,
    char *err_msg, int err_cap
) {
    if (err_msg != NULL && err_cap > 0) {
        err_msg[0] = '\0';
    }
    tjhandle h_ = tjInitDecompress();
    if (h_ == NULL) {
        if (err_msg != NULL && err_cap > 0) {
            const char *m = tjGetErrorStr();
            if (m != NULL) {
                strncpy(err_msg, m, err_cap - 1);
                err_msg[err_cap - 1] = '\0';
            }
        }
        return -1;
    }
    int subsamp = 0;
    int colorspace = 0;
    int rc = tjDecompressHeader3(h_, src, src_size, out_w, out_h, &subsamp, &colorspace);
    if (rc != 0 && err_msg != NULL && err_cap > 0) {
        const char *m = tjGetErrorStr2(h_);
        if (m != NULL) {
            strncpy(err_msg, m, err_cap - 1);
            err_msg[err_cap - 1] = '\0';
        }
    }
    tjDestroy(h_);
    return rc;
}
*/
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/wsilabs/opentile-go/internal/jpeg"
)

// Crop performs an MCU-aligned lossless crop of src using libjpeg-turbo's
// tjTransform with TJXOPT_CROP and TJXOPT_PERFECT. A region that is not
// MCU-aligned, or that extends past the source dimensions, is rejected by
// libjpeg-turbo with a non-zero return code and Crop returns an error.
//
// Concurrency: Crop creates and destroys a fresh tjhandle per call, so
// concurrent invocations from different goroutines do not share mutable
// state. libjpeg-turbo's tjInitTransform/tjDestroy are reentrant.
//
// See CropWithBackground for a variant that tolerates out-of-bounds crops
// by filling the OOB blocks with a background color.
func Crop(src []byte, r Region) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("jpegturbo: empty source")
	}
	var dst *C.uchar
	var dstSize C.ulong

	// Error message buffer. tjGetErrorStr2 messages are typically short;
	// 256 bytes is ample.
	const errBufLen = 256
	errBuf := make([]byte, errBufLen)
	errPtr := (*C.char)(unsafe.Pointer(&errBuf[0]))

	rc := C.go_tj_transform_crop(
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		C.int(r.X), C.int(r.Y), C.int(r.Width), C.int(r.Height),
		&dst, &dstSize,
		errPtr, C.int(errBufLen),
	)
	// Always free dst when non-NULL — libjpeg-turbo may have allocated it
	// partially before an error is detected.
	if dst != nil {
		defer C.tjFree(dst)
	}
	if rc != 0 {
		msg := C.GoString(errPtr)
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("jpegturbo: tjTransform failed (rc=%d): %s", rc, msg)
	}
	out := C.GoBytes(unsafe.Pointer(dst), C.int(dstSize))
	return out, nil
}

// CropWithBackground behaves like Crop but tolerates crop regions that
// extend past the source image. Out-of-bounds DCT blocks are filled via
// a CUSTOMFILTER callback so the requested crop can be any size. Required
// for NDPI edge tiles, where the pyramid level's JPEG dimensions are not
// an integer multiple of the output tile size.
//
// Fill behavior: the OOB region is filled with white (luminance=1.0) to
// match Python opentile's PyTurboJPEG.crop_multiple default. The DC DCT
// coefficient for OOB luma blocks is computed per-JPEG from the source's
// luma quantization table (DQT table ID 0), reproducing
// PyTurboJPEG.__map_luminance_to_dc_dct_coefficient exactly. Chroma DC
// coefficients are left at 0 (level-shift-neutral 128), producing
// pixels that decode close-to-but-not-exactly white (the small residual
// chroma offset comes from JPEG's centred-128 chroma representation).
// This matches PyTurboJPEG's behavior bit-for-bit; for exact white the
// caller would need a chroma extension that we don't currently
// implement (tracked as a future enhancement).
//
// For non-white fills (e.g. background_luminance=0 for black), use
// CropWithBackgroundLuminance.
//
// Concurrency: as Crop — fresh tjhandle per call; safe for parallel use.
func CropWithBackground(src []byte, r Region) ([]byte, error) {
	return CropWithBackgroundLuminance(src, r, DefaultBackgroundLuminance)
}

// CropWithBackgroundLuminance is the full-featured variant of
// CropWithBackground with a caller-specified background luminance in
// the range [0, 1] (0 = black, 1 = white, 0.5 = mid-gray). Equivalent
// to CropWithBackgroundLuminanceOpts with empty opts (per-call DQT
// parse).
func CropWithBackgroundLuminance(src []byte, r Region, luminance BackgroundLuminance) ([]byte, error) {
	return CropWithBackgroundLuminanceOpts(src, r, luminance, CropOpts{})
}

// CropWithBackgroundLuminanceOpts is the full-featured variant. If
// opts.DCBackground != 0, that value is used directly as the OOB luma DC
// coefficient; otherwise the function parses the source's DQT and
// computes it via internal/jpeg.LuminanceToDCCoefficient (the per-call
// path). Tilers reading many tiles from the same source amortise the
// DQT parse by caching the DC value at level construction.
func CropWithBackgroundLuminanceOpts(src []byte, r Region, luminance BackgroundLuminance, opts CropOpts) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("jpegturbo: empty source")
	}
	var lum int
	if opts.DCBackground != 0 {
		lum = opts.DCBackground
	} else {
		// Derive the luma DC coefficient to write into OOB blocks. Match
		// Python opentile's PyTurboJPEG: scan the source's DQT (table 0)
		// for the DC quantization element, then compute
		// round((luminance * 2047 - 1024) / dc_quant). See
		// internal/jpeg/dqt.go for the port.
		var err error
		lum, err = jpeg.LuminanceToDCCoefficient(src, float64(luminance))
		if err != nil {
			return nil, fmt.Errorf("jpegturbo: derive luma DC from source: %w", err)
		}
	}

	// Read the source image dimensions so the callback knows what's OOB.
	var imgW, imgH C.int
	const errBufLen = 256
	errBuf := make([]byte, errBufLen)
	errPtr := (*C.char)(unsafe.Pointer(&errBuf[0]))

	rcDim := C.go_tj_header_dims(
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		&imgW, &imgH,
		errPtr, C.int(errBufLen),
	)
	if rcDim != 0 {
		msg := C.GoString(errPtr)
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("jpegturbo: tjDecompressHeader3 failed (rc=%d): %s", rcDim, msg)
	}

	var dst *C.uchar
	var dstSize C.ulong

	// Re-zero the error buffer.
	for i := range errBuf {
		errBuf[i] = 0
	}

	rc := C.go_tj_transform_crop_fill(
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		C.int(r.X), C.int(r.Y), C.int(r.Width), C.int(r.Height),
		imgW, imgH, C.int(lum),
		&dst, &dstSize,
		errPtr, C.int(errBufLen),
	)
	if dst != nil {
		defer C.tjFree(dst)
	}
	if rc != 0 {
		msg := C.GoString(errPtr)
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("jpegturbo: tjTransform (fill) failed (rc=%d): %s", rc, msg)
	}
	out := C.GoBytes(unsafe.Pointer(dst), C.int(dstSize))
	return out, nil
}

// FillFrame returns a copy of src whose decoded pixels are a solid
// luminance fill (luminance ∈ [0, 1]; 0 = black, 1 = white). The
// original encoding tables (DQT, DHT, sampling factors, APPn segments)
// are preserved bit-for-bit; every DCT coefficient is overwritten with
// luminance-derived DC + zero AC. Used by the Philips sparse-tile path
// to derive a "blank" replacement tile from a known-valid frame.
//
// Direct port of Python opentile's JpegFiller.fill_image
// (jpeg_filler.py:228-305). The luminance-to-DC mapping is shared with
// CropWithBackground via internal/jpeg.LuminanceToDCCoefficient.
//
// Concurrency: per-call tjInitTransform / tjDestroy. Safe for parallel
// use; output is byte-deterministic for fixed inputs (v0.5 T2 gate).
func FillFrame(src []byte, luminance BackgroundLuminance) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("jpegturbo: empty source")
	}

	lum, err := jpeg.LuminanceToDCCoefficient(src, float64(luminance))
	if err != nil {
		return nil, fmt.Errorf("jpegturbo: derive luma DC from source: %w", err)
	}

	const errBufLen = 256
	errBuf := make([]byte, errBufLen)
	errPtr := (*C.char)(unsafe.Pointer(&errBuf[0]))

	var imgW, imgH C.int
	rcDim := C.go_tj_header_dims(
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		&imgW, &imgH,
		errPtr, C.int(errBufLen),
	)
	if rcDim != 0 {
		msg := C.GoString(errPtr)
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("jpegturbo: tjDecompressHeader3 failed (rc=%d): %s", rcDim, msg)
	}

	for i := range errBuf {
		errBuf[i] = 0
	}

	var dst *C.uchar
	var dstSize C.ulong
	rc := C.go_tj_transform_fill_frame(
		(*C.uchar)(unsafe.Pointer(&src[0])),
		C.ulong(len(src)),
		imgW, imgH, C.int(lum),
		&dst, &dstSize,
		errPtr, C.int(errBufLen),
	)
	if dst != nil {
		defer C.tjFree(dst)
	}
	if rc != 0 {
		msg := C.GoString(errPtr)
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("jpegturbo: tjTransform (fill-frame) failed (rc=%d): %s", rc, msg)
	}
	out := C.GoBytes(unsafe.Pointer(dst), C.int(dstSize))
	return out, nil
}
