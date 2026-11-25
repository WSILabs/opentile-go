# opentile-go C API (`capi/`, `libopentile`) — design

**Issue:** #120 — one C API for all non-Go consumers; Python binding built on it.
**Status:** decisions settled (§10); step 2 implemented in `capi/` (branch `feat/capi`).
**Date:** 2026-11-23

## 1. Goal

One C ABI over opentile-go that replaces both existing shims:

| | `python/cshim` (this repo) | downstream `libopentile` (`openscope-native`, vendored in `openscope-qt`) |
|---|---|---|
| Handle | `cgo.Handle` as `uintptr_t` | `int64_t` from a handle table |
| Errors | `char** err_out`, Go-malloc'd, freed with `ot_free` | process-global `ot_last_error` + negative codes |
| Buffers | Go `C.malloc`s every result, caller `ot_free`s | caller-supplied |
| Metadata | one JSON blob | `ot_slide_info_t` / `ot_level_info_t` structs |
| Surface | raw tile, decoded tile, `read_region`, thumbnail, macro, associated by name, TIFF tags (JSON) | decoded RGBA tile, ICC profile bytes, lcms2 slide→sRGB transform |
| Pyramids | pyramid 0 only | pyramid 0 only |
| Windows | wheel job failing | working self-contained DLL + `.def` → MSVC `.lib` |

The merged API is a **superset** of both and follows six rules (from #120): caller-supplied buffers
with size queries, structs for fixed metadata, JSON for open-ended data, a thread-local
last error, ICC with an optional lcms2 transform, and an API/ABI version function.

### Non-goals (v1 of the C API)

- `ScaledStrips` / `StripIterator` (DZI-conversion primitive). Needs an iterator
  object with its own lifecycle and worker pool; the consumers that want it
  (wsitools) are Go. Can be added later as `ot_strips_*` without breaking v1.
- `Validate()`, `TIFFDirectories()` completeness view, `TileReader`/`Tiles`
  iteration, splice/bandwidth-dedup API (`TilePrefix`/`TileBodyInto`),
  `AssociatedImage.Encoding()`. These are Go-ecosystem features with no current
  non-Go consumer.
- `io.ReaderAt`-backed open (C callbacks for reads). Path-only for v1.
- An OpenSlide-compatible C API. A shim on top of this one could provide it later.

## 2. Settled decisions

All settled with the owner on 2026-11-23 (see §10).

1. **Handle = `ot_slide` (`uint64_t`), 0 is never valid.** Backed by a handle
   table (like downstream), not raw `cgo.Handle`: a stale or double-closed handle
   returns `OT_ERR_HANDLE` instead of panicking inside Go. Handles are never reused
   within a process; the counter is monotonic.
2. **Every function returns `int` status** (`OT_OK` = 0, negatives = errors).
   Outputs go through pointer parameters. There are no exceptions to this rule:
   version and error accessors return their value through out-params too, so
   ctypes/cffi/P-Invoke bindings follow one pattern.
3. **No Go-allocated memory crosses the boundary**, so there is no `ot_free`.
   Variable-length outputs use the **size-query pattern** (§5). Every
   variable-length result in v1 has either a cheap exact size or a cheap upper
   bound (`TileMaxSize`, deterministic JSON), so no function ever has to
   allocate. If a future function truly can't bound its output, it adds
   `ot_free` then, scoped to that function.
4. **Thread-local last error, set in C.** A cgo `//export` call runs on the
   calling OS thread for its whole duration, so a `_Thread_local` buffer in a C
   file (written through a tiny C helper from Go) is genuinely per-caller-thread.
   On success the error is *not* cleared, matching errno semantics.
   `ot_last_error_code()` returns the code alongside the message.
5. **Explicit `(pyramid, level)` addressing** on every level-scoped call.
   Multi-pyramid slides (OME-TIFF, Leica SCN) are first-class. Both current
   shims silently expose only pyramid 0, so a "pyramid 0 shortcut" overload is
   deliberately *not* provided. Callers pass `0`.
6. **Color management is per call, not per handle.** `OT_DECODE_SRGB` in
   `ot_decode_opts.flags` asks for slide-ICC→sRGB. The lcms2 transform is created
   lazily once per slide (`sync.Once`) with `cmsFLAGS_NOCACHE`, so concurrent
   decodes share it, as the downstream `icc.c` does today. Per-call avoids
   the downstream design's mutable per-handle mode (a toggle racing in-flight
   decodes). `ot_image.color_managed` reports whether it was actually applied:
   it is 0 when the slide has no profile, lcms2 can't link the profile (the
   Iris `cervix_2x_jpeg` sample's embedded profile is one), or the build has no
   lcms2. None of those is an error.
