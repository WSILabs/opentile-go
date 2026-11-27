// Command capi builds libopentile, the C API over opentile-go
// (-buildmode=c-shared or c-archive). The public header is opentile.h,
// hand-written because Go's generated header isn't MSVC-compatible;
// header_test.go keeps it in sync with the //export set.
//
// Design: docs/superpowers/specs/2026-11-23-capi-design.md.
package main

/*
#cgo CFLAGS: -DOT_NO_PROTOTYPES -DOT_BUILDING
#include <stdlib.h>
#include <string.h>
#include "opentile.h"
#include "internal.h"
*/
import "C"

import (
	"sync"
	"unsafe"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {} // required by -buildmode=c-shared / c-archive

// ---- errors ----

// fail records (code, msg) as the calling thread's last error and returns code.
func fail(code int, msg string) C.int {
	if msg == "" {
		C.ot__set_error(C.int(code), nil, 0)
	} else {
		C.ot__set_error(C.int(code), (*C.char)(unsafe.Pointer(unsafe.StringData(msg))), C.size_t(len(msg)))
	}
	return C.int(code)
}

func failErr(err error) C.int { return fail(codeOf(err), err.Error()) }

// ---- handle table ----

// entry is one open slide. inflight counts calls currently using it, so
// ot_close can wait for them before closing the slide.
type entry struct {
	slide    *opentile.Slide
	inflight sync.WaitGroup
}

var (
	tableMu    sync.RWMutex
	table      = map[uint64]*entry{}
	nextHandle uint64
)

// acquire resolves h and pins it until release; the caller must defer
// e.release() when ok.
func acquire(h C.ot_slide) (e *entry, status C.int) {
	tableMu.RLock()
	e = table[uint64(h)]
	if e != nil {
		e.inflight.Add(1) // under the read lock: happens-before ot_close's Wait
	}
	tableMu.RUnlock()
	if e == nil {
		return nil, fail(errHandle, "opentile: unknown or closed slide handle")
	}
	return e, statusOK
}

func (e *entry) release() { e.inflight.Done() }

// ---- struct / buffer helpers ----

// checkStruct validates a caller struct's leading struct_size against the
// v1 layout size.
func checkStruct(p unsafe.Pointer, size uintptr, name string) C.int {
	if p == nil {
		return fail(errArg, "opentile: "+name+" is NULL")
	}
	if uintptr(*(*C.uint32_t)(p)) < size {
		return fail(errArg, "opentile: "+name+".struct_size is smaller than sizeof("+name+")")
	}
	return statusOK
}

// writeOut copies the fully populated v (struct_size included) to the
// caller's struct and zeroes any tail a newer caller's struct has beyond it.
func writeOut(dst unsafe.Pointer, v unsafe.Pointer, size uintptr) {
	callerSize := uintptr(*(*C.uint32_t)(dst))
	C.memcpy(dst, v, C.size_t(size))
	*(*C.uint32_t)(dst) = C.uint32_t(callerSize)
	if callerSize > size {
		C.memset(unsafe.Add(dst, size), 0, C.size_t(callerSize-size))
	}
}

// writeBytes implements the (dst, capacity, out_len) size-query convention.
// asString adds (and requires room for) a NUL terminator.
func writeBytes(dst unsafe.Pointer, capacity C.size_t, outLen *C.size_t, b []byte, asString bool) C.int {
	if outLen == nil {
		return fail(errArg, "opentile: out_len is NULL")
	}
	*outLen = C.size_t(len(b))
	if dst == nil {
		return statusOK
	}
	need := len(b)
	if asString {
		need++
	}
	if int(capacity) < need {
		return fail(errBuffer, "opentile: destination buffer too small")
	}
	if len(b) > 0 {
		copy(unsafe.Slice((*byte)(dst), len(b)), b)
	}
	if asString {
		*(*byte)(unsafe.Add(dst, len(b))) = 0
	}
	return statusOK
}

func cbool(b bool) C.int32_t {
	if b {
		return 1
	}
	return 0
}

// ---- decode options & images ----

type decodeOpts struct {
	format decoder.PixelFormat
	srgb   bool
	scale  int
	kernel int32
}

func parseDecodeOpts(p *C.ot_decode_opts) (decodeOpts, C.int) {
	var d decodeOpts
	if p == nil {
		return d, statusOK
	}
	if st := checkStruct(unsafe.Pointer(p), unsafe.Sizeof(*p), "ot_decode_opts"); st != statusOK {
		return d, st
	}
	switch p.pixel_format {
	case pixelRGB8:
		d.format = decoder.PixelFormatRGB
	case pixelRGBA8:
		d.format = decoder.PixelFormatRGBA
	default:
		return d, fail(errArg, "opentile: unknown ot_decode_opts.pixel_format")
	}
	if uint32(p.flags)&^knownFlags != 0 {
		return d, fail(errArg, "opentile: unknown ot_decode_opts.flags bits")
	}
	d.srgb = uint32(p.flags)&decodeSRGB != 0
	switch p.scale {
	case 0, 1:
		d.scale = 1
	case 2, 4, 8:
		d.scale = int(p.scale)
	default:
		return d, fail(errArg, "opentile: ot_decode_opts.scale must be 0, 1, 2, 4 or 8")
	}
	d.kernel = int32(p.kernel)
	if _, ok := kernelCodes[d.kernel]; !ok && d.kernel != 0 {
		return d, fail(errArg, "opentile: unknown ot_decode_opts.kernel")
	}
	return d, statusOK
}

func (d decodeOpts) options() []opentile.DecodeOption {
	opts := []opentile.DecodeOption{opentile.WithFormat(d.format)}
	if d.scale > 1 {
		opts = append(opts, opentile.WithScale(d.scale))
	}
	if k, ok := kernelCodes[d.kernel]; ok {
		opts = append(opts, opentile.WithResampleKernel(k))
	}
	return opts
}

// checkImage validates an ot_image before any decode work.
func checkImage(img *C.ot_image) C.int {
	if st := checkStruct(unsafe.Pointer(img), unsafe.Sizeof(*img), "ot_image"); st != statusOK {
		return st
	}
	if img.stride < 0 {
		return fail(errArg, "opentile: ot_image.stride is negative")
	}
	return statusOK
}

// describe fills the output geometry of img for a w x h image in format f
// and returns the number of bytes required.
func describe(img *C.ot_image, w, h int, f decoder.PixelFormat) (need int, st C.int) {
	row := w * bytesPerPixel(f)
	stride := int(img.stride)
	if stride == 0 {
		stride = row
	} else if stride < row {
		return 0, fail(errArg, "opentile: ot_image.stride is smaller than width * bytes per pixel")
	}
	img.width, img.height = C.int32_t(w), C.int32_t(h)
	img.stride = C.int32_t(stride)
	img.pixel_format = C.int32_t(pixelCode(f))
	img.color_managed = 0
	if h > 0 {
		need = stride*(h-1) + row
	}
	return need, statusOK
}

// query answers a data == NULL size query without decoding, when the
// output geometry is known up front.
func query(img *C.ot_image, w, h int, f decoder.PixelFormat) C.int {
	_, st := describe(img, w, h, f)
	return st
}

// emit copies a decoded image into the caller's ot_image (honoring its
// stride) and, for OT_DECODE_SRGB, converts the copy to sRGB with the
// slide's transform (opentile.Slide.ToSRGB) when it has a usable profile.
func emit(e *entry, src *decoder.Image, d decodeOpts, img *C.ot_image) C.int {
	need, st := describe(img, src.Width, src.Height, src.Format)
	if st != statusOK {
		return st
	}
	if img.data == nil {
		return statusOK
	}
	if int(img.capacity) < need {
		return fail(errBuffer, "opentile: ot_image.capacity is too small")
	}
	if need == 0 {
		return statusOK
	}
	row := src.Width * bytesPerPixel(src.Format)
	stride := int(img.stride)
	dst := unsafe.Slice((*byte)(unsafe.Pointer(img.data)), need)
	for y := 0; y < src.Height; y++ {
		copy(dst[y*stride:y*stride+row], src.Pix[y*src.Stride:y*src.Stride+row])
	}
	if d.srgb {
		view := decoder.Image{Width: src.Width, Height: src.Height, Stride: stride, Format: src.Format, Pix: dst}
		if e.slide.ToSRGB(&view) {
			img.color_managed = 1
		}
	}
	return statusOK
}

// level resolves (pyramid, level).
func level(s *opentile.Slide, pyramid, lvl C.int32_t) (*opentile.Level, C.int) {
	p := s.Pyramid(int(pyramid))
	if p == nil {
		return nil, fail(errRange, "opentile: pyramid index out of range")
	}
	l, err := p.Level(int(lvl))
	if err != nil {
		return nil, failErr(err)
	}
	return l, statusOK
}

func associated(s *opentile.Slide, index C.int32_t) (opentile.AssociatedImage, C.int) {
	imgs := s.AssociatedImages()
	if index < 0 || int(index) >= len(imgs) {
		return nil, fail(errRange, "opentile: associated image index out of range")
	}
	return imgs[index], statusOK
}

// ---- library ----

//export ot_api_version
func ot_api_version(major, minor *C.int32_t) C.int {
	if major == nil || minor == nil {
		return fail(errArg, "opentile: NULL argument")
	}
	*major, *minor = apiVersionMajor, apiVersionMinor
	return statusOK
}

//export ot_library_version
func ot_library_version(dst *C.char, capacity C.size_t, outLen *C.size_t) C.int {
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, []byte(libraryVersion()), true)
}

