package opentile

import (
	"context"
	"runtime"

	"github.com/wsilabs/opentile-go/resample"
)

// StripOption configures a ScaledStrips iterator. See WithStripWorkers,
// WithStripLookahead, WithStripIDCTScale, WithStripKernel,
// WithStripContext.
type StripOption func(*stripConfig)

// stripConfig is the resolved option set.
//
// Defaults:
//   - workers: runtime.NumCPU()
//   - lookahead: 2
//   - idctScale: 0 (auto-select at iterator init)
//   - kernel: resample.Lanczos
//   - ctx: context.Background()
type stripConfig struct {
	workers   int
	lookahead int
	idctScale int
	kernel    resample.Kernel
	ctx       context.Context
}

func newStripConfig(opts []StripOption) stripConfig {
	c := stripConfig{
		workers:   runtime.NumCPU(),
		lookahead: 2,
		idctScale: 0, // auto
		kernel:    resample.Lanczos,
		ctx:       context.Background(),
	}
	for _, o := range opts {
		o(&c)
	}
	if c.workers < 1 {
		c.workers = 1
	}
	if c.lookahead < 0 {
		c.lookahead = 0
	}
	return c
}

// WithStripWorkers sets the number of parallel source-tile decode
// workers. Default: runtime.NumCPU(). Values < 1 are clamped to 1.
func WithStripWorkers(n int) StripOption {
	return func(c *stripConfig) { c.workers = n }
}

// WithStripLookahead sets how many strips ahead the iterator
// pre-fetches source tiles for. Default: 2. 0 disables lookahead
// entirely (workers idle between Next() calls). Higher values
// trade memory for steady-state throughput on slow consumers.
func WithStripLookahead(strips int) StripOption {
	return func(c *stripConfig) { c.lookahead = strips }
}

// WithStripIDCTScale overrides the auto-selected codec-domain downscale
// factor. Valid: 1, 2, 4, 8. Default 0 = auto-select based on output
// downsample. Honored by scale-capable codecs (jpeg, jpeg2000, htj2k);
// other codecs ignore it (decode at full level resolution).
func WithStripIDCTScale(scale int) StripOption {
	return func(c *stripConfig) { c.idctScale = scale }
}

// WithStripKernel sets the resample kernel applied after the
// source-tile decode step (when IDCT alone doesn't reach the
// target output dims). Default: resample.Lanczos.
func WithStripKernel(k resample.Kernel) StripOption {
	return func(c *stripConfig) { c.kernel = k }
}

// WithStripContext binds the iterator's worker pool to a
// cancellation context. Default: context.Background().
//
// Cancelling ctx aborts in-flight Next() calls with ctx.Err().
// Workers also respond to *StripIterator.Close().
func WithStripContext(ctx context.Context) StripOption {
	return func(c *stripConfig) { c.ctx = ctx }
}
