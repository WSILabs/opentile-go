"""opentile_go — Python bindings for the Go opentile-go whole-slide-image reader."""

__version__ = "0.1.0.dev0"


class OpenTileError(Exception):
    """Raised for any error surfaced by the opentile-go FFI shim."""


from .slide import Slide, Level  # noqa: E402

__all__ = ["Slide", "Level", "OpenTileError", "__version__"]
