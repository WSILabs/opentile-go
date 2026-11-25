package bif

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
)

// blankTileQuality matches the JPEG quality factor used by VENTANA DP 200
// scanners on real tiles (per ImageDescription `quality=95`). Using the same
// quality keeps the synthesized blank tiles visually consistent with real tiles
// when adjacent in a viewer.
const blankTileQuality = 95

// blankTileKey is the cache key for blankTile. Tile dimensions are typically
// 1024x1024 (Ventana-1) or 1024x1360 (OS-1); white is the ScanWhitePoint XMP
// attribute (235 on Ventana-1) or 255 default for legacy iScan.
type blankTileKey struct {
	w, h  int
	white uint8
}

var (
	blankTileCacheMu sync.Mutex
	blankTileCache   = map[blankTileKey][]byte{}
)

// blankTile returns a JPEG-encoded tile of size w×h filled with the given
// white-point luminance value (R, G, B all set to white). The result is cached
// per (w, h, white) tuple — first call encodes; subsequent calls return the
// cached bytes.
//
// Returns an error if w or h is non-positive. The returned byte slice is
// read-only — callers must not mutate it.
func blankTile(w, h int, white uint8) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("bif: blankTile: invalid dimensions %dx%d", w, h)
	}
	key := blankTileKey{w: w, h: h, white: white}

	blankTileCacheMu.Lock()
	if b, ok := blankTileCache[key]; ok {
		blankTileCacheMu.Unlock()
		return b, nil
	}
	blankTileCacheMu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill := color.NRGBA{R: white, G: white, B: white, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: blankTileQuality}); err != nil {
		return nil, fmt.Errorf("bif: blankTile: jpeg.Encode: %w", err)
	}
	out := buf.Bytes()

	blankTileCacheMu.Lock()
	// Re-check (another goroutine might have populated it while we encoded).
	if b, ok := blankTileCache[key]; ok {
		blankTileCacheMu.Unlock()
		return b, nil
	}
	blankTileCache[key] = out
	blankTileCacheMu.Unlock()
	return out, nil
}
