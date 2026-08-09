# opentile-go (Python)

numpy-first Python bindings for [opentile-go](https://github.com/WSILabs/opentile-go),
a whole-slide-image tile reader.

```python
from opentile_go import Slide

with Slide("slide.svs") as s:
    print(s.format, s.mpp, s.magnification)
    region = s.levels[0].read_region(0, 0, 1024, 1024)   # -> numpy (H, W, 3) uint8
    raw = s.levels[0].tile(0, 0)                          # -> bytes (compressed)
    label = s.associated_images["label"]                 # -> numpy
    thumb = s.thumbnail(width=1024)                       # -> numpy
```

Decoded pixels are `numpy` `uint8` arrays shaped `(H, W, 3)` (RGB) or
`(H, W, 4)` when you pass `rgba=True`; raw tiles are compressed `bytes`. Every
error is an `OpenTileError`. Requires Python ≥ 3.10 and `numpy`.

## API reference

### `Slide(path)`

Open a whole-slide image. Use as a context manager so the native handle is
always released. `path` is a filesystem path (or, for multi-file formats like
DICOM, a directory or member file).

| Attribute | Type | Description |
|-----------|------|-------------|
| `format` | `str` | Detected format id (`"svs"`, `"ndpi"`, …). |
| `mpp` | `tuple[float, float] \| None` | Microns-per-pixel `(x, y)` at level 0. |
| `magnification` | `float \| None` | Objective magnification (e.g. `40.0`). |
| `properties` | `dict[str, str]` | Format/vendor metadata key/value pairs. |
| `levels` | `list[Level]` | Pyramid levels, finest first (`levels[0]` = full res). |
| `associated_images` | `Mapping[str, np.ndarray]` | Lazy `{name: decoded array}` (e.g. `"label"`, `"macro"`, `"thumbnail"`). |

| Method | Returns | Description |
|--------|---------|-------------|
| `thumbnail(width=None, height=None)` | `np.ndarray (H,W,3)` | Whole-slide thumbnail fit to the box (default width 1024). |
| `macro(width=None, height=None)` | `np.ndarray (H,W,3)` | Synthesized pseudo-macro at true physical size. |
| `close()` | `None` | Release the handle (also on context exit / GC). |

### `Level`

A pyramid level, from `slide.levels[i]` (not constructed directly).

| Attribute | Type | Description |
|-----------|------|-------------|
| `index` | `int` | Level index (`0` = full resolution). |
| `size` | `tuple[int, int]` | `(width, height)` in pixels. |
| `tile_size` | `tuple[int, int]` | `(tile_width, tile_height)`. |
| `grid` | `tuple[int, int]` | `(columns, rows)` of the tile grid. |
| `downsample` | `float` | Linear downsample vs. level 0. |
| `overlapping` | `bool` | Tiles overlap (use `read_region`, not tile assembly). |
| `tiff_tags` | `list[dict] \| None` | Raw TIFF tags, or `None`. |

| Method | Returns | Description |
|--------|---------|-------------|
| `tile(x, y)` | `bytes` | Raw compressed tile bitstream — no decode. |
| `decoded_tile(x, y, rgba=False)` | `np.ndarray (H,W,3\|4)` | Decode one tile. Edge tiles may be smaller than `tile_size`. |
| `read_region(x, y, w, h, rgba=False)` | `np.ndarray (h,w,3\|4)` | Decode an arbitrary region (this level's pixel space). |

### `OpenTileError`

Exception raised for any binding error — open failures, out-of-range
tiles/regions, unavailable codecs, or use of a slide after `close()`.

Full docstrings are available via `help(opentile_go.Slide)` in a REPL.

## Building from source (development)

Requires Go 1.23+ and the codec libraries (jpeg-turbo, openjpeg, openjph,
jpeg-xl, libavif, libwebp) — on macOS: `brew install jpeg-turbo openjpeg openjph
jpeg-xl libavif webp pkg-config`.

```sh
cd python && ./build_dev.sh && pip install -e . && pytest
```

Wheels bundle all six codecs statically (no system install needed).
