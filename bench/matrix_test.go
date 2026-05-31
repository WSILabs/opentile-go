package bench

import (
	"testing"
	"time"
)

func TestMpixPerSec(t *testing.T) {
	// 2,000,000 pixels in 1s = 2 Mpix/s.
	if got := MpixPerSec(2_000_000, time.Second); got != 2.0 {
		t.Fatalf("MpixPerSec = %v, want 2.0", got)
	}
	if got := MpixPerSec(100, 0); got != 0 {
		t.Fatalf("MpixPerSec(_, 0) = %v, want 0 (no div-by-zero)", got)
	}
}

func TestMatrixWellFormed(t *testing.T) {
	if len(Matrix) != 10 {
		t.Fatalf("Matrix has %d entries, want 10 (one per supported format)", len(Matrix))
	}
	seen := map[string]bool{}
	for _, e := range Matrix {
		if e.Format == "" || e.Fixture == "" {
			t.Errorf("entry %+v has empty Format or Fixture", e)
		}
		if seen[e.Format] {
			t.Errorf("duplicate format %q in Matrix", e.Format)
		}
		seen[e.Format] = true
	}
}
