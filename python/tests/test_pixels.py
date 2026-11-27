import numpy as np
from opentile_go import _ffi

def test_raw_tile_bytes(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        b = _ffi.tile(h, 0, 0, 0)
        assert isinstance(b, (bytes, bytearray)) and len(b) > 0
    finally:
        _ffi.close_slide(h)

def test_decoded_tile_shape(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        meta = _ffi.metadata(h)
        tw, th = meta["levels"][0]["tile_size"]
        arr = _ffi.decoded_tile(h, 0, 0, 0, rgba=False)
        assert arr.dtype == np.uint8 and arr.ndim == 3 and arr.shape[2] == 3
        assert arr.shape[0] <= th and arr.shape[1] <= tw  # edge tiles may be smaller
        rgba = _ffi.decoded_tile(h, 0, 0, 0, rgba=True)
        assert rgba.shape[2] == 4
    finally:
        _ffi.close_slide(h)

def test_read_region_shape(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        arr = _ffi.read_region(h, 0, 0, 0, 64, 48, rgba=False)
        assert arr.shape == (48, 64, 3) and arr.dtype == np.uint8
    finally:
        _ffi.close_slide(h)

def test_srgb_flag(slide_path):
    """srgb=True passes OT_DECODE_SRGB; without a usable profile the pixels
    are unchanged, so only the shape and the srgb=False default are fixed."""
    h = _ffi.open_slide(slide_path)
    try:
        plain = _ffi.read_region(h, 0, 0, 0, 64, 48)
        assert np.array_equal(plain, _ffi.read_region(h, 0, 0, 0, 64, 48, srgb=False))
        conv = _ffi.read_region(h, 0, 0, 0, 64, 48, srgb=True)
        assert conv.shape == plain.shape and conv.dtype == np.uint8
        assert _ffi.thumbnail(h, 64, 0, srgb=True).shape[1] == 64
    finally:
        _ffi.close_slide(h)
