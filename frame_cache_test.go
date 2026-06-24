package opentile

import (
	"errors"
	"sync"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func frame(w, h int, fill byte) *decoder.Image {
	img := decoder.NewImageFormat(w, h, decoder.PixelFormatRGB)
	for i := range img.Pix {
		img.Pix[i] = fill
	}
	return img
}

func TestDecodedFrameCacheLoadsOnceThenHits(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{image: 0, level: 0, col: 1, row: 2, format: decoder.PixelFormatRGB}
	var loads int
	load := func() (*decoder.Image, error) { loads++; return frame(4, 4, 9), nil }

	a, err := c.getOrLoad(key, load)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.getOrLoad(key, load)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loads = %d, want 1 (second call must hit)", loads)
	}
	if a != b {
		t.Fatal("hit must return the same cached image pointer")
	}
}

func TestDecodedFrameCacheBytesBound(t *testing.T) {
	// Each 100x100 RGB frame is 30000 bytes; budget holds ~3.
	c := newDecodedFrameCache(100 << 10) // 102400 bytes
	for i := 0; i < 10; i++ {
		k := frameCacheKey{col: i, format: decoder.PixelFormatRGB}
		if _, err := c.getOrLoad(k, func() (*decoder.Image, error) { return frame(100, 100, byte(i)), nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.len(); got == 0 || got*30000 > 102400 {
		t.Fatalf("len = %d (%d bytes), want bounded under 102400", got, got*30000)
	}
}

func TestDecodedFrameCacheErrorNotCached(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{col: 5, format: decoder.PixelFormatRGB}
	boom := errors.New("boom")
	if _, err := c.getOrLoad(key, func() (*decoder.Image, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	var second int
	if _, err := c.getOrLoad(key, func() (*decoder.Image, error) { second++; return frame(2, 2, 1), nil }); err != nil {
		t.Fatal(err)
	}
	if second != 1 {
		t.Fatal("after an error the entry must not be cached; load should re-run")
	}
}

func TestDecodedFrameCacheConcurrentSameKeyLoadsOnce(t *testing.T) {
	c := newDecodedFrameCache(64 << 20)
	key := frameCacheKey{col: 7, format: decoder.PixelFormatRGB}
	release := make(chan struct{})
	var mu sync.Mutex
	loads := 0
	load := func() (*decoder.Image, error) {
		mu.Lock()
		loads++
		mu.Unlock()
		<-release // hold the load so concurrent callers must wait on the promise
		return frame(8, 8, 3), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.getOrLoad(key, load) }()
	}
	// Give goroutines time to all block on the promise, then release.
	for c.inflightCountForTest() == 0 {
	}
	close(release)
	wg.Wait()
	if loads != 1 {
		t.Fatalf("loads = %d, want 1 (promise pattern must dedupe)", loads)
	}
}

func TestDecodedFrameCacheDoesNotEvictInflight(t *testing.T) {
	c := newDecodedFrameCache(60 << 10) // ~2 frames of 100x100 (30000 ea)
	hold := make(chan struct{})
	started := make(chan struct{})
	inflightKey := frameCacheKey{col: 99, format: decoder.PixelFormatRGB}

	go func() {
		_, _ = c.getOrLoad(inflightKey, func() (*decoder.Image, error) {
			close(started)
			<-hold
			return frame(100, 100, 1), nil
		})
	}()
	<-started // inflightKey is now in-flight, occupying a slot but 0 accounted bytes

	// Flood with completed frames to blow the byte budget while inflightKey is held.
	for i := 0; i < 6; i++ {
		k := frameCacheKey{col: i, format: decoder.PixelFormatRGB}
		_, _ = c.getOrLoad(k, func() (*decoder.Image, error) { return frame(100, 100, byte(i)), nil })
	}
	if !c.presentForTest(inflightKey) {
		t.Fatal("in-flight entry must not be evicted")
	}
	close(hold)
}
