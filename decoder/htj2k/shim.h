#ifndef WSI_HTJ2K_DECODE_H
#define WSI_HTJ2K_DECODE_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// wsi_htj2k_dimensions reads only the codestream header and returns the
// width/height at the given resolution_factor (1/2^r). Returns 0 on success,
// -1 on error.
int wsi_htj2k_dimensions(const uint8_t *src, size_t src_len,
                          int resolution_factor, int *out_w, int *out_h);

// wsi_htj2k_decode decodes a HTJ2K codestream into packed RGB888 at the given
// resolution_factor (DWT resolution-level downscale, 1/2^r). dst_rgb must be
// pre-allocated to at least dst_stride * h bytes (reduced h). dst_stride must
// be >= reduced_w * 3. The reduced width and height are returned via
// out_w / out_h. Returns 0 on success, -1 on error.
int wsi_htj2k_decode(const uint8_t *src, size_t src_len, int resolution_factor,
                     uint8_t *dst_rgb, size_t dst_stride,
                     int *out_w, int *out_h);

// wsi_htj2k_encode_test encodes packed RGB888 as a lossless HTJ2K codestream.
// For test use only — not exposed by the decoder package API.
// On success *outbuf is malloc'd; caller must free. Returns 0 on success, -1 on error.
int wsi_htj2k_encode_test(const uint8_t *rgb, int width, int height,
                           int num_decomp,
                           uint8_t **outbuf, size_t *outsize);

#ifdef __cplusplus
}
#endif

#endif /* WSI_HTJ2K_DECODE_H */
