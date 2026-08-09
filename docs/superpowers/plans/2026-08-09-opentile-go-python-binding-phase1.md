# opentile-go Python binding — Phase 1 (vertical slice) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working `opentile_go` Python package that opens a slide, reads raw + decoded tiles, regions, metadata, associated images, thumbnails/macro, and TIFF tags — as numpy arrays — via a Go `c-shared` FFI shim + ctypes, fully building and passing pytest **on the local macOS/Linux dev machine**.

**Architecture:** Go `-buildmode=c-shared` shim (`//export` C-ABI, `cgo.Handle` handles, malloc'd buffers + `ot_free`, `err_out` string errors, one JSON call for cold metadata) → pure-Python `ctypes` layer (declarations + numpy/bytes marshaling) → numpy-first `Slide`/`Level` facade. Phase 1 links the codecs **dynamically against the local brew/apt libs** (the exact vcpkg-static wheel build is Phase 2).

**Tech Stack:** Go 1.23+ (cgo, `runtime/cgo.Handle`), Python 3.10+, `ctypes` (stdlib), numpy (`np.frombuffer`), pytest.

**Spec:** `docs/superpowers/specs/2026-08-09-opentile-go-python-binding-design.md`

**Branch:** `feat/python-binding` (already created).

**Scope note:** This plan is **Phase 1 only** (the spec's local vertical slice). **Phase 2** (vcpkg-static wheels across 5 targets + `wheels.yml` CI + PyPI trusted publishing) is a distinct CI/packaging subsystem and gets its **own** plan once Phase 1 is green — its exact cibuildwheel/repair shape is best pinned empirically after the local build works.

## Exact Go types the shim wraps (verified)
- `opentile.OpenFile(path string) (*Slide, error)`; `Slide.Close() error`.
- `Slide.Format() Format` (`type Format string`); `Slide.Metadata() Metadata` (`Magnification float64`, `MPP MPP{X,Y float64}`, `Properties map[string]string`); `Slide.Levels() []*Level`; `Slide.AssociatedImages() []AssociatedImage` (`Type() AssociatedType` [string], `Decode(decoder.DecodeOptions)`... but use `Level`/opts helpers below); `Slide.RenderThumbnail(bounds Size, ...DecodeOption)`, `Slide.RenderMacro(bounds Size, ...DecodeOption)`.
- `Level` fields: `Index int`, `Size Size{W,H}`, `TileSize Size`, `Grid Size`, `Downsample float64`, `Overlapping bool`. Methods: `Tile(tx,ty) ([]byte,error)`, `DecodedTile(tx,ty, ...DecodeOption) (*decoder.Image,error)`, `ReadRegion(Region, ...DecodeOption) (*decoder.Image,error)`, `TIFFTags() (TIFFTags,bool)`.
- `Region{Origin Point{X,Y int}, Size Size{W,H int}}`.
- `opentile.WithFormat(decoder.PixelFormatRGBA)` selects RGBA; default is `PixelFormatRGB`.
- `decoder.Image{Width, Height, Stride int, Format PixelFormat, Pix []byte}` — **`Pix` is row-padded**: `len(Pix)==Stride*Height`, `Stride ≥ Width*bands`. `PixelFormatRGB`=3 bands, `PixelFormatRGBA`=4.
- `AssociatedImage.Type() AssociatedType`; decode a named associated image by finding the matching `Type()` and calling its `Decode(decoder.DecodeOptions{Format: ...})`.
- `TIFFTag{Number uint16, Name string, ...}` with `ASCII() (string,bool)`, `Uints() ([]uint64,bool)`; `TIFFTags []TIFFTag`.

---

### Task 1: Scaffold the `python/` package + dev build script

**Files:**
- Create: `python/pyproject.toml`, `python/opentile_go/__init__.py`, `python/opentile_go/py.typed`, `python/tests/__init__.py`, `python/build_dev.sh`, `python/.gitignore`
- Test: `python/tests/test_import.py`

- [ ] **Step 1: Write the failing test**

`python/tests/test_import.py`:
```python
def test_import_and_version():
    import opentile_go
    assert isinstance(opentile_go.__version__, str)
    assert opentile_go.__version__
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && python -m pytest tests/test_import.py -q`
Expected: FAIL — `ModuleNotFoundError: No module named 'opentile_go'` (package not on path yet).

- [ ] **Step 3: Create the package skeleton**

`python/opentile_go/__init__.py`:
```python
"""opentile_go — Python bindings for the Go opentile-go whole-slide-image reader."""

__version__ = "0.1.0.dev0"
```

`python/opentile_go/py.typed` — empty file.
`python/tests/__init__.py` — empty file.

`python/pyproject.toml`:
```toml
[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[project]
name = "opentile-go"
version = "0.1.0.dev0"
description = "Python bindings for opentile-go — whole-slide-image tile reader"
requires-python = ">=3.10"
dependencies = ["numpy>=1.23"]

[project.optional-dependencies]
test = ["pytest>=7"]

[tool.setuptools]
packages = ["opentile_go"]

[tool.setuptools.package-data]
opentile_go = ["py.typed", "_opentilego.*"]
```

`python/.gitignore`:
```
_opentilego.*
*.h
build/
dist/
*.egg-info/
__pycache__/
```

`python/build_dev.sh` (dev-only local build of the c-shared lib; exercised from Task 2 onward):
```bash
#!/usr/bin/env bash
# Local development build of the opentile-go c-shared FFI lib. Links the codec
# libraries dynamically against the system (brew/apt) install — NOT the vcpkg
# static build used for distributable wheels (that is Phase 2). Produces
# opentile_go/_opentilego.so next to the Python package.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
CGO_ENABLED=1 go build -buildmode=c-shared \
  -o "$here/opentile_go/_opentilego.so" \
  "$here/cshim"
echo "built: $here/opentile_go/_opentilego.so"
```
Then `chmod +x python/build_dev.sh`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd python && python -m pytest tests/test_import.py -q` (run from `python/` so the package dir is importable; if needed `PYTHONPATH=. python -m pytest tests/test_import.py -q`)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add python/pyproject.toml python/opentile_go/__init__.py python/opentile_go/py.typed \
  python/tests/__init__.py python/tests/test_import.py python/build_dev.sh python/.gitignore
git commit -m "feat(python): scaffold opentile_go package + dev build script"
```

---

### Task 2: Go shim — open/close/free/error + metadata JSON

**Files:**
- Create: `python/cshim/main.go`

- [ ] **Step 1: Write the shim**

`python/cshim/main.go`:
```go
// Command cshim is the C-ABI FFI shim exposing opentile-go to Python (ctypes).
// Built with `go build -buildmode=c-shared`. Cold structured metadata crosses
// as one JSON blob (ot_metadata_json); hot pixel/byte payloads cross as raw
// malloc'd buffers (Task 3). Handles are runtime/cgo.Handle tokens — never raw
// Go pointers.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	opentile "github.com/wsilabs/opentile-go"
)

func main() {}

// cstr copies a Go string into a C.malloc'd NUL-terminated buffer (Python frees
// it via ot_free).
func cstr(s string) *C.char { return C.CString(s) }

// setErr writes msg into *errOut as a malloc'd C string (if errOut != nil).
func setErr(errOut **C.char, msg string) {
	if errOut != nil {
		*errOut = cstr(msg)
	}
}

//export ot_open
func ot_open(path *C.char, errOut **C.char) C.uintptr_t {
	s, err := opentile.OpenFile(C.GoString(path))
	if err != nil {
		setErr(errOut, err.Error())
		return 0
	}
	return C.uintptr_t(cgo.NewHandle(s))
}

//export ot_close
func ot_close(h C.uintptr_t) {
	if h == 0 {
		return
	}
	hd := cgo.Handle(h)
	if s, ok := hd.Value().(*opentile.Slide); ok {
		_ = s.Close()
	}
	hd.Delete()
}

//export ot_free
func ot_free(p unsafe.Pointer) { C.free(p) }

// slideOf resolves a handle to its *Slide, or nil (+ sets err) on a bad handle.
func slideOf(h C.uintptr_t, errOut **C.char) *opentile.Slide {
	if h == 0 {
		setErr(errOut, "opentile: nil handle")
		return nil
	}
	s, ok := cgo.Handle(h).Value().(*opentile.Slide)
	if !ok {
		setErr(errOut, "opentile: invalid handle")
		return nil
	}
	return s
}

// metaLevel / metaDoc mirror the JSON schema in the spec (§4.2).
type metaLevel struct {
	Size        [2]int  `json:"size"`
	TileSize    [2]int  `json:"tile_size"`
	Grid        [2]int  `json:"grid"`
	Downsample  float64 `json:"downsample"`
	Overlapping bool    `json:"overlapping"`
}

type metaDoc struct {
	Format        string            `json:"format"`
	MPP           *[2]float64       `json:"mpp"`
	Magnification *float64          `json:"magnification"`
	Properties    map[string]string `json:"properties"`
	Levels        []metaLevel       `json:"levels"`
	Associated    []string          `json:"associated"`
}

//export ot_metadata_json
func ot_metadata_json(h C.uintptr_t, errOut **C.char) *C.char {
	s := slideOf(h, errOut)
	if s == nil {
		return nil
	}
	md := s.Metadata()
	doc := metaDoc{
		Format:     string(s.Format()),
		Properties: md.Properties,
	}
	if md.MPP.X != 0 || md.MPP.Y != 0 {
		doc.MPP = &[2]float64{md.MPP.X, md.MPP.Y}
	}
	if md.Magnification != 0 {
		m := md.Magnification
		doc.Magnification = &m
	}
	for _, lv := range s.Levels() {
		doc.Levels = append(doc.Levels, metaLevel{
			Size:        [2]int{lv.Size.W, lv.Size.H},
			TileSize:    [2]int{lv.TileSize.W, lv.TileSize.H},
			Grid:        [2]int{lv.Grid.W, lv.Grid.H},
			Downsample:  lv.Downsample,
			Overlapping: lv.Overlapping,
		})
	}
	for _, a := range s.AssociatedImages() {
		doc.Associated = append(doc.Associated, string(a.Type()))
	}
	b, err := json.Marshal(doc)
	if err != nil {
		setErr(errOut, "opentile: metadata marshal: "+err.Error())
		return nil
	}
	return cstr(string(b))
}
```

- [ ] **Step 2: Build it as c-shared and verify it fails first, then links**

Run: `cd python && ./build_dev.sh`
Expected: on a machine with the codec libs (this dev box has them via brew), it produces `opentile_go/_opentilego.so` and `opentile_go/_opentilego.h`. If it errors on a missing symbol, that's a build failure to fix before proceeding.

- [ ] **Step 3: Probe the built lib end-to-end via ctypes (inline)**

Pick a fixture path (a small SVS in `sample_files/`, e.g. `sample_files/svs/CMU-1-Small-Region.svs`). Run this one-liner from `python/`:
```bash
cd python && python - <<'PY'
import ctypes, glob, json, os
lib = ctypes.CDLL(glob.glob("opentile_go/_opentilego.*")[0])
lib.ot_open.restype = ctypes.c_size_t
lib.ot_open.argtypes = [ctypes.c_char_p, ctypes.POINTER(ctypes.c_char_p)]
lib.ot_metadata_json.restype = ctypes.c_void_p
lib.ot_metadata_json.argtypes = [ctypes.c_size_t, ctypes.POINTER(ctypes.c_char_p)]
lib.ot_free.argtypes = [ctypes.c_void_p]
lib.ot_close.argtypes = [ctypes.c_size_t]
err = ctypes.c_char_p()
p = os.path.abspath("../sample_files/svs/CMU-1-Small-Region.svs").encode()
h = lib.ot_open(p, ctypes.byref(err))
assert h != 0, err.value
ptr = lib.ot_metadata_json(h, ctypes.byref(err))
assert ptr, err.value
doc = json.loads(ctypes.cast(ptr, ctypes.c_char_p).value)
lib.ot_free(ptr); lib.ot_close(h)
print("format:", doc["format"], "| levels:", len(doc["levels"]), "| assoc:", doc["associated"])
assert doc["format"] and doc["levels"]
PY
```
Expected: prints the format + level count + associated names, no assertion error. (If `sample_files/` is absent, substitute any local slide path.)

- [ ] **Step 4: Commit**

```bash
git add python/cshim/main.go
git commit -m "feat(python): Go c-shared shim — open/close/free + metadata JSON"
```

---

### Task 3: ctypes layer (`_ffi.py`) + metadata pytest

**Files:**
- Create: `python/opentile_go/_ffi.py`, `python/tests/conftest.py`
- Test: `python/tests/test_metadata.py`

- [ ] **Step 1: Write the failing test**

`python/tests/conftest.py` (locate a fixture slide; skip the suite if none):
```python
import os
import pathlib
import pytest

def _find_slide():
    # OPENTILE_TESTDIR (CI corpus) or the repo's local sample_files.
    roots = []
    if os.environ.get("OPENTILE_TESTDIR"):
        roots.append(pathlib.Path(os.environ["OPENTILE_TESTDIR"]))
    roots.append(pathlib.Path(__file__).resolve().parents[2] / "sample_files")
    for root in roots:
        for pat in ("**/CMU-1-Small-Region.svs", "**/*.svs", "**/*.tiff", "**/*.ndpi"):
            hits = sorted(root.glob(pat))
            if hits:
                return str(hits[0])
    return None

@pytest.fixture(scope="session")
def slide_path():
    p = _find_slide()
    if not p:
        pytest.skip("no fixture slide found (set OPENTILE_TESTDIR or add sample_files/)")
    return p
```

`python/tests/test_metadata.py`:
```python
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && ./build_dev.sh && python -m pytest tests/test_metadata.py -q`
Expected: FAIL — `ModuleNotFoundError`/`AttributeError` (`opentile_go._ffi` / `OpenTileError` don't exist yet).

- [ ] **Step 3: Write `_ffi.py` and the exception**

Add to `python/opentile_go/__init__.py` (top, after the docstring/version):
```python
class OpenTileError(Exception):
    """Raised for any error surfaced by the opentile-go FFI shim."""
```

`python/opentile_go/_ffi.py`:
```python
"""ctypes binding to the opentile-go c-shared shim. Loads the bundled
_opentilego.<so|dylib|pyd>, declares the C-ABI, and marshals buffers to
Python bytes / numpy arrays. Pure Python — keeps the wheel Python-agnostic."""

import ctypes
import glob
import json
import os

from . import OpenTileError

_c_char_pp = ctypes.POINTER(ctypes.c_char_p)


def _load():
    here = os.path.dirname(os.path.abspath(__file__))
    hits = sorted(glob.glob(os.path.join(here, "_opentilego.*")))
    hits = [h for h in hits if not h.endswith(".h")]
    if not hits:
        raise OpenTileError(
            "opentile_go native library not built; run python/build_dev.sh (dev) "
            "or install a wheel"
        )
    return ctypes.CDLL(hits[0])


_lib = _load()

_lib.ot_open.restype = ctypes.c_size_t
_lib.ot_open.argtypes = [ctypes.c_char_p, _c_char_pp]
_lib.ot_close.argtypes = [ctypes.c_size_t]
_lib.ot_free.argtypes = [ctypes.c_void_p]
_lib.ot_metadata_json.restype = ctypes.c_void_p
_lib.ot_metadata_json.argtypes = [ctypes.c_size_t, _c_char_pp]


def _take_err(err):
    """Consume an err_out char* (free + return its text) or return None."""
    if err.value is None:
        return None
    msg = err.value.decode("utf-8", "replace")
    _lib.ot_free(ctypes.cast(err, ctypes.c_void_p))
    return msg


def _take_cstr(ptr):
    """Copy a malloc'd char* into a Python str and free it."""
    s = ctypes.cast(ptr, ctypes.c_char_p).value.decode("utf-8", "replace")
    _lib.ot_free(ptr)
    return s


def open_slide(path):
    err = ctypes.c_char_p()
    h = _lib.ot_open(os.fspath(path).encode("utf-8"), ctypes.byref(err))
    if h == 0:
        raise OpenTileError(_take_err(err) or "opentile: open failed")
    return h


def close_slide(handle):
    if handle:
        _lib.ot_close(handle)


def metadata(handle):
    err = ctypes.c_char_p()
    ptr = _lib.ot_metadata_json(handle, ctypes.byref(err))
    if not ptr:
        raise OpenTileError(_take_err(err) or "opentile: metadata failed")
    return json.loads(_take_cstr(ptr))
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd python && python -m pytest tests/test_metadata.py -q`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add python/opentile_go/_ffi.py python/opentile_go/__init__.py python/tests/conftest.py python/tests/test_metadata.py
git commit -m "feat(python): ctypes layer + metadata parsing + OpenTileError"
```

---

### Task 4: Go shim + ctypes — pixel & byte payloads (tile / decoded_tile / read_region)

**Files:**
- Modify: `python/cshim/main.go` (append pixel/byte exports)
- Modify: `python/opentile_go/_ffi.py` (declarations + marshaling)
- Test: `python/tests/test_pixels.py`

- [ ] **Step 1: Write the failing test**

`python/tests/test_pixels.py`:
```python
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && python -m pytest tests/test_pixels.py -q`
Expected: FAIL — `_ffi` has no `tile`/`decoded_tile`/`read_region`.

- [ ] **Step 3: Append pixel/byte exports to `main.go`**

Add the `decoder` import to `main.go`'s import block:
```go
	"github.com/wsilabs/opentile-go/decoder"
```
Then append this helper + the exports to `python/cshim/main.go`:
```go
// blitTight copies a decoder.Image's row-padded Pix into a freshly malloc'd,
// tightly-packed Height*Width*bands C buffer (dropping per-row stride padding)
// and returns the pointer, byte length, width, height, and band count. The
// caller frees the buffer via ot_free.
func blitTight(img *decoder.Image) (unsafe.Pointer, C.size_t, C.int, C.int, C.int) {
	bands := 3
	if img.Format == decoder.PixelFormatRGBA {
		bands = 4
	}
	rowBytes := img.Width * bands
	total := rowBytes * img.Height
	buf := C.malloc(C.size_t(total))
	dst := unsafe.Slice((*byte)(buf), total)
	for y := 0; y < img.Height; y++ {
		copy(dst[y*rowBytes:(y+1)*rowBytes], img.Pix[y*img.Stride:y*img.Stride+rowBytes])
	}
	return buf, C.size_t(total), C.int(img.Width), C.int(img.Height), C.int(bands)
}

func fmtOpt(rgba C.int) []opentile.DecodeOption {
	if rgba != 0 {
		return []opentile.DecodeOption{opentile.WithFormat(decoder.PixelFormatRGBA)}
	}
	return nil
}

//export ot_tile
func ot_tile(h C.uintptr_t, level, x, y C.int, out **C.uint8_t, outLen *C.size_t, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	b, err := lv.Tile(int(x), int(y))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf := C.malloc(C.size_t(len(b)))
	copy(unsafe.Slice((*byte)(buf), len(b)), b)
	*out = (*C.uint8_t)(buf)
	*outLen = C.size_t(len(b))
	return 0
}

//export ot_decoded_tile
func ot_decoded_tile(h C.uintptr_t, level, x, y, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	img, err := lv.DecodedTile(int(x), int(y), fmtOpt(rgba)...)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_read_region
func ot_read_region(h C.uintptr_t, level, x, y, w, ht, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	region := opentile.Region{Origin: opentile.Point{X: int(x), Y: int(y)}, Size: opentile.Size{W: int(w), H: int(ht)}}
	img, err := lv.ReadRegion(region, fmtOpt(rgba)...)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, ow, oh, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, ow, oh, bands
	return 0
}
```

- [ ] **Step 4: Add ctypes declarations + marshaling to `_ffi.py`**

Append to `python/opentile_go/_ffi.py`:
```python
import numpy as np

_u8pp = ctypes.POINTER(ctypes.POINTER(ctypes.c_uint8))
_sizep = ctypes.POINTER(ctypes.c_size_t)
_intp = ctypes.POINTER(ctypes.c_int)

_lib.ot_tile.restype = ctypes.c_int
_lib.ot_tile.argtypes = [ctypes.c_size_t, ctypes.c_int, ctypes.c_int, ctypes.c_int, _u8pp, _sizep, _c_char_pp]

_pix_argtypes = [ctypes.c_size_t] + [ctypes.c_int] * 4 + [_u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_decoded_tile.restype = ctypes.c_int
_lib.ot_decoded_tile.argtypes = [ctypes.c_size_t] + [ctypes.c_int] * 4 + [_u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_read_region.restype = ctypes.c_int
_lib.ot_read_region.argtypes = [ctypes.c_size_t] + [ctypes.c_int] * 6 + [_u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]


def _take_buf(ptr, n):
    """Copy an ot_free-owned uint8* buffer of length n into Python bytes, free it."""
    out = ctypes.string_at(ptr, n)
    _lib.ot_free(ctypes.cast(ptr, ctypes.c_void_p))
    return out


def _take_image(ptr, n, w, h, bands):
    """Copy a tight (h,w,bands) uint8 buffer into an owned numpy array, free it."""
    buf = ctypes.string_at(ptr, n)  # a copy
    _lib.ot_free(ctypes.cast(ptr, ctypes.c_void_p))
    return np.frombuffer(buf, dtype=np.uint8).reshape((h, w, bands)).copy()


def tile(handle, level, x, y):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n = ctypes.c_size_t()
    err = ctypes.c_char_p()
    if _lib.ot_tile(handle, level, x, y, ctypes.byref(out), ctypes.byref(n), ctypes.byref(err)) != 0:
        raise OpenTileError(_take_err(err) or "opentile: tile failed")
    return _take_buf(out, n.value)


def _decode_call(fn, handle, ints):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n, w, h, bands = ctypes.c_size_t(), ctypes.c_int(), ctypes.c_int(), ctypes.c_int()
    err = ctypes.c_char_p()
    args = [handle, *ints, ctypes.byref(out), ctypes.byref(n),
            ctypes.byref(w), ctypes.byref(h), ctypes.byref(bands), ctypes.byref(err)]
    if fn(*args) != 0:
        raise OpenTileError(_take_err(err) or "opentile: decode failed")
    return _take_image(out, n.value, w.value, h.value, bands.value)


def decoded_tile(handle, level, x, y, rgba=False):
    return _decode_call(_lib.ot_decoded_tile, handle, [level, x, y, 1 if rgba else 0])


def read_region(handle, level, x, y, w, h, rgba=False):
    return _decode_call(_lib.ot_read_region, handle, [level, x, y, w, h, 1 if rgba else 0])
```

- [ ] **Step 5: Rebuild + run the tests to verify they pass**

Run: `cd python && ./build_dev.sh && python -m pytest tests/test_pixels.py -q`
Expected: PASS (raw bytes non-empty; decoded arrays `(H,W,3)`/`(H,W,4)` uint8; region `(48,64,3)`).

- [ ] **Step 6: Commit**

```bash
git add python/cshim/main.go python/opentile_go/_ffi.py python/tests/test_pixels.py
git commit -m "feat(python): tile/decoded_tile/read_region FFI + numpy marshaling (stride-tight)"
```

---

### Task 5: Go shim + ctypes — associated images, thumbnail, macro, TIFF tags

**Files:**
- Modify: `python/cshim/main.go` (append exports)
- Modify: `python/opentile_go/_ffi.py` (declarations)
- Test: `python/tests/test_extras.py`

- [ ] **Step 1: Write the failing test**

`python/tests/test_extras.py`:
```python
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
        tags = _ffi.tiff_tags(h, 0)   # dict | None
        assert tags is None or isinstance(tags, list)
    finally:
        _ffi.close_slide(h)
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && python -m pytest tests/test_extras.py -q`
Expected: FAIL — `_ffi` has no `thumbnail`/`associated`/`tiff_tags`.

- [ ] **Step 3: Append exports to `main.go`**

```go
//export ot_thumbnail
func ot_thumbnail(h C.uintptr_t, maxW, maxH C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	img, err := s.RenderThumbnail(opentile.Size{W: int(maxW), H: int(maxH)})
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_macro
func ot_macro(h C.uintptr_t, maxW, maxH C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	img, err := s.RenderMacro(opentile.Size{W: int(maxW), H: int(maxH)})
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	buf, n, w, ht, bands := blitTight(img)
	*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
	return 0
}

//export ot_associated
func ot_associated(h C.uintptr_t, name *C.char, rgba C.int, out **C.uint8_t, outLen *C.size_t, outW, outH, outBands *C.int, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	want := C.GoString(name)
	var fmt decoder.PixelFormat
	if rgba != 0 {
		fmt = decoder.PixelFormatRGBA
	}
	for _, a := range s.AssociatedImages() {
		if string(a.Type()) == want {
			img, err := a.Decode(decoder.DecodeOptions{Format: fmt})
			if err != nil {
				setErr(errOut, err.Error())
				return -1
			}
			buf, n, w, ht, bands := blitTight(img)
			*out, *outLen, *outW, *outH, *outBands = (*C.uint8_t)(buf), n, w, ht, bands
			return 0
		}
	}
	setErr(errOut, "opentile: no associated image named "+want)
	return -1
}

//export ot_tiff_tags_json
func ot_tiff_tags_json(h C.uintptr_t, level C.int, out **C.char, errOut **C.char) C.int {
	s := slideOf(h, errOut)
	if s == nil {
		return -1
	}
	lv, err := s.Level(int(level))
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	tags, ok := lv.TIFFTags()
	if !ok {
		*out = nil // signal "no tags" to Python (null pointer, status 0)
		return 0
	}
	type jtag struct {
		Number uint16  `json:"number"`
		Name   string  `json:"name"`
		ASCII  *string `json:"ascii,omitempty"`
		Uints  []uint64 `json:"uints,omitempty"`
	}
	var out2 []jtag
	for _, t := range tags {
		jt := jtag{Number: t.Number, Name: t.Name}
		if a, ok := t.ASCII(); ok {
			jt.ASCII = &a
		} else if u, ok := t.Uints(); ok {
			jt.Uints = u
		}
		out2 = append(out2, jt)
	}
	b, err := json.Marshal(out2)
	if err != nil {
		setErr(errOut, err.Error())
		return -1
	}
	*out = cstr(string(b))
	return 0
}
```
(Requires the `decoder` import added in Task 4.)

- [ ] **Step 4: Add ctypes declarations to `_ffi.py`**

```python
_lib.ot_thumbnail.restype = ctypes.c_int
_lib.ot_thumbnail.argtypes = [ctypes.c_size_t, ctypes.c_int, ctypes.c_int, _u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_macro.restype = ctypes.c_int
_lib.ot_macro.argtypes = _lib.ot_thumbnail.argtypes
_lib.ot_associated.restype = ctypes.c_int
_lib.ot_associated.argtypes = [ctypes.c_size_t, ctypes.c_char_p, ctypes.c_int, _u8pp, _sizep, _intp, _intp, _intp, _c_char_pp]
_lib.ot_tiff_tags_json.restype = ctypes.c_int
_lib.ot_tiff_tags_json.argtypes = [ctypes.c_size_t, ctypes.c_int, _c_char_pp, _c_char_pp]


def thumbnail(handle, max_w, max_h):
    return _decode_call(_lib.ot_thumbnail, handle, [max_w, max_h])


def macro(handle, max_w, max_h):
    return _decode_call(_lib.ot_macro, handle, [max_w, max_h])


def associated(handle, name, rgba=False):
    out = ctypes.POINTER(ctypes.c_uint8)()
    n, w, h, bands = ctypes.c_size_t(), ctypes.c_int(), ctypes.c_int(), ctypes.c_int()
    err = ctypes.c_char_p()
    rc = _lib.ot_associated(handle, name.encode("utf-8"), 1 if rgba else 0,
                            ctypes.byref(out), ctypes.byref(n), ctypes.byref(w),
                            ctypes.byref(h), ctypes.byref(bands), ctypes.byref(err))
    if rc != 0:
        raise OpenTileError(_take_err(err) or "opentile: associated failed")
    return _take_image(out, n.value, w.value, h.value, bands.value)


def tiff_tags(handle, level):
    out = ctypes.c_char_p()
    err = ctypes.c_char_p()
    if _lib.ot_tiff_tags_json(handle, level, ctypes.byref(out), ctypes.byref(err)) != 0:
        raise OpenTileError(_take_err(err) or "opentile: tiff tags failed")
    if not out.value:
        return None
    return json.loads(_take_cstr(ctypes.cast(out, ctypes.c_void_p)))
```

- [ ] **Step 5: Rebuild + run the tests to verify they pass**

Run: `cd python && ./build_dev.sh && python -m pytest tests/test_extras.py -q`
Expected: PASS (thumbnail `(≤256,≤256,3)`; each associated image decodes; tiff_tags is a list or None).

- [ ] **Step 6: Commit**

```bash
git add python/cshim/main.go python/opentile_go/_ffi.py python/tests/test_extras.py
git commit -m "feat(python): associated/thumbnail/macro/tiff-tags FFI + ctypes"
```

---

### Task 6: The numpy-first facade (`Slide` / `Level`)

**Files:**
- Create: `python/opentile_go/slide.py`
- Modify: `python/opentile_go/__init__.py` (export `Slide`, `Level`)
- Test: `python/tests/test_facade.py`

- [ ] **Step 1: Write the failing test**

`python/tests/test_facade.py`:
```python
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && python -m pytest tests/test_facade.py -q`
Expected: FAIL — `Slide`/`Level` not importable.

- [ ] **Step 3: Write `slide.py` + exports**

`python/opentile_go/slide.py`:
```python
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

    def tile(self, x, y):
        return _ffi.tile(self._slide._h, self._i, x, y)

    def decoded_tile(self, x, y, rgba=False):
        return _ffi.decoded_tile(self._slide._h, self._i, x, y, rgba=rgba)

    def read_region(self, x, y, w, h, rgba=False):
        return _ffi.read_region(self._slide._h, self._i, x, y, w, h, rgba=rgba)

    @property
    def tiff_tags(self):
        return _ffi.tiff_tags(self._slide._h, self._i)


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

    def _associated(self, name):
        return _ffi.associated(self._h, name)

    def thumbnail(self, width=None, height=None):
        return _ffi.thumbnail(self._h, width or 0, height or 0)

    def macro(self, width=None, height=None):
        return _ffi.macro(self._h, width or 0, height or 0)

    def close(self):
        if not self._closed:
            _ffi.close_slide(self._h)
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
```

Add to `python/opentile_go/__init__.py` (after `OpenTileError`):
```python
from .slide import Slide, Level  # noqa: E402

__all__ = ["Slide", "Level", "OpenTileError", "__version__"]
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd python && python -m pytest tests/test_facade.py -q`
Expected: PASS (facade roundtrip; use-after-close raises).

- [ ] **Step 5: Commit**

```bash
git add python/opentile_go/slide.py python/opentile_go/__init__.py python/tests/test_facade.py
git commit -m "feat(python): numpy-first Slide/Level facade + lazy associated-images map"
```

---

### Task 7: Byte-parity test vs the Go reader + full-suite green + README

**Files:**
- Create: `python/tests/golden/gen_region.go` (a `//go:build ignore` helper), `python/tests/test_parity.py`, `python/README.md`

- [ ] **Step 1: Write the parity helper + failing test**

`python/tests/golden/gen_region.go` (a standalone Go program that prints a region's tight RGB bytes to stdout, run via `go run`):
```go
//go:build ignore

// Emits the tight RGB888 bytes of level-0 region (x,y,w,h) of a slide to stdout,
// as the parity oracle for the Python read_region path.
package main

import (
	"os"
	"strconv"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

func main() {
	path := os.Args[1]
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	x, y, w, h := atoi(os.Args[2]), atoi(os.Args[3]), atoi(os.Args[4]), atoi(os.Args[5])
	s, err := opentile.OpenFile(path)
	if err != nil {
		panic(err)
	}
	defer s.Close()
	lv, err := s.Level(0)
	if err != nil {
		panic(err)
	}
	img, err := lv.ReadRegion(opentile.Region{Origin: opentile.Point{X: x, Y: y}, Size: opentile.Size{W: w, H: h}})
	if err != nil {
		panic(err)
	}
	bands := 3
	if img.Format == decoder.PixelFormatRGBA {
		bands = 4
	}
	row := img.Width * bands
	for r := 0; r < img.Height; r++ {
		os.Stdout.Write(img.Pix[r*img.Stride : r*img.Stride+row])
	}
}
```

`python/tests/test_parity.py`:
```python
import subprocess
import pathlib
import numpy as np
from opentile_go import Slide

def test_read_region_byte_parity_with_go(slide_path):
    x, y, w, h = 0, 0, 64, 48
    gen = pathlib.Path(__file__).parent / "golden" / "gen_region.go"
    repo = pathlib.Path(__file__).resolve().parents[2]
    ref = subprocess.run(
        ["go", "run", str(gen), slide_path, str(x), str(y), str(w), str(h)],
        cwd=str(repo), capture_output=True, check=True,
    ).stdout
    with Slide(slide_path) as s:
        arr = s.levels[0].read_region(x, y, w, h, rgba=False)
    assert arr.tobytes() == ref, "python read_region bytes differ from Go reader"
    assert arr.shape == (h, w, 3)
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && python -m pytest tests/test_parity.py -q`
Expected: FAIL initially only if the facade region path is wrong — but since Task 6 is green, this should surface any stride/offset bug. If Task 4–6 are correct it may PASS immediately; the value is the guard. Run it and confirm behaviour; if it fails, the stride copy or region mapping is wrong — fix in `blitTight`/`ot_read_region`.

- [ ] **Step 3: Ensure it passes; write the README**

`python/README.md`:
```markdown
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

## Building from source (development)

Requires Go 1.23+ and the codec libraries (jpeg-turbo, openjpeg, openjph,
jpeg-xl, libavif, libwebp) — on macOS: `brew install jpeg-turbo openjpeg openjph
jpeg-xl libavif webp pkg-config`.

```sh
cd python && ./build_dev.sh && pip install -e . && pytest
```

Wheels bundle all six codecs statically (no system install needed).
```

- [ ] **Step 4: Run the ENTIRE Python suite green**

Run: `cd python && ./build_dev.sh && python -m pytest -q`
Expected: PASS — all of test_import, test_metadata, test_pixels, test_extras, test_facade, test_parity.

Also confirm the Go side still builds/vets clean (the shim is a new package under the module):
Run: `go build ./... && go vet ./python/cshim/`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add python/tests/golden/gen_region.go python/tests/test_parity.py python/README.md
git commit -m "test(python): byte-parity vs Go reader + README; Phase 1 vertical slice complete"
```

---

## Self-review notes (for the executor)

- **Spec coverage (Phase 1):** §3 architecture → all tasks; §4.1 handle/close/free → T2; §4.2 metadata JSON → T2/T3; §4.3 pixels/bytes + **stride-tight copy** → T4 (`blitTight`), extras → T5; §5 facade → T6; §7 testing incl. **byte-parity** → T3–T7. §6 packaging + §8 Phase 2 (wheels/CI/PyPI) are **out of this plan** (separate Phase 2 plan).
- **The stride copy (`blitTight`) is the highest-risk correctness point** — the T7 byte-parity test is its guard. Do not skip it.
- **Local build links codecs dynamically** (brew/apt). Do NOT wire vcpkg here — that's Phase 2.
- **Handle type:** `ot_open` returns `uintptr` (ctypes `c_size_t`); every call passes it back as `c_size_t`. Keep this consistent across `main.go` and `_ffi.py`.
- The `python/` tree lives inside the Go module but `cshim` is `package main` (c-shared) — `go build ./...` includes it; that's intended.

## Completion & next

After Task 7 is green, **Phase 1 is done** (a working, locally-tested binding). Do **not** finish/merge the branch yet if Phase 2 will continue on it — otherwise use **superpowers:finishing-a-development-branch**. Then write the **Phase 2 plan** (writing-plans): vcpkg-static build integration in `pyproject`, the five-target `cibuildwheel` matrix, `.github/workflows/wheels.yml`, and PyPI OIDC trusted publishing — the packaging/CI subsystem, verified empirically on runners like the sub-project-1 Windows loop.
