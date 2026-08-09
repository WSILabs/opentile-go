"""Public numpy-first facade: :class:`Slide` and :class:`Level`.

Open a whole-slide image with :class:`Slide`, then read raw or decoded tiles,
arbitrary regions, associated images, and rendered thumbnails/macro — decoded
pixels come back as ``numpy`` ``uint8`` arrays shaped ``(H, W, 3)`` (or
``(H, W, 4)`` with ``rgba=True``); raw tiles come back as compressed ``bytes``.
"""

from collections.abc import Mapping

from . import _ffi
from . import OpenTileError


class _AssociatedMap(Mapping):
    """Lazy, read-only ``{name: numpy array}`` mapping of associated images.

    Behaves like a dict (``in``, ``len``, iteration, ``.keys()``/``.items()``);
    each value is decoded on first access via the FFI shim, openslide-style.
    Reachable as :attr:`Slide.associated_images`.
    """

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
    """One pyramid level of a :class:`Slide`.

    Obtained from :attr:`Slide.levels` (do not construct directly). All pixel
    methods raise :class:`OpenTileError` if the owning slide has been closed.

    Attributes
    ----------
    index : int
        Level index; ``0`` is full resolution.
    size : tuple[int, int]
        ``(width, height)`` of the level in pixels.
    tile_size : tuple[int, int]
        ``(tile_width, tile_height)`` of the stored tile grid.
    grid : tuple[int, int]
        ``(columns, rows)`` — the number of tiles across and down.
    downsample : float
        Linear downsample factor relative to level 0 (``1.0`` at level 0).
    overlapping : bool
        ``True`` when stored tiles overlap and ``grid`` no longer tiles ``size``
        cleanly (e.g. stitched Ventana BIF); use :meth:`read_region` for a clean
        image rather than assembling tiles yourself.
    """

    def __init__(self, slide, meta, index):
        self._slide = slide
        self.index = index
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
        """Return the raw compressed bytes of tile ``(x, y)`` — no decode.

        The bitstream is returned exactly as stored on disk (e.g. a JPEG or
        JPEG 2000 frame), the zero-copy fast path for tile servers/transcoders.

        Parameters
        ----------
        x, y : int
            Tile column and row in the level's tile grid.

        Returns
        -------
        bytes
            The compressed tile bitstream.

        Raises
        ------
        OpenTileError
            If the slide is closed or ``(x, y)`` is out of range.
        """
        return _ffi.tile(self._handle(), self.index, x, y)

    def decoded_tile(self, x, y, rgba=False):
        """Decode tile ``(x, y)`` to a numpy array.

        Parameters
        ----------
        x, y : int
            Tile column and row in the level's tile grid.
        rgba : bool, optional
            If ``True`` return ``(H, W, 4)`` RGBA; otherwise ``(H, W, 3)`` RGB.

        Returns
        -------
        numpy.ndarray
            ``uint8`` array of shape ``(H, W, 3)`` or ``(H, W, 4)``. Edge tiles
            may be smaller than ``tile_size``.

        Raises
        ------
        OpenTileError
            If the slide is closed, the tile is out of range, or the codec is
            unavailable in this build.
        """
        return _ffi.decoded_tile(self._handle(), self.index, x, y, rgba=rgba)

    def read_region(self, x, y, w, h, rgba=False):
        """Decode an arbitrary ``w``×``h`` region of this level.

        Coordinates and size are in this level's pixel space (not level-0). For
        overlapping levels this composites a clean image (unlike raw tiles).

        Parameters
        ----------
        x, y : int
            Top-left corner of the region, in this level's pixels.
        w, h : int
            Region width and height in pixels.
        rgba : bool, optional
            If ``True`` return ``(h, w, 4)`` RGBA; otherwise ``(h, w, 3)`` RGB.

        Returns
        -------
        numpy.ndarray
            ``uint8`` array of shape ``(h, w, 3)`` or ``(h, w, 4)``.

        Raises
        ------
        OpenTileError
            If the slide is closed or the region cannot be read.
        """
        return _ffi.read_region(self._handle(), self.index, x, y, w, h, rgba=rgba)

    @property
    def tiff_tags(self):
        """Raw TIFF tags for this level as a list of dicts, or ``None``.

        Each entry is ``{"number": int, "name": str}`` plus ``"ascii"`` or
        ``"uints"`` when the tag carries such a value. ``None`` for non-TIFF
        formats or levels without tags.

        Raises
        ------
        OpenTileError
            If the slide is closed.
        """
        return _ffi.tiff_tags(self._handle(), self.index)


