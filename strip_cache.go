package opentile

import (
	"container/list"
	"context"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// tileKey identifies a single source-level tile within an iterator's
// scope. The source-level index is implicit (one iterator = one level).
type tileKey struct {
	tx, ty int
}

// tileEntry holds a cached tile's decoded data + error. ready is
// closed when the entry has been populated via put().
type tileEntry struct {
	img      *decoder.Image
	err      error
	ready    chan struct{} // closed when img/err populated; nil when entry is born ready
	refCount int          // > 0 means "in use; do not evict"
	lruElem  *list.Element // back-pointer into the LRU list
}

// tileCache is a per-iterator LRU cache keyed by (tx, ty). Bounded
// by entry count. Eviction policy: evict the least-recently-used
// entry whose refCount == 0. If capacity is hit and all entries are
// referenced, put() blocks until release() frees an entry.
//
// All methods are safe for concurrent use.
type tileCache struct {
	mu       sync.Mutex
	cond     *sync.Cond
	capacity int
	entries  map[tileKey]*tileEntry
	lru      *list.List // front = most recently used
}

func newTileCache(capacity int) *tileCache {
	if capacity < 1 {
		capacity = 1
	}
	c := &tileCache{
		capacity: capacity,
		entries:  make(map[tileKey]*tileEntry, capacity),
		lru:      list.New(),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// reserve marks a key as in-flight. waitGet will block on it until
// put() resolves the entry. Returns true if the reservation was
// taken; false if the key already exists (no-op).
func (c *tileCache) reserve(k tileKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[k]; exists {
		return false
	}
	c.evictLocked()
	e := &tileEntry{ready: make(chan struct{})}
	e.lruElem = c.lru.PushFront(k)
	c.entries[k] = e
	return true
}

// put stores a decoded tile (or error) at k. If k was reserved,
// closes the ready channel so waiting waitGet() callers unblock.
// If k was not reserved, the entry is born ready (no ready channel).
func (c *tileCache) put(k tileKey, img *decoder.Image, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.entries[k]; ok {
		existing.img = img
		existing.err = err
		if existing.ready != nil {
			close(existing.ready)
			existing.ready = nil
		}
		c.lru.MoveToFront(existing.lruElem)
		c.cond.Broadcast()
		return
	}
	c.evictLocked()
	e := &tileEntry{img: img, err: err}
	e.lruElem = c.lru.PushFront(k)
	c.entries[k] = e
	c.cond.Broadcast()
}

// tryGet returns the cached value if present and ready. Does NOT
// block on in-flight entries. ok=false means "not in cache yet."
func (c *tileCache) tryGet(k tileKey) (*decoder.Image, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || e.ready != nil {
		return nil, nil, false
	}
	c.lru.MoveToFront(e.lruElem)
	return e.img, e.err, true
}

// waitGet returns the cached value, blocking until the entry is
// populated (after reserve+put cycle). If ctx is non-nil, cancellation
// returns (nil, ctx.Err(), false).
func (c *tileCache) waitGet(k tileKey, ctx context.Context) (*decoder.Image, error, bool) {
	c.mu.Lock()
	e, ok := c.entries[k]
	if !ok {
		c.mu.Unlock()
		return nil, nil, false
	}
	if e.ready == nil {
		c.lru.MoveToFront(e.lruElem)
		img, err := e.img, e.err
		c.mu.Unlock()
		return img, err, true
	}
	ready := e.ready
	c.mu.Unlock()

	if ctx != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err(), false
		}
	} else {
		<-ready
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	e2 := c.entries[k]
	if e2 == nil {
		return nil, nil, false
	}
	c.lru.MoveToFront(e2.lruElem)
	return e2.img, e2.err, true
}

// acquire increments an entry's refCount, pinning it against
// eviction. No-op if the key is absent.
func (c *tileCache) acquire(k tileKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		e.refCount++
	}
}

// release decrements an entry's refCount. If it reaches 0, the
// entry becomes eligible for eviction. Wakes any put() callers
// blocked on capacity.
func (c *tileCache) release(k tileKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok && e.refCount > 0 {
		e.refCount--
		if e.refCount == 0 {
			c.cond.Broadcast()
		}
	}
}

// evictLocked tries to evict the LRU entry with refCount=0. If all
// entries are referenced (capacity full + all in use), blocks via
// cond until an entry's refCount drops or an entry is removed.
//
// Caller must hold c.mu.
func (c *tileCache) evictLocked() {
	for len(c.entries) >= c.capacity {
		// Scan LRU back-to-front for a refCount=0 entry.
		var victim *list.Element
		for e := c.lru.Back(); e != nil; e = e.Prev() {
			k := e.Value.(tileKey)
			if c.entries[k].refCount == 0 {
				victim = e
				break
			}
		}
		if victim != nil {
			k := victim.Value.(tileKey)
			c.lru.Remove(victim)
			delete(c.entries, k)
			return
		}
		// All entries pinned — wait for release().
		c.cond.Wait()
	}
}

// close marks the cache as closed and wakes all waiters. After
// close, tryGet/waitGet on absent keys return (nil, nil, false);
// in-flight reserve() callers see closed ready channels with
// the entries' img/err preserved as-is (may be nil).
func (c *tileCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Close all ready channels so waitGet() callers unblock.
	for _, e := range c.entries {
		if e.ready != nil {
			close(e.ready)
			e.ready = nil
		}
	}
	c.cond.Broadcast()
}
