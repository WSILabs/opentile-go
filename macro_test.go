package opentile

import (
	"math"
	"testing"
)

func TestFitAspect(t *testing.T) {
	cases := []struct {
		name   string
		aw, ah float64
		bounds Size
		want   Size
		err    bool
	}{
		// scan-area aspect 50:25 = 2:1
		{"fit-box wide bounds → height binds", 50, 25, Size{600, 600}, Size{600, 300}, false},
		{"fit-box tall bounds → width binds", 50, 25, Size{600, 100}, Size{200, 100}, false},
		{"fit-width", 50, 25, Size{600, 0}, Size{600, 300}, false},
		{"fit-height", 50, 25, Size{0, 100}, Size{200, 100}, false},
		// NO upscale clamp (synthetic canvas can be any size, unlike thumbnail)
		{"upscale allowed", 2, 1, Size{4000, 0}, Size{4000, 2000}, false},
		{"both zero", 50, 25, Size{0, 0}, Size{}, true},
		{"degenerate aspect", 0, 25, Size{600, 0}, Size{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := fitAspect(c.aw, c.ah, c.bounds)
			if c.err {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("fitAspect(%g,%g,%v) = %v, want %v", c.aw, c.ah, c.bounds, got, c.want)
			}
		})
	}
}

func TestEffectiveMPP(t *testing.T) {
	cases := []struct {
		name   string
		md     Metadata
		wx, wy float64
		err    bool
	}{
		{"explicit MPP", Metadata{MPP: MPP{0.25, 0.25}}, 0.25, 0.25, false},
		{"explicit asymmetric MPP", Metadata{MPP: MPP{0.25, 0.26}}, 0.25, 0.26, false},
		{"MPP one-axis fills other", Metadata{MPP: MPP{0.5, 0}}, 0.5, 0.5, false},
		{"objective 40x → 0.25", Metadata{Magnification: 40}, 0.25, 0.25, false},
		{"objective 20x → 0.5", Metadata{Magnification: 20}, 0.5, 0.5, false},
		{"objective 10x → 1.0", Metadata{Magnification: 10}, 1.0, 1.0, false},
		{"MPP wins over objective", Metadata{MPP: MPP{0.23, 0.23}, Magnification: 40}, 0.23, 0.23, false},
		{"neither → error", Metadata{}, 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y, err := effectiveMPP(c.md)
			if c.err {
				if err == nil {
					t.Fatalf("want error, got %g,%g", x, y)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(x-c.wx) > 1e-9 || math.Abs(y-c.wy) > 1e-9 {
				t.Errorf("effectiveMPP = %g,%g, want %g,%g", x, y, c.wx, c.wy)
			}
		})
	}
}
