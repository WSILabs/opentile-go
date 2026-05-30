package ndpi

import (
	"container/list"
	"sync"
)

// frameByteLRU is a byte-bounded LRU of assembled JPEG frames keyed by
// frameKey. It replaces the v0.2 unbounded framesByKey map. On the
// single-pass ScaledStrips traversal each frame is read once (via the
// pixelCache miss), so this provides retention only for the slow
// Tile() random-access path; a modest byte budget suffices.
//
// Eviction: LRU by recency until total bytes <= maxBytes. A
// just-inserted entry is never evicted, so a single entry larger than
// maxBytes is stored alone (correctness over a strict bound).
//
// All methods are safe for concurrent use.
type frameByteLRU struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	entries  map[frameKey]*frameLRUEntry
	order    *list.List // values are frameKey; front = MRU
}

type frameLRUEntry struct {
	data []byte
	elem *list.Element
}

func newFrameByteLRU(maxBytes int64) *frameByteLRU {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &frameByteLRU{
		maxBytes: maxBytes,
		entries:  make(map[frameKey]*frameLRUEntry),
		order:    list.New(),
	}
}

// get returns the cached frame and moves it to MRU. ok=false on miss.
func (c *frameByteLRU) get(k frameKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e.elem)
	return e.data, true
}

// put inserts (or replaces) k=data and evicts LRU entries until the
// total is within budget (never evicting the entry just inserted).
func (c *frameByteLRU) put(k frameKey, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		c.curBytes += int64(len(data)) - int64(len(e.data))
		e.data = data
		c.order.MoveToFront(e.elem)
	} else {
		e = &frameLRUEntry{data: data}
		e.elem = c.order.PushFront(k)
		c.entries[k] = e
		c.curBytes += int64(len(data))
	}
	for c.curBytes > c.maxBytes {
		back := c.order.Back()
		if back == nil {
			break
		}
		bk := back.Value.(frameKey)
		if bk == k {
			break // never evict the just-inserted entry
		}
		be := c.entries[bk]
		c.order.Remove(back)
		delete(c.entries, bk)
		c.curBytes -= int64(len(be.data))
	}
}

// bytes returns the current resident byte total (test helper).
func (c *frameByteLRU) bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.curBytes
}
