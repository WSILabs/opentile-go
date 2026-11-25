# SVS corrupt-edge reconstruct — port notes (v0.4 R4)

Drafted in v0.4 Task 4 (R4 mechanism audit). The notes record the
upstream algorithm we're porting, the Go-side dependencies it pulls
in, the byte-parity bar (set by v0.4 Task 1), and the open questions
that shape Theme C / D / E. The plan's task bodies for R4
(Tasks 14-16) reference this file.

## 1. Upstream algorithm — `_get_scaled_tile` chain

Source: `opentile/formats/svs/svs_image.py:248-396`.

### 1.1 Detection (`_detect_corrupt_edges`, lines 267-291)

Walks the right and bottom edges of the level's tile grid. A tile is
**corrupt** iff its `databytecounts[frame_index] == 0` (zero-length
tile entry — the most common Aperio scanner failure mode at edges
of reduced-resolution levels).

```python
def _detect_corrupt_edges(self) -> Tuple[bool, bool]:
    if self._parent is None:
        return False, False
    right_edge  = Region(Point(W-1, 0),     Size(1, H-1))
    bottom_edge = Region(Point(0,   H-1),   Size(W-1, 1))
    return _detect_corrupt_edge(right_edge), _detect_corrupt_edge(bottom_edge)
```

Note: detection only runs when `self._parent is not None`. The
baseline level (pyramid_index 0) has no parent and so never
reconstructs anything; that's correct because the parent is the
data source.

The right_edge / bottom_edge regions exclude the bottom-right
corner from each (`Size(1, H-1)` / `Size(W-1, 1)`). This avoids
double-counting the corner tile. Our port should preserve that.

### 1.2 Reconstruct (`_get_scaled_tile`, lines 301-372)

Five steps:

1. **Compute scale** = `2^(this.pyramid_index - parent.pyramid_index)`.
   For typical SVS pyramids this is 4 (each level halves both axes).
   Generalized — we don't hardcode 2× because some SVS files skip
   levels.

2. **Walk the parent's `scale × scale` tile region** at `tile_point`.
   Each parent tile is decoded to a numpy array via
   `parent.get_decoded_tiles([...])`, which calls
   `tifffile.TiffPage.decode(frame, frame_index)`.

3. **Paste decoded tiles into a scratch raster** of shape
   `(W*scale, H*scale, samples_per_pixel)` where `(W, H) =
   self.tile_size`. Each parent tile occupies a `tile_size`-sized
   slot via numpy slicing:
   ```python
   image_data[
       y*W : (y+1)*W,
       x*H : (x+1)*H,
   ] = next(decoded_tiles)
   ```

4. **BILINEAR resize** the scratch raster down to `tile_size` via
   `Image.fromarray(image_data).resize(tile_size, BILINEAR)`. Pillow's
   default BILINEAR is a 2-tap separable filter (horizontal pass then
   vertical pass), 8-bit-per-channel kernel weighted by the resampler
   builder.

5. **Re-encode** in the level's compression:
   - JPEG → `imagecodecs.jpeg8_encode(np.array(image), level=95,
     colorspace=<JPEG8.CS.RGB|YCbCr>, subsampling=page.subsampling,
     lossless=False, bitspersample=bit_depth)`. Defaults differ from
     the JP2K case — note `level=95`, not 80.
   - JP2K → `imagecodecs.jpeg2k_encode(...)` with the same options
     v0.4 Task 1 verified determinism for (`level=80,
     codecformat=J2K, colorspace=SRGB, mct=True, reversible=False,
     bitspersample=8`).
   - Anything else → `NonSupportedCompressionException`.

### 1.3 Caching (`_get_fixed_tile`, lines 374-396)

Per-level dict keyed by tile `Point`. First call computes the
reconstruct via `_get_scaled_tile`; subsequent calls at the same
position return the cached bytes. The cache is on the level
instance so it lives as long as the Tiler.

### 1.4 Dispatch (`SvsTiledImage.get_tile`, lines 398-403)

```python
def get_tile(self, tile_position):
    tile_point = Point.from_tuple(tile_position)
    if self._tile_is_corrupt(tile_point):
        return self._get_fixed_tile(tile_point)
    return super().get_tile(tile_position)
```

`_tile_is_corrupt` is positional only — checks whether
`tile_point.x == W-1 AND right_edge_corrupt` (or analogous for
bottom). It does NOT re-check the per-tile databytecounts inside
the edge — once the edge is flagged, every tile on that edge gets
reconstructed even if some happened to be valid.

That's a deliberate upstream simplification (the symptoms of
"scrambled image data" can't be detected from databytecounts alone,
and a single corrupt tile usually indicates the whole edge is
suspect).

