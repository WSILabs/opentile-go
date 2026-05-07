package ndpi

import "testing"

func TestAdjustTileSize(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		strip     int
		wantW     int
	}{
		{"equal_to_strip", 640, 640, 640},
		{"smaller_than_strip_ratio_close_to_1", 500, 640, 640}, // factor 1.28, log2≈0.36, round→0, factor_2=1
		{"smaller_than_strip_needs_doubling", 256, 640, 1280},  // factor 2.5, round→1, factor_2=2 → 2*640
		{"larger_than_strip_ratio_3", 2048, 640, 2560},         // factor 3.2, round→2, factor_2=4 → 4*640
		{"larger_than_strip_ratio_2", 1280, 640, 1280},         // factor 2, log2=1, factor_2=2
		{"no_strip_pages", 1024, 0, 1024},                      // no stripped pages → passthrough
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AdjustTileSize(tc.requested, tc.strip)
			if got.W != tc.wantW || got.H != tc.wantW {
				t.Errorf("AdjustTileSize(%d, %d): got %v, want {%d,%d}",
					tc.requested, tc.strip, got, tc.wantW, tc.wantW)
			}
		})
	}
}
