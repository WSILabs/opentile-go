//go:build cgo && !nocgo && !nolcms2

package color

/*
#cgo pkg-config: lcms2
#include <lcms2.h>

// A transform from the slide profile to sRGB for 3- or 4-byte pixels, or
// NULL if lcms2 can't use the profile. cmsFLAGS_NOCACHE lets concurrent
// calls share one transform.
static void* ot_color_create(const void* profile, cmsUInt32Number size, int rgba) {
    cmsHPROFILE in = cmsOpenProfileFromMem(profile, size);
    if (!in) return NULL;
    cmsHPROFILE out = cmsCreate_sRGBProfile();
    cmsUInt32Number fmt = rgba ? TYPE_RGBA_8 : TYPE_RGB_8;
    cmsHTRANSFORM t = cmsCreateTransform(in, fmt, out, fmt, INTENT_PERCEPTUAL,
                                         cmsFLAGS_NOCACHE | (rgba ? cmsFLAGS_COPY_ALPHA : 0));
    cmsCloseProfile(in);
    cmsCloseProfile(out);
    return t;
}

static void ot_color_apply(void* t, unsigned char* pix, int width, int height, int stride) {
    for (int y = 0; y < height; y++) {
        unsigned char* row = pix + (long long)y * stride;
        cmsDoTransform((cmsHTRANSFORM)t, row, row, (cmsUInt32Number)width);
    }
}

static void ot_color_free(void* t) {
    if (t) cmsDeleteTransform((cmsHTRANSFORM)t);
}

// For tests: an RGB profile with sRGB's D65 white point and primaries but
// linear (gamma 1.0) tone curves, serialized into buf. Returns the size,
// or 0 if buf is too small or lcms2 fails.
static cmsUInt32Number ot_color_linear_profile(void* buf, cmsUInt32Number cap) {
    cmsCIExyY white = {0.3127, 0.3290, 1.0};
    cmsCIExyYTRIPLE prim = {{0.64, 0.33, 1.0}, {0.30, 0.60, 1.0}, {0.15, 0.06, 1.0}};
    cmsToneCurve* lin = cmsBuildGamma(NULL, 1.0);
    if (!lin) return 0;
    cmsToneCurve* curves[3] = {lin, lin, lin};
    cmsHPROFILE p = cmsCreateRGBProfile(&white, &prim, curves);
    cmsFreeToneCurve(lin);
    if (!p) return 0;
    cmsUInt32Number n = 0;
    if (!cmsSaveProfileToMem(p, NULL, &n) || n > cap || !cmsSaveProfileToMem(p, buf, &n)) n = 0;
    cmsCloseProfile(p);
    return n;
}
*/
import "C"

import (
	"unsafe"

	opentile "github.com/wsilabs/opentile-go"
)

func init() { opentile.SetSRGBTransformHook(newTransform) }

// newTransform builds a profile -> sRGB transform, or returns ok=false if
// lcms2 can't use the profile.
func newTransform(profile []byte, rgba bool) (apply func(pix []byte, width, height, stride int), free func(), ok bool) {
	if len(profile) == 0 {
		return nil, nil, false
	}
	r := C.int(0)
	if rgba {
		r = 1
	}
	t := C.ot_color_create(unsafe.Pointer(&profile[0]), C.cmsUInt32Number(len(profile)), r)
	if t == nil {
		return nil, nil, false
	}
	apply = func(pix []byte, width, height, stride int) {
		if width <= 0 || height <= 0 {
			return
		}
		C.ot_color_apply(t, (*C.uchar)(unsafe.Pointer(&pix[0])), C.int(width), C.int(height), C.int(stride))
	}
	free = func() { C.ot_color_free(t) }
	return apply, free, true
}

// linearProfile returns ot_color_linear_profile's profile (for tests).
func linearProfile() []byte {
	buf := make([]byte, 64<<10)
	n := C.ot_color_linear_profile(unsafe.Pointer(&buf[0]), C.cmsUInt32Number(len(buf)))
	return buf[:n]
}