//export ot_codec_available
func ot_codec_available(compression C.int32_t, out *C.int32_t) C.int {
	if out == nil {
		return fail(errArg, "opentile: out_available is NULL")
	}
	c, ok := compressionFromCode(int32(compression))
	if !ok {
		return fail(errArg, "opentile: unknown compression code")
	}
	*out = cbool(opentile.CodecAvailable(c))
	return statusOK
}

//export ot_color_management_available
func ot_color_management_available(out *C.int32_t) C.int {
	if out == nil {
		return fail(errArg, "opentile: out_available is NULL")
	}
	*out = cbool(opentile.ColorManagementAvailable())
	return statusOK
}

// ---- errors ----

//export ot_last_error_code
func ot_last_error_code() C.int { return C.ot__error_code() }

//export ot_last_error
func ot_last_error(dst *C.char, capacity C.size_t, outLen *C.size_t) C.int {
	var n C.size_t
	msg := C.ot__error_msg(&n)
	// Copy without touching the thread's error state (writeBytes' own
	// failures would overwrite the message being read).
	if outLen == nil {
		return errArg
	}
	*outLen = n
	if dst == nil {
		return statusOK
	}
	if capacity < n+1 {
		return errBuffer
	}
	C.memcpy(unsafe.Pointer(dst), unsafe.Pointer(msg), n)
	*(*C.char)(unsafe.Add(unsafe.Pointer(dst), n)) = 0
	return statusOK
}

