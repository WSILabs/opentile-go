"""opentile_go — Python bindings for the Go opentile-go whole-slide-image reader.

Read whole-slide pathology images as numpy arrays::

    from opentile_go import Slide

    with Slide("slide.svs") as s:
        print(s.format, s.mpp, s.magnification)
        region = s.levels[0].read_region(0, 0, 1024, 1024)  # numpy (H, W, 3) uint8
        raw    = s.levels[0].tile(0, 0)                      # bytes (compressed)
        label  = s.associated_images["label"]               # numpy
        thumb  = s.thumbnail(width=1024)                     # numpy

Public API: :class:`Slide`, :class:`Level`, and :class:`OpenTileError`.
"""

__version__ = "0.1.0.dev0"


class OpenTileError(Exception):
    """Raised for any error from the binding.

    Covers open failures, out-of-range tiles/regions, unavailable codecs, and
    use of a slide after :meth:`Slide.close`.
    """


from .slide import Slide, Level  # noqa: E402

__all__ = ["Slide", "Level", "OpenTileError", "__version__"]
