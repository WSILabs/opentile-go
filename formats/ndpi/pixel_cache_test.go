package ndpi

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsilabs/opentile-go/decoder"
)

func mkImg(w, h int) *decoder.Image {
	return &decoder.Image{
		Width: w, Height: h,
		Format: decoder.PixelFormatRGB,
		Stride: w * 3,
		Pix:    make([]byte, w*h*3),
	}
}

func TestPixelCacheHitAfterMiss(t *testing.T) {
	c := newPixelFrameCache(4)
	k := frameKey{0, 0, 16, 16}
	calls := 0
	load := func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	}
	a, err := c.getOrLoad(k, load)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.getOrLoad(k, load)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("cache returned different pointers; expected hit reuse")
	}
	if calls != 1 {
		t.Fatalf("load called %d times; want 1 (one miss + one hit)", calls)
	}
}

func TestPixelCacheEvictsOldest(t *testing.T) {
	c := newPixelFrameCache(2)
	keys := []frameKey{
		{0, 0, 16, 16},
		{16, 0, 16, 16},
		{32, 0, 16, 16},
	}
	for _, k := range keys {
		_, err := c.getOrLoad(k, func() (*decoder.Image, error) {
			return mkImg(16, 16), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// keys[0] should now be evicted; loading it again should miss.
	calls := 0
	_, err := c.getOrLoad(keys[0], func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("evicted key reload: load called %d times; want 1", calls)
	}
}

func TestPixelCachePromiseWait(t *testing.T) {
	c := newPixelFrameCache(4)
	k := frameKey{0, 0, 16, 16}
	start := make(chan struct{})
	var loads atomic.Int32
	load := func() (*decoder.Image, error) {
		loads.Add(1)
		<-start
		return mkImg(16, 16), nil
	}
	var wg sync.WaitGroup
	const N = 16
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.getOrLoad(k, load)
			if err != nil {
				t.Errorf("getOrLoad: %v", err)
			}
		}()
	}
	// Give all N goroutines time to enter getOrLoad and either run
	// load or block on the ready chan.
	time.Sleep(50 * time.Millisecond)
	close(start)
	wg.Wait()
	if got := loads.Load(); got != 1 {
		t.Fatalf("load called %d times; want 1 across %d concurrent gets", got, N)
	}
}

func TestPixelCacheErrPropagates(t *testing.T) {
	c := newPixelFrameCache(4)
	want := errors.New("boom")
	_, err := c.getOrLoad(frameKey{0, 0, 16, 16}, func() (*decoder.Image, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	// A subsequent call should NOT cache the error — the entry should
	// have been removed so a fresh attempt can succeed.
	good := mkImg(16, 16)
	got, err := c.getOrLoad(frameKey{0, 0, 16, 16}, func() (*decoder.Image, error) {
		return good, nil
	})
	if err != nil {
		t.Fatalf("retry after error: %v", err)
	}
	if got != good {
		t.Fatal("retry did not load fresh image")
	}
}

func TestPixelCacheBoundsLen(t *testing.T) {
	c := newPixelFrameCache(3)
	for i := 0; i < 10; i++ {
		_, err := c.getOrLoad(frameKey{i * 16, 0, 16, 16}, func() (*decoder.Image, error) {
			return mkImg(16, 16), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := c.len(); got > 3 {
		t.Fatalf("cache holds %d entries; capacity is 3", got)
	}
}

func TestPixelCacheThrash(t *testing.T) {
	c := newPixelFrameCache(2)
	keys := make([]frameKey, 10)
	for i := range keys {
		keys[i] = frameKey{posX: i * 16, posY: 0, w: 16, h: 16}
	}
	calls := 0
	load := func() (*decoder.Image, error) {
		calls++
		return mkImg(16, 16), nil
	}
	// Round-robin 5 times.
	for round := 0; round < 5; round++ {
		for _, k := range keys {
			_, err := c.getOrLoad(k, load)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls < 45 {
		t.Fatalf("expected ~50 loads under thrash; got %d", calls)
	}
	if got := c.len(); got > 2 {
		t.Fatalf("capacity exceeded after thrash: %d > 2", got)
	}
}