7. **Versioned, extensible structs.** Every struct passed *in* or filled *out*
   starts with `uint32_t struct_size`, set by the caller to `sizeof`. The library
   reads and writes only the prefix it knows and fills the rest of a newer
   caller's struct with zeros. New fields are appended in minor versions and
   never reordered.
8. **Enums are `int32_t` constants with explicit values**, never C `enum`
   types in structs (enum width is implementation-defined across MSVC/GCC).
   Values are frozen once released. Each enum has an `_UNKNOWN = 0`.
9. **Strings are UTF-8, NUL-terminated on output.** `ot_open` takes a UTF-8 path
   on every platform, Windows included; Go converts it internally.
10. **Hand-written `opentile.h`** (Go's generated header uses `_Complex`, which
    MSVC rejects, as downstream found). A CI test parses the header and the
    `//export` set and fails if they diverge. The `.def` is generated from the
    same list.

## 3. Module & build layout

```
capi/
├── go.mod              module github.com/wsilabs/opentile-go/capi
│                       requires opentile-go; `replace ../` in-repo, so capi always
│                       builds against the working tree. lcms2/cgo deps stay out of
│                       the Go library's module graph.
├── opentile.h          the public header (this doc §6)
├── libopentile.def     generated; MSVC import lib via `lib /def:`
├── capi.go             //export functions, handle table
├── errors.c / .h       thread-local last error
├── icc.c / .h          lcms2 transform (ported from openscope-native, build tag !nolcms2)
├── icc_nolcms.go       stub: color_managed always 0
├── scripts/build-release.sh  release build for every platform (port of build-dll.sh); build/ is gitignored output
├── tools/pkgconfig-static/   pkg-config --static wrapper for cgo
└── test/               C smoke test (C99, compiled by MSVC in CI on Windows)
```

Codecs: the same set as the Go library, each disableable with the existing
`no<codec>` tags. `ot_codec_available()` reports what a given binary contains.
The release libraries are self-contained: codecs and lcms2 are linked statically,
and the Windows DLL depends only on system DLLs (the downstream recipe).

## 4. Errors

| Code | Value | Mapped from |
|---|---|---|
| `OT_OK` | 0 | |
| `OT_ERR` | -1 | anything unmapped |
| `OT_ERR_HANDLE` | -2 | unknown / closed handle |
| `OT_ERR_ARG` | -3 | NULL required pointer, bad `struct_size`, bad enum value, non-positive size |
| `OT_ERR_BUFFER` | -4 | destination too small (required size is still reported, §5) |
| `OT_ERR_RANGE` | -5 | `ErrLevelOutOfRange`, `ErrImageIndexOutOfRange`, `ErrTileOutOfBounds`, bad pyramid/associated index, `ErrRegionEmpty` |
| `OT_ERR_FORMAT` | -6 | `ErrUnsupportedFormat`, `ErrInvalidTIFF` (file not readable as a WSI) |
| `OT_ERR_IO` | -7 | `*fs.PathError` / OS errors at open or read |
| `OT_ERR_CODEC` | -8 | `ErrCodecUnavailable`, `ErrCodecNotRegistered`, `ErrUnsupportedCompression`, `ErrCGORequired` |
| `OT_ERR_CORRUPT` | -9 | `ErrCorruptTile`, `decoder.ErrCorruptInput`, `ErrBadJPEGBitstream` |
| `OT_ERR_UNSUPPORTED` | -10 | operation not meaningful here: `ErrDimensionUnavailable`, `ErrUnsupportedScale`, `ErrTileSizeRequired`, no TIFF tags for a non-TIFF format |
| `OT_ERR_SPARSE` | -11 | `ErrSparseTile` (no stored bytes at this position; a decode would fill blank) |

Mapping uses `errors.Is`, first match wins in table order. The message is always
the full Go `err.Error()` text.

## 5. Buffer conventions

**Byte/string outputs** (JSON, raw tile, ICC, associated bytes):

```c
int f(..., void* dst, size_t capacity, size_t* out_len);
```

- `out_len` is required. It always receives the exact byte length of the result,
  for strings excluding the NUL.
- `dst == NULL` (any capacity) is a size query: returns `OT_OK` and sets `*out_len`.
- capacity too small: returns `OT_ERR_BUFFER`, sets `*out_len`, and writes nothing.
- strings need `capacity >= *out_len + 1`; the output is NUL-terminated.

