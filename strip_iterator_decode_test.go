//go:build !nocgo

package opentile

import (
	"io"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg" // register JPEG decoder
)

func TestScaledStripsSingleStripWholeSlide(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.imageScaledStrips(
		0,
		Region{Origin: Point{X: 0, Y: 0}, Size: Size{W: 1000, H: 1000}},
		Size{W: 100, H: 100},
		100, // stripHeight = outH → 1 strip
	)
	defer it.Close()

	if it.Strips() != 1 {
		t.Fatalf("Strips: got %d, want 1", it.Strips())
	}

	img, err := it.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if img.Width != 100 || img.Height != 100 {
		t.Errorf("dimensions: got %dx%d, want 100x100", img.Width, img.Height)
	}

	_, err = it.Next()
	if err != io.EOF {
		t.Errorf("second Next: got %v, want io.EOF", err)
	}
}

func TestScaledStripsMultipleStrips(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.imageScaledStrips(
		0,
		Region{Origin: Point{X: 0, Y: 0}, Size: Size{W: 1000, H: 1000}},
		Size{W: 100, H: 200},
		50, // stripHeight = 50 → 4 strips
	)
	defer it.Close()

	if it.Strips() != 4 {
		t.Fatalf("Strips: got %d, want 4", it.Strips())
	}

	for i := 0; i < 4; i++ {
		img, err := it.Next()
		if err != nil {
			t.Fatalf("Next strip %d: %v", i, err)
		}
		if img.Width != 100 || img.Height != 50 {
			t.Errorf("strip %d: got %dx%d, want 100x50", i, img.Width, img.Height)
		}
	}

	_, err := it.Next()
	if err != io.EOF {
		t.Errorf("after final Next: got %v, want io.EOF", err)
	}
}

func TestScaledStripsShortLastStrip(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.imageScaledStrips(
		0,
		Region{Origin: Point{X: 0, Y: 0}, Size: Size{W: 1000, H: 1000}},
		Size{W: 100, H: 130},
		50, // 130 / 50 = 2 strips of 50 + last of 30
	)
	defer it.Close()

	imgs := make([]*decoder.Image, 0, 3)
	for {
		img, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		imgs = append(imgs, img)
	}
	if len(imgs) != 3 {
		t.Fatalf("got %d strips, want 3", len(imgs))
	}
	if imgs[0].Height != 50 || imgs[1].Height != 50 || imgs[2].Height != 30 {
		t.Errorf("strip heights: %d, %d, %d (want 50, 50, 30)",
			imgs[0].Height, imgs[1].Height, imgs[2].Height)
	}
}
