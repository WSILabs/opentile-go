/* opentile.h — libopentile: the C API over opentile-go.
 *
 * Reads tiles, regions and metadata from whole-slide images (SVS, NDPI,
 * Philips-TIFF, OME-TIFF, BIF, Leica-SCN, generic-TIFF, COG-WSI, IFE, SZI,
 * DICOM-WSI, DZI) and decodes them with native codecs.
 *
 * Conventions (docs/superpowers/specs/2026-11-23-capi-design.md):
 *   - Every function returns an int status: OT_OK (0) or a negative OT_ERR_*.
 *     ot_last_error() / ot_last_error_code() give the calling thread's most
 *     recent failure; success does not clear them.
 *   - Slides are ot_slide handles; 0 is never valid. Handles are not reused.
 *   - The library never allocates memory for the caller. Byte/string outputs
 *     take (dst, capacity, out_len): dst == NULL is a size query; a too-small
 *     buffer returns OT_ERR_BUFFER with *out_len set. Strings are UTF-8 and
 *     NUL-terminated (capacity must be >= *out_len + 1). Pixels go through
 *     ot_image (see below).
 *   - Every struct starts with struct_size: set it to sizeof(the struct).
 *   - Enum values are int32_t constants, frozen once released.
 *   - All functions are safe to call concurrently from any thread.
 *     ot_close blocks until calls in flight on that handle have returned.
 *   - Do not unload the library (FreeLibrary/dlclose): the Go runtime it
 *     contains cannot be torn down.
 *
 * Inside the library this header is included with OT_NO_PROTOTYPES defined
 * (types only), because cgo generates its own prototypes.
 */
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
#elif defined(__GNUC__)
#  define OT_API __attribute__((visibility("default")))
#else
#  define OT_API
#endif

