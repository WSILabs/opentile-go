package opentile

import "testing"

func TestSize(t *testing.T) {
	s := Size{W: 10, H: 20}
	if s.Area() != 200 {
		t.Fatalf("Area: want 200, got %d", s.Area())
	}
	if s.String() != "10x20" {
		t.Fatalf("String: want 10x20, got %s", s.String())
	}
}

func TestPoint(t *testing.T) {
	p := Point{X: 3, Y: 4}
	if p.String() != "(3,4)" {
		t.Fatalf("String: want (3,4), got %s", p.String())
	}
}

func TestTileCoord(t *testing.T) {
	// Default-zero (Z=C=T=0) renders as a plain (x,y) tuple.
	c := TileCoord{X: 3, Y: 5}
	if got := c.String(); got != "(3,5)" {
		t.Errorf("String of 2D-default coord: got %q, want (3,5)", got)
	}
	// Any non-zero Z/C/T flips to the multi-dim long form.
	cZ := TileCoord{X: 3, Y: 5, Z: 2}
	if got := cZ.String(); got != "(3,5, Z=2, C=0, T=0)" {
		t.Errorf("String of multi-dim coord: got %q", got)
	}
	cC := TileCoord{X: 3, Y: 5, C: 1}
	if got := cC.String(); got != "(3,5, Z=0, C=1, T=0)" {
		t.Errorf("String of multi-dim coord: got %q", got)
	}
	cT := TileCoord{X: 3, Y: 5, T: 7}
	if got := cT.String(); got != "(3,5, Z=0, C=0, T=7)" {
		t.Errorf("String of multi-dim coord: got %q", got)
	}
	// Comparable-as-map-key: TileCoord is all-int so Go map keys
	// work natively (this is one reason §11 Q2 deferred Index()).
	m := map[TileCoord]int{
		{X: 0, Y: 0}:                  1,
		{X: 0, Y: 0, Z: 1}:            2,
		{X: 1, Y: 0}:                  3,
		{X: 0, Y: 0, Z: 1, C: 1}:      4,
	}
	if m[TileCoord{X: 0, Y: 0}] != 1 || m[TileCoord{Z: 1}] != 2 || m[TileCoord{X: 1}] != 3 || m[TileCoord{Z: 1, C: 1}] != 4 {
		t.Error("TileCoord must work as a map key with all-int field comparison")
	}
}

func TestRegionContains(t *testing.T) {
	r := Region{Origin: Point{X: 5, Y: 5}, Size: Size{W: 10, H: 10}}
	tests := []struct {
		p    Point
		want bool
	}{
		{Point{X: 5, Y: 5}, true},
		{Point{X: 14, Y: 14}, true},
		{Point{X: 15, Y: 14}, false}, // exclusive far X
		{Point{X: 14, Y: 15}, false}, // exclusive far Y
		{Point{X: 15, Y: 15}, false}, // both far edges
		{Point{X: 4, Y: 5}, false},   // below-origin X
		{Point{X: 5, Y: 4}, false},   // below-origin Y
	}
	for _, tt := range tests {
		if got := r.Contains(tt.p); got != tt.want {
			t.Errorf("Contains(%v) = %v, want %v", tt.p, got, tt.want)
		}
	}
}
