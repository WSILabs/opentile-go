package opentile

import (
	"context"
	"runtime"
	"testing"

	"github.com/wsilabs/opentile-go/resample"
)

func TestStripConfigDefaults(t *testing.T) {
	c := newStripConfig(nil)
	if c.workers != runtime.NumCPU() {
		t.Errorf("default workers: got %d, want %d", c.workers, runtime.NumCPU())
	}
	if c.lookahead != 2 {
		t.Errorf("default lookahead: got %d, want 2", c.lookahead)
	}
	if c.idctScale != 0 {
		t.Errorf("default idctScale: got %d, want 0 (auto)", c.idctScale)
	}
	if c.kernel != resample.Lanczos {
		t.Errorf("default kernel: got %v, want Lanczos", c.kernel)
	}
	if c.ctx != context.Background() {
		t.Errorf("default ctx: got %v, want context.Background()", c.ctx)
	}
}

func TestWithStripWorkers(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripWorkers(8)})
	if c.workers != 8 {
		t.Errorf("workers: got %d, want 8", c.workers)
	}
}

func TestWithStripWorkersMinimum(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripWorkers(0)})
	if c.workers != 1 {
		t.Errorf("workers (0 → 1 clamp): got %d, want 1", c.workers)
	}
	c2 := newStripConfig([]StripOption{WithStripWorkers(-5)})
	if c2.workers != 1 {
		t.Errorf("workers (negative → 1 clamp): got %d, want 1", c2.workers)
	}
}

func TestWithStripLookahead(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripLookahead(5)})
	if c.lookahead != 5 {
		t.Errorf("lookahead: got %d, want 5", c.lookahead)
	}
}

func TestWithStripLookaheadZero(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripLookahead(0)})
	if c.lookahead != 0 {
		t.Errorf("lookahead 0 should be allowed (disable lookahead): got %d", c.lookahead)
	}
}

func TestWithStripIDCTScale(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripIDCTScale(4)})
	if c.idctScale != 4 {
		t.Errorf("idctScale: got %d, want 4", c.idctScale)
	}
}

func TestWithStripKernel(t *testing.T) {
	c := newStripConfig([]StripOption{WithStripKernel(resample.Box)})
	if c.kernel != resample.Box {
		t.Errorf("kernel: got %v, want Box", c.kernel)
	}
}

func TestWithStripContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newStripConfig([]StripOption{WithStripContext(ctx)})
	if c.ctx != ctx {
		t.Errorf("ctx: not set correctly")
	}
}

func TestMultipleStripOptions(t *testing.T) {
	c := newStripConfig([]StripOption{
		WithStripWorkers(4),
		WithStripLookahead(8),
		WithStripIDCTScale(2),
		WithStripKernel(resample.Box),
	})
	if c.workers != 4 || c.lookahead != 8 || c.idctScale != 2 || c.kernel != resample.Box {
		t.Errorf("multi-option: got %+v", c)
	}
}
