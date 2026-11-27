"""ctypes binding to libopentile, the opentile-go C API (capi/opentile.h).

Loads the bundled libopentile.<so|dylib|dll>, declares the functions the
facade uses, and marshals buffers to Python bytes / numpy arrays. Pure Python,
so the wheel is Python-version agnostic.

C API conventions this module relies on:
  - every call returns an int status (0 = OK, negative = OT_ERR_*); the
    message is the calling thread's ot_last_error();
  - the library never allocates for the caller: outputs go into buffers we
    provide, and a too-small one fails with OT_ERR_BUFFER and the needed size;
  - pixel reads fill an ot_image; structs start with struct_size.
"""

import ctypes
import glob
import json
import os

import numpy as np

from . import OpenTileError

# ---- constants (capi/opentile.h; frozen) ------------------------------------
OT_OK = 0
OT_ERR_BUFFER = -4
OT_ERR_RANGE = -5
OT_ERR_UNSUPPORTED = -10
OT_DECODE_SRGB = 1 << 0
OT_PIXEL_RGB8 = 0
OT_PIXEL_RGBA8 = 1
_API_MAJOR = 1


# ---- structs ------------------------------------------------------------------
class _DecodeOpts(ctypes.Structure):
    _fields_ = [
        ("struct_size", ctypes.c_uint32),
        ("pixel_format", ctypes.c_int32),
        ("flags", ctypes.c_uint32),
        ("scale", ctypes.c_int32),
        ("kernel", ctypes.c_int32),
    ]


class _Image(ctypes.Structure):
    _fields_ = [
        ("struct_size", ctypes.c_uint32),
        ("data", ctypes.c_void_p),
        ("capacity", ctypes.c_size_t),
        ("stride", ctypes.c_int32),
        ("width", ctypes.c_int32),
        ("height", ctypes.c_int32),
        ("pixel_format", ctypes.c_int32),
        ("color_managed", ctypes.c_int32),
    ]


class _LevelInfo(ctypes.Structure):
    _fields_ = [
        ("struct_size", ctypes.c_uint32),
        ("width", ctypes.c_int32),
        ("height", ctypes.c_int32),
        ("tile_w", ctypes.c_int32),
        ("tile_h", ctypes.c_int32),
        ("grid_w", ctypes.c_int32),
        ("grid_h", ctypes.c_int32),
        ("stitched_grid_w", ctypes.c_int32),
        ("stitched_grid_h", ctypes.c_int32),
        ("overlap_mode", ctypes.c_int32),
        ("tile_overlap_x", ctypes.c_int32),
        ("tile_overlap_y", ctypes.c_int32),
        ("compression", ctypes.c_int32),
        ("downsample", ctypes.c_double),
        ("mpp_x", ctypes.c_double),
        ("mpp_y", ctypes.c_double),
        ("focal_plane", ctypes.c_double),
        ("tile_max_bytes", ctypes.c_uint64),
    ]


class _AssociatedInfo(ctypes.Structure):
    _fields_ = [
        ("struct_size", ctypes.c_uint32),
        ("type", ctypes.c_char * 32),
        ("width", ctypes.c_int32),
        ("height", ctypes.c_int32),
        ("compression", ctypes.c_int32),
    ]


# ---- loading --------------------------------------------------------------------
def _load():
    here = os.path.dirname(os.path.abspath(__file__))
    hits = [h for h in sorted(glob.glob(os.path.join(here, "libopentile.*")))
            if os.path.splitext(h)[1].lower() in (".so", ".dylib", ".dll")]
    if not hits:
        raise OpenTileError(
            "opentile_go native library not found; run python/build_dev.sh (dev) "
            "or install a wheel"
        )
    return ctypes.CDLL(hits[0])


_lib = _load()

_i32p = ctypes.POINTER(ctypes.c_int32)
_sizep = ctypes.POINTER(ctypes.c_size_t)
_u64 = ctypes.c_uint64
_i32 = ctypes.c_int32
_i64 = ctypes.c_int64


def _decl(name, *argtypes):
    fn = getattr(_lib, name)
    fn.restype = ctypes.c_int
    fn.argtypes = list(argtypes)
    return fn


