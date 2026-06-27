package dzi

import "testing"

func TestContentRect(t *testing.T) {
	// Level 46000x32914, T=256, ov=1 (CMU-1 level 16).
	cases := []struct {
		c, r, ox, oy, w, h int
	}{
		{0, 0, 0, 0, 256, 256},
		{1, 1, 1, 1, 256, 256},
		{179, 0, 1, 0, 176, 256},
		{0, 128, 0, 1, 256, 146},
		{179, 128, 1, 1, 176, 146},
	}
	for _, c := range cases {
		ox, oy, w, h := ContentRect(c.c, c.r, 46000, 32914, 256, 1)
		if ox != c.ox || oy != c.oy || w != c.w || h != c.h {
			t.Errorf("ContentRect(%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.c, c.r, ox, oy, w, h, c.ox, c.oy, c.w, c.h)
		}
	}
}

func TestContentRectZeroOverlap(t *testing.T) {
	ox, oy, w, h := ContentRect(1, 1, 1000, 1000, 256, 0)
	if ox != 0 || oy != 0 || w != 256 || h != 256 {
		t.Errorf("overlap=0 interior = (%d,%d,%d,%d), want (0,0,256,256)", ox, oy, w, h)
	}
}