// ---- lifecycle ----

//export ot_open
func ot_open(path *C.char, opts *C.ot_open_opts, out *C.ot_slide) C.int {
	if path == nil || out == nil {
		return fail(errArg, "opentile: ot_open: path and out must be non-NULL")
	}
	var options []opentile.Option
	backing := int32(backingDefault)
	if opts != nil {
		if st := checkStruct(unsafe.Pointer(opts), unsafe.Sizeof(*opts), "ot_open_opts"); st != statusOK {
			return st
		}
		if opts.tile_w < 0 || opts.tile_h < 0 || (opts.tile_w == 0) != (opts.tile_h == 0) {
			return fail(errArg, "opentile: ot_open_opts.tile_w/tile_h must both be 0 or both positive")
		}
		if opts.tile_w > 0 {
			options = append(options, opentile.WithTileSize(int(opts.tile_w), int(opts.tile_h)))
		}
		if opts.memory_budget < 0 {
			return fail(errArg, "opentile: ot_open_opts.memory_budget is negative")
		}
		if opts.memory_budget > 0 {
			options = append(options, opentile.WithMemoryBudget(int64(opts.memory_budget)))
		}
		backing = int32(opts.backing)
		switch opts.corrupt_tile_policy {
		case corruptTileError:
		case corruptTileBlank:
			options = append(options, opentile.WithCorruptTilePolicy(opentile.CorruptTileBlank))
		default:
			return fail(errArg, "opentile: unknown ot_open_opts.corrupt_tile_policy")
		}
		if opts.ndpi_no_synthesized_label != 0 {
			options = append(options, opentile.WithNDPISynthesizedLabel(false))
		}
	}
	p := C.GoString(path)
	var s *opentile.Slide
	var err error
	switch backing {
	case backingDefault:
		s, err = opentile.OpenFile(p, append(options, opentile.WithBacking(opentile.BackingAuto))...)
	case backingMmap:
		s, err = opentile.OpenFile(p, append(options, opentile.WithBacking(opentile.BackingMmap))...)
	case backingPread:
		s, err = opentile.OpenFile(p, append(options, opentile.WithBacking(opentile.BackingPread))...)
	default:
		return fail(errArg, "opentile: unknown ot_open_opts.backing")
	}
	if err != nil {
		return failErr(err)
	}
	tableMu.Lock()
	nextHandle++
	h := nextHandle
	table[h] = &entry{slide: s}
	tableMu.Unlock()
	*out = C.ot_slide(h)
	return statusOK
}