## 2. Go-side dependencies

### 2.1 Decode parent tiles to raster

Currently `internal/jpegturbo` exposes only `Crop` and
`CropWithBackground`. Reconstruct needs **decode** to get parent
tiles into an `image.RGBA` (or equivalent). Two paths:

- **JPEG**: Add `internal/jpegturbo.Decode(src []byte) (image, w, h,
  err)` mirroring libjpeg-turbo's `tjDecompress2` flow. Output:
  interleaved RGB or RGBA8 with the slide's photometric. Goes into
  Theme C (alongside the libopenjp2 work).
- **JP2K**: `internal/openjp2.Decode` from Theme C Task 11. Already
  scoped.

### 2.2 Paste tiles into a scratch raster

Pure Go via stdlib `image` package:
```go
raster := image.NewRGBA(image.Rect(0, 0, W*scale, H*scale))
// for each parent tile (px, py):
draw.Draw(raster, image.Rect(px*W, py*H, (px+1)*W, (py+1)*H),
          parentTile, image.Point{0, 0}, draw.Src)
```

No new dep — `image/draw` is stdlib.

### 2.3 BILINEAR resize

**This is the v0.4 Task 1 byte-parity decision point.** Task 1
confirmed `imagecodecs.jpeg2k_encode` is byte-deterministic, so R4's
done-when bar is byte-parity with Python opentile. Byte-parity
post-encode requires byte-equal pixel input, which means our
BILINEAR resize must match Pillow's `Image.resize(BILINEAR)` byte-
for-byte.

Two options examined:
- **Pillow port** — port `Pillow/src/libImaging/Resample.c`'s
  `ImagingResampleHorizontal_8bpc` + `ImagingResampleVertical_8bpc`
  (~200 lines C, mechanical port to Go). Byte-equivalent.
- **`golang.org/x/image/draw.BiLinear`** — different filter
  builder; almost certainly produces different bytes from Pillow
  even on identical input.

**Decision: port Pillow.** Theme D Task 13 lands an
`internal/imageops/bilinear.go` mirroring Pillow's implementation
plus a parity test against Pillow output on a known input.

The Pillow port is the v0.4 R4 critical-path dependency. If at any
point during the port we hit a Pillow algorithm complexity that
makes byte-equivalence impractical (it shouldn't — Pillow's
BILINEAR is well-documented and finite), the fallback is to
re-classify R4 as pixel-parity-only and document that change. The
JP2K determinism gate would still hold; we'd just relax the bar to
"numpy.array_equal of decoded reconstructed tiles."

### 2.4 Re-encode

