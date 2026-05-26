package opentile

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestTileCachePutGet(t *testing.T) {
	c := newTileCache(10)
	img := decoder.NewImage(16, 16)

	c.put(tileKey{tx: 0, ty: 0}, img, nil)

	got, err, ok := c.tryGet(tileKey{tx: 0, ty: 0})
	if !ok {
		t.Fatalf("tryGet: not found")
	}
	if err != nil {
		t.Errorf("err: %v, want nil", err)
	}
	if got != img {
		t.Errorf("img pointer mismatch")
	}
}

func TestTileCacheTryGetMissing(t *testing.T) {
	c := newTileCache(10)
	_, _, ok := c.tryGet(tileKey{tx: 5, ty: 7})
	if ok {
		t.Errorf("tryGet missing key: ok=true, want false")
	}
}

func TestTileCacheWaitForDecode(t *testing.T) {
	c := newTileCache(10)
	k := tileKey{tx: 0, ty: 0}
	c.reserve(k) // mark as in-flight

	img := decoder.NewImage(16, 16)
	go func() {
		time.Sleep(50 * time.Millisecond)
		c.put(k, img, nil)
	}()

	got, err, ok := c.waitGet(k, nil)
	if !ok {
		t.Fatalf("waitGet: cache miss after put")
	}
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if got != img {
		t.Errorf("img pointer mismatch")
	}
}

func TestTileCacheWaitGetError(t *testing.T) {
	c := newTileCache(10)
	k := tileKey{tx: 0, ty: 0}
	c.reserve(k)

	want := errors.New("decode failed")
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.put(k, nil, want)
	}()

	_, err, ok := c.waitGet(k, nil)
	if !ok {
		t.Fatalf("waitGet: should report cache hit even on error")
	}
	if err != want {
		t.Errorf("err: got %v, want %v", err, want)
	}
}

func TestTileCacheRefCountEviction(t *testing.T) {
	c := newTileCache(2) // capacity 2

	k1 := tileKey{tx: 1, ty: 0}
	k2 := tileKey{tx: 2, ty: 0}
	k3 := tileKey{tx: 3, ty: 0}

	c.put(k1, decoder.NewImage(16, 16), nil)
	c.put(k2, decoder.NewImage(16, 16), nil)

	// k1's refCount is 0 (no consumer); k2's also 0. Putting k3
	// should evict the oldest entry with refCount=0 (k1).
	c.put(k3, decoder.NewImage(16, 16), nil)

	if _, _, ok := c.tryGet(k1); ok {
		t.Errorf("k1 should have been evicted")
	}
	if _, _, ok := c.tryGet(k2); !ok {
		t.Errorf("k2 should still be present")
	}
	if _, _, ok := c.tryGet(k3); !ok {
		t.Errorf("k3 should be present")
	}
}

func TestTileCacheRefCountBlocksEviction(t *testing.T) {
	c := newTileCache(2)
	k1 := tileKey{tx: 1, ty: 0}
	k2 := tileKey{tx: 2, ty: 0}
	k3 := tileKey{tx: 3, ty: 0}

	c.put(k1, decoder.NewImage(16, 16), nil)
	c.put(k2, decoder.NewImage(16, 16), nil)

	// Increment refCount on both — neither can be evicted.
	c.acquire(k1)
	c.acquire(k2)

	// Putting k3 should block waiting for an entry to free up.
	done := make(chan struct{})
	go func() {
		c.put(k3, decoder.NewImage(16, 16), nil)
		close(done)
	}()

	select {
	case <-done:
		t.Errorf("put completed despite all entries having refCount > 0")
	case <-time.After(50 * time.Millisecond):
		// OK — put is blocked.
	}

	// Release one entry; put should unblock.
	c.release(k1)
	select {
	case <-done:
		// OK — put completed after release.
	case <-time.After(500 * time.Millisecond):
		t.Errorf("put did not unblock after release")
	}
}

func TestTileCacheConcurrentAccess(t *testing.T) {
	c := newTileCache(100)
	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := tileKey{tx: i, ty: 0}
			c.put(k, decoder.NewImage(16, 16), nil)
			if _, _, ok := c.tryGet(k); !ok {
				t.Errorf("concurrent put/tryGet %d: ok=false", i)
			}
		}(i)
	}
	wg.Wait()
}
