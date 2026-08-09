import numpy as np
from opentile_go import _ffi

def test_thumbnail(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        arr = _ffi.thumbnail(h, 256, 256)
        assert arr.ndim == 3 and arr.shape[2] == 3 and arr.dtype == np.uint8
        assert arr.shape[0] <= 256 and arr.shape[1] <= 256
    finally:
        _ffi.close_slide(h)

def test_associated(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        names = _ffi.metadata(h)["associated"]
        for name in names:
            arr = _ffi.associated(h, name)
            assert arr.ndim == 3 and arr.shape[2] in (3, 4)
    finally:
        _ffi.close_slide(h)

def test_tiff_tags(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        tags = _ffi.tiff_tags(h, 0)   # list | None
        assert tags is None or isinstance(tags, list)
    finally:
        _ffi.close_slide(h)
