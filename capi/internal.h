/* Internal helpers shared by the cgo files of libopentile. Not public. */
#ifndef OT_INTERNAL_H
#define OT_INTERNAL_H
#include <stddef.h>

/* errors.c: per-thread last error. */
void ot__set_error(int code, const char* msg, size_t n);
int ot__error_code(void);
const char* ot__error_msg(size_t* n);

/* icc.c (built unless nolcms2): slide ICC profile -> sRGB via lcms2.
 * rgba selects TYPE_RGBA_8 (1) or TYPE_RGB_8 (0). Transforms use
 * cmsFLAGS_NOCACHE so one transform is safe across threads. */
void* ot__icc_create(const void* profile, unsigned int size, int rgba);
void ot__icc_apply(void* t, unsigned char* pix, int width, int height, int stride);
void ot__icc_free(void* t);
#endif
