package ndpi

import "testing"

func TestFrameByteLRUEvictsByBytes(t *testing.T) {
	// Budget 250 bytes; each entry 100 bytes → at most 2 resident.
	c := newFrameByteLRU(250)
	k := func(i int) frameKey { return frameKey{posX: i} }
	c.put(k(1), make([]byte, 100))
	c.put(k(2), make([]byte, 100))
	if got := c.bytes(); got != 200 {
		t.Fatalf("bytes after 2 puts = %d, want 200", got)
	}
	// Third put must evict the LRU (k1) to stay <= 250.
	c.put(k(3), make([]byte, 100))
	if got := c.bytes(); got != 200 {
		t.Fatalf("bytes after 3 puts = %d, want 200 (one evicted)", got)
	}
	if _, ok := c.get(k(1)); ok {
		t.Fatalf("k1 should have been evicted as LRU")
	}
	if _, ok := c.get(k(3)); !ok {
		t.Fatalf("k3 should be present")
	}
}

func TestFrameByteLRUGetIsMRU(t *testing.T) {
	c := newFrameByteLRU(250)
	k := func(i int) frameKey { return frameKey{posX: i} }
	c.put(k(1), make([]byte, 100))
	c.put(k(2), make([]byte, 100))
	_, _ = c.get(k(1)) // touch k1 → MRU
	c.put(k(3), make([]byte, 100)) // evicts LRU, which is now k2
	if _, ok := c.get(k(2)); ok {
		t.Fatalf("k2 should have been evicted (k1 was touched)")
	}
	if _, ok := c.get(k(1)); !ok {
		t.Fatalf("k1 should survive (was MRU)")
	}
}

func TestFrameByteLRUOversizeEntryStillStored(t *testing.T) {
	// A single entry larger than the budget is stored alone (we never
	// drop the just-inserted entry; correctness > strict bound).
	c := newFrameByteLRU(50)
	c.put(frameKey{posX: 1}, make([]byte, 100))
	if _, ok := c.get(frameKey{posX: 1}); !ok {
		t.Fatalf("oversize entry must still be retrievable")
	}
}

func TestFrameByteLRUReplaceKeyDeltaAccounts(t *testing.T) {
	// Re-putting an existing key must delta-account bytes (not double
	// count) and replace the stored data. Budget is generous so no
	// eviction interferes with the accounting check.
	c := newFrameByteLRU(1000)
	k := frameKey{posX: 1}
	c.put(k, make([]byte, 100))
	if got := c.bytes(); got != 100 {
		t.Fatalf("bytes after first put = %d, want 100", got)
	}
	c.put(k, make([]byte, 150)) // replace same key, larger
	if got := c.bytes(); got != 150 {
		t.Fatalf("bytes after replace = %d, want 150 (delta-accounted, not 250)", got)
	}
	got, ok := c.get(k)
	if !ok || len(got) != 150 {
		t.Fatalf("get after replace = (len %d, ok %v), want (150, true)", len(got), ok)
	}
	// Shrinking replace must also delta-account downward.
	c.put(k, make([]byte, 40))
	if got := c.bytes(); got != 40 {
		t.Fatalf("bytes after shrink replace = %d, want 40", got)
	}
}
