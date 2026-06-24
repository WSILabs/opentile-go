package opentile

import (
	"container/list"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// frameCacheKey identifies one decoded raw tile of an (overlapping) level.
// The format is part of the key because the decoded pixel layout depends on it.
type frameCacheKey struct {
	image, level, col, row int
	format                 decoder.PixelFormat
}

// decodedFrameCache is a byte-bounded, promise-pattern LRU of decoded raw
// tiles, held per-Slide and used by imageStitchedTile to composite display
// tiles without re-decoding source frames shared by adjacent tiles.
//
// Concurrency: operations are serialized by mu, but mu is released before the
// load callback runs. Concurrent callers for the same key block on the entry's
// ready chan (the first caller's load runs; the rest share its result).
//
// Eviction: LRU by recency, bounded by maxBytes (sum of decoded frame byte
// lengths). In-flight entries are NEVER evicted (their bytes are 0 until the
// load completes, and skipping them avoids the v0.47.1 eviction-race class).
type decodedFrameCache struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	entries  map[frameCacheKey]*frameCacheEntry
	order    *list.List // values are frameCacheKey; front = MRU
}

type frameCacheEntry struct {
	pix      *decoder.Image
	err      error
	bytes    int64
	key      frameCacheKey
	elem     *list.Element
	ready    chan struct{}
	inflight bool
}

// newDecodedFrameCache constructs an empty cache bounded to maxBytes
// (clamped to >= 1).
func newDecodedFrameCache(maxBytes int64) *decodedFrameCache {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &decodedFrameCache{
		maxBytes: maxBytes,
		entries:  make(map[frameCacheKey]*frameCacheEntry),
		order:    list.New(),
	}
}

// getOrLoad returns the cached decoded frame for key, calling load on a miss.
// Concurrent callers for the same key share the first caller's load.
// On load error the entry is dropped (next call retries) and the error is
// returned to every waiter.
func (c *decodedFrameCache) getOrLoad(key frameCacheKey, load func() (*decoder.Image, error)) (*decoder.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.order.MoveToFront(e.elem)
		ready := e.ready
		c.mu.Unlock()
		<-ready
		return e.pix, e.err
	}
	e := &frameCacheEntry{key: key, ready: make(chan struct{}), inflight: true}
	c.entries[key] = e
	e.elem = c.order.PushFront(key)
	c.mu.Unlock()

	pix, err := load()

	c.mu.Lock()
	if err != nil {
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
	e.bytes = int64(len(pix.Pix))
	e.inflight = false
	c.curBytes += e.bytes
	c.evictIfOverLocked()
	c.mu.Unlock()
	close(e.ready)
	return pix, nil
}

// evictIfOverLocked must be called with c.mu held. Evicts the least-recently
// used NON-in-flight entries until curBytes <= maxBytes (or none remain).
func (c *decodedFrameCache) evictIfOverLocked() {
	for c.curBytes > c.maxBytes {
		var victim *list.Element
		for el := c.order.Back(); el != nil; el = el.Prev() {
			if ent := c.entries[el.Value.(frameCacheKey)]; ent != nil && !ent.inflight {
				victim = el
				break
			}
		}
		if victim == nil {
			return // everything is in-flight; cannot evict
		}
		k := victim.Value.(frameCacheKey)
		ent := c.entries[k]
		c.order.Remove(victim)
		delete(c.entries, k)
		c.curBytes -= ent.bytes
	}
}

// len reports the current entry count (tests).
func (c *decodedFrameCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// inflightCountForTest reports how many entries are mid-load (tests).
func (c *decodedFrameCache) inflightCountForTest() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.entries {
		if e.inflight {
			n++
		}
	}
	return n
}

// presentForTest reports whether key is currently cached (tests).
func (c *decodedFrameCache) presentForTest(key frameCacheKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[key]
	return ok
}
