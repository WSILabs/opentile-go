//go:build !nolcms2

package main

/*
#cgo pkg-config: lcms2
#include "internal.h"
*/
import "C"

import "unsafe"

const colorManagementBuilt = true

// iccCreate builds a slide-profile -> sRGB transform for 3- or 4-byte
// pixels, or returns nil if lcms2 can't use the profile.
func iccCreate(profile []byte, rgba bool) unsafe.Pointer {
	if len(profile) == 0 {
		return nil
	}
	r := C.int(0)
	if rgba {
		r = 1
	}
	return C.ot__icc_create(unsafe.Pointer(&profile[0]), C.uint(len(profile)), r)
}

// iccApply transforms width x height pixels in place at C memory pix.
func iccApply(t unsafe.Pointer, pix unsafe.Pointer, width, height, stride int) {
	C.ot__icc_apply(t, (*C.uchar)(pix), C.int(width), C.int(height), C.int(stride))
}

func iccFree(t unsafe.Pointer) { C.ot__icc_free(t) }