A raw-tile size query has to read the tile. Callers that want one call should
size the buffer from `ot_level_info.tile_max_bytes` (`Level.TileMaxSize()`).

**Pixel outputs** use one image descriptor:

```c
typedef struct {
    uint32_t struct_size;
    uint8_t* data;        /* in: destination; NULL = size query               */
    size_t   capacity;    /* in: bytes available at data                      */
    int32_t  stride;      /* in: row pitch in bytes, 0 = tight (width*bpp);
                             out: pitch used                                  */
    int32_t  width;       /* out                                              */
    int32_t  height;      /* out                                              */
    int32_t  pixel_format;/* out: OT_PIXEL_RGB8 / OT_PIXEL_RGBA8              */
    int32_t  color_managed;/* out: 1 if the ICC->sRGB transform was applied   */
} ot_image;
```

- The required size is `stride * (height-1) + width*bpp`. A caller `stride`
  smaller than `width*bpp` is `OT_ERR_ARG`. Caller stride lets apps decode straight
  into e.g. a 4-byte-aligned `QImage` scanline buffer.
- `data == NULL` or too small: `OT_OK` (query) or `OT_ERR_BUFFER`, with
  `width/height/stride/pixel_format` filled either way.
- Size queries skip the decode only where the Go API guarantees the output
  size: `ot_read_region` and `ot_associated_decode` at scale 1, and
  `ot_read_region_scaled`. Every other query decodes and discards. Tile callers
  should size buffers from `ot_level_info` (`tile_w * tile_h * bpp`) and call once.
- Internally the decode goes into a pooled `decoder.Image` and is copied once
  into `data` (the same single copy both shims do today). Decoding straight into
  caller memory via `*Into` is a later optimization. It doesn't change the ABI.

## 6. The header (draft)