//export ot_close
func ot_close(h C.ot_slide) C.int {
	tableMu.Lock()
	e := table[uint64(h)]
	delete(table, uint64(h))
	tableMu.Unlock()
	if e == nil {
		return fail(errHandle, "opentile: unknown or closed slide handle")
	}
	e.inflight.Wait()
	if err := e.slide.Close(); err != nil {
		return failErr(err)
	}
	return statusOK
}

// ---- metadata ----

//export ot_slide_info_get
func ot_slide_info_get(h C.ot_slide, out *C.ot_slide_info) C.int {
	if st := checkStruct(unsafe.Pointer(out), unsafe.Sizeof(*out), "ot_slide_info"); st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	s := e.slide
	md := s.Metadata()
	v := C.ot_slide_info{
		format:           C.int32_t(formatCode(s.Format())),
		pyramid_count:    C.int32_t(len(s.Pyramids())),
		associated_count: C.int32_t(len(s.AssociatedImages())),
		magnification:    C.double(md.Magnification),
		mpp_x:            C.double(md.MPP.X),
		mpp_y:            C.double(md.MPP.Y),
		icc_size:         C.uint64_t(len(s.ICCProfile())),
	}
	writeOut(unsafe.Pointer(out), unsafe.Pointer(&v), unsafe.Sizeof(v))
	return statusOK
}

//export ot_pyramid_info_get
func ot_pyramid_info_get(h C.ot_slide, pyramid C.int32_t, out *C.ot_pyramid_info) C.int {
	if st := checkStruct(unsafe.Pointer(out), unsafe.Sizeof(*out), "ot_pyramid_info"); st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	p := e.slide.Pyramid(int(pyramid))
	if p == nil {
		return fail(errRange, "opentile: pyramid index out of range")
	}
	v := C.ot_pyramid_info{level_count: C.int32_t(len(p.Levels))}
	writeOut(unsafe.Pointer(out), unsafe.Pointer(&v), unsafe.Sizeof(v))
	return statusOK
}

//export ot_level_info_get
func ot_level_info_get(h C.ot_slide, pyramid, lvl C.int32_t, out *C.ot_level_info) C.int {
	if st := checkStruct(unsafe.Pointer(out), unsafe.Sizeof(*out), "ot_level_info"); st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	l, st := level(e.slide, pyramid, lvl)
	if st != statusOK {
		return st
	}
	sg := l.StitchedGrid()
	v := C.ot_level_info{
		width: C.int32_t(l.Size.W), height: C.int32_t(l.Size.H),
		tile_w: C.int32_t(l.TileSize.W), tile_h: C.int32_t(l.TileSize.H),
		grid_w: C.int32_t(l.Grid.W), grid_h: C.int32_t(l.Grid.H),
		stitched_grid_w: C.int32_t(sg.W), stitched_grid_h: C.int32_t(sg.H),
		overlap_mode:   C.int32_t(overlapCode(l.OverlapMode)),
		tile_overlap_x: C.int32_t(l.TileOverlap.X), tile_overlap_y: C.int32_t(l.TileOverlap.Y),
		compression: C.int32_t(compressionCode(l.Compression)),
		downsample:  C.double(l.Downsample),
		mpp_x:       C.double(l.MPP.X), mpp_y: C.double(l.MPP.Y),
		focal_plane:    C.double(l.FocalPlane),
		tile_max_bytes: C.uint64_t(max(l.TileMaxSize(), 0)),
	}
	writeOut(unsafe.Pointer(out), unsafe.Pointer(&v), unsafe.Sizeof(v))
	return statusOK
}

//export ot_metadata_json
func ot_metadata_json(h C.ot_slide, dst *C.char, capacity C.size_t, outLen *C.size_t) C.int {
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	b, err := metadataJSON(e.slide)
	if err != nil {
		return failErr(err)
	}
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, b, true)
}

//export ot_icc_profile
func ot_icc_profile(h C.ot_slide, dst *C.uint8_t, capacity C.size_t, outLen *C.size_t) C.int {
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, e.slide.ICCProfile(), false)
}

