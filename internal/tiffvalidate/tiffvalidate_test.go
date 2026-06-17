package tiffvalidate

import "testing"

// fakeSink records flag calls.
type fakeSink struct {
	oob    int
	orphan int
}

func (s *fakeSink) OffsetOutOfBounds(level int, msg string) { s.oob++ }
func (s *fakeSink) OrphanIFD(msg string)                    { s.orphan++ }

func TestCheckByteRangeInBounds(t *testing.T) {
	s := &fakeSink{}
	checkRange(s, 0, 100, 50, 1000) // off=100 len=50 size=1000 → in bounds
	if s.oob != 0 {
		t.Fatalf("in-bounds range flagged %d times, want 0", s.oob)
	}
}

func TestCheckByteRangeOutOfBounds(t *testing.T) {
	s := &fakeSink{}
	checkRange(s, 2, 990, 50, 1000) // 990+50 = 1040 > 1000 → flag
	if s.oob != 1 {
		t.Fatalf("out-of-bounds range flagged %d times, want 1", s.oob)
	}
}

func TestCheckByteRangeOverflow(t *testing.T) {
	s := &fakeSink{}
	const maxU64 = ^uint64(0)
	checkRange(s, 0, maxU64-10, 1000, 1<<40) // offset+length overflows
	if s.oob != 1 {
		t.Fatalf("overflowing range flagged %d times, want 1", s.oob)
	}
}
