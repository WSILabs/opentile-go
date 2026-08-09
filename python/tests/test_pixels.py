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
