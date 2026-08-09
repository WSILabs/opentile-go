from opentile_go import _ffi

def test_open_metadata_close(slide_path):
    h = _ffi.open_slide(slide_path)
    try:
        meta = _ffi.metadata(h)
        assert meta["format"]
        assert isinstance(meta["levels"], list) and meta["levels"]
        assert all("size" in lv and "downsample" in lv for lv in meta["levels"])
    finally:
        _ffi.close_slide(h)

def test_open_bad_path_raises():
    import pytest
    from opentile_go import OpenTileError
    with pytest.raises(OpenTileError):
        _ffi.open_slide("/no/such/slide.svs")