```c
/* opentile.h — libopentile C API. */
#ifndef OPENTILE_H
#define OPENTILE_H
#include <stddef.h>
#include <stdint.h>

#if defined(_WIN32) && !defined(OT_STATIC)
#  ifdef OT_BUILDING
#    define OT_API __declspec(dllexport)
#  else
#    define OT_API __declspec(dllimport)
#  endif
#else
#  define OT_API __attribute__((visibility("default")))
#endif

#ifdef __cplusplus
extern "C" {
#endif

#define OT_API_VERSION_MAJOR 1
#define OT_API_VERSION_MINOR 0

typedef uint64_t ot_slide;

/* ---- status codes (§4) ---- */
#define OT_OK               0
#define OT_ERR             -1
#define OT_ERR_HANDLE      -2
#define OT_ERR_ARG         -3
#define OT_ERR_BUFFER      -4
#define OT_ERR_RANGE       -5
#define OT_ERR_FORMAT      -6
#define OT_ERR_IO          -7
#define OT_ERR_CODEC       -8
#define OT_ERR_CORRUPT     -9
#define OT_ERR_UNSUPPORTED -10
#define OT_ERR_SPARSE      -11

/* ---- enums (frozen values) ---- */
#define OT_FORMAT_UNKNOWN       0
#define OT_FORMAT_SVS           1
#define OT_FORMAT_NDPI          2
#define OT_FORMAT_PHILIPS_TIFF  3
#define OT_FORMAT_OME_TIFF      4
#define OT_FORMAT_BIF           5
#define OT_FORMAT_IFE           6
#define OT_FORMAT_GENERIC_TIFF  7
#define OT_FORMAT_LEICA_SCN     8
#define OT_FORMAT_SZI           9
#define OT_FORMAT_COG_WSI      10
#define OT_FORMAT_DICOM        11
#define OT_FORMAT_DZI          12

#define OT_COMPRESSION_UNKNOWN  0   /* values mirror opentile.Compression */
#define OT_COMPRESSION_NONE     1
#define OT_COMPRESSION_JPEG     2
#define OT_COMPRESSION_JP2K     3
#define OT_COMPRESSION_LZW      4
#define OT_COMPRESSION_AVIF     5
#define OT_COMPRESSION_IRIS     6
#define OT_COMPRESSION_DEFLATE  7
#define OT_COMPRESSION_WEBP     8
#define OT_COMPRESSION_JPEGXL   9
#define OT_COMPRESSION_HTJ2K   10
#define OT_COMPRESSION_PNG     11

#define OT_OVERLAP_NONE      0
#define OT_OVERLAP_BORDERED  1
#define OT_OVERLAP_STITCHED  2

#define OT_PIXEL_RGB8   0      /* default */
#define OT_PIXEL_RGBA8  1      /* alpha = 0xFF */

#define OT_KERNEL_DEFAULT  0   /* library default for the call */
#define OT_KERNEL_NEAREST  1
#define OT_KERNEL_BILINEAR 2
#define OT_KERNEL_LANCZOS  3
#define OT_KERNEL_BOX      4

#define OT_BACKING_DEFAULT 0   /* mmap, falls back to pread */
#define OT_BACKING_MMAP    1
#define OT_BACKING_PREAD   2

#define OT_CORRUPT_TILE_ERROR 0
#define OT_CORRUPT_TILE_BLANK 1

#define OT_DECODE_SRGB  (1u << 0)  /* apply slide ICC -> sRGB if available */

/* ---- structs ---- */
typedef struct {
    uint32_t struct_size;
    int32_t  tile_w, tile_h;        /* WithTileSize; 0 = native */
    int64_t  memory_budget;         /* WithMemoryBudget bytes; 0 = default */
    int32_t  backing;               /* OT_BACKING_* */
    int32_t  corrupt_tile_policy;   /* OT_CORRUPT_TILE_* */
    int32_t  ndpi_no_synthesized_label; /* nonzero = WithNDPISynthesizedLabel(false) */
} ot_open_opts;

typedef struct {
    uint32_t struct_size;
    int32_t  format;                /* OT_FORMAT_* */
    int32_t  pyramid_count;
    int32_t  associated_count;
    double   magnification;         /* 0 = unknown */
    double   mpp_x, mpp_y;          /* microns/pixel at pyramid 0 level 0; 0 = unknown */
    uint64_t icc_size;              /* 0 = no embedded profile */
} ot_slide_info;

typedef struct {
    uint32_t struct_size;
    int32_t  level_count;
    /* name is in ot_metadata_json (open-ended string) */
} ot_pyramid_info;

typedef struct {
    uint32_t struct_size;
    int32_t  width, height;         /* Level.Size (stitched content hull for BIF) */
    int32_t  tile_w, tile_h;        /* Level.TileSize */
    int32_t  grid_w, grid_h;        /* Level.Grid (raw stored-tile grid) */
    int32_t  stitched_grid_w, stitched_grid_h; /* Level.StitchedGrid() */
    int32_t  overlap_mode;          /* OT_OVERLAP_*; != NONE => Grid doesn't tile Size */
    int32_t  tile_overlap_x, tile_overlap_y;
    int32_t  compression;           /* OT_COMPRESSION_* */
    double   downsample;
    double   mpp_x, mpp_y;
    double   focal_plane;
    uint64_t tile_max_bytes;        /* Level.TileMaxSize(): upper bound for ot_tile_raw */
} ot_level_info;

typedef struct {
    uint32_t struct_size;
    char     type[32];              /* "label", "overview", "thumbnail", "map", ... */
    int32_t  width, height;
    int32_t  compression;           /* OT_COMPRESSION_* */
} ot_associated_info;

typedef struct {
    uint32_t struct_size;
    int32_t  pixel_format;          /* OT_PIXEL_* */
    uint32_t flags;                 /* OT_DECODE_* */
    int32_t  scale;                 /* WithScale: 0/1 = full, 2/4/8 = codec-domain downscale */
    int32_t  kernel;                /* OT_KERNEL_* (scaled region / thumbnail resample) */
} ot_decode_opts;                   /* NULL opts = all defaults */

typedef struct {                    /* see §5 */
    uint32_t struct_size;
    uint8_t* data;
    size_t   capacity;
    int32_t  stride;
    int32_t  width, height;
    int32_t  pixel_format;
    int32_t  color_managed;
} ot_image;

/* ---- library ---- */
OT_API int ot_api_version(int32_t* major, int32_t* minor);
OT_API int ot_library_version(char* dst, size_t capacity, size_t* out_len); /* opentile-go module version */
OT_API int ot_codec_available(int32_t compression, int32_t* out_available);
OT_API int ot_color_management_available(int32_t* out_available);          /* built with lcms2? */

/* ---- errors (thread-local) ---- */
OT_API int ot_last_error_code(void);
OT_API int ot_last_error(char* dst, size_t capacity, size_t* out_len);

/* ---- lifecycle ---- */
OT_API int ot_open(const char* path_utf8, const ot_open_opts* opts /*nullable*/, ot_slide* out);
OT_API int ot_close(ot_slide s);

/* ---- metadata ---- */
OT_API int ot_slide_info_get(ot_slide s, ot_slide_info* out);
OT_API int ot_pyramid_info_get(ot_slide s, int32_t pyramid, ot_pyramid_info* out);
OT_API int ot_level_info_get(ot_slide s, int32_t pyramid, int32_t level, ot_level_info* out);
OT_API int ot_metadata_json(ot_slide s, char* dst, size_t capacity, size_t* out_len);
OT_API int ot_icc_profile(ot_slide s, uint8_t* dst, size_t capacity, size_t* out_len);
OT_API int ot_tiff_tags_json(ot_slide s, int32_t pyramid, int32_t level,
                             char* dst, size_t capacity, size_t* out_len);

/* ---- tiles & regions ---- */
OT_API int ot_tile_raw(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                       uint8_t* dst, size_t capacity, size_t* out_len);
OT_API int ot_tile_decoded(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                           const ot_decode_opts* opts, ot_image* img);
OT_API int ot_tile_stitched(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                            const ot_decode_opts* opts, ot_image* img);  /* display tile on StitchedGrid */
OT_API int ot_read_region(ot_slide s, int32_t pyramid, int32_t level,
                          int64_t x, int64_t y, int32_t w, int32_t h,
                          const ot_decode_opts* opts, ot_image* img);
OT_API int ot_read_region_scaled(ot_slide s, int32_t pyramid,
                                 int64_t x, int64_t y, int64_t w, int64_t h, /* level-0 coords */
                                 int32_t out_w, int32_t out_h,
                                 const ot_decode_opts* opts, ot_image* img);

/* ---- rendered & associated images ---- */
OT_API int ot_thumbnail(ot_slide s, int32_t max_w, int32_t max_h,
                        const ot_decode_opts* opts, ot_image* img);
OT_API int ot_macro(ot_slide s, int32_t max_w, int32_t max_h,
                    const ot_decode_opts* opts, ot_image* img);
OT_API int ot_associated_info_get(ot_slide s, int32_t index, ot_associated_info* out);
OT_API int ot_associated_decode(ot_slide s, int32_t index, const ot_decode_opts* opts, ot_image* img);
OT_API int ot_associated_raw(ot_slide s, int32_t index, uint8_t* dst, size_t capacity, size_t* out_len);
OT_API int ot_associated_tiff_tags_json(ot_slide s, int32_t index,
                                        char* dst, size_t capacity, size_t* out_len);

#ifdef __cplusplus
}
#endif
#endif /* OPENTILE_H */
```

