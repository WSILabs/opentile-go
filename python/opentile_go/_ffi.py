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
