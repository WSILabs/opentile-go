//go:build !nolcms2

/* ICC colour management for libopentile: slide profile -> sRGB with lcms2.
 * Ported from openscope-native/libopentile/icc.c, extended to RGB8. */
#include <lcms2.h>
#include "internal.h"

void* ot__icc_create(const void* profile, unsigned int size, int rgba) {
    cmsHPROFILE in = cmsOpenProfileFromMem(profile, size);
    if (!in) return 0;
    cmsHPROFILE out = cmsCreate_sRGBProfile();
    cmsUInt32Number fmt = rgba ? TYPE_RGBA_8 : TYPE_RGB_8;
    cmsHTRANSFORM t = cmsCreateTransform(in, fmt, out, fmt, INTENT_PERCEPTUAL,
                                         cmsFLAGS_NOCACHE | (rgba ? cmsFLAGS_COPY_ALPHA : 0));
    cmsCloseProfile(in);
    cmsCloseProfile(out);
    return t;
}

void ot__icc_apply(void* t, unsigned char* pix, int width, int height, int stride) {
    for (int y = 0; y < height; y++) {
        unsigned char* row = pix + (long long)y * stride;
        cmsDoTransform((cmsHTRANSFORM)t, row, row, (cmsUInt32Number)width);
    }
}

void ot__icc_free(void* t) {
    if (t) cmsDeleteTransform((cmsHTRANSFORM)t);
}