_ot_api_version = _decl("ot_api_version", _i32p, _i32p)
_ot_last_error = _decl("ot_last_error", ctypes.c_char_p, ctypes.c_size_t, _sizep)
_ot_open = _decl("ot_open", ctypes.c_char_p, ctypes.c_void_p, ctypes.POINTER(_u64))
_ot_close = _decl("ot_close", _u64)
_ot_metadata_json = _decl("ot_metadata_json", _u64, ctypes.c_char_p, ctypes.c_size_t, _sizep)
_ot_level_info_get = _decl("ot_level_info_get", _u64, _i32, _i32, ctypes.POINTER(_LevelInfo))
_ot_associated_info_get = _decl("ot_associated_info_get", _u64, _i32, ctypes.POINTER(_AssociatedInfo))
_ot_tiff_tags_json = _decl("ot_tiff_tags_json", _u64, _i32, _i32, ctypes.c_char_p, ctypes.c_size_t, _sizep)
_ot_tile_raw = _decl("ot_tile_raw", _u64, _i32, _i32, _i32, _i32, ctypes.c_char_p, ctypes.c_size_t, _sizep)
_img_args = (ctypes.POINTER(_DecodeOpts), ctypes.POINTER(_Image))
_ot_tile_decoded = _decl("ot_tile_decoded", _u64, _i32, _i32, _i32, _i32, *_img_args)
_ot_read_region = _decl("ot_read_region", _u64, _i32, _i32, _i64, _i64, _i32, _i32, *_img_args)
_ot_thumbnail = _decl("ot_thumbnail", _u64, _i32, _i32, *_img_args)
_ot_macro = _decl("ot_macro", _u64, _i32, _i32, *_img_args)
_ot_associated_decode = _decl("ot_associated_decode", _u64, _i32, *_img_args)


def _check_api():
    major, minor = _i32(), _i32()
    _ot_api_version(ctypes.byref(major), ctypes.byref(minor))
    if major.value != _API_MAJOR:
        raise OpenTileError(
            f"opentile_go needs libopentile API {_API_MAJOR}.x, found {major.value}.{minor.value}"
        )


_check_api()


# ---- errors and buffers ---------------------------------------------------------
def _last_error():
    n = ctypes.c_size_t()
    _ot_last_error(None, 0, ctypes.byref(n))
    buf = ctypes.create_string_buffer(n.value + 1)
    _ot_last_error(buf, len(buf), ctypes.byref(n))
    return buf.value.decode("utf-8", "replace")


def _raise(rc, what):
    msg = _last_error() or f"opentile: {what} failed"
    raise OpenTileError(msg)


def _bytes_call(fn, args, guess=0, as_string=False):
    """Call a (dst, capacity, out_len) function; return the output bytes.

    Tries a guessed capacity first, then retries once with the reported size,
    so the common case is one call.
    """
    n = ctypes.c_size_t()
    extra = 1 if as_string else 0
    cap = guess
    if cap <= 0:
        rc = fn(*args, None, 0, ctypes.byref(n))
        if rc != OT_OK:
            _raise(rc, fn.__name__)
        cap = n.value + extra
    for _ in range(2):
        buf = ctypes.create_string_buffer(max(cap, 1))
        rc = fn(*args, buf, cap, ctypes.byref(n))
        if rc == OT_OK:
            return buf.raw[: n.value]
        if rc != OT_ERR_BUFFER:
            _raise(rc, fn.__name__)
        cap = n.value + extra
    _raise(rc, fn.__name__)


def _decode_opts(rgba, srgb=False):
    o = _DecodeOpts()
    o.struct_size = ctypes.sizeof(_DecodeOpts)
    o.pixel_format = OT_PIXEL_RGBA8 if rgba else OT_PIXEL_RGB8
    o.flags = OT_DECODE_SRGB if srgb else 0
    return o


def _image_call(fn, args, rgba, guess=None, srgb=False):
    """Call an ot_image-filling function; return an (H, W, bands) uint8 array.

    guess is an upper bound (w, h) on the output size. With it the pixels
    decode straight into a numpy buffer in one call; otherwise (or if the
    guess is too small) a size query or OT_ERR_BUFFER gives the exact size.
    """
    bands = 4 if rgba else 3
    opts = _decode_opts(rgba, srgb)
    img = _Image()
    img.struct_size = ctypes.sizeof(_Image)
    if guess is None or guess[0] <= 0 or guess[1] <= 0:
        rc = fn(*args, ctypes.byref(opts), ctypes.byref(img))  # data = NULL: size query
        if rc != OT_OK:
            _raise(rc, fn.__name__)
        guess = (img.width, img.height)
    for _ in range(2):
        w, h = guess
        arr = np.empty(max(w * h * bands, 1), dtype=np.uint8)
        img.data = arr.ctypes.data
        img.capacity = arr.nbytes
        img.stride = 0  # tight rows
        rc = fn(*args, ctypes.byref(opts), ctypes.byref(img))
        if rc == OT_OK:
            ow, oh = img.width, img.height
            out = arr[: ow * oh * bands].reshape((oh, ow, bands))
            return out if out.size == arr.size else out.copy()
        if rc != OT_ERR_BUFFER:
            _raise(rc, fn.__name__)
        guess = (img.width, img.height)
        img.stride = 0
    _raise(rc, fn.__name__)


