# opentile-go Python binding (`opentile_go`) — design

**Status:** approved design → this spec.
**Scope:** Sub-project 2 of 2 in the "release a Python interface" arc. Sub-project 1
(opentile-go Windows CI parity, v0.63.0) is shipped and de-risks the hardest part
of this work — all six codecs linking on Windows.

## 1. Goal

Ship `opentile-go` on PyPI: a Python package (`import opentile_go`) that reads
whole-slide images by binding the Go opentile-go library through a C-ABI shim,
exposing an openslide-inspired, **numpy-first** API. Thin MVP surface; full-codec
self-contained wheels for Linux (x86_64 + aarch64), macOS (x86_64 + arm64), and
Windows (x86_64).

Non-goals for v1 in §9.

## 2. Settled decisions (from brainstorm)

- **API shape:** openslide-*inspired*, not a drop-in — an `opentile_go`-native,
  Pythonic, numpy-first API (its own names; not obligated to mirror
  openslide-python signatures or return PIL).
- **MVP surface:** open/close, levels (+ size/downsample/tile_size/grid), raw
  compressed `tile()` bytes, decoded `decoded_tile()`/`read_region()` → numpy,
  metadata (format/mpp/magnification/properties), openslide-style lazy
  associated-images dict, **thumbnails/macro**, and **raw TIFF tags**. Deferred:
  scaled-region/DZI strips, `Validate()`.
- **Binding mechanism:** Go `-buildmode=c-shared` + **ctypes** (stdlib) facade.
  Pure-Python facade → the only native artifact is the Python-agnostic Go
  shared lib → **one wheel per platform** (`py3-none-<platform>`), not per
  (platform × Python).
- **Linking:** **vcpkg-static-everywhere** — the six codecs are statically
  linked into the shared lib via vcpkg static triplets (the same recipe
  sub-project 1 proved). Wheel-repair tools then have ~nothing to bundle.
