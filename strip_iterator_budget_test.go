package opentile

import "testing"

func TestStripCacheCapacityByteDerived(t *testing.T) {
	// bytesPerTile for a 512x512 RGB source tile = 786432.
	const bpt = 512 * 512 * 3
	// Budget 100 MiB → 100*1<<20 / 786432 ≈ 133 tiles.
	got := stripCacheCapacity(100<<20, bpt, /*workers*/ 10, /*countFormulaCap*/ 7440)
	if got < 130 || got > 140 {
		t.Fatalf("byte-derived cap = %d, want ~133", got)
	}
}

func TestStripCacheCapacityFlooredAtWorkers(t *testing.T) {
	const bpt = 512 * 512 * 3
	// Tiny budget → would be < workers; must floor at max(workers,8).
	got := stripCacheCapacity(1<<20, bpt, /*workers*/ 12, /*countFormulaCap*/ 7440)
	if got != 12 {
		t.Fatalf("cap = %d, want floor 12 (workers)", got)
	}
}

func TestStripCacheCapacityFlooredAtEight(t *testing.T) {
	const bpt = 512 * 512 * 3
	got := stripCacheCapacity(1<<20, bpt, /*workers*/ 2, /*countFormulaCap*/ 7440)
	if got != 8 {
		t.Fatalf("cap = %d, want floor 8", got)
	}
}

func TestStripCacheCapacityNeverExceedsCountFormula(t *testing.T) {
	const bpt = 512 * 512 * 3
	// Huge budget must not exceed the original count formula cap.
	got := stripCacheCapacity(64<<30, bpt, /*workers*/ 10, /*countFormulaCap*/ 300)
	if got != 300 {
		t.Fatalf("cap = %d, want countFormulaCap 300", got)
	}
}
