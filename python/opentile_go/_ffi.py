"""ctypes binding to the opentile-go c-shared shim. Loads the bundled
_opentilego.<so|dylib|pyd>, declares the C-ABI, and marshals buffers to
Python bytes / numpy arrays. Pure Python — keeps the wheel Python-agnostic."""

import ctypes
import glob
import json
import os

from . import OpenTileError

_c_char_pp = ctypes.POINTER(ctypes.c_char_p)


def _load():
    here = os.path.dirname(os.path.abspath(__file__))
    hits = sorted(glob.glob(os.path.join(here, "_opentilego.*")))
    hits = [h for h in hits if not h.endswith(".h")]
    if not hits:
        raise OpenTileError(
            "opentile_go native library not built; run python/build_dev.sh (dev) "
            "or install a wheel"
        )
    return ctypes.CDLL(hits[0])


_lib = _load()

_lib.ot_open.restype = ctypes.c_size_t
_lib.ot_open.argtypes = [ctypes.c_char_p, _c_char_pp]
_lib.ot_close.argtypes = [ctypes.c_size_t]
_lib.ot_free.argtypes = [ctypes.c_void_p]
_lib.ot_metadata_json.restype = ctypes.c_void_p
_lib.ot_metadata_json.argtypes = [ctypes.c_size_t, _c_char_pp]


def _take_err(err):
    """Consume an err_out char* (free + return its text) or return None."""
    if err.value is None:
        return None
    msg = err.value.decode("utf-8", "replace")
    _lib.ot_free(ctypes.cast(err, ctypes.c_void_p))
    return msg


def _take_cstr(ptr):
    """Copy a malloc'd char* into a Python str and free it."""
    s = ctypes.cast(ptr, ctypes.c_char_p).value.decode("utf-8", "replace")
    _lib.ot_free(ptr)
    return s


def open_slide(path):
    err = ctypes.c_char_p()
    h = _lib.ot_open(os.fspath(path).encode("utf-8"), ctypes.byref(err))
    if h == 0:
        raise OpenTileError(_take_err(err) or "opentile: open failed")
    return h


def close_slide(handle):
    if handle:
        _lib.ot_close(handle)


def metadata(handle):
    err = ctypes.c_char_p()
    ptr = _lib.ot_metadata_json(handle, ctypes.byref(err))
    if not ptr:
        raise OpenTileError(_take_err(err) or "opentile: metadata failed")
    return json.loads(_take_cstr(ptr))


import numpy as np

_u8pp = ctypes.POINTER(ctypes.POINTER(ctypes.c_uint8))
_sizep = ctypes.POINTER(ctypes.c_size_t)
_intp = ctypes.POINTER(ctypes.c_int)

_lib.ot_tile.restype = ctypes.c_int
_lib.ot_tile.argtypes = [ctypes.c_size_t, ctypes.c_int, ctypes.c_int, ctypes.c_int, _u8pp, _sizep, _c_char_pp]

_lib.ot_decoded_tile.restype = ctypes.c_int
_lib.ot_decoded_tile.argtypes = [ctypes.c_size_t] + [ctypes.c_int] * 4 + [_u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_read_region.restype = ctypes.c_int
_lib.ot_read_region.argtypes = [ctypes.c_size_t] + [ctypes.c_int] * 6 + [_u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]


def _take_buf(ptr, n):
    """Copy an ot_free-owned uint8* buffer of length n into Python bytes, free it."""
    out = ctypes.string_at(ptr, n)
    _lib.ot_free(ctypes.cast(ptr, ctypes.c_void_p))
    return out


def _take_image(ptr, n, w, h, bands):
    """Copy a tight (h,w,bands) uint8 buffer into an owned numpy array, free it."""
    buf = ctypes.string_at(ptr, n)  # a copy
    _lib.ot_free(ctypes.cast(ptr, ctypes.c_void_p))
    return np.frombuffer(buf, dtype=np.uint8).reshape((h, w, bands)).copy()


def tile(handle, level, x, y):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n = ctypes.c_size_t()
    err = ctypes.c_char_p()
    if _lib.ot_tile(handle, level, x, y, ctypes.byref(out), ctypes.byref(n), ctypes.byref(err)) != 0:
        raise OpenTileError(_take_err(err) or "opentile: tile failed")
    return _take_buf(out, n.value)


def _decode_call(fn, handle, ints):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n, w, h, bands = ctypes.c_size_t(), ctypes.c_int(), ctypes.c_int(), ctypes.c_int()
    err = ctypes.c_char_p()
    args = [handle, *ints, ctypes.byref(out), ctypes.byref(n),
            ctypes.byref(w), ctypes.byref(h), ctypes.byref(bands), ctypes.byref(err)]
    if fn(*args) != 0:
        raise OpenTileError(_take_err(err) or "opentile: decode failed")
    return _take_image(out, n.value, w.value, h.value, bands.value)


def decoded_tile(handle, level, x, y, rgba=False):
    return _decode_call(_lib.ot_decoded_tile, handle, [level, x, y, 1 if rgba else 0])


def read_region(handle, level, x, y, w, h, rgba=False):
    return _decode_call(_lib.ot_read_region, handle, [level, x, y, w, h, 1 if rgba else 0])


_lib.ot_thumbnail.restype = ctypes.c_int
_lib.ot_thumbnail.argtypes = [ctypes.c_size_t, ctypes.c_int, ctypes.c_int, _u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_macro.restype = ctypes.c_int
_lib.ot_macro.argtypes = _lib.ot_thumbnail.argtypes
_lib.ot_associated.restype = ctypes.c_int
_lib.ot_associated.argtypes = [ctypes.c_size_t, ctypes.c_char_p, ctypes.c_int, _u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_tiff_tags_json.restype = ctypes.c_int
_lib.ot_tiff_tags_json.argtypes = [ctypes.c_size_t, ctypes.c_int, _c_char_pp, _c_char_pp]


def thumbnail(handle, max_w, max_h):
    return _decode_call(_lib.ot_thumbnail, handle, [max_w, max_h])


def macro(handle, max_w, max_h):
    return _decode_call(_lib.ot_macro, handle, [max_w, max_h])


def associated(handle, name, rgba=False):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n, w, h, bands = ctypes.c_size_t(), ctypes.c_int(), ctypes.c_int(), ctypes.c_int()
    err = ctypes.c_char_p()
    rc = _lib.ot_associated(handle, name.encode("utf-8"), 1 if rgba else 0,
                            ctypes.byref(out), ctypes.byref(n), ctypes.byref(w),
                            ctypes.byref(h), ctypes.byref(bands), ctypes.byref(err))
    if rc != 0:
        raise OpenTileError(_take_err(err) or "opentile: associated failed")
    return _take_image(out, n.value, w.value, h.value, bands.value)


def tiff_tags(handle, level):
    out = ctypes.c_char_p()
    err = ctypes.c_char_p()
    if _lib.ot_tiff_tags_json(handle, level, ctypes.byref(out), ctypes.byref(err)) != 0:
        raise OpenTileError(_take_err(err) or "opentile: tiff tags failed")
    if not out.value:
        return None
    return json.loads(_take_cstr(ctypes.cast(out, ctypes.c_void_p)))
