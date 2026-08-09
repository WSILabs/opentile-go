"""Public numpy-first facade: Slide and Level."""

from collections.abc import Mapping

from . import _ffi
from . import OpenTileError


class _AssociatedMap(Mapping):
    """Lazy openslide-style mapping: name -> decoded numpy array."""

    def __init__(self, slide, names):
        self._slide = slide
        self._names = list(names)

    def __getitem__(self, name):
        if name not in self._names:
            raise KeyError(name)
        return self._slide._associated(name)

    def __iter__(self):
        return iter(self._names)

    def __len__(self):
        return len(self._names)


class Level:
    def __init__(self, slide, meta, index):
        self._slide = slide
        self.index = index
        self._i = index
        self.size = tuple(meta["size"])
        self.tile_size = tuple(meta["tile_size"])
        self.grid = tuple(meta["grid"])
        self.downsample = float(meta["downsample"])
        self.overlapping = bool(meta.get("overlapping", False))

    def _handle(self):
        """Return the live FFI handle, or raise OpenTileError if the slide is closed."""
        h = self._slide._h
        if h is None:
            raise OpenTileError("opentile: slide is closed")
        return h

    def tile(self, x, y):
        return _ffi.tile(self._handle(), self._i, x, y)

    def decoded_tile(self, x, y, rgba=False):
        return _ffi.decoded_tile(self._handle(), self._i, x, y, rgba=rgba)

    def read_region(self, x, y, w, h, rgba=False):
        return _ffi.read_region(self._handle(), self._i, x, y, w, h, rgba=rgba)

    @property
    def tiff_tags(self):
        return _ffi.tiff_tags(self._handle(), self._i)


class Slide:
    def __init__(self, path):
        self._h = _ffi.open_slide(path)
        self._closed = False
        meta = _ffi.metadata(self._h)
        self.format = meta["format"]
        self.mpp = tuple(meta["mpp"]) if meta.get("mpp") else None
        self.magnification = meta.get("magnification")
        self.properties = meta.get("properties") or {}
        self._levels = [Level(self, lmeta, i) for i, lmeta in enumerate(meta["levels"])]
        self.associated_images = _AssociatedMap(self, meta.get("associated") or [])

    @property
    def levels(self):
        return self._levels

    def _handle(self):
        """Return the live FFI handle, or raise OpenTileError if closed."""
        if self._h is None:
            raise OpenTileError("opentile: slide is closed")
        return self._h

    def _associated(self, name):
        return _ffi.associated(self._handle(), name)

    def thumbnail(self, width=None, height=None):
        return _ffi.thumbnail(self._handle(), width or 0, height or 0)

    def macro(self, width=None, height=None):
        return _ffi.macro(self._handle(), width or 0, height or 0)

    def close(self):
        if not self._closed:
            _ffi.close_slide(self._h)
            self._h = None  # sentinel: prevents Level._handle() from passing a stale handle to the shim
            self._closed = True

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()

    def __del__(self):
        try:
            self.close()
        except Exception:
            pass
