#ifndef WSI_HTJ2K_DECODE_H
#define WSI_HTJ2K_DECODE_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// wsi_htj2k_dimensions reads only the codestream header and returns width/height.
// Returns 0 on success, -1 on error.
int wsi_htj2k_dimensions(const uint8_t *src, size_t src_len,
                          int *out_w, int *out_h);

// wsi_htj2k_decode decodes a HTJ2K codestream into packed RGB888.
// dst_rgb must be pre-allocated to at least dst_stride * h bytes.
// dst_stride must be >= w * 3.
// Width and height are returned via out_w / out_h.
// Returns 0 on success, -1 on error.
int wsi_htj2k_decode(const uint8_t *src, size_t src_len,
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
