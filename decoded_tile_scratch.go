package opentile

import (
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// scratchKey identifies a buffer size class for the tile-Image
// sync.Pool. Two formats (RGB, RGBA) × N tile sizes per format mean
// we key on (W, H, Format) — but in practice every level of a single
// Slide uses the same TileSize, so the per-Slide active key set is
// small (typically 1 or 2).
type scratchKey struct {
	w, h   int
	format decoder.PixelFormat
}

// tileScratchPool is the package-level pool of *decoder.Image
// instances reused as per-tile decode-Into scratch buffers in
// imageReadRegionImpl. Module-level (not per-Slide) so multiple
// Slides sharing a layout share buffers transparently.
//
// Members are stateless after each blit; sync.Pool auto-shrinks
// under GC pressure. No Slide.Close drain required.
//
// Added in v0.29.
var tileScratchPool sync.Map // scratchKey -> *sync.Pool of *decoder.Image

// borrowTileScratch returns a *decoder.Image of (w, h, format) from
// the pool, or allocates a fresh one if the pool is empty. The
// returned Image's Pix is NOT zeroed — caller must fully overwrite
// before reading.
//
// Added in v0.29.
func borrowTileScratch(w, h int, format decoder.PixelFormat) *decoder.Image {
	key := scratchKey{w, h, format}
	pi, _ := tileScratchPool.LoadOrStore(key, &sync.Pool{
		New: func() any {
			return decoder.NewImageFormat(w, h, format)
		},
	})
	return pi.(*sync.Pool).Get().(*decoder.Image)
}

// returnTileScratch returns a scratch Image to the pool. Safe with
// nil. Caller MUST stop reading from the Image after Return.
//
// Added in v0.29.
func returnTileScratch(img *decoder.Image) {
	if img == nil {
		return
	}
	key := scratchKey{img.Width, img.Height, img.Format}
	pi, ok := tileScratchPool.Load(key)
	if !ok {
		return // unknown key; let GC reclaim
	}
	pi.(*sync.Pool).Put(img)
}