Notes on specific calls:

- **Region coordinates are `int64_t`.** Level-0 coordinates of large slides can
  exceed 2³¹ only in pathological cases, but `int32` would make that a silent
  truncation. Output dimensions stay `int32_t` because they are buffer-bounded.
- **`ot_tile_decoded` returns the stored tile at full tile size.** Edge tiles are
  not cropped; the existing Go `DecodedTile` behaves this way and the downstream
  header documents it. For overlapping levels (BIF), viewers should use
  `ot_tile_stitched` on `stitched_grid` or `ot_read_region`.
- **`ot_tiff_tags_json`** returns `OT_ERR_UNSUPPORTED` for non-TIFF formats. Today
  the Python shim signals this with a null pointer and status 0. Each JSON entry is
  `{number, name, type, count, ascii?, uints?, rationals?, raw_b64}`, a superset of
  the current schema. `raw_b64` keeps full fidelity for types without a typed getter.
- **`ot_associated_*` is index-based**, `0..associated_count-1`, with `type` in
  the info struct. Several images can share a type (generic-TIFF `"associated"`),
  so addressing by name, as the Python shim does, is ambiguous. Python keeps
  its by-name lookup as a convenience on top.

## 7. `ot_metadata_json` schema

A superset of the current Python schema. Existing keys keep their meaning, so
`opentile_go.Slide.properties` and friends don't change:

```json
{
  "api_version": [1, 0],
  "format": "svs",
  "mpp": [0.499, 0.499],            // null if unknown
  "magnification": 20.0,             // null if unknown
  "scanner": {"manufacturer": "...", "model": "...", "software": ["..."], "serial": "..."},
  "writer": "...",
  "acquisition_datetime": "2009-12-29T09:59:15Z",   // RFC 3339, null if unknown
  "image_description": "...",
  "properties": {"aperio.AppMag": "20", "...": "..."},
  "levels": [ /* pyramid 0, unchanged per-level objects: size, tile_size, grid, downsample, overlapping */ ],
  "pyramids": [ {"index": 0, "name": "", "levels": [ /* same objects + mpp, compression, overlap_mode */ ]} ],
  "associated": ["label", "macro"],                     // unchanged: type names, index order
  "associated_images": [ {"index": 0, "type": "label", "size": [w, h], "compression": "lzw"} ]
}
```

