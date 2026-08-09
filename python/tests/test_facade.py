import numpy as np
from opentile_go import Slide, OpenTileError

def test_facade_roundtrip(slide_path):
    with Slide(slide_path) as s:
        assert s.format
        assert isinstance(s.properties, dict)
        assert s.mpp is None or (len(s.mpp) == 2)
        assert len(s.levels) >= 1
        lv = s.levels[0]
        assert len(lv.size) == 2 and lv.downsample >= 1.0
        assert isinstance(lv.tile(0, 0), (bytes, bytearray))
        arr = lv.read_region(0, 0, 32, 24)
        assert arr.shape == (24, 32, 3) and arr.dtype == np.uint8
        assert lv.decoded_tile(0, 0).shape[2] == 3
        for name, img in s.associated_images.items():
            assert img.ndim == 3
        assert s.thumbnail(width=128).shape[2] == 3

def test_use_after_close_raises(slide_path):
    import pytest
    s = Slide(slide_path)
    s.close()
    with pytest.raises(OpenTileError):
        _ = s.levels[0].tile(0, 0)