//export ot_tiff_tags_json
func ot_tiff_tags_json(h C.ot_slide, pyramid, lvl C.int32_t, dst *C.char, capacity C.size_t, outLen *C.size_t) C.int {
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	l, st := level(e.slide, pyramid, lvl)
	if st != statusOK {
		return st
	}
	tags, ok := l.TIFFTags()
	if !ok {
		return fail(errUnsupported, "opentile: this level has no TIFF tags")
	}
	b, err := tiffTagsJSON(tags)
	if err != nil {
		return failErr(err)
	}
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, b, true)
}

// ---- tiles & regions ----

//export ot_tile_raw
func ot_tile_raw(h C.ot_slide, pyramid, lvl, x, y C.int32_t, dst *C.uint8_t, capacity C.size_t, outLen *C.size_t) C.int {
	if outLen == nil {
		return fail(errArg, "opentile: out_len is NULL")
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	l, st := level(e.slide, pyramid, lvl)
	if st != statusOK {
		return st
	}
	b, err := l.Tile(int(x), int(y))
	if err != nil {
		return failErr(err)
	}
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, b, false)
}

//export ot_tile_decoded
func ot_tile_decoded(h C.ot_slide, pyramid, lvl, x, y C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	return levelImage(h, pyramid, lvl, opts, img, nil, func(l *opentile.Level, o []opentile.DecodeOption) (*decoder.Image, error) {
		return l.DecodedTile(int(x), int(y), o...)
	})
}

//export ot_tile_stitched
func ot_tile_stitched(h C.ot_slide, pyramid, lvl, x, y C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	return levelImage(h, pyramid, lvl, opts, img, nil, func(l *opentile.Level, o []opentile.DecodeOption) (*decoder.Image, error) {
		return l.StitchedTile(int(x), int(y), o...)
	})
}

//export ot_read_region
func ot_read_region(h C.ot_slide, pyramid, lvl C.int32_t, x, y C.int64_t, w, ht C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	if w <= 0 || ht <= 0 {
		return fail(errArg, "opentile: ot_read_region: w and h must be positive")
	}
	size := &opentile.Size{W: int(w), H: int(ht)}
	return levelImage(h, pyramid, lvl, opts, img, size, func(l *opentile.Level, o []opentile.DecodeOption) (*decoder.Image, error) {
		return l.ReadRegion(opentile.Region{Origin: opentile.Point{X: int(x), Y: int(y)}, Size: *size}, o...)
	})
}

// levelImage is the shared body of the level-scoped pixel calls. known,
// when non-nil, is the output size at scale 1, letting a size query skip
// the decode.
func levelImage(h C.ot_slide, pyramid, lvl C.int32_t, opts *C.ot_decode_opts, img *C.ot_image,
	known *opentile.Size, read func(*opentile.Level, []opentile.DecodeOption) (*decoder.Image, error)) C.int {
	if st := checkImage(img); st != statusOK {
		return st
	}
	d, st := parseDecodeOpts(opts)
	if st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	l, st := level(e.slide, pyramid, lvl)
	if st != statusOK {
		return st
	}
	if img.data == nil && known != nil && d.scale == 1 {
		return query(img, known.W, known.H, d.format)
	}
	src, err := read(l, d.options())
	if err != nil {
		return failErr(err)
	}
	return emit(e, src, d, img)
}

//export ot_read_region_scaled
func ot_read_region_scaled(h C.ot_slide, pyramid C.int32_t, x, y, w, ht C.int64_t, outW, outH C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	if w <= 0 || ht <= 0 || outW <= 0 || outH <= 0 {
		return fail(errArg, "opentile: ot_read_region_scaled: sizes must be positive")
	}
	if st := checkImage(img); st != statusOK {
		return st
	}
	d, st := parseDecodeOpts(opts)
	if st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	p := e.slide.Pyramid(int(pyramid))
	if p == nil {
		return fail(errRange, "opentile: pyramid index out of range")
	}
	if img.data == nil {
		return query(img, int(outW), int(outH), d.format)
	}
	src := opentile.Region{Origin: opentile.Point{X: int(x), Y: int(y)}, Size: opentile.Size{W: int(w), H: int(ht)}}
	out, err := p.ReadRegionScaled(src, opentile.Size{W: int(outW), H: int(outH)}, d.options()...)
	if err != nil {
		return failErr(err)
	}
	return emit(e, out, d, img)
}

// ---- rendered & associated images ----

