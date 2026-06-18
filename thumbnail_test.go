package opentile

import "testing"

func TestThumbnailTargetSize(t *testing.T) {
	cases := []struct {
		name       string
		l0, bounds Size
		want       Size
		wantErr    bool
	}{
		// fit-box: portrait L0, square box → height binds
		{"fit-box portrait", Size{2000, 3000}, Size{256, 256}, Size{171, 256}, false},
		// fit-box: landscape L0, square box → width binds
		{"fit-box landscape", Size{3000, 2000}, Size{256, 256}, Size{256, 171}, false},
		// fit-box: square L0
		{"fit-box square", Size{1000, 1000}, Size{256, 256}, Size{256, 256}, false},
		// fit-width: height derived from aspect
		{"fit-width", Size{2000, 3000}, Size{256, 0}, Size{256, 384}, false},
		// fit-height: width derived from aspect
		{"fit-height", Size{2000, 3000}, Size{0, 256}, Size{171, 256}, false},
		// no upscale: bounds bigger than L0 → clamp to L0
		{"no-upscale box", Size{500, 800}, Size{4096, 4096}, Size{500, 800}, false},
		{"no-upscale width", Size{500, 800}, Size{4096, 0}, Size{500, 800}, false},
		// extreme downscale floors at 1px
		{"1px floor", Size{100000, 50}, Size{0, 1}, Size{2000, 1}, false},
		// both axes zero → error
		{"both zero", Size{2000, 3000}, Size{0, 0}, Size{}, true},
		// negative treated as unconstrained (W<0,H set → fit-height)
		{"negative width unconstrained", Size{2000, 3000}, Size{-1, 256}, Size{171, 256}, false},
		// degenerate L0 → error
		{"empty l0", Size{0, 0}, Size{256, 256}, Size{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := thumbnailTargetSize(c.l0, c.bounds)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("thumbnailTargetSize(%v, %v) = %v, want %v", c.l0, c.bounds, got, c.want)
			}
		})
	}
}
