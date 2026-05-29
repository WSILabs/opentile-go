package opentile

import (
	"sync"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestTileScratchPoolReuse(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	returnTileScratch(a)
	b := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	if a != b {
		t.Fatal("expected sync.Pool to reuse the returned scratch on next Borrow")
	}
	returnTileScratch(b)
}

func TestTileScratchPoolSizeKeyed(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	c := borrowTileScratch(512, 512, decoder.PixelFormatRGB)
	if a == c {
		t.Fatal("different-sized scratches should not share buffers")
	}
	returnTileScratch(a)
	returnTileScratch(c)
}

func TestTileScratchPoolFormatKeyed(t *testing.T) {
	a := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
	c := borrowTileScratch(256, 256, decoder.PixelFormatRGBA)
	if a == c {
		t.Fatal("different-format scratches should not share buffers")
	}
	returnTileScratch(a)
	returnTileScratch(c)
}

func TestTileScratchPoolConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
				returnTileScratch(s)
			}
		}()
	}
	wg.Wait()
}

func TestTileScratchPoolReturnNilSafe(t *testing.T) {
	// Must not panic.
	returnTileScratch(nil)
}
