//go:build openslidebench

// The cgo implementation of openslideshim. Built only under the
// `openslidebench` tag; the package doc lives in doc.go (untagged).
package openslideshim

/*
#cgo pkg-config: openslide
#include <openslide.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Slide is an open openslide handle.
type Slide struct {
	h *C.openslide_t
}

// Open opens path with openslide.
func Open(path string) (*Slide, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	h := C.openslide_open(cpath)
	if h == nil {
		return nil, fmt.Errorf("openslideshim: openslide_open(%q) returned NULL", path)
	}
	if errp := C.openslide_get_error(h); errp != nil {
		msg := C.GoString(errp)
		C.openslide_close(h)
		return nil, fmt.Errorf("openslideshim: open error: %s", msg)
	}
	return &Slide{h: h}, nil
}

// LevelDimensions returns the pixel dimensions of the given level.
func (s *Slide) LevelDimensions(level int) (w, h int64) {
	var cw, ch C.int64_t
	C.openslide_get_level_dimensions(s.h, C.int32_t(level), &cw, &ch)
	return int64(cw), int64(ch)
}

// ReadRegion reads a w×h region whose top-left is (x, y) in level-0
// reference coordinates, at the given level, into dst as packed ARGB
// (pre-multiplied) uint32 pixels. dst must hold at least w*h elements.
func (s *Slide) ReadRegion(dst []uint32, level int, x, y, w, h int64) error {
	if int64(len(dst)) < w*h {
		return fmt.Errorf("openslideshim: dst len %d < w*h %d", len(dst), w*h)
	}
	C.openslide_read_region(s.h,
		(*C.uint32_t)(unsafe.Pointer(&dst[0])),
		C.int64_t(x), C.int64_t(y), C.int32_t(level),
		C.int64_t(w), C.int64_t(h))
	if errp := C.openslide_get_error(s.h); errp != nil {
		return fmt.Errorf("openslideshim: read_region error: %s", C.GoString(errp))
	}
	return nil
}

// Close releases the handle.
func (s *Slide) Close() {
	if s.h != nil {
		C.openslide_close(s.h)
		s.h = nil
	}
}
