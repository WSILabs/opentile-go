package ndpi

import (
	"container/list"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// pixelFrameCache is a small bounded LRU of decoded RGB frames keyed
// by (framePos, frameSize). Used by strippedImage.DecodedTile to
// avoid re-decoding the same assembled strip frame for adjacent
// tiles.
//
// Concurrency: cache operations are serialized by mu, but mu is
// released BEFORE the slow load callback runs. Concurrent goroutines
// requesting the same key block on the entry's `ready` chan rather
// than redundantly running load (promise pattern).
//
// Eviction: simple LRU by recency. When len(entries) > capacity,
// the back of `order` is evicted. Eviction happens after each
// successful insert; a single getOrLoad both inserts and evicts in
// one critical section.
//
// Memory: bounded by capacity × per-frame-pixel-size. For typical
// NDPI levels (frame ≈ 4096×256 RGB ≈ 3 MB), capacity=16 gives a
// ~48 MB ceiling.
type pixelFrameCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[frameKey]*pixelFrameEntry
	order    *list.List // values are frameKey; front = MRU

	// v0.29 Layer 3: pool of evicted decoded frames. Reused by the
	// next getOrLoadInto call that needs a same-sized buffer.
	// sync.Pool auto-shrinks under GC pressure. Per-cache (per-
	// strippedImage) instance so we never get cross-level size
	// mismatches.
	scratchPool sync.Pool
}

// pixelFrameEntry is one cache slot. ready is closed when pix/err is
// populated. Once closed, pix and err are safe to read from any
// goroutine — they are written only by the loader and not modified
// afterwards.
type pixelFrameEntry struct {
	pix   *decoder.Image
	err   error
	elem  *list.Element // back-pointer into pixelFrameCache.order
	ready chan struct{}
}

// newPixelFrameCache constructs an empty cache with the given
// capacity. capacity must be > 0; smaller values are clamped to 1.
func newPixelFrameCache(capacity int) *pixelFrameCache {
	if capacity < 1 {
		capacity = 1
	}
	return &pixelFrameCache{
		capacity: capacity,
		entries:  make(map[frameKey]*pixelFrameEntry, capacity),
		order:    list.New(),
	}
}

// getOrLoad returns the cached pixels for key. On miss, calls load
// to populate the entry. Concurrent calls for the same key share
// the result: only the first caller's load runs; the rest block on
// the ready chan.
//
// If load returns an error, the entry is removed from the cache and
// the error is returned to every waiter. The next getOrLoad for the
// same key will retry load.
func (c *pixelFrameCache) getOrLoad(key frameKey, load func() (*decoder.Image, error)) (*decoder.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.order.MoveToFront(e.elem)
		ready := e.ready
		c.mu.Unlock()
		<-ready
		return e.pix, e.err
	}
	e := &pixelFrameEntry{ready: make(chan struct{})}
	c.entries[key] = e
	e.elem = c.order.PushFront(key)
	_ = c.evictIfOverLocked() // T4.2 routes survivors to scratchPool
	c.mu.Unlock()

	pix, err := load()
	if err != nil {
		c.mu.Lock()
		// Only remove if our entry is still the one in the map; an
		// eviction may have already removed it.
		if cur, ok := c.entries[key]; ok && cur == e {
			delete(c.entries, key)
			c.order.Remove(e.elem)
		}
		c.mu.Unlock()
		e.err = err
		close(e.ready)
		return nil, err
	}
	e.pix = pix
	close(e.ready)
	return pix, nil
}

// getOrLoadInto is the v0.29 Layer 3 variant of getOrLoad. The load
// callback receives a scratch *decoder.Image (or nil if the pool is
// empty); decoders that honor opts.Dst can write into it,
// eliminating the per-miss allocation. Evicted entries route into
// the scratch pool best-effort (matching size only).
//
// wantW / wantH describe the expected frame dimensions; the pool
// only serves matching-size buffers. Mismatches are GC'd.
//
// Added in v0.29.
func (c *pixelFrameCache) getOrLoadInto(
	key frameKey,
	wantW, wantH int,
	load func(scratch *decoder.Image) (*decoder.Image, error),
) (*decoder.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.order.MoveToFront(e.elem)
		ready := e.ready
		c.mu.Unlock()
		<-ready
		return e.pix, e.err
	}
	e := &pixelFrameEntry{ready: make(chan struct{})}
	c.entries[key] = e
	e.elem = c.order.PushFront(key)
	evicted := c.evictIfOverLocked()
	c.mu.Unlock()

	// Route evicted entries (populated, size-matching) into the pool.
	for _, ev := range evicted {
		if ev.pix != nil &&
			ev.pix.Width == wantW &&
			ev.pix.Height == wantH {
			c.scratchPool.Put(ev.pix)
		}
	}

	// Try to borrow a same-sized scratch from the pool.
	var scratch *decoder.Image
	if v := c.scratchPool.Get(); v != nil {
		s := v.(*decoder.Image)
		if s.Width == wantW && s.Height == wantH {
			scratch = s
		}
		// Mismatched-size pool drop: let GC reclaim.
	}

	pix, err := load(scratch)
	if err != nil {
		c.mu.Lock()
		if cur, ok := c.entries[key]; ok && cur == e {
			delete(c.entries, key)
			c.order.Remove(e.elem)
		}
		c.mu.Unlock()
		e.err = err
		close(e.ready)
		return nil, err
	}
	e.pix = pix
	close(e.ready)
	return pix, nil
}

// evictIfOverLocked must be called with c.mu held. Evicts entries
// from the back of order until len(entries) <= capacity. Returns
// the evicted entries so callers can route their populated *pix
// into a scratch pool (v0.29 Layer 3).
//
// An evicted entry may still be in flight (its load callback is
// running). That's safe because (a) waiters on the entry's ready
// chan still get notified when close(e.ready) runs, and (b) the
// entry is no longer in entries/order so future lookups miss and
// re-load. In-flight entries in the returned slice will have
// pix==nil — callers must filter for non-nil pix before pooling.
func (c *pixelFrameCache) evictIfOverLocked() []*pixelFrameEntry {
	var evicted []*pixelFrameEntry
	for len(c.entries) > c.capacity {
		back := c.order.Back()
		if back == nil {
			return evicted
		}
		key := back.Value.(frameKey)
		if e, ok := c.entries[key]; ok {
			evicted = append(evicted, e)
		}
		c.order.Remove(back)
		delete(c.entries, key)
	}
	return evicted
}

// len returns the current number of cached entries. Used by tests
// to verify the capacity bound is respected.
func (c *pixelFrameCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