# ---- slides -----------------------------------------------------------------------
def open_slide(path):
    h = _u64()
    rc = _ot_open(os.fspath(path).encode("utf-8"), None, ctypes.byref(h))
    if rc != OT_OK:
        _raise(rc, "open")
    return h.value


def close_slide(handle):
    if handle:
        _ot_close(handle)


def metadata(handle):
    """The slide's metadata document (see capi/json.go; a superset of the
    keys the facade reads: format, mpp, magnification, properties, levels,
    associated)."""
    raw = _bytes_call(_ot_metadata_json, (handle,), as_string=True)
    return json.loads(raw.decode("utf-8"))


def _level_info(handle, level):
    info = _LevelInfo()
    info.struct_size = ctypes.sizeof(_LevelInfo)
    rc = _ot_level_info_get(handle, 0, level, ctypes.byref(info))
    if rc != OT_OK:
        _raise(rc, "level info")
    return info


def tile(handle, level, x, y):
    guess = int(_level_info(handle, level).tile_max_bytes)
    return _bytes_call(_ot_tile_raw, (handle, 0, level, x, y), guess=guess)


def decoded_tile(handle, level, x, y, rgba=False, srgb=False):
    info = _level_info(handle, level)
    return _image_call(_ot_tile_decoded, (handle, 0, level, x, y), rgba,
                       guess=(info.tile_w, info.tile_h), srgb=srgb)


def read_region(handle, level, x, y, w, h, rgba=False, srgb=False):
    return _image_call(_ot_read_region, (handle, 0, level, x, y, w, h), rgba,
                       guess=(w, h), srgb=srgb)


def thumbnail(handle, max_w, max_h, srgb=False):
    # Upper bound on the fitted size, so the decode goes straight into the
    # array. With one axis given, the other follows level 0's aspect ratio
    # (+2 covers rounding); a too-small guess just costs a retry.
    guess = (max_w, max_h)
    if max_w <= 0 or max_h <= 0:
        l0 = _level_info(handle, 0)
        if l0.width > 0 and l0.height > 0:
            if max_w > 0:
                guess = (max_w, max_w * l0.height // l0.width + 2)
            elif max_h > 0:
                guess = (max_h * l0.width // l0.height + 2, max_h)
    return _image_call(_ot_thumbnail, (handle, max_w, max_h), False, guess=guess, srgb=srgb)


def macro(handle, max_w, max_h, srgb=False):
    # The macro canvas has its own aspect ratio; with one axis given, ask.
    guess = (max_w, max_h) if max_w > 0 and max_h > 0 else None
    return _image_call(_ot_macro, (handle, max_w, max_h), False, guess=guess, srgb=srgb)


def associated(handle, name, rgba=False):
    """Decode the first associated image whose type is name."""
    i = 0
    while True:
        info = _AssociatedInfo()
        info.struct_size = ctypes.sizeof(_AssociatedInfo)
        rc = _ot_associated_info_get(handle, i, ctypes.byref(info))
        if rc == OT_ERR_RANGE:
            raise OpenTileError(f"opentile: no associated image named {name}")
        if rc != OT_OK:
            _raise(rc, "associated info")
        if info.type.decode("utf-8", "replace") == name:
            return _image_call(_ot_associated_decode, (handle, i), rgba,
                               guess=(info.width, info.height))
        i += 1


def tiff_tags(handle, level):
    """Raw TIFF tags for a level of the main pyramid, or None for non-TIFF
    formats and levels without tags."""
    n = ctypes.c_size_t()
    rc = _ot_tiff_tags_json(handle, 0, level, None, 0, ctypes.byref(n))
    if rc == OT_ERR_UNSUPPORTED:
        return None
    if rc != OT_OK:
        _raise(rc, "tiff tags")
    raw = _bytes_call(_ot_tiff_tags_json, (handle, 0, level), guess=n.value + 1, as_string=True)
    return json.loads(raw.decode("utf-8")) or None
