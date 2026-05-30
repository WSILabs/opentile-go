package opentile

import (
	"sync"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestTileScratchPoolReuse(t *testing.T) {
	// sync.Pool is documented to drop items at any time (especially
	// under GC pressure or -race), so asserting exact pointer equality
	// on a Put→Get cycle is flaky across platforms. Instead, run many
	// borrow-return cycles and assert that the pool produces at least
	// some reuse — i.e., the number of distinct pointers seen is less
	// than the number of borrows. Bug-class this test catches: a
	// refactor that accidentally bypasses the pool entirely.
	const iterations = 32
	seen := map[*decoder.Image]bool{}
	for i := 0; i < iterations; i++ {
		s := borrowTileScratch(256, 256, decoder.PixelFormatRGB)
		seen[s] = true
		returnTileScratch(s)
	}
	if len(seen) >= iterations {
		t.Fatalf("expected sync.Pool reuse across %d borrows; got %d distinct pointers (no reuse — pool not engaged)", iterations, len(seen))
	}
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
