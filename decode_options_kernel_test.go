package opentile

import (
	"testing"

	"github.com/wsilabs/opentile-go/resample"
)

func TestDecodeConfigKernelDefault(t *testing.T) {
	c := newDecodeConfig(nil)
	if c.kernel != resample.Lanczos {
		t.Errorf("default kernel: got %v, want Lanczos", c.kernel)
	}
}

func TestWithResampleKernel(t *testing.T) {
	c := newDecodeConfig([]DecodeOption{WithResampleKernel(resample.Box)})
	if c.kernel != resample.Box {
		t.Errorf("WithResampleKernel(Box): got %v", c.kernel)
	}
}
