package leicascn

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
)

// blankTileQuality is the JPEG quality factor used to encode blank
// fill tiles for inter-region SCN gaps. Real SCN tile bytes don't
// carry an ImageDescription quality marker (the JPEG comes from the
// scanner's libtiff encoder), so we pick a high-quality value that
// keeps the synthesised white tiles visually consistent when
// adjacent to real tissue in a viewer.
const blankTileQuality = 95

// blankTileKey caches by tile size only (SCN has no per-slide
// white-point setting; gaps are pure white 255).
type blankTileKey struct{ w, h int }

var (
	blankTileCacheMu sync.Mutex
	blankTileCache   = map[blankTileKey][]byte{}
)

// blankTile returns a JPEG-encoded tile of size w×h filled with
// pure-white pixels (R=G=B=255). The result is cached per (w, h)
// — first call encodes; subsequent calls return the cached bytes.
//
// Returns an error if w or h is non-positive. The returned byte
// slice is read-only — callers must not mutate it (defensive copy
// happens in the consumer at the AssociatedImage / TileBytes
// boundary).
//
// Owner directive (sealed Q6): inter-region "gap" tiles must look
// like normal tile bytes to consumers — the discontinuous-scanning
// detail is hidden. blankTile is the synthesis primitive that
// implements that contract.
func blankTile(w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("leicascn: blankTile: invalid dimensions %dx%d", w, h)
	}
	key := blankTileKey{w: w, h: h}

	blankTileCacheMu.Lock()
	if b, ok := blankTileCache[key]; ok {
		blankTileCacheMu.Unlock()
		return b, nil
	}
	blankTileCacheMu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: blankTileQuality}); err != nil {
		return nil, fmt.Errorf("leicascn: blankTile: jpeg.Encode: %w", err)
	}
	out := buf.Bytes()

	blankTileCacheMu.Lock()
	blankTileCache[key] = out
	blankTileCacheMu.Unlock()
	return out, nil
}