- **Python floor:** `requires-python = ">=3.10"` (3.9 is EOL; numpy 2.x needs
  3.10+). numpy is a loose runtime dep (`numpy>=1.23`, no upper pin — we use
  `np.frombuffer`, never numpy's C ABI, so we are numpy-version-agnostic).
- **Cold vs hot data:** cold structured metadata crosses as **one JSON call**;
  hot pixel/byte payloads cross as **raw binary buffers** (§4).
- **Location & name:** in-repo `python/` subdir; PyPI `opentile-go`; import
  `opentile_go`. The name is a thin swappable layer (a future repo rename is
  planned) — only `pyproject`'s `name`, the package dir, and the `ot_` C symbol
  prefix carry it; the public API stays name-agnostic (`Slide`, `Level`).

## 3. Architecture — three layers + a build

```
opentile_go/           pure-Python facade (Slide, Level), numpy-first  ← user API
      │ ctypes (CDLL, GIL released per call)
_opentilego.{so,dylib,pyd}   Go c-shared: //export shim + opentile-go + 6 static codecs
```

1. **Go FFI shim** — `python/cshim/` (`package main`, `import "C"`, `func main(){}`),
   built `-buildmode=c-shared`. `//export`ed C-ABI functions (§4). Statically
   links the codecs (vcpkg).
2. **ctypes layer** — `opentile_go/_ffi.py`: loads the bundled lib, declares
   arg/restypes, marshals buffers to numpy/bytes, raises on error.
3. **Facade** — `opentile_go/__init__.py` + `slide.py`: `Slide`/`Level`,
   metadata, associated-images dict (§5).

## 4. The C-ABI contract (Go shim)

All exported symbols are prefixed `ot_`. All strings are UTF-8 `char*`.

### 4.1 Handle & lifecycle
- `ot_open(path *char, err_out **char) uintptr` — opens via
  `opentile.OpenFile(path)`; returns a `runtime/cgo.Handle` (as `uintptr`)
  wrapping the `*opentile.Slide`, or `0` with `*err_out` set. **Never returns a
  raw Go pointer** — the moving GC would invalidate it; `cgo.Handle` is the
  stable token.
- `ot_close(h uintptr)` — deletes the handle (which calls `Slide.Close()`).
- Path-only input for v1 (Python `str`/`os.PathLike` → filesystem path).
  Arbitrary `io.ReaderAt`/file-like objects are out of scope (§9).

### 4.2 Cold metadata — one JSON call
- `ot_metadata_json(h uintptr, err_out **char) char*` — returns a malloc'd
  UTF-8 JSON document (Python frees via `ot_free`). Parsed once at open. Schema:
  ```json
  {
    "format": "svs",
    "mpp": [0.499, 0.499],            // null if unset (MPP.X/Y == 0)
    "magnification": 40.0,             // null if 0/unknown
    "properties": { "aperio.AppMag": "40", ... },
    "levels": [
      { "size": [w, h], "tile_size": [tw, th], "grid": [cols, rows],
        "downsample": 1.0, "overlapping": false }
    ],
    "associated": ["label", "macro", "thumbnail"]
  }
  ```
  Sourced from `Slide.Format()`, `Slide.Metadata()` (Magnification, MPP,
  Properties), `Slide.Levels()` (Size/TileSize/Grid/Downsample/Overlapping), and
  `Slide.AssociatedImages()` (`Type()` names). Encoded with Go `encoding/json`;
  parsed with Python stdlib `json`. Adding a field later needs no new C symbol.

### 4.3 Hot pixels & bytes — raw binary buffers
Each returns `int` (0 = ok, non-zero = error with `*err_out` set) and writes
out-params. Python copies the buffer into numpy/bytes, then calls `ot_free`.

- `ot_tile(h, level int, x, y int, out **uint8, out_len *size_t, err **char) int`
  — raw compressed tile bytes (`Level.Tile`), no decode.
- `ot_decoded_tile(h, level, x, y int, rgba int, out **uint8, out_len *size_t,
  out_w, out_h, out_bands *int, err **char) int` — `Level.DecodedTile`.
- `ot_read_region(h, level, x, y, w, h_ int, rgba int, out **uint8,
  out_len *size_t, out_w, out_h, out_bands *int, err **char) int`
  — `Level.ReadRegion`.
- `ot_associated(h, name *char, rgba int, out **uint8, out_len *size_t,
  out_w, out_h, out_bands *int, err **char) int` — the named associated image's
  `Decode`.
- `ot_thumbnail(h, max_w, max_h int, out **uint8, ...same tail...) int`
  — `Slide.RenderThumbnail`.
- `ot_macro(h, max_w, max_h int, out **uint8, ...same tail...) int`
  — `Slide.RenderMacro`.
- `ot_tiff_tags_json(h, level int, out **char, err **char) int` — per-level raw
  TIFF tags as JSON (from `Level.TIFFTags`); returns a sentinel/empty when the
  level has none (`ok == false`). (JSON, since tags are cold + structured.)
- `ot_free(ptr *void)` — frees any buffer/string the shim returned.

**Stride handling (critical):** `decoder.Image.Pix` is row-padded
(`len(Pix) == Stride*Height`, `Stride ≥ Width*bands`). The shim MUST copy into a
**tight, contiguous** `Height*Width*bands` buffer (dropping the per-row padding)
so Python receives a contiguous `(H, W, bands)` array. `bands` is 3 for
`PixelFormatRGB`, 4 for `PixelFormatRGBA`; `rgba` in-param selects
`decoder.DecodeOptions{Format: RGB|RGBA}`.

### 4.4 Concurrency
`*opentile.Slide` is immutable after open with lock-free tile reads, and
`ctypes.CDLL` releases the GIL around each call — so multiple Python threads
decoding tiles run with real parallelism. The shim adds no shared mutable state
(handles are independent; `cgo.Handle` is safe for concurrent lookup).

## 5. The numpy facade (public Python API)

```python
from opentile_go import Slide, OpenTileError

with Slide("a.svs") as s:               # context manager; .close() also exposed
    s.format                            # "svs"
    s.mpp                               # (0.499, 0.499) | None
    s.magnification                     # 40.0 | None
    s.properties                        # dict[str, str]
    for lv in s.levels:                 # list[Level]
        lv.index                        # 0..
        lv.size                         # (w, h)
        lv.downsample                   # float
        lv.tile_size                    # (tw, th)
        lv.grid                         # (cols, rows)
        lv.tile(x, y)                   # bytes  (raw compressed, pure passthrough)
        lv.decoded_tile(x, y, rgba=False)   # np.ndarray (H, W, 3|4) uint8
        lv.read_region(x, y, w, h, rgba=False)  # np.ndarray
        lv.tiff_tags                    # dict | None (lazy)
    s.associated_images                 # Mapping[str, np.ndarray] (lazy, openslide-style)
    s.associated_images["label"]        # decodes on access
    s.thumbnail(width=1024)             # np.ndarray (fit-box; height/width optional)
    s.macro()                           # np.ndarray
```

- `Slide.__init__` opens the handle and parses `ot_metadata_json` once, building
  the `Level` list and caching scalar metadata. `close()`/`__exit__` calls
  `ot_close`; use-after-close raises `OpenTileError`.
- `Level` holds a back-reference to the slide handle + its level index; pixel
  methods call the corresponding `ot_*` and marshal the buffer to numpy.
- `associated_images` is a lazy `Mapping` (keys from JSON `associated`; a value
  decodes via `ot_associated` on first access).
- `OpenTileError(Exception)` wraps every shim error (message from `err_out`).
- Decoded arrays are `uint8`, shape `(H, W, 3)` (or `(H, W, 4)` when `rgba=True`).
- **PIL is not a dependency.** A user wanting a PIL image does
  `Image.fromarray(arr)` themselves.

## 6. Packaging (vcpkg-static + cibuildwheel)

- **Build backend:** setuptools with a custom build step that (a) ensures the
  vcpkg static codecs are present (built in `CIBW_BEFORE_ALL`), then (b) runs
  `go build -buildmode=c-shared -o opentile_go/_opentilego.<ext> ./cshim` with
  `CGO_ENABLED=1` and `PKG_CONFIG_PATH` pointing at the vcpkg static libs. The
  wheel is marked platform-specific and Python-agnostic and tagged
  **`py3-none-<platform>`** (one build per platform, not per Python).
- **cibuildwheel** provides the manylinux container (Linux) and drives repair
  (`auditwheel` / `delocate` / `delvewheel`) — which, because the codecs are
  static, find no non-system codec libs to bundle and only ship the one Go
  `.so`/`.dylib`/`.pyd`. `CIBW_BEFORE_ALL` installs Go + bootstraps vcpkg (pinned
  `vcpkg.json` baseline `40f3c709…`) + builds the six static codecs for the
  target triplet (`x64-linux-static`, `arm64-linux-static`, `x64-osx-static`,
  `arm64-osx-static`, `x64-mingw-static`).
- **Targets:** manylinux `x86_64` + `aarch64`, macOS `x86_64` + `arm64`,
  Windows `amd64` — the five wsitools already ships.
- **CI/publish:** a new `.github/workflows/wheels.yml` builds the matrix on
  `v*.*.*` tags and publishes to PyPI via OIDC **trusted publishing** (no stored
  token). A `workflow_dispatch` allows dry-run matrix builds without publishing.

## 7. Testing

- **pytest** (`python/tests/`): open a fixture slide and assert level count +
  dims + downsamples match the JSON metadata; `tile()` returns non-empty bytes;
  `decoded_tile`/`read_region` return a `(H, W, 3)` `uint8` array of the expected
  shape; `mpp`/`magnification` where the fixture carries them; associated-images
  keys + a decoded `(H,W,3)` array; `thumbnail`/`macro` shape; use-after-close
  raises `OpenTileError`. Gated on the public `WSILabs/wsi-fixtures` corpus
  (skip-if-absent).
- **Parity:** one decoded `read_region` asserted **byte-identical** to the Go
  reader's output for the same region on a CC0 fixture — proves the FFI + stride
  copy is faithful. (Generate the Go-side reference with a tiny `go run` helper
  or a committed golden.)
- **Shim build test:** a CI step that builds `_opentilego` locally and imports
  `opentile_go` + opens a fixture, ensuring the vertical slice links and loads.

## 8. File structure & implementation phasing

```
python/
  pyproject.toml
  cshim/main.go              # //export C-ABI shim (package main)
  opentile_go/__init__.py    # Slide, Level, OpenTileError, version
  opentile_go/_ffi.py        # ctypes loader, symbol declarations, marshaling
  opentile_go/py.typed
  tests/
  README.md
.github/workflows/wheels.yml
```

One spec, **two implementation phases** (the plan sequences them):

- **Phase 1 — vertical slice (de-risks everything):** the Go shim + ctypes layer
  + facade, producing a working wheel **on the local platform only**
  (macOS arm64 dev machine) with the pytest suite green. Proves the whole
  Go→C→ctypes→numpy path end to end.
- **Phase 2 — breadth:** the full five-target cibuildwheel matrix +
  `wheels.yml` CI + PyPI trusted-publishing.

## 9. Out of scope (v1)

- Scaled-region / DZI-strip APIs (`ReadRegionScaled`, `ScaledStrips`) and
  `Validate()` — deferred to a later version.
- Arbitrary `io.ReaderAt` / Python file-like inputs (path-only for v1; remote
  I/O would need C→Python read callbacks).
- Windows `arm64` and Linux `musl` wheels (GitHub runners / demand not there
  yet).
- PIL return types (numpy only; PIL is a one-liner for the user).
- A `nocgo`/pure-Python fallback (the package is inherently native).

## 10. Risks & mitigations

- **Wheel build time** (vcpkg codec compile per target, ~25–40 min cold):
  mitigated by the vcpkg binary cache in `wheels.yml`, exactly as in
  sub-project 1. Only tag builds pay it.
- **`py3-none-<platform>` tagging** with setuptools is non-default: the backend
  must force a platform wheel (`root_is_pure = False`) with a `py3`/`none` tag.
  Proven pattern; the plan pins the exact backend hook. Phase 1 validates it on
  one platform before the matrix.
- **Stride mismatch** producing garbled images: covered by the byte-parity test
  (§7) against the Go reader — a stride bug would fail it immediately.
- **`delvewheel` over the mingw Go `.dll`:** minimized because codecs are static
  (nothing to bundle but the one Go lib); if `delvewheel` still balks, the Go
  `.pyd` is already self-contained and can ship unrepaired on Windows.
- **cgo.Handle leak** on unclosed slides: documented; `Slide` is a context
  manager and `__del__` also calls `ot_close` as a backstop.