// checkFitBounds validates a thumbnail/macro fit box. As in RenderThumbnail
// and RenderMacro, 0 leaves an axis unconstrained; at least one must be set.
func checkFitBounds(maxW, maxH C.int32_t, fn string) C.int {
	if maxW < 0 || maxH < 0 || (maxW == 0 && maxH == 0) {
		return fail(errArg, "opentile: "+fn+": max_w and max_h must be >= 0, with at least one positive")
	}
	return statusOK
}

// slideImage is the shared body of the slide-scoped pixel calls.
func slideImage(h C.ot_slide, opts *C.ot_decode_opts, img *C.ot_image,
	render func(*opentile.Slide, decodeOpts) (*decoder.Image, error)) C.int {
	if st := checkImage(img); st != statusOK {
		return st
	}
	d, st := parseDecodeOpts(opts)
	if st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	out, err := render(e.slide, d)
	if err != nil {
		return failErr(err)
	}
	return emit(e, out, d, img)
}

//export ot_thumbnail
func ot_thumbnail(h C.ot_slide, maxW, maxH C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	if st := checkFitBounds(maxW, maxH, "ot_thumbnail"); st != statusOK {
		return st
	}
	return slideImage(h, opts, img, func(s *opentile.Slide, d decodeOpts) (*decoder.Image, error) {
		return s.RenderThumbnail(opentile.Size{W: int(maxW), H: int(maxH)}, d.options()...)
	})
}

//export ot_macro
func ot_macro(h C.ot_slide, maxW, maxH C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	if st := checkFitBounds(maxW, maxH, "ot_macro"); st != statusOK {
		return st
	}
	return slideImage(h, opts, img, func(s *opentile.Slide, d decodeOpts) (*decoder.Image, error) {
		return s.RenderMacro(opentile.Size{W: int(maxW), H: int(maxH)}, d.options()...)
	})
}

//export ot_associated_info_get
func ot_associated_info_get(h C.ot_slide, index C.int32_t, out *C.ot_associated_info) C.int {
	if st := checkStruct(unsafe.Pointer(out), unsafe.Sizeof(*out), "ot_associated_info"); st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	a, st := associated(e.slide, index)
	if st != statusOK {
		return st
	}
	sz := a.Size()
	v := C.ot_associated_info{
		width: C.int32_t(sz.W), height: C.int32_t(sz.H),
		compression: C.int32_t(compressionCode(a.Compression())),
	}
	t := string(a.Type())
	if len(t) > len(v._type)-1 {
		t = t[:len(v._type)-1]
	}
	for i := 0; i < len(t); i++ {
		v._type[i] = C.char(t[i])
	}
	writeOut(unsafe.Pointer(out), unsafe.Pointer(&v), unsafe.Sizeof(v))
	return statusOK
}

//export ot_associated_decode
func ot_associated_decode(h C.ot_slide, index C.int32_t, opts *C.ot_decode_opts, img *C.ot_image) C.int {
	if st := checkImage(img); st != statusOK {
		return st
	}
	d, st := parseDecodeOpts(opts)
	if st != statusOK {
		return st
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	a, st := associated(e.slide, index)
	if st != statusOK {
		return st
	}
	if img.data == nil && d.scale == 1 {
		sz := a.Size()
		return query(img, sz.W, sz.H, d.format)
	}
	out, err := a.Decode(decoder.DecodeOptions{Format: d.format, Scale: d.scale})
	if err != nil {
		return failErr(err)
	}
	return emit(e, out, d, img)
}

//export ot_associated_raw
func ot_associated_raw(h C.ot_slide, index C.int32_t, dst *C.uint8_t, capacity C.size_t, outLen *C.size_t) C.int {
	if outLen == nil {
		return fail(errArg, "opentile: out_len is NULL")
	}
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	a, st := associated(e.slide, index)
	if st != statusOK {
		return st
	}
	b, err := a.Bytes()
	if err != nil {
		return failErr(err)
	}
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, b, false)
}

//export ot_associated_tiff_tags_json
func ot_associated_tiff_tags_json(h C.ot_slide, index C.int32_t, dst *C.char, capacity C.size_t, outLen *C.size_t) C.int {
	e, st := acquire(h)
	if st != statusOK {
		return st
	}
	defer e.release()
	a, st := associated(e.slide, index)
	if st != statusOK {
		return st
	}
	tags, ok := a.TIFFTags()
	if !ok {
		return fail(errUnsupported, "opentile: this associated image has no TIFF tags")
	}
	b, err := tiffTagsJSON(tags)
	if err != nil {
		return failErr(err)
	}
	return writeBytes(unsafe.Pointer(dst), capacity, outLen, b, true)
}
