//go:build cgo && !nocgo && !nolcms2

package color

import (
	"math"
	"sync"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// srgbEncode is the sRGB transfer function on a 0..255 linear value.
func srgbEncode(v int) float64 {
	l := float64(v) / 255
	if l <= 0.0031308 {
		return 255 * 12.92 * l
	}
	return 255 * (1.055*math.Pow(l, 1/2.4) - 0.055)
}

func TestRegistered(t *testing.T) {
	if !opentile.ColorManagementAvailable() {
		t.Fatal("internal/color is linked but ColorManagementAvailable is false")
	}
}

// A linear-light profile with sRGB primaries differs from sRGB only in its
// tone curve, so a neutral gray v must come out as the sRGB encoding of v.
func TestLinearProfileToSRGB(t *testing.T) {
	prof := linearProfile()
	if len(prof) == 0 {
		t.Fatal("could not build the test profile")
	}
	for _, rgba := range []bool{false, true} {
		apply, free, ok := newTransform(prof, rgba)
		if !ok {
			t.Fatalf("rgba=%v: transform not built", rgba)
		}
		bpp := 3
		if rgba {
			bpp = 4
		}
		grays := []int{0, 16, 64, 128, 200, 255}
		const pad = 5 // stride padding, must be left alone
		stride := len(grays)*bpp + pad
		pix := make([]byte, 2*stride)
		for y := 0; y < 2; y++ {
			for i, g := range grays {
				p := pix[y*stride+i*bpp:]
				p[0], p[1], p[2] = byte(g), byte(g), byte(g)
				if rgba {
					p[3] = byte(17 * (i + 1)) // alpha is copied through
				}
			}
			for k := 0; k < pad; k++ {
				pix[y*stride+len(grays)*bpp+k] = 0xAB
			}
		}
		apply(pix, len(grays), 2, stride)
		for y := 0; y < 2; y++ {
			for i, g := range grays {
				p := pix[y*stride+i*bpp:]
				want := srgbEncode(g)
				for c := 0; c < 3; c++ {
					if math.Abs(float64(p[c])-want) > 2 {
						t.Errorf("rgba=%v row %d gray %d channel %d = %d, want %.1f", rgba, y, g, c, p[c], want)
					}
				}
				if rgba && p[3] != byte(17*(i+1)) {
					t.Errorf("rgba row %d gray %d: alpha %d changed", y, g, p[3])
				}
			}
			for k := 0; k < pad; k++ {
				if pix[y*stride+len(grays)*bpp+k] != 0xAB {
					t.Errorf("rgba=%v row %d: stride padding written", rgba, y)
				}
			}
		}
		free()
	}
}

// Unusable or missing profiles build no transform (not an error).
func TestUnusableProfile(t *testing.T) {
	for _, p := range [][]byte{nil, {}, []byte("not an icc profile")} {
		if _, _, ok := newTransform(p, false); ok {
			t.Errorf("profile %q: transform built", p)
		}
	}
}

// One transform shared by many goroutines (cmsFLAGS_NOCACHE); run with -race.
func TestConcurrentApply(t *testing.T) {
	apply, free, ok := newTransform(linearProfile(), false)
	if !ok {
		t.Fatal("transform not built")
	}
	defer free()
	want := byte(math.Round(srgbEncode(128)))
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pix := make([]byte, 64*64*3)
			for i := range pix {
				pix[i] = 128
			}
			for n := 0; n < 20; n++ {
				for i := range pix {
					pix[i] = 128
				}
				apply(pix, 64, 64, 64*3)
				if d := int(pix[0]) - int(want); d < -2 || d > 2 {
					t.Errorf("got %d, want about %d", pix[0], want)
					return
				}
			}
		}()
	}
	wg.Wait()
}