class Slide:
    """An open whole-slide image.

    Use as a context manager (recommended) so the native handle is always
    released::

        with Slide("slide.svs") as s:
            region = s.levels[0].read_region(0, 0, 1024, 1024)

    Parameters
    ----------
    path : str or os.PathLike
        Filesystem path to the slide (or, for multi-file formats such as DICOM,
        a directory or a member file).

    Attributes
    ----------
    format : str
        Detected format identifier (e.g. ``"svs"``, ``"ndpi"``).
    mpp : tuple[float, float] or None
        Microns-per-pixel ``(x, y)`` at level 0, or ``None`` if the slide does
        not carry it.
    magnification : float or None
        Objective magnification (e.g. ``40.0``), or ``None`` if unknown.
    properties : dict[str, str]
        Format-specific and vendor-namespaced metadata key/value pairs.
    levels : list[Level]
        Pyramid levels, finest first (``levels[0]`` is full resolution).
    associated_images : Mapping[str, numpy.ndarray]
        Lazy mapping of associated-image name (e.g. ``"label"``, ``"macro"``,
        ``"thumbnail"``) to a decoded ``(H, W, 3|4)`` ``uint8`` array.

    Raises
    ------
    OpenTileError
        If the slide cannot be opened.
    """

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
        """list[Level]: the pyramid levels, finest first."""
        return self._levels

    def _handle(self):
        """Return the live FFI handle, or raise OpenTileError if closed."""
        if self._h is None:
            raise OpenTileError("opentile: slide is closed")
        return self._h

    def _associated(self, name):
        return _ffi.associated(self._handle(), name)

    def thumbnail(self, width=None, height=None):
        """Render a whole-slide thumbnail as a numpy array.

        Fits the slide within the given bounding box, preserving aspect ratio
        and never upscaling past level 0. Pass one axis (the other is derived);
        with neither given a default width of 1024 is used.

        Parameters
        ----------
        width, height : int, optional
            Maximum width and/or height of the thumbnail, in pixels.

        Returns
        -------
        numpy.ndarray
            ``uint8`` array of shape ``(H, W, 3)``.

        Raises
        ------
        OpenTileError
            If the slide is closed.
        """
        if not width and not height:
            width = 1024  # default fit-box when neither axis is given
        return _ffi.thumbnail(self._handle(), width or 0, height or 0)

    def macro(self, width=None, height=None):
        """Render a synthesized pseudo-macro image as a numpy array.

        Composites the tissue at its true physical size (using :attr:`mpp` or
        :attr:`magnification`) on a scan-area canvas. Bounds behave as in
        :meth:`thumbnail` (default width 1024 when neither axis is given).

        Parameters
        ----------
        width, height : int, optional
            Maximum width and/or height, in pixels.

        Returns
        -------
        numpy.ndarray
            ``uint8`` array of shape ``(H, W, 3)``.

        Raises
        ------
        OpenTileError
            If the slide is closed or carries neither MPP nor magnification.
        """
        if not width and not height:
            width = 1024
        return _ffi.macro(self._handle(), width or 0, height or 0)

    def close(self):
        """Release the native slide handle. Idempotent; called automatically on
        context-manager exit and garbage collection. After ``close()`` every
        pixel/metadata method raises :class:`OpenTileError`."""
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
