package decoder

import "testing"

func TestNewImageRGB(t *testing.T) {
	im := NewImage(100, 50)
	if im.Width != 100 || im.Height != 50 {
		t.Errorf("dimensions: got %dx%d want 100x50", im.Width, im.Height)
	}
	if im.Format != PixelFormatRGB {
		t.Errorf("default format: got %d want PixelFormatRGB", im.Format)
	}
	if im.Stride != 100*3 {
		t.Errorf("RGB stride: got %d want %d", im.Stride, 100*3)
	}
	if len(im.Pix) != im.Stride*im.Height {
		t.Errorf("Pix size: got %d want %d", len(im.Pix), im.Stride*im.Height)
	}
}

func TestNewImageFormatRGBA(t *testing.T) {
	im := NewImageFormat(100, 50, PixelFormatRGBA)
	if im.Format != PixelFormatRGBA {
		t.Errorf("format: got %d want PixelFormatRGBA", im.Format)
	}
	if im.Stride != 100*4 {
		t.Errorf("RGBA stride: got %d want %d", im.Stride, 100*4)
	}
	if len(im.Pix) != im.Stride*im.Height {
		t.Errorf("Pix size: got %d want %d", len(im.Pix), im.Stride*im.Height)
	}
}

func TestNewImageZeroDimensions(t *testing.T) {
	im := NewImage(0, 0)
	if im.Width != 0 || im.Height != 0 || len(im.Pix) != 0 {
		t.Errorf("zero dimensions: got %+v", im)
	}
}