#ifdef __cplusplus
extern "C" {
#endif

#define OT_API_VERSION_MAJOR 1
#define OT_API_VERSION_MINOR 0

typedef uint64_t ot_slide;

/* ---- status codes ---- */
#define OT_OK                0
#define OT_ERR              -1  /* unclassified failure */
#define OT_ERR_HANDLE       -2  /* unknown or closed handle */
#define OT_ERR_ARG          -3  /* NULL required pointer, bad struct_size/enum/flag/size */
#define OT_ERR_BUFFER       -4  /* destination too small; required size reported */
#define OT_ERR_RANGE        -5  /* pyramid/level/tile/associated index or region out of range */
#define OT_ERR_FORMAT       -6  /* not a readable whole-slide image */
#define OT_ERR_IO           -7  /* file system / read error */
#define OT_ERR_CODEC        -8  /* codec not available in this build */
#define OT_ERR_CORRUPT      -9  /* corrupt tile or codestream */
#define OT_ERR_UNSUPPORTED -10  /* operation not meaningful for this slide/format */
#define OT_ERR_SPARSE      -11  /* no stored tile at this position */

/* ---- enums ---- */
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

#define OT_COMPRESSION_UNKNOWN  0
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

#define OT_OVERLAP_NONE      0  /* Grid tiles Size cleanly */
#define OT_OVERLAP_BORDERED  1  /* tiles carry a redundant border */
#define OT_OVERLAP_STITCHED  2  /* tiles are overlapping frames (BIF); use stitched/region reads */

#define OT_PIXEL_RGB8   0  /* 3 bytes/pixel (default) */
#define OT_PIXEL_RGBA8  1  /* 4 bytes/pixel, alpha = 0xFF */

#define OT_KERNEL_DEFAULT   0
#define OT_KERNEL_NEAREST   1
#define OT_KERNEL_BILINEAR  2
#define OT_KERNEL_LANCZOS   3
#define OT_KERNEL_BOX       4

#define OT_BACKING_DEFAULT  0  /* mmap, falling back to pread if mapping fails */
#define OT_BACKING_MMAP     1  /* mmap only */
#define OT_BACKING_PREAD    2  /* positional reads */

#define OT_CORRUPT_TILE_ERROR  0  /* corrupt tiles fail with OT_ERR_CORRUPT */
#define OT_CORRUPT_TILE_BLANK  1  /* corrupt tiles read as blank */

#define OT_DECODE_SRGB  (1u << 0)  /* convert slide ICC profile -> sRGB when one exists */

/* ---- structs ---- */
typedef struct {
    uint32_t struct_size;
    int32_t  tile_w, tile_h;              /* caller-chosen tile size; 0 = native */
    int64_t  memory_budget;               /* read-path budget in bytes; 0 = default */
    int32_t  backing;                     /* OT_BACKING_* */
    int32_t  corrupt_tile_policy;         /* OT_CORRUPT_TILE_* */
    int32_t  ndpi_no_synthesized_label;   /* nonzero: don't synthesize an NDPI label */
} ot_open_opts;                           /* NULL opts = all defaults */

typedef struct {
    uint32_t struct_size;
    int32_t  format;                      /* OT_FORMAT_* */
    int32_t  pyramid_count;
    int32_t  associated_count;
    double   magnification;               /* 0 = unknown */
    double   mpp_x, mpp_y;                /* microns/pixel at base resolution; 0 = unknown */
    uint64_t icc_size;                    /* bytes; 0 = no embedded profile */
} ot_slide_info;

typedef struct {
    uint32_t struct_size;
    int32_t  level_count;                 /* pyramid name: see ot_metadata_json */
} ot_pyramid_info;

typedef struct {
    uint32_t struct_size;
    int32_t  width, height;               /* level size (stitched content hull for BIF) */
    int32_t  tile_w, tile_h;              /* stored tile size */
    int32_t  grid_w, grid_h;              /* stored tile grid */
    int32_t  stitched_grid_w, stitched_grid_h; /* grid for ot_tile_stitched */
    int32_t  overlap_mode;                /* OT_OVERLAP_* */
    int32_t  tile_overlap_x, tile_overlap_y;
    int32_t  compression;                 /* OT_COMPRESSION_* */
    double   downsample;                  /* relative to level 0 */
    double   mpp_x, mpp_y;                /* 0 = unknown */
    double   focal_plane;
    uint64_t tile_max_bytes;              /* upper bound for ot_tile_raw; 0 = unknown */
} ot_level_info;

typedef struct {
    uint32_t struct_size;
    char     type[32];                    /* "label", "overview", "thumbnail", "map", ... */
    int32_t  width, height;
    int32_t  compression;                 /* OT_COMPRESSION_* */
} ot_associated_info;

typedef struct {
    uint32_t struct_size;
    int32_t  pixel_format;                /* OT_PIXEL_* */
    uint32_t flags;                       /* OT_DECODE_* */
    int32_t  scale;                       /* codec-domain downscale: 0/1 = none, 2, 4, 8 */
    int32_t  kernel;                      /* OT_KERNEL_*: resampling for scaled reads */
} ot_decode_opts;                         /* NULL opts = all defaults */

/* Pixel output. The caller sets struct_size, data, capacity and stride;
 * the library sets width, height, stride, pixel_format and color_managed
 * (also on OT_ERR_BUFFER and on a data == NULL size query).
 * Required bytes: stride * (height - 1) + width * bytes_per_pixel. */
typedef struct {
    uint32_t struct_size;
    uint8_t* data;                        /* in: destination, NULL = size query */
    size_t   capacity;                    /* in: bytes available at data */
    int32_t  stride;                      /* in: row pitch, 0 = tight; out: pitch used */
    int32_t  width, height;               /* out */
    int32_t  pixel_format;                /* out: OT_PIXEL_* */
    int32_t  color_managed;               /* out: 1 if ICC -> sRGB was applied */
} ot_image;

#ifndef OT_NO_PROTOTYPES

/* ---- library ---- */
OT_API int ot_api_version(int32_t* major, int32_t* minor);
OT_API int ot_library_version(char* dst, size_t capacity, size_t* out_len);
OT_API int ot_codec_available(int32_t compression, int32_t* out_available);
OT_API int ot_color_management_available(int32_t* out_available);

/* ---- errors (per calling thread) ---- */
OT_API int ot_last_error_code(void);
OT_API int ot_last_error(char* dst, size_t capacity, size_t* out_len);

/* ---- lifecycle ---- */
OT_API int ot_open(const char* path_utf8, const ot_open_opts* opts, ot_slide* out);
OT_API int ot_close(ot_slide s);

/* ---- metadata ---- */
OT_API int ot_slide_info_get(ot_slide s, ot_slide_info* out);
OT_API int ot_pyramid_info_get(ot_slide s, int32_t pyramid, ot_pyramid_info* out);
OT_API int ot_level_info_get(ot_slide s, int32_t pyramid, int32_t level, ot_level_info* out);
OT_API int ot_metadata_json(ot_slide s, char* dst, size_t capacity, size_t* out_len);
OT_API int ot_icc_profile(ot_slide s, uint8_t* dst, size_t capacity, size_t* out_len);
/* OT_ERR_UNSUPPORTED for non-TIFF formats. */
OT_API int ot_tiff_tags_json(ot_slide s, int32_t pyramid, int32_t level,
                             char* dst, size_t capacity, size_t* out_len);

/* ---- tiles & regions ---- */
/* Stored (compressed) tile bytes. */
OT_API int ot_tile_raw(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                       uint8_t* dst, size_t capacity, size_t* out_len);
/* Decoded stored tile at full tile size (edge tiles are not cropped). */
OT_API int ot_tile_decoded(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                           const ot_decode_opts* opts, ot_image* img);
/* Display tile (x, y) on the level's stitched grid; composites overlapping frames. */
OT_API int ot_tile_stitched(ot_slide s, int32_t pyramid, int32_t level, int32_t x, int32_t y,
                            const ot_decode_opts* opts, ot_image* img);
/* Region in the level's own pixel coordinates; out-of-bounds pixels are white. */
OT_API int ot_read_region(ot_slide s, int32_t pyramid, int32_t level,
                          int64_t x, int64_t y, int32_t w, int32_t h,
                          const ot_decode_opts* opts, ot_image* img);
/* Region in level-0 coordinates, resampled to out_w x out_h. */
OT_API int ot_read_region_scaled(ot_slide s, int32_t pyramid,
                                 int64_t x, int64_t y, int64_t w, int64_t h,
                                 int32_t out_w, int32_t out_h,
                                 const ot_decode_opts* opts, ot_image* img);

/* ---- rendered & associated images ---- */
OT_API int ot_thumbnail(ot_slide s, int32_t max_w, int32_t max_h,
                        const ot_decode_opts* opts, ot_image* img);
OT_API int ot_macro(ot_slide s, int32_t max_w, int32_t max_h,
                    const ot_decode_opts* opts, ot_image* img);
OT_API int ot_associated_info_get(ot_slide s, int32_t index, ot_associated_info* out);
OT_API int ot_associated_decode(ot_slide s, int32_t index,
                                const ot_decode_opts* opts, ot_image* img);
OT_API int ot_associated_raw(ot_slide s, int32_t index,
                             uint8_t* dst, size_t capacity, size_t* out_len);
OT_API int ot_associated_tiff_tags_json(ot_slide s, int32_t index,
                                        char* dst, size_t capacity, size_t* out_len);

#endif /* OT_NO_PROTOTYPES */

#ifdef __cplusplus
}
#endif

#endif /* OPENTILE_H */
