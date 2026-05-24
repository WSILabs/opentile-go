package resample

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// TestBoxSimpleCorrectness ports TestAreaAverage_SimpleCorrectness from
// wsitools/internal/resample/area_test.go.
//
// 2×2 source → 1×1 output. Pixels:
//
//	(10,20,30) (40,50,60)
//	(70,80,90) (100,110,120)
//
// avg R = (10+40+70+100)/4 = 55
// avg G = (20+50+80+110)/4 = 65
// avg B = (30+60+90+120)/4 = 75
func TestBoxSimpleCorrectness(t *testing.T) {
	src := decoder.NewImage(2, 2)
	copy(src.Pix, []byte{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120})

	dst := Image(src, 1, 1, Box)

	if dst.Pix[0] != 55 || dst.Pix[1] != 65 || dst.Pix[2] != 75 {
		t.Errorf("got %v, want [55 65 75]", dst.Pix[:3])
	}
}

// TestBoxUniform ports TestAreaAverage_4x4 from wsitools area_test.go.
// 4×4 source all (100,100,100) → 2×2 output all (100,100,100).
func TestBoxUniform(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = 100
	}

	dst := Image(src, 2, 2, Box)

	if dst.Width != 2 || dst.Height != 2 {
		t.Errorf("dst dims: got %dx%d, want 2x2", dst.Width, dst.Height)
	}
	// Only the first bpp*w*h bytes of each row are meaningful; Stride == Width*bpp here.
	for y := 0; y < dst.Height; y++ {
		for x := 0; x < dst.Width; x++ {
			off := y*dst.Stride + x*3
			for c := 0; c < 3; c++ {
				if dst.Pix[off+c] != 100 {
					t.Errorf("pixel [%d,%d] channel %d: got %d, want 100", x, y, c, dst.Pix[off+c])
				}
			}
		}
	}
}

// TestBoxIdentity verifies that a 1:1 resample is a pixel-perfect copy.
func TestBoxIdentity(t *testing.T) {
	src := decoder.NewImage(4, 4)
	for i := range src.Pix {
		src.Pix[i] = byte(i * 7)
	}

	dst := Image(src, 4, 4, Box)

	for y := 0; y < src.Height; y++ {
		for x := 0; x < src.Width; x++ {
			off := y*src.Stride + x*3
			for c := 0; c < 3; c++ {
				if dst.Pix[off+c] != src.Pix[off+c] {
					t.Errorf("pixel [%d,%d] channel %d: got %d, want %d",
						x, y, c, dst.Pix[off+c], src.Pix[off+c])
				}
			}
		}
	}
}