The schema follows the same struct-size rule: keys are added in minor versions
and never removed or retyped.

## 8. Concurrency & lifecycle

- Every function is safe to call concurrently from any thread, on the same handle
  or different ones. This holds today for the Go library; the handle table adds
  only a `sync.RWMutex` lookup.
- **`ot_close` with calls in flight.** Each handle entry carries an in-flight
  counter (`sync.WaitGroup`). Calls acquire on entry and release on return.
  `ot_close` removes the handle from the table immediately, so new calls get
  `OT_ERR_HANDLE`. It then **blocks** until in-flight calls return and closes
  the `Slide` and its lcms2 transforms, so the file is released when `ot_close`
  returns and the close error can be reported. Neither shim handles this today.
  Without it a viewer closing a slide while tile threads drain would race
  `Slide.Close`. `capi/test/threads.c` exercises it.
- Leaked (never-closed) handles stay alive until process exit. The library
  holds no global background goroutines beyond what the Go runtime and the
  decoder pools already hold.
- The Go runtime is initialized on first call (c-shared) and never torn down.
  The DLL must not be unloaded with `FreeLibrary`; Go doesn't support that. Document this.

## 9. Migration map

| Old (`python/cshim`) | Old (downstream) | New |
|---|---|---|
| `ot_open(path, err**)` → handle | `ot_open(path, int64*)` | `ot_open(path, opts, ot_slide*)` |
| `ot_close(h)` | `ot_close(h)` | `ot_close(s)` |
| `ot_free` | — | removed |
| err out-param | `ot_last_error(buf,size)` → len | `ot_last_error(dst,cap,&len)` + `ot_last_error_code()` |
| `ot_metadata_json` (Go-malloc) | — | `ot_metadata_json` (size-query) |
| — | `ot_slide_info` | `ot_slide_info_get` (+format, pyramid/associated counts) |
| — | `ot_level_info` | `ot_level_info_get` (+pyramid, downsample, mpp, compression, …) |
| — | `ot_icc_profile(h,dst,size)` | `ot_icc_profile(s,dst,cap,&len)` |
| `ot_tile` | — | `ot_tile_raw` |
| `ot_decoded_tile(rgba)` | `ot_decode_tile_rgba` | `ot_tile_decoded` + `ot_decode_opts.pixel_format` |
| — | `ot_set_color_management(h,on)` | `OT_DECODE_SRGB` per call |
| `ot_read_region` | — | `ot_read_region` (+pyramid, int64 origin) |
| `ot_thumbnail` / `ot_macro` | — | same names, + opts |
| `ot_associated(name)` | — | `ot_associated_info_get` + `ot_associated_decode` (index) |
| `ot_tiff_tags_json(level)` | — | same name, + pyramid, size-query, richer entries |

Names are reused where the semantics carry over. Each consumer migrates as a
single change, so no compatibility aliases are planned.

## 10. Settled questions (2026-11-23)

1. **Handle:** `uint64_t` table handle, not an opaque pointer. A pointer would
   be fake, since Go can't safely hand out real ones, and the table catches
   stale or double-closed handles.
2. **Color management:** per call (`OT_DECODE_SRGB`), not per handle.
3. **Pyramid:** an explicit argument on every level-scoped call.
4. **Scope:** `ScaledStrips` deferred; `ot_read_region_scaled` and
   `ot_tile_stitched` are in v1.
5. **lcms2:** always linked into release builds, wheels included; a `nolcms2`
   build tag exists for source builds.

## 11. Next steps (issue #120 steps 2–6)

2. Scaffold `capi/` (go.mod, header, handle table, errors, version); port the
   cshim calls and downstream `icc.c`; header↔export sync test.
3. Per-platform build scripts (Windows: downstream recipe → DLL + `.def` + `.lib`);
   release-asset job.
4. Re-point `python/opentile_go/_ffi.py` at libopentile; delete `python/cshim`;
   wheels bundle the library; fix the Windows wheel.
5. C smoke test (MSVC on Windows, cc elsewhere) + existing Python parity tests on
   all platforms.
6. openscope-native / openscope-qt switch to the release artifact.
