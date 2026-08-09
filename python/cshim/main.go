// Command cshim is the C-ABI FFI shim exposing opentile-go to Python (ctypes).
// Built with `go build -buildmode=c-shared`. Cold structured metadata crosses
// as one JSON blob (ot_metadata_json); hot pixel/byte payloads cross as raw
// malloc'd buffers (later tasks). Handles are runtime/cgo.Handle tokens — never
// raw Go pointers.
package main

/*
#include <stdlib.h>
#include <stdint.h>
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
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

// blitTight copies a decoder.Image's row-padded Pix into a freshly malloc'd,
// tightly-packed Height*Width*bands C buffer (dropping per-row stride padding)
// and returns the pointer, byte length, width, height, and band count. The
// caller frees the buffer via ot_free.
func blitTight(img *decoder.Image) (unsafe.Pointer, C.size_t, C.int, C.int, C.int) {
	bands := 3
	if img.Format == decoder.PixelFormatRGBA {
		bands = 4
	}
	rowBytes := img.Width * bands
	total := rowBytes * img.Height
	buf := C.malloc(C.size_t(total))
	dst := unsafe.Slice((*byte)(buf), total)
	for y := 0; y < img.Height; y++ {
		copy(dst[y*rowBytes:(y+1)*rowBytes], img.Pix[y*img.Stride:y*img.Stride+rowBytes])
	}
	return buf, C.size_t(total), C.int(img.Width), C.int(img.Height), C.int(bands)
}

func fmtOpt(rgba C.int) []opentile.DecodeOption {
	if rgba != 0 {
		return []opentile.DecodeOption{opentile.WithFormat(decoder.PixelFormatRGBA)}
	}
	return nil
}

//export ot_tile
func ot_tile(h C.uintptr_t, level, x, y C.int, out **C.uint8_t, outLen *C.size_t, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	b, err := lv.Tile(int(x), int(y))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf := C.malloc(C.size_t(len(b)))
	copy(unsafe.Slice((*byte)(buf), len(b)), b)
	*out = (*C.uint8_t)(buf)
	*outLen = C.size_t(len(b))
	return 0
}

//export ot_decoded_tile
func ot_decoded_tile(h C.uintptr_t, level, x, y, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	img, err := lv.DecodedTile(int(x), int(y), fmtOpt(rgba)...)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_read_region
func ot_read_region(h C.uintptr_t, level, x, y, w, ht, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	region := opentile.Region{Origin: opentile.Point{X: int(x), Y: int(y)}, Size: opentile.Size{W: int(w), H: int(ht)}}
	img, err := lv.ReadRegion(region, fmtOpt(rgba)...)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, ow, oh, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, ow, oh, bands
	return 0
}

//export ot_thumbnail
func ot_thumbnail(h C.uintptr_t, maxW, maxH C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	img, err := s.RenderThumbnail(opentile.Size{W: int(maxW), H: int(maxH)})
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_macro
func ot_macro(h C.uintptr_t, maxW, maxH C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	img, err := s.RenderMacro(opentile.Size{W: int(maxW), H: int(maxH)})
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_associated
func ot_associated(h C.uintptr_t, name *C.char, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	want := C.GoString(name)
	var fmt decoder.PixelFormat
	if rgba != 0 {
		fmt = decoder.PixelFormatRGBA
	}
	for _, a := range s.AssociatedImages() {
		if string(a.Type()) == want {
			img, err := a.Decode(decoder.DecodeOptions{Format: fmt})
			if err != nil {
				setErr(errOut, err.Error())
				return -1
			}
			buf, n, w, ht, bands := blitTight(img)
			*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
			return 0
		}
	}
	setErr(errOut, "opentile: no associated image named "+want)
	return -1
}

//export ot_tiff_tags_json
func ot_tiff_tags_json(h C.uintptr_t, level C.int, out **C.char, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	tags, ok := lv.TIFFTags()
	if !ok {
		*out = nil // signal "no tags" (null pointer, status 0)
		return 0
	}
	type jtag struct {
		Number uint16   `json:"number"`
		Name   string   `json:"name"`
		ASCII  *string  `json:"ascii,omitempty"`
		Uints  []uint64 `json:"uints,omitempty"`
	}
	var out2 []jtag
	for _, t := range tags {
		jt := jtag{Number: t.Number, Name: t.Name}
		if a, ok := t.ASCII(); ok {
			jt.ASCII = &a
		} else if u, ok := t.Uints(); ok {
			jt.Uints = u
		}
		out2 = append(out2, jt)
	}
	b, err := json.Marshal(out2)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	*out = cstr(string(b))
	return 0
}
