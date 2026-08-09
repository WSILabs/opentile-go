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