JPEG path needs **`internal/jpegturbo.Encode`** — a NEW addition
to v0.3's `Crop`/`CropWithBackground` API. Mirrors imagecodecs'
`jpeg8_encode` options:
- `level=95` (note: differs from JP2K's level=80)
- `colorspace`: RGB or YCbCr based on page photometric
- `subsampling`: derived from the page's chroma subsampling
- `lossless=False`, `bitspersample=8`

libjpeg-turbo's `tjCompress2` is the underlying call. Same cgo
pattern as the existing helpers.

JP2K path: `internal/openjp2.Encode` from Theme C Task 12. Already
scoped.

## 3. Wiring

### 3.1 Parent-level threading

Upstream `SvsTiledImage` takes a `parent: Optional["SvsTiledImage"]`
argument. The tiler constructs them in pyramid order:
```python
def _get_level(self, level, page=0):
    if level > 0:
        parent = self._get_level(level - 1, page)
    else:
        parent = None
    return SvsTiledImage(..., parent)
```

Our `formats/svs/svs.go::Open` currently builds level structs
without any parent reference. The port adds:
- A `parent *tiledImage` field to `tiledImage`.
- Construction logic in `Open` that threads each level's parent.

The reconstruct path holds the parent only via this pointer —
parent tiles are decoded on demand, not pre-loaded.

### 3.2 Dispatch in `Tile()`

The current `tiledImage.Tile` (post-v0.3 cd850a0) errors with
`ErrCorruptTile` on zero-length tile entries via the `indexOf`
helper. Reconstruct adds a fallback:

```go
func (l *tiledImage) Tile(x, y int) ([]byte, error) {
    idx, err := l.indexOf(x, y)
    if errors.Is(err, opentile.ErrCorruptTile) && l.cfg.SVSReconstructEdges() {
        return l.reconstructTile(x, y)
    }
    if err != nil {
        return nil, err
    }
    // ... existing path ...
}
```

The opt-in flag (`Config.WithSVSReconstructEdges(bool)`, default
`false` per §4 below) gates the fallback. When the flag is off,
behavior matches v0.3 exactly — corrupt tiles still error.

### 3.3 Per-level cache

Mirror upstream's `_fixed_tiles` dict with a Go map keyed by
`opentile.TilePos`:
```go
type tiledImage struct {
    // ... existing fields ...
    parent       *tiledImage
    fixedTilesMu sync.Mutex
    fixedTiles   map[opentile.TilePos][]byte
}
```

Lazy population on first `reconstructTile` call per position.

## 4. Open questions and decisions

### 4.1 Opt-in vs always-on

Lean **opt-in for v0.4 (`Config.WithSVSReconstructEdges(false)` default)**, default-on by v0.5.

Reasoning: the v0.3 contract returns `ErrCorruptTile` on edge tiles.
Flipping the default to "silently substitute reconstructed bytes"
is observable behavior change for downstream consumers who may
have been counting on the error to signal slide damage. Opt-in
preserves v0.3 semantics for existing callers; consumers who want
the reconstruct opt-in.

We can revisit the default in v0.5 once external users (if any)
have had a chance to opine.

### 4.2 Test fixture availability

**None of our local SVS slides have corrupt edges** (verified
2026-04-26 via Python opentile's `level.right_edge_corrupt` /
`bottom_edge_corrupt` properties — see commit log for the audit).
CMU-1, CMU-1-Small-Region, JP2K-33003-1, scan_620, svs_40x_bigtiff
all clean.

**Implication:** the v0.4 R4 test path needs a **synthetic** SVS
TIFF builder that emits zero-length tile entries on a chosen edge.
We already have `buildSVSTIFF` in `formats/svs/tiled_test.go`; it
just needs an option for "deliberately corrupt the right-edge / 
bottom-edge tile entries." This is straightforward — set the
relevant `TileByteCounts[idx]` entries to 0 and skip writing those
tile bytes.

The test asserts:
- Without the `WithSVSReconstructEdges` flag, `Tile(corrupt_x, y)`
  returns `ErrCorruptTile` (v0.3 contract).
- With the flag, `Tile(corrupt_x, y)` returns non-empty bytes that
  decode to a non-zero-luminance image (the reconstructed
  downscale of the parent).

For Python-parity testing, we can construct a synthetic SVS,
shell out to Python opentile, compare. Same shape as the parity
oracle; opt-in via the same env vars.

### 4.3 Multi-level reconstruct

Upstream's reconstruct only fires on levels with a parent. Our
port matches. But there's a subtle case: if level N has a corrupt
edge AND level N-1 also has a corrupt edge AND the corrupt-edge
region of N maps onto the corrupt region of N-1, the reconstruct
of N pulls from N-1's reconstruct. Upstream handles this via
recursion — `parent.get_decoded_tiles` calls
`parent.get_tile(...)` which itself dispatches through
`_tile_is_corrupt → _get_fixed_tile`.

Our port gets the same behavior for free if `reconstructTile`
calls `parent.Tile(...)` (not `parent.indexOf(...)` directly).
Recursion bottoms out at the level whose parent is nil
(pyramid_index == 0). Worth noting in code comments.

### 4.4 Unsupported-compression handling

Upstream raises `NonSupportedCompressionException` for any
non-JPEG / non-JP2K compression. Our port should return a wrapped
opentile error:
```go
return nil, &opentile.TileError{
    Level: l.index, X: x, Y: y,
    Err: fmt.Errorf("svs: cannot reconstruct corrupt tile at level with %s compression: %w",
        l.compression, opentile.ErrCorruptTile),
}
```

This matters for LZW / Deflate slides we haven't seen yet.
v0.3 Task 20 (L3) added a `Compression=999` synthetic test; the
reconstruct path for unrecognized compression needs an analog.

## 5. Theme dependency graph

```
Task 1 (JP2K determinism gate)
    │
    ├─→ Task 4 (this doc) ─→ Task 13 (BILINEAR resize, Pillow port)
    │                              │
    │                              ↓
    └─→ Task 12 (openjp2 Encode)   ├─→ Task 14 (corrupt-edge detect)
        Task 11 (openjp2 Decode)   │       │
        + new jpegturbo.Decode     │       ↓
        + new jpegturbo.Encode ────┘   Task 15 (reconstruct via _get_scaled_tile)
                                            │
                                            ↓
                                       Task 16 (synthetic R4 fixture +
                                                integration test +
                                                parity assertion)
```

Theme C is the bottleneck — three new cgo entry points
(jpegturbo.Decode, jpegturbo.Encode, openjp2.Decode/Encode) plus
the Pillow port — before any of Theme E's reconstruct work can
start. Each is mechanical but adds up; budget 1-2 sessions per
new cgo function.
