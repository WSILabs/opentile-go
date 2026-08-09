// Command cshim is the C-ABI FFI shim exposing opentile-go to Python (ctypes).
// Built with `go build -buildmode=c-shared`. Cold structured metadata crosses
// as one JSON blob (ot_metadata_json); hot pixel/byte payloads cross as raw
// malloc'd buffers (later tasks). Handles are runtime/cgo.Handle tokens — never
// raw Go pointers.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {}

// cstr copies a Go string into a C.malloc'd NUL-terminated buffer (Python frees
// it via ot_free).
func cstr(s string) *C.char { return C.CString(s) }

// setErr writes msg into *errOut as a malloc'd C string (if errOut != nil).
func setErr(errOut **C.char, msg string) {
	if errOut != nil {
		*errOut = cstr(msg)
	}
}

//export ot_open
func ot_open(path *C.char, errOut **C.char) C.uintptr_t {
	s, err := opentile.OpenFile(C.GoString(path))
	if err != nil {
		setErr(errOut, err.Error())
		return 0
	}
	return C.uintptr_t(cgo.NewHandle(s))
}

//export ot_close
func ot_close(h C.uintptr_t) {
	if h == 0 {
		return
	}
	hd := cgo.Handle(h)
	if s, ok := hd.Value().(*opentile.Slide); ok {
		_ = s.Close()
	}
	hd.Delete()
}

//export ot_free
func ot_free(p unsafe.Pointer) { C.free(p) }

// slideOf resolves a handle to its *Slide, or nil (+ sets err) on a bad handle.
func slideOf(h C.uintptr_t, errOut **C.char) *opentile.Slide {
	if h == 0 {
		setErr(errOut, "opentile: nil handle")
		return nil
	}
	s, ok := cgo.Handle(h).Value().(*opentile.Slide)
	if !ok {
		setErr(errOut, "opentile: invalid handle")
		return nil
	}
	return s
}

// metaLevel / metaDoc mirror the JSON schema in the spec.
type metaLevel struct {
	Size        [2]int  `json:"size"`
	TileSize    [2]int  `json:"tile_size"`
	Grid        [2]int  `json:"grid"`
	Downsample  float64 `json:"downsample"`
	Overlapping bool    `json:"overlapping"`
}

type metaDoc struct {
	Format        string            `json:"format"`
	MPP           *[2]float64       `json:"mpp"`
	Magnification *float64          `json:"magnification"`
	Properties    map[string]string `json:"properties"`
	Levels        []metaLevel       `json:"levels"`
	Associated    []string          `json:"associated"`
}

//export ot_metadata_json
func ot_metadata_json(h C.uintptr_t, errOut **C.char) *C.char {
	s := slideOf(h, errOut)
	if s == nil {
		return nil
	}
	md := s.Metadata()
	props := md.Properties
	if props == nil {
		props = map[string]string{} // JSON {} not null when a format sets no properties
	}
	doc := metaDoc{
		Format:     string(s.Format()),
		Properties: props,
	}
	if md.MPP.X != 0 || md.MPP.Y != 0 {
		doc.MPP = &[2]float64{md.MPP.X, md.MPP.Y}
	}
	if md.Magnification != 0 {
		m := md.Magnification
		doc.Magnification = &m
	}
	for _, lv := range s.Levels() {
		doc.Levels = append(doc.Levels, metaLevel{
			Size:        [2]int{lv.Size.W, lv.Size.H},
			TileSize:    [2]int{lv.TileSize.W, lv.TileSize.H},
			Grid:        [2]int{lv.Grid.W, lv.Grid.H},
			Downsample:  lv.Downsample,
			Overlapping: lv.Overlapping,
		})
	}
	for _, a := range s.AssociatedImages() {
		doc.Associated = append(doc.Associated, string(a.Type()))
	}
	b, err := json.Marshal(doc)
	if err != nil {
		setErr(errOut, "opentile: metadata marshal: "+err.Error())
		return nil
	}
	return cstr(string(b))
}
