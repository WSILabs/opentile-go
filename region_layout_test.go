package opentile

import "testing"

type fakeLayoutReader struct {
	originX int
}

func (f *fakeLayoutReader) TileOrigin(level, col, row int) (int, int, bool) {
	return f.originX + col*100, row*100, true
}
func (f *fakeLayoutReader) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	return []struct{ Col, Row int }{{0, 0}}
}
func (f *fakeLayoutReader) StitchedSize(level int) (int, int, bool) { return 200, 100, true }

func TestRegionLayoutOfDiscovery(t *testing.T) {
	r := &fakeLayoutReader{originX: 7}
	rl, ok := regionLayoutOf(r)
	if !ok {
		t.Fatal("regionLayoutOf should find the interface on a direct reader")
	}
	x, _, _ := rl.TileOrigin(0, 1, 0)
	if x != 107 {
		t.Errorf("TileOrigin pass-through x = %d, want 107", x)
	}
	// Non-implementer → ok=false.
	if _, ok := regionLayoutOf(struct{ x int }{}); ok {
		t.Error("regionLayoutOf should return false for non-implementer")
	}
}
