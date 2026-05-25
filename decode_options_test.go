package opentile

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestDecodeConfigDefaults(t *testing.T) {
	c := newDecodeConfig(nil)
	if c.format != decoder.PixelFormatRGB {
		t.Errorf("default format: got %v, want PixelFormatRGB", c.format)
	}
	if c.scale != 1 {
		t.Errorf("default scale: got %d, want 1", c.scale)
	}
}

func TestWithFormat(t *testing.T) {
	c := newDecodeConfig([]DecodeOption{WithFormat(decoder.PixelFormatRGBA)})
	if c.format != decoder.PixelFormatRGBA {
		t.Errorf("WithFormat(RGBA): got %v", c.format)
	}
}

func TestWithScale(t *testing.T) {
	c := newDecodeConfig([]DecodeOption{WithScale(4)})
	if c.scale != 4 {
		t.Errorf("WithScale(4): got %d", c.scale)
	}
}

func TestMultipleOptions(t *testing.T) {
	c := newDecodeConfig([]DecodeOption{
		WithFormat(decoder.PixelFormatRGBA),
		WithScale(2),
	})
	if c.format != decoder.PixelFormatRGBA || c.scale != 2 {
		t.Errorf("multi-option: got format=%v scale=%d", c.format, c.scale)
	}
}
